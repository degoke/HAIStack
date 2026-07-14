package auth

import (
	"context"
	"errors"
	"fmt"
)

// PolicyEngine is the auth decision surface used by the rest of the stack.
type PolicyEngine interface {
	CanReadResource(ctx context.Context, req ReadRequest) (Decision, error)
	CanWriteResource(ctx context.Context, req WriteRequest) (Decision, error)
	CanExecuteView(ctx context.Context, req ViewRequest) (Decision, error)
	CanExecuteAITool(ctx context.Context, req AIToolRequest) (Decision, error)
	CanPushDeviceEvent(ctx context.Context, req DevicePushRequest) (Decision, error)
	CanInstallModule(ctx context.Context, req ModuleInstallRequest) (Decision, error)
	CheckPatientScope(ctx context.Context, req PatientScopeRequest) (Decision, error)
}

// Config configures an Engine.
type Config struct {
	// Catalog supplies principals, roles, and devices. When nil, a new empty
	// catalog is created. Seed Principals/Roles/Devices are loaded into it.
	Catalog *Catalog

	Principals []Principal
	Roles      []Role
	Devices    []DeviceIdentity

	// Policy is a pre-parsed policy document. Ignored when Compiled is set.
	Policy *PolicyDocument

	// PolicyBytes are parsed according to PolicyFormat when Policy and Compiled
	// are both nil.
	PolicyBytes  []byte
	PolicyFormat PolicyFormat

	// Compiled is a pre-compiled policy evaluator.
	Compiled *CompiledPolicy
}

// Engine evaluates authorization decisions against an in-memory catalog and
// compiled policy. It does not own persistence or HTTP auth flows.
type Engine struct {
	catalog *Catalog
	policy  *CompiledPolicy
}

var _ PolicyEngine = (*Engine)(nil)

// NewEngine constructs an Engine from Config.
func NewEngine(cfg Config) (*Engine, error) {
	catalog := cfg.Catalog
	if catalog == nil {
		catalog = NewCatalog()
	}
	for _, r := range cfg.Roles {
		if err := catalog.PutRole(r); err != nil {
			return nil, err
		}
	}
	for _, p := range cfg.Principals {
		if err := catalog.PutPrincipal(p); err != nil {
			return nil, err
		}
	}
	for _, d := range cfg.Devices {
		if err := catalog.PutDevice(d); err != nil {
			return nil, err
		}
	}

	policy := cfg.Compiled
	switch {
	case policy != nil:
		// use as-is
	case cfg.Policy != nil:
		compiled, err := CompilePolicy(*cfg.Policy)
		if err != nil {
			return nil, err
		}
		policy = compiled
	case len(cfg.PolicyBytes) > 0:
		compiled, err := ParseAndCompilePolicy(cfg.PolicyBytes, cfg.PolicyFormat)
		if err != nil {
			return nil, err
		}
		policy = compiled
	default:
		return nil, fmt.Errorf("%w: policy required", ErrInvalidConfig)
	}

	return &Engine{catalog: catalog, policy: policy}, nil
}

// Catalog returns the engine's identity catalog.
func (e *Engine) Catalog() *Catalog {
	return e.catalog
}

// CanReadResource implements PolicyEngine.
func (e *Engine) CanReadResource(ctx context.Context, req ReadRequest) (Decision, error) {
	if err := requirePrincipalTenant(req.Principal, req.Tenant); err != nil {
		return Decision{}, err
	}
	if d := e.checkTenantBinding(req.Principal, req.Tenant); !d.Allowed {
		return d, nil
	}
	if d := e.checkPatientStub(req.Tenant, patientIDFromSubject(req.ID, req.ResourceType)); !d.Allowed {
		return d, nil
	}
	perms, roles, err := e.catalog.PermissionsFor(req.Principal, req.Tenant)
	if err != nil {
		return Decision{}, err
	}
	if d := checkRequiredPermissions(perms, req.RequiredPermissions); !d.Allowed {
		return d, nil
	}
	return e.policy.Evaluate(evalInput{
		Principal:           req.Principal,
		Tenant:              req.Tenant,
		Permissions:         perms,
		Roles:               roles,
		Action:              ActionRead,
		ResourceType:        req.ResourceType,
		ResourceID:          req.ID,
		RequiredPermissions: req.RequiredPermissions,
	}), nil
}

