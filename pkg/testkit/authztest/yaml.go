package authztest

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/degoke/health-ai-stack/pkg/auth"
	"github.com/degoke/health-ai-stack/pkg/smart"
	"gopkg.in/yaml.v3"
)

// YAMLFile is a machine-readable authorization scenario catalogue.
type YAMLFile struct {
	Version          string                                  `yaml:"version"`
	Roles            []auth.Role                             `yaml:"roles"`
	Principals       map[string]YAMLPrincipal                `yaml:"principals"`
	PolicyRoleGrants map[string]map[string][]auth.Permission `yaml:"policyRoleGrants"`
	Policies         map[string]auth.PolicyDocument          `yaml:"policies"`
	Scenarios        []YAMLScenario                          `yaml:"scenarios"`
}

// YAMLPrincipal is a SMART identity declared in the catalogue (not a Go fixture).
type YAMLPrincipal struct {
	ID       string   `yaml:"id"`
	Kind     string   `yaml:"kind"`
	ClientID string   `yaml:"clientId"`
	Tenant   string   `yaml:"tenant"`
	Roles    []string `yaml:"roles"`
}

// YAMLScenario is one vendor-neutral policy × SMART-scope test case.
//
// ExpectAllow is the intersection of SMART scope grants and pkg/auth
// policy allows. A case is allowed only when both layers permit the action.
//
// Policy names a document in YAMLFile.Policies. PolicyDocument embeds a
// portable pkg/auth policy inline. Named Go policy enums are not used.
// Principal names YAMLFile.Principals. RoleGrants overlay extra permissions
// for this scenario only (in addition to PolicyRoleGrants).
type YAMLScenario struct {
	Name           string                       `yaml:"name"`
	Doc            string                       `yaml:"doc"`
	Principal      string                       `yaml:"principal"`
	Scopes         string                       `yaml:"scopes"`
	Policy         string                       `yaml:"policy"`
	PolicyDocument *auth.PolicyDocument         `yaml:"policyDocument"`
	RoleGrants     map[string][]auth.Permission `yaml:"roleGrants"`
	Action         string                       `yaml:"action"`
	ResourceType   string                       `yaml:"resourceType"`
	ResourceID     string                       `yaml:"resourceId"`
	ViewName       string                       `yaml:"viewName"`
	ToolName       string                       `yaml:"toolName"`
	PatientID      string                       `yaml:"patientId"`
	PatientScope   string                       `yaml:"patientScope"`
	Tenant         string                       `yaml:"tenant"`
	ExpectAllow    bool                         `yaml:"expectAllow"`
}

// LoadYAMLFile reads a YAML scenario catalogue from disk.
func LoadYAMLFile(path string) (YAMLFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return YAMLFile{}, err
	}
	return ParseYAML(data)
}

// ParseYAML unmarshals a YAML scenario catalogue.
func ParseYAML(data []byte) (YAMLFile, error) {
	var file YAMLFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return YAMLFile{}, fmt.Errorf("authztest: parse yaml: %w", err)
	}
	if len(file.Scenarios) == 0 {
		return YAMLFile{}, fmt.Errorf("authztest: yaml catalogue has no scenarios")
	}
	if len(file.Principals) == 0 {
		return YAMLFile{}, fmt.Errorf("authztest: yaml catalogue has no principals")
	}
	return file, nil
}

