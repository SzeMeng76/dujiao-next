package upstream

// acg-faka（异次元发卡）共享店铺协议 —— 出站适配器。
//
// 与入站实现（internal/modules/upstreamapi/transport/http/acg）互为对偶：
// 入站那一套是本站扮演上游，这一套是本站扮演下游去 acg 站进货。
//
// 协议要点：
//   - 全部 POST，application/x-www-form-urlencoded
//   - 鉴权走表单字段：app_id + sign，sign = md5(键名升序的 k=v 用 & 拼接，
//     剔除空值与 sign 本身，末尾追加 "&key=" + app_key)
//   - 响应恒为 HTTP 200，业务错误用 {code:0,msg} 表达
//   - 下单同步返回卡密（data.secret）；未交付时回 code:0，用同一 request_no
//     重试即可命中原订单幂等，因此这类响应属于「可重试」而不是失败

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dujiao-next/internal/constants"
	"github.com/dujiao-next/internal/logger"
	siteconnectiondomain "github.com/dujiao-next/internal/modules/siteconnection/domain"
	"github.com/dujiao-next/internal/shared/jsonmap"
)

// acgFakaEndpoints 端点相对路径（挂在 /shared 前缀下）。
const (
	acgFakaConnect = "authentication/connect"
	acgFakaItems   = "commodity/items"
	acgFakaTrade   = "commodity/trade"
	acgFakaQuery   = "commodity/query"
)

// acgBizError acg 用 HTTP 200 + {code:0,msg} 表达业务失败。
type acgBizError struct{ Message string }

func (e *acgBizError) Error() string { return e.Message }

// acgThrottleMarker 上游限流文案。命中说明请求太密被掐了，不是业务失败：
// 请求没有被处理，退避后重发是安全的。
const acgThrottleMarker = "服务器频繁"

// acgThrottleBackoff 限流后的退避基数（第 n 次重试等待 n × 该值）。包级变量便于测试清零。
var acgThrottleBackoff = 10 * time.Second

func isAcgThrottled(message string) bool {
	return strings.Contains(message, acgThrottleMarker)
}

// acgNumber acg 的 id / 金额既可能是数字也可能是数字字符串，统一按原始文本收。
type acgNumber string

func (n *acgNumber) UnmarshalJSON(data []byte) error {
	text := strings.TrimSpace(string(data))
	if text == "" || text == "null" {
		*n = ""
		return nil
	}
	if strings.HasPrefix(text, "\"") {
		var value string
		if err := json.Unmarshal(data, &value); err != nil {
			return err
		}
		*n = acgNumber(strings.TrimSpace(value))
		return nil
	}
	*n = acgNumber(text)
	return nil
}

// AcgFakaAdapter acg-faka 共享店铺协议适配器。
type AcgFakaAdapter struct {
	baseURL    string
	appID      string
	appKey     string
	uploadsDir string
	client     *http.Client
}