// CanWriteResource implements PolicyEngine.
func (e *Engine) CanWriteResource(ctx context.Context, req WriteRequest) (Decision, error) {
	if err := requirePrincipalTenant(req.Principal, req.Tenant); err != nil {
		return Decision{}, err
	}
	if d := e.checkTenantBinding(req.Principal, req.Tenant); !d.Allowed {
		return d, nil
	}
	if d := e.checkPatientStub(req.Tenant, patientIDFromSubject(req.ID, req.ResourceType)); !d.Allowed {
		return d, nil
	}
	perms, roles, err := e.catalog.PermissionsFor(req.Principal, req.Tenant)
	if err != nil {
		return Decision{}, err
	}
	if d := checkRequiredPermissions(perms, req.RequiredPermissions); !d.Allowed {
		return d, nil
	}
	return e.policy.Evaluate(evalInput{
		Principal:           req.Principal,
		Tenant:              req.Tenant,
		Permissions:         perms,
		Roles:               roles,
		Action:              ActionWrite,
		ResourceType:        req.ResourceType,
		ResourceID:          req.ID,
		RequiredPermissions: req.RequiredPermissions,
	}), nil
}

// CanExecuteView implements PolicyEngine.
func (e *Engine) CanExecuteView(ctx context.Context, req ViewRequest) (Decision, error) {
	if err := requirePrincipalTenant(req.Principal, req.Tenant); err != nil {
		return Decision{}, err
	}
	if d := e.checkTenantBinding(req.Principal, req.Tenant); !d.Allowed {
		return d, nil
	}
	perms, roles, err := e.catalog.PermissionsFor(req.Principal, req.Tenant)
	if err != nil {
		return Decision{}, err
	}
	// View declared permissions: principal must hold at least one (match view
	// executor semantics). Empty RequiredPermissions skips this gate.
	if d := checkAnyRequiredPermission(perms, req.RequiredPermissions); !d.Allowed {
		return d, nil
	}
	return e.policy.Evaluate(evalInput{
		Principal:           req.Principal,
		Tenant:              req.Tenant,
		Permissions:         perms,
		Roles:               roles,
		Action:              ActionExecuteView,
		ResourceType:        req.ResourceType,
		ViewName:            req.ViewName,
		ViewVersion:         req.Version,
		RequiredPermissions: req.RequiredPermissions,
	}), nil
}

// CanExecuteAITool implements PolicyEngine.
func (e *Engine) CanExecuteAITool(ctx context.Context, req AIToolRequest) (Decision, error) {
	if err := requirePrincipalTenant(req.Principal, req.Tenant); err != nil {
		return Decision{}, err
	}
	if d := e.checkTenantBinding(req.Principal, req.Tenant); !d.Allowed {
		return d, nil
	}
	perms, roles, err := e.catalog.PermissionsFor(req.Principal, req.Tenant)
	if err != nil {
		return Decision{}, err
	}
	if d := checkRequiredPermissions(perms, req.RequiredPermissions); !d.Allowed {
		return d, nil
	}
	return e.policy.Evaluate(evalInput{
		Principal:           req.Principal,
		Tenant:              req.Tenant,
		Permissions:         perms,
		Roles:               roles,
		Action:              ActionExecuteAITool,
		ResourceType:        req.ResourceType,
		ViewName:            req.ViewName,
		ToolName:            req.ToolName,
		RequiredPermissions: req.RequiredPermissions,
	}), nil
}

