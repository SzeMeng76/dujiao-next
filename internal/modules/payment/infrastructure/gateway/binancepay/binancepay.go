package binancepay

import (
	"context"
	"crypto"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/dujiao-next/internal/constants"
	gatewaycommon "github.com/dujiao-next/internal/modules/payment/infrastructure/gateway/common"

	"github.com/shopspring/decimal"
)

var (
	ErrConfigInvalid    = errors.New("binancepay config invalid")
	ErrRequestFailed    = errors.New("binancepay request failed")
	ErrResponseInvalid  = errors.New("binancepay response invalid")
	ErrSignatureInvalid = errors.New("binancepay signature invalid")
)

const (
	defaultAPIBaseURL        = "https://bpay.binanceapi.com"
	defaultTimeout           = 12 * time.Second
	defaultWebhookToleranceS = 300
	nonceLength              = 32
)

// Config Binance Pay 渠道配置。
// APIKey 对应后台的 Certificate SN，SecretKey 对应 Secret Key。
type Config struct {
	gatewaycommon.ExchangeRateConfig
	APIKey                  string `json:"api_key"`
	SecretKey               string `json:"secret_key"`
	ReturnURL               string `json:"return_url"`
	CancelURL               string `json:"cancel_url"`
	APIBaseURL              string `json:"api_base_url"`
	WebhookToleranceSeconds int    `json:"webhook_tolerance_seconds"`
	Currency                string `json:"currency"`
}

func (c *Config) Normalize() {
	c.APIKey = strings.TrimSpace(c.APIKey)
	c.SecretKey = strings.TrimSpace(c.SecretKey)
	c.ReturnURL = strings.TrimSpace(c.ReturnURL)
	c.CancelURL = strings.TrimSpace(c.CancelURL)
	c.APIBaseURL = strings.TrimRight(strings.TrimSpace(c.APIBaseURL), "/")
	if c.APIBaseURL == "" {
		c.APIBaseURL = defaultAPIBaseURL
	}
	if c.WebhookToleranceSeconds <= 0 {
		c.WebhookToleranceSeconds = defaultWebhookToleranceS
	}
	c.Currency = strings.ToUpper(strings.TrimSpace(c.Currency))
	if c.Currency == "" {
		c.Currency = "USDT"
	}
	c.ExchangeRateConfig.NormalizeExchangeRate()
}

func ParseConfig(raw map[string]interface{}) (*Config, error) {
	return gatewaycommon.ParseConfig[Config](raw, ErrConfigInvalid)
}

func ValidateConfig(cfg *Config) error {
	if cfg == nil {
		return fmt.Errorf("%w: config is nil", ErrConfigInvalid)
	}
	if cfg.APIKey == "" {
		return fmt.Errorf("%w: api_key is required", ErrConfigInvalid)
	}
	if cfg.SecretKey == "" {
		return fmt.Errorf("%w: secret_key is required", ErrConfigInvalid)
	}
	if cfg.ReturnURL == "" {
		return fmt.Errorf("%w: return_url is required", ErrConfigInvalid)
	}
	return nil
}

// CreateInput 创建 Binance Pay 支付输入。
type CreateInput struct {
	OrderNo      string
	Amount       string
	Currency     string
	Description  string
	ReturnURL    string
	CancelURL    string
	TerminalType string // WEB 或 WAP
}

// CreateResult 创建 Binance Pay 支付返回。
type CreateResult struct {
	PrepayID     string
	UniversalURL string
	Raw          map[string]interface{}
}

// WebhookResult Binance Pay Webhook 解析结果。
type WebhookResult struct {
	BizType   string
	BizStatus string
	BizID     string
	OrderNo   string
	Amount    string
	Currency  string
	PaidAt    *time.Time
	Status    string
	Raw       map[string]interface{}
}

