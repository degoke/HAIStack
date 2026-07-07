package modules_test

import (
	"context"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/modules"
	"github.com/degoke/health-ai-stack/pkg/store"
)

type memModuleStore struct {
	modules map[string]store.ModuleRecord
}

func newMemModuleStore() *memModuleStore {
	return &memModuleStore{modules: make(map[string]store.ModuleRecord)}
}

func (s *memModuleStore) Register(_ context.Context, m store.ModuleRecord) error {
	s.modules[m.Name] = m
	return nil
}

func (s *memModuleStore) Get(_ context.Context, name string) (*store.ModuleRecord, error) {
	m, ok := s.modules[name]
	if !ok {
		return nil, modules.ErrModuleNotFound
	}
	copy := m
	return &copy, nil
}

func (s *memModuleStore) List(_ context.Context) ([]store.ModuleRecord, error) {
	out := make([]store.ModuleRecord, 0, len(s.modules))
	for _, m := range s.modules {
		out = append(out, m)
	}
	return out, nil
}

func (s *memModuleStore) Unregister(_ context.Context, name string) error {
	delete(s.modules, name)
	return nil
}

func registerModule(t *testing.T, s *memModuleStore, name, version string, deps []modules.DependencyRef) {
	t.Helper()
	manifest := modules.Manifest{Name: name, Version: version, Dependencies: deps}
	meta, err := modules.ManifestToMetadata(manifest)
	if err != nil {
		t.Fatalf("manifest metadata: %v", err)
	}
	if err := s.Register(context.Background(), store.ModuleRecord{
		Name:         name,
		Version:      version,
		Metadata:     meta,
		RegisteredAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("register %s: %v", name, err)
	}
}

func TestDependencyResolverSuccess(t *testing.T) {
	s := newMemModuleStore()
	registerModule(t, s, "core", "1.0.0", nil)
	resolver := modules.NewDependencyResolver(s)
	mod := &modules.Module{Manifest: modules.Manifest{
		Name:         "scheduling",
		Version:      "1.0.0",
		Dependencies: []modules.DependencyRef{{Name: "core", Version: "1.0.0"}},
	}}
	if err := resolver.Resolve(context.Background(), mod); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
}

func TestDependencyResolverMissingDependency(t *testing.T) {
	s := newMemModuleStore()
	resolver := modules.NewDependencyResolver(s)
	mod := &modules.Module{Manifest: modules.Manifest{
		Name:         "scheduling",
		Version:      "1.0.0",
		Dependencies: []modules.DependencyRef{{Name: "core", Version: "1.0.0"}},
	}}
	if err := resolver.Resolve(context.Background(), mod); err == nil {
		t.Fatal("expected missing dependency error")
	} else if !isError(err, modules.ErrMissingDependency) {
		t.Errorf("error = %v, want ErrMissingDependency", err)
	}
}

func TestDependencyResolverVersionMismatch(t *testing.T) {
	s := newMemModuleStore()
	registerModule(t, s, "core", "1.0.0", nil)
	resolver := modules.NewDependencyResolver(s)
	mod := &modules.Module{Manifest: modules.Manifest{
		Name:         "scheduling",
		Version:      "1.0.0",
		Dependencies: []modules.DependencyRef{{Name: "core", Version: "2.0.0"}},
	}}
	if err := resolver.Resolve(context.Background(), mod); err == nil {
		t.Fatal("expected version mismatch error")
	} else if !isError(err, modules.ErrDependencyVersionMismatch) {
		t.Errorf("error = %v, want ErrDependencyVersionMismatch", err)
	}
}

func TestDependencyResolverCircularDependency(t *testing.T) {
	s := newMemModuleStore()
	registerModule(t, s, "a", "1.0.0", []modules.DependencyRef{{Name: "b", Version: "1.0.0"}})
	registerModule(t, s, "b", "1.0.0", []modules.DependencyRef{{Name: "a", Version: "1.0.0"}})
	resolver := modules.NewDependencyResolver(s)
	mod := &modules.Module{Manifest: modules.Manifest{
		Name:    "c",
		Version: "1.0.0",
	}}
	if err := resolver.Resolve(context.Background(), mod); err == nil {
		t.Fatal("expected circular dependency error")
	} else if !isError(err, modules.ErrCircularDependency) {
		t.Errorf("error = %v, want ErrCircularDependency", err)
	}
}

func TestDependencyResolverSelfCycle(t *testing.T) {
	s := newMemModuleStore()
	resolver := modules.NewDependencyResolver(s)
	mod := &modules.Module{Manifest: modules.Manifest{
		Name:         "self",
		Version:      "1.0.0",
		Dependencies: []modules.DependencyRef{{Name: "self", Version: "1.0.0"}},
	}}
	if err := resolver.Resolve(context.Background(), mod); err == nil {
		t.Fatal("expected circular dependency error")
	} else if !isError(err, modules.ErrCircularDependency) {
		t.Errorf("error = %v, want ErrCircularDependency", err)
	}
}
