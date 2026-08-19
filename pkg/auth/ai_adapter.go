package auth

import (
	"context"
	"fmt"
	"net/url"
	"slices"

	"github.com/degoke/health-ai-stack/pkg/ai"
)

// AIConstraints holds optional AI-specific narrowing that stays in the AI layer
// (field/param allow-lists, de-identify, approval). Auth decides access; these
// constraints shape the ToolResult when access is allowed.
type AIConstraints struct {
	Read   map[string]ai.ReadTypePolicy
	Search map[string]ai.SearchTypePolicy
	Views  map[string]ai.ViewTypePolicy
	Write  map[string]ai.WriteTypePolicy
}

// AIPolicyAdapter adapts Engine to ai.PolicyEngine. Access is decided by auth;
// optional AIConstraints supply field/param narrowing and de-identify/approval
// flags.
type AIPolicyAdapter struct {
	Engine      *Engine
	Resolve     ActorResolver
	TenantID    string
	Constraints *AIConstraints
	// PatientSearchParams maps a searchable resource type to the FHIR search
	// parameter that scopes it to the resolved patient. Patient searches use
	// _id automatically. Other resource types are denied for a scoped subject
	// unless this map contains an explicit relationship parameter.
	PatientSearchParams map[string]string
	// PatientViewScope must inject/validate a patient scope parameter for views.
	// Scoped view calls are denied when it is nil because ViewDefinitions are
	// otherwise free to scan unrelated patients.
	PatientViewScope func(ctx context.Context, req ai.ViewPolicyRequest, patientID string) (map[string]any, error)
}

var _ ai.PolicyEngine = (*AIPolicyAdapter)(nil)
var _ ai.SearchCountPolicy = (*AIPolicyAdapter)(nil)
var _ ai.ViewDecisionPolicy = (*AIPolicyAdapter)(nil)
var _ ai.ViewDecisionPolicyContext = (*AIPolicyAdapter)(nil)
var _ ai.ViewScopePolicy = (*AIPolicyAdapter)(nil)

// CheckRead implements ai.PolicyEngine.
func (a *AIPolicyAdapter) CheckRead(ctx context.Context, req ai.ReadPolicyRequest) (*ai.ReadPolicyDecision, error) {
	principal, tenant, err := a.resolve(ctx, req.Actor, req.Subject)
	if err != nil {
		return nil, err
	}
	d, err := a.Engine.CanReadResource(ctx, ReadRequest{
		Principal:    principal,
		Tenant:       tenant,
		ResourceType: req.ResourceType,
		ID:           req.ID,
	})
	if err != nil {
		return nil, err
	}
	if !d.Allowed {
		return &ai.ReadPolicyDecision{Allowed: false}, nil
	}
	out := &ai.ReadPolicyDecision{Allowed: true}
	if a.Constraints != nil {
		if cfg, ok := a.Constraints.Read[req.ResourceType]; ok {
			out.AllowedFields = append([]string(nil), cfg.AllowedFields...)
			out.Deidentify = cfg.Deidentify
		}
	}
	return out, nil
}

// CheckSearch implements ai.PolicyEngine.
func (a *AIPolicyAdapter) CheckSearch(ctx context.Context, req ai.SearchPolicyRequest) (*ai.SearchPolicyDecision, error) {
	principal, tenant, err := a.resolve(ctx, req.Actor, req.Subject)
	if err != nil {
		return nil, err
	}
	// Search is treated as a read of the resource type for auth decisions.
	d, err := a.Engine.CanReadResource(ctx, ReadRequest{
		Principal:    principal,
		Tenant:       tenant,
		ResourceType: req.ResourceType,
	})
	if err != nil {
		return nil, err
	}
	if !d.Allowed {
		return &ai.SearchPolicyDecision{Allowed: false}, nil
	}

	out := &ai.SearchPolicyDecision{Allowed: true, Params: cloneValues(req.Params)}
	if a.Constraints == nil {
		return a.applyPatientSearchScope(out, req.ResourceType, tenant)
	}
	cfg, ok := a.Constraints.Search[req.ResourceType]
	if !ok {
		return &ai.SearchPolicyDecision{Allowed: false}, nil
	}
	allowed := make(map[string]struct{}, len(cfg.AllowedParams))
	for _, name := range cfg.AllowedParams {
		allowed[name] = struct{}{}
	}
	narrowed := url.Values{}
	for key, values := range req.Params {
		base := searchParamBase(key)
		if _, ok := allowed[base]; !ok {
			return &ai.SearchPolicyDecision{Allowed: false}, fmt.Errorf("%w: search parameter %q is not allowed", ai.ErrPolicyDenied, key)
		}
		if valueErr := ai.ValidateSearchParameterValues(cfg, key, values); valueErr != nil {
			return &ai.SearchPolicyDecision{Allowed: false}, valueErr
		}
		for _, v := range values {
			narrowed.Add(key, v)
		}
	}
	if len(narrowed) == 0 && len(req.Params) > 0 {
		return &ai.SearchPolicyDecision{Allowed: false}, nil
	}
	out = &ai.SearchPolicyDecision{
		Allowed:        true,
		Params:         narrowed,
		AllowedFields:  append([]string(nil), cfg.AllowedFields...),
		AllowAllFields: cfg.AllowAllFields,
		Deidentify:     cfg.Deidentify,
	}
	var scopeErr error
	out.Params, scopeErr = a.applyPatientSearchScopeToParams(narrowed, req.ResourceType, tenant)
	return out, scopeErr
}

