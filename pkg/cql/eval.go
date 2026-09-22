package cql

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/degoke/haistack/pkg/fhirpath"
	"github.com/degoke/haistack/pkg/types"
	"github.com/google/fhir/go/fhirversion"
	"github.com/google/fhir/go/jsonformat"
	gproto "google.golang.org/protobuf/proto"
)

type evalState struct {
	ctx            context.Context
	engine         *Engine
	patient        any
	params         map[string]any
	libraries      []*Library
	current        *Library
	now            time.Time
	retriever      Retriever
	this           any
	thisSet        bool
	stack          map[string][]any
	evaluating     map[string]bool
	functionParams []FunctionParam
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
		out := []any{}
		for _, el := range x.elems {
			v, err := st.eval(el)
			if err != nil {
				return nil, err
			}
			out = append(out, st.wrapListElement(el, v))
		}
		return out, nil
	case *tupleNode:
		obj := map[string]any{}
		for _, f := range x.fields {
			v, err := st.eval(f.value)
			if err != nil {
				return nil, err
			}
			obj[f.name] = st.wrapListElement(f.value, v)
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
		return st.evalIs(x)
	case *asNode:
		return st.evalAs(x)
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
	if cs := st.lookupCodeSystem(name); cs != nil {
		return []any{*cs}, nil
	}
	if cpt := st.lookupConcept(name); cpt != nil {
		return st.conceptValues(*cpt), nil
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
			if isListParameterType(p.Type) {
				if v == nil {
					st.params[p.Name] = []any{}
				} else {
					st.params[p.Name] = append([]any{}, v...)
				}
			} else {
				st.params[p.Name] = singletonOrList(v)
			}
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
	prevFnParams := st.functionParams
	st.functionParams = fn.Params
	defer func() { st.functionParams = prevFnParams }()
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
		if len(v) != 1 {
			return nil, nil
		}
		b := asBool(v)
		if b == nil {
			return nil, nil
		}
		return []any{!*b}, nil
	case "exists":
		return []any{existsNonNull(v)}, nil
	case "-":
		if len(v) != 1 {
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
		if len(v) != 1 {
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
		if len(v) != 1 {
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
		if len(v) != 1 {
			return nil, nil
		}
		if iv, ok := asInterval(v[0]); ok {
			return intervalWidth(iv)
		}
		return nil, nil
	case "tolist":
		return toListResult(v), nil
	case "collapse":
		return collapseIntervals(v, nil, st.compareContext()), nil
	case "expand":
		return expandValues(v, nil, st.compareContext()), nil
	case "successor", "predecessor":
		if len(v) != 1 {
			return nil, nil
		}
		out, ok := successorValue(v[0], n.op == "predecessor")
		if !ok {
			return nil, nil
		}
		return []any{out}, nil
	case "point from":
		if len(v) != 1 {
			return nil, nil
		}
		if iv, ok := asInterval(v[0]); ok {
			return pointFromInterval(iv, st.compareContext())
		}
		return nil, nil
	case "precision":
		if len(v) != 1 {
			return nil, nil
		}
		p, ok := valuePrecision(v[0])
		if !ok {
			return nil, nil
		}
		return []any{int64(p)}, nil
	}
	if strings.HasSuffix(n.op, " from") {
		return st.evalDateComponent(v, strings.TrimSuffix(n.op, " from"))
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
		if !st.isListValued(n.left) && !st.isListValued(n.right) && len(left) == 1 && len(right) == 1 {
			if li, ok := isCQLInterval(left[0]); ok {
				if ri, ok := isCQLInterval(right[0]); ok {
					cmp := st.compareContext()
					if intervalOverlaps(li, ri, cmp) || intervalMeets(li, ri, cmp) {
						merged, ok := intervalUnion(li, ri, cmp)
						if !ok {
							return nil, nil
						}
						return []any{merged}, nil
					}
					return nil, nil
				}
			}
		}
		return st.distinctValues(append(append([]any{}, left...), right...)), nil
	case "&":
		left, err := st.eval(n.left)
		if err != nil {
			return nil, err
		}
		right, err := st.eval(n.right)
		if err != nil {
			return nil, err
		}
		if len(left) != 1 || len(right) != 1 {
			return nil, nil
		}
		ls, lok := unwrapPrimitive(left[0]).(string)
		rs, rok := unwrapPrimitive(right[0]).(string)
		if !lok || !rok {
			return nil, nil
		}
		return []any{ls + rs}, nil
	case ":":
		left, err := st.eval(n.left)
		if err != nil {
			return nil, err
		}
		right, err := st.eval(n.right)
		if err != nil {
			return nil, err
		}
		if len(left) != 1 || len(right) != 1 {
			return nil, nil
		}
		num, ok1 := asQuantity(left[0])
		den, ok2 := asQuantity(right[0])
		if !ok1 {
			if f, ok := asFloat(left[0]); ok {
				num = Quantity{Value: f, Unit: "1"}
				ok1 = true
			}
		}
		if !ok2 {
			if f, ok := asFloat(right[0]); ok {
				den = Quantity{Value: f, Unit: "1"}
				ok2 = true
			}
		}
		if !ok1 || !ok2 {
			return nil, nil
		}
		return []any{Ratio{Numerator: num, Denominator: den}}, nil
	case "in", "all in", "any in":
		left, err := st.eval(n.left)
		if err != nil {
			return nil, err
		}
		right, err := st.eval(n.right)
		if err != nil {
			return nil, err
		}
		if left == nil || right == nil {
			return nil, nil
		}
		if vs, ok := singletonValueSet(right); ok {
			if n.op == "all in" {
				return st.allInValueSetResult(left, vs)
			}
			ok, err := st.inValueSet(left, vs)
			if err != nil {
				return nil, err
			}
			return []any{ok}, nil
		}
		if cs, ok := singletonCodeSystem(right); ok {
			if n.op == "all in" {
				return st.allInCodeSystemResult(left, cs)
			}
			ok, err := st.inCodeSystem(left, cs)
			if err != nil {
				return nil, err
			}
			return []any{ok}, nil
		}
		if n.op == "all in" {
			return st.allContainsResult(right, left), nil
		}
		if n.op == "any in" {
			return st.anyContainsResult(right, left), nil
		}
		if n.op == "in" && len(left) == 1 && len(right) == 1 {
			ls, lok := unwrapPrimitive(left[0]).(string)
			rs, rok := unwrapPrimitive(right[0]).(string)
			if lok && rok {
				return []any{strings.Contains(rs, ls)}, nil
			}
		}
		if len(right) == 1 {
			if iv, ok := asInterval(right[0]); ok {
				return intervalContainsAll(iv, left, st.compareContext())
			}
		}
		if st.isListValued(n.left) {
			if pointsVersusIntervals(left, right) {
				return st.allPointsInAnyInterval(left, right), nil
			}
			return st.containsResult(right, st.membershipItem(n.left, left)), nil
		}
		return st.containsResultWithIntervals(right, st.membershipItem(n.left, left)), nil
	case "contains":
		left, err := st.eval(n.left)
		if err != nil {
			return nil, err
		}
		right, err := st.eval(n.right)
		if err != nil {
			return nil, err
		}
		if left == nil || right == nil {
			return nil, nil
		}
		if len(left) == 1 && len(right) == 1 {
			ls, lok := unwrapPrimitive(left[0]).(string)
			rs, rok := unwrapPrimitive(right[0]).(string)
			if lok && rok {
				return []any{strings.Contains(ls, rs)}, nil
			}
		}
		if len(left) == 1 {
			if iv, ok := asInterval(left[0]); ok {
				return intervalContainsAll(iv, right, st.compareContext())
			}
		}
		if st.isListValued(n.right) {
			if pointsVersusIntervals(right, left) {
				return st.allPointsInAnyInterval(right, left), nil
			}
			return st.containsResult(left, st.membershipItem(n.right, right)), nil
		}
		return st.containsResultWithIntervals(left, st.membershipItem(n.right, right)), nil
	case "intersect":
		left, err := st.eval(n.left)
		if err != nil {
			return nil, err
		}
		right, err := st.eval(n.right)
		if err != nil {
			return nil, err
		}
		if !st.isListValued(n.left) && !st.isListValued(n.right) && len(left) == 1 && len(right) == 1 {
			if li, ok := isCQLInterval(left[0]); ok {
				if ri, ok := isCQLInterval(right[0]); ok {
					out, ok := intervalIntersect(li, ri, st.compareContext())
					if !ok {
						return nil, nil
					}
					return []any{out}, nil
				}
			}
		}
		return st.listIntersect(left, right), nil
	case "except":
		left, err := st.eval(n.left)
		if err != nil {
			return nil, err
		}
		right, err := st.eval(n.right)
		if err != nil {
			return nil, err
		}
		if !st.isListValued(n.left) && !st.isListValued(n.right) && len(left) == 1 && len(right) == 1 {
			if li, ok := isCQLInterval(left[0]); ok {
				if ri, ok := isCQLInterval(right[0]); ok {
					return intervalExcept(li, ri, st.compareContext()), nil
				}
			}
		}
		return st.listExcept(left, right), nil
	}
	if strings.HasPrefix(n.op, "same") {
		return st.evalSameAs(n)
	}
	switch n.op {
	case "subsumes", "properly subsumes":
		left, err := st.eval(n.left)
		if err != nil {
			return nil, err
		}
		right, err := st.eval(n.right)
		if err != nil {
			return nil, err
		}
		return st.cqlSubsumesResult(left, right, strings.HasPrefix(n.op, "properly"))
	case "subsumed by", "properly subsumed by":
		left, err := st.eval(n.left)
		if err != nil {
			return nil, err
		}
		right, err := st.eval(n.right)
		if err != nil {
			return nil, err
		}
		return st.cqlSubsumesResult(right, left, strings.HasPrefix(n.op, "properly"))
	}
	if intervalRelOp(n.op) {
		return st.evalIntervalRel(n)
	}
	left, err := st.eval(n.left)
	if err != nil {
		return nil, err
	}
	right, err := st.eval(n.right)
	if err != nil {
		return nil, err
	}
	switch n.op {
	case "=":
		return st.cqlEqualResult(left, right), nil
	case "!=":
		if neq := st.cqlNotEqualResult(left, right); neq != nil {
			return neq, nil
		}
		return nil, nil
	case "~":
		return st.cqlEquivalentResult(left, right), nil
	case "!~":
		eq := st.cqlEquivalentResult(left, right)
		if len(eq) == 0 {
			return nil, nil
		}
		return []any{eq[0] != true}, nil
	}
	if len(left) == 0 || len(right) == 0 {
		return nil, nil
	}
	switch n.op {
	case "<", ">", "<=", ">=":
		if len(left) != 1 || len(right) != 1 {
			return nil, nil
		}
		lv, rv := left[0], right[0]
		cmp, ok := st.compareContext().Compare(lv, rv)
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
	case "+", "-", "*", "/", "div", "mod", "^":
		if len(left) != 1 || len(right) != 1 {
			return nil, nil
		}
		return st.evalArithmetic(n.op, left[0], right[0])
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
	if strings.EqualFold(name, "Indexer") {
		var src Node
		if len(n.args) > 0 {
			src = n.args[0]
		}
		return st.evalIndexer(args, src)
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
		return listValueResult(recv[0]), nil
	case "last":
		if len(recv) == 0 {
			return nil, nil
		}
		return listValueResult(recv[len(recv)-1]), nil
	case "count":
		return []any{countNonNull(recv)}, nil
	case "exists":
		return []any{existsNonNull(recv)}, nil
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
		return st.distinctValues(recv), nil
	case "indexer":
		return st.evalIndexer(append([][]any{recv}, args...), nil)
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
		return listValueResult(args[0][0]), nil
	case "last":
		if len(args) == 0 || len(args[0]) == 0 {
			return nil, nil
		}
		a := args[0]
		return listValueResult(a[len(a)-1]), nil
	case "count":
		if len(args) == 0 {
			return []any{int64(0)}, nil
		}
		return []any{countNonNull(args[0])}, nil
	case "exists":
		if len(args) == 0 {
			return []any{false}, nil
		}
		return []any{existsNonNull(args[0])}, nil
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
		item, ok := singletonArg(args)
		if !ok {
			return nil, nil
		}
		s, ok := cqlToString(item)
		if !ok {
			return nil, nil
		}
		return []any{s}, nil
	case "tointeger", "tolong":
		item, ok := singletonArg(args)
		if !ok {
			return nil, nil
		}
		n, ok := asInt(item)
		if !ok {
			if s, ok := unwrapPrimitive(item).(string); ok {
				parsed, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
				if err != nil {
					return nil, nil
				}
				return []any{parsed}, nil
			}
			return nil, nil
		}
		return []any{n}, nil
	case "todecimal":
		item, ok := singletonArg(args)
		if !ok {
			return nil, nil
		}
		f, ok := asFloat(item)
		if !ok {
			if s, ok := unwrapPrimitive(item).(string); ok {
				parsed, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
				if err != nil {
					return nil, nil
				}
				return []any{parsed}, nil
			}
			return nil, nil
		}
		return []any{f}, nil
	case "toboolean":
		item, ok := singletonArg(args)
		if !ok {
			return nil, nil
		}
		b, ok := booleanFromValue(item)
		if !ok {
			return nil, nil
		}
		return []any{b}, nil
	case "length":
		return listOrStringLength(args)
	case "today", "now", "timeofday":
		t := clockInZone(st.now)
		loc := t.Location()
		if loc == nil {
			loc = time.UTC
		}
		if len(args) > 0 && len(args[0]) > 0 {
			if off, ok := asFloat(args[0][0]); ok {
				loc = time.FixedZone("", int(off*3600))
				t = t.In(loc)
			}
		}
		if n == "today" {
			return []any{time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)}, nil
		}
		if n == "timeofday" {
			todLoc := timeOnlyLoc
			if len(args) > 0 && len(args[0]) > 0 {
				if off, ok := asFloat(args[0][0]); ok {
					todLoc = time.FixedZone("CQL-TIME", int(off*3600))
				}
			}
			return []any{time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), todLoc)}, nil
		}
		return []any{t}, nil
	case "tointerval":
		item, ok := singletonArg(args)
		if !ok {
			return nil, nil
		}
		if iv, ok := asInterval(item); ok {
			return []any{iv}, nil
		}
		return nil, nil
	case "toquantity":
		item, ok := singletonArg(args)
		if !ok {
			return nil, nil
		}
		q, ok := quantityFromValue(item)
		if !ok {
			return nil, nil
		}
		return []any{q}, nil
	case "todatetime", "todate", "totime":
		item, ok := singletonArg(args)
		if !ok {
			return nil, nil
		}
		tm, ok := asTime(item)
		if !ok {
			if s, isStr := unwrapPrimitive(item).(string); isStr {
				if n == "totime" {
					if parsed, pok := parseELMTimeString(s); pok {
						tm = parsed
						ok = true
					}
				}
				if !ok {
					parsed, err := parseCQLDate(s)
					if err != nil {
						return nil, nil
					}
					tm = parsed
					ok = true
				}
			}
			if !ok {
				return nil, nil
			}
		}
		loc := tm.Location()
		if loc == nil {
			loc = time.UTC
		}
		if n == "todate" {
			tm = time.Date(tm.Year(), tm.Month(), tm.Day(), 0, 0, 0, 0, loc)
		}
		if n == "totime" {
			now := clockInZone(st.now)
			tm = time.Date(now.Year(), now.Month(), now.Day(), tm.Hour(), tm.Minute(), tm.Second(), tm.Nanosecond(), timeOnlyLoc)
		}
		return []any{tm}, nil
	case "coalesce":
		for _, a := range args {
			if a == nil {
				continue
			}
			if len(a) == 1 && a[0] == nil {
				continue
			}
			return a, nil
		}
		return nil, nil
	case "flatten":
		if len(args) == 0 || args[0] == nil {
			return nil, nil
		}
		return flattenValues(args[0]), nil
	case "min":
		return listExtremum(args, true, st.compareContext())
	case "max":
		return listExtremum(args, false, st.compareContext())
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
	case "slice":
		return listSlice(args)
	case "tail":
		if len(args) == 0 || args[0] == nil {
			return nil, nil
		}
		return listTakeSkip([][]any{args[0], []any{int64(1)}}, false)
	case "indexof":
		return st.listIndexOf(args)
	case "positionof":
		return stringPositionOf(args, false)
	case "lastpositionof":
		return stringPositionOf(args, true)
	case "round", "abs", "floor", "ceiling", "truncate", "ln", "log", "exp", "power":
		return evalMath(n, args)
	case "convertquantity":
		return convertQuantityArgs(args, st.engine.ucum())
	case "splitonmatches":
		return stringSplitOnMatches(args)
	case "replacematches":
		return stringReplaceMatches(args)
	case "distinct":
		if len(args) == 0 {
			return nil, nil
		}
		return st.distinctValues(args[0]), nil
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
	case "matches":
		return stringMatches(args, false)
	case "matchesfull":
		return stringMatches(args, true)
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
		return st.evalIndexer(args, nil)
	case "collapse":
		if len(args) == 0 {
			return nil, nil
		}
		var per *Quantity
		if len(args) > 1 && len(args[1]) > 0 {
			if q, ok := asQuantity(args[1][0]); ok {
				per = &q
			} else if f, ok := asFloat(args[1][0]); ok {
				per = &Quantity{Value: f}
			}
		}
		return collapseIntervals(args[0], per, st.compareContext()), nil
	case "expand":
		if len(args) == 0 {
			return nil, nil
		}
		var per *Quantity
		if len(args) > 1 && len(args[1]) > 0 {
			if q, ok := asQuantity(args[1][0]); ok {
				per = &q
			} else if f, ok := asFloat(args[1][0]); ok {
				per = &Quantity{Value: f}
			}
		}
		return expandValues(args[0], per, st.compareContext()), nil
	case "toconcept":
		if len(args) == 0 || args[0] == nil {
			return nil, nil
		}
		if len(args[0]) == 1 {
			return toConceptValue(args[0][0]), nil
		}
		return toConceptValue(args[0]), nil
	case "tochars":
		item, ok := singletonArg(args)
		if !ok {
			return nil, nil
		}
		s, ok := unwrapPrimitive(item).(string)
		if !ok {
			return nil, nil
		}
		out := make([]any, 0, len(s))
		for _, r := range s {
			out = append(out, string(r))
		}
		return out, nil
	case "convertstointeger", "convertstolong", "convertstodecimal", "convertstoboolean",
		"convertstostring", "convertstoquantity", "convertstodate", "convertstodatetime", "convertstotime":
		return convertsToResult(n, args)
	case "canconvertquantity":
		if len(args) < 2 || args[0] == nil || args[1] == nil || len(args[0]) == 0 || len(args[1]) == 0 {
			return nil, nil
		}
		q, ok := asQuantity(args[0][0])
		if !ok {
			return []any{false}, nil
		}
		unit, ok := quantityUnitArg(args[1][0])
		if !ok {
			return []any{false}, nil
		}
		_, ok = convertQuantityValue(q, unit, st.engine.ucum())
		return []any{ok}, nil
	case "median":
		return listMedian(args)
	case "mode":
		return st.listMode(args)
	case "stddev", "stdev", "standarddeviation":
		return listStdDev(args)
	case "variance":
		return listVariance(args)
	case "populationvariance":
		return listPopulationVariance(args)
	case "populationstddev", "populationstdev":
		return listPopulationStdDev(args)
	case "toratio":
		return ratioFromArgs(args)
	case "repeat":
		return listRepeat(args)
	case "times":
		return st.listTimes(args)
	case "contains":
		return stringContains(args)
	case "in":
		return stringInFunction(args)
	case "truncatequantity":
		return truncateQuantity(args)
	case "convertstoratio":
		return convertsToRatio(args)
	case "tolist":
		return evalToList(args)
	case "message":
		if len(args) == 0 || args[0] == nil || len(args[0]) == 0 {
			return nil, nil
		}
		return []any{args[0][0]}, nil
	case "product":
		return listProduct(args)
	case "geometricmean":
		return listGeometricMean(args)
	case "highboundary":
		return boundaryValue(args, true)
	case "lowboundary":
		return boundaryValue(args, false)
	case "precision":
		item, ok := singletonArg(args)
		if !ok {
			return nil, nil
		}
		p, ok := valuePrecision(item)
		if !ok {
			return nil, nil
		}
		return []any{int64(p)}, nil
	case "pointfrom":
		item, ok := singletonArg(args)
		if !ok {
			return nil, nil
		}
		if iv, ok := asInterval(item); ok {
			return pointFromInterval(iv, st.compareContext())
		}
		return nil, nil
	case "minvalue":
		return typeMinMaxValue(args, true)
	case "maxvalue":
		return typeMinMaxValue(args, false)
	case "children":
		return structureChildrenArgs(args)
	case "descendants":
		return structureDescendantsArgs(args)
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
	when = clockInZone(when)
	loc := when.Location()
	birthDay := birth.UTC()
	years := when.Year() - birthDay.Year()
	anniversary := time.Date(when.Year(), birthDay.Month(), birthDay.Day(), 0, 0, 0, 0, loc)
	if anniversary.Month() != birthDay.Month() {
		anniversary = time.Date(when.Year(), birthDay.Month()+1, 0, 0, 0, 0, 0, loc)
	}
	if when.Before(anniversary) {
		years--
	}
	if years < 0 {
		years = 0
	}
	return []any{int64(years)}, nil
}

func (st *evalState) evalRetrieve(n *retrieveNode) ([]any, error) {
	if strings.EqualFold(n.resourceType, "Patient") && st.patient != nil && n.terminology == "" {
		return st.filterRetrieveDates([]any{st.patient}, n)
	}
	if st.retriever == nil {
		return nil, errf("%w: retrieve [%s] requires a data retriever", ErrUnsupported, n.resourceType)
	}
	req := st.retrieveRequest(n)
	items, err := st.retriever.Retrieve(st.ctx, req, st.patient)
	if err != nil {
		return nil, err
	}
	if req.Terminology != "" || req.ValueSetURL != "" || req.Code != "" {
		var out []any
		for _, item := range items {
			ok, err := matchResourceTerminology(st.ctx, item, req, st.terminology(), st.resolveReferenceCodings)
			if err != nil {
				return nil, err
			}
			if ok {
				out = append(out, item)
			}
		}
		items = out
	}
	return st.filterRetrieveDates(items, n)
}

func (st *evalState) retrieveRequest(n *retrieveNode) RetrieveRequest {
	req := RetrieveRequest{ResourceType: n.resourceType, Terminology: n.terminology, Comparator: n.comparator, CodePath: n.codePath}
	if n.terminology == "" {
		return req
	}
	if cpt := st.lookupConcept(n.terminology); cpt != nil {
		return st.retrieveFromConcept(req, *cpt)
	}
	if strings.Contains(n.terminology, ";") {
		var terms []string
		for _, part := range strings.Split(n.terminology, ";") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			if c := st.lookupCode(part); c != nil {
				if c.System != "" && c.Code != "" {
					terms = append(terms, c.System+"|"+c.Code)
				} else if c.Code != "" {
					terms = append(terms, c.Code)
				} else {
					terms = append(terms, part)
				}
			} else {
				terms = append(terms, part)
			}
		}
		req.Terminology = strings.Join(terms, ";")
		return req
	}
	if n.comparator != "=" && n.comparator != "~" {
		if vs := st.lookupValueSetMatch(n.terminology, true); vs != nil {
			req.ValueSetURL = vs.URL
			return req
		}
		if c := st.lookupCodeMatch(n.terminology, true); c != nil {
			req.System = c.System
			req.Code = c.Code
			return req
		}
		if cpt := st.lookupConcept(n.terminology); cpt != nil {
			return st.retrieveFromConcept(req, *cpt)
		}
		if vs := st.lookupValueSetMatch(n.terminology, false); vs != nil {
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
	if cpt := st.lookupConcept(n.terminology); cpt != nil {
		return st.retrieveFromConcept(req, *cpt)
	}
	if sys, code, ok := splitSystemCode(n.terminology); ok {
		req.System = sys
		req.Code = code
	}
	return req
}

func (st *evalState) filterRetrieveDates(items []any, n *retrieveNode) ([]any, error) {
	if n == nil || (n.datePath == "" && n.dateLow == nil && n.dateHigh == nil) {
		return items, nil
	}
	window, apply, err := st.retrieveDateWindow(n)
	if err != nil {
		return nil, err
	}
	if !apply {
		return items, nil
	}
	if window == nil {
		return []any{}, nil
	}
	path := n.datePath
	if path == "" {
		path = "effective"
	}
	var out []any
	for _, item := range items {
		vals, ok := fieldValues(item, path)
		if !ok {
			continue
		}
		for _, v := range vals {
			if st.valueInDateWindow(v, *window) {
				out = append(out, item)
				break
			}
		}
	}
	return out, nil
}

func (st *evalState) retrieveDateWindow(n *retrieveNode) (*Interval, bool, error) {
	low, err := st.eval(n.dateLow)
	if err != nil {
		return nil, false, err
	}
	high, err := st.eval(n.dateHigh)
	if err != nil {
		return nil, false, err
	}
	if len(low) > 1 || len(high) > 1 {
		return nil, true, nil
	}
	if len(low) == 0 && len(high) == 0 {
		return nil, false, nil
	}
	if n.dateHigh == nil && len(low) == 1 {
		if iv, ok := asInterval(low[0]); ok {
			return &iv, true, nil
		}
	}
	if n.dateLow == nil && len(high) == 1 {
		if iv, ok := asInterval(high[0]); ok {
			return &iv, true, nil
		}
	}
	lowClosed, err := st.evalClosed(n.dateLowClosed, n.dateLowClosedExpr)
	if err != nil {
		return nil, false, err
	}
	highClosed, err := st.evalClosed(n.dateHighClosed, n.dateHighClosedExpr)
	if err != nil {
		return nil, false, err
	}
	iv := Interval{LowClosed: lowClosed, HighClosed: highClosed}
	if len(low) == 1 {
		iv.Low = singletonOrList(low)
	}
	if len(high) == 1 {
		iv.High = singletonOrList(high)
	}
	return &iv, true, nil
}

func (st *evalState) valueInDateWindow(v any, window Interval) bool {
	if v == nil {
		return false
	}
	cmp := st.compareContext()
	if iv, ok := asInterval(v); ok {
		return intervalOverlaps(iv, window, cmp)
	}
	ok, comparable := intervalContains(window, v, false, cmp)
	return comparable && ok
}

func (st *evalState) evalClosed(flag bool, expr Node) (bool, error) {
	if expr == nil {
		return flag, nil
	}
	v, err := st.eval(expr)
	if err != nil {
		return flag, err
	}
	if b := asBool(v); b != nil {
		return *b, nil
	}
	return flag, nil
}

func looksLikeCanonical(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	return strings.Contains(s, "://") || strings.HasPrefix(strings.ToLower(s), "urn:")
}

func (st *evalState) resolveReferenceCodings(ref string) []fhirCoding {
	if st == nil || st.retriever == nil {
		return nil
	}
	rt, id := splitReference(ref)
	if rt == "" || id == "" {
		return nil
	}
	items, err := st.retriever.Retrieve(st.ctx, RetrieveRequest{ResourceType: rt}, nil)
	if err != nil {
		return nil
	}
	var out []fhirCoding
	for _, item := range items {
		got := ""
		if env, ok := item.(*types.ResourceEnvelope); ok && env != nil {
			got = env.ID
		}
		if got == "" {
			if obj, ok := asObject(item); ok {
				got, _ = obj["id"].(string)
			}
		}
		if got != id {
			continue
		}
		out = append(out, extractCodings(item, "", nil)...)
	}
	return out
}

func (st *evalState) terminology() fhirpath.TerminologyValidator {
	if st.engine == nil {
		return nil
	}
	return st.engine.terminology
}

func (st *evalState) subsumption() fhirpath.SubsumptionValidator {
	term := st.terminology()
	if term == nil {
		return nil
	}
	sub, _ := term.(fhirpath.SubsumptionValidator)
	return sub
}

func (st *evalState) lookupValueSet(name string) *ValueSet {
	return st.lookupValueSetMatch(name, false)
}

func (st *evalState) lookupValueSetMatch(name string, exact bool) *ValueSet {
	search := func(lib *Library) *ValueSet {
		if lib == nil {
			return nil
		}
		for i := range lib.ValueSets {
			if lib.ValueSets[i].Name == name || (!exact && strings.EqualFold(lib.ValueSets[i].Name, name)) {
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
	return st.lookupCodeMatch(name, false)
}

func (st *evalState) lookupCodeSystem(name string) *CodeSystem {
	search := func(lib *Library) *CodeSystem {
		if lib == nil {
			return nil
		}
		for i := range lib.CodeSystems {
			if lib.CodeSystems[i].Name == name || strings.EqualFold(lib.CodeSystems[i].Name, name) {
				return &lib.CodeSystems[i]
			}
		}
		return nil
	}
	if cs := search(st.current); cs != nil {
		return cs
	}
	for _, lib := range st.libraries {
		if cs := search(lib); cs != nil {
			return cs
		}
	}
	return nil
}

func (st *evalState) lookupConcept(name string) *Concept {
	search := func(lib *Library) *Concept {
		if lib == nil {
			return nil
		}
		for i := range lib.Concepts {
			if lib.Concepts[i].Name == name || strings.EqualFold(lib.Concepts[i].Name, name) {
				return &lib.Concepts[i]
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

func (st *evalState) conceptValues(cpt Concept) []any {
	var out []any
	for _, name := range cpt.Codes {
		if c := st.lookupCode(name); c != nil {
			out = append(out, *c)
		} else {
			out = append(out, Code{Code: name})
		}
	}
	return out
}

func (st *evalState) retrieveFromConcept(req RetrieveRequest, cpt Concept) RetrieveRequest {
	var terms []string
	for _, name := range cpt.Codes {
		if c := st.lookupCode(name); c != nil {
			if c.System != "" && c.Code != "" {
				terms = append(terms, c.System+"|"+c.Code)
			} else if c.Code != "" {
				terms = append(terms, c.Code)
			} else {
				terms = append(terms, name)
			}
		} else {
			terms = append(terms, name)
		}
	}
	if len(terms) == 0 {
		req.Terminology = cpt.Name
		return req
	}
	if len(terms) == 1 {
		if sys, code, ok := splitSystemCode(terms[0]); ok {
			req.System = sys
			req.Code = code
			return req
		}
		req.Terminology = terms[0]
		return req
	}
	req.Terminology = strings.Join(terms, ";")
	return req
}

func (st *evalState) lookupCodeMatch(name string, exact bool) *Code {
	search := func(lib *Library) *Code {
		if lib == nil {
			return nil
		}
		for i := range lib.Codes {
			if lib.Codes[i].Name == name || (!exact && strings.EqualFold(lib.Codes[i].Name, name)) {
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
		ok, err := matchResourceTerminology(st.ctx, v, req, st.terminology(), st.resolveReferenceCodings)
		if err != nil {
			return false, err
		}
		if ok {
			return true, nil
		}
	}
	return false, nil
}

func (st *evalState) allInValueSetResult(values []any, vs ValueSet) ([]any, error) {
	if len(values) == 0 {
		return []any{true}, nil
	}
	req := RetrieveRequest{ValueSetURL: vs.URL, Terminology: vs.Name}
	unknown := false
	tested := false
	for _, v := range values {
		if v == nil || unwrapPrimitive(v) == nil {
			unknown = true
			continue
		}
		tested = true
		ok, err := matchResourceTerminology(st.ctx, v, req, st.terminology(), st.resolveReferenceCodings)
		if err != nil {
			return nil, err
		}
		if !ok {
			return []any{false}, nil
		}
	}
	if !tested && unknown {
		return nil, nil
	}
	if unknown {
		return nil, nil
	}
	return []any{true}, nil
}

func singletonCodeSystem(v []any) (CodeSystem, bool) {
	if len(v) != 1 {
		return CodeSystem{}, false
	}
	cs, ok := v[0].(CodeSystem)
	return cs, ok
}

func (st *evalState) inCodeSystem(values []any, cs CodeSystem) (bool, error) {
	for _, v := range values {
		if codingInCodeSystem(v, cs) {
			return true, nil
		}
	}
	return false, nil
}

func (st *evalState) allInCodeSystemResult(values []any, cs CodeSystem) ([]any, error) {
	if len(values) == 0 {
		return []any{true}, nil
	}
	unknown := false
	tested := false
	for _, v := range values {
		if v == nil || unwrapPrimitive(v) == nil {
			unknown = true
			continue
		}
		tested = true
		if !codingInCodeSystem(v, cs) {
			return []any{false}, nil
		}
	}
	if !tested && unknown {
		return nil, nil
	}
	if unknown {
		return nil, nil
	}
	return []any{true}, nil
}

func codingInCodeSystem(item any, cs CodeSystem) bool {
	url := cs.URL
	if url == "" {
		url = cs.Name
	}
	if url == "" {
		return false
	}
	match := func(system string) bool {
		return system == url || strings.EqualFold(system, url)
	}
	switch x := unwrapPrimitive(item).(type) {
	case Code:
		return match(x.System)
	case fhirCoding:
		return match(x.System)
	}
	if obj, ok := asObject(item); ok {
		if match(strField(obj, "system")) {
			return true
		}
		if raw, ok := obj["coding"].([]any); ok {
			for _, c := range raw {
				if m, ok := asObject(c); ok && match(strField(m, "system")) {
					return true
				}
			}
		}
	}
	return false
}

func (st *evalState) allContainsResult(haystack, needles []any) []any {
	if len(needles) == 0 {
		return []any{true}
	}
	unknown := false
	tested := false
	for _, n := range needles {
		if n == nil || unwrapPrimitive(n) == nil {
			unknown = true
			continue
		}
		tested = true
		res := st.containsResult(haystack, n)
		if res == nil {
			unknown = true
			continue
		}
		if res[0] != true {
			return []any{false}
		}
	}
	if !tested && unknown {
		return nil
	}
	if unknown {
		return nil
	}
	return []any{true}
}

func (st *evalState) anyContainsResult(haystack, needles []any) []any {
	if len(needles) == 0 {
		return []any{false}
	}
	unknown := false
	tested := false
	for _, n := range needles {
		if n == nil || unwrapPrimitive(n) == nil {
			unknown = true
			continue
		}
		tested = true
		res := st.containsResult(haystack, n)
		if res == nil {
			unknown = true
			continue
		}
		if res[0] == true {
			return []any{true}
		}
	}
	if !tested && unknown {
		return nil
	}
	if unknown {
		return nil
	}
	return []any{false}
}

func (st *evalState) evalDateComponent(v []any, component string) ([]any, error) {
	if len(v) != 1 {
		return nil, nil
	}
	tm, ok := asTime(v[0])
	if !ok {
		if s, isStr := unwrapPrimitive(v[0]).(string); isStr {
			if parsed, err := parseCQLDate(s); err == nil {
				tm = parsed
				ok = true
			} else if parsed, pok := parseELMTimeString(s); pok {
				tm = parsed
				ok = true
			}
		}
	}
	if !ok {
		return nil, nil
	}
	switch strings.ToLower(strings.TrimSpace(component)) {
	case "date":
		return []any{time.Date(tm.Year(), tm.Month(), tm.Day(), 0, 0, 0, 0, dateOnlyLoc)}, nil
	case "time":
		now := clockInZone(st.now)
		return []any{time.Date(now.Year(), now.Month(), now.Day(), tm.Hour(), tm.Minute(), tm.Second(), tm.Nanosecond(), timeOnlyLoc)}, nil
	case "year":
		return []any{int64(tm.Year())}, nil
	case "month":
		return []any{int64(tm.Month())}, nil
	case "day":
		return []any{int64(tm.Day())}, nil
	case "hour":
		return []any{int64(tm.Hour())}, nil
	case "minute":
		return []any{int64(tm.Minute())}, nil
	case "second":
		return []any{int64(tm.Second())}, nil
	case "millisecond":
		return []any{int64(tm.Nanosecond() / 1e6)}, nil
	case "week":
		_, w := tm.ISOWeek()
		return []any{int64(w)}, nil
	case "timezoneoffset", "timezone":
		_, off := tm.Zone()
		return []any{float64(off) / 3600}, nil
	}
	return nil, errf("%w: date component %q", ErrUnsupported, component)
}

func (st *evalState) memberValues(item any, name string) ([]any, bool) {
	if st.engine != nil && st.engine.fhirpath != nil && fhirPathNavigable(item) {
		vals, err := st.engine.fhirpath.Eval(st.ctx, name, item)
		if err == nil {
			if out, ok := convertFHIRPathCollection(vals); ok && len(out) > 0 {
				return coerceTemporalValues(name, out), true
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
		if _, ok := v[0].([]any); ok {
			return v
		}
		return v[0]
	}
	return v
}

func asBool(v []any) *bool {
	if len(v) != 1 {
		return nil
	}
	if x, ok := unwrapPrimitive(v[0]).(bool); ok {
		return &x
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
	if x, ok := unwrapPrimitive(v).(time.Time); ok {
		return x, true
	}
	return time.Time{}, false
}

func temporalLocation(v any) *time.Location {
	if t, ok := asTime(unwrapPrimitive(v)); ok && t.Location() != nil && !isDateOnlyTime(t) {
		return t.Location()
	}
	return nil
}

func asTemporal(v any, loc *time.Location) (time.Time, bool) {
	t, ok := asTime(v)
	if !ok {
		return time.Time{}, false
	}
	if isFloatingTime(t) {
		if loc == nil {
			loc = t.Location()
			if loc == nil {
				loc = time.UTC
			}
		}
		return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), loc), true
	}
	return t, true
}

func (st *evalState) membershipItem(n Node, v []any) any {
	return st.membershipItemLets(n, v, nil)
}

func (st *evalState) membershipItemLets(n Node, v []any, listLets map[string]bool) any {
	if st.isListValuedExpr(n, listLets) {
		if v == nil {
			return []any{}
		}
		return append([]any{}, v...)
	}
	if len(v) == 0 {
		return nil
	}
	if len(v) == 1 {
		return v[0]
	}
	return append([]any{}, v...)
}

func (st *evalState) wrapListElement(el Node, v []any) any {
	return st.wrapListElementLets(el, v, nil)
}

func (st *evalState) wrapListElementLets(el Node, v []any, listLets map[string]bool) any {
	if st.isListValuedExpr(el, listLets) {
		if v == nil {
			return []any{}
		}
		return append([]any{}, v...)
	}
	if len(v) == 0 {
		return nil
	}
	if len(v) == 1 {
		return v[0]
	}
	return append([]any{}, v...)
}

func (st *evalState) isListValued(n Node) bool {
	return st.isListValuedExpr(n, nil)
}

func (st *evalState) isListValuedExpr(n Node, listLets map[string]bool) bool {
	return st.isListValuedExprVisit(n, listLets, nil)
}

func (st *evalState) isListValuedExprVisit(n Node, listLets map[string]bool, visiting map[string]bool) bool {
	if n == nil {
		return false
	}
	switch x := n.(type) {
	case *listNode, *retrieveNode, *queryNode:
		return true
	case *identNode:
		if listLetIsListValued(listLets, x.name) {
			return true
		}
		if st.isListTypedName(x.name) {
			return true
		}
		if def, _ := st.lookupDefine(x.name); def != nil && def.Expression != nil {
			key := "def:" + strings.ToLower(x.name)
			if visiting != nil && visiting[key] {
				return false
			}
			if visiting == nil {
				visiting = map[string]bool{}
			}
			visiting[key] = true
			return st.isListValuedExprVisit(def.Expression, listLets, visiting)
		}
		if fn, _ := st.lookupFunction(x.name); fn != nil && len(fn.Params) == 0 {
			if isListParameterType(fn.ReturnType) {
				return true
			}
			if fn.Body != nil {
				key := "fn:" + strings.ToLower(x.name)
				if visiting != nil && visiting[key] {
					return false
				}
				if visiting == nil {
					visiting = map[string]bool{}
				}
				visiting[key] = true
				return st.isListValuedExprVisit(fn.Body, listLets, visiting)
			}
		}
		return false
	case *binaryNode:
		if x.op == "|" {
			return st.isListValuedExprVisit(x.left, listLets, visiting) &&
				st.isListValuedExprVisit(x.right, listLets, visiting)
		}
	case *callNode:
		name, ok := callCalleeName(x.callee)
		if !ok {
			return false
		}
		if fn, _ := st.lookupFunction(name); fn != nil && len(fn.Params) == len(x.args) {
			if isListParameterType(fn.ReturnType) {
				return true
			}
			if fn.Body != nil {
				key := "fn:" + strings.ToLower(name)
				if visiting != nil && visiting[key] {
					return false
				}
				if visiting == nil {
					visiting = map[string]bool{}
				}
				visiting[key] = true
				return st.isListValuedExprVisit(fn.Body, listLets, visiting)
			}
		}
	}
	return false
}

func callCalleeName(n Node) (string, bool) {
	switch c := n.(type) {
	case *identNode:
		return c.name, true
	case *memberNode:
		return c.name, true
	}
	return "", false
}

func (st *evalState) lookupParameter(name string) (Parameter, bool) {
	search := func(lib *Library) (Parameter, bool) {
		if lib == nil {
			return Parameter{}, false
		}
		for _, p := range lib.Parameters {
			if p.Name == name || strings.EqualFold(p.Name, name) {
				return p, true
			}
		}
		return Parameter{}, false
	}
	if p, ok := search(st.current); ok {
		return p, true
	}
	for _, lib := range st.libraries {
		if p, ok := search(lib); ok {
			return p, true
		}
	}
	return Parameter{}, false
}

func (st *evalState) isListTypedName(name string) bool {
	if p, ok := st.lookupParameter(name); ok && isListParameterType(p.Type) {
		return true
	}
	if st.functionParams != nil {
		for _, p := range st.functionParams {
			if p.Name == name || strings.EqualFold(p.Name, name) {
				return isListParameterType(p.Type)
			}
		}
	}
	return false
}

func listLetIsListValued(listLets map[string]bool, name string) bool {
	if listLets == nil {
		return false
	}
	if listLets[name] {
		return true
	}
	for k, v := range listLets {
		if v && strings.EqualFold(k, name) {
			return true
		}
	}
	return false
}

func isListParameterType(typ string) bool {
	typ = strings.TrimSpace(typ)
	if typ == "" {
		return false
	}
	if i := strings.Index(typ, "<"); i > 0 {
		return strings.EqualFold(typ[:i], "List")
	}
	return strings.EqualFold(typ, "List")
}

func listValueResult(v any) []any {
	if v == nil {
		return []any{nil}
	}
	if list, ok := v.([]any); ok {
		return append([]any{}, list...)
	}
	return []any{v}
}

func (st *evalState) cqlEqualResult(left, right []any) []any {
	if left == nil || right == nil {
		return nil
	}
	return st.cqlEqual3List(left, right)
}

func (st *evalState) cqlNotEqualResult(left, right []any) []any {
	if left == nil || right == nil {
		return nil
	}
	cmp := st.compareContext()
	if len(left) == 1 && len(right) == 1 {
		if qa, oka := asQuantity(left[0]); oka {
			if qb, okb := asQuantity(right[0]); okb {
				return []any{!cmp.Equivalent(qa, qb)}
			}
		}
	}
	eq := st.cqlEqualResult(left, right)
	if len(eq) == 0 {
		return nil
	}
	return []any{eq[0] != true}
}

func (st *evalState) cqlEqual3List(left, right []any) []any {
	cmp := st.compareContext()
	if len(left) != len(right) {
		return []any{false}
	}
	unknown := false
	for i := range left {
		eq := st.cqlEqual3Value(left[i], right[i], cmp)
		if eq == nil {
			unknown = true
			continue
		}
		if eq[0] != true {
			return []any{false}
		}
	}
	if unknown {
		return nil
	}
	return []any{true}
}

func (st *evalState) cqlEqual3Value(a, b any, cmp compareCtx) []any {
	a, b = unwrapPrimitive(a), unwrapPrimitive(b)
	if a == nil || b == nil {
		return nil
	}
	if la, ok := a.([]any); ok {
		lb, ok := b.([]any)
		if !ok {
			return []any{false}
		}
		return st.cqlEqual3List(la, lb)
	}
	if _, ok := b.([]any); ok {
		return []any{false}
	}
	return []any{cmp.Equal(a, b)}
}

func (st *evalState) cqlMember3Value(a, b any, cmp compareCtx) []any {
	a, b = unwrapPrimitive(a), unwrapPrimitive(b)
	if a == nil || b == nil {
		return nil
	}
	if la, ok := a.([]any); ok {
		lb, ok := b.([]any)
		if !ok {
			return []any{false}
		}
		if len(la) != len(lb) {
			return []any{false}
		}
		unknown := false
		for i := range la {
			eq := st.cqlMember3Value(la[i], lb[i], cmp)
			if eq == nil {
				unknown = true
				continue
			}
			if eq[0] != true {
				return []any{false}
			}
		}
		if unknown {
			return nil
		}
		return []any{true}
	}
	if _, ok := b.([]any); ok {
		return []any{false}
	}
	return []any{cmp.MemberEqual(a, b)}
}

func (st *evalState) cqlEquivalentResult(left, right []any) []any {
	if left == nil && right == nil {
		return []any{true}
	}
	if left == nil || right == nil {
		return []any{false}
	}
	return []any{st.cqlEquivalentValues(left, right)}
}

func (st *evalState) cqlEquivalentValues(left, right []any) bool {
	cmp := st.compareContext()
	if len(left) != 1 || len(right) != 1 {
		if len(left) != len(right) {
			return false
		}
		for i := range left {
			if !cmp.Equivalent(left[i], right[i]) {
				return false
			}
		}
		return true
	}
	return cmp.Equivalent(left[0], right[0])
}

func (st *evalState) evalArithmetic(op string, lv, rv any) ([]any, error) {
	conv := st.ucumConv()
	if r1, ok := asRatio(lv); ok {
		if r2, ok := asRatio(rv); ok {
			return evalRatioArith(op, r1, r2, conv)
		}
		if scaled, ok := scaleRatioByScalar(op, r1, rv); ok {
			return scaled, nil
		}
	}
	if q1, ok := asQuantity(lv); ok {
		if scaled, ok := scaleQuantityByScalar(op, q1, rv); ok {
			return scaled, nil
		}
		if q2, ok := asQuantity(rv); ok {
			return evalQuantityArith(op, q1, q2, conv)
		}
	}
	if q2, ok := asQuantity(rv); ok {
		if scaled, ok := scaleQuantityByScalarLeft(op, lv, q2); ok {
			return scaled, nil
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
			ls, aok := unwrapPrimitive(lv).(string)
			rs, bok := unwrapPrimitive(rv).(string)
			if aok && bok {
				return []any{ls + rs}, nil
			}
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
	case "^":
		out = math.Pow(lf, rf)
		if math.IsNaN(out) || math.IsInf(out, 0) {
			return nil, nil
		}
	}
	if isIntLike(lv) && isIntLike(rv) && op != "/" && (op != "^" || out == float64(int64(out))) {
		return []any{int64(out)}, nil
	}
	return []any{out}, nil
}

func singletonArg(args [][]any) (any, bool) {
	if len(args) == 0 || args[0] == nil || len(args[0]) != 1 {
		return nil, false
	}
	return args[0][0], true
}

func cqlToString(v any) (string, bool) {
	v = unwrapPrimitive(v)
	if v == nil {
		return "", false
	}
	if t, ok := asTime(v); ok {
		if isDateOnlyTime(t) {
			return t.Format("2006-01-02"), true
		}
		if isTimeOnlyTime(t) {
			if t.Nanosecond() == 0 {
				return t.Format("15:04:05"), true
			}
			return t.Format("15:04:05.000"), true
		}
		if isNaiveDateTimeTime(t) {
			if t.Nanosecond() == 0 {
				return t.Format("2006-01-02T15:04:05"), true
			}
			return t.Format("2006-01-02T15:04:05.000"), true
		}
		if t.Nanosecond() == 0 {
			return t.Format(time.RFC3339), true
		}
		return t.Format("2006-01-02T15:04:05.000Z07:00"), true
	}
	if r, ok := asRatio(v); ok {
		ns, nok := cqlToString(r.Numerator)
		ds, dok := cqlToString(r.Denominator)
		if nok && dok {
			return ns + " : " + ds, true
		}
	}
	if q, ok := asQuantity(v); ok {
		if isDimensionlessUnit(q.Unit) {
			if isIntLike(q.Value) {
				n, _ := asInt(q.Value)
				return strconv.FormatInt(n, 10), true
			}
			return strconv.FormatFloat(q.Value, 'f', -1, 64), true
		}
		if isIntLike(q.Value) || q.Value == float64(int64(q.Value)) {
			return fmt.Sprintf("%g '%s'", q.Value, q.Unit), true
		}
		return fmt.Sprintf("%g '%s'", q.Value, q.Unit), true
	}
	if s, ok := v.(string); ok {
		return s, true
	}
	if b, ok := v.(bool); ok {
		if b {
			return "true", true
		}
		return "false", true
	}
	if isIntLike(v) {
		n, _ := asInt(v)
		return strconv.FormatInt(n, 10), true
	}
	if f, ok := asFloat(v); ok {
		return strconv.FormatFloat(f, 'f', -1, 64), true
	}
	if iv, ok := asInterval(v); ok {
		return intervalToCQLString(iv)
	}
	if c, ok := unwrapPrimitive(v).(Code); ok {
		if c.System != "" {
			return fmt.Sprintf("'%s'|%s", c.Code, c.System), true
		}
		if c.Code != "" {
			return "'" + c.Code + "'", true
		}
	}
	if m, ok := asObject(v); ok {
		if s, ok := tupleToCQLString(m); ok {
			return s, true
		}
	}
	return fmt.Sprint(v), true
}

func tupleToCQLString(m map[string]any) (string, bool) {
	if rt, _ := m["resourceType"].(string); rt != "" {
		return "", false
	}
	if len(m) == 0 {
		return "{}", true
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		vs, ok := cqlToString(m[k])
		if !ok {
			return "", false
		}
		parts = append(parts, k+": "+vs)
	}
	return "{" + strings.Join(parts, ", ") + "}", true
}

func intervalToCQLString(iv Interval) (string, bool) {
	low, lok := cqlIntervalBoundString(iv.Low)
	high, hok := cqlIntervalBoundString(iv.High)
	if !lok || !hok {
		return "", false
	}
	left, right := "[", "]"
	if !iv.LowClosed {
		left = "("
	}
	if !iv.HighClosed {
		right = ")"
	}
	return "Interval" + left + low + ", " + high + right, true
}

func cqlIntervalBoundString(v any) (string, bool) {
	if v == nil {
		return "", false
	}
	return cqlToString(v)
}

func isIntLike(v any) bool {
	switch unwrapPrimitive(v).(type) {
	case int, int32, int64:
		return true
	}
	return false
}

func isWholeNumber(f float64) bool {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return false
	}
	return f == math.Trunc(f)
}

// cqlEqual is the package-default strict equality helper (default UCUM for ratios/interval bounds).
func cqlEqual(a, b any) bool {
	return cqlEqualWithUCUM(a, b, defaultUCUM)
}

func resourceIdentity(v any) (string, bool) {
	if env, ok := v.(*types.ResourceEnvelope); ok && env != nil {
		if env.ResourceType != "" && env.ID != "" {
			return env.ResourceType + "/" + env.ID, true
		}
	}
	if env, ok := v.(types.ResourceEnvelope); ok {
		if env.ResourceType != "" && env.ID != "" {
			return env.ResourceType + "/" + env.ID, true
		}
	}
	obj, ok := asObject(v)
	if !ok {
		return "", false
	}
	rt, _ := obj["resourceType"].(string)
	id, _ := obj["id"].(string)
	if rt != "" && id != "" {
		return rt + "/" + id, true
	}
	return "", false
}

func countNonNull(v []any) int64 {
	var n int64
	for _, item := range v {
		if unwrapPrimitive(item) != nil {
			n++
		}
	}
	return n
}

func existsNonNull(v []any) bool {
	for _, item := range v {
		if unwrapPrimitive(item) != nil {
			return true
		}
	}
	return false
}

func asCodeLike(v any) (fhirCoding, bool) {
	switch x := v.(type) {
	case Code:
		return fhirCoding{System: x.System, Code: x.Code, Display: x.Display}, true
	case fhirCoding:
		return x, true
	}
	obj, ok := asObject(v)
	if !ok {
		return fhirCoding{}, false
	}
	if isCoding(obj) {
		return fhirCoding{System: strField(obj, "system"), Code: strField(obj, "code"), Display: strField(obj, "display")}, true
	}
	if raw, ok := obj["coding"].([]any); ok && len(raw) > 0 {
		if m, ok := raw[0].(map[string]any); ok {
			return fhirCoding{System: strField(m, "system"), Code: strField(m, "code"), Display: strField(m, "display"), Text: strField(obj, "text")}, true
		}
	}
	return fhirCoding{}, false
}

func codesEquivalent(a, b fhirCoding) bool {
	if a.Code != "" && b.Code != "" && strings.EqualFold(a.Code, b.Code) {
		if a.System == "" || b.System == "" || strings.EqualFold(a.System, b.System) {
			return true
		}
	}
	if a.Display != "" && b.Display != "" && strings.EqualFold(a.Display, b.Display) {
		return true
	}
	if a.Text != "" && b.Text != "" && strings.EqualFold(a.Text, b.Text) {
		return true
	}
	return false
}

func clockInZone(t time.Time) time.Time {
	if t.IsZero() {
		return t
	}
	if t.Location() == nil {
		return t.UTC()
	}
	return t
}

func (st *evalState) containsMember(list []any, item any) bool {
	cmp := st.compareContext()
	for _, el := range list {
		if cmp.MemberEqual(el, item) {
			return true
		}
	}
	return false
}

func (st *evalState) containsResult(list []any, item any) []any {
	if item == nil {
		return nil
	}
	cmp := st.compareContext()
	unknown := false
	for _, el := range list {
		eq := st.cqlMember3Value(el, item, cmp)
		if eq == nil {
			unknown = true
			continue
		}
		if eq[0] == true {
			return []any{true}
		}
	}
	if unknown {
		return nil
	}
	return []any{false}
}

func (st *evalState) containsResultWithIntervals(list []any, item any) []any {
	if item == nil {
		return nil
	}
	if _, ok := asInterval(item); ok {
		return st.containsResult(list, item)
	}
	cmp := st.compareContext()
	unknown := false
	for _, el := range list {
		if iv, ok := asInterval(el); ok {
			ok, comparable := intervalContains(iv, item, false, cmp)
			if !comparable {
				unknown = true
				continue
			}
			if ok {
				return []any{true}
			}
			continue
		}
		eq := st.cqlMember3Value(el, item, cmp)
		if eq == nil {
			unknown = true
			continue
		}
		if eq[0] == true {
			return []any{true}
		}
	}
	if unknown {
		return nil
	}
	return []any{false}
}

func pointsVersusIntervals(points, intervals []any) bool {
	hasInterval := false
	for _, el := range intervals {
		if el == nil || unwrapPrimitive(el) == nil {
			continue
		}
		if _, ok := asInterval(el); !ok {
			return false
		}
		hasInterval = true
	}
	if !hasInterval {
		return false
	}
	for _, p := range points {
		if p == nil || unwrapPrimitive(p) == nil {
			continue
		}
		if _, ok := asInterval(p); ok {
			return false
		}
	}
	return true
}

func (st *evalState) allPointsInAnyInterval(points, intervals []any) []any {
	unknown := false
	for _, p := range points {
		res := st.containsResultWithIntervals(intervals, p)
		if res == nil {
			unknown = true
			continue
		}
		if res[0] != true {
			return []any{false}
		}
	}
	if unknown {
		return nil
	}
	return []any{true}
}

func (st *evalState) evalIs(n *isNode) ([]any, error) {
	v, err := st.eval(n.x)
	if err != nil {
		return nil, err
	}
	isNull := v == nil || (len(v) == 1 && v[0] == nil)
	if strings.EqualFold(n.target, "null") {
		result := isNull
		if n.not {
			result = !result
		}
		return []any{result}, nil
	}
	if isNull {
		return nil, nil
	}
	if len(v) == 0 {
		result := false
		if n.not {
			result = !result
		}
		return []any{result}, nil
	}
	matches := true
	unknown := false
	for _, item := range v {
		if item == nil || unwrapPrimitive(item) == nil {
			unknown = true
			continue
		}
		if !typeEquals(item, n.target) {
			matches = false
			break
		}
	}
	if !matches {
		result := false
		if n.not {
			result = !result
		}
		return []any{result}, nil
	}
	if unknown {
		return nil, nil
	}
	result := true
	if n.not {
		result = !result
	}
	return []any{result}, nil
}

func (st *evalState) evalAs(n *asNode) ([]any, error) {
	v, err := st.eval(n.x)
	if err != nil {
		return nil, err
	}
	if v == nil || (len(v) == 1 && v[0] == nil) {
		return nil, nil
	}
	if len(v) == 0 {
		return nil, nil
	}
	out := make([]any, len(v))
	for i, item := range v {
		if item == nil || unwrapPrimitive(item) == nil {
			return nil, nil
		}
		if !typeCompatible(item, n.target) {
			return nil, nil
		}
		out[i] = promoteAsValue(item, n.target)
	}
	return out, nil
}

func typeEquals(v any, target string) bool {
	return strings.EqualFold(typeName(v), target)
}

func typeCompatible(v any, target string) bool {
	if typeEquals(v, target) {
		return true
	}
	if strings.EqualFold(target, "Decimal") && typeEquals(v, "Integer") {
		return true
	}
	if strings.EqualFold(target, "DateTime") && typeEquals(v, "Date") {
		return true
	}
	if strings.EqualFold(target, "Date") && typeEquals(v, "DateTime") {
		return true
	}
	if (strings.EqualFold(target, "Integer") || strings.EqualFold(target, "Long")) && typeEquals(v, "Decimal") {
		f, ok := asFloat(unwrapPrimitive(v))
		return ok && isWholeNumber(f)
	}
	if strings.EqualFold(target, "Time") && typeEquals(v, "DateTime") {
		return true
	}
	if strings.EqualFold(target, "DateTime") && typeEquals(v, "Time") {
		return true
	}
	if q, ok := asQuantity(unwrapPrimitive(v)); ok {
		if strings.EqualFold(target, "Decimal") && isDimensionlessUnit(q.Unit) {
			return true
		}
		if (strings.EqualFold(target, "Integer") || strings.EqualFold(target, "Long")) &&
			isDimensionlessUnit(q.Unit) && isWholeNumber(q.Value) {
			return true
		}
	}
	return false
}

func promoteAsValue(v any, target string) any {
	v = unwrapPrimitive(v)
	if strings.EqualFold(target, "Integer") || strings.EqualFold(target, "Long") {
		if typeEquals(v, "Decimal") {
			f, ok := asFloat(v)
			if ok && isWholeNumber(f) {
				return int64(f)
			}
		}
	}
	if strings.EqualFold(target, "DateTime") && typeEquals(v, "Date") {
		if t, ok := asTime(v); ok && isDateOnlyTime(t) {
			loc := t.Location()
			if loc == nil {
				loc = time.UTC
			}
			return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
		}
	}
	if strings.EqualFold(target, "Date") && typeEquals(v, "DateTime") {
		if t, ok := asTime(v); ok {
			loc := t.Location()
			if loc == nil {
				loc = time.UTC
			}
			return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
		}
	}
	if strings.EqualFold(target, "Time") && typeEquals(v, "DateTime") {
		if t, ok := asTime(v); ok {
			loc := t.Location()
			if loc == nil {
				loc = time.UTC
			}
			return time.Date(1, 1, 1, t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), loc)
		}
	}
	if strings.EqualFold(target, "DateTime") && typeEquals(v, "Time") {
		if t, ok := asTime(v); ok && isTimeOnlyTime(t) {
			loc := t.Location()
			if loc == nil {
				loc = time.UTC
			}
			return time.Date(1, 1, 1, t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), loc)
		}
	}
	if q, ok := asQuantity(unwrapPrimitive(v)); ok && isDimensionlessUnit(q.Unit) {
		if strings.EqualFold(target, "Decimal") {
			return q.Value
		}
		if (strings.EqualFold(target, "Integer") || strings.EqualFold(target, "Long")) && isWholeNumber(q.Value) {
			return int64(q.Value)
		}
	}
	return v
}

func listOrStringLength(args [][]any) ([]any, error) {
	if len(args) == 0 || args[0] == nil {
		return nil, nil
	}
	v := args[0]
	if len(v) == 1 {
		item := unwrapPrimitive(v[0])
		if s, ok := item.(string); ok {
			return []any{int64(len(s))}, nil
		}
		if item != nil {
			if _, isList := v[0].([]any); !isList {
				return nil, nil
			}
		}
	}
	return []any{int64(len(v))}, nil
}

func (st *evalState) distinctValues(in []any) []any {
	if in == nil {
		return nil
	}
	out := []any{}
	for _, item := range in {
		if st.containsMember(out, item) {
			continue
		}
		out = append(out, item)
	}
	return out
}

func (st *evalState) listIntersect(left, right []any) []any {
	if left == nil || right == nil {
		return nil
	}
	out := []any{}
	for _, el := range left {
		if st.containsMember(right, el) && !st.containsMember(out, el) {
			out = append(out, el)
		}
	}
	return out
}

func (st *evalState) listExcept(left, right []any) []any {
	if left == nil {
		return nil
	}
	out := []any{}
	for _, el := range left {
		if !st.containsMember(right, el) && !st.containsMember(out, el) {
			out = append(out, el)
		}
	}
	return out
}

func typeName(v any) string {
	switch x := unwrapPrimitive(v).(type) {
	case bool:
		return "Boolean"
	case string:
		return "String"
	case int, int32, int64:
		return "Integer"
	case float32, float64:
		return "Decimal"
	case time.Time:
		if isDateOnlyTime(x) {
			return "Date"
		}
		if isTimeOnlyTime(x) {
			return "Time"
		}
		return "DateTime"
	case Quantity:
		return "Quantity"
	case Interval:
		return "Interval"
	case Ratio:
		return "Ratio"
	case Code:
		return "Code"
	}
	if _, ok := asRatio(v); ok {
		return "Ratio"
	}
	if obj, ok := asObject(v); ok {
		if rt, _ := obj["resourceType"].(string); rt != "" {
			return rt
		}
		return "Tuple"
	}
	return fmt.Sprintf("%T", v)
}

func fieldValues(v any, name string) ([]any, bool) {
	if v == nil {
		return nil, false
	}
	if obj, ok := asObject(v); ok {
		if raw, exists := obj[name]; exists {
			return flattenField(name, raw), true
		}
		var fold []string
		var choice []string
		for k := range obj {
			if strings.EqualFold(k, name) {
				fold = append(fold, k)
				continue
			}
			if len(k) > len(name) && strings.HasPrefix(k, name) {
				rest := k[len(name):]
				if rest != "" && rest[0] >= 'A' && rest[0] <= 'Z' {
					choice = append(choice, k)
				}
			}
		}
		sort.Strings(fold)
		sort.Strings(choice)
		if len(fold) > 0 {
			return flattenField(fold[0], obj[fold[0]]), true
		}
		if len(choice) > 0 {
			return flattenField(choice[0], obj[choice[0]]), true
		}
		return nil, false
	}
	return nil, false
}

func flattenField(name string, raw any) []any {
	return coerceTemporalValues(name, flattenJSON(raw))
}

func coerceTemporalValues(name string, vals []any) []any {
	if !isTemporalFieldName(name) {
		return vals
	}
	out := make([]any, len(vals))
	for i, v := range vals {
		if s, ok := unwrapPrimitive(v).(string); ok {
			if tm, err := parseCQLDate(s); err == nil {
				out[i] = tm
				continue
			}
		}
		out[i] = v
	}
	return out
}

func isTemporalFieldName(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	switch n {
	case "issued", "birthdate", "start", "end", "low", "high":
		return true
	}
	return strings.HasSuffix(n, "datetime") || strings.HasSuffix(n, "date") || strings.HasSuffix(n, "instant")
}

func flattenJSON(v any) []any {
	if v == nil {
		return []any{nil}
	}
	switch x := v.(type) {
	case []any:
		var out []any
		for _, el := range x {
			if _, ok := el.([]any); ok {
				out = append(out, el)
				continue
			}
			out = append(out, flattenJSON(el)...)
		}
		return out
	case string:
		return []any{x}
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
