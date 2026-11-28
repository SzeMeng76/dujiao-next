package acg

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/dujiao-next/internal/cache"
	"github.com/dujiao-next/internal/constants"
	"github.com/dujiao-next/internal/logger"
	downstreamcallbackdomain "github.com/dujiao-next/internal/modules/downstreamcallback/domain"
	orderdomain "github.com/dujiao-next/internal/modules/order/domain"
	upstreamhttp "github.com/dujiao-next/internal/modules/upstreamapi/transport/http"

	"github.com/gin-gonic/gin"
)

// Trade POST /shared/commodity/trade（shared_code,num,request_no）
func (h *Handler) Trade(c *gin.Context) {
	credential, form, ok := h.authenticate(c)
	if !ok {
		return
	}

	code := strings.TrimSpace(form["shared_code"])
	requestNo := strings.TrimSpace(form["request_no"])
	quantity := atoiOr(form["num"], 1)
	if code == "" || requestNo == "" {
		respondError(c, "参数缺失（shared_code/request_no 必填）")
		return
	}
	if quantity < 1 {
		respondError(c, "购买数量非法")
		return
	}

	product, options, found := h.loadProduct(code)
	if !found {
		respondError(c, msgUnavailable)
		return
	}
	sku, skuOK := selectSKU(options, form["race"])
	if !skuOK {
		respondError(c, msgRaceInvalid)
		return
	}
	if product.MinPurchaseQuantity > 0 && quantity < product.MinPurchaseQuantity {
		respondError(c, msgMinPurchase)
		return
	}
	if product.MaxPurchaseQuantity > 0 && quantity > product.MaxPurchaseQuantity {
		respondError(c, msgMaxPurchase)
		return
	}

	// 幂等：同一凭证 + 同一 request_no 串行，后到的请求读到先到请求写入的引用。
	downstreamOrderNo := orderPrefix + requestNo
	lockCtx, cancelLock := context.WithTimeout(c.Request.Context(), 30*time.Second)
	unlock, lockErr := cache.AcquireLock(lockCtx,
		fmt.Sprintf("upstream:order_create:%d:%s", credential.ID, downstreamOrderNo), 60*time.Second)
	cancelLock()
	if lockErr != nil {
		logger.Warnw("acg_supply_order_lock_failed",
			"credential_id", credential.ID, "request_no", requestNo, "error", lockErr)
		respondError(c, msgRetryable)
		return
	}
	defer unlock()

	if existingRef, _ := h.base.DownstreamRefs.GetByCredentialAndDownstreamNo(credential.ID, downstreamOrderNo); existingRef != nil {
		// 已取消的旧单（如上次余额不足）允许下游用同一 request_no 重下。
		if existingOrder, err := h.base.Orders.GetOrderByUser(existingRef.OrderID, credential.UserID); err == nil &&
			existingOrder != nil && existingOrder.Status != constants.OrderStatusCanceled {
			h.respondReceipt(c, existingOrder)
			return
		}
	}

	order, err := h.base.Orders.CreateOrder(upstreamhttp.CreateOrderInput{
		UserID: credential.UserID,
		Items: []upstreamhttp.CreateOrderItem{{
			ProductID:       product.ID,
			SKUID:           sku.ID,
			Quantity:        quantity,
			FulfillmentType: product.FulfillmentType,
		}},
		ClientIP:        c.ClientIP(),
		SkipRiskControl: true,
	})
	if err != nil {
		switch {
		case errors.Is(err, upstreamhttp.ErrWalletInsufficient):
			respondError(c, msgNoBalance)
		case errors.Is(err, upstreamhttp.ErrStockInsufficient):
			respondError(c, msgNoStock)
		case errors.Is(err, upstreamhttp.ErrProductUnavailable), errors.Is(err, upstreamhttp.ErrSKUUnavailable):
			respondError(c, msgUnavailable)
		case errors.Is(err, upstreamhttp.ErrManualFormInvalid):
			respondError(c, "商品需要填写购买资料，暂不支持")
		default:
			logger.Errorw("acg_supply_create_order_failed", "credential_id", credential.ID, "code", code, "error", err)
			respondError(c, "下单失败，请稍后重试")
		}
		return
	}

	ref := &downstreamcallbackdomain.OrderRef{
		OrderID:           order.ID,
		ApiCredentialID:   credential.ID,
		DownstreamOrderNo: downstreamOrderNo,
		TraceID:           "acg-" + requestNo,
		CallbackStatus:    downstreamcallbackdomain.StatusPending,
	}
	if createErr := h.base.DownstreamRefs.Create(ref); createErr != nil {
		logger.Errorw("acg_supply_create_ref_failed",
			"order_id", order.ID, "credential_id", credential.ID, "request_no", requestNo, "error", createErr)
		// 引用没写进去就不能扣款，否则留下既无法幂等命中、也无法回调的已支付订单。
		if _, cancelErr := h.base.Orders.CancelOrder(order.ID, credential.UserID); cancelErr != nil {
			logger.Warnw("acg_supply_cancel_unpaid_order_failed", "order_id", order.ID, "error", cancelErr)
		}
		respondError(c, "下单失败，请稍后重试")
		return
	}

	payResult, payErr := h.base.Payments.CreatePayment(upstreamhttp.CreatePaymentInput{
		OrderID:    order.ID,
		UseBalance: true,
		ClientIP:   c.ClientIP(),
	})
	if payErr != nil || payResult == nil || !payResult.OrderPaid {
		message := "wallet payment failed"
		if payErr != nil {
			message = payErr.Error()
		}
		logger.Errorw("acg_supply_auto_wallet_pay_failed", "order_id", order.ID, "error", message)
		if _, cancelErr := h.base.Orders.CancelOrder(order.ID, credential.UserID); cancelErr != nil {
			logger.Warnw("acg_supply_cancel_unpaid_order_failed", "order_id", order.ID, "error", cancelErr)
		}
		respondError(c, msgNoBalance)
		return
	}

	// acg 要求同步交付：显式触发一次自动交付。已交付时 CreateAuto 返回「已存在」，
	// 忽略错误、重查订单即可拿到交付内容，不会重复发卡。
	if h.Fulfillment != nil {
		if _, fulfillErr := h.Fulfillment.CreateAuto(order.ID); fulfillErr != nil {
			logger.Infow("acg_supply_sync_fulfillment_pending", "order_id", order.ID, "error", fulfillErr)
		}
	}

	if fresh, fetchErr := h.base.Orders.GetOrderByUser(order.ID, credential.UserID); fetchErr == nil && fresh != nil {
		order = fresh
	}
	h.respondReceipt(c, order)
}

