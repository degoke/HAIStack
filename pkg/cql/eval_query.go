package cql

import (
	"sort"
)

type queryRow struct {
	locals  map[string][]any
	this    any
	thisSet bool
	item    any
}

func (st *evalState) evalQuery(q *queryNode) ([]any, error) {
	if q == nil || len(q.sources) == 0 {
		return nil, nil
	}
	sourceVals := make([][]any, len(q.sources))
	for i, src := range q.sources {
		v, err := st.eval(src.expr)
		if err != nil {
			return nil, err
		}
		sourceVals[i] = v
	}
	var rows []queryRow
	for _, row := range cartesian(sourceVals) {
		locals := map[string][]any{}
		var this any
		thisSet := false
		for i, src := range q.sources {
			val := row[i]
			if src.alias != "" {
				if val == nil {
					locals[src.alias] = nil
				} else {
					locals[src.alias] = []any{val}
				}
			}
			if i == 0 {
				this, thisSet = val, true
			}
		}
		ok, err := withQueryScope(st, locals, this, thisSet, func() (bool, error) {
			for _, let := range q.lets {
				v, err := st.eval(let.expr)
				if err != nil {
					return false, err
				}
				st.stack[let.name] = v
				locals[let.name] = v
			}
			for _, rel := range q.related {
				pass, err := st.evalRelated(rel)
				if err != nil {
					return false, err
				}
				if !pass {
					return false, nil
				}
			}
			if q.where != nil {
				v, err := st.eval(q.where)
				if err != nil {
					return false, err
				}
				b := asBool(v)
				if b == nil || !*b {
					return false, nil
				}
			}
			return true, nil
		})
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		qr := queryRow{locals: copyQueryLocals(locals), this: this, thisSet: thisSet}
		if q.agg != nil {
			rows = append(rows, qr)
			continue
		}
		item, err := withQueryScope(st, qr.locals, this, thisSet, func() (any, error) {
			if q.ret != nil {
				v, err := st.eval(q.ret)
				if err != nil {
					return nil, err
				}
				return singletonOrList(v), nil
			}
			if thisSet {
				return this, nil
			}
			return nil, nil
		})
		if err != nil {
			return nil, err
		}
		qr.item = item
		rows = append(rows, qr)
	}
	if q.agg != nil {
		acc, err := st.evalAggregate(rows, q)
		if err != nil {
			return nil, err
		}
		if len(q.sort) == 0 {
			return acc, nil
		}
		aggRows := make([]queryRow, len(acc))
		for i, item := range acc {
			aggRows[i] = queryRow{item: item, this: item, thisSet: true}
		}
		if err := st.sortQuery(aggRows, q.sort); err != nil {
			return nil, err
		}
		out := make([]any, len(aggRows))
		for i, row := range aggRows {
			out[i] = row.item
		}
		return out, nil
	}
	if q.distinct {
		rows = st.distinctQueryRows(rows)
	}
	if len(q.sort) > 0 {
		if err := st.sortQuery(rows, q.sort); err != nil {
			return nil, err
		}
	}
	out := make([]any, len(rows))
	for i, row := range rows {
		out[i] = row.item
	}
	return out, nil
}

func copyQueryLocals(locals map[string][]any) map[string][]any {
	out := make(map[string][]any, len(locals))
	for k, v := range locals {
		out[k] = v
	}
	return out
}

func (st *evalState) distinctQueryRows(rows []queryRow) []queryRow {
	var out []queryRow
	var seen []any
	for _, row := range rows {
		if st.containsMember(seen, row.item) {
			continue
		}
		seen = append(seen, row.item)
		out = append(out, row)
	}
	return out
}

