package cql

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/degoke/health-ai-stack/pkg/fhirpath"
	"github.com/degoke/health-ai-stack/pkg/types"
	"github.com/google/fhir/go/fhirversion"
	"github.com/google/fhir/go/jsonformat"
	gproto "google.golang.org/protobuf/proto"
)

type evalState struct {
	ctx        context.Context
	engine     *Engine
	patient    any
	params     map[string]any
	libraries  []*Library
	current    *Library
	now        time.Time
	retriever  Retriever
	this       any
	thisSet    bool
	stack      map[string][]any
	evaluating map[string]bool
}

func (e *Engine) evalNode(ctx context.Context, n Node, env EvalContext, libs []*Library) ([]any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	resolved, err := e.resolveIncludes(ctx, libs)
	if err != nil {
		return nil, err
	}
	libs = resolved
	st := &evalState{
		ctx:        ctx,
		engine:     e,
		patient:    env.Patient,
		params:     env.Parameters,
		libraries:  libs,
		now:        env.Now,
		retriever:  env.Retriever,
		stack:      map[string][]any{},
		evaluating: map[string]bool{},
	}
	if st.now.IsZero() {
		st.now = e.clock()
	}
	if st.retriever == nil {
		st.retriever = e.retriever
	}
	if st.params == nil {
		st.params = map[string]any{}
	}
	if len(libs) > 0 {
		st.current = libs[0]
	}
	if err := st.applyParameterDefaults(); err != nil {
		return nil, err
	}
	return st.eval(n)
}

func (st *evalState) eval(n Node) ([]any, error) {
	if err := st.ctx.Err(); err != nil {
		return nil, err
	}
	if n == nil {
		return nil, nil
	}
	switch x := n.(type) {
	case *litNode:
		if x.value == nil {
			return nil, nil
		}
		return []any{x.value}, nil
	case *identNode:
		return st.evalIdent(x.name)
	case *unaryNode:
		return st.evalUnary(x)
	case *binaryNode:
		return st.evalBinary(x)
	case *memberNode:
		return st.evalMember(x)
	case *callNode:
		return st.evalCall(x)
	case *listNode:
		var out []any
		for _, el := range x.elems {
			v, err := st.eval(el)
			if err != nil {
				return nil, err
			}
			out = append(out, v...)
		}
		return out, nil
	case *tupleNode:
		obj := map[string]any{}
		for _, f := range x.fields {
			v, err := st.eval(f.value)
			if err != nil {
				return nil, err
			}
			obj[f.name] = singletonOrList(v)
		}
		return []any{obj}, nil
	case *ifNode:
		cond, err := st.eval(x.cond)
		if err != nil {
			return nil, err
		}
		tb := asBool(cond)
		if tb == nil {
			return nil, nil
		}
		if *tb {
			return st.eval(x.thenN)
		}
		return st.eval(x.elseN)
	case *retrieveNode:
		return st.evalRetrieve(x)
	case *isNode:
		v, err := st.eval(x.x)
		if err != nil {
			return nil, err
		}
		isNull := len(v) == 0 || (len(v) == 1 && v[0] == nil)
		result := isNull
		if !strings.EqualFold(x.target, "null") {
			if isNull {
				result = false
			} else {
				result = typeName(v[0]) == x.target || strings.EqualFold(typeName(v[0]), x.target)
			}
		}
		if x.not {
			result = !result
		}
		return []any{result}, nil
	case *quantityNode:
		return st.evalQuantity(x)
	case *intervalNode:
		return st.evalInterval(x)
	case *betweenNode:
		return st.evalBetween(x)
	case *durationNode:
		return st.evalDuration(x)
	case *caseNode:
		return st.evalCase(x)
	case *queryNode:
		return st.evalQuery(x)
	case *indexNode:
		return st.evalIndex(x)
	case *convertNode:
		return st.evalConvert(x)
	case *codeLitNode:
		c := Code{Code: x.code, System: x.system, Display: x.display}
		if st.current != nil {
			for _, cs := range st.current.CodeSystems {
				if cs.Name == c.System || strings.EqualFold(cs.Name, c.System) {
					c.System = cs.URL
					break
				}
			}
		}
		return []any{c}, nil
	default:
		return nil, errf("%w: unknown expression node", ErrUnsupported)
	}
}

func (st *evalState) evalIdent(name string) ([]any, error) {
	if name == "" {
		return nil, errf("CQL identifier is empty")
	}
	if strings.EqualFold(name, "$this") && st.thisSet {
		if st.this == nil {
			return nil, nil
		}
		return []any{st.this}, nil
	}
	if v, ok := st.stackValues(name); ok {
		return v, nil
	}
	if st.thisSet {
		if v, ok := st.memberValues(st.this, name); ok {
			return v, nil
		}
	}
	if def, lib := st.lookupDefine(name); def != nil {
		return st.evalDefine(lib, *def)
	}
	if fn, lib := st.lookupFunction(name); fn != nil && len(fn.Params) == 0 {
		return st.evalUserFunction(lib, fn, nil)
	}
	if st.params != nil {
		if v, ok := paramLookup(st.params, name); ok {
			return wrapValue(v), nil
		}
	}
	if strings.EqualFold(name, "Patient") || strings.EqualFold(name, "Unfiltered") {
		if st.patient == nil {
			return nil, ErrMissingContext
		}
		return []any{st.patient}, nil
	}
	if strings.EqualFold(name, "FHIRHelpers") || strings.EqualFold(name, "System") {
		return []any{builtinNS{name: name}}, nil
	}
	if vs := st.lookupValueSet(name); vs != nil {
		return []any{*vs}, nil
	}
	if c := st.lookupCode(name); c != nil {
		return []any{*c}, nil
	}
	if lib := st.lookupLibrary(name); lib != nil {
		if isFHIRHelpersLibrary(lib) {
			return []any{builtinNS{name: "FHIRHelpers"}}, nil
		}
		return []any{lib}, nil
	}
	if !st.thisSet && st.patient != nil {
		if v, ok := st.memberValues(st.patient, name); ok {
			return v, nil
		}
	}
	return nil, errf("%w: %s", ErrExpressionNotFound, name)
}

