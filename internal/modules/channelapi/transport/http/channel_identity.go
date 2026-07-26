package channelhttp

import (
	"strings"

	externalidentitydomain "github.com/dujiao-next/internal/modules/identity/externalidentity/domain"
	userdomain "github.com/dujiao-next/internal/modules/identity/user/domain"

	"github.com/gin-gonic/gin"
)

type telegramIdentityRequest struct {
	ChannelUserID  string `json:"channel_user_id"`
	TelegramUserID string `json:"telegram_user_id"`
	Username       string `json:"username"`
	TelegramUser   string `json:"telegram_username"`
	FirstName      string `json:"first_name"`
	LastName       string `json:"last_name"`
	AvatarURL      string `json:"avatar_url"`
	Locale         string `json:"locale"`
}

type telegramBindRequest struct {
	ChannelUserID  string `json:"channel_user_id"`
	TelegramUserID string `json:"telegram_user_id"`
	Username       string `json:"username"`
	TelegramUser   string `json:"telegram_username"`
	FirstName      string `json:"first_name"`
	LastName       string `json:"last_name"`
	AvatarURL      string `json:"avatar_url"`
	BindMode       string `json:"bind_mode"`
	Email          string `json:"email"`
	Code           string `json:"code"`
}

// ResolveTelegramIdentity POST /api/v1/channel/identities/telegram/resolve
func (h *Handler) ResolveTelegramIdentity(c *gin.Context) {
	var req telegramIdentityRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondChannelBindError(c, err)
		return
	}

	input := buildTelegramChannelIdentityInput(req)
	if strings.TrimSpace(input.ChannelUserID) == "" {
		respondChannelError(c, 400, 400, "validation_error", "error.bad_request", nil)
		return
	}

	user, identity, err := h.UserAuthService.ResolveTelegramChannelIdentity(input)
	if err != nil {
		respondChannelIdentityServiceError(c, err)
		return
	}
	if identity == nil || user == nil {
		respondChannelSuccess(c, gin.H{"bound": false})
		return
	}

	respondChannelSuccess(c, buildChannelIdentityResponse(true, false, user, identity, h.UserAuthServiceConcrete))
}

// ProvisionTelegramIdentity POST /api/v1/channel/identities/telegram/provision
func (h *Handler) ProvisionTelegramIdentity(c *gin.Context) {
	var req telegramIdentityRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondChannelBindError(c, err)
		return
	}

	input := buildTelegramChannelIdentityInput(req)
	if strings.TrimSpace(input.ChannelUserID) == "" {
		respondChannelError(c, 400, 400, "validation_error", "error.bad_request", nil)
		return
	}

	user, identity, created, err := h.UserAuthService.ProvisionTelegramChannelIdentity(input)
	if err != nil {
		respondChannelIdentityServiceError(c, err)
		return
	}

	respondChannelSuccess(c, buildChannelIdentityResponse(true, created, user, identity, h.UserAuthServiceConcrete))
}

// BindTelegramIdentity POST /api/v1/channel/identities/telegram/bind
func (h *Handler) BindTelegramIdentity(c *gin.Context) {
	var req telegramBindRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondChannelBindError(c, err)
		return
	}
	if mode := strings.ToLower(strings.TrimSpace(req.BindMode)); mode != "" && mode != "email_code" {
		respondChannelError(c, 400, 400, "validation_error", "error.bad_request", nil)
		return
	}

	user, identity, previousUserID, err := h.UserAuthService.BindTelegramChannelByEmailCode(BindTelegramIdentityInput{
		Identity: telegramChannelIdentityInput(
			req.ChannelUserID,
			req.TelegramUserID,
			req.Username,
			req.TelegramUser,
			req.FirstName,
			req.LastName,
			req.AvatarURL,
		),
		Email: req.Email,
		Code:  req.Code,
	})
	if err != nil {
		respondChannelIdentityServiceError(c, err)
		return
	}

	resp := buildChannelIdentityResponse(true, false, user, identity, h.UserAuthServiceConcrete)
	resp["bound"] = true
	if previousUserID != 0 {
		resp["previous_user_id"] = previousUserID
	}
	respondChannelSuccess(c, resp)
}

// GetCurrentIdentity GET /api/v1/channel/me
func (h *Handler) GetCurrentIdentity(c *gin.Context) {
	input := TelegramIdentityInput{
		ChannelUserID: channelUserIDFromQuery(c),
		Username:      strings.TrimSpace(c.Query("username")),
		AvatarURL:     strings.TrimSpace(c.Query("avatar_url")),
	}
	if strings.TrimSpace(input.ChannelUserID) == "" {
		respondChannelError(c, 400, 400, "validation_error", "error.bad_request", nil)
		return
	}

	user, identity, err := h.UserAuthService.ResolveTelegramChannelIdentity(input)
	if err != nil {
		respondChannelIdentityServiceError(c, err)
		return
	}
	if identity == nil || user == nil {
		respondChannelSuccess(c, gin.H{"bound": false})
		return
	}

	respondChannelSuccess(c, buildChannelIdentityResponse(true, false, user, identity, h.UserAuthServiceConcrete))
}

func buildTelegramChannelIdentityInput(req telegramIdentityRequest) TelegramIdentityInput {
	return telegramChannelIdentityInput(
		req.ChannelUserID,
		req.TelegramUserID,
		req.Username,
		req.TelegramUser,
		req.FirstName,
		req.LastName,
		req.AvatarURL,
	)
}

func telegramChannelIdentityInput(channelUserID, legacyUserID, username, legacyUsername, firstName, lastName, avatarURL string) TelegramIdentityInput {
	return TelegramIdentityInput{
		ChannelUserID: channelUserIDValue(channelUserID, legacyUserID),
		Username:      strings.TrimSpace(firstNonEmpty(username, legacyUsername)),
		FirstName:     strings.TrimSpace(firstName),
		LastName:      strings.TrimSpace(lastName),
		AvatarURL:     strings.TrimSpace(avatarURL),
	}
}

func (h *Handler) provisionTelegramChannelUserID(input TelegramIdentityInput) (uint, error) {
	return h.UserAuthService.ProvisionTelegramChannelUserID(input)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// buildChannelIdentityResponse 构造 Telegram 渠道身份响应载荷。
// 如果用户是占位账号，会自动生成 JWT token 以支持升级流程。
func buildChannelIdentityResponse(bound, created bool, user *userdomain.User, identity *externalidentitydomain.Identity, jwtGenerator JWTGenerator) gin.H {
	resp := gin.H{"bound": bound}
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
		if user.PasswordSetupRequired && jwtGenerator != nil {
			if token, _, err := jwtGenerator.GenerateUserJWT(user, 0); err == nil {
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
