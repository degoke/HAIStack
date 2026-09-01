package modules

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/degoke/health-ai-stack/pkg/registry"
	"github.com/degoke/health-ai-stack/pkg/store"
)

// Config supplies the stable persistence pieces required by the module manager.
// Tenant-aware wrappers can be added later without changing this shape.
type Config struct {
	ModuleStore          store.ModuleStore
	DefinitionStore      store.DefinitionStore
	RegistryInstallStore store.RegistryInstallStore
	RegistryManager      *registry.Manager
	ResourceStore        store.ResourceStore
	Authorizer           InstallAuthorizer
	Verifier             ModuleVerifier
	Now                  func() time.Time
}

// Manager is the public runtime-facing API for installing, upgrading,
// uninstalling, and inspecting modules.
type Manager struct {
	cfg         Config
	loader      *Loader
	resolver    *DependencyResolver
	installer   *Installer
	builder     *CapabilitySnapshotBuilder
	mu          sync.Mutex
	afterChange func(ctx context.Context) error
}

// NewManager constructs a module manager from persistence stores.
func NewManager(cfg Config) *Manager {
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &Manager{
		cfg:       cfg,
		loader:    NewLoader(),
		resolver:  NewDependencyResolver(cfg.ModuleStore),
		installer: NewInstallerWithResources(cfg.ModuleStore, cfg.DefinitionStore, cfg.RegistryInstallStore, cfg.RegistryManager, cfg.ResourceStore, now),
		builder:   NewCapabilitySnapshotBuilder(cfg.ModuleStore, cfg.RegistryInstallStore),
	}
}

// SetAfterChange registers a callback invoked after successful install,
// upgrade, or uninstall operations.
func (m *Manager) SetAfterChange(fn func(ctx context.Context) error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.afterChange = fn
}

func (m *Manager) notifyChange(ctx context.Context) error {
	if m.afterChange == nil {
		return nil
	}
	return m.afterChange(ctx)
}

// Install loads a local module directory and applies it to the registry.
func (m *Manager) Install(ctx context.Context, path string) (InstallResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.installLocked(ctx, path)
}

func (m *Manager) installLocked(ctx context.Context, path string) (InstallResult, error) {
	mod, err := m.loader.Load(path)
	if err != nil {
		return InstallResult{}, err
	}
	if err := m.verifyModule(ctx, mod); err != nil {
		return InstallResult{}, err
	}
	if err := m.resolver.Resolve(ctx, mod); err != nil {
		return InstallResult{}, err
	}
	plan, err := m.installer.PlanInstall(ctx, mod)
	if err != nil {
		return InstallResult{}, err
	}
	if err := m.authorizeInstall(ctx, path, mod, plan); err != nil {
		return InstallResult{}, err
	}
	result, err := m.installer.Install(ctx, mod)
	if err != nil {
		return InstallResult{}, err
	}
	if err := m.notifyChange(ctx); err != nil {
		return InstallResult{}, err
	}
	return *result, nil
}

// InstallAll installs a build-time module set as one logical operation. If a
// later module fails, all state changes made by this batch are compensated.
func (m *Manager) InstallAll(ctx context.Context, paths ...string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	state, err := m.installer.captureState(ctx)
	if err != nil {
		return err
	}
	for _, path := range paths {
		if _, err := m.installLocked(ctx, path); err != nil {
			return errors.Join(err, m.installer.restoreState(state))
		}
	}
	return nil
}

// Upgrade loads a local module directory and upgrades the already-installed
// module of the same name to a newer version.
func (m *Manager) Upgrade(ctx context.Context, path string) (UpgradeResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	mod, err := m.loader.Load(path)
	if err != nil {
		return UpgradeResult{}, err
	}
	if err := m.verifyModule(ctx, mod); err != nil {
		return UpgradeResult{}, err
	}
	if err := m.resolver.Resolve(ctx, mod); err != nil {
		return UpgradeResult{}, err
	}
	plan, err := m.installer.PlanInstall(ctx, mod)
	if err != nil {
		return UpgradeResult{}, err
	}
	if err := m.authorizeInstall(ctx, path, mod, plan); err != nil {
		return UpgradeResult{}, err
	}
	result, err := m.installer.Upgrade(ctx, mod)
	if err != nil {
		return UpgradeResult{}, err
	}
	if err := m.notifyChange(ctx); err != nil {
		return UpgradeResult{}, err
	}
	return *result, nil
}

// Uninstall removes a module and its registry contributions.
func (m *Manager) Uninstall(ctx context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.installer.Uninstall(ctx, name); err != nil {
		return err
	}
	return m.notifyChange(ctx)
}

// List returns all installed modules with their runtime contributions.
func (m *Manager) List(ctx context.Context) ([]InstalledModule, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.builder.Build(ctx)
}

// Inspect returns one installed module, or ErrModuleNotFound if it is not
// registered.
func (m *Manager) Inspect(ctx context.Context, name string) (*InstalledModule, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	all, err := m.builder.Build(ctx)
	if err != nil {
		return nil, err
	}
	for _, mod := range all {
		if mod.Name == name {
			return &mod, nil
		}
	}
	return nil, fmt.Errorf("%w: %s", ErrModuleNotFound, name)
}

// PlanInstall returns the intended install or upgrade plan without mutating
// persistent state.
func (m *Manager) PlanInstall(ctx context.Context, path string) (*Plan, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	mod, err := m.loader.Load(path)
	if err != nil {
		return nil, err
	}
	if err := m.verifyModule(ctx, mod); err != nil {
		return nil, err
	}
	if err := m.resolver.Resolve(ctx, mod); err != nil {
		return nil, err
	}
	return m.installer.PlanInstall(ctx, mod)
}

func (m *Manager) authorizeInstall(ctx context.Context, path string, mod *Module, plan *Plan) error {
	if m.cfg.Authorizer == nil {
		return nil
	}
	return m.cfg.Authorizer.AuthorizeModuleInstall(ctx, InstallAuthRequest{
		Path:   path,
		Module: *mod,
		Plan:   plan,
		Action: plan.Action,
	})
}

func (m *Manager) verifyModule(ctx context.Context, mod *Module) error {
	if m.cfg.Verifier == nil {
		return nil
	}
	if mod == nil {
		return fmt.Errorf("module verifier received nil module")
	}
	return m.cfg.Verifier.VerifyModule(ctx, *mod)
}
