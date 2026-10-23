package application_test

import (
	"errors"
	"testing"
	"time"

	. "github.com/dujiao-next/internal/modules/fulfillment/application"
	fulfillmentdomain "github.com/dujiao-next/internal/modules/fulfillment/domain"
	fulfillmentgormstore "github.com/dujiao-next/internal/modules/fulfillment/infrastructure/gormstore"
	orderdomain "github.com/dujiao-next/internal/modules/order/domain"
	ordergormstore "github.com/dujiao-next/internal/modules/order/infrastructure/gormstore"

	"github.com/dujiao-next/internal/constants"
	cardsecretdomain "github.com/dujiao-next/internal/modules/cardsecret/domain"
	"github.com/dujiao-next/internal/shared/jsonmap"
	"github.com/dujiao-next/internal/shared/money"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

type replaceSecretsFixture struct {
	db      *gorm.DB
	svc     *Service
	order   *orderdomain.Order
	oldIDs  []uint
	payload *fulfillmentdomain.Fulfillment
}

// newReplaceSecretsFixture 创建一个已完成、含两条已交付卡密（OLD-1/OLD-2）的自动发货订单。
func newReplaceSecretsFixture(t *testing.T, orderStatus string, availableSecrets ...string) *replaceSecretsFixture {
	t.Helper()
	db := setupFulfillmentServiceTestDB(t)
	now := time.Now()

	order := &orderdomain.Order{
		OrderNo:          "REPLACE-SECRET-001",
		UserID:           1,
		Status:           orderStatus,
		Currency:         "CNY",
		OriginalAmount:   money.FromDecimal(decimal.NewFromInt(20)),
		DiscountAmount:   money.FromDecimal(decimal.Zero),
		TotalAmount:      money.FromDecimal(decimal.NewFromInt(20)),
		WalletPaidAmount: money.FromDecimal(decimal.Zero),
		OnlinePaidAmount: money.FromDecimal(decimal.NewFromInt(20)),
		RefundedAmount:   money.FromDecimal(decimal.Zero),
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if err := db.Create(order).Error; err != nil {
		t.Fatalf("create order failed: %v", err)
	}
	if err := db.Create(&orderdomain.OrderItem{
		OrderID:         order.ID,
		ProductID:       100,
		SKUID:           1001,
		TitleJSON:       jsonmap.JSON{"zh-CN": "测试商品"},
		Quantity:        2,
		FulfillmentType: constants.FulfillmentTypeAuto,
		CreatedAt:       now,
		UpdatedAt:       now,
	}).Error; err != nil {
		t.Fatalf("create order item failed: %v", err)
	}

	var oldIDs []uint
	for _, text := range []string{"OLD-1", "OLD-2"} {
		orderID := order.ID
		usedAt := now
		secret := &cardsecretdomain.Secret{
			ProductID: 100,
			SKUID:     1001,
			Secret:    text,
			Status:    cardsecretdomain.StatusUsed,
			OrderID:   &orderID,
			UsedAt:    &usedAt,
			CreatedAt: now,
			UpdatedAt: now,
		}
		if err := db.Create(secret).Error; err != nil {
			t.Fatalf("create used secret failed: %v", err)
		}
		oldIDs = append(oldIDs, secret.ID)
	}
	for _, text := range availableSecrets {
		if err := db.Create(&cardsecretdomain.Secret{
			ProductID: 100,
			SKUID:     1001,
			Secret:    text,
			Status:    cardsecretdomain.StatusAvailable,
			CreatedAt: now,
			UpdatedAt: now,
		}).Error; err != nil {
			t.Fatalf("create available secret failed: %v", err)
		}
	}

	fulfillment := &fulfillmentdomain.Fulfillment{
		OrderID:     order.ID,
		Type:        constants.FulfillmentTypeAuto,
		Status:      constants.FulfillmentStatusDelivered,
		Payload:     "OLD-1\nOLD-2",
		DeliveredAt: &now,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := db.Create(fulfillment).Error; err != nil {
		t.Fatalf("create fulfillment failed: %v", err)
	}

	svc := New(Options{
		OrderStore:       ordergormstore.New(db, "test-guest-credential-secret-with-32-bytes"),
		FulfillmentStore: fulfillmentgormstore.New(db),
	})
	return &replaceSecretsFixture{db: db, svc: svc, order: order, oldIDs: oldIDs, payload: fulfillment}
}

func (f *replaceSecretsFixture) secretByText(t *testing.T, text string) cardsecretdomain.Secret {
	t.Helper()
	var row cardsecretdomain.Secret
	if err := f.db.Where("secret = ?", text).First(&row).Error; err != nil {
		t.Fatalf("query secret %s failed: %v", text, err)
	}
	return row
}

func (f *replaceSecretsFixture) currentPayload(t *testing.T) string {
	t.Helper()
	var row fulfillmentdomain.Fulfillment
	if err := f.db.First(&row, f.payload.ID).Error; err != nil {
		t.Fatalf("query fulfillment failed: %v", err)
	}
	return row.Payload
}

func TestReplaceSecretsReplacesOnlySelectedSecret(t *testing.T) {
	f := newReplaceSecretsFixture(t, constants.OrderStatusCompleted, "NEW-1", "NEW-2")

	updated, replaced, err := f.svc.ReplaceSecrets(ReplaceSecretsInput{
		OrderID: f.order.ID,
		AdminID: 9,
		Secrets: []string{"OLD-1"},
		Reason:  "卡密失效",
	})
	if err != nil {
		t.Fatalf("replace secrets failed: %v", err)
	}
	if replaced != 1 {
		t.Fatalf("replaced want 1 got %d", replaced)
	}
	if updated == nil || updated.Payload != "NEW-1\nOLD-2" {
		t.Fatalf("unexpected payload: %+v", updated)
	}
	if got := f.currentPayload(t); got != "NEW-1\nOLD-2" {
		t.Fatalf("persisted payload want NEW-1\\nOLD-2 got %q", got)
	}

	oldRow := f.secretByText(t, "OLD-1")
	if oldRow.Status != cardsecretdomain.StatusUsed || oldRow.OrderID == nil || *oldRow.OrderID != f.order.ID {
		t.Fatalf("old secret must stay used and bound to the order: %+v", oldRow)
	}
	newRow := f.secretByText(t, "NEW-1")
	if newRow.Status != cardsecretdomain.StatusUsed || newRow.OrderID == nil || *newRow.OrderID != f.order.ID {
		t.Fatalf("new secret must be used by the order: %+v", newRow)
	}
	if untouched := f.secretByText(t, "NEW-2"); untouched.Status != cardsecretdomain.StatusAvailable {
		t.Fatalf("unused stock must stay available: %+v", untouched)
	}
}

func TestReplaceSecretsReplacesAllWhenNoneSelected(t *testing.T) {
	f := newReplaceSecretsFixture(t, constants.OrderStatusDelivered, "NEW-1", "NEW-2")

	_, replaced, err := f.svc.ReplaceSecrets(ReplaceSecretsInput{OrderID: f.order.ID, AdminID: 9})
	if err != nil {
		t.Fatalf("replace secrets failed: %v", err)
	}
	if replaced != 2 {
		t.Fatalf("replaced want 2 got %d", replaced)
	}
	if got := f.currentPayload(t); got != "NEW-1\nNEW-2" {
		t.Fatalf("persisted payload want NEW-1\\nNEW-2 got %q", got)
	}
}

func TestReplaceSecretsRollsBackWhenStockInsufficient(t *testing.T) {
	f := newReplaceSecretsFixture(t, constants.OrderStatusCompleted, "NEW-1")

	_, _, err := f.svc.ReplaceSecrets(ReplaceSecretsInput{OrderID: f.order.ID, AdminID: 9})
	if !errors.Is(err, ErrCardSecretInsufficient) {
		t.Fatalf("want ErrCardSecretInsufficient got %v", err)
	}
	if got := f.currentPayload(t); got != "OLD-1\nOLD-2" {
		t.Fatalf("payload must stay unchanged, got %q", got)
	}
	if row := f.secretByText(t, "NEW-1"); row.Status != cardsecretdomain.StatusAvailable {
		t.Fatalf("stock must not be consumed on rollback: %+v", row)
	}
}

func TestReplaceSecretsRejectsUnknownSecretAndWrongStatus(t *testing.T) {
	f := newReplaceSecretsFixture(t, constants.OrderStatusCompleted, "NEW-1", "NEW-2")
	if _, _, err := f.svc.ReplaceSecrets(ReplaceSecretsInput{
		OrderID: f.order.ID, AdminID: 9, Secrets: []string{"NOT-IN-ORDER"},
	}); !errors.Is(err, ErrFulfillmentInvalid) {
		t.Fatalf("unknown secret want ErrFulfillmentInvalid got %v", err)
	}

	paid := newReplaceSecretsFixture(t, constants.OrderStatusPaid, "NEW-1", "NEW-2")
	if _, _, err := paid.svc.ReplaceSecrets(ReplaceSecretsInput{OrderID: paid.order.ID, AdminID: 9}); !errors.Is(err, ErrOrderStatusInvalid) {
		t.Fatalf("paid order want ErrOrderStatusInvalid got %v", err)
	}
}
