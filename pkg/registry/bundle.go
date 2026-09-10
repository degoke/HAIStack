package registry

import (
	"embed"
	"io/fs"
	"strings"
	"sync"
)

//go:embed internal/bundles/r4/structure-definitions/*.json internal/bundles/r4/search-parameters/*.json
var r4BundleFS embed.FS

const (
	embeddedStructureDefinitionsRoot = "internal/bundles/r4/structure-definitions"
	embeddedSearchParametersRoot     = "internal/bundles/r4/search-parameters"
)

// forEachEmbeddedDefinitionJSON reads bundled JSON definitions under root one file at a time.
func forEachEmbeddedDefinitionJSON(root string, fn func([]byte) error) error {
	return fs.WalkDir(r4BundleFS, root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".json") {
			return nil
		}
		raw, err := fs.ReadFile(r4BundleFS, path)
		if err != nil {
			return err
		}
		return fn(raw)
	})
}

func forEachEmbeddedBundledDefinition(fn func([]byte) error) error {
	for _, raw := range bundledDefinitionRecords() {
		if err := fn(raw); err != nil {
			return err
		}
	}
	return nil
}

var (
	bundledDefinitionsOnce sync.Once
	bundledDefinitionsRaw  [][]byte
	bundledDefinitionsErr  error
)

func bundledDefinitionRecords() [][]byte {
	bundledDefinitionsOnce.Do(func() {
		err := forEachEmbeddedDefinitionJSON(embeddedStructureDefinitionsRoot, func(raw []byte) error {
			bundledDefinitionsRaw = append(bundledDefinitionsRaw, raw)
			return nil
		})
		if err != nil {
			bundledDefinitionsErr = err
			return
		}
		bundledDefinitionsErr = forEachEmbeddedDefinitionJSON(embeddedSearchParametersRoot, func(raw []byte) error {
			bundledDefinitionsRaw = append(bundledDefinitionsRaw, raw)
			return nil
		})
	})
	if bundledDefinitionsErr != nil {
		return nil
	}
	return bundledDefinitionsRaw
}