// NewAcgFakaAdapter 创建 acg-faka 适配器。
func NewAcgFakaAdapter(conn *siteconnectiondomain.Connection, uploadsDir string) *AcgFakaAdapter {
	return &AcgFakaAdapter{
		baseURL:    normalizeAcgBaseURL(conn.BaseURL),
		appID:      strings.TrimSpace(conn.ApiKey),
		appKey:     conn.ApiSecret,
		uploadsDir: uploadsDir,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// normalizeAcgBaseURL 允许用户填站点根地址，也允许直接粘贴 /shared 供货地址。
func normalizeAcgBaseURL(raw string) string {
	base := strings.TrimRight(strings.TrimSpace(raw), "/")
	base = strings.TrimSuffix(base, "/shared")
	return strings.TrimRight(base, "/")
}

// acgFakaSign acg-faka 表单签名：
// 键名升序、剔除空值与 sign 本身，以 k=v 用 & 拼接后追加 "&key=" + app_key，
// 取 md5 十六进制小写。必须与入站侧 signValid 完全一致。
func acgFakaSign(params map[string]string, appKey string) string {
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
	sum := md5.Sum([]byte(strings.Join(pairs, "&") + "&key=" + appKey))
	return hex.EncodeToString(sum[:])
}

// acgEnvelope acg 响应外壳。
type acgEnvelope struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

// postForm 发送带签名表单请求（读取类）：限流时退避重试。
func (a *AcgFakaAdapter) postForm(ctx context.Context, endpoint string, params map[string]string, out interface{}) error {
	return a.doForm(ctx, endpoint, params, out, true)
}

// doForm 发送带签名表单请求，成功时把 data 解到 out。
//
// retryThrottle 只对读取类接口开启。下单（trade）遇到限流不重发：
// 「上游回限流就说明没受理」只是推断，不是协议保证；一旦推断错了就是重复下单，
// 代价远大于让采购层按 30/60/300 秒重试。这与 zcard 的 `withoutRetries` 口径一致。
func (a *AcgFakaAdapter) doForm(ctx context.Context, endpoint string, params map[string]string, out interface{}, retryThrottle bool) error {
	form := make(map[string]string, len(params)+2)
	for key, value := range params {
		if value != "" {
			form[key] = value
		}
	}
	form["app_id"] = a.appID
	form["sign"] = acgFakaSign(form, a.appKey)

	values := make(url.Values, len(form))
	for key, value := range form {
		values.Set(key, value)
	}
	body := values.Encode()

	pacer := pacerFor(a.baseURL + "|" + a.appID)
	path := "/shared/" + endpoint

	for attempt := 1; ; attempt++ {
		if err := pacer.wait(ctx); err != nil {
			return err
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+path, bytes.NewReader([]byte(body)))
		if err != nil {
			return fmt.Errorf("create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Accept", "application/json")

		resp, err := a.client.Do(req)
		if err != nil {
			return fmt.Errorf("send request: %w", err)
		}
		respBody, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			return fmt.Errorf("read response: %w", readErr)
		}

		if resp.StatusCode == http.StatusOK {
			var envelope acgEnvelope
			if err := json.Unmarshal(respBody, &envelope); err != nil {
				return fmt.Errorf("unmarshal response: %w", err)
			}
			if envelope.Code != http.StatusOK {
				message := strings.TrimSpace(envelope.Msg)
				if message == "" {
					message = "acg 接口返回未知错误"
				}
				// 限流：请求没有被处理，退避后重发是安全的。当成普通业务失败会直接
				// 消耗采购的重试档位，而对方的窗口往往要几十秒才恢复。
				if retryThrottle && attempt < maxRequestAttempts && isAcgThrottled(message) {
					wait := time.Duration(attempt) * acgThrottleBackoff
					if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > wait {
						if waitErr := sleepCtx(ctx, wait); waitErr == nil {
							continue
						}
					}
				}
				return &acgBizError{Message: message}
			}
			if out != nil {
				if err := json.Unmarshal(envelope.Data, out); err != nil {
					return fmt.Errorf("unmarshal data: %w", err)
				}
			}
			return nil
		}

		logger.Warnw("acg_upstream_request_error",
			"endpoint", endpoint, "status", resp.StatusCode, "attempt", attempt, "body", string(respBody))

		if attempt < maxRequestAttempts && resp.StatusCode == http.StatusTooManyRequests {
			wait := upstreamRetryWait(resp, attempt)
			if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) <= wait {
				return newUpstreamHTTPError(resp.StatusCode, respBody)
			}
			if waitErr := sleepCtx(ctx, wait); waitErr != nil {
				return newUpstreamHTTPError(resp.StatusCode, respBody)
			}
			continue
		}

		return newUpstreamHTTPError(resp.StatusCode, respBody)
	}
}

// ── 连接 ───────────────────────────────────────────────────

// Ping 连接测试（acg: authentication/connect）。
func (a *AcgFakaAdapter) Ping(ctx context.Context) (*PingResult, error) {
	var data struct {
		ShopName string    `json:"shopName"`
		Balance  acgNumber `json:"balance"`
	}
	if err := a.postForm(ctx, acgFakaConnect, nil, &data); err != nil {
		return nil, err
	}
	return &PingResult{
		SiteName: data.ShopName,
		Balance:  acgAmountString(data.Balance),
		Currency: constants.SiteCurrencyDefault,
	}, nil
}

// ── 目录 ───────────────────────────────────────────────────

// acgCategory acg 目录节点：顶层是分类，children 是商品。
type acgCategory struct {
	ID       acgNumber       `json:"id"`
	Name     string          `json:"name"`
	Children []acgProductRow `json:"children"`
}

// acgProductRow acg 商品行。
//
// id 与 code 分工：id 是数据库主键（数字），code 是下游对接用的商品编号（任意字符串，
// 实测为 16 位十六进制）。trade 只认 code，所以两者都必须留下。
//
// config 是规格配置（INI 字符串或已解析对象）：[category] 是「种类」（每种一个价），
// [sku] 是「规格」（维度 → 选项 → 加价）。下单时必须带 race / sku[...]，否则上游会
// 按「未选种类」处理——库存跨种类合并、成交走基础价，等于悄悄买错档位。
type acgProductRow struct {
	ID           acgNumber       `json:"id"`
	Code         string          `json:"code"`
	Name         string          `json:"name"`
	Price        acgNumber       `json:"price"`
	FactoryPrice acgNumber       `json:"factory_price"`
	Description  string          `json:"description"`
	Introduce    string          `json:"introduce"`
	Cover        string          `json:"cover"`
	Status       int             `json:"status"`
	Stock        int             `json:"stock"`
	CategoryID   acgNumber       `json:"category_id"`
	Config       json.RawMessage `json:"config"`
}

// acgSpecConfig 商品 config 里的规格配置。
//   - Race：[category] 段，种类名 → 单价（元），每个种类独立定价
//   - SKU：[sku] 段，规格维度 → 选项 → 加价（元）
type acgSpecConfig struct {
	Race map[string]string
	SKU  map[string]map[string]string
}

// acgMaxCombos 组合数护栏：种类 × 各规格选项的笛卡尔积超限时整品不收录，
// 避免把价格语义不完整的商品放给下游（对齐 zcard 的护栏口径）。
const acgMaxCombos = 64

// parseAcgSpec 解析商品 config。config 可能是 INI 字符串（标准 acg-faka），
// 也可能是上游直接给出的解析对象。
func parseAcgSpec(raw json.RawMessage) acgSpecConfig {
	text := strings.TrimSpace(string(raw))
	if text == "" || text == "null" {
		return acgSpecConfig{}
	}
	if strings.HasPrefix(text, "{") {
		return parseAcgSpecObject(raw)
	}
	var content string
	if err := json.Unmarshal(raw, &content); err != nil {
		return acgSpecConfig{}
	}
	return parseAcgSpecINI(content)
}

// parseAcgSpecObject 解析「已反序列化」形态的 config 对象。
func parseAcgSpecObject(raw json.RawMessage) acgSpecConfig {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return acgSpecConfig{}
	}
	spec := acgSpecConfig{}
	if section, ok := obj["category"]; ok {
		var race map[string]acgNumber
		if err := json.Unmarshal(section, &race); err == nil && len(race) > 0 {
			spec.Race = make(map[string]string, len(race))
			for name, price := range race {
				spec.Race[name] = string(price)
			}
		}
	}
	if section, ok := obj["sku"]; ok {
		var dims map[string]map[string]acgNumber
		if err := json.Unmarshal(section, &dims); err == nil && len(dims) > 0 {
			spec.SKU = make(map[string]map[string]string, len(dims))
			for dim, options := range dims {
				values := make(map[string]string, len(options))
				for option, premium := range options {
					values[option] = string(premium)
				}
				spec.SKU[dim] = values
			}
		}
	}
	return spec
}

