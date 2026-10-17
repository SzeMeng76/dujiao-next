package integrationtest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	categorydomain "github.com/dujiao-next/internal/modules/catalog/category/domain"
	productapplication "github.com/dujiao-next/internal/modules/catalog/product/application"
	productdomain "github.com/dujiao-next/internal/modules/catalog/product/domain"
	productgormstore "github.com/dujiao-next/internal/modules/catalog/product/store/gormstore"
	channeltransport "github.com/dujiao-next/internal/modules/channelapi/transport/http"
	externalidentitydomain "github.com/dujiao-next/internal/modules/identity/externalidentity/domain"
	userdomain "github.com/dujiao-next/internal/modules/identity/user/domain"
	promotiondomain "github.com/dujiao-next/internal/modules/promotion/domain"
	settingsapp "github.com/dujiao-next/internal/modules/settings/application"
	settingsstore "github.com/dujiao-next/internal/modules/settings/infrastructure/gormstore"
	"github.com/dujiao-next/internal/shared/jsonmap"
	"github.com/dujiao-next/internal/shared/money"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

type exclusiveIdentity struct {
	channeltransport.IdentityService
	userID uint
}

func (f exclusiveIdentity) ResolveTelegramChannelIdentity(channeltransport.TelegramIdentityInput) (*userdomain.User, *externalidentitydomain.Identity, error) {
	return &userdomain.User{ID: f.userID}, nil, nil
}

type exclusiveLevels struct{ exclusiveUserID uint }

func (l exclusiveLevels) ResolveMemberPrice(_, _, _ uint, base decimal.Decimal) (decimal.Decimal, decimal.Decimal) {
	return base, decimal.Zero
}

func (l exclusiveLevels) ResolveUserPrice(userID, _, _ uint, base decimal.Decimal) (decimal.Decimal, bool) {
	price := decimal.NewFromInt(95)
	if userID == l.exclusiveUserID && price.LessThan(base) {
		return price, true
	}
	return base, false
}

type fixedPromotion struct{}

func (fixedPromotion) ApplyPromotion(p *productdomain.Product, _ int) (*promotiondomain.Promotion, money.Amount, error) {
	return &promotiondomain.Promotion{Name: "sale", Type: "percent"}, money.FromDecimal(decimal.NewFromInt(80)), nil
}

// 命中用户专属一口价时，渠道目录不得同时返回活动价；未命中的用户仍按原样返回活动价。
func TestChannelCatalogExclusivePriceSuppressesPromotionPrice(t *testing.T) {
	dsn := fmt.Sprintf("file:channel_catalog_exclusive_%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite failed: %v", err)
	}
	if err := db.AutoMigrate(&categorydomain.Category{}, &productdomain.Product{}, &productdomain.ProductSKU{}); err != nil {
		t.Fatalf("auto migrate failed: %v", err)
	}
	category := categorydomain.Category{Slug: "c", NameJSON: jsonmap.JSON{"zh-CN": "c"}, IsActive: true}
	if err := db.Create(&category).Error; err != nil {
		t.Fatalf("create category failed: %v", err)
	}
	product := productdomain.Product{
		CategoryID: category.ID, Slug: "p", TitleJSON: jsonmap.JSON{"zh-CN": "p"},
		PriceAmount: money.FromDecimal(decimal.NewFromInt(100)), FulfillmentType: "manual", IsActive: true,
	}
	if err := db.Create(&product).Error; err != nil {
		t.Fatalf("create product failed: %v", err)
	}
	sku := productdomain.ProductSKU{
		ProductID: product.ID, SKUCode: "s", PriceAmount: money.FromDecimal(decimal.NewFromInt(100)), IsActive: true,
	}
	if err := db.Create(&sku).Error; err != nil {
		t.Fatalf("create sku failed: %v", err)
	}

	productRepo := productgormstore.NewProductStore(db)
	call := func(userID uint) map[string]any {
		handler := &channeltransport.Handler{Dependencies: channeltransport.Dependencies{
			ProductRepo:        productRepo,
			ProductService:     productapplication.NewService(productapplication.Options{Products: productRepo}),
			SettingService:     settingsapp.NewService(settingsstore.New(db)),
			UserAuthService:    exclusiveIdentity{userID: userID},
			MemberLevelService: exclusiveLevels{exclusiveUserID: 7},
			PromotionService:   fixedPromotion{},
		}}
		gin.SetMode(gin.TestMode)
		router := gin.New()
		router.GET("/p/:id", handler.GetProductDetail)
		req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/p/%d?channel_user_id=1", product.ID), nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
		var payload channelCatalogTestResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return payload.Data
	}

	hit := call(7)
	hitSKU := hit["skus"].([]any)[0].(map[string]any)
	if hit["member_price_from"] != "95.00" || hitSKU["member_price"] != "95.00" {
		t.Fatalf("exclusive price missing: %v / %v", hit["member_price_from"], hitSKU["member_price"])
	}
	if v, ok := hit["promotion_price_from"]; ok && v != "" {
		t.Fatalf("promotion_price_from must be hidden, got %v", v)
	}
	if v, ok := hitSKU["promotion_price"]; ok && v != "" {
		t.Fatalf("sku promotion_price must be hidden, got %v", v)
	}

	miss := call(8)
	missSKU := miss["skus"].([]any)[0].(map[string]any)
	if miss["promotion_price_from"] != "80.00" || missSKU["promotion_price"] != "80.00" {
		t.Fatalf("promotion price should remain: %v / %v", miss["promotion_price_from"], missSKU["promotion_price"])
	}
}
