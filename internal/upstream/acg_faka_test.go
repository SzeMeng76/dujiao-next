package upstream

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	siteconnectiondomain "github.com/dujiao-next/internal/modules/siteconnection/domain"
)

// 签名必须与入站 signValid 完全一致：键名升序、剔除空值与 sign 本身，
// k=v 用 & 拼接后追加 &key=<app_key>，取 md5 小写。
func TestAcgFakaSign(t *testing.T) {
	params := map[string]string{
		"app_id":      "K",
		"num":         "1",
		"request_no":  "R",
		"shared_code": "7",
		"empty":       "",
		"sign":        "should-be-ignored",
	}
	const want = "009176ff7535029be83d49968d417a6b"
	if got := acgFakaSign(params, "S"); got != want {
		t.Fatalf("sign mismatch: got %s want %s", got, want)
	}
}

// 用户可能填站点根地址，也可能直接粘贴 /shared 供货地址。
func TestAcgFakaNormalizeBaseURL(t *testing.T) {
	cases := map[string]string{
		"https://shop.example":            "https://shop.example",
		"https://shop.example/":           "https://shop.example",
		"https://shop.example/shared":     "https://shop.example",
		"https://shop.example/shared/":    "https://shop.example",
		" https://shop.example// ":        "https://shop.example",
		"https://shop.example/api/shared": "https://shop.example/api",
	}
	for input, want := range cases {
		if got := normalizeAcgBaseURL(input); got != want {
			t.Fatalf("normalize %q: got %q want %q", input, want, got)
		}
	}
}

// acgTestCatalog 真实站点的形状：商品 code 是 16 位十六进制，不是数字。
const acgTestCatalog = `{"code":200,"msg":"success","data":[
	{"id":1,"name":"分类A","children":[
		{"id":7,"code":"1C5CFB8023144BFB","name":"商品七","price":"9.9","description":"说明","cover":"https://cdn.example/a.png","status":1,"stock":5},
		{"id":8,"code":"E6F59FD14374DAC5","name":"售罄","price":"1","status":1,"stock":0},
		{"id":9,"code":"58310DE1BCC3EFB4","name":"已下架","price":"2","status":0,"stock":-1},
		{"id":0,"code":"","name":"无编号脏数据","price":"1","status":1,"stock":1}
	]},
	{"id":0,"name":"默认分类","children":[]}
]}`

func newAcgTestAdapter(t *testing.T, handler http.HandlerFunc) *AcgFakaAdapter {
	t.Helper()
	disableRequestPacing(t)
	restoreBackoff := acgThrottleBackoff
	acgThrottleBackoff = 0
	t.Cleanup(func() { acgThrottleBackoff = restoreBackoff })
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return NewAcgFakaAdapter(&siteconnectiondomain.Connection{
		BaseURL:   server.URL,
		ApiKey:    "K",
		ApiSecret: "S",
	}, t.TempDir())
}

func TestAcgFakaPing(t *testing.T) {
	adapter := newAcgTestAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/shared/authentication/connect" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"code":200,"msg":"success","data":{"shopName":"上游店","balance":12.5}}`))
	})

	result, err := adapter.Ping(context.Background())
	if err != nil {
		t.Fatalf("ping: %v", err)
	}
	if result.SiteName != "上游店" {
		t.Fatalf("site name = %q", result.SiteName)
	}
	if result.Balance != "12.50" {
		t.Fatalf("balance = %q", result.Balance)
	}
	if result.Currency != "CNY" {
		t.Fatalf("currency = %q", result.Currency)
	}
}

