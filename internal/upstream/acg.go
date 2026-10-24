package upstream

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	siteconnectiondomain "github.com/dujiao-next/internal/modules/siteconnection/domain"
	"github.com/dujiao-next/internal/logger"
	"github.com/dujiao-next/internal/shared/jsonmap"
)

// ACGAdapter 对接异次元（ACG-Faka）共享店铺 API 的上游适配器。
//
// 协议要点：
//   - 全部请求为 POST + application/x-www-form-urlencoded；
//   - 每个请求体都带 app_id 与 sign，app_key 只在本地参与签名、不发送；
//   - 商品与订单都用字符串 code / tradeNo 标识。
//
// 与框架的对接：框架内部用 uint 标识上游商品与 SKU。异次元的 code 通常为数字，
// 此时直接换算为 uint；非数字 code 使用 FNV-1a 31 位散列，并在进程内缓存
// 反向映射，供 GetProduct 使用。
type ACGAdapter struct {
	baseURL string
	appID   string
	appKey  string
	client  *http.Client

	// codeCache 缓存 散列ID → 原始 code，供 GetProduct 反查。
	// ListProducts 时填充；进程重启后若先调用 GetProduct 会查不到，
	// 由调用方触发一次同步即可恢复。
	mu        sync.RWMutex
	codeCache map[uint]string
}

// NewACGAdapter 创建异次元适配器。
func NewACGAdapter(conn *siteconnectiondomain.Connection) *ACGAdapter {
	return &ACGAdapter{
		baseURL:   strings.TrimRight(conn.BaseURL, "/"),
		appID:     conn.ApiKey,
		appKey:    conn.ApiSecret,
		client:    &http.Client{Timeout: 30 * time.Second},
		codeCache: make(map[uint]string),
	}
}

var _ Adapter = (*ACGAdapter)(nil)

// acgReply 是异次元统一响应结构。
type acgReply struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

// acgGroup 是共享商品分组。
type acgGroup struct {
	ID       json.RawMessage `json:"id"`
	Name     string          `json:"name"`
	Children []acgChild      `json:"children"`
}

// acgChild 是分组下的共享商品。共享目录接口会直接返回售价与库存，
// 因此导入时不逐项询价；下单前的实时核价由 procurement 的提交阶段负责。
//
// 注意 stock 在真实接口里是字符串（如 "1848"），price / user_price 是数字。
type acgChild struct {
	ID          json.RawMessage `json:"id"`
	Code        json.RawMessage `json:"code"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Cover       string          `json:"cover"`
	Stock       json.RawMessage `json:"stock"`
	Inventory   json.RawMessage `json:"inventory"`
	UserPrice   *float64        `json:"user_price"`
	Price       *float64        `json:"price"`
}

// acgStockValue 解析库存；接口可能返回数字或字符串，-1 表示无限。
func acgStockValue(raw json.RawMessage) int {
	text := acgRawString(raw)
	if text == "" {
		return -1
	}
	value, err := strconv.Atoi(text)
	if err != nil {
		// 可能是小数形式，取整数部分。
		asFloat, floatErr := strconv.ParseFloat(text, 64)
		if floatErr != nil {
			return -1
		}
		value = int(asFloat)
	}
	return value
}

func (c acgChild) stockValue() int {
	if stock := acgStockValue(c.Stock); stock != -1 || acgRawString(c.Stock) != "" {
		return stock
	}
	if stock := acgStockValue(c.Inventory); stock != -1 || acgRawString(c.Inventory) != "" {
		return stock
	}
	return -1
}

func (c acgChild) priceValue() string {
	if c.UserPrice != nil {
		return strconv.FormatFloat(*c.UserPrice, 'f', 2, 64)
	}
	if c.Price != nil {
		return strconv.FormatFloat(*c.Price, 'f', 2, 64)
	}
	return "0.00"
}

func acgRawString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	text := strings.TrimSpace(string(raw))
	if text == "null" {
		return ""
	}
	if strings.HasPrefix(text, `"`) {
		var decoded string
		if err := json.Unmarshal(raw, &decoded); err == nil {
			return strings.TrimSpace(decoded)
		}
	}
	return strings.Trim(text, `"`)
}

