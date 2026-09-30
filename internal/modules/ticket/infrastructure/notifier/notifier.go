package notifier

import (
	"fmt"
	"strings"

	"github.com/dujiao-next/internal/constants"
	"github.com/dujiao-next/internal/logger"
	notificationcontract "github.com/dujiao-next/internal/modules/notification/contract"
	ticketdomain "github.com/dujiao-next/internal/modules/ticket/domain"
	"github.com/dujiao-next/internal/queue"
	"github.com/dujiao-next/internal/shared/jsonmap"
)

// Notifier 工单通知实现：站长侧走通知中心事件，用户侧走队列异步邮件。
type Notifier struct {
	enqueuer notificationcontract.NotificationEnqueuer
	queue    *queue.Client
}

// New 创建工单通知器。enqueuer/queueClient 均可为 nil（表示对应通知不发送）。
func New(enqueuer notificationcontract.NotificationEnqueuer, queueClient *queue.Client) *Notifier {
	return &Notifier{enqueuer: enqueuer, queue: queueClient}
}

// NotifyAdminNewMessage 用户创建/回复工单时，通知站长（走通知中心配置的渠道）。
func (n *Notifier) NotifyAdminNewMessage(ticket *ticketdomain.Ticket, message *ticketdomain.TicketMessage) {
	if n == nil || n.enqueuer == nil || ticket == nil || message == nil {
		return
	}
	err := n.enqueuer.Enqueue(notificationcontract.EnqueueInput{
		EventType: constants.NotificationEventTicketMessage,
		BizType:   constants.NotificationBizTypeTicket,
		BizID:     ticket.ID,
		Data: jsonmap.JSON{
			"ticket_no":       ticket.TicketNo,
			"ticket_title":    ticket.Title,
			"ticket_priority": ticket.Priority,
			"sender_label":    senderLabel(message.SenderType),
			"content_excerpt": excerpt(message.Content, 200),
		},
	})
	if err != nil {
		logger.Warnw("ticket_notify_admin_enqueue_failed", "ticket_id", ticket.ID, "error", err)
	}
}

// NotifyUserReplied 客服回复工单时，通知用户（走独立队列任务直发邮件）。
func (n *Notifier) NotifyUserReplied(ticket *ticketdomain.Ticket, message *ticketdomain.TicketMessage) {
	if n == nil || n.queue == nil || ticket == nil || message == nil {
		return
	}
	err := n.queue.EnqueueTicketMessageEmail(queue.TicketMessageEmailPayload{
		TicketID:  ticket.ID,
		MessageID: message.ID,
	})
	if err != nil {
		logger.Warnw("ticket_notify_user_enqueue_failed", "ticket_id", ticket.ID, "error", err)
	}
}

func senderLabel(senderType string) string {
	if senderType == ticketdomain.SenderAdmin {
		return "客服"
	}
	return "用户"
}

func excerpt(content string, maxRunes int) string {
	content = strings.TrimSpace(content)
	runes := []rune(content)
	if len(runes) <= maxRunes {
		return content
	}
	return fmt.Sprintf("%s...", string(runes[:maxRunes]))
}