// parseAcgSpecINI 极简 INI 解析：只取 [category] 与 [sku] 两段；
// [sku] 用点号表达一层嵌套（`颜色.黑色=0` → 颜色 → 黑色 → 0）。
func parseAcgSpecINI(content string) acgSpecConfig {
	spec := acgSpecConfig{}
	section := ""
	for _, line := range strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.TrimSpace(line[1 : len(line)-1])
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		switch {
		case strings.EqualFold(section, "category"):
			if spec.Race == nil {
				spec.Race = map[string]string{}
			}
			spec.Race[key] = value
		case strings.EqualFold(section, "sku"):
			dot := strings.Index(key, ".")
			if dot <= 0 || dot == len(key)-1 {
				continue
			}
			dim := strings.TrimSpace(key[:dot])
			option := strings.TrimSpace(key[dot+1:])
			if dim == "" || option == "" {
				continue
			}
			if spec.SKU == nil {
				spec.SKU = map[string]map[string]string{}
			}
			if spec.SKU[dim] == nil {
				spec.SKU[dim] = map[string]string{}
			}
			spec.SKU[dim][option] = value
		}
	}
	return spec
}

// acgCombo 一个可购组合（种类 × 各规格选项）。
type acgCombo struct {
	Race    string
	Choices map[string]string
	Price   string
}

