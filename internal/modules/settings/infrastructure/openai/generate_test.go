package openaitranslate

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	settingsintegration "github.com/dujiao-next/internal/modules/settings/schema/integration"
)

// chatCompletionHandler 返回一个模拟的 OpenAI 兼容响应体。
func chatCompletionHandler(content string, finishReason string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sk-test" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message":       map[string]string{"content": content},
					"finish_reason": finishReason,
				},
			},
		})
	}
}

func testSetting(baseURL string) settingsintegration.TranslationSetting {
	return settingsintegration.TranslationSetting{
		Enabled:        true,
		APIKey:         "sk-test",
		BaseURL:        baseURL,
		Model:          "test-model",
		TimeoutSeconds: 10,
	}
}

func TestGenerateSeoParsesEntries(t *testing.T) {
	content := `{"languages":[{"locale":"zh-CN","title":"测试标题","keywords":"a,b","description":"这是一段描述"},{"locale":"en-US","title":"Test Title","keywords":"c,d","description":"A description"}]}`
	server := httptest.NewServer(chatCompletionHandler(content, "stop"))
	defer server.Close()

	client := New()
	result, err := client.GenerateSeo(context.Background(), testSetting(server.URL), SeoSource{
		Title: "原商品标题",
	}, []string{"zh-CN", "en-US"})
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("got %d entries, want 2", len(result))
	}
	if result["zh-CN"].Title != "测试标题" {
		t.Fatalf("zh-CN title = %q", result["zh-CN"].Title)
	}
	if result["en-US"].Description != "A description" {
		t.Fatalf("en-US description = %q", result["en-US"].Description)
	}
}

func TestGenerateSeoRejectsTruncatedOutput(t *testing.T) {
	server := httptest.NewServer(chatCompletionHandler(`{"languages":[]}`, "length"))
	defer server.Close()

	client := New()
	if _, err := client.GenerateSeo(context.Background(), testSetting(server.URL), SeoSource{Title: "x"}, nil); err == nil {
		t.Fatal("expected error on truncated output")
	}
}

func TestGenerateSeoDropsEmptyEntries(t *testing.T) {
	// description 为空的条目应被丢弃，不能写入空字段。
	content := `{"languages":[{"locale":"zh-CN","title":"","keywords":"","description":""},{"locale":"en-US","title":"Good","keywords":"k","description":"Fine"}]}`
	server := httptest.NewServer(chatCompletionHandler(content, "stop"))
	defer server.Close()

	client := New()
	result, err := client.GenerateSeo(context.Background(), testSetting(server.URL), SeoSource{Title: "x"}, nil)
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	if _, exists := result["zh-CN"]; exists {
		t.Fatal("empty entry must be dropped")
	}
	if result["en-US"].Title != "Good" {
		t.Fatalf("en-US title = %q", result["en-US"].Title)
	}
}

func TestGenerateSeoErrorsWhenNoUsableEntries(t *testing.T) {
	content := `{"languages":[{"locale":"zh-CN","title":"","keywords":"","description":""}]}`
	server := httptest.NewServer(chatCompletionHandler(content, "stop"))
	defer server.Close()

	client := New()
	if _, err := client.GenerateSeo(context.Background(), testSetting(server.URL), SeoSource{Title: "x"}, nil); err == nil {
		t.Fatal("expected error when model returns no usable entries")
	}
}

func TestGenerateSeoNotConfiguredWithoutAPIKey(t *testing.T) {
	client := New()
	setting := settingsintegration.TranslationSetting{Enabled: true, Model: "m"}
	if _, err := client.GenerateSeo(context.Background(), setting, SeoSource{Title: "x"}, nil); err != ErrNotConfigured {
		t.Fatalf("err = %v, want ErrNotConfigured", err)
	}
}

func TestGenerateSeoTruncatesOverlongFields(t *testing.T) {
	longTitle := strings.Repeat("字", seoTitleMaxRunes+50)
	longDesc := strings.Repeat("描", seoDescriptionMax+50)
	content := `{"languages":[{"locale":"zh-CN","title":"` + longTitle + `","keywords":"k","description":"` + longDesc + `"}]}`
	server := httptest.NewServer(chatCompletionHandler(content, "stop"))
	defer server.Close()

	client := New()
	result, err := client.GenerateSeo(context.Background(), testSetting(server.URL), SeoSource{Title: "x"}, nil)
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	entry := result["zh-CN"]
	if got := len([]rune(entry.Title)); got != seoTitleMaxRunes {
		t.Fatalf("title runes = %d, want %d", got, seoTitleMaxRunes)
	}
	if got := len([]rune(entry.Description)); got != seoDescriptionMax {
		t.Fatalf("description runes = %d, want %d", got, seoDescriptionMax)
	}
}

func TestNormalizeSeoLocales(t *testing.T) {
	tests := []struct {
		name  string
		input []string
		want  []string
	}{
		{name: "empty returns all", input: nil, want: []string{"zh-CN", "zh-TW", "en-US"}},
		{name: "keeps stable order", input: []string{"en-US", "zh-CN"}, want: []string{"zh-CN", "en-US"}},
		{name: "dedupes", input: []string{"en-US", "en-US"}, want: []string{"en-US"}},
		{name: "drops unknown", input: []string{"ja-JP"}, want: []string{"zh-CN", "zh-TW", "en-US"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeSeoLocales(tc.input)
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("got %v, want %v", got, tc.want)
				}
			}
		})
	}
}

func TestTimeoutForFallsBackWhenUnset(t *testing.T) {
	if got := timeoutFor(settingsintegration.TranslationSetting{}); got != requestTimeout {
		t.Fatalf("timeout = %v, want fallback %v", got, requestTimeout)
	}
	if got := timeoutFor(settingsintegration.TranslationSetting{TimeoutSeconds: 20}); got.Seconds() != 20 {
		t.Fatalf("timeout = %v, want 20s", got)
	}
}
