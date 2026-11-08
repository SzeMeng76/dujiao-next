package userauthhttp

import (
	"context"
	"errors"

	"github.com/dujiao-next/internal/i18n"
	userdomain "github.com/dujiao-next/internal/modules/identity/user/domain"

	userpresenter "github.com/dujiao-next/internal/modules/identity/userauth/transport/presenter"
	"github.com/dujiao-next/internal/platform/http/ginutil"
	"github.com/dujiao-next/internal/platform/http/response"

	"github.com/gin-gonic/gin"
)

// UserUpgradeService 是占位账号升级端点所需的最小端口。
type UserUpgradeService interface {
	SendUpgradePlaceholderCode(ctx context.Context, userID uint, newEmail, locale string) error
	UpgradePlaceholderAccount(userID uint, newEmail, code, password string) (*userdomain.User, error)
	ResolveEmailChangeMode(user *userdomain.User) (string, error)
	ResolvePasswordChangeMode(user *userdomain.User) (string, error)
}

// UserUpgradeHandler 处理占位账号（Telegram 自动开户）升级为真实邮箱账号的 HTTP 请求。
type UserUpgradeHandler struct {
	service UserUpgradeService
}

func NewUserUpgradeHandler(service UserUpgradeService) *UserUpgradeHandler {
	if service == nil {
		panic("user upgrade handler: service is nil")
	}
	return &UserUpgradeHandler{service: service}
}

// UpgradePlaceholderSendCodeRequest 升级占位账号发送验证码请求。
type UpgradePlaceholderSendCodeRequest struct {
	Email string `json:"email" binding:"required"`
}

// SendUpgradePlaceholderCode 向占位账号要升级成的邮箱发送验证码（需登录态）。
func (h *UserUpgradeHandler) SendUpgradePlaceholderCode(c *gin.Context) {
	id, ok := ginutil.GetUserID(c)
	if !ok {
		return
	}

	var req UpgradePlaceholderSendCodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ginutil.RespondBindError(c, err)
		return
	}

	locale := i18n.ResolveLocale(c)
	if err := h.service.SendUpgradePlaceholderCode(c.Request.Context(), id, req.Email, locale); err != nil {
		switch {
		case errors.Is(err, ErrUserNotFound):
			ginutil.RespondError(c, response.CodeNotFound, "error.user_not_found", nil)
		case errors.Is(err, ErrUserOAuthAlreadyBound):
			ginutil.RespondError(c, response.CodeBadRequest, "error.not_placeholder_account", nil)
		case errors.Is(err, ErrInvalidEmail):
			ginutil.RespondError(c, response.CodeBadRequest, "error.email_invalid", nil)
		case errors.Is(err, ErrEmailExists):
			ginutil.RespondError(c, response.CodeBadRequest, "error.email_exists", nil)
		case errors.Is(err, ErrVerifyCodeTooFrequent):
			ginutil.RespondError(c, response.CodeTooManyRequests, "error.verify_code_too_frequent", nil)
		case errors.Is(err, ErrEmailRecipientRejected):
			ginutil.RespondError(c, response.CodeBadRequest, "error.email_recipient_not_found", nil)
		case errors.Is(err, ErrEmailServiceDisabled),
			errors.Is(err, ErrEmailServiceNotConfigured):
			ginutil.RespondError(c, response.CodeInternal, "error.email_service_not_configured", err)
		default:
			ginutil.RespondError(c, response.CodeInternal, "error.send_verify_code_failed", err)
		}
		return
	}

	response.Success(c, gin.H{"sent": true})
}

// UpgradePlaceholderAccountRequest 升级占位账号请求。
type UpgradePlaceholderAccountRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Code     string `json:"code" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// UpgradePlaceholderAccount 将占位账号升级为真实邮箱账号。
func (h *UserUpgradeHandler) UpgradePlaceholderAccount(c *gin.Context) {
	id, ok := ginutil.GetUserID(c)
	if !ok {
		return
	}

	var req UpgradePlaceholderAccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ginutil.RespondBindError(c, err)
		return
	}

	user, err := h.service.UpgradePlaceholderAccount(id, req.Email, req.Code, req.Password)
	if err != nil {
		switch {
		case errors.Is(err, ErrUserNotFound):
			ginutil.RespondError(c, response.CodeNotFound, "error.user_not_found", nil)
		case errors.Is(err, ErrUserOAuthAlreadyBound):
			ginutil.RespondError(c, response.CodeBadRequest, "error.not_placeholder_account", nil)
		case errors.Is(err, ErrInvalidEmail):
			ginutil.RespondError(c, response.CodeBadRequest, "error.email_invalid", nil)
		case errors.Is(err, ErrEmailExists):
			ginutil.RespondError(c, response.CodeBadRequest, "error.email_exists", nil)
		case errors.Is(err, ErrVerifyCodeInvalid):
			ginutil.RespondError(c, response.CodeBadRequest, "error.verify_code_invalid", nil)
		case errors.Is(err, ErrVerifyCodeExpired):
			ginutil.RespondError(c, response.CodeBadRequest, "error.verify_code_expired", nil)
		case errors.Is(err, ErrWeakPassword):
			respondWeakPassword(c, err)
		default:
			ginutil.RespondError(c, response.CodeInternal, "error.upgrade_account_failed", err)
		}
		return
	}

	profile, respErr := h.upgradeProfileResponse(user)
	if respErr != nil {
		ginutil.RespondError(c, response.CodeInternal, "error.upgrade_account_failed", respErr)
		return
	}
	response.Success(c, profile)
}

func (h *UserUpgradeHandler) upgradeProfileResponse(user *userdomain.User) (userpresenter.UserProfileResp, error) {
	emailMode, err := h.service.ResolveEmailChangeMode(user)
	if err != nil {
		return userpresenter.UserProfileResp{}, err
	}
	passwordMode, err := h.service.ResolvePasswordChangeMode(user)
	if err != nil {
		return userpresenter.UserProfileResp{}, err
	}
	return userpresenter.NewUserProfileResp(user, emailMode, passwordMode), nil
}
