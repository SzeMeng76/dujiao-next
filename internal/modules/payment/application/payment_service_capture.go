package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	orderapp "github.com/dujiao-next/internal/modules/order/application"
	orderdomain "github.com/dujiao-next/internal/modules/order/domain"
	paymentcontract "github.com/dujiao-next/internal/modules/payment/contract"

	paymentdomain "github.com/dujiao-next/internal/modules/payment/domain"

	"github.com/dujiao-next/internal/constants"
	"github.com/dujiao-next/internal/logger"
	"github.com/dujiao-next/internal/shared/jsonmap"
)

// CapturePaymentInput 捕获支付输入。
type CapturePaymentInput struct {
	PaymentID uint
	Context   context.Context
}

func (s *PaymentService) CapturePayment(input CapturePaymentInput) (*paymentdomain.Payment, error) {
	if input.PaymentID == 0 {
		return nil, ErrPaymentInvalid
	}
	payment, err := s.paymentRepo.GetByID(input.PaymentID)
	if err != nil {
		return nil, ErrPaymentUpdateFailed
	}
	if payment == nil {
		return nil, ErrPaymentNotFound
	}
	if payment.Status == constants.PaymentStatusSuccess {
		return payment, nil
	}

	channel, err := s.channelRepo.GetByID(payment.ChannelID)
	if err != nil {
		return nil, ErrPaymentUpdateFailed
	}
	if channel == nil {
		return nil, ErrPaymentChannelNotFound
	}

	providerType := strings.ToLower(strings.TrimSpace(channel.ProviderType))
	if providerType != constants.PaymentProviderOfficial {
		return nil, ErrPaymentProviderNotSupported
	}
	if strings.TrimSpace(payment.ProviderRef) == "" {
		return nil, ErrPaymentInvalid
	}

	// 统一通过 Registry 路由。Registry.Lookup 会返回 channel 对应的 adapter,
	// 如果 adapter 不实现 Capturer,type assertion 失败,返回 ErrPaymentProviderNotSupported。
	// 因此无需在此显式检查 channel 是否支持 capture。
	return s.captureViaRegistry(input, payment, channel)
}

// captureViaRegistry 通过 PaymentProviderRegistry 路由调用 QueryPayment。
// stripe + paypal + wechat 实现了 paymentcontract.GatewayCapturer 接口,其它 channel
// (alipay / epay / epusdt / bepusdt / tokenpay / okpay) 仅实现 webhook 回调,
// type assertion 失败时返回 ErrPaymentProviderNotSupported。
func (s *PaymentService) captureViaRegistry(input CapturePaymentInput, payment *paymentdomain.Payment, channel *paymentdomain.PaymentChannel) (*paymentdomain.Payment, error) {
	logger.Infow("payment_capture_via_registry",
		"payment_id", payment.ID,
		"provider_type", channel.ProviderType,
		"channel_type", channel.ChannelType,
	)
	if s.paymentProviderRegistry == nil {
		return nil, ErrPaymentProviderNotSupported
	}
	p, ok := s.paymentProviderRegistry.Lookup(channel.ProviderType, channel.ChannelType)
	if !ok {
		return nil, ErrPaymentProviderNotSupported
	}
	capturer, ok := p.(paymentcontract.GatewayCapturer)
	if !ok {
		logger.Warnw("payment_provider_capture_not_implemented",
			"provider_type", channel.ProviderType,
			"channel_type", channel.ChannelType,
		)
		return nil, ErrPaymentProviderNotSupported
	}

	// 第二参数是 interactionMode,不是 channelType。stripe/wechat adapter
	// 会拒绝任何非法 mode,传 channelType 会导致  一律 ErrConfigInvalid。
	if err := capturer.ValidateConfig(channel.ConfigJSON, channel.InteractionMode); err != nil {
		return nil, mapProviderErrorToService(err)
	}

	ctx, cancel := detachOutboundRequestContext(input.Context)
	defer cancel()

	queryResult, err := capturer.QueryPayment(ctx, channel.ConfigJSON, payment.ProviderRef)
	if err != nil {
		return nil, mapProviderErrorToService(err)
	}

	payload := jsonmap.JSON{}
	if queryResult.Payload != nil {
		payload = queryResult.Payload
	}
	status := strings.TrimSpace(queryResult.Status)
	if status == "" {
		status = constants.PaymentStatusPending
	}

	callbackInput := PaymentCallbackInput{
		PaymentID:   payment.ID,
		ChannelID:   channel.ID,
		Status:      status,
		ProviderRef: pickFirstNonEmpty(queryResult.ProviderRef, payment.ProviderRef),
		Amount:      queryResult.Amount,
		Currency:    strings.ToUpper(strings.TrimSpace(queryResult.Currency)),
		PaidAt:      queryResult.PaidAt,
		Payload:     payload,
	}
	return s.HandleCallback(callbackInput)
}

