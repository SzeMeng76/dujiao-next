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
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
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
	acgFakaItem    = "commodity/item"
	acgFakaTrade   = "commodity/trade"
	acgFakaQuery   = "commodity/query"
)

// acgBizError acg 用 HTTP 200 + {code:0,msg} 表达业务失败。
type acgBizError struct{ Message string }

func (e *acgBizError) Error() string { return e.Message }

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

// postForm 发送带签名表单请求，成功时把 data 解到 out。
// 被限流（429）时退避重试——429 表示上游没有处理，重发安全。
func (a *AcgFakaAdapter) postForm(ctx context.Context, endpoint string, params map[string]string, out interface{}) error {
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

// acgProductRow acg 商品行。acg 没有多规格维度，一个商品就是一个 SKU。
type acgProductRow struct {
	Code         acgNumber `json:"code"`
	Name         string    `json:"name"`
	Price        acgNumber `json:"price"`
	FactoryPrice acgNumber `json:"factory_price"`
	Description  string    `json:"description"`
	Introduce    string    `json:"introduce"`
	Cover        string    `json:"cover"`
	Status       int       `json:"status"`
	Stock        int       `json:"stock"`
	CategoryID   acgNumber `json:"category_id"`
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
	tree, err := a.fetchTree(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]UpstreamProduct, 0)
	for _, node := range tree {
		categoryID := acgUint(node.ID)
		for _, row := range node.Children {
			product, ok := acgToProduct(row, categoryID)
			if !ok {
				continue
			}
			items = append(items, product)
		}
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
	var row acgProductRow
	if err := a.postForm(ctx, acgFakaItem, map[string]string{
		"code": strconv.FormatUint(uint64(productID), 10),
	}, &row); err != nil {
		var biz *acgBizError
		if errors.As(err, &biz) {
			// acg 不区分「已删除」与「已下架」，按可恢复的下架处理更安全。
			return nil, ErrUpstreamProductUnavailable
		}
		return nil, err
	}
	// 单品接口带 category_id；同步流程会用它更新映射上的上游分类，
	// 返回 0 会把已经存好的分类（以及基于分类的加价）覆盖掉。
	product, ok := acgToProduct(row, acgUint(row.CategoryID))
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
// acg 一个商品只有一条隐式 SKU，因此 SKU ID 取商品 code。
func acgToProduct(row acgProductRow, categoryID uint) (UpstreamProduct, bool) {
	id := acgUint(row.Code)
	if id == 0 {
		return UpstreamProduct{}, false
	}
	price := acgAmountString(row.Price)
	if price == "" {
		price = "0.00"
	}
	stockStatus := constants.ProductStockStatusOutOfStock
	switch {
	case row.Stock < 0:
		stockStatus = constants.ProductStockStatusUnlimited
	case row.Stock > 0:
		stockStatus = constants.ProductStockStatusInStock
	}

	description := jsonmap.JSON{}
	if text := strings.TrimSpace(row.Description); text != "" {
		description = acgLocalized(text)
	}

	return UpstreamProduct{
		ID:              id,
		Title:           acgLocalized(row.Name),
		Description:     description,
		Content:         jsonmap.JSON{},
		Images:          acgImages(row.Cover),
		PriceAmount:     price,
		Currency:        constants.SiteCurrencyDefault,
		FulfillmentType: constants.FulfillmentTypeAuto,
		IsActive:        row.Status == 1,
		CategoryID:      categoryID,
		SKUs: []UpstreamSKU{{
			ID:            id,
			SKUCode:       strconv.FormatUint(uint64(id), 10),
			PriceAmount:   price,
			StockStatus:   stockStatus,
			StockQuantity: row.Stock,
			IsActive:      row.Status == 1,
		}},
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

	var data struct {
		Amount  acgNumber `json:"amount"`
		TradeNo acgNumber `json:"tradeNo"`
		Secret  string    `json:"secret"`
	}
	err := a.postForm(ctx, acgFakaTrade, map[string]string{
		"shared_code": strconv.FormatUint(uint64(req.SKUID), 10),
		"num":         strconv.Itoa(quantity),
		"request_no":  req.DownstreamOrderNo,
	}, &data)
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
	default:
		return "acg_upstream_rejected"
	}
}
