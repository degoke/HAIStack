package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/degoke/haistack/pkg/core"
	"github.com/google/uuid"

	"github.com/degoke/haistack/pkg/types"
)

type transactionEntrySpec struct {
	Method       string
	ResourceType string
	ID           string
	Fields       map[string]any
	Patches      map[string]any
	FullURL      string
	ProvenanceRef string // set after build: target reference for optional Provenance entry
}

func (e *Executor) execExecuteFhirTransaction(ctx context.Context, req ToolRequest, input map[string]any) (any, []Citation, string, bool, string, []string, error) {
	bundleType, specs, err := parseBundleInput(input)
	if err != nil {
		return nil, nil, "", false, "", nil, err
	}
	return e.execBundleSpecs(ctx, req, bundleType, specs, true)
}

func (e *Executor) execBundleSpecs(ctx context.Context, req ToolRequest, bundleType string, specs []transactionEntrySpec, userInitiated bool) (any, []Citation, string, bool, string, []string, error) {
	if e.cfg.Core == nil {
		return nil, nil, "", false, "", nil, fmt.Errorf("%w: core service required for transaction writes", ErrMissingDependency)
	}
	if err := e.ensureWriteValidator(); err != nil {
		return nil, nil, "", false, "", nil, err
	}
	if len(specs) == 0 {
		return nil, nil, "", false, "", nil, fmt.Errorf("%w: at least one transaction entry is required", ErrInvalidInput)
	}

	preview := map[string]any{"entries": len(specs)}
	approvalFields := map[string]any{}
	requiresApproval := false
	for i, spec := range specs {
		if err := e.preflightBundleEntry(ctx, req, bundleType, i, spec, &requiresApproval, approvalFields); err != nil {
			return nil, nil, "", false, "", nil, err
		}
	}

	if userInitiated && requiresApproval {
		approvalReq := ApprovalRequest{
			Actor: req.Actor, Subject: req.Subject,
			Operation: bundleType, ResourceType: "Bundle",
			Fields: approvalFields, Preview: preview,
		}
		if _, token, outcome, approvalErr := e.handleWriteApproval(ctx, req, approvalReq); approvalErr != nil {
			return nil, nil, "", false, "", nil, approvalErr
		} else if outcome == "approval-required" {
			return preview, nil, outcome, true, token, nil, nil
		}
	}

	bundleEntries, writtenTargets, err := e.buildBundleEntries(ctx, req, bundleType, specs)
	if err != nil {
		return nil, nil, "", false, "", nil, err
	}
	if e.cfg.AIAttribution.Enabled && e.cfg.AIAttribution.createProvenance() {
		explicitProv := provenanceTargetsExplicitInSpecs(specs)
		for _, target := range writtenTargets {
			if provenanceTargetCovered(explicitProv, target) {
				continue
			}
			provJSON, err := e.marshalAIProvenance(req, target)
			if err != nil {
				return nil, nil, "", false, "", nil, err
			}
			bundleEntries = append(bundleEntries, map[string]any{
				"request": map[string]any{
					"method": "POST",
					"url":    "Provenance",
				},
				"resource": jsonRawObject(provJSON),
			})
		}
	}

	bundleJSON, err := json.Marshal(map[string]any{
		"resourceType": "Bundle",
		"type":         bundleType,
		"entry":        bundleEntries,
	})
	if err != nil {
		return nil, nil, "", false, "", nil, err
	}

	var resp *types.ResourceEnvelope
	switch bundleType {
	case "transaction":
		resp, err = e.cfg.Core.ProcessTransactionBundle(ctx, &types.ResourceEnvelope{
			ResourceType: "Bundle",
			JSON:         bundleJSON,
		})
	case "batch":
		resp, err = e.cfg.Core.ProcessBatchBundle(ctx, &types.ResourceEnvelope{
			ResourceType: "Bundle",
			JSON:         bundleJSON,
		})
	default:
		return nil, nil, "", false, "", nil, fmt.Errorf("%w: unknown bundleType %q", ErrInvalidInput, bundleType)
	}
	if err != nil {
		if core.KindOf(err) == core.ErrorKindInvalid {
			return nil, nil, "", false, "", nil, fmt.Errorf("%w: %v", ErrValidationFailed, err)
		}
		return nil, nil, "", false, "", nil, err
	}

	citations := []Citation{e.cfg.Citations.WriteCitation(bundleType, "Bundle", "")}
	data := map[string]any{
		"operation":    bundleType,
		"bundleType":   bundleType,
		"resourceType": "Bundle",
		"entryCount":   len(bundleEntries),
	}
	if resp != nil && len(resp.JSON) > 0 {
		data["response"] = jsonRawObject(resp.JSON)
	}
	return data, citations, "success", false, "", nil, nil
}

