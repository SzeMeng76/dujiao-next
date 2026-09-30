package tickethttp

import (
	"strings"

	userdomain "github.com/dujiao-next/internal/modules/identity/user/domain"
	ticketapp "github.com/dujiao-next/internal/modules/ticket/application"
	ticketcontract "github.com/dujiao-next/internal/modules/ticket/contract"
	ticketpresenter "github.com/dujiao-next/internal/modules/ticket/transport/presenter"

	"github.com/dujiao-next/internal/platform/http/ginutil"
	"github.com/dujiao-next/internal/platform/http/response"

	"github.com/gin-gonic/gin"
)

// UserDirectory 用户目录端口（用于补充工单响应里的用户邮箱/昵称）。
type UserDirectory interface {
	GetByID(id uint) (*userdomain.User, error)
	ListByIDs(ids []uint) ([]userdomain.User, error)
}

// AdminHandler 处理管理端工单请求。
type AdminHandler struct {
	service *ticketapp.Service
	users   UserDirectory
}

// NewAdminHandler 创建管理端工单 handler。
func NewAdminHandler(service *ticketapp.Service, users UserDirectory) *AdminHandler {
	if service == nil || users == nil {
		panic("ticket admin handler: required dependency is nil")
	}
	return &AdminHandler{service: service, users: users}
}

func fillTicketUserInfo(summary *ticketpresenter.TicketSummary, userMap map[uint]userdomain.User) {
	if user, ok := userMap[summary.UserID]; ok {
		summary.UserEmail = user.Email
		summary.UserDisplayName = user.DisplayName
	}
}

// ListTickets 管理端工单列表。
func (h *AdminHandler) ListTickets(c *gin.Context) {
	page, pageSize := ginutil.ParsePagination(c)
	status := strings.TrimSpace(c.Query("status"))
	priority := strings.TrimSpace(c.Query("priority"))
	keyword := strings.TrimSpace(c.Query("keyword"))

	tickets, total, err := h.service.ListAdmin(ticketcontract.AdminListFilter{
		Page:     page,
		PageSize: pageSize,
		Status:   status,
		Priority: priority,
		Keyword:  keyword,
	})
	if err != nil {
		ginutil.RespondError(c, response.CodeInternal, "error.ticket_fetch_failed", err)
		return
	}

	userIDs := make([]uint, 0, len(tickets))
	seen := map[uint]struct{}{}
	for _, ticket := range tickets {
		if ticket.UserID == 0 {
			continue
		}
		if _, ok := seen[ticket.UserID]; ok {
			continue
		}
		seen[ticket.UserID] = struct{}{}
		userIDs = append(userIDs, ticket.UserID)
	}
	userMap := make(map[uint]userdomain.User, len(userIDs))
	if len(userIDs) > 0 {
		users, err := h.users.ListByIDs(userIDs)
		if err != nil {
			ginutil.RespondError(c, response.CodeInternal, "error.ticket_fetch_failed", err)
			return
		}
		for _, user := range users {
			userMap[user.ID] = user
		}
	}

	items := ticketpresenter.NewTicketSummaryList(tickets)
	for i := range items {
		fillTicketUserInfo(&items[i], userMap)
	}

	pagination := response.BuildPagination(page, pageSize, total)
	response.SuccessWithPage(c, items, pagination)
}

// GetTicket 管理端工单详情。
func (h *AdminHandler) GetTicket(c *gin.Context) {
	id, err := ginutil.ParseParamUint(c, "id")
	if err != nil {
		ginutil.RespondError(c, response.CodeBadRequest, "error.ticket_not_found", nil)
		return
	}

	ticket, err := h.service.GetForAdmin(id)
	if err != nil {
		respondTicketError(c, err, "error.ticket_fetch_failed")
		return
	}

	detail := ticketpresenter.NewTicketDetail(ticket)
	if ticket.UserID != 0 {
		if user, err := h.users.GetByID(ticket.UserID); err == nil && user != nil {
			detail.UserEmail = user.Email
			detail.UserDisplayName = user.DisplayName
		}
	}

	response.Success(c, detail)
}

// AdminReplyRequest 管理员回复工单请求体。
type AdminReplyRequest struct {
	Content string `json:"content" binding:"required"`
	Close   bool   `json:"close"`
}

// ReplyTicket 管理员回复工单（close=true 时回复后直接关闭）。
func (h *AdminHandler) ReplyTicket(c *gin.Context) {
	adminID, ok := ginutil.GetAdminID(c)
	if !ok {
		return
	}
	id, err := ginutil.ParseParamUint(c, "id")
	if err != nil {
		ginutil.RespondError(c, response.CodeBadRequest, "error.ticket_not_found", nil)
		return
	}

	var req AdminReplyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ginutil.RespondBindError(c, err)
		return
	}

	message, err := h.service.AdminReply(id, adminID, req.Content, req.Close)
	if err != nil {
		respondTicketError(c, err, "error.ticket_reply_failed")
		return
	}

	response.Success(c, ticketpresenter.NewTicketMessageResp(message))
}

// CloseTicket 管理员直接关闭工单。
func (h *AdminHandler) CloseTicket(c *gin.Context) {
	id, err := ginutil.ParseParamUint(c, "id")
	if err != nil {
		ginutil.RespondError(c, response.CodeBadRequest, "error.ticket_not_found", nil)
		return
	}

	if err := h.service.Close(id); err != nil {
		respondTicketError(c, err, "error.ticket_reply_failed")
		return
	}

	response.Success(c, gin.H{"status": "closed"})
}

// Badge 管理端待处理工单数徽标。
func (h *AdminHandler) Badge(c *gin.Context) {
	count, err := h.service.AdminBadge()
	if err != nil {
		ginutil.RespondError(c, response.CodeInternal, "error.ticket_fetch_failed", err)
		return
	}
	response.Success(c, gin.H{"count": count})
}
