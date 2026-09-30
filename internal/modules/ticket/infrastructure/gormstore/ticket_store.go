package gormstore

import (
	ticketcontract "github.com/dujiao-next/internal/modules/ticket/contract"
	ticketdomain "github.com/dujiao-next/internal/modules/ticket/domain"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Store 是工单持久化端口的 GORM 实现。
type Store struct {
	db *gorm.DB
}

var _ ticketcontract.Store = (*Store)(nil)

// New 创建工单存储。
func New(db *gorm.DB) *Store {
	return &Store{db: db}
}

func (s *Store) bind(tx *gorm.DB) *Store {
	if tx == nil {
		return s
	}
	return &Store{db: tx}
}

// Create 创建工单及其首条消息（同一事务内完成）。
func (s *Store) Create(ticket *ticketdomain.Ticket, firstMessage *ticketdomain.TicketMessage) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(ticket).Error; err != nil {
			return err
		}
		firstMessage.TicketID = ticket.ID
		return tx.Create(firstMessage).Error
	})
}

// GetByID 按ID查询工单（含消息列表）。
func (s *Store) GetByID(id uint) (*ticketdomain.Ticket, error) {
	var ticket ticketdomain.Ticket
	err := s.db.Preload("Messages", func(db *gorm.DB) *gorm.DB {
		return db.Order("created_at ASC")
	}).First(&ticket, id).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &ticket, nil
}

// GetByIDAndUser 按ID+所属用户查询工单（用户端权限隔离）。
func (s *Store) GetByIDAndUser(id uint, userID uint) (*ticketdomain.Ticket, error) {
	var ticket ticketdomain.Ticket
	err := s.db.Preload("Messages", func(db *gorm.DB) *gorm.DB {
		return db.Order("created_at ASC")
	}).Where("user_id = ?", userID).First(&ticket, id).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &ticket, nil
}

// GetByIDForUpdate 加行锁查询工单，用于回复/关闭等写操作前的状态校验。
func (s *Store) GetByIDForUpdate(id uint) (*ticketdomain.Ticket, error) {
	var ticket ticketdomain.Ticket
	err := s.db.Clauses(clause.Locking{Strength: "UPDATE"}).First(&ticket, id).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &ticket, nil
}

// ListByUser 用户端工单列表。
func (s *Store) ListByUser(filter ticketcontract.UserListFilter) ([]ticketdomain.Ticket, int64, error) {
	query := s.db.Model(&ticketdomain.Ticket{}).Where("user_id = ?", filter.UserID)
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	page, pageSize := filter.Page, filter.PageSize
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}

	var tickets []ticketdomain.Ticket
	err := query.Order("updated_at DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&tickets).Error
	if err != nil {
		return nil, 0, err
	}
	return tickets, total, nil
}

// ListAdmin 管理端工单列表。
func (s *Store) ListAdmin(filter ticketcontract.AdminListFilter) ([]ticketdomain.Ticket, int64, error) {
	query := s.db.Model(&ticketdomain.Ticket{})
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}
	if filter.Priority != "" {
		query = query.Where("priority = ?", filter.Priority)
	}
	if filter.Keyword != "" {
		keyword := "%" + filter.Keyword + "%"
		query = query.Where("ticket_no LIKE ? OR title LIKE ?", keyword, keyword)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	page, pageSize := filter.Page, filter.PageSize
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}

	var tickets []ticketdomain.Ticket
	err := query.Order("updated_at DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&tickets).Error
	if err != nil {
		return nil, 0, err
	}
	return tickets, total, nil
}

// ListMessages 查询工单的全部消息。
func (s *Store) ListMessages(ticketID uint) ([]ticketdomain.TicketMessage, error) {
	var messages []ticketdomain.TicketMessage
	err := s.db.Where("ticket_id = ?", ticketID).Order("created_at ASC").Find(&messages).Error
	return messages, err
}

// AppendMessage 追加一条工单消息。
func (s *Store) AppendMessage(message *ticketdomain.TicketMessage) error {
	return s.db.Create(message).Error
}

// UpdateStatus 更新工单状态。
func (s *Store) UpdateStatus(id uint, status string) error {
	return s.db.Model(&ticketdomain.Ticket{}).Where("id = ?", id).Update("status", status).Error
}

// CountOpenByUser 统计用户未关闭的工单数（用于个人中心徽标）。
func (s *Store) CountOpenByUser(userID uint) (int64, error) {
	var count int64
	err := s.db.Model(&ticketdomain.Ticket{}).
		Where("user_id = ? AND status <> ?", userID, ticketdomain.StatusClosed).
		Count(&count).Error
	return count, err
}

// CountOpenAdmin 统计待客服处理的工单数（管理端徽标）。
func (s *Store) CountOpenAdmin() (int64, error) {
	var count int64
	err := s.db.Model(&ticketdomain.Ticket{}).
		Where("status = ?", ticketdomain.StatusOpen).
		Count(&count).Error
	return count, err
}

// WithinTransaction 在事务中执行工单相关写操作。
func (s *Store) WithinTransaction(fn func(ticketcontract.Transaction) error) error {
	if fn == nil {
		return nil
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		return fn(transaction{db: tx})
	})
}

type transaction struct {
	db *gorm.DB
}

var _ ticketcontract.Transaction = transaction{}

func (t transaction) Tickets() ticketcontract.Store {
	return New(t.db)
}
