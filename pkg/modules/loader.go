package modules

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Loader reads a local module directory, parses module.json, resolves relative
// file references, and returns a normalized Module.
type Loader struct {
	fs fileSystem
}

// NewLoader creates a loader that reads from the real filesystem.
func NewLoader() *Loader {
	return &Loader{fs: osFS{}}
}

// Load reads the manifest at path/module.json and loads all referenced
// definition files.
func (l *Loader) Load(path string) (*Module, error) {
	manifestPath := filepath.Join(path, "module.json")
	data, err := l.fs.ReadFile(manifestPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", ErrManifestNotFound, manifestPath)
		}
		return nil, fmt.Errorf("read manifest: %w", err)
	}

	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("%w: decode module.json: %v", ErrInvalidManifest, err)
	}

	if err := validateManifest(&manifest); err != nil {
		return nil, err
	}

	definitions, err := l.loadDefinitions(path, manifest.DefinitionFiles)
	if err != nil {
		return nil, err
	}

	return &Module{
		Path:        path,
		Manifest:    manifest,
		Definitions: definitions,
	}, nil
}

func validateManifest(m *Manifest) error {
	if m.Name == "" {
		return fmt.Errorf("%w: missing name", ErrInvalidManifest)
	}
	if m.Version == "" {
		return fmt.Errorf("%w: missing version", ErrInvalidManifest)
	}
	if _, err := parseVersion(m.Version); err != nil {
		return fmt.Errorf("%w: invalid version %q: %v", ErrInvalidManifest, m.Version, err)
	}
	for i, dep := range m.Dependencies {
		if dep.Name == "" {
			return fmt.Errorf("%w: dependency %d missing name", ErrInvalidManifest, i)
		}
		if dep.Version == "" {
			return fmt.Errorf("%w: dependency %q missing version", ErrInvalidManifest, dep.Name)
		}
		if _, err := parseVersion(dep.Version); err != nil {
			return fmt.Errorf("%w: dependency %q has invalid version %q: %v", ErrInvalidManifest, dep.Name, dep.Version, err)
		}
	}
	seenDeps := make(map[string]struct{})
	for _, dep := range m.Dependencies {
		if _, ok := seenDeps[dep.Name]; ok {
			return fmt.Errorf("%w: duplicate dependency %q", ErrInvalidManifest, dep.Name)
		}
		seenDeps[dep.Name] = struct{}{}
	}
	seenResources := make(map[string]struct{})
	for _, r := range m.Resources {
		if r == "" {
			return fmt.Errorf("%w: empty resource entry", ErrInvalidManifest)
		}
		if _, ok := seenResources[r]; ok {
			return fmt.Errorf("%w: duplicate resource %q", ErrInvalidManifest, r)
		}
		seenResources[r] = struct{}{}
	}
	seenFiles := make(map[string]struct{})
	for _, f := range m.DefinitionFiles {
		if f == "" {
			return fmt.Errorf("%w: empty definition file entry", ErrInvalidManifest)
		}
		if _, ok := seenFiles[f]; ok {
			return fmt.Errorf("%w: duplicate definition file %q", ErrInvalidManifest, f)
		}
		seenFiles[f] = struct{}{}
	}
	return nil
}

func (l *Loader) loadDefinitions(path string, files []string) ([][]byte, error) {
	definitions := make([][]byte, 0, len(files))
	for _, f := range files {
		resolved := filepath.Join(path, f)
		if !isPathUnderRoot(path, resolved) {
			return nil, fmt.Errorf("%w: definition file %q escapes module directory", ErrInvalidManifest, f)
		}
		data, err := l.fs.ReadFile(resolved)
		if err != nil {
			return nil, fmt.Errorf("read definition file %q: %w", f, err)
		}
		definitions = append(definitions, data)
	}
	return definitions, nil
}

func isPathUnderRoot(root, target string) bool {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(absRoot, absTarget)
	if err != nil {
		return false
	}
	return rel != ".." && !filepath.IsAbs(rel) && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// fileSystem abstracts filesystem access so tests can inject fake module
// directories.
type fileSystem interface {
	ReadFile(path string) ([]byte, error)
}

type osFS struct{}

func (osFS) ReadFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

// sortedStringSet returns a sorted, deduplicated copy of xs.
func sortedStringSet(xs []string) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0, len(xs))
	for _, x := range xs {
		if _, ok := seen[x]; ok {
			continue
		}
		seen[x] = struct{}{}
		out = append(out, x)
	}
	sort.Strings(out)
	return out
}

// manifestFromMetadata decodes a manifest that was serialized into a module
// record's metadata.
func manifestFromMetadata(meta map[string]string) (Manifest, error) {
	var manifest Manifest
	if raw := meta[metadataManifestKey]; raw != "" {
		if err := json.Unmarshal([]byte(raw), &manifest); err != nil {
			return Manifest{}, fmt.Errorf("decode stored manifest metadata: %w", err)
		}
	}
	return manifest, nil
}

// ManifestToMetadata serializes a manifest for storage in a module record.
func ManifestToMetadata(manifest Manifest) (map[string]string, error) {
	data, err := json.Marshal(manifest)
	if err != nil {
		return nil, fmt.Errorf("marshal manifest metadata: %w", err)
	}
	return map[string]string{metadataManifestKey: string(data)}, nil
}
