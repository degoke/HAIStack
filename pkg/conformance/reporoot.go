package conformance

import (
	"fmt"
	"os"
	"path/filepath"
)

// RepoRoot walks up from the working directory until go.mod is found.
func RepoRoot() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := wd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("could not find repository root from %s", wd)
		}
		dir = parent
	}
}