// buildAcgCombos 把规格配置展开成可购组合。无规格时返回单个空组合（沿用商品价）。
func buildAcgCombos(spec acgSpecConfig, basePrice string) ([]acgCombo, error) {
	if len(spec.Race) == 0 && len(spec.SKU) == 0 {
		return []acgCombo{{Price: basePrice}}, nil
	}

	races := make([]string, 0, len(spec.Race))
	for name := range spec.Race {
		races = append(races, name)
	}
	sort.Strings(races)
	if len(races) == 0 {
		races = []string{""}
	}

	dims := make([]string, 0, len(spec.SKU))
	for dim := range spec.SKU {
		dims = append(dims, dim)
	}
	sort.Strings(dims)

	total := len(races)
	for _, dim := range dims {
		total *= len(spec.SKU[dim])
	}
	if total > acgMaxCombos {
		return nil, fmt.Errorf("规格组合数 %d 超过上限 %d", total, acgMaxCombos)
	}

	combos := make([]acgCombo, 0, total)
	choices := make(map[string]string, len(dims))
	var walk func(index int, premium float64)
	walk = func(index int, premium float64) {
		if index == len(dims) {
			for _, race := range races {
				price := basePrice
				if race != "" {
					price = spec.Race[race]
				}
				copied := make(map[string]string, len(choices))
				for dim, option := range choices {
					copied[dim] = option
				}
				combos = append(combos, acgCombo{
					Race:    race,
					Choices: copied,
					Price:   acgAddAmount(price, premium),
				})
			}
			return
		}
		dim := dims[index]
		options := make([]string, 0, len(spec.SKU[dim]))
		for option := range spec.SKU[dim] {
			options = append(options, option)
		}
		sort.Strings(options)
		for _, option := range options {
			choices[dim] = option
			walk(index+1, premium+acgParseAmount(spec.SKU[dim][option]))
			delete(choices, dim)
		}
	}
	walk(0, 0)
	return combos, nil
}

// acgSelection 下单时回传给上游的规格选择（编码进 sku_mappings.upstream_sku_code）。
type acgSelection struct {
	Race string            `json:"r,omitempty"`
	SKU  map[string]string `json:"s,omitempty"`
}

// encodeAcgSelection 组合 → 编码；无规格时返回空串（沿用单 SKU 路径）。
func encodeAcgSelection(combo acgCombo) string {
	if combo.Race == "" && len(combo.Choices) == 0 {
		return ""
	}
	data, err := json.Marshal(acgSelection{Race: combo.Race, SKU: combo.Choices})
	if err != nil {
		return ""
	}
	return string(data)
}

// decodeAcgSelection 编码 → 种类 + 规格选择。
func decodeAcgSelection(code string) (string, map[string]string, bool) {
	text := strings.TrimSpace(code)
	if text == "" || !strings.HasPrefix(text, "{") {
		return "", nil, false
	}
	var selection acgSelection
	if err := json.Unmarshal([]byte(text), &selection); err != nil {
		return "", nil, false
	}
	return selection.Race, selection.SKU, true
}

// acgComboID 组合的数字身份（映射表只存 uint）：对编码取 FNV-1a 32 位。
// 同一个组合内容总是得到同一个 ID，上游改配置导致组合消失时旧映射会被判失效，
// 不会误指向另一个组合。
func acgComboID(encoded string) uint {
	if encoded == "" {
		return 0
	}
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(encoded))
	if id := hash.Sum32(); id != 0 {
		return uint(id)
	}
	return 1
}

// acgParseAmount 元字符串 → float64；解析失败按 0 处理。
func acgParseAmount(raw string) float64 {
	value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		return 0
	}
	return value
}

// acgAddAmount 基准价 + 加价，输出两位小数元字符串。
func acgAddAmount(base string, premium float64) string {
	return strconv.FormatFloat(acgParseAmount(base)+premium, 'f', 2, 64)
}

