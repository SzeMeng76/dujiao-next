// Package acg 实现 acg-faka（异次元）共享店铺协议的「入站」供货接口。
//
// 任何 acg 系站点在后台「共享店铺 → 对接异次元」填本站地址 + (app_id, app_key)
// 即可直接进货，不需要改对方一行代码。
//
// 与 /api/v1/upstream/*（dujiao-next 协议）完全隔离：不同路由前缀、不同验签、
// 不同 handler。新增本包不改变现有 dujiao-next 对接的任何行为。
package acg

import (
	"crypto/md5"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/dujiao-next/internal/constants"
	"github.com/dujiao-next/internal/logger"
	apicredentialdomain "github.com/dujiao-next/internal/modules/apicredential/domain"
	fulfillmentdomain "github.com/dujiao-next/internal/modules/fulfillment/domain"
	upstreamhttp "github.com/dujiao-next/internal/modules/upstreamapi/transport/http"

	"github.com/gin-gonic/gin"
)

// orderPrefix acg 幂等键命名空间：与 dujiao-next 的 downstream_order_no 隔离，
// 同一账户同时用两种协议时不会互相命中。
const orderPrefix = "acg:"

// 客户端可识别的错误文案。
// 最新版下游按字符串分类采购结果，改这些文案会导致误判：
//   - msgRetryable：明确未受理，下游可用原 request_no 安全重试
//   - msgThrottled：限流，下游暂停整批读取
//   - 含「最少购买」「最多购买」：下游判为商品不可购
//
// 其余文案一律被下游归类为「结果不明」并转人工核对，因此不能随手改。
const (
	msgSuccess     = "success"
	msgRetryable   = "订单未完成，请检查库存、规格或余额后再试"
	msgThrottled   = "服务器频繁请联系客服支持"
	msgNoBalance   = "余额不足"
	msgNoStock     = "库存不足"
	msgUnavailable = "商品不存在或已下架"
	msgMinPurchase = "低于最少购买数量"
	msgMaxPurchase = "超过最多购买数量"
	msgUnknownKey  = "商户ID不存在"
	msgDisabled    = "商户被禁用"
	msgBadSecret   = "密钥错误"
	msgCurrency    = "此协议仅支持人民币计价"
)

const lastUsedThrottle = time.Minute

// CredentialStore 验签所需的凭证能力。复用 api_credentials 表：
// app_id 即 api_key、app_key 即 api_secret，不新增字段。
type CredentialStore interface {
	GetByApiKey(apiKey string) (*apicredentialdomain.ApiCredential, error)
	TouchLastUsedAt(id uint, at time.Time) error
}

// FulfillmentService 同步交付能力。acg 协议要求下单即返回卡密，
// 而本站自动交付默认异步入队，因此这里显式同步调用一次 CreateAuto；
// 已交付时返回「已存在」错误，调用方重查订单即可，不会重复发卡。
type FulfillmentService interface {
	CreateAuto(orderID uint) (*fulfillmentdomain.Fulfillment, error)
}

// Handler acg-faka 入站供货接口。
type Handler struct {
	base        *upstreamhttp.Handler
	Credentials CredentialStore
	// Fulfillment 可为 nil：为 nil 时下单后不等待交付，由下游走 query 轮询。
	Fulfillment FulfillmentService
}

// New 构造处理器。base 提供订单、钱包、商品、设置等既有服务。
func New(base *upstreamhttp.Handler, credentials CredentialStore, fulfillment FulfillmentService) *Handler {
	if base == nil || credentials == nil {
		panic("acg supply handler: required dependency is nil")
	}
	return &Handler{base: base, Credentials: credentials, Fulfillment: fulfillment}
}

