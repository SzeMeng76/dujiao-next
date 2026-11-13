package paymentcallbackhttp

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	paymentdomain "github.com/dujiao-next/internal/modules/payment/domain"

	ginutil "github.com/dujiao-next/internal/platform/http/ginutil"

	"github.com/dujiao-next/internal/shared/jsonmap"

	"github.com/gin-gonic/gin"
)

const maxCallbackBodyBytes = 1 << 20

// WechatWebhookInput is the transport-owned input for a WeChat callback.
type WechatWebhookInput struct {
	ChannelID uint
	Headers   map[string]string
	Body      []byte
	Context   context.Context
}

// BinancepayWebhookInput is the transport-owned input for a Binance Pay callback.
type BinancepayWebhookInput struct {
	ChannelID uint
	Headers   map[string]string
	Body      []byte
	Context   context.Context
}

// Service is the application boundary used by synchronous payment callbacks.
type Service interface {
	HandleSyncCallback(channel *paymentdomain.PaymentChannel, form map[string][]string, body []byte) (*paymentdomain.Payment, error)
	HandleWechatWebhook(input WechatWebhookInput) (*paymentdomain.Payment, string, error)
	HandleBinancepayWebhook(input BinancepayWebhookInput) (*paymentdomain.Payment, string, error)
}

// PaymentLookup contains only the payment reads required to locate callback targets.
type PaymentLookup interface {
	GetByGatewayOrderNo(gatewayOrderNo string) (*paymentdomain.Payment, error)
	GetLatestByProviderRef(providerRef string) (*paymentdomain.Payment, error)
}

// ChannelLookup contains only the channel read required to validate a callback provider.
type ChannelLookup interface {
	GetByID(id uint) (*paymentdomain.PaymentChannel, error)
}

// ExceptionAlerter queues operational callback alerts without coupling HTTP to notifications.
type ExceptionAlerter interface {
	EnqueuePaymentExceptionAlert(method, path, clientIP string, data jsonmap.JSON) error
}

// Handler dispatches the shared synchronous callback endpoint to its provider protocol.
type Handler struct {
	service  Service
	payments PaymentLookup
	channels ChannelLookup
	alerts   ExceptionAlerter
}

func NewHandler(service Service, payments PaymentLookup, channels ChannelLookup, alerts ExceptionAlerter) *Handler {
	if service == nil || payments == nil || channels == nil {
		panic("payment callback handler: required dependency is nil")
	}
	return &Handler{service: service, payments: payments, channels: channels, alerts: alerts}
}

// PaymentCallback preserves the historical provider detection order on the shared endpoint.
func (h *Handler) PaymentCallback(c *gin.Context) {
	if c.Request.Body != nil {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxCallbackBodyBytes)
	}
	ginutil.RequestLog(c).Infow("payment_callback_received",
		"method", c.Request.Method,
		"client_ip", c.ClientIP(),
		"content_type", strings.TrimSpace(c.GetHeader("Content-Type")),
	)
	for _, handle := range []func(*gin.Context) bool{
		h.handleWechatCallback,
		h.handleOkpayCallback,
		h.handleAlipayCallback,
		h.handleEpayCallback,
		h.handleTokenPayCallback,
		h.handleEpusdtCallback,
		h.handleBepusdtCallback,
		h.handleGlobepayCallback,
		h.handleNihaopayCallback,
	} {
		if handle(c) {
			return
		}
	}

	ginutil.RequestLog(c).Warnw("payment_callback_unrecognized",
		"method", c.Request.Method,
		"client_ip", c.ClientIP(),
		"content_type", strings.TrimSpace(c.GetHeader("Content-Type")),
	)
	h.enqueuePaymentExceptionAlert(c, jsonmap.JSON{
		"alert_type":  "callback_unrecognized",
		"alert_level": "warning",
		"message":     "支付回调请求无法匹配已支持的回调格式",
	})
	c.AbortWithStatus(http.StatusNotFound)
}

// enqueuePaymentExceptionAlert 是回调侧所有异常告警的唯一出口，
// 同时负责把同一条异常写入后台错误日志（error_logs 表）。
//
// 为什么要在这里写：支付回调端点必须对网关返回 HTTP 200（否则网关按投递失败
// 无限重试），失败信息只存在于响应体和 TG 通知里。ErrorLogMiddleware 依赖状态码
// 判断是否落库，对这类端点永远命中不了，导致后台错误日志一片空白、TG 消息滚走后
// 无法追溯。这里显式标记，让所有渠道（alipay/epay/globepay/nihaopay/wechat）统一生效。
func (h *Handler) enqueuePaymentExceptionAlert(c *gin.Context, data jsonmap.JSON) {
	if h == nil || h.alerts == nil || c == nil || c.Request == nil {
		return
	}
	path := ""
	if c.Request.URL != nil {
		path = strings.TrimSpace(c.Request.URL.Path)
	}

	// 与 TG 告警保持同一份事实：级别取 alert_level，内容取 alert_type + message，
	// 再拼上定位异常所需的业务字段，避免后台只剩一句笼统的失败描述。
	markCallbackErrorLog(c, data)

	if err := h.alerts.EnqueuePaymentExceptionAlert(
		strings.TrimSpace(c.Request.Method),
		path,
		strings.TrimSpace(c.ClientIP()),
		data,
	); err != nil {
		ginutil.RequestLog(c).Warnw("enqueue_payment_exception_alert_failed", "error", err)
	}
}

// markCallbackErrorLog 把回调异常标记给 ErrorLogMiddleware 落库。
func markCallbackErrorLog(c *gin.Context, data jsonmap.JSON) {
	message := strings.TrimSpace(readAlertString(data, "message"))
	// 补上订单/支付标识：后台排查时这些字段比错误文案更关键。
	for _, key := range []string{"payment_id", "partner_order_id", "out_trade_no", "reference", "channel_id", "provider"} {
		if v := strings.TrimSpace(readAlertString(data, key)); v != "" {
			message += " [" + key + "=" + v + "]"
		}
	}

	ginutil.MarkAlertErrorLog(c,
		readAlertString(data, "alert_type"),
		readAlertString(data, "alert_level"),
		message,
	)
}

// readAlertString 读取告警字段的字符串形式，兼容 JSON 里可能出现的数字类型。
func readAlertString(data jsonmap.JSON, key string) string {
	v, ok := data[key]
	if !ok || v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}

func getFirstValue(form map[string][]string, key string) string {
	if values, ok := form[key]; ok && len(values) > 0 {
		return values[0]
	}
	return ""
}