// acg 目录是「分类 → children 商品」的树，一个商品只有一条隐式 SKU。
// 身份用数字 id（映射表只存 uint），真实商品编号 code 放进 SKUCode。
// 关键是 code 不是纯数字时不能被丢掉——真实站点全是 16 位十六进制。
func TestAcgFakaListProducts(t *testing.T) {
	adapter := newAcgTestAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/shared/commodity/items" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(acgTestCatalog))
	})

	result, err := adapter.ListProducts(context.Background(), ListProductsOpts{Page: 1, PageSize: 200})
	if err != nil {
		t.Fatalf("list products: %v", err)
	}
	if result.Total != 3 || len(result.Items) != 3 {
		t.Fatalf("expected 3 products, got total=%d len=%d", result.Total, len(result.Items))
	}

	first := result.Items[0]
	if first.ID != 7 || first.CategoryID != 1 {
		t.Fatalf("unexpected first product %+v", first)
	}
	if first.PriceAmount != "9.90" {
		t.Fatalf("price = %q", first.PriceAmount)
	}
	if first.Title["zh-CN"] != "商品七" {
		t.Fatalf("title = %+v", first.Title)
	}
	// acg 只有一个 description 列，语义是商品详情：要进本站的「详情」，不能进「简介」。
	if first.Content["zh-CN"] != "说明" {
		t.Fatalf("description must land in content, got content=%+v", first.Content)
	}
	if len(first.Description) != 0 {
		t.Fatalf("intro must stay empty, got %+v", first.Description)
	}
	if len(first.SKUs) != 1 || first.SKUs[0].ID != 7 {
		t.Fatalf("sku should reuse product id: %+v", first.SKUs)
	}
	if first.SKUs[0].SKUCode != "1C5CFB8023144BFB" {
		t.Fatalf("sku code must be preserved verbatim, got %q", first.SKUs[0].SKUCode)
	}
	if first.SKUs[0].StockStatus != "in_stock" || first.SKUs[0].StockQuantity != 5 {
		t.Fatalf("unexpected stock %+v", first.SKUs[0])
	}
	if !first.IsActive {
		t.Fatal("expected active product")
	}

	if result.Items[1].SKUs[0].StockStatus != "out_of_stock" {
		t.Fatalf("expected out_of_stock, got %+v", result.Items[1].SKUs[0])
	}
	if result.Items[2].IsActive || result.Items[2].SKUs[0].StockStatus != "unlimited" {
		t.Fatalf("expected inactive unlimited product, got %+v", result.Items[2])
	}
}

// 配置了「种类」的商品暂时不收录：下单必须按种类传 race，
// 不带 race 会按未选种类处理，等于悄悄买错档位。
func TestAcgFakaSkipsCategorizedProducts(t *testing.T) {
	adapter := newAcgTestAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":200,"msg":"success","data":[
			{"id":1,"name":"分类A","children":[
				{"id":7,"code":"AAA","name":"单规格","price":"9.9","status":1,"stock":5,"config":""},
				{"id":8,"code":"BBB","name":"有种类INI","price":"9.9","status":1,"stock":5,"config":"[category]\n标准=9.90\n高级=19.90\n"},
				{"id":9,"code":"CCC","name":"有种类对象","price":"9.9","status":1,"stock":5,"config":{"category":{"标准":"9.90"}}},
				{"id":10,"code":"DDD","name":"只有规格","price":"9.9","status":1,"stock":5,"config":"[sku]\n颜色.黑色=0\n"}
			]}
		]}`))
	})

	result, err := adapter.ListProducts(context.Background(), ListProductsOpts{Page: 1, PageSize: 200})
	if err != nil {
		t.Fatalf("list products: %v", err)
	}
	if len(result.Items) != 2 {
		t.Fatalf("expected 2 importable products, got %d", len(result.Items))
	}
	if result.Items[0].ID != 7 || result.Items[1].ID != 10 {
		t.Fatalf("unexpected product ids: %d, %d", result.Items[0].ID, result.Items[1].ID)
	}
}

func TestAcgFakaConfigCategories(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want int
	}{
		{"empty", `null`, 0},
		{"blank string", `""`, 0},
		{"no category", `"[sku]\n颜色.黑色=0\n"`, 0},
		{"ini two", `"[category]\n标准=9.90\n高级=19.90\n"`, 2},
		{"ini whitespace", `"\r\n  [category]  \r\n  标准 = 9.90  \r\n"`, 1},
		{"object one", `{"category":{"标准":"9.90"}}`, 1},
		{"object empty", `{"category":{}}`, 0},
		{"object no category", `{"sku":{}}`, 0},
	}
	for _, tc := range cases {
		if got := acgConfigCategories([]byte(tc.raw)); got != tc.want {
			t.Fatalf("%s: got %d want %d", tc.name, got, tc.want)
		}
	}
}

// acg 的 items 不分页，第 2 页起返回空，让调用方翻页循环正常收尾。
func TestAcgFakaListProductsSecondPageEmpty(t *testing.T) {
	adapter := newAcgTestAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("page 2 should not hit upstream")
	})
	result, err := adapter.ListProducts(context.Background(), ListProductsOpts{Page: 2, PageSize: 200})
	if err != nil {
		t.Fatalf("list products: %v", err)
	}
	if len(result.Items) != 0 {
		t.Fatalf("expected empty page, got %d items", len(result.Items))
	}
}

// 单品同步按数字 id 在目录里找，而不是拿 id 去当 code 查。
func TestAcgFakaGetProductByID(t *testing.T) {
	adapter := newAcgTestAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(acgTestCatalog))
	})

	product, err := adapter.GetProduct(context.Background(), 7)
	if err != nil {
		t.Fatalf("get product: %v", err)
	}
	if product.ID != 7 || product.SKUs[0].SKUCode != "1C5CFB8023144BFB" {
		t.Fatalf("unexpected product %+v", product)
	}

	if _, err := adapter.GetProduct(context.Background(), 999); !errors.Is(err, ErrUpstreamProductUnavailable) {
		t.Fatalf("expected ErrUpstreamProductUnavailable, got %v", err)
	}
}

// 下单必须用商品的 code（不是 id）作为 shared_code，并带 acg 约定的表单字段与有效签名；
// 响应里的 secret 要转成交付内容。
func TestAcgFakaCreateOrderReturnsFulfillment(t *testing.T) {
	adapter := newAcgTestAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/shared/commodity/items":
			_, _ = w.Write([]byte(acgTestCatalog))
		case "/shared/commodity/trade":
			if err := r.ParseForm(); err != nil {
				t.Fatalf("parse form: %v", err)
			}
			form := map[string]string{}
			for key := range r.PostForm {
				form[key] = r.PostForm.Get(key)
			}
			if form["app_id"] != "K" {
				t.Errorf("app_id = %q", form["app_id"])
			}
			if form["shared_code"] != "1C5CFB8023144BFB" {
				t.Errorf("shared_code must be the commodity code, got %q", form["shared_code"])
			}
			if form["num"] != "2" || form["request_no"] != "R1" {
				t.Errorf("unexpected business fields %+v", form)
			}
			if want := acgFakaSign(form, "S"); form["sign"] != want {
				t.Errorf("sign mismatch: got %q want %q", form["sign"], want)
			}
			_, _ = w.Write([]byte(`{"code":200,"msg":"success","data":{"url":null,"amount":19.8,"tradeNo":"123","secret":"卡密1\n卡密2"}}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})

	resp, err := adapter.CreateOrder(context.Background(), CreateUpstreamOrderReq{
		SKUID: 7, Quantity: 2, DownstreamOrderNo: "R1",
	})
	if err != nil {
		t.Fatalf("create order: %v", err)
	}
	if !resp.OK || resp.OrderID != 123 || resp.OrderNo != "123" {
		t.Fatalf("unexpected response %+v", resp)
	}
	if resp.Amount != "19.80" {
		t.Fatalf("amount = %q", resp.Amount)
	}
	if resp.Status != "delivered" {
		t.Fatalf("status = %q", resp.Status)
	}
	if resp.Fulfillment == nil || resp.Fulfillment.Payload != "卡密1\n卡密2" {
		t.Fatalf("fulfillment = %+v", resp.Fulfillment)
	}
	if resp.Fulfillment.Status != "delivered" || resp.Fulfillment.DeliveredAt == nil {
		t.Fatalf("fulfillment not marked delivered: %+v", resp.Fulfillment)
	}
}

