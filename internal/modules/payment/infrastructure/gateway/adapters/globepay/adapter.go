package globepayadapter

import (
	"context"
	"fmt"
	"strings"

	gatewaycommon "github.com/dujiao-next/internal/modules/payment/infrastructure/gateway/common"

	paymentcontract "github.com/dujiao-next/internal/modules/payment/contract"

	"github.com/dujiao-next/internal/constants"
	"github.com/dujiao-next/internal/modules/payment/infrastructure/gateway/globepay"
	"github.com/dujiao-next/internal/shared/jsonmap"
	"github.com/dujiao-next/internal/shared/money"

	"github.com/shopspring/decimal"
)

// globepayAdapter 是 globepay 网关的 paymentcontract.GatewayProvider + paymentcontract.GatewayCallbackVerifier 实现。
// globepay 用同步 form/JSON POST 回调，不是 JSON webhook，所以不实现 paymentcontract.GatewayWebhooker。
type globepayAdapter struct{}

// NewGlobepayAdapter 实例化 globepay adapter。
func NewGlobepayAdapter() paymentcontract.GatewayProvider { return &globepayAdapter{} }

var (
	_ paymentcontract.GatewayProvider         = (*globepayAdapter)(nil)
	_ paymentcontract.GatewayCallbackVerifier = (*globepayAdapter)(nil)
)

// Type 返回 provider 标识。
func (a *globepayAdapter) Type() string {
	return constants.PaymentProviderGlobepay + ":"
}

// ValidateConfig 验证 channel.ConfigJSON。
func (a *globepayAdapter) ValidateConfig(raw jsonmap.JSON, channelType string) error {
	if channelType != "" && !globepay.IsSupportedChannelType(channelType) {
		return fmt.Errorf("%w: globepay channel_type %s", paymentcontract.ErrGatewayUnsupportedChannel, channelType)
	}
	cfg, err := globepay.ParseConfig(raw)
	if err != nil {
		return fmt.Errorf("%w: %v", paymentcontract.ErrGatewayConfigInvalid, err)
	}
	if err := globepay.ValidateConfig(cfg); err != nil {
		return fmt.Errorf("%w: %v", paymentcontract.ErrGatewayConfigInvalid, err)
	}
	return nil
}

// CreatePayment 创建支付。
func (a *globepayAdapter) CreatePayment(ctx context.Context, raw jsonmap.JSON, input paymentcontract.GatewayCreateInput) (*paymentcontract.GatewayCreateResult, error) {
	if !globepay.IsSupportedChannelType(input.ChannelType) {
		return nil, fmt.Errorf("%w: globepay channel_type %s", paymentcontract.ErrGatewayUnsupportedChannel, input.ChannelType)
	}
	cfg, err := globepay.ParseConfig(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", paymentcontract.ErrGatewayConfigInvalid, err)
	}

	notifyURL := strings.TrimSpace(input.NotifyURL)
	if notifyURL == "" {
		notifyURL = cfg.NotifyURL
	}
	returnURL := strings.TrimSpace(input.ReturnURL)
	if returnURL == "" {
		returnURL = cfg.ReturnURL
	}
	returnURL = gatewaycommon.AppendQueryParams(returnURL, input.ReturnURLQuery)

	interactionMode, _ := input.Extra["interaction_mode"].(string)
	native := globepay.CreateInput{
		OrderNo:         input.OrderNo,
		Amount:          input.Amount.Decimal.String(),
		Subject:         input.Subject,
		ChannelType:     input.ChannelType,
		InteractionMode: interactionMode,
		NotifyURL:       notifyURL,
		ReturnURL:       returnURL,
	}

	result, err := globepay.CreatePayment(ctx, cfg, native)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", paymentcontract.ErrGatewayRequestFailed, err)
	}

	payload := jsonmap.JSON{}
	if result.Raw != nil {
		payload = jsonmap.JSON(result.Raw)
	}

	return &paymentcontract.GatewayCreateResult{
		ProviderRef:  result.TradeNo,
		RedirectURL:  result.PayURL,
		QRCodeURL:    result.QRCode,
		Payload:      payload,
		AmountSent:   result.ActualAmount,
		CurrencySent: result.ActualCurrency,
	}, nil
}

// VerifyCallback 实现 paymentcontract.GatewayCallbackVerifier。
func (a *globepayAdapter) VerifyCallback(raw jsonmap.JSON, form map[string][]string, body []byte) (*paymentcontract.GatewayCallbackResult, error) {
	cfg, err := globepay.ParseConfig(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", paymentcontract.ErrGatewayConfigInvalid, err)
	}

	data := make(map[string]string, len(form))
	for k, v := range form {
		if len(v) > 0 {
			data[k] = v[0]
		}
	}

	if err := globepay.VerifyCallback(cfg, data); err != nil {
		return nil, fmt.Errorf("%w: %v", paymentcontract.ErrGatewaySignatureInvalid, err)
	}

	orderNo := data["partner_order_id"]
	providerRef := data["order_id"]

	// Globepay 回调字段：
	// - real_fee: 实际支付金额（单位：分）
	// - currency: 币种（GBP/CNY）
	amountStr := data["real_fee"]
	currency := strings.ToUpper(strings.TrimSpace(data["currency"]))

	amount := money.Amount{}
	if s := strings.TrimSpace(amountStr); s != "" {
		if d, parseErr := decimal.NewFromString(s); parseErr == nil {
			// Globepay 回调金额单位是分
			amount = money.FromDecimal(d.Div(decimal.NewFromInt(100)))
		}
	}

	return &paymentcontract.GatewayCallbackResult{
		OrderNo:     orderNo,
		ProviderRef: providerRef,
		Status:      constants.PaymentStatusSuccess,
		Amount:      amount,
		Currency:    currency, // 使用回调返回的币种（GBP/CNY）
		PaidAt:      nil,
		Payload:     gatewaycommon.FormToJSON(form),
	}, nil
}
