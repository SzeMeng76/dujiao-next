package application

import (
	"strings"

	"github.com/dujiao-next/internal/logger"
	ticketcontract "github.com/dujiao-next/internal/modules/ticket/contract"
	ticketdomain "github.com/dujiao-next/internal/modules/ticket/domain"
	"github.com/dujiao-next/internal/shared/serial"
)

// OrderOwnershipChecker 校验订单是否属于该用户（工单关联订单时使用）。
type OrderOwnershipChecker interface {
	// ExistsForUser 返回该订单号是否存在且属于该用户。
	ExistsForUser(orderNo string, userID uint) (orderID uint, exists bool, err error)
}

// TicketNotifier 工单通知端口（新工单/新消息提醒站长，客服回复提醒用户）。
type TicketNotifier interface {
	NotifyAdminNewMessage(ticket *ticketdomain.Ticket, message *ticketdomain.TicketMessage)
	NotifyUserReplied(ticket *ticketdomain.Ticket, message *ticketdomain.TicketMessage)
}

// AttachmentDeleter 删除工单图片凭证所需的最小端口（复用 upload 模块的本地文件存储）。
type AttachmentDeleter interface {
	Delete(publicURL string) error
}

var validPriorities = map[string]struct{}{
	ticketdomain.PriorityLow:    {},
	ticketdomain.PriorityNormal: {},
	ticketdomain.PriorityHigh:   {},
}

var validTicketTypes = map[string]struct{}{
	ticketdomain.TypePreSale:   {},
	ticketdomain.TypeAfterSale: {},
}

// Service 工单业务逻辑服务。
type Service struct {
	store       ticketcontract.Store
	orders      OrderOwnershipChecker
	notifier    TicketNotifier
	attachments AttachmentDeleter
}

// NewService 创建工单服务。orders/notifier/attachments 可为 nil（表示不校验订单关联 / 不发送通知 / 不清理附件文件）。
func NewService(store ticketcontract.Store, orders OrderOwnershipChecker, notifier TicketNotifier, attachments AttachmentDeleter) *Service {
	if store == nil {
		panic("ticket service: store is nil")
	}
	return &Service{store: store, orders: orders, notifier: notifier, attachments: attachments}
}

// CreateInput 创建工单入参。
type CreateInput struct {
	UserID     uint
	Title      string
	Content    string
	Priority   string
	OrderNo    string
	ImageURL   string
	TicketType string
	ProductID  *uint
}

// Create 用户创建工单。
func (s *Service) Create(input CreateInput) (*ticketdomain.Ticket, error) {
	title := strings.TrimSpace(input.Title)
	if title == "" {
		return nil, ErrTicketTitleEmpty
	}
	content := strings.TrimSpace(input.Content)
	if content == "" {
		return nil, ErrTicketContentEmpty
	}
	priority := strings.TrimSpace(input.Priority)
	if priority == "" {
		priority = ticketdomain.PriorityNormal
	}
	if _, ok := validPriorities[priority]; !ok {
		return nil, ErrTicketPriorityBad
	}

	// 服务类型：未传时按 after_sale 兼容旧客户端；显式传入的非法值直接拒绝。
	ticketType := strings.TrimSpace(input.TicketType)
	if ticketType == "" {
		ticketType = ticketdomain.TypeAfterSale
	}
	if _, ok := validTicketTypes[ticketType]; !ok {
		return nil, ErrTicketTypeBad
	}

	orderNo := strings.TrimSpace(input.OrderNo)
	// 售后必须关联订单：无论订单来自下拉选择还是手动输入，都必须属于当前用户。
	if ticketType == ticketdomain.TypeAfterSale && orderNo == "" {
		return nil, ErrTicketOrderMissing
	}

	var orderID *uint
	if orderNo != "" {
		if s.orders == nil {
			return nil, ErrTicketOrderInvalid
		}
		id, exists, err := s.orders.ExistsForUser(orderNo, input.UserID)
		if err != nil {
			return nil, err
		}
		if !exists {
			return nil, ErrTicketOrderInvalid
		}
		orderID = &id
	}

	ticket := &ticketdomain.Ticket{
		TicketNo:   serial.Generate("TK"),
		UserID:     input.UserID,
		OrderID:    orderID,
		TicketType: ticketType,
		ProductID:  input.ProductID,
		Title:      title,
		Priority:   priority,
		Status:     ticketdomain.StatusOpen,
	}
	message := &ticketdomain.TicketMessage{
		SenderType: ticketdomain.SenderUser,
		SenderID:   input.UserID,
		Content:    content,
		ImageURL:   strings.TrimSpace(input.ImageURL),
	}

	if err := s.store.Create(ticket, message); err != nil {
		return nil, err
	}
	ticket.Messages = []ticketdomain.TicketMessage{*message}

	if s.notifier != nil {
		s.notifier.NotifyAdminNewMessage(ticket, message)
	}
	return ticket, nil
}

