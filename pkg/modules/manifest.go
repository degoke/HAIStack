package modules

import (
	"time"

	"github.com/degoke/health-ai-stack/pkg/registry"
)

// Manifest describes a module's identity and declared capabilities.
// Declarations for views, aiTools, permissions, syncPolicies, subscriptions,
// and migrations are persisted but not executed in v1.
type Manifest struct {
	Name            string          `json:"name"`
	Version         string          `json:"version"`
	Description     string          `json:"description,omitempty"`
	Dependencies    []DependencyRef `json:"dependencies,omitempty"`
	Resources       []string        `json:"resources,omitempty"`
	DefinitionFiles []string        `json:"definitionFiles,omitempty"`
	Views           []string        `json:"views,omitempty"`
	AITools         []string        `json:"aiTools,omitempty"`
	Permissions     []string        `json:"permissions,omitempty"`
	SyncPolicies    []string        `json:"syncPolicies,omitempty"`
	Subscriptions   []string        `json:"subscriptions,omitempty"`
	Migrations      []string        `json:"migrations,omitempty"`
}

// DependencyRef names a required module and its minimum compatible version.
type DependencyRef struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// Declarations groups deferred subsystem names that are stored but not executed
// in v1.
type Declarations struct {
	Views         []string `json:"views,omitempty"`
	AITools       []string `json:"aiTools,omitempty"`
	Permissions   []string `json:"permissions,omitempty"`
	SyncPolicies  []string `json:"syncPolicies,omitempty"`
	Subscriptions []string `json:"subscriptions,omitempty"`
	Migrations    []string `json:"migrations,omitempty"`
}

// Module is a normalized, loaded module with its manifest and in-memory
// definition payloads.
type Module struct {
	Path          string
	Manifest      Manifest
	ManifestBytes []byte
	Definitions   [][]byte
}

// DefinitionRef identifies one installed definition by canonical URL and
// version.
type DefinitionRef struct {
	CanonicalURL string `json:"canonicalUrl"`
	Version      string `json:"version"`
}

// Plan describes the intended outcome of an install or upgrade without
// mutating persistent state.
type Plan struct {
	Name                 string          `json:"name"`
	Version              string          `json:"version"`
	Action               string          `json:"action"`
	Dependencies         []DependencyRef `json:"dependencies,omitempty"`
	ResourcesToEnable    []string        `json:"resourcesToEnable,omitempty"`
	DefinitionsToInstall []DefinitionRef `json:"definitionsToInstall,omitempty"`
	DefinitionsToRemove  []DefinitionRef `json:"definitionsToRemove,omitempty"`
	Deferred             Declarations    `json:"deferred,omitempty"`
}

// InstallResult reports what an install changed in the registry.
type InstallResult struct {
	Action               string             `json:"action"`
	Name                 string             `json:"name"`
	Version              string             `json:"version"`
	EnabledResources     []string           `json:"enabledResources,omitempty"`
	InstalledDefinitions []DefinitionRef    `json:"installedDefinitions,omitempty"`
	Deferred             Declarations       `json:"deferred,omitempty"`
	Snapshot             *registry.Snapshot `json:"-"`
}

// UpgradeResult reports what an upgrade changed in the registry.
type UpgradeResult struct {
	Name                 string             `json:"name"`
	OldVersion           string             `json:"oldVersion"`
	NewVersion           string             `json:"newVersion"`
	EnabledResources     []string           `json:"enabledResources,omitempty"`
	InstalledDefinitions []DefinitionRef    `json:"installedDefinitions,omitempty"`
	RemovedDefinitions   []DefinitionRef    `json:"removedDefinitions,omitempty"`
	Deferred             Declarations       `json:"deferred,omitempty"`
	Snapshot             *registry.Snapshot `json:"-"`
}

// InstalledModule is a module-centric view of one registered module and its
// runtime contributions.
type InstalledModule struct {
	Name         string          `json:"name"`
	Version      string          `json:"version"`
	Dependencies []DependencyRef `json:"dependencies,omitempty"`
	Resources    []string        `json:"resources,omitempty"`
	Definitions  []DefinitionRef `json:"definitions,omitempty"`
	Deferred     Declarations    `json:"deferred,omitempty"`
	RegisteredAt time.Time       `json:"registeredAt"`
}

const metadataManifestKey = "manifest"
