package upstream

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestACGSignStableOrder(t *testing.T) {
	// 键顺序不影响签名结果。
	first := acgSign(map[string]interface{}{
		"app_id":      "1001",
		"shared_code": "9527",
		"num":         "1",
	}, "secret-key")
	second := acgSign(map[string]interface{}{
		"num":         "1",
		"app_id":      "1001",
		"shared_code": "9527",
	}, "secret-key")
	if first != second {
		t.Fatalf("signature should not depend on map order: %s != %s", first, second)
	}
	if len(first) != 32 {
		t.Fatalf("expected md5 hex length 32, got %d (%s)", len(first), first)
	}
}

func TestACGSignSkipsEmptyAndSign(t *testing.T) {
	base := acgSign(map[string]interface{}{"app_id": "1001", "code": "9527"}, "secret")
	withEmpty := acgSign(map[string]interface{}{
		"app_id": "1001", "code": "9527", "empty": "", "sign": "ignored",
	}, "secret")
	if base != withEmpty {
		t.Fatalf("empty values and sign itself must not affect the signature: %s != %s", base, withEmpty)
	}
}

func TestACGSignBooleanEncoding(t *testing.T) {
	truthy := acgSign(map[string]interface{}{"app_id": "1", "flag": true}, "secret")
	asString := acgSign(map[string]interface{}{"app_id": "1", "flag": "1"}, "secret")
	if truthy != asString {
		t.Fatalf("boolean true must serialize as \"1\": %s != %s", truthy, asString)
	}

	falsy := acgSign(map[string]interface{}{"app_id": "1", "flag": false}, "secret")
	asZero := acgSign(map[string]interface{}{"app_id": "1", "flag": "0"}, "secret")
	if falsy != asZero {
		t.Fatalf("boolean false must serialize as \"0\": %s != %s", falsy, asZero)
	}
}

func TestACGFormPairsExpandNested(t *testing.T) {
	pairs := acgFormPairs(map[string]interface{}{
		"filter": map[string]interface{}{"b": "2", "a": "1"},
		"list":   []interface{}{"x", "y"},
	})
	want := [][2]string{
		{"filter[a]", "1"},
		{"filter[b]", "2"},
		{"list[0]", "x"},
		{"list[1]", "y"},
	}
	if len(pairs) != len(want) {
		t.Fatalf("expected %d pairs, got %d: %v", len(want), len(pairs), pairs)
	}
	for i := range want {
		if pairs[i] != want[i] {
			t.Fatalf("pair %d: want %v, got %v", i, want[i], pairs[i])
		}
	}
}

func TestACGIDForCodeNumericAndHashed(t *testing.T) {
	if got := acgIDForCode("9527"); got != 9527 {
		t.Fatalf("numeric code should map to its value, got %d", got)
	}
	// 非数字 code 必须稳定且非 0。
	first := acgIDForCode("abc-123")
	second := acgIDForCode("abc-123")
	if first != second || first == 0 {
		t.Fatalf("hashed code must be stable and non-zero: %d vs %d", first, second)
	}
	if acgIDForCode("other-code") == first {
		t.Fatalf("different codes should not collide in this sample")
	}
}

func TestTruncateTextKeepsValidUTF8(t *testing.T) {
	// 中文错误信息被截断后必须仍是合法 UTF-8，否则 PostgreSQL 拒绝写入（SQLSTATE 22021）。
	text := strings.Repeat("商品不存在", 20)
	for limit := 1; limit <= 40; limit++ {
		got := truncateText(text, limit)
		if !utf8.ValidString(got) {
			t.Fatalf("limit %d produced invalid UTF-8: %q", limit, got)
		}
	}
	// 未超长时原样返回。
	if got := truncateText("库存不足", 100); got != "库存不足" {
		t.Fatalf("short input should be unchanged, got %q", got)
	}
}