func paramLookup(params map[string]any, name string) (any, bool) {
	if params == nil {
		return nil, false
	}
	if v, ok := params[name]; ok {
		return v, true
	}
	for k, v := range params {
		if strings.EqualFold(k, name) {
			return v, true
		}
	}
	return nil, false
}

func (st *evalState) applyParameterDefaults() error {
	seen := map[string]bool{}
	for k := range st.params {
		seen[k] = true
		seen[strings.ToLower(k)] = true
	}
	apply := func(lib *Library) error {
		if lib == nil {
			return nil
		}
		for _, p := range lib.Parameters {
			if seen[p.Name] || seen[strings.ToLower(p.Name)] {
				continue
			}
			if p.Default == nil {
				continue
			}
			v, err := st.eval(p.Default)
			if err != nil {
				return err
			}
			st.params[p.Name] = singletonOrList(v)
			seen[p.Name] = true
			seen[strings.ToLower(p.Name)] = true
		}
		return nil
	}
	if err := apply(st.current); err != nil {
		return err
	}
	for _, lib := range st.libraries {
		if err := apply(lib); err != nil {
			return err
		}
	}
	return nil
}

func (st *evalState) lookupDefine(name string) (*Define, *Library) {
	search := func(lib *Library) *Define {
		if lib == nil {
			return nil
		}
		for i := range lib.Defines {
			if lib.Defines[i].Name == name {
				return &lib.Defines[i]
			}
		}
		for i := range lib.Defines {
			if strings.EqualFold(lib.Defines[i].Name, name) {
				return &lib.Defines[i]
			}
		}
		return nil
	}
	if def := search(st.current); def != nil {
		return def, st.current
	}
	for _, lib := range st.libraries {
		if def := search(lib); def != nil {
			return def, lib
		}
	}
	return nil, nil
}

func (st *evalState) lookupFunction(name string) (*Function, *Library) {
	search := func(lib *Library) *Function {
		if lib == nil {
			return nil
		}
		for i := range lib.Functions {
			if lib.Functions[i].Name == name {
				return &lib.Functions[i]
			}
		}
		for i := range lib.Functions {
			if strings.EqualFold(lib.Functions[i].Name, name) {
				return &lib.Functions[i]
			}
		}
		return nil
	}
	if fn := search(st.current); fn != nil {
		return fn, st.current
	}
	for _, lib := range st.libraries {
		if fn := search(lib); fn != nil {
			return fn, lib
		}
	}
	return nil, nil
}

func (st *evalState) evalUserFunction(lib *Library, fn *Function, args [][]any) ([]any, error) {
	if fn == nil {
		return nil, errf("%w: function is nil", ErrExpressionNotFound)
	}
	if len(args) != len(fn.Params) {
		return nil, errf("CQL function %q expected %d arguments, got %d", fn.Name, len(fn.Params), len(args))
	}
	key := "fn:" + fn.Name
	if lib != nil && lib.Name != "" {
		key = "fn:" + lib.Name + "." + fn.Name
	}
	if st.evaluating[key] {
		return nil, errf("CQL function %q has a cyclic definition", fn.Name)
	}
	st.evaluating[key] = true
	defer delete(st.evaluating, key)
	prev := st.current
	st.current = lib
	defer func() { st.current = prev }()
	locals := map[string][]any{}
	for i, p := range fn.Params {
		locals[p.Name] = args[i]
	}
	return st.withLocals(locals, func() ([]any, error) {
		return st.eval(fn.Body)
	})
}

func (st *evalState) withLocals(locals map[string][]any, fn func() ([]any, error)) ([]any, error) {
	prev := map[string][]any{}
	had := map[string]bool{}
	for k, v := range locals {
		if old, ok := st.stack[k]; ok {
			prev[k] = old
			had[k] = true
		}
		st.stack[k] = v
	}
	defer func() {
		for k := range locals {
			if had[k] {
				st.stack[k] = prev[k]
			} else {
				delete(st.stack, k)
			}
		}
	}()
	return fn()
}

func (st *evalState) lookupLibrary(name string) *Library {
	for _, lib := range st.libraries {
		if lib == nil {
			continue
		}
		if lib.Name == name || strings.EqualFold(lib.Name, name) {
			return lib
		}
	}
	return nil
}

func (st *evalState) evalDefine(lib *Library, def Define) ([]any, error) {
	key := def.Name
	if lib != nil && lib.Name != "" {
		key = lib.Name + "." + def.Name
	}
	if st.evaluating[key] {
		return nil, errf("CQL expression %q has a cyclic definition", def.Name)
	}
	st.evaluating[key] = true
	defer delete(st.evaluating, key)
	prev := st.current
	st.current = lib
	defer func() { st.current = prev }()
	return st.eval(def.Expression)
}

func (st *evalState) evalUnary(n *unaryNode) ([]any, error) {
	v, err := st.eval(n.x)
	if err != nil {
		return nil, err
	}
	switch n.op {
	case "not":
		b := asBool(v)
		if b == nil {
			return nil, nil
		}
		return []any{!*b}, nil
	case "exists":
		return []any{len(v) > 0}, nil
	case "-":
		if len(v) == 0 {
			return nil, nil
		}
		if q, ok := asQuantity(v[0]); ok {
			q.Value = -q.Value
			return []any{q}, nil
		}
		num, ok := asFloat(v[0])
		if !ok {
			return nil, errf("CQL unary '-' requires a number")
		}
		return []any{-num}, nil
	case "singleton":
		if len(v) == 0 {
			return nil, nil
		}
		if len(v) > 1 {
			return nil, errf("CQL singleton from expected one item, got %d", len(v))
		}
		return []any{v[0]}, nil
	case "flatten":
		return flattenValues(v), nil
	case "start":
		if len(v) == 0 {
			return nil, nil
		}
		if iv, ok := asInterval(v[0]); ok {
			if iv.Low == nil {
				return nil, nil
			}
			return []any{iv.Low}, nil
		}
		return nil, nil
	case "end":
		if len(v) == 0 {
			return nil, nil
		}
		if iv, ok := asInterval(v[0]); ok {
			if iv.High == nil {
				return nil, nil
			}
			return []any{iv.High}, nil
		}
		return nil, nil
	case "width":
		if len(v) == 0 {
			return nil, nil
		}
		if iv, ok := asInterval(v[0]); ok {
			return intervalWidth(iv)
		}
		return nil, nil
	case "collapse":
		return collapseIntervals(v), nil
	case "expand":
		return v, nil
	}
	return nil, errf("%w: unary operator %q", ErrUnsupported, n.op)
}