// 未交付时 acg 回 code:0，应作为「可重试业务失败」返回而不是错误，
// 调用方会用同一 request_no 重试并命中原订单幂等。
func TestAcgFakaCreateOrderPendingIsRetryable(t *testing.T) {
	adapter := newAcgTestAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/shared/commodity/items" {
			_, _ = w.Write([]byte(acgTestCatalog))
			return
		}
		_, _ = w.Write([]byte(`{"code":0,"msg":"订单未完成，请检查库存、规格或余额后再试"}`))
	})

	resp, err := adapter.CreateOrder(context.Background(), CreateUpstreamOrderReq{
		SKUID: 7, Quantity: 1, DownstreamOrderNo: "R1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.OK {
		t.Fatal("expected business failure")
	}
	if resp.Fulfillment != nil {
		t.Fatal("pending order must not carry fulfillment")
	}
	if resp.ErrorCode != "acg_upstream_rejected" {
		t.Fatalf("error code = %q", resp.ErrorCode)
	}
}

// 上游目录里已经没有这个商品时，不能发 trade 去撞「商品不存在」，直接按不可购返回。
func TestAcgFakaCreateOrderUnknownProduct(t *testing.T) {
	adapter := newAcgTestAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/shared/commodity/trade" {
			t.Fatal("trade must not be called for an unknown product")
		}
		_, _ = w.Write([]byte(acgTestCatalog))
	})

	resp, err := adapter.CreateOrder(context.Background(), CreateUpstreamOrderReq{
		SKUID: 999, Quantity: 1, DownstreamOrderNo: "R1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.OK || resp.ErrorCode != "product_unavailable" {
		t.Fatalf("unexpected response %+v", resp)
	}
}

