package ginutil

import (
	"errors"
	"fmt"
	"net/http"
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

// ServerErrorLevelKey 是塞进 gin.Context 的错误级别键名，供 ErrorLogMiddleware 覆盖默认级别。
//
// 默认级别由状态码推导（5xx→error，4xx→warn）。但支付回调这类端点必须对网关返回
// HTTP 200（否则会被网关当成投递失败无限重试），失败信息只能放在响应体里，
// 状态码推导不出任何级别，只能由 handler 显式声明。
const ServerErrorLevelKey = "error_log_level"

// MarkErrorLog 把「对外返回 200、但业务上已经失败」的请求标记为需要写入后台错误日志。
//
// 典型场景是支付回调：网关要求回调端点无论处理结果如何都回 HTTP 200，
// ErrorLogMiddleware 靠状态码无法识别失败，只能由 handler 主动标记。
// level 取 "error" / "warn"；message 会作为后台错误日志的 message 保存。
// 不修改响应内容，调用方仍需自行写出网关要求的响应体。
func MarkErrorLog(c *gin.Context, level, message string) {
	if c == nil {
		return
	}
	if level != "" {
		c.Set(ServerErrorLevelKey, level)
	}
	if message != "" {
		c.Set(ServerErrorMessageKey, message)
	}
	// 以 5xx 触发记录：collect_4xx 关闭时 4xx 不会被收集，而这里标记的
	// 恰恰是必须留痕的失败，不能依赖那个开关。真实级别由 ServerErrorLevelKey 决定。
	c.Set(ServerErrorStatusKey, http.StatusInternalServerError)
}

// MarkAlertErrorLog 把一条支付/上游异常告警同时标记为后台错误日志。
//
// 支付回调端点必须对网关返回 HTTP 200（否则网关按投递失败无限重试），
// webhook 端点即使返回 5xx 也可能被映射成 4xx，两者都进不了默认的错误日志收集条件。
// 为了让「发了 TG 告警」与「后台留下记录」保持一致，这里按告警自身的字段生成日志：
// 级别取 alert_level，内容取 alert_type + message，再拼上定位所需的业务标识。
//
// alertType / alertLevel / message 为空时分别回退到默认值，保证永远不会写出空日志。
func MarkAlertErrorLog(c *gin.Context, alertType, alertLevel, message string) {
	if c == nil {
		return
	}

	level := "error"
	if strings.EqualFold(strings.TrimSpace(alertLevel), "warning") ||
		strings.EqualFold(strings.TrimSpace(alertLevel), "warn") {
		level = "warn"
	}

	text := strings.TrimSpace(alertType)
	if detail := strings.TrimSpace(message); detail != "" {
		if text == "" {
			text = detail
		} else {
			text += ": " + detail
		}
	}

	MarkErrorLog(c, level, text)
}

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
