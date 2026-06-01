package globepay

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strings"
	"time"
)

var (
	ErrConfigInvalid    = errors.New("globepay config invalid")
	ErrRequestFailed    = errors.New("globepay request failed")
	ErrResponseInvalid  = errors.New("globepay response invalid")
	ErrSignatureInvalid = errors.New("globepay signature invalid")
)

const (
	defaultAPIBaseURL = "https://pay.globepay.co"
	defaultTimeout    = 15 * time.Second
)

// Config Globepay 配置
type Config struct {
	PartnerCode    string `json:"partner_code"`
	CredentialCode string `json:"credential_code"`
	NotifyURL      string `json:"notify_url"`
	ReturnURL      string `json:"return_url"`
	APIBaseURL     string `json:"api_base_url"`
}

// CreateInput 创建支付输入
type CreateInput struct {
	OrderNo         string
	Amount          string
	Subject         string
	ChannelType     string
	InteractionMode string
	NotifyURL       string
	ReturnURL       string
}

// CreateResult 创建支付结果
type CreateResult struct {
	TradeNo string
	PayURL  string
	QRCode  string
	Raw     map[string]interface{}
}

// APIResponse Globepay API 响应
type APIResponse struct {
	ReturnCode string `json:"return_code"`
	ReturnMsg  string `json:"return_msg"`
	OrderID    string `json:"order_id"`
	CodeURL    string `json:"code_url"`
	PayURL     string `json:"pay_url"`
}

// ParseConfig 解析配置
func ParseConfig(raw map[string]interface{}) (*Config, error) {
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: marshal failed: %v", ErrConfigInvalid, err)
	}
	cfg := &Config{}
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("%w: unmarshal failed: %v", ErrConfigInvalid, err)
	}
	return cfg, nil
}

// ValidateConfig 验证配置
func ValidateConfig(cfg *Config) error {
	if strings.TrimSpace(cfg.PartnerCode) == "" {
		return fmt.Errorf("%w: partner_code is required", ErrConfigInvalid)
	}
	if strings.TrimSpace(cfg.CredentialCode) == "" {
		return fmt.Errorf("%w: credential_code is required", ErrConfigInvalid)
	}
	return nil
}

// IsSupportedChannelType 检查是否支持该渠道类型
func IsSupportedChannelType(channelType string) bool {
	switch strings.ToLower(channelType) {
	case "wechat", "alipay", "alipayhk", "tng", "dana", "gcash":
		return true
	}
	return false
}

// CreatePayment 创建支付订单
func CreatePayment(ctx context.Context, cfg *Config, input CreateInput) (*CreateResult, error) {
	baseURL := strings.TrimSpace(cfg.APIBaseURL)
	if baseURL == "" {
		baseURL = defaultAPIBaseURL
	}

	timestamp := getTimestamp()
	nonceStr := randString(30)
	sign := generateSign(cfg.PartnerCode, timestamp, nonceStr, cfg.CredentialCode)
	query := fmt.Sprintf("?time=%d&nonce_str=%s&sign=%s", timestamp, nonceStr, sign)

	channelType := strings.ToLower(input.ChannelType)
	params := map[string]interface{}{
		"description": input.Subject,
		"price":       parseAmountToCents(input.Amount),
		"currency":    "CNY",
		"notify_url":  input.NotifyURL,
	}

	var apiURL string
	switch channelType {
	case "wechat":
		apiURL = fmt.Sprintf("%s/api/v1.0/gateway/partners/%s/orders/%s%s",
			baseURL, cfg.PartnerCode, input.OrderNo, query)
		params["channel"] = "Wechat"
	case "alipay":
		apiURL = fmt.Sprintf("%s/api/v1.0/h5_payment/partners/%s/orders/%s%s",
			baseURL, cfg.PartnerCode, input.OrderNo, query)
		params["channel"] = "Alipay"
	case "alipayhk", "tng", "dana", "gcash":
		apiURL = fmt.Sprintf("%s/api/v1.0/h5_payment/partners/%s/orders/%s%s",
			baseURL, cfg.PartnerCode, input.OrderNo, query)
		gbpAmount, err := exchangeCNYtoGBP(ctx, input.Amount)
		if err != nil {
			return nil, fmt.Errorf("%w: currency exchange failed: %v", ErrRequestFailed, err)
		}
		params["price"] = parseAmountToCents(gbpAmount)
		params["currency"] = "GBP"
		params["channel"] = "AlipayPlus"
		params["extra"] = map[string]string{"payType": strings.ToUpper(channelType)}
	default:
		return nil, fmt.Errorf("%w: unsupported channel_type: %s", ErrConfigInvalid, channelType)
	}

	resp, err := sendRequest(ctx, apiURL, params)
	if err != nil {
		return nil, err
	}
	if resp.ReturnCode != "SUCCESS" {
		return nil, fmt.Errorf("%w: %s", ErrResponseInvalid, resp.ReturnMsg)
	}

	result := &CreateResult{
		TradeNo: resp.OrderID,
		Raw:     map[string]interface{}{"return_code": resp.ReturnCode, "order_id": resp.OrderID},
	}
	if channelType == "wechat" {
		result.QRCode = resp.CodeURL
		result.PayURL = resp.CodeURL
	} else {
		newNonceStr := randString(30)
		newSign := generateSign(cfg.PartnerCode, timestamp, newNonceStr, cfg.CredentialCode)
		result.PayURL = fmt.Sprintf("%s?time=%d&nonce_str=%s&sign=%s",
			resp.PayURL, timestamp, newNonceStr, newSign)
	}
	return result, nil
}

