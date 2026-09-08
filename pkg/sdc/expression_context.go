package sdc

import (
	"context"
	"fmt"
	"strings"

	"github.com/degoke/health-ai-stack/pkg/fhirpath"
)

// ExpressionEnvironment carries SDC FHIRPath % constants.
type ExpressionEnvironment struct {
	Resource      any
	Questionnaire any
	Context       any
	QItem         any
	Constants     map[string]any
}

func buildExpressionEnvironment(ctx context.Context, q Questionnaire, r QuestionnaireResponse, opts ValidationOptions) ExpressionEnvironment {
	launch := mergeLaunchContext(q.LaunchContexts, opts.LaunchContext)
	pc := PopulationContext{
		Subject:       opts.Subject,
		LaunchContext: launch,
		Provider:      opts.Expressions,
	}
	vars := map[string]any{}
	if opts.Expressions != nil {
		vars = evaluateQuestionnaireVariables(ctx, q, pc, opts.Expressions)
	}
	env := ExpressionEnvironment{
		Resource:      r,
		Questionnaire: q,
		Constants:     map[string]any{},
	}
	if opts.Subject != nil {
		env.Constants["subject"] = opts.Subject
	}
	for name, value := range launch {
		if name != "" && value != nil {
			env.Constants[name] = value
		}
	}
	for name, value := range vars {
		if name != "" {
			env.Constants[name] = value
		}
	}
	return env
}

func buildPopulationExpressionEnvironment(ctx context.Context, q Questionnaire, pc PopulationContext) ExpressionEnvironment {
	launch := mergeLaunchContext(q.LaunchContexts, pc.LaunchContext)
	vars := map[string]any{}
	if pc.Provider != nil {
		pc.LaunchContext = launch
		vars = evaluateQuestionnaireVariables(ctx, q, pc, pc.Provider)
	}
	env := ExpressionEnvironment{Questionnaire: q, Constants: map[string]any{}}
	if pc.Subject != nil {
		env.Constants["subject"] = pc.Subject
		env.Context = pc.Subject
	}
	for name, value := range launch {
		if name != "" && value != nil {
			env.Constants[name] = value
		}
	}
	for name, value := range vars {
		if name != "" {
			env.Constants[name] = value
		}
	}
	if pc.InitialResponse != nil {
		env.Resource = *pc.InitialResponse
	}
	return env
}

func (e ExpressionEnvironment) withItem(item *Item, response QuestionnaireResponse) ExpressionEnvironment {
	out := e
	if item != nil {
		out.QItem = *item
	}
	if response.ResourceType != "" {
		out.Resource = response
	}
	return out
}

func (e ExpressionEnvironment) envMap() map[string]any {
	env := map[string]any{}
	if e.Resource != nil {
		env["resource"] = e.Resource
	}
	if e.Questionnaire != nil {
		switch q := e.Questionnaire.(type) {
		case Questionnaire:
			env["questionnaire"] = q
		default:
			env["questionnaire"] = e.Questionnaire
		}
	}
	if e.Context != nil {
		env["context"] = e.Context
	}
	if e.QItem != nil {
		switch item := e.QItem.(type) {
		case Item:
			env["qitem"] = item
		default:
			env["qitem"] = e.QItem
		}
	}
	for name, value := range e.Constants {
		env[name] = value
	}
	return env
}

type contextualExpressionProvider struct {
	inner fhirpath.Engine
	base  ExpressionProvider
	env   ExpressionEnvironment
}

func wrapExpressionProvider(ctx context.Context, q Questionnaire, r QuestionnaireResponse, opts ValidationOptions, provider ExpressionProvider) ExpressionProvider {
	if provider == nil {
		return nil
	}
	engine := extractFHIRPathEngine(provider)
	if engine == nil {
		return provider
	}
	return contextualExpressionProvider{
		inner: engine,
		base:  provider,
		env:   buildExpressionEnvironment(ctx, q, r, opts),
	}
}

func wrapPopulationExpressionProvider(ctx context.Context, q Questionnaire, pc PopulationContext, provider ExpressionProvider) ExpressionProvider {
	if provider == nil {
		return nil
	}
	engine := extractFHIRPathEngine(provider)
	if engine == nil {
		return provider
	}
	return contextualExpressionProvider{
		inner: engine,
		base:  provider,
		env:   buildPopulationExpressionEnvironment(ctx, q, pc),
	}
}

func extractFHIRPathEngine(provider ExpressionProvider) fhirpath.Engine {
	switch p := provider.(type) {
	case FHIRPathExpressions:
		return p.Engine
	case contextualExpressionProvider:
		return p.inner
	default:
		return nil
	}
}

func (p contextualExpressionProvider) Evaluate(ctx context.Context, e Expression, input any) ([]any, error) {
	if !strings.EqualFold(e.Language, "text/fhirpath") {
		return p.base.Evaluate(ctx, e, input)
	}
	if p.inner == nil {
		return nil, fmt.Errorf("FHIRPath engine is unavailable")
	}
	root, err := fhirPathInput(input)
	if err != nil {
		return nil, err
	}
	env := map[string]any{}
	for name, value := range p.env.Constants {
		env[name] = value
	}
	switch x := input.(type) {
	case QuestionnaireResponse:
		if envQR, err := responseExpressionEnvelope(x); err == nil {
			env["resource"] = envQR
		}
	case *QuestionnaireResponse:
		if x != nil {
			if envQR, err := responseExpressionEnvelope(*x); err == nil {
				env["resource"] = envQR
			}
		}
	}
	if q, ok := p.env.Questionnaire.(Questionnaire); ok {
		if envQR, err := ProjectionEnvelope(q); err == nil {
			env["questionnaire"] = envQR
		}
	}
	vs, err := p.inner.EvalWithEnv(ctx, e.Expression, root, env)
	if err != nil {
		return nil, err
	}
	out := make([]any, len(vs))
	for i, v := range vs {
		out[i] = fhirPathScalar(v)
	}
	return out, nil
}