// CanPushDeviceEvent implements PolicyEngine.
func (e *Engine) CanPushDeviceEvent(ctx context.Context, req DevicePushRequest) (Decision, error) {
	if req.DeviceID == "" {
		return Decision{}, fmt.Errorf("%w: device id required", ErrInvalidConfig)
	}
	if req.TenantID == "" {
		return Decision{}, fmt.Errorf("%w: tenant id required", ErrInvalidConfig)
	}
	device, err := e.catalog.GetDevice(req.DeviceID)
	if err != nil {
		return Deny(fmt.Sprintf("device %q is not registered", req.DeviceID)), nil
	}
	if device.TenantID != req.TenantID {
		return Deny(fmt.Sprintf("device %q is registered to tenant %q, not %q", req.DeviceID, device.TenantID, req.TenantID)), nil
	}
	if device.Status == DeviceStatusRevoked {
		return Deny(fmt.Sprintf("device %q is revoked", req.DeviceID)), nil
	}
	if !device.Trusted {
		return Deny(fmt.Sprintf("device %q is not trusted", req.DeviceID)), nil
	}
	if device.Status != "" && device.Status != DeviceStatusActive {
		return Deny(fmt.Sprintf("device %q status is %q", req.DeviceID, device.Status)), nil
	}

	principal := Principal{ID: device.DeviceID, Kind: KindDevice}
	if device.LinkedPrincipalID != "" {
		if p, err := e.catalog.GetPrincipal(device.LinkedPrincipalID); err == nil {
			principal = p
		}
	}
	tenant := TenantContext{TenantID: req.TenantID}
	perms, roles, err := e.catalog.PermissionsFor(principal, tenant)
	if err != nil && !isRoleNotFound(err) {
		return Decision{}, err
	}
	if isRoleNotFound(err) {
		perms, roles = nil, nil
	}
	d := e.policy.Evaluate(evalInput{
		Principal:   principal,
		Tenant:      tenant,
		Permissions: perms,
		Roles:       roles,
		Action:      ActionPushDevice,
		Device:      &device,
	})
	if !d.Allowed {
		return d, nil
	}
	d.Reason = fmt.Sprintf("device %q trusted for tenant %q; %s", req.DeviceID, req.TenantID, d.Reason)
	return d, nil
}

// CanInstallModule implements PolicyEngine.
func (e *Engine) CanInstallModule(ctx context.Context, req ModuleInstallRequest) (Decision, error) {
	if err := requirePrincipalTenant(req.Principal, req.Tenant); err != nil {
		return Decision{}, err
	}
	if req.ModuleName == "" {
		return Decision{}, fmt.Errorf("%w: module name required", ErrInvalidConfig)
	}
	if d := e.checkTenantBinding(req.Principal, req.Tenant); !d.Allowed {
		return d, nil
	}
	perms, roles, err := e.catalog.PermissionsFor(req.Principal, req.Tenant)
	if err != nil {
		return Decision{}, err
	}
	// Module-declared permissions must be held by the installer (or empty).
	if d := checkRequiredPermissions(perms, req.RequiredPermissions); !d.Allowed {
		d.Reason = fmt.Sprintf("installer lacks module permissions: %s", d.Reason)
		return d, nil
	}
	return e.policy.Evaluate(evalInput{
		Principal:           req.Principal,
		Tenant:              req.Tenant,
		Permissions:         perms,
		Roles:               roles,
		Action:              ActionInstallModule,
		ModuleName:          req.ModuleName,
		ModuleVersion:       req.ModuleVersion,
		RequiredPermissions: req.RequiredPermissions,
	}), nil
}