// acgIDForCode 把上游字符串 code 映射为框架内的 uint。
// 数字 code 直接用其数值；非数字使用 FNV-1a 31 位散列。
func acgIDForCode(code string) uint {
	if numeric, err := strconv.ParseUint(strings.TrimSpace(code), 10, 32); err == nil && numeric > 0 {
		return uint(numeric)
	}
	hasher := fnv.New32a()
	_, _ = hasher.Write([]byte(code))
	value := hasher.Sum32() & 0x7FFFFFFF
	if value == 0 {
		value = 1
	}
	return uint(value)
}

// Ping 通过共享店铺鉴权接口验证凭据。
func (a *ACGAdapter) Ping(ctx context.Context) (*PingResult, error) {
	var out struct {
		ShopName string `json:"shopName"`
		Balance  any    `json:"balance"`
	}
	if err := a.post(ctx, "/shared/authentication/connect", map[string]interface{}{}, &out); err != nil {
		return nil, err
	}
	return &PingResult{
		SiteName:        out.ShopName,
		ProtocolVersion: "acg-shared",
		UserID:          1,
		Balance:         fmt.Sprintf("%v", out.Balance),
		Currency:        "CNY",
	}, nil
}

// ListCategories 从共享商品分组中提取分类。
func (a *ACGAdapter) ListCategories(ctx context.Context) (*CategoryListResult, error) {
	groups, err := a.listGroups(ctx)
	if err != nil {
		return nil, err
	}
	categories := make([]UpstreamCategory, 0, len(groups))
	for index, group := range groups {
		code := acgRawString(group.ID)
		if code == "" {
			code = strconv.Itoa(index + 1)
		}
		categories = append(categories, UpstreamCategory{
			ID:        acgIDForCode(code),
			Slug:      "acg-" + code,
			Name:      jsonmap.JSON{"zh-CN": strings.TrimSpace(group.Name)},
			SortOrder: index + 1,
		})
	}
	return &CategoryListResult{Supported: true, Categories: categories}, nil
}

// ListProducts 拉取共享商品目录。
// 异次元目录接口只返回商品标识与名称，价格与库存需要逐项询价，
// 因此这里对每个商品调用 valuation + stock 两个接口。
func (a *ACGAdapter) ListProducts(ctx context.Context, opts ListProductsOpts) (*ProductListResult, error) {
	groups, err := a.listGroups(ctx)
	if err != nil {
		return nil, err
	}

	type pending struct {
		child      acgChild
		productID  uint
		categoryID uint
	}
	items := make([]pending, 0, 128)
	for index, group := range groups {
		groupCode := acgRawString(group.ID)
		if groupCode == "" {
			groupCode = strconv.Itoa(index + 1)
		}
		categoryID := acgIDForCode(groupCode)
		for _, child := range group.Children {
			code := acgRawString(child.Code)
			if code == "" {
				code = acgRawString(child.ID)
			}
			if code == "" {
				continue
			}
			items = append(items, pending{child: child, productID: acgIDForCode(code), categoryID: categoryID})
		}
	}

	// 分页：异次元目录接口本身不分页，这里对拍平后的列表做内存分页。
	page := opts.Page
	if page < 1 {
		page = 1
	}
	pageSize := opts.PageSize
	if pageSize <= 0 {
		pageSize = 100
	}
	start := (page - 1) * pageSize
	end := start + pageSize
	if start > len(items) {
		start = len(items)
	}
	if end > len(items) {
		end = len(items)
	}
	pageItems := items[start:end]

	products := make([]UpstreamProduct, 0, len(pageItems))
	for _, item := range pageItems {
		code := acgRawString(item.child.Code)
		if code == "" {
			code = acgRawString(item.child.ID)
		}
		a.rememberCode(item.productID, code)

		price := item.child.priceValue()
		stock := item.child.stockValue()

		var images []string
		if cover := strings.TrimSpace(item.child.Cover); cover != "" {
			if !strings.HasPrefix(cover, "http") {
				cover = strings.TrimRight(a.baseURL, "/") + cover
			}
			images = []string{cover}
		}

		products = append(products, UpstreamProduct{
			ID:              item.productID,
			Code:            code,
			Slug:            "acg-" + code,
			Title:           jsonmap.JSON{"zh-CN": strings.TrimSpace(item.child.Name)},
			Description:     jsonmap.JSON{"zh-CN": item.child.Description},
			Images:          images,
			PriceAmount:     price,
			Currency:        "CNY",
			FulfillmentType: "auto",
			IsActive:        true,
			CategoryID:      item.categoryID,
			UpdatedAt:       time.Now(),
			SKUs: []UpstreamSKU{{
				ID:            item.productID,
				SKUCode:       code,
				PriceAmount:   price,
				StockStatus:   stockStatusFor(stock),
				StockQuantity: stock,
				IsActive:      true,
			}},
		})
	}

	total := len(items)
	includesInactive := false
	return &ProductListResult{Total: total, Items: products, IncludesInactive: includesInactive}, nil
}

