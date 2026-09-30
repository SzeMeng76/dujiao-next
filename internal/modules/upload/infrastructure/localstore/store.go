package localstore

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/dujiao-next/internal/modules/upload/contract"
)

// Store 将上传文件写入本地 uploads 目录。
type Store struct {
	root string
}

var _ contract.Store = (*Store)(nil)

// New 创建本地文件存储适配器。
func New(root string) *Store {
	if root == "" {
		panic("upload local store: root is empty")
	}
	return &Store{root: root}
}

// Save 保存文件并返回公开访问 URL。
func (s *Store) Save(input contract.StoreInput) (string, error) {
	directory := filepath.Join(s.root, input.Scene, input.Year, input.Month)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(directory, input.Filename)
	destination, err := os.Create(path)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(destination, input.Source); err != nil {
		_ = destination.Close()
		return "", err
	}
	if err := destination.Close(); err != nil {
		return "", err
	}
	return fmt.Sprintf("/uploads/%s/%s/%s/%s", input.Scene, input.Year, input.Month, input.Filename), nil
}

// Delete 删除一个先前由 Save 返回的公开 URL 对应的文件。
// 只接受形如 /uploads/<scene>/<year>/<month>/<filename> 的相对路径，
// 并校验解析后的绝对路径确实位于 root 目录之下，防止路径穿越删到 root 外的文件。
func (s *Store) Delete(publicURL string) error {
	const prefix = "/uploads/"
	if !strings.HasPrefix(publicURL, prefix) {
		return nil
	}
	relative := strings.TrimPrefix(publicURL, prefix)
	if relative == "" || strings.Contains(relative, "..") {
		return nil
	}

	root, err := filepath.Abs(s.root)
	if err != nil {
		return err
	}
	path, err := filepath.Abs(filepath.Join(root, relative))
	if err != nil {
		return err
	}
	if path != root && !strings.HasPrefix(path, root+string(filepath.Separator)) {
		return nil
	}

	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