func (st *evalState) evalBinary(n *binaryNode) ([]any, error) {
	switch n.op {
	case "and", "or", "xor", "implies":
		return st.evalLogic(n)
	case "|":
		left, err := st.eval(n.left)
		if err != nil {
			return nil, err
		}
		right, err := st.eval(n.right)
		if err != nil {
			return nil, err
		}
		return distinctValues(append(append([]any{}, left...), right...)), nil
	case "&":
		left, err := st.eval(n.left)
		if err != nil {
			return nil, err
		}
		right, err := st.eval(n.right)
		if err != nil {
			return nil, err
		}
		return []any{fmt.Sprint(singletonOrEmpty(left)) + fmt.Sprint(singletonOrEmpty(right))}, nil
	case "in":
		left, err := st.eval(n.left)
		if err != nil {
			return nil, err
		}
		right, err := st.eval(n.right)
		if err != nil {
			return nil, err
		}
		if len(left) == 0 {
			return nil, nil
		}
		if vs, ok := singletonValueSet(right); ok {
			ok, err := st.inValueSet(left, vs)
			if err != nil {
				return nil, err
			}
			return []any{ok}, nil
		}
		if len(right) == 1 {
			if iv, ok := asInterval(right[0]); ok {
				return []any{intervalContains(iv, left[0], false)}, nil
			}
		}
		return []any{containsValue(right, left[0])}, nil
	case "contains":
		left, err := st.eval(n.left)
		if err != nil {
			return nil, err
		}
		right, err := st.eval(n.right)
		if err != nil {
			return nil, err
		}
		if len(right) == 0 {
			return nil, nil
		}
		if len(left) == 1 {
			if iv, ok := asInterval(left[0]); ok {
				return []any{intervalContains(iv, right[0], false)}, nil
			}
		}
		return []any{containsValue(left, right[0])}, nil
	case "intersect":
		left, err := st.eval(n.left)
		if err != nil {
			return nil, err
		}
		right, err := st.eval(n.right)
		if err != nil {
			return nil, err
		}
		if len(left) == 1 && len(right) == 1 {
			if li, ok := asInterval(left[0]); ok {
				if ri, ok := asInterval(right[0]); ok {
					out, ok := intervalIntersect(li, ri)
					if !ok {
						return nil, nil
					}
					return []any{out}, nil
				}
			}
		}
		return listIntersect(left, right), nil
	case "except":
		left, err := st.eval(n.left)
		if err != nil {
			return nil, err
		}
		right, err := st.eval(n.right)
		if err != nil {
			return nil, err
		}
		return listExcept(left, right), nil
	case "includes", "properly includes", "included in", "properly included in", "during", "properly during", "overlaps", "starts", "ends", "meets", "before", "after":
		return st.evalIntervalRel(n)
	}
	if strings.HasPrefix(n.op, "same") {
		return st.evalSameAs(n)
	}
	left, err := st.eval(n.left)
	if err != nil {
		return nil, err
	}
	right, err := st.eval(n.right)
	if err != nil {
		return nil, err
	}
	if len(left) == 0 || len(right) == 0 {
		return nil, nil
	}
	lv, rv := left[0], right[0]
	switch n.op {
	case "=":
		return []any{cqlEqual(lv, rv)}, nil
	case "!=":
		return []any{!cqlEqual(lv, rv)}, nil
	case "~":
		return []any{cqlEquivalent(lv, rv)}, nil
	case "!~":
		return []any{!cqlEquivalent(lv, rv)}, nil
	case "<", ">", "<=", ">=":
		cmp, ok := cqlCompare(lv, rv)
		if !ok {
			return nil, nil
		}
		switch n.op {
		case "<":
			return []any{cmp < 0}, nil
		case ">":
			return []any{cmp > 0}, nil
		case "<=":
			return []any{cmp <= 0}, nil
		case ">=":
			return []any{cmp >= 0}, nil
		}
	case "+", "-", "*", "/", "div", "mod":
		return evalArithmetic(n.op, lv, rv)
	}
	return nil, errf("%w: operator %q", ErrUnsupported, n.op)
}

func (st *evalState) evalLogic(n *binaryNode) ([]any, error) {
	left, err := st.eval(n.left)
	if err != nil {
		return nil, err
	}
	lb := asBool(left)
	switch n.op {
	case "and":
		if lb != nil && !*lb {
			return []any{false}, nil
		}
	case "or":
		if lb != nil && *lb {
			return []any{true}, nil
		}
	case "implies":
		if lb != nil && !*lb {
			return []any{true}, nil
		}
	}
	right, err := st.eval(n.right)
	if err != nil {
		return nil, err
	}
	rb := asBool(right)
	switch n.op {
	case "and":
		if lb == nil || rb == nil {
			if (lb != nil && !*lb) || (rb != nil && !*rb) {
				return []any{false}, nil
			}
			return nil, nil
		}
		return []any{*lb && *rb}, nil
	case "or":
		if lb == nil || rb == nil {
			if (lb != nil && *lb) || (rb != nil && *rb) {
				return []any{true}, nil
			}
			return nil, nil
		}
		return []any{*lb || *rb}, nil
	case "xor":
		if lb == nil || rb == nil {
			return nil, nil
		}
		return []any{*lb != *rb}, nil
	case "implies":
		if rb == nil {
			if lb != nil && !*lb {
				return []any{true}, nil
			}
			return nil, nil
		}
		if lb == nil {
			return nil, nil
		}
		return []any{!*lb || *rb}, nil
	}
	return nil, errf("%w: logic operator %q", ErrUnsupported, n.op)
}

