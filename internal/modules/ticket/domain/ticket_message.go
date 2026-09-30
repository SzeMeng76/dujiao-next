package domain

import "time"

// 发送者类型
const (
	SenderUser  = "user"
	SenderAdmin = "admin"
)

// TicketMessage 工单消息表（工单的对话记录）
type TicketMessage struct {
	ID         uint      `gorm:"primarykey" json:"id"`
	TicketID   uint      `gorm:"index;not null" json:"ticket_id"`
	SenderType string    `gorm:"size:16;not null" json:"sender_type"` // user / admin
	SenderID   uint      `gorm:"not null" json:"sender_id"`           // 用户ID或管理员ID
	Content    string    `gorm:"type:text;not null" json:"content"`
	ImageURL   string    `gorm:"size:500" json:"image_url,omitempty"`
	CreatedAt  time.Time `gorm:"index" json:"created_at"`
}

// TableName 指定表名
func (TicketMessage) TableName() string {
	return "ticket_messages"
}
