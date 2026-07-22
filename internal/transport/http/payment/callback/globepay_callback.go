package paymentcallbackhttp

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/dujiao-next/internal/constants"
	"github.com/dujiao-next/internal/platform/http/ginutil"
	"github.com/dujiao-next/internal/shared/jsonmap"

	"github.com/gin-gonic/gin"
)

var errGlobepayCallbackPaymentNotFound = errors.New("globepay callback payment not found")

func (h *Handler) handleGlobepayCallback(c *gin.Context) bool {
	log := ginutil.RequestLog(c)

	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return false
	}
	c.Request.Body = io.NopCloser(bytes.NewBuffer(body))

	// Globepay 回调是 JSON 格式，从 JSON body 解析字段
	var jsonData map[string]interface{}
	form := make(map[string][]string)
	contentType := strings.ToLower(strings.TrimSpace(c.Request.Header.Get("Content-Type")))
	if strings.Contains(contentType, "application/json") || contentType == "" {
		if err := json.Unmarshal(body, &jsonData); err == nil {
			for k, v := range jsonData {
				form[k] = []string{fmt.Sprintf("%v", v)}
			}
		}
	}
	// 兼容 form 格式
	if len(form) == 0 {
		var parseErr error
		form, parseErr = parseCallbackForm(c)
		if parseErr != nil {
			return false
		}
	}

	// 特征检测：globepay 回调包含 partner_order_id + sign
	partnerOrderID := strings.TrimSpace(getFirstValue(form, "partner_order_id"))
	sign := strings.TrimSpace(getFirstValue(form, "sign"))
	if partnerOrderID == "" || sign == "" {
		log.Debugw("globepay_callback_not_matched", "reason", "missing_partner_order_id_or_sign")
		return false
	}

	log.Infow("globepay_callback_received",
		"client_ip", c.ClientIP(),
		"partner_order_id", partnerOrderID,
		"raw_form", callbackRawFormForLog(form),
	)

	payment, err := h.payments.GetByGatewayOrderNo(partnerOrderID)
	if err != nil || payment == nil {
		log.Warnw("globepay_callback_payment_not_found", "partner_order_id", partnerOrderID, "error", err)
		h.enqueuePaymentExceptionAlert(c, jsonmap.JSON{
			"alert_type":       "globepay_callback_payment_not_found",
			"alert_level":      "warning",
			"partner_order_id": partnerOrderID,
			"message":          "Globepay 回调找不到对应的支付订单",
			"provider":         constants.PaymentProviderGlobepay,
		})
		c.String(http.StatusOK, "fail")
		return true
	}

	log.Debugw("globepay_callback_payment_found", "payment_id", payment.ID, "channel_id", payment.ChannelID)

	channel, err := h.channels.GetByID(payment.ChannelID)
	if err != nil || channel == nil {
		log.Warnw("globepay_callback_channel_not_found", "payment_id", payment.ID, "channel_id", payment.ChannelID, "error", err)
		c.String(http.StatusOK, "fail")
		return true
	}
	if strings.ToLower(strings.TrimSpace(channel.ProviderType)) != constants.PaymentProviderGlobepay {
		log.Warnw("globepay_callback_provider_invalid", "provider_type", channel.ProviderType)
		return false
	}

	updated, err := h.service.HandleSyncCallback(channel, form, body)
	if err != nil {
		log.Errorw("globepay_callback_handle_failed", "payment_id", payment.ID, "error", err)
		h.enqueuePaymentExceptionAlert(c, jsonmap.JSON{
			"alert_type":       "globepay_callback_handle_failed",
			"alert_level":      "error",
			"payment_id":       fmt.Sprintf("%d", payment.ID),
			"partner_order_id": partnerOrderID,
			"message":          strings.TrimSpace(err.Error()),
			"provider":         constants.PaymentProviderGlobepay,
		})
		c.String(http.StatusOK, "fail")
		return true
	}

	log.Infow("globepay_callback_processed", "payment_id", payment.ID, "status", updated.Status)
	c.String(http.StatusOK, "SUCCESS")
	return true
}

