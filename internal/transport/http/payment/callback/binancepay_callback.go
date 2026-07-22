package paymentcallbackhttp

import (
	"encoding/json"
	"net/http"
	"strings"

	ginutil "github.com/dujiao-next/internal/platform/http/ginutil"

	"github.com/dujiao-next/internal/constants"
	"github.com/dujiao-next/internal/shared/jsonmap"

	"github.com/gin-gonic/gin"
)

const (
	binancepayCallbackRespCodeSuccess = "SUCCESS"
	binancepayCallbackRespCodeFail    = "FAIL"
)

type binancepayCallbackQuery struct {
	ChannelID uint `form:"channel_id"`
}

func (h *Handler) handleBinancepayCallback(c *gin.Context) bool {
	log := ginutil.RequestLog(c)
	body, ok := readCallbackBody(c)
	if !ok || !isBinancepayCallbackRequest(c, body) {
		log.Debugw("binancepay_callback_not_matched")
		return false
	}
	var query binancepayCallbackQuery
	_ = c.ShouldBindQuery(&query)
	log.Infow("binancepay_callback_received", "channel_id", query.ChannelID, "client_ip", c.ClientIP(), "body_size", len(body),
		"binancepay_signature", truncateCallbackLogValue(strings.TrimSpace(c.GetHeader("BinancePay-Signature"))),
		"binancepay_timestamp", strings.TrimSpace(c.GetHeader("BinancePay-Timestamp")),
		"binancepay_nonce", truncateCallbackLogValue(strings.TrimSpace(c.GetHeader("BinancePay-Nonce"))),
		"raw_body", callbackRawBodyForLog(body))

	payment, _, err := h.service.HandleBinancepayWebhook(BinancepayWebhookInput{
		ChannelID: query.ChannelID, Headers: collectRequestHeaders(c), Body: body, Context: c.Request.Context(),
	})
	if err != nil {
		log.Warnw("binancepay_callback_handle_failed", "channel_id", query.ChannelID, "error", err)
		h.enqueuePaymentExceptionAlert(c, jsonmap.JSON{
			"alert_type": "binancepay_callback_handle_failed", "alert_level": "error",
			"message": strings.TrimSpace(err.Error()), "provider": constants.PaymentChannelTypeBinancepay,
		})
		respondBinancepayCallback(c, false)
		return true
	}
	if payment == nil {
		log.Infow("binancepay_callback_accepted_no_payment", "channel_id", query.ChannelID)
		respondBinancepayCallback(c, true)
		return true
	}
	log.Infow("binancepay_callback_processed", "channel_id", query.ChannelID, "payment_id", payment.ID, "status", payment.Status)
	respondBinancepayCallback(c, true)
	return true
}

func isBinancepayCallbackRequest(c *gin.Context, body []byte) bool {
	for _, header := range []string{"BinancePay-Signature", "BinancePay-Timestamp", "BinancePay-Nonce"} {
		if strings.TrimSpace(c.GetHeader(header)) == "" {
			return false
		}
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return false
	}
	bizType := strings.TrimSpace(strings.ToLower(getString(payload, "bizType")))
	return bizType != ""
}

func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func respondBinancepayCallback(c *gin.Context, success bool) {
	if success {
		c.JSON(http.StatusOK, gin.H{"returnCode": binancepayCallbackRespCodeSuccess, "returnMessage": ""})
		return
	}
	c.JSON(http.StatusBadRequest, gin.H{"returnCode": binancepayCallbackRespCodeFail, "returnMessage": "处理失败"})
}