// ScenariosFromYAML converts machine-readable cases into runnable Scenario values.
func ScenariosFromYAML(file YAMLFile) ([]Scenario, error) {
	catalogPrincipals, err := principalsFromYAML(file)
	if err != nil {
		return nil, err
	}
	out := make([]Scenario, 0, len(file.Scenarios))
	for i, spec := range file.Scenarios {
		if spec.Name == "" {
			return nil, fmt.Errorf("authztest: yaml scenario[%d] missing name", i)
		}
		doc, err := resolveYAMLPolicy(file, spec)
		if err != nil {
			return nil, fmt.Errorf("authztest: yaml scenario %s: %w", spec.Name, err)
		}
		if _, err := resolveYAMLPrincipal(file, spec); err != nil {
			return nil, fmt.Errorf("authztest: yaml scenario %s: %w", spec.Name, err)
		}
		spec := spec
		policy := doc
		roles := resolveYAMLRoles(file, spec)
		principals := catalogPrincipals
		byName := file.Principals
		out = append(out, Scenario{
			Name: spec.Name,
			Doc:  spec.Doc,
			Run: func(ctx context.Context, _ *Kit) error {
				return runYAMLScenario(ctx, spec, policy, roles, principals, byName)
			},
		})
	}
	return out, nil
}

func runYAMLScenario(ctx context.Context, spec YAMLScenario, policy auth.PolicyDocument, roles []auth.Role, principals []auth.Principal, byName map[string]YAMLPrincipal) error {
	eng, err := engineForYAMLPolicy(policy, roles, principals)
	if err != nil {
		return fmt.Errorf("%s: %w", spec.Name, err)
	}
	adapter, bundle, err := bundleForYAML(spec, byName)
	if err != nil {
		return fmt.Errorf("%s: %w", spec.Name, err)
	}

	scopeOK, policyDecision, err := evaluateYAMLIntersection(ctx, eng, adapter, bundle, spec)
	if err != nil {
		return fmt.Errorf("%s: %w", spec.Name, err)
	}
	allowed := scopeOK && policyDecision.Allowed
	reason := fmt.Sprintf("scope=%v policy=%v (%s)", scopeOK, policyDecision.Allowed, policyDecision.Reason)
	if spec.ExpectAllow && !allowed {
		return fmt.Errorf("%s: expected allow, got deny (%s)", spec.Name, reason)
	}
	if !spec.ExpectAllow && allowed {
		return fmt.Errorf("%s: expected deny, got allow (%s)", spec.Name, reason)
	}
	return nil
}

func evaluateYAMLIntersection(ctx context.Context, eng *auth.Engine, adapter *smart.AuthAdapter, bundle smart.AuthBundle, spec YAMLScenario) (scopeOK bool, decision auth.Decision, err error) {
	action := spec.Action
	if action == "" {
		action = auth.ActionRead
	}
	switch action {
	case auth.ActionRead, "search":
		scopeOK = adapter.ScopeImplies(bundle, spec.ResourceType, smart.VerbRead)
		req := adapter.ToReadRequest(bundle, spec.ResourceType, spec.ResourceID)
		req.Tenant = overlayTenant(req.Tenant, spec)
		decision, err = eng.CanReadResource(ctx, req)
		return scopeOK, decision, err
	case auth.ActionWrite:
		scopeOK = adapter.ScopeImplies(bundle, spec.ResourceType, smart.VerbWrite)
		req := adapter.ToWriteRequest(bundle, "update", spec.ResourceType, spec.ResourceID)
		req.Tenant = overlayTenant(req.Tenant, spec)
		decision, err = eng.CanWriteResource(ctx, req)
		return scopeOK, decision, err
	case auth.ActionExecuteView:
		scopeOK = adapter.ScopeImplies(bundle, spec.ResourceType, smart.VerbRead)
		decision, err = eng.CanExecuteView(ctx, auth.ViewRequest{
			Principal:    bundle.Principal,
			Tenant:       overlayTenant(bundle.Tenant, spec),
			ViewName:     spec.ViewName,
			ResourceType: spec.ResourceType,
		})
		return scopeOK, decision, err
	case auth.ActionExecuteAITool:
		scopeOK = adapter.ScopeImplies(bundle, spec.ResourceType, smart.VerbRead)
		decision, err = eng.CanExecuteAITool(ctx, auth.AIToolRequest{
			Principal:    bundle.Principal,
			Tenant:       overlayTenant(bundle.Tenant, spec),
			ToolName:     spec.ToolName,
			ResourceType: spec.ResourceType,
			ViewName:     spec.ViewName,
		})
		return scopeOK, decision, err
	case auth.ActionPatientAccess:
		scopeOK = true
		req := adapter.ToPatientScopeRequest(bundle, spec.PatientID)
		req.Tenant = overlayTenant(req.Tenant, spec)
		decision, err = eng.CheckPatientScope(ctx, req)
		return scopeOK, decision, err
	default:
		return false, auth.Decision{}, fmt.Errorf("unsupported action %q", action)
	}
}

