package presenter

import (
	"time"

	ticketdomain "github.com/dujiao-next/internal/modules/ticket/domain"
	"github.com/dujiao-next/internal/shared/jsonmap"
)

// TicketSummary 工单列表响应（精简字段，不含消息内容）。
type TicketSummary struct {
	ID              uint   `json:"id"`
	TicketNo        string `json:"ticket_no"`
	UserID          uint   `json:"user_id"`
	UserEmail       string `json:"user_email,omitempty"`
	UserDisplayName string `json:"user_display_name,omitempty"`
	OrderID         *uint  `json:"order_id,omitempty"`
	// OrderNo 是 OrderID 对应的订单号，由 transport 层解析填充，仅用于展示。
	OrderNo    string `json:"order_no,omitempty"`
	TicketType string `json:"ticket_type"`
	ProductID  *uint  `json:"product_id,omitempty"`
	// ProductTitleJSON 是 ProductID 对应商品的多语言标题（raw JSON），
	// 与订单项 Title 契约一致，由前端按 locale 解析；商品已删除时为空。
	ProductTitleJSON jsonmap.JSON `json:"product_title,omitempty"`
	Title            string       `json:"title"`
	Priority         string       `json:"priority"`
	Status           string       `json:"status"`
	CreatedAt        time.Time    `json:"created_at"`
	UpdatedAt        time.Time    `json:"updated_at"`
}

// TicketMessageResp 工单消息响应。
type TicketMessageResp struct {
	ID         uint      `json:"id"`
	SenderType string    `json:"sender_type"`
	SenderID   uint      `json:"sender_id"`
	Content    string    `json:"content"`
	ImageURL   string    `json:"image_url,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

// TicketDetail 工单详情响应（含消息列表）。
type TicketDetail struct {
	TicketSummary
	Messages []TicketMessageResp `json:"messages"`
}

// NewTicketSummary 从 ticketdomain.Ticket 构造 TicketSummary。
func NewTicketSummary(t *ticketdomain.Ticket) TicketSummary {
	return TicketSummary{
		ID:         t.ID,
		TicketNo:   t.TicketNo,
		UserID:     t.UserID,
		OrderID:    t.OrderID,
		TicketType: t.TicketType,
		ProductID:  t.ProductID,
		Title:      t.Title,
		Priority:   t.Priority,
		Status:     t.Status,
		CreatedAt:  t.CreatedAt,
		UpdatedAt:  t.UpdatedAt,
	}
}

// NewTicketSummaryList 批量转换工单列表。
func NewTicketSummaryList(tickets []ticketdomain.Ticket) []TicketSummary {
	result := make([]TicketSummary, 0, len(tickets))
	for i := range tickets {
		result = append(result, NewTicketSummary(&tickets[i]))
	}
	return result
}

// NewTicketMessageResp 从 ticketdomain.TicketMessage 构造响应。
func NewTicketMessageResp(m *ticketdomain.TicketMessage) TicketMessageResp {
	return TicketMessageResp{
		ID:         m.ID,
		SenderType: m.SenderType,
		SenderID:   m.SenderID,
		Content:    m.Content,
		ImageURL:   m.ImageURL,
		CreatedAt:  m.CreatedAt,
	}
}

// NewTicketDetail 从 ticketdomain.Ticket 构造详情响应（需已 Preload Messages）。
func NewTicketDetail(t *ticketdomain.Ticket) TicketDetail {
	detail := TicketDetail{
		TicketSummary: NewTicketSummary(t),
		Messages:      make([]TicketMessageResp, 0, len(t.Messages)),
	}
	for i := range t.Messages {
		detail.Messages = append(detail.Messages, NewTicketMessageResp(&t.Messages[i]))
	}
	return detail
}
