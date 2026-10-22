package seoguidehttp

import (
	"bytes"
	"context"
	"html/template"
	"net/http"
	"strings"

	seoguidedomain "github.com/dujiao-next/internal/modules/seoguide/domain"
	"github.com/gin-gonic/gin"
)

// GuideBuilder 构建 SEO 引导页渲染数据。
type GuideBuilder interface {
	BuildHomePage(locale string) (*seoguidedomain.GuidePage, error)
	BuildCategoryPage(categorySlug, locale string) (*seoguidedomain.GuidePage, error)
	BuildProductPage(productSlug, locale string) (*seoguidedomain.GuidePage, error)
}

// Handler 处理 /guide 系列路由，以及向商品详情页注入 SEO head。
type Handler struct {
	builder GuideBuilder
	tmpl    *template.Template
}

// NewHandler 创建 SEO 引导处理器。
func NewHandler(builder GuideBuilder) (*Handler, error) {
	tmpl, err := template.New("guide").Funcs(template.FuncMap{
		"safeURL": func(s string) template.URL { return template.URL(s) }, //nolint:gosec
	}).Parse(guideTmplSrc)
	if err != nil {
		return nil, err
	}
	return &Handler{builder: builder, tmpl: tmpl}, nil
}

// GetGuideHome GET /guide
func (h *Handler) GetGuideHome(c *gin.Context) {
	locale := resolveLocale(c)
	page, err := h.builder.BuildHomePage(locale)
	if err != nil {
		c.String(http.StatusInternalServerError, "internal error")
		return
	}
	h.renderPage(c, page)
}

// GetGuideCategory GET /guide/c/:slug
func (h *Handler) GetGuideCategory(c *gin.Context) {
	slug := c.Param("slug")
	locale := resolveLocale(c)
	page, err := h.builder.BuildCategoryPage(slug, locale)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			c.String(http.StatusNotFound, "not found")
			return
		}
		c.String(http.StatusInternalServerError, "internal error")
		return
	}
	h.renderPage(c, page)
}

// InjectProductHead 将商品页 SEO head 注入进 SPA index.html，
// 由 internal/web 的 NoRoute 在命中 /products/:slug 时调用。
// 若商品不存在或不活跃，返回 nil，调用方直接返回原始 index.html 即可。
func (h *Handler) InjectProductHead(ctx context.Context, slug, acceptLanguage string, indexHTML []byte) []byte {
	page, err := h.builder.BuildProductPage(slug, resolveLocaleFromHeader(acceptLanguage))
	if err != nil {
		// 商品不存在 / 已下架，不注入，让 SPA 自己处理 404 状态
		return nil
	}
	return injectHeadInto(indexHTML, page)
}

// renderPage 渲染引导页 HTML。
func (h *Handler) renderPage(c *gin.Context, page *seoguidedomain.GuidePage) {
	var buf bytes.Buffer
	if err := h.tmpl.Execute(&buf, page); err != nil {
		c.String(http.StatusInternalServerError, "template error")
		return
	}
	c.Header("Cache-Control", "public, max-age=300")
	c.Data(http.StatusOK, "text/html; charset=utf-8", buf.Bytes())
}

// resolveLocale 从 Accept-Language 或查询参数推断语言，默认 zh-CN。
func resolveLocale(c *gin.Context) string {
	if lang := c.Query("lang"); lang != "" {
		return lang
	}
	return resolveLocaleFromHeader(c.GetHeader("Accept-Language"))
}

// resolveLocaleFromHeader 从 Accept-Language 头推断语言，默认 zh-CN。
func resolveLocaleFromHeader(accept string) string {
	if strings.Contains(accept, "zh-TW") || strings.Contains(accept, "zh-Hant") {
		return "zh-TW"
	}
	if strings.Contains(accept, "en") {
		return "en-US"
	}
	return "zh-CN"
}