func (st *evalState) evalRelated(rel relatedClause) (bool, error) {
	vals, err := st.eval(rel.source.expr)
	if err != nil {
		return false, err
	}
	matched := false
	for _, item := range vals {
		locals := map[string][]any{}
		if rel.source.alias != "" {
			locals[rel.source.alias] = []any{item}
		}
		ok, err := withQueryScope(st, locals, item, true, func() (bool, error) {
			v, err := st.eval(rel.such)
			if err != nil {
				return false, err
			}
			b := asBool(v)
			return b != nil && *b, nil
		})
		if err != nil {
			return false, err
		}
		if ok {
			matched = true
			break
		}
	}
	if rel.without {
		return !matched, nil
	}
	return matched, nil
}

func withQueryScope[T any](st *evalState, locals map[string][]any, this any, thisSet bool, fn func() (T, error)) (T, error) {
	prevThis, prevSet := st.this, st.thisSet
	if thisSet {
		st.this, st.thisSet = this, true
	}
	prevStack := map[string][]any{}
	for k, v := range st.stack {
		prevStack[k] = v
	}
	for k, v := range locals {
		st.stack[k] = v
	}
	defer func() {
		st.this, st.thisSet = prevThis, prevSet
		st.stack = prevStack
	}()
	return fn()
}

func cartesian(lists [][]any) [][]any {
	if len(lists) == 0 {
		return nil
	}
	out := [][]any{{}}
	for _, list := range lists {
		if len(list) == 0 {
			return nil
		}
		var next [][]any
		for _, prefix := range out {
			for _, item := range list {
				row := append(append([]any{}, prefix...), item)
				next = append(next, row)
			}
		}
		out = next
	}
	return out
}

func (st *evalState) sortQuery(rows []queryRow, keys []sortItem) error {
	type keyed struct {
		row  queryRow
		keys []any
	}
	keyedRows := make([]keyed, 0, len(rows))
	for _, row := range rows {
		var ks []any
		_, err := withQueryScope(st, row.locals, row.this, row.thisSet, func() (bool, error) {
			for _, k := range keys {
				v, err := st.eval(k.expr)
				if err != nil {
					return false, err
				}
				ks = append(ks, singletonOrList(v))
			}
			return true, nil
		})
		if err != nil {
			return err
		}
		keyedRows = append(keyedRows, keyed{row: row, keys: ks})
	}
	sort.SliceStable(keyedRows, func(i, j int) bool {
		for ki, k := range keys {
			cmp, ok := st.compareContext().Compare(keyedRows[i].keys[ki], keyedRows[j].keys[ki])
			if !ok || cmp == 0 {
				continue
			}
			if k.desc {
				return cmp > 0
			}
			return cmp < 0
		}
		return false
	})
	for i := range keyedRows {
		rows[i] = keyedRows[i].row
	}
	return nil
}

func flattenValues(v []any) []any {
	if v == nil {
		return nil
	}
	out := []any{}
	for _, item := range v {
		switch x := item.(type) {
		case []any:
			out = append(out, flattenValues(x)...)
		default:
			out = append(out, item)
		}
	}
	return out
}

func listExtremum(args [][]any, min bool, vcmp compareCtx) ([]any, error) {
	if len(args) == 0 || args[0] == nil {
		return nil, nil
	}
	var best any
	found := false
	for _, item := range args[0] {
		if unwrapPrimitive(item) == nil {
			continue
		}
		if !found {
			best = item
			found = true
			continue
		}
		cmp, ok := vcmp.Compare(item, best)
		if !ok {
			continue
		}
		if min && cmp < 0 {
			best = item
		}
		if !min && cmp > 0 {
			best = item
		}
	}
	if !found {
		return nil, nil
	}
	return []any{best}, nil
}

func listSum(args [][]any) ([]any, error) {
	if len(args) == 0 {
		return nil, nil
	}
	sum := 0.0
	intLike := true
	n := 0
	for _, item := range args[0] {
		if unwrapPrimitive(item) == nil {
			continue
		}
		f, ok := asFloat(item)
		if !ok {
			return nil, nil
		}
		if !isIntLike(item) {
			intLike = false
		}
		sum += f
		n++
	}
	if n == 0 {
		return nil, nil
	}
	if intLike {
		return []any{int64(sum)}, nil
	}
	return []any{sum}, nil
}

