package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ToolProposeWriteResource is a harness-only tool. It returns write_fhir_resource input
// without committing; use Harness.CommitWrite to execute through Executor policy.
const ToolProposeWriteResource = "propose_write_resource"

// ResourceWriteDraft is a structured create/update proposal (field map), not arbitrary FHIR JSON.
type ResourceWriteDraft struct {
	Operation    string
	ResourceType string
	ID           string
	Fields       map[string]any
}

// Validate checks the draft matches write_fhir_resource input rules at the harness layer.
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
	if len(d.Fields) == 0 {
		return fmt.Errorf("%w: at least one field is required", ErrInvalidInput)
	}
	for key := range d.Fields {
		if key == "resourceType" || key == "id" {
			return fmt.Errorf("%w: field %q cannot be set in fields map", ErrInvalidInput, key)
		}
	}
	return nil
}

// ToWriteFhirResourceInput returns write_fhir_resource tool input only (no execution).
func (d ResourceWriteDraft) ToWriteFhirResourceInput() (map[string]any, error) {
	if err := d.Validate(); err != nil {
		return nil, err
	}
	out := map[string]any{
		"operation":    strings.TrimSpace(d.Operation),
		"resourceType": strings.TrimSpace(d.ResourceType),
		"fields":       d.Fields,
	}
	if id := strings.TrimSpace(d.ID); id != "" {
		out["id"] = id
	}
	return out, nil
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
	fields, err := parseFields(input["fields"])
	if err != nil {
		return ResourceWriteDraft{}, err
	}
	return ResourceWriteDraft{
		Operation:    op,
		ResourceType: rt,
		ID:           id,
		Fields:       fields,
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
		Description: "Propose a create or update using structured fields; returns write_fhir_resource input without committing",
		Generic:     false,
		InputKeys:   []string{"operation", "resourceType", "id", "fields"},
	}
}

// CommitWriteOptions configures host confirmation for CommitWrite.
type CommitWriteOptions struct {
	// SkipHostConfirm bypasses RequireCommitConfirmation (tests and trusted automation only).
	SkipHostConfirm bool
}

// CommitWrite executes write_fhir_resource for a validated draft via Executor.
func (h *Harness) CommitWrite(ctx context.Context, draft ResourceWriteDraft) (*ToolResult, error) {
	return h.CommitWriteWithOptions(ctx, draft, CommitWriteOptions{})
}

// CommitWriteWithOptions executes write_fhir_resource after optional host confirmation.
func (h *Harness) CommitWriteWithOptions(ctx context.Context, draft ResourceWriteDraft, opts CommitWriteOptions) (*ToolResult, error) {
	if h == nil {
		return nil, errors.New("ai: nil harness")
	}
	if err := h.confirmCommitWrite(ctx, draft, opts); err != nil {
		return nil, err
	}
	input, err := draft.ToWriteFhirResourceInput()
	if err != nil {
		return nil, err
	}
	return h.cfg.Executor.ExecuteTool(ctx, ToolRequest{
		ToolName:       ToolWriteFhirResource,
		Actor:          h.cfg.Actor,
		TenantID:       h.cfg.TenantID,
		Subject:        h.cfg.Subject,
		Input:          input,
		ConversationID: h.session.ConversationID,
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

func (h *Harness) confirmCommitWrite(ctx context.Context, draft ResourceWriteDraft, opts CommitWriteOptions) error {
	if opts.SkipHostConfirm || !h.cfg.RequireCommitConfirmation {
		return nil
	}
	if h.cfg.CommitWriteConfirm == nil {
		return ErrCommitNotConfirmed
	}
	return h.cfg.CommitWriteConfirm(ctx, draft)
}

// confirmBeforeWriteToolInput applies the same host gate as CommitWrite for write_fhir_resource tool input.
func (h *Harness) confirmBeforeWriteToolInput(ctx context.Context, input map[string]any, opts CommitWriteOptions) error {
	if opts.SkipHostConfirm || !h.cfg.RequireCommitConfirmation {
		return nil
	}
	draft, err := ResourceWriteDraftFromMap(input)
	if err != nil {
		return err
	}
	return h.confirmCommitWrite(ctx, draft, opts)
}
