package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
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
	Resources             store.ResourceStore
	Search                *search.Service
	Views                 *view.Executor
	Core                  *core.ResourceService
	Validator             validate.Engine
	Policy                PolicyEngine
	Registry              *Registry
	Audit                 AuditLogger
	AuditRequired         bool
	RequireConversationID bool
	Approval              ApprovalHook
	ApprovalStore         ApprovalStore
	Deidentify            Deidentifier
	ModelRouter           *ModelRouter
	Citations             *CitationBuilder
	Formatter             *ContextFormatter
	Now                   func() time.Time
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
	if cfg.AuditRequired && cfg.Audit == nil {
		return nil, ErrMissingAudit
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
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
	if e.cfg.RequireConversationID {
		if err := validateConversationID(req.ConversationID); err != nil {
			return nil, err
		}
	}
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
	sort.Strings(names)
	return names
}

// ExecuteTool validates a request, enforces policy, runs the backing operation,
// and returns structured output with citations and audit metadata.
func (e *Executor) ExecuteTool(ctx context.Context, req ToolRequest) (*ToolResult, error) {
	start := e.cfg.Now()
	if e.cfg.RequireConversationID {
		if err := validateConversationID(req.ConversationID); err != nil {
			return nil, err
		}
	}
	toolName := req.ToolName
	input := req.Input
	if input == nil {
		input = map[string]any{}
	}

	if spec, err := e.cfg.Registry.Resolve(toolName); err == nil {
		mapped, mapErr := spec.MapInput(input)
		if mapErr != nil {
			details := auditDetailsForTool(toolName, input, nil)
			details["error"] = mapErr.Error()
			if auditErr := e.logAudit(ctx, req, toolName, "error", details); auditErr != nil && e.cfg.AuditRequired {
				return nil, errors.Join(mapErr, auditErr)
			}
			return nil, mapErr
		}
		toolName = spec.Delegate
		input = mapped
	} else if !IsGeneric(toolName) {
		details := auditDetailsForTool(req.ToolName, input, nil)
		details["error"] = err.Error()
		if auditErr := e.logAudit(ctx, req, req.ToolName, "error", details); auditErr != nil && e.cfg.AuditRequired {
			return nil, errors.Join(err, auditErr)
		}
		return nil, err
	}

	var (
		data          any
		citations     []Citation
		outcome       string
		approval      bool
		approvalToken string
		redactions    []string
		err           error
	)

	switch toolName {
	case ToolReadFhirResource:
		data, citations, outcome, redactions, err = e.execRead(ctx, req, input)
	case ToolSearchFhirResources:
		data, citations, outcome, redactions, err = e.execSearch(ctx, req, input)
	case ToolRunView:
		data, citations, outcome, redactions, err = e.execView(ctx, req, input)
	case ToolWriteFhirResource:
		data, citations, outcome, approval, approvalToken, redactions, err = e.execWrite(ctx, req, input)
	default:
		err = fmt.Errorf("%w: %s", ErrToolNotFound, req.ToolName)
	}

	if err != nil {
		details := auditDetailsForTool(req.ToolName, input, nil)
		details["error"] = err.Error()
		if auditErr := e.logAudit(ctx, req, req.ToolName, outcomeForError(err), details); auditErr != nil && e.cfg.AuditRequired {
			return nil, errors.Join(err, auditErr)
		}
		return nil, err
	}

	ctxText, fmtErr := e.cfg.Formatter.Format(data)
	if fmtErr != nil {
		details := auditDetailsForTool(req.ToolName, input, data)
		details["error"] = fmtErr.Error()
		if auditErr := e.logAudit(ctx, req, req.ToolName, "error", details); auditErr != nil && e.cfg.AuditRequired {
			return nil, errors.Join(fmtErr, auditErr)
		}
		return nil, fmtErr
	}

	var auditErr error
	if approval {
		auditOutcome := "approval-required"
		auditErr = e.logAudit(ctx, req, req.ToolName, auditOutcome, auditDetailsForTool(toolName, input, data))
		outcome = auditOutcome
	} else {
		auditErr = e.logAudit(ctx, req, req.ToolName, outcome, auditDetailsForTool(toolName, input, data))
	}
	if auditErr != nil && e.cfg.AuditRequired {
		return nil, auditErr
	}

	return &ToolResult{
		ToolName:         req.ToolName,
		Data:             data,
		Context:          ctxText,
		Citations:        citations,
		AuditMeta:        AuditMeta{ExecutedAt: e.cfg.Now(), Duration: e.cfg.Now().Sub(start), Outcome: outcome},
		ApprovalRequired: approval,
		ApprovalToken:    approvalToken,
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
	if decision == nil {
		return nil, nil, "", nil, fmt.Errorf("%w: read policy returned no decision", ErrPolicyDenied)
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
		if e.cfg.Deidentify == nil {
			return nil, nil, "", nil, ErrMissingDeidentifier
		}
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
	if decision == nil {
		return nil, nil, "", nil, fmt.Errorf("%w: search policy returned no decision", ErrPolicyDenied)
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
		item, err := filterSearchResourceJSON(res.JSON, decision.AllowedFields, decision.AllowAllFields)
		if err != nil {
			return nil, nil, "", nil, err
		}
		resources = append(resources, item)
	}

	included := make([]map[string]any, 0, len(result.Included))
	var includedResources []*types.ResourceEnvelope
	for _, inc := range result.Included {
		if inc.Resource == nil {
			continue
		}
		readDecision, readErr := e.cfg.Policy.CheckRead(ctx, ReadPolicyRequest{
			Actor: req.Actor, Subject: req.Subject,
			ResourceType: inc.ResourceType, ID: inc.ID,
		})
		if readErr != nil {
			return nil, nil, "", nil, readErr
		}
		if !readDecision.Allowed {
			continue
		}
		item, itemErr := filterResourceJSON(inc.Resource.JSON, readDecision.AllowedFields)
		if itemErr != nil {
			return nil, nil, "", nil, itemErr
		}
		included = append(included, item)
		includedResources = append(includedResources, inc.Resource)
	}

	dataMap := map[string]any{
		"resourceType": parsed.ResourceType,
		"total":        result.Total,
		"count":        result.Count,
		"resources":    resources,
	}
	if len(included) > 0 {
		dataMap["included"] = included
	}
	var data any = dataMap
	var redactions []string
	if decision.Deidentify {
		if e.cfg.Deidentify == nil {
			return nil, nil, "", nil, ErrMissingDeidentifier
		}
		data, redactions, err = e.cfg.Deidentify.Deidentify(ctx, DeidentifyRequest{
			ToolName: ToolSearchFhirResources, ResourceType: parsed.ResourceType, Data: data,
		})
		if err != nil {
			return nil, nil, "", nil, err
		}
	}

	citations := e.cfg.Citations.SearchCitationsWithIncludes(parsed.ResourceType, params, result.Resources, includedResources)
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

	viewReq := ViewPolicyRequest{
		Actor: req.Actor, Subject: req.Subject,
		ViewName: parsed.ViewName, Version: parsed.Version, Parameters: parsed.Parameters,
		Limit: parsed.Limit, Offset: parsed.Offset,
	}
	if scoped, ok := e.cfg.Policy.(ViewScopePolicy); ok {
		narrowed, scopeErr := scoped.ApplyViewScope(ctx, viewReq)
		if scopeErr != nil {
			return nil, nil, "", nil, scopeErr
		}
		if narrowed == nil {
			return nil, nil, "", nil, fmt.Errorf("%w: view scope decision is nil", ErrPolicyDenied)
		}
		viewReq = *narrowed
		parsed.Parameters = viewReq.Parameters
	}
	if err := e.cfg.Policy.CheckView(ctx, viewReq); err != nil {
		return nil, nil, "", nil, err
	}
	viewDecision, decisionErr := viewPolicyDecision(ctx, e.cfg.Policy, viewReq)
	if decisionErr != nil {
		return nil, nil, "", nil, decisionErr
	}
	limit := parsed.Limit
	maxLimit := 100
	if viewDecision != nil && viewDecision.MaxCount > 0 {
		maxLimit = viewDecision.MaxCount
	} else if countPolicy, ok := e.cfg.Policy.(ViewCountPolicy); ok {
		if count, ok := countPolicy.MaxViewCount(parsed.ViewName, parsed.Version); ok && count > 0 {
			maxLimit = count
		}
	}
	if maxLimit > 100 {
		maxLimit = 100
	}
	if limit <= 0 || limit > maxLimit {
		limit = maxLimit
	}

	result, err := e.cfg.Views.Execute(ctx, view.ExecuteRequest{
		ViewName:   parsed.ViewName,
		Version:    parsed.Version,
		Actor:      req.Actor,
		Subject:    req.Subject,
		Limit:      limit,
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
		if e.cfg.Deidentify == nil {
			return nil, nil, "", nil, ErrMissingDeidentifier
		}
		var deidErr error
		data, redactions, deidErr = e.cfg.Deidentify.Deidentify(ctx, DeidentifyRequest{
			ToolName: ToolRunView, ResourceType: result.Metadata.SourceResourceType,
			ViewName: result.ViewName, ViewVersion: result.Version, Data: viewData,
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

func (e *Executor) execWrite(ctx context.Context, req ToolRequest, input map[string]any) (any, []Citation, string, bool, string, []string, error) {
	if e.cfg.Core == nil {
		return nil, nil, "", false, "", nil, fmt.Errorf("%w: core service required for writes", ErrMissingDependency)
	}
	parsed, err := parseWriteInput(input)
	if err != nil {
		return nil, nil, "", false, "", nil, err
	}

	decision, err := e.cfg.Policy.CheckWrite(ctx, WritePolicyRequest{
		Actor: req.Actor, Subject: req.Subject,
		Operation: parsed.Operation, ResourceType: parsed.ResourceType,
		ID: parsed.ID, Fields: parsed.Fields,
	})
	if err != nil {
		return nil, nil, "", false, "", nil, err
	}
	if decision == nil {
		return nil, nil, "", false, "", nil, fmt.Errorf("%w: write policy returned no decision", ErrPolicyDenied)
	}
	if !decision.Allowed {
		return nil, nil, "", false, "", nil, fmt.Errorf("%w: write %s %s", ErrPolicyDenied, parsed.Operation, parsed.ResourceType)
	}
	for field := range parsed.Fields {
		if field == "resourceType" || field == "id" {
			return nil, nil, "", false, "", nil, fmt.Errorf("%w: field %q cannot be written", ErrPolicyDenied, field)
		}
	}

	allowedFields := filterAllowedFields(parsed.Fields, decision.AllowedFields)
	jsonData, err := envelopeJSON(parsed.ResourceType, parsed.ID, allowedFields)
	if err != nil {
		return nil, nil, "", false, "", nil, err
	}
	var existing *types.ResourceEnvelope
	var merged []byte
	if parsed.Operation == "update" {
		existing, err = e.cfg.Core.Read(ctx, parsed.ResourceType, parsed.ID)
		if err != nil {
			return nil, nil, "", false, "", nil, err
		}
		merged, err = mergeUpdateJSON(existing.JSON, allowedFields)
		if err != nil {
			return nil, nil, "", false, "", nil, err
		}
	}

	if e.cfg.Validator != nil {
		candidate := jsonData
		if parsed.Operation == "update" {
			candidate = merged
		}
		env := &types.ResourceEnvelope{ResourceType: parsed.ResourceType, ID: parsed.ID, JSON: candidate}
		result, valErr := e.cfg.Validator.Validate(ctx, env, validate.ValidateOptions{})
		if valErr != nil {
			return nil, nil, "", false, "", nil, valErr
		}
		if result != nil && !result.Valid {
			return nil, nil, "", false, "", nil, fmt.Errorf("%w: %d issue(s)", ErrValidationFailed, len(result.Issues))
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

	approvalReq := ApprovalRequest{
		Actor: req.Actor, Subject: req.Subject,
		Operation: parsed.Operation, ResourceType: parsed.ResourceType,
		ID: parsed.ID, Fields: allowedFields, Preview: preview,
	}
	if decision.RequiresApproval {
		var approval *ApprovalResult
		if req.ApprovalToken != "" {
			if e.cfg.ApprovalStore == nil {
				return nil, nil, "", false, "", nil, ErrMissingApprovalStore
			}
			if verifyErr := e.cfg.ApprovalStore.VerifyAndConsume(ctx, req.ApprovalToken, approvalReq); verifyErr != nil {
				return nil, nil, "", false, "", nil, fmt.Errorf("%w: %v", ErrApprovalTokenInvalid, verifyErr)
			}
			approval = &ApprovalResult{Approved: true, Token: req.ApprovalToken}
		} else if e.cfg.Approval != nil {
			approval, err = e.cfg.Approval.RequestApproval(ctx, approvalReq)
			if err != nil {
				return nil, nil, "", false, "", nil, err
			}
		} else if e.cfg.ApprovalStore != nil {
			token, createErr := e.cfg.ApprovalStore.CreatePending(ctx, approvalReq)
			if createErr != nil {
				return nil, nil, "", false, "", nil, createErr
			}
			return preview, nil, "approval-required", true, token, nil, nil
		} else {
			return nil, nil, "", false, "", nil, ErrMissingApprovalStore
		}
		if approval == nil || !approval.Approved {
			token := ""
			if approval != nil {
				token = approval.Token
			}
			if token == "" && e.cfg.ApprovalStore != nil {
				token, err = e.cfg.ApprovalStore.CreatePending(ctx, approvalReq)
				if err != nil {
					return nil, nil, "", false, "", nil, err
				}
			}
			return preview, nil, "approval-required", true, token, nil, nil
		}
		if approval.Token == "" {
			return nil, nil, "", false, "", nil, ErrApprovalTokenRequired
		}
		if req.ApprovalToken == "" {
			if e.cfg.ApprovalStore == nil {
				return nil, nil, "", false, "", nil, ErrMissingApprovalStore
			}
			if verifyErr := e.cfg.ApprovalStore.VerifyAndConsume(ctx, approval.Token, approvalReq); verifyErr != nil {
				return nil, nil, "", false, "", nil, fmt.Errorf("%w: %v", ErrApprovalTokenInvalid, verifyErr)
			}
		}
	} else if req.ApprovalToken != "" {
		return nil, nil, "", false, "", nil, fmt.Errorf("%w: approval token supplied for a write that does not require approval", ErrInvalidInput)
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
		written, err = e.cfg.Core.Update(ctx, &types.ResourceEnvelope{
			ResourceType: parsed.ResourceType,
			ID:           parsed.ID,
			JSON:         merged,
		})
	}
	if err != nil {
		if core.KindOf(err) == core.ErrorKindInvalid {
			return nil, nil, "", false, "", nil, fmt.Errorf("%w: %v", ErrValidationFailed, err)
		}
		return nil, nil, "", false, "", nil, err
	}

	data := map[string]any{
		"operation":    parsed.Operation,
		"resourceType": written.ResourceType,
		"id":           written.ID,
		"versionId":    written.VersionID,
	}
	citations := []Citation{e.cfg.Citations.WriteCitation(parsed.Operation, written.ResourceType, written.ID)}
	return data, citations, "success", false, "", nil, nil
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
		if e.cfg.AuditRequired {
			return ErrMissingAudit
		}
		return nil
	}
	if err := e.cfg.Audit.LogToolAccess(ctx, AuditRecord{
		ToolName:       toolName,
		Actor:          req.Actor,
		Tenant:         req.TenantID,
		Subject:        req.Subject,
		Outcome:        outcome,
		Details:        details,
		ConversationID: req.ConversationID,
		Timestamp:      e.cfg.Now(),
	}); err != nil {
		if e.cfg.AuditRequired {
			return fmt.Errorf("%w: %v", ErrAuditFailed, err)
		}
		return err
	}
	return nil
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
	capped, ok := policy.(SearchCountPolicy)
	if !ok {
		return 0, false
	}
	return capped.MaxSearchCount(resourceType)
}

func defaultSearchCount(policy PolicyEngine, resourceType string) int {
	max, ok := policyMaxCount(policy, resourceType)
	if ok {
		return max
	}
	return 50
}

func validateConversationID(id string) error {
	trimmed := strings.TrimSpace(id)
	if trimmed == "" {
		return ErrMissingConversationID
	}
	if len(trimmed) > 256 || strings.ContainsAny(trimmed, "\r\n") {
		return fmt.Errorf("%w: invalid format", ErrMissingConversationID)
	}
	return nil
}

func viewPolicyDecision(ctx context.Context, policy PolicyEngine, req ViewPolicyRequest) (*ViewPolicyDecision, error) {
	if decisionProvider, ok := policy.(ViewDecisionPolicyContext); ok {
		return decisionProvider.CheckViewDecisionContext(ctx, req)
	}
	if decisionProvider, ok := policy.(ViewDecisionPolicy); ok {
		return decisionProvider.CheckViewDecision(req)
	}
	return nil, nil
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
