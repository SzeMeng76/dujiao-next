package public

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"github.com/dujiao-next/internal/constants"
	"github.com/dujiao-next/internal/http/handlers/shared"
	"github.com/dujiao-next/internal/models"

	"github.com/gin-gonic/gin"
)

func (h *Handler) HandleGlobepayCallback(c *gin.Context) bool {
	log := shared.RequestLog(c)

	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return false
	}
	c.Request.Body = io.NopCloser(bytes.NewBuffer(body))

	// Globepay 回调 content_type 可能为空，强制设置为 form 格式以确保 ParseForm 能解析 body
	if strings.TrimSpace(c.Request.Header.Get("Content-Type")) == "" {
		c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}

	// 特征检测：globepay 回调包含 partner_order_id + sign + time + nonce_str
	form, parseErr := parseCallbackForm(c)
	if parseErr != nil {
		return false
	}

	partnerOrderID := strings.TrimSpace(getFirstValue(form, "partner_order_id"))
	sign := strings.TrimSpace(getFirstValue(form, "sign"))
	if partnerOrderID == "" || sign == "" {
		log.Debugw("globepay_callback_not_matched", "reason", "missing_partner_order_id_or_sign")
		return false
	}

	log.Infow("globepay_callback_received",
		"partner_order_id", partnerOrderID,
		"raw_form", callbackRawFormForLog(form),
	)

	payment, err := h.PaymentRepo.GetByGatewayOrderNo(partnerOrderID)
	if err != nil || payment == nil {
		log.Warnw("globepay_callback_payment_not_found", "partner_order_id", partnerOrderID, "error", err)
		c.String(200, "fail")
		return true
	}

	log.Debugw("globepay_callback_payment_found", "payment_id", payment.ID, "channel_id", payment.ChannelID)

	channel, err := h.PaymentChannelRepo.GetByID(payment.ChannelID)
	if err != nil || channel == nil {
		log.Warnw("globepay_callback_channel_not_found", "payment_id", payment.ID, "channel_id", payment.ChannelID, "error", err)
		c.String(200, "fail")
		return true
	}
	if strings.ToLower(strings.TrimSpace(channel.ProviderType)) != constants.PaymentProviderGlobepay {
		log.Warnw("globepay_callback_provider_invalid", "provider_type", channel.ProviderType)
		return false
	}

	updated, err := h.PaymentService.HandleSyncCallback(channel, form, body)
	if err != nil {
		log.Errorw("globepay_callback_handle_failed", "payment_id", payment.ID, "error", err)
		h.enqueuePaymentExceptionAlert(c, models.JSON{
			"alert_type":       "globepay_callback_handle_failed",
			"alert_level":      "error",
			"payment_id":       fmt.Sprintf("%d", payment.ID),
			"partner_order_id": partnerOrderID,
			"message":          strings.TrimSpace(err.Error()),
			"provider":         constants.PaymentProviderGlobepay,
		})
		c.String(200, "fail")
		return true
	}

	log.Infow("globepay_callback_processed", "payment_id", payment.ID, "status", updated.Status)
	c.String(200, "SUCCESS")
	return true
}
