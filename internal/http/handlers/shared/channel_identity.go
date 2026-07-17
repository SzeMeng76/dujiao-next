package shared

import (
	"strings"

	"github.com/dujiao-next/internal/models"
	"github.com/dujiao-next/internal/service"

	"github.com/gin-gonic/gin"
)

// BuildChannelIdentityResponse 构造 Telegram 渠道身份响应载荷。
// 如果用户是占位账号，会自动生成 JWT token 以支持升级流程。
func BuildChannelIdentityResponse(bound, created bool, user *models.User, identity *models.UserOAuthIdentity, authService *service.UserAuthService) gin.H {
	resp := gin.H{
		"bound": bound,
	}
	if identity != nil {
		resp["identity"] = gin.H{
			"provider":         identity.Provider,
			"provider_user_id": identity.ProviderUserID,
			"username":         identity.Username,
			"avatar_url":       identity.AvatarURL,
		}
	}
	if user != nil {
		userResp := gin.H{
			"id":                      user.ID,
			"email":                   user.Email,
			"display_name":            user.DisplayName,
			"status":                  user.Status,
			"locale":                  user.Locale,
			"email_verified":          user.EmailVerifiedAt != nil,
			"password_setup_required": user.PasswordSetupRequired,
		}

		// 为占位账号生成 token，以便 bot 可以调用升级接口
		if user.PasswordSetupRequired && authService != nil {
			if token, _, err := authService.GenerateUserJWT(user, 0); err == nil {
				userResp["token"] = token
			}
		}

		resp["user"] = userResp
	}
	if bound {
		resp["created"] = created
	}
	return resp
}
