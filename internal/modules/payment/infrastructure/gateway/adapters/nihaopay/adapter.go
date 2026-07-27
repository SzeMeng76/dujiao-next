package nihaopayadapter

import (
	"context"
	"fmt"
	"strings"

	paymentcontract "github.com/dujiao-next/internal/modules/payment/contract"
	gatewaycommon "github.com/dujiao-next/internal/modules/payment/infrastructure/gateway/common"

	"github.com/dujiao-next/internal/constants"
	"github.com/dujiao-next/internal/modules/payment/infrastructure/gateway/nihaopay"
	"github.com/dujiao-next/internal/shared/jsonmap"
	"github.com/dujiao-next/internal/shared/money"

	"github.com/shopspring/decimal"
)

// nihaopayadapter 是 nihaopay 网关的 paymentcontract.GatewayProvider 实现。
// Nihaopay 使用 IPN 回调（POST form data），需要实现 GatewayCallbackVerifier。
type nihaopayadapter struct{}

// NewNihaopayAdapter 实例化 nihaopay adapter。
func NewNihaopayAdapter() paymentcontract.GatewayProvider { return &nihaopayadapter{} }

var (
	_ paymentcontract.GatewayProvider         = (*nihaopayadapter)(nil)
	_ paymentcontract.GatewayCallbackVerifier = (*nihaopayadapter)(nil)
)

// Type 返回 provider 标识。
func (a *nihaopayadapter) Type() string {
	return constants.PaymentProviderNihaopay + ":"
}

// ValidateConfig 验证 channel.ConfigJSON。
func (a *nihaopayadapter) ValidateConfig(raw jsonmap.JSON, channelType string) error {
	if channelType != "" && !nihaopay.IsSupportedChannelType(channelType) {
		return fmt.Errorf("%w: nihaopay channel_type %s", paymentcontract.ErrGatewayUnsupportedChannel, channelType)
	}
	cfg, err := nihaopay.ParseConfig(raw)
	if err != nil {
		return fmt.Errorf("%w: %v", paymentcontract.ErrGatewayConfigInvalid, err)
	}
	if err := nihaopay.ValidateConfig(cfg); err != nil {
		return fmt.Errorf("%w: %v", paymentcontract.ErrGatewayConfigInvalid, err)
	}
	return nil
}

// CreatePayment 创建支付。
func (a *nihaopayadapter) CreatePayment(ctx context.Context, raw jsonmap.JSON, input paymentcontract.GatewayCreateInput) (*paymentcontract.GatewayCreateResult, error) {
	if !nihaopay.IsSupportedChannelType(input.ChannelType) {
		return nil, fmt.Errorf("%w: nihaopay channel_type %s", paymentcontract.ErrGatewayUnsupportedChannel, input.ChannelType)
	}
	cfg, err := nihaopay.ParseConfig(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", paymentcontract.ErrGatewayConfigInvalid, err)
	}

	// callback_url 必填：支付完成后用户浏览器重定向 (GET)
	callbackURL := strings.TrimSpace(input.ReturnURL)
	if callbackURL == "" {
		callbackURL = cfg.ReturnURL
	}
	callbackURL = gatewaycommon.AppendQueryParams(callbackURL, input.ReturnURLQuery)

	// ipn_url 必填：异步通知 (POST)
	ipnURL := strings.TrimSpace(input.NotifyURL)
	if ipnURL == "" {
		ipnURL = cfg.NotifyURL
	}
	if ipnURL == "" {
		return nil, fmt.Errorf("%w: ipn_url is required but not configured", paymentcontract.ErrGatewayConfigInvalid)
	}

	native := nihaopay.CreateInput{
		OrderNo:     input.OrderNo,
		Amount:      input.Amount.Decimal.StringFixed(2),
		Currency:    input.Currency,
		Subject:     input.Subject,
		ChannelType: input.ChannelType,
		CallbackURL: callbackURL, // 必填
		IPNUrl:      ipnURL,      // 可选
		Reference:   input.OrderNo,
	}

	result, err := nihaopay.CreatePayment(ctx, cfg, native)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", paymentcontract.ErrGatewayRequestFailed, err)
	}

	payload := jsonmap.JSON{}
	if result.Raw != nil {
		payload = jsonmap.JSON(result.Raw)
	}

	// 存储表单数据到 payment.gateway_data，供 redirect handler 使用
	gatewayData := jsonmap.JSON{
		"form_action": result.FormAction,
		"form_method": result.FormMethod,
		"form_params": result.FormParams,
	}

	// 构造 redirect URL 指向我们的表单提交页面
	formRedirectURL := fmt.Sprintf("/api/v1/payments/%d/nihaopay-redirect", input.PaymentID)

	return &paymentcontract.GatewayCreateResult{
		ProviderRef:  result.TransactionID,
		RedirectURL:  formRedirectURL,
		Payload:      payload,
		GatewayData:  gatewayData,
		AmountSent:   input.Amount.Decimal.StringFixed(2),
		CurrencySent: input.Currency,
	}, nil
}

// VerifyCallback 实现 paymentcontract.GatewayCallbackVerifier。
func (a *nihaopayadapter) VerifyCallback(raw jsonmap.JSON, form map[string][]string, body []byte) (*paymentcontract.GatewayCallbackResult, error) {
	cfg, err := nihaopay.ParseConfig(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", paymentcontract.ErrGatewayConfigInvalid, err)
	}

	data := make(map[string]string, len(form))
	for k, v := range form {
		if len(v) > 0 {
			data[k] = v[0]
		}
	}

	if err := nihaopay.VerifyCallback(cfg, data); err != nil {
		return nil, fmt.Errorf("%w: %v", paymentcontract.ErrGatewaySignatureInvalid, err)
	}

	orderNo := data["note"]
	providerRef := data["transaction_id"]
	amountStr := data["amount"]
	currency := data["currency"]
	status := data["status"]

	amount := money.Amount{}
	if s := strings.TrimSpace(amountStr); s != "" {
		if d, parseErr := decimal.NewFromString(s); parseErr == nil {
			amount = money.FromDecimal(d)
		}
	}

	gatewayStatus := constants.PaymentStatusPending
	switch strings.ToLower(status) {
	case "success":
		gatewayStatus = constants.PaymentStatusSuccess
	case "failed":
		gatewayStatus = constants.PaymentStatusFailed
	}

	return &paymentcontract.GatewayCallbackResult{
		OrderNo:     orderNo,
		ProviderRef: providerRef,
		Status:      gatewayStatus,
		Amount:      amount,
		Currency:    currency,
		PaidAt:      nil,
		Payload:     gatewaycommon.FormToJSON(form),
	}, nil
}