// CheckView implements ai.PolicyEngine.
func (a *AIPolicyAdapter) CheckView(ctx context.Context, req ai.ViewPolicyRequest) error {
	_, err := a.checkViewDecision(ctx, req)
	return err
}

// CheckViewDecision returns the AI-layer view decision, combining auth access
// with optional AI-only de-identification hints.
func (a *AIPolicyAdapter) CheckViewDecision(req ai.ViewPolicyRequest) (*ai.ViewPolicyDecision, error) {
	return a.checkViewDecision(context.Background(), req)
}

// CheckViewDecisionContext is the context-aware view decision entrypoint used
// by ai.Executor so actor/subject resolution is preserved for de-identification.
func (a *AIPolicyAdapter) CheckViewDecisionContext(ctx context.Context, req ai.ViewPolicyRequest) (*ai.ViewPolicyDecision, error) {
	return a.checkViewDecision(ctx, req)
}

// ApplyViewScope enforces a fail-closed rule for patient-scoped principals.
func (a *AIPolicyAdapter) ApplyViewScope(ctx context.Context, req ai.ViewPolicyRequest) (*ai.ViewPolicyRequest, error) {
	_, tenant, err := a.resolve(ctx, req.Actor, req.Subject)
	if err != nil {
		return nil, err
	}
	if tenant.PatientScope == "" {
		return &req, nil
	}
	if a.PatientViewScope == nil {
		return nil, fmt.Errorf("%w: patient-scoped view execution requires a view scope enforcer", ai.ErrPolicyDenied)
	}
	params, err := a.PatientViewScope(ctx, req, tenant.PatientScope)
	if err != nil {
		return nil, err
	}
	req.Parameters = params
	return &req, nil
}

// MaxSearchCount implements ai.SearchCountPolicy.
func (a *AIPolicyAdapter) MaxSearchCount(resourceType string) (int, bool) {
	if a == nil || a.Constraints == nil {
		return 0, false
	}
	cfg, ok := a.Constraints.Search[resourceType]
	if !ok || cfg.MaxCount <= 0 {
		return 0, false
	}
	return cfg.MaxCount, true
}

func (a *AIPolicyAdapter) checkViewDecision(ctx context.Context, req ai.ViewPolicyRequest) (*ai.ViewPolicyDecision, error) {
	principal, tenant, err := a.resolve(ctx, req.Actor, req.Subject)
	if err != nil {
		return nil, err
	}
	d, err := a.Engine.CanExecuteView(ctx, ViewRequest{
		Principal:  principal,
		Tenant:     tenant,
		ViewName:   req.ViewName,
		Version:    req.Version,
		Parameters: req.Parameters,
	})
	if err != nil {
		return nil, err
	}
	if !d.Allowed {
		return nil, fmt.Errorf("%w: %s", ai.ErrPolicyDenied, d.Reason)
	}
	out := &ai.ViewPolicyDecision{Allowed: true}
	if a.Constraints == nil {
		return out, nil
	}
	if req.Version != "" {
		if cfg, ok := a.Constraints.Views[req.ViewName+"|"+req.Version]; ok {
			out.Deidentify = cfg.Deidentify
			out.MaxCount = cfg.MaxCount
			return out, nil
		}
	}
	if cfg, ok := a.Constraints.Views[req.ViewName]; ok {
		out.Deidentify = cfg.Deidentify
		out.MaxCount = cfg.MaxCount
	}
	return out, nil
}