// VerifyCallback 验证回调签名
func VerifyCallback(cfg *Config, data map[string]string) error {
	receivedSign, ok := data["sign"]
	if !ok || receivedSign == "" {
		return fmt.Errorf("%w: missing sign", ErrSignatureInvalid)
	}
	var timestamp int64
	fmt.Sscanf(data["time"], "%d", &timestamp)
	nonceStr := data["nonce_str"]
	expectedSign := generateSign(cfg.PartnerCode, timestamp, nonceStr, cfg.CredentialCode)
	if receivedSign != expectedSign {
		return fmt.Errorf("%w: sign mismatch", ErrSignatureInvalid)
	}
	return nil
}

func generateSign(partnerCode string, timestamp int64, nonceStr, credentialCode string) string {
	raw := fmt.Sprintf("%s&%d&%s&%s", partnerCode, timestamp, nonceStr, credentialCode)
	hash := sha256.Sum256([]byte(raw))
	return strings.ToLower(hex.EncodeToString(hash[:]))
}

func getTimestamp() int64 {
	return time.Now().UnixMilli()
}

func randString(length int) string {
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	result := make([]byte, length)
	for i := range result {
		result[i] = chars[rand.Intn(len(chars))]
	}
	return string(result)
}

func parseAmountToCents(amountStr string) int {
	var amount float64
	fmt.Sscanf(strings.TrimSpace(amountStr), "%f", &amount)
	return int(amount * 100)
}

func sendRequest(ctx context.Context, url string, params map[string]interface{}) (*APIResponse, error) {
	body, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("%w: marshal params failed: %v", ErrRequestFailed, err)
	}
	req, err := http.NewRequestWithContext(ctx, "PUT", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("%w: create request failed: %v", ErrRequestFailed, err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: defaultTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: http request failed: %v", ErrRequestFailed, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("%w: read response failed: %v", ErrRequestFailed, err)
	}
	var apiResp APIResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("%w: unmarshal response failed: %v", ErrResponseInvalid, err)
	}
	return &apiResp, nil
}

func exchangeCNYtoGBP(ctx context.Context, amountCNY string) (string, error) {
	url := fmt.Sprintf("https://api.frankfurter.dev/v1/latest?amount=%s&from=CNY&to=GBP",
		strings.TrimSpace(amountCNY))
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", err
	}
	client := &http.Client{Timeout: defaultTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	var result struct {
		Rates map[string]float64 `json:"rates"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", err
	}
	gbp, ok := result.Rates["GBP"]
	if !ok {
		return "", fmt.Errorf("GBP rate not found in response")
	}
	return fmt.Sprintf("%.2f", gbp), nil
}