// CheckPatientScope implements the patient-level access stub. A principal with
// an empty PatientScope is unrestricted. A scoped principal may only access the
// listed patient id.
func (e *Engine) CheckPatientScope(ctx context.Context, req PatientScopeRequest) (Decision, error) {
	if err := requirePrincipalTenant(req.Principal, req.Tenant); err != nil {
		return Decision{}, err
	}
	if req.PatientID == "" {
		return Decision{}, fmt.Errorf("%w: patient id required", ErrInvalidConfig)
	}
	if d := e.checkTenantBinding(req.Principal, req.Tenant); !d.Allowed {
		return d, nil
	}
	if d := e.checkPatientStub(req.Tenant, req.PatientID); !d.Allowed {
		return d, nil
	}
	perms, roles, err := e.catalog.PermissionsFor(req.Principal, req.Tenant)
	if err != nil {
		return Decision{}, err
	}
	return e.policy.Evaluate(evalInput{
		Principal:    req.Principal,
		Tenant:       req.Tenant,
		Permissions:  perms,
		Roles:        roles,
		Action:       ActionPatientAccess,
		PatientID:    req.PatientID,
		ResourceType: "Patient",
		ResourceID:   req.PatientID,
	}), nil
}

func (e *Engine) checkTenantBinding(p Principal, tenant TenantContext) Decision {
	if tenant.TenantID == "" {
		return Deny("tenant id required")
	}
	if len(tenant.RoleBindings) > 0 {
		return Allow("tenant context supplies role bindings")
	}
	for _, b := range p.TenantBindings {
		if b.TenantID == tenant.TenantID || b.TenantID == "*" {
			return Allow("principal bound to tenant")
		}
	}
	// Device and service principals may act without role bindings when policy
	// rules alone authorize (e.g. trusted device push uses device record).
	if p.Kind == KindDevice || p.Kind == KindService {
		return Allow("device/service principal")
	}
	return Deny(fmt.Sprintf("principal %q is not bound to tenant %q", p.ID, tenant.TenantID))
}

func (e *Engine) checkPatientStub(tenant TenantContext, patientID string) Decision {
	if patientID == "" || tenant.PatientScope == "" {
		return Allow("patient scope not constrained")
	}
	if tenant.PatientScope == patientID {
		return Allow(fmt.Sprintf("principal scoped to patient %q", patientID))
	}
	return Deny(fmt.Sprintf("principal scoped to patient %q cannot access %q", tenant.PatientScope, patientID))
}

func requirePrincipalTenant(p Principal, tenant TenantContext) error {
	if p.ID == "" {
		return fmt.Errorf("%w: principal id required", ErrInvalidConfig)
	}
	if p.Kind == "" {
		return fmt.Errorf("%w: principal kind required", ErrInvalidConfig)
	}
	if tenant.TenantID == "" {
		return fmt.Errorf("%w: tenant id required", ErrInvalidConfig)
	}
	return nil
}

func checkRequiredPermissions(have []Permission, required []string) Decision {
	if len(required) == 0 {
		return Allow("no required permissions")
	}
	missing := make([]string, 0)
	for _, r := range required {
		if !hasAnyPermission(have, []string{r}) {
			missing = append(missing, r)
		}
	}
	if len(missing) > 0 {
		d := Deny(fmt.Sprintf("missing required permissions: %v", missing))
		d.RequiredPermissions = append([]string(nil), required...)
		return d
	}
	d := Allow("required permissions satisfied")
	d.RequiredPermissions = append([]string(nil), required...)
	return d
}

// checkAnyRequiredPermission requires the principal to hold at least one of the
// declared permissions (view Authorizer semantics).
func checkAnyRequiredPermission(have []Permission, required []string) Decision {
	if len(required) == 0 {
		return Allow("no required permissions")
	}
	if hasAnyPermission(have, required) {
		d := Allow("holds at least one required permission")
		d.RequiredPermissions = append([]string(nil), required...)
		return d
	}
	d := Deny(fmt.Sprintf("missing any of required permissions: %v", required))
	d.RequiredPermissions = append([]string(nil), required...)
	return d
}

func patientIDFromSubject(id, resourceType string) string {
	if resourceType == "Patient" {
		return id
	}
	return ""
}

func isRoleNotFound(err error) bool {
	return errors.Is(err, ErrRoleNotFound)
}
