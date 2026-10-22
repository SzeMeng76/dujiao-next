package seoguidehttp

import "testing"

func TestResolveLocaleFromHeader(t *testing.T) {
	cases := map[string]string{
		"":                                  "zh-CN",
		"zh-CN,zh;q=0.9,en;q=0.8":           "zh-CN",
		"zh-TW,zh;q=0.9,en;q=0.8":           "zh-TW",
		"zh-HK,zh;q=0.9":                    "zh-TW",
		"zh-Hant-TW":                        "zh-TW",
		"en-US,en;q=0.9,zh-CN;q=0.8":        "en-US",
		"en":                                "en-US",
		"fr-FR,fr;q=0.9,en;q=0.8":           "en-US",
		"fr-FR,fr;q=0.9":                    "zh-CN",
	}
	for header, want := range cases {
		if got := resolveLocaleFromHeader(header); got != want {
			t.Errorf("resolveLocaleFromHeader(%q) = %q, want %q", header, got, want)
		}
	}
}
