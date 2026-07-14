package auth

import (
	"context"
	"fmt"

	"github.com/degoke/health-ai-stack/pkg/modules"
)

// PrincipalResolver maps ambient request context to a principal and tenant for
// non-view, non-AI package integrations such as module installation.
type PrincipalResolver func(ctx context.Context) (Principal, TenantContext, error)

// ModuleInstallerAuthorizer adapts Engine to modules.InstallAuthorizer.
type ModuleInstallerAuthorizer struct {
	Engine  *Engine
	Resolve PrincipalResolver
}

var _ modules.InstallAuthorizer = (*ModuleInstallerAuthorizer)(nil)

// AuthorizeModuleInstall implements modules.InstallAuthorizer.
func (a *ModuleInstallerAuthorizer) AuthorizeModuleInstall(ctx context.Context, req modules.InstallAuthRequest) error {
	if a == nil || a.Engine == nil {
		return fmt.Errorf("%w: engine required", ErrInvalidConfig)
	}
	if a.Resolve == nil {
		return fmt.Errorf("%w", ErrMissingResolver)
	}
	principal, tenant, err := a.Resolve(ctx)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrDenied, err)
	}
	d, err := a.Engine.CanInstallModule(ctx, ModuleInstallRequest{
		Principal:           principal,
		Tenant:              tenant,
		ModuleName:          req.Module.Manifest.Name,
		ModuleVersion:       req.Module.Manifest.Version,
		RequiredPermissions: append([]string(nil), req.Module.Manifest.Permissions...),
	})
	if err != nil {
		return err
	}
	if !d.Allowed {
		return fmt.Errorf("%w: %s", ErrDenied, d.Reason)
	}
	return nil
}
