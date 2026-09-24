package format

import (
	"fmt"
	"strings"

	productdomain "github.com/dujiao-next/internal/modules/catalog/product/domain"
	orderdomain "github.com/dujiao-next/internal/modules/order/domain"

	"github.com/dujiao-next/internal/constants"
	"github.com/dujiao-next/internal/shared/jsonmap"
)

type OrderItemCounts struct {
	Total    int
	Auto     int
	Manual   int
	Upstream int
}

// StockInfoProvider 提供库存信息查询接口
type StockInfoProvider interface {
	GetProductByID(id string) (*productdomain.Product, error)
	GetSKUByID(id uint) (*productdomain.ProductSKU, error)
}

func BuildOrderItemSummaries(items []orderdomain.OrderItem, locale string) (string, string, OrderItemCounts) {
	return BuildOrderItemSummariesWithStock(items, locale, nil)
}

// BuildOrderItemSummariesWithStock 构建商品明细，可选择性包含库存信息
func BuildOrderItemSummariesWithStock(items []orderdomain.OrderItem, locale string, stockProvider StockInfoProvider) (string, string, OrderItemCounts) {
	counts := OrderItemCounts{Total: len(items)}
	if len(items) == 0 {
		empty := localizedNotificationText(locale, "暂无商品明细", "暫無商品明細", "No item details")
		return empty, empty, counts
	}

	allLines := make([]string, 0, len(items))
	fulfillmentLines := make([]string, 0, len(items))
	for idx, item := range items {
		line := buildNotificationOrderItemLine(idx, item, locale, stockProvider)
		allLines = append(allLines, line)

		switch NormalizeFulfillmentType(item.FulfillmentType) {
		case constants.FulfillmentTypeAuto:
			counts.Auto++
		case constants.FulfillmentTypeUpstream:
			counts.Upstream++
			fulfillmentLines = append(fulfillmentLines, line)
		default:
			counts.Manual++
			fulfillmentLines = append(fulfillmentLines, line)
		}
	}

	if len(fulfillmentLines) == 0 {
		fulfillmentLines = append(fulfillmentLines, localizedNotificationText(locale, "无需人工跟进", "無需人工跟進", "No manual follow-up required"))
	}
	return strings.Join(allLines, "\n"), strings.Join(fulfillmentLines, "\n"), counts
}

func buildNotificationOrderItemLine(index int, item orderdomain.OrderItem, locale string, stockProvider StockInfoProvider) string {
	title := resolveNotificationLocalizedJSON(item.TitleJSON, locale, constants.LocaleZhCN)
	if title == "" {
		title = localizedNotificationText(locale, "未命名商品", "未命名商品", "Unnamed item")
	}

	skuText := buildNotificationSKUSummary(item.SKUSnapshotJSON, locale)
	fulfillmentLabel := notificationFulfillmentLabel(locale, item.FulfillmentType)
	line := fmt.Sprintf("%d. %s", index+1, title)
	if skuText != "" {
		line += " / " + skuText
	}
	line += fmt.Sprintf(" x%d", item.Quantity)
	if fulfillmentLabel != "" {
		line += " [" + fulfillmentLabel + "]"
	}

	return line
}

// buildStockInfo 构建库存信息，仅在非无限库存时返回
func buildStockInfo(item orderdomain.OrderItem, locale string, stockProvider StockInfoProvider) string {
	fulfillmentType := NormalizeFulfillmentType(item.FulfillmentType)

	// 根据交付类型获取库存
	var stockValue int64
	var hasStock bool

	switch fulfillmentType {
	case constants.FulfillmentTypeAuto:
		// 自动交付：查询 SKU 的自动库存
		if item.SKUID > 0 {
			if sku, err := stockProvider.GetSKUByID(item.SKUID); err == nil && sku != nil {
				stockValue = sku.AutoStockAvailable
				hasStock = true
			}
		} else if item.ProductID > 0 {
			// 无 SKU，查询商品的自动库存
			if product, err := stockProvider.GetProductByID(fmt.Sprintf("%d", item.ProductID)); err == nil && product != nil {
				stockValue = product.AutoStockAvailable
				hasStock = true
			}
		}

	case constants.FulfillmentTypeManual:
		// 人工交付：查询手动库存
		if item.SKUID > 0 {
			if sku, err := stockProvider.GetSKUByID(item.SKUID); err == nil && sku != nil {
				// -1 表示无限库存，不显示
				if sku.ManualStockTotal >= 0 {
					stockValue = int64(sku.ManualStockTotal)
					hasStock = true
				}
			}
		} else if item.ProductID > 0 {
			if product, err := stockProvider.GetProductByID(fmt.Sprintf("%d", item.ProductID)); err == nil && product != nil {
				// -1 表示无限库存，不显示
				if product.ManualStockTotal >= 0 {
					stockValue = int64(product.ManualStockTotal)
					hasStock = true
				}
			}
		}

	case constants.FulfillmentTypeUpstream:
		// 上游交付：查询上游库存
		if item.SKUID > 0 {
			if sku, err := stockProvider.GetSKUByID(item.SKUID); err == nil && sku != nil {
				// -1 表示无限库存，不显示；0 表示售罄；>0 表示有货
				if sku.UpstreamStock >= 0 {
					stockValue = int64(sku.UpstreamStock)
					hasStock = true
				}
			}
		}
	}

	if !hasStock {
		return ""
	}

	// 格式化库存信息
	stockLabel := localizedNotificationText(locale, "库存", "庫存", "Stock")
	stockText := fmt.Sprintf("[%s:%d]", stockLabel, stockValue)

	// 如果库存为0，添加补货提醒
	if stockValue == 0 {
		alertLabel := localizedNotificationText(locale, "⚠️需补货", "⚠️需補貨", "⚠️Need Restock")
		stockText = fmt.Sprintf("[%s:%d %s]", stockLabel, stockValue, alertLabel)
	}

	return stockText
}

