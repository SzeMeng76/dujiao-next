package upstream

// 上游图片下载：各协议适配器共用。只允许下载与上游站点同主机的图片（含重定向），
// 防止上游返回内网/元数据地址，把内部响应落到公开的 /uploads。

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

// maxUpstreamImageBytes 单张上游图片的下载上限，防止异常响应写满磁盘。
const maxUpstreamImageBytes = 10 << 20

var allowedUpstreamImageExts = map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true}

// downloadUpstreamImage 下载图片并返回本地相对路径（/uploads/upstream/<uuid>.<ext>）。
// 非上游主机、非图片响应或超过大小上限时返回错误，调用方保留原始 URL。
func downloadUpstreamImage(ctx context.Context, client *http.Client, baseURL, uploadsDir, imageURL string) (string, error) {
	fullURL := imageURL
	if strings.HasPrefix(imageURL, "/") {
		fullURL = baseURL + imageURL
	}
	base, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("parse base url: %w", err)
	}
	target, err := url.Parse(fullURL)
	if err != nil || !strings.EqualFold(target.Host, base.Host) {
		return "", fmt.Errorf("download image: host not allowed: %s", fullURL)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return "", fmt.Errorf("create download request: %w", err)
	}

	downloadClient := *client
	downloadClient.CheckRedirect = func(next *http.Request, _ []*http.Request) error {
		if !strings.EqualFold(next.URL.Host, base.Host) {
			return fmt.Errorf("download image: redirect host not allowed: %s", next.URL.Host)
		}
		return nil
	}
	resp, err := downloadClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("download image: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download image: status %d", resp.StatusCode)
	}

	ext := strings.ToLower(path.Ext(target.Path))
	if !allowedUpstreamImageExts[ext] {
		ext = ".jpg"
	}

	filename := uuid.New().String() + ext
	dir := filepath.Join(uploadsDir, "upstream")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("create uploads dir: %w", err)
	}

	filePath := filepath.Join(dir, filename)
	f, err := os.Create(filePath)
	if err != nil {
		return "", fmt.Errorf("create file: %w", err)
	}
	written, err := io.Copy(f, io.LimitReader(resp.Body, maxUpstreamImageBytes+1))
	closeErr := f.Close()
	if err == nil && written > maxUpstreamImageBytes {
		err = fmt.Errorf("image exceeds %d bytes", maxUpstreamImageBytes)
	}
	if err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(filePath)
		return "", fmt.Errorf("write file: %w", err)
	}

	return "/uploads/upstream/" + filename, nil
}
