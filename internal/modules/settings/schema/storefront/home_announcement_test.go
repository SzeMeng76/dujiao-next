package settingsstorefront

import (
	"testing"
	"time"
)

func TestHomeAnnouncementNormalize(t *testing.T) {
	result := normalizeHomeAnnouncement(map[string]interface{}{
		"enabled":  true,
		"type":     "banana",
		"title":    map[string]interface{}{"zh-CN": "  标题  "},
		"content":  map[string]interface{}{"zh-CN": "<p>正文</p>"},
		"start_at": "not-a-time",
		"end_at":   "2026-06-01T00:00:00Z",
	})
	if result["type"] != "normal" {
		t.Fatalf("expected invalid type to fall back to normal, got %v", result["type"])
	}
	if result["enabled"] != true {
		t.Fatalf("expected enabled true, got %v", result["enabled"])
	}
	title, _ := result["title"].(map[string]interface{})
	if title["zh-CN"] != "标题" {
		t.Fatalf("expected title trimmed, got %q", title["zh-CN"])
	}
	if title["en-US"] != "" {
		t.Fatalf("expected missing locale to be empty string, got %v", title["en-US"])
	}
	if result["start_at"] != "" {
		t.Fatalf("expected invalid start_at cleared, got %v", result["start_at"])
	}
	if result["end_at"] != "2026-06-01T00:00:00Z" {
		t.Fatalf("expected valid end_at kept, got %v", result["end_at"])
	}
}

func TestHomeAnnouncementVersion(t *testing.T) {
	title := map[string]interface{}{"zh-CN": "标题", "zh-TW": "", "en-US": ""}
	contentA := map[string]interface{}{"zh-CN": "<p>A</p>", "zh-TW": "", "en-US": ""}
	contentB := map[string]interface{}{"zh-CN": "<p>B</p>", "zh-TW": "", "en-US": ""}

	v1 := homeAnnouncementVersion("info", title, contentA)
	v2 := homeAnnouncementVersion("info", title, contentA)
	v3 := homeAnnouncementVersion("info", title, contentB)

	if v1 != v2 {
		t.Fatalf("expected stable version for identical content, got %s and %s", v1, v2)
	}
	if v1 == v3 {
		t.Fatalf("expected version to change when content changes")
	}
	if len(v1) != 8 {
		t.Fatalf("expected 8-char version, got %q", v1)
	}
}

func TestHomeAnnouncementSchedule(t *testing.T) {
	now := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)

	if !isHomeAnnouncementInSchedule("", "", now) {
		t.Fatalf("expected empty schedule to be always active")
	}
	if isHomeAnnouncementInSchedule("2026-05-21T00:00:00Z", "", now) {
		t.Fatalf("expected inactive before start time")
	}
	if isHomeAnnouncementInSchedule("", "2026-05-19T00:00:00Z", now) {
		t.Fatalf("expected inactive after end time")
	}
	if !isHomeAnnouncementInSchedule("2026-05-01T00:00:00Z", "2026-06-01T00:00:00Z", now) {
		t.Fatalf("expected active within window")
	}
}

func TestHomeAnnouncementDisplay(t *testing.T) {
	legacy := normalizeHomeAnnouncement(map[string]interface{}{"enabled": true})
	if legacy["display"] != "modal" {
		t.Fatalf("expected missing display to default to modal, got %v", legacy["display"])
	}
	invalid := normalizeHomeAnnouncement(map[string]interface{}{"display": "banana"})
	if invalid["display"] != "modal" {
		t.Fatalf("expected invalid display to fall back to modal, got %v", invalid["display"])
	}

	now := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	active, ok := ActiveHomeAnnouncement(map[string]interface{}{
		"enabled": true,
		"display": "ticker",
		"content": map[string]interface{}{"zh-CN": "<p>正文</p>"},
	}, now)
	if !ok {
		t.Fatalf("expected announcement to be active")
	}
	if active["display"] != "ticker" {
		t.Fatalf("expected ticker display in public payload, got %v", active["display"])
	}
}
