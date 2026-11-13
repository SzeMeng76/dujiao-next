package binancepay

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// newCertServer 构造一个模拟 Query Certificate API 的服务器。
// certs 为 certSerial -> certPublic 的映射，按 map 顺序返回（顺序不保证）。
func newCertServer(t *testing.T, certs [][2]string) (*httptest.Server, *int32) {
	t.Helper()
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		data := make([]map[string]any, 0, len(certs))
		for _, c := range certs {
			data = append(data, map[string]any{
				"certSerial": c[0],
				"certPublic": c[1],
			})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "SUCCESS",
			"data":   data,
		})
	}))
	t.Cleanup(func() {
		server.Close()
		resetCertPublicKeyCache()
	})
	return server, &calls
}

func newCertTestConfig(apiBaseURL string) *Config {
	cfg := &Config{
		APIKey:     "test-cert-sn",
		SecretKey:  "test-secret",
		ReturnURL:  "https://example.com/return",
		APIBaseURL: apiBaseURL,
	}
	cfg.Normalize()
	return cfg
}

// TestFetchCertPublicKey_MatchesByCertSerial 是本次加固的核心回归：
// 必须按 webhook 头里的 Certificate-SN 取对应公钥，而不是盲取列表第一个。
// 证书轮换期新旧并存时，取错公钥会让真实回调验签失败。
func TestFetchCertPublicKey_MatchesByCertSerial(t *testing.T) {
	resetCertPublicKeyCache()

	// 故意把要匹配的证书放在第二个，确保实现不能靠"取第一个"碰巧通过。
	server, _ := newCertServer(t, [][2]string{
		{"old-cert-sn", "-----BEGIN PUBLIC KEY-----\nOLD\n-----END PUBLIC KEY-----"},
		{"new-cert-sn", "-----BEGIN PUBLIC KEY-----\nNEW\n-----END PUBLIC KEY-----"},
	})
	cfg := newCertTestConfig(server.URL)

	got, err := fetchCertPublicKey(context.Background(), cfg, "new-cert-sn")
	if err != nil {
		t.Fatalf("fetchCertPublicKey() error = %v", err)
	}
	if got != "-----BEGIN PUBLIC KEY-----\nNEW\n-----END PUBLIC KEY-----" {
		t.Fatalf("fetchCertPublicKey() = %q, want the cert matching certSerial", got)
	}
}

// TestFetchCertPublicKey_FallsBackWhenSerialUnknown 确认 SN 匹配不到时回落首个公钥，
// 保证证书轮换时序差异不会把正常回调硬失败。
func TestFetchCertPublicKey_FallsBackWhenSerialUnknown(t *testing.T) {
	resetCertPublicKeyCache()

	server, _ := newCertServer(t, [][2]string{
		{"known-cert-sn", "key-known"},
	})
	cfg := newCertTestConfig(server.URL)

	got, err := fetchCertPublicKey(context.Background(), cfg, "unknown-cert-sn")
	if err != nil {
		t.Fatalf("fetchCertPublicKey() error = %v", err)
	}
	if got != "key-known" {
		t.Fatalf("fetchCertPublicKey() = %q, want fallback key", got)
	}
}

// TestFetchCertPublicKey_CachesWithinTTL 确认同一 SN 在 TTL 内不会重复请求远程接口。
// 回调被刷时反复拉公钥会触发限流，进而拖垮真实回调。
func TestFetchCertPublicKey_CachesWithinTTL(t *testing.T) {
	resetCertPublicKeyCache()

	server, calls := newCertServer(t, [][2]string{{"cert-sn", "key-a"}})
	cfg := newCertTestConfig(server.URL)

	for i := 0; i < 5; i++ {
		if _, err := fetchCertPublicKey(context.Background(), cfg, "cert-sn"); err != nil {
			t.Fatalf("fetchCertPublicKey() error = %v", err)
		}
	}

	if n := atomic.LoadInt32(calls); n != 1 {
		t.Fatalf("remote certificate API called %d times, want 1 (cached)", n)
	}
}

// TestFetchCertPublicKey_RefetchesAfterExpiry 确认缓存过期后会重新拉取。
func TestFetchCertPublicKey_RefetchesAfterExpiry(t *testing.T) {
	resetCertPublicKeyCache()

	server, calls := newCertServer(t, [][2]string{{"cert-sn", "key-a"}})
	cfg := newCertTestConfig(server.URL)

	if _, err := fetchCertPublicKey(context.Background(), cfg, "cert-sn"); err != nil {
		t.Fatalf("first fetch error = %v", err)
	}

	// 手动把缓存条目标记为已过期。
	certPublicKeyCache.Lock()
	for k, v := range certPublicKeyCache.entries {
		v.expiresAt = v.expiresAt.Add(-2 * certPublicKeyCacheTTL)
		certPublicKeyCache.entries[k] = v
	}
	certPublicKeyCache.Unlock()

	if _, err := fetchCertPublicKey(context.Background(), cfg, "cert-sn"); err != nil {
		t.Fatalf("second fetch error = %v", err)
	}

	if n := atomic.LoadInt32(calls); n != 2 {
		t.Fatalf("remote certificate API called %d times, want 2 after expiry", n)
	}
}

// TestFetchCertPublicKey_CacheIsolatedByBaseURL 确认不同 APIBaseURL 的公钥不串用。
func TestFetchCertPublicKey_CacheIsolatedByBaseURL(t *testing.T) {
	resetCertPublicKeyCache()

	serverA, _ := newCertServer(t, [][2]string{{"cert-sn", "key-a"}})
	serverB, _ := newCertServer(t, [][2]string{{"cert-sn", "key-b"}})

	if _, err := fetchCertPublicKey(context.Background(), newCertTestConfig(serverA.URL), "cert-sn"); err != nil {
		t.Fatalf("fetch from A error = %v", err)
	}
	got, err := fetchCertPublicKey(context.Background(), newCertTestConfig(serverB.URL), "cert-sn")
	if err != nil {
		t.Fatalf("fetch from B error = %v", err)
	}
	if got != "key-b" {
		t.Fatalf("fetchCertPublicKey() = %q, want key-b for a different base URL", got)
	}
}
