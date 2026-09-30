package localstore

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/dujiao-next/internal/modules/upload/contract"
)

func TestStoreSavesFileUnderConfiguredRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "uploads")
	store := New(root)
	url, err := store.Save(contract.StoreInput{
		Source:   bytes.NewBufferString("asset-content"),
		Scene:    "common",
		Year:     "2026",
		Month:    "07",
		Filename: "asset.txt",
	})
	if err != nil {
		t.Fatalf("save failed: %v", err)
	}
	if url != "/uploads/common/2026/07/asset.txt" {
		t.Fatalf("public URL got %q", url)
	}
	data, err := os.ReadFile(filepath.Join(root, "common", "2026", "07", "asset.txt"))
	if err != nil {
		t.Fatalf("read saved file: %v", err)
	}
	if string(data) != "asset-content" {
		t.Fatalf("saved content got %q", data)
	}
}

func TestStoreDeletesFileByPublicURL(t *testing.T) {
	root := filepath.Join(t.TempDir(), "uploads")
	store := New(root)
	url, err := store.Save(contract.StoreInput{
		Source:   bytes.NewBufferString("asset-content"),
		Scene:    "ticket",
		Year:     "2026",
		Month:    "07",
		Filename: "asset.txt",
	})
	if err != nil {
		t.Fatalf("save failed: %v", err)
	}
	if err := store.Delete(url); err != nil {
		t.Fatalf("delete failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "ticket", "2026", "07", "asset.txt")); !os.IsNotExist(err) {
		t.Fatalf("expected file to be removed, stat err = %v", err)
	}
}

func TestStoreDeleteIgnoresMissingFile(t *testing.T) {
	root := filepath.Join(t.TempDir(), "uploads")
	store := New(root)
	if err := store.Delete("/uploads/ticket/2026/07/does-not-exist.txt"); err != nil {
		t.Fatalf("expected nil error for missing file, got %v", err)
	}
}

func TestStoreDeleteRejectsPathTraversal(t *testing.T) {
	root := filepath.Join(t.TempDir(), "uploads")
	store := New(root)
	outside := filepath.Join(filepath.Dir(root), "sensitive.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o644); err != nil {
		t.Fatalf("prepare outside file: %v", err)
	}
	if err := store.Delete("/uploads/../sensitive.txt"); err != nil {
		t.Fatalf("delete should not error, got %v", err)
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("expected outside file to remain untouched, stat err = %v", err)
	}
}

func TestStoreDeleteRejectsNonUploadsPrefix(t *testing.T) {
	root := filepath.Join(t.TempDir(), "uploads")
	store := New(root)
	if err := store.Delete("https://example.com/evil"); err != nil {
		t.Fatalf("delete should not error for foreign URL, got %v", err)
	}
}
