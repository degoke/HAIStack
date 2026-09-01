// Package modules implements haistack-modules, a manifest-driven installer for
// Health AI Stack. It turns a local module directory into runtime capabilities
// by driving the existing registry catalog, ModuleStore, and
// RegistryInstallStore.
//
// v1 is registry-first: it fully installs and upgrades resource enablement,
// profiles, and search parameters. It persists declarations for views, AI tools,
// permissions, sync policies, subscriptions, and migrations in module
// metadata but does not execute those subsystems yet.
//
// # Module layout
//
// A module is a directory containing a module.json manifest and optional
// definition JSON files. Compiled IG artefacts may be loaded from an ig/
// directory declared as igPackage:
//
//	modules/<name>/
//	├── module.json
//	├── ig/*.json
//	└── definitions/*.json
//
// module.json declares the module identity, dependencies, resources to enable,
// definition files and/or an igPackage directory to ingest, and deferred
// subsystem declarations.
//
// # Manifest format
//
// Recommended manifest shape for v1:
//
//	{
//	  "name": "scheduling",
//	  "version": "1.0.0",
//	  "dependencies": [{"name": "core", "version": "1.0.0"}],
//	  "resources": ["Appointment", "Schedule", "Slot"],
//	  "definitionFiles": ["definitions/Appointment-search-date.json"],
//	  "views": ["AppointmentCalendar"],
//	  "aiTools": [],
//	  "permissions": ["read-appointment"],
//	  "syncPolicies": [],
//	  "subscriptions": [],
//	  "migrations": []
//	}
//
// Fields:
//   - name: module identity. Must be unique across installed modules.
//   - version: semver string. Upgrades must move to a greater version.
//   - dependencies: required modules with a minimum compatible semver version.
//   - resources: FHIR resource types to enable from the seeded base definitions.
//   - definitionFiles: relative JSON files to ingest as profiles or search parameters.
//   - igPackage: relative directory of compiled IG JSON (all *.json files are loaded).
//   - views, aiTools, permissions, syncPolicies, subscriptions, migrations:
//     declaration-only arrays stored in ModuleRecord.Metadata for future subsystems.
//
// # Public API
//
// Manager is the primary runtime-facing entry point. Construct it with the
// existing persistence stores and a registry.Manager:
//
//	reg := registry.NewManager(registry.Config{Definitions: defs, Installs: installs})
//	mgr := modules.NewManager(modules.Config{
//	    ModuleStore:          moduleStore,
//	    DefinitionStore:      defs,
//	    RegistryInstallStore: installs,
//	    RegistryManager:      reg,
//	    Now:                  time.Now,
//	})
//
//	result, err := mgr.Install(ctx, "modules/scheduling")
//
// Manager methods:
//   - Install loads a module directory and applies it to the registry.
//   - InstallAll applies a build-time module set as one compensating
//     transaction.
//   - Upgrade loads a module directory and upgrades an already-installed module.
//   - Uninstall removes a module and its registry contributions.
//   - List returns all installed modules with their runtime contributions.
//   - Inspect returns one installed module.
//   - PlanInstall returns the intended install or upgrade plan without mutating state.
//
// Deployments that distribute modules from an untrusted source should set
// modules.Config.Verifier. Ed25519ModuleVerifier verifies a detached
// module.json.sig over the exact manifest and definition bytes; custom
// ModuleVerifier implementations can enforce another trust root or format.
//
// Install/upgrade behavior:
//   - Dependencies are validated for exact name match and minimum semver version.
//   - Cycles in the dependency graph, including self-references, are rejected.
//   - Base FHIR definitions are seeded before any resource is enabled.
//   - Declared resources are enabled and declared definitions are ingested with
//     ModuleName and SourceModule provenance.
//   - The registry snapshot is rebuilt once at the end.
//   - Upgrades must be to a newer version and are additive only in v1; removing
//     a previously declared resource or definition returns ErrUpgradeWouldRemove.
//
// Uninstall behavior:
//   - Refuses if another installed module depends on the target.
//   - Refuses to disable a resource type that still has persisted resources
//     when ResourceStore is configured.
//   - Disables resources contributed by the target that are not still required.
//   - Removes registry install rows and definition records owned by the target.
//   - Removes the ModuleStore entry last, after registry state is safely updated.
//   - Rebuilds the registry snapshot after uninstall.
//
// # Internal services
//
// The Manager composes several focused services:
//   - Loader reads and validates a module directory.
//   - DependencyResolver validates dependencies and detects cycles.
//   - RegistryApplier enables resources, installs definitions, and rebuilds snapshots.
//   - Installer orchestrates install/upgrade/uninstall planning and execution.
//   - CapabilitySnapshotBuilder returns a module-centric view of installed modules.
//
// Paid modules, remote registry syncing, hospital-specific patch layering, and
// tenant-specific modules are out of v1 scope but are intended future
// extensions.
package modules
