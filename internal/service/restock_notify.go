package service

import (
	"strconv"
	"strings"

	"github.com/dujiao-next/internal/constants"
	"github.com/dujiao-next/internal/logger"
	"github.com/dujiao-next/internal/models"
)

// restockNotifier 封装补货通知所需依赖，供卡密入库、人工库存增加等场景复用。
type restockNotifier struct {
	notificationSvc *NotificationService
	settingService  *SettingService
}

// enqueueRestockNotification 在商品补货时投递补货通知事件。
// product 为补货的商品；stockAdded 为本次新增的数量；stockAvailable 为补货后的可用库存（<0 表示未知/无限，将省略）。
// 投递失败仅记录日志，不影响主流程。
func (n *restockNotifier) enqueueRestockNotification(product *models.Product, stockAdded int, stockAvailable int64) {
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

	data := models.JSON{
		"product_id":    strconv.FormatUint(uint64(product.ID), 10),
		"product_title": title,
		"product_slug":  strings.TrimSpace(product.Slug),
		"stock_added":   strconv.Itoa(stockAdded),
	}
	if stockAvailable >= 0 {
		data["stock_available"] = strconv.FormatInt(stockAvailable, 10)
	} else {
		data["stock_available"] = localizedNotificationText(locale, "无限", "無限", "Unlimited")
	}
	if productURL := buildProductPublicURL(siteURL, product.Slug); productURL != "" {
		data["product_url"] = productURL
	}

	if err := n.notificationSvc.Enqueue(NotificationEnqueueInput{
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