// acgCatalogTTL 目录缓存寿命。acg 的 items 一次返回整站目录（实测 8000+ 商品、近 1MB、
// 生成一次要几十秒），而 id→code 的换算是下单与单品同步的必需信息：每次现拉既慢
// 又会撞上游限流，所以按连接做 TTL 缓存。批量导入时一轮同步就可能超过 5 分钟，
// TTL 太短会在导入中途反复现拉。
const acgCatalogTTL = 15 * time.Minute

// acgCachedProduct 目录里的一条商品记录。
type acgCachedProduct struct {
	row        acgProductRow
	categoryID uint
}

type acgCatalogEntry struct {
	fetchedAt time.Time
	items     []acgCachedProduct
	byID      map[uint]acgCachedProduct
}

var acgCatalogStore = struct {
	mu      sync.Mutex
	entries map[string]*acgCatalogEntry
}{entries: make(map[string]*acgCatalogEntry)}

func (a *AcgFakaAdapter) catalogKey() string { return a.baseURL + "|" + a.appID }

// cachedCatalog 返回未过期的目录缓存，没有时返回 nil。
func (a *AcgFakaAdapter) cachedCatalog() *acgCatalogEntry {
	acgCatalogStore.mu.Lock()
	defer acgCatalogStore.mu.Unlock()
	entry := acgCatalogStore.entries[a.catalogKey()]
	if entry == nil || time.Since(entry.fetchedAt) > acgCatalogTTL {
		return nil
	}
	return entry
}

// refreshCatalog 现拉一次目录并写入缓存。
func (a *AcgFakaAdapter) refreshCatalog(ctx context.Context) (*acgCatalogEntry, error) {
	tree, err := a.fetchTree(ctx)
	if err != nil {
		return nil, err
	}
	entry := &acgCatalogEntry{
		fetchedAt: time.Now(),
		items:     make([]acgCachedProduct, 0),
		byID:      make(map[uint]acgCachedProduct),
	}
	skippedInvalidSpec := 0
	for _, node := range tree {
		categoryID := acgUint(node.ID)
		for _, row := range node.Children {
			id := acgUint(row.ID)
			if id == 0 {
				continue
			}
			// 规格组合超护栏时整品不收录：价格语义不完整，放给下游只会卖错。
			if _, err := buildAcgCombos(parseAcgSpec(row.Config), acgAmountString(row.Price)); err != nil {
				skippedInvalidSpec++
				continue
			}
			cached := acgCachedProduct{row: row, categoryID: categoryID}
			entry.items = append(entry.items, cached)
			entry.byID[id] = cached
		}
	}
	if skippedInvalidSpec > 0 {
		logger.Warnw("acg_catalog_skip_invalid_spec_products",
			"base_url", a.baseURL, "skipped", skippedInvalidSpec)
	}
	acgCatalogStore.mu.Lock()
	acgCatalogStore.entries[a.catalogKey()] = entry
	acgCatalogStore.mu.Unlock()
	return entry, nil
}

// catalogOrFetch 取目录：优先缓存，force=true 或无缓存时现拉。
func (a *AcgFakaAdapter) catalogOrFetch(ctx context.Context, force bool) (*acgCatalogEntry, error) {
	if !force {
		if entry := a.cachedCatalog(); entry != nil {
			return entry, nil
		}
	}
	return a.refreshCatalog(ctx)
}

// productCode 用商品 id 换 trade 需要的 shared_code。
// 缓存未命中时强制刷新一次，仍找不到返回空串（商品已下架或删除）。
func (a *AcgFakaAdapter) productCode(ctx context.Context, productID uint) (string, error) {
	entry, err := a.catalogOrFetch(ctx, false)
	if err != nil {
		return "", err
	}
	if cached, ok := entry.byID[productID]; ok {
		return strings.TrimSpace(cached.row.Code), nil
	}
	entry, err = a.catalogOrFetch(ctx, true)
	if err != nil {
		return "", err
	}
	if cached, ok := entry.byID[productID]; ok {
		return strings.TrimSpace(cached.row.Code), nil
	}
	return "", nil
}

// ListCategories 拉取上游分类列表。
func (a *AcgFakaAdapter) ListCategories(ctx context.Context) (*CategoryListResult, error) {
	tree, err := a.fetchTree(ctx)
	if err != nil {
		return nil, err
	}
	categories := make([]UpstreamCategory, 0, len(tree))
	for _, node := range tree {
		categories = append(categories, UpstreamCategory{
			ID:   acgUint(node.ID),
			Name: acgLocalized(node.Name),
		})
	}
	return &CategoryListResult{Supported: true, Categories: categories}, nil
}

