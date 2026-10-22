package seoguideapp

import (
	"fmt"
	"strings"

	seoguidecontract "github.com/dujiao-next/internal/modules/seoguide/contract"
	seoguidedomain "github.com/dujiao-next/internal/modules/seoguide/domain"
)

const (
	// 首页每个分类最多展示的商品数
	homeProductsPerCategory = 6
	// 分类页最多展示的商品数
	categoryPageMaxProducts = 200
	// 首页最多展示的文章数
	homeMaxPosts = 10
	// 首页最多展示的分类数
	homeMaxCategories = 50
)

// Service 生成 SEO 引导页的渲染数据。
type Service struct {
	catalog seoguidecontract.CatalogReader
	posts   seoguidecontract.PostReader
	brand   seoguidecontract.BrandReader
}

// NewService 创建 SEO 引导服务。
func NewService(
	catalog seoguidecontract.CatalogReader,
	posts seoguidecontract.PostReader,
	brand seoguidecontract.BrandReader,
) (*Service, error) {
	if catalog == nil || posts == nil || brand == nil {
		return nil, fmt.Errorf("seoguide: required dependency is nil")
	}
	return &Service{catalog: catalog, posts: posts, brand: brand}, nil
}

// BuildHomePage 构建 /guide 首页的渲染数据。
func (s *Service) BuildHomePage(locale string) (*seoguidedomain.GuidePage, error) {
	b, err := s.brand.GetSiteBrand()
	if err != nil {
		return nil, fmt.Errorf("seoguide home: get brand: %w", err)
	}

	cats, err := s.catalog.ListActiveCategories()
	if err != nil {
		return nil, fmt.Errorf("seoguide home: list categories: %w", err)
	}
	if len(cats) > homeMaxCategories {
		cats = cats[:homeMaxCategories]
	}

	postRows, err := s.posts.ListPublishedPosts(homeMaxPosts)
	if err != nil {
		return nil, fmt.Errorf("seoguide home: list posts: %w", err)
	}

	siteURL := strings.TrimRight(b.SiteURL, "/")
	catItems := make([]seoguidedomain.CategoryItem, 0, len(cats))
	for _, c := range cats {
		catItems = append(catItems, seoguidedomain.CategoryItem{
			Slug:     c.Slug,
			Name:     localizedText(c.Name, locale),
			GuideURL: siteURL + "/guide/c/" + c.Slug,
		})
	}

	// 首页商品：对每个分类取少量热门商品，避免一页内容过长
	var productItems []seoguidedomain.ProductItem
	// 用分类 ID→slug 做一个本地 map，以便商品找到自己所属分类的 GuideURL
	catSlugByID := make(map[uint]string, len(cats))
	for _, c := range cats {
		catSlugByID[c.ID] = c.Slug
	}
	// 先拉一批商品
	allProducts, err := s.catalog.ListActiveProducts(homeMaxCategories * homeProductsPerCategory)
	if err != nil {
		return nil, fmt.Errorf("seoguide home: list products: %w", err)
	}
	// 统计每个分类已加入商品数
	countByCat := make(map[uint]int)
	for _, p := range allProducts {
		if countByCat[p.CategoryID] >= homeProductsPerCategory {
			continue
		}
		countByCat[p.CategoryID]++
		catSlug := catSlugByID[p.CategoryID]
		guideURL := ""
		if catSlug != "" {
			guideURL = siteURL + "/guide/c/" + catSlug
		}
		productItems = append(productItems, buildProductItem(p, b.Currency, siteURL, guideURL, locale))
	}

	postItems := make([]seoguidedomain.PostItem, 0, len(postRows))
	for _, p := range postRows {
		postItems = append(postItems, seoguidedomain.PostItem{
			Slug:    p.Slug,
			Title:   localizedText(p.Title, locale),
			PostURL: siteURL + "/blog/" + p.Slug,
		})
	}

	siteName := b.SiteName
	title := siteName
	if title == "" {
		title = "商品目录"
	}

	return &seoguidedomain.GuidePage{
		Title:        title,
		Description:  title,
		CanonicalURL: siteURL + "/guide",
		SiteName:     siteName,
		SiteURL:      siteURL,
		Currency:     b.Currency,
		Categories:   catItems,
		Products:     productItems,
		Posts:        postItems,
	}, nil
}

