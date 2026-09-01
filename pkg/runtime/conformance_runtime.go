package runtime

import (
	"context"
	"fmt"
	"sync"

	"github.com/degoke/health-ai-stack/pkg/fhirpath"
	"github.com/degoke/health-ai-stack/pkg/registry"
	"github.com/degoke/health-ai-stack/pkg/search"
	"github.com/degoke/health-ai-stack/pkg/validate"
)

// ConformanceRuntime holds the live registry snapshot and validation state used
// by HTTP handlers and resource validation. Call Refresh after catalog changes.
type ConformanceRuntime struct {
	mu sync.RWMutex

	manager         *registry.Manager
	fhirpath        fhirpath.Engine
	snapshot        *registry.Snapshot
	profileCatalog  *validate.RegistryProfileCatalog
	engine          *validate.ReloadableEngine
	searchRegistry  *search.SnapshotRegistry
	patientResolver *registry.PatientReferenceResolver
}

// ConformanceRuntimeConfig wires a conformance runtime from registry state.
type ConformanceRuntimeConfig struct {
	Manager         *registry.Manager
	FHIRPath        fhirpath.Engine
	ProfileCatalog  *validate.RegistryProfileCatalog
	Engine          *validate.ReloadableEngine
	SearchRegistry  *search.SnapshotRegistry
	PatientResolver *registry.PatientReferenceResolver
	InitialSnapshot *registry.Snapshot
}

// NewConformanceRuntime builds a runtime view from an initial compiled snapshot.
func NewConformanceRuntime(cfg ConformanceRuntimeConfig) (*ConformanceRuntime, error) {
	if cfg.Manager == nil {
		return nil, fmt.Errorf("runtime: conformance runtime requires manager")
	}
	if cfg.Engine == nil {
		return nil, fmt.Errorf("runtime: conformance runtime requires engine")
	}
	if cfg.ProfileCatalog == nil {
		return nil, fmt.Errorf("runtime: conformance runtime requires profile catalog")
	}
	snapshot := cfg.InitialSnapshot
	if snapshot == nil {
		var err error
		snapshot, err = cfg.Manager.RebuildSnapshot(context.Background())
		if err != nil {
			return nil, err
		}
	}
	if err := cfg.ProfileCatalog.Reload(snapshot); err != nil {
		return nil, err
	}
	engineCfg := cfg.Engine.Config()
	engineCfg.InstalledTypes = snapshot
	engineCfg.ProfileCatalog = cfg.ProfileCatalog
	if err := cfg.Engine.Reload(engineCfg); err != nil {
		return nil, err
	}
	if cfg.SearchRegistry != nil {
		cfg.SearchRegistry.SetSnapshot(snapshot)
	}
	if cfg.PatientResolver != nil {
		cfg.PatientResolver.Snapshot = snapshot
	}
	return &ConformanceRuntime{
		manager:         cfg.Manager,
		fhirpath:        cfg.FHIRPath,
		snapshot:        snapshot,
		profileCatalog:  cfg.ProfileCatalog,
		engine:          cfg.Engine,
		searchRegistry:  cfg.SearchRegistry,
		patientResolver: cfg.PatientResolver,
	}, nil
}

// Refresh rebuilds the compiled snapshot and reloads validation consumers.
func (r *ConformanceRuntime) Refresh(ctx context.Context) (*registry.Snapshot, error) {
	if r == nil || r.manager == nil {
		return nil, fmt.Errorf("runtime: conformance runtime is not configured")
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	snapshot, err := r.manager.RebuildSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	if err := r.profileCatalog.Reload(snapshot); err != nil {
		return nil, err
	}
	engineCfg := r.engine.Config()
	engineCfg.InstalledTypes = snapshot
	engineCfg.ProfileCatalog = r.profileCatalog
	if err := r.engine.Reload(engineCfg); err != nil {
		return nil, err
	}
	if r.searchRegistry != nil {
		r.searchRegistry.SetSnapshot(snapshot)
	}
	if r.patientResolver != nil {
		r.patientResolver.Snapshot = snapshot
	}
	r.snapshot = snapshot
	return snapshot, nil
}

// Snapshot returns the current compiled registry view.
func (r *ConformanceRuntime) Snapshot() *registry.Snapshot {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.snapshot
}

// Engine returns the current validation engine.
func (r *ConformanceRuntime) Engine() validate.Engine {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.engine
}

// ProfileCatalog returns the live profile catalog.
func (r *ConformanceRuntime) ProfileCatalog() *validate.RegistryProfileCatalog {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.profileCatalog
}
