package ai

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ToolProposeWritePlan is a harness-only tool for multi-step write proposals.
const ToolProposeWritePlan = "propose_write_plan"

// ResourceWritePlan is an ordered list of bundle steps (writes and optional batch reads).
// When AI attribution is enabled the executor may also append Provenance for clinical writes.
type ResourceWritePlan struct {
	// BundleType is transaction (default) or batch. Batch is required when Entries include reads.
	BundleType string
	Entries    []ResourceWriteDraft
}

// Validate checks each entry and bundle shape.
func (p ResourceWritePlan) Validate() error {
	if len(p.Entries) == 0 {
		return fmt.Errorf("%w: at least one plan entry is required", ErrInvalidInput)
	}
	hasRead := false
	hasWrite := false
	for i, d := range p.Entries {
		if err := d.Validate(); err != nil {
			return fmt.Errorf("%w: entry %d: %v", ErrInvalidInput, i, err)
		}
		switch strings.TrimSpace(d.Operation) {
		case WriteOperationRead:
			hasRead = true
		case WriteOperationCreate, WriteOperationUpdate:
			hasWrite = true
		}
	}
	bt := strings.TrimSpace(p.BundleType)
	if bt == "" {
		if hasRead {
			bt = "batch"
		} else {
			bt = "transaction"
		}
	}
	if hasRead && bt != "batch" {
		return fmt.Errorf("%w: batch bundleType is required when plan includes read entries", ErrInvalidInput)
	}
	_ = hasWrite
	_ = hasRead
	return nil
}

// ToTransactionInput builds execute_fhir_bundle input.
func (p ResourceWritePlan) ToTransactionInput() (map[string]any, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	bundleType := strings.TrimSpace(p.BundleType)
	if bundleType == "" {
		bundleType = "transaction"
		for _, d := range p.Entries {
			if strings.TrimSpace(d.Operation) == WriteOperationRead {
				bundleType = "batch"
				break
			}
		}
	}
	entries := make([]any, 0, len(p.Entries))
	for _, d := range p.Entries {
		entry, err := draftToTransactionEntry(d)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return map[string]any{
		"bundleType": bundleType,
		"entries":    entries,
	}, nil
}

func draftToTransactionEntry(d ResourceWriteDraft) (map[string]any, error) {
	rt := strings.TrimSpace(d.ResourceType)
	switch strings.TrimSpace(d.Operation) {
	case WriteOperationRead:
		return map[string]any{
			"method":       "GET",
			"resourceType": rt,
			"id":           strings.TrimSpace(d.ID),
		}, nil
	case WriteOperationCreate:
		out := map[string]any{
			"method":       "POST",
			"resourceType": rt,
			"fields":       d.Fields,
		}
		if id := strings.TrimSpace(d.ID); id != "" {
			out["id"] = id
		}
		return out, nil
	case WriteOperationUpdate:
		return map[string]any{
			"method":       "PUT",
			"resourceType": rt,
			"id":           strings.TrimSpace(d.ID),
			"patches":      d.Patches,
		}, nil
	default:
		return nil, fmt.Errorf("%w: unknown operation %q", ErrInvalidInput, d.Operation)
	}
}

// ResourceWritePlanFromMap parses harness tool arguments.
func ResourceWritePlanFromMap(input map[string]any) (ResourceWritePlan, error) {
	plan := ResourceWritePlan{}
	if bt, err := optionalStringValue(input, "bundleType"); err != nil {
		return ResourceWritePlan{}, err
	} else {
		plan.BundleType = bt
	}
	raw, ok := input["entries"]
	if !ok {
		return ResourceWritePlan{}, fmt.Errorf("%w: entries is required", ErrInvalidInput)
	}
	list, ok := raw.([]any)
	if !ok || len(list) == 0 {
		return ResourceWritePlan{}, fmt.Errorf("%w: entries must be a non-empty array", ErrInvalidInput)
	}
	plan.Entries = make([]ResourceWriteDraft, 0, len(list))
	for i, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			return ResourceWritePlan{}, fmt.Errorf("%w: entries[%d] must be an object", ErrInvalidInput, i)
		}
		draft, err := ResourceWriteDraftFromMap(m)
		if err != nil {
			return ResourceWritePlan{}, fmt.Errorf("%w: entries[%d]: %v", ErrInvalidInput, i, err)
		}
		plan.Entries = append(plan.Entries, draft)
	}
	return plan, nil
}

// ParseResourceWritePlanJSON decodes a JSON plan from model or app output.
func ParseResourceWritePlanJSON(raw string) (ResourceWritePlan, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ResourceWritePlan{}, fmt.Errorf("%w: empty write plan json", ErrInvalidInput)
	}
	var input map[string]any
	if err := json.Unmarshal([]byte(raw), &input); err != nil {
		return ResourceWritePlan{}, err
	}
	return ResourceWritePlanFromMap(input)
}

// ExtractResourceWritePlan scans assistant messages for a ```write_plan block.
func ExtractResourceWritePlan(messages []ChatMessage) (ResourceWritePlan, bool) {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != ChatRoleAssistant {
			continue
		}
		if block := extractFencedBlock(messages[i].Content, "write_plan"); block != "" {
			plan, err := ParseResourceWritePlanJSON(block)
			if err == nil {
				return plan, true
			}
		}
	}
	return ResourceWritePlan{}, false
}

// ResourceWritePlanFromTransactionInput converts execute_fhir_bundle input to a plan.
func ResourceWritePlanFromTransactionInput(input map[string]any) (ResourceWritePlan, error) {
	bundleType, specs, err := parseBundleInput(input)
	if err != nil {
		return ResourceWritePlan{}, err
	}
	plan := ResourceWritePlan{
		BundleType: bundleType,
		Entries:    make([]ResourceWriteDraft, 0, len(specs)),
	}
	for _, spec := range specs {
		method := strings.ToUpper(strings.TrimSpace(spec.Method))
		switch method {
		case "GET":
			plan.Entries = append(plan.Entries, ResourceWriteDraft{
				Operation:    WriteOperationRead,
				ResourceType: spec.ResourceType,
				ID:           spec.ID,
			})
		case "POST":
			plan.Entries = append(plan.Entries, ResourceWriteDraft{
				Operation:    WriteOperationCreate,
				ResourceType: spec.ResourceType,
				ID:           spec.ID,
				Fields:       spec.Fields,
			})
		case "PUT":
			plan.Entries = append(plan.Entries, ResourceWriteDraft{
				Operation:    WriteOperationUpdate,
				ResourceType: spec.ResourceType,
				ID:           spec.ID,
				Patches:      spec.Patches,
			})
		default:
			return ResourceWritePlan{}, fmt.Errorf("%w: plan does not support bundle method %q", ErrInvalidInput, spec.Method)
		}
	}
	return plan, nil
}

// ProposeWritePlanToolDescriptor returns harness-only tool metadata.
func ProposeWritePlanToolDescriptor() ToolDescriptor {
	return ToolDescriptor{
		Name:        ToolProposeWritePlan,
		Description: "Propose multiple create/update steps (and optional batch reads); returns execute_fhir_bundle input without committing. Host CommitWritePlan executes.",
		Generic:     false,
		InputKeys:   []string{"bundleType", "entries"},
	}
}
