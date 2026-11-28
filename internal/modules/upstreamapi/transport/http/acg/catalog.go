package acg

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/dujiao-next/internal/constants"
	"github.com/dujiao-next/internal/logger"
	domaincatalog "github.com/dujiao-next/internal/modules/catalog"
	productdomain "github.com/dujiao-next/internal/modules/catalog/product/domain"
	"github.com/dujiao-next/internal/shared/money"

	"github.com/gin-gonic/gin"
)

// ── 连接 ───────────────────────────────────────────────────

// Connect POST /shared/authentication/connect
func (h *Handler) Connect(c *gin.Context) {
	credential, _, ok := h.authenticate(c)
	if !ok {
		return
	}
	balance := 0.0
	if account, err := h.base.Wallet.GetAccount(credential.UserID); err == nil && account != nil {
		if parsed, parseErr := strconv.ParseFloat(account.Balance.StringFixed(2), 64); parseErr == nil {
			balance = parsed
		}
	}
	respondOK(c, gin.H{"shopName": h.shopName(), "balance": balance})
}

// ── 目录 ───────────────────────────────────────────────────

// Items POST /shared/commodity/items —— 分类树，children 为商品。
func (h *Handler) Items(c *gin.Context) {
	credential, _, ok := h.authenticate(c)
	if !ok {
		return
	}

	products, err := h.allProducts()
	if err != nil {
		logger.Errorw("acg_supply_list_products_failed", "error", err)
		respondError(c, "商品读取失败")
		return
	}

	byCategory := make(map[uint][]gin.H, len(products))
	for i := range products {
		product := &products[i]
		// 只暴露本地自动交付商品：acg 的 trade 必须同步返回卡密，而上游对接商品
		// 走采购队列异步交付（CreateAuto 只接受 auto 项），暴露出去只会让下游
		// 拿到可重试错误无限重试，最终转人工。
		if product.FulfillmentType != constants.FulfillmentTypeAuto {
			continue
		}
		options := skuOptions(product)
		if len(options) == 0 {
			continue
		}
		byCategory[product.CategoryID] = append(byCategory[product.CategoryID], h.productRow(credential.UserID, product, options))
	}

	categories, err := h.base.Categories.List()
	if err != nil {
		logger.Errorw("acg_supply_list_categories_failed", "error", err)
		respondError(c, "分类读取失败")
		return
	}

	tree := make([]gin.H, 0, len(categories)+1)
	if uncategorized := byCategory[0]; len(uncategorized) > 0 {
		tree = append(tree, gin.H{"id": 0, "name": "默认分类", "children": uncategorized})
	}
	for _, category := range categories {
		children := byCategory[category.ID]
		if children == nil {
			children = []gin.H{}
		}
		tree = append(tree, gin.H{
			"id":       category.ID,
			"name":     localizedText(category.NameJSON),
			"children": children,
		})
	}
	respondOK(c, tree)
}

// allProducts 全量分页拉取（含下架，由 status 字段表达）并补齐库存计数。
func (h *Handler) allProducts() ([]productdomain.Product, error) {
	const pageSize = 200
	collected := make([]productdomain.Product, 0, pageSize)
	for page := 1; ; page++ {
		items, total, err := h.base.Products.ListForUpstreamSync(nil, true, page, pageSize)
		if err != nil {
			return nil, err
		}
		collected = append(collected, items...)
		if len(items) == 0 || len(collected) >= int(total) {
			break
		}
	}
	h.hydrateStock(collected)
	return collected, nil
}

// Item POST /shared/commodity/item（code）
func (h *Handler) Item(c *gin.Context) {
	credential, form, ok := h.authenticate(c)
	if !ok {
		return
	}
	product, options, found := h.loadProduct(form["code"])
	if !found {
		respondError(c, msgUnavailable)
		return
	}
	respondOK(c, h.productRow(credential.UserID, product, options))
}

