package consumer

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dujiao-next/internal/logger"
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

	subject := fmt.Sprintf("您的工单 %s 有新回复", ticket.TicketNo)
	body := fmt.Sprintf(
		"您好，\n\n您的工单「%s」（工单号：%s）收到了客服的新回复：\n\n%s\n\n请登录网站查看详情并回复。",
		ticket.Title, ticket.TicketNo, strings.TrimSpace(replyContent),
	)

	if err := c.EmailSender.SendCustomEmail(receiverEmail, subject, body); err != nil {
		logger.Warnw("worker_ticket_message_email_send_failed", "ticket_id", payload.TicketID, "receiver", receiverEmail, "error", err)
		return err
	}
	return nil
}
