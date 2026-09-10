package view

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// safeLocalPathSegment validates a single filesystem path component.
func safeLocalPathSegment(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("path segment is required")
	}
	if strings.Contains(name, "/") || strings.Contains(name, "\\") {
		return "", fmt.Errorf("invalid path segment %q", name)
	}
	if name == "." || name == ".." {
		return "", fmt.Errorf("invalid path segment %q", name)
	}
	return name, nil
}

func ensurePathWithinRoot(root, full string) error {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("view: resolve export root: %w", err)
	}
	absFull, err := filepath.Abs(full)
	if err != nil {
		return fmt.Errorf("view: resolve export path: %w", err)
	}
	rel, err := filepath.Rel(absRoot, absFull)
	if err != nil {
		return err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return fmt.Errorf("view: path escapes export root")
	}
	return nil
}
