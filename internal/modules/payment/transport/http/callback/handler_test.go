package paymentcallbackhttp

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	ginutil "github.com/dujiao-next/internal/platform/http/ginutil"
	"github.com/dujiao-next/internal/shared/jsonmap"

	"github.com/gin-gonic/gin"
)

func TestPaymentCallbackRejectsUnknownPayload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/payments/callback", strings.NewReader(`{"payment_id":1,"status":"success"}`))
	c.Request.Header.Set("Content-Type", "application/json")

	(&Handler{}).PaymentCallback(c)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, w.Code)
	}
}

func TestParseCallbackFormNormalizesNonStandardQuery(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for name, target := range map[string]string{
		"semicolon separator":    "/api/v1/payments/callback?pid=2026;out_trade_no=ORDER-1;trade_status=TRADE_SUCCESS;sign=abc",
		"html escaped ampersand": "/api/v1/payments/callback?pid=2026&amp;out_trade_no=ORDER-1&amp;trade_status=TRADE_SUCCESS&amp;sign=abc",
	} {
		t.Run(name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodGet, target, nil)
			form, err := parseCallbackForm(c)
			if err != nil {
				t.Fatalf("parse callback form failed: %v", err)
			}
			for key, want := range map[string]string{"out_trade_no": "ORDER-1", "trade_status": "TRADE_SUCCESS", "sign": "abc"} {
				if got := getFirstValue(form, key); got != want {
					t.Fatalf("unexpected %s: %q", key, got)
				}
			}
		})
	}
}

func TestParseCallbackFormPrefersSignedPostForm(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/payments/callback?channel_id=999",
		strings.NewReader("out_trade_no=ORDER-POST&trade_status=TRADE_SUCCESS&sign=abc&notify_id=n1"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	c.Request = req

	form, err := parseCallbackForm(c)
	if err != nil {
		t.Fatalf("parse callback form failed: %v", err)
	}
	if got := getFirstValue(form, "out_trade_no"); got != "ORDER-POST" {
		t.Fatalf("unexpected out_trade_no: %s", got)
	}
	if got := getFirstValue(form, "channel_id"); got != "" {
		t.Fatalf("expected query param excluded from signed form, got %s", got)
	}
}

func TestWechatCallbackFeatureGuard(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := `{"id":"EV-1","resource":{"algorithm":"AEAD_AES_256_GCM"}}`
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/payments/callback", strings.NewReader(body))
	for key, value := range map[string]string{
		"Wechatpay-Signature": "mock-sign", "Wechatpay-Timestamp": "1760000000",
		"Wechatpay-Nonce": "mock-nonce", "Wechatpay-Serial": "mock-serial",
	} {
		c.Request.Header.Set(key, value)
	}
	if !isWechatCallbackRequest(c, []byte(body)) {
		t.Fatal("expected wechat callback request")
	}
	c.Request.Header.Del("Wechatpay-Signature")
	if isWechatCallbackRequest(c, []byte(body)) {
		t.Fatal("expected missing header to reject wechat callback")
	}
}

// alertRecorder 记录回调侧发出的告警，用于验证统一出口。
type alertRecorder struct {
	mu     sync.Mutex
	alerts []jsonmap.JSON
}

func (r *alertRecorder) EnqueuePaymentExceptionAlert(_, _, _ string, data jsonmap.JSON) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.alerts = append(r.alerts, data)
	return nil
}

func (r *alertRecorder) all() []jsonmap.JSON {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]jsonmap.JSON, len(r.alerts))
	copy(out, r.alerts)
	return out
}

// 所有回调渠道的 TG 告警都经 enqueuePaymentExceptionAlert 发出。
// 这里守护「发出告警的同时必须标记后台错误日志」，否则回调失败
// 只存在于 TG 消息里，后台错误日志页永远是空的。
func TestEnqueuePaymentExceptionAlert_MarksErrorLog(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := &alertRecorder{}
	h := &Handler{alerts: recorder}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/payment/callback", strings.NewReader(""))

	h.enqueuePaymentExceptionAlert(c, jsonmap.JSON{
		"alert_type":       "globepay_callback_handle_failed",
		"alert_level":      "error",
		"payment_id":       "2047",
		"partner_order_id": "DJP20261113115508698099",
		"message":          "payment gateway response invalid",
	})

	if len(recorder.all()) != 1 {
		t.Fatalf("应发出 1 条告警，实际 %d 条", len(recorder.all()))
	}

	status, ok := c.Get(ginutil.ServerErrorStatusKey)
	if !ok {
		t.Fatal("必须标记 ServerErrorStatusKey，否则 ErrorLogMiddleware 不会落库")
	}
	if code, _ := status.(int); code < 500 {
		t.Fatalf("标记状态码需 >=500 才能绕过 collect_4xx 开关，实际 %d", code)
	}

	if lv, _ := c.Get(ginutil.ServerErrorLevelKey); lv != "error" {
		t.Fatalf("alert_level=error 应映射为 error 级别，实际 %v", lv)
	}

	msg, _ := c.Get(ginutil.ServerErrorMessageKey)
	text, _ := msg.(string)
	for _, want := range []string{
		"globepay_callback_handle_failed",
		"payment gateway response invalid",
		"payment_id=2047",
		"DJP20261113115508698099",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("后台日志 message 缺少 %q，实际 %q", want, text)
		}
	}
}

// alert_level=warning 的告警（如 payment_not_found）应记为 warn，不能一律 error。
func TestEnqueuePaymentExceptionAlert_WarningMapsToWarnLevel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := &alertRecorder{}
	h := &Handler{alerts: recorder}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/payment/callback", strings.NewReader(""))

	h.enqueuePaymentExceptionAlert(c, jsonmap.JSON{
		"alert_type":  "nihaopay_callback_payment_not_found",
		"alert_level": "warning",
		"reference":   "REF-1",
	})

	if lv, _ := c.Get(ginutil.ServerErrorLevelKey); lv != "warn" {
		t.Fatalf("alert_level=warning 应映射为 warn 级别，实际 %v", lv)
	}
}

// 未配置告警出口时不能 panic，也不应标记（没有可发的告警就没有对应的错误日志）。
func TestEnqueuePaymentExceptionAlert_NilAlerterIsSafe(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/payment/callback", strings.NewReader(""))

	(&Handler{}).enqueuePaymentExceptionAlert(c, jsonmap.JSON{"alert_type": "x"})

	if _, ok := c.Get(ginutil.ServerErrorStatusKey); ok {
		t.Fatal("没有告警出口时不应标记错误日志")
	}
}

func TestEpusdtFeatureGuardFallsThroughWithoutPID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for name, body := range map[string]string{
		"bepusdt payload": `{"trade_id":"t1","order_id":"o1","status":2,"signature":"x"}`,
		"empty payload":   "",
	} {
		t.Run(name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/payments/callback", bytes.NewBufferString(body))
			if handled := (&Handler{}).handleEpusdtCallback(c); handled {
				t.Fatal("expected epusdt feature guard to fall through")
			}
		})
	}
}
