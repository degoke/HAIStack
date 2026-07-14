package modules

import "context"

// InstallAuthRequest carries module authorization context for install and
// upgrade operations.
type InstallAuthRequest struct {
	Path   string
	Module Module
	Plan   *Plan
	Action string
}

// InstallAuthorizer is the pluggable authorization seam for module
// install/upgrade operations.
type InstallAuthorizer interface {
	AuthorizeModuleInstall(ctx context.Context, req InstallAuthRequest) error
}

// InstallAuthorizerFunc adapts a function to InstallAuthorizer.
type InstallAuthorizerFunc func(ctx context.Context, req InstallAuthRequest) error

// AuthorizeModuleInstall implements InstallAuthorizer.
func (f InstallAuthorizerFunc) AuthorizeModuleInstall(ctx context.Context, req InstallAuthRequest) error {
	return f(ctx, req)
}