// authenticate 解析表单 → app_id 查账户 → 状态校验 → 验签 → 币种校验。
func (h *Handler) authenticate(c *gin.Context) (*apicredentialdomain.ApiCredential, map[string]string, bool) {
	if c.Request.Method != http.MethodPost {
		respondError(c, "仅支持 POST")
		return nil, nil, false
	}
	if err := c.Request.ParseForm(); err != nil {
		respondError(c, "表单解析失败")
		return nil, nil, false
	}
	form := make(map[string]string, len(c.Request.PostForm))
	for key, values := range c.Request.PostForm {
		if len(values) > 0 {
			form[key] = values[0]
		}
	}

	appID := strings.TrimSpace(form["app_id"])
	sign := strings.TrimSpace(form["sign"])
	if appID == "" || sign == "" {
		respondError(c, msgUnknownKey)
		return nil, nil, false
	}

	credential, err := h.Credentials.GetByApiKey(appID)
	if err != nil || credential == nil {
		respondError(c, msgUnknownKey)
		return nil, nil, false
	}
	if credential.Status != constants.ApiCredentialStatusApproved || !credential.IsActive {
		respondError(c, msgDisabled)
		return nil, nil, false
	}
	user, err := h.base.Users.GetByID(credential.UserID)
	if err != nil || user == nil || user.Status != constants.UserStatusActive {
		respondError(c, msgDisabled)
		return nil, nil, false
	}
	if !signValid(form, credential.ApiSecret, sign) {
		respondError(c, msgBadSecret)
		return nil, nil, false
	}

	now := time.Now()
	if credential.LastUsedAt == nil || now.Sub(*credential.LastUsedAt) > lastUsedThrottle {
		credentialID := credential.ID
		go func() {
			if err := h.Credentials.TouchLastUsedAt(credentialID, now); err != nil {
				logger.Warnw("acg_supply_touch_last_used_failed", "credential_id", credentialID, "error", err)
			}
		}()
	}

	if currency, err := h.base.Settings.GetSiteCurrency("CNY"); err != nil || !strings.EqualFold(currency, "CNY") {
		respondError(c, msgCurrency)
		return nil, nil, false
	}

	return credential, form, true
}

// signValid acg-faka body 签名：
// md5(ksort 升序、剔除空字符串值、除 sign 外全部参数以 k=v 用 & 拼接 + "&key=" + app_secret)。
// 常数时间比较。
func signValid(params map[string]string, appSecret, sign string) bool {
	keys := make([]string, 0, len(params))
	for key, value := range params {
		if key == "sign" || value == "" {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	pairs := make([]string, 0, len(keys))
	for _, key := range keys {
		pairs = append(pairs, key+"="+params[key])
	}
	payload := strings.Join(pairs, "&") + "&key=" + appSecret
	sum := md5.Sum([]byte(payload))
	return subtle.ConstantTimeCompare([]byte(hex.EncodeToString(sum[:])), []byte(sign)) == 1
}

// respondOK 成功壳：{code:200,msg,data}，HTTP 恒 200。
func respondOK(c *gin.Context, data any) {
	c.Header("Cache-Control", "no-store, private")
	c.JSON(http.StatusOK, gin.H{"code": 200, "msg": msgSuccess, "data": data})
}

// respondError 失败壳：{code:0,msg}，HTTP 恒 200（acg 协议口径）。
func respondError(c *gin.Context, message string) {
	c.Header("Cache-Control", "no-store, private")
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": message})
}

// shopName 站点名（设置里的 site_name，缺省固定文案）。
func (h *Handler) shopName() string {
	config, err := h.base.Settings.GetByKey(constants.SettingKeySiteConfig)
	if err == nil && config != nil {
		if name, ok := config["site_name"].(string); ok && strings.TrimSpace(name) != "" {
			return strings.TrimSpace(name)
		}
	}
	return "Dujiao Next"
}

// localizedText 多语言字段取展示文本：zh-CN → zh-TW → en-US → 任意非空。
func localizedText(value map[string]interface{}) string {
	if len(value) == 0 {
		return ""
	}
	for _, locale := range []string{"zh-CN", "zh-TW", "zh", "en-US", "en"} {
		if text, ok := value[locale].(string); ok && strings.TrimSpace(text) != "" {
			return strings.TrimSpace(text)
		}
	}
	for _, raw := range value {
		if text, ok := raw.(string); ok && strings.TrimSpace(text) != "" {
			return strings.TrimSpace(text)
		}
	}
	return ""
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func atoiOr(value string, fallback int) int {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return fallback
	}
	return parsed
}
