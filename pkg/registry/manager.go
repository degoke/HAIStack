package registry

import (
	"context"
	"time"

	"github.com/degoke/health-ai-stack/pkg/store"
)

const defaultFHIRVersion = "4.0.1"

// Config configures a registry Manager.
type Config struct {
	Definitions   store.DefinitionStore
	Installs      store.RegistryInstallStore
	FHIRVersion   string
	Now           func() time.Time
	SearchReindex SearchReindexNotifier
}

// Manager seeds, installs, enables, and compiles the FHIR definition catalog.
type Manager struct {
	definitions   store.DefinitionStore
	installs      store.RegistryInstallStore
	fhirVersion   string
	now           func() time.Time
	searchReindex SearchReindexNotifier
	snapshot      *Snapshot
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
		definitions:   cfg.Definitions,
		installs:      cfg.Installs,
		fhirVersion:   fhirVersion,
		now:           now,
		searchReindex: cfg.SearchReindex,
	}
}

// SeedBundled loads embedded R4 base definitions into the catalog idempotently.
func (m *Manager) SeedBundled(ctx context.Context) error {
	resources, err := loadR4Bundle()
	if err != nil {
		return err
	}
	for _, raw := range resources {
		if err := m.ingestDefinition(ctx, raw, InstallProvenance{
			PackageName:    "hl7.fhir.r4.core",
			PackageVersion: m.fhirVersion,
		}, false); err != nil {
			return err
		}
	}
	return nil
}

// InstallDefinition ingests one additional definition resource with provenance.
func (m *Manager) InstallDefinition(ctx context.Context, jsonData []byte, provenance InstallProvenance) error {
	return m.ingestDefinition(ctx, jsonData, provenance, true)
}

func (m *Manager) ingestDefinition(ctx context.Context, jsonData []byte, provenance InstallProvenance, scheduleReindex bool) error {
	parsed, targets, err := ParseDefinition(jsonData)
	if err != nil {
		return err
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