func (e *Executor) preflightBundleEntry(
	ctx context.Context,
	req ToolRequest,
	bundleType string,
	index int,
	spec transactionEntrySpec,
	requiresApproval *bool,
	approvalFields map[string]any,
) error {
	method := strings.ToUpper(strings.TrimSpace(spec.Method))
	if method == "GET" {
		if bundleType != "batch" {
			return fmt.Errorf("%w: GET is only supported in batch bundles", ErrInvalidInput)
		}
		id := strings.TrimSpace(spec.ID)
		if id == "" {
			return fmt.Errorf("%w: GET entry requires id", ErrInvalidInput)
		}
		decision, err := e.cfg.Policy.CheckRead(ctx, ReadPolicyRequest{
			Actor: req.Actor, Subject: req.Subject,
			ResourceType: spec.ResourceType, ID: id,
		})
		if err != nil {
			return err
		}
		if decision == nil || !decision.Allowed {
			return fmt.Errorf("%w: batch entry %d read %s", ErrPolicyDenied, index, spec.ResourceType)
		}
		return nil
	}
	op, fields, err := bundleEntryPolicyFields(spec)
	if err != nil {
		return err
	}
	if err := validateWriteMapKeys(fields); err != nil {
		return err
	}
	decision, err := e.cfg.Policy.CheckWrite(ctx, WritePolicyRequest{
		Actor: req.Actor, Subject: req.Subject,
		Operation: op, ResourceType: spec.ResourceType,
		ID: spec.ID, Fields: fields,
	})
	if err != nil {
		return err
	}
	if decision == nil || !decision.Allowed {
		return fmt.Errorf("%w: bundle entry %d %s %s", ErrPolicyDenied, index, op, spec.ResourceType)
	}
	if decision.RequiresApproval {
		*requiresApproval = true
	}
	for k, v := range fields {
		approvalFields[fmt.Sprintf("%d.%s", index, k)] = v
	}
	return nil
}

func bundleEntryPolicyFields(spec transactionEntrySpec) (operation string, fields map[string]any, err error) {
	method := strings.ToUpper(strings.TrimSpace(spec.Method))
	switch method {
	case "POST":
		if len(spec.Fields) == 0 {
			return "", nil, fmt.Errorf("%w: POST entry requires fields", ErrInvalidInput)
		}
		return WriteOperationCreate, spec.Fields, nil
	case "PUT":
		if len(spec.Patches) == 0 {
			return "", nil, fmt.Errorf("%w: PUT entry requires patches", ErrInvalidInput)
		}
		if strings.TrimSpace(spec.ID) == "" {
			return "", nil, fmt.Errorf("%w: PUT entry requires id", ErrInvalidInput)
		}
		return WriteOperationUpdate, spec.Patches, nil
	default:
		return "", nil, fmt.Errorf("%w: unsupported bundle method %q", ErrInvalidInput, spec.Method)
	}
}

