package cql

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/degoke/health-ai-stack/pkg/fhirpath"
	"github.com/degoke/health-ai-stack/pkg/types"
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
	if st.thisSet {
		if v, ok := fieldValues(st.this, name); ok {
			return v, nil
		}
	}
	if v, ok := st.stack[name]; ok {
		return v, nil
	}
	if def, lib := st.lookupDefine(name); def != nil {
		return st.evalDefine(lib, *def)
	}
	if st.params != nil {
		if v, ok := st.params[name]; ok {
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
	if lib := st.lookupLibrary(name); lib != nil {
		return []any{lib}, nil
	}
	if st.patient != nil {
		if v, ok := fieldValues(st.patient, name); ok {
			return v, nil
		}
	}
	return nil, errf("%w: %s", ErrExpressionNotFound, name)
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

func (st *evalState) lookupLibrary(name string) *Library {
	for _, lib := range st.libraries {
		if lib == nil {
			continue
		}
		if lib.Name == name || strings.EqualFold(lib.Name, name) {
			return lib
		}
		for _, inc := range lib.Includes {
			if inc.Called == name || inc.Name == name {
				return lib
			}
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
		num, ok := asFloat(v[0])
		if !ok {
			return nil, errf("CQL unary '-' requires a number")
		}
		return []any{-num}, nil
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
		return append(append([]any{}, left...), right...), nil
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
		return []any{containsValue(left, right[0])}, nil
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
		return nil, errf("%w: %s.%s", ErrExpressionNotFound, lib.Name, n.name)
	}
	if ns, ok := singletonNS(base); ok {
		return []any{builtinNS{name: ns.name + "." + n.name}}, nil
	}
	var out []any
	for _, item := range base {
		vals, ok := fieldValues(item, n.name)
		if !ok {
			continue
		}
		out = append(out, vals...)
	}
	return out, nil
}

func (st *evalState) evalCall(n *callNode) ([]any, error) {
	name := callName(n.callee)
	// Method-style: X.where(...) / X.first() — delay where/select args so $this is bound.
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

func (st *evalState) evalFunction(name string, args [][]any) ([]any, error) {
	n := strings.ToLower(name)
	n = strings.TrimPrefix(n, "fhirhelpers.")
	n = strings.TrimPrefix(n, "system.")
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
	if st.retriever == nil {
		return nil, errf("%w: retrieve [%s] requires a data retriever", ErrUnsupported, n.resourceType)
	}
	if strings.EqualFold(n.resourceType, "Patient") && st.patient != nil {
		return []any{st.patient}, nil
	}
	items, err := st.retriever.Retrieve(st.ctx, RetrieveRequest{ResourceType: n.resourceType, Terminology: n.terminology}, st.patient)
	if err != nil {
		return nil, err
	}
	return items, nil
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
	if val, has := obj["value"]; has && len(obj) <= 3 {
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
