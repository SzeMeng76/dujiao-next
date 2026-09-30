package contract

import (
	ticketdomain "github.com/dujiao-next/internal/modules/ticket/domain"
)

// UserListFilter 用户端工单列表过滤。
type UserListFilter struct {
	Page     int
	PageSize int
	UserID   uint
	Status   string
}

// AdminListFilter 管理端工单列表过滤。
type AdminListFilter struct {
	Page     int
	PageSize int
	Status   string
	Priority string
	Keyword  string // 匹配工单号或标题
}

// Store 是工单模块所需的持久化端口。
type Store interface {
	Create(ticket *ticketdomain.Ticket, firstMessage *ticketdomain.TicketMessage) error
	GetByID(id uint) (*ticketdomain.Ticket, error)
	GetByIDAndUser(id uint, userID uint) (*ticketdomain.Ticket, error)
	GetByIDForUpdate(id uint) (*ticketdomain.Ticket, error)
	ListByUser(filter UserListFilter) ([]ticketdomain.Ticket, int64, error)
	ListAdmin(filter AdminListFilter) ([]ticketdomain.Ticket, int64, error)
	ListMessages(ticketID uint) ([]ticketdomain.TicketMessage, error)
	AppendMessage(message *ticketdomain.TicketMessage) error
	UpdateStatus(id uint, status string) error
	CountOpenByUser(userID uint) (int64, error)
	CountOpenAdmin() (int64, error)
	// DeleteMany 删除给定 ID 的工单及其全部消息，返回删除前的完整数据（含消息，用于清理附件）。
	// 不存在的 ID 会被静默忽略。
	DeleteMany(ids []uint) ([]ticketdomain.Ticket, error)

	WithinTransaction(fn func(Transaction) error) error
}

// Transaction 是已打开事务的工单工作单元。
type Transaction interface {
	Tickets() Store
}
