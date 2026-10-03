package domain

import "time"

// 工单优先级
const (
	PriorityLow    = "low"
	PriorityNormal = "normal"
	PriorityHigh   = "high"
)

// 工单状态
const (
	// StatusOpen 待处理：等待客服回复（新建或用户刚回复）
	StatusOpen = "open"
	// StatusReplied 已回复：客服已回复，等待用户查看/回复
	StatusReplied = "replied"
	// StatusClosed 已关闭：工单已结束，不可再回复
	StatusClosed = "closed"
)

// 工单服务类型
const (
	// TypePreSale 售前咨询：购买前咨询商品、功能或交付方式，商品可选关联
	TypePreSale = "pre_sale"
	// TypeAfterSale 售后支持：已购商品出现问题需协助，通常关联订单
	TypeAfterSale = "after_sale"
)

// Ticket 工单表
type Ticket struct {
	ID       uint   `gorm:"primarykey" json:"id"`
	TicketNo string `gorm:"uniqueIndex;size:32;not null" json:"ticket_no"` // 工单编号
	UserID   uint   `gorm:"index;not null" json:"user_id"`                 // 提交用户ID
	OrderID  *uint  `gorm:"index" json:"order_id,omitempty"`               // 关联订单ID（可选）
	// TicketType 服务类型（pre_sale/after_sale）。历史数据与未传类型的旧客户端统一按 after_sale 兼容，
	// 这是兼容默认值，不代表历史工单的真实类型。
	TicketType string `gorm:"size:16;not null;default:after_sale;index" json:"ticket_type"`
	// ProductID 关联商品ID（可选，售前咨询常见）。弱关联，不建跨模块外键，商品删除后允许悬空。
	ProductID *uint  `gorm:"index" json:"product_id,omitempty"`
	Title     string `gorm:"size:200;not null" json:"title"` // 标题
	Priority  string `gorm:"size:16;not null;default:normal" json:"priority"`
	Status    string `gorm:"size:16;not null;default:open;index" json:"status"`

	CreatedAt time.Time `gorm:"index" json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	Messages []TicketMessage `gorm:"foreignKey:TicketID" json:"messages,omitempty"`
}

// TableName 指定表名
func (Ticket) TableName() string {
	return "tickets"
}

// IsClosed 工单是否已关闭
func (t *Ticket) IsClosed() bool {
	return t.Status == StatusClosed
}
