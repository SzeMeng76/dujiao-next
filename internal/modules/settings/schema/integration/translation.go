package settingsintegration

import (
	"errors"
	"fmt"
	"strings"

	settingsvalue "github.com/dujiao-next/internal/modules/settings/schema/value"
	"github.com/dujiao-next/internal/shared/jsonmap"
)

const defaultTranslationModel = "gpt-5-mini"

var ErrTranslationConfigInvalid = errors.New("translation config invalid")

// TranslationSetting AI 翻译（机器翻译商品/内容多语言字段）配置。
type TranslationSetting struct {
	Enabled bool   `json:"enabled"`
	APIKey  string `json:"api_key"`
	BaseURL string `json:"base_url"`
	Model   string `json:"model"`
}

// DefaultTranslationSetting 返回稳定的翻译功能默认设置。
func DefaultTranslationSetting() TranslationSetting {
	return NormalizeTranslationSetting(TranslationSetting{Model: defaultTranslationModel})
}

// NormalizeTranslationSetting 归一化翻译配置。
func NormalizeTranslationSetting(setting TranslationSetting) TranslationSetting {
	setting.APIKey = strings.TrimSpace(setting.APIKey)
	setting.BaseURL = strings.TrimRight(strings.TrimSpace(setting.BaseURL), "/")
	setting.Model = strings.TrimSpace(setting.Model)
	if setting.Model == "" {
		setting.Model = defaultTranslationModel
	}
	return setting
}

// ValidateTranslationSetting 校验翻译配置合法性。
func ValidateTranslationSetting(setting TranslationSetting) error {
	if !setting.Enabled {
		return nil
	}
	if setting.APIKey == "" {
		return fmt.Errorf("%w: API Key 不能为空", ErrTranslationConfigInvalid)
	}
	if setting.Model == "" {
		return fmt.Errorf("%w: 模型名称不能为空", ErrTranslationConfigInvalid)
	}
	return nil
}

// DecodeTranslationSetting 从持久化 JSON 解码，并对缺失字段使用 fallback。
func DecodeTranslationSetting(raw jsonmap.JSON, fallback TranslationSetting) TranslationSetting {
	next := fallback
	if raw == nil {
		return next
	}
	if value, exists := raw["enabled"]; exists {
		next.Enabled = settingsvalue.ParseBool(value)
	}
	if value, exists := raw["api_key"]; exists {
		if text, ok := value.(string); ok {
			next.APIKey = text
		}
	}
	if value, exists := raw["base_url"]; exists {
		if text, ok := value.(string); ok {
			next.BaseURL = text
		}
	}
	if value, exists := raw["model"]; exists {
		if text, ok := value.(string); ok {
			next.Model = text
		}
	}
	return NormalizeTranslationSetting(next)
}

// EncodeTranslationSetting 将翻译配置编码为 settings 表结构。
func EncodeTranslationSetting(setting TranslationSetting) jsonmap.JSON {
	normalized := NormalizeTranslationSetting(setting)
	return jsonmap.JSON{
		"enabled":  normalized.Enabled,
		"api_key":  normalized.APIKey,
		"base_url": normalized.BaseURL,
		"model":    normalized.Model,
	}
}

// MaskTranslationSettingForAdmin 返回脱敏后的翻译配置（不回传 API Key 明文）。
func MaskTranslationSettingForAdmin(setting TranslationSetting) jsonmap.JSON {
	normalized := NormalizeTranslationSetting(setting)
	return jsonmap.JSON{
		"enabled":     normalized.Enabled,
		"api_key":     "",
		"has_api_key": normalized.APIKey != "",
		"base_url":    normalized.BaseURL,
		"model":       normalized.Model,
	}
}

// ApplyTranslationSettingPatch 把补丁应用到当前翻译配置并完成校验。
// api_key 为空字符串时保留原值，避免每次保存都要求重新输入。
func ApplyTranslationSettingPatch(current TranslationSetting, patch TranslationSetting) (TranslationSetting, error) {
	next := current
	next.Enabled = patch.Enabled
	next.BaseURL = patch.BaseURL
	next.Model = patch.Model
	if strings.TrimSpace(patch.APIKey) != "" {
		next.APIKey = patch.APIKey
	}

	normalized := NormalizeTranslationSetting(next)
	if err := ValidateTranslationSetting(normalized); err != nil {
		return TranslationSetting{}, err
	}
	return normalized, nil
}