func (st *evalState) evalMember(n *memberNode) ([]any, error) {
	base, err := st.eval(n.x)
	if err != nil {
		return nil, err
	}
	if lib, ok := singletonLibrary(base); ok {
		if def, found := defineByName(lib, n.name); found {
			return st.evalDefine(lib, def)
		}
		if fn, found := functionByName(lib, n.name); found && len(fn.Params) == 0 {
			return st.evalUserFunction(lib, &fn, nil)
		}
		return nil, errf("%w: %s.%s", ErrExpressionNotFound, lib.Name, n.name)
	}
	if ns, ok := singletonNS(base); ok {
		return []any{builtinNS{name: ns.name + "." + n.name}}, nil
	}
	var out []any
	for _, item := range base {
		vals, ok := st.memberValues(item, n.name)
		if !ok {
			out = append(out, nil)
			continue
		}
		out = append(out, vals...)
	}
	return out, nil
}

func (st *evalState) evalCall(n *callNode) ([]any, error) {
	name := callName(n.callee)
	if mem, ok := n.callee.(*memberNode); ok {
		recv, err := st.eval(mem.x)
		if err != nil {
			return nil, err
		}
		method := strings.ToLower(mem.name)
		if method == "where" || method == "select" {
			return st.evalMethod(mem.name, recv, n.args, nil)
		}
		args := make([][]any, len(n.args))
		for i, a := range n.args {
			v, err := st.eval(a)
			if err != nil {
				return nil, err
			}
			args[i] = v
		}
		if lib, ok := singletonLibrary(recv); ok {
			if isFHIRHelpersLibrary(lib) {
				return st.evalFunction("FHIRHelpers."+mem.name, args)
			}
			if fn, found := functionByName(lib, mem.name); found {
				return st.evalUserFunction(lib, &fn, args)
			}
			if def, found := defineByName(lib, mem.name); found {
				return st.evalDefine(lib, def)
			}
			return st.evalFunction(lib.Name+"."+mem.name, args)
		}
		if ns, ok := singletonNS(recv); ok {
			return st.evalFunction(ns.name+"."+mem.name, args)
		}
		return st.evalMethod(mem.name, recv, n.args, args)
	}
	args := make([][]any, len(n.args))
	for i, a := range n.args {
		v, err := st.eval(a)
		if err != nil {
			return nil, err
		}
		args[i] = v
	}
	if fn, lib := st.lookupFunction(name); fn != nil {
		return st.evalUserFunction(lib, fn, args)
	}
	return st.evalFunction(name, args)
}

func (st *evalState) evalMethod(name string, recv []any, rawArgs []Node, args [][]any) ([]any, error) {
	switch strings.ToLower(name) {
	case "where":
		if len(rawArgs) != 1 {
			return nil, errf("CQL where() requires one argument")
		}
		return st.filterWhere(recv, rawArgs[0])
	case "select":
		if len(rawArgs) != 1 {
			return nil, errf("CQL select() requires one argument")
		}
		return st.mapSelect(recv, rawArgs[0])
	case "first":
		if len(recv) == 0 {
			return nil, nil
		}
		return []any{recv[0]}, nil
	case "last":
		if len(recv) == 0 {
			return nil, nil
		}
		return []any{recv[len(recv)-1]}, nil
	case "count":
		return []any{int64(len(recv))}, nil
	case "exists":
		return []any{len(recv) > 0}, nil
	case "empty":
		return []any{len(recv) == 0}, nil
	case "single":
		if len(recv) == 0 {
			return nil, nil
		}
		if len(recv) > 1 {
			return nil, errf("CQL single() expected one item, got %d", len(recv))
		}
		return []any{recv[0]}, nil
	case "distinct":
		return distinctValues(recv), nil
	case "value":
		var out []any
		for _, item := range recv {
			out = append(out, unwrapPrimitive(item))
		}
		return out, nil
	}
	if fn, lib := st.lookupFunction(name); fn != nil && (fn.Fluent || len(fn.Params) == len(rawArgs)+1) {
		callArgs := make([][]any, 0, len(rawArgs)+1)
		callArgs = append(callArgs, recv)
		for _, a := range rawArgs {
			v, err := st.eval(a)
			if err != nil {
				return nil, err
			}
			callArgs = append(callArgs, v)
		}
		return st.evalUserFunction(lib, fn, callArgs)
	}
	return st.evalFunction(name, append([][]any{recv}, args...))
}

func (st *evalState) filterWhere(recv []any, pred Node) ([]any, error) {
	var out []any
	for _, item := range recv {
		prev, prevSet := st.this, st.thisSet
		st.this, st.thisSet = item, true
		v, err := st.eval(pred)
		st.this, st.thisSet = prev, prevSet
		if err != nil {
			return nil, err
		}
		b := asBool(v)
		if b != nil && *b {
			out = append(out, item)
		}
	}
	return out, nil
}

func (st *evalState) mapSelect(recv []any, proj Node) ([]any, error) {
	var out []any
	for _, item := range recv {
		prev, prevSet := st.this, st.thisSet
		st.this, st.thisSet = item, true
		v, err := st.eval(proj)
		st.this, st.thisSet = prev, prevSet
		if err != nil {
			return nil, err
		}
		out = append(out, v...)
	}
	return out, nil
}

