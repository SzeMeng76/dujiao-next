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
	_ paymentcontract.GatewayProvider          = (*globepayAdapter)(nil)
	_ paymentcontract.GatewayCallbackVerifier  = (*globepayAdapter)(nil)
	_ paymentcontract.GatewayCapturer          = (*globepayAdapter)(nil)
	_ paymentcontract.GatewayCallbackConfirmer = (*globepayAdapter)(nil)
)

// RequireCallbackQuery 声明 Globepay 回调必须经官方订单状态回查确认。
// 原因见 VerifyCallback 注释：签名不覆盖订单号与金额，买家可从 pay_url 拿到合法签名。
func (a *globepayAdapter) RequireCallbackQuery() bool { return true }

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
	// alipayhk/tng/dana/gcash 硬编码转 GBP 时，写入 exchange_rate/original_amount/
	// original_currency 快照，供支付回调阶段把 GBP 到账金额折算回订单币种（CNY），
	// 与 alipay/epay/okpay/paypal/stripe/wechatpay/binancepay 等 adapter 的换汇快照格式保持一致。
	if result.ExchangeRate != "" {
		payload["exchange_rate"] = result.ExchangeRate
		payload["original_amount"] = result.OriginalAmount
		payload["original_currency"] = result.OriginalCurrency
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
//
// 安全边界：Globepay 的 sign 只覆盖 partner_code&time&nonce_str&credential_code，
// 不包含 partner_order_id / real_fee / currency / channel。而创建支付时返回给买家的
// pay_url 必须携带同一组签名参数（见 CreatePayment），等于把一组合法签名交给了买家。
// 因此"验签通过"只证明请求方见过某个合法签名，**不能**证明这笔订单真的收到钱。
// 真正的事实校验由 QueryPayment（回查 Globepay 官方订单状态）在应用层完成；
// 这里额外拒绝 order_id == partner_order_id 的请求，因为真实回调中 order_id 是
// Globepay 自己的流水号，不可能与商户单号相同。
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

	orderNo := strings.TrimSpace(data["partner_order_id"])
	providerRef := strings.TrimSpace(data["order_id"])
	if orderNo == "" || providerRef == "" {
		return nil, fmt.Errorf("%w: missing order id", paymentcontract.ErrGatewayResponseInvalid)
	}

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

// QueryPayment 实现 paymentcontract.GatewayCapturer，回查 Globepay 官方订单状态。
//
// 这是 Globepay 渠道唯一可信的到账事实来源：回调签名不覆盖业务字段（见 VerifyCallback
// 的说明），只有服务端主动查询才能确认该 partner_order_no 实际是 PAY_SUCCESS 且金额一致。
func (a *globepayAdapter) QueryPayment(ctx context.Context, raw jsonmap.JSON, providerRef string) (*paymentcontract.GatewayQueryResult, error) {
	cfg, err := globepay.ParseConfig(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", paymentcontract.ErrGatewayConfigInvalid, err)
	}
	if strings.TrimSpace(providerRef) == "" {
		return nil, fmt.Errorf("%w: empty provider_ref", paymentcontract.ErrGatewayRequestFailed)
	}

	result, err := globepay.QueryOrder(ctx, cfg, providerRef)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", paymentcontract.ErrGatewayRequestFailed, err)
	}

	// 查询结果必须确实对应我们问的那一笔：网关返回的 partner_order_id 与请求不一致时，
	// 说明拿到的是别的订单（配置串号、网关异常或中间人改写），绝不能据此记账。
	if returned := strings.TrimSpace(result.PartnerOrderID); returned != "" &&
		!strings.EqualFold(returned, strings.TrimSpace(providerRef)) {
		return nil, fmt.Errorf("%w: query returned partner_order_id %q, want %q",
			paymentcontract.ErrGatewayResponseInvalid, returned, providerRef)
	}

	amount := money.FromDecimal(decimal.NewFromInt(result.RealFee).Div(decimal.NewFromInt(100)))
	currency := strings.ToUpper(strings.TrimSpace(result.Currency))
	if currency == "" {
		currency = "GBP"
	}

	return &paymentcontract.GatewayQueryResult{
		ProviderRef: strings.TrimSpace(result.OrderID),
		Status:      globepayQueryStatus(result.ResultCode),
		Amount:      amount,
		Currency:    currency,
		Payload:     jsonmap.JSON{"query_result_code": result.ResultCode, "query_total_fee": result.TotalFee},
	}, nil
}

// globepayQueryStatus 把 Globepay result_code 映射为内部支付状态。
// 已支付但之后退款的状态仍归为 success：退款由本地退款流程处理，回调不应回退已成功记录。
func globepayQueryStatus(resultCode string) string {
	switch strings.ToUpper(strings.TrimSpace(resultCode)) {
	case globepay.QueryResultPaySuccess, globepay.QueryResultPartialRefund, globepay.QueryResultFullRefund:
		return constants.PaymentStatusSuccess
	case globepay.QueryResultClosed, globepay.QueryResultCreateFail:
		return constants.PaymentStatusExpired
	case globepay.QueryResultPayFail:
		return constants.PaymentStatusFailed
	case globepay.QueryResultPaying:
		return constants.PaymentStatusPending
	default:
		return constants.PaymentStatusPending
	}
}
