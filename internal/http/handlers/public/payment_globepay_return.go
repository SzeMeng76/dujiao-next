package public

import (
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
)

// GlobepayReturn 处理 Globepay 支付完成后的跳转
// Globepay 支付完成后跳回 /api/v1/payments/globepay/return?order_no=xxx
// 这里再 redirect 到前端支付页面，带上 globepay_return=1 marker
func (h *Handler) GlobepayReturn(c *gin.Context) {
	orderNo := strings.TrimSpace(c.Query("order_no"))
	guest := strings.TrimSpace(c.Query("guest"))

	baseURL := h.resolveSitemapBaseURL(c)

	var target string
	if guest == "1" {
		target = fmt.Sprintf("%s/pay?order_no=%s&globepay_return=1&guest=1", baseURL, orderNo)
	} else {
		target = fmt.Sprintf("%s/pay?order_no=%s&globepay_return=1", baseURL, orderNo)
	}

	c.Redirect(302, target)
}
