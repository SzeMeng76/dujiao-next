package paymenthttp

import (
	"errors"
	"html/template"
	"net/http"

	"github.com/dujiao-next/internal/platform/http/response"
	ginutil "github.com/dujiao-next/internal/platform/http/ginutil"
	"github.com/gin-gonic/gin"
)

// RedirectHandler 处理支付网关重定向（如 Nihaopay 表单提交）
type RedirectHandler struct {
	payments PaymentWriter
}

func NewRedirectHandler(payments PaymentWriter) *RedirectHandler {
	if payments == nil {
		panic("payment redirect handler: required dependency is nil")
	}
	return &RedirectHandler{payments: payments}
}

// NihaopayRedirect 处理 Nihaopay 表单自动提交页面
func (h *RedirectHandler) NihaopayRedirect(c *gin.Context) {
	paymentID, err := ginutil.ParseParamUint(c, "id")
	if err != nil {
		ginutil.RespondError(c, response.CodeBadRequest, "error.payment_invalid", nil)
		return
	}

	payment, err := h.payments.GetPayment(paymentID)
	if err != nil {
		if errors.Is(err, ErrPaymentNotFound) {
			ginutil.RespondError(c, response.CodeNotFound, "error.payment_not_found", nil)
			return
		}
		ginutil.RespondError(c, response.CodeInternal, "error.payment_fetch_failed", err)
		return
	}

	if payment.GatewayData == nil {
		ginutil.RespondError(c, response.CodeBadRequest, "error.payment_invalid", nil)
		return
	}

	formAction, ok := payment.GatewayData["form_action"].(string)
	if !ok || formAction == "" {
		ginutil.RespondError(c, response.CodeBadRequest, "error.payment_invalid", nil)
		return
	}

	formMethod, ok := payment.GatewayData["form_method"].(string)
	if !ok || formMethod == "" {
		formMethod = "POST"
	}

	formParams, ok := payment.GatewayData["form_params"].(map[string]interface{})
	if !ok {
		formParams = make(map[string]interface{})
	}

	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Status(http.StatusOK)

	tmpl := template.Must(template.New("redirect").Parse(nihaopayRedirectHTML))
	_ = tmpl.Execute(c.Writer, map[string]interface{}{
		"FormAction": formAction,
		"FormMethod": formMethod,
		"FormParams": formParams,
	})
}

const nihaopayRedirectHTML = `<!DOCTYPE html>
<html>
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Redirecting to Payment...</title>
<style>
body {
  margin: 0;
  padding: 0;
  font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
  background: #0F172A;
  color: #F8FAFC;
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 100vh;
}
.container {
  text-align: center;
  padding: 2rem;
}
.spinner {
  width: 48px;
  height: 48px;
  border: 4px solid #1E293B;
  border-top-color: #38BDF8;
  border-radius: 50%;
  animation: spin 0.8s linear infinite;
  margin: 0 auto 1.5rem;
}
@keyframes spin {
  to { transform: rotate(360deg); }
}
h1 {
  font-size: 1.5rem;
  font-weight: 600;
  margin: 0 0 0.5rem;
}
p {
  color: #94A3B8;
  margin: 0;
}
</style>
</head>
<body>
<div class="container">
  <div class="spinner"></div>
  <h1>Redirecting to Payment Gateway</h1>
  <p>Please wait...</p>
</div>
<form id="payForm" action="{{.FormAction}}" method="{{.FormMethod}}">
  {{range $key, $value := .FormParams}}
  <input type="hidden" name="{{$key}}" value="{{$value}}">
  {{end}}
</form>
<script>
document.getElementById('payForm').submit();
</script>
</body>
</html>`

