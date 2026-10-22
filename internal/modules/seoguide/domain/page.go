package seoguidedomain

// GuidePage 代表一个 SEO 引导页的完整渲染数据。
type GuidePage struct {
	// HTML lang 属性（zh-CN / zh-TW / en-US）
	Lang string
	// 页面固定文案（随语言切换）
	Labels Labels
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

// Labels 引导页的固定文案，按语言切换。
type Labels struct {
	DefaultTitle string // 站点名缺失时首页标题
	BackToAll    string // 分类页「返回全部分类」
	CategoryPre  string // 分类页「分类：」前缀
	LatestPosts  string // 「最新资讯」
	GuideIndex   string // 页脚「商品索引」
}

// LabelsFor 返回指定语言的固定文案，未知语言回落到 zh-CN。
func LabelsFor(locale string) (lang string, l Labels) {
	switch locale {
	case "en-US":
		return "en-US", Labels{
			DefaultTitle: "Product Catalog",
			BackToAll:    "← Back to all categories",
			CategoryPre:  "Category: ",
			LatestPosts:  "Latest Posts",
			GuideIndex:   "Product Index",
		}
	case "zh-TW":
		return "zh-TW", Labels{
			DefaultTitle: "商品目錄",
			BackToAll:    "← 返回全部分類",
			CategoryPre:  "分類：",
			LatestPosts:  "最新資訊",
			GuideIndex:   "商品索引",
		}
	default:
		return "zh-CN", Labels{
			DefaultTitle: "商品目录",
			BackToAll:    "← 返回全部分类",
			CategoryPre:  "分类：",
			LatestPosts:  "最新资讯",
			GuideIndex:   "商品索引",
		}
	}
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