func (st *evalState) stackValues(name string) ([]any, bool) {
	if st == nil || st.stack == nil {
		return nil, false
	}
	if v, ok := st.stack[name]; ok {
		return v, true
	}
	for k, v := range st.stack {
		if strings.EqualFold(k, name) {
			return v, true
		}
	}
	return nil, false
}

func (st *evalState) normalizeFunctionName(name string) string {
	n := strings.ToLower(name)
	for {
		trimmed := strings.TrimPrefix(n, "fhirhelpers.")
		trimmed = strings.TrimPrefix(trimmed, "system.")
		if i := strings.LastIndex(trimmed, "."); i >= 0 {
			prefix := trimmed[:i]
			if st.isHelpersPrefix(prefix) {
				trimmed = trimmed[i+1:]
			}
		}
		if trimmed == n {
			return n
		}
		n = trimmed
	}
}

func (st *evalState) isHelpersPrefix(prefix string) bool {
	if prefix == "" {
		return false
	}
	if strings.EqualFold(prefix, "FHIRHelpers") || strings.EqualFold(prefix, "System") {
		return true
	}
	return isFHIRHelpersLibrary(st.lookupLibrary(prefix))
}

func (st *evalState) evalFunction(name string, args [][]any) ([]any, error) {
	n := st.normalizeFunctionName(name)
	switch n {
	case "first":
		if len(args) == 0 || len(args[0]) == 0 {
			return nil, nil
		}
		return []any{args[0][0]}, nil
	case "last":
		if len(args) == 0 || len(args[0]) == 0 {
			return nil, nil
		}
		a := args[0]
		return []any{a[len(a)-1]}, nil
	case "count":
		if len(args) == 0 {
			return []any{int64(0)}, nil
		}
		return []any{int64(len(args[0]))}, nil
	case "exists":
		if len(args) == 0 {
			return []any{false}, nil
		}
		return []any{len(args[0]) > 0}, nil
	case "empty":
		if len(args) == 0 {
			return []any{true}, nil
		}
		return []any{len(args[0]) == 0}, nil
	case "ageinyears":
		return st.ageInYears(nil)
	case "ageinyearsat":
		var at time.Time
		if len(args) > 0 && len(args[0]) > 0 {
			if tm, ok := asTime(args[0][0]); ok {
				at = tm
			}
		}
		return st.ageInYears(&at)
	case "tostring":
		if len(args) == 0 || len(args[0]) == 0 {
			return nil, nil
		}
		return []any{fmt.Sprint(unwrapPrimitive(args[0][0]))}, nil
	case "tointeger":
		if len(args) == 0 || len(args[0]) == 0 {
			return nil, nil
		}
		n, ok := asInt(args[0][0])
		if !ok {
			return nil, nil
		}
		return []any{n}, nil
	case "todecimal":
		if len(args) == 0 || len(args[0]) == 0 {
			return nil, nil
		}
		f, ok := asFloat(args[0][0])
		if !ok {
			return nil, nil
		}
		return []any{f}, nil
	case "toboolean":
		if len(args) == 0 || len(args[0]) == 0 {
			return nil, nil
		}
		b := asBool(args[0])
		if b == nil {
			return nil, nil
		}
		return []any{*b}, nil
	case "length":
		if len(args) == 0 || len(args[0]) == 0 {
			return nil, nil
		}
		return []any{int64(len(fmt.Sprint(unwrapPrimitive(args[0][0]))))}, nil
	case "today", "now":
		if n == "today" {
			t := st.now.UTC()
			return []any{time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)}, nil
		}
		return []any{st.now.UTC()}, nil
	case "tointerval":
		if len(args) == 0 || len(args[0]) == 0 {
			return nil, nil
		}
		if iv, ok := asInterval(args[0][0]); ok {
			return []any{iv}, nil
		}
		return nil, nil
	case "toquantity":
		if len(args) == 0 || len(args[0]) == 0 {
			return nil, nil
		}
		if q, ok := asQuantity(args[0][0]); ok {
			return []any{q}, nil
		}
		return nil, nil
	case "todatetime", "todate":
		if len(args) == 0 || len(args[0]) == 0 {
			return nil, nil
		}
		tm, ok := asTime(args[0][0])
		if !ok {
			return nil, nil
		}
		if n == "todate" {
			tm = time.Date(tm.Year(), tm.Month(), tm.Day(), 0, 0, 0, 0, time.UTC)
		}
		return []any{tm}, nil
	case "flatten":
		if len(args) == 0 {
			return nil, nil
		}
		return flattenValues(args[0]), nil
	case "min":
		return listExtremum(args, true)
	case "max":
		return listExtremum(args, false)
	case "sum":
		return listSum(args)
	case "avg", "average":
		return listAvg(args)
	case "alltrue":
		return listAllAny(args, true)
	case "anytrue":
		return listAllAny(args, false)
	case "take":
		return listTakeSkip(args, true)
	case "skip":
		return listTakeSkip(args, false)
	case "indexof":
		return listIndexOf(args)
	case "singletonfrom":
		if len(args) == 0 {
			return nil, nil
		}
		v := args[0]
		if len(v) == 0 {
			return nil, nil
		}
		if len(v) > 1 {
			return nil, errf("CQL SingletonFrom expected one item, got %d", len(v))
		}
		return []any{v[0]}, nil
	case "date":
		return constructDate(args, false)
	case "datetime":
		return constructDate(args, true)
	case "time":
		return st.constructTime(args)
	case "startswith":
		return stringPred(args, strings.HasPrefix)
	case "endswith":
		return stringPred(args, strings.HasSuffix)
	case "matches", "matchesfull":
		return stringContains(args)
	case "replace":
		return stringReplace(args)
	case "split":
		return stringSplit(args)
	case "combine":
		return stringCombine(args)
	case "upper":
		return stringCase(args, true)
	case "lower":
		return stringCase(args, false)
	case "substring":
		return stringSubstring(args)
	case "indexer":
		return st.evalIndexer(args)
	case "collapse":
		if len(args) == 0 {
			return nil, nil
		}
		return collapseIntervals(args[0]), nil
	case "expand":
		if len(args) == 0 {
			return nil, nil
		}
		return args[0], nil
	}
	return nil, errf("%w: function %s", ErrUnsupported, name)
}

