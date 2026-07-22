package provider

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dujiao-next/internal/constants"
	"github.com/dujiao-next/internal/payment/binancepay"
	"github.com/dujiao-next/internal/shared/jsonmap"
	"github.com/dujiao-next/internal/shared/money"

	"github.com/shopspring/decimal"
)

type binancepayAdapter struct{}

func NewBinancepayAdapter() Provider { return &binancepayAdapter{} }

var (
	_ Provider  = (*binancepayAdapter)(nil)
	_ Webhooker = (*binancepayAdapter)(nil)
)

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

func (a *binancepayAdapter) ValidateConfig(raw jsonmap.JSON, interactionMode string) error {
	if interactionMode != "" && strings.ToLower(strings.TrimSpace(interactionMode)) != constants.PaymentInteractionRedirect {
		return fmt.Errorf("%w: binancepay only supports redirect interaction_mode", ErrConfigInvalid)
	}
	_, err := a.parseConfig(raw)
	return err
}

func (a *binancepayAdapter) CreatePayment(ctx context.Context, raw jsonmap.JSON, input CreateInput) (*CreateResult, error) {
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
			return nil, fmt.Errorf("%w: %v", ErrConfigInvalid, convErr)
		}
		payAmount = convAmount
		payCurrency = convCurrency
		converted = true
	}

	returnURL := strings.TrimSpace(input.ReturnURL)
	if returnURL == "" {
		returnURL = cfg.ReturnURL
	}
	returnURL = appendQueryParams(returnURL, input.ReturnURLQuery)

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

	return &CreateResult{
		ProviderRef:  result.PrepayID,
		RedirectURL:  result.UniversalURL,
		Payload:      payload,
		AmountSent:   payAmount,
		CurrencySent: payCurrency,
	}, nil
}

func (a *binancepayAdapter) ParseWebhook(ctx context.Context, raw jsonmap.JSON, headers map[string]string, body []byte, now time.Time) (*WebhookResult, error) {
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
		return &WebhookResult{Status: result.BizStatus}, nil
	}

	amount := money.Amount{}
	if s := strings.TrimSpace(result.Amount); s != "" {
		if parsed, parseErr := decimal.NewFromString(s); parseErr == nil {
			amount = money.FromDecimal(parsed)
		}
	}

	return &WebhookResult{
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
		return fmt.Errorf("%w: %v", ErrConfigInvalid, err)
	case errors.Is(err, binancepay.ErrRequestFailed):
		return fmt.Errorf("%w: %v", ErrRequestFailed, err)
	case errors.Is(err, binancepay.ErrResponseInvalid):
		return fmt.Errorf("%w: %v", ErrResponseInvalid, err)
	case errors.Is(err, binancepay.ErrSignatureInvalid):
		return fmt.Errorf("%w: %v", ErrSignatureInvalid, err)
	default:
		return err
	}
}
