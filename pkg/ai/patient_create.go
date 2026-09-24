package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ToolProposePatientCreate is deprecated: use ToolProposeWriteResource.
const ToolProposePatientCreate = "propose_patient_create"

// PatientCreateDraft is a convenience wrapper for Patient create proposals.
// Prefer ResourceWriteDraft for generic create/update flows.
type PatientCreateDraft struct {
	Family    string
	Given     []string
	Gender    string
	BirthDate string
	Phone     string
}

// Validate checks required demographics for a Patient create proposal.
func (d PatientCreateDraft) Validate() error {
	if strings.TrimSpace(d.Family) == "" {
		return fmt.Errorf("%w: family name is required", ErrInvalidInput)
	}
	if len(d.Given) == 0 || strings.TrimSpace(d.Given[0]) == "" {
		return fmt.Errorf("%w: at least one given name is required", ErrInvalidInput)
	}
	return nil
}

// ToResourceWriteDraft converts the patient-specific draft to a generic write draft.
func (d PatientCreateDraft) ToResourceWriteDraft() (ResourceWriteDraft, error) {
	input, err := d.ToWriteFhirResourceInput()
	if err != nil {
		return ResourceWriteDraft{}, err
	}
	return ResourceWriteDraftFromMap(input)
}

// ToWriteFhirResourceInput returns write_fhir_resource tool input only (no execution).
func (d PatientCreateDraft) ToWriteFhirResourceInput() (map[string]any, error) {
	if err := d.Validate(); err != nil {
		return nil, err
	}
	name := map[string]any{
		"family": d.Family,
		"given":  d.Given,
	}
	fields := map[string]any{
		"name": []any{name},
	}
	if g := strings.TrimSpace(d.Gender); g != "" {
		fields["gender"] = g
	}
	if bd := strings.TrimSpace(d.BirthDate); bd != "" {
		fields["birthDate"] = bd
	}
	if phone := strings.TrimSpace(d.Phone); phone != "" {
		fields["telecom"] = []any{
			map[string]any{
				"system": "phone",
				"value":  phone,
			},
		}
	}
	return map[string]any{
		"operation":    WriteOperationCreate,
		"resourceType": "Patient",
		"fields":       fields,
	}, nil
}

// PatientCreateDraftFromMap parses harness tool arguments or JSON objects.
func PatientCreateDraftFromMap(input map[string]any) (PatientCreateDraft, error) {
	if input == nil {
		return PatientCreateDraft{}, fmt.Errorf("%w: empty patient draft", ErrInvalidInput)
	}
	family, err := requireString(input, "family")
	if err != nil {
		return PatientCreateDraft{}, err
	}
	given, err := parseGivenNames(input["given"])
	if err != nil {
		return PatientCreateDraft{}, err
	}
	gender, _ := optionalStringValue(input, "gender")
	birthDate, _ := optionalStringValue(input, "birthDate")
	phone, _ := optionalStringValue(input, "phone")
	return PatientCreateDraft{
		Family:    family,
		Given:     given,
		Gender:    gender,
		BirthDate: birthDate,
		Phone:     phone,
	}, nil
}

// ParsePatientCreateDraftJSON decodes a JSON object from model or app output.
func ParsePatientCreateDraftJSON(raw string) (PatientCreateDraft, error) {
	raw = trimSpace(raw)
	if raw == "" {
		return PatientCreateDraft{}, fmt.Errorf("%w: empty patient draft json", ErrInvalidInput)
	}
	var input map[string]any
	if err := json.Unmarshal([]byte(raw), &input); err != nil {
		return PatientCreateDraft{}, err
	}
	return PatientCreateDraftFromMap(input)
}

// ExtractPatientCreateDraft scans assistant messages for a fenced ```patient_create block.
func ExtractPatientCreateDraft(messages []ChatMessage) (PatientCreateDraft, bool) {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != ChatRoleAssistant {
			continue
		}
		block := extractFencedBlock(messages[i].Content, "patient_create")
		if block == "" {
			continue
		}
		draft, err := ParsePatientCreateDraftJSON(block)
		if err != nil {
			continue
		}
		return draft, true
	}
	return PatientCreateDraft{}, false
}