func (st *evalState) ageInYears(at *time.Time) ([]any, error) {
	if st.patient == nil {
		return nil, ErrMissingContext
	}
	birth, ok := patientBirthDate(st.patient)
	if !ok {
		return nil, nil
	}
	when := st.now
	if at != nil && !at.IsZero() {
		when = *at
	}
	years := when.UTC().Year() - birth.UTC().Year()
	anniversary := time.Date(when.Year(), birth.Month(), birth.Day(), 0, 0, 0, 0, time.UTC)
	if when.UTC().Before(anniversary) {
		years--
	}
	if years < 0 {
		years = 0
	}
	return []any{int64(years)}, nil
}

func (st *evalState) evalRetrieve(n *retrieveNode) ([]any, error) {
	if strings.EqualFold(n.resourceType, "Patient") && st.patient != nil && n.terminology == "" {
		return []any{st.patient}, nil
	}
	if st.retriever == nil {
		return nil, errf("%w: retrieve [%s] requires a data retriever", ErrUnsupported, n.resourceType)
	}
	req := st.retrieveRequest(n)
	items, err := st.retriever.Retrieve(st.ctx, req, st.patient)
	if err != nil {
		return nil, err
	}
	if req.Terminology == "" && req.ValueSetURL == "" && req.Code == "" {
		return items, nil
	}
	var out []any
	for _, item := range items {
		ok, err := matchResourceTerminology(st.ctx, item, req, st.terminology())
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, item)
		}
	}
	return out, nil
}

func (st *evalState) retrieveRequest(n *retrieveNode) RetrieveRequest {
	req := RetrieveRequest{ResourceType: n.resourceType, Terminology: n.terminology, Comparator: n.comparator}
	if n.terminology == "" {
		return req
	}
	if n.comparator != "=" && n.comparator != "~" {
		if vs := st.lookupValueSet(n.terminology); vs != nil {
			req.ValueSetURL = vs.URL
			return req
		}
		if looksLikeCanonical(n.terminology) {
			req.ValueSetURL = n.terminology
			return req
		}
	}
	if c := st.lookupCode(n.terminology); c != nil {
		req.System = c.System
		req.Code = c.Code
		return req
	}
	if sys, code, ok := splitSystemCode(n.terminology); ok {
		req.System = sys
		req.Code = code
	}
	return req
}

func looksLikeCanonical(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	return strings.Contains(s, "://") || strings.HasPrefix(strings.ToLower(s), "urn:")
}

func (st *evalState) terminology() fhirpath.TerminologyValidator {
	if st.engine == nil {
		return nil
	}
	return st.engine.terminology
}

func (st *evalState) lookupValueSet(name string) *ValueSet {
	search := func(lib *Library) *ValueSet {
		if lib == nil {
			return nil
		}
		for i := range lib.ValueSets {
			if lib.ValueSets[i].Name == name || strings.EqualFold(lib.ValueSets[i].Name, name) {
				return &lib.ValueSets[i]
			}
		}
		return nil
	}
	if vs := search(st.current); vs != nil {
		return vs
	}
	for _, lib := range st.libraries {
		if vs := search(lib); vs != nil {
			return vs
		}
	}
	return nil
}

func (st *evalState) lookupCode(name string) *Code {
	search := func(lib *Library) *Code {
		if lib == nil {
			return nil
		}
		for i := range lib.Codes {
			if lib.Codes[i].Name == name || strings.EqualFold(lib.Codes[i].Name, name) {
				return &lib.Codes[i]
			}
		}
		return nil
	}
	if c := search(st.current); c != nil {
		return c
	}
	for _, lib := range st.libraries {
		if c := search(lib); c != nil {
			return c
		}
	}
	return nil
}

func (st *evalState) inValueSet(values []any, vs ValueSet) (bool, error) {
	req := RetrieveRequest{ValueSetURL: vs.URL, Terminology: vs.Name}
	for _, v := range values {
		ok, err := matchResourceTerminology(st.ctx, v, req, st.terminology())
		if err != nil {
			return false, err
		}
		if ok {
			return true, nil
		}
	}
	return false, nil
}

func (st *evalState) memberValues(item any, name string) ([]any, bool) {
	if st.engine != nil && st.engine.fhirpath != nil && fhirPathNavigable(item) {
		vals, err := st.engine.fhirpath.Eval(st.ctx, name, item)
		if err == nil {
			if out, ok := convertFHIRPathCollection(vals); ok && len(out) > 0 {
				return out, true
			}
		}
	}
	return fieldValues(item, name)
}

func fhirPathNavigable(v any) bool {
	switch x := v.(type) {
	case *types.ResourceEnvelope:
		return x != nil
	case types.ResourceEnvelope:
		return true
	case map[string]any:
		_, ok := x["resourceType"].(string)
		return ok && x["resourceType"] != ""
	}
	return false
}

func convertFHIRPathCollection(vals []fhirpath.Value) ([]any, bool) {
	if len(vals) == 0 {
		return nil, false
	}
	out := make([]any, 0, len(vals))
	for _, v := range vals {
		cv, ok := convertFHIRPathValue(v)
		if !ok {
			return nil, false
		}
		out = append(out, flattenJSON(cv)...)
	}
	return out, true
}

func convertFHIRPathValue(v fhirpath.Value) (any, bool) {
	switch v.Type() {
	case "Boolean", "String", "Integer", "Decimal", "Date", "DateTime", "Time", "Quantity":
		return fhirPathScalar(v), true
	case "null":
		return nil, true
	}
	raw := v.Raw()
	if raw == nil {
		return nil, true
	}
	if _, ok := asObject(raw); ok {
		return raw, true
	}
	if msg, ok := raw.(gproto.Message); ok && msg != nil {
		if mapped, ok := protoElementToValue(msg); ok {
			return mapped, true
		}
	}
	if s, err := v.String(); err == nil {
		return s, true
	}
	return nil, false
}

