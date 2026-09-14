package fhirpath

import (
	"context"
	"time"

	"github.com/degoke/health-ai-stack/pkg/proto"
)

// Engine evaluates FHIRPath expressions against a single in-memory resource.
type Engine interface {
	Compile(expr string) (CompiledExpression, error)
	Eval(ctx context.Context, expr string, resource any) ([]Value, error)
	EvalWithEnv(ctx context.Context, expr string, resource any, env map[string]any) ([]Value, error)
	EvalBool(ctx context.Context, expr string, resource any) (bool, error)
	EvalString(ctx context.Context, expr string, resource any) (string, error)
}

// CompiledExpression is a compiled FHIRPath expression safe for concurrent evaluation.
type CompiledExpression interface {
	Expr() string
	Eval(ctx context.Context, resource any) ([]Value, error)
	EvalWithEnv(ctx context.Context, resource any, env map[string]any) ([]Value, error)
	EvalBool(ctx context.Context, resource any) (bool, error)
	EvalString(ctx context.Context, resource any) (string, error)
}

// Collection is the FHIRPath result type: an ordered list of values.
type Collection []Value

// Function is a custom FHIRPath function registered at engine construction time.
// The first argument is the input collection; remaining arguments are evaluated
// expression results passed as collections (one collection per FHIRPath argument).
//
// Set Config.FunctionArity[name] to the number of FHIRPath arguments the function
// accepts (0 for niladic calls). Each FHIRPath argument must evaluate to a
// singleton upstream; the adapter wraps that value as a one-item Collection.
type Function func(input Collection, args ...Collection) (Collection, error)

// Config configures a FHIRPath engine.
type Config struct {
	ProtoCodec       proto.ProtoCodec
	Functions        map[string]Function
	FunctionArity    map[string]int
	CacheSize        int
	DefaultTimeout   time.Duration
	MaxExpressionLen int
	MaxResultItems   int
	// Resolve enables resolve() by looking up reference strings at evaluation time.
	Resolve ResolveFunc
	// Terminology enables memberOf() via the experimental FHIRPath function table.
	Terminology TerminologyValidator
}

// ResolveFunc resolves one FHIR reference string to a resource envelope or proto.
type ResolveFunc func(ctx context.Context, ref string) (any, error)

// TerminologyValidator validates whether a code is a member of a value set.
type TerminologyValidator interface {
	MemberOf(ctx context.Context, valueSetURL, system, code string) (bool, error)
}

// DefaultCacheSize is the default compile-cache capacity.
const DefaultCacheSize = 256

// DefaultMaxExpressionLen is the default maximum FHIRPath source length.
const DefaultMaxExpressionLen = 4096

// DefaultMaxResultItems is the default maximum number of items in a result collection.
const DefaultMaxResultItems = 1024

// MaxCustomFunctionArity is the maximum number of FHIRPath arguments a custom function may declare.
const MaxCustomFunctionArity = 8
