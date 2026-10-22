package seoguidecontract

import "time"

// Category 分类的 SEO 引导页投影。
type Category struct {
	ID        uint
	Slug      string
	Name      map[string]interface{} // 多语言
	SortOrder int
}

// Product 商品的 SEO 引导页投影。
type Product struct {
	Slug        string
	Title       map[string]interface{} // 多语言
	Description map[string]interface{} // 多语言
	SeoMeta     map[string]interface{} // 多语言 keywords / description 覆盖
	PriceAmount string
	Images      []string
	CategoryID  uint
	UpdatedAt   time.Time
}

// Post 已发布文章的 SEO 引导页投影。
type Post struct {
	Slug        string
	Title       map[string]interface{}
	Description map[string]interface{}
	PublishedAt *time.Time
	CreatedAt   time.Time
}

// SiteBrand 站点品牌信息。
type SiteBrand struct {
	SiteName string
	SiteURL  string
	Currency string
}

// CatalogReader 提供引导页所需的分类与商品数据。
type CatalogReader interface {
	ListActiveCategories() ([]Category, error)
	ListActiveProducts(limit int) ([]Product, error)
	ListActiveProductsByCategory(categoryID uint, limit int) ([]Product, error)
}

// PostReader 提供引导页所需的已发布文章数据。
type PostReader interface {
	ListPublishedPosts(limit int) ([]Post, error)
}

// BrandReader 提供站点品牌与基础配置。
type BrandReader interface {
	GetSiteBrand() (SiteBrand, error)
}