// ListProducts 拉取上游商品列表。
// acg 的 commodity/items 不支持分页，一次返回整站目录；第 2 页起返回空，
// 让调用方的翻页循环在第一页后正常收尾。
func (a *AcgFakaAdapter) ListProducts(ctx context.Context, opts ListProductsOpts) (*ProductListResult, error) {
	if opts.Page > 1 {
		return &ProductListResult{Items: []UpstreamProduct{}, Page: opts.Page}, nil
	}
	entry, err := a.refreshCatalog(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]UpstreamProduct, 0, len(entry.items))
	for _, cached := range entry.items {
		product, ok := acgToProduct(cached.row, cached.categoryID)
		if !ok {
			continue
		}
		items = append(items, product)
	}
	return &ProductListResult{
		Total:            len(items),
		Items:            items,
		Page:             1,
		PageSize:         len(items),
		IncludesInactive: true,
	}, nil
}

// GetProduct 获取单个商品详情。
func (a *AcgFakaAdapter) GetProduct(ctx context.Context, productID uint) (*UpstreamProduct, error) {
	if productID == 0 {
		return nil, ErrUpstreamProductDeleted
	}
	entry, err := a.catalogOrFetch(ctx, false)
	if err != nil {
		return nil, err
	}
	cached, ok := entry.byID[productID]
	if !ok {
		// 缓存可能还停留在商品上架时：强制刷新一次再判定是否真的没了。
		if entry, err = a.catalogOrFetch(ctx, true); err != nil {
			return nil, err
		}
		if cached, ok = entry.byID[productID]; !ok {
			return nil, ErrUpstreamProductUnavailable
		}
	}
	product, ok := acgToProduct(cached.row, cached.categoryID)
	if !ok {
		return nil, ErrUpstreamProductUnavailable
	}
	return &product, nil
}

// fetchTree 拉取分类 + 商品目录树。
func (a *AcgFakaAdapter) fetchTree(ctx context.Context) ([]acgCategory, error) {
	var tree []acgCategory
	if err := a.postForm(ctx, acgFakaItems, nil, &tree); err != nil {
		return nil, err
	}
	return tree, nil
}

// acgToProduct 商品行 → 内部商品模型。
// acg 一个商品只有一条隐式 SKU：SKU ID 复用商品 id（数字主键，本站映射表只存 uint），
// 真正的上游商品编号 code 放在 SKUCode 里，下单时再换回去。
func acgToProduct(row acgProductRow, categoryID uint) (UpstreamProduct, bool) {
	id := acgUint(row.ID)
	if id == 0 {
		return UpstreamProduct{}, false
	}
	price := acgAmountString(row.Price)
	if price == "" {
		price = "0.00"
	}
	combos, err := buildAcgCombos(parseAcgSpec(row.Config), price)
	if err != nil {
		return UpstreamProduct{}, false
	}
	stockStatus := constants.ProductStockStatusOutOfStock
	switch {
	case row.Stock < 0:
		stockStatus = constants.ProductStockStatusUnlimited
	case row.Stock > 0:
		stockStatus = constants.ProductStockStatusInStock
	}

	// acg 只有一个 description 列，里面装的就是商品详情（富文本），它没有单独的「简介」。
	// 本站 简介/详情 是两栏，这里放进详情，简介留空——否则详情会显示在商品简介位置。
	detail := jsonmap.JSON{}
	if text := strings.TrimSpace(row.Description); text != "" {
		detail = acgLocalized(text)
	}

	skus := make([]UpstreamSKU, 0, len(combos))
	lowest := price
	for index, combo := range combos {
		encoded := encodeAcgSelection(combo)
		skuID := id
		code := strconv.FormatUint(uint64(id), 10)
		if encoded != "" {
			// 数字 ID 表达不了规格选择：身份取选择编码的哈希，选择本身放 UpstreamCode。
			skuID = acgComboID(encoded)
			code = "C" + strconv.Itoa(index+1)
		}
		specValues := jsonmap.JSON{}
		if combo.Race != "" {
			specValues["种类"] = combo.Race
		}
		for dim, option := range combo.Choices {
			specValues[dim] = option
		}
		if index == 0 || acgParseAmount(combo.Price) < acgParseAmount(lowest) {
			lowest = combo.Price
		}
		skus = append(skus, UpstreamSKU{
			ID:            skuID,
			UpstreamCode:  encoded,
			SKUCode:       code,
			SpecValues:    specValues,
			PriceAmount:   combo.Price,
			StockStatus:   stockStatus,
			StockQuantity: row.Stock,
			IsActive:      row.Status == 1,
		})
	}

	return UpstreamProduct{
		ID:              id,
		Title:           acgLocalized(row.Name),
		Description:     jsonmap.JSON{},
		Content:         detail,
		Images:          acgImages(row.Cover),
		PriceAmount:     lowest,
		Currency:        constants.SiteCurrencyDefault,
		FulfillmentType: constants.FulfillmentTypeAuto,
		IsActive:        row.Status == 1,
		CategoryID:      categoryID,
		SKUs:            skus,
	}, true
}