// StockAlert 单个商品的库存预警信息
type StockAlert struct {
	ProductTitle string
	SKUText      string
	StockValue   int64
	NeedsRestock bool
}

// BuildStockSummary 构建库存摘要，只显示需要关注的库存状态（低库存或售罄）
func BuildStockSummary(items []orderdomain.OrderItem, locale string, stockProvider StockInfoProvider, lowStockThreshold int64) string {
	if stockProvider == nil || len(items) == 0 {
		return ""
	}

	if lowStockThreshold <= 0 {
		lowStockThreshold = 5 // 默认阈值：库存 <= 5 时预警
	}

	alerts := make([]StockAlert, 0)

	for _, item := range items {
		fulfillmentType := NormalizeFulfillmentType(item.FulfillmentType)
		var stockValue int64
		var hasStock bool

		// 查询库存
		switch fulfillmentType {
		case constants.FulfillmentTypeAuto:
			if item.SKUID > 0 {
				if sku, err := stockProvider.GetSKUByID(item.SKUID); err == nil && sku != nil {
					stockValue = sku.AutoStockAvailable
					hasStock = true
				}
			} else if item.ProductID > 0 {
				if product, err := stockProvider.GetProductByID(fmt.Sprintf("%d", item.ProductID)); err == nil && product != nil {
					stockValue = product.AutoStockAvailable
					hasStock = true
				}
			}

		case constants.FulfillmentTypeManual:
			if item.SKUID > 0 {
				if sku, err := stockProvider.GetSKUByID(item.SKUID); err == nil && sku != nil && sku.ManualStockTotal >= 0 {
					stockValue = int64(sku.ManualStockTotal)
					hasStock = true
				}
			} else if item.ProductID > 0 {
				if product, err := stockProvider.GetProductByID(fmt.Sprintf("%d", item.ProductID)); err == nil && product != nil && product.ManualStockTotal >= 0 {
					stockValue = int64(product.ManualStockTotal)
					hasStock = true
				}
			}

		case constants.FulfillmentTypeUpstream:
			if item.SKUID > 0 {
				if sku, err := stockProvider.GetSKUByID(item.SKUID); err == nil && sku != nil && sku.UpstreamStock >= 0 {
					stockValue = int64(sku.UpstreamStock)
					hasStock = true
				}
			}
		}

		// 只记录需要关注的库存（低库存或售罄）
		if hasStock && stockValue <= lowStockThreshold {
			title := resolveNotificationLocalizedJSON(item.TitleJSON, locale, constants.LocaleZhCN)
			if title == "" {
				title = localizedNotificationText(locale, "未命名商品", "未命名商品", "Unnamed item")
			}

			skuText := buildNotificationSKUSummary(item.SKUSnapshotJSON, locale)

			alerts = append(alerts, StockAlert{
				ProductTitle: title,
				SKUText:      skuText,
				StockValue:   stockValue,
				NeedsRestock: stockValue == 0,
			})
		}
	}

	if len(alerts) == 0 {
		return localizedNotificationText(locale, "✅ 库存充足", "✅ 庫存充足", "✅ Stock sufficient")
	}

	// 构建库存摘要
	lines := make([]string, 0, len(alerts))
	for _, alert := range alerts {
		line := alert.ProductTitle
		if alert.SKUText != "" {
			line += " / " + alert.SKUText
		}

		if alert.NeedsRestock {
			statusText := localizedNotificationText(locale, "库存", "庫存", "Stock")
			alertText := localizedNotificationText(locale, "⚠️ 需补货", "⚠️ 需補貨", "⚠️ Need restock")
			line += fmt.Sprintf(" - %s: %d %s", statusText, alert.StockValue, alertText)
		} else {
			statusText := localizedNotificationText(locale, "库存", "庫存", "Stock")
			warnText := localizedNotificationText(locale, "⚡ 库存偏低", "⚡ 庫存偏低", "⚡ Low stock")
			line += fmt.Sprintf(" - %s: %d %s", statusText, alert.StockValue, warnText)
		}

		lines = append(lines, line)
	}

	return strings.Join(lines, "\n")
}

