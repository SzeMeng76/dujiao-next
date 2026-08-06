package cryptomusadapter

import (
	"context"
	"errors"
	"testing"
	"time"

	paymentcontract "github.com/dujiao-next/internal/modules/payment/contract"

	"github.com/dujiao-next/internal/constants"
	"github.com/dujiao-next/internal/modules/payment/infrastructure/gateway/cryptomus"
	"github.com/dujiao-next/internal/shared/jsonmap"
)

func TestCryptomusAdapter_Type(t *testing.T) {
	a := NewCryptomusAdapter()
	want := constants.PaymentProviderCryptomus + ":"
	if got := a.Type(); got != want {
		t.Fatalf("Type() = %q, want %q", got, want)
	}
}

func TestCryptomusAdapter_ValidateConfig_EmptyRejected(t *testing.T) {
	a := NewCryptomusAdapter()
	err := a.ValidateConfig(jsonmap.JSON{}, "")
	if !errors.Is(err, paymentcontract.ErrGatewayConfigInvalid) {
		t.Fatalf("expected wrapped paymentcontract.ErrGatewayConfigInvalid, got %v", err)
	}
}

func TestCryptomusAdapter_ValidateConfig_UnsupportedChannelTypeRejected(t *testing.T) {
	a := NewCryptomusAdapter()
	cfg := jsonmap.JSON{"merchant_id": "m-1", "payment_api_key": "key-1"}
	err := a.ValidateConfig(cfg, "usdt-trc20")
	if !errors.Is(err, paymentcontract.ErrGatewayUnsupportedChannel) {
		t.Fatalf("expected wrapped paymentcontract.ErrGatewayUnsupportedChannel, got %v", err)
	}
}

func TestCryptomusAdapter_ValidateConfig_EmptyOrProviderChannelTypeAccepted(t *testing.T) {
	a := NewCryptomusAdapter()
	cfg := jsonmap.JSON{"merchant_id": "m-1", "payment_api_key": "key-1"}
	if err := a.ValidateConfig(cfg, ""); err != nil {
		t.Fatalf("ValidateConfig(\"\") failed: %v", err)
	}
	if err := a.ValidateConfig(cfg, "cryptomus"); err != nil {
		t.Fatalf("ValidateConfig(\"cryptomus\") failed: %v", err)
	}
}

func TestCryptomusAdapter_CreatePayment_ConfigInvalidMapped(t *testing.T) {
	a := NewCryptomusAdapter()
	_, err := a.CreatePayment(context.Background(), jsonmap.JSON{}, paymentcontract.GatewayCreateInput{
		OrderNo:  "ORDER-1",
		Currency: "CNY",
	})
	if !errors.Is(err, paymentcontract.ErrGatewayConfigInvalid) {
		t.Fatalf("expected wrapped paymentcontract.ErrGatewayConfigInvalid, got %v", err)
	}
}

func TestCryptomusAdapter_CreatePayment_RejectsQRInteractionMode(t *testing.T) {
	a := NewCryptomusAdapter()
	cfg := jsonmap.JSON{"merchant_id": "m-1", "payment_api_key": "key-1"}
	_, err := a.CreatePayment(context.Background(), cfg, paymentcontract.GatewayCreateInput{
		OrderNo:  "ORDER-1",
		Currency: "CNY",
		Extra:    jsonmap.JSON{"interaction_mode": constants.PaymentInteractionQR},
	})
	if !errors.Is(err, paymentcontract.ErrGatewayConfigInvalid) {
		t.Fatalf("expected wrapped paymentcontract.ErrGatewayConfigInvalid, got %v", err)
	}
}

func TestCryptomusAdapter_ParseWebhook_SignatureMismatchMapped(t *testing.T) {
	a := NewCryptomusAdapter()
	webhooker, ok := a.(paymentcontract.GatewayWebhooker)
	if !ok {
		t.Fatalf("Cryptomus adapter must implement GatewayWebhooker")
	}
	cfg := jsonmap.JSON{"merchant_id": "m-1", "payment_api_key": "key-1"}
	body := []byte(`{"type":"payment","uuid":"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa","order_id":"ORDER-1","amount":"15.00","status":"paid","currency":"CNY","sign":"deadbeef"}`)
	_, err := webhooker.ParseWebhook(context.Background(), cfg, nil, body, time.Now())
	if !errors.Is(err, paymentcontract.ErrGatewaySignatureInvalid) {
		t.Fatalf("expected wrapped paymentcontract.ErrGatewaySignatureInvalid, got %v", err)
	}
}

func TestCryptomusAdapter_MapCryptomusError(t *testing.T) {
	cases := []struct {
		name string
		in   error
		want error
	}{
		{"config", cryptomus.ErrConfigInvalid, paymentcontract.ErrGatewayConfigInvalid},
		{"request", cryptomus.ErrRequestFailed, paymentcontract.ErrGatewayRequestFailed},
		{"response", cryptomus.ErrResponseInvalid, paymentcontract.ErrGatewayResponseInvalid},
		{"signature", cryptomus.ErrSignatureInvalid, paymentcontract.ErrGatewaySignatureInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := mapCryptomusError(tc.in)
			if !errors.Is(got, tc.want) {
				t.Fatalf("mapCryptomusError(%v) errors.Is %v = false, want true", tc.in, tc.want)
			}
		})
	}
}