// ── 订单 ───────────────────────────────────────────────────

// CreateOrder 发起采购单（acg: commodity/trade）。
// acg 在响应里同步带卡密；未交付时返回 code:0，按可重试错误返回，
// 调用方用同一 downstream_order_no 重试会命中原订单幂等。
func (a *AcgFakaAdapter) CreateOrder(ctx context.Context, req CreateUpstreamOrderReq) (*CreateUpstreamOrderResp, error) {
	if req.SKUID == 0 {
		return &CreateUpstreamOrderResp{OK: false, ErrorCode: "invalid_request", ErrorMessage: "缺少上游 SKU"}, nil
	}
	quantity := req.Quantity
	if quantity < 1 {
		quantity = 1
	}

	// trade 只认商品的 code（字符串），映射表里存的是数字 id，先换算。
	code, err := a.productCode(ctx, req.SKUID)
	if err != nil {
		return nil, err
	}
	if code == "" {
		return &CreateUpstreamOrderResp{
			OK:           false,
			ErrorCode:    "product_unavailable",
			ErrorMessage: "上游商品不存在或已下架",
		}, nil
	}

	var data struct {
		Amount  acgNumber `json:"amount"`
		TradeNo acgNumber `json:"tradeNo"`
		Secret  string    `json:"secret"`
	}
	params := map[string]string{
		"shared_code": code,
		"num":         strconv.Itoa(quantity),
		"request_no":  req.DownstreamOrderNo,
	}
	// 规格选择：种类走 race，规格走 sku[维度]=选项。PHP 侧把 sku[..] 解析成嵌套数组，
	// 签名与表单都按展平后的键排序，与服务端 http_build_query 的输出字节一致。
	if race, choices, ok := decodeAcgSelection(req.SKUCode); ok {
		if race != "" {
			params["race"] = race
		}
		dims := make([]string, 0, len(choices))
		for dim := range choices {
			dims = append(dims, dim)
		}
		sort.Strings(dims)
		for _, dim := range dims {
			params["sku["+dim+"]"] = choices[dim]
		}
	}

	// 下单不因限流重发（见 doForm 的说明）。
	err = a.doForm(ctx, acgFakaTrade, params, &data, false)
	if err != nil {
		var biz *acgBizError
		if errors.As(err, &biz) {
			return &CreateUpstreamOrderResp{
				OK:           false,
				ErrorCode:    acgFakaErrorCode(biz.Message),
				ErrorMessage: biz.Message,
			}, nil
		}
		return nil, err
	}

	orderID := acgUint(data.TradeNo)
	resp := &CreateUpstreamOrderResp{
		OK:       true,
		OrderID:  orderID,
		OrderNo:  strconv.FormatUint(uint64(orderID), 10),
		Amount:   acgAmountString(data.Amount),
		Currency: constants.SiteCurrencyDefault,
	}
	// 已同步带卡密时直接给了交付内容，采购管道可跳过轮询。
	if payload := strings.TrimSpace(data.Secret); payload != "" {
		now := time.Now()
		resp.Status = "delivered"
		resp.Fulfillment = &UpstreamFulfillment{
			Type:        constants.FulfillmentTypeAuto,
			Status:      constants.FulfillmentStatusDelivered,
			Payload:     payload,
			DeliveredAt: &now,
		}
	} else {
		resp.Status = "accepted"
	}
	return resp, nil
}

