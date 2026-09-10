package fhirpath

import (
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/degoke/health-ai-stack/pkg/fhirpath/internal/verily"
	"github.com/verily-src/fhirpath-go/fhirpath/system"
)

func adaptFunctions(functions map[string]Function, arity map[string]int) (map[string]any, error) {
	if len(functions) == 0 {
		return nil, nil
	}
	out := make(map[string]any, len(functions))
	for name, fn := range functions {
		n := 0
		if arity != nil {
			n = arity[name]
		}
		adapted, err := adaptFunction(fn, n)
		if err != nil {
			return nil, fmt.Errorf("fhirpath: custom function %q: %w", name, err)
		}
		out[name] = adapted
	}
	return out, nil
}

func adaptFunction(fn Function, arity int) (any, error) {
	if arity < 0 || arity > MaxCustomFunctionArity {
		return nil, fmt.Errorf("arity %d out of range 0-%d", arity, MaxCustomFunctionArity)
	}

	inTypes := make([]reflect.Type, 1+arity)
	inTypes[0] = reflect.TypeOf(system.Collection(nil))
	anyType := reflect.TypeOf((*any)(nil)).Elem()
	for i := 1; i <= arity; i++ {
		inTypes[i] = anyType
	}
	funcType := reflect.FuncOf(inTypes, []reflect.Type{
		reflect.TypeOf(system.Collection(nil)),
		reflect.TypeOf((*error)(nil)).Elem(),
	}, false)

	handler := reflect.MakeFunc(funcType, func(args []reflect.Value) []reflect.Value {
		input := args[0].Interface().(system.Collection)
		fnArgs := make([]any, arity)
		for i := 0; i < arity; i++ {
			fnArgs[i] = args[i+1].Interface()
		}
		out, err := invokeFunction(fn, input, fnArgs...)
		if err != nil {
			return []reflect.Value{reflect.Zero(reflect.TypeOf(system.Collection(nil))), reflect.ValueOf(err)}
		}
		return []reflect.Value{reflect.ValueOf(out), reflect.Zero(reflect.TypeOf((*error)(nil)).Elem())}
	})
	return handler.Interface(), nil
}

func invokeFunction(fn Function, input system.Collection, args ...any) (system.Collection, error) {
	in := adaptCollectionFromBackend(input)
	argCols := make([]Collection, len(args))
	for i, arg := range args {
		argCols[i] = Collection{NewValue(arg)}
	}
	out, err := fn(in, argCols...)
	if err != nil {
		return nil, err
	}
	return system.Collection(backendFromCollection(out)), nil
}

func adaptCollectionFromBackend(items system.Collection) Collection {
	out := make(Collection, len(items))
	for i, item := range items {
		out[i] = NewValue(item)
	}
	return out
}

func mapVerilyError(err error) error {
	return mapVerilyErrorWithConfig(err, false, false)
}

func mapVerilyErrorWithConfig(err error, resolveConfigured, terminologyConfigured bool) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, verily.ErrShadowsBuiltin) {
		return ErrShadowsBuiltin
	}
	if errors.Is(err, verily.ErrInvalidInput) {
		return ErrInvalidInput
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "resolve() function requires"):
		if resolveConfigured {
			return err
		}
		return fmt.Errorf("%w: resolve()", ErrNotSupported)
	case strings.Contains(msg, "memberOf() function requires"):
		if terminologyConfigured {
			return err
		}
		return fmt.Errorf("%w: terminology", ErrNotSupported)
	case strings.Contains(msg, "function identifier can't be resolved: memberOf"):
		if terminologyConfigured {
			return err
		}
		return fmt.Errorf("%w: terminology", ErrNotSupported)
	}
	if errors.Is(err, verily.ErrNotSupported) {
		return ErrNotSupported
	}
	return err
}
