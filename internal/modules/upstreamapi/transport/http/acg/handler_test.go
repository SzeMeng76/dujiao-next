package acg

// handler_test.go — acg 供货接口的契约测试。
// 重点覆盖两件事：
//  1. 验签与签名串口径（排序、剔除空值、排除 sign）——错一个字符整站对接失败；
//  2. 下单幂等与收据返回——重复 request_no 不能二次扣款。

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/dujiao-next/internal/constants"
	apicredentialdomain "github.com/dujiao-next/internal/modules/apicredential/domain"
	categorydomain "github.com/dujiao-next/internal/modules/catalog/category/domain"
	productdomain "github.com/dujiao-next/internal/modules/catalog/product/domain"
	downstreamcallbackdomain "github.com/dujiao-next/internal/modules/downstreamcallback/domain"
	fulfillmentdomain "github.com/dujiao-next/internal/modules/fulfillment/domain"
	userdomain "github.com/dujiao-next/internal/modules/identity/user/domain"
	orderdomain "github.com/dujiao-next/internal/modules/order/domain"
	upstreamhttp "github.com/dujiao-next/internal/modules/upstreamapi/transport/http"
	"github.com/dujiao-next/internal/shared/jsonmap"
	"github.com/dujiao-next/internal/shared/money"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

func testSign(params map[string]string, secret string) string {
	keys := make([]string, 0, len(params))
	for key, value := range params {
		if key == "sign" || value == "" {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	pairs := make([]string, 0, len(keys))
	for _, key := range keys {
		pairs = append(pairs, key+"="+params[key])
	}
	sum := md5.Sum([]byte(strings.Join(pairs, "&") + "&key=" + secret))
	return hex.EncodeToString(sum[:])
}

func TestSignValid(t *testing.T) {
	secret := "app-secret"
	base := map[string]string{"app_id": "key-1", "num": "2", "shared_code": "101"}

	t.Run("valid signature", func(t *testing.T) {
		params := map[string]string{"app_id": "key-1", "num": "2", "shared_code": "101", "sign": "ignored"}
		if !signValid(params, secret, testSign(base, secret)) {
			t.Fatal("expected signature to verify")
		}
	})

	t.Run("order does not matter", func(t *testing.T) {
		params := map[string]string{"shared_code": "101", "app_id": "key-1", "num": "2"}
		if !signValid(params, secret, testSign(base, secret)) {
			t.Fatal("parameter insertion order must not affect the signature")
		}
	})

	t.Run("empty values excluded", func(t *testing.T) {
		params := map[string]string{"app_id": "key-1", "num": "2", "shared_code": "101", "note": ""}
		if !signValid(params, secret, testSign(base, secret)) {
			t.Fatal("empty string values must be excluded from the signature")
		}
	})

	t.Run("wrong secret rejected", func(t *testing.T) {
		params := map[string]string{"app_id": "key-1", "num": "2", "shared_code": "101"}
		if signValid(params, secret, testSign(params, "another-secret")) {
			t.Fatal("signature computed with a different secret must be rejected")
		}
	})

	t.Run("tampered field rejected", func(t *testing.T) {
		params := map[string]string{"app_id": "key-1", "num": "9", "shared_code": "101"}
		if signValid(params, secret, testSign(base, secret)) {
			t.Fatal("tampered quantity must invalidate the signature")
		}
	})
}

func TestLocalizedText(t *testing.T) {
	tests := []struct {
		name  string
		input map[string]interface{}
		want  string
	}{
		{"zh-CN first", map[string]interface{}{"zh-CN": "简体", "en-US": "English"}, "简体"},
		{"fallback zh-TW", map[string]interface{}{"zh-TW": "繁體"}, "繁體"},
		{"fallback en-US", map[string]interface{}{"en-US": "English"}, "English"},
		{"fallback any", map[string]interface{}{"ja-JP": "日本語"}, "日本語"},
		{"empty map", map[string]interface{}{}, ""},
		{"blank skipped", map[string]interface{}{"zh-CN": "   ", "en-US": "English"}, "English"},
	}
	for _, tc := range tests {
		if got := localizedText(tc.input); got != tc.want {
			t.Errorf("%s: want %q got %q", tc.name, tc.want, got)
		}
	}
}

func TestSingleActiveSKU(t *testing.T) {
	tests := []struct {
		name   string
		skus   []productdomain.ProductSKU
		wantID uint
		wantOK bool
	}{
		{"single active", []productdomain.ProductSKU{{ID: 7, IsActive: true}}, 7, true},
		{"ignore inactive", []productdomain.ProductSKU{{ID: 7, IsActive: true}, {ID: 8, IsActive: false}}, 7, true},
		{"no active", []productdomain.ProductSKU{{ID: 7, IsActive: false}}, 0, false},
		{"multiple active unsupported", []productdomain.ProductSKU{{ID: 7, IsActive: true}, {ID: 8, IsActive: true}}, 0, false},
		{"no sku", nil, 0, false},
	}
	for _, tc := range tests {
		product := &productdomain.Product{SKUs: tc.skus}
		sku, ok := singleActiveSKU(product)
		if ok != tc.wantOK {
			t.Errorf("%s: want ok=%v got %v", tc.name, tc.wantOK, ok)
			continue
		}
		if ok && sku.ID != tc.wantID {
			t.Errorf("%s: want sku %d got %d", tc.name, tc.wantID, sku.ID)
		}
	}
}

// ── 处理器测试 ─────────────────────────────────────────────

type fakeCredentials struct {
	credential *apicredentialdomain.ApiCredential
}

func (s fakeCredentials) GetByApiKey(apiKey string) (*apicredentialdomain.ApiCredential, error) {
	if s.credential == nil || s.credential.ApiKey != apiKey {
		return nil, nil
	}
	return s.credential, nil
}

func (fakeCredentials) TouchLastUsedAt(uint, time.Time) error { return nil }

type fakeUsers struct{ user *userdomain.User }

func (s fakeUsers) GetByID(uint) (*userdomain.User, error) { return s.user, nil }

type fakeSettings struct{}

func (fakeSettings) GetByKey(string) (jsonmap.JSON, error) { return jsonmap.JSON{}, nil }

func (fakeSettings) GetSiteCurrency(string) (string, error) { return "CNY", nil }

type fakeProducts struct {
	product   *productdomain.Product
	listCalls int
}

func (s *fakeProducts) GetAdminByID(string) (*productdomain.Product, error) { return s.product, nil }

func (s *fakeProducts) ListForUpstreamSync(_ *time.Time, _ bool, _ int, _ int) ([]productdomain.Product, int64, error) {
	s.listCalls++
	if s.listCalls > 1 {
		return nil, 0, nil
	}
	return []productdomain.Product{*s.product}, 1, nil
}

func (*fakeProducts) ApplyAutoStockCounts([]productdomain.Product) error { return nil }

type fakeCategories struct{}

func (fakeCategories) List() ([]categorydomain.Category, error) {
	return []categorydomain.Category{{ID: 3, NameJSON: jsonmap.JSON{"zh-CN": "卡密"}}}, nil
}

type fakeRefs struct {
	refs map[string]*downstreamcallbackdomain.OrderRef
}

func (s *fakeRefs) Create(ref *downstreamcallbackdomain.OrderRef) error {
	if s.refs == nil {
		s.refs = map[string]*downstreamcallbackdomain.OrderRef{}
	}
	s.refs[ref.DownstreamOrderNo] = ref
	return nil
}

func (s *fakeRefs) GetByCredentialAndDownstreamNo(_ uint, orderNo string) (*downstreamcallbackdomain.OrderRef, error) {
	if s.refs == nil {
		return nil, nil
	}
	return s.refs[orderNo], nil
}

type fakeOrders struct {
	order      *orderdomain.Order
	createCall int
}

func (s *fakeOrders) CreateOrder(upstreamhttp.CreateOrderInput) (*orderdomain.Order, error) {
	s.createCall++
	return s.order, nil
}

func (s *fakeOrders) GetOrderByUser(uint, uint) (*orderdomain.Order, error) { return s.order, nil }

func (s *fakeOrders) CancelOrder(uint, uint) (*orderdomain.Order, error) { return s.order, nil }

func (s *fakeOrders) BuildLocalRefundRecordsForOrder(*orderdomain.Order) ([]jsonmap.JSON, error) {
	return nil, nil
}

type fakePayments struct{ calls int }

func (s *fakePayments) CreatePayment(upstreamhttp.CreatePaymentInput) (*upstreamhttp.CreatePaymentResult, error) {
	s.calls++
	return &upstreamhttp.CreatePaymentResult{OrderPaid: true}, nil
}

type fakeFulfillment struct{ calls int }

func (s *fakeFulfillment) CreateAuto(uint) (*fulfillmentdomain.Fulfillment, error) {
	s.calls++
	return &fulfillmentdomain.Fulfillment{Status: constants.FulfillmentStatusDelivered, Payload: "CARD-1"}, nil
}

type fakeWallet struct{}

func newTestHandler(t *testing.T) (*Handler, *fakeOrders, *fakePayments, *fakeFulfillment, *fakeProducts) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	orders := &fakeOrders{order: &orderdomain.Order{
		ID:          42,
		OrderNo:     "LOCAL-42",
		Status:      constants.OrderStatusDelivered,
		TotalAmount: money.FromDecimal(decimal.NewFromInt(10)),
		Fulfillment: &fulfillmentdomain.Fulfillment{
			Status:  constants.FulfillmentStatusDelivered,
			Payload: "CARD-1\nCARD-2",
		},
	}}
	payments := &fakePayments{}
	fulfiller := &fakeFulfillment{}
	products := &fakeProducts{product: &productdomain.Product{
		ID:              101,
		CategoryID:      3,
		TitleJSON:       jsonmap.JSON{"zh-CN": "测试卡密"},
		PriceAmount:     money.FromDecimal(decimal.NewFromInt(10)),
		FulfillmentType: constants.FulfillmentTypeAuto,
		IsActive:        true,
		SKUs: []productdomain.ProductSKU{{
			ID:          201,
			ProductID:   101,
			IsActive:    true,
			PriceAmount: money.FromDecimal(decimal.NewFromInt(10)),
		}},
	}}

	base := &upstreamhttp.Handler{Dependencies: upstreamhttp.Dependencies{
		Categories:     fakeCategories{},
		Products:       products,
		Users:          fakeUsers{user: &userdomain.User{ID: 9, Status: constants.UserStatusActive}},
		Orders:         orders,
		Payments:       payments,
		DownstreamRefs: &fakeRefs{},
		Settings:       fakeSettings{},
	}}
	handler := New(base, fakeCredentials{credential: &apicredentialdomain.ApiCredential{
		ID:        5,
		UserID:    9,
		ApiKey:    "key-1",
		ApiSecret: "secret-1",
		Status:    constants.ApiCredentialStatusApproved,
		IsActive:  true,
	}}, fulfiller)
	return handler, orders, payments, fulfiller, products
}

func postForm(t *testing.T, handler gin.HandlerFunc, form url.Values) map[string]interface{} {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/shared", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = request
	handler(c)

	var payload map[string]interface{}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v body=%s", err, recorder.Body.String())
	}
	return payload
}

