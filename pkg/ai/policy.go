package ai

import (
	"context"
	"fmt"
	"net/url"
	"slices"
)

// ReadPolicyRequest carries read authorization context.
type ReadPolicyRequest struct {
	Actor        string
	Subject      string
	ResourceType string
	ID           string
}

// ReadPolicyDecision is the outcome of a read policy check.
type ReadPolicyDecision struct {
	Allowed       bool
	AllowedFields []string
	Deidentify    bool
}

// SearchPolicyRequest carries search authorization context.
type SearchPolicyRequest struct {
	Actor        string
	Subject      string
	ResourceType string
	Params       url.Values
}

// SearchPolicyDecision is the outcome of a search policy check. Params may be
// narrowed by policy before execution.
type SearchPolicyDecision struct {
	Allowed        bool
	Params         url.Values
	AllowedFields  []string
	AllowAllFields bool
	Deidentify     bool
}

// ViewPolicyRequest carries view authorization context.
type ViewPolicyRequest struct {
	Actor      string
	Subject    string
	ViewName   string
	Version    string
	Parameters map[string]any
	Limit      int
	Offset     int
}

// WritePolicyRequest carries write authorization context.
type WritePolicyRequest struct {
	Actor        string
	Subject      string
	Operation    string
	ResourceType string
	ID           string
	Fields       map[string]any
}

// WritePolicyDecision is the outcome of a write policy check.
type WritePolicyDecision struct {
	Allowed          bool
	AllowedFields    []string
	RequiresApproval bool
	Deidentify       bool
}

// PolicyEngine is the central allow-list and safety layer for AI tool operations.
type PolicyEngine interface {
	CheckRead(ctx context.Context, req ReadPolicyRequest) (*ReadPolicyDecision, error)
	CheckSearch(ctx context.Context, req SearchPolicyRequest) (*SearchPolicyDecision, error)
	CheckView(ctx context.Context, req ViewPolicyRequest) error
	CheckWrite(ctx context.Context, req WritePolicyRequest) (*WritePolicyDecision, error)
}

// SearchCountPolicy optionally exposes per-resource search count caps.
type SearchCountPolicy interface {
	MaxSearchCount(resourceType string) (int, bool)
}

// ReadTypePolicy configures read access for one resource type.
type ReadTypePolicy struct {
	AllowedFields []string
	Deidentify    bool
}

// SearchTypePolicy configures search access for one resource type.
type SearchTypePolicy struct {
	AllowedParams      []string
	AllowedFields      []string
	AllowAllFields     bool
	AllowedIncludes    []string
	AllowedRevIncludes []string
	MaxCount           int
	Deidentify         bool
}

// ViewTypePolicy configures view access for one view name or name|version key.
type ViewTypePolicy struct {
	Deidentify bool
	MaxCount   int
}

// WriteTypePolicy configures write access for one resource type.
type WriteTypePolicy struct {
	CreateFields   []string
	UpdateFields   []string
	CreateApproval bool
	UpdateApproval bool
}

// AllowListPolicy is a v1 policy engine backed by explicit allow-lists. When a
// resource type or view is not listed, access is denied.
type AllowListPolicy struct {
	Read   map[string]ReadTypePolicy
	Search map[string]SearchTypePolicy
	Views  map[string]ViewTypePolicy
	Write  map[string]WriteTypePolicy
}

// NewAllowListPolicy returns an empty allow-list policy. All operations are
// denied until entries are added.
func NewAllowListPolicy() *AllowListPolicy {
	return &AllowListPolicy{
		Read:   make(map[string]ReadTypePolicy),
		Search: make(map[string]SearchTypePolicy),
		Views:  make(map[string]ViewTypePolicy),
		Write:  make(map[string]WriteTypePolicy),
	}
}

// CheckRead implements PolicyEngine.
func (p *AllowListPolicy) CheckRead(_ context.Context, req ReadPolicyRequest) (*ReadPolicyDecision, error) {
	cfg, ok := p.Read[req.ResourceType]
	if !ok {
		return &ReadPolicyDecision{Allowed: false}, nil
	}
	return &ReadPolicyDecision{
		Allowed:       true,
		AllowedFields: append([]string(nil), cfg.AllowedFields...),
		Deidentify:    cfg.Deidentify,
	}, nil
}

// CheckSearch implements PolicyEngine. Every supplied parameter must be on the
// allow-list; silently dropping a parameter could broaden a clinical query.
func (p *AllowListPolicy) CheckSearch(_ context.Context, req SearchPolicyRequest) (*SearchPolicyDecision, error) {
	cfg, ok := p.Search[req.ResourceType]
	if !ok {
		return &SearchPolicyDecision{Allowed: false}, nil
	}
	allowed := make(map[string]struct{}, len(cfg.AllowedParams))
	for _, name := range cfg.AllowedParams {
		allowed[name] = struct{}{}
	}

	narrowed := url.Values{}
	for key, values := range req.Params {
		base := paramBaseName(key)
		if _, ok := allowed[base]; !ok {
			return &SearchPolicyDecision{Allowed: false}, fmt.Errorf("%w: search parameter %q is not allowed", ErrPolicyDenied, key)
		}
		if err := ValidateSearchParameterValues(cfg, key, values); err != nil {
			return &SearchPolicyDecision{Allowed: false}, err
		}
		for _, v := range values {
			narrowed.Add(key, v)
		}
	}
	return &SearchPolicyDecision{
		Allowed:        true,
		Params:         narrowed,
		AllowedFields:  append([]string(nil), cfg.AllowedFields...),
		AllowAllFields: cfg.AllowAllFields,
		Deidentify:     cfg.Deidentify,
	}, nil
}

