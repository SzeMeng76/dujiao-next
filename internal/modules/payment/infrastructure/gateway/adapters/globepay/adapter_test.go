package globepayadapter

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dujiao-next/internal/constants"
	paymentcontract "github.com/dujiao-next/internal/modules/payment/contract"
	"github.com/dujiao-next/internal/shared/jsonmap"
)

const (
	testPartnerCode    = "TESTPARTNER"
	testCredentialCode = "test-credential-code"
)

// globepayTestSign 复刻网关侧 generateSign：
// SHA256(partner_code&time&nonce_str&credential_code)。
func globepayTestSign(partnerCode string, timestamp int64, nonceStr, credentialCode string) string {
	raw := fmt.Sprintf("%s&%d&%s&%s", partnerCode, timestamp, nonceStr, credentialCode)
	sum := sha256.Sum256([]byte(raw))
	return strings.ToLower(hex.EncodeToString(sum[:]))
}

func globepayTestConfig() jsonmap.JSON {
	return jsonmap.JSON{
		"partner_code":    testPartnerCode,
		"credential_code": testCredentialCode,
	}
}

func globepayTestForm(partnerOrderID, orderID, realFee, currency string) map[string][]string {
	return map[string][]string{
		"time":             {"1794542108603"},
		"nonce_str":        {"5caD2MszzkKiuFSGYZoh66kX8Kq7eL"},
		"sign":             {globepayTestSign(testPartnerCode, 1794542108603, "5caD2MszzkKiuFSGYZoh66kX8Kq7eL", testCredentialCode)},
		"partner_order_id": {partnerOrderID},
		"order_id":         {orderID},
		"real_fee":         {realFee},
		"currency":         {currency},
	}
}

// TestGlobepayAdapter_VerifyCallback_SignatureAloneIsNotTrusted 守护
// 2026-11-13 伪造回调事故的根因防线。
//
// Globepay 的签名只覆盖 partner_code&time&nonce_str&credential_code，不含订单号与金额。
// 创建支付返回给买家的 pay_url 必须携带同一组签名（文档要求），因此攻击者能从自己的
// 订单页面抄下合法签名，把 partner_order_id 换成目标订单号即通过验签。
//
// 结论：验签通过不能作为到账凭据。这里显式断言"仅靠签名就能构造出通过验签的回调"，
// 从而把安全性锁定在 RequireCallbackQuery（官方订单状态回查）上——
// 如果有人误以为验签足够而删掉回查，这个测试会提醒他真实威胁模型。
func TestGlobepayAdapter_VerifyCallback_SignatureAloneIsNotTrusted(t *testing.T) {
	a := NewGlobepayAdapter().(*globepayAdapter)

	// 用同一组 time/nonce_str/sign，把订单号换成任意目标单，验签依然通过。
	form := globepayTestForm("DJP20261113115508698099", "DJP20261113115508698099", "57", "GBP")

	if _, err := a.VerifyCallback(globepayTestConfig(), form, nil); err != nil {
		t.Fatalf("precondition changed: callback with valid signature was rejected: %v", err)
	}

	confirmer, ok := NewGlobepayAdapter().(paymentcontract.GatewayCallbackConfirmer)
	if !ok {
		t.Fatal("globepay adapter must implement GatewayCallbackConfirmer")
	}
	if !confirmer.RequireCallbackQuery() {
		t.Fatal("globepay must require an official order query: signature does not cover order id or amount")
	}
}

// TestGlobepayAdapter_VerifyCallback_AcceptsDistinctOrderIDs 确认正常回调仍然通过：
// 签名正确、且 order_id 是网关独立流水号。
func TestGlobepayAdapter_VerifyCallback_AcceptsDistinctOrderIDs(t *testing.T) {
	a := NewGlobepayAdapter().(*globepayAdapter)

	form := globepayTestForm("DJP20261113115508698099", "202611131155086980991", "57", "GBP")

	result, err := a.VerifyCallback(globepayTestConfig(), form, nil)
	if err != nil {
		t.Fatalf("VerifyCallback() error = %v, want nil", err)
	}
	if result.OrderNo != "DJP20261113115508698099" {
		t.Fatalf("OrderNo = %q", result.OrderNo)
	}
	if result.ProviderRef != "202611131155086980991" {
		t.Fatalf("ProviderRef = %q", result.ProviderRef)
	}
	if result.Status != constants.PaymentStatusSuccess {
		t.Fatalf("Status = %q", result.Status)
	}
	if result.Amount.String() != "0.57" {
		t.Fatalf("Amount = %s, want 0.57", result.Amount.String())
	}
	if result.Currency != "GBP" {
		t.Fatalf("Currency = %q", result.Currency)
	}
}

