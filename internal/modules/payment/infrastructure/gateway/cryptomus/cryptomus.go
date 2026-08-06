// Package cryptomus 封装 Cryptomus (https://cryptomus.com) 商户 API。
//
// Cryptomus 和 HashPay 的交互模型相同：下单只传法币金额，不指定具体加密币种/链，
// 返回一个托管收银台 URL，付款人在收银台页面自行选择要支付的币种和网络；
// 因此本包只支持 redirect 交互模式，不做站内二维码渲染。
//
// API Base URL 是 Cryptomus 官方固定地址（无沙箱环境），不支持自定义。
package cryptomus

import (
	"bytes"
	"context"
	"crypto/md5" //nolint:gosec // Cryptomus 官方文档规定的签名算法固定使用 MD5，非我方可选
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/dujiao-next/internal/constants"
	"github.com/dujiao-next/internal/modules/payment/infrastructure/gateway/common"
)

var (
	ErrConfigInvalid    = errors.New("cryptomus config invalid")
	ErrRequestFailed    = errors.New("cryptomus request failed")
	ErrResponseInvalid  = errors.New("cryptomus response invalid")
	ErrSignatureInvalid = errors.New("cryptomus signature invalid")
)

// apiBaseURL 是 Cryptomus 官方 API 固定地址，官方未提供沙箱环境。
// 声明为 var（而非 const）仅为了让同包测试可以指向 httptest.Server。
var apiBaseURL = "https://api.cryptomus.com/v1"

const (
	createPaymentPath = "/payment"
	paymentInfoPath   = "/payment/info"
	servicesPath      = "/payment/services"

	// Cryptomus 发票状态（https://doc.cryptomus.com/merchant-api/payments/payment-statuses）
	StatusPaid               = "paid"
	StatusPaidOver           = "paid_over"
	StatusWrongAmount        = "wrong_amount"
	StatusWrongAmountWaiting = "wrong_amount_waiting"
	StatusProcess            = "process"
	StatusConfirmCheck       = "confirm_check"
	StatusCheck              = "check"
	StatusFail               = "fail"
	StatusCancel             = "cancel"
	StatusSystemFail         = "system_fail"
	StatusRefundProcess      = "refund_process"
	StatusRefundFail         = "refund_fail"
	StatusRefundPaid         = "refund_paid"
	StatusLocked             = "locked"
)

// Config Cryptomus 配置。merchant_id 和 payment_api_key 均从 Cryptomus 商户后台
// 「API keys」页面获取；payment_api_key 同时用于请求签名和 webhook 验签。
type Config struct {
	MerchantID    string `json:"merchant_id"`
	PaymentAPIKey string `json:"payment_api_key"`
	Currency      string `json:"currency,omitempty"` // 下单法币币种，留空使用站点订单币种
	NotifyURL     string `json:"notify_url"`         // url_callback
	ReturnURL     string `json:"return_url"`         // url_success，支付成功后跳转
}

// ParseConfig 把 channel.ConfigJSON 反序列化为 Config。
func ParseConfig(raw map[string]interface{}) (*Config, error) {
	return common.ParseConfig[Config](raw, ErrConfigInvalid)
}

// Normalize 统一字段格式。
func (c *Config) Normalize() {
	if c == nil {
		return
	}
	c.MerchantID = strings.TrimSpace(c.MerchantID)
	c.PaymentAPIKey = strings.TrimSpace(c.PaymentAPIKey)
	c.Currency = strings.ToUpper(strings.TrimSpace(c.Currency))
	c.NotifyURL = strings.TrimSpace(c.NotifyURL)
	c.ReturnURL = strings.TrimSpace(c.ReturnURL)
}

