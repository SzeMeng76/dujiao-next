package seoguideapp

import (
	"testing"

	seoguidecontract "github.com/dujiao-next/internal/modules/seoguide/contract"
)

type catalogStub struct {
	categories []seoguidecontract.Category
	products   []seoguidecontract.Product
}

func (c *catalogStub) ListActiveCategories() ([]seoguidecontract.Category, error) {
	return c.categories, nil
}

func (c *catalogStub) ListActiveProducts(limit int) ([]seoguidecontract.Product, error) {
	if limit < len(c.products) {
		return c.products[:limit], nil
	}
	return c.products, nil
}

func (c *catalogStub) ListActiveProductsByCategory(categoryID uint, limit int) ([]seoguidecontract.Product, error) {
	var out []seoguidecontract.Product
	for _, p := range c.products {
		if p.CategoryID == categoryID {
			out = append(out, p)
		}
	}
	if limit < len(out) {
		out = out[:limit]
	}
	return out, nil
}

type postStub struct {
	posts []seoguidecontract.Post
}

func (p *postStub) ListPublishedPosts(limit int) ([]seoguidecontract.Post, error) {
	if limit < len(p.posts) {
		return p.posts[:limit], nil
	}
	return p.posts, nil
}

type brandStub struct {
	brand seoguidecontract.SiteBrand
}

func (b *brandStub) GetSiteBrand() (seoguidecontract.SiteBrand, error) {
	return b.brand, nil
}

func newFixtureService() *Service {
	catalog := &catalogStub{
		categories: []seoguidecontract.Category{
			{ID: 1, Slug: "games", Name: map[string]interface{}{"zh-CN": "游戏"}},
		},
		products: []seoguidecontract.Product{
			{
				Slug:        "steam-key",
				Title:       map[string]interface{}{"zh-CN": "Steam 充值卡"},
				Description: map[string]interface{}{"zh-CN": "官方正版 Steam 充值卡"},
				PriceAmount: "99.00",
				Images:      []string{"https://example.com/steam.png"},
				CategoryID:  1,
			},
		},
	}
	posts := &postStub{}
	brand := &brandStub{brand: seoguidecontract.SiteBrand{SiteName: "独角测试站", SiteURL: "https://shop.example.com/", Currency: "CNY"}}
	svc, err := NewService(catalog, posts, brand)
	if err != nil {
		panic(err)
	}
	return svc
}

func TestNewServiceRejectsNilDependency(t *testing.T) {
	brand := &brandStub{}
	if _, err := NewService(nil, &postStub{}, brand); err == nil {
		t.Fatal("want error when catalog reader is nil")
	}
}

func TestBuildHomePageLinksProductToItsCategoryGuideURL(t *testing.T) {
	svc := newFixtureService()

	page, err := svc.BuildHomePage("zh-CN")
	if err != nil {
		t.Fatalf("BuildHomePage: %v", err)
	}
	if len(page.Products) != 1 {
		t.Fatalf("want 1 product, got %d", len(page.Products))
	}
	got := page.Products[0]
	if got.GuideURL != "https://shop.example.com/guide/c/games" {
		t.Fatalf("product GuideURL should resolve to its category's guide page, got %q", got.GuideURL)
	}
	if got.DetailURL != "https://shop.example.com/products/steam-key" {
		t.Fatalf("product DetailURL got %q", got.DetailURL)
	}
	if page.CanonicalURL != "https://shop.example.com/guide" {
		t.Fatalf("home canonical URL got %q", page.CanonicalURL)
	}
}

func TestBuildCategoryPageFiltersProductsByCategory(t *testing.T) {
	svc := newFixtureService()

	page, err := svc.BuildCategoryPage("games", "zh-CN")
	if err != nil {
		t.Fatalf("BuildCategoryPage: %v", err)
	}
	if page.CurrentCategory == nil || page.CurrentCategory.Slug != "games" {
		t.Fatalf("CurrentCategory got %+v", page.CurrentCategory)
	}
	if len(page.Products) != 1 || page.Products[0].Slug != "steam-key" {
		t.Fatalf("want products filtered to category, got %+v", page.Products)
	}
}

func TestBuildCategoryPageUnknownSlugReturnsError(t *testing.T) {
	svc := newFixtureService()

	if _, err := svc.BuildCategoryPage("does-not-exist", "zh-CN"); err == nil {
		t.Fatal("want error for unknown category slug")
	}
}

func TestBuildProductPageReturnsSEOData(t *testing.T) {
	svc := newFixtureService()

	page, err := svc.BuildProductPage("steam-key", "zh-CN")
	if err != nil {
		t.Fatalf("BuildProductPage: %v", err)
	}
	if page.Title != "Steam 充值卡 - 独角测试站" {
		t.Fatalf("title got %q", page.Title)
	}
	if page.CanonicalURL != "https://shop.example.com/products/steam-key" {
		t.Fatalf("canonical URL got %q", page.CanonicalURL)
	}
	if len(page.Products) != 1 || page.Products[0].Image != "https://example.com/steam.png" {
		t.Fatalf("product item got %+v", page.Products)
	}
}

func TestBuildProductPageUnknownSlugReturnsError(t *testing.T) {
	svc := newFixtureService()

	if _, err := svc.BuildProductPage("does-not-exist", "zh-CN"); err == nil {
		t.Fatal("want error for unknown product slug")
	}
}

func TestLocalizedTextFallsBackToPrefixThenAnyValue(t *testing.T) {
	m := map[string]interface{}{"zh-TW": "繁體標題"}
	if got := localizedText(m, "zh-CN"); got != "繁體標題" {
		t.Fatalf("want prefix match fallback, got %q", got)
	}

	m = map[string]interface{}{"en-US": "English Title"}
	if got := localizedText(m, "zh-CN"); got != "English Title" {
		t.Fatalf("want any-value fallback, got %q", got)
	}

	if got := localizedText(nil, "zh-CN"); got != "" {
		t.Fatalf("want empty string for nil map, got %q", got)
	}
}
