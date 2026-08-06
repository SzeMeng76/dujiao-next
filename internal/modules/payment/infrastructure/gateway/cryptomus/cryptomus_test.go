package cryptomus

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCreatePaymentPostsSignedInvoiceRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/payment" {
			t.Fatalf("path = %s, want /payment", r.URL.Path)
		}
		if r.Header.Get("merchant") != "merchant-1" {
			t.Fatalf("merchant header = %q, want merchant-1", r.Header.Get("merchant"))
		}
		if r.Header.Get("sign") != "c98b148549c7fffce7f4e04c636b0b5e" {
			t.Fatalf("sign header = %q, want c98b148549c7fffce7f4e04c636b0b5e", r.Header.Get("sign"))
		}
		var payload map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if payload["amount"] != "15" || payload["currency"] != "CNY" || payload["order_id"] != "ORDER-1" {
			t.Fatalf("unexpected request payload: %+v", payload)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"state": 0,
			"result": {
				"uuid": "88967899-674f-4c5f-9326-78584a0afeab",
				"order_id": "ORDER-1",
				"amount": "15.00",
				"currency": "CNY",
				"status": "check",
				"url": "https://pay.cryptomus.com/pay/88967899-674f-4c5f-9326-78584a0afeab",
				"expired_at": 1786009590
			}
		}`))
	}))
	defer server.Close()
	restore := useTestBaseURL(server.URL)
	defer restore()

	result, err := CreatePayment(context.Background(), &Config{
		MerchantID:    "merchant-1",
		PaymentAPIKey: "test-payment-api-key",
	}, CreateInput{
		OrderNo:  "ORDER-1",
		Amount:   "15",
		Currency: "CNY",
	})
	if err != nil {
		t.Fatalf("CreatePayment failed: %v", err)
	}
	if result.UUID != "88967899-674f-4c5f-9326-78584a0afeab" {
		t.Fatalf("UUID = %q", result.UUID)
	}
	if result.CheckoutURL != "https://pay.cryptomus.com/pay/88967899-674f-4c5f-9326-78584a0afeab" {
		t.Fatalf("CheckoutURL = %q", result.CheckoutURL)
	}
	if result.ExpiresAt != 1786009590 {
		t.Fatalf("ExpiresAt = %d", result.ExpiresAt)
	}
}

func TestCreatePaymentRejectsNonZeroState(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"state":1,"errors":{"order_id":["validation.required"]}}`))
	}))
	defer server.Close()
	restore := useTestBaseURL(server.URL)
	defer restore()

	_, err := CreatePayment(context.Background(), &Config{
		MerchantID:    "merchant-1",
		PaymentAPIKey: "test-payment-api-key",
	}, CreateInput{OrderNo: "ORDER-1", Amount: "15", Currency: "CNY"})
	if !errors.Is(err, ErrRequestFailed) {
		t.Fatalf("err = %v, want ErrRequestFailed", err)
	}
}

func TestQueryPaymentParsesResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/payment/info" {
			t.Fatalf("path = %s, want /payment/info", r.URL.Path)
		}
		var payload map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		if payload["uuid"] != "88967899-674f-4c5f-9326-78584a0afeab" {
			t.Fatalf("unexpected request payload: %+v", payload)
		}
		_, _ = w.Write([]byte(`{
			"state": 0,
			"result": {
				"uuid": "88967899-674f-4c5f-9326-78584a0afeab",
				"order_id": "ORDER-1",
				"amount": "15.00",
				"currency": "CNY",
				"status": "paid"
			}
		}`))
	}))
	defer server.Close()
	restore := useTestBaseURL(server.URL)
	defer restore()

	result, err := QueryPayment(context.Background(), &Config{
		MerchantID:    "merchant-1",
		PaymentAPIKey: "test-payment-api-key",
	}, "88967899-674f-4c5f-9326-78584a0afeab")
	if err != nil {
		t.Fatalf("QueryPayment failed: %v", err)
	}
	if ToPaymentStatus(result.Status) != "success" {
		t.Fatalf("mapped status = %q, want success", ToPaymentStatus(result.Status))
	}
}

// TestVerifyWebhookSignatureMatchesReferenceAlgorithm 用 Python 独立实现的
// MD5(base64(body)+apiKey) 算出的签名做交叉验证，而不是自我验证 Go 实现。
// 覆盖了官方文档特别提到的坑：txid 字段里带 "/"，原始字节里是 PHP 风格的转义 "\/"。
func TestVerifyWebhookSignatureMatchesReferenceAlgorithm(t *testing.T) {
	body := []byte(`{"type":"payment","uuid":"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa","order_id":"ORDER-1","amount":"15.00","status":"paid","currency":"CNY","txid":"someTxidWith\/Slash","sign":"f3d8fdf391af9d161fb6c59a609786c4"}`)

	payload, err := VerifyWebhookSignature(&Config{PaymentAPIKey: "test-payment-api-key"}, body)
	if err != nil {
		t.Fatalf("VerifyWebhookSignature failed: %v", err)
	}
	if payload.UUID != "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa" {
		t.Fatalf("UUID = %q", payload.UUID)
	}
	if payload.OrderID != "ORDER-1" {
		t.Fatalf("OrderID = %q", payload.OrderID)
	}
	if payload.Status != "paid" {
		t.Fatalf("Status = %q", payload.Status)
	}
	if ToPaymentStatus(payload.Status) != "success" {
		t.Fatalf("mapped status = %q, want success", ToPaymentStatus(payload.Status))
	}
}

func TestVerifyWebhookSignatureRejectsTamperedBody(t *testing.T) {
	body := []byte(`{"type":"payment","uuid":"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa","order_id":"ORDER-1","amount":"999.00","status":"paid","currency":"CNY","txid":"someTxidWith\/Slash","sign":"f3d8fdf391af9d161fb6c59a609786c4"}`)

	_, err := VerifyWebhookSignature(&Config{PaymentAPIKey: "test-payment-api-key"}, body)
	if !errors.Is(err, ErrSignatureInvalid) {
		t.Fatalf("err = %v, want ErrSignatureInvalid", err)
	}
}

func TestVerifyWebhookSignatureRejectsMissingSign(t *testing.T) {
	body := []byte(`{"type":"payment","uuid":"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa","order_id":"ORDER-1","amount":"15.00","status":"paid","currency":"CNY"}`)

	_, err := VerifyWebhookSignature(&Config{PaymentAPIKey: "test-payment-api-key"}, body)
	if !errors.Is(err, ErrResponseInvalid) {
		t.Fatalf("err = %v, want ErrResponseInvalid", err)
	}
}

func TestToPaymentStatusMapsKnownStates(t *testing.T) {
	cases := map[string]string{
		StatusPaid:               "success",
		StatusPaidOver:           "success",
		StatusCancel:             "expired",
		StatusWrongAmount:        "failed",
		StatusFail:               "failed",
		StatusSystemFail:         "failed",
		StatusProcess:            "pending",
		StatusConfirmCheck:       "pending",
		StatusCheck:              "pending",
		StatusWrongAmountWaiting: "pending",
		StatusRefundPaid:         "",
		StatusLocked:             "",
		"unknown_status":         "",
	}
	for status, want := range cases {
		if got := ToPaymentStatus(status); got != want {
			t.Fatalf("ToPaymentStatus(%q) = %q, want %q", status, got, want)
		}
	}
}

// useTestBaseURL 把包级 apiBaseURL 指向 httptest.Server，返回值用于测试结束后还原。
func useTestBaseURL(url string) func() {
	original := apiBaseURL
	apiBaseURL = url
	return func() { apiBaseURL = original }
}