// ValidateConfig 校验必填字段。
func ValidateConfig(cfg *Config) error {
	if cfg == nil {
		return fmt.Errorf("%w: config is nil", ErrConfigInvalid)
	}
	checks := []struct {
		field string
		val   string
	}{
		{"merchant_id", cfg.MerchantID},
		{"payment_api_key", cfg.PaymentAPIKey},
	}
	for _, c := range checks {
		if strings.TrimSpace(c.val) == "" {
			return fmt.Errorf("%w: %s is required", ErrConfigInvalid, c.field)
		}
	}
	return nil
}

// CreateInput 创建发票输入。
type CreateInput struct {
	OrderNo   string
	Amount    string
	Currency  string
	NotifyURL string
	ReturnURL string
}

// CreateResult 创建发票结果。
type CreateResult struct {
	UUID        string
	OrderID     string
	CheckoutURL string
	Amount      string
	Currency    string
	ExpiresAt   int64
	Status      string
	Raw         map[string]interface{}
}

// QueryResult 查询发票结果。
type QueryResult struct {
	UUID     string
	OrderID  string
	Amount   string
	Currency string
	Status   string
	Raw      map[string]interface{}
}

// CallbackPayload 是 webhook 验签通过后的业务字段。
type CallbackPayload struct {
	UUID     string
	OrderID  string
	Amount   string
	Currency string
	Status   string
	Raw      map[string]interface{}
}

// ToPaymentStatus 把 Cryptomus 发票状态映射为内部 payment 状态。
// 退款相关状态（refund_*）和 AML 锁定（locked）不属于建单支付生命周期，返回空串由上层忽略。
func ToPaymentStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case StatusPaid, StatusPaidOver:
		return constants.PaymentStatusSuccess
	case StatusCancel:
		return constants.PaymentStatusExpired
	case StatusWrongAmount, StatusFail, StatusSystemFail:
		return constants.PaymentStatusFailed
	case StatusProcess, StatusConfirmCheck, StatusCheck, StatusWrongAmountWaiting:
		return constants.PaymentStatusPending
	default:
		return ""
	}
}

// CreatePayment 调 POST /v1/payment 创建（或复用，order_id 相同时 Cryptomus 直接返回已有发票）发票。
// 不传 to_currency/network，付款人在托管收银台自选币种和网络，因此只支持 redirect 交互模式。
func CreatePayment(ctx context.Context, cfg *Config, input CreateInput) (*CreateResult, error) {
	if err := ValidateConfig(cfg); err != nil {
		return nil, err
	}
	orderNo := strings.TrimSpace(input.OrderNo)
	amount := strings.TrimSpace(input.Amount)
	if orderNo == "" || amount == "" {
		return nil, fmt.Errorf("%w: order_no and amount are required", ErrConfigInvalid)
	}
	if parsed, err := strconv.ParseFloat(amount, 64); err != nil || parsed <= 0 {
		return nil, fmt.Errorf("%w: invalid amount %q", ErrConfigInvalid, input.Amount)
	}
	currency := strings.ToUpper(strings.TrimSpace(input.Currency))
	if cfg.Currency != "" {
		currency = cfg.Currency
	}
	if currency == "" {
		return nil, fmt.Errorf("%w: currency is required", ErrConfigInvalid)
	}

	notifyURL := common.PickFirstNonEmpty(input.NotifyURL, cfg.NotifyURL)
	returnURL := common.PickFirstNonEmpty(input.ReturnURL, cfg.ReturnURL)

	payload := map[string]interface{}{
		"amount":   amount,
		"currency": currency,
		"order_id": orderNo,
	}
	if notifyURL != "" {
		payload["url_callback"] = notifyURL
	}
	if returnURL != "" {
		payload["url_success"] = returnURL
	}

	raw, err := signedRequest(ctx, cfg, createPaymentPath, payload)
	if err != nil {
		return nil, err
	}

	result := &CreateResult{
		UUID:        common.ReadString(raw, "uuid"),
		OrderID:     common.ReadString(raw, "order_id"),
		CheckoutURL: common.ReadString(raw, "url"),
		Amount:      common.ReadString(raw, "amount"),
		Currency:    common.ReadString(raw, "currency"),
		Status:      common.ReadString(raw, "status"),
		Raw:         raw,
	}
	if v, err := strconv.ParseInt(common.ReadString(raw, "expired_at"), 10, 64); err == nil {
		result.ExpiresAt = v
	}
	if result.UUID == "" || result.CheckoutURL == "" {
		return nil, fmt.Errorf("%w: missing uuid/url", ErrResponseInvalid)
	}
	return result, nil
}