// Stock POST /shared/commodity/stock（code）
func (h *Handler) Stock(c *gin.Context) {
	_, form, ok := h.authenticate(c)
	if !ok {
		return
	}
	product, options, found := h.loadProduct(form["code"])
	if !found {
		respondError(c, msgUnavailable)
		return
	}
	sku, ok := selectSKU(options, form["race"])
	if !ok {
		respondError(c, msgRaceInvalid)
		return
	}
	_, stock := skuStock(*product, *sku)
	respondOK(c, gin.H{"stock": stock})
}

// Valuation POST /shared/commodity/valuation（code,num）
func (h *Handler) Valuation(c *gin.Context) {
	credential, form, ok := h.authenticate(c)
	if !ok {
		return
	}
	product, options, found := h.loadProduct(form["code"])
	if !found {
		respondError(c, msgUnavailable)
		return
	}
	sku, ok := selectSKU(options, form["race"])
	if !ok {
		respondError(c, msgRaceInvalid)
		return
	}
	quantity := atoiOr(form["num"], 1)
	if quantity < 1 {
		quantity = 1
	}
	price := h.effectivePrice(credential.UserID, product.ID, sku.ID, sku.PriceAmount)
	respondOK(c, gin.H{"price": yuanFloat(price) * float64(quantity)})
}

// Inventory POST /shared/commodity/inventory（sharedCode）—— 旧端点名。
func (h *Handler) Inventory(c *gin.Context) {
	credential, form, ok := h.authenticate(c)
	if !ok {
		return
	}
	product, options, found := h.loadProduct(form["sharedCode"])
	if !found {
		respondError(c, msgUnavailable)
		return
	}
	sku, ok := selectSKU(options, form["race"])
	if !ok {
		respondError(c, msgRaceInvalid)
		return
	}
	_, stock := skuStock(*product, *sku)
	price := h.effectivePrice(credential.UserID, product.ID, sku.ID, sku.PriceAmount).StringFixed(2)
	respondOK(c, gin.H{
		"count":         stock,
		"price":         price,
		"user_price":    price,
		"factory_price": price,
		"delivery_way":  0,
		"draft_status":  0,
		"config":        "",
	})
}

// InventoryState POST /shared/commodity/inventoryState（shared_code,num）—— 旧端点名。
func (h *Handler) InventoryState(c *gin.Context) {
	_, form, ok := h.authenticate(c)
	if !ok {
		return
	}
	product, options, found := h.loadProduct(form["shared_code"])
	quantity := atoiOr(form["num"], 0)
	if !found || quantity < 1 {
		respondError(c, "商品或购买数量非法")
		return
	}
	sku, ok := selectSKU(options, form["race"])
	if !ok {
		respondError(c, msgRaceInvalid)
		return
	}
	_, stock := skuStock(*product, *sku)
	if stock < quantity {
		respondError(c, msgNoStock)
		return
	}
	respondOK(c, gin.H{"state": true})
}

// ── 商品解析 ───────────────────────────────────────────────

// loadProduct code（商品 ID）→ 商品 + 全部在售规格。
// 只接受本地自动交付商品：acg 的 trade 必须同步返回卡密，上游对接商品走采购队列。
func (h *Handler) loadProduct(code string) (*productdomain.Product, []skuOption, bool) {
	productID, err := strconv.ParseUint(strings.TrimSpace(code), 10, 64)
	if err != nil || productID == 0 {
		return nil, nil, false
	}
	product, err := h.base.Products.GetAdminByID(strconv.FormatUint(productID, 10))
	if err != nil || product == nil || !product.IsActive {
		return nil, nil, false
	}

	products := []productdomain.Product{*product}
	h.hydrateStock(products)
	product = &products[0]

	// 与目录同口径：只接受本地自动交付商品（见 Items 中的说明）。
	if product.FulfillmentType != constants.FulfillmentTypeAuto {
		return nil, nil, false
	}
	options := skuOptions(product)
	if len(options) == 0 {
		return nil, nil, false
	}
	return product, options, true
}

