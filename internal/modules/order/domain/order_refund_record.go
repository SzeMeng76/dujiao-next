package domain

import (
	"time"

	"github.com/dujiao-next/internal/shared/money"
	"github.com/shopspring/decimal"
)

// OrderRefundRecord 退款记录
type OrderRefundRecord struct {
	ID                       uint         `gorm:"primarykey" json:"id"`
	UserID                   uint         `gorm:"index;not null;default:0" json:"user_id"`
	GuestEmail               string       `gorm:"index;type:varchar(255)" json:"guest_email,omitempty"`
	OrderID                  uint         `gorm:"index;not null" json:"order_id"`
	Type                     string       `gorm:"index;type:varchar(32);not null" json:"type"`
	Amount                   money.Amount `gorm:"type:decimal(20,2);not null;default:0" json:"amount"`
	PaymentFeeRefunded       bool         `gorm:"not null;default:false" json:"payment_fee_refunded"`
	PaymentFeeRefundedAmount money.Amount `gorm:"type:decimal(20,2);not null;default:0" json:"payment_fee_refunded_amount"`
	Currency                 string       `gorm:"type:varchar(16);not null;default:''" json:"currency"`
	Remark                   string       `gorm:"type:text" json:"remark,omitempty"`
	CreatedAt                time.Time    `gorm:"index" json:"created_at"`
	UpdatedAt                time.Time    `gorm:"index" json:"updated_at"`
	DeletedAt                *time.Time   `gorm:"index" json:"-"`
}

// TableName 指定表名
func (OrderRefundRecord) TableName() string {
	return "order_refund_records"
}

// CalculatePaymentFeeRefundAmount allocates the original payment fee to one
// refund. It uses cumulative values so the final refund absorbs rounding
// differences without ever returning more than the original payment fee.
func CalculatePaymentFeeRefundAmount(
	paymentAmount decimal.Decimal,
	paymentFee decimal.Decimal,
	refundedPrincipalBefore decimal.Decimal,
	refundedFeeBefore decimal.Decimal,
	refundAmount decimal.Decimal,
) decimal.Decimal {
	paymentAmount = paymentAmount.Round(2)
	paymentFee = paymentFee.Round(2)
	refundedPrincipalBefore = refundedPrincipalBefore.Round(2)
	refundedFeeBefore = refundedFeeBefore.Round(2)
	refundAmount = refundAmount.Round(2)
	if paymentAmount.LessThanOrEqual(decimal.Zero) ||
		paymentFee.LessThanOrEqual(decimal.Zero) ||
		refundAmount.LessThanOrEqual(decimal.Zero) {
		return decimal.Zero
	}

	if refundedPrincipalBefore.IsNegative() {
		refundedPrincipalBefore = decimal.Zero
	}
	if refundedFeeBefore.IsNegative() {
		refundedFeeBefore = decimal.Zero
	}
	if refundedPrincipalBefore.GreaterThan(paymentAmount) {
		refundedPrincipalBefore = paymentAmount
	}
	if refundedFeeBefore.GreaterThan(paymentFee) {
		refundedFeeBefore = paymentFee
	}

	cumulativePrincipal := refundedPrincipalBefore.Add(refundAmount)
	if cumulativePrincipal.GreaterThan(paymentAmount) {
		cumulativePrincipal = paymentAmount
	}
	targetRefundedFee := paymentFee.Mul(cumulativePrincipal).Div(paymentAmount).Round(2)
	if cumulativePrincipal.GreaterThanOrEqual(paymentAmount) {
		targetRefundedFee = paymentFee
	}

	current := targetRefundedFee.Sub(refundedFeeBefore).Round(2)
	if current.IsNegative() {
		return decimal.Zero
	}
	remaining := paymentFee.Sub(refundedFeeBefore).Round(2)
	if current.GreaterThan(remaining) {
		return remaining
	}
	return current
}

// OrderManualConfirmLog 记录后台"人工确认支付"操作，用于审计追溯。
type OrderManualConfirmLog struct {
	ID               uint      `gorm:"primarykey" json:"id"`
	OrderID          uint      `gorm:"index;not null" json:"order_id"`
	PaymentID        uint      `gorm:"index;not null" json:"payment_id"`
	OperatorAdminID  uint      `gorm:"index;not null" json:"operator_admin_id"`
	OperatorUsername string    `gorm:"type:varchar(100);not null;default:''" json:"operator_username"`
	FromStatus       string    `gorm:"type:varchar(50);not null;default:''" json:"from_status"`
	ToStatus         string    `gorm:"type:varchar(50);not null;default:''" json:"to_status"`
	ProviderRef      string    `gorm:"type:varchar(255);not null;default:''" json:"provider_ref,omitempty"`
	Remark           string    `gorm:"type:text;not null" json:"remark"`
	CreatedAt        time.Time `gorm:"index" json:"created_at"`
}

// TableName 指定表名
func (OrderManualConfirmLog) TableName() string {
	return "order_manual_confirm_logs"
}