// CheckWrite implements ai.PolicyEngine.
func (a *AIPolicyAdapter) CheckWrite(ctx context.Context, req ai.WritePolicyRequest) (*ai.WritePolicyDecision, error) {
	principal, tenant, err := a.resolve(ctx, req.Actor, req.Subject)
	if err != nil {
		return nil, err
	}
	d, err := a.Engine.CanWriteResource(ctx, WriteRequest{
		Principal:    principal,
		Tenant:       tenant,
		Operation:    req.Operation,
		ResourceType: req.ResourceType,
		ID:           req.ID,
	})
	if err != nil {
		return nil, err
	}
	if !d.Allowed {
		return &ai.WritePolicyDecision{Allowed: false}, nil
	}

	out := &ai.WritePolicyDecision{Allowed: true, RequiresApproval: d.RequiresApproval}
	if a.Constraints == nil {
		return out, nil
	}
	cfg, ok := a.Constraints.Write[req.ResourceType]
	if !ok {
		return &ai.WritePolicyDecision{Allowed: false}, nil
	}
	var allowedFields []string
	var requiresApproval bool
	switch req.Operation {
	case "create":
		allowedFields = append([]string(nil), cfg.CreateFields...)
		requiresApproval = cfg.CreateApproval
	case "update":
		allowedFields = append([]string(nil), cfg.UpdateFields...)
		requiresApproval = cfg.UpdateApproval
	default:
		return &ai.WritePolicyDecision{Allowed: false}, nil
	}
	if len(allowedFields) == 0 {
		return &ai.WritePolicyDecision{Allowed: false}, nil
	}
	for field := range req.Fields {
		if !slices.Contains(allowedFields, field) {
			return nil, fmt.Errorf("%w: field %q not allowed for %s %s", ai.ErrPolicyDenied, field, req.Operation, req.ResourceType)
		}
	}
	return &ai.WritePolicyDecision{
		Allowed:          true,
		AllowedFields:    allowedFields,
		RequiresApproval: d.RequiresApproval || requiresApproval,
	}, nil
}

func (a *AIPolicyAdapter) applyPatientSearchScope(out *ai.SearchPolicyDecision, resourceType string, tenant TenantContext) (*ai.SearchPolicyDecision, error) {
	if tenant.PatientScope == "" {
		return out, nil
	}
	var err error
	out.Params, err = a.applyPatientSearchScopeToParams(out.Params, resourceType, tenant)
	return out, err
}

func (a *AIPolicyAdapter) applyPatientSearchScopeToParams(params url.Values, resourceType string, tenant TenantContext) (url.Values, error) {
	if tenant.PatientScope == "" {
		return params, nil
	}
	if params == nil {
		params = url.Values{}
	}
	if resourceType == "Patient" {
		params.Set("_id", tenant.PatientScope)
		return params, nil
	}
	param := a.PatientSearchParams[resourceType]
	if param == "" {
		return nil, fmt.Errorf("%w: no patient search scope configured for %s", ai.ErrPolicyDenied, resourceType)
	}
	params.Set(param, "Patient/"+tenant.PatientScope)
	return params, nil
}

func (a *AIPolicyAdapter) resolve(ctx context.Context, actor, subject string) (Principal, TenantContext, error) {
	if a == nil || a.Engine == nil {
		return Principal{}, TenantContext{}, fmt.Errorf("%w: engine required", ErrInvalidConfig)
	}
	if a.Resolve == nil {
		return Principal{}, TenantContext{}, fmt.Errorf("%w", ErrMissingResolver)
	}
	principal, tenant, err := a.Resolve(ctx, actor, subject)
	if err != nil {
		return Principal{}, TenantContext{}, err
	}
	if tenant.TenantID == "" {
		tenant.TenantID = a.TenantID
	}
	return principal, tenant, nil
}

func searchParamBase(param string) string {
	for idx := 0; idx < len(param); idx++ {
		if param[idx] == ':' {
			return param[:idx]
		}
	}
	return param
}

func cloneValues(v url.Values) url.Values {
	if v == nil {
		return nil
	}
	out := make(url.Values, len(v))
	for k, vals := range v {
		out[k] = append([]string(nil), vals...)
	}
	return out
}