// QueryPayment 调 POST /v1/payment/info 按 Cryptomus 发票 uuid 查询状态。
func QueryPayment(ctx context.Context, cfg *Config, uuid string) (*QueryResult, error) {
	if err := ValidateConfig(cfg); err != nil {
		return nil, err
	}
	uuid = strings.TrimSpace(uuid)
	if uuid == "" {
		return nil, fmt.Errorf("%w: uuid is required", ErrConfigInvalid)
	}

	raw, err := signedRequest(ctx, cfg, paymentInfoPath, map[string]interface{}{"uuid": uuid})
	if err != nil {
		return nil, err
	}

	result := &QueryResult{
		UUID:     common.ReadString(raw, "uuid"),
		OrderID:  common.ReadString(raw, "order_id"),
		Amount:   common.ReadString(raw, "amount"),
		Currency: common.ReadString(raw, "currency"),
		Status:   common.ReadString(raw, "status"),
		Raw:      raw,
	}
	if result.UUID == "" || result.Status == "" {
		return nil, fmt.Errorf("%w: missing uuid/status", ErrResponseInvalid)
	}
	return result, nil
}

// CheckHealth 调 POST /v1/payment/services 验证商户凭证是否有效（只读、无副作用）。
func CheckHealth(ctx context.Context, cfg *Config) error {
	if err := ValidateConfig(cfg); err != nil {
		return err
	}
	_, err := signedRequest(ctx, cfg, servicesPath, map[string]interface{}{})
	return err
}

// DecryptCallback 名称沿用 hashpay 包习惯，实际是校验 Cryptomus webhook 签名（非加密信封）。
// 算法：从原始请求体中原样摘出除 sign 外的所有键值对（保留原始顺序和原始字节，不做任何
// 反序列化再序列化），base64 编码后与 payment_api_key 拼接做 MD5，和 sign 字段比对。
// 之所以要保留原始字节而不是重新 json.Marshal，是因为 Cryptomus 服务端用 PHP 生成签名时
// json_encode 默认会把字符串里的 "/" 转义成 "\/"，而 Go/JS 的 JSON 编码器默认不转义 "/"；
// 如果重新序列化会导致签出的哈希和 Cryptomus 原始哈希不一致（官方文档专门提到了这个坑）。
// 直接摘抄原始字节可以绕开这个问题。
func VerifyWebhookSignature(cfg *Config, body []byte) (*CallbackPayload, error) {
	if cfg == nil {
		return nil, fmt.Errorf("%w: config is nil", ErrConfigInvalid)
	}
	if strings.TrimSpace(cfg.PaymentAPIKey) == "" {
		return nil, fmt.Errorf("%w: payment_api_key is required", ErrConfigInvalid)
	}

	unsigned, sign, err := extractSignPreservingOrder(body)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrResponseInvalid, err)
	}
	if sign == "" {
		return nil, fmt.Errorf("%w: missing sign", ErrResponseInvalid)
	}

	expected := signPayload([]byte(unsigned), cfg.PaymentAPIKey)
	if !strings.EqualFold(expected, sign) {
		return nil, fmt.Errorf("%w: signature mismatch", ErrSignatureInvalid)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("%w: decode payload failed", ErrResponseInvalid)
	}

	status := common.ReadString(raw, "status")
	if status == "" {
		status = common.ReadString(raw, "payment_status")
	}
	payload := &CallbackPayload{
		UUID:     common.ReadString(raw, "uuid"),
		OrderID:  common.ReadString(raw, "order_id"),
		Amount:   common.ReadString(raw, "amount"),
		Currency: strings.ToUpper(common.ReadString(raw, "currency")),
		Status:   status,
		Raw:      raw,
	}
	if payload.UUID == "" || payload.OrderID == "" || payload.Status == "" {
		return nil, fmt.Errorf("%w: missing uuid/order_id/status", ErrResponseInvalid)
	}
	if payload.Amount == "" || payload.Currency == "" {
		return nil, fmt.Errorf("%w: missing amount/currency", ErrResponseInvalid)
	}
	if amount, err := strconv.ParseFloat(payload.Amount, 64); err != nil || amount <= 0 {
		return nil, fmt.Errorf("%w: invalid amount %q", ErrResponseInvalid, payload.Amount)
	}
	return payload, nil
}

