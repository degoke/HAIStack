package http

import (
	"fmt"
	"path/filepath"
	"strings"
)

// validateModulePath resolves path under an allowlisted module root.
func validateModulePath(path string, allowedRoots []string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", invalidRequest("path is required for $install", nil)
	}
	clean := filepath.Clean(path)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", invalidRequest("module path must not contain ..", nil)
	}
	abs, err := filepath.Abs(clean)
	if err != nil {
		return "", invalidRequest("invalid module path", err)
	}
	if len(allowedRoots) == 0 {
		return "", invalidRequest("module install paths are not configured", nil)
	}
	for _, root := range allowedRoots {
		rootAbs, err := filepath.Abs(filepath.Clean(root))
		if err != nil {
			continue
		}
		if abs == rootAbs || strings.HasPrefix(abs, rootAbs+string(filepath.Separator)) {
			return abs, nil
		}
	}
	return "", invalidRequest(fmt.Sprintf("module path %q is not under an allowed root", abs), nil)
}
