package seoguidedomain

// GuidePage 代表一个 SEO 引导页的完整渲染数据。
type GuidePage struct {
	// 页面标题（已拼好"商品名 - 站点名"）
	Title string
	// 页面描述（meta description）
	Description string
	// 关键词（meta keywords）
	Keywords string
	// Canonical URL
	CanonicalURL string
	// 站点名
	SiteName string
	// 站点根 URL
	SiteURL string
	// 币种
	Currency string
	// 当前分类（分类页专用，首页为空）
	CurrentCategory *CategoryItem
	// 分类列表（首页展示全部分类；分类页展示其他分类作为导航）
	Categories []CategoryItem
	// 商品列表（首页展示所有分类热门商品；分类页展示该分类全部商品）
	Products []ProductItem
	// 文章列表（首页展示最近文章）
	Posts []PostItem
}

// CategoryItem 引导页中展示的分类条目。
type CategoryItem struct {
	Slug     string
	Name     string
	GuideURL string // /guide/c/:slug
}

// ProductItem 引导页中展示的商品条目。
type ProductItem struct {
	Slug        string
	Title       string
	Description string
	PriceAmount string
	Currency    string
	Image       string
	DetailURL   string // /products/:slug
	GuideURL    string // 对应分类的 /guide/c/:slug
}

// PostItem 引导页中展示的文章条目。
type PostItem struct {
	Slug    string
	Title   string
	PostURL string // /blog/:slug
}
