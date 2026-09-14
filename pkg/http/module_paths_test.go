package http

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateModulePath(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "mods", "core")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatal(err)
	}
	allowed, err := validateModulePath(child, []string{root})
	if err != nil || allowed != child {
		t.Fatalf("validateModulePath=%q err=%v", allowed, err)
	}
	if _, err := validateModulePath(filepath.Join(root, "..", "etc", "passwd"), []string{root}); err == nil {
		t.Fatal("expected rejection for path outside allowlist")
	}
	if _, err := validateModulePath(child, nil); err == nil {
		t.Fatal("expected rejection when allowlist is empty")
	}
}
