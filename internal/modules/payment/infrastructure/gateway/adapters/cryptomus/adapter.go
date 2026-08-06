package cryptomusadapter

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	gatewaycommon "github.com/dujiao-next/internal/modules/payment/infrastructure/gateway/common"

	paymentcontract "github.com/dujiao-next/internal/modules/payment/contract"

	"github.com/dujiao-next/internal/constants"
	"github.com/dujiao-next/internal/modules/payment/infrastructure/gateway/cryptomus"
	"github.com/dujiao-next/internal/shared/jsonmap"
	"github.com/dujiao-next/internal/shared/money"

	"github.com/shopspring/decimal"
)

// cryptomusAdapter 是 Cryptomus 网关的 paymentcontract.GatewayProvider + GatewayWebhooker + GatewayCapturer 实现。
// 和 HashPay 一样：下单不指定币种/网络，付款人在托管收银台自选，只支持 redirect 交互模式；
// 回调是独立 webhook（body 内嵌 sign 字段做 MD5 验签），走 /payments/webhook/cryptomus 专属路由。
type cryptomusAdapter struct{}

// NewCryptomusAdapter 实例化 Cryptomus adapter。
func NewCryptomusAdapter() paymentcontract.GatewayProvider { return &cryptomusAdapter{} }

var (
	_ paymentcontract.GatewayProvider  = (*cryptomusAdapter)(nil)
	_ paymentcontract.GatewayWebhooker = (*cryptomusAdapter)(nil)
	_ paymentcontract.GatewayCapturer  = (*cryptomusAdapter)(nil)
)

// Type 返回 provider 标识。Cryptomus 是单 provider 收银台网关，channelType 部分为空。
func (a *cryptomusAdapter) Type() string {
	return constants.PaymentProviderCryptomus + ":"
}

func (a *cryptomusAdapter) parseConfig(raw jsonmap.JSON) (*cryptomus.Config, error) {
	cfg, err := cryptomus.ParseConfig(raw)
	if err != nil {
		return nil, mapCryptomusError(err)
	}
	if err := cryptomus.ValidateConfig(cfg); err != nil {
		return nil, mapCryptomusError(err)
	}
	return cfg, nil
}

// ValidateConfig 验证 channel.ConfigJSON。channel_type 固定为空或 cryptomus。
func (a *cryptomusAdapter) ValidateConfig(raw jsonmap.JSON, channelType string) error {
	normalized := strings.ToLower(strings.TrimSpace(channelType))
	if normalized != "" && normalized != constants.PaymentProviderCryptomus {
		return fmt.Errorf("%w: cryptomus channel_type %s", paymentcontract.ErrGatewayUnsupportedChannel, channelType)
	}
	_, err := a.parseConfig(raw)
	return err
}

// CreatePayment 创建 Cryptomus 发票并返回托管收银台跳转地址。
func (a *cryptomusAdapter) CreatePayment(ctx context.Context, raw jsonmap.JSON, input paymentcontract.GatewayCreateInput) (*paymentcontract.GatewayCreateResult, error) {
	cfg, err := a.parseConfig(raw)
	if err != nil {
		return nil, err
	}
	// 下单响应没有收款地址（付款人在收银台自选币种/网络后才生成），无法渲染站内二维码。
	if mode, ok := input.Extra["interaction_mode"].(string); ok && strings.ToLower(strings.TrimSpace(mode)) == constants.PaymentInteractionQR {
		return nil, fmt.Errorf("%w: cryptomus only supports redirect interaction_mode", paymentcontract.ErrGatewayConfigInvalid)
	}

	notifyURL := gatewaycommon.PickFirstNonEmpty(input.NotifyURL, cfg.NotifyURL)
	returnURL := gatewaycommon.PickFirstNonEmpty(input.ReturnURL, cfg.ReturnURL)
	returnURL = gatewaycommon.AppendQueryParams(returnURL, input.ReturnURLQuery)

	currencySent := strings.ToUpper(strings.TrimSpace(input.Currency))
	if cfg.Currency != "" {
		currencySent = cfg.Currency
	}

	result, err := cryptomus.CreatePayment(ctx, cfg, cryptomus.CreateInput{
		OrderNo:   strings.TrimSpace(input.OrderNo),
		Amount:    input.Amount.Decimal.String(),
		Currency:  currencySent,
		NotifyURL: notifyURL,
		ReturnURL: returnURL,
	})
	if err != nil {
		return nil, mapCryptomusError(err)
	}

	payload := jsonmap.JSON{}
	for key, value := range result.Raw {
		payload[key] = value
	}

	return &paymentcontract.GatewayCreateResult{
		ProviderRef:  result.UUID,
		RedirectURL:  result.CheckoutURL,
		QRCodeURL:    result.CheckoutURL,
		Payload:      payload,
		AmountSent:   input.Amount.Decimal.String(),
		CurrencySent: currencySent,
	}, nil
}

