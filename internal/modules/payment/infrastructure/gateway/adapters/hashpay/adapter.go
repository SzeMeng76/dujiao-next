package hashpayadapter

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	gatewaycommon "github.com/dujiao-next/internal/modules/payment/infrastructure/gateway/common"

	paymentcontract "github.com/dujiao-next/internal/modules/payment/contract"

	"github.com/dujiao-next/internal/constants"
	"github.com/dujiao-next/internal/modules/payment/infrastructure/gateway/hashpay"
	"github.com/dujiao-next/internal/shared/jsonmap"
	"github.com/dujiao-next/internal/shared/money"

	"github.com/shopspring/decimal"
)

// hashpayAdapter 是 HashPay 网关的 paymentcontract.GatewayProvider + GatewayWebhooker + GatewayCapturer 实现。
// HashPay 下单不指定链/币种（付款人在托管收银台自选），只支持 redirect 交互模式；
// 回调是独立 webhook（RSA-OAEP+AES-GCM 加密信封），走 /payments/webhook/hashpay 专属路由。
type hashpayAdapter struct{}

// NewHashpayAdapter 实例化 HashPay adapter。
func NewHashpayAdapter() paymentcontract.GatewayProvider { return &hashpayAdapter{} }

var (
	_ paymentcontract.GatewayProvider  = (*hashpayAdapter)(nil)
	_ paymentcontract.GatewayWebhooker = (*hashpayAdapter)(nil)
	_ paymentcontract.GatewayCapturer  = (*hashpayAdapter)(nil)
)

// Type 返回 provider 标识。HashPay 是单 provider 收银台网关，channelType 部分为空。
func (a *hashpayAdapter) Type() string {
	return constants.PaymentProviderHashpay + ":"
}

func (a *hashpayAdapter) parseConfig(raw jsonmap.JSON) (*hashpay.Config, error) {
	cfg, err := hashpay.ParseConfig(raw)
	if err != nil {
		return nil, mapHashpayError(err)
	}
	if err := hashpay.ValidateConfig(cfg); err != nil {
		return nil, mapHashpayError(err)
	}
	return cfg, nil
}

// ValidateConfig 验证 channel.ConfigJSON。channel_type 固定为空或 hashpay。
func (a *hashpayAdapter) ValidateConfig(raw jsonmap.JSON, channelType string) error {
	normalized := strings.ToLower(strings.TrimSpace(channelType))
	if normalized != "" && normalized != constants.PaymentProviderHashpay {
		return fmt.Errorf("%w: hashpay channel_type %s", paymentcontract.ErrGatewayUnsupportedChannel, channelType)
	}
	_, err := a.parseConfig(raw)
	return err
}

// CreatePayment 创建 HashPay 订单并返回收银台跳转地址。
func (a *hashpayAdapter) CreatePayment(ctx context.Context, raw jsonmap.JSON, input paymentcontract.GatewayCreateInput) (*paymentcontract.GatewayCreateResult, error) {
	cfg, err := a.parseConfig(raw)
	if err != nil {
		return nil, err
	}
	// 下单响应没有收款地址（付款人在收银台自选链/币后才生成），无法渲染站内二维码。
	if mode, ok := input.Extra["interaction_mode"].(string); ok && strings.ToLower(strings.TrimSpace(mode)) == constants.PaymentInteractionQR {
		return nil, fmt.Errorf("%w: hashpay only supports redirect interaction_mode", paymentcontract.ErrGatewayConfigInvalid)
	}

	returnURL := gatewaycommon.PickFirstNonEmpty(input.ReturnURL, cfg.ReturnURL)
	returnURL = gatewaycommon.AppendQueryParams(returnURL, input.ReturnURLQuery)

	currencySent := strings.ToUpper(strings.TrimSpace(input.Currency))
	if cfg.Currency != "" {
		currencySent = cfg.Currency
	}

	result, err := hashpay.CreatePayment(ctx, cfg, hashpay.CreateInput{
		OrderNo:     strings.TrimSpace(input.OrderNo),
		Amount:      input.Amount.Decimal.String(),
		Currency:    currencySent,
		Description: input.Subject,
		ReturnURL:   returnURL,
	})
	if err != nil {
		return nil, mapHashpayError(err)
	}

	payload := jsonmap.JSON{}
	for key, value := range result.Raw {
		payload[key] = value
	}

	return &paymentcontract.GatewayCreateResult{
		ProviderRef:  result.OrderID,
		RedirectURL:  result.CheckoutURL,
		QRCodeURL:    result.CheckoutURL,
		Payload:      payload,
		AmountSent:   input.Amount.Decimal.String(),
		CurrencySent: currencySent,
	}, nil
}

