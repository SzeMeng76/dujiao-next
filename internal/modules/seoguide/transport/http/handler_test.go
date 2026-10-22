package seoguidehttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	seoguidedomain "github.com/dujiao-next/internal/modules/seoguide/domain"
	"github.com/gin-gonic/gin"
)

type fakeBuilder struct {
	home     *seoguidedomain.GuidePage
	homeErr  error
	category *seoguidedomain.GuidePage
	catErr   error
	product  *seoguidedomain.GuidePage
	prodErr  error

	gotHomeLocale string
	gotCatSlug    string
	gotCatLocale  string
	gotProdSlug   string
	gotProdLocale string
}

func (f *fakeBuilder) BuildHomePage(locale string) (*seoguidedomain.GuidePage, error) {
	f.gotHomeLocale = locale
	return f.home, f.homeErr
}

func (f *fakeBuilder) BuildCategoryPage(categorySlug, locale string) (*seoguidedomain.GuidePage, error) {
	f.gotCatSlug, f.gotCatLocale = categorySlug, locale
	return f.category, f.catErr
}

func (f *fakeBuilder) BuildProductPage(productSlug, locale string) (*seoguidedomain.GuidePage, error) {
	f.gotProdSlug, f.gotProdLocale = productSlug, locale
	return f.product, f.prodErr
}

func samplePage() *seoguidedomain.GuidePage {
	return &seoguidedomain.GuidePage{
		Title:        "示例商品 - 独角测试站",
		Description:  "示例商品描述",
		Keywords:     "示例,测试",
		CanonicalURL: "https://shop.example.com/products/demo",
		SiteName:     "独角测试站",
		Products: []seoguidedomain.ProductItem{
			{
				Slug:        "demo",
				Title:       "示例商品",
				Description: "示例商品描述",
				PriceAmount: "10.00",
				Currency:    "CNY",
				Image:       "https://shop.example.com/demo.png",
				DetailURL:   "https://shop.example.com/products/demo",
			},
		},
	}
}

func TestGetGuideHomeRendersPage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	builder := &fakeBuilder{home: samplePage()}
	h, err := NewHandler(builder)
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/guide", nil)

	h.GetGuideHome(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status want 200 got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "示例商品") {
		t.Fatalf("body should contain rendered product, got %s", w.Body.String())
	}
	if builder.gotHomeLocale != "zh-CN" {
		t.Fatalf("default locale want zh-CN got %q", builder.gotHomeLocale)
	}
}

func TestGetGuideCategoryNotFoundReturns404(t *testing.T) {
	gin.SetMode(gin.TestMode)
	builder := &fakeBuilder{catErr: errNotFound("seoguide category: slug not found")}
	h, err := NewHandler(builder)
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/guide/c/missing", nil)
	c.Params = gin.Params{{Key: "slug", Value: "missing"}}

	h.GetGuideCategory(c)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status want 404 got %d", w.Code)
	}
	if builder.gotCatSlug != "missing" {
		t.Fatalf("category slug got %q", builder.gotCatSlug)
	}
}

type errNotFound string

func (e errNotFound) Error() string { return string(e) }

func TestInjectProductHeadInsertsHeadTagsBeforeHeadClose(t *testing.T) {
	builder := &fakeBuilder{product: samplePage()}
	h, err := NewHandler(builder)
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}

	index := []byte("<html><head><title></title></head><body><div id=\"app\"></div></body></html>")
	got := h.InjectProductHead(context.Background(), "demo", "en-US", index)
	if got == nil {
		t.Fatal("want injected HTML, got nil")
	}
	body := string(got)
	if !strings.Contains(body, "<title>示例商品 - 独角测试站</title>") {
		t.Fatalf("missing injected title, got %s", body)
	}
	if !strings.Contains(body, `name="description" content="示例商品描述"`) {
		t.Fatalf("missing meta description, got %s", body)
	}
	if !strings.Contains(body, `rel="canonical" href="https://shop.example.com/products/demo"`) {
		t.Fatalf("missing canonical link, got %s", body)
	}
	if !strings.Contains(body, `"@type":"Product"`) {
		t.Fatalf("missing JSON-LD product schema, got %s", body)
	}
	if builder.gotProdSlug != "demo" || builder.gotProdLocale != "en-US" {
		t.Fatalf("builder called with slug=%q locale=%q", builder.gotProdSlug, builder.gotProdLocale)
	}
}

func TestInjectProductHeadReturnsNilWhenProductNotFound(t *testing.T) {
	builder := &fakeBuilder{prodErr: errNotFound("not found")}
	h, err := NewHandler(builder)
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}

	got := h.InjectProductHead(context.Background(), "missing", "zh-CN", []byte("<html><head></head></html>"))
	if got != nil {
		t.Fatalf("want nil when product not found, got %s", got)
	}
}

func TestResolveLocaleFromHeaderDefaultsToZhCN(t *testing.T) {
	cases := map[string]string{
		"zh-TW,zh;q=0.9": "zh-TW",
		"en-US,en;q=0.9": "en-US",
		"":               "zh-CN",
		"zh-CN,zh;q=0.9": "zh-CN",
	}
	for header, want := range cases {
		if got := resolveLocaleFromHeader(header); got != want {
			t.Errorf("resolveLocaleFromHeader(%q) = %q, want %q", header, got, want)
		}
	}
}