func overlayTenant(tenant auth.TenantContext, spec YAMLScenario) auth.TenantContext {
	if spec.PatientScope != "" {
		tenant.PatientScope = spec.PatientScope
	}
	if spec.Tenant != "" {
		tenant.TenantID = spec.Tenant
		// Drop adapter-supplied role bindings so tenant-binding is evaluated
		// against the principal catalog (cross-tenant deny).
		tenant.RoleBindings = nil
	}
	return tenant
}

func resolveYAMLPolicy(file YAMLFile, spec YAMLScenario) (auth.PolicyDocument, error) {
	if spec.PolicyDocument != nil {
		return *spec.PolicyDocument, nil
	}
	name := spec.Policy
	if name == "" {
		name = "base"
	}
	doc, ok := file.Policies[name]
	if !ok {
		return auth.PolicyDocument{}, fmt.Errorf("unknown policy %q (declare it under policies:)", name)
	}
	return doc, nil
}

func resolveYAMLPrincipal(file YAMLFile, spec YAMLScenario) (YAMLPrincipal, error) {
	name := spec.Principal
	if name == "" {
		name = "clinician"
	}
	p, ok := file.Principals[name]
	if !ok {
		return YAMLPrincipal{}, fmt.Errorf("unknown principal %q (declare it under principals:)", name)
	}
	if p.ID == "" {
		return YAMLPrincipal{}, fmt.Errorf("principal %q missing id", name)
	}
	return p, nil
}

func resolveYAMLRoles(file YAMLFile, spec YAMLScenario) []auth.Role {
	roles := cloneYAMLRoles(file.Roles)
	policyName := spec.Policy
	if spec.PolicyDocument == nil && policyName == "" {
		policyName = "base"
	}
	if policyName != "" {
		if grants, ok := file.PolicyRoleGrants[policyName]; ok {
			roles = applyRoleGrants(roles, grants)
		}
	}
	if len(spec.RoleGrants) > 0 {
		roles = applyRoleGrants(roles, spec.RoleGrants)
	}
	return roles
}

func cloneYAMLRoles(roles []auth.Role) []auth.Role {
	out := make([]auth.Role, len(roles))
	for i, r := range roles {
		out[i] = auth.Role{
			Name:        r.Name,
			Permissions: append([]auth.Permission(nil), r.Permissions...),
		}
	}
	return out
}

func applyRoleGrants(roles []auth.Role, grants map[string][]auth.Permission) []auth.Role {
	if len(grants) == 0 {
		return roles
	}
	out := cloneYAMLRoles(roles)
	for i := range out {
		extra, ok := grants[out[i].Name]
		if !ok || len(extra) == 0 {
			continue
		}
		out[i].Permissions = append(out[i].Permissions, extra...)
	}
	return out
}

func principalsFromYAML(file YAMLFile) ([]auth.Principal, error) {
	if len(file.Principals) == 0 {
		return nil, fmt.Errorf("authztest: yaml catalogue has no principals")
	}
	out := make([]auth.Principal, 0, len(file.Principals))
	for name, p := range file.Principals {
		if p.ID == "" {
			return nil, fmt.Errorf("authztest: principal %q missing id", name)
		}
		kind, err := parseYAMLPrincipalKind(p.Kind)
		if err != nil {
			return nil, fmt.Errorf("authztest: principal %q: %w", name, err)
		}
		tenant := p.Tenant
		if tenant == "" {
			tenant = TenantA
		}
		roles := append([]string(nil), p.Roles...)
		if len(roles) == 0 {
			return nil, fmt.Errorf("authztest: principal %q missing roles", name)
		}
		out = append(out, auth.Principal{
			ID:   p.ID,
			Kind: kind,
			TenantBindings: []auth.TenantBinding{{
				TenantID: tenant,
				Roles:    roles,
			}},
		})
	}
	return out, nil
}