// CreatePayment 创建 Binance Pay 订单，返回跳转 URL。
func CreatePayment(ctx context.Context, cfg *Config, input CreateInput) (*CreateResult, error) {
	if err := ValidateConfig(cfg); err != nil {
		return nil, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	orderNo := strings.TrimSpace(input.OrderNo)
	if orderNo == "" {
		return nil, fmt.Errorf("%w: order_no is required", ErrConfigInvalid)
	}
	amount, err := decimal.NewFromString(strings.TrimSpace(input.Amount))
	if err != nil || amount.LessThanOrEqual(decimal.Zero) {
		return nil, fmt.Errorf("%w: amount is invalid", ErrConfigInvalid)
	}
	currency := strings.ToUpper(strings.TrimSpace(input.Currency))
	if currency == "" {
		currency = cfg.Currency
	}
	terminalType := strings.TrimSpace(input.TerminalType)
	if terminalType == "" {
		terminalType = "WEB"
	}
	returnURL := strings.TrimSpace(input.ReturnURL)
	if returnURL == "" {
		returnURL = cfg.ReturnURL
	}
	cancelURL := strings.TrimSpace(input.CancelURL)
	if cancelURL == "" {
		cancelURL = cfg.CancelURL
	}
	description := strings.TrimSpace(input.Description)
	if description == "" {
		description = orderNo
	}
	if len(description) > 256 {
		description = description[:256]
	}

	params := map[string]interface{}{
		"env":             map[string]interface{}{"terminalType": terminalType},
		"merchantTradeNo": orderNo,
		"orderAmount":     amount.InexactFloat64(),
		"currency":        currency,
		"goods": map[string]interface{}{
			"goodsType":        "01",
			"goodsCategory":    "D000",
			"referenceGoodsId": orderNo,
			"goodsName":        description,
		},
		"returnUrl": returnURL,
		"cancelUrl": cancelURL,
	}

	body, statusCode, err := doSignedRequest(ctx, cfg, "/binancepay/openapi/v2/order", params)
	if err != nil {
		return nil, err
	}
	if statusCode < 200 || statusCode >= 300 {
		return nil, fmt.Errorf("%w: create order status %d", ErrResponseInvalid, statusCode)
	}
	raw, err := decodeRawMap(body)
	if err != nil {
		return nil, err
	}
	if gatewaycommon.ReadString(raw, "status") != "SUCCESS" {
		return nil, fmt.Errorf("%w: %s", ErrResponseInvalid, gatewaycommon.ReadString(raw, "errorMessage"))
	}
	dataRaw, _ := raw["data"].(map[string]interface{})
	if dataRaw == nil {
		return nil, fmt.Errorf("%w: missing data in response", ErrResponseInvalid)
	}
	universalURL := gatewaycommon.ReadString(dataRaw, "universalUrl")
	if universalURL == "" {
		universalURL = gatewaycommon.ReadString(dataRaw, "checkoutUrl")
	}
	if universalURL == "" {
		return nil, fmt.Errorf("%w: missing universalUrl in response", ErrResponseInvalid)
	}
	return &CreateResult{
		PrepayID:     gatewaycommon.ReadString(dataRaw, "prepayId"),
		UniversalURL: universalURL,
		Raw:          raw,
	}, nil
}

// fetchCertPublicKey 从 Binance Pay 服务器拉取 RSA 公钥用于 webhook 验签。
func fetchCertPublicKey(ctx context.Context, cfg *Config) (string, error) {
	body, statusCode, err := doSignedRequest(ctx, cfg, "/binancepay/openapi/certificates", map[string]interface{}{})
	if err != nil {
		return "", err
	}
	if statusCode < 200 || statusCode >= 300 {
		return "", fmt.Errorf("%w: fetch certificates status %d", ErrResponseInvalid, statusCode)
	}
	raw, err := decodeRawMap(body)
	if err != nil {
		return "", err
	}
	if gatewaycommon.ReadString(raw, "status") != "SUCCESS" {
		return "", fmt.Errorf("%w: %s", ErrResponseInvalid, gatewaycommon.ReadString(raw, "errorMessage"))
	}
	dataArr, ok := raw["data"].([]interface{})
	if !ok || len(dataArr) == 0 {
		return "", fmt.Errorf("%w: missing certificates data", ErrResponseInvalid)
	}
	first, ok := dataArr[0].(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("%w: invalid certificate format", ErrResponseInvalid)
	}
	pubKey := gatewaycommon.ReadString(first, "certPublic")
	if pubKey == "" {
		return "", fmt.Errorf("%w: missing certPublic", ErrResponseInvalid)
	}
	return pubKey, nil
}

// VerifyAndParseWebhook 校验并解析 Binance Pay webhook。
// Binance 用 RSA-SHA256 签名，公钥需从其服务器动态拉取。
func VerifyAndParseWebhook(ctx context.Context, cfg *Config, headers map[string]string, body []byte, now time.Time) (*WebhookResult, error) {
	if cfg == nil {
		return nil, fmt.Errorf("%w: config is nil", ErrConfigInvalid)
	}
	if len(body) == 0 {
		return nil, fmt.Errorf("%w: body is empty", ErrResponseInvalid)
	}
	if now.IsZero() {
		now = time.Now()
	}

	timestampStr := getHeaderValue(headers, "BinancePay-Timestamp")
	nonce := getHeaderValue(headers, "BinancePay-Nonce")
	signatureB64 := getHeaderValue(headers, "BinancePay-Signature")
	if timestampStr == "" || nonce == "" || signatureB64 == "" {
		return nil, fmt.Errorf("%w: missing required BinancePay headers", ErrSignatureInvalid)
	}

	if cfg.WebhookToleranceSeconds > 0 {
		var tsMs int64
		fmt.Sscanf(timestampStr, "%d", &tsMs)
		if tsMs > 0 {
			delta := now.Unix() - tsMs/1000
			if delta < 0 {
				delta = -delta
			}
			if delta > int64(cfg.WebhookToleranceSeconds) {
				return nil, fmt.Errorf("%w: timestamp outside tolerance", ErrSignatureInvalid)
			}
		}
	}

	pubKeyPEM, err := fetchCertPublicKey(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("%w: fetch public key: %v", ErrSignatureInvalid, err)
	}
	sigBytes, err := base64.StdEncoding.DecodeString(signatureB64)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid signature encoding", ErrSignatureInvalid)
	}
	payload := timestampStr + "\n" + nonce + "\n" + string(body) + "\n"
	if err := verifyRSASHA256(pubKeyPEM, []byte(payload), sigBytes); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSignatureInvalid, err)
	}

	raw, err := decodeRawMap(body)
	if err != nil {
		return nil, err
	}
	bizType := gatewaycommon.ReadString(raw, "bizType")
	bizStatus := gatewaycommon.ReadString(raw, "bizStatus")
	bizID := gatewaycommon.ReadString(raw, "bizId")

	result := &WebhookResult{
		BizType:   bizType,
		BizStatus: bizStatus,
		BizID:     bizID,
		Raw:       raw,
	}

	// bizType != PAY 是非支付通知，直接返回不报错
	if !strings.EqualFold(bizType, "PAY") {
		return result, nil
	}

	dataStr := gatewaycommon.ReadString(raw, "data")
	var dataInner map[string]interface{}
	if err := json.Unmarshal([]byte(dataStr), &dataInner); err != nil {
		return nil, fmt.Errorf("%w: decode data field failed", ErrResponseInvalid)
	}
	result.OrderNo = gatewaycommon.ReadString(dataInner, "merchantTradeNo")
	result.Currency = gatewaycommon.ReadString(dataInner, "currency")
	result.Amount = gatewaycommon.ReadString(dataInner, "totalFee")

	switch strings.ToUpper(bizStatus) {
	case "PAY_SUCCESS":
		result.Status = constants.PaymentStatusSuccess
		t := time.Now()
		result.PaidAt = &t
	case "PAY_CLOSED":
		result.Status = constants.PaymentStatusFailed
	default:
		result.Status = constants.PaymentStatusPending
	}
	return result, nil
}

