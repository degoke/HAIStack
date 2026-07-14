package view

import "context"

// AuthRequest carries the authorization context for one view execution.
type AuthRequest struct {
	ViewName     string         `json:"viewName"`
	Version      string         `json:"version,omitempty"`
	ResourceType string         `json:"resourceType"`
	Actor        string         `json:"actor"`
	Subject      string         `json:"subject,omitempty"`
	Permissions  []string       `json:"permissions,omitempty"`
	Parameters   map[string]any `json:"parameters,omitempty"`
}

// Authorizer is the pluggable authorization seam. If an Executor is configured
// without an Authorizer, executions proceed without permission checks. If an
// Authorizer is configured and the view declares permissions, the Authorizer is
// invoked before any resources are read.
type Authorizer interface {
	AuthorizeView(ctx context.Context, req AuthRequest) error
}

// AuthorizerFunc adapts a function to the Authorizer interface.
type AuthorizerFunc func(ctx context.Context, req AuthRequest) error

// AuthorizeView implements Authorizer.
func (f AuthorizerFunc) AuthorizeView(ctx context.Context, req AuthRequest) error {
	return f(ctx, req)
}