func parseYAMLPrincipalKind(kind string) (auth.PrincipalKind, error) {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "", "user":
		return auth.KindUser, nil
	case "service":
		return auth.KindService, nil
	case "device":
		return auth.KindDevice, nil
	case "ai-agent", "ai":
		return auth.KindAIAgent, nil
	default:
		return "", fmt.Errorf("unknown kind %q", kind)
	}
}

func engineForYAMLPolicy(doc auth.PolicyDocument, roles []auth.Role, principals []auth.Principal) (*auth.Engine, error) {
	if len(roles) == 0 {
		return nil, fmt.Errorf("yaml catalogue has no roles")
	}
	if len(principals) == 0 {
		return nil, fmt.Errorf("yaml catalogue has no principals")
	}
	return auth.NewEngine(auth.Config{
		Roles:      roles,
		Principals: principals,
		Policy:     &doc,
	})
}

func adapterForYAMLPrincipal(p YAMLPrincipal) (*smart.AuthAdapter, error) {
	kind, err := parseYAMLPrincipalKind(p.Kind)
	if err != nil {
		return nil, err
	}
	tenant := p.Tenant
	if tenant == "" {
		tenant = TenantA
	}
	roles := append([]string(nil), p.Roles...)
	if len(roles) == 0 {
		return nil, fmt.Errorf("principal %q missing roles", p.ID)
	}
	cfg := smart.AuthAdapterConfig{DefaultTenantID: tenant}
	if kind == auth.KindService {
		cfg.DefaultServiceRoles = roles
	} else {
		cfg.DefaultUserRoles = roles
	}
	return smart.NewAuthAdapter(cfg), nil
}

func bundleForYAML(spec YAMLScenario, byName map[string]YAMLPrincipal) (*smart.AuthAdapter, smart.AuthBundle, error) {
	p, err := resolveYAMLPrincipal(YAMLFile{Principals: byName}, spec)
	if err != nil {
		return nil, smart.AuthBundle{}, err
	}
	adapter, err := adapterForYAMLPrincipal(p)
	if err != nil {
		return nil, smart.AuthBundle{}, err
	}
	scopesRaw := spec.Scopes
	if scopesRaw == "" {
		scopesRaw = "user/*.read"
	}
	scopes, err := smart.ParseScopes(scopesRaw)
	if err != nil {
		return nil, smart.AuthBundle{}, err
	}
	subject := p.ID
	clientID := p.ClientID
	kind, err := parseYAMLPrincipalKind(p.Kind)
	if err != nil {
		return nil, smart.AuthBundle{}, err
	}
	tenant := p.Tenant
	if tenant == "" {
		tenant = TenantA
	}
	if kind == auth.KindService && clientID == "" {
		clientID = subject
	}
	claims := smart.TokenClaims{
		Subject:    subject,
		ClientID:   clientID,
		TenantHint: tenant,
		Scope:      scopes.SpaceSeparated(),
		Scopes:     scopes,
		Patient:    spec.PatientID,
	}
	if clientID != "" && kind == auth.KindService {
		bundle, err := adapter.FromBackendService(claims, smart.BackendClient{
			ClientID:      clientID,
			TenantHint:    tenant,
			AllowedScopes: []string{scopesRaw},
		}, smart.LaunchContext{})
		return adapter, bundle, err
	}
	bundle, err := adapter.ToAuthRequests(claims, smart.BuildLaunchContext(smart.LaunchContextInput{
		Claims: &claims,
		Scopes: scopes,
	}))
	return adapter, bundle, err
}
