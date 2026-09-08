package verily

import (
	"fmt"
	"strings"

	verilyfhirpath "github.com/verily-src/fhirpath-go/fhirpath"
	"github.com/verily-src/fhirpath-go/fhirpath/compopts"
	"github.com/verily-src/fhirpath-go/fhirpath/system"
)

// CompiledExpression wraps a Verily FHIRPath expression.
type CompiledExpression struct {
	expr *verilyfhirpath.Expression
}

// Expr returns the original FHIRPath source.
func (c *CompiledExpression) Expr() string {
	return c.expr.String()
}

// Evaluate runs the expression against a single FHIR resource root.
func (c *CompiledExpression) Evaluate(resource verilyfhirpath.Resource, options ...verilyfhirpath.EvaluateOption) (system.Collection, error) {
	return c.expr.Evaluate([]verilyfhirpath.Resource{resource}, options...)
}

// Compile parses and compiles a FHIRPath expression with optional custom functions.
// Each custom function value must satisfy a Verily signature of the form
// func(system.Collection, ...any) (system.Collection, error) with a fixed arity.
func Compile(expr string, functions map[string]any) (*CompiledExpression, error) {
	opts, err := compileOptions(functions)
	if err != nil {
		return nil, err
	}
	compiled, err := verilyfhirpath.Compile(expr, opts...)
	if err != nil {
		return nil, err
	}
	return &CompiledExpression{expr: compiled}, nil
}

// ValidateCustomFunctions checks that custom function names do not shadow built-ins.
func ValidateCustomFunctions(functions map[string]any) error {
	for name := range functions {
		_, err := verilyfhirpath.Compile("true", compopts.AddFunction(name, functions[name]))
		if err == nil {
			continue
		}
		if strings.Contains(err.Error(), "already exists in default table") {
			return fmt.Errorf("custom function %q shadows built-in: %w", name, ErrShadowsBuiltin)
		}
	}
	return nil
}

func compileOptions(functions map[string]any) ([]verilyfhirpath.CompileOption, error) {
	if len(functions) == 0 {
		return nil, nil
	}
	opts := make([]verilyfhirpath.CompileOption, 0, len(functions))
	for name, fn := range functions {
		opts = append(opts, compopts.AddFunction(name, fn))
	}
	return opts, nil
}