func signedForm(fields map[string]string) url.Values {
	form := url.Values{}
	for key, value := range fields {
		form.Set(key, value)
	}
	form.Set("sign", testSign(fields, "secret-1"))
	return form
}

func TestTradeRejectsBadSignature(t *testing.T) {
	handler, orders, payments, _, _ := newTestHandler(t)

	form := url.Values{"app_id": {"key-1"}, "shared_code": {"101"}, "num": {"1"}, "request_no": {"r-1"}, "sign": {"deadbeef"}}
	payload := postForm(t, handler.Trade, form)

	if payload["code"] != float64(0) {
		t.Fatalf("expected failure code, got %v", payload)
	}
	if payload["msg"] != msgBadSecret {
		t.Fatalf("expected %q, got %v", msgBadSecret, payload["msg"])
	}
	if orders.createCall != 0 || payments.calls != 0 {
		t.Fatal("rejected signature must not reach the money path")
	}
}

func TestTradeReturnsReceiptAndReplaysIdempotently(t *testing.T) {
	handler, orders, payments, fulfiller, _ := newTestHandler(t)

	form := signedForm(map[string]string{
		"app_id": "key-1", "shared_code": "101", "num": "1", "request_no": "r-1",
	})
	data, ok := postForm(t, handler.Trade, form)["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected receipt data, got %v", data)
	}
	if data["secret"] != "CARD-1\nCARD-2" {
		t.Fatalf("expected cards in receipt, got %v", data["secret"])
	}
	if data["tradeNo"] != "42" {
		t.Fatalf("expected tradeNo=42, got %v", data["tradeNo"])
	}
	if orders.createCall != 1 || payments.calls != 1 || fulfiller.calls != 1 {
		t.Fatalf("first call should create/pay/fulfill once, got create=%d pay=%d fulfill=%d",
			orders.createCall, payments.calls, fulfiller.calls)
	}

	if _, ok := postForm(t, handler.Trade, form)["data"].(map[string]interface{}); !ok {
		t.Fatal("replay should return the original receipt")
	}
	if orders.createCall != 1 || payments.calls != 1 {
		t.Fatalf("replay must not create or pay again, got create=%d pay=%d", orders.createCall, payments.calls)
	}
}

