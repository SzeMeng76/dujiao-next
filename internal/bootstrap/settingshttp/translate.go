package settingsbootstrap

import (
	"context"
	"errors"

	openaitranslate "github.com/dujiao-next/internal/modules/settings/infrastructure/openai"
	settingsintegration "github.com/dujiao-next/internal/modules/settings/schema/integration"

	settingsapp "github.com/dujiao-next/internal/modules/settings/application"
	settingstransport "github.com/dujiao-next/internal/modules/settings/transport/http"
)

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
