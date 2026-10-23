package fulfillmenthttp

import (
	"errors"
	"strings"
	"time"

	fulfillmentdomain "github.com/dujiao-next/internal/modules/fulfillment/domain"
	orderdomain "github.com/dujiao-next/internal/modules/order/domain"

	"github.com/dujiao-next/internal/platform/http/ginutil"
	"github.com/dujiao-next/internal/platform/http/response"
	"github.com/dujiao-next/internal/shared/jsonmap"

	"github.com/gin-gonic/gin"
)

var (
	ErrFulfillmentExists  = errors.New("fulfillment exists")
	ErrFulfillmentInvalid = errors.New("fulfillment invalid")
	ErrOrderStatusInvalid = errors.New("order status invalid")
	ErrOrderNotFound      = errors.New("order not found")

	ErrFulfillmentNotFound     = errors.New("fulfillment not found")
	ErrFulfillmentNotAuto      = errors.New("fulfillment not auto")
	ErrFulfillmentUpdateFailed = errors.New("fulfillment update failed")
	ErrCardSecretInsufficient  = errors.New("card secret insufficient")
)

// ReplaceSecretsInput 替换已交付订单卡密输入。
type ReplaceSecretsInput struct {
	OrderID uint
	AdminID uint
	Secrets []string
	Pairs   []SecretPair
	Reason  string
}

// SecretPair 指定用库存中的哪条卡密替换订单里的哪条已交付卡密。
type SecretPair struct {
	OldSecretID uint
	NewSecretID uint
}

// DeliveredSecretView 订单当前交付内容中的一条卡密。
type DeliveredSecretView struct {
	ID        uint       `json:"id"`
	Secret    string     `json:"secret"`
	ProductID uint       `json:"product_id"`
	SKUID     uint       `json:"sku_id"`
	UsedAt    *time.Time `json:"used_at,omitempty"`
}

// StockSecretView 可用于替换的库存卡密。
type StockSecretView struct {
	ID        uint      `json:"id"`
	Secret    string    `json:"secret"`
	ProductID uint      `json:"product_id"`
	SKUID     uint      `json:"sku_id"`
	CreatedAt time.Time `json:"created_at"`
}

// ReplaceCandidatesView 替换卡密候选数据。
type ReplaceCandidatesView struct {
	Delivered []DeliveredSecretView `json:"delivered"`
	Stock     []StockSecretView     `json:"stock"`
}

// SecretReplacer 管理端替换卡密端口，返回更新后的交付记录与替换数量。
type SecretReplacer interface {
	ReplaceSecrets(input ReplaceSecretsInput) (*fulfillmentdomain.Fulfillment, int, error)
	ListReplaceCandidates(orderID uint) (*ReplaceCandidatesView, error)
}

// CreateManualInput 创建人工交付输入。
type CreateManualInput struct {
	OrderID      uint
	AdminID      uint
	Payload      string
	DeliveryData jsonmap.JSON
}

// ManualCreator 管理端录入交付端口。
type ManualCreator interface {
	CreateManual(input CreateManualInput) (*fulfillmentdomain.Fulfillment, error)
}

// AdminOrderReader 管理端订单读取端口（下载交付用）。
type AdminOrderReader interface {
	GetOrderForAdmin(orderID uint) (*orderdomain.Order, error)
}

// AdminHandler 处理后台交付相关 HTTP 请求。
type AdminHandler struct {
	creator  ManualCreator
	orders   AdminOrderReader
	replacer SecretReplacer
}

// WithSecretReplacer 注入替换卡密能力；未注入时对应接口返回内部错误。
func (h *AdminHandler) WithSecretReplacer(replacer SecretReplacer) *AdminHandler {
	h.replacer = replacer
	return h
}

// AdminReplaceSecretsRequest 替换卡密请求，secrets 为空表示替换该订单的全部卡密。
type AdminReplaceSecretsRequest struct {
	Secrets []string                  `json:"secrets"`
	Pairs   []AdminReplaceSecretsPair `json:"pairs"`
	Reason  string                    `json:"reason" binding:"max=500"`
}

// AdminReplaceSecretsPair 旧卡密 ID 与库存新卡密 ID 的配对；非空时优先于 secrets。
type AdminReplaceSecretsPair struct {
	OldSecretID uint `json:"old_secret_id"`
	NewSecretID uint `json:"new_secret_id"`
}

// AdminListReplaceCandidates 管理端查询可替换的已交付卡密与同规格库存（最旧在前）。
func (h *AdminHandler) AdminListReplaceCandidates(c *gin.Context) {
	if h.replacer == nil {
		ginutil.RespondError(c, response.CodeInternal, "error.fulfillment_update_failed", nil)
		return
	}
	orderID, err := ginutil.ParseParamUint(c, "id")
	if err != nil {
		ginutil.RespondError(c, response.CodeBadRequest, "error.order_item_invalid", nil)
		return
	}
	candidates, err := h.replacer.ListReplaceCandidates(orderID)
	if err != nil {
		respondReplaceError(c, err)
		return
	}
	response.Success(c, candidates)
}

