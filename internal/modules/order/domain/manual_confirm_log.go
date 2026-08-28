package domain

import "time"

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
