package seoguidebrandreader

import (
	seoguidecontract "github.com/dujiao-next/internal/modules/seoguide/contract"
	settingsapp "github.com/dujiao-next/internal/modules/settings/application"
)

// Reader 从 settings 应用服务读取站点品牌信息。
type Reader struct {
	settings *settingsapp.Service
}

var _ seoguidecontract.BrandReader = (*Reader)(nil)

// New 创建品牌读取适配器。
func New(settings *settingsapp.Service) *Reader {
	if settings == nil {
		panic("seoguide brand reader: settings service is nil")
	}
	return &Reader{settings: settings}
}

// GetSiteBrand 读取站点品牌与货币配置。
func (r *Reader) GetSiteBrand() (seoguidecontract.SiteBrand, error) {
	brand, err := r.settings.GetSiteBrand()
	if err != nil {
		return seoguidecontract.SiteBrand{}, err
	}
	currency, err := r.settings.GetSiteCurrency("CNY")
	if err != nil {
		currency = "CNY"
	}
	return seoguidecontract.SiteBrand{
		SiteName: brand.SiteName,
		SiteURL:  brand.SiteURL,
		Currency: currency,
	}, nil
}
