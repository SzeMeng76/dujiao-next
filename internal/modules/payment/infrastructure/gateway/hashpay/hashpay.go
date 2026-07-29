package hashpay

import (
	"bytes"
	"context"
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/dujiao-next/internal/constants"
	"github.com/dujiao-next/internal/modules/payment/infrastructure/gateway/common"
)

var (
	ErrConfigInvalid    = errors.New("hashpay config invalid")
	ErrRequestFailed    = errors.New("hashpay request failed")
	ErrResponseInvalid  = errors.New("hashpay response invalid")
	ErrSignatureInvalid = errors.New("hashpay signature invalid")
)

const (
	createOrderPath = "/api/merchant/new"
	queryOrderPath  = "/api/order/"

	// callbackEnvelopeAlg 是 HashPay 回调加密信封的固定算法标识。
	callbackEnvelopeAlg = "RSA-OAEP-256+A256GCM"

	// callbackTimeTolerance 校验解密后明文里 timestamp 的允许偏差。
	// HashPay 每次重试投递都会用当前时间重新加密信封，所以固定窗口不会误伤重试。
	callbackTimeTolerance = 5 * time.Minute

	// HashPay 订单状态（src/shared/types/domain.ts OrderStatus）
	StatusPending = "pending"
	StatusPaid    = "paid"
	StatusExpired = "expired"
	StatusInvalid = "invalid"
)

// Config HashPay 配置。商户密钥对由 HashPay 后台创建商户时生成，
// 私钥只显示一次，同时用于请求签名和回调信封解密。
type Config struct {
	GatewayURL         string `json:"gateway_url"`          // HashPay 服务地址，如 https://pay.example.com
	MerchantID         string `json:"merchant_id"`          // 商户 ID
	MerchantPrivateKey string `json:"merchant_private_key"` // 商户 RSA 私钥（PEM，PKCS8/PKCS1）
	Currency           string `json:"currency,omitempty"`   // 下单法币币种，留空使用站点订单币种
	ReturnURL          string `json:"return_url"`           // 支付完成同步跳转地址
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
	c.GatewayURL = strings.TrimRight(strings.TrimSpace(c.GatewayURL), "/")
	c.MerchantID = strings.TrimSpace(c.MerchantID)
	c.MerchantPrivateKey = strings.TrimSpace(c.MerchantPrivateKey)
	c.Currency = strings.ToUpper(strings.TrimSpace(c.Currency))
	c.ReturnURL = strings.TrimSpace(c.ReturnURL)
}

// ValidateConfig 校验必填字段，并确认私钥可解析。
func ValidateConfig(cfg *Config) error {
	if cfg == nil {
		return fmt.Errorf("%w: config is nil", ErrConfigInvalid)
	}
	checks := []struct {
		field string
		val   string
	}{
		{"gateway_url", cfg.GatewayURL},
		{"merchant_id", cfg.MerchantID},
		{"merchant_private_key", cfg.MerchantPrivateKey},
	}
	for _, c := range checks {
		if strings.TrimSpace(c.val) == "" {
			return fmt.Errorf("%w: %s is required", ErrConfigInvalid, c.field)
		}
	}
	if _, err := url.ParseRequestURI(cfg.GatewayURL); err != nil {
		return fmt.Errorf("%w: gateway_url is invalid", ErrConfigInvalid)
	}
	if _, err := parsePrivateKey(cfg.MerchantPrivateKey); err != nil {
		return err
	}
	return nil
}

// CreateInput 创建订单输入。
type CreateInput struct {
	OrderNo     string
	Amount      string
	Currency    string
	Description string
	ReturnURL   string
}

// CreateResult 创建订单结果。
type CreateResult struct {
	OrderID     string
	CheckoutURL string
	Amount      string
	Currency    string
	ExpiresAt   int64
	Status      string
	Reused      bool
	Raw         map[string]interface{}
}

// OrderResult 查询订单结果。payment 快照仅在用户选定支付渠道后才有内容。
type OrderResult struct {
	OrderID    string
	MerchantNo string
	Amount     string
	Currency   string
	Status     string
	PaidAt     *time.Time
	Raw        map[string]interface{}
}