// BuildCategoryPage 构建 /guide/c/:slug 分类引导页的渲染数据。
func (s *Service) BuildCategoryPage(categorySlug, locale string) (*seoguidedomain.GuidePage, error) {
	b, err := s.brand.GetSiteBrand()
	if err != nil {
		return nil, fmt.Errorf("seoguide category: get brand: %w", err)
	}

	cats, err := s.catalog.ListActiveCategories()
	if err != nil {
		return nil, fmt.Errorf("seoguide category: list categories: %w", err)
	}

	siteURL := strings.TrimRight(b.SiteURL, "/")

	// 找到目标分类
	var current *seoguidecontract.Category
	for i, c := range cats {
		if c.Slug == categorySlug {
			current = &cats[i]
			break
		}
	}
	if current == nil {
		return nil, fmt.Errorf("seoguide category: slug %q not found", categorySlug)
	}

	// 其他分类（导航用）
	catItems := make([]seoguidedomain.CategoryItem, 0, len(cats))
	for _, c := range cats {
		catItems = append(catItems, seoguidedomain.CategoryItem{
			Slug:     c.Slug,
			Name:     localizedText(c.Name, locale),
			GuideURL: siteURL + "/guide/c/" + c.Slug,
		})
	}

	products, err := s.catalog.ListActiveProductsByCategory(current.ID, categoryPageMaxProducts)
	if err != nil {
		return nil, fmt.Errorf("seoguide category: list products: %w", err)
	}

	guideURL := siteURL + "/guide/c/" + current.Slug
	productItems := make([]seoguidedomain.ProductItem, 0, len(products))
	for _, p := range products {
		productItems = append(productItems, buildProductItem(p, b.Currency, siteURL, guideURL, locale))
	}

	catName := localizedText(current.Name, locale)
	siteName := b.SiteName
	title := catName
	if siteName != "" {
		title = catName + " - " + siteName
	}

	return &seoguidedomain.GuidePage{
		Title:        title,
		Description:  title,
		CanonicalURL: guideURL,
		SiteName:     siteName,
		SiteURL:      siteURL,
		Currency:     b.Currency,
		CurrentCategory: &seoguidedomain.CategoryItem{
			Slug:     current.Slug,
			Name:     catName,
			GuideURL: guideURL,
		},
		Categories: catItems,
		Products:   productItems,
	}, nil
}

// BuildProductPage 构建 /products/:slug 商品详情页需注入的 SEO head 数据。
func (s *Service) BuildProductPage(productSlug, locale string) (*seoguidedomain.GuidePage, error) {
	b, err := s.brand.GetSiteBrand()
	if err != nil {
		return nil, fmt.Errorf("seoguide product: get brand: %w", err)
	}

	// 只拉这一个商品
	products, err := s.catalog.ListActiveProducts(10000)
	if err != nil {
		return nil, fmt.Errorf("seoguide product: list products: %w", err)
	}

	siteURL := strings.TrimRight(b.SiteURL, "/")

	var target *seoguidecontract.Product
	for i, p := range products {
		if p.Slug == productSlug {
			target = &products[i]
			break
		}
	}
	if target == nil {
		return nil, fmt.Errorf("seoguide product: slug %q not found or inactive", productSlug)
	}

	item := buildProductItem(*target, b.Currency, siteURL, "", locale)

	// SEO meta 覆盖（商品后台填的 seo_meta.description 优先）
	desc := item.Description
	if seoDesc := localizedText(target.SeoMeta, locale+"_description"); seoDesc != "" {
		desc = seoDesc
	}
	keywords := localizedText(target.SeoMeta, locale+"_keywords")
	if keywords == "" {
		keywords = localizedText(target.SeoMeta, "keywords")
	}

	title := item.Title + " - " + b.SiteName

	return &seoguidedomain.GuidePage{
		Title:        title,
		Description:  desc,
		Keywords:     keywords,
		CanonicalURL: siteURL + "/products/" + target.Slug,
		SiteName:     b.SiteName,
		SiteURL:      siteURL,
		Currency:     b.Currency,
		Products:     []seoguidedomain.ProductItem{item},
	}, nil
}

// ——— helpers ———

func localizedText(m map[string]interface{}, locale string) string {
	if m == nil {
		return ""
	}
	// 尝试精确匹配（如 "zh-CN"）
	if v, ok := m[locale]; ok {
		if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	// 尝试语言前缀匹配（如 "zh"）
	prefix := strings.SplitN(locale, "-", 2)[0]
	for k, v := range m {
		if strings.HasPrefix(k, prefix) {
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				return strings.TrimSpace(s)
			}
		}
	}
	// 兜底：取第一个非空值
	for _, v := range m {
		if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

func firstImage(images []string) string {
	for _, img := range images {
		if strings.TrimSpace(img) != "" {
			return strings.TrimSpace(img)
		}
	}
	return ""
}

func buildProductItem(p seoguidecontract.Product, currency, siteURL, catGuideURL, locale string) seoguidedomain.ProductItem {
	title := localizedText(p.Title, locale)
	if title == "" {
		title = p.Slug
	}
	desc := localizedText(p.Description, locale)
	// 截短过长的描述，避免 meta description 超出建议字数
	if len([]rune(desc)) > 160 {
		runes := []rune(desc)
		desc = string(runes[:157]) + "..."
	}
	return seoguidedomain.ProductItem{
		Slug:        p.Slug,
		Title:       title,
		Description: desc,
		PriceAmount: p.PriceAmount,
		Currency:    currency,
		Image:       firstImage(p.Images),
		DetailURL:   siteURL + "/products/" + p.Slug,
		GuideURL:    catGuideURL,
	}
}