func TestTradeRejectsMultiSKUProduct(t *testing.T) {
	handler, orders, payments, _, products := newTestHandler(t)
	products.product.SKUs = append(products.product.SKUs,
		productdomain.ProductSKU{ID: 202, ProductID: 101, IsActive: true})

	form := signedForm(map[string]string{
		"app_id": "key-1", "shared_code": "101", "num": "1", "request_no": "r-2",
	})
	payload := postForm(t, handler.Trade, form)
	if payload["msg"] != msgUnavailable {
		t.Fatalf("multi-SKU product must be unavailable, got %v", payload["msg"])
	}
	if orders.createCall != 0 || payments.calls != 0 {
		t.Fatal("unsupported product must not reach the money path")
	}
}

// 上游对接商品与人工发货商品都走异步交付，无法满足 acg 的同步返回卡密要求，
// 因此不得出现在目录里，也不能被下单——否则下游只会拿到可重试错误并无限重试。
func TestCatalogExcludesNonAutoDeliveryProducts(t *testing.T) {
	for _, fulfillmentType := range []string{
		constants.FulfillmentTypeUpstream,
		constants.FulfillmentTypeManual,
	} {
		t.Run(fulfillmentType, func(t *testing.T) {
			handler, orders, payments, _, products := newTestHandler(t)
			products.product.FulfillmentType = fulfillmentType

			tree, ok := postForm(t, handler.Items, signedForm(map[string]string{"app_id": "key-1"}))["data"].([]interface{})
			if !ok {
				t.Fatalf("expected category tree, got %v", tree)
			}
			listed := 0
			for _, node := range tree {
				category, _ := node.(map[string]interface{})
				children, _ := category["children"].([]interface{})
				listed += len(children)
			}
			if listed != 0 {
				t.Fatalf("%s product must not be listed, got %d entries", fulfillmentType, listed)
			}

			payload := postForm(t, handler.Trade, signedForm(map[string]string{
				"app_id": "key-1", "shared_code": "101", "num": "1", "request_no": "r-3",
			}))
			if payload["msg"] != msgUnavailable {
				t.Fatalf("%s product must be unavailable, got %v", fulfillmentType, payload["msg"])
			}
			if orders.createCall != 0 || payments.calls != 0 {
				t.Fatal("non-auto product must not reach the money path")
			}
		})
	}
}

func TestItemsOnlyListsSingleSKUAutoProducts(t *testing.T) {
	handler, _, _, _, products := newTestHandler(t)

	tree, ok := postForm(t, handler.Items, signedForm(map[string]string{"app_id": "key-1"}))["data"].([]interface{})
	if !ok || len(tree) == 0 {
		t.Fatalf("expected category tree, got %v", tree)
	}
	category, _ := tree[0].(map[string]interface{})
	children, _ := category["children"].([]interface{})
	if len(children) != 1 {
		t.Fatalf("expected one purchasable product, got %v", children)
	}

	// 多一个在售 SKU 后该商品必须从目录消失，避免下游导入后无法下单。
	products.product.SKUs = append(products.product.SKUs,
		productdomain.ProductSKU{ID: 202, ProductID: 101, IsActive: true})
	products.listCalls = 0
	tree, ok = postForm(t, handler.Items, signedForm(map[string]string{"app_id": "key-1"}))["data"].([]interface{})
	if !ok {
		t.Fatalf("expected category tree, got %v", tree)
	}
	category, _ = tree[0].(map[string]interface{})
	children, _ = category["children"].([]interface{})
	if len(children) != 0 {
		t.Fatalf("multi-SKU product must not be listed, got %v", children)
	}
}
