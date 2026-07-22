package service

import (
	"strconv"
	"strings"

	"github.com/dujiao-next/internal/constants"
	"github.com/dujiao-next/internal/logger"
	"github.com/dujiao-next/internal/models"
	"github.com/dujiao-next/internal/modules/notification"
)

// restockNotifier 封装补货通知所需依赖，供卡密入库、人工库存增加等场景复用。
type restockNotifier struct {
	notificationSvc *notification.Service
	settingService  *SettingService
}

// enqueueRestockNotification 在商品补货时投递补货通知事件。
// product 为补货的商品；sku 为补货的 SKU（nil 表示无 SKU 或单规格商品）；
// stockAdded 为本次新增的数量；stockAvailable 为补货后的可用库存（<0 表示未知/无限，将省略）。
// 投递失败仅记录日志，不影响主流程。
func (n *restockNotifier) enqueueRestockNotification(product *models.Product, sku *models.ProductSKU, stockAdded int, stockAvailable int64) {
	if n == nil || n.notificationSvc == nil || product == nil {
		return
	}
	if stockAdded <= 0 {
		return
	}

	locale := constants.LocaleZhCN
	siteURL := ""
	if n.settingService != nil {
		if setting, err := n.settingService.GetNotificationCenterSetting(); err == nil {
			locale = resolveNotificationLocale(setting.DefaultLocale, constants.LocaleZhCN)
		}
		if brand, err := n.settingService.GetSiteBrand(); err == nil {
			siteURL = strings.TrimRight(strings.TrimSpace(brand.SiteURL), "/")
		}
	}

	title := resolveNotificationLocalizedJSON(product.TitleJSON, locale, constants.LocaleZhCN)
	if title == "" {
		title = localizedNotificationText(locale, "未命名商品", "未命名商品", "Unnamed item")
	}

	skuTitle := ""
	if sku != nil {
		skuTitle = notificationInterfaceText(sku.SpecValuesJSON, locale, constants.LocaleZhCN)
		if skuTitle == "" {
			skuTitle = strings.TrimSpace(sku.SKUCode)
		}
		if strings.EqualFold(skuTitle, models.DefaultSKUCode) {
			skuTitle = ""
		}
	}

	productTitleWithSKU := title
	if skuTitle != "" {
		productTitleWithSKU = title + " · " + skuTitle
	}

	data := models.JSON{
		"product_id":    strconv.FormatUint(uint64(product.ID), 10),
		"product_title": productTitleWithSKU,
		"product_slug":  strings.TrimSpace(product.Slug),
		"stock_added":   strconv.Itoa(stockAdded),
	}
	if skuTitle != "" {
		data["sku_title"] = skuTitle
	}
	if stockAvailable >= 0 {
		data["stock_available"] = strconv.FormatInt(stockAvailable, 10)
	} else {
		data["stock_available"] = localizedNotificationText(locale, "无限", "無限", "Unlimited")
	}
	if productURL := buildProductPublicURL(siteURL, product.Slug); productURL != "" {
		data["product_url"] = productURL
	}

	if err := n.notificationSvc.Enqueue(notification.EnqueueInput{
		EventType: constants.NotificationEventRestockSuccess,
		BizType:   constants.NotificationBizTypeRestock,
		BizID:     product.ID,
		Locale:    locale,
		Data:      data,
	}); err != nil {
		logger.Warnw("notification_enqueue_restock_failed",
			"product_id", product.ID,
			"product_slug", product.Slug,
			"error", err,
		)
	}
}

// buildProductPublicURL 根据站点地址与商品 slug 构建前台商品详情页链接。
// 前端路由约定为 /products/:slug（见 user 前端 router）。
func buildProductPublicURL(siteURL, slug string) string {
	siteURL = strings.TrimRight(strings.TrimSpace(siteURL), "/")
	slug = strings.TrimSpace(slug)
	if siteURL == "" || slug == "" {
		return ""
	}
	return siteURL + "/products/" + slug
}

// 本地化辅助函数（从 notification 模块复制，因为它们是私有的）
func resolveNotificationLocale(locale, fallback string) string {
	locale = strings.TrimSpace(locale)
	if locale == "" {
		locale = strings.TrimSpace(fallback)
	}
	return normalizeNotificationLocale(locale)
}

func normalizeNotificationLocale(locale string) string {
	locale = strings.TrimSpace(locale)
	if locale == "" {
		return constants.LocaleZhCN
	}
	locale = strings.ToLower(locale)
	if strings.HasPrefix(locale, "zh") {
		if strings.Contains(locale, "tw") || strings.Contains(locale, "hk") || strings.Contains(locale, "hant") {
			return constants.LocaleZhTW
		}
		return constants.LocaleZhCN
	}
	if strings.HasPrefix(locale, "en") {
		return constants.LocaleEnUS
	}
	return constants.LocaleZhCN
}

func resolveNotificationLocalizedJSON(value models.JSON, locale, defaultLocale string) string {
	locale = normalizeNotificationLocale(locale)
	defaultLocale = normalizeNotificationLocale(defaultLocale)

	if text, ok := value[locale].(string); ok && strings.TrimSpace(text) != "" {
		return strings.TrimSpace(text)
	}
	if locale != defaultLocale {
		if text, ok := value[defaultLocale].(string); ok && strings.TrimSpace(text) != "" {
			return strings.TrimSpace(text)
		}
	}
	for _, text := range value {
		if str, ok := text.(string); ok && strings.TrimSpace(str) != "" {
			return strings.TrimSpace(str)
		}
	}
	return ""
}

func localizedNotificationText(locale, zhCN, zhTW, enUS string) string {
	locale = normalizeNotificationLocale(locale)
	switch locale {
	case constants.LocaleZhTW:
		return zhTW
	case constants.LocaleEnUS:
		return enUS
	default:
		return zhCN
	}
}

func notificationInterfaceText(value interface{}, locale, defaultLocale string) string {
	if typed, ok := value.(models.JSON); ok {
		return resolveNotificationLocalizedJSON(typed, locale, defaultLocale)
	}
	if typed, ok := value.(map[string]interface{}); ok {
		return resolveNotificationLocalizedJSON(models.JSON(typed), locale, defaultLocale)
	}
	return ""
}
