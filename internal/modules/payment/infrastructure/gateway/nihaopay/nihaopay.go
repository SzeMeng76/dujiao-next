package nihaopay

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

var (
	ErrConfigInvalid    = errors.New("nihaopay config invalid")
	ErrRequestFailed    = errors.New("nihaopay request failed")
	ErrResponseInvalid  = errors.New("nihaopay response invalid")
	ErrSignatureInvalid = errors.New("nihaopay signature invalid")
)

const (
	defaultAPIBaseURL = "https://api.nihaopay.com"
	defaultTimeout    = 15 * time.Second
)

// Config Nihaopay 配置
type Config struct {
	Token              string `json:"token"`
	APIBaseURL         string `json:"api_base_url"`
	ReturnURL          string `json:"return_url"`          // callback_url 默认值
	NotifyURL          string `json:"notify_url"`          // ipn_url 默认值（可选）
	SettlementCurrency string `json:"settlement_currency"` // 结算货币，CNY订单时使用
}

// CreateInput 创建支付输入
type CreateInput struct {
	OrderNo     string
	Amount      string
	Currency    string
	Subject     string
	ChannelType string
	CallbackURL string // 支付完成后浏览器重定向 URL (GET)
	IPNUrl      string // 异步通知 URL (POST)
	Reference   string
}

// CreateResult 创建支付结果
type CreateResult struct {
	TransactionID string
	FormAction    string
	FormMethod    string
	FormParams    map[string]string
	Raw           map[string]interface{}
}

// SecurePayFormResponse Nihaopay securepay 返回的表单对象（response_format=JSON 时）
type SecurePayFormResponse struct {
	Form struct {
		ActionURL string                 `json:"actionUrl"`
		Method    string                 `json:"method"`
		Inputs    map[string]interface{} `json:"inputs"`
	} `json:"form"`
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
	if strings.TrimSpace(cfg.Token) == "" {
		return fmt.Errorf("%w: token is required", ErrConfigInvalid)
	}
	return nil
}

// IsSupportedChannelType 检查是否支持该渠道类型
func IsSupportedChannelType(channelType string) bool {
	switch strings.ToLower(channelType) {
	case "alipay", "wechatpay", "unionpay":
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

	channelType := strings.ToLower(input.ChannelType)
	if !IsSupportedChannelType(channelType) {
		return nil, fmt.Errorf("%w: unsupported channel_type: %s", ErrConfigInvalid, channelType)
	}

	apiURL := fmt.Sprintf("%s/v1.2/transactions/securepay", baseURL)

	params := url.Values{}

	// Nihaopay 逻辑：
	// - 如果订单货币是 CNY：使用 rmb_amount，currency 是结算货币
	// - 如果订单货币不是 CNY：使用 amount，currency 是订单货币
	currency := strings.ToUpper(input.Currency)
	if currency == "CNY" {
		params.Set("rmb_amount", convertAmount(input.Amount))
		// 使用结算货币，默认 USD
		settlementCurrency := strings.ToUpper(strings.TrimSpace(cfg.SettlementCurrency))
		if settlementCurrency == "" {
			settlementCurrency = "USD"
		}
		params.Set("currency", settlementCurrency)
	} else {
		params.Set("amount", convertAmount(input.Amount))
		params.Set("currency", currency)
	}
	params.Set("vendor", channelType)
	params.Set("reference", input.Reference)
	params.Set("callback_url", input.CallbackURL) // 必填：支付完成后浏览器重定向 (GET)
	if input.IPNUrl != "" {
		params.Set("ipn_url", input.IPNUrl) // 可选：异步通知 (POST)
	}
	params.Set("description", input.Subject)
	params.Set("note", input.OrderNo)
	params.Set("response_format", "JSON")

	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, strings.NewReader(params.Encode()))
	if err != nil {
		return nil, fmt.Errorf("%w: failed to create request: %v", ErrRequestFailed, err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", cfg.Token))

	client := &http.Client{Timeout: defaultTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: http request failed: %v", ErrRequestFailed, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read response: %v", ErrRequestFailed, err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: status %d, body: %s", ErrResponseInvalid, resp.StatusCode, string(body))
	}

	var formResp SecurePayFormResponse
	if err := json.Unmarshal(body, &formResp); err != nil {
		return nil, fmt.Errorf("%w: failed to parse json: %v", ErrResponseInvalid, err)
	}

	if formResp.Form.ActionURL == "" {
		return nil, fmt.Errorf("%w: missing actionUrl in response", ErrResponseInvalid)
	}

	formParams := make(map[string]string)
	for k, v := range formResp.Form.Inputs {
		formParams[k] = fmt.Sprintf("%v", v)
	}

	result := &CreateResult{
		FormAction: formResp.Form.ActionURL,
		FormMethod: strings.ToUpper(formResp.Form.Method),
		FormParams: formParams,
		Raw:        map[string]interface{}{"form": formResp.Form},
	}

	if txnID, ok := formParams["txnId"]; ok {
		result.TransactionID = txnID
	}

	return result, nil
}

// VerifyCallback 验证 IPN 回调签名
func VerifyCallback(cfg *Config, data map[string]string) error {
	receivedSign, ok := data["verify_sign"]
	if !ok || receivedSign == "" {
		return fmt.Errorf("%w: missing verify_sign", ErrSignatureInvalid)
	}

	expectedSign := generateSign(data, cfg.Token)
	if receivedSign != expectedSign {
		return fmt.Errorf("%w: sign mismatch", ErrSignatureInvalid)
	}

	return nil
}

// generateSign 生成签名
func generateSign(data map[string]string, token string) string {
	keys := make([]string, 0, len(data))
	for k := range data {
		if k != "verify_sign" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)

	var buf bytes.Buffer
	for _, k := range keys {
		if buf.Len() > 0 {
			buf.WriteString("&")
		}
		buf.WriteString(k)
		buf.WriteString("=")
		buf.WriteString(data[k])
	}
	buf.WriteString(token)

	hash := md5.Sum([]byte(buf.String()))
	return hex.EncodeToString(hash[:])
}

// convertAmount 将金额转换为 Nihaopay 要求的格式（整数分）
// 例如："100.00" -> "10000"
func convertAmount(amount string) string {
	amount = strings.TrimSpace(amount)
	if amount == "" {
		return "0"
	}

	// 解析小数金额
	d, err := decimal.NewFromString(amount)
	if err != nil {
		return "0"
	}

	// 转换为分（乘以 100）
	cents := d.Mul(decimal.NewFromInt(100))
	return cents.StringFixed(0)
}
