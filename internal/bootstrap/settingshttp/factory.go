package settingsbootstrap

import (
	"context"
	"errors"

	"github.com/dujiao-next/internal/app/container"
	"github.com/dujiao-next/internal/config"
	openaitranslate "github.com/dujiao-next/internal/modules/settings/infrastructure/openai"
	settingsintegration "github.com/dujiao-next/internal/modules/settings/schema/integration"
	settingstransport "github.com/dujiao-next/internal/modules/settings/transport/http"

	settingsapp "github.com/dujiao-next/internal/modules/settings/application"
)

func NewSMTPHandler(c *container.Container, cfg *config.Config) *settingstransport.SMTPHandler {
	return settingstransport.NewSMTPHandler(settingsSMTPAdapter{
		settings: c.SettingService, cfg: cfg, email: c.EmailSender,
	})
}

func NewCaptchaHandler(c *container.Container, cfg *config.Config) *settingstransport.CaptchaHandler {
	return settingstransport.NewCaptchaHandler(settingsCaptchaAdapter{
		settings: c.SettingService, cfg: cfg, captcha: c.CaptchaService,
	})
}

func NewTelegramAuthHandler(c *container.Container, cfg *config.Config) *settingstransport.TelegramAuthHandler {
	return settingstransport.NewTelegramAuthHandler(settingsTelegramAuthAdapter{
		settings: c.SettingService, cfg: cfg, telegramAuth: c.TelegramAuthService,
	})
}

func NewGoogleAuthHandler(c *container.Container, cfg *config.Config) *settingstransport.GoogleAuthHandler {
	return settingstransport.NewGoogleAuthHandler(settingsGoogleAuthAdapter{
		settings: c.SettingService, cfg: cfg, googleAuth: c.GoogleAuthService,
	})
}

func NewTranslationHandler(c *container.Container) *settingstransport.TranslationHandler {
	// 创建异步翻译processor
	jobProcessor := settingsapp.NewTranslationJobProcessor(
		c.SettingsStore,
		openaitranslate.New(),
		c.SettingService,
	)

	return settingstransport.NewTranslationHandlerWithJobService(
		settingsTranslationAdapter{
			settings: c.SettingService,
			client:   openaitranslate.New(),
		},
		jobProcessor,
	)
}

type settingsTranslationAdapter struct {
	settings *settingsapp.Service
	client   openaitranslate.Client
}

func (a settingsTranslationAdapter) GetTranslationSetting() (settingsintegration.TranslationSetting, error) {
	return a.settings.GetTranslationSetting()
}

func (a settingsTranslationAdapter) PatchTranslationSetting(patch settingsintegration.TranslationSetting) (settingsintegration.TranslationSetting, error) {
	return a.settings.PatchTranslationSetting(patch)
}

func (a settingsTranslationAdapter) Translate(ctx context.Context, fields map[string]string) (map[string]map[string]string, error) {
	setting, err := a.settings.GetTranslationSetting()
	if err != nil {
		return nil, err
	}
	if !setting.Enabled || setting.APIKey == "" {
		return nil, settingstransport.ErrTranslateNotConfigured
	}

	items := make([]openaitranslate.Item, 0, len(fields))
	for key, text := range fields {
		if text == "" {
			continue
		}
		items = append(items, openaitranslate.Item{Key: key, Text: text})
	}
	if len(items) == 0 {
		return map[string]map[string]string{}, nil
	}

	result, err := a.client.Translate(ctx, setting, items)
	if err != nil {
		if errors.Is(err, openaitranslate.ErrNotConfigured) {
			return nil, settingstransport.ErrTranslateNotConfigured
		}
		return nil, errors.Join(settingstransport.ErrTranslateFailed, err)
	}
	return result, nil
}
