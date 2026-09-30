package notifier

import (
	"fmt"
	"strings"

	"github.com/dujiao-next/internal/constants"
	"github.com/dujiao-next/internal/logger"
	userdomain "github.com/dujiao-next/internal/modules/identity/user/domain"
	notificationcontract "github.com/dujiao-next/internal/modules/notification/contract"
	settingsmessaging "github.com/dujiao-next/internal/modules/settings/schema/messaging"
	ticketdomain "github.com/dujiao-next/internal/modules/ticket/domain"
	"github.com/dujiao-next/internal/queue"
	"github.com/dujiao-next/internal/shared/jsonmap"
)

// UserLookup 是查询用户信息（用于通知文案里展示提交人）所需的最小端口。
type UserLookup interface {
	GetByID(id uint) (*userdomain.User, error)
}

// SettingsReader 是读取通知中心默认语言所需的最小端口。
type SettingsReader interface {
	GetNotificationCenterSetting() (settingsmessaging.NotificationCenterSetting, error)
}

// Notifier 工单通知实现：站长侧走通知中心事件，用户侧走队列异步邮件。
type Notifier struct {
	enqueuer notificationcontract.NotificationEnqueuer
	queue    *queue.Client
	users    UserLookup
	settings SettingsReader
}

// New 创建工单通知器。enqueuer/queueClient/users/settings 均可为 nil（表示对应能力不可用，语言回退中文）。
func New(enqueuer notificationcontract.NotificationEnqueuer, queueClient *queue.Client, users UserLookup, settings SettingsReader) *Notifier {
	return &Notifier{enqueuer: enqueuer, queue: queueClient, users: users, settings: settings}
}

// notificationLocale 返回通知中心配置的默认语言，读取失败时回退中文。
func (n *Notifier) notificationLocale() string {
	if n.settings == nil {
		return constants.LocaleZhCN
	}
	setting, err := n.settings.GetNotificationCenterSetting()
	if err != nil {
		return constants.LocaleZhCN
	}
	return settingsmessaging.NormalizeNotificationLocale(setting.DefaultLocale)
}

// NotifyAdminNewMessage 用户创建/回复工单时，通知站长（走通知中心配置的渠道）。
func (n *Notifier) NotifyAdminNewMessage(ticket *ticketdomain.Ticket, message *ticketdomain.TicketMessage) {
	if n == nil || n.enqueuer == nil || ticket == nil || message == nil {
		return
	}
	locale := n.notificationLocale()
	err := n.enqueuer.Enqueue(notificationcontract.EnqueueInput{
		EventType: constants.NotificationEventTicketMessage,
		BizType:   constants.NotificationBizTypeTicket,
		BizID:     ticket.ID,
		Locale:    locale,
		Data: jsonmap.JSON{
			"ticket_no":       ticket.TicketNo,
			"ticket_title":    ticket.Title,
			"ticket_priority": priorityLabel(ticket.Priority, locale),
			"sender_label":    n.senderLabel(message.SenderType, ticket.UserID, locale),
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

// adminLabelByLocale 按语言返回"客服"固定文案。
func adminLabelByLocale(locale string) string {
	switch locale {
	case constants.LocaleZhTW:
		return "客服"
	case constants.LocaleEnUS:
		return "Support"
	default:
		return "客服"
	}
}

// guestLabelByLocale 按语言返回匿名用户占位文案（查不到用户信息时兜底）。
func guestLabelByLocale(locale string, userID uint) string {
	switch locale {
	case constants.LocaleEnUS:
		return fmt.Sprintf("User #%d", userID)
	default:
		return fmt.Sprintf("用户#%d", userID)
	}
}

// senderLabel 返回通知文案中的发送人标识：客服走固定文案，用户则尽量取真实昵称/邮箱。
func (n *Notifier) senderLabel(senderType string, userID uint, locale string) string {
	if senderType == ticketdomain.SenderAdmin {
		return adminLabelByLocale(locale)
	}
	if n.users != nil && userID != 0 {
		if user, err := n.users.GetByID(userID); err == nil && user != nil {
			if name := strings.TrimSpace(user.DisplayName); name != "" {
				return name
			}
			if email := strings.TrimSpace(user.Email); email != "" {
				return email
			}
		}
	}
	return guestLabelByLocale(locale, userID)
}

// priorityLabel 把优先级枚举值按语言转换为可读文案，供通知模板变量使用。
func priorityLabel(priority string, locale string) string {
	switch locale {
	case constants.LocaleEnUS:
		switch priority {
		case ticketdomain.PriorityLow:
			return "Low"
		case ticketdomain.PriorityHigh:
			return "High"
		default:
			return "Normal"
		}
	default:
		switch priority {
		case ticketdomain.PriorityLow:
			return "低"
		case ticketdomain.PriorityHigh:
			return "高"
		default:
			return "中"
		}
	}
}

func excerpt(content string, maxRunes int) string {
	content = strings.TrimSpace(content)
	runes := []rune(content)
	if len(runes) <= maxRunes {
		return content
	}
	return fmt.Sprintf("%s...", string(runes[:maxRunes]))
}