// GetOrder 查询上游订单状态（acg: commodity/query，按 tradeNo）。
func (a *AcgFakaAdapter) GetOrder(ctx context.Context, orderID uint) (*UpstreamOrderDetail, error) {
	var data struct {
		Status int    `json:"status"`
		Secret string `json:"secret"`
	}
	if err := a.postForm(ctx, acgFakaQuery, map[string]string{
		"tradeNo": strconv.FormatUint(uint64(orderID), 10),
	}, &data); err != nil {
		return nil, err
	}

	detail := &UpstreamOrderDetail{
		OrderID:  orderID,
		OrderNo:  strconv.FormatUint(uint64(orderID), 10),
		Currency: constants.SiteCurrencyDefault,
	}
	if data.Status == 1 {
		detail.Status = "delivered"
		if payload := strings.TrimSpace(data.Secret); payload != "" {
			now := time.Now()
			detail.Fulfillment = &UpstreamFulfillment{
				Type:        constants.FulfillmentTypeAuto,
				Status:      constants.FulfillmentStatusDelivered,
				Payload:     payload,
				DeliveredAt: &now,
			}
		}
	} else {
		detail.Status = "pending"
	}
	return detail, nil
}

// CancelOrder acg 协议没有取消接口。返回哨兵错误，调用方按「上游拒绝取消」处理。
func (a *AcgFakaAdapter) CancelOrder(ctx context.Context, orderID uint) error {
	return fmt.Errorf("%w: acg-faka 协议不支持取消订单", ErrUpstreamCancelNotAllowed)
}

// DownloadImage 下载上游封面到本地（只允许同主机，防止内网地址写入 /uploads）。
func (a *AcgFakaAdapter) DownloadImage(ctx context.Context, imageURL string) (string, error) {
	return downloadUpstreamImage(ctx, a.client, a.baseURL, a.uploadsDir, imageURL)
}

// ── 工具 ───────────────────────────────────────────────────

// acgUint id 统一成 uint；非数字（acg 理论上不会出现）返回 0。
func acgUint(raw acgNumber) uint {
	text := strings.TrimSpace(string(raw))
	if text == "" {
		return 0
	}
	id, err := strconv.ParseUint(text, 10, 64)
	if err != nil {
		return 0
	}
	return uint(id)
}

// acgAmountString 金额统一格式化成两位小数的元字符串（本站 money 口径）。
func acgAmountString(raw acgNumber) string {
	text := strings.TrimSpace(string(raw))
	if text == "" {
		return ""
	}
	value, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return ""
	}
	return strconv.FormatFloat(value, 'f', 2, 64)
}

// acgLocalized 单语言文案 → 三语 JSON（上游只有一种文字，三种语言同值）。
func acgLocalized(text string) jsonmap.JSON {
	value := strings.TrimSpace(text)
	return jsonmap.JSON{
		"zh-CN": value,
		"zh-TW": value,
		"en-US": value,
	}
}

func acgImages(cover string) []string {
	if strings.TrimSpace(cover) == "" {
		return []string{}
	}
	return []string{cover}
}

// acgFakaErrorCode 把 acg 的中文错误文案归一到采购管道的错误码。
// 未知文案按可重试处理：重试几次仍失败会转人工，比直接判死更安全。
func acgFakaErrorCode(message string) string {
	switch {
	case strings.Contains(message, "余额不足"), strings.Contains(message, "餘額不足"):
		return "insufficient_balance"
	case strings.Contains(message, "库存不足"), strings.Contains(message, "庫存不足"):
		return "product_out_of_stock"
	case strings.Contains(message, "商品不存在"), strings.Contains(message, "下架"):
		return "product_unavailable"
	case strings.Contains(message, "最少购买"), strings.Contains(message, "最多购买"):
		return "invalid_request"
	case isAcgThrottled(message):
		return "rate_limited"
	case strings.Contains(message, "request ID already exists"), strings.Contains(message, "订单已存在"):
		// request_no 是「重复即报错」的防重键：说明同键请求已被受理过，
		// 结果可能已扣款发货，不能重试（重试永远撞同一堵墙），转人工核对。
		return "duplicate_order"
	default:
		return "acg_upstream_rejected"
	}
}