// Query POST /shared/commodity/query（tradeNo）
func (h *Handler) Query(c *gin.Context) {
	credential, form, ok := h.authenticate(c)
	if !ok {
		return
	}
	orderID, err := strconv.ParseUint(strings.TrimSpace(form["tradeNo"]), 10, 64)
	if err != nil || orderID == 0 {
		respondError(c, "订单号非法")
		return
	}
	order, err := h.base.Orders.GetOrderByUser(uint(orderID), credential.UserID)
	if err != nil || order == nil {
		respondError(c, "订单不存在")
		return
	}

	// acg 口径：0=未完成，1=已支付/已交付。卡密随 status=1 一起返回。
	status := 0
	switch order.Status {
	case constants.OrderStatusPaid, constants.OrderStatusFulfilling, constants.OrderStatusPartiallyDelivered,
		constants.OrderStatusDelivered, constants.OrderStatusCompleted:
		status = 1
	}
	secret := ""
	if status == 1 {
		secret = fulfillmentPayload(order)
	}
	respondOK(c, gin.H{"secret": secret, "widget": nil, "status": status})
}

// respondReceipt 已交付返回收据；未交付返回可重试文案，下游可用原 request_no 重试。
func (h *Handler) respondReceipt(c *gin.Context, order *orderdomain.Order) {
	payload := fulfillmentPayload(order)
	if payload == "" {
		// 订单已付款但交付尚未完成：下游重试同一 request_no 会命中幂等并拿到卡密。
		respondError(c, msgRetryable)
		return
	}
	respondOK(c, gin.H{
		"url":     nil,
		"amount":  yuanFloat(order.TotalAmount),
		"tradeNo": strconv.FormatUint(uint64(order.ID), 10),
		"secret":  payload,
	})
}

// fulfillmentPayload 取订单（或其子订单）已交付的卡密，按换行拼接。
func fulfillmentPayload(order *orderdomain.Order) string {
	if order == nil {
		return ""
	}
	if order.Fulfillment != nil && order.Fulfillment.Status == constants.FulfillmentStatusDelivered {
		if payload := strings.TrimSpace(order.Fulfillment.Payload); payload != "" {
			return payload
		}
	}
	for i := range order.Children {
		child := &order.Children[i]
		if child.Fulfillment == nil || child.Fulfillment.Status != constants.FulfillmentStatusDelivered {
			continue
		}
		if payload := strings.TrimSpace(child.Fulfillment.Payload); payload != "" {
			return payload
		}
	}
	return ""
}