// 余额不足 / 库存不足 / 下架 / 数量越界要归类成采购管道的既有错误码。
func TestAcgFakaErrorCodeMapping(t *testing.T) {
	cases := map[string]string{
		"余额不足":                      "insufficient_balance",
		"餘額不足":                      "insufficient_balance",
		"库存不足":                      "product_out_of_stock",
		"商品不存在或已下架":                 "product_unavailable",
		"低于最少购买数量":                  "invalid_request",
		"超过最多购买数量":                  "invalid_request",
		"服务器频繁请联系客服支持":              "rate_limited",
		"request ID already exists": "duplicate_order",
		"其他未知错误":                    "acg_upstream_rejected",
	}
	for message, want := range cases {
		if got := acgFakaErrorCode(message); got != want {
			t.Fatalf("message %q: got %q want %q", message, got, want)
		}
	}
}

func TestAcgFakaGetOrder(t *testing.T) {
	adapter := newAcgTestAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/shared/commodity/query" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		switch r.PostForm.Get("tradeNo") {
		case "123":
			_, _ = w.Write([]byte(`{"code":200,"msg":"success","data":{"secret":"卡密1","status":1}}`))
		case "456":
			_, _ = w.Write([]byte(`{"code":200,"msg":"success","data":{"secret":"","status":0}}`))
		default:
			t.Errorf("unexpected tradeNo %q", r.PostForm.Get("tradeNo"))
		}
	})

	delivered, err := adapter.GetOrder(context.Background(), 123)
	if err != nil {
		t.Fatalf("get order: %v", err)
	}
	if delivered.Status != "delivered" || delivered.Fulfillment == nil || delivered.Fulfillment.Payload != "卡密1" {
		t.Fatalf("unexpected delivered detail %+v", delivered)
	}

	pending, err := adapter.GetOrder(context.Background(), 456)
	if err != nil {
		t.Fatalf("get order: %v", err)
	}
	if pending.Status != "pending" || pending.Fulfillment != nil {
		t.Fatalf("unexpected pending detail %+v", pending)
	}
}

// acg 协议没有取消接口，必须返回哨兵错误让上层按「上游拒绝取消」处理，不再重试。
func TestAcgFakaCancelNotAllowed(t *testing.T) {
	adapter := newAcgTestAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("cancel must not hit upstream")
	})
	err := adapter.CancelOrder(context.Background(), 1)
	if !errors.Is(err, ErrUpstreamCancelNotAllowed) {
		t.Fatalf("expected ErrUpstreamCancelNotAllowed, got %v", err)
	}
}

// 上游返回非 200 时要带状态码返回结构化错误，供日志与重试判断使用。
// 上游限流说明请求没有被处理：应退避后重发，而不是直接判失败。
func TestAcgFakaRetriesWhenThrottled(t *testing.T) {
	var calls int
	adapter := newAcgTestAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			_, _ = w.Write([]byte(`{"code":0,"msg":"服务器频繁请联系客服支持"}`))
			return
		}
		_, _ = w.Write([]byte(`{"code":200,"msg":"success","data":{"shopName":"店","balance":1.5}}`))
	})

	result, err := adapter.Ping(context.Background())
	if err != nil {
		t.Fatalf("expected retry to succeed, got %v", err)
	}
	if calls != 2 {
		t.Fatalf("expected 2 attempts, got %d", calls)
	}
	if result.SiteName != "店" {
		t.Fatalf("site name = %q", result.SiteName)
	}
}

// 持续限流时重试有上限，最终按业务失败返回，调用方能看到真实原因。
func TestAcgFakaThrottleGivesUp(t *testing.T) {
	adapter := newAcgTestAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":0,"msg":"服务器频繁请联系客服支持"}`))
	})

	_, err := adapter.Ping(context.Background())
	var biz *acgBizError
	if !errors.As(err, &biz) {
		t.Fatalf("expected acgBizError, got %v", err)
	}
	if !isAcgThrottled(biz.Message) {
		t.Fatalf("expected throttle message, got %q", biz.Message)
	}
}

func TestAcgFakaHTTPError(t *testing.T) {
	adapter := newAcgTestAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"code":401,"reason":"invalid_signature"}`))
	})
	_, err := adapter.Ping(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	var httpErr *upstreamHTTPError
	if !errors.As(err, &httpErr) || httpErr.Status != http.StatusUnauthorized {
		t.Fatalf("expected 401 upstreamHTTPError, got %v", err)
	}
	if !strings.Contains(err.Error(), "401") {
		t.Fatalf("error should mention status: %v", err)
	}
}