// injectHeadInto 把 SEO head 标签注入进 SPA index.html 的 </head> 之前。
func injectHeadInto(indexHTML []byte, page *seoguidedomain.GuidePage) []byte {
	var sb strings.Builder
	sb.WriteString("\n")
	if page.Title != "" {
		sb.WriteString("  <title>")
		sb.WriteString(template.HTMLEscapeString(page.Title))
		sb.WriteString("</title>\n")
	}
	if page.Description != "" {
		sb.WriteString(`  <meta name="description" content="`)
		sb.WriteString(template.HTMLEscapeString(page.Description))
		sb.WriteString("\">\n")
	}
	if page.Keywords != "" {
		sb.WriteString(`  <meta name="keywords" content="`)
		sb.WriteString(template.HTMLEscapeString(page.Keywords))
		sb.WriteString("\">\n")
	}
	if page.CanonicalURL != "" {
		sb.WriteString(`  <link rel="canonical" href="`)
		sb.WriteString(template.HTMLEscapeString(page.CanonicalURL))
		sb.WriteString("\">\n")
	}
	// Open Graph
	if page.Title != "" {
		sb.WriteString(`  <meta property="og:title" content="`)
		sb.WriteString(template.HTMLEscapeString(page.Title))
		sb.WriteString("\">\n")
	}
	if page.Description != "" {
		sb.WriteString(`  <meta property="og:description" content="`)
		sb.WriteString(template.HTMLEscapeString(page.Description))
		sb.WriteString("\">\n")
	}
	if page.CanonicalURL != "" {
		sb.WriteString(`  <meta property="og:url" content="`)
		sb.WriteString(template.HTMLEscapeString(page.CanonicalURL))
		sb.WriteString("\">\n")
		sb.WriteString(`  <meta property="og:type" content="product">` + "\n")
	}
	if len(page.Products) > 0 && page.Products[0].Image != "" {
		sb.WriteString(`  <meta property="og:image" content="`)
		sb.WriteString(template.HTMLEscapeString(page.Products[0].Image))
		sb.WriteString("\">\n")
	}
	if page.SiteName != "" {
		sb.WriteString(`  <meta property="og:site_name" content="`)
		sb.WriteString(template.HTMLEscapeString(page.SiteName))
		sb.WriteString("\">\n")
	}

	// JSON-LD Product
	if len(page.Products) > 0 {
		p := page.Products[0]
		var ld strings.Builder
		ld.WriteString(`  <script type="application/ld+json">{"@context":"https://schema.org","@type":"Product","name":"`)
		ld.WriteString(jsonEscape(p.Title))
		ld.WriteString(`","url":"`)
		ld.WriteString(jsonEscape(p.DetailURL))
		ld.WriteString(`","offers":{"@type":"Offer","price":"`)
		ld.WriteString(jsonEscape(p.PriceAmount))
		ld.WriteString(`","priceCurrency":"`)
		ld.WriteString(jsonEscape(p.Currency))
		ld.WriteString(`","availability":"https://schema.org/InStock"}`)
		if p.Description != "" {
			ld.WriteString(`,"description":"`)
			ld.WriteString(jsonEscape(p.Description))
			ld.WriteString(`"`)
		}
		if p.Image != "" {
			ld.WriteString(`,"image":"`)
			ld.WriteString(jsonEscape(p.Image))
			ld.WriteString(`"`)
		}
		ld.WriteString("}</script>\n")
		sb.WriteString(ld.String())
	}

	injection := sb.String()
	marker := []byte("</head>")
	idx := bytes.Index(bytes.ToLower(indexHTML), marker)
	if idx < 0 {
		return indexHTML
	}
	result := make([]byte, 0, len(indexHTML)+len(injection))
	result = append(result, indexHTML[:idx]...)
	result = append(result, []byte(injection)...)
	result = append(result, indexHTML[idx:]...)
	return result
}

// jsonEscape 对 JSON 字符串值做最小转义，防止 XSS。
func jsonEscape(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, "\r", `\r`)
	s = strings.ReplaceAll(s, "<", `<`)
	s = strings.ReplaceAll(s, ">", `>`)
	s = strings.ReplaceAll(s, "&", `&`)
	return s
}
