package ai

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/degoke/haistack/pkg/core"
	"github.com/degoke/haistack/pkg/types"
	"github.com/degoke/haistack/pkg/validate"
)

func (e *Executor) execCreateFhirResource(ctx context.Context, req ToolRequest, input map[string]any) (any, []Citation, string, bool, string, []string, error) {
	parsed, err := parseCreateInput(input)
	if err != nil {
		return nil, nil, "", false, "", nil, err
	}
	return e.execPersistWrite(ctx, req, persistWriteParams{
		Operation:    WriteOperationCreate,
		ResourceType: parsed.ResourceType,
		ID:           parsed.ID,
		Allowed:      parsed.Fields,
		PreviewKey:   "fields",
		buildJSON: func(allowed map[string]any) ([]byte, error) {
			return envelopeJSON(parsed.ResourceType, parsed.ID, allowed)
		},
	})
}

func (e *Executor) execUpdateFhirResource(ctx context.Context, req ToolRequest, input map[string]any) (any, []Citation, string, bool, string, []string, error) {
	parsed, err := parseUpdateInput(input)
	if err != nil {
		return nil, nil, "", false, "", nil, err
	}
	return e.execPersistWrite(ctx, req, persistWriteParams{
		Operation:    WriteOperationUpdate,
		ResourceType: parsed.ResourceType,
		ID:           parsed.ID,
		Allowed:      parsed.Patches,
		PreviewKey:   "patches",
		buildJSON: func(allowed map[string]any) ([]byte, error) {
			existing, err := e.cfg.Core.Read(ctx, parsed.ResourceType, parsed.ID)
			if err != nil {
				return nil, err
			}
			var root map[string]any
			if err := json.Unmarshal(existing.JSON, &root); err != nil {
				return nil, err
			}
			if err := core.ApplyPathUpdates(parsed.ResourceType, root, allowed); err != nil {
				return nil, fmt.Errorf("%w: %v", ErrInvalidInput, err)
			}
			return json.Marshal(root)
		},
	})
}

type persistWriteParams struct {
	Operation    string
	ResourceType string
	ID           string
	Allowed      map[string]any
	PreviewKey   string
	buildJSON    func(allowed map[string]any) ([]byte, error)
}

func (e *Executor) execPersistWrite(ctx context.Context, req ToolRequest, params persistWriteParams) (any, []Citation, string, bool, string, []string, error) {
	if e.cfg.Core == nil {
		return nil, nil, "", false, "", nil, fmt.Errorf("%w: core service required for writes", ErrMissingDependency)
	}
	if err := e.ensureWriteValidator(); err != nil {
		return nil, nil, "", false, "", nil, err
	}

	decision, err := e.cfg.Policy.CheckWrite(ctx, WritePolicyRequest{
		Actor: req.Actor, Subject: req.Subject,
		Operation: params.Operation, ResourceType: params.ResourceType,
		ID: params.ID, Fields: params.Allowed,
	})
	if err != nil {
		return nil, nil, "", false, "", nil, err
	}
	if decision == nil {
		return nil, nil, "", false, "", nil, fmt.Errorf("%w: write policy returned no decision", ErrPolicyDenied)
	}
	if !decision.Allowed {
		return nil, nil, "", false, "", nil, fmt.Errorf("%w: write %s %s", ErrPolicyDenied, params.Operation, params.ResourceType)
	}
	for field := range params.Allowed {
		if field == "resourceType" || field == "id" {
			return nil, nil, "", false, "", nil, fmt.Errorf("%w: %q cannot be written", ErrPolicyDenied, field)
		}
	}

	allowed := filterAllowedFields(params.Allowed, decision.AllowedFields)
	jsonData, err := params.buildJSON(allowed)
	if err != nil {
		return nil, nil, "", false, "", nil, err
	}

	jsonData, err = e.stampWriteJSONForAttribution(jsonData, req)
	if err != nil {
		return nil, nil, "", false, "", nil, err
	}

	if err := e.validateWriteJSON(ctx, params.ResourceType, params.ID, jsonData); err != nil {
		return nil, nil, "", false, "", nil, err
	}

	preview := map[string]any{
		"operation":    params.Operation,
		"resourceType": params.ResourceType,
		params.PreviewKey: allowed,
	}
	if params.ID != "" {
		preview["id"] = params.ID
	}

	approvalReq := ApprovalRequest{
		Actor: req.Actor, Subject: req.Subject,
		Operation: params.Operation, ResourceType: params.ResourceType,
		ID: params.ID, Fields: allowed, Preview: preview,
	}
	if decision.RequiresApproval {
		_, approvalToken, approvalOutcome, approvalErr := e.handleWriteApproval(ctx, req, approvalReq)
		if approvalErr != nil {
			return nil, nil, "", false, "", nil, approvalErr
		}
		if approvalOutcome == "approval-required" {
			return preview, nil, approvalOutcome, true, approvalToken, nil, nil
		}
	} else if req.ApprovalToken != "" {
		return nil, nil, "", false, "", nil, fmt.Errorf("%w: approval token supplied for a write that does not require approval", ErrInvalidInput)
	}

	var written *types.ResourceEnvelope
	switch params.Operation {
	case WriteOperationCreate:
		written, err = e.cfg.Core.Create(ctx, &types.ResourceEnvelope{
			ResourceType: params.ResourceType,
			ID:           params.ID,
			JSON:         jsonData,
		})
	case WriteOperationUpdate:
		written, err = e.cfg.Core.Update(ctx, &types.ResourceEnvelope{
			ResourceType: params.ResourceType,
			ID:           params.ID,
			JSON:         jsonData,
		})
	default:
		return nil, nil, "", false, "", nil, fmt.Errorf("%w: unknown operation %q", ErrInvalidInput, params.Operation)
	}
	if err != nil {
		if core.KindOf(err) == core.ErrorKindInvalid {
			return nil, nil, "", false, "", nil, fmt.Errorf("%w: %v", ErrValidationFailed, err)
		}
		return nil, nil, "", false, "", nil, err
	}
	prov := e.recordWriteProvenance(ctx, req, written)
	if prov.Err != nil && !e.cfg.AIAttribution.provenanceBestEffort() {
		return nil, nil, "", false, "", nil, prov.Err
	}

	data := map[string]any{
		"operation":    params.Operation,
		"resourceType": written.ResourceType,
		"id":           written.ID,
		"versionId":    written.VersionID,
	}
	if prov.Warning != "" {
		data["provenanceWarning"] = prov.Warning
	}
	citations := []Citation{e.cfg.Citations.WriteCitation(params.Operation, written.ResourceType, written.ID)}
	return data, citations, "success", false, "", nil, nil
}

