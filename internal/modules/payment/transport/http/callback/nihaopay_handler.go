package paymentcallbackhttp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	ginutil "github.com/dujiao-next/internal/platform/http/ginutil"
	"github.com/dujiao-next/internal/shared/jsonmap"
	"github.com/gin-gonic/gin"
)

func (h *Handler) handleNihaopayCallback(c *gin.Context) bool {
	log := ginutil.RequestLog(c)

	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return false
	}
	c.Request.Body = io.NopCloser(bytes.NewBuffer(body))

	// Nihaopay 回调可能是 JSON 或 form 格式
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
		if err := c.Request.ParseForm(); err == nil {
			form = c.Request.Form
		}
	}

	if len(form) == 0 {
		return false
	}

	// 检查是否是 Nihaopay 回调（必须有 reference 字段）
	reference := getFirstValue(form, "reference")
	if reference == "" {
		return false
	}

	log.Infow("nihaopay_callback_received",
		"client_ip", c.ClientIP(),
		"reference", reference,
		"status", getFirstValue(form, "status"),
	)

	// 使用 reference（即 gateway_order_no）查找支付记录
	payment, err := h.payments.GetByGatewayOrderNo(reference)
	if err != nil || payment == nil {
		log.Warnw("nihaopay_callback_payment_not_found", "reference", reference, "error", err)
		h.enqueuePaymentExceptionAlert(c, jsonmap.JSON{
			"alert_type":  "nihaopay_callback_payment_not_found",
			"alert_level": "warning",
			"reference":   reference,
			"status":      getFirstValue(form, "status"),
		})
		c.String(http.StatusOK, "fail")
		return true
	}

	channel, err := h.channels.GetByID(payment.ChannelID)
	if err != nil || channel == nil {
		log.Warnw("nihaopay_callback_channel_not_found", "channel_id", payment.ChannelID, "error", err)
		h.enqueuePaymentExceptionAlert(c, jsonmap.JSON{
			"alert_type":  "nihaopay_callback_channel_not_found",
			"alert_level": "warning",
			"channel_id":  payment.ChannelID,
		})
		c.String(http.StatusOK, "fail")
		return true
	}

	// 使用统一的回调处理
	updatedPayment, err := h.service.HandleSyncCallback(channel, form, nil)
	if err != nil {
		log.Warnw("nihaopay_callback_handle_failed", "reference", reference, "error", err)
		h.enqueuePaymentExceptionAlert(c, jsonmap.JSON{
			"alert_type":  "nihaopay_callback_handle_failed",
			"alert_level": "error",
			"reference":   reference,
			"error":       err.Error(),
		})
		c.String(http.StatusOK, "fail")
		return true
	}

	if updatedPayment != nil {
		log.Infow("nihaopay_callback_processed", "reference", reference, "payment_id", payment.ID)
	} else {
		log.Infow("nihaopay_callback_no_update", "reference", reference, "payment_id", payment.ID)
	}

	c.String(http.StatusOK, "success")
	return true
}
