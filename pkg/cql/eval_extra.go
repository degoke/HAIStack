package cql

import (
	"regexp"
	"strings"
	"time"
)

func (st *evalState) evalIndex(n *indexNode) ([]any, error) {
	base, err := st.eval(n.x)
	if err != nil {
		return nil, err
	}
	idx, err := st.eval(n.index)
	if err != nil {
		return nil, err
	}
	if len(idx) == 0 {
		return nil, nil
	}
	i, ok := asInt(idx[0])
	if !ok {
		return nil, nil
	}
	if !indexesAsList(n.x) && len(base) == 1 {
		if s, ok := unwrapPrimitive(base[0]).(string); ok {
			if i < 0 || int(i) >= len(s) {
				return nil, nil
			}
			return []any{string(s[i])}, nil
		}
	}
	if i < 0 || int(i) >= len(base) {
		return nil, nil
	}
	return []any{base[i]}, nil
}

func indexesAsList(n Node) bool {
	switch n.(type) {
	case *listNode, *retrieveNode, *queryNode, *memberNode:
		return true
	}
	return false
}

func (st *evalState) evalIndexer(args [][]any, src Node) ([]any, error) {
	if len(args) < 2 || len(args[0]) == 0 || len(args[1]) == 0 {
		return nil, nil
	}
	i, ok := asInt(args[1][0])
	if !ok {
		return nil, nil
	}
	if !indexesAsList(src) && len(args[0]) == 1 {
		if s, ok := unwrapPrimitive(args[0][0]).(string); ok {
			if i < 0 || int(i) >= len(s) {
				return nil, nil
			}
			return []any{string(s[i])}, nil
		}
	}
	if i < 0 || int(i) >= len(args[0]) {
		return nil, nil
	}
	return []any{args[0][i]}, nil
}

func (st *evalState) evalConvert(n *convertNode) ([]any, error) {
	v, err := st.eval(n.x)
	if err != nil {
		return nil, err
	}
	if len(v) == 0 {
		return nil, nil
	}
	if len(v) != 1 {
		return nil, nil
	}
	target := strings.ToLower(n.target)
	item := v[0]
	switch target {
	case "integer":
		n, ok := asInt(item)
		if !ok {
			return nil, nil
		}
		return []any{n}, nil
	case "decimal":
		f, ok := asFloat(item)
		if !ok {
			return nil, nil
		}
		return []any{f}, nil
	case "string":
		s, ok := cqlToString(item)
		if !ok {
			return nil, nil
		}
		return []any{s}, nil
	case "boolean":
		b := asBool(v)
		if b == nil {
			return nil, nil
		}
		return []any{*b}, nil
	case "datetime", "date":
		tm, ok := asTime(item)
		if !ok {
			return nil, nil
		}
		return []any{tm}, nil
	case "quantity":
		if q, ok := asQuantity(item); ok {
			return []any{q}, nil
		}
		return nil, nil
	case "interval":
		if iv, ok := asInterval(item); ok {
			return []any{iv}, nil
		}
		return nil, nil
	}
	return v, nil
}

func (st *evalState) evalSameAs(n *binaryNode) ([]any, error) {
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
	if len(left) != len(right) {
		return []any{false}, nil
	}
	unit, before, after := splitSameOp(n.op)
	unknown := false
	for i := range left {
		eq := sameAsPair(left[i], right[i], unit, before, after)
		if eq == nil {
			unknown = true
			continue
		}
		if eq[0] != true {
			return []any{false}, nil
		}
	}
	if unknown {
		return nil, nil
	}
	return []any{true}, nil
}

func sameAsPair(lv, rv any, unit string, before, after bool) []any {
	lt, lok := asTime(lv)
	rt, rok := asTime(rv)
	var cmp int
	var ok bool
	if lok && rok {
		cmp, ok = samePrecisionCompare(lt, rt, unit)
	} else {
		cmp, ok = cqlCompare(lv, rv)
	}
	if !ok {
		return nil
	}
	switch {
	case before:
		return []any{cmp <= 0}
	case after:
		return []any{cmp >= 0}
	default:
		return []any{cmp == 0}
	}
}

