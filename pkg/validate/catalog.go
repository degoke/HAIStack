package validate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// MergeProfileCatalogs combines multiple in-memory catalogs. Later entries override
// earlier ones for the same canonical URL.
func MergeProfileCatalogs(catalogs ...MemoryProfileCatalog) MemoryProfileCatalog {
	merged := make(MemoryProfileCatalog)
	for _, catalog := range catalogs {
		for url, sd := range catalog {
			merged[url] = sd
		}
	}
	return merged
}

// LoadProfileCatalogFromDirs loads StructureDefinitions from multiple directories.
func LoadProfileCatalogFromDirs(dirs ...string) (MemoryProfileCatalog, error) {
	merged := make(MemoryProfileCatalog)
	for _, dir := range dirs {
		if strings.TrimSpace(dir) == "" {
			continue
		}
		catalog, err := LoadProfileCatalogFromDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				return nil, fmt.Errorf("profile directory %q does not exist", dir)
			}
			return nil, fmt.Errorf("load profiles from %q: %w", dir, err)
		}
		for url, sd := range catalog {
			merged[url] = sd
		}
	}
	return merged, nil
}

// LoadProfileCatalogFromDirTree walks dir and loads StructureDefinitions from every
// subdirectory named ig or structure-definitions, plus JSON files directly under dir.
func LoadProfileCatalogFromDirTree(root string) (MemoryProfileCatalog, error) {
	var dirs []string
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return LoadProfileCatalogFromDir(root)
	}
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil || !d.IsDir() {
			return nil
		}
		name := d.Name()
		if name == "ig" || name == "structure-definitions" || path == root {
			dirs = append(dirs, path)
		}
		return nil
	})
	if len(dirs) == 0 {
		dirs = []string{root}
	}
	seen := make(map[string]struct{})
	var unique []string
	for _, dir := range dirs {
		if _, ok := seen[dir]; ok {
			continue
		}
		seen[dir] = struct{}{}
		unique = append(unique, dir)
	}
	return LoadProfileCatalogFromDirs(unique...)
}
