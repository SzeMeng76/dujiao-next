package upstream

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

// ACG（异次元）共享 API 的签名算法。
//
// 规则来自异次元官方共享店铺协议：
//   - 参数按键名字典序排序；
//   - 跳过 sign 与空值；
//   - 嵌套对象展开为 key[child]，数组展开为 key[index]；
//   - 布尔值转 "1" / "0"；
//   - 拼接为 k1=v1&k2=v2 后再追加 &key=<app_key>，取 MD5 小写十六进制。
//
// app_id 参与签名，app_key 本身不发送。
func acgSign(values map[string]interface{}, appKey string) string {
	pairs := acgFormPairs(values)
	parts := make([]string, 0, len(pairs))
	for _, pair := range pairs {
		parts = append(parts, pair[0]+"="+pair[1])
	}
	raw := strings.Join(parts, "&") + "&key=" + appKey
	sum := md5.Sum([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// acgFormPairs 把参数展开成有序的 key/value 对。
func acgFormPairs(values map[string]interface{}) [][2]string {
	keys := make([]string, 0, len(values))
	for key := range values {
		if key == "sign" {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)

	pairs := make([][2]string, 0, len(keys))
	for _, key := range keys {
		pairs = appendAcgPairs(pairs, key, values[key])
	}
	return pairs
}

func appendAcgPairs(pairs [][2]string, key string, value interface{}) [][2]string {
	switch typed := value.(type) {
	case nil:
		return pairs
	case string:
		if typed == "" {
			return pairs
		}
		return append(pairs, [2]string{key, typed})
	case bool:
		if typed {
			return append(pairs, [2]string{key, "1"})
		}
		return append(pairs, [2]string{key, "0"})
	case map[string]interface{}:
		children := make([]string, 0, len(typed))
		for child := range typed {
			children = append(children, child)
		}
		sort.Strings(children)
		for _, child := range children {
			pairs = appendAcgPairs(pairs, fmt.Sprintf("%s[%s]", key, child), typed[child])
		}
		return pairs
	case []interface{}:
		for index, item := range typed {
			pairs = appendAcgPairs(pairs, fmt.Sprintf("%s[%d]", key, index), item)
		}
		return pairs
	default:
		return append(pairs, [2]string{key, fmt.Sprintf("%v", typed)})
	}
}
