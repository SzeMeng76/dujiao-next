package ginutil

import (
	"errors"
	"fmt"
	"strings"

	"github.com/dujiao-next/internal/i18n"
	"github.com/dujiao-next/internal/logger"
	"github.com/dujiao-next/internal/platform/http/response"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
	"go.uber.org/zap"
)

// RequestLog 提供携带 request_id 的日志实例。
func RequestLog(c *gin.Context) *zap.SugaredLogger {
	if c == nil {
		return logger.S()
	}
	if requestID, ok := c.Get("request_id"); ok {
		if id, ok := requestID.(string); ok && id != "" {
			return logger.SW("request_id", id)
		}
	}
	return logger.S()
}

// RespondError 返回国际化错误响应，并在有原始错误时记录日志。
func RespondError(c *gin.Context, code int, key string, err error) {
	locale := i18n.ResolveLocale(c)
	msg := i18n.T(locale, key)
	appErr := response.WrapError(code, msg, err)
	if err != nil {
		RequestLog(c).Errorw("handler_error",
			"code", appErr.Code,
			"message", appErr.Message,
			"error", err,
		)
	}
	// 错误信息挂到 gin.Context，供 ErrorLogMiddleware 落库。
	// 这里传 i18n key 而不是翻译后的文案：后台按原因分组统计不受请求语言影响。
	attachErrorContext(c, code, key, err)
	response.Error(c, appErr.Code, appErr.Message)
}

// RespondBindError 处理 ShouldBindJSON 等绑定错误，返回具体的字段校验提示。
func RespondBindError(c *gin.Context, err error) {
	locale := i18n.ResolveLocale(c)

	var ve validator.ValidationErrors
	if errors.As(err, &ve) {
		details := make([]string, 0, len(ve))
		for _, fe := range ve {
			details = append(details, formatFieldError(locale, fe))
		}
		msg := strings.Join(details, "; ")
		RequestLog(c).Warnw("bind_validation_error", "details", msg, "error", err)
		response.Error(c, response.CodeBadRequest, msg)
		return
	}

	// 非校验错误（JSON 格式错误等），回退到通用提示
	msg := i18n.T(locale, "error.bad_request")
	RequestLog(c).Warnw("bind_error", "message", msg, "error", err)
	response.Error(c, response.CodeBadRequest, msg)
}

// formatFieldError 将单个字段校验错误格式化为可读消息。
func formatFieldError(locale string, fe validator.FieldError) string {
	field := fe.Field()
	tag := fe.Tag()
	param := fe.Param()

	// 尝试查找 i18n 中的自定义翻译: validation.Field.tag
	customKey := fmt.Sprintf("validation.%s.%s", field, tag)
	if msg := i18n.T(locale, customKey); msg != customKey {
		return msg
	}

	// 通用规则翻译
	ruleKey := "validation.rule." + tag
	if ruleMsg := i18n.T(locale, ruleKey); ruleMsg != ruleKey {
		if param != "" {
			return fmt.Sprintf("%s: %s", field, fmt.Sprintf(ruleMsg, param))
		}
		return fmt.Sprintf("%s: %s", field, ruleMsg)
	}

	// 最终回退
	if param != "" {
		return fmt.Sprintf("%s: %s=%s", field, tag, param)
	}
	return fmt.Sprintf("%s: %s", field, tag)
}

// RespondErrorWithMsg 返回自定义消息错误响应，并在有原始错误时记录日志。
func RespondErrorWithMsg(c *gin.Context, code int, msg string, err error) {
	appErr := response.WrapError(code, msg, err)
	if err != nil {
		RequestLog(c).Errorw("handler_error",
			"code", appErr.Code,
			"message", appErr.Message,
			"error", err,
		)
	}
	// 自定义消息直接作为错误日志的 message（调用方传的已是最终文案，没有 key 可查）。
	attachErrorContext(c, code, msg, err)
	response.Error(c, appErr.Code, appErr.Message)
}

// ServerErrorStatusKey 是塞进 gin.Context 的业务状态码键名。
//
// 为什么不直接用 c.Writer.Status()：response.Error() 遵循本项目「HTTP 200 +
// body.status_code」的约定，业务 500 的 HTTP 状态码仍是 200。只看 Writer.Status()
// 会漏掉全部业务错误，因此这里显式把业务码透传给 ErrorLogMiddleware。
const ServerErrorStatusKey = "error_log_status_code"

// ServerErrorMessageKey 是塞进 gin.Context 的错误描述键名，供 ErrorLogMiddleware 落库。
//
// 为什么不复用 c.Errors：LoggerMiddleware 见到 c.Errors 非空就会打 error 级别日志，
// 4xx 挂上去会把正常的用户输错刷成 error 日志，所以 4xx 不能走 c.Error()。
// 这个独立键解决的是「4xx 没有 c.Errors 可读、message 只能兜底成 server error」的问题。
const ServerErrorMessageKey = "error_log_message"

// attachErrorContext 把业务状态码与错误描述挂到 gin.Context 上，供 ErrorLogMiddleware 落库。
//
// 状态码一律透传（含 4xx）：中间件需要它才能实现「可选收集 4xx」。
// message 传 i18n key（RespondError）或已确定的文案（RespondErrorWithMsg）。
// c.Error(err) 仍只在 5xx 时调用。
func attachErrorContext(c *gin.Context, code int, message string, err error) {
	if c == nil {
		return
	}
	c.Set(ServerErrorStatusKey, code)
	if message != "" {
		c.Set(ServerErrorMessageKey, message)
	}
	if code < response.CodeInternal {
		return
	}
	if err == nil {
		err = fmt.Errorf("server error: status %d", code)
	}
	_ = c.Error(err)
}
