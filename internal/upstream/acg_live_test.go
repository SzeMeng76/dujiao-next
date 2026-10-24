package upstream

import (
	"context"
	"os"
	"testing"
	"time"

	siteconnectiondomain "github.com/dujiao-next/internal/modules/siteconnection/domain"
)

// TestACGAdapterLive 需要真实异次元凭据，通过环境变量启用：
//
//	ACG_LIVE_URL=https://example.com ACG_LIVE_ID=xxx ACG_LIVE_KEY=xxx go test ./internal/upstream/ -run Live -v
func TestACGAdapterLive(t *testing.T) {
	baseURL := os.Getenv("ACG_LIVE_URL")
	appID := os.Getenv("ACG_LIVE_ID")
	appKey := os.Getenv("ACG_LIVE_KEY")
	if baseURL == "" || appID == "" || appKey == "" {
		t.Skip("ACG live credentials not configured")
	}

	adapter := NewACGAdapter(&siteconnectiondomain.Connection{
		BaseURL: baseURL, ApiKey: appID, ApiSecret: appKey,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	t.Run("Ping", func(t *testing.T) {
		result, err := adapter.Ping(ctx)
		if err != nil {
			t.Fatalf("ping: %v", err)
		}
		t.Logf("shop=%s balance=%s %s", result.SiteName, result.Balance, result.Currency)
	})

	t.Run("ListCategories", func(t *testing.T) {
		result, err := adapter.ListCategories(ctx)
		if err != nil {
			t.Fatalf("categories: %v", err)
		}
		if len(result.Categories) == 0 {
			t.Fatal("expected at least one category")
		}
		t.Logf("categories=%d first=%v", len(result.Categories), result.Categories[0])
	})

	var firstID uint
	t.Run("ListProducts", func(t *testing.T) {
		result, err := adapter.ListProducts(ctx, ListProductsOpts{Page: 1, PageSize: 5})
		if err != nil {
			t.Fatalf("products: %v", err)
		}
		if len(result.Items) == 0 {
			t.Fatal("expected at least one product")
		}
		first := result.Items[0]
		firstID = first.ID
		t.Logf("total=%d page_items=%d first: id=%d code=%s price=%s stock=%d title=%v",
			result.Total, len(result.Items), first.ID, first.Code, first.PriceAmount,
			first.SKUs[0].StockQuantity, first.Title["zh-CN"])
		if first.Code == "" {
			t.Fatal("product code must not be empty")
		}
	})

	t.Run("GetProduct", func(t *testing.T) {
		if firstID == 0 {
			t.Skip("no product id from ListProducts")
		}
		product, err := adapter.GetProduct(ctx, firstID)
		if err != nil {
			t.Fatalf("get product %d: %v", firstID, err)
		}
		t.Logf("id=%d code=%s price=%s stock=%d", product.ID, product.Code, product.PriceAmount, product.SKUs[0].StockQuantity)
	})
}