func listAvg(args [][]any) ([]any, error) {
	if len(args) == 0 {
		return nil, nil
	}
	sum := 0.0
	n := 0
	for _, item := range args[0] {
		if unwrapPrimitive(item) == nil {
			continue
		}
		f, ok := asFloat(item)
		if !ok {
			return nil, nil
		}
		sum += f
		n++
	}
	if n == 0 {
		return nil, nil
	}
	return []any{sum / float64(n)}, nil
}

func listAllAny(args [][]any, all bool) ([]any, error) {
	if len(args) == 0 {
		return []any{all}, nil
	}
	for _, item := range args[0] {
		if unwrapPrimitive(item) == nil {
			continue
		}
		b := asBool([]any{item})
		if all {
			if b == nil || !*b {
				return []any{false}, nil
			}
		} else if b != nil && *b {
			return []any{true}, nil
		}
	}
	return []any{all}, nil
}

func listTakeSkip(args [][]any, take bool) ([]any, error) {
	if len(args) == 0 || args[0] == nil {
		return nil, nil
	}
	list := args[0]
	if len(args) < 2 || args[1] == nil || len(args[1]) == 0 {
		return nil, nil
	}
	n64, ok := asInt(args[1][0])
	if !ok {
		return nil, nil
	}
	n := int(n64)
	if n < 0 {
		n = 0
	}
	if take {
		if n > len(list) {
			n = len(list)
		}
		return append([]any{}, list[:n]...), nil
	}
	if n >= len(list) {
		return []any{}, nil
	}
	return append([]any{}, list[n:]...), nil
}

func listSlice(args [][]any) ([]any, error) {
	if len(args) == 0 || args[0] == nil {
		return nil, nil
	}
	list := args[0]
	start := 0
	end := len(list)
	if len(args) > 1 && args[1] != nil && len(args[1]) > 0 {
		n, ok := asInt(args[1][0])
		if !ok {
			return nil, nil
		}
		start = int(n)
	}
	if len(args) > 2 && args[2] != nil && len(args[2]) > 0 {
		n, ok := asInt(args[2][0])
		if !ok {
			return nil, nil
		}
		end = int(n)
	}
	if start < 0 {
		start = 0
	}
	if end > len(list) {
		end = len(list)
	}
	if start >= end {
		return []any{}, nil
	}
	return append([]any{}, list[start:end]...), nil
}

func listIndexOf(args [][]any) ([]any, error) {
	if len(args) < 2 || args[1] == nil || len(args[1]) == 0 {
		return nil, nil
	}
	item := args[1][0]
	if unwrapPrimitive(item) == nil {
		return nil, nil
	}
	for i, el := range args[0] {
		if cqlEqual(el, item) {
			return []any{int64(i)}, nil
		}
	}
	return []any{int64(-1)}, nil
}

func (st *evalState) evalCase(n *caseNode) ([]any, error) {
	if n.test != nil {
		test, err := st.eval(n.test)
		if err != nil {
			return nil, err
		}
		tv := singletonOrList(test)
		for _, w := range n.whens {
			when, err := st.eval(w.when)
			if err != nil {
				return nil, err
			}
			eq := st.cqlEqual3Value(tv, singletonOrList(when), st.compareContext())
			if len(eq) == 1 && eq[0] == true {
				return st.eval(w.then)
			}
		}
	} else {
		for _, w := range n.whens {
			when, err := st.eval(w.when)
			if err != nil {
				return nil, err
			}
			b := asBool(when)
			if b != nil && *b {
				return st.eval(w.then)
			}
		}
	}
	if n.elseN != nil {
		return st.eval(n.elseN)
	}
	return nil, nil
}
