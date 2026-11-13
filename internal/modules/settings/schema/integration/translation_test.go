package settingsintegration

import "testing"

func TestNormalizeTranslationSettingTimeoutBounds(t *testing.T) {
	tests := []struct {
		name    string
		input   int
		want    int
	}{
		{name: "zero falls back to default", input: 0, want: DefaultTranslationTimeoutSeconds},
		{name: "negative falls back to default", input: -5, want: DefaultTranslationTimeoutSeconds},
		{name: "below minimum is clamped", input: 1, want: MinTranslationTimeoutSeconds},
		{name: "above maximum is clamped", input: 9999, want: MaxTranslationTimeoutSeconds},
		{name: "valid value is kept", input: 60, want: 60},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := NormalizeTranslationSetting(TranslationSetting{TimeoutSeconds: tc.input})
			if got.TimeoutSeconds != tc.want {
				t.Fatalf("timeout = %d, want %d", got.TimeoutSeconds, tc.want)
			}
		})
	}
}

func TestDecodeTranslationSettingReadsTimeout(t *testing.T) {
	fallback := DefaultTranslationSetting()
	decoded := DecodeTranslationSetting(map[string]interface{}{
		"enabled":         true,
		"api_key":         "sk-test",
		"timeout_seconds": float64(30),
	}, fallback)

	if decoded.TimeoutSeconds != 30 {
		t.Fatalf("timeout = %d, want 30", decoded.TimeoutSeconds)
	}
	if !decoded.Enabled || decoded.APIKey != "sk-test" {
		t.Fatalf("unexpected decode result: %#v", decoded)
	}
}

func TestDecodeTranslationSettingKeepsFallbackTimeoutWhenMissing(t *testing.T) {
	fallback := DefaultTranslationSetting()
	decoded := DecodeTranslationSetting(map[string]interface{}{"enabled": true}, fallback)

	if decoded.TimeoutSeconds != DefaultTranslationTimeoutSeconds {
		t.Fatalf("timeout = %d, want default %d", decoded.TimeoutSeconds, DefaultTranslationTimeoutSeconds)
	}
}

func TestEncodeTranslationSettingRoundTripsTimeout(t *testing.T) {
	encoded := EncodeTranslationSetting(TranslationSetting{
		Enabled:        true,
		APIKey:         "sk-test",
		TimeoutSeconds: 75,
	})
	if got := encoded["timeout_seconds"]; got != 75 {
		t.Fatalf("encoded timeout = %v, want 75", got)
	}

	decoded := DecodeTranslationSetting(encoded, DefaultTranslationSetting())
	if decoded.TimeoutSeconds != 75 {
		t.Fatalf("round-tripped timeout = %d, want 75", decoded.TimeoutSeconds)
	}
}

func TestApplyTranslationSettingPatchKeepsAPIKeyAndUpdatesTimeout(t *testing.T) {
	current := TranslationSetting{Enabled: true, APIKey: "sk-existing", TimeoutSeconds: 45}

	next, err := ApplyTranslationSettingPatch(current, TranslationSetting{
		Enabled:        true,
		APIKey:         "",
		TimeoutSeconds: 20,
	})
	if err != nil {
		t.Fatalf("patch failed: %v", err)
	}
	if next.APIKey != "sk-existing" {
		t.Fatalf("api key = %q, want preserved value", next.APIKey)
	}
	if next.TimeoutSeconds != 20 {
		t.Fatalf("timeout = %d, want 20", next.TimeoutSeconds)
	}
}

func TestValidateTranslationSettingRejectsTimeoutOutOfRange(t *testing.T) {
	// 走 ApplyTranslationSettingPatch 是因为 Normalize 会先夹紧到合法区间，
	// 这里直接校验未归一化的输入。
	if err := ValidateTranslationSetting(TranslationSetting{
		Enabled:        true,
		APIKey:         "sk-test",
		Model:          "gpt-test",
		TimeoutSeconds: 1,
	}); err == nil {
		t.Fatal("expected validation error for timeout below minimum")
	}
}

func TestMaskedTranslationSettingHidesAPIKeyButExposesTimeout(t *testing.T) {
	masked := MaskTranslationSettingForAdmin(TranslationSetting{
		Enabled:        true,
		APIKey:         "sk-secret",
		TimeoutSeconds: 45,
	})
	if masked["api_key"] != "" {
		t.Fatalf("masked api_key = %v, want empty", masked["api_key"])
	}
	if masked["has_api_key"] != true {
		t.Fatalf("has_api_key = %v, want true", masked["has_api_key"])
	}
	if masked["timeout_seconds"] != 45 {
		t.Fatalf("timeout = %v, want 45", masked["timeout_seconds"])
	}
}
