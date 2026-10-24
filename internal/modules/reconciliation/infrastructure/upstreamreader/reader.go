package upstreamreader

import (
	"context"
	"fmt"

	reconciliationcontract "github.com/dujiao-next/internal/modules/reconciliation/contract"
	siteconnectiondomain "github.com/dujiao-next/internal/modules/siteconnection/domain"
	"github.com/dujiao-next/internal/upstream"
)

type ConnectionProvider interface {
	GetByID(id uint) (*siteconnectiondomain.Connection, error)
	GetAdapter(connection *siteconnectiondomain.Connection) (upstream.Adapter, error)
}

type Reader struct {
	connections ConnectionProvider
}

var _ reconciliationcontract.UpstreamOrderProvider = (*Reader)(nil)

type session struct {
	adapter upstream.Adapter
}

var _ reconciliationcontract.UpstreamOrderReader = (*session)(nil)

func New(connections ConnectionProvider) *Reader {
	if connections == nil {
		panic("reconciliation upstream reader: connections is nil")
	}
	return &Reader{connections: connections}
}

func (r *Reader) Open(connectionID uint) (reconciliationcontract.UpstreamOrderReader, error) {
	connection, err := r.connections.GetByID(connectionID)
	if err != nil {
		return nil, fmt.Errorf("get connection: %w", err)
	}
	adapter, err := r.connections.GetAdapter(connection)
	if err != nil {
		return nil, fmt.Errorf("get adapter: %w", err)
	}
	return &session{adapter: adapter}, nil
}

func (s *session) Get(ctx context.Context, upstreamOrderID uint, upstreamOrderNo string) (*reconciliationcontract.UpstreamOrder, error) {
	var detail *upstream.UpstreamOrderDetail
	var err error
	// 以字符串订单号标识订单的协议（如异次元）必须走 orderNo。
	if byNo, ok := s.adapter.(upstream.OrderNoAdapter); ok && upstreamOrderNo != "" {
		detail, err = byNo.GetOrderByNo(ctx, upstreamOrderNo)
	} else {
		detail, err = s.adapter.GetOrder(ctx, upstreamOrderID)
	}
	if err != nil {
		return nil, err
	}
	return &reconciliationcontract.UpstreamOrder{Status: detail.Status, Amount: detail.Amount}, nil
}
