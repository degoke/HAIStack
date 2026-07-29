package modules

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/degoke/health-ai-stack/pkg/registry"
	"github.com/degoke/health-ai-stack/pkg/store"
)

// Installer orchestrates install/upgrade/uninstall planning and execution.
// It owns the registry applier and module store updates.
type Installer struct {
	modules  store.ModuleStore
	applier  *RegistryApplier
	installs store.RegistryInstallStore
	defs     store.DefinitionStore
	now      func() time.Time
}

// NewInstaller creates an installer from the same persistence pieces used by
// the public manager.
func NewInstaller(modules store.ModuleStore, defs store.DefinitionStore, installs store.RegistryInstallStore, reg *registry.Manager, now func() time.Time) *Installer {
	if now == nil {
		now = time.Now
	}
	return &Installer{
		modules:  modules,
		applier:  NewRegistryApplier(reg, defs, installs),
		installs: installs,
		defs:     defs,
		now:      now,
	}
}

// PlanInstall computes the desired state for a module without mutating the
// registry or module store.
func (i *Installer) PlanInstall(ctx context.Context, mod *Module) (*Plan, error) {
	existing, existingErr := i.modules.Get(ctx, mod.Manifest.Name)
	action := "install"
	oldVersion := ""
	if existingErr == nil {
		action = "upgrade"
		oldVersion = existing.Version
	}

	plan := &Plan{
		Name:              mod.Manifest.Name,
		Version:           mod.Manifest.Version,
		Action:            action,
		Dependencies:      append([]DependencyRef(nil), mod.Manifest.Dependencies...),
		ResourcesToEnable: sortedStringSet(mod.Manifest.Resources),
		Deferred:          declarationsFromManifest(mod.Manifest),
	}

	for _, def := range mod.Definitions {
		parsed, _, err := registry.ParseDefinition(def)
		if err != nil {
			return nil, fmt.Errorf("parse definition for plan: %w", err)
		}
		plan.DefinitionsToInstall = append(plan.DefinitionsToInstall, DefinitionRef{
			CanonicalURL: parsed.CanonicalURL,
			Version:      parsed.Version,
		})
	}

	if action == "upgrade" && oldVersion != "" {
		if ok, err := isGreaterVersion(mod.Manifest.Version, oldVersion); err != nil {
			return nil, fmt.Errorf("compare versions: %w", err)
		} else if !ok {
			if mod.Manifest.Version == oldVersion {
				plan.Action = "noop"
				return plan, nil
			}
			return nil, fmt.Errorf("%w: %s %s -> %s", ErrDowngradeNotAllowed, mod.Manifest.Name, oldVersion, mod.Manifest.Version)
		}
		oldManifest, err := manifestFromMetadata(existing.Metadata)
		if err != nil {
			return nil, fmt.Errorf("decode existing manifest: %w", err)
		}
		oldDefs, err := i.installedDefinitionsByModule(ctx, mod.Manifest.Name)
		if err != nil {
			return nil, err
		}
		removedResources := stringSetMinus(oldManifest.Resources, mod.Manifest.Resources)
		removedDefs := definitionRefsMinus(oldDefs, plan.DefinitionsToInstall)
		if len(removedResources) > 0 || len(removedDefs) > 0 {
			return nil, fmt.Errorf("%w: resources %v, definitions %v", ErrUpgradeWouldRemove, removedResources, removedDefs)
		}
		plan.ResourcesToEnable = sortedStringSet(union(oldManifest.Resources, mod.Manifest.Resources))
		plan.DefinitionsToInstall = sortedDefinitionRefs(unionDefinitionRefs(oldDefs, plan.DefinitionsToInstall))
	}

	return plan, nil
}

