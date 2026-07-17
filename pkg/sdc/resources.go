package sdc

// This file is the canonical resource boundary for SDC. SDC operations accept
// and return ResourceEnvelope values, matching the rest of haistack. The small
// questionnaire projections in types.go are only an internal JSON view used
// by the behavior engine; they are never a second persistence model.

import (
	"context"
	"fmt"

	"github.com/degoke/health-ai-stack/pkg/proto"
	"github.com/degoke/health-ai-stack/pkg/types"
)

// QuestionnaireResource validates and returns an existing canonical Questionnaire envelope.
func QuestionnaireResource(env *types.ResourceEnvelope) (*types.ResourceEnvelope, error) {
	if err := requireResource(env, "Questionnaire"); err != nil {
		return nil, err
	}
	return env, nil
}

// QuestionnaireResponseResource validates and returns an existing canonical QuestionnaireResponse envelope.
func QuestionnaireResponseResource(env *types.ResourceEnvelope) (*types.ResourceEnvelope, error) {
	if err := requireResource(env, "QuestionnaireResponse"); err != nil {
		return nil, err
	}
	return env, nil
}

// ProjectionEnvelope is a compatibility helper for JSON behavior projections.
// New code should construct envelopes through types.JSONCodec or proto.ToEnvelope.
func ProjectionEnvelope(q Questionnaire) (*types.ResourceEnvelope, error) {
	return Envelope("Questionnaire", q.ID, q)
}
func ResponseProjectionEnvelope(r QuestionnaireResponse) (*types.ResourceEnvelope, error) {
	return Envelope("QuestionnaireResponse", r.ID, r)
}

// DecodeQuestionnaireResource validates and decodes an envelope's canonical JSON
// into the package's internal behavior projection.
func DecodeQuestionnaireResource(env *types.ResourceEnvelope) (Questionnaire, error) {
	if err := requireResource(env, "Questionnaire"); err != nil {
		return Questionnaire{}, err
	}
	return DecodeQuestionnaire(env.JSON)
}
func DecodeQuestionnaireResponseResource(env *types.ResourceEnvelope) (QuestionnaireResponse, error) {
	if err := requireResource(env, "QuestionnaireResponse"); err != nil {
		return QuestionnaireResponse{}, err
	}
	return DecodeResponse(env.JSON)
}

func ValidateQuestionnaireResource(_ context.Context, env *types.ResourceEnvelope, opts ValidationOptions) Outcome {
	q, e := DecodeQuestionnaireResource(env)
	if e != nil {
		return failed(e)
	}
	return ValidateQuestionnaire(q, opts)
}
func ValidateQuestionnaireResponseResource(_ context.Context, qenv, renv *types.ResourceEnvelope, opts ValidationOptions) Outcome {
	q, e := DecodeQuestionnaireResource(qenv)
	if e != nil {
		return failed(e)
	}
	r, e := DecodeQuestionnaireResponseResource(renv)
	if e != nil {
		return failed(e)
	}
	return ValidateResponse(q, r, opts)
}

// PopulateResource executes SDC population and returns a canonical response
// envelope without persisting it.
func PopulateResource(ctx context.Context, qenv *types.ResourceEnvelope, pc PopulationContext) (*types.ResourceEnvelope, Outcome) {
	q, e := DecodeQuestionnaireResource(qenv)
	if e != nil {
		return nil, failed(e)
	}
	r, o := Populate(ctx, q, pc)
	if r == nil {
		return nil, o
	}
	env, e := ResponseProjectionEnvelope(*r)
	if e != nil {
		return nil, failed(e)
	}
	return env, o
}

// ExtractResource converts an extraction result into the canonical Bundle
// envelope used by core transaction processing. It has no persistence side effects.
func ExtractResource(ctx context.Context, qenv, renv *types.ResourceEnvelope, x Extractor) (*types.ResourceEnvelope, []ExtractionDiagnostic, error) {
	if x == nil {
		return nil, nil, fmt.Errorf("extractor is nil")
	}
	q, e := DecodeQuestionnaireResource(qenv)
	if e != nil {
		return nil, nil, e
	}
	r, e := DecodeQuestionnaireResponseResource(renv)
	if e != nil {
		return nil, nil, e
	}
	result, e := x.Extract(ctx, q, r)
	if e != nil {
		return nil, result.Diagnostics, e
	}
	if result.Bundle == nil {
		return nil, result.Diagnostics, fmt.Errorf("extractor returned a nil Bundle envelope")
	}
	return result.Bundle, result.Diagnostics, nil
}

// ParseR4 returns the generated Google R4 representation through the existing
// proto adapter. Callers should generally retain the envelope as the canonical
// value and use this only when typed protobuf access is required.
func ParseR4(env *types.ResourceEnvelope) (any, error) {
	if env == nil {
		return nil, fmt.Errorf("resource envelope is nil")
	}
	return proto.NewGoogleR4Codec().ParseJSONToProto(env.ResourceType, env.JSON)
}
func requireResource(env *types.ResourceEnvelope, typ string) error {
	if env == nil {
		return fmt.Errorf("%s envelope is nil", typ)
	}
	if env.ResourceType != "" && env.ResourceType != typ {
		return fmt.Errorf("expected %s, got %s", typ, env.ResourceType)
	}
	return nil
}
