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

// Ticket 工单表
type Ticket struct {
	ID       uint   `gorm:"primarykey" json:"id"`
	TicketNo string `gorm:"uniqueIndex;size:32;not null" json:"ticket_no"` // 工单编号
	UserID   uint   `gorm:"index;not null" json:"user_id"`                 // 提交用户ID
	OrderID  *uint  `gorm:"index" json:"order_id,omitempty"`               // 关联订单ID（可选）
	Title    string `gorm:"size:200;not null" json:"title"`                // 标题
	Priority string `gorm:"size:16;not null;default:normal" json:"priority"`
	Status   string `gorm:"size:16;not null;default:open;index" json:"status"`

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