// hydrateStock 填充自动发货库存计数，供 skuStock 使用。
// 本层只接受本地自动交付商品（FulfillmentTypeAuto），上游对接与人工发货商品
// 已在目录与单品解析阶段排除，因此无需 SKU 映射数据。
func (h *Handler) hydrateStock(products []productdomain.Product) {
	if err := h.base.Products.ApplyAutoStockCounts(products); err != nil {
		logger.Warnw("acg_supply_apply_stock_counts_failed", "error", err)
	}
}

// skuOption 一个可售规格，对应 acg 的「种类」。
type skuOption struct {
	label string
	sku   *productdomain.ProductSKU
}

// skuOptions 返回商品的在售 SKU（按 ID 升序），并给每个规格算一个稳定且唯一的种类名。
// 顺序稳定很重要：下游会把种类名存下来，下次下单原样传回来。
func skuOptions(p *productdomain.Product) []skuOption {
	active := make([]*productdomain.ProductSKU, 0, len(p.SKUs))
	for i := range p.SKUs {
		if p.SKUs[i].IsActive {
			active = append(active, &p.SKUs[i])
		}
	}
	sort.Slice(active, func(i, j int) bool { return active[i].ID < active[j].ID })

	options := make([]skuOption, 0, len(active))
	used := make(map[string]int, len(active))
	for _, sku := range active {
		base := raceLabel(sku)
		label := base
		for used[label] > 0 {
			used[base]++
			label = base + " (" + strconv.Itoa(used[base]) + ")"
		}
		used[label] = 1
		options = append(options, skuOption{label: label, sku: sku})
	}
	return options
}

// raceLabel SKU → acg 种类名：优先规格值组合（「黑色 / 月付」，按键名排序保证稳定），
// 没有规格值时退到 SKU 编码。换行和 '=' 会破坏下游的 INI 解析，统一处理掉。
func raceLabel(sku *productdomain.ProductSKU) string {
	keys := make([]string, 0, len(sku.SpecValuesJSON))
	for key := range sku.SpecValuesJSON {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		if text := specValueText(sku.SpecValuesJSON[key]); text != "" {
			parts = append(parts, text)
		}
	}
	label := strings.Join(parts, " / ")
	if label == "" {
		label = strings.TrimSpace(sku.SKUCode)
	}
	if label == "" {
		label = "SKU" + strconv.FormatUint(uint64(sku.ID), 10)
	}
	label = strings.NewReplacer("\r", " ", "\n", " ", "=", "-").Replace(label)
	return strings.TrimSpace(label)
}

// specValueText 规格值统一成文本，用于拼种类名。
func specValueText(value interface{}) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(typed)
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
}

// selectSKU 按 acg 的 race 选出规格。单规格商品忽略 race（旧下游不会传）；
// 多规格商品必须命中，否则返回 false 让调用方报错——绝不默认取第一个，那是卖错货。
func selectSKU(options []skuOption, race string) (*productdomain.ProductSKU, bool) {
	if len(options) == 0 {
		return nil, false
	}
	if len(options) == 1 {
		return options[0].sku, true
	}
	race = strings.TrimSpace(race)
	if race == "" {
		return nil, false
	}
	for _, option := range options {
		if option.label == race {
			return option.sku, true
		}
	}
	// 兼容下游按 SKU 编码传 race 的情况
	for _, option := range options {
		if strings.EqualFold(strings.TrimSpace(option.sku.SKUCode), race) {
			return option.sku, true
		}
	}
	return nil, false
}

// skuStock 计算 SKU 可用库存。
// 与 upstreamhttp.computeSKUStock 同口径；两处需保持一致（受该目录文件数预算约束，
// 无法共用未导出的 helper）。
func skuStock(p productdomain.Product, s productdomain.ProductSKU) (string, int) {
	var available int64
	switch p.FulfillmentType {
	case constants.FulfillmentTypeManual:
		available = int64(s.ManualStockTotal)
	case constants.FulfillmentTypeUpstream:
		available = int64(s.UpstreamStock)
	default:
		available = s.AutoStockAvailable
	}
	return domaincatalog.UpstreamStockPolicy().Status(available), int(available)
}

