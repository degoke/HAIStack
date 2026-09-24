package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/degoke/haistack/pkg/core"
)

// ToolProposeWriteResource is a harness-only tool. It returns create/update tool input
// without committing; use Harness.CommitWrite to execute through Executor policy.
const ToolProposeWriteResource = "propose_write_resource"

// ResourceWriteDraft is a structured create/update proposal, not arbitrary full Resource JSON.
// Create uses top-level fields; update uses FHIRPath patches only.
type ResourceWriteDraft struct {
	Operation    string
	ResourceType string
	ID           string
	Fields       map[string]any
	Patches      map[string]any
}

// Validate checks the draft matches executor write tool rules at the harness layer.
func (d ResourceWriteDraft) Validate() error {
	op := strings.TrimSpace(d.Operation)
	if op != WriteOperationCreate && op != WriteOperationUpdate {
		return fmt.Errorf("%w: operation must be create or update", ErrInvalidInput)
	}
	if strings.TrimSpace(d.ResourceType) == "" {
		return fmt.Errorf("%w: resourceType is required", ErrInvalidInput)
	}
	if op == WriteOperationUpdate && strings.TrimSpace(d.ID) == "" {
		return fmt.Errorf("%w: id is required for update", ErrInvalidInput)
	}
	switch op {
	case WriteOperationCreate:
		if len(d.Fields) == 0 {
			return fmt.Errorf("%w: at least one field is required for create", ErrInvalidInput)
		}
		for key := range d.Fields {
			if err := validateWriteKey(key); err != nil {
				return err
			}
		}
	case WriteOperationUpdate:
		if len(d.Fields) != 0 {
			return fmt.Errorf("%w: updates must use patches (FHIRPath keys), not fields", ErrInvalidInput)
		}
		if len(d.Patches) == 0 {
			return fmt.Errorf("%w: at least one patch is required for update", ErrInvalidInput)
		}
		for path := range d.Patches {
			if err := validateDraftPatchPath(d.ResourceType, path); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateDraftPatchPath(resourceType, path string) error {
	if err := validateWriteKey(path); err != nil {
		return err
	}
	if err := core.ValidateFHIRResourcePath(resourceType, path); err != nil {
		return fmt.Errorf("%w: invalid FHIRPath %q: %v", ErrInvalidInput, path, err)
	}
	return nil
}

// ToExecutorToolInput returns the create_fhir_resource or update_fhir_resource tool input (no execution).
func (d ResourceWriteDraft) ToExecutorToolInput() (string, map[string]any, error) {
	if err := d.Validate(); err != nil {
		return "", nil, err
	}
	rt := strings.TrimSpace(d.ResourceType)
	switch strings.TrimSpace(d.Operation) {
	case WriteOperationCreate:
		out := map[string]any{
			"resourceType": rt,
			"fields":       d.Fields,
		}
		if id := strings.TrimSpace(d.ID); id != "" {
			out["id"] = id
		}
		return ToolCreateFhirResource, out, nil
	case WriteOperationUpdate:
		return ToolUpdateFhirResource, map[string]any{
			"resourceType": rt,
			"id":           strings.TrimSpace(d.ID),
			"patches":      d.Patches,
		}, nil
	default:
		return "", nil, fmt.Errorf("%w: unknown operation", ErrInvalidInput)
	}
}

// ResourceWriteDraftFromMap parses harness tool arguments or JSON objects.
func ResourceWriteDraftFromMap(input map[string]any) (ResourceWriteDraft, error) {
	if input == nil {
		return ResourceWriteDraft{}, fmt.Errorf("%w: empty write draft", ErrInvalidInput)
	}
	op, err := requireString(input, "operation")
	if err != nil {
		return ResourceWriteDraft{}, err
	}
	rt, err := requireString(input, "resourceType")
	if err != nil {
		return ResourceWriteDraft{}, err
	}
	id, _ := optionalStringValue(input, "id")
	fields, _ := parseFields(input["fields"])
	patches, _ := parsePatches(input["patches"], rt)
	return ResourceWriteDraft{
		Operation:    op,
		ResourceType: rt,
		ID:           id,
		Fields:       fields,
		Patches:      patches,
	}, nil
}

// ParseResourceWriteDraftJSON decodes a JSON object from model or app output.
func ParseResourceWriteDraftJSON(raw string) (ResourceWriteDraft, error) {
	raw = trimSpace(raw)
	if raw == "" {
		return ResourceWriteDraft{}, fmt.Errorf("%w: empty write draft json", ErrInvalidInput)
	}
	var input map[string]any
	if err := json.Unmarshal([]byte(raw), &input); err != nil {
		return ResourceWriteDraft{}, err
	}
	return ResourceWriteDraftFromMap(input)
}

// ExtractResourceWriteDraft scans assistant messages for a fenced ```write_resource block.
// Legacy ```patient_create blocks are converted to Patient create drafts.
func ExtractResourceWriteDraft(messages []ChatMessage) (ResourceWriteDraft, bool) {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != ChatRoleAssistant {
			continue
		}
		if block := extractFencedBlock(messages[i].Content, "write_resource"); block != "" {
			draft, err := ParseResourceWriteDraftJSON(block)
			if err == nil {
				return draft, true
			}
		}
		if block := extractFencedBlock(messages[i].Content, "patient_create"); block != "" {
			patient, err := ParsePatientCreateDraftJSON(block)
			if err == nil {
				draft, convErr := patient.ToResourceWriteDraft()
				if convErr == nil {
					return draft, true
				}
			}
		}
	}
	return ResourceWriteDraft{}, false
}

// ProposeWriteResourceToolDescriptor returns harness-only tool metadata.
func ProposeWriteResourceToolDescriptor() ToolDescriptor {
	return ToolDescriptor{
		Name:        ToolProposeWriteResource,
		Description: "Propose a create or update; returns create_fhir_resource or update_fhir_resource input without committing",
		Generic:     false,
		InputKeys:   []string{"operation", "resourceType", "id", "fields", "patches"},
	}
}

// CommitWriteOptions configures host confirmation for CommitWrite.
type CommitWriteOptions struct {
	// SkipHostConfirm bypasses RequireCommitConfirmation (tests and trusted automation only).
	SkipHostConfirm bool
	// ApprovalToken resumes a policy-gated commit (same bundle input as the pending approval).
	ApprovalToken string
}

// CommitWrite executes one write draft as a transaction bundle (host adds Provenance when configured).
func (h *Harness) CommitWrite(ctx context.Context, draft ResourceWriteDraft) (*ToolResult, error) {
	return h.CommitWriteWithOptions(ctx, draft, CommitWriteOptions{})
}

// CommitWriteWithOptions commits one draft via CommitWritePlan (transaction bundle).
func (h *Harness) CommitWriteWithOptions(ctx context.Context, draft ResourceWriteDraft, opts CommitWriteOptions) (*ToolResult, error) {
	return h.CommitWritePlanWithOptions(ctx, ResourceWritePlan{Entries: []ResourceWriteDraft{draft}}, opts)
}

// CommitWritePlan commits multiple drafts in one atomic transaction (host adds Provenance when configured).
func (h *Harness) CommitWritePlan(ctx context.Context, plan ResourceWritePlan) (*ToolResult, error) {
	return h.CommitWritePlanWithOptions(ctx, plan, CommitWriteOptions{})
}

// CommitWritePlanWithOptions runs host confirmation then execute_fhir_bundle for the plan.
func (h *Harness) CommitWritePlanWithOptions(ctx context.Context, plan ResourceWritePlan, opts CommitWriteOptions) (*ToolResult, error) {
	if h == nil {
		return nil, errors.New("ai: nil harness")
	}
	return h.executeCommitPlan(ctx, plan, opts)
}

func (h *Harness) executeCommitPlan(ctx context.Context, plan ResourceWritePlan, opts CommitWriteOptions) (*ToolResult, error) {
	if err := h.confirmCommitWritePlan(ctx, plan, opts); err != nil {
		return nil, err
	}
	input, err := plan.ToTransactionInput()
	if err != nil {
		return nil, err
	}
	return h.cfg.Executor.ExecuteTool(ctx, ToolRequest{
		ToolName:       ToolExecuteFhirBundle,
		Actor:          h.cfg.Actor,
		TenantID:       h.cfg.TenantID,
		Subject:        h.cfg.Subject,
		Input:          input,
		ConversationID: h.session.ConversationID,
		ApprovalToken:  opts.ApprovalToken,
	})
}

// CommitWriteFromSession extracts a write_resource (or legacy patient_create) block and commits.
func (h *Harness) CommitWriteFromSession(ctx context.Context) (*ToolResult, error) {
	return h.CommitWriteFromSessionWithOptions(ctx, CommitWriteOptions{})
}

// CommitWriteFromSessionWithOptions commits a session draft after optional host confirmation.
func (h *Harness) CommitWriteFromSessionWithOptions(ctx context.Context, opts CommitWriteOptions) (*ToolResult, error) {
	if h == nil {
		return nil, errors.New("ai: nil harness")
	}
	if err := h.ensureSessionLoaded(ctx); err != nil {
		return nil, err
	}
	draft, ok := ExtractResourceWriteDraft(h.session.Messages)
	if !ok {
		return nil, fmt.Errorf("%w: no write_resource draft in session", ErrInvalidInput)
	}
	return h.CommitWriteWithOptions(ctx, draft, opts)
}

// CommitWritePlanFromSession extracts a write_plan block and commits via transaction bundle.
func (h *Harness) CommitWritePlanFromSession(ctx context.Context) (*ToolResult, error) {
	return h.CommitWritePlanFromSessionWithOptions(ctx, CommitWriteOptions{})
}

// CommitWritePlanFromSessionWithOptions commits a session write_plan after optional host confirmation.
func (h *Harness) CommitWritePlanFromSessionWithOptions(ctx context.Context, opts CommitWriteOptions) (*ToolResult, error) {
	if h == nil {
		return nil, errors.New("ai: nil harness")
	}
	if err := h.ensureSessionLoaded(ctx); err != nil {
		return nil, err
	}
	plan, ok := ExtractResourceWritePlan(h.session.Messages)
	if !ok {
		return nil, fmt.Errorf("%w: no write_plan in session", ErrInvalidInput)
	}
	return h.CommitWritePlanWithOptions(ctx, plan, opts)
}

func (h *Harness) confirmCommitWritePlan(ctx context.Context, plan ResourceWritePlan, opts CommitWriteOptions) error {
	if opts.SkipHostConfirm || !h.cfg.RequireCommitConfirmation {
		return nil
	}
	if h.cfg.CommitWritePlanConfirm == nil {
		return ErrCommitNotConfirmed
	}
	return h.cfg.CommitWritePlanConfirm(ctx, plan)
}

// confirmBeforeWriteToolInput applies the same host gate as CommitWrite for FHIR write tools.
func (h *Harness) confirmBeforeWriteToolInput(ctx context.Context, toolName string, input map[string]any, opts CommitWriteOptions) error {
	if opts.SkipHostConfirm || !h.cfg.RequireCommitConfirmation || !IsWriteTool(toolName) {
		return nil
	}
	if toolName == ToolExecuteFhirBundle || toolName == ToolExecuteFhirTransaction {
		plan, err := ResourceWritePlanFromTransactionInput(input)
		if err != nil {
			return err
		}
		return h.confirmCommitWritePlan(ctx, plan, opts)
	}
	draft, err := draftFromWriteToolInput(toolName, input)
	if err != nil {
		return err
	}
	return h.confirmCommitWritePlan(ctx, ResourceWritePlan{Entries: []ResourceWriteDraft{draft}}, opts)
}

func draftFromWriteToolInput(toolName string, input map[string]any) (ResourceWriteDraft, error) {
	switch toolName {
	case ToolCreateFhirResource:
		parsed, err := parseCreateInput(input)
		if err != nil {
			return ResourceWriteDraft{}, err
		}
		return ResourceWriteDraft{
			Operation:    WriteOperationCreate,
			ResourceType: parsed.ResourceType,
			ID:           parsed.ID,
			Fields:       parsed.Fields,
		}, nil
	case ToolUpdateFhirResource:
		parsed, err := parseUpdateInput(input)
		if err != nil {
			return ResourceWriteDraft{}, err
		}
		return ResourceWriteDraft{
			Operation:    WriteOperationUpdate,
			ResourceType: parsed.ResourceType,
			ID:           parsed.ID,
			Patches:      parsed.Patches,
		}, nil
	default:
		return ResourceWriteDraft{}, fmt.Errorf("%w: not a write tool", ErrInvalidInput)
	}
}
