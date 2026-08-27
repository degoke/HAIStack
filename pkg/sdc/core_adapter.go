package sdc

import (
	"context"
	"fmt"

	"github.com/degoke/health-ai-stack/pkg/types"
	"github.com/degoke/health-ai-stack/pkg/validate"
)

// ResponseValidator validates QuestionnaireResponse envelopes with SDC rules.
// An optional base validator runs first for structural FHIR validation.
type ResponseValidator struct {
	Base     validate.Validator
	Resolver QuestionnaireResolver
	Options  ValidationOptions
}

// ValidateResource runs base validation when configured, then SDC response validation.
func (v *ResponseValidator) ValidateResource(ctx context.Context, resource *types.ResourceEnvelope) error {
	if resource == nil {
		return fmt.Errorf("resource envelope is nil")
	}
	if v.Base != nil {
		if err := v.Base.ValidateResource(ctx, resource); err != nil {
			return err
		}
	}
	if resource.ResourceType != "QuestionnaireResponse" {
		return nil
	}
	qenv, err := v.resolveQuestionnaire(ctx, resource)
	if err != nil {
		return ValidationError{Outcome: failed(err)}
	}
	if qenv == nil {
		return nil
	}
	outcome := ValidateQuestionnaireResponseResource(ctx, qenv, resource, v.Options)
	return ErrFromOutcome(outcome)
}

func (v *ResponseValidator) resolveQuestionnaire(ctx context.Context, response *types.ResourceEnvelope) (*types.ResourceEnvelope, error) {
	r, err := DecodeQuestionnaireResponseResource(response)
	if err != nil {
		return nil, err
	}
	if r.Questionnaire == "" {
		return nil, nil
	}
	if v.Resolver == nil {
		return nil, fmt.Errorf("questionnaire resolver is required for QuestionnaireResponse validation")
	}
	q, err := v.Resolver.Resolve(ctx, r.Questionnaire)
	if err != nil {
		return nil, err
	}
	return ProjectionEnvelope(q)
}