// TestGlobepayAdapter_VerifyCallback_RejectsBadSignature 确认签名错误仍被拒绝。
func TestGlobepayAdapter_VerifyCallback_RejectsBadSignature(t *testing.T) {
	a := NewGlobepayAdapter().(*globepayAdapter)

	form := globepayTestForm("DJP20261113115508698099", "202611131155086980991", "57", "GBP")
	form["sign"] = []string{"deadbeef"}

	_, err := a.VerifyCallback(globepayTestConfig(), form, nil)
	if err == nil {
		t.Fatal("VerifyCallback() accepted a callback with an invalid signature")
	}
}

// TestGlobepayAdapter_RequireCallbackQuery 守护"Globepay 回调必须回查官方状态"的声明。
// 若有人删掉这个方法，伪造回调会重新变成可落库的支付。
func TestGlobepayAdapter_RequireCallbackQuery(t *testing.T) {
	a := NewGlobepayAdapter()

	confirmer, ok := a.(paymentcontract.GatewayCallbackConfirmer)
	if !ok {
		t.Fatal("globepay adapter must implement paymentcontract.GatewayCallbackConfirmer")
	}
	if !confirmer.RequireCallbackQuery() {
		t.Fatal("globepay must require an official order query to confirm callbacks")
	}
}

// TestGlobepayAdapter_QueryPayment_MapsOfficialStatus 守护官方订单状态回查的字段映射。
func TestGlobepayAdapter_QueryPayment_MapsOfficialStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if !strings.Contains(r.URL.Path, "/api/v1.0/gateway/partners/"+testPartnerCode+"/orders/DJP20261113115508698099") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("sign") == "" {
			t.Error("query sign is empty")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"return_code":      "SUCCESS",
			"result_code":      "PAY_SUCCESS",
			"order_id":         "202611131155086980991",
			"partner_order_id": "DJP20261113115508698099",
			"total_fee":        57,
			"real_fee":         57,
			"currency":         "GBP",
		})
	}))
	defer server.Close()

	a := NewGlobepayAdapter().(*globepayAdapter)
	raw := globepayTestConfig()
	raw["api_base_url"] = server.URL

	result, err := a.QueryPayment(context.Background(), raw, "DJP20261113115508698099")
	if err != nil {
		t.Fatalf("QueryPayment() error = %v", err)
	}
	if result.Status != constants.PaymentStatusSuccess {
		t.Fatalf("Status = %q, want success", result.Status)
	}
	if result.Amount.String() != "0.57" {
		t.Fatalf("Amount = %s, want 0.57", result.Amount.String())
	}
	if result.Currency != "GBP" {
		t.Fatalf("Currency = %q", result.Currency)
	}
	if result.ProviderRef != "202611131155086980991" {
		t.Fatalf("ProviderRef = %q", result.ProviderRef)
	}
}

// TestGlobepayAdapter_QueryPayment_RejectsOrderNotExist 确认查无此单时返回错误，
// 绝不能被当作"未支付"放行或被误判成已支付。
func TestGlobepayAdapter_QueryPayment_RejectsOrderNotExist(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"return_code": "ORDER_NOT_EXIST",
			"return_msg":  "Order does not exist",
		})
	}))
	defer server.Close()

	a := NewGlobepayAdapter().(*globepayAdapter)
	raw := globepayTestConfig()
	raw["api_base_url"] = server.URL

	if _, err := a.QueryPayment(context.Background(), raw, "DJP_NOTEXIST"); err == nil {
		t.Fatal("QueryPayment() must fail when the gateway reports ORDER_NOT_EXIST")
	}
}

// TestGlobepayAdapter_QueryPayment_UnpaidOrderIsNotSuccess 确认 PAYING 不会被当成成功。
func TestGlobepayAdapter_QueryPayment_UnpaidOrderIsNotSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"return_code":      "SUCCESS",
			"result_code":      "PAYING",
			"partner_order_id": "DJP_PAYING",
			"total_fee":        57,
			"real_fee":         0,
			"currency":         "GBP",
		})
	}))
	defer server.Close()

	a := NewGlobepayAdapter().(*globepayAdapter)
	raw := globepayTestConfig()
	raw["api_base_url"] = server.URL

	result, err := a.QueryPayment(context.Background(), raw, "DJP_PAYING")
	if err != nil {
		t.Fatalf("QueryPayment() error = %v", err)
	}
	if result.Status == constants.PaymentStatusSuccess {
		t.Fatal("PAYING must not map to success")
	}
}