var (
	elementMarshalerOnce sync.Once
	elementMarshaler     *jsonformat.Marshaller
	elementMarshalerMu   sync.Mutex
)

func protoElementToValue(msg gproto.Message) (any, bool) {
	elementMarshalerOnce.Do(func() {
		m, err := jsonformat.NewMarshaller(false, "", "", fhirversion.R4)
		if err != nil {
			return
		}
		elementMarshaler = m
	})
	if elementMarshaler == nil {
		return nil, false
	}
	elementMarshalerMu.Lock()
	defer elementMarshalerMu.Unlock()
	raw, err := elementMarshaler.MarshalElement(msg)
	if err != nil {
		raw, err = elementMarshaler.MarshalResource(msg)
	}
	if err != nil {
		return nil, false
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, false
	}
	return v, true
}

func singletonValueSet(v []any) (ValueSet, bool) {
	if len(v) != 1 {
		return ValueSet{}, false
	}
	vs, ok := v[0].(ValueSet)
	return vs, ok
}

func callName(n Node) string {
	switch x := n.(type) {
	case *identNode:
		return x.name
	case *memberNode:
		if prefix, ok := identName(x.x); ok {
			return prefix + "." + x.name
		}
		if ns, ok := x.x.(*memberNode); ok {
			return callName(ns) + "." + x.name
		}
		return x.name
	default:
		return n.Source()
	}
}

type builtinNS struct{ name string }

func singletonLibrary(v []any) (*Library, bool) {
	if len(v) != 1 {
		return nil, false
	}
	lib, ok := v[0].(*Library)
	return lib, ok
}

func singletonNS(v []any) (builtinNS, bool) {
	if len(v) != 1 {
		return builtinNS{}, false
	}
	ns, ok := v[0].(builtinNS)
	return ns, ok
}

func defineByName(lib *Library, name string) (Define, bool) {
	for _, d := range lib.Defines {
		if d.Name == name || strings.EqualFold(d.Name, name) {
			return d, true
		}
	}
	return Define{}, false
}

func functionByName(lib *Library, name string) (Function, bool) {
	if lib == nil {
		return Function{}, false
	}
	for _, f := range lib.Functions {
		if f.Name == name || strings.EqualFold(f.Name, name) {
			return f, true
		}
	}
	return Function{}, false
}

func wrapValue(v any) []any {
	if v == nil {
		return nil
	}
	if list, ok := v.([]any); ok {
		return list
	}
	return []any{v}
}

func singletonOrList(v []any) any {
	if len(v) == 0 {
		return nil
	}
	if len(v) == 1 {
		return v[0]
	}
	return v
}

func singletonOrEmpty(v []any) any {
	if len(v) == 0 {
		return ""
	}
	return unwrapPrimitive(v[0])
}

func asBool(v []any) *bool {
	if len(v) == 0 {
		return nil
	}
	switch x := unwrapPrimitive(v[0]).(type) {
	case bool:
		return &x
	case string:
		if strings.EqualFold(x, "true") {
			t := true
			return &t
		}
		if strings.EqualFold(x, "false") {
			t := false
			return &t
		}
	}
	return nil
}

func asFloat(v any) (float64, bool) {
	switch x := unwrapPrimitive(v).(type) {
	case int:
		return float64(x), true
	case int32:
		return float64(x), true
	case int64:
		return float64(x), true
	case float32:
		return float64(x), true
	case float64:
		return x, true
	case json.Number:
		f, err := x.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(x, 64)
		return f, err == nil
	}
	return 0, false
}

func asInt(v any) (int64, bool) {
	f, ok := asFloat(v)
	if !ok {
		return 0, false
	}
	return int64(f), true
}

func asTime(v any) (time.Time, bool) {
	switch x := unwrapPrimitive(v).(type) {
	case time.Time:
		return x, true
	case string:
		if tm, err := parseCQLDate(x); err == nil {
			return tm, true
		}
	}
	return time.Time{}, false
}

func evalArithmetic(op string, lv, rv any) ([]any, error) {
	if q1, ok := asQuantity(lv); ok {
		if q2, ok := asQuantity(rv); ok {
			return evalQuantityArith(op, q1, q2)
		}
	}
	if t, ok := asTime(lv); ok {
		if q, ok := asQuantity(rv); ok {
			out, ok := addDuration(t, q, op)
			if !ok {
				return nil, nil
			}
			return []any{out}, nil
		}
	}
	lf, lok := asFloat(lv)
	rf, rok := asFloat(rv)
	if !lok || !rok {
		if op == "+" {
			return []any{fmt.Sprint(unwrapPrimitive(lv)) + fmt.Sprint(unwrapPrimitive(rv))}, nil
		}
		return nil, nil
	}
	var out float64
	switch op {
	case "+":
		out = lf + rf
	case "-":
		out = lf - rf
	case "*":
		out = lf * rf
	case "/":
		if rf == 0 {
			return nil, nil
		}
		out = lf / rf
	case "div":
		if rf == 0 {
			return nil, nil
		}
		return []any{int64(lf / rf)}, nil
	case "mod":
		if rf == 0 {
			return nil, nil
		}
		return []any{math.Mod(lf, rf)}, nil
	}
	if isIntLike(lv) && isIntLike(rv) && op != "/" {
		return []any{int64(out)}, nil
	}
	return []any{out}, nil
}

func isIntLike(v any) bool {
	switch unwrapPrimitive(v).(type) {
	case int, int32, int64:
		return true
	}
	return false
}

