package consumer

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/dujiao-next/internal/logger"
	"github.com/dujiao-next/internal/modules/notification/application/format"
	settingsmessaging "github.com/dujiao-next/internal/modules/settings/schema/messaging"
	"github.com/dujiao-next/internal/queue"
	"github.com/dujiao-next/internal/telegramidentity"

	"github.com/hibiken/asynq"
)

// handleTicketMessageEmail 处理工单新消息邮件通知任务（通知工单发起人：客服已回复）。
func (c *Consumer) handleTicketMessageEmail(ctx context.Context, task *asynq.Task) error {
	if c == nil || task == nil {
		return nil
	}
	var payload queue.TicketMessageEmailPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		logger.Warnw("worker_ticket_message_email_unmarshal_failed", "error", err)
		return err
	}
	if payload.TicketID == 0 {
		return nil
	}

	// 消费侧二次校验：SMTP 未启用则跳过，避免堆积任务在开关关闭期间持续报错重试。
	if c.SettingService != nil && c.Config != nil {
		smtpSetting, err := c.SettingService.GetSMTPSetting(c.Config.Email)
		if err != nil {
			logger.Warnw("worker_ticket_message_email_load_smtp_setting_failed", "ticket_id", payload.TicketID, "error", err)
			return nil
		}
		if !smtpSetting.Enabled {
			logger.Debugw("worker_ticket_message_email_skip_smtp_disabled", "ticket_id", payload.TicketID)
			return nil
		}
	}

	if c.TicketStore == nil || c.UserStore == nil || c.EmailSender == nil {
		logger.Debugw("worker_ticket_message_email_skip_dependency_nil", "ticket_id", payload.TicketID)
		return nil
	}

	ticket, err := c.TicketStore.GetByID(payload.TicketID)
	if err != nil {
		logger.Warnw("worker_ticket_message_email_fetch_ticket_failed", "ticket_id", payload.TicketID, "error", err)
		return err
	}
	if ticket == nil {
		return nil
	}

	user, err := c.UserStore.GetByID(ticket.UserID)
	if err != nil {
		logger.Warnw("worker_ticket_message_email_fetch_user_failed", "ticket_id", payload.TicketID, "user_id", ticket.UserID, "error", err)
		return err
	}
	if user == nil {
		return nil
	}
	receiverEmail := strings.TrimSpace(user.Email)
	if receiverEmail == "" || telegramidentity.IsPlaceholderEmail(receiverEmail) {
		logger.Debugw("worker_ticket_message_email_skip_invalid_receiver", "ticket_id", payload.TicketID)
		return nil
	}

	var replyContent string
	for _, message := range ticket.Messages {
		if message.ID == payload.MessageID {
			replyContent = message.Content
			break
		}
	}

	locale := settingsmessaging.NormalizeNotificationLocale(user.Locale)
	subject, body := c.renderTicketReplyEmail(locale, ticket.Title, ticket.TicketNo, strings.TrimSpace(replyContent))

	if err := c.EmailSender.SendCustomEmail(receiverEmail, subject, body); err != nil {
		logger.Warnw("worker_ticket_message_email_send_failed", "ticket_id", payload.TicketID, "receiver", receiverEmail, "error", err)
		return err
	}
	return nil
}

// renderTicketReplyEmail 按站长在"订单邮件模板"设置里配置的 ticket_reply 场景渲染标题与正文。
func (c *Consumer) renderTicketReplyEmail(locale, title, ticketNo, replyContent string) (subject, body string) {
	tmplSetting := settingsmessaging.DefaultOrderEmailTemplateSetting()
	if c.SettingService != nil {
		if setting, err := c.SettingService.GetOrderEmailTemplateSetting(); err == nil {
			tmplSetting = setting
		} else {
			logger.Warnw("worker_ticket_message_email_load_template_failed", "error", err)
		}
	}

	var siteName, siteURL string
	if c.SettingService != nil {
		if brand, err := c.SettingService.GetSiteBrand(); err == nil {
			siteName = strings.TrimSpace(brand.SiteName)
			siteURL = strings.TrimRight(strings.TrimSpace(brand.SiteURL), "/")
		}
	}

	tmpl := settingsmessaging.ResolveOrderEmailLocaleTemplate(tmplSetting.Templates.TicketReply, locale)
	variables := map[string]interface{}{
		"ticket_no":     ticketNo,
		"ticket_title":  title,
		"reply_content": replyContent,
		"site_name":     siteName,
		"site_url":      siteURL,
	}
	subject = format.RenderTemplate(tmpl.Subject, variables)
	body = format.RenderTemplate(tmpl.Body, variables)
	return subject, body
}