// UserReply 用户回复工单。
func (s *Service) UserReply(ticketID, userID uint, content, imageURL string) (*ticketdomain.TicketMessage, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, ErrTicketContentEmpty
	}

	ticket, err := s.store.GetByIDAndUser(ticketID, userID)
	if err != nil {
		return nil, err
	}
	if ticket == nil {
		return nil, ErrTicketNotFound
	}
	if ticket.IsClosed() {
		return nil, ErrTicketClosed
	}

	message := &ticketdomain.TicketMessage{
		TicketID:   ticket.ID,
		SenderType: ticketdomain.SenderUser,
		SenderID:   userID,
		Content:    content,
		ImageURL:   strings.TrimSpace(imageURL),
	}
	if err := s.store.AppendMessage(message); err != nil {
		return nil, err
	}
	if err := s.store.UpdateStatus(ticket.ID, ticketdomain.StatusOpen); err != nil {
		return nil, err
	}

	if s.notifier != nil {
		s.notifier.NotifyAdminNewMessage(ticket, message)
	}
	return message, nil
}

// AdminReply 管理员回复工单。close=true 时回复后直接关闭工单。
func (s *Service) AdminReply(ticketID, adminID uint, content string, close bool) (*ticketdomain.TicketMessage, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, ErrTicketContentEmpty
	}

	ticket, err := s.store.GetByID(ticketID)
	if err != nil {
		return nil, err
	}
	if ticket == nil {
		return nil, ErrTicketNotFound
	}
	if ticket.IsClosed() {
		return nil, ErrTicketClosed
	}

	message := &ticketdomain.TicketMessage{
		TicketID:   ticket.ID,
		SenderType: ticketdomain.SenderAdmin,
		SenderID:   adminID,
		Content:    content,
	}
	if err := s.store.AppendMessage(message); err != nil {
		return nil, err
	}

	nextStatus := ticketdomain.StatusReplied
	if close {
		nextStatus = ticketdomain.StatusClosed
	}
	if err := s.store.UpdateStatus(ticket.ID, nextStatus); err != nil {
		return nil, err
	}

	if s.notifier != nil {
		s.notifier.NotifyUserReplied(ticket, message)
	}
	return message, nil
}

// Close 管理员直接关闭工单（不追加回复内容）。
func (s *Service) Close(ticketID uint) error {
	ticket, err := s.store.GetByID(ticketID)
	if err != nil {
		return err
	}
	if ticket == nil {
		return ErrTicketNotFound
	}
	if ticket.IsClosed() {
		return nil
	}
	return s.store.UpdateStatus(ticketID, ticketdomain.StatusClosed)
}

// Reopen 管理员重新打开一个已关闭的工单，恢复为待处理状态。
// 只允许对已关闭工单操作；对未关闭工单调用会返回 ErrTicketNotClosed。
func (s *Service) Reopen(ticketID uint) error {
	ticket, err := s.store.GetByID(ticketID)
	if err != nil {
		return err
	}
	if ticket == nil {
		return ErrTicketNotFound
	}
	if !ticket.IsClosed() {
		return ErrTicketNotClosed
	}
	return s.store.UpdateStatus(ticketID, ticketdomain.StatusOpen)
}

// ListByUser 用户端工单列表。
func (s *Service) ListByUser(filter ticketcontract.UserListFilter) ([]ticketdomain.Ticket, int64, error) {
	return s.store.ListByUser(filter)
}

// ListAdmin 管理端工单列表。
func (s *Service) ListAdmin(filter ticketcontract.AdminListFilter) ([]ticketdomain.Ticket, int64, error) {
	return s.store.ListAdmin(filter)
}

// GetForUser 用户端工单详情（含消息）。
func (s *Service) GetForUser(ticketID, userID uint) (*ticketdomain.Ticket, error) {
	ticket, err := s.store.GetByIDAndUser(ticketID, userID)
	if err != nil {
		return nil, err
	}
	if ticket == nil {
		return nil, ErrTicketNotFound
	}
	return ticket, nil
}

// GetForAdmin 管理端工单详情（含消息）。
func (s *Service) GetForAdmin(ticketID uint) (*ticketdomain.Ticket, error) {
	ticket, err := s.store.GetByID(ticketID)
	if err != nil {
		return nil, err
	}
	if ticket == nil {
		return nil, ErrTicketNotFound
	}
	return ticket, nil
}

// UserBadge 用户端未读徽标（未关闭的工单数）。
func (s *Service) UserBadge(userID uint) (int64, error) {
	return s.store.CountOpenByUser(userID)
}

// AdminBadge 管理端未读徽标（待处理工单数）。
func (s *Service) AdminBadge() (int64, error) {
	return s.store.CountOpenAdmin()
}

// Delete 删除给定 ID 的工单（含全部消息），并清理消息中的图片凭证文件。
// 不存在的 ID 静默忽略；单个附件文件删除失败只记录日志，不影响数据库记录已删除的结果。
func (s *Service) Delete(ids []uint) (int, error) {
	deleted, err := s.store.DeleteMany(ids)
	if err != nil {
		return 0, err
	}
	if s.attachments != nil {
		for _, ticket := range deleted {
			for _, message := range ticket.Messages {
				imageURL := strings.TrimSpace(message.ImageURL)
				if imageURL == "" {
					continue
				}
				if err := s.attachments.Delete(imageURL); err != nil {
					logger.Warnw("ticket_delete_attachment_failed", "ticket_id", ticket.ID, "image_url", imageURL, "error", err)
				}
			}
		}
	}
	return len(deleted), nil
}
