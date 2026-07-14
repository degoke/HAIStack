package auth

import (
	"context"

	"github.com/degoke/health-ai-stack/pkg/audit"
)

// AuditingEngine wraps a PolicyEngine and optionally emits allow/deny events
// through pkg/audit. Auth does not own audit storage; supply an audit.Logger
// (typically audit.StoreAdapter) from the host application.
type AuditingEngine struct {
	Inner PolicyEngine
	Audit audit.Logger
}

var _ PolicyEngine = (*AuditingEngine)(nil)

// CanReadResource implements PolicyEngine and emits an auth decision event.
func (a *AuditingEngine) CanReadResource(ctx context.Context, req ReadRequest) (Decision, error) {
	d, err := a.Inner.CanReadResource(ctx, req)
	if err == nil {
		a.log(ctx, audit.AuthDecisionEvent{
			Actor:        req.Principal.ID,
			Tenant:       req.Tenant.TenantID,
			AuthAction:   string(ActionRead),
			ResourceType: req.ResourceType,
			ResourceID:   req.ID,
			Allowed:      d.Allowed,
			Reason:       d.Reason,
		})
	}
	return d, err
}

// CanWriteResource implements PolicyEngine and emits an auth decision event.
func (a *AuditingEngine) CanWriteResource(ctx context.Context, req WriteRequest) (Decision, error) {
	d, err := a.Inner.CanWriteResource(ctx, req)
	if err == nil {
		a.log(ctx, audit.AuthDecisionEvent{
			Actor:        req.Principal.ID,
			Tenant:       req.Tenant.TenantID,
			AuthAction:   string(ActionWrite),
			ResourceType: req.ResourceType,
			ResourceID:   req.ID,
			Allowed:      d.Allowed,
			Reason:       d.Reason,
		})
	}
	return d, err
}

// CanExecuteView implements PolicyEngine and emits an auth decision event.
func (a *AuditingEngine) CanExecuteView(ctx context.Context, req ViewRequest) (Decision, error) {
	d, err := a.Inner.CanExecuteView(ctx, req)
	if err == nil {
		a.log(ctx, audit.AuthDecisionEvent{
			Actor:        req.Principal.ID,
			Tenant:       req.Tenant.TenantID,
			AuthAction:   string(ActionExecuteView),
			ResourceType: req.ResourceType,
			ViewName:     req.ViewName,
			Allowed:      d.Allowed,
			Reason:       d.Reason,
		})
	}
	return d, err
}

// CanExecuteAITool implements PolicyEngine and emits an auth decision event.
func (a *AuditingEngine) CanExecuteAITool(ctx context.Context, req AIToolRequest) (Decision, error) {
	d, err := a.Inner.CanExecuteAITool(ctx, req)
	if err == nil {
		a.log(ctx, audit.AuthDecisionEvent{
			Actor:        req.Principal.ID,
			Tenant:       req.Tenant.TenantID,
			AuthAction:   string(ActionExecuteAITool),
			ResourceType: req.ResourceType,
			ViewName:     req.ViewName,
			ToolName:     req.ToolName,
			Allowed:      d.Allowed,
			Reason:       d.Reason,
		})
	}
	return d, err
}

// CanPushDeviceEvent implements PolicyEngine and emits an auth decision event.
func (a *AuditingEngine) CanPushDeviceEvent(ctx context.Context, req DevicePushRequest) (Decision, error) {
	d, err := a.Inner.CanPushDeviceEvent(ctx, req)
	if err == nil {
		a.log(ctx, audit.AuthDecisionEvent{
			Actor:      req.DeviceID,
			Tenant:     req.TenantID,
			AuthAction: string(ActionPushDevice),
			Allowed:    d.Allowed,
			Reason:     d.Reason,
		})
	}
	return d, err
}

// CanInstallModule implements PolicyEngine and emits an auth decision event.
func (a *AuditingEngine) CanInstallModule(ctx context.Context, req ModuleInstallRequest) (Decision, error) {
	d, err := a.Inner.CanInstallModule(ctx, req)
	if err == nil {
		a.log(ctx, audit.AuthDecisionEvent{
			Actor:      req.Principal.ID,
			Tenant:     req.Tenant.TenantID,
			AuthAction: string(ActionInstallModule),
			ModuleName: req.ModuleName,
			Allowed:    d.Allowed,
			Reason:     d.Reason,
		})
	}
	return d, err
}

// CheckPatientScope implements PolicyEngine and emits an auth decision event.
func (a *AuditingEngine) CheckPatientScope(ctx context.Context, req PatientScopeRequest) (Decision, error) {
	d, err := a.Inner.CheckPatientScope(ctx, req)
	if err == nil {
		a.log(ctx, audit.AuthDecisionEvent{
			Actor:        req.Principal.ID,
			Tenant:       req.Tenant.TenantID,
			AuthAction:   string(ActionPatientAccess),
			ResourceType: "Patient",
			ResourceID:   req.PatientID,
			Allowed:      d.Allowed,
			Reason:       d.Reason,
		})
	}
	return d, err
}

func (a *AuditingEngine) log(ctx context.Context, ev audit.AuthDecisionEvent) {
	if a == nil || a.Audit == nil {
		return
	}
	_ = audit.LogAuthDecision(ctx, a.Audit, ev)
}