func splitSameOp(op string) (unit string, before, after bool) {
	op = strings.ToLower(strings.TrimSpace(op))
	before = strings.Contains(op, "before")
	after = strings.Contains(op, "after")
	rest := strings.TrimSpace(strings.TrimPrefix(op, "same"))
	rest = strings.TrimSpace(strings.TrimSuffix(rest, "as"))
	rest = strings.ReplaceAll(rest, "or before", "")
	rest = strings.ReplaceAll(rest, "or after", "")
	rest = strings.TrimSpace(rest)
	if u := timeUnitName(rest); u != "" {
		return u, before, after
	}
	return "", before, after
}

func samePrecisionCompare(lt, rt time.Time, unit string) (int, bool) {
	cmpInt := func(a, b int) int {
		if a < b {
			return -1
		}
		if a > b {
			return 1
		}
		return 0
	}
	switch unit {
	case "year":
		return cmpInt(lt.Year(), rt.Year()), true
	case "month":
		if c := cmpInt(lt.Year(), rt.Year()); c != 0 {
			return c, true
		}
		return cmpInt(int(lt.Month()), int(rt.Month())), true
	case "week":
		ly, lw := lt.ISOWeek()
		ry, rw := rt.ISOWeek()
		if c := cmpInt(ly, ry); c != 0 {
			return c, true
		}
		return cmpInt(lw, rw), true
	case "day":
		if c := cmpInt(lt.Year(), rt.Year()); c != 0 {
			return c, true
		}
		if c := cmpInt(int(lt.Month()), int(rt.Month())); c != 0 {
			return c, true
		}
		return cmpInt(lt.Day(), rt.Day()), true
	case "hour":
		if c, ok := samePrecisionCompare(lt, rt, "day"); !ok || c != 0 {
			return c, ok
		}
		return cmpInt(lt.Hour(), rt.Hour()), true
	case "minute":
		if c, ok := samePrecisionCompare(lt, rt, "hour"); !ok || c != 0 {
			return c, ok
		}
		return cmpInt(lt.Minute(), rt.Minute()), true
	case "second":
		if c, ok := samePrecisionCompare(lt, rt, "minute"); !ok || c != 0 {
			return c, ok
		}
		return cmpInt(lt.Second(), rt.Second()), true
	case "millisecond":
		if c, ok := samePrecisionCompare(lt, rt, "second"); !ok || c != 0 {
			return c, ok
		}
		return cmpInt(lt.Nanosecond()/1e6, rt.Nanosecond()/1e6), true
	}
	switch {
	case lt.Before(rt):
		return -1, true
	case lt.After(rt):
		return 1, true
	default:
		return 0, true
	}
}

func (st *evalState) evalAggregate(rows []queryRow, q *queryNode) ([]any, error) {
	agg := q.agg
	var acc []any
	if agg.starting != nil {
		v, err := st.eval(agg.starting)
		if err != nil {
			return nil, err
		}
		acc = v
	}
	for _, row := range rows {
		locals := copyQueryLocals(row.locals)
		locals[agg.name] = acc
		next, err := withQueryScope(st, locals, row.this, row.thisSet, func() ([]any, error) {
			return st.eval(agg.body)
		})
		if err != nil {
			return nil, err
		}
		acc = next
	}
	if agg.distinct {
		acc = distinctValues(acc)
	}
	return acc, nil
}

func collapseIntervals(v []any) []any {
	var ivs []Interval
	for _, item := range v {
		if item == nil || unwrapPrimitive(item) == nil {
			continue
		}
		iv, ok := asInterval(item)
		if !ok {
			return nil
		}
		ivs = append(ivs, iv)
	}
	if len(ivs) == 0 {
		if v == nil {
			return nil
		}
		return []any{}
	}
	changed := true
	for changed {
		changed = false
		var next []Interval
		used := map[int]bool{}
		for i := range ivs {
			if used[i] {
				continue
			}
			cur := ivs[i]
			for j := i + 1; j < len(ivs); j++ {
				if used[j] {
					continue
				}
				if intervalOverlaps(cur, ivs[j]) || intervalMeets(cur, ivs[j]) {
					merged, ok := intervalUnion(cur, ivs[j])
					if ok {
						cur = merged
						used[j] = true
						changed = true
					}
				}
			}
			next = append(next, cur)
		}
		ivs = next
	}
	out := make([]any, len(ivs))
	for i, iv := range ivs {
		out[i] = iv
	}
	return out
}