// CallbackPayload 是回调信封解密后的业务字段。
type CallbackPayload struct {
	OrderID    string
	MerchantNo string
	Amount     string
	Currency   string
	Status     string
	Payment    map[string]interface{}
	Raw        map[string]interface{}
}

type createRequest struct {
	MerchantNo  string      `json:"merchantNo"`
	Amount      json.Number `json:"amount"`
	Currency    string      `json:"currency,omitempty"`
	Description string      `json:"description,omitempty"`
	ReturnURL   string      `json:"return_url,omitempty"`
}

// CreatePayment 调 POST /api/merchant/new 创建（或复用）订单。
// HashPay 下单不指定链/币种，返回 checkoutUrl 由付款人在收银台自选，
// 因此只支持 redirect 交互模式。
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
	returnURL := strings.TrimSpace(input.ReturnURL)
	if returnURL == "" {
		returnURL = cfg.ReturnURL
	}

	body, err := json.Marshal(createRequest{
		MerchantNo:  orderNo,
		Amount:      json.Number(amount),
		Currency:    currency,
		Description: strings.TrimSpace(input.Description),
		ReturnURL:   returnURL,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: marshal create request failed", ErrConfigInvalid)
	}

	raw, err := signedRequest(ctx, cfg, http.MethodPost, cfg.GatewayURL+createOrderPath, body)
	if err != nil {
		return nil, err
	}

	order := common.ReadMap(raw, "order")
	result := &CreateResult{
		OrderID:     common.ReadString(order, "id"),
		CheckoutURL: common.ReadString(raw, "checkoutUrl"),
		Amount:      common.ReadString(order, "amount"),
		Currency:    common.ReadString(order, "currency"),
		Status:      common.ReadString(order, "status"),
		Reused:      raw["reused"] == true,
		Raw:         raw,
	}
	if v, err := strconv.ParseInt(common.ReadString(order, "expiresAt"), 10, 64); err == nil {
		result.ExpiresAt = v
	}
	if result.OrderID == "" || result.CheckoutURL == "" {
		return nil, fmt.Errorf("%w: missing order.id/checkoutUrl", ErrResponseInvalid)
	}
	return result, nil
}

// QueryPayment 调 GET /api/order/:orderId 查询订单。GET 签名时 body 为空字符串。
func QueryPayment(ctx context.Context, cfg *Config, orderID string) (*OrderResult, error) {
	if err := ValidateConfig(cfg); err != nil {
		return nil, err
	}
	orderID = strings.TrimSpace(orderID)
	if orderID == "" {
		return nil, fmt.Errorf("%w: order_id is required", ErrConfigInvalid)
	}

	raw, err := signedRequest(ctx, cfg, http.MethodGet, cfg.GatewayURL+queryOrderPath+url.PathEscape(orderID), nil)
	if err != nil {
		return nil, err
	}

	result := &OrderResult{
		OrderID:    common.ReadString(raw, "id"),
		MerchantNo: common.ReadString(raw, "merchantNo"),
		Amount:     common.ReadString(raw, "amount"),
		Currency:   common.ReadString(raw, "currency"),
		Status:     common.ReadString(raw, "status"),
		Raw:        raw,
	}
	if result.OrderID == "" || result.Status == "" {
		return nil, fmt.Errorf("%w: missing id/status", ErrResponseInvalid)
	}
	if paidAt, err := strconv.ParseInt(common.ReadString(raw, "paidAt"), 10, 64); err == nil && paidAt > 0 {
		t := time.Unix(paidAt, 0)
		result.PaidAt = &t
	}
	return result, nil
}

// ToPaymentStatus 把 HashPay 订单状态映射为内部 payment 状态。
// 未知状态返回空串，由上层按“不可识别事件”忽略。
func ToPaymentStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case StatusPaid:
		return constants.PaymentStatusSuccess
	case StatusExpired:
		return constants.PaymentStatusExpired
	case StatusInvalid:
		return constants.PaymentStatusFailed
	case StatusPending:
		return constants.PaymentStatusPending
	default:
		return ""
	}
}