// GetProduct 按框架内 ID 查询单个商品。
func (a *ACGAdapter) GetProduct(ctx context.Context, productID uint) (*UpstreamProduct, error) {
	code, ok := a.lookupCode(productID)
	if !ok {
		// 非数字 code 且缓存未命中：重新拉一次目录填充缓存。
		if _, err := a.ListProducts(ctx, ListProductsOpts{Page: 1, PageSize: 10000}); err != nil {
			return nil, err
		}
		code, ok = a.lookupCode(productID)
		if !ok {
			return nil, ErrUpstreamProductDeleted
		}
	}

	var out struct {
		Code        json.RawMessage `json:"code"`
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Cover       string          `json:"cover"`
		Stock       json.RawMessage `json:"stock"`
	}
	if err := a.post(ctx, "/shared/commodity/item", map[string]interface{}{"code": code}, &out); err != nil {
		var ue *upstreamHTTPError
		if errors.As(err, &ue) && ue.Status == http.StatusNotFound {
			return nil, ErrUpstreamProductDeleted
		}
		return nil, err
	}

	price, stock := a.quote(ctx, code)

	var images []string
	if cover := strings.TrimSpace(out.Cover); cover != "" {
		if !strings.HasPrefix(cover, "http") {
			cover = strings.TrimRight(a.baseURL, "/") + cover
		}
		images = []string{cover}
	}

	return &UpstreamProduct{
		ID:              productID,
		Code:            code,
		Slug:            "acg-" + code,
		Title:           jsonmap.JSON{"zh-CN": strings.TrimSpace(out.Name)},
		Description:     jsonmap.JSON{"zh-CN": out.Description},
		Images:          images,
		PriceAmount:     price,
		Currency:        "CNY",
		FulfillmentType: "auto",
		IsActive:        true,
		UpdatedAt:       time.Now(),
		SKUs: []UpstreamSKU{{
			ID:            productID,
			SKUCode:       code,
			PriceAmount:   price,
			StockStatus:   stockStatusFor(stock),
			StockQuantity: stock,
			IsActive:      true,
		}},
	}, nil
}