// mapProviderErrorToService 把 provider.ErrXxx 转换为 service 层的 ErrPaymentXxx。
func mapProviderErrorToService(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, paymentcontract.ErrGatewayConfigInvalid):
		return fmt.Errorf("%w: %v", ErrPaymentChannelConfigInvalid, err)
	case errors.Is(err, paymentcontract.ErrGatewayRequestFailed), errors.Is(err, paymentcontract.ErrGatewayAuthFailed):
		return fmt.Errorf("%w: %v", ErrPaymentGatewayRequestFailed, err)
	case errors.Is(err, paymentcontract.ErrGatewayResponseInvalid), errors.Is(err, paymentcontract.ErrGatewaySignatureInvalid):
		return fmt.Errorf("%w: %v", ErrPaymentGatewayResponseInvalid, err)
	case errors.Is(err, paymentcontract.ErrGatewayUnsupportedChannel), errors.Is(err, paymentcontract.ErrGatewayProviderNotFound):
		return ErrPaymentProviderNotSupported
	default:
		return fmt.Errorf("%w: %v", ErrPaymentGatewayRequestFailed, err)
	}
}

var (
	// ErrOrderManualConfirmNotAllowed 表示订单当前状态不允许人工确认支付。
	ErrOrderManualConfirmNotAllowed = errors.New("order manual confirm payment not allowed")
	// ErrManualConfirmRemarkRequired 表示人工确认支付缺少必填备注。
	ErrManualConfirmRemarkRequired = errors.New("manual confirm remark required")
)

// manualConfirmablePaymentStatuses 允许人工确认支付的支付记录状态：
// 未成功送达/未处理成功的回调（pending/failed/expired），success 已经是终态，
// 由 HandleCallback 自身的幂等逻辑拦截，这里不需要再选它。
var manualConfirmablePaymentStatuses = map[string]bool{
	constants.PaymentStatusPending: true,
	constants.PaymentStatusFailed:  true,
	constants.PaymentStatusExpired: true,
}

// manualConfirmableOrderStatuses 允许人工确认支付的订单状态：待支付，以及因超时
// 被系统自动取消但用户实际已经付款成功的场景。fulfilling 及之后的状态说明支付
// 早已成功处理过（该订单不会再有 pending/failed/expired 的支付记录），不在此列；
// 已支付/已发货/已完成/已退款等其他终态同样一律拒绝，避免误操作重复触发发货或
// 库存扣减。
//
// 已取消订单人工确认支付时：取消时释放的库存/卡密如果已被其他订单占用，
// HandleCallback 内部扣减库存/卡密的步骤会因为数量不足而报错并整体回滚，
// 不会导致超卖；优惠券/返利等取消时的周边回滚不会被自动撤销，需人工核对账目。
var manualConfirmableOrderStatuses = map[string]bool{
	constants.OrderStatusPendingPayment: true,
	constants.OrderStatusCanceled:       true,
}

// ManualConfirmPaymentInput 后台人工确认支付输入。
type ManualConfirmPaymentInput struct {
	OrderID          uint
	OperatorAdminID  uint
	OperatorUsername string
	ProviderRef      string // 可选：第三方支付流水号 / 链上交易 Hash
	Remark           string // 必填：人工确认备注
}