type callbackEnvelope struct {
	Alg  string `json:"alg"`
	Key  string `json:"key"`
	IV   string `json:"iv"`
	Data string `json:"data"`
}

type callbackPlaintext struct {
	Timestamp int64           `json:"timestamp"`
	Payload   json.RawMessage `json:"payload"`
}

// DecryptCallback 解密并严格校验 HashPay 回调信封。
//
// 信封用商户公钥做 RSA-OAEP(SHA-256) + AES-256-GCM 加密，没有独立的发送者签名；
// AES-GCM 认证标签保证密文未被篡改，能用错误私钥解出信封的概率可忽略，
// 因此“解密成功”即视为信封归属确认（也是 webhook 盲匹配候选渠道的依据）。
// 业务事实（金额/币种/订单号/渠道）仍由上层 HandleCallback 强制比对，缺失即拒绝。
func DecryptCallback(cfg *Config, headers map[string]string, body []byte, now time.Time) (*CallbackPayload, error) {
	if cfg == nil {
		return nil, fmt.Errorf("%w: config is nil", ErrConfigInvalid)
	}
	privateKey, err := parsePrivateKey(cfg.MerchantPrivateKey)
	if err != nil {
		return nil, err
	}
	// 商户头是未认证的明文，仅作候选渠道的廉价预过滤，不作为信任依据。
	if merchant := headerValue(headers, "X-Hashpay-Merchant"); merchant != "" && merchant != cfg.MerchantID {
		return nil, fmt.Errorf("%w: merchant mismatch", ErrSignatureInvalid)
	}

	var envelope callbackEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("%w: decode envelope failed", ErrResponseInvalid)
	}
	if envelope.Alg != callbackEnvelopeAlg {
		return nil, fmt.Errorf("%w: unsupported envelope alg %q", ErrResponseInvalid, envelope.Alg)
	}
	encryptedKey, err := base64.StdEncoding.DecodeString(strings.TrimSpace(envelope.Key))
	if err != nil {
		return nil, fmt.Errorf("%w: decode key failed", ErrResponseInvalid)
	}
	iv, err := base64.StdEncoding.DecodeString(strings.TrimSpace(envelope.IV))
	if err != nil {
		return nil, fmt.Errorf("%w: decode iv failed", ErrResponseInvalid)
	}
	data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(envelope.Data))
	if err != nil {
		return nil, fmt.Errorf("%w: decode data failed", ErrResponseInvalid)
	}

	contentKey, err := rsa.DecryptOAEP(sha256.New(), nil, privateKey, encryptedKey, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: decrypt content key failed", ErrSignatureInvalid)
	}
	block, err := aes.NewCipher(contentKey)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid content key", ErrSignatureInvalid)
	}
	gcm, err := cipher.NewGCMWithNonceSize(block, len(iv))
	if err != nil {
		return nil, fmt.Errorf("%w: invalid iv", ErrSignatureInvalid)
	}
	plain, err := gcm.Open(nil, iv, data, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: decrypt data failed", ErrSignatureInvalid)
	}

	var plaintext callbackPlaintext
	if err := json.Unmarshal(plain, &plaintext); err != nil {
		return nil, fmt.Errorf("%w: decode plaintext failed", ErrResponseInvalid)
	}
	// 时间戳取加密明文里的值（经过 GCM 认证），不是未认证的请求头。
	if now.IsZero() {
		now = time.Now()
	}
	eventTime := time.Unix(plaintext.Timestamp, 0)
	if now.Sub(eventTime) > callbackTimeTolerance || eventTime.Sub(now) > callbackTimeTolerance {
		return nil, fmt.Errorf("%w: callback timestamp outside tolerance", ErrSignatureInvalid)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(plaintext.Payload, &raw); err != nil {
		return nil, fmt.Errorf("%w: decode payload failed", ErrResponseInvalid)
	}
	payload := &CallbackPayload{
		OrderID:    common.ReadString(raw, "orderId"),
		MerchantNo: common.ReadString(raw, "merchantNo"),
		Amount:     common.ReadString(raw, "amount"),
		Currency:   strings.ToUpper(common.ReadString(raw, "currency")),
		Status:     common.ReadString(raw, "status"),
		Payment:    common.ReadMap(raw, "payment"),
		Raw:        raw,
	}
	// 强制校验：关键业务字段缺一不可，缺失不默认可信。
	if payload.OrderID == "" || payload.MerchantNo == "" || payload.Status == "" {
		return nil, fmt.Errorf("%w: missing orderId/merchantNo/status", ErrResponseInvalid)
	}
	if payload.Amount == "" || payload.Currency == "" {
		return nil, fmt.Errorf("%w: missing amount/currency", ErrResponseInvalid)
	}
	if amount, err := strconv.ParseFloat(payload.Amount, 64); err != nil || amount <= 0 {
		return nil, fmt.Errorf("%w: invalid amount %q", ErrResponseInvalid, payload.Amount)
	}
	return payload, nil
}