// ValidateSearchParameterValues validates value-sensitive search controls.
// Include directives are intentionally allow-listed by their exact target
// declaration rather than by the broad parameter name alone.
func ValidateSearchParameterValues(cfg SearchTypePolicy, key string, values []string) error {
	base := paramBaseName(key)
	var allowed []string
	switch base {
	case "_include":
		allowed = cfg.AllowedIncludes
	case "_revinclude":
		allowed = cfg.AllowedRevIncludes
	default:
		return nil
	}
	if len(allowed) == 0 {
		return fmt.Errorf("%w: %s requires explicit directive allow-list", ErrPolicyDenied, base)
	}
	for _, value := range values {
		if !slices.Contains(allowed, value) {
			return fmt.Errorf("%w: %s directive %q is not allowed", ErrPolicyDenied, base, value)
		}
	}
	return nil
}

// ViewPolicyDecision is the outcome of a view policy check.
type ViewPolicyDecision struct {
	Allowed    bool
	Deidentify bool
	MaxCount   int
}

// ViewDecisionPolicy optionally exposes structured view decisions to callers
// that need AI-layer de-identification hints.
type ViewDecisionPolicy interface {
	CheckViewDecision(req ViewPolicyRequest) (*ViewPolicyDecision, error)
}

// ViewDecisionPolicyContext is the context-aware variant used by the executor
// when policy decisions depend on the actor or subject.
type ViewDecisionPolicyContext interface {
	CheckViewDecisionContext(ctx context.Context, req ViewPolicyRequest) (*ViewPolicyDecision, error)
}

// ViewScopePolicy optionally narrows view parameters for a subject-scoped
// request. Implementations must ensure the registered view consumes the
// injected scope parameter before allowing execution.
type ViewScopePolicy interface {
	ApplyViewScope(ctx context.Context, req ViewPolicyRequest) (*ViewPolicyRequest, error)
}

// ViewCountPolicy optionally exposes a maximum row count for AI view calls.
type ViewCountPolicy interface {
	MaxViewCount(viewName, version string) (int, bool)
}

// CheckView implements PolicyEngine.
func (p *AllowListPolicy) CheckView(_ context.Context, req ViewPolicyRequest) error {
	_, err := p.CheckViewDecision(req)
	return err
}

// CheckViewDecision returns the view policy decision for callers that need
// de-identification flags.
func (p *AllowListPolicy) CheckViewDecision(req ViewPolicyRequest) (*ViewPolicyDecision, error) {
	if req.ViewName == "" {
		return nil, fmt.Errorf("%w: view name required", ErrPolicyDenied)
	}
	if req.Version != "" {
		if cfg, ok := p.Views[req.ViewName+"|"+req.Version]; ok {
			return &ViewPolicyDecision{Allowed: true, Deidentify: cfg.Deidentify, MaxCount: cfg.MaxCount}, nil
		}
	}
	if cfg, ok := p.Views[req.ViewName]; ok {
		return &ViewPolicyDecision{Allowed: true, Deidentify: cfg.Deidentify, MaxCount: cfg.MaxCount}, nil
	}
	return nil, fmt.Errorf("%w: view %q not allowed", ErrPolicyDenied, req.ViewName)
}

// MaxViewCount implements ViewCountPolicy. A zero value means the executor's
// safe default is used.
func (p *AllowListPolicy) MaxViewCount(viewName, version string) (int, bool) {
	if p == nil {
		return 0, false
	}
	if version != "" {
		if cfg, ok := p.Views[viewName+"|"+version]; ok && cfg.MaxCount > 0 {
			return cfg.MaxCount, true
		}
	}
	if cfg, ok := p.Views[viewName]; ok && cfg.MaxCount > 0 {
		return cfg.MaxCount, true
	}
	return 0, false
}

// MaxSearchCount implements SearchCountPolicy.
func (p *AllowListPolicy) MaxSearchCount(resourceType string) (int, bool) {
	cfg, ok := p.Search[resourceType]
	if !ok || cfg.MaxCount <= 0 {
		return 0, false
	}
	return cfg.MaxCount, true
}

// CheckWrite implements PolicyEngine.
func (p *AllowListPolicy) CheckWrite(_ context.Context, req WritePolicyRequest) (*WritePolicyDecision, error) {
	cfg, ok := p.Write[req.ResourceType]
	if !ok {
		return &WritePolicyDecision{Allowed: false}, nil
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
		return &WritePolicyDecision{Allowed: false}, nil
	}
	if len(allowedFields) == 0 {
		return &WritePolicyDecision{Allowed: false}, nil
	}
	for field := range req.Fields {
		if !slices.Contains(allowedFields, field) {
			return nil, fmt.Errorf("%w: field %q not allowed for %s %s", ErrPolicyDenied, field, req.Operation, req.ResourceType)
		}
	}
	return &WritePolicyDecision{
		Allowed:          true,
		AllowedFields:    allowedFields,
		RequiresApproval: requiresApproval,
	}, nil
}

func paramBaseName(param string) string {
	if i := len(param); i > 0 {
		for idx := 0; idx < len(param); idx++ {
			if param[idx] == ':' {
				return param[:idx]
			}
		}
	}
	return param
}
