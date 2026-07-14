package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/degoke/health-ai-stack/pkg/core"
	"github.com/degoke/health-ai-stack/pkg/search"
	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/types"
	"github.com/degoke/health-ai-stack/pkg/validate"
	"github.com/degoke/health-ai-stack/pkg/view"
)

// Config configures an Executor. Policy is required; backing services are
// optional but required for their respective tools.
type Config struct {
	Resources   store.ResourceStore
	Search      *search.Service
	Views       *view.Executor
	Core        *core.ResourceService
	Validator   validate.Engine
	Policy      PolicyEngine
	Registry    *Registry
	Audit       AuditLogger
	Approval    ApprovalHook
	Deidentify  Deidentifier
	ModelRouter *ModelRouter
	Citations   *CitationBuilder
	Formatter   *ContextFormatter
	Now         func() time.Time
}

// Executor validates requests, enforces policy, invokes backing packages, builds
// citations, and emits audit records.
type Executor struct {
	cfg Config
}

// NewExecutor validates configuration and returns an Executor.
func NewExecutor(cfg Config) (*Executor, error) {
	if cfg.Policy == nil {
		return nil, ErrMissingPolicy
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Deidentify == nil {
		cfg.Deidentify = PassThroughDeidentifier{}
	}
	if cfg.Citations == nil {
		cfg.Citations = NewCitationBuilder()
	}
	if cfg.Formatter == nil {
		cfg.Formatter = NewContextFormatter()
	}
	if cfg.Registry == nil {
		cfg.Registry = NewRegistry()
	}
	return &Executor{cfg: cfg}, nil
}

// InvokeModel routes an optional model invocation using the configured ModelRouter.
// Tool execution does not require a model adapter; this helper is for callers that
// want to combine tool output with model generation in the same session.
func (e *Executor) InvokeModel(ctx context.Context, req ToolRequest, prompt, context string) (*ModelResponse, error) {
	if e.cfg.ModelRouter == nil {
		return nil, nil
	}
	return e.cfg.ModelRouter.Invoke(ctx, ModelRequest{
		Hint:    req.ModelHint,
		Prompt:  prompt,
		Context: context,
		Tools:   toolNames(e.cfg.Registry),
	})
}

func toolNames(reg *Registry) []string {
	names := make([]string, 0, len(GenericToolDescriptors())+len(reg.List()))
	for _, desc := range GenericToolDescriptors() {
		names = append(names, desc.Name)
	}
	for _, spec := range reg.List() {
		names = append(names, spec.Name)
	}
	return names
}

// ExecuteTool validates a request, enforces policy, runs the backing operation,
// and returns structured output with citations and audit metadata.
func (e *Executor) ExecuteTool(ctx context.Context, req ToolRequest) (*ToolResult, error) {
	start := e.cfg.Now()
	toolName := req.ToolName
	input := req.Input
	if input == nil {
		input = map[string]any{}
	}

	if spec, err := e.cfg.Registry.Resolve(toolName); err == nil {
		mapped, mapErr := spec.MapInput(input)
		if mapErr != nil {
			_ = e.logAudit(ctx, req, toolName, "error", map[string]string{"error": mapErr.Error()})
			return nil, mapErr
		}
		toolName = spec.Delegate
		input = mapped
	} else if !IsGeneric(toolName) {
		_ = e.logAudit(ctx, req, req.ToolName, "error", map[string]string{"error": err.Error()})
		return nil, err
	}

	var (
		data      any
		citations []Citation
		outcome   string
		approval  bool
		redactions []string
		err       error
	)

	switch toolName {
	case ToolReadFhirResource:
		data, citations, outcome, redactions, err = e.execRead(ctx, req, input)
	case ToolSearchFhirResources:
		data, citations, outcome, redactions, err = e.execSearch(ctx, req, input)
	case ToolRunView:
		data, citations, outcome, redactions, err = e.execView(ctx, req, input)
	case ToolWriteFhirResource:
		data, citations, outcome, approval, redactions, err = e.execWrite(ctx, req, input)
	default:
		err = fmt.Errorf("%w: %s", ErrToolNotFound, req.ToolName)
	}

	if err != nil {
		_ = e.logAudit(ctx, req, req.ToolName, outcomeForError(err), map[string]string{"error": err.Error()})
		return nil, err
	}

	ctxText, fmtErr := e.cfg.Formatter.Format(data)
	if fmtErr != nil {
		_ = e.logAudit(ctx, req, req.ToolName, "error", map[string]string{"error": fmtErr.Error()})
		return nil, fmtErr
	}

	if approval {
		auditOutcome := "approval-required"
		_ = e.logAudit(ctx, req, req.ToolName, auditOutcome, auditDetailsForTool(toolName, input, data))
		outcome = auditOutcome
	} else {
		_ = e.logAudit(ctx, req, req.ToolName, outcome, auditDetailsForTool(toolName, input, data))
	}

	return &ToolResult{
		ToolName:         req.ToolName,
		Data:             data,
		Context:          ctxText,
		Citations:        citations,
		AuditMeta:        AuditMeta{ExecutedAt: e.cfg.Now(), Duration: e.cfg.Now().Sub(start), Outcome: outcome},
		ApprovalRequired: approval,
		Redactions:       redactions,
	}, nil
}

func (e *Executor) execRead(ctx context.Context, req ToolRequest, input map[string]any) (any, []Citation, string, []string, error) {
	if e.cfg.Resources == nil {
		return nil, nil, "", nil, fmt.Errorf("%w: resource store required for read", ErrMissingDependency)
	}
	parsed, err := parseReadInput(input)
	if err != nil {
		return nil, nil, "", nil, err
	}

	decision, err := e.cfg.Policy.CheckRead(ctx, ReadPolicyRequest{
		Actor: req.Actor, Subject: req.Subject,
		ResourceType: parsed.ResourceType, ID: parsed.ID,
	})
	if err != nil {
		return nil, nil, "", nil, err
	}
	if !decision.Allowed {
		return nil, nil, "", nil, fmt.Errorf("%w: read %s/%s", ErrPolicyDenied, parsed.ResourceType, parsed.ID)
	}

	env, err := e.cfg.Resources.Read(ctx, parsed.ResourceType, parsed.ID)
	if err != nil {
		return nil, nil, "", nil, err
	}

	filtered, err := filterResourceJSON(env.JSON, decision.AllowedFields)
	if err != nil {
		return nil, nil, "", nil, err
	}

	var data any = filtered
	var redactions []string
	if decision.Deidentify {
		data, redactions, err = e.cfg.Deidentify.Deidentify(ctx, DeidentifyRequest{
			ToolName: ToolReadFhirResource, ResourceType: parsed.ResourceType, Data: filtered,
		})
		if err != nil {
			return nil, nil, "", nil, err
		}
	}

	citations := []Citation{e.cfg.Citations.ResourceRef(parsed.ResourceType, parsed.ID)}
	return data, citations, "success", redactions, nil
}

func (e *Executor) execSearch(ctx context.Context, req ToolRequest, input map[string]any) (any, []Citation, string, []string, error) {
	if e.cfg.Search == nil {
		return nil, nil, "", nil, fmt.Errorf("%w: search service required", ErrMissingDependency)
	}
	parsed, err := parseSearchInput(input)
	if err != nil {
		return nil, nil, "", nil, err
	}

	params := url.Values{}
	for k, vals := range parsed.Params {
		for _, v := range vals {
			params.Add(k, v)
		}
	}

	decision, err := e.cfg.Policy.CheckSearch(ctx, SearchPolicyRequest{
		Actor: req.Actor, Subject: req.Subject,
		ResourceType: parsed.ResourceType, Params: params,
	})
	if err != nil {
		return nil, nil, "", nil, err
	}
	if !decision.Allowed {
		return nil, nil, "", nil, fmt.Errorf("%w: search %s", ErrPolicyDenied, parsed.ResourceType)
	}
	if len(params) > 0 && len(decision.Params) == 0 {
		return nil, nil, "", nil, fmt.Errorf("%w: no allowed search params remain", ErrPolicyDenied)
	}
	if len(decision.Params) > 0 {
		params = decision.Params
	}
	count := parsed.Count
	if cfg, ok := policyMaxCount(e.cfg.Policy, parsed.ResourceType); ok && count > cfg {
		count = cfg
	}
	if count <= 0 {
		count = defaultSearchCount(e.cfg.Policy, parsed.ResourceType)
	}
	if count > 0 {
		params.Set("_count", fmt.Sprintf("%d", count))
	}
	if parsed.Offset > 0 {
		params.Set("_offset", fmt.Sprintf("%d", parsed.Offset))
	}

	result, err := e.cfg.Search.Search(ctx, parsed.ResourceType, params)
	if err != nil {
		return nil, nil, "", nil, err
	}

	resources := make([]map[string]any, 0, len(result.Resources))
	for _, res := range result.Resources {
		item, err := filterResourceJSON(res.JSON, nil)
		if err != nil {
			return nil, nil, "", nil, err
		}
		resources = append(resources, item)
	}

	var data any = map[string]any{
		"resourceType": parsed.ResourceType,
		"total":        result.Total,
		"count":        result.Count,
		"resources":    resources,
	}
	var redactions []string
	if decision.Deidentify {
		data, redactions, err = e.cfg.Deidentify.Deidentify(ctx, DeidentifyRequest{
			ToolName: ToolSearchFhirResources, ResourceType: parsed.ResourceType, Data: data,
		})
		if err != nil {
			return nil, nil, "", nil, err
		}
	}

	citations := e.cfg.Citations.SearchCitations(parsed.ResourceType, params, result.Resources)
	return data, citations, "success", redactions, nil
}

func (e *Executor) execView(ctx context.Context, req ToolRequest, input map[string]any) (any, []Citation, string, []string, error) {
	if e.cfg.Views == nil {
		return nil, nil, "", nil, fmt.Errorf("%w: view executor required", ErrMissingDependency)
	}
	parsed, err := parseViewInput(input)
	if err != nil {
		return nil, nil, "", nil, err
	}

	if err := e.cfg.Policy.CheckView(ctx, ViewPolicyRequest{
		Actor: req.Actor, Subject: req.Subject,
		ViewName: parsed.ViewName, Version: parsed.Version, Parameters: parsed.Parameters,
	}); err != nil {
		return nil, nil, "", nil, err
	}
	viewDecision := viewPolicyDecision(e.cfg.Policy, ViewPolicyRequest{
		ViewName: parsed.ViewName, Version: parsed.Version,
	})

	result, err := e.cfg.Views.Execute(ctx, view.ExecuteRequest{
		ViewName:   parsed.ViewName,
		Version:    parsed.Version,
		Actor:      req.Actor,
		Subject:    req.Subject,
		Limit:      parsed.Limit,
		Offset:     parsed.Offset,
		Parameters: parsed.Parameters,
	})
	if err != nil {
		return nil, nil, "", nil, err
	}

	columnNames := make([]string, len(result.Columns))
	for i, col := range result.Columns {
		columnNames[i] = col.Name
	}

	viewData := map[string]any{
		"viewName": result.ViewName,
		"version":  result.Version,
		"columns":  result.Columns,
		"rows":     result.Rows,
		"total":    result.Total,
	}
	if result.NextOffset != nil {
		viewData["nextOffset"] = *result.NextOffset
	}

	var data any = viewData
	var redactions []string
	if viewDecision != nil && viewDecision.Deidentify {
		var deidErr error
		data, redactions, deidErr = e.cfg.Deidentify.Deidentify(ctx, DeidentifyRequest{
			ToolName: ToolRunView, Data: viewData,
		})
		if deidErr != nil {
			return nil, nil, "", nil, deidErr
		}
	}

	citations := e.cfg.Citations.ViewCitations(
		result.ViewName, result.Version, result.Metadata.SourceResourceType, columnNames, result.Rows,
	)
	return data, citations, "success", redactions, nil
}

func (e *Executor) execWrite(ctx context.Context, req ToolRequest, input map[string]any) (any, []Citation, string, bool, []string, error) {
	if e.cfg.Core == nil {
		return nil, nil, "", false, nil, fmt.Errorf("%w: core service required for writes", ErrMissingDependency)
	}
	parsed, err := parseWriteInput(input)
	if err != nil {
		return nil, nil, "", false, nil, err
	}

	decision, err := e.cfg.Policy.CheckWrite(ctx, WritePolicyRequest{
		Actor: req.Actor, Subject: req.Subject,
		Operation: parsed.Operation, ResourceType: parsed.ResourceType,
		ID: parsed.ID, Fields: parsed.Fields,
	})
	if err != nil {
		return nil, nil, "", false, nil, err
	}
	if !decision.Allowed {
		return nil, nil, "", false, nil, fmt.Errorf("%w: write %s %s", ErrPolicyDenied, parsed.Operation, parsed.ResourceType)
	}

	allowedFields := filterAllowedFields(parsed.Fields, decision.AllowedFields)
	jsonData, err := envelopeJSON(parsed.ResourceType, parsed.ID, allowedFields)
	if err != nil {
		return nil, nil, "", false, nil, err
	}

	if e.cfg.Validator != nil {
		env := &types.ResourceEnvelope{ResourceType: parsed.ResourceType, JSON: jsonData}
		if parsed.Operation == "update" {
			env.ID = parsed.ID
		}
		result, valErr := e.cfg.Validator.Validate(ctx, env, validate.ValidateOptions{})
		if valErr != nil {
			return nil, nil, "", false, nil, valErr
		}
		if result != nil && !result.Valid {
			return nil, nil, "", false, nil, fmt.Errorf("%w: %d issue(s)", ErrValidationFailed, len(result.Issues))
		}
	}

	preview := map[string]any{
		"operation":    parsed.Operation,
		"resourceType": parsed.ResourceType,
		"fields":       allowedFields,
	}
	if parsed.ID != "" {
		preview["id"] = parsed.ID
	}

	if decision.RequiresApproval {
		if e.cfg.Approval == nil {
			return preview, nil, "approval-required", true, nil, nil
		}
		approval, apprErr := e.cfg.Approval.RequestApproval(ctx, ApprovalRequest{
			Actor: req.Actor, Subject: req.Subject,
			Operation: parsed.Operation, ResourceType: parsed.ResourceType,
			ID: parsed.ID, Fields: allowedFields, Preview: preview,
		})
		if apprErr != nil {
			return nil, nil, "", false, nil, apprErr
		}
		if approval == nil || !approval.Approved {
			return preview, nil, "approval-required", true, nil, nil
		}
	}

	var written *types.ResourceEnvelope
	switch parsed.Operation {
	case "create":
		written, err = e.cfg.Core.Create(ctx, &types.ResourceEnvelope{
			ResourceType: parsed.ResourceType,
			ID:           parsed.ID,
			JSON:         jsonData,
		})
	case "update":
		existing, readErr := e.cfg.Core.Read(ctx, parsed.ResourceType, parsed.ID)
		if readErr != nil {
			return nil, nil, "", false, nil, readErr
		}
		merged, mergeErr := mergeUpdateJSON(existing.JSON, allowedFields)
		if mergeErr != nil {
			return nil, nil, "", false, nil, mergeErr
		}
		written, err = e.cfg.Core.Update(ctx, &types.ResourceEnvelope{
			ResourceType: parsed.ResourceType,
			ID:           parsed.ID,
			JSON:         merged,
		})
	}
	if err != nil {
		if core.KindOf(err) == core.ErrorKindInvalid {
			return nil, nil, "", false, nil, fmt.Errorf("%w: %v", ErrValidationFailed, err)
		}
		return nil, nil, "", false, nil, err
	}

	data := map[string]any{
		"operation":    parsed.Operation,
		"resourceType": written.ResourceType,
		"id":           written.ID,
		"versionId":    written.VersionID,
	}
	citations := []Citation{e.cfg.Citations.WriteCitation(parsed.Operation, written.ResourceType, written.ID)}
	return data, citations, "success", false, nil, nil
}

func mergeUpdateJSON(existing []byte, fields map[string]any) ([]byte, error) {
	var root map[string]any
	if err := json.Unmarshal(existing, &root); err != nil {
		return nil, err
	}
	if err := applyFields(root, fields); err != nil {
		return nil, err
	}
	return json.Marshal(root)
}

func filterAllowedFields(requested map[string]any, allowed []string) map[string]any {
	if len(allowed) == 0 {
		return requested
	}
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, f := range allowed {
		allowedSet[f] = struct{}{}
	}
	out := make(map[string]any)
	for k, v := range requested {
		if _, ok := allowedSet[k]; ok {
			out[k] = v
		}
	}
	return out
}

func (e *Executor) logAudit(ctx context.Context, req ToolRequest, toolName, outcome string, details map[string]string) error {
	if e.cfg.Audit == nil {
		return nil
	}
	return e.cfg.Audit.LogToolAccess(ctx, AuditRecord{
		ToolName:       toolName,
		Actor:          req.Actor,
		Subject:        req.Subject,
		Outcome:        outcome,
		Details:        details,
		ConversationID: req.ConversationID,
		Timestamp:      e.cfg.Now(),
	})
}

func auditDetailsForTool(toolName string, input map[string]any, data any) map[string]string {
	details := map[string]string{"delegate": toolName}
	if input != nil {
		if rt, ok := input["resourceType"].(string); ok {
			details["resourceType"] = rt
		}
		if id, ok := input["id"].(string); ok {
			details["resourceId"] = id
		}
		if view, ok := input["viewName"].(string); ok {
			details["viewName"] = view
		}
		if op, ok := input["operation"].(string); ok {
			details["operation"] = op
		}
	}
	if m, ok := data.(map[string]any); ok {
		if id, ok := m["id"].(string); ok {
			details["writtenResourceId"] = id
		}
		if rt, ok := m["resourceType"].(string); ok && details["resourceType"] == "" {
			details["resourceType"] = rt
		}
	}
	return details
}

func policyMaxCount(policy PolicyEngine, resourceType string) (int, bool) {
	allow, ok := policy.(*AllowListPolicy)
	if !ok {
		return 0, false
	}
	cfg, ok := allow.Search[resourceType]
	if !ok || cfg.MaxCount <= 0 {
		return 0, false
	}
	return cfg.MaxCount, true
}

func defaultSearchCount(policy PolicyEngine, resourceType string) int {
	max, ok := policyMaxCount(policy, resourceType)
	if ok {
		return max
	}
	return 50
}

func viewPolicyDecision(policy PolicyEngine, req ViewPolicyRequest) *ViewPolicyDecision {
	allow, ok := policy.(*AllowListPolicy)
	if !ok {
		return nil
	}
	decision, err := allow.CheckViewDecision(req)
	if err != nil {
		return nil
	}
	return decision
}

func outcomeForError(err error) string {
	switch {
	case errors.Is(err, ErrPolicyDenied):
		return "denied"
	case errors.Is(err, ErrValidationFailed):
		return "validation-failed"
	case errors.Is(err, ErrUnauthorized):
		return "denied"
	default:
		return "error"
	}
}
