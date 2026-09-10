package fhirpath

import "context"

type evaluationResourceKey struct{}

// WithEvaluationResource attaches the resource currently under evaluation to ctx.
// resolve() uses it to resolve contained #fragment references.
func WithEvaluationResource(ctx context.Context, resource any) context.Context {
	if resource == nil {
		return ctx
	}
	return context.WithValue(ctx, evaluationResourceKey{}, resource)
}

// EvaluationResource returns the resource attached by WithEvaluationResource.
func EvaluationResource(ctx context.Context) any {
	if ctx == nil {
		return nil
	}
	return ctx.Value(evaluationResourceKey{})
}