func (e *Executor) ensureWriteValidator() error {
	if e.cfg.RequireValidatorOnWrites && e.cfg.Validator == nil {
		return fmt.Errorf("%w: validator required for writes (RequireValidatorOnWrites)", ErrMissingDependency)
	}
	return nil
}

func (e *Executor) validateWriteJSON(ctx context.Context, resourceType, id string, jsonData []byte) error {
	if e.cfg.Validator == nil {
		return nil
	}
	env := &types.ResourceEnvelope{ResourceType: resourceType, ID: id, JSON: jsonData}
	result, valErr := e.cfg.Validator.Validate(ctx, env, validate.ValidateOptions{})
	if valErr != nil {
		return valErr
	}
	if result != nil && !result.Valid {
		return fmt.Errorf("%w: %d issue(s)", ErrValidationFailed, len(result.Issues))
	}
	return nil
}

func (e *Executor) handleWriteApproval(ctx context.Context, req ToolRequest, approvalReq ApprovalRequest) (*ApprovalResult, string, string, error) {
	var approval *ApprovalResult
	var err error
	if req.ApprovalToken != "" {
		if e.cfg.ApprovalStore == nil {
			return nil, "", "", ErrMissingApprovalStore
		}
		if verifyErr := e.cfg.ApprovalStore.VerifyAndConsume(ctx, req.ApprovalToken, approvalReq); verifyErr != nil {
			return nil, "", "", fmt.Errorf("%w: %v", ErrApprovalTokenInvalid, verifyErr)
		}
		approval = &ApprovalResult{Approved: true, Token: req.ApprovalToken}
	} else if e.cfg.Approval != nil {
		approval, err = e.cfg.Approval.RequestApproval(ctx, approvalReq)
		if err != nil {
			return nil, "", "", err
		}
	} else if e.cfg.ApprovalStore != nil {
		token, createErr := e.cfg.ApprovalStore.CreatePending(ctx, approvalReq)
		if createErr != nil {
			return nil, "", "", createErr
		}
		return nil, token, "approval-required", nil
	} else {
		return nil, "", "", ErrMissingApprovalStore
	}
	if approval == nil || !approval.Approved {
		token := ""
		if approval != nil {
			token = approval.Token
		}
		if token == "" && e.cfg.ApprovalStore != nil {
			token, err = e.cfg.ApprovalStore.CreatePending(ctx, approvalReq)
			if err != nil {
				return nil, "", "", err
			}
		}
		return nil, token, "approval-required", nil
	}
	if approval.Token == "" {
		return nil, "", "", ErrApprovalTokenRequired
	}
	if req.ApprovalToken == "" {
		if e.cfg.ApprovalStore == nil {
			return nil, "", "", ErrMissingApprovalStore
		}
		if verifyErr := e.cfg.ApprovalStore.VerifyAndConsume(ctx, approval.Token, approvalReq); verifyErr != nil {
			return nil, "", "", fmt.Errorf("%w: %v", ErrApprovalTokenInvalid, verifyErr)
		}
	}
	return approval, "", "approved", nil
}