func intervalUnion(a, b Interval) (Interval, bool) {
	out := Interval{LowClosed: true, HighClosed: true}
	if a.Low == nil || b.Low == nil {
		out.Low = nil
	} else {
		cmp, ok := cqlCompare(a.Low, b.Low)
		if !ok {
			return Interval{}, false
		}
		if cmp <= 0 {
			out.Low, out.LowClosed = a.Low, a.LowClosed
		} else {
			out.Low, out.LowClosed = b.Low, b.LowClosed
		}
	}
	if a.High == nil || b.High == nil {
		out.High = nil
	} else {
		cmp, ok := cqlCompare(a.High, b.High)
		if !ok {
			return Interval{}, false
		}
		if cmp >= 0 {
			out.High, out.HighClosed = a.High, a.HighClosed
		} else {
			out.High, out.HighClosed = b.High, b.HighClosed
		}
	}
	return out, true
}

func constructDateArgInt(args [][]any, i int, def int) (int, bool) {
	if len(args) <= i || len(args[i]) == 0 {
		return def, true
	}
	n, ok := asInt(args[i][0])
	if !ok {
		return 0, false
	}
	return int(n), true
}

func constructDate(args [][]any, dateTime bool) ([]any, error) {
	if len(args) == 0 || len(args[0]) == 0 {
		return nil, nil
	}
	y, ok := constructDateArgInt(args, 0, 0)
	if !ok {
		return nil, nil
	}
	m, ok := constructDateArgInt(args, 1, 1)
	if !ok {
		return nil, nil
	}
	d, ok := constructDateArgInt(args, 2, 1)
	if !ok {
		return nil, nil
	}
	h, min, s, ns := 0, 0, 0, 0
	if dateTime {
		if h, ok = constructDateArgInt(args, 3, 0); !ok {
			return nil, nil
		}
		if min, ok = constructDateArgInt(args, 4, 0); !ok {
			return nil, nil
		}
		if s, ok = constructDateArgInt(args, 5, 0); !ok {
			return nil, nil
		}
		if ms, ok := constructDateArgInt(args, 6, 0); ok {
			ns = ms * int(time.Millisecond)
		} else {
			return nil, nil
		}
	}
	loc := naiveDateTimeLoc
	if !dateTime {
		loc = dateOnlyLoc
	} else if len(args) > 7 && len(args[7]) > 0 {
		off, ok := asFloat(args[7][0])
		if !ok {
			return nil, nil
		}
		loc = time.FixedZone("", int(off*3600))
	}
	return []any{time.Date(y, time.Month(m), d, h, min, s, ns, loc)}, nil
}

func (st *evalState) constructTime(args [][]any) ([]any, error) {
	h, m, s, ns := 0, 0, 0, 0
	if len(args) > 0 && len(args[0]) > 0 {
		if n, ok := asInt(args[0][0]); ok {
			h = int(n)
		}
	}
	if len(args) > 1 && len(args[1]) > 0 {
		if n, ok := asInt(args[1][0]); ok {
			m = int(n)
		}
	}
	if len(args) > 2 && len(args[2]) > 0 {
		if n, ok := asInt(args[2][0]); ok {
			s = int(n)
		}
	}
	if len(args) > 3 && len(args[3]) > 0 {
		if n, ok := asInt(args[3][0]); ok {
			ns = int(n) * int(time.Millisecond)
		}
	}
	now := time.Now()
	if st != nil && !st.now.IsZero() {
		now = clockInZone(st.now)
	}
	loc := now.Location()
	if loc == nil {
		loc = time.UTC
	}
	return []any{time.Date(now.Year(), now.Month(), now.Day(), h, m, s, ns, loc)}, nil
}

func stringPred(args [][]any, fn func(string, string) bool) ([]any, error) {
	if len(args) < 2 || len(args[0]) == 0 || len(args[1]) == 0 {
		return nil, nil
	}
	a, aok := unwrapPrimitive(args[0][0]).(string)
	b, bok := unwrapPrimitive(args[1][0]).(string)
	if !aok || !bok {
		return nil, nil
	}
	return []any{fn(a, b)}, nil
}

