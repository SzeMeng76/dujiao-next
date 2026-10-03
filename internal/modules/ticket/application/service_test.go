package application

import (
	"errors"
	"testing"

	ticketcontract "github.com/dujiao-next/internal/modules/ticket/contract"
	ticketdomain "github.com/dujiao-next/internal/modules/ticket/domain"
)

// fakeStore 只实现服务层创建路径需要的持久化行为，其余方法为占位实现。
type fakeStore struct {
	createdTicket  *ticketdomain.Ticket
	createdMessage *ticketdomain.TicketMessage
	createErr      error
}

func (f *fakeStore) Create(ticket *ticketdomain.Ticket, firstMessage *ticketdomain.TicketMessage) error {
	if f.createErr != nil {
		return f.createErr
	}
	ticket.ID = 1
	f.createdTicket = ticket
	f.createdMessage = firstMessage
	return nil
}

func (f *fakeStore) GetByID(uint) (*ticketdomain.Ticket, error) { return nil, nil }
func (f *fakeStore) GetByIDAndUser(uint, uint) (*ticketdomain.Ticket, error) {
	return nil, nil
}
func (f *fakeStore) GetByIDForUpdate(uint) (*ticketdomain.Ticket, error) { return nil, nil }
func (f *fakeStore) ListByUser(ticketcontract.UserListFilter) ([]ticketdomain.Ticket, int64, error) {
	return nil, 0, nil
}
func (f *fakeStore) ListAdmin(ticketcontract.AdminListFilter) ([]ticketdomain.Ticket, int64, error) {
	return nil, 0, nil
}
func (f *fakeStore) ListMessages(uint) ([]ticketdomain.TicketMessage, error) { return nil, nil }
func (f *fakeStore) AppendMessage(*ticketdomain.TicketMessage) error         { return nil }
func (f *fakeStore) UpdateStatus(uint, string) error                         { return nil }
func (f *fakeStore) CountOpenByUser(uint) (int64, error)                     { return 0, nil }
func (f *fakeStore) CountOpenAdmin() (int64, error)                          { return 0, nil }
func (f *fakeStore) DeleteMany([]uint) ([]ticketdomain.Ticket, error)        { return nil, nil }
func (f *fakeStore) WithinTransaction(fn func(ticketcontract.Transaction) error) error {
	return fn(nil)
}

// fakeOrders 模拟订单归属校验：只认 owned 中登记的订单号。
type fakeOrders struct {
	owned map[string]uint
	err   error
}

func (f *fakeOrders) ExistsForUser(orderNo string, _ uint) (uint, bool, error) {
	if f.err != nil {
		return 0, false, f.err
	}
	id, ok := f.owned[orderNo]
	return id, ok, nil
}

func newTestService(orders *fakeOrders) (*Service, *fakeStore) {
	store := &fakeStore{}
	return NewService(store, orders, nil, nil), store
}