// CreateOrder 在异次元创建共享商品订单。
// req.SKUCode 即商品的共享编号（shared_code）。
func (a *ACGAdapter) CreateOrder(ctx context.Context, req CreateUpstreamOrderReq) (*CreateUpstreamOrderResp, error) {
	sharedCode := strings.TrimSpace(req.SKUCode)
	if sharedCode == "" {
		sharedCode = strconv.FormatUint(uint64(req.SKUID), 10)
	}
	contact := "buyer@invalid.local"
	if value, ok := req.ManualFormData["contact"]; ok {
		if text := strings.TrimSpace(fmt.Sprintf("%v", value)); text != "" {
			contact = text
		}
	}

	var out struct {
		TradeNo json.RawMessage `json:"tradeNo"`
		Amount  any             `json:"amount"`
		Secret  string          `json:"secret"`
	}
	err := a.post(ctx, "/shared/commodity/trade", map[string]interface{}{
		"request_no":  req.DownstreamOrderNo,
		"shared_code": sharedCode,
		"num":         strconv.Itoa(req.Quantity),
		"card_id":     "0",
		"device":      "0",
		"password":    "",
		"contact":     contact,
	}, &out)
	if err != nil {
		var ue *upstreamHTTPError
		if errors.As(err, &ue) {
			return &CreateUpstreamOrderResp{OK: false, ErrorCode: ue.Code, ErrorMessage: ue.Message}, nil
		}
		return nil, err
	}

	tradeNo := acgRawString(out.TradeNo)
	resp := &CreateUpstreamOrderResp{
		OK:       true,
		OrderNo:  tradeNo,
		Amount:   fmt.Sprintf("%v", out.Amount),
		Currency: "CNY",
	}
	// 异次元下单即可能直接返回卡密；此时视为已交付。
	if strings.TrimSpace(out.Secret) != "" {
		resp.Status = "delivered"
	}
	return resp, nil
}

// GetOrder 查询异次元订单。框架的 uint 接口无法承载 tradeNo，
// 因此 ACG 的查单走 GetOrderByNo 可选接口。
func (a *ACGAdapter) GetOrder(ctx context.Context, orderID uint) (*UpstreamOrderDetail, error) {
	return nil, fmt.Errorf("acg: order lookup requires order number, use GetOrderByNo")
}

// GetOrderByNo 按异次元订单号查询。
func (a *ACGAdapter) GetOrderByNo(ctx context.Context, orderNo string) (*UpstreamOrderDetail, error) {
	var out struct {
		Secret string `json:"secret"`
		Status *int   `json:"status"`
	}
	if err := a.post(ctx, "/shared/commodity/query", map[string]interface{}{"tradeNo": orderNo}, &out); err != nil {
		var ue *upstreamHTTPError
		if errors.As(err, &ue) && ue.Status == http.StatusNotFound {
			return nil, fmt.Errorf("acg order %s not found", orderNo)
		}
		return nil, err
	}

	detail := &UpstreamOrderDetail{OrderNo: orderNo, Status: "pending", Currency: "CNY"}
	if strings.TrimSpace(out.Secret) != "" {
		now := time.Now()
		detail.Status = "delivered"
		detail.Fulfillment = &UpstreamFulfillment{
			Type:        "auto",
			Status:      "delivered",
			Payload:     out.Secret,
			DeliveredAt: &now,
		}
	}
	return detail, nil
}

// CancelOrder 异次元共享接口没有取消能力。
func (a *ACGAdapter) CancelOrder(ctx context.Context, orderID uint) error {
	return fmt.Errorf("%w: acg shared api has no cancel endpoint", ErrUpstreamCancelNotAllowed)
}

// DownloadImage 异次元共享接口不返回商品图片，保留原始 URL。
func (a *ACGAdapter) DownloadImage(ctx context.Context, imageURL string) (string, error) {
	return "", fmt.Errorf("acg: image download not supported")
}

func (a *ACGAdapter) rememberCode(id uint, code string) {
	if id == 0 || code == "" {
		return
	}
	a.mu.Lock()
	a.codeCache[id] = code
	a.mu.Unlock()
}

func (a *ACGAdapter) lookupCode(id uint) (string, bool) {
	a.mu.RLock()
	code, ok := a.codeCache[id]
	a.mu.RUnlock()
	return code, ok
}

func (a *ACGAdapter) listGroups(ctx context.Context) ([]acgGroup, error) {
	var groups []acgGroup
	if err := a.post(ctx, "/shared/commodity/items", map[string]interface{}{}, &groups); err != nil {
		return nil, err
	}
	return groups, nil
}