// ProposePatientCreateToolDescriptor is deprecated: use ProposeWriteResourceToolDescriptor.
func ProposePatientCreateToolDescriptor() ToolDescriptor {
	d := ProposeWriteResourceToolDescriptor()
	d.Name = ToolProposePatientCreate
	d.Description = "Deprecated alias for propose_write_resource (Patient-oriented fields)"
	d.InputKeys = []string{"family", "given", "gender", "birthDate", "phone"}
	return d
}

func patientDraftFromWriteFields(fields map[string]any) (PatientCreateDraft, error) {
	names := sliceOfMapsFromPatientFields(fields["name"])
	if len(names) == 0 {
		return PatientCreateDraft{}, fmt.Errorf("%w: patient name is required", ErrInvalidInput)
	}
	n := names[0]
	given := stringSliceField(n, "given")
	family := stringField(n, "family")
	phone := ""
	if telecom := sliceOfMapsFromPatientFields(fields["telecom"]); len(telecom) > 0 {
		phone = stringField(telecom[0], "value")
	}
	return PatientCreateDraft{
		Family:    family,
		Given:     given,
		Gender:    stringField(fields, "gender"),
		BirthDate: stringField(fields, "birthDate"),
		Phone:     phone,
	}, nil
}

func sliceOfMapsFromPatientFields(raw any) []map[string]any {
	if raw == nil {
		return nil
	}
	if m, ok := raw.(map[string]any); ok {
		return []map[string]any{m}
	}
	switch items := raw.(type) {
	case []map[string]any:
		return items
	case []any:
		out := make([]map[string]any, 0, len(items))
		for _, item := range items {
			if m, ok := item.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	default:
		return nil
	}
}

func parseGivenNames(raw any) ([]string, error) {
	if raw == nil {
		return nil, fmt.Errorf("%w: given is required", ErrInvalidInput)
	}
	switch vals := raw.(type) {
	case []string:
		if len(vals) == 0 {
			return nil, fmt.Errorf("%w: given is required", ErrInvalidInput)
		}
		return vals, nil
	case []any:
		out := make([]string, 0, len(vals))
		for _, v := range vals {
			s, ok := v.(string)
			if !ok || strings.TrimSpace(s) == "" {
				return nil, fmt.Errorf("%w: given must be strings", ErrInvalidInput)
			}
			out = append(out, s)
		}
		if len(out) == 0 {
			return nil, fmt.Errorf("%w: given is required", ErrInvalidInput)
		}
		return out, nil
	case string:
		if strings.TrimSpace(vals) == "" {
			return nil, fmt.Errorf("%w: given is required", ErrInvalidInput)
		}
		return []string{vals}, nil
	default:
		return nil, fmt.Errorf("%w: given must be an array of strings", ErrInvalidInput)
	}
}

func extractFencedBlock(content, lang string) string {
	marker := "```" + lang
	idx := strings.Index(content, marker)
	if idx < 0 {
		return ""
	}
	rest := content[idx+len(marker):]
	if len(rest) > 0 && rest[0] == '\n' {
		rest = rest[1:]
	}
	end := strings.Index(rest, "```")
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(rest[:end])
}

// CommitPatientCreateFromSession is deprecated: use CommitWriteFromSession.
func (h *Harness) CommitPatientCreateFromSession(ctx context.Context) (*ToolResult, error) {
	if h == nil {
		return nil, errors.New("ai: nil harness")
	}
	if err := h.ensureSessionLoaded(ctx); err != nil {
		return nil, err
	}
	draft, ok := ExtractPatientCreateDraft(h.session.Messages)
	if !ok {
		return nil, fmt.Errorf("%w: no patient_create draft in session", ErrInvalidInput)
	}
	return h.CommitPatientCreate(ctx, draft)
}

// CommitPatientCreate is deprecated: use CommitWrite with ResourceWriteDraft.
func (h *Harness) CommitPatientCreate(ctx context.Context, draft PatientCreateDraft) (*ToolResult, error) {
	generic, err := draft.ToResourceWriteDraft()
	if err != nil {
		return nil, err
	}
	return h.CommitWrite(ctx, generic)
}