// signPayload 按 Cryptomus 文档算法计算签名：
// MD5(base64(body) + payment_api_key)，十六进制小写输出。
func signPayload(body []byte, apiKey string) string {
	b64 := base64.StdEncoding.EncodeToString(body)
	sum := md5.Sum([]byte(b64 + apiKey)) //nolint:gosec // 算法由 Cryptomus 官方文档规定
	return hex.EncodeToString(sum[:])
}

// extractSignPreservingOrder 从原始 JSON 对象里摘出 "sign" 字段的值，返回去掉该字段后
// 保留原始键顺序和原始字节的 JSON 文本（用于重新计算签名做比对）。
func extractSignPreservingOrder(body []byte) (string, string, error) {
	dec := json.NewDecoder(bytes.NewReader(body))
	tok, err := dec.Token()
	if err != nil {
		return "", "", err
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '{' {
		return "", "", fmt.Errorf("expected json object")
	}

	var buf bytes.Buffer
	buf.WriteByte('{')
	first := true
	sign := ""
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return "", "", err
		}
		key, ok := keyTok.(string)
		if !ok {
			return "", "", fmt.Errorf("expected string key")
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return "", "", err
		}
		if key == "sign" {
			var s string
			if err := json.Unmarshal(raw, &s); err != nil {
				return "", "", err
			}
			sign = s
			continue
		}
		if !first {
			buf.WriteByte(',')
		}
		first = false
		keyJSON, err := json.Marshal(key)
		if err != nil {
			return "", "", err
		}
		buf.Write(keyJSON)
		buf.WriteByte(':')
		buf.Write(raw)
	}
	if _, err := dec.Token(); err != nil {
		return "", "", err
	}
	buf.WriteByte('}')
	return buf.String(), sign, nil
}

// signedRequest 发送带签名头的 POST 请求并解析 JSON 响应。
func signedRequest(ctx context.Context, cfg *Config, path string, payload map[string]interface{}) (map[string]interface{}, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("%w: marshal request failed", ErrConfigInvalid)
	}

	ctx, cancel := common.WithDefaultTimeout(ctx)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiBaseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("%w: build request failed", ErrRequestFailed)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("merchant", cfg.MerchantID)
	req.Header.Set("sign", signPayload(body, cfg.PaymentAPIKey))

	resp, err := (&http.Client{Timeout: common.DefaultTimeout}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrRequestFailed, err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("%w: read response failed", ErrRequestFailed)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("%w: status=%d body=%s", ErrRequestFailed, resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var envelope struct {
		State  int                    `json:"state"`
		Result map[string]interface{} `json:"result"`
	}
	if err := json.Unmarshal(respBody, &envelope); err != nil {
		return nil, fmt.Errorf("%w: decode response failed", ErrResponseInvalid)
	}
	if envelope.State != 0 {
		return nil, fmt.Errorf("%w: state=%d body=%s", ErrResponseInvalid, envelope.State, strings.TrimSpace(string(respBody)))
	}
	if envelope.Result == nil {
		return nil, fmt.Errorf("%w: missing result", ErrResponseInvalid)
	}
	return envelope.Result, nil
}
