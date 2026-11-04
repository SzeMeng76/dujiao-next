package errorloghttp

import (
	"strings"

	"github.com/dujiao-next/internal/modules/errorlog/contract"
	"github.com/dujiao-next/internal/modules/errorlog/domain"
	"github.com/dujiao-next/internal/platform/http/ginutil"
	"github.com/dujiao-next/internal/platform/http/response"

	"github.com/gin-gonic/gin"
)

// LogReader 管理端只读接口（transport 层依赖接口，不依赖具体 service）。
type LogReader interface {
	ListForAdmin(filter contract.Filter) ([]domain.ErrorLog, int64, error)
}

// AdminHandler 管理端错误日志处理器。
type AdminHandler struct {
	svc LogReader
}

// NewAdminHandler 创建管理端处理器。
func NewAdminHandler(svc LogReader) *AdminHandler {
	return &AdminHandler{svc: svc}
}

// List 分页查询错误日志。
func (h *AdminHandler) List(c *gin.Context) {
	page, pageSize := ginutil.ParsePagination(c)

	level := strings.TrimSpace(c.Query("level"))
	category := strings.TrimSpace(c.Query("category"))
	method := strings.TrimSpace(c.Query("method"))
	path := strings.TrimSpace(c.Query("path"))
	requestID := strings.TrimSpace(c.Query("request_id"))
	clientIP := strings.TrimSpace(c.Query("client_ip"))
	keyword := strings.TrimSpace(c.Query("keyword"))

	createdFrom, createdTo, err := ginutil.ParseQueryTimeRange(c, "created_from", "created_to")
	if err != nil {
		ginutil.RespondError(c, response.CodeBadRequest, "error.bad_request", err)
		return
	}

	items, total, err := h.svc.ListForAdmin(contract.Filter{
		Page:        page,
		PageSize:    pageSize,
		Level:       level,
		Category:    category,
		Method:      method,
		Path:        path,
		RequestID:   requestID,
		ClientIP:    clientIP,
		Keyword:     keyword,
		CreatedFrom: createdFrom,
		CreatedTo:   createdTo,
	})
	if err != nil {
		ginutil.RespondError(c, response.CodeInternal, "error.config_fetch_failed", err)
		return
	}

	pagination := response.BuildPagination(page, pageSize, total)
	response.SuccessWithPage(c, items, pagination)
}