// installedDefinitionsByModule returns the canonical URLs of non-StructureDefinition
// registry installs sourced by the named module.
func (i *Installer) installedDefinitionsByModule(ctx context.Context, name string) ([]DefinitionRef, error) {
	allInstalls, err := i.installs.ListInstalled(ctx, store.RegistryInstallFilter{})
	if err != nil {
		return nil, fmt.Errorf("list registry installs: %w", err)
	}
	var refs []DefinitionRef
	for _, inst := range allInstalls {
		if inst.SourceModule != name {
			continue
		}
		if inst.DefinitionKind == store.DefinitionKindStructureDefinition {
			continue
		}
		refs = append(refs, DefinitionRef{
			CanonicalURL: inst.CanonicalURL,
			Version:      inst.Version,
		})
	}
	return refs, nil
}

// Install applies a module to the registry and registers the module.
func (i *Installer) Install(ctx context.Context, mod *Module) (*InstallResult, error) {
	plan, err := i.PlanInstall(ctx, mod)
	if err != nil {
		return nil, err
	}
	if plan.Action == "noop" {
		snapshot, err := i.applier.RebuildSnapshot(ctx)
		if err != nil {
			return nil, err
		}
		return &InstallResult{
			Name:     plan.Name,
			Version:  plan.Version,
			Deferred: plan.Deferred,
			Snapshot: snapshot,
		}, nil
	}

	// Ensure base definitions are present before enabling anything.
	if err := i.applier.SeedBundled(ctx); err != nil {
		return nil, err
	}

	result := &InstallResult{
		Name:     plan.Name,
		Version:  plan.Version,
		Deferred: plan.Deferred,
	}

	for _, resourceType := range plan.ResourcesToEnable {
		if err := i.applier.EnableResource(ctx, resourceType); err != nil {
			return nil, err
		}
		result.EnabledResources = append(result.EnabledResources, resourceType)
	}

	for _, def := range mod.Definitions {
		parsed, _, err := registry.ParseDefinition(def)
		if err != nil {
			return nil, fmt.Errorf("parse definition: %w", err)
		}
		provenance := registry.InstallProvenance{
			PackageName:    "haistack-modules",
			PackageVersion: mod.Manifest.Version,
			ModuleName:     mod.Manifest.Name,
			SourceModule:   mod.Manifest.Name,
		}
		if err := i.applier.InstallDefinition(ctx, def, provenance); err != nil {
			return nil, fmt.Errorf("install definition %s: %w", parsed.CanonicalURL, err)
		}
		result.InstalledDefinitions = append(result.InstalledDefinitions, DefinitionRef{
			CanonicalURL: parsed.CanonicalURL,
			Version:      parsed.Version,
		})
	}

	if err := i.registerModule(ctx, mod); err != nil {
		return nil, err
	}

	snapshot, err := i.applier.RebuildSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	result.Snapshot = snapshot

	return result, nil
}

