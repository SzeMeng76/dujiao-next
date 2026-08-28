package application

import (
	"errors"
	"strings"
	"time"

	orderapp "github.com/dujiao-next/internal/modules/order/application"
	orderdomain "github.com/dujiao-next/internal/modules/order/domain"
	paymentdomain "github.com/dujiao-next/internal/modules/payment/domain"

	"github.com/dujiao-next/internal/constants"
)

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

// manualConfirmableOrderStatuses 允许人工确认支付的订单状态：
// 待支付、以及已支付但存在人工发货子单仍处于处理中的场景。已支付/已发货/已完成/
// 已退款/已取消等状态一律拒绝，避免误操作重复触发发货或库存扣减。
var manualConfirmableOrderStatuses = map[string]bool{
	constants.OrderStatusPendingPayment: true,
	constants.OrderStatusFulfilling:     true,
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
		PaymentID:   payment.ID,
		OrderNo:     order.OrderNo,
		ChannelID:   payment.ChannelID,
		Status:      constants.PaymentStatusSuccess,
		ProviderRef: strings.TrimSpace(input.ProviderRef),
		Amount:      payment.Amount,
		Currency:    payment.Currency,
		PaidAt:      &now,
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