func respondReplaceError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrOrderNotFound), errors.Is(err, ErrFulfillmentNotFound):
		ginutil.RespondError(c, response.CodeNotFound, "error.fulfillment_not_found", nil)
	case errors.Is(err, ErrOrderStatusInvalid):
		ginutil.RespondError(c, response.CodeBadRequest, "error.order_status_invalid", nil)
	case errors.Is(err, ErrFulfillmentInvalid), errors.Is(err, ErrFulfillmentNotAuto):
		ginutil.RespondError(c, response.CodeBadRequest, "error.fulfillment_invalid", nil)
	case errors.Is(err, ErrCardSecretInsufficient):
		ginutil.RespondError(c, response.CodeBadRequest, "error.card_secret_insufficient", nil)
	default:
		ginutil.RespondError(c, response.CodeInternal, "error.fulfillment_update_failed", err)
	}
}

// AdminReplaceFulfillmentSecrets 管理端替换已交付订单的卡密并重新推送。
func (h *AdminHandler) AdminReplaceFulfillmentSecrets(c *gin.Context) {
	if h.replacer == nil {
		ginutil.RespondError(c, response.CodeInternal, "error.fulfillment_update_failed", nil)
		return
	}
	adminID, ok := ginutil.GetAdminID(c)
	if !ok {
		return
	}
	orderID, err := ginutil.ParseParamUint(c, "id")
	if err != nil {
		ginutil.RespondError(c, response.CodeBadRequest, "error.order_item_invalid", nil)
		return
	}
	var req AdminReplaceSecretsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ginutil.RespondBindError(c, err)
		return
	}
	pairs := make([]SecretPair, 0, len(req.Pairs))
	for _, pair := range req.Pairs {
		pairs = append(pairs, SecretPair{OldSecretID: pair.OldSecretID, NewSecretID: pair.NewSecretID})
	}
	fulfillment, replaced, err := h.replacer.ReplaceSecrets(ReplaceSecretsInput{
		OrderID: orderID,
		AdminID: adminID,
		Secrets: req.Secrets,
		Pairs:   pairs,
		Reason:  req.Reason,
	})
	if err != nil {
		respondReplaceError(c, err)
		return
	}
	fulfillment.TruncatePayload(fulfillmentdomain.PayloadMaxPreviewLines)
	response.Success(c, gin.H{"fulfillment": fulfillment, "replaced": replaced})
}

func NewAdminHandler(creator ManualCreator, orders AdminOrderReader) *AdminHandler {
	if creator == nil {
		panic("fulfillment admin handler: creator is nil")
	}
	if orders == nil {
		panic("fulfillment admin handler: orders is nil")
	}
	return &AdminHandler{creator: creator, orders: orders}
}

// AdminCreateFulfillmentRequest 管理端录入交付请求。
type AdminCreateFulfillmentRequest struct {
	OrderID      uint         `json:"order_id" binding:"required"`
	Payload      string       `json:"payload"`
	DeliveryData jsonmap.JSON `json:"delivery_data"`
}

// AdminCreateFulfillment 管理端录入交付内容。
func (h *AdminHandler) AdminCreateFulfillment(c *gin.Context) {
	adminID, ok := ginutil.GetAdminID(c)
	if !ok {
		return
	}

	var req AdminCreateFulfillmentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ginutil.RespondBindError(c, err)
		return
	}

	fulfillment, err := h.creator.CreateManual(CreateManualInput{
		OrderID:      req.OrderID,
		AdminID:      adminID,
		Payload:      req.Payload,
		DeliveryData: req.DeliveryData,
	})
	if err != nil {
		switch {
		case errors.Is(err, ErrFulfillmentExists):
			ginutil.RespondError(c, response.CodeBadRequest, "error.fulfillment_exists", nil)
		case errors.Is(err, ErrFulfillmentInvalid):
			ginutil.RespondError(c, response.CodeBadRequest, "error.fulfillment_invalid", nil)
		case errors.Is(err, ErrOrderStatusInvalid):
			ginutil.RespondError(c, response.CodeBadRequest, "error.order_status_invalid", nil)
		case errors.Is(err, ErrOrderNotFound):
			ginutil.RespondError(c, response.CodeNotFound, "error.order_not_found", nil)
		default:
			ginutil.RespondError(c, response.CodeInternal, "error.fulfillment_create_failed", err)
		}
		return
	}

	response.Success(c, fulfillment)
}

// AdminDownloadFulfillment 管理端下载订单交付内容。
func (h *AdminHandler) AdminDownloadFulfillment(c *gin.Context) {
	orderID, err := ginutil.ParseParamUint(c, "id")
	if err != nil {
		ginutil.RespondError(c, response.CodeBadRequest, "error.order_item_invalid", nil)
		return
	}
	order, err := h.orders.GetOrderForAdmin(orderID)
	if err != nil || order == nil {
		ginutil.RespondError(c, response.CodeNotFound, "error.order_not_found", nil)
		return
	}
	payload := collectAdminFulfillmentPayload(order)
	if payload == "" {
		ginutil.RespondError(c, response.CodeNotFound, "error.fulfillment_not_found", nil)
		return
	}
	filename := "fulfillment-" + order.OrderNo + ".txt"
	c.Header("Content-Type", "text/plain; charset=utf-8")
	c.Header("Content-Disposition", "attachment; filename=\""+filename+"\"")
	c.Data(200, "text/plain; charset=utf-8", []byte(payload))
}

func collectAdminFulfillmentPayload(order *orderdomain.Order) string {
	if order.Fulfillment != nil && order.Fulfillment.Payload != "" {
		return order.Fulfillment.Payload
	}
	var parts []string
	for _, child := range order.Children {
		if child.Fulfillment != nil && child.Fulfillment.Payload != "" {
			parts = append(parts, child.Fulfillment.Payload)
		}
	}
	return strings.Join(parts, "\n")
}
