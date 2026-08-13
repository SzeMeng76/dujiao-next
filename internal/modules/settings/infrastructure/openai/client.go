// Package openaitranslate calls an OpenAI-compatible Chat Completions API to
// machine-translate admin-authored zh-CN content into zh-TW and en-US. Chat
// Completions (rather than the newer Responses API) is used deliberately:
// most third-party OpenAI-compatible relays/proxies popular with mainland
// users only mirror the older, more widely supported surface.
package openaitranslate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	settingsintegration "github.com/dujiao-next/internal/modules/settings/schema/integration"
)

const (
	defaultBaseURL   = "https://api.openai.com"
	requestTimeout   = 30 * time.Second
	maxResponseBytes = 1 << 20
	targetLocaleZhTW = "zh-TW"
	targetLocaleEnUS = "en-US"
)

var (
	ErrNotConfigured = errors.New("openai translate: not configured")
	ErrRequestFailed = errors.New("openai translate: request failed")
	ErrBadResponse   = errors.New("openai translate: unexpected response")
)

// Item is one field to translate, identified by an opaque caller-supplied key.
type Item struct {
	Key  string
	Text string
}

// Client calls the OpenAI (or compatible) Chat Completions API.
type Client struct {
	httpClient *http.Client
}

// New creates a translation client.
func New() Client {
	return Client{httpClient: &http.Client{Timeout: requestTimeout}}
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type jsonSchemaFormat struct {
	Type       string                 `json:"type"`
	JSONSchema jsonSchemaFormatDetail `json:"json_schema"`
}

type jsonSchemaFormatDetail struct {
	Name   string      `json:"name"`
	Strict bool        `json:"strict"`
	Schema interface{} `json:"schema"`
}

type chatRequest struct {
	Model          string           `json:"model"`
	Messages       []chatMessage    `json:"messages"`
	Temperature    float64          `json:"temperature"`
	ResponseFormat jsonSchemaFormat `json:"response_format"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

type translationEntry struct {
	Key  string `json:"key"`
	ZhTW string `json:"zh_TW"`
	EnUS string `json:"en_US"`
}

type translationPayload struct {
	Translations []translationEntry `json:"translations"`
}

var translationJSONSchema = map[string]interface{}{
	"type": "object",
	"properties": map[string]interface{}{
		"translations": map[string]interface{}{
			"type": "array",
			"items": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"key":   map[string]interface{}{"type": "string"},
					"zh_TW": map[string]interface{}{"type": "string"},
					"en_US": map[string]interface{}{"type": "string"},
				},
				"required":             []string{"key", "zh_TW", "en_US"},
				"additionalProperties": false,
			},
		},
	},
	"required":             []string{"translations"},
	"additionalProperties": false,
}

// Translate sends the given zh-CN items to the model and returns, for each
// input key, the translated zh-TW and en-US text (keyed by locale code).
func (c Client) Translate(ctx context.Context, cfg settingsintegration.TranslationSetting, items []Item) (map[string]map[string]string, error) {
	apiKey := strings.TrimSpace(cfg.APIKey)
	if apiKey == "" {
		return nil, ErrNotConfigured
	}
	if len(items) == 0 {
		return map[string]map[string]string{}, nil
	}

	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		return nil, ErrNotConfigured
	}

	type sourceItem struct {
		Key  string `json:"key"`
		Text string `json:"text"`
	}
	sourceItems := make([]sourceItem, 0, len(items))
	for _, item := range items {
		sourceItems = append(sourceItems, sourceItem{Key: item.Key, Text: item.Text})
	}
	sourceJSON, err := json.Marshal(sourceItems)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrRequestFailed, err)
	}

	reqBody := chatRequest{
		Model: model,
		Messages: []chatMessage{
			{
				Role: "system",
				Content: "You are a professional translator for an e-commerce admin console. " +
					"For each item, translate the \"text\" field from Simplified Chinese (zh-CN) into " +
					"Traditional Chinese using Taiwan Mandarin conventions (zh-TW) and into English (en-US). " +
					"Preserve placeholders such as {n}, HTML tags, and Markdown formatting exactly as-is. " +
					"Return every item using its original \"key\" so results can be matched back.",
			},
			{Role: "user", Content: string(sourceJSON)},
		},
		Temperature: 0,
		ResponseFormat: jsonSchemaFormat{
			Type: "json_schema",
			JSONSchema: jsonSchemaFormatDetail{
				Name:   "translation_result",
				Strict: true,
				Schema: translationJSONSchema,
			},
		},
	}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrRequestFailed, err)
	}

	requestCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodPost, baseURL+"/v1/chat/completions", strings.NewReader(string(bodyBytes)))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrRequestFailed, err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+apiKey)

	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrRequestFailed, err)
	}
	defer response.Body.Close()

	limited := io.LimitReader(response.Body, maxResponseBytes)
	if response.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(limited)
		return nil, fmt.Errorf("%w: status %d: %s", ErrRequestFailed, response.StatusCode, truncate(string(raw), 500))
	}

	var payload chatResponse
	if err := json.NewDecoder(limited).Decode(&payload); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBadResponse, err)
	}
	if payload.Error != nil && payload.Error.Message != "" {
		return nil, fmt.Errorf("%w: %s", ErrRequestFailed, payload.Error.Message)
	}
	if len(payload.Choices) == 0 {
		return nil, fmt.Errorf("%w: no choices returned", ErrBadResponse)
	}

	var result translationPayload
	if err := json.Unmarshal([]byte(payload.Choices[0].Message.Content), &result); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBadResponse, err)
	}

	out := make(map[string]map[string]string, len(result.Translations))
	for _, entry := range result.Translations {
		out[entry.Key] = map[string]string{
			targetLocaleZhTW: entry.ZhTW,
			targetLocaleEnUS: entry.EnUS,
		}
	}
	return out, nil
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
