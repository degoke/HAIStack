package authztest

import (
	"context"
	"fmt"
	"os"

	"github.com/degoke/health-ai-stack/pkg/auth"
	"github.com/degoke/health-ai-stack/pkg/smart"
	"gopkg.in/yaml.v3"
)

// YAMLFile is a machine-readable authorization scenario catalogue.
type YAMLFile struct {
	Version   string         `yaml:"version"`
	Scenarios []YAMLScenario `yaml:"scenarios"`
}

// YAMLScenario is one vendor-neutral policy × SMART-scope test case.
//
// ExpectedAllow is the intersection of SMART scope grants and pkg/auth
// policy allows. A case is allowed only when both layers permit the action.
type YAMLScenario struct {
	Name         string `yaml:"name"`
	Doc          string `yaml:"doc"`
	Principal    string `yaml:"principal"`
	Scopes       string `yaml:"scopes"`
	Policy       string `yaml:"policy"`
	Action       string `yaml:"action"`
	ResourceType string `yaml:"resourceType"`
	ResourceID   string `yaml:"resourceId"`
	ViewName     string `yaml:"viewName"`
	ToolName     string `yaml:"toolName"`
	PatientID    string `yaml:"patientId"`
	PatientScope string `yaml:"patientScope"`
	Tenant       string `yaml:"tenant"`
	ExpectAllow  bool   `yaml:"expectAllow"`
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
	return file, nil
}

// ScenariosFromYAML converts machine-readable cases into runnable Scenario values.
func ScenariosFromYAML(file YAMLFile) ([]Scenario, error) {
	out := make([]Scenario, 0, len(file.Scenarios))
	for i, spec := range file.Scenarios {
		if spec.Name == "" {
			return nil, fmt.Errorf("authztest: yaml scenario[%d] missing name", i)
		}
		spec := spec
		out = append(out, Scenario{
			Name: spec.Name,
			Doc:  spec.Doc,
			Run: func(ctx context.Context, kit *Kit) error {
				return runYAMLScenario(ctx, kit, spec)
			},
		})
	}
	return out, nil
}

func runYAMLScenario(ctx context.Context, kit *Kit, spec YAMLScenario) error {
	eng, err := engineForYAMLPolicy(spec.Policy)
	if err != nil {
		return fmt.Errorf("%s: %w", spec.Name, err)
	}
	adapter := kit.Adapter
	if adapter == nil {
		adapter = SmartAdapter()
	}

	bundle, err := bundleForYAML(adapter, spec)
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

func engineForYAMLPolicy(name string) (*auth.Engine, error) {
	cfg := BaseConfig()
	switch name {
	case "", "base":
		cfg.Policy = BasePolicy()
	case "observation-only":
		cfg.Roles = []auth.Role{{
			Name:        "clinician",
			Permissions: []auth.Permission{"*.read", "observation.read", "patient.read", "appointment.read"},
		}, {
			Name:        "tenant-admin",
			Permissions: []auth.Permission{"*.read", "module.install"},
		}, {
			Name:        "backend",
			Permissions: []auth.Permission{"*.read", "patient.read"},
		}}
		cfg.Policy = NarrowObservationPolicy()
	case "deny-first":
		cfg = NarrowEngineConfigWithDenyFirst()
	case "allow-all-read":
		cfg.Policy = allowAllReadPolicy()
	default:
		return nil, fmt.Errorf("unknown policy %q", name)
	}
	return auth.NewEngine(cfg)
}

func allowAllReadPolicy() *auth.PolicyDocument {
	return &auth.PolicyDocument{
		Version: "1",
		Rules: []auth.PolicyRule{
			{
				Name:   "allow-all-read",
				Effect: auth.EffectAllow,
				Match: auth.RuleMatch{
					Actions: []string{auth.ActionRead, auth.ActionWrite, auth.ActionExecuteView, auth.ActionExecuteAITool, auth.ActionPatientAccess},
				},
				Reason: "broad policy allows when scopes grant",
			},
		},
	}
}

func bundleForYAML(adapter *smart.AuthAdapter, spec YAMLScenario) (smart.AuthBundle, error) {
	scopesRaw := spec.Scopes
	if scopesRaw == "" {
		scopesRaw = "user/*.read"
	}
	scopes, err := smart.ParseScopes(scopesRaw)
	if err != nil {
		return smart.AuthBundle{}, err
	}
	subject := "user-clinician"
	clientID := ""
	switch spec.Principal {
	case "", "clinician":
		subject = "user-clinician"
	case "admin":
		subject = "user-admin"
	case "backend":
		subject = "backend-app"
		clientID = "backend-app"
	default:
		subject = spec.Principal
	}
	claims := smart.TokenClaims{
		Subject:  subject,
		ClientID: clientID,
		Scope:    scopes.SpaceSeparated(),
		Scopes:   scopes,
		Patient:  spec.PatientID,
	}
	if clientID != "" {
		return adapter.FromBackendService(claims, smart.BackendClient{
			ClientID:      clientID,
			AllowedScopes: []string{scopesRaw},
		}, smart.LaunchContext{})
	}
	return adapter.ToAuthRequests(claims, smart.BuildLaunchContext(smart.LaunchContextInput{
		Claims: &claims,
		Scopes: scopes,
	}))
}