func buildNotificationSKUSummary(snapshot jsonmap.JSON, locale string) string {
	if len(snapshot) == 0 {
		return ""
	}
	specText := notificationInterfaceText(snapshot["spec_values"], locale, constants.LocaleZhCN)
	if specText != "" {
		return specText
	}
	code := strings.TrimSpace(fmt.Sprintf("%v", snapshot["sku_code"]))
	if code == "" || code == "<nil>" {
		return ""
	}
	return code
}

// BuildTelegramInlineButton 为补货广播消息构建「立即购买」inline 按钮。
// variables 中的 product_url 缺失或非法时返回 nil（不附加按钮）。
func BuildTelegramInlineButton(locale string, variables map[string]interface{}) map[string]interface{} {
	if len(variables) == 0 {
		return nil
	}
	rawURL := strings.TrimSpace(fmt.Sprintf("%v", variables["product_url"]))
	if rawURL == "" || rawURL == "<nil>" {
		return nil
	}
	if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
		return nil
	}
	buttonText := localizedNotificationText(locale, "🛒 立即购买", "🛒 立即購買", "🛒 Buy now")
	return map[string]interface{}{
		"inline_keyboard": [][]map[string]interface{}{
			{
				{"text": buttonText, "url": rawURL},
			},
		},
	}
}

func BuildDeliverySummary(locale string, counts OrderItemCounts) string {
	return localizedNotificationText(
		locale,
		fmt.Sprintf("共%d项，自动交付%d项，人工交付%d项，上游交付%d项", counts.Total, counts.Auto, counts.Manual, counts.Upstream),
		fmt.Sprintf("共%d項，自動交付%d項，人工交付%d項，上游交付%d項", counts.Total, counts.Auto, counts.Manual, counts.Upstream),
		fmt.Sprintf("Total %d items, auto %d, manual %d, upstream %d", counts.Total, counts.Auto, counts.Manual, counts.Upstream),
	)
}

func notificationFulfillmentLabel(locale, fulfillmentType string) string {
	switch NormalizeFulfillmentType(fulfillmentType) {
	case constants.FulfillmentTypeAuto:
		return localizedNotificationText(locale, "自动交付", "自動交付", "Auto")
	case constants.FulfillmentTypeUpstream:
		return localizedNotificationText(locale, "上游交付", "上游交付", "Upstream")
	default:
		return localizedNotificationText(locale, "人工交付", "人工交付", "Manual")
	}
}

func NormalizeFulfillmentType(fulfillmentType string) string {
	switch strings.ToLower(strings.TrimSpace(fulfillmentType)) {
	case constants.FulfillmentTypeAuto:
		return constants.FulfillmentTypeAuto
	case constants.FulfillmentTypeUpstream:
		return constants.FulfillmentTypeUpstream
	default:
		return constants.FulfillmentTypeManual
	}
}

func notificationInterfaceText(value interface{}, locale, defaultLocale string) string {
	switch typed := value.(type) {
	case jsonmap.JSON:
		return resolveNotificationLocalizedJSON(typed, locale, defaultLocale)
	case map[string]interface{}:
		return resolveNotificationLocalizedJSON(jsonmap.JSON(typed), locale, defaultLocale)
	case nil:
		return ""
	default:
		text := strings.TrimSpace(fmt.Sprintf("%v", typed))
		if text == "<nil>" {
			return ""
		}
		return text
	}
}

func resolveNotificationLocalizedJSON(value jsonmap.JSON, locale, defaultLocale string) string {
	if len(value) == 0 {
		return ""
	}
	if text := strings.TrimSpace(fmt.Sprintf("%v", value[locale])); text != "" && text != "<nil>" {
		return text
	}
	if text := strings.TrimSpace(fmt.Sprintf("%v", value[defaultLocale])); text != "" && text != "<nil>" {
		return text
	}
	for _, item := range value {
		if text := strings.TrimSpace(fmt.Sprintf("%v", item)); text != "" && text != "<nil>" {
			return text
		}
	}
	return ""
}

func localizedNotificationText(locale, zhCN, zhTW, enUS string) string {
	switch normalizeNotificationLocale(locale) {
	case constants.LocaleZhTW:
		return zhTW
	case constants.LocaleEnUS:
		return enUS
	default:
		return zhCN
	}
}
