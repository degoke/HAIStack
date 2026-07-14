package ai

import "context"

// ApprovalRequest carries context for a write approval decision.
type ApprovalRequest struct {
	Actor        string
	Subject      string
	Operation    string
	ResourceType string
	ID           string
	Fields       map[string]any
	Preview      any
}

// ApprovalResult is the outcome of an approval request.
type ApprovalResult struct {
	Approved bool
	Token    string
}

// ApprovalHook is the optional human approval seam for policy-gated writes.
type ApprovalHook interface {
	RequestApproval(ctx context.Context, req ApprovalRequest) (*ApprovalResult, error)
}

// ApprovalHookFunc adapts a function to the ApprovalHook interface.
type ApprovalHookFunc func(ctx context.Context, req ApprovalRequest) (*ApprovalResult, error)

// RequestApproval implements ApprovalHook.
func (f ApprovalHookFunc) RequestApproval(ctx context.Context, req ApprovalRequest) (*ApprovalResult, error) {
	return f(ctx, req)
}