func cqlEqual(a, b any) bool {
	a, b = unwrapPrimitive(a), unwrapPrimitive(b)
	if qa, ok := asQuantity(a); ok {
		if qb, ok := asQuantity(b); ok {
			return qa.Value == qb.Value && sameUnit(qa.Unit, qb.Unit)
		}
	}
	if ia, ok := asInterval(a); ok {
		if ib, ok := asInterval(b); ok {
			return cqlEqual(ia.Low, ib.Low) && cqlEqual(ia.High, ib.High) && ia.LowClosed == ib.LowClosed && ia.HighClosed == ib.HighClosed
		}
	}
	if ta, ok := asTime(a); ok {
		if tb, ok := asTime(b); ok {
			return ta.Equal(tb)
		}
	}
	if fa, ok := asFloat(a); ok {
		if fb, ok := asFloat(b); ok {
			return fa == fb
		}
	}
	return fmt.Sprint(a) == fmt.Sprint(b)
}

func cqlEquivalent(a, b any) bool {
	return strings.EqualFold(fmt.Sprint(unwrapPrimitive(a)), fmt.Sprint(unwrapPrimitive(b)))
}

func cqlCompare(a, b any) (int, bool) {
	a, b = unwrapPrimitive(a), unwrapPrimitive(b)
	if qa, ok := asQuantity(a); ok {
		if qb, ok := asQuantity(b); ok && (sameUnit(qa.Unit, qb.Unit) || (isTimeUnit(qa.Unit) && isTimeUnit(qb.Unit))) {
			va, vb := qa.Value, qb.Value
			if !sameUnit(qa.Unit, qb.Unit) {
				va, vb = toSeconds(qa), toSeconds(qb)
			}
			if va < vb {
				return -1, true
			}
			if va > vb {
				return 1, true
			}
			return 0, true
		}
	}
	if ta, ok := asTime(a); ok {
		if tb, ok := asTime(b); ok {
			if ta.Before(tb) {
				return -1, true
			}
			if ta.After(tb) {
				return 1, true
			}
			return 0, true
		}
	}
	if fa, ok := asFloat(a); ok {
		if fb, ok := asFloat(b); ok {
			if fa < fb {
				return -1, true
			}
			if fa > fb {
				return 1, true
			}
			return 0, true
		}
	}
	sa, sb := fmt.Sprint(a), fmt.Sprint(b)
	if sa < sb {
		return -1, true
	}
	if sa > sb {
		return 1, true
	}
	return 0, true
}

func containsValue(list []any, item any) bool {
	for _, el := range list {
		if cqlEqual(el, item) {
			return true
		}
	}
	return false
}

func distinctValues(in []any) []any {
	var out []any
	for _, item := range in {
		if containsValue(out, item) {
			continue
		}
		out = append(out, item)
	}
	return out
}

func typeName(v any) string {
	switch unwrapPrimitive(v).(type) {
	case bool:
		return "Boolean"
	case string:
		return "String"
	case int, int32, int64:
		return "Integer"
	case float32, float64:
		return "Decimal"
	case time.Time:
		return "DateTime"
	case Quantity:
		return "Quantity"
	case Interval:
		return "Interval"
	}
	if obj, ok := asObject(v); ok {
		if rt, _ := obj["resourceType"].(string); rt != "" {
			return rt
		}
	}
	return fmt.Sprintf("%T", v)
}

func fieldValues(v any, name string) ([]any, bool) {
	if v == nil {
		return nil, false
	}
	if obj, ok := asObject(v); ok {
		raw, exists := obj[name]
		if !exists {
			// Choice types: valueQuantity, deceasedBoolean, effectiveDateTime, …
			for k, val := range obj {
				if k == name || strings.EqualFold(k, name) {
					raw = val
					exists = true
					break
				}
				if len(k) > len(name) && strings.HasPrefix(k, name) {
					rest := k[len(name):]
					if rest != "" && rest[0] >= 'A' && rest[0] <= 'Z' {
						raw = val
						exists = true
						break
					}
				}
			}
		}
		if !exists {
			return nil, false
		}
		return flattenJSON(raw), true
	}
	return nil, false
}

func flattenJSON(v any) []any {
	if v == nil {
		return nil
	}
	switch x := v.(type) {
	case []any:
		var out []any
		for _, el := range x {
			out = append(out, flattenJSON(el)...)
		}
		return out
	default:
		return []any{unwrapPrimitive(x)}
	}
}

func unwrapPrimitive(v any) any {
	if v == nil {
		return nil
	}
	if fp, ok := v.(fhirpath.Value); ok {
		return fhirPathScalar(fp)
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return v
	}
	if val, has := obj["value"]; has {
		for k := range obj {
			if k != "value" && k != "id" && k != "extension" {
				return v
			}
		}
		return val
	}
	return v
}

func fhirPathScalar(v fhirpath.Value) any {
	switch v.Type() {
	case "Boolean":
		if value, err := v.Bool(); err == nil {
			return value
		}
	case "String":
		if value, err := v.String(); err == nil {
			return value
		}
	case "Date", "DateTime", "Time":
		if value, err := v.String(); err == nil {
			return value
		}
	case "Integer":
		if value, err := v.Float64(); err == nil {
			return int64(value)
		}
	case "Decimal":
		if value, err := v.Float64(); err == nil {
			return value
		}
	}
	return v.Raw()
}

func asObject(v any) (map[string]any, bool) {
	switch x := v.(type) {
	case map[string]any:
		return x, true
	case *types.ResourceEnvelope:
		if x == nil || len(x.JSON) == 0 {
			return nil, false
		}
		var m map[string]any
		if err := json.Unmarshal(x.JSON, &m); err != nil {
			return nil, false
		}
		return m, true
	case types.ResourceEnvelope:
		return asObject(&x)
	}
	return nil, false
}

func patientBirthDate(patient any) (time.Time, bool) {
	obj, ok := asObject(patient)
	if !ok {
		return time.Time{}, false
	}
	raw, ok := obj["birthDate"]
	if !ok {
		return time.Time{}, false
	}
	s := fmt.Sprint(unwrapPrimitive(raw))
	tm, err := parseCQLDate(s)
	if err != nil {
		return time.Time{}, false
	}
	return tm, true
}