func verifyRSASHA256(pubKeyPEM string, payload, sig []byte) error {
	block, _ := pem.Decode([]byte(pubKeyPEM))
	var pubKey *rsa.PublicKey
	if block != nil {
		key, err := x509.ParsePKIXPublicKey(block.Bytes)
		if err != nil {
			return fmt.Errorf("parse public key: %w", err)
		}
		var ok bool
		pubKey, ok = key.(*rsa.PublicKey)
		if !ok {
			return fmt.Errorf("not an RSA public key")
		}
	} else {
		// 尝试裸 base64 DER
		der, err := base64.StdEncoding.DecodeString(strings.TrimSpace(pubKeyPEM))
		if err != nil {
			return fmt.Errorf("decode public key: %w", err)
		}
		key, err := x509.ParsePKIXPublicKey(der)
		if err != nil {
			return fmt.Errorf("parse public key DER: %w", err)
		}
		var ok bool
		pubKey, ok = key.(*rsa.PublicKey)
		if !ok {
			return fmt.Errorf("not an RSA public key")
		}
	}
	hash := sha256.Sum256(payload)
	return rsa.VerifyPKCS1v15(pubKey, crypto.SHA256, hash[:], sig)
}

func doSignedRequest(ctx context.Context, cfg *Config, path string, params interface{}) ([]byte, int, error) {
	data, err := json.Marshal(params)
	if err != nil {
		return nil, 0, fmt.Errorf("%w: marshal params failed", ErrRequestFailed)
	}
	tsMs := time.Now().UnixMilli()
	nonce, err := generateNonce(nonceLength)
	if err != nil {
		return nil, 0, fmt.Errorf("%w: generate nonce failed", ErrRequestFailed)
	}
	payload := fmt.Sprintf("%d\n%s\n%s\n", tsMs, nonce, string(data))
	sig := computeHMACSHA512Upper(cfg.SecretKey, payload)

	endpoint := strings.TrimRight(cfg.APIBaseURL, "/") + path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(data)))
	if err != nil {
		return nil, 0, fmt.Errorf("%w: build request failed", ErrRequestFailed)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("BinancePay-Timestamp", fmt.Sprintf("%d", tsMs))
	req.Header.Set("BinancePay-Nonce", nonce)
	req.Header.Set("BinancePay-Certificate-SN", cfg.APIKey)
	req.Header.Set("BinancePay-Signature", sig)

	resp, err := (&http.Client{Timeout: defaultTimeout}).Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("%w: %v", ErrRequestFailed, err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("%w: read response failed", ErrResponseInvalid)
	}
	return respBody, resp.StatusCode, nil
}

// computeHMACSHA512Upper 生成 HMAC-SHA512 大写十六进制（Binance Pay 签名格式）。
func computeHMACSHA512Upper(secret, payload string) string {
	h := hmac.New(sha512.New, []byte(secret))
	h.Write([]byte(payload))
	return strings.ToUpper(fmt.Sprintf("%x", h.Sum(nil)))
}

func generateNonce(length int) (string, error) {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	result := make([]byte, length)
	for i := range result {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		if err != nil {
			return "", err
		}
		result[i] = charset[n.Int64()]
	}
	return string(result), nil
}

func decodeRawMap(body []byte) (map[string]interface{}, error) {
	var raw map[string]interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("%w: decode response failed", ErrResponseInvalid)
	}
	return raw, nil
}

func getHeaderValue(headers map[string]string, key string) string {
	for h, v := range headers {
		if strings.EqualFold(strings.TrimSpace(h), key) {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
