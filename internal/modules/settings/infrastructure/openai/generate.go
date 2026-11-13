package openaitranslate

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"

	settingsintegration "github.com/dujiao-next/internal/modules/settings/schema/integration"
)

// SeoSource 是生成 SEO 文案时喂给模型的事实来源。
// 只放已经确认的商品信息，prompt 里会明确禁止模型补充不存在的规格。
type SeoSource struct {
	SiteName    string
	Title       string
	Category    string
	Price       string
	Description string
	Content     string
}

// SeoEntry 是某一语言的生成结果。
type SeoEntry struct {
	Locale      string `json:"locale"`
	Title       string `json:"title"`
	Keywords    string `json:"keywords"`
	Description string `json:"description"`
}

type seoPayload struct {
	Languages []SeoEntry `json:"languages"`
}

// 生成结果的长度上限，超出的部分按 rune 截断。
const (
	seoTitleMaxRunes    = 70
	seoDescriptionMax   = 200
	seoKeywordsMaxRunes = 300
	seoContentMaxRunes  = 1200
)

// seoTargetLocales 是需要生成的目标语言，顺序固定便于结果对齐。
var seoTargetLocales = []string{"zh-CN", "zh-TW", "en-US"}

var seoJSONSchema = map[string]interface{}{
	"type": "object",
	"properties": map[string]interface{}{
		"languages": map[string]interface{}{
			"type": "array",
			"items": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"locale":      map[string]interface{}{"type": "string"},
					"title":       map[string]interface{}{"type": "string"},
					"keywords":    map[string]interface{}{"type": "string"},
					"description": map[string]interface{}{"type": "string"},
				},
				"required":             []string{"locale", "title", "keywords", "description"},
				"additionalProperties": false,
			},
		},
	},
	"required":             []string{"languages"},
	"additionalProperties": false,
}

const seoSystemPrompt = `You are an e-commerce SEO specialist writing product metadata for an online store that sells digital goods and subscription top-ups.

You will receive product facts as JSON plus the list of target locales. For every locale, write:
- "title": the SEO page title (aim for 50-60 characters, core keyword first, no site name)
- "keywords": 5-8 comma-separated search terms a buyer would realistically type in that language
- "description": the meta description (aim for 120-160 characters, include the concrete value proposition and a call to action)

Hard rules:
1. Use ONLY facts present in the input. Never invent specifications, durations, quantities, regions, brands, or guarantees.
2. If the input lacks a detail, write around it instead of guessing.
3. Localize naturally: do not translate literally. Use the wording buyers in that locale actually search for.
4. Do not repeat the same sentence structure across locales; each locale must read as native copy.
5. Plain text only — no markdown, no quotes wrapping the whole string, no HTML.
6. Return exactly one entry per requested locale, echoing the locale code verbatim.`

// GenerateSeo 根据商品事实生成各语言的 SEO 标题 / 关键词 / 描述。
// locales 为空时生成全部支持语言。
func (c Client) GenerateSeo(ctx context.Context, cfg settingsintegration.TranslationSetting, source SeoSource, locales []string) (map[string]SeoEntry, error) {
	apiKey := strings.TrimSpace(cfg.APIKey)
	if apiKey == "" {
		return nil, ErrNotConfigured
	}

	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		return nil, ErrNotConfigured
	}

	targets := normalizeSeoLocales(locales)

	payload := map[string]interface{}{
		"locales": targets,
		"product": map[string]string{
			"site_name":   strings.TrimSpace(source.SiteName),
			"title":       strings.TrimSpace(source.Title),
			"category":    strings.TrimSpace(source.Category),
			"price":       strings.TrimSpace(source.Price),
			"description": truncateRunes(strings.TrimSpace(source.Description), seoContentMaxRunes),
			"content":     truncateRunes(strings.TrimSpace(source.Content), seoContentMaxRunes),
		},
	}
	sourceJSON, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrRequestFailed, err)
	}

	reqBody := chatRequest{
		Model: model,
		Messages: []chatMessage{
			{Role: "system", Content: seoSystemPrompt},
			{Role: "user", Content: string(sourceJSON)},
		},
		// SEO 文案需要一点措辞多样性，但不要发散到偏离事实。
		Temperature: 0.4,
		ResponseFormat: jsonSchemaFormat{
			Type: "json_schema",
			JSONSchema: jsonSchemaFormatDetail{
				Name:   "seo_metadata",
				Strict: true,
				Schema: seoJSONSchema,
			},
		},
		MaxCompletionTokens: seoTokenBudget(len(targets)),
	}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrRequestFailed, err)
	}

	requestCtx, cancel := context.WithTimeout(ctx, timeoutFor(cfg))
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

	var chatPayload chatResponse
	if err := json.NewDecoder(limited).Decode(&chatPayload); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBadResponse, err)
	}
	if chatPayload.Error != nil && chatPayload.Error.Message != "" {
		return nil, fmt.Errorf("%w: %s", ErrRequestFailed, chatPayload.Error.Message)
	}
	if len(chatPayload.Choices) == 0 {
		return nil, fmt.Errorf("%w: no choices returned", ErrBadResponse)
	}
	choice := chatPayload.Choices[0]
	if choice.FinishReason == "length" {
		return nil, fmt.Errorf("%w: output truncated (finish_reason=length)", ErrBadResponse)
	}

	var result seoPayload
	if err := json.Unmarshal([]byte(choice.Message.Content), &result); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBadResponse, err)
	}
	if len(result.Languages) == 0 {
		return nil, fmt.Errorf("%w: model returned no entries", ErrBadResponse)
	}

	out := make(map[string]SeoEntry, len(result.Languages))
	for _, entry := range result.Languages {
		locale := strings.TrimSpace(entry.Locale)
		if locale == "" {
			continue
		}
		title := strings.TrimSpace(entry.Title)
		description := strings.TrimSpace(entry.Description)
		keywords := strings.TrimSpace(entry.Keywords)
		// 空结果不入库：宁可报错让用户重试，也不要写入空字段。
		if title == "" || description == "" {
			continue
		}
		out[locale] = SeoEntry{
			Locale:      locale,
			Title:       truncateRunes(title, seoTitleMaxRunes),
			Keywords:    truncateRunes(keywords, seoKeywordsMaxRunes),
			Description: truncateRunes(description, seoDescriptionMax),
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: model returned no usable entries", ErrBadResponse)
	}
	return out, nil
}

// normalizeSeoLocales 去重并保留固定顺序，未知语言被丢弃。
func normalizeSeoLocales(locales []string) []string {
	wanted := make(map[string]bool, len(locales))
	for _, locale := range locales {
		wanted[strings.TrimSpace(locale)] = true
	}
	out := make([]string, 0, len(seoTargetLocales))
	for _, locale := range seoTargetLocales {
		if wanted[locale] {
			out = append(out, locale)
		}
	}
	if len(out) == 0 {
		return append([]string(nil), seoTargetLocales...)
	}
	return out
}

func seoTokenBudget(localeCount int) int {
	const (
		minBudget    = 1024
		maxBudget    = 8192
		perLocale    = 600
		baseOverhead = 500
	)
	budget := localeCount*perLocale + baseOverhead
	if budget < minBudget {
		return minBudget
	}
	if budget > maxBudget {
		return maxBudget
	}
	return budget
}

func truncateRunes(s string, max int) string {
	if max <= 0 || utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return string(runes[:max])
}
