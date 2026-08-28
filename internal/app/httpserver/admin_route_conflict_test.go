package httpserver

import (
	"testing"

	coupondomain "github.com/dujiao-next/internal/modules/coupon/domain"
	userdomain "github.com/dujiao-next/internal/modules/identity/user/domain"
	orderdomain "github.com/dujiao-next/internal/modules/order/domain"
	ordertransport "github.com/dujiao-next/internal/modules/order/transport/http"
	paymentdomain "github.com/dujiao-next/internal/modules/payment/domain"
	paymenttransport "github.com/dujiao-next/internal/modules/payment/transport/http"
	promotiondomain "github.com/dujiao-next/internal/modules/promotion/domain"

	"github.com/gin-gonic/gin"
)

type routeConflictFakeOrderQuery struct{}

func (routeConflictFakeOrderQuery) ListOrdersForAdmin(ordertransport.OrderListFilter) ([]orderdomain.Order, int64, error) {
	return nil, 0, nil
}
func (routeConflictFakeOrderQuery) GetOrderForAdmin(uint) (*orderdomain.Order, error) {
	return nil, nil
}
func (routeConflictFakeOrderQuery) UpdateOrderStatus(uint, string) (*orderdomain.Order, error) {
	return nil, nil
}

type routeConflictFakeUserDirectory struct{}

func (routeConflictFakeUserDirectory) ListByIDs([]uint) ([]userdomain.User, error) { return nil, nil }
func (routeConflictFakeUserDirectory) GetByID(uint) (*userdomain.User, error)      { return nil, nil }

type routeConflictFakeCouponLookup struct{}

func (routeConflictFakeCouponLookup) GetByID(uint) (*coupondomain.Coupon, error) { return nil, nil }

type routeConflictFakePromotionLookup struct{}

func (routeConflictFakePromotionLookup) GetByID(uint) (*promotiondomain.Promotion, error) {
	return nil, nil
}

type routeConflictFakePaymentDirectory struct{}

func (routeConflictFakePaymentDirectory) ListByOrderID(uint) ([]paymentdomain.Payment, error) {
	return nil, nil
}

type routeConflictFakePaymentChannelDirectory struct{}

func (routeConflictFakePaymentChannelDirectory) ListByIDs(ids []uint) ([]paymentdomain.PaymentChannel, error) {
	return nil, nil
}

type routeConflictFakeAdminPaymentQuery struct{}

func (routeConflictFakeAdminPaymentQuery) ListPayments(paymenttransport.AdminPaymentListFilter) ([]paymentdomain.Payment, int64, error) {
	return nil, 0, nil
}
func (routeConflictFakeAdminPaymentQuery) GetPayment(uint) (*paymentdomain.Payment, error) {
	return nil, nil
}

type routeConflictFakeAdminRefundReader struct{}

func (routeConflictFakeAdminRefundReader) ListAdminRefundItems(ordertransport.AdminRefundListQuery) ([]ordertransport.AdminRefundItem, int64, error) {
	return nil, 0, nil
}
func (routeConflictFakeAdminRefundReader) GetAdminRefundItem(uint) (*ordertransport.AdminRefundItem, error) {
	return nil, nil
}

// TestOrderAndPaymentAdminRoutesDoNotConflict guards against a real startup panic:
// gin's route tree requires every route sharing the same path prefix to use the
// same wildcard parameter name. order/transport/http and payment/transport/http
// both register routes under "/admin/orders/", so a mismatched param name
// (e.g. ":order_id" vs ":id") would panic at server startup — a class of bug no
// existing test caught because none of them actually register both route sets
// onto the same gin engine. Registration order matters: gin's route tree only
// flags the conflict once a route with a trailing segment after the wildcard
// (e.g. "/orders/:id/refund-to-wallet") has already been inserted, so both
// RegisterAdminRoutes and RegisterAdminRefundWriteRoutes must be exercised
// together, matching how routes_admin.go wires them in production.
func TestOrderAndPaymentAdminRoutesDoNotConflict(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	apiV1 := engine.Group("/api/v1")
	group := apiV1.Group("/admin")

	orderHandler := ordertransport.NewAdminHandler(
		routeConflictFakeOrderQuery{},
		routeConflictFakeUserDirectory{},
		routeConflictFakeCouponLookup{},
		routeConflictFakePromotionLookup{},
		routeConflictFakePaymentDirectory{},
		routeConflictFakePaymentChannelDirectory{},
	)
	orderRefundHandler := ordertransport.NewAdminRefundHandler(
		routeConflictFakeAdminRefundReader{},
		nil,
		nil,
		nil,
		nil,
	)
	paymentHandler := paymenttransport.NewAdminHandler(
		routeConflictFakeAdminPaymentQuery{},
		nil,
		nil,
		nil,
		nil,
	)

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("registering order and payment admin routes on the same engine panicked: %v", r)
		}
	}()

	ordertransport.RegisterAdminRoutes(group, orderHandler)
	ordertransport.RegisterAdminRefundWriteRoutes(group, orderRefundHandler)
	paymenttransport.RegisterAdminRoutes(group, paymentHandler)
}
