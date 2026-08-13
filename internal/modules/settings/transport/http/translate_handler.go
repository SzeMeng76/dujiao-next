package settingshttp

import (
	"context"
	"errors"

	ginutil "github.com/dujiao-next/internal/platform/http/ginutil"

	settingsintegration "github.com/dujiao-next/internal/modules/settings/schema/integration"
	"github.com/dujiao-next/internal/platform/http/response"

	"github.com/gin-gonic/gin"
)

var (
	ErrTranslateNotConfigured = errors.New("translate: not configured")
	ErrTranslateFailed        = errors.New("translate: upstream call failed")
)

const translateFieldsMax = 20

// TranslationAdminService 是后台 AI 翻译设置与翻译动作所需端口。
type TranslationAdminService interface {
	GetTranslationSetting() (settingsintegration.TranslationSetting, error)
	PatchTranslationSetting(patch settingsintegration.TranslationSetting) (settingsintegration.TranslationSetting, error)
	Translate(ctx context.Context, fields map[string]string) (map[string]map[string]string, error)
}

// TranslationHandler 处理后台 AI 翻译设置与翻译请求。
type TranslationHandler struct {
	translation TranslationAdminService
}

func NewTranslationHandler(translation TranslationAdminService) *TranslationHandler {
	if translation == nil {
		panic("settings translation handler: translation is nil")
	}
	return &TranslationHandler{translation: translation}
}

// GetTranslation 获取 AI 翻译配置（脱敏）。
func (h *TranslationHandler) GetTranslation(c *gin.Context) {
	setting, err := h.translation.GetTranslationSetting()
	if err != nil {
		ginutil.RespondError(c, response.CodeInternal, "error.settings_fetch_failed", err)
		return
	}
	response.Success(c, settingsintegration.MaskTranslationSettingForAdmin(setting))
}

// UpdateTranslation 更新 AI 翻译配置。
func (h *TranslationHandler) UpdateTranslation(c *gin.Context) {
	var req settingsintegration.TranslationSetting
	if err := c.ShouldBindJSON(&req); err != nil {
		ginutil.RespondBindError(c, err)
		return
	}

	setting, err := h.translation.PatchTranslationSetting(req)
	if err != nil {
		switch {
		case errors.Is(err, settingsintegration.ErrTranslationConfigInvalid):
			ginutil.RespondErrorWithMsg(c, response.CodeBadRequest, err.Error(), nil)
		default:
			ginutil.RespondError(c, response.CodeInternal, "error.settings_save_failed", err)
		}
		return
	}
	response.Success(c, settingsintegration.MaskTranslationSettingForAdmin(setting))
}

type translateRequest struct {
	Fields map[string]string `json:"fields" binding:"required"`
}

// Translate 把给定字段的 zh-CN 文本翻译为 zh-TW / en-US。
func (h *TranslationHandler) Translate(c *gin.Context) {
	var req translateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ginutil.RespondBindError(c, err)
		return
	}
	if len(req.Fields) == 0 {
		ginutil.RespondError(c, response.CodeBadRequest, "error.bad_request", nil)
		return
	}
	if len(req.Fields) > translateFieldsMax {
		ginutil.RespondError(c, response.CodeBadRequest, "error.bad_request", nil)
		return
	}

	result, err := h.translation.Translate(c.Request.Context(), req.Fields)
	if err != nil {
		switch {
		case errors.Is(err, ErrTranslateNotConfigured):
			ginutil.RespondError(c, response.CodeBadRequest, "error.translation_not_configured", nil)
		default:
			ginutil.RespondError(c, response.CodeInternal, "error.translation_failed", err)
		}
		return
	}
	response.Success(c, gin.H{"fields": result})
}
