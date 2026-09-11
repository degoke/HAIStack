package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/degoke/health-ai-stack/pkg/jobs"
	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/terminology"
)

const DefaultFHIRVersion = "4.0.1"

const defaultFHIRVersion = DefaultFHIRVersion

// BundledCorePackageName is the package id for embedded R4 base definitions.
// SeedBundled intentionally does not enqueue ValueSet pre-expand for this pack.
const BundledCorePackageName = "hl7.fhir.r4.core"

// ModulesPackageName is the prefix for haistack module package ids.
const ModulesPackageName = "haistack-modules"

// ModulesPackageID returns the package id for one module (haistack-modules/<name>).
// Completion is tracked as ModulesPackageID(name)@manifestVersion.
func ModulesPackageID(moduleName string) string {
	if moduleName == "" {
		return ModulesPackageName
	}
	return ModulesPackageName + "/" + moduleName
}

// Config configures a registry Manager.
type Config struct {
	Definitions         store.DefinitionStore
	Installs            store.RegistryInstallStore
	FHIRVersion         string
	Now                 func() time.Time
	SearchReindex       SearchReindexNotifier
	Terminology         store.TerminologyStore
	TerminologyScope    string
	GlobalTerminology   store.TerminologyStore
	TerminologyInstalls store.TerminologyInstallStore
	TerminologyCache    terminology.Invalidator
	JobStore            store.JobStore
	PackageInstalls     store.PackageInstallStore
	PreExpandValueSets  bool
}

// Manager seeds, installs, enables, and compiles the FHIR definition catalog.
type Manager struct {
	definitions         store.DefinitionStore
	installs            store.RegistryInstallStore
	fhirVersion         string
	now                 func() time.Time
	searchReindex       SearchReindexNotifier
	terminology         store.TerminologyStore
	terminologyScope    string
	globalTerminology   store.TerminologyStore
	terminologyInstalls store.TerminologyInstallStore
	terminologyCache    terminology.Invalidator
	jobStore            store.JobStore
	packageInstalls     store.PackageInstallStore
	preExpandValueSets  bool
	snapshot            *Snapshot
	seedMu              sync.Mutex
	seeded              bool
}

