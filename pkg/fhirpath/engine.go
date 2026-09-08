package fhirpath

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/degoke/health-ai-stack/pkg/fhirpath/internal/verily"
	"github.com/degoke/health-ai-stack/pkg/proto"
)

type engine struct {
	codec            proto.ProtoCodec
	customFunctions  map[string]any
	cache            *exprCache
	defaultTimeout   time.Duration
	maxExpressionLen int
	maxResultItems   int
}

type compiledExpression struct {
	engine *engine
	expr   string
	inner  *verily.CompiledExpression
}

var _ Engine = (*engine)(nil)
var _ CompiledExpression = (*compiledExpression)(nil)

// NewEngine constructs a FHIRPath engine backed by github.com/verily-src/fhirpath-go.
//
// Soft timeout behavior: when DefaultTimeout is set and the caller context has no
// earlier deadline, evaluation runs under a derived timeout context. Because the
// upstream evaluator has no cancellation hook, timeout enforcement is best-effort:
// the wrapper returns context.DeadlineExceeded when the deadline is reached but
// does not force-stop upstream computation already in flight.
func NewEngine(cfg Config) (Engine, error) {
	if cfg.ProtoCodec == nil {
		cfg.ProtoCodec = proto.NewGoogleR4Codec()
	}
	if cfg.CacheSize == 0 {
		cfg.CacheSize = DefaultCacheSize
	}
	if cfg.MaxExpressionLen == 0 {
		cfg.MaxExpressionLen = DefaultMaxExpressionLen
	}
	if cfg.MaxResultItems == 0 {
		cfg.MaxResultItems = DefaultMaxResultItems
	}
	if cfg.Functions == nil {
		cfg.Functions = map[string]Function{}
	}
	customFunctions, err := adaptFunctions(cfg.Functions, cfg.FunctionArity)
	if err != nil {
		return nil, err
	}
	if err := verily.ValidateCustomFunctions(customFunctions); err != nil {
		return nil, mapVerilyError(err)
	}

	return &engine{
		codec:            cfg.ProtoCodec,
		customFunctions:  customFunctions,
		cache:            newExprCache(cfg.CacheSize),
		defaultTimeout:   cfg.DefaultTimeout,
		maxExpressionLen: cfg.MaxExpressionLen,
		maxResultItems:   cfg.MaxResultItems,
	}, nil
}

func (e *engine) Compile(expr string) (CompiledExpression, error) {
	if err := e.validateExprLen(expr); err != nil {
		return nil, err
	}
	if compiled, ok := e.cache.get(expr); ok {
		return compiled, nil
	}
	inner, err := verily.Compile(expr, e.customFunctions)
	if err != nil {
		return nil, mapVerilyError(err)
	}
	compiled := &compiledExpression{engine: e, expr: expr, inner: inner}
	e.cache.put(expr, compiled)
	return compiled, nil
}

func (e *engine) Eval(ctx context.Context, expr string, resource any) ([]Value, error) {
	return e.EvalWithEnv(ctx, expr, resource, nil)
}

func (e *engine) EvalWithEnv(ctx context.Context, expr string, resource any, env map[string]any) ([]Value, error) {
	compiled, err := e.Compile(expr)
	if err != nil {
		return nil, err
	}
	return compiled.EvalWithEnv(ctx, resource, env)
}

func (e *engine) EvalBool(ctx context.Context, expr string, resource any) (bool, error) {
	compiled, err := e.Compile(expr)
	if err != nil {
		return false, err
	}
	return compiled.EvalBool(ctx, resource)
}

func (e *engine) EvalString(ctx context.Context, expr string, resource any) (string, error) {
	compiled, err := e.Compile(expr)
	if err != nil {
		return "", err
	}
	return compiled.EvalString(ctx, resource)
}

func (c *compiledExpression) Expr() string {
	return c.expr
}

func (c *compiledExpression) Eval(ctx context.Context, resource any) ([]Value, error) {
	return c.EvalWithEnv(ctx, resource, nil)
}

func (c *compiledExpression) EvalWithEnv(ctx context.Context, resource any, env map[string]any) ([]Value, error) {
	items, err := c.engine.evalWithContext(ctx, c.inner, resource, env)
	if err != nil {
		return nil, err
	}
	return valuesFromBackend(items), nil
}

func (c *compiledExpression) EvalBool(ctx context.Context, resource any) (bool, error) {
	items, err := c.engine.evalWithContext(ctx, c.inner, resource, nil)
	if err != nil {
		return false, err
	}
	switch len(items) {
	case 0:
		return false, ErrEmptyResult
	case 1:
		v := NewValue(items[0])
		b, err := v.Bool()
		if err != nil {
			return false, ErrTypeMismatch
		}
		return b, nil
	default:
		return false, ErrNotSingleton
	}
}

func (c *compiledExpression) EvalString(ctx context.Context, resource any) (string, error) {
	items, err := c.engine.evalWithContext(ctx, c.inner, resource, nil)
	if err != nil {
		return "", err
	}
	switch len(items) {
	case 0:
		return "", ErrEmptyResult
	case 1:
		v := NewValue(items[0])
		s, err := v.String()
		if err != nil {
			return "", ErrTypeMismatch
		}
		return s, nil
	default:
		return "", ErrNotSingleton
	}
}

func (e *engine) validateExprLen(expr string) error {
	if len(expr) > e.maxExpressionLen {
		return fmt.Errorf("%w: length %d exceeds %d", ErrExpressionTooLong, len(expr), e.maxExpressionLen)
	}
	return nil
}

func (e *engine) evalWithContext(ctx context.Context, compiled *verily.CompiledExpression, resource any, env map[string]any) ([]any, error) {
	if err := e.validateExprLen(compiled.Expr()); err != nil {
		return nil, err
	}
	evalCtx, cancel := e.evalContext(ctx)
	defer cancel()

	type evalResult struct {
		items []any
		err   error
	}
	done := make(chan evalResult, 1)
	go func() {
		fhirResource, err := verily.ResourceFromInput(resource, e.codec)
		if err != nil {
			done <- evalResult{err: mapVerilyError(err)}
			return
		}
		evalOpts, err := verily.EvalOptionsFromEnv(env, e.codec)
		if err != nil {
			done <- evalResult{err: err}
			return
		}
		collection, err := compiled.Evaluate(fhirResource, evalOpts...)
		if err != nil {
			done <- evalResult{err: mapVerilyError(err)}
			return
		}
		done <- evalResult{items: collection}
	}()

	select {
	case <-evalCtx.Done():
		if errors.Is(evalCtx.Err(), context.DeadlineExceeded) {
			return nil, context.DeadlineExceeded
		}
		return nil, evalCtx.Err()
	case result := <-done:
		if result.err != nil {
			return nil, mapVerilyError(result.err)
		}
		if len(result.items) > e.maxResultItems {
			return nil, fmt.Errorf("%w: got %d items", ErrTooManyResults, len(result.items))
		}
		return result.items, nil
	}
}

func (e *engine) evalContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if e.defaultTimeout <= 0 {
		return ctx, func() {}
	}
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, e.defaultTimeout)
}