// effectivePrice 下游专属价 / 会员价优先，未命中回落到 SKU 基础价。
func (h *Handler) effectivePrice(userID, productID, skuID uint, base money.Amount) money.Amount {
	if h.base.MemberLevels == nil || userID == 0 {
		return base
	}
	var memberLevelID uint
	if user, err := h.base.Users.GetByID(userID); err == nil && user != nil {
		memberLevelID = user.MemberLevelID
	}
	priceBook, err := h.base.MemberLevels.LoadUserPriceBook(userID)
	if err != nil {
		return base
	}
	if price, hit, _ := h.base.MemberLevels.ResolveCatalogPrice(priceBook, memberLevelID, productID, skuID, base.Decimal); hit {
		return money.FromDecimal(price)
	}
	return base
}

// productRow 商品行（字段全集对齐 acg 3.5.7+ 客户端 sync() 读取）。
func (h *Handler) productRow(userID uint, p *productdomain.Product, options []skuOption) gin.H {
	primary := options[0].sku
	price := h.effectivePrice(userID, p.ID, primary.ID, primary.PriceAmount)
	totalStock := 0
	for _, option := range options {
		if _, stock := skuStock(*p, *option.sku); stock > 0 {
			totalStock += stock
		}
	}
	config := ""
	if len(options) > 1 {
		config = h.raceConfigINI(userID, p, options)
	}

	minimum := p.MinPurchaseQuantity
	if minimum < 1 {
		minimum = 1
	}
	maximum := p.MaxPurchaseQuantity
	if maximum < 1 {
		maximum = 99
	}

	return gin.H{
		"code":          strconv.FormatUint(uint64(p.ID), 10),
		"name":          localizedText(p.TitleJSON),
		"price":         price.StringFixed(2),
		"user_price":    price.StringFixed(2),
		"factory_price": primary.CostPriceAmount.StringFixed(2),
		// acg 只有一个 description 列，语义是「商品详情」；本站与之对应的是 ContentJSON。
		// 发简介过去会让下游把简介当成详情展示（反向导入同理）。
		"description":        localizedText(p.ContentJSON),
		"introduce":          localizedText(p.ContentJSON),
		"cover":              coverImage(p),
		"status":             boolInt(p.IsActive),
		"delivery_way":       1, // 自动发货
		"draft_status":       0,
		"draft_premium":      "0",
		"minimum":            minimum,
		"maximum":            maximum,
		"stock":              totalStock,
		"config":             config,
		"widget":             nil,
		"category_id":        p.CategoryID,
		"seckill_status":     0,
		"seckill_start_time": nil,
		"seckill_end_time":   nil,
		"contact_type":       0, // 任意：避免下游把 null 写进 NOT NULL 列
	}
}

// raceConfigINI 多规格商品的出站配置：[category] 是种类名 → 售价，
// 下游靠它渲染种类选择器；[category_factory] 是给下游算拿货价的成本快照。
// 单规格商品不发 config，保持原有行为。
func (h *Handler) raceConfigINI(userID uint, p *productdomain.Product, options []skuOption) string {
	var builder strings.Builder
	builder.WriteString("[category]\n")
	for _, option := range options {
		builder.WriteString(option.label)
		builder.WriteString("=")
		builder.WriteString(h.effectivePrice(userID, p.ID, option.sku.ID, option.sku.PriceAmount).StringFixed(2))
		builder.WriteString("\n")
	}
	builder.WriteString("[category_factory]\n")
	for _, option := range options {
		builder.WriteString(option.label)
		builder.WriteString("=")
		builder.WriteString(option.sku.CostPriceAmount.StringFixed(2))
		builder.WriteString("\n")
	}
	return builder.String()
}

func coverImage(p *productdomain.Product) string {
	if len(p.Images) > 0 {
		return p.Images[0]
	}
	return ""
}

// yuanFloat 金额 → 元（数值，acg connect/valuation/trade 口径）。
func yuanFloat(amount money.Amount) float64 {
	value, err := strconv.ParseFloat(amount.StringFixed(2), 64)
	if err != nil {
		return 0
	}
	return value
}