// NewManager constructs a registry manager from persistence stores.
func NewManager(cfg Config) *Manager {
	fhirVersion := cfg.FHIRVersion
	if fhirVersion == "" {
		fhirVersion = defaultFHIRVersion
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &Manager{
		definitions:         cfg.Definitions,
		installs:            cfg.Installs,
		fhirVersion:         fhirVersion,
		now:                 now,
		searchReindex:       cfg.SearchReindex,
		terminology:         cfg.Terminology,
		terminologyScope:    cfg.TerminologyScope,
		globalTerminology:   cfg.GlobalTerminology,
		terminologyInstalls: cfg.TerminologyInstalls,
		terminologyCache:    cfg.TerminologyCache,
		jobStore:            cfg.JobStore,
		packageInstalls:     cfg.PackageInstalls,
		preExpandValueSets:  cfg.PreExpandValueSets,
	}
}

func (m *Manager) terminologyTarget(resourceType string) (store.TerminologyStore, string) {
	if m.globalTerminology != nil && (resourceType == "CodeSystem" || resourceType == "ValueSet") {
		return m.globalTerminology, terminology.GlobalScopeID
	}
	return m.terminology, m.terminologyScope
}

// SeedBundled loads embedded R4 base definitions into the catalog idempotently.
// Bundled core definitions never enqueue ValueSet pre-expand jobs; see BundledCorePackageName.
func (m *Manager) SeedBundled(ctx context.Context) error {
	m.seedMu.Lock()
	defer m.seedMu.Unlock()
	if m.seeded {
		return nil
	}
	resources, err := loadR4Bundle()
	if err != nil {
		return err
	}
	for _, raw := range resources {
		parsed, _, err := ParseDefinition(raw)
		if err != nil {
			return err
		}
		if _, err := m.definitions.Get(ctx, parsed.CanonicalURL, parsed.Version); err == nil {
			// Bundled definitions are immutable base catalog content. Never
			// overwrite a module-owned definition or reset a prior custom JSON
			// payload merely because another install reseeds the bundle.
			continue
		} else if !strings.Contains(strings.ToLower(err.Error()), "not found") {
			return fmt.Errorf("check bundled definition %s: %w", parsed.CanonicalURL, err)
		}
		if err := m.ingestDefinition(ctx, raw, InstallProvenance{
			PackageName:    BundledCorePackageName,
			PackageVersion: m.fhirVersion,
		}, false); err != nil {
			return err
		}
	}
	m.seeded = true
	return nil
}

// InstallDefinitionsFromFS ingests every .json definition file under root from fsys.
func (m *Manager) InstallDefinitionsFromFS(ctx context.Context, fsys fs.FS, root string, provenance InstallProvenance) error {
	resources, err := LoadDefinitionJSONs(fsys, root)
	if err != nil {
		return err
	}
	for _, raw := range resources {
		if err := m.ingestDefinition(ctx, raw, provenance, true); err != nil {
			return err
		}
	}
	return m.CompletePackageInstall(ctx, provenance)
}

// InstallDefinitionsFromDir ingests every .json definition file under dir on the local filesystem.
func (m *Manager) InstallDefinitionsFromDir(ctx context.Context, dir string, provenance InstallProvenance) error {
	return m.InstallDefinitionsFromFS(ctx, os.DirFS(dir), ".", provenance)
}

// InstallDefinition ingests one additional definition resource with provenance.
func (m *Manager) InstallDefinition(ctx context.Context, jsonData []byte, provenance InstallProvenance) error {
	return m.ingestDefinition(ctx, jsonData, provenance, true)
}

// CompletePackageInstall enqueues one batched ValueSet pre-expand job for a package
// after callers finish a loop of InstallDefinition calls and records completion.
func (m *Manager) CompletePackageInstall(ctx context.Context, provenance InstallProvenance) error {
	if err := m.enqueuePackPreExpand(ctx, provenance); err != nil {
		return err
	}
	if m.packageInstalls != nil && provenance.PackageName != "" && provenance.PackageVersion != "" {
		if err := m.packageInstalls.MarkComplete(ctx, provenance.PackageName, provenance.PackageVersion, m.now().UTC()); err != nil {
			return fmt.Errorf("mark package install complete: %w", err)
		}
	}
	return nil
}

// DeleteDefinition removes a catalog entry and its terminology projection.
func (m *Manager) DeleteDefinition(ctx context.Context, canonicalURL, version string) error {
	m.seedMu.Lock()
	m.seeded = false
	m.seedMu.Unlock()
	r, err := m.definitions.Get(ctx, canonicalURL, version)
	if err != nil {
		return err
	}
	if (m.terminology != nil || m.globalTerminology != nil) && (r.FHIRResourceType == "CodeSystem" || r.FHIRResourceType == "ValueSet") {
		termStore, termScope := m.terminologyTarget(r.FHIRResourceType)
		if err := termStore.DeleteProjections(ctx, termScope, r.FHIRResourceType, canonicalURL, version); err != nil {
			return err
		}
		if err := termStore.DeleteResource(ctx, termScope, r.FHIRResourceType, canonicalURL, version); err != nil {
			return err
		}
		if m.terminologyCache != nil {
			if r.FHIRResourceType == "CodeSystem" {
				m.terminologyCache.InvalidateCodeSystem(canonicalURL, version)
			} else {
				m.terminologyCache.InvalidateValueSet(canonicalURL, version)
			}
		}
	}
	if (r.FHIRResourceType == "CodeSystem" || r.FHIRResourceType == "ValueSet") && m.terminologyInstalls != nil {
		if err := m.terminologyInstalls.Delete(ctx, store.TerminologyInstallFilter{
			ResourceType: r.FHIRResourceType,
			CanonicalURL: canonicalURL,
			Version:      version,
		}); err != nil {
			return err
		}
	}
	return m.definitions.Delete(ctx, canonicalURL, version)
}

func (m *Manager) ingestDefinition(ctx context.Context, jsonData []byte, provenance InstallProvenance, scheduleReindex bool) error {
	parsed, targets, err := ParseDefinition(jsonData)
	if err != nil {
		return err
	}
	if existing, err := m.definitions.Get(ctx, parsed.CanonicalURL, parsed.Version); err == nil {
		if existing.ModuleName != provenance.ModuleName {
			return fmt.Errorf("%w: %s|%s is owned by %q", ErrDefinitionConflict, parsed.CanonicalURL, parsed.Version, existing.ModuleName)
		}
	} else if !strings.Contains(strings.ToLower(err.Error()), "not found") {
		return fmt.Errorf("check existing definition %s: %w", parsed.CanonicalURL, err)
	}
	record := store.DefinitionResourceRecord{
		CanonicalURL:     parsed.CanonicalURL,
		Version:          parsed.Version,
		FHIRVersion:      parsed.FHIRVersion,
		FHIRResourceType: parsed.FHIRResourceType,
		DefinitionKind:   parsed.DefinitionKind,
		Name:             parsed.Name,
		Status:           parsed.Status,
		PackageName:      provenance.PackageName,
		PackageVersion:   provenance.PackageVersion,
		ModuleName:       provenance.ModuleName,
		JSONData:         append([]byte(nil), jsonData...),
		InstalledAt:      m.now().UTC(),
	}
	if err := m.definitions.Upsert(ctx, record, targets); err != nil {
		return err
	}
	if (m.terminology != nil || m.globalTerminology != nil) && (parsed.FHIRResourceType == "CodeSystem" || parsed.FHIRResourceType == "ValueSet") {
		var meta struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(jsonData, &meta); err != nil {
			return err
		}
		termStore, termScope := m.terminologyTarget(parsed.FHIRResourceType)
		tr := store.TerminologyResourceRecord{ScopeID: termScope, ResourceType: parsed.FHIRResourceType, ResourceID: meta.ID, CanonicalURL: parsed.CanonicalURL, Version: parsed.Version, Status: parsed.Status, ResourceJSON: append([]byte(nil), jsonData...), SourceModule: provenance.SourceModule}
		existing, err := termStore.FindResource(ctx, termScope, parsed.FHIRResourceType, parsed.CanonicalURL, parsed.Version)
		if err != nil {
			return fmt.Errorf("check terminology resource %s: %w", parsed.CanonicalURL, err)
		}
		if existing == nil {
			if err := terminology.Install(ctx, termStore, tr); err != nil {
				return fmt.Errorf("compile terminology: %w", err)
			}
		}
		if m.terminologyInstalls != nil {
			sourceModule := provenance.SourceModule
			if sourceModule == "" {
				sourceModule = provenance.PackageName
			}
			if err := terminology.EnsureInstallOptIn(ctx, m.terminologyInstalls, store.TerminologyInstallRecord{
				PackName:     provenance.PackageName,
				PackVersion:  provenance.PackageVersion,
				ResourceType: parsed.FHIRResourceType,
				CanonicalURL: parsed.CanonicalURL,
				Version:      parsed.Version,
				Enabled:      true,
				SourceModule: sourceModule,
				InstalledAt:  m.now().UTC(),
			}); err != nil {
				return err
			}
		}
		if m.terminologyCache != nil {
			if parsed.FHIRResourceType == "CodeSystem" {
				m.terminologyCache.InvalidateCodeSystem(parsed.CanonicalURL, parsed.Version)
			} else {
				m.terminologyCache.InvalidateValueSet(parsed.CanonicalURL, parsed.Version)
			}
		}
	}
	if scheduleReindex && parsed.DefinitionKind == store.DefinitionKindSearchParameter && m.searchReindex != nil {
		types := searchBaseResourceTypes(targets)
		if spNotifier, ok := m.searchReindex.(SearchParameterReindexNotifier); ok {
			if err := spNotifier.ScheduleSearchParameterReindex(ctx, parsed.CanonicalURL, parsed.Version, types...); err != nil {
				return err
			}
		} else if err := m.scheduleSearchReindex(ctx, types...); err != nil {
			return err
		}
	}
	if provenance.SourceModule != "" {
		install := store.RegistryInstallRecord{
			DefinitionKind:     parsed.DefinitionKind,
			CanonicalURL:       parsed.CanonicalURL,
			Version:            parsed.Version,
			TargetResourceType: firstTargetResourceType(targets),
			Enabled:            true,
			SourceModule:       provenance.SourceModule,
			InstalledAt:        m.now().UTC(),
		}
		if install.TargetResourceType != "" {
			if err := m.installs.UpsertInstall(ctx, install); err != nil {
				return err
			}
		}
	}
	return nil
}

func firstTargetResourceType(targets []store.DefinitionTargetRecord) string {
	for _, target := range targets {
		if target.TargetResourceType != "" {
			return target.TargetResourceType
		}
	}
	return ""
}

// EnableResource marks a resource type as enabled when its base StructureDefinition exists.
func (m *Manager) EnableResource(ctx context.Context, resourceType string) error {
	definitions, err := m.definitions.List(ctx, store.DefinitionFilter{
		FHIRVersion:        m.fhirVersion,
		DefinitionKind:     store.DefinitionKindStructureDefinition,
		TargetResourceType: resourceType,
	})
	if err != nil {
		return err
	}
	sort.Slice(definitions, func(i, j int) bool {
		if definitions[i].CanonicalURL != definitions[j].CanonicalURL {
			return definitions[i].CanonicalURL < definitions[j].CanonicalURL
		}
		return definitions[i].Version > definitions[j].Version
	})
	var base *store.DefinitionResourceRecord
	for i := range definitions {
		if definitions[i].Name == resourceType || definitions[i].CanonicalURL == structureDefinitionURL(resourceType) {
			base = &definitions[i]
			break
		}
	}
	if base == nil {
		return ErrMissingDefinition
	}
	if err := m.installs.SetEnabled(ctx, store.RegistryInstallRecord{
		DefinitionKind:     store.DefinitionKindStructureDefinition,
		CanonicalURL:       base.CanonicalURL,
		Version:            base.Version,
		TargetResourceType: resourceType,
		Enabled:            true,
		InstalledAt:        m.now().UTC(),
	}); err != nil {
		return err
	}
	return m.scheduleSearchReindex(ctx, resourceType)
}

// DisableResource marks a resource type as disabled in the install overlay.
func (m *Manager) DisableResource(ctx context.Context, resourceType string) error {
	enabled, err := m.installs.ListEnabled(ctx)
	if err != nil {
		return err
	}
	for _, row := range enabled {
		if row.TargetResourceType != resourceType {
			continue
		}
		row.Enabled = false
		row.InstalledAt = m.now().UTC()
		return m.installs.SetEnabled(ctx, row)
	}
	return nil
}

// RebuildSnapshot reloads catalog and install state into an immutable compiled view.
func (m *Manager) RebuildSnapshot(ctx context.Context) (*Snapshot, error) {
	snapshot, err := CompileSnapshot(ctx, m.definitions, m.installs, m.fhirVersion, m.now)
	if err != nil {
		return nil, err
	}
	m.snapshot = snapshot
	return snapshot, nil
}

// Snapshot returns the last compiled snapshot, if any.
func (m *Manager) Snapshot() *Snapshot {
	return m.snapshot
}

func structureDefinitionURL(resourceType string) string {
	return "http://hl7.org/fhir/StructureDefinition/" + resourceType
}

func searchBaseResourceTypes(targets []store.DefinitionTargetRecord) []string {
	seen := make(map[string]struct{})
	var out []string
	for _, target := range targets {
		if target.TargetResourceType == "" {
			continue
		}
		if target.TargetRole != "" && target.TargetRole != targetRoleSearchBase {
			continue
		}
		if _, ok := seen[target.TargetResourceType]; ok {
			continue
		}
		seen[target.TargetResourceType] = struct{}{}
		out = append(out, target.TargetResourceType)
	}
	return out
}

func (m *Manager) scheduleSearchReindex(ctx context.Context, resourceTypes ...string) error {
	if m.searchReindex == nil || len(resourceTypes) == 0 {
		return nil
	}
	return m.searchReindex.ScheduleReindex(ctx, resourceTypes...)
}

func (m *Manager) enqueuePackPreExpand(ctx context.Context, provenance InstallProvenance) error {
	if !m.preExpandValueSets || m.jobStore == nil || provenance.PackageName == "" || provenance.PackageVersion == "" {
		return nil
	}
	if provenance.PackageName == BundledCorePackageName {
		return nil
	}
	listStore := m.terminology
	if m.globalTerminology != nil {
		listStore = m.globalTerminology
	}
	termScope := m.terminologyScope
	if m.globalTerminology != nil {
		termScope = terminology.GlobalScopeID
	}
	if m.definitions == nil || listStore == nil {
		return nil
	}
	urls, err := terminology.EligiblePackPreExpandURLs(ctx, m.definitions, listStore, termScope, provenance.PackageName, provenance.PackageVersion)
	if err != nil {
		return fmt.Errorf("check pack pre-expand eligibility: %w", err)
	}
	if len(urls) == 0 {
		return nil
	}
	if err := jobs.EnqueuePackPreExpand(ctx, m.jobStore, termScope, provenance.PackageName, provenance.PackageVersion, m.terminologyScope, m.now); err != nil {
		return fmt.Errorf("enqueue pack pre-expand: %w", err)
	}
	return nil
}