func stringContains(args [][]any) ([]any, error) {
	if len(args) < 2 || len(args[0]) == 0 || len(args[1]) == 0 {
		return nil, nil
	}
	a, aok := unwrapPrimitive(args[0][0]).(string)
	b, bok := unwrapPrimitive(args[1][0]).(string)
	if !aok || !bok {
		return nil, nil
	}
	return []any{strings.Contains(a, b)}, nil
}

func stringReplace(args [][]any) ([]any, error) {
	if len(args) < 3 || len(args[0]) == 0 || len(args[1]) == 0 || len(args[2]) == 0 {
		return nil, nil
	}
	s, sok := unwrapPrimitive(args[0][0]).(string)
	old, ook := unwrapPrimitive(args[1][0]).(string)
	newv, nok := unwrapPrimitive(args[2][0]).(string)
	if !sok || !ook || !nok {
		return nil, nil
	}
	return []any{strings.ReplaceAll(s, old, newv)}, nil
}

func stringSplit(args [][]any) ([]any, error) {
	if len(args) < 2 || len(args[0]) == 0 || len(args[1]) == 0 {
		return nil, nil
	}
	s, sok := unwrapPrimitive(args[0][0]).(string)
	sep, sepok := unwrapPrimitive(args[1][0]).(string)
	if !sok || !sepok {
		return nil, nil
	}
	parts := strings.Split(s, sep)
	out := make([]any, 0, len(parts))
	for _, p := range parts {
		out = append(out, p)
	}
	return out, nil
}

func stringCombine(args [][]any) ([]any, error) {
	if len(args) == 0 || args[0] == nil {
		return nil, nil
	}
	sep := ""
	if len(args) > 1 {
		if len(args[1]) == 0 {
			return nil, nil
		}
		s, ok := unwrapPrimitive(args[1][0]).(string)
		if !ok {
			return nil, nil
		}
		sep = s
	}
	var parts []string
	for _, el := range args[0] {
		item := unwrapPrimitive(el)
		if item == nil {
			continue
		}
		s, ok := item.(string)
		if !ok {
			return nil, nil
		}
		parts = append(parts, s)
	}
	return []any{strings.Join(parts, sep)}, nil
}

func stringCase(args [][]any, upper bool) ([]any, error) {
	if len(args) == 0 || len(args[0]) == 0 {
		return nil, nil
	}
	s, ok := unwrapPrimitive(args[0][0]).(string)
	if !ok {
		return nil, nil
	}
	if upper {
		return []any{strings.ToUpper(s)}, nil
	}
	return []any{strings.ToLower(s)}, nil
}

func stringSubstring(args [][]any) ([]any, error) {
	if len(args) == 0 || len(args[0]) == 0 {
		return nil, nil
	}
	s, ok := unwrapPrimitive(args[0][0]).(string)
	if !ok {
		return nil, nil
	}
	start := 0
	if len(args) > 1 && len(args[1]) > 0 {
		if n, ok := asInt(args[1][0]); ok {
			start = int(n)
		}
	}
	if start < 0 {
		start = 0
	}
	if start >= len(s) {
		return []any{""}, nil
	}
	end := len(s)
	if len(args) > 2 && len(args[2]) > 0 {
		if n, ok := asInt(args[2][0]); ok {
			end = start + int(n)
			if end > len(s) {
				end = len(s)
			}
		}
	}
	return []any{s[start:end]}, nil
}

func stringMatches(args [][]any, full bool) ([]any, error) {
	if len(args) < 2 || len(args[0]) == 0 || len(args[1]) == 0 {
		return nil, nil
	}
	s, sok := unwrapPrimitive(args[0][0]).(string)
	pat, pok := unwrapPrimitive(args[1][0]).(string)
	if !sok || !pok {
		return nil, nil
	}
	if full {
		if !strings.HasPrefix(pat, "^") {
			pat = "^" + pat
		}
		if !strings.HasSuffix(pat, "$") {
			pat = pat + "$"
		}
	}
	re, err := regexp.Compile(pat)
	if err != nil {
		return nil, nil
	}
	return []any{re.MatchString(s)}, nil
}
