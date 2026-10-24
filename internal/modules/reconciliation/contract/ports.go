package contract

import (
	"context"
	"time"

	reconciliationdomain "github.com/dujiao-next/internal/modules/reconciliation/domain"
)

type JobRepository interface {
	Create(job *reconciliationdomain.Job) error
	GetByID(id uint) (*reconciliationdomain.Job, error)
	Update(job *reconciliationdomain.Job) error
	List(filter JobListFilter) ([]reconciliationdomain.Job, int64, error)
}

type ItemRepository interface {
	BatchCreate(items []reconciliationdomain.Item) error
	GetByID(id uint) (*reconciliationdomain.Item, error)
	Update(item *reconciliationdomain.Item) error
	ListByJobID(jobID uint, page, pageSize int) ([]reconciliationdomain.Item, int64, error)
}

type ProcurementReader interface {
	ListByConnectionAndTimeRange(connectionID uint, start, end time.Time) ([]ProcurementOrder, error)
}

type UpstreamOrderProvider interface {
	Open(connectionID uint) (UpstreamOrderReader, error)
}

type UpstreamOrderReader interface {
	// Get 按上游订单查询。orderNo 用于以字符串标识订单的上游（如异次元共享 API），
	// 数字标识的协议可以忽略该参数。
	Get(ctx context.Context, upstreamOrderID uint, upstreamOrderNo string) (*UpstreamOrder, error)
}

type Enqueuer interface {
	Enqueue(jobID uint) error
}

type MismatchNotifier interface {
	NotifyMismatch(job *reconciliationdomain.Job) error
}

// UseCase 是 HTTP 与异步消费者共享的正式应用契约。
type UseCase interface {
	CreateAndEnqueue(input RunInput) (*reconciliationdomain.Job, error)
	Execute(ctx context.Context, jobID uint) error
	GetJob(id uint) (*reconciliationdomain.Job, error)
	ListJobs(filter JobListFilter) ([]reconciliationdomain.Job, int64, error)
	GetJobItems(jobID uint, page, pageSize int) ([]reconciliationdomain.Item, int64, error)
	ResolveItem(itemID, adminID uint, remark string) error
}