// QueryPayment 实现 paymentcontract.GatewayCapturer，主动查询 HashPay 订单状态。
func (a *hashpayAdapter) QueryPayment(ctx context.Context, raw jsonmap.JSON, providerRef string) (*paymentcontract.GatewayQueryResult, error) {
	cfg, err := a.parseConfig(raw)
	if err != nil {
		return nil, err
	}
	result, err := hashpay.QueryPayment(ctx, cfg, providerRef)
	if err != nil {
		return nil, mapHashpayError(err)
	}
	return &paymentcontract.GatewayQueryResult{
		ProviderRef: result.OrderID,
		Status:      hashpay.ToPaymentStatus(result.Status),
		Amount:      parseHashpayAmount(result.Amount),
		Currency:    strings.ToUpper(strings.TrimSpace(result.Currency)),
		PaidAt:      result.PaidAt,
		Payload:     jsonmap.JSON(result.Raw),
	}, nil
}

// ParseWebhook 解密 HashPay 回调信封并映射为统一回调结果。
// 解密与关键字段完整性校验在 native 包完成；金额/币种/订单号/渠道归属的
// 最终强制比对由上层 HandleCallback 的 validateCallbackPaymentFacts 执行。
func (a *hashpayAdapter) ParseWebhook(_ context.Context, raw jsonmap.JSON, headers map[string]string, body []byte, now time.Time) (*paymentcontract.GatewayCallbackResult, error) {
	cfg, err := hashpay.ParseConfig(raw)
	if err != nil {
		return nil, mapHashpayError(err)
	}
	payload, err := hashpay.DecryptCallback(cfg, headers, body, now)
	if err != nil {
		return nil, mapHashpayError(err)
	}

	var paidAt *time.Time
	if tx, ok := payload.Payment["tx"].(map[string]interface{}); ok {
		if ts := gatewaycommon.ReadString(tx, "timestamp"); ts != "" {
			if parsed, parseErr := decimal.NewFromString(ts); parseErr == nil && parsed.IsPositive() {
				t := time.Unix(parsed.IntPart(), 0)
				paidAt = &t
			}
		}
	}

	return &paymentcontract.GatewayCallbackResult{
		OrderNo:     payload.MerchantNo,
		ProviderRef: payload.OrderID,
		Status:      hashpay.ToPaymentStatus(payload.Status),
		Amount:      parseHashpayAmount(payload.Amount),
		Currency:    payload.Currency,
		PaidAt:      paidAt,
		Payload:     jsonmap.JSON(payload.Raw),
	}, nil
}

// parseHashpayAmount 金额解析失败时返回零值：wrapper 仅做适配，金额异常由业务层判定。
func parseHashpayAmount(raw string) money.Amount {
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

// mapHashpayError 把 hashpay 包的 sentinel error 映射为 provider 统一错误。
func mapHashpayError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, hashpay.ErrConfigInvalid):
		return fmt.Errorf("%w: %v", paymentcontract.ErrGatewayConfigInvalid, err)
	case errors.Is(err, hashpay.ErrRequestFailed):
		return fmt.Errorf("%w: %v", paymentcontract.ErrGatewayRequestFailed, err)
	case errors.Is(err, hashpay.ErrResponseInvalid):
		return fmt.Errorf("%w: %v", paymentcontract.ErrGatewayResponseInvalid, err)
	case errors.Is(err, hashpay.ErrSignatureInvalid):
		return fmt.Errorf("%w: %v", paymentcontract.ErrGatewaySignatureInvalid, err)
	default:
		return err
	}
}
