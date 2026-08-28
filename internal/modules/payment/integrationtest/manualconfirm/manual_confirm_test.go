package manualconfirm_test

import (
	"testing"
	"time"

	paymentapp "github.com/dujiao-next/internal/modules/payment/application"
	paymentcontract "github.com/dujiao-next/internal/modules/payment/contract"
	paymentgormstore "github.com/dujiao-next/internal/modules/payment/infrastructure/gormstore"

	paymentdomain "github.com/dujiao-next/internal/modules/payment/domain"

	fulfillmentdomain "github.com/dujiao-next/internal/modules/fulfillment/domain"
	ordercontract "github.com/dujiao-next/internal/modules/order/contract"
	orderdomain "github.com/dujiao-next/internal/modules/order/domain"
	ordergormstore "github.com/dujiao-next/internal/modules/order/infrastructure/gormstore"

	productdomain "github.com/dujiao-next/internal/modules/catalog/product/domain"
	productgormstore "github.com/dujiao-next/internal/modules/catalog/product/store/gormstore"

	userdomain "github.com/dujiao-next/internal/modules/identity/user/domain"

	"github.com/dujiao-next/internal/constants"
	"github.com/dujiao-next/internal/shared/jsonmap"
	"github.com/dujiao-next/internal/shared/money"

	"github.com/glebarez/sqlite"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

type manualConfirmFixture struct {
	db             *gorm.DB
	orderRepo      ordercontract.Store
	paymentRepo    paymentcontract.Store
	manualLogStore *ordergormstore.ManualConfirmLogStore
	paymentService *paymentapp.PaymentService
	order          *orderdomain.Order
	payment        *paymentdomain.Payment
}

func newManualConfirmFixture(t *testing.T, dsnSuffix string, orderStatus, paymentStatus, fulfillmentType string) *manualConfirmFixture {
	t.Helper()

	dsn := "file:payment_manual_confirm_" + dsnSuffix + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite failed: %v", err)
	}
	if err := db.AutoMigrate(
		&userdomain.User{},
		&productdomain.Product{},
		&productdomain.ProductSKU{},
		&orderdomain.Order{},
		&orderdomain.OrderItem{},
		&fulfillmentdomain.Fulfillment{},
		&paymentdomain.PaymentChannel{},
		&paymentdomain.Payment{},
		&orderdomain.OrderManualConfirmLog{},
	); err != nil {
		t.Fatalf("auto migrate failed: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	user := &userdomain.User{
		Email:        "manual-confirm-" + dsnSuffix + "@example.com",
		PasswordHash: "hash",
		Status:       constants.UserStatusActive,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("create user failed: %v", err)
	}

	order := &orderdomain.Order{
		OrderNo:          "DJMANUALCONFIRM" + dsnSuffix,
		UserID:           user.ID,
		Status:           orderStatus,
		Currency:         "CNY",
		OriginalAmount:   money.FromDecimal(decimal.NewFromInt(88)),
		TotalAmount:      money.FromDecimal(decimal.NewFromInt(88)),
		WalletPaidAmount: money.FromDecimal(decimal.Zero),
		OnlinePaidAmount: money.FromDecimal(decimal.NewFromInt(88)),
		RefundedAmount:   money.FromDecimal(decimal.Zero),
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if err := db.Create(order).Error; err != nil {
		t.Fatalf("create order failed: %v", err)
	}
	if fulfillmentType != "" {
		item := &orderdomain.OrderItem{
			OrderID:         order.ID,
			ProductID:       1,
			TitleJSON:       jsonmap.JSON{"zh-CN": "test product"},
			UnitPrice:       money.FromDecimal(decimal.NewFromInt(88)),
			Quantity:        1,
			TotalPrice:      money.FromDecimal(decimal.NewFromInt(88)),
			FulfillmentType: fulfillmentType,
			CreatedAt:       now,
			UpdatedAt:       now,
		}
		if err := db.Create(item).Error; err != nil {
			t.Fatalf("create order item failed: %v", err)
		}
	}

	channel := &paymentdomain.PaymentChannel{
		Name:            "TestChannel",
		ProviderType:    constants.PaymentProviderOfficial,
		ChannelType:     constants.PaymentChannelTypeAlipay,
		InteractionMode: constants.PaymentInteractionQR,
		FeeRate:         money.FromDecimal(decimal.Zero),
		IsActive:        true,
		SortOrder:       10,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := db.Create(channel).Error; err != nil {
		t.Fatalf("create channel failed: %v", err)
	}

	var payment *paymentdomain.Payment
	if paymentStatus != "" {
		payment = &paymentdomain.Payment{
			OrderID:         order.ID,
			ChannelID:       channel.ID,
			ProviderType:    channel.ProviderType,
			ChannelType:     channel.ChannelType,
			InteractionMode: channel.InteractionMode,
			Amount:          money.FromDecimal(decimal.NewFromInt(88)),
			FeeRate:         money.FromDecimal(decimal.Zero),
			FeeAmount:       money.FromDecimal(decimal.Zero),
			Currency:        "CNY",
			Status:          paymentStatus,
			ProviderRef:     "MANUAL-CONFIRM-" + dsnSuffix,
			CreatedAt:       now,
			UpdatedAt:       now,
		}
		if err := db.Create(payment).Error; err != nil {
			t.Fatalf("create payment failed: %v", err)
		}
	}

	orderRepo := ordergormstore.New(db, "test-guest-credential-secret-with-32-bytes")
	paymentRepo := paymentgormstore.New(db, "test-guest-credential-secret-with-32-bytes")
	channelRepo := paymentgormstore.NewChannelStore(db)
	productRepo := productgormstore.NewProductStore(db)
	productSKURepo := productgormstore.NewSKUStore(db)
	manualLogStore := ordergormstore.NewManualConfirmLogStore(db)

	paymentService := paymentapp.NewPaymentService(paymentapp.PaymentServiceOptions{
		OrderStore:            orderRepo,
		ProductRepo:           productRepo,
		ProductSKURepo:        productSKURepo,
		PaymentStore:          paymentRepo,
		ChannelStore:          channelRepo,
		ExpireMinutes:         15,
		ManualConfirmLogStore: manualLogStore,
	})

	return &manualConfirmFixture{
		db:             db,
		orderRepo:      orderRepo,
		paymentRepo:    paymentRepo,
		manualLogStore: manualLogStore,
		paymentService: paymentService,
		order:          order,
		payment:        payment,
	}
}

func TestManualConfirmPaymentSuccessMarksOrderPaidAndLogsAudit(t *testing.T) {
	fixture := newManualConfirmFixture(t, "success", constants.OrderStatusPendingPayment, constants.PaymentStatusPending, constants.FulfillmentTypeManual)

	order, payment, err := fixture.paymentService.ManualConfirmPayment(paymentapp.ManualConfirmPaymentInput{
		OrderID:          fixture.order.ID,
		OperatorAdminID:  1,
		OperatorUsername: "admin",
		ProviderRef:      "chain-tx-hash-123",
		Remark:           "支付平台已确认到账，回调异常，人工确认",
	})
	if err != nil {
		t.Fatalf("ManualConfirmPayment failed: %v", err)
	}
	if order == nil || order.Status != constants.OrderStatusPaid {
		t.Fatalf("expected order status paid, got %+v", order)
	}
	if payment == nil || payment.Status != constants.PaymentStatusSuccess {
		t.Fatalf("expected payment status success, got %+v", payment)
	}
	if payment.PaidAt == nil {
		t.Fatalf("expected payment paid_at to be set")
	}

	logs, err := fixture.manualLogStore.ListByOrderID(fixture.order.ID)
	if err != nil {
		t.Fatalf("list manual confirm logs failed: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("expected 1 audit log entry, got %d", len(logs))
	}
	log := logs[0]
	if log.OperatorAdminID != 1 || log.OperatorUsername != "admin" {
		t.Fatalf("unexpected operator fields: %+v", log)
	}
	if log.FromStatus != constants.OrderStatusPendingPayment || log.ToStatus != constants.OrderStatusPaid {
		t.Fatalf("unexpected status transition recorded: %+v", log)
	}
	if log.ProviderRef != "chain-tx-hash-123" {
		t.Fatalf("expected provider ref recorded, got %+v", log)
	}
}

func TestManualConfirmPaymentRejectsWhenOrderStatusNotEligible(t *testing.T) {
	for _, status := range []string{
		constants.OrderStatusPaid,
		constants.OrderStatusFulfilling,
		constants.OrderStatusDelivered,
		constants.OrderStatusCompleted,
		constants.OrderStatusRefunded,
	} {
		fixture := newManualConfirmFixture(t, "notallowed-"+status, status, constants.PaymentStatusPending, constants.FulfillmentTypeManual)
		_, _, err := fixture.paymentService.ManualConfirmPayment(paymentapp.ManualConfirmPaymentInput{
			OrderID:         fixture.order.ID,
			OperatorAdminID: 1,
			Remark:          "test",
		})
		if err == nil {
			t.Fatalf("expected error for order status %s, got nil", status)
		}
	}
}

func TestManualConfirmPaymentSucceedsWhenOrderWasAutoCanceled(t *testing.T) {
	// 模拟：订单因超时被系统自动取消，支付记录在取消时被标记为 expired，
	// 但用户实际已经付款成功——人工确认支付应当允许把订单重新推进为已支付。
	fixture := newManualConfirmFixture(t, "canceled-recover", constants.OrderStatusCanceled, constants.PaymentStatusExpired, constants.FulfillmentTypeManual)

	order, payment, err := fixture.paymentService.ManualConfirmPayment(paymentapp.ManualConfirmPaymentInput{
		OrderID:          fixture.order.ID,
		OperatorAdminID:  1,
		OperatorUsername: "admin",
		Remark:           "订单被超时取消，但支付平台已确认到账，人工确认",
	})
	if err != nil {
		t.Fatalf("ManualConfirmPayment on canceled order failed: %v", err)
	}
	if order == nil || order.Status != constants.OrderStatusPaid {
		t.Fatalf("expected order status paid, got %+v", order)
	}
	if payment == nil || payment.Status != constants.PaymentStatusSuccess {
		t.Fatalf("expected payment status success, got %+v", payment)
	}

	logs, err := fixture.manualLogStore.ListByOrderID(fixture.order.ID)
	if err != nil {
		t.Fatalf("list manual confirm logs failed: %v", err)
	}
	if len(logs) != 1 || logs[0].FromStatus != constants.OrderStatusCanceled || logs[0].ToStatus != constants.OrderStatusPaid {
		t.Fatalf("unexpected audit log entries: %+v", logs)
	}
}

// TestManualConfirmPaymentOnCanceledOrderFailsWhenStockAlreadyReassigned 验证：
// 订单取消时释放的库存如果已经被其他订单占用/售出，人工确认支付在扣减库存这一步
// 会因为库存不足而报错并整体回滚，绝不会静默超卖或把库存扣成负数。
func TestManualConfirmPaymentOnCanceledOrderFailsWhenStockAlreadyReassigned(t *testing.T) {
	fixture := newManualConfirmFixture(t, "canceled-stock-gone", constants.OrderStatusCanceled, constants.PaymentStatusExpired, constants.FulfillmentTypeManual)

	now := time.Now().UTC().Truncate(time.Second)
	product := &productdomain.Product{
		CategoryID:      1,
		Slug:            "manual-confirm-canceled-stock-gone",
		TitleJSON:       jsonmap.JSON{"zh-CN": "test product"},
		PriceAmount:     money.FromDecimal(decimal.NewFromInt(88)),
		FulfillmentType: constants.FulfillmentTypeManual,
		// ManualStockTotal/Locked 均为默认值 0：模拟取消时释放的库存已被其他订单买走售出。
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := fixture.db.Create(product).Error; err != nil {
		t.Fatalf("create product failed: %v", err)
	}
	if err := fixture.db.Model(&orderdomain.OrderItem{}).
		Where("order_id = ?", fixture.order.ID).
		Update("product_id", product.ID).Error; err != nil {
		t.Fatalf("point order item at real product failed: %v", err)
	}

	_, _, err := fixture.paymentService.ManualConfirmPayment(paymentapp.ManualConfirmPaymentInput{
		OrderID:         fixture.order.ID,
		OperatorAdminID: 1,
		Remark:          "订单被取消后库存已被其他订单占用，测试库存不足是否正确拦截",
	})
	if err == nil {
		t.Fatalf("expected manual confirm to fail when manual stock is insufficient, got nil error")
	}

	reloadedOrder, err := fixture.orderRepo.GetByID(fixture.order.ID)
	if err != nil {
		t.Fatalf("reload order failed: %v", err)
	}
	if reloadedOrder.Status != constants.OrderStatusCanceled {
		t.Fatalf("order status must remain unchanged after failed manual confirm, got %s", reloadedOrder.Status)
	}
	var freshProduct productdomain.Product
	if err := fixture.db.First(&freshProduct, product.ID).Error; err != nil {
		t.Fatalf("reload product failed: %v", err)
	}
	if freshProduct.ManualStockTotal != 0 || freshProduct.ManualStockSold != 0 {
		t.Fatalf("manual stock must not be mutated by a failed confirm, got %+v", freshProduct)
	}
}

func TestManualConfirmPaymentRejectsWhenNoEligiblePayment(t *testing.T) {
	fixture := newManualConfirmFixture(t, "nopayment", constants.OrderStatusPendingPayment, constants.PaymentStatusSuccess, constants.FulfillmentTypeManual)
	_, _, err := fixture.paymentService.ManualConfirmPayment(paymentapp.ManualConfirmPaymentInput{
		OrderID:         fixture.order.ID,
		OperatorAdminID: 1,
		Remark:          "test",
	})
	if err == nil {
		t.Fatalf("expected error when no manual-confirmable payment exists, got nil")
	}
}

func TestManualConfirmPaymentRejectsEmptyRemark(t *testing.T) {
	fixture := newManualConfirmFixture(t, "noremark", constants.OrderStatusPendingPayment, constants.PaymentStatusPending, constants.FulfillmentTypeManual)
	_, _, err := fixture.paymentService.ManualConfirmPayment(paymentapp.ManualConfirmPaymentInput{
		OrderID:         fixture.order.ID,
		OperatorAdminID: 1,
		Remark:          "   ",
	})
	if err == nil {
		t.Fatalf("expected error for empty remark, got nil")
	}
}

func TestManualConfirmPaymentIsIdempotent(t *testing.T) {
	fixture := newManualConfirmFixture(t, "idempotent", constants.OrderStatusPendingPayment, constants.PaymentStatusPending, constants.FulfillmentTypeAuto)

	input := paymentapp.ManualConfirmPaymentInput{
		OrderID:         fixture.order.ID,
		OperatorAdminID: 1,
		Remark:          "first confirm",
	}
	order1, payment1, err := fixture.paymentService.ManualConfirmPayment(input)
	if err != nil {
		t.Fatalf("first ManualConfirmPayment failed: %v", err)
	}
	if order1.Status != constants.OrderStatusPaid {
		t.Fatalf("expected order paid after first confirm, got %s", order1.Status)
	}

	// 第二次调用：订单已经是 paid，不在允许的状态白名单内，必须被拒绝，不能重复扣库存/重复发货。
	_, _, err = fixture.paymentService.ManualConfirmPayment(paymentapp.ManualConfirmPaymentInput{
		OrderID:         fixture.order.ID,
		OperatorAdminID: 1,
		Remark:          "second confirm attempt",
	})
	if err == nil {
		t.Fatalf("expected second manual confirm on an already-paid order to be rejected")
	}

	reloadedPayment, err := fixture.paymentRepo.GetByID(payment1.ID)
	if err != nil {
		t.Fatalf("reload payment failed: %v", err)
	}
	if reloadedPayment.Status != constants.PaymentStatusSuccess {
		t.Fatalf("payment status should remain success, got %s", reloadedPayment.Status)
	}
}
