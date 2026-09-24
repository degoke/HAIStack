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
	specs, err := parseTransactionInput(input)
	if err != nil {
		return nil, nil, "", false, "", nil, err
	}
	return e.execTransactionSpecs(ctx, req, specs, true)
}

func (e *Executor) execTransactionSpecs(ctx context.Context, req ToolRequest, specs []transactionEntrySpec, userInitiated bool) (any, []Citation, string, bool, string, []string, error) {
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
		op, fields, err := transactionEntryPolicyFields(spec)
		if err != nil {
			return nil, nil, "", false, "", nil, err
		}
		if err := validateWriteMapKeys(fields); err != nil {
			return nil, nil, "", false, "", nil, err
		}
		decision, err := e.cfg.Policy.CheckWrite(ctx, WritePolicyRequest{
			Actor: req.Actor, Subject: req.Subject,
			Operation: op, ResourceType: spec.ResourceType,
			ID: spec.ID, Fields: fields,
		})
		if err != nil {
			return nil, nil, "", false, "", nil, err
		}
		if decision == nil || !decision.Allowed {
			return nil, nil, "", false, "", nil, fmt.Errorf("%w: transaction entry %d %s %s", ErrPolicyDenied, i, op, spec.ResourceType)
		}
		if decision.RequiresApproval {
			requiresApproval = true
		}
		for k, v := range fields {
			approvalFields[fmt.Sprintf("%d.%s", i, k)] = v
		}
	}

	if userInitiated && requiresApproval {
		approvalReq := ApprovalRequest{
			Actor: req.Actor, Subject: req.Subject,
			Operation: "transaction", ResourceType: "Bundle",
			Fields: approvalFields, Preview: preview,
		}
		if _, token, outcome, approvalErr := e.handleWriteApproval(ctx, req, approvalReq); approvalErr != nil {
			return nil, nil, "", false, "", nil, approvalErr
		} else if outcome == "approval-required" {
			return preview, nil, outcome, true, token, nil, nil
		}
	}

	bundleEntries, writtenTargets, err := e.buildTransactionBundleEntries(ctx, req, specs)
	if err != nil {
		return nil, nil, "", false, "", nil, err
	}
	if e.cfg.AIAttribution.Enabled && e.cfg.AIAttribution.createProvenance() {
		for _, target := range writtenTargets {
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
		"type":         "transaction",
		"entry":        bundleEntries,
	})
	if err != nil {
		return nil, nil, "", false, "", nil, err
	}

	resp, err := e.cfg.Core.ProcessTransactionBundle(ctx, &types.ResourceEnvelope{
		ResourceType: "Bundle",
		JSON:         bundleJSON,
	})
	if err != nil {
		if core.KindOf(err) == core.ErrorKindInvalid {
			return nil, nil, "", false, "", nil, fmt.Errorf("%w: %v", ErrValidationFailed, err)
		}
		return nil, nil, "", false, "", nil, err
	}

	citations := []Citation{e.cfg.Citations.WriteCitation("transaction", "Bundle", "")}
	data := map[string]any{
		"operation":    "transaction",
		"resourceType": "Bundle",
		"entryCount":   len(bundleEntries),
	}
	if resp != nil && len(resp.JSON) > 0 {
		data["response"] = jsonRawObject(resp.JSON)
	}
	return data, citations, "success", false, "", nil, nil
}

func transactionEntryPolicyFields(spec transactionEntrySpec) (operation string, fields map[string]any, err error) {
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
		return "", nil, fmt.Errorf("%w: unsupported transaction method %q (use POST or PUT)", ErrInvalidInput, spec.Method)
	}
}

func (e *Executor) buildTransactionBundleEntries(ctx context.Context, req ToolRequest, specs []transactionEntrySpec) ([]map[string]any, []string, error) {
	var bundleEntries []map[string]any
	var provenanceTargets []string
	for _, spec := range specs {
		method := strings.ToUpper(strings.TrimSpace(spec.Method))
		rt := strings.TrimSpace(spec.ResourceType)
		switch method {
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
			return nil, nil, fmt.Errorf("%w: unsupported transaction method %q", ErrInvalidInput, spec.Method)
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