// QueryPayment 实现 paymentcontract.GatewayCapturer，主动查询 Cryptomus 发票状态。
func (a *cryptomusAdapter) QueryPayment(ctx context.Context, raw jsonmap.JSON, providerRef string) (*paymentcontract.GatewayQueryResult, error) {
	cfg, err := a.parseConfig(raw)
	if err != nil {
		return nil, err
	}
	result, err := cryptomus.QueryPayment(ctx, cfg, providerRef)
	if err != nil {
		return nil, mapCryptomusError(err)
	}
	return &paymentcontract.GatewayQueryResult{
		ProviderRef: result.UUID,
		Status:      cryptomus.ToPaymentStatus(result.Status),
		Amount:      parseCryptomusAmount(result.Amount),
		Currency:    strings.ToUpper(strings.TrimSpace(result.Currency)),
		Payload:     jsonmap.JSON(result.Raw),
	}, nil
}

// ParseWebhook 校验 Cryptomus webhook 签名并映射为统一回调结果。
// 验签在 native 包完成；金额/币种/订单号/渠道归属的最终强制比对由上层
// HandleCallback 的 validateCallbackPaymentFacts 执行。
func (a *cryptomusAdapter) ParseWebhook(_ context.Context, raw jsonmap.JSON, _ map[string]string, body []byte, _ time.Time) (*paymentcontract.GatewayCallbackResult, error) {
	cfg, err := cryptomus.ParseConfig(raw)
	if err != nil {
		return nil, mapCryptomusError(err)
	}
	payload, err := cryptomus.VerifyWebhookSignature(cfg, body)
	if err != nil {
		return nil, mapCryptomusError(err)
	}

	return &paymentcontract.GatewayCallbackResult{
		OrderNo:     payload.OrderID,
		ProviderRef: payload.UUID,
		Status:      cryptomus.ToPaymentStatus(payload.Status),
		Amount:      parseCryptomusAmount(payload.Amount),
		Currency:    payload.Currency,
		Payload:     jsonmap.JSON(payload.Raw),
	}, nil
}

// parseCryptomusAmount 金额解析失败时返回零值：wrapper 仅做适配，金额异常由业务层判定。
func parseCryptomusAmount(raw string) money.Amount {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return money.Amount{}
	}
	parsed, err := decimal.NewFromString(raw)
	if err != nil || !parsed.IsPositive() {
		return money.Amount{}
	}
	return money.FromDecimal(parsed)
}

// mapCryptomusError 把 cryptomus 包的 sentinel error 映射为 provider 统一错误。
func mapCryptomusError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, cryptomus.ErrConfigInvalid):
		return fmt.Errorf("%w: %v", paymentcontract.ErrGatewayConfigInvalid, err)
	case errors.Is(err, cryptomus.ErrRequestFailed):
		return fmt.Errorf("%w: %v", paymentcontract.ErrGatewayRequestFailed, err)
	case errors.Is(err, cryptomus.ErrResponseInvalid):
		return fmt.Errorf("%w: %v", paymentcontract.ErrGatewayResponseInvalid, err)
	case errors.Is(err, cryptomus.ErrSignatureInvalid):
		return fmt.Errorf("%w: %v", paymentcontract.ErrGatewaySignatureInvalid, err)
	default:
		return err
	}
}
