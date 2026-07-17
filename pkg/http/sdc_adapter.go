package http

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/degoke/health-ai-stack/pkg/core"
	"github.com/degoke/health-ai-stack/pkg/sdc"
	"github.com/degoke/health-ai-stack/pkg/types"
)

// CoreSDCService is the default runtime adapter. It reuses core persistence,
// store-backed canonical resolution, and caller-supplied SDC policies.
type CoreSDCService struct {
	Resources      *core.ResourceService
	Resolver       sdc.QuestionnaireResolver
	Provider       sdc.ExpressionProvider
	Extractor      sdc.Extractor
	AdaptiveEngine sdc.AdaptiveEngine
}

func (a CoreSDCService) questionnaire(ctx context.Context, req SDCRequest) (sdc.Questionnaire, error) {
	qenv := req.Questionnaire
	if qenv == nil && req.Body != nil && req.Body.ResourceType == "Questionnaire" {
		qenv = req.Body
	}
	if qenv == nil && req.Body != nil && req.Body.ResourceType == "Parameters" {
		var p struct {
			Parameter []struct {
				Name           string          `json:"name"`
				ValueCanonical string          `json:"valueCanonical,omitempty"`
				Resource       json.RawMessage `json:"resource,omitempty"`
			} `json:"parameter"`
		}
		if json.Unmarshal(req.Body.JSON, &p) == nil {
			for _, param := range p.Parameter {
				if param.Name != "questionnaire" {
					continue
				}
				if len(param.Resource) > 0 {
					var probe struct {
						ResourceType string `json:"resourceType"`
					}
					if json.Unmarshal(param.Resource, &probe) == nil && probe.ResourceType == "Questionnaire" {
						if env, e := types.NewJSONCodec().ParseJSON("Questionnaire", param.Resource); e == nil {
							qenv = env
						}
						break
					}
				}
				if param.ValueCanonical != "" && a.Resolver != nil {
					return a.Resolver.Resolve(ctx, param.ValueCanonical)
				}
			}
		}
	}
	if qenv == nil && req.Query.Get("questionnaire") != "" && a.Resolver != nil {
		return a.Resolver.Resolve(ctx, req.Query.Get("questionnaire"))
	}
	if qenv == nil {
		return sdc.Questionnaire{}, fmt.Errorf("Questionnaire input is required")
	}
	return sdc.DecodeQuestionnaireResource(qenv)
}
func (a CoreSDCService) response(req SDCRequest) (sdc.QuestionnaireResponse, error) {
	if req.QuestionnaireResponse != nil {
		return sdc.DecodeQuestionnaireResponseResource(req.QuestionnaireResponse)
	}
	if req.Body != nil && req.Body.ResourceType == "QuestionnaireResponse" {
		return sdc.DecodeQuestionnaireResponseResource(req.Body)
	}
	return sdc.QuestionnaireResponse{}, fmt.Errorf("QuestionnaireResponse input is required")
}
func (a CoreSDCService) Populate(ctx context.Context, req SDCRequest) (*types.ResourceEnvelope, error) {
	q, e := a.questionnaire(ctx, req)
	if e != nil {
		return nil, e
	}
	var initial *sdc.QuestionnaireResponse
	if req.Body != nil && req.Body.ResourceType == "QuestionnaireResponse" {
		r, e := sdc.DecodeQuestionnaireResponseResource(req.Body)
		if e != nil {
			return nil, e
		}
		initial = &r
	}
	r, o := sdc.Populate(ctx, q, sdc.PopulationContext{InitialResponse: initial, Provider: a.Provider})
	if len(o.Issue) > 0 {
		return nil, o
	}
	return sdc.ResponseProjectionEnvelope(*r)
}
func (a CoreSDCService) Validate(ctx context.Context, req SDCRequest) (*types.OperationOutcome, error) {
	r, e := a.response(req)
	if e != nil {
		return nil, e
	}
	q, e := a.questionnaire(ctx, req)
	if e != nil && a.Resolver != nil && r.Questionnaire != "" {
		q, e = a.Resolver.Resolve(ctx, r.Questionnaire)
	}
	if e != nil {
		return nil, e
	}
	o := sdc.ValidateResponse(q, r, sdc.ValidationOptions{Expressions: a.Provider})
	return toOperationOutcome(o), nil
}
func (a CoreSDCService) Extract(ctx context.Context, req SDCRequest) (*types.ResourceEnvelope, error) {
	if a.Extractor == nil {
		return nil, &core.ServiceError{Kind: core.ErrorKindNotSupported, Message: "SDC extraction adapter is unavailable"}
	}
	q, e := a.questionnaire(ctx, req)
	if e != nil {
		return nil, e
	}
	r, e := a.response(req)
	if e != nil {
		return nil, e
	}
	result, e := a.Extractor.Extract(ctx, q, r)
	if e != nil {
		return nil, e
	}
	return result.Bundle, nil
}
func (a CoreSDCService) Assemble(ctx context.Context, req SDCRequest) (*types.ResourceEnvelope, error) {
	q, e := a.questionnaire(ctx, req)
	if e != nil {
		return nil, e
	}
	assembled, o := sdc.Assembler{Resolver: a.Resolver}.Assemble(ctx, q)
	if len(o.Issue) > 0 {
		return nil, o
	}
	return sdc.ProjectionEnvelope(assembled)
}
func (a CoreSDCService) Adaptive(ctx context.Context, op string, req SDCRequest) (*types.ResourceEnvelope, error) {
	if a.AdaptiveEngine == nil {
		return nil, &core.ServiceError{Kind: core.ErrorKindNotSupported, Message: "adaptive SDC adapter is unavailable"}
	}
	return nil, &core.ServiceError{Kind: core.ErrorKindNotSupported, Message: fmt.Sprintf("adaptive operation %s requires a session adapter", op)}
}
func toOperationOutcome(o sdc.Outcome) *types.OperationOutcome {
	out := &types.OperationOutcome{ResourceType: "OperationOutcome"}
	for _, i := range o.Issue {
		out.Issue = append(out.Issue, types.OperationIssue{Severity: i.Severity, Code: i.Code, Diagnostics: i.Diagnostics, Expression: i.Expression})
	}
	return out
}
