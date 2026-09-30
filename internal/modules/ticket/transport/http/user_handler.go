package tickethttp

import (
	"errors"
	"mime/multipart"
	"strings"

	ticketapp "github.com/dujiao-next/internal/modules/ticket/application"
	ticketcontract "github.com/dujiao-next/internal/modules/ticket/contract"
	ticketpresenter "github.com/dujiao-next/internal/modules/ticket/transport/presenter"
	uploadcontract "github.com/dujiao-next/internal/modules/upload/contract"

	"github.com/dujiao-next/internal/platform/http/ginutil"
	"github.com/dujiao-next/internal/platform/http/response"

	"github.com/gin-gonic/gin"
)

// FileUploader 是文件落盘端口（复用 upload 模块）。
type FileUploader interface {
	SaveFileWithMeta(file *multipart.FileHeader, scene string) (*uploadcontract.Result, error)
}

// UserHandler 处理用户端工单请求。
type UserHandler struct {
	service  *ticketapp.Service
	uploader FileUploader
}

// NewUserHandler 创建用户端工单 handler。
func NewUserHandler(service *ticketapp.Service, uploader FileUploader) *UserHandler {
	if service == nil || uploader == nil {
		panic("ticket user handler: required dependency is nil")
	}
	return &UserHandler{service: service, uploader: uploader}
}

// CreateTicketRequest 创建工单请求体。
type CreateTicketRequest struct {
	Title    string `json:"title" binding:"required"`
	Content  string `json:"content" binding:"required"`
	Priority string `json:"priority"`
	OrderNo  string `json:"order_no"`
	ImageURL string `json:"image_url"`
}

// CreateTicket 用户创建工单。
func (h *UserHandler) CreateTicket(c *gin.Context) {
	uid, ok := ginutil.GetUserID(c)
	if !ok {
		return
	}

	var req CreateTicketRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ginutil.RespondBindError(c, err)
		return
	}

	ticket, err := h.service.Create(ticketapp.CreateInput{
		UserID:   uid,
		Title:    req.Title,
		Content:  req.Content,
		Priority: req.Priority,
		OrderNo:  req.OrderNo,
		ImageURL: req.ImageURL,
	})
	if err != nil {
		respondTicketError(c, err, "error.ticket_create_failed")
		return
	}

	response.Success(c, ticketpresenter.NewTicketDetail(ticket))
}

// ListTickets 用户端工单列表。
func (h *UserHandler) ListTickets(c *gin.Context) {
	uid, ok := ginutil.GetUserID(c)
	if !ok {
		return
	}

	page, pageSize := ginutil.ParsePagination(c)
	status := strings.TrimSpace(c.Query("status"))

	tickets, total, err := h.service.ListByUser(ticketcontract.UserListFilter{
		Page:     page,
		PageSize: pageSize,
		UserID:   uid,
		Status:   status,
	})
	if err != nil {
		ginutil.RespondError(c, response.CodeInternal, "error.ticket_fetch_failed", err)
		return
	}

	pagination := response.BuildPagination(page, pageSize, total)
	response.SuccessWithPage(c, ticketpresenter.NewTicketSummaryList(tickets), pagination)
}

// GetTicket 用户端工单详情。
func (h *UserHandler) GetTicket(c *gin.Context) {
	uid, ok := ginutil.GetUserID(c)
	if !ok {
		return
	}
	id, err := ginutil.ParseParamUint(c, "id")
	if err != nil {
		ginutil.RespondError(c, response.CodeBadRequest, "error.ticket_not_found", nil)
		return
	}

	ticket, err := h.service.GetForUser(id, uid)
	if err != nil {
		respondTicketError(c, err, "error.ticket_fetch_failed")
		return
	}

	response.Success(c, ticketpresenter.NewTicketDetail(ticket))
}

// ReplyTicketRequest 用户回复工单请求体。
type ReplyTicketRequest struct {
	Content  string `json:"content" binding:"required"`
	ImageURL string `json:"image_url"`
}

// ReplyTicket 用户回复工单。
func (h *UserHandler) ReplyTicket(c *gin.Context) {
	uid, ok := ginutil.GetUserID(c)
	if !ok {
		return
	}
	id, err := ginutil.ParseParamUint(c, "id")
	if err != nil {
		ginutil.RespondError(c, response.CodeBadRequest, "error.ticket_not_found", nil)
		return
	}

	var req ReplyTicketRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ginutil.RespondBindError(c, err)
		return
	}

	message, err := h.service.UserReply(id, uid, req.Content, req.ImageURL)
	if err != nil {
		respondTicketError(c, err, "error.ticket_reply_failed")
		return
	}

	response.Success(c, ticketpresenter.NewTicketMessageResp(message))
}

// UploadImage 工单图片上传（凭证/截图）。
func (h *UserHandler) UploadImage(c *gin.Context) {
	if _, ok := ginutil.GetUserID(c); !ok {
		return
	}

	file, err := c.FormFile("file")
	if err != nil {
		ginutil.RespondError(c, response.CodeBadRequest, "error.file_missing", nil)
		return
	}

	result, err := h.uploader.SaveFileWithMeta(file, "ticket")
	if err != nil {
		if isUploadValidationError(err) {
			ginutil.RespondErrorWithMsg(c, response.CodeBadRequest, err.Error(), nil)
			return
		}
		ginutil.RespondError(c, response.CodeInternal, "error.upload_failed", err)
		return
	}

	response.Success(c, gin.H{"url": result.URL})
}

// Badge 用户端工单未读徽标（未关闭工单数）。
func (h *UserHandler) Badge(c *gin.Context) {
	uid, ok := ginutil.GetUserID(c)
	if !ok {
		return
	}
	count, err := h.service.UserBadge(uid)
	if err != nil {
		ginutil.RespondError(c, response.CodeInternal, "error.ticket_fetch_failed", err)
		return
	}
	response.Success(c, gin.H{"count": count})
}

func isUploadValidationError(err error) bool {
	var marker interface{ UploadValidationError() }
	return errors.As(err, &marker)
}

func respondTicketError(c *gin.Context, err error, fallbackKey string) {
	switch {
	case errors.Is(err, ticketapp.ErrTicketNotFound):
		ginutil.RespondError(c, response.CodeNotFound, "error.ticket_not_found", err)
	case errors.Is(err, ticketapp.ErrTicketTitleEmpty):
		ginutil.RespondError(c, response.CodeBadRequest, "error.ticket_title_required", err)
	case errors.Is(err, ticketapp.ErrTicketContentEmpty):
		ginutil.RespondError(c, response.CodeBadRequest, "error.ticket_content_required", err)
	case errors.Is(err, ticketapp.ErrTicketClosed):
		ginutil.RespondError(c, response.CodeBadRequest, "error.ticket_closed", err)
	case errors.Is(err, ticketapp.ErrTicketPriorityBad):
		ginutil.RespondError(c, response.CodeBadRequest, "error.ticket_priority_invalid", err)
	case errors.Is(err, ticketapp.ErrTicketOrderInvalid):
		ginutil.RespondError(c, response.CodeBadRequest, "error.ticket_order_invalid", err)
	default:
		ginutil.RespondError(c, response.CodeInternal, fallbackKey, err)
	}
}