// quote 返回商品的实付价与库存；失败时返回 0 价与 0 库存，由调用方按不可售处理。
//
// 真实响应：valuation 返回 {"price":"0.40","currency_code":"CNY"}，
// stock 返回 {"stock":"1848"}，两者都是字符串。
func (a *ACGAdapter) quote(ctx context.Context, code string) (string, int) {
	var valuation struct {
		Price    json.RawMessage `json:"price"`
		Currency string          `json:"currency_code"`
	}
	// race 为空时不参与签名，因此直接不传。
	if err := a.post(ctx, "/shared/commodity/valuation", map[string]interface{}{
		"code": code, "num": "1", "card_id": "0",
	}, &valuation); err != nil {
		return "0.00", 0
	}

	var stockOut struct {
		Stock json.RawMessage `json:"stock"`
	}
	stock := 0
	if err := a.post(ctx, "/shared/commodity/stock", map[string]interface{}{"code": code}, &stockOut); err == nil {
		if parsed := acgStockValue(stockOut.Stock); parsed != -1 {
			stock = parsed
		}
	}

	price := acgRawString(valuation.Price)
	if price == "" {
		price = "0.00"
	}
	return price, stock
}

func stockStatusFor(stock int) string {
	if stock == -1 {
		return "in_stock"
	}
	if stock <= 0 {
		return "out_of_stock"
	}
	return "in_stock"
}

// post 发送签名表单请求并解包 {code,msg,data}。
func (a *ACGAdapter) post(ctx context.Context, path string, data map[string]interface{}, result interface{}) error {
	payload := make(map[string]interface{}, len(data)+2)
	for key, value := range data {
		payload[key] = value
	}
	payload["app_id"] = a.appID
	payload["sign"] = acgSign(payload, a.appKey)

	form := url.Values{}
	for _, pair := range acgFormPairs(payload) {
		form.Add(pair[0], pair[1])
	}
	// sign 也在 acgFormPairs 之外单独补上（它被跳过，因为不参与签名）。
	form.Set("sign", payload["sign"].(string))

	targetURL := a.baseURL + path

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("acg create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := a.client.Do(req)
	if err != nil {
		logger.Warnw("acg_request_error", "url", targetURL, "error", err.Error())
		return fmt.Errorf("acg send request to %s: %w", targetURL, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("acg read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		logger.Warnw("acg_request_error",
			"url", targetURL, "status", resp.StatusCode,
			"content_type", resp.Header.Get("Content-Type"),
			"body", truncateText(string(body), 300))
		return &upstreamHTTPError{
			Status:  resp.StatusCode,
			Code:    fmt.Sprintf("acg_http_%d", resp.StatusCode),
			Message: truncateText(fmt.Sprintf("%s returned HTTP %d: %s", targetURL, resp.StatusCode, string(body)), 300),
			Body:    string(body),
		}
	}

	var reply acgReply
	if err := json.Unmarshal(body, &reply); err != nil {
		logger.Warnw("acg_response_not_json",
			"url", targetURL, "content_type", resp.Header.Get("Content-Type"),
			"body", truncateText(string(body), 300))
		return fmt.Errorf("acg unmarshal response from %s: %w", targetURL, err)
	}
	if reply.Code != http.StatusOK {
		return &upstreamHTTPError{
			Status:  reply.Code,
			Code:    fmt.Sprintf("acg_rejected_%d", reply.Code),
			Message: truncateText(reply.Msg, 200),
			Body:    string(body),
		}
	}

	if result != nil && len(reply.Data) > 0 && string(reply.Data) != "null" {
		if err := json.Unmarshal(reply.Data, result); err != nil {
			return fmt.Errorf("acg unmarshal data: %w", err)
		}
	}
	return nil
}

// truncateText 按字符边界截断，避免把多字节 UTF-8 字符截成半个。
// 截断后的内容会写入 PostgreSQL，非法 UTF-8 会导致整个更新失败（SQLSTATE 22021）。
func truncateText(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	// 从 limit 处向前回退，直到落在完整的 UTF-8 字符起点上。
	cut := limit
	for cut > 0 && cut < len(value) && (value[cut]&0xC0) == 0x80 {
		cut--
	}
	return value[:cut]
}
