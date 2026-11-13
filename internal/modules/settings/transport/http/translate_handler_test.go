package settingshttp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	settingsintegration "github.com/dujiao-next/internal/modules/settings/schema/integration"
)

type translationAdminStub struct {
	generated map[string]SeoEntry
	err       error
	gotSource SeoSource
	gotLocals []string
	calls     int
}

func (s *translationAdminStub) GetTranslationSetting() (settingsintegration.TranslationSetting, error) {
	return settingsintegration.TranslationSetting{}, nil
}

func (s *translationAdminStub) PatchTranslationSetting(patch settingsintegration.TranslationSetting) (settingsintegration.TranslationSetting, error) {
	return settingsintegration.TranslationSetting{}, nil
}

func (s *translationAdminStub) Translate(ctx context.Context, fields map[string]string) (map[string]map[string]string, error) {
	return nil, nil
}

func (s *translationAdminStub) GenerateSeo(ctx context.Context, source SeoSource, locales []string) (map[string]SeoEntry, error) {
	s.calls++
	s.gotSource = source
	s.gotLocals = locales
	if s.err != nil {
		return nil, s.err
	}
	return s.generated, nil
}

func newSeoTestContext(body string) (*gin.Context, *httptest.ResponseRecorder) {
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/settings/translation/generate-seo", strings.NewReader(body))
	context.Request.Header.Set("Content-Type", "application/json")
	return context, recorder
}

func TestGenerateSeoHandlerReturnsEntries(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stub := &translationAdminStub{
		generated: map[string]SeoEntry{
			"zh-CN": {Locale: "zh-CN", Title: "标题", Keywords: "k", Description: "描述"},
		},
	}
	handler := NewTranslationHandler(stub)
	context, recorder := newSeoTestContext(`{"locales":["zh-CN"],"source":{"title":"原商品"}}`)

	handler.GenerateSeo(context)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if stub.calls != 1 {
		t.Fatalf("generator calls = %d, want 1", stub.calls)
	}
	if stub.gotSource.Title != "原商品" {
		t.Fatalf("source title = %q", stub.gotSource.Title)
	}
	if len(stub.gotLocals) != 1 || stub.gotLocals[0] != "zh-CN" {
		t.Fatalf("locales = %v", stub.gotLocals)
	}
	if !strings.Contains(recorder.Body.String(), "标题") {
		t.Fatalf("response body missing generated title: %s", recorder.Body.String())
	}
}

func TestGenerateSeoHandlerRejectsEmptyTitle(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stub := &translationAdminStub{}
	handler := NewTranslationHandler(stub)
	context, recorder := newSeoTestContext(`{"source":{"title":"   "}}`)

	handler.GenerateSeo(context)

	// 业务错误统一以 HTTP 200 + body 内 status_code 返回。
	if !strings.Contains(recorder.Body.String(), `"status_code":400`) {
		t.Fatalf("expected 400 business error, body = %s", recorder.Body.String())
	}
	if stub.calls != 0 {
		t.Fatalf("generator must not be called, got %d calls", stub.calls)
	}
}

func TestGenerateSeoHandlerMapsNotConfigured(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stub := &translationAdminStub{err: ErrTranslateNotConfigured}
	handler := NewTranslationHandler(stub)
	context, recorder := newSeoTestContext(`{"source":{"title":"商品"}}`)

	handler.GenerateSeo(context)

	if !strings.Contains(recorder.Body.String(), `"status_code":400`) {
		t.Fatalf("expected 400 for unconfigured AI, body = %s", recorder.Body.String())
	}
}

func TestGenerateSeoHandlerMapsUpstreamFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stub := &translationAdminStub{err: errors.Join(ErrTranslateFailed, errors.New("boom"))}
	handler := NewTranslationHandler(stub)
	context, recorder := newSeoTestContext(`{"source":{"title":"商品"}}`)

	handler.GenerateSeo(context)

	if !strings.Contains(recorder.Body.String(), `"status_code":500`) {
		t.Fatalf("expected 500 internal error in body, got %s", recorder.Body.String())
	}
}