// ManualConfirmPayment 后台人工确认支付：作为第三方支付回调失败时的兜底方案。
//
// 不直接改订单状态字段，而是复用与真实网关回调完全相同的 HandleCallback 流水线，
// 因此库存扣减、自动发货触发、支付记录更新、幂等保护等副作用与正常支付成功路径一致。
func (s *PaymentService) ManualConfirmPayment(input ManualConfirmPaymentInput) (*orderdomain.Order, *paymentdomain.Payment, error) {
	if strings.TrimSpace(input.Remark) == "" {
		return nil, nil, ErrManualConfirmRemarkRequired
	}
	if input.OrderID == 0 || input.OperatorAdminID == 0 {
		return nil, nil, ErrPaymentInvalid
	}

	order, err := s.orderRepo.GetByID(input.OrderID)
	if err != nil {
		return nil, nil, orderapp.ErrOrderFetchFailed
	}
	if order == nil {
		return nil, nil, orderapp.ErrOrderNotFound
	}
	if !manualConfirmableOrderStatuses[order.Status] {
		return nil, nil, ErrOrderManualConfirmNotAllowed
	}

	payments, err := s.paymentRepo.ListByOrderID(order.ID)
	if err != nil {
		return nil, nil, ErrPaymentUpdateFailed
	}
	payment := latestManualConfirmablePayment(payments)
	if payment == nil {
		return nil, nil, ErrPaymentNotFound
	}

	fromStatus := order.Status
	now := time.Now()
	updatedPayment, err := s.HandleCallback(PaymentCallbackInput{
		PaymentID:                payment.ID,
		OrderNo:                  order.OrderNo,
		ChannelID:                payment.ChannelID,
		Status:                   constants.PaymentStatusSuccess,
		ProviderRef:              strings.TrimSpace(input.ProviderRef),
		Amount:                   payment.Amount,
		Currency:                 payment.Currency,
		PaidAt:                   &now,
		allowReopenCanceledOrder: fromStatus == constants.OrderStatusCanceled,
	})
	if err != nil {
		return nil, nil, err
	}

	processedOrder, err := s.orderRepo.GetByID(order.ID)
	if err != nil {
		return nil, updatedPayment, orderapp.ErrOrderFetchFailed
	}

	s.recordManualConfirmLog(order.ID, payment.ID, input, fromStatus, processedOrder)

	return processedOrder, updatedPayment, nil
}

// latestManualConfirmablePayment 取订单下最新一条未成功送达/未处理成功的支付记录。
// 没有任何可确认的支付记录（订单从未生成过支付，或已全部处于 success/其他终态）时返回 nil，
// 由调用方拒绝——该功能只补一次没送达的回调，不负责从零创建支付记录。
func latestManualConfirmablePayment(payments []paymentdomain.Payment) *paymentdomain.Payment {
	var latest *paymentdomain.Payment
	for i := range payments {
		p := &payments[i]
		if !manualConfirmablePaymentStatuses[p.Status] {
			continue
		}
		if latest == nil || p.CreatedAt.After(latest.CreatedAt) {
			latest = p
		}
	}
	return latest
}

func (s *PaymentService) recordManualConfirmLog(
	orderID, paymentID uint,
	input ManualConfirmPaymentInput,
	fromStatus string,
	processedOrder *orderdomain.Order,
) {
	if s.manualConfirmLogStore == nil {
		return
	}
	toStatus := ""
	if processedOrder != nil {
		toStatus = processedOrder.Status
	}
	log := &orderdomain.OrderManualConfirmLog{
		OrderID:          orderID,
		PaymentID:        paymentID,
		OperatorAdminID:  input.OperatorAdminID,
		OperatorUsername: strings.TrimSpace(input.OperatorUsername),
		FromStatus:       fromStatus,
		ToStatus:         toStatus,
		ProviderRef:      strings.TrimSpace(input.ProviderRef),
		Remark:           strings.TrimSpace(input.Remark),
	}
	if err := s.manualConfirmLogStore.Create(log); err != nil {
		paymentLogger(
			"order_id", orderID,
			"payment_id", paymentID,
			"operator_admin_id", input.OperatorAdminID,
			"error", err,
		).Errorw("manual_confirm_payment_audit_log_failed")
	}
}
