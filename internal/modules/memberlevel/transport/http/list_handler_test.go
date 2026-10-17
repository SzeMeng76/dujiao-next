package memberlevelhttp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	memberleveldomain "github.com/dujiao-next/internal/modules/memberlevel/domain"
	"github.com/dujiao-next/internal/shared/jsonmap"
	"github.com/dujiao-next/internal/shared/money"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

func TestResolveLocalizedJSONPrefersLocaleThenDefault(t *testing.T) {
	m := jsonmap.JSON{
		"en-US": "Gold",
		"zh-CN": "黄金",
	}
	if got := resolveLocalizedJSON(m, "en-US", "zh-CN"); got != "Gold" {
		t.Fatalf("locale prefer: got %q", got)
	}
	if got := resolveLocalizedJSON(m, "ja-JP", "zh-CN"); got != "黄金" {
		t.Fatalf("default prefer: got %q", got)
	}
	if got := resolveLocalizedJSON(jsonmap.JSON{}, "zh-CN", "zh-CN"); got != "" {
		t.Fatalf("empty map: got %q", got)
	}
}

type stubUserPriceReader struct {
	gotUserID uint
	prices    []memberleveldomain.UserProductPrice
}

func (r *stubUserPriceReader) ListUserPrices(userID uint, _, _ int) ([]memberleveldomain.UserProductPrice, int64, error) {
	r.gotUserID = userID
	return r.prices, int64(len(r.prices)), nil
}

// 前台只能读到当前登录用户自己的专属价，且响应不泄露其他字段。
func TestListMyPricesReturnsOnlyCurrentUserPrices(t *testing.T) {
	gin.SetMode(gin.TestMode)
	reader := &stubUserPriceReader{prices: []memberleveldomain.UserProductPrice{
		{ID: 1, UserID: 7, ProductID: 18, SKUID: 19, PriceAmount: money.FromDecimal(decimal.NewFromInt(95))},
	}}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/me/product-prices", nil)
	c.Set("user_id", uint(7))

	NewUserHandler(reader).ListMyPrices(c)

	if reader.gotUserID != 7 {
		t.Fatalf("reader queried user %d, want 7", reader.gotUserID)
	}
	var resp struct {
		Data []map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v body=%s", err, recorder.Body.String())
	}
	if len(resp.Data) != 1 {
		t.Fatalf("got %d rows, want 1: %s", len(resp.Data), recorder.Body.String())
	}
	row := resp.Data[0]
	if row["product_id"] != float64(18) || row["sku_id"] != float64(19) {
		t.Fatalf("unexpected row: %v", row)
	}
	if _, leaked := row["user_id"]; leaked {
		t.Fatalf("response must not expose user_id: %v", row)
	}
}

func TestListMyPricesRejectsMissingUser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	reader := &stubUserPriceReader{}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/me/product-prices", nil)

	NewUserHandler(reader).ListMyPrices(c)

	if reader.gotUserID != 0 {
		t.Fatalf("reader must not be called without a user, got %d", reader.gotUserID)
	}
}
