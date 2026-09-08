package modules

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	maxManifestBytes   = 1 << 20
	maxDefinitionBytes = 16 << 20
	maxDefinitionFiles = 1024
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
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve module path: %w", err)
	}
	manifestPath := filepath.Join(path, "module.json")
	data, err := l.fs.ReadFile(manifestPath, maxManifestBytes)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", ErrManifestNotFound, manifestPath)
		}
		return nil, fmt.Errorf("read manifest: %w", err)
	}

	var manifest Manifest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return nil, fmt.Errorf("%w: decode module.json: %v", ErrInvalidManifest, err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("%w: module.json contains multiple JSON values", ErrInvalidManifest)
		}
		return nil, fmt.Errorf("%w: decode module.json: %v", ErrInvalidManifest, err)
	}

	if err := validateManifest(&manifest); err != nil {
		return nil, err
	}

	files, err := l.definitionFileList(path, &manifest)
	if err != nil {
		return nil, err
	}

	definitions, err := l.loadDefinitions(path, files)
	if err != nil {
		return nil, err
	}

	return &Module{
		Path:            path,
		Manifest:        manifest,
		ManifestBytes:   append([]byte(nil), data...),
		Definitions:     definitions,
		DefinitionPaths: append([]string(nil), files...),
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
	if len(m.DefinitionFiles) > maxDefinitionFiles {
		return fmt.Errorf("%w: too many definition files (maximum %d)", ErrInvalidManifest, maxDefinitionFiles)
	}
	for _, f := range m.DefinitionFiles {
		if f == "" {
			return fmt.Errorf("%w: empty definition file entry", ErrInvalidManifest)
		}
		if _, ok := seenFiles[f]; ok {
			return fmt.Errorf("%w: duplicate definition file %q", ErrInvalidManifest, f)
		}
		seenFiles[f] = struct{}{}
	}
	if strings.TrimSpace(m.IGPackage) == "" {
		return nil
	}
	if filepath.IsAbs(m.IGPackage) {
		return fmt.Errorf("%w: igPackage must be relative to the module directory", ErrInvalidManifest)
	}
	return nil
}

func (l *Loader) definitionFileList(path string, m *Manifest) ([]string, error) {
	files := append([]string(nil), m.DefinitionFiles...)
	if strings.TrimSpace(m.IGPackage) == "" {
		return files, nil
	}
	igFiles, err := l.listIGPackageFiles(path, m.IGPackage)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{}, len(files))
	for _, f := range files {
		seen[f] = struct{}{}
	}
	for _, f := range igFiles {
		if _, ok := seen[f]; ok {
			continue
		}
		files = append(files, f)
	}
	if len(files) > maxDefinitionFiles {
		return nil, fmt.Errorf("%w: too many definition files (maximum %d)", ErrInvalidManifest, maxDefinitionFiles)
	}
	return files, nil
}

func (l *Loader) listIGPackageFiles(modulePath, igPackage string) ([]string, error) {
	dir := filepath.Join(modulePath, igPackage)
	if !isPathUnderRoot(modulePath, dir) {
		return nil, fmt.Errorf("%w: igPackage %q escapes module directory", ErrInvalidManifest, igPackage)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read igPackage %q: %w", igPackage, err)
	}
	var files []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.EqualFold(filepath.Ext(name), ".json") {
			continue
		}
		rel := filepath.ToSlash(filepath.Join(igPackage, name))
		files = append(files, rel)
	}
	sort.Strings(files)
	return files, nil
}

// LoadDefinitionsFromIG reads FHIR JSON definition files from a compiled IG
// output directory (SUSHI fsh-generated/resources or an unpacked NPM package).
func LoadDefinitionsFromIG(dir string) ([][]byte, error) {
	loader := NewLoader()
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("resolve IG directory: %w", err)
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		return nil, fmt.Errorf("read IG directory: %w", err)
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !strings.EqualFold(filepath.Ext(entry.Name()), ".json") {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	if len(names) > maxDefinitionFiles {
		return nil, fmt.Errorf("%w: too many definition files (maximum %d)", ErrInvalidManifest, maxDefinitionFiles)
	}
	out := make([][]byte, 0, len(names))
	for _, name := range names {
		data, err := loader.fs.ReadFile(filepath.Join(abs, name), maxDefinitionBytes)
		if err != nil {
			return nil, fmt.Errorf("read IG resource %q: %w", name, err)
		}
		out = append(out, data)
	}
	return out, nil
}

func (l *Loader) loadDefinitions(path string, files []string) ([][]byte, error) {
	definitions := make([][]byte, 0, len(files))
	realRoot, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, fmt.Errorf("resolve module directory: %w", err)
	}
	for _, f := range files {
		resolved := filepath.Join(path, f)
		if !isPathUnderRoot(path, resolved) {
			return nil, fmt.Errorf("%w: definition file %q escapes module directory", ErrInvalidManifest, f)
		}
		if realTarget, evalErr := filepath.EvalSymlinks(resolved); evalErr == nil {
			if !isPathUnderRoot(realRoot, realTarget) {
				return nil, fmt.Errorf("%w: definition file %q escapes module directory through a symlink", ErrInvalidManifest, f)
			}
		} else if !os.IsNotExist(evalErr) {
			return nil, fmt.Errorf("resolve definition file %q: %w", f, evalErr)
		}
		data, err := l.fs.ReadFile(resolved, maxDefinitionBytes)
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
	ReadFile(path string, maxBytes int) ([]byte, error)
}

type osFS struct{}

func (osFS) ReadFile(path string, maxBytes int) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, int64(maxBytes)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxBytes {
		return nil, fmt.Errorf("%w: maximum %d bytes", ErrModuleFileTooLarge, maxBytes)
	}
	return data, nil
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