func (e *Executor) buildBundleEntries(ctx context.Context, req ToolRequest, bundleType string, specs []transactionEntrySpec) ([]map[string]any, []string, error) {
	var bundleEntries []map[string]any
	var provenanceTargets []string
	for _, spec := range specs {
		method := strings.ToUpper(strings.TrimSpace(spec.Method))
		rt := strings.TrimSpace(spec.ResourceType)
		switch method {
		case "GET":
			if bundleType != "batch" {
				return nil, nil, fmt.Errorf("%w: GET is only supported in batch bundles", ErrInvalidInput)
			}
			id := strings.TrimSpace(spec.ID)
			bundleEntries = append(bundleEntries, map[string]any{
				"request": map[string]any{
					"method": "GET",
					"url":    rt + "/" + id,
				},
			})
		case "POST":
			op := WriteOperationCreate
			decision, err := e.cfg.Policy.CheckWrite(ctx, WritePolicyRequest{
				Actor: req.Actor, Subject: req.Subject,
				Operation: op, ResourceType: rt,
				ID: spec.ID, Fields: spec.Fields,
			})
			if err != nil {
				return nil, nil, err
			}
			allowed := filterAllowedFields(spec.Fields, decision.AllowedFields)
			jsonData, err := envelopeJSON(rt, spec.ID, allowed)
			if err != nil {
				return nil, nil, err
			}
			jsonData, err = e.stampWriteJSONForAttribution(jsonData, req)
			if err != nil {
				return nil, nil, err
			}
			if err := e.validateWriteJSON(ctx, rt, spec.ID, jsonData); err != nil {
				return nil, nil, err
			}
			fullURL := strings.TrimSpace(spec.FullURL)
			targetRef := rt + "/" + strings.TrimSpace(spec.ID)
			if strings.TrimSpace(spec.ID) == "" {
				if fullURL == "" {
					fullURL = "urn:uuid:" + uuid.NewString()
				}
				targetRef = fullURL
			}
			entry := map[string]any{
				"fullUrl": fullURL,
				"request": map[string]any{
					"method": "POST",
					"url":    rt,
				},
				"resource": jsonRawObject(jsonData),
			}
			if fullURL == "" {
				delete(entry, "fullUrl")
			}
			bundleEntries = append(bundleEntries, entry)
			provenanceTargets = append(provenanceTargets, targetRef)
		case "PUT":
			id := strings.TrimSpace(spec.ID)
			decision, err := e.cfg.Policy.CheckWrite(ctx, WritePolicyRequest{
				Actor: req.Actor, Subject: req.Subject,
				Operation: WriteOperationUpdate, ResourceType: rt,
				ID: id, Fields: spec.Patches,
			})
			if err != nil {
				return nil, nil, err
			}
			allowed := filterAllowedFields(spec.Patches, decision.AllowedFields)
			existing, err := e.cfg.Core.Read(ctx, rt, id)
			if err != nil {
				return nil, nil, err
			}
			var root map[string]any
			if err := json.Unmarshal(existing.JSON, &root); err != nil {
				return nil, nil, err
			}
			if err := core.ApplyPathUpdates(rt, root, allowed); err != nil {
				return nil, nil, fmt.Errorf("%w: %v", ErrInvalidInput, err)
			}
			jsonData, err := json.Marshal(root)
			if err != nil {
				return nil, nil, err
			}
			jsonData, err = e.stampWriteJSONForAttribution(jsonData, req)
			if err != nil {
				return nil, nil, err
			}
			if err := e.validateWriteJSON(ctx, rt, id, jsonData); err != nil {
				return nil, nil, err
			}
			bundleEntries = append(bundleEntries, map[string]any{
				"request": map[string]any{
					"method": "PUT",
					"url":    rt + "/" + id,
				},
				"resource": jsonRawObject(jsonData),
			})
			provenanceTargets = append(provenanceTargets, rt+"/"+id)
		default:
			return nil, nil, fmt.Errorf("%w: unsupported bundle method %q", ErrInvalidInput, spec.Method)
		}
	}
	return bundleEntries, provenanceTargets, nil
}

func jsonRawObject(data []byte) any {
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		return map[string]any{}
	}
	return v
}
