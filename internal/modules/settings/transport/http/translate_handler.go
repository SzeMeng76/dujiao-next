package settingshttp

import (
	"context"
	"errors"
	"strings"

	ginutil "github.com/dujiao-next/internal/platform/http/ginutil"

	settingsintegration "github.com/dujiao-next/internal/modules/settings/schema/integration"
	"github.com/dujiao-next/internal/platform/http/response"

	"github.com/gin-gonic/gin"
)

var (
	ErrTranslateNotConfigured = errors.New("translate: not configured")
	ErrTranslateFailed        = errors.New("translate: upstream call failed")
	ErrJobNotFound            = errors.New("translation job not found")
	ErrSeoSourceInvalid       = errors.New("seo: source text required")
)

const translateFieldsMax = 20

// SeoSource 是生成 SEO 文案时提交给 AI 的商品事实。
type SeoSource struct {
	SiteName    string `json:"site_name"`
	Title       string `json:"title"`
	Category    string `json:"category"`
	Price       string `json:"price"`
	Description string `json:"description"`
	Content     string `json:"content"`
}

// SeoEntry 是某一语言的生成结果。
type SeoEntry struct {
	Locale      string `json:"locale"`
	Title       string `json:"title"`
	Keywords    string `json:"keywords"`
	Description string `json:"description"`
}

// TranslationAdminService 是后台 AI 翻译设置与翻译动作所需端口。
type TranslationAdminService interface {
	GetTranslationSetting() (settingsintegration.TranslationSetting, error)
	PatchTranslationSetting(patch settingsintegration.TranslationSetting) (settingsintegration.TranslationSetting, error)
	Translate(ctx context.Context, fields map[string]string) (map[string]map[string]string, error)
	GenerateSeo(ctx context.Context, source SeoSource, locales []string) (map[string]SeoEntry, error)
}

// TranslationJobService 是异步翻译任务所需端口。
type TranslationJobService interface {
	SubmitJob(ctx context.Context, fields map[string]string) (string, error)
	GetJobStatus(ctx context.Context, jobID string) (interface{}, error)
}

// TranslationHandler 处理后台 AI 翻译设置与翻译请求。
type TranslationHandler struct {
	translation TranslationAdminService
	jobService  TranslationJobService
}

func NewTranslationHandler(translation TranslationAdminService) *TranslationHandler {
	if translation == nil {
		panic("settings translation handler: translation is nil")
	}
	return &TranslationHandler{translation: translation}
}

func NewTranslationHandlerWithJobService(translation TranslationAdminService, jobService TranslationJobService) *TranslationHandler {
	if translation == nil {
		panic("settings translation handler: translation is nil")
	}
	return &TranslationHandler{translation: translation, jobService: jobService}
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

// SubmitTranslateJob 提交异步翻译任务
func (h *TranslationHandler) SubmitTranslateJob(c *gin.Context) {
	if h.jobService == nil {
		ginutil.RespondError(c, response.CodeInternal, "error.feature_not_available", nil)
		return
	}

	var req translateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ginutil.RespondBindError(c, err)
		return
	}
	if len(req.Fields) == 0 {
		ginutil.RespondError(c, response.CodeBadRequest, "error.bad_request", nil)
		return
	}

	jobID, err := h.jobService.SubmitJob(c.Request.Context(), req.Fields)
	if err != nil {
		ginutil.RespondError(c, response.CodeInternal, "error.translation_job_submit_failed", err)
		return
	}

	response.Success(c, gin.H{"job_id": jobID})
}

// GetTranslateJobStatus 查询翻译任务状态
func (h *TranslationHandler) GetTranslateJobStatus(c *gin.Context) {
	if h.jobService == nil {
		ginutil.RespondError(c, response.CodeInternal, "error.feature_not_available", nil)
		return
	}

	jobID := c.Param("id")
	if jobID == "" {
		ginutil.RespondError(c, response.CodeBadRequest, "error.bad_request", nil)
		return
	}

	job, err := h.jobService.GetJobStatus(c.Request.Context(), jobID)
	if err != nil {
		ginutil.RespondError(c, response.CodeInternal, "error.translation_job_query_failed", err)
		return
	}

	response.Success(c, job)
}

type generateSeoRequest struct {
	Locales []string  `json:"locales"`
	Source  SeoSource `json:"source" binding:"required"`
}

// GenerateSeo 根据商品事实生成各语言的 SEO 标题 / 关键词 / 描述。
// 只返回生成结果，不写库——由后台在用户确认后再保存。
func (h *TranslationHandler) GenerateSeo(c *gin.Context) {
	var req generateSeoRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ginutil.RespondBindError(c, err)
		return
	}
	if strings.TrimSpace(req.Source.Title) == "" {
		ginutil.RespondError(c, response.CodeBadRequest, "error.seo_source_required", nil)
		return
	}

	result, err := h.translation.GenerateSeo(c.Request.Context(), req.Source, req.Locales)
	if err != nil {
		switch {
		case errors.Is(err, ErrTranslateNotConfigured):
			ginutil.RespondError(c, response.CodeBadRequest, "error.translation_not_configured", nil)
		case errors.Is(err, ErrSeoSourceInvalid):
			ginutil.RespondError(c, response.CodeBadRequest, "error.seo_source_required", nil)
		default:
			ginutil.RespondError(c, response.CodeInternal, "error.translation_failed", err)
		}
		return
	}

	locales := make([]string, 0, len(result))
	for locale := range result {
		locales = append(locales, locale)
	}
	response.Success(c, gin.H{"entries": result, "locales": locales})
}