// signedRequest 发送带 RSA 签名头的请求并解析 JSON 响应。
// 签名原文：METHOD\npath+query\ntimestamp\nbody，用商户私钥做 RSASSA-PKCS1-v1_5 SHA-256。
func signedRequest(ctx context.Context, cfg *Config, method, endpoint string, body []byte) (map[string]interface{}, error) {
	privateKey, err := parsePrivateKey(cfg.MerchantPrivateKey)
	if err != nil {
		return nil, err
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid endpoint", ErrConfigInvalid)
	}
	pathWithQuery := parsed.EscapedPath()
	if parsed.RawQuery != "" {
		pathWithQuery += "?" + parsed.RawQuery
	}

	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	payload := strings.Join([]string{strings.ToUpper(method), pathWithQuery, timestamp, string(body)}, "\n")
	digest := sha256.Sum256([]byte(payload))
	signature, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, digest[:])
	if err != nil {
		return nil, fmt.Errorf("%w: sign request failed", ErrConfigInvalid)
	}

	ctx, cancel := common.WithDefaultTimeout(ctx)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("%w: build request failed", ErrRequestFailed)
	}
	req.Header.Set("Accept", "application/json")
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("X-Merchant-Id", cfg.MerchantID)
	req.Header.Set("X-Timestamp", timestamp)
	req.Header.Set("X-Signature", base64.StdEncoding.EncodeToString(signature))

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
	var raw map[string]interface{}
	if err := json.Unmarshal(respBody, &raw); err != nil {
		return nil, fmt.Errorf("%w: decode response failed", ErrResponseInvalid)
	}
	return raw, nil
}

// parsePrivateKey 解析 PKCS8/PKCS1 PEM 私钥（HashPay 后台生成的是 PKCS8）。
func parsePrivateKey(raw string) (*rsa.PrivateKey, error) {
	normalized := strings.TrimSpace(strings.ReplaceAll(raw, "\\n", "\n"))
	if normalized == "" {
		return nil, fmt.Errorf("%w: merchant_private_key is empty", ErrConfigInvalid)
	}
	if !strings.Contains(normalized, "BEGIN") {
		normalized = "-----BEGIN PRIVATE KEY-----\n" + normalized + "\n-----END PRIVATE KEY-----"
	}
	block, _ := pem.Decode([]byte(normalized))
	if block == nil {
		return nil, fmt.Errorf("%w: private key pem decode failed", ErrConfigInvalid)
	}
	if parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		if privateKey, ok := parsed.(*rsa.PrivateKey); ok {
			return privateKey, nil
		}
		return nil, fmt.Errorf("%w: private key type is not rsa", ErrConfigInvalid)
	}
	if privateKey, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return privateKey, nil
	}
	return nil, fmt.Errorf("%w: parse private key failed", ErrConfigInvalid)
}

func headerValue(headers map[string]string, name string) string {
	if len(headers) == 0 {
		return ""
	}
	if value := strings.TrimSpace(headers[name]); value != "" {
		return value
	}
	for key, value := range headers {
		if strings.EqualFold(key, name) {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