// Upgrade applies a newer version of an already-installed module.
func (i *Installer) Upgrade(ctx context.Context, mod *Module) (*UpgradeResult, error) {
	existing, err := i.modules.Get(ctx, mod.Manifest.Name)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrModuleNotFound, mod.Manifest.Name)
	}

	plan, err := i.PlanInstall(ctx, mod)
	if err != nil {
		return nil, err
	}
	if plan.Action != "upgrade" {
		return nil, fmt.Errorf("%w: %s is not installed", ErrModuleNotFound, mod.Manifest.Name)
	}

	oldManifest, err := manifestFromMetadata(existing.Metadata)
	if err != nil {
		return nil, fmt.Errorf("decode existing manifest: %w", err)
	}

	if err := i.applier.SeedBundled(ctx); err != nil {
		return nil, err
	}

	result := &UpgradeResult{
		Name:       plan.Name,
		OldVersion: existing.Version,
		NewVersion: plan.Version,
		Deferred:   plan.Deferred,
	}

	oldResources := make(map[string]struct{})
	for _, r := range oldManifest.Resources {
		oldResources[r] = struct{}{}
	}
	for _, resourceType := range plan.ResourcesToEnable {
		if _, ok := oldResources[resourceType]; ok {
			continue
		}
		if err := i.applier.EnableResource(ctx, resourceType); err != nil {
			return nil, err
		}
		result.EnabledResources = append(result.EnabledResources, resourceType)
	}

	oldDefs, err := i.installedDefinitionsByModule(ctx, mod.Manifest.Name)
	if err != nil {
		return nil, err
	}
	oldDefKeys := make(map[string]struct{})
	for _, ref := range oldDefs {
		oldDefKeys[ref.CanonicalURL+"|"+ref.Version] = struct{}{}
	}
	for _, def := range mod.Definitions {
		parsed, _, err := registry.ParseDefinition(def)
		if err != nil {
			return nil, fmt.Errorf("parse definition: %w", err)
		}
		key := parsed.CanonicalURL + "|" + parsed.Version
		if _, ok := oldDefKeys[key]; ok {
			continue
		}
		provenance := registry.InstallProvenance{
			PackageName:    "haistack-modules",
			PackageVersion: mod.Manifest.Version,
			ModuleName:     mod.Manifest.Name,
			SourceModule:   mod.Manifest.Name,
		}
		if err := i.applier.InstallDefinition(ctx, def, provenance); err != nil {
			return nil, fmt.Errorf("install definition %s: %w", parsed.CanonicalURL, err)
		}
		result.InstalledDefinitions = append(result.InstalledDefinitions, DefinitionRef{
			CanonicalURL: parsed.CanonicalURL,
			Version:      parsed.Version,
		})
	}

	if err := i.registerModule(ctx, mod); err != nil {
		return nil, err
	}

	snapshot, err := i.applier.RebuildSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	result.Snapshot = snapshot

	return result, nil
}

// Uninstall removes a module's registry contributions and module record.
func (i *Installer) Uninstall(ctx context.Context, name string) error {
	existing, err := i.modules.Get(ctx, name)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrModuleNotFound, name)
	}

	installed, err := i.modules.List(ctx)
	if err != nil {
		return fmt.Errorf("list installed modules: %w", err)
	}
	for _, rec := range installed {
		if rec.Name == name {
			continue
		}
		manifest, err := manifestFromMetadata(rec.Metadata)
		if err != nil {
			return fmt.Errorf("decode manifest for %q: %w", rec.Name, err)
		}
		for _, dep := range manifest.Dependencies {
			if dep.Name == name {
				return fmt.Errorf("%w: %s is required by %s", ErrModuleInUse, name, rec.Name)
			}
		}
	}

	existingManifest, err := manifestFromMetadata(existing.Metadata)
	if err != nil {
		return fmt.Errorf("decode existing manifest: %w", err)
	}

	// Gather the set of resources that other installed modules still need.
	otherResources := make(map[string]struct{})
	for _, rec := range installed {
		if rec.Name == name {
			continue
		}
		manifest, err := manifestFromMetadata(rec.Metadata)
		if err != nil {
			return fmt.Errorf("decode manifest for %q: %w", rec.Name, err)
		}
		for _, r := range manifest.Resources {
			otherResources[r] = struct{}{}
		}
	}

	// Disable registry contributions that are not still required by other
	// modules.
	for _, resourceType := range existingManifest.Resources {
		if _, ok := otherResources[resourceType]; ok {
			continue
		}
		if err := i.applier.DisableResource(ctx, resourceType); err != nil {
			return err
		}
	}

	// Remove registry install rows for definitions contributed by this module.
	allInstalls, err := i.installs.ListInstalled(ctx, store.RegistryInstallFilter{})
	if err != nil {
		return fmt.Errorf("list registry installs: %w", err)
	}
	for _, inst := range allInstalls {
		if inst.SourceModule != name {
			continue
		}
		if err := i.applier.DeleteInstall(ctx, store.RegistryInstallFilter{
			DefinitionKind:     inst.DefinitionKind,
			TargetResourceType: inst.TargetResourceType,
			CanonicalURL:       inst.CanonicalURL,
			Version:            inst.Version,
		}); err != nil {
			return fmt.Errorf("remove registry install for %s: %w", inst.TargetResourceType, err)
		}
	}

	// Remove definition catalog entries owned by this module. Definitions that
	// another module installed will have a different or overwritten module_name,
	// so this removes only this module's direct contributions.
	allDefs, err := i.defs.List(ctx, store.DefinitionFilter{ModuleName: name})
	if err != nil {
		return fmt.Errorf("list module definitions: %w", err)
	}
	for _, def := range allDefs {
		if err := i.applier.DeleteDefinition(ctx, def.CanonicalURL, def.Version); err != nil {
			return fmt.Errorf("delete definition %s: %w", def.CanonicalURL, err)
		}
	}

	if err := i.modules.Unregister(ctx, name); err != nil {
		return fmt.Errorf("unregister module: %w", err)
	}

	if _, err := i.applier.RebuildSnapshot(ctx); err != nil {
		return err
	}

	return nil
}

