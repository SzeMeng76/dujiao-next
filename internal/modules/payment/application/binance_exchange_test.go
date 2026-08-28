package application

import (
	"testing"
	"time"

	orderdomain "github.com/dujiao-next/internal/modules/order/domain"
	paymentdomain "github.com/dujiao-next/internal/modules/payment/domain"

	"github.com/dujiao-next/internal/constants"
	"github.com/dujiao-next/internal/shared/jsonmap"
	"github.com/dujiao-next/internal/shared/money"

	"github.com/shopspring/decimal"
)

// TestBinancePayUSDTExchangeDoesNotMarkUnderpaid guards against the regression where
// Binance Pay (and other crypto gateways using exchange_rate conversion) store the
// converted amount (9.15 USDT) as payment.Amount after CreatePayment, but the underpaid
// guard added in 2b8cebd8 directly compared this USDT value against the order's CNY
// amount (61), always judging it as underpaid. Adapter CreatePayment stores the
// original CNY amount in payment.ProviderPayload["original_amount"]; the guard must
// read this field when present instead of using payment.Amount raw.
func TestBinancePayUSDTExchangeDoesNotMarkUnderpaid(t *testing.T) {
	svc, db := setupPaymentServiceWalletTest(t)
	channel := createUnderpaidChannel(t, db, svc, "Binance Pay USDT", constants.PaymentChannelTypeBinancepay)

	now := time.Now()
	order := &orderdomain.Order{
		OrderNo: "BINANCE001", UserID: 1, Status: constants.OrderStatusPendingPayment, Currency: "CNY",
		OriginalAmount: money.FromDecimal(decimal.NewFromInt(61)), TotalAmount: money.FromDecimal(decimal.NewFromInt(61)),
		DiscountAmount: money.FromDecimal(decimal.Zero), PromotionDiscountAmount: money.FromDecimal(decimal.Zero),
		WalletPaidAmount: money.FromDecimal(decimal.Zero), OnlinePaidAmount: money.FromDecimal(decimal.NewFromInt(61)),
		RefundedAmount: money.FromDecimal(decimal.Zero), CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(order).Error; err != nil {
		t.Fatalf("create order failed: %v", err)
	}

	// 模拟 Binance Pay 换汇支付: 订单 61 CNY 经汇率 0.15 换算后网关实付 9.15 USDT。
	// applyProviderPayment 回写后 payment.Amount=9.15, payment.Currency=USDT,
	// ProviderPayload["original_amount"]="61"。
	payment := &paymentdomain.Payment{
		OrderID: order.ID, ChannelID: channel.ID, ProviderType: channel.ProviderType, ChannelType: channel.ChannelType,
		InteractionMode: channel.InteractionMode,
		Amount:          money.FromDecimal(decimal.NewFromFloat(9.15)),
		FeeAmount:       money.FromDecimal(decimal.Zero), FeePolicy: constants.PaymentFeePolicyNone,
		Currency: "USDT",
		Status:   constants.PaymentStatusPending,
		ProviderPayload: jsonmap.JSON{
			"original_amount":   "61",
			"original_currency": "CNY",
			"exchange_rate":     "0.15",
		},
		CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(payment).Error; err != nil {
		t.Fatalf("create payment failed: %v", err)
	}

	// Binance webhook 回调返回的也是 USDT 金额(9.15)，币种 USDT。
	paid, err := svc.HandleCallback(PaymentCallbackInput{
		PaymentID: payment.ID, OrderNo: order.OrderNo, ChannelID: channel.ID,
		Status: constants.PaymentStatusSuccess,
		Amount: money.FromDecimal(decimal.NewFromFloat(9.15)), Currency: "USDT",
		ProviderRef: "binance-ref-001",
	})
	if err != nil {
		t.Fatalf("handle binance callback failed: %v", err)
	}

	// 金额守恒检查应从 ProviderPayload["original_amount"](61 CNY) 而非 payment.Amount(9.15 USDT)
	// 读取，61 == 61 判定足额，不再标记为 underpaid。
	if paid.ExceptionCode == constants.PaymentExceptionUnderpaidSucceeded {
		t.Fatalf("binance USDT exchange payment should not be marked underpaid, got exception_code=%s", paid.ExceptionCode)
	}

	var reloadedOrder orderdomain.Order
	if err := db.First(&reloadedOrder, order.ID).Error; err != nil {
		t.Fatalf("reload order failed: %v", err)
	}
	if reloadedOrder.Status != constants.OrderStatusPaid || reloadedOrder.PaidAt == nil {
		t.Fatalf("binance USDT exchange payment must fulfill the order, got status=%s paid_at=%v", reloadedOrder.Status, reloadedOrder.PaidAt)
	}
}