func TestCreateDefaultsToAfterSaleType(t *testing.T) {
	service, store := newTestService(&fakeOrders{owned: map[string]uint{"ORDER-1": 10}})

	ticket, err := service.Create(CreateInput{
		UserID:  1,
		Title:   "订单没到账",
		Content: "已经付款但没有收到商品",
		OrderNo: "ORDER-1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ticket.TicketType != ticketdomain.TypeAfterSale {
		t.Fatalf("empty type should default to after_sale, got %q", ticket.TicketType)
	}
	if store.createdTicket.TicketType != ticketdomain.TypeAfterSale {
		t.Fatalf("persisted ticket type = %q, want after_sale", store.createdTicket.TicketType)
	}
}

func TestCreateRejectsInvalidType(t *testing.T) {
	service, _ := newTestService(&fakeOrders{owned: map[string]uint{}})

	_, err := service.Create(CreateInput{
		UserID:     1,
		Title:      "标题",
		Content:    "内容",
		TicketType: "unknown_type",
	})
	if !errors.Is(err, ErrTicketTypeBad) {
		t.Fatalf("want ErrTicketTypeBad, got %v", err)
	}
}

func TestCreatePreSaleWithoutOrderAndProduct(t *testing.T) {
	service, store := newTestService(&fakeOrders{owned: map[string]uint{}})

	ticket, err := service.Create(CreateInput{
		UserID:     1,
		Title:      "想咨询交付方式",
		Content:    "请问支持哪些交付方式？",
		TicketType: ticketdomain.TypePreSale,
	})
	if err != nil {
		t.Fatalf("pre_sale should not require an order, got error: %v", err)
	}
	if ticket.OrderID != nil {
		t.Fatalf("order id should stay empty, got %v", *ticket.OrderID)
	}
	if ticket.ProductID != nil {
		t.Fatalf("product id should stay empty, got %v", *ticket.ProductID)
	}
	if store.createdMessage.ImageURL != "" {
		t.Fatalf("image url should default to empty, got %q", store.createdMessage.ImageURL)
	}
}

func TestCreatePreSaleSavesProductID(t *testing.T) {
	service, store := newTestService(&fakeOrders{owned: map[string]uint{}})
	productID := uint(42)

	ticket, err := service.Create(CreateInput{
		UserID:     1,
		Title:      "咨询商品",
		Content:    "这个商品怎么用？",
		TicketType: ticketdomain.TypePreSale,
		ProductID:  &productID,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ticket.ProductID == nil || *ticket.ProductID != productID {
		t.Fatalf("product id not saved, got %v", ticket.ProductID)
	}
	if store.createdTicket.ProductID == nil || *store.createdTicket.ProductID != productID {
		t.Fatalf("persisted product id mismatch")
	}
}

func TestCreateAfterSaleRequiresOrder(t *testing.T) {
	service, _ := newTestService(&fakeOrders{owned: map[string]uint{}})

	_, err := service.Create(CreateInput{
		UserID:     1,
		Title:      "售后问题",
		Content:    "商品有问题",
		TicketType: ticketdomain.TypeAfterSale,
	})
	if !errors.Is(err, ErrTicketOrderMissing) {
		t.Fatalf("want ErrTicketOrderMissing, got %v", err)
	}
}

func TestCreateAfterSaleWithOwnedOrder(t *testing.T) {
	service, store := newTestService(&fakeOrders{owned: map[string]uint{"ORDER-9": 77}})

	ticket, err := service.Create(CreateInput{
		UserID:     1,
		Title:      "售后问题",
		Content:    "商品有问题",
		TicketType: ticketdomain.TypeAfterSale,
		OrderNo:    "ORDER-9",
		ImageURL:   "/uploads/ticket/proof.png",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ticket.OrderID == nil || *ticket.OrderID != 77 {
		t.Fatalf("order id not resolved, got %v", ticket.OrderID)
	}
	if store.createdMessage.ImageURL != "/uploads/ticket/proof.png" {
		t.Fatalf("image url not passed through, got %q", store.createdMessage.ImageURL)
	}
}

func TestCreateRejectsOrderNotOwnedByUser(t *testing.T) {
	service, _ := newTestService(&fakeOrders{owned: map[string]uint{"ORDER-OTHER": 5}})

	_, err := service.Create(CreateInput{
		UserID:     1,
		Title:      "售后问题",
		Content:    "别人的订单",
		TicketType: ticketdomain.TypeAfterSale,
		OrderNo:    "ORDER-NOT-MINE",
	})
	if !errors.Is(err, ErrTicketOrderInvalid) {
		t.Fatalf("want ErrTicketOrderInvalid, got %v", err)
	}
}

// 旧客户端不传 ticket_type，但会传订单号；必须继续可用。
func TestCreateLegacyPayloadStillWorks(t *testing.T) {
	service, _ := newTestService(&fakeOrders{owned: map[string]uint{"ORDER-LEGACY": 3}})

	ticket, err := service.Create(CreateInput{
		UserID:   1,
		Title:    "老请求",
		Content:  "没有类型字段",
		Priority: ticketdomain.PriorityHigh,
		OrderNo:  "ORDER-LEGACY",
	})
	if err != nil {
		t.Fatalf("legacy payload should still work, got %v", err)
	}
	if ticket.TicketType != ticketdomain.TypeAfterSale {
		t.Fatalf("legacy payload should default to after_sale, got %q", ticket.TicketType)
	}
	if ticket.Priority != ticketdomain.PriorityHigh {
		t.Fatalf("priority not preserved, got %q", ticket.Priority)
	}
}

// 售前也允许传订单号（例如咨询已下单的商品），此时同样要走归属校验。
func TestCreatePreSaleWithOrderStillValidatesOwnership(t *testing.T) {
	service, _ := newTestService(&fakeOrders{owned: map[string]uint{}})

	_, err := service.Create(CreateInput{
		UserID:     1,
		Title:      "售前咨询",
		Content:    "咨询一下",
		TicketType: ticketdomain.TypePreSale,
		OrderNo:    "ORDER-NOT-MINE",
	})
	if !errors.Is(err, ErrTicketOrderInvalid) {
		t.Fatalf("want ErrTicketOrderInvalid, got %v", err)
	}
}
