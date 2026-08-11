package refund

import (
	"strings"
	"time"

	ordercontract "github.com/dujiao-next/internal/modules/order/contract"
	orderdomain "github.com/dujiao-next/internal/modules/order/domain"
	paymentdomain "github.com/dujiao-next/internal/modules/payment/domain"

	"github.com/dujiao-next/internal/constants"
	"github.com/dujiao-next/internal/shared/money"

	"github.com/shopspring/decimal"
)

type paymentFeeReader interface {
	ListByOrderID(orderID uint) ([]paymentdomain.Payment, error)
}

// UpdatePaymentFeeRefundedInput updates the accounting fact attached to an
// existing manual refund record. It does not call a payment gateway.
type UpdatePaymentFeeRefundedInput struct {
	RefundRecordID     uint
	PaymentFeeRefunded bool
}

func (s *Service) UpdatePaymentFeeRefunded(input UpdatePaymentFeeRefundedInput) (*orderdomain.OrderRefundRecord, error) {
	if input.RefundRecordID == 0 {
		return nil, ErrOrderNotFound
	}
	var updated *orderdomain.OrderRefundRecord
	err := s.orderStore.WithinTransaction(func(tx ordercontract.Transaction) error {
		orders := tx.Orders()
		initial, err := orders.GetRefundRecordByID(input.RefundRecordID)
		if err != nil {
			return ErrOrderFetchFailed
		}
		if initial == nil {
			return ErrOrderNotFound
		}
		if initial.Type != constants.OrderRefundTypeManual {
			return ErrOrderStatusInvalid
		}
		order, err := orders.GetByIDForUpdate(initial.OrderID)
		if err != nil {
			return ErrOrderFetchFailed
		}
		if order == nil {
			return ErrOrderNotFound
		}
		record, err := orders.GetRefundRecordByIDForUpdate(input.RefundRecordID)
		if err != nil {
			return ErrOrderFetchFailed
		}
		if record == nil || record.OrderID != order.ID {
			return ErrOrderNotFound
		}

		feeAmount := money.FromDecimal(decimal.Zero)
		if input.PaymentFeeRefunded {
			feeAmount, err = s.resolvePaymentFeeRefundAmount(orders, order, record.Amount.Decimal, record.ID)
			if err != nil {
				return err
			}
		}
		now := time.Now()
		if err := orders.UpdateRefundRecordPaymentFee(record.ID, input.PaymentFeeRefunded, feeAmount, now); err != nil {
			return ErrOrderUpdateFailed
		}
		record.PaymentFeeRefunded = input.PaymentFeeRefunded
		record.PaymentFeeRefundedAmount = feeAmount
		record.UpdatedAt = now
		updated = record
		return nil
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

func (s *Service) resolvePaymentFeeRefundAmount(
	orders ordercontract.Store,
	order *orderdomain.Order,
	refundAmount decimal.Decimal,
	excludeRefundRecordID uint,
) (money.Amount, error) {
	zero := money.FromDecimal(decimal.Zero)
	if s == nil || s.payments == nil || orders == nil || order == nil {
		return zero, ErrOrderUpdateFailed
	}

	rootID := order.ID
	if order.ParentID != nil && *order.ParentID > 0 {
		rootID = *order.ParentID
	}
	payments, err := s.payments.ListByOrderID(rootID)
	if err != nil {
		return zero, ErrOrderFetchFailed
	}
	paymentAmount, paymentFee := refundablePaymentFeeSnapshot(payments, order.Currency)
	if paymentAmount.LessThanOrEqual(decimal.Zero) || paymentFee.LessThanOrEqual(decimal.Zero) {
		return zero, nil
	}

	orderIDs := []uint{rootID}
	children, err := orders.ListChildren(rootID)
	if err != nil {
		return zero, ErrOrderFetchFailed
	}
	for _, child := range children {
		orderIDs = append(orderIDs, child.ID)
	}
	records, err := orders.ListRefundRecordsByOrderIDs(orderIDs)
	if err != nil {
		return zero, ErrOrderFetchFailed
	}
	refundedPrincipalBefore := decimal.Zero
	refundedFeeBefore := decimal.Zero
	for _, record := range records {
		if record.ID == excludeRefundRecordID || !record.PaymentFeeRefunded {
			continue
		}
		refundedPrincipalBefore = refundedPrincipalBefore.Add(record.Amount.Decimal)
		refundedFeeBefore = refundedFeeBefore.Add(record.PaymentFeeRefundedAmount.Decimal)
	}

	amount := orderdomain.CalculatePaymentFeeRefundAmount(
		paymentAmount,
		paymentFee,
		refundedPrincipalBefore,
		refundedFeeBefore,
		refundAmount,
	)
	return money.FromDecimal(amount), nil
}

func refundablePaymentFeeSnapshot(payments []paymentdomain.Payment, currency string) (decimal.Decimal, decimal.Decimal) {
	paymentAmount := decimal.Zero
	paymentFee := decimal.Zero
	for _, payment := range payments {
		if payment.Status != constants.PaymentStatusSuccess ||
			payment.ProviderType == constants.PaymentProviderWallet ||
			payment.FeePolicy != constants.PaymentFeePolicyMerchantAbsorbed ||
			strings.TrimSpace(payment.ExceptionCode) != "" {
			continue
		}
		if currency != "" && payment.Currency != "" && !strings.EqualFold(currency, payment.Currency) {
			continue
		}
		amount := payment.Amount.Decimal.Round(2)
		fee := payment.FeeAmount.Decimal.Round(2)
		if amount.LessThanOrEqual(decimal.Zero) || fee.LessThanOrEqual(decimal.Zero) {
			continue
		}
		paymentAmount = paymentAmount.Add(amount)
		paymentFee = paymentFee.Add(fee)
	}
	return paymentAmount.Round(2), paymentFee.Round(2)
}
