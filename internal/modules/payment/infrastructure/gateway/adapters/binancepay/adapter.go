package binancepayadapter

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	gatewaycommon "github.com/dujiao-next/internal/modules/payment/infrastructure/gateway/common"

	paymentcontract "github.com/dujiao-next/internal/modules/payment/contract"

	"github.com/dujiao-next/internal/constants"
	"github.com/dujiao-next/internal/modules/payment/infrastructure/gateway/binancepay"
	"github.com/dujiao-next/internal/shared/jsonmap"
	"github.com/dujiao-next/internal/shared/money"

	"github.com/shopspring/decimal"
)

// binancepayAdapter 是 binancepay 网关的 paymentcontract.GatewayProvider/paymentcontract.GatewayWebhooker 实现。
// binancepay 只有异步 webhook 通知，没有同步 form callback，所以不实现 paymentcontract.GatewayCallbackVerifier。
type binancepayAdapter struct{}

// NewBinancepayAdapter 实例化 binancepay adapter。
func NewBinancepayAdapter() paymentcontract.GatewayProvider { return &binancepayAdapter{} }

var (
	_ paymentcontract.GatewayProvider  = (*binancepayAdapter)(nil)
	_ paymentcontract.GatewayWebhooker = (*binancepayAdapter)(nil)
)

// Type 返回 provider 标识。
func (a *binancepayAdapter) Type() string {
	return constants.PaymentProviderOfficial + ":" + constants.PaymentChannelTypeBinancepay
}

func (a *binancepayAdapter) parseConfig(raw jsonmap.JSON) (*binancepay.Config, error) {
	cfg, err := binancepay.ParseConfig(raw)
	if err != nil {
		return nil, mapBinancepayError(err)
	}
	if err := binancepay.ValidateConfig(cfg); err != nil {
		return nil, mapBinancepayError(err)
	}
	return cfg, nil
}

// ValidateConfig 验证 channel.ConfigJSON。binancepay 只支持 redirect 交互模式。
func (a *binancepayAdapter) ValidateConfig(raw jsonmap.JSON, interactionMode string) error {
	if interactionMode != "" && strings.ToLower(strings.TrimSpace(interactionMode)) != constants.PaymentInteractionRedirect {
		return fmt.Errorf("%w: binancepay only supports redirect interaction_mode", paymentcontract.ErrGatewayConfigInvalid)
	}
	_, err := a.parseConfig(raw)
	return err
}

// CreatePayment 创建支付。
func (a *binancepayAdapter) CreatePayment(ctx context.Context, raw jsonmap.JSON, input paymentcontract.GatewayCreateInput) (*paymentcontract.GatewayCreateResult, error) {
	cfg, err := a.parseConfig(raw)
	if err != nil {
		return nil, err
	}

	originalAmount := input.Amount.Decimal.String()
	originalCurrency := input.Currency
	payAmount := originalAmount
	payCurrency := originalCurrency
	converted := false
	if cfg.NeedsCurrencyConversion() {
		convAmount, convCurrency, convErr := cfg.ConvertAmount(payAmount, payCurrency, 8)
		if convErr != nil {
			return nil, fmt.Errorf("%w: %v", paymentcontract.ErrGatewayConfigInvalid, convErr)
		}
		payAmount = convAmount
		payCurrency = convCurrency
		converted = true
	}

	returnURL := strings.TrimSpace(input.ReturnURL)
	if returnURL == "" {
		returnURL = cfg.ReturnURL
	}
	returnURL = gatewaycommon.AppendQueryParams(returnURL, input.ReturnURLQuery)

	cancelURL, _ := input.Extra["cancel_url"].(string)

	terminalType := "WEB"
	if t, ok := input.Extra["terminal_type"].(string); ok && t != "" {
		terminalType = t
	}

	native := binancepay.CreateInput{
		OrderNo:      input.OrderNo,
		Amount:       payAmount,
		Currency:     payCurrency,
		Description:  input.Subject,
		ReturnURL:    returnURL,
		CancelURL:    cancelURL,
		TerminalType: terminalType,
	}
	result, err := binancepay.CreatePayment(ctx, cfg, native)
	if err != nil {
		return nil, mapBinancepayError(err)
	}

	payload := jsonmap.JSON{}
	if result.Raw != nil {
		payload = jsonmap.JSON(result.Raw)
	}
	if converted {
		payload["exchange_rate"] = strings.TrimSpace(cfg.ExchangeRate)
		payload["original_amount"] = originalAmount
		payload["original_currency"] = originalCurrency
	}

	return &paymentcontract.GatewayCreateResult{
		ProviderRef:  result.PrepayID,
		RedirectURL:  result.UniversalURL,
		Payload:      payload,
		AmountSent:   payAmount,
		CurrencySent: payCurrency,
	}, nil
}

// ParseWebhook 验签并解析 webhook(实现 paymentcontract.GatewayWebhooker)。
func (a *binancepayAdapter) ParseWebhook(ctx context.Context, raw jsonmap.JSON, headers map[string]string, body []byte, now time.Time) (*paymentcontract.GatewayWebhookResult, error) {
	cfg, err := a.parseConfig(raw)
	if err != nil {
		return nil, err
	}

	result, err := binancepay.VerifyAndParseWebhook(ctx, cfg, headers, body, now)
	if err != nil {
		return nil, mapBinancepayError(err)
	}

	// bizType != PAY 是非支付通知，返回空 result 表示无需处理
	if !strings.EqualFold(result.BizType, "PAY") {
		return &paymentcontract.GatewayWebhookResult{Status: result.BizStatus}, nil
	}

	amount := money.Amount{}
	if s := strings.TrimSpace(result.Amount); s != "" {
		if parsed, parseErr := decimal.NewFromString(s); parseErr == nil {
			amount = money.FromDecimal(parsed)
		}
	}

	return &paymentcontract.GatewayWebhookResult{
		OrderNo:     result.OrderNo,
		ProviderRef: result.BizID,
		Status:      result.Status,
		Amount:      amount,
		Currency:    strings.ToUpper(strings.TrimSpace(result.Currency)),
		PaidAt:      result.PaidAt,
		Payload:     jsonmap.JSON(result.Raw),
	}, nil
}

func mapBinancepayError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, binancepay.ErrConfigInvalid):
		return fmt.Errorf("%w: %v", paymentcontract.ErrGatewayConfigInvalid, err)
	case errors.Is(err, binancepay.ErrRequestFailed):
		return fmt.Errorf("%w: %v", paymentcontract.ErrGatewayRequestFailed, err)
	case errors.Is(err, binancepay.ErrResponseInvalid):
		return fmt.Errorf("%w: %v", paymentcontract.ErrGatewayResponseInvalid, err)
	case errors.Is(err, binancepay.ErrSignatureInvalid):
		return fmt.Errorf("%w: %v", paymentcontract.ErrGatewaySignatureInvalid, err)
	default:
		return err
	}
}