func (i *Installer) registerModule(ctx context.Context, mod *Module) error {
	meta, err := ManifestToMetadata(mod.Manifest)
	if err != nil {
		return err
	}
	record := store.ModuleRecord{
		Name:         mod.Manifest.Name,
		Version:      mod.Manifest.Version,
		Metadata:     meta,
		RegisteredAt: i.now().UTC(),
	}
	if err := i.modules.Register(ctx, record); err != nil {
		return fmt.Errorf("register module: %w", err)
	}
	return nil
}

func declarationsFromManifest(m Manifest) Declarations {
	return Declarations{
		Views:         append([]string(nil), m.Views...),
		AITools:       append([]string(nil), m.AITools...),
		Permissions:   append([]string(nil), m.Permissions...),
		SyncPolicies:  append([]string(nil), m.SyncPolicies...),
		Subscriptions: append([]string(nil), m.Subscriptions...),
		Migrations:    append([]string(nil), m.Migrations...),
	}
}

func definitionRefsMinus(a, b []DefinitionRef) []DefinitionRef {
	seen := make(map[string]struct{})
	for _, ref := range b {
		seen[ref.CanonicalURL+"|"+ref.Version] = struct{}{}
	}
	var out []DefinitionRef
	for _, ref := range a {
		if _, ok := seen[ref.CanonicalURL+"|"+ref.Version]; ok {
			continue
		}
		out = append(out, ref)
	}
	return out
}

func unionDefinitionRefs(a, b []DefinitionRef) []DefinitionRef {
	seen := make(map[string]struct{})
	out := make([]DefinitionRef, 0, len(a)+len(b))
	for _, ref := range a {
		key := ref.CanonicalURL + "|" + ref.Version
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, ref)
	}
	for _, ref := range b {
		key := ref.CanonicalURL + "|" + ref.Version
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, ref)
	}
	return out
}

func sortedDefinitionRefs(refs []DefinitionRef) []DefinitionRef {
	out := make([]DefinitionRef, len(refs))
	copy(out, refs)
	sort.Slice(out, func(i, j int) bool {
		if out[i].CanonicalURL != out[j].CanonicalURL {
			return out[i].CanonicalURL < out[j].CanonicalURL
		}
		return out[i].Version < out[j].Version
	})
	return out
}

func stringSetMinus(a, b []string) []string {
	seen := make(map[string]struct{})
	for _, x := range b {
		seen[x] = struct{}{}
	}
	var out []string
	for _, x := range a {
		if _, ok := seen[x]; ok {
			continue
		}
		out = append(out, x)
	}
	return out
}

func union(a, b []string) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0, len(a)+len(b))
	for _, x := range a {
		if _, ok := seen[x]; ok {
			continue
		}
		seen[x] = struct{}{}
		out = append(out, x)
	}
	for _, x := range b {
		if _, ok := seen[x]; ok {
			continue
		}
		seen[x] = struct{}{}
		out = append(out, x)
	}
	return out
}
