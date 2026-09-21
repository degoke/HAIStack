package cql

import (
	"math"
	"regexp"
	"sort"
	"strconv"
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

func collapseIntervals(v []any, per *Quantity) []any {
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
				if collapseMergeable(cur, ivs[j], per) {
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

func collapseMergeable(a, b Interval, per *Quantity) bool {
	if intervalOverlaps(a, b) || intervalMeets(a, b) {
		return true
	}
	if per == nil {
		return false
	}
	if ae, ok := intervalExpandHigh(a, *per); ok && (intervalOverlaps(ae, b) || intervalMeets(ae, b) || intervalSuccessorMeets(ae, b)) {
		return true
	}
	if be, ok := intervalExpandHigh(b, *per); ok && (intervalOverlaps(a, be) || intervalMeets(a, be) || intervalSuccessorMeets(a, be)) {
		return true
	}
	return false
}

func intervalSuccessorMeets(a, b Interval) bool {
	if a.High != nil && b.Low != nil {
		if next, ok := successorValue(a.High, false); ok && cqlEqual(next, b.Low) {
			return true
		}
	}
	if b.High != nil && a.Low != nil {
		if next, ok := successorValue(b.High, false); ok && cqlEqual(next, a.Low) {
			return true
		}
	}
	return false
}

func intervalExpandHigh(iv Interval, per Quantity) (Interval, bool) {
	if iv.High == nil {
		return iv, false
	}
	next, ok := addPerValue(iv.High, per)
	if !ok {
		return iv, false
	}
	out := iv
	out.High = next
	return out, true
}

func addPerValue(v any, per Quantity) (any, bool) {
	if t, ok := asTime(v); ok {
		return addDuration(t, per, "+")
	}
	if q, ok := asQuantity(v); ok {
		if per.Unit == "" || sameUnit(q.Unit, per.Unit) {
			q.Value += per.Value
			return q, true
		}
		return Quantity{}, false
	}
	if isIntLike(v) {
		n, _ := asInt(v)
		if per.Value == float64(int64(per.Value)) {
			return n + int64(per.Value), true
		}
		return float64(n) + per.Value, true
	}
	if f, ok := asFloat(v); ok {
		return f + per.Value, true
	}
	return nil, false
}

func successorValue(v any, pred bool) (any, bool) {
	delta := 1
	if pred {
		delta = -1
	}
	if q, ok := asQuantity(v); ok {
		q.Value += float64(delta)
		return q, true
	}
	if t, ok := asTime(v); ok {
		if isDateOnlyTime(t) {
			return t.AddDate(0, 0, delta), true
		}
		return t.Add(time.Duration(delta) * time.Millisecond), true
	}
	if isIntLike(v) {
		n, _ := asInt(v)
		return n + int64(delta), true
	}
	if f, ok := asFloat(v); ok {
		return f + float64(delta)*1e-8, true
	}
	return nil, false
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

func stringPositionOf(args [][]any, last bool) ([]any, error) {
	if len(args) < 2 || len(args[0]) == 0 || len(args[1]) == 0 {
		return nil, nil
	}
	pat, pok := unwrapPrimitive(args[0][0]).(string)
	s, sok := unwrapPrimitive(args[1][0]).(string)
	if !pok || !sok {
		return nil, nil
	}
	idx := strings.Index(s, pat)
	if last {
		idx = strings.LastIndex(s, pat)
	}
	return []any{int64(idx)}, nil
}

func stringReplaceMatches(args [][]any) ([]any, error) {
	if len(args) < 3 || len(args[0]) == 0 || len(args[1]) == 0 || len(args[2]) == 0 {
		return nil, nil
	}
	s, sok := unwrapPrimitive(args[0][0]).(string)
	pat, pok := unwrapPrimitive(args[1][0]).(string)
	sub, subok := unwrapPrimitive(args[2][0]).(string)
	if !sok || !pok || !subok {
		return nil, nil
	}
	re, err := regexp.Compile(pat)
	if err != nil {
		return nil, nil
	}
	return []any{re.ReplaceAllString(s, sub)}, nil
}

func stringSplitOnMatches(args [][]any) ([]any, error) {
	if len(args) < 2 || len(args[0]) == 0 || len(args[1]) == 0 {
		return nil, nil
	}
	s, sok := unwrapPrimitive(args[0][0]).(string)
	pat, pok := unwrapPrimitive(args[1][0]).(string)
	if !sok || !pok {
		return nil, nil
	}
	re, err := regexp.Compile(pat)
	if err != nil {
		return nil, nil
	}
	parts := re.Split(s, -1)
	out := make([]any, 0, len(parts))
	for _, p := range parts {
		out = append(out, p)
	}
	return out, nil
}

func evalMath(name string, args [][]any) ([]any, error) {
	if len(args) == 0 || len(args[0]) == 0 {
		return nil, nil
	}
	item := args[0][0]
	if q, ok := asQuantity(item); ok {
		switch name {
		case "abs":
			if q.Value < 0 {
				q.Value = -q.Value
			}
			return []any{q}, nil
		case "floor":
			q.Value = math.Floor(q.Value)
			return []any{q}, nil
		case "ceiling":
			q.Value = math.Ceil(q.Value)
			return []any{q}, nil
		case "truncate":
			q.Value = math.Trunc(q.Value)
			return []any{q}, nil
		case "round":
			prec := 0
			if len(args) > 1 && len(args[1]) > 0 {
				if n, ok := asInt(args[1][0]); ok {
					prec = int(n)
				}
			}
			q.Value = roundTo(q.Value, prec)
			return []any{q}, nil
		}
	}
	f, ok := asFloat(item)
	if !ok {
		return nil, nil
	}
	switch name {
	case "abs":
		return []any{math.Abs(f)}, nil
	case "floor":
		return []any{math.Floor(f)}, nil
	case "ceiling":
		return []any{math.Ceil(f)}, nil
	case "truncate":
		return []any{math.Trunc(f)}, nil
	case "round":
		prec := 0
		if len(args) > 1 && len(args[1]) > 0 {
			if n, ok := asInt(args[1][0]); ok {
				prec = int(n)
			}
		}
		out := roundTo(f, prec)
		if prec == 0 {
			return []any{int64(out)}, nil
		}
		return []any{out}, nil
	case "ln":
		if f <= 0 {
			return nil, nil
		}
		return []any{math.Log(f)}, nil
	case "exp":
		out := math.Exp(f)
		if math.IsNaN(out) || math.IsInf(out, 0) {
			return nil, nil
		}
		return []any{out}, nil
	case "log":
		if f <= 0 {
			return nil, nil
		}
		base := 10.0
		if len(args) > 1 && len(args[1]) > 0 {
			b, ok := asFloat(args[1][0])
			if !ok || b <= 0 || b == 1 {
				return nil, nil
			}
			base = b
		}
		out := math.Log(f) / math.Log(base)
		if math.IsNaN(out) || math.IsInf(out, 0) {
			return nil, nil
		}
		return []any{out}, nil
	case "power":
		if len(args) < 2 || len(args[1]) == 0 {
			return nil, nil
		}
		exp, ok := asFloat(args[1][0])
		if !ok {
			return nil, nil
		}
		out := math.Pow(f, exp)
		if math.IsNaN(out) || math.IsInf(out, 0) {
			return nil, nil
		}
		if isIntLike(item) && isIntLike(args[1][0]) && out == float64(int64(out)) {
			return []any{int64(out)}, nil
		}
		return []any{out}, nil
	}
	return nil, nil
}

func roundTo(f float64, prec int) float64 {
	if prec <= 0 {
		return math.Round(f)
	}
	pow := math.Pow(10, float64(prec))
	return math.Round(f*pow) / pow
}

func convertQuantityArgs(args [][]any) ([]any, error) {
	if len(args) < 2 || len(args[0]) == 0 || len(args[1]) == 0 {
		return nil, nil
	}
	q, ok := asQuantity(args[0][0])
	if !ok {
		return nil, nil
	}
	unit, ok := quantityUnitArg(args[1][0])
	if !ok {
		return nil, nil
	}
	out, ok := convertQuantityValue(q, unit)
	if !ok {
		return nil, nil
	}
	return []any{out}, nil
}

func quantityUnitArg(v any) (string, bool) {
	if s, ok := unwrapPrimitive(v).(string); ok && strings.TrimSpace(s) != "" {
		return s, true
	}
	if q, ok := asQuantity(v); ok && q.Unit != "" {
		return q.Unit, true
	}
	return "", false
}

func quantityValuesComparable(qa, qb Quantity) (float64, float64, bool) {
	if sameUnit(qa.Unit, qb.Unit) {
		return qa.Value, qb.Value, true
	}
	if conv, ok := convertQuantityValue(qb, qa.Unit); ok {
		return qa.Value, conv.Value, true
	}
	if conv, ok := convertQuantityValue(qa, qb.Unit); ok {
		return conv.Value, qb.Value, true
	}
	if isTimeUnit(qa.Unit) && isTimeUnit(qb.Unit) {
		return toSeconds(qa), toSeconds(qb), true
	}
	return 0, 0, false
}

func ratioFromArgs(args [][]any) ([]any, error) {
	if len(args) < 2 || len(args[0]) == 0 || len(args[1]) == 0 {
		return nil, nil
	}
	num, ok1 := asQuantity(args[0][0])
	den, ok2 := asQuantity(args[1][0])
	if !ok1 || !ok2 {
		return nil, nil
	}
	return []any{Ratio{Numerator: num, Denominator: den}}, nil
}

func evalToList(args [][]any) ([]any, error) {
	if len(args) == 0 || args[0] == nil {
		return toListResult(nil), nil
	}
	return toListResult(args[0]), nil
}

func toListResult(v []any) []any {
	if len(v) == 0 {
		return []any{[]any{}}
	}
	if len(v) > 1 {
		return []any{v}
	}
	if unwrapPrimitive(v[0]) == nil {
		return []any{[]any{}}
	}
	if list, ok := v[0].([]any); ok {
		return []any{list}
	}
	return []any{[]any{v[0]}}
}

func cqlSubsumesResult(left, right []any, proper bool) ([]any, error) {
	if len(left) != 1 || len(right) != 1 {
		return nil, nil
	}
	ca, oka := asCodeLike(left[0])
	cb, okb := asCodeLike(right[0])
	if !oka || !okb {
		return nil, nil
	}
	ca, cb = alignCodings(ca, cb)
	ok := codingSubsumes(ca, cb)
	if ok && proper {
		ok = !codesEquivalent(ca, cb)
	}
	return []any{ok}, nil
}

func alignCodings(a, b fhirCoding) (fhirCoding, fhirCoding) {
	if a.System == "" && b.System != "" {
		a.System = b.System
	}
	if b.System == "" && a.System != "" {
		b.System = a.System
	}
	return a, b
}

func codingSubsumes(broad, narrow fhirCoding) bool {
	if codesEquivalent(broad, narrow) {
		return true
	}
	if broad.Code == "" || narrow.Code == "" {
		return false
	}
	if broad.System != "" && narrow.System != "" && !strings.EqualFold(broad.System, narrow.System) {
		return false
	}
	if strings.EqualFold(broad.Code, narrow.Code) {
		return true
	}
	bc := strings.ToLower(broad.Code)
	nc := strings.ToLower(narrow.Code)
	return codePrefixSubsumes(bc, nc)
}

func codePrefixSubsumes(bc, nc string) bool {
	if !strings.HasPrefix(nc, bc) || len(nc) <= len(bc) {
		return false
	}
	rest := nc[len(bc):]
	if rest[0] == '.' {
		return true
	}
	if strings.Contains(bc, "-") {
		return rest[0] == '-'
	}
	if rest[0] == '-' {
		return false
	}
	return true
}

func convertQuantityValue(q Quantity, unit string) (Quantity, bool) {
	if sameUnit(q.Unit, unit) {
		return Quantity{Value: q.Value, Unit: unit}, true
	}
	if isTimeUnit(q.Unit) && isTimeUnit(unit) {
		sec := toSeconds(q)
		den := toSeconds(Quantity{Value: 1, Unit: unit})
		if den == 0 {
			return Quantity{}, false
		}
		return Quantity{Value: sec / den, Unit: unit}, true
	}
	from, fromDim, fok := quantitySIFactor(q.Unit)
	to, toDim, tok := quantitySIFactor(unit)
	if !fok || !tok || to == 0 || fromDim != toDim {
		return Quantity{}, false
	}
	return Quantity{Value: q.Value * from / to, Unit: unit}, true
}

func quantitySIFactor(unit string) (float64, string, bool) {
	unit = normalizeQuantityUnit(unit)
	switch strings.ToLower(strings.TrimSpace(unit)) {
	case "[in_i]", "[in_us]", "in", "inch", "inches":
		return 0.0254, "length", true
	case "[lb_av]", "lb", "lbs", "pound", "pounds":
		return 453.59237, "mass", true
	case "[oz_av]", "oz", "ounce", "ounces":
		return 28.349523125, "mass", true
	case "[st_av]", "st", "stone", "stones":
		return 6350.29318, "mass", true
	case "[mi_us]", "[mi_i]", "mi", "mile", "miles":
		return 1609.344, "length", true
	case "g", "gm", "gram", "grams":
		return 1, "mass", true
	case "mg":
		return 0.001, "mass", true
	case "kg":
		return 1000, "mass", true
	case "mcg", "ug":
		return 1e-6, "mass", true
	case "m", "meter", "meters":
		return 1, "length", true
	case "cm":
		return 0.01, "length", true
	case "mm":
		return 0.001, "length", true
	case "km":
		return 1000, "length", true
	case "l", "liter", "liters":
		return 1, "volume", true
	case "ml":
		return 0.001, "volume", true
	case "dl":
		return 0.1, "volume", true
	}
	return 0, "", false
}

func normalizeQuantityUnit(unit string) string {
	u := strings.TrimSpace(unit)
	if strings.HasPrefix(u, "[") && strings.HasSuffix(u, "]") {
		return u
	}
	return u
}

func asRatio(v any) (Ratio, bool) {
	switch x := unwrapPrimitive(v).(type) {
	case Ratio:
		return x, true
	case *Ratio:
		if x == nil {
			return Ratio{}, false
		}
		return *x, true
	}
	obj, ok := asObject(v)
	if !ok {
		return Ratio{}, false
	}
	num, ok1 := asQuantity(obj["numerator"])
	den, ok2 := asQuantity(obj["denominator"])
	if !ok1 || !ok2 {
		return Ratio{}, false
	}
	return Ratio{Numerator: num, Denominator: den}, true
}

func ratioEqual(a, b Ratio) bool {
	if cqlEqual(a.Numerator, b.Numerator) && cqlEqual(a.Denominator, b.Denominator) {
		return true
	}
	return ratioCrossEqual(a, b)
}

func ratioEquivalent(a, b Ratio) bool {
	return cqlEquivalent(a.Numerator, b.Numerator) && cqlEquivalent(a.Denominator, b.Denominator)
}

func ratioCrossEqual(a, b Ratio) bool {
	left, right, ok := ratioCrossSI(a, b)
	if !ok {
		return false
	}
	return ratioFloatEqual(left, right)
}

func ratioCompare(a, b Ratio) (int, bool) {
	left, right, ok := ratioCrossSI(a, b)
	if !ok {
		return 0, false
	}
	if ratioFloatEqual(left, right) {
		return 0, true
	}
	if left < right {
		return -1, true
	}
	return 1, true
}

func ratioFloatEqual(a, b float64) bool {
	if a == b {
		return true
	}
	scale := math.Max(math.Abs(a), math.Abs(b))
	if scale == 0 {
		return true
	}
	return math.Abs(a-b) <= scale*1e-9
}

func ratioCrossSI(a, b Ratio) (float64, float64, bool) {
	if a.Denominator.Value == 0 || b.Denominator.Value == 0 {
		return 0, 0, false
	}
	left, okL := quantityProductSI(a.Numerator, b.Denominator)
	right, okR := quantityProductSI(b.Numerator, a.Denominator)
	return left, right, okL && okR
}

func quantityProductSI(q1, q2 Quantity) (float64, bool) {
	if isDimensionlessUnit(q1.Unit) {
		return q1.Value * scalarQuantitySI(q2), true
	}
	if isDimensionlessUnit(q2.Unit) {
		return scalarQuantitySI(q1) * q2.Value, true
	}
	f1, _, ok1 := quantitySIFactor(q1.Unit)
	f2, _, ok2 := quantitySIFactor(q2.Unit)
	if !ok1 || !ok2 {
		return 0, false
	}
	return q1.Value * f1 * q2.Value * f2, true
}

func scalarQuantitySI(q Quantity) float64 {
	if isDimensionlessUnit(q.Unit) {
		return q.Value
	}
	f, _, ok := quantitySIFactor(q.Unit)
	if !ok {
		return q.Value
	}
	return q.Value * f
}

func isDimensionlessUnit(unit string) bool {
	u := strings.TrimSpace(unit)
	return u == "" || u == "1"
}

func combineRatios(op string, a, b Ratio) (Ratio, bool) {
	left, ok1 := multiplyQuantityValues(a.Numerator, b.Denominator)
	right, ok2 := multiplyQuantityValues(b.Numerator, a.Denominator)
	denVal, ok3 := multiplyQuantityValues(a.Denominator, b.Denominator)
	if !ok1 || !ok2 || !ok3 || denVal == 0 {
		return Ratio{}, false
	}
	numVal := left + right
	if op == "-" {
		numVal = left - right
	}
	return simplifyRatio(Ratio{
		Numerator:   Quantity{Value: numVal, Unit: a.Numerator.Unit},
		Denominator: Quantity{Value: denVal, Unit: a.Denominator.Unit},
	}), true
}

func simplifyRatio(r Ratio) Ratio {
	n, d := r.Numerator.Value, r.Denominator.Value
	if n == 0 || d == 0 {
		return r
	}
	if ratioNearInteger(n) && ratioNearInteger(d) {
		g := ratioValueGCD(n, d)
		if g > 0 && g != 1 {
			r.Numerator.Value = n / g
			r.Denominator.Value = d / g
		}
	}
	return r
}

func ratioNearInteger(v float64) bool {
	return math.Abs(v-math.Round(v)) <= 1e-9*math.Max(1, math.Abs(v))
}

func scaleRatioByScalar(op string, r Ratio, rv any) ([]any, bool) {
	f, ok := asFloat(rv)
	if !ok {
		return nil, false
	}
	switch op {
	case "*":
		r.Numerator.Value *= f
		return []any{simplifyRatio(r)}, true
	case "/":
		if f == 0 {
			return nil, false
		}
		r.Numerator.Value /= f
		return []any{simplifyRatio(r)}, true
	default:
		return nil, false
	}
}

func ratioValueGCD(a, b float64) float64 {
	for scale := 1e3; scale <= 1e12; scale *= 10 {
		ia := int64(math.Round(a * scale))
		ib := int64(math.Round(b * scale))
		if ia == 0 || ib == 0 {
			continue
		}
		g := int64GCD(ia, ib)
		if g > 1 {
			return float64(g) / scale
		}
	}
	return 1
}

func int64GCD(a, b int64) int64 {
	if a < 0 {
		a = -a
	}
	if b < 0 {
		b = -b
	}
	for b != 0 {
		a, b = b, a%b
	}
	return a
}

func scaleQuantityByScalar(op string, q Quantity, rv any) ([]any, bool) {
	f, ok := asFloat(rv)
	if !ok {
		return nil, false
	}
	switch op {
	case "*":
		q.Value *= f
		return []any{q}, true
	case "/":
		if f == 0 {
			return nil, false
		}
		q.Value /= f
		return []any{q}, true
	default:
		return nil, false
	}
}

func scaleQuantityByScalarLeft(op string, lv any, q Quantity) ([]any, bool) {
	if op != "*" {
		return nil, false
	}
	f, ok := asFloat(lv)
	if !ok {
		return nil, false
	}
	q.Value *= f
	return []any{q}, true
}

func multiplyQuantityValues(q1, q2 Quantity) (float64, bool) {
	if isDimensionlessUnit(q1.Unit) {
		return q1.Value * q2.Value, true
	}
	if isDimensionlessUnit(q2.Unit) {
		return q1.Value * q2.Value, true
	}
	return q1.Value * q2.Value, true
}

func evalRatioArith(op string, a, b Ratio) ([]any, error) {
	switch op {
	case "+", "-":
		out, ok := combineRatios(op, a, b)
		if !ok {
			return nil, nil
		}
		return []any{simplifyRatio(out)}, nil
	case "*":
		num := Quantity{Value: a.Numerator.Value * b.Numerator.Value, Unit: a.Numerator.Unit}
		den := Quantity{Value: a.Denominator.Value * b.Denominator.Value, Unit: a.Denominator.Unit}
		if isDimensionlessUnit(num.Unit) {
			num.Unit = b.Numerator.Unit
		}
		if isDimensionlessUnit(den.Unit) {
			den.Unit = b.Denominator.Unit
		}
		return []any{simplifyRatio(Ratio{Numerator: num, Denominator: den})}, nil
	case "/":
		if b.Numerator.Value == 0 {
			return nil, nil
		}
		num := Quantity{Value: a.Numerator.Value * b.Denominator.Value, Unit: a.Numerator.Unit}
		den := Quantity{Value: a.Denominator.Value * b.Numerator.Value, Unit: a.Denominator.Unit}
		if isDimensionlessUnit(num.Unit) {
			num.Unit = a.Numerator.Unit
		}
		if isDimensionlessUnit(den.Unit) {
			den.Unit = a.Denominator.Unit
		}
		return []any{simplifyRatio(Ratio{Numerator: num, Denominator: den})}, nil
	default:
		return nil, nil
	}
}

func listRepeat(args [][]any) ([]any, error) {
	if len(args) < 2 || args[0] == nil || len(args[1]) == 0 {
		return nil, nil
	}
	n, ok := asInt(args[1][0])
	if !ok {
		return nil, nil
	}
	if n <= 0 {
		return []any{}, nil
	}
	var out []any
	for i := 0; i < int(n); i++ {
		out = append(out, args[0]...)
	}
	return out, nil
}

func listTimes(args [][]any) ([]any, error) {
	if len(args) < 2 || args[0] == nil || len(args[1]) == 0 {
		return nil, nil
	}
	right := args[1]
	if len(right) == 1 {
		if n, ok := asInt(right[0]); ok {
			var out []any
			for _, item := range args[0] {
				if f, ok := asFloat(item); ok {
					out = append(out, f*float64(n))
					continue
				}
				if q, ok := asQuantity(item); ok {
					q.Value *= float64(n)
					out = append(out, q)
					continue
				}
				return nil, nil
			}
			return out, nil
		}
	}
	var out []any
	for _, lv := range args[0] {
		for _, rv := range right {
			if lf, lok := asFloat(lv); lok {
				if rf, rok := asFloat(rv); rok {
					out = append(out, lf*rf)
					continue
				}
			}
			if lq, lok := asQuantity(lv); lok {
				if rq, rok := asQuantity(rv); rok {
					if conv, ok := convertQuantityValue(rq, lq.Unit); ok {
						lq.Value *= conv.Value
						out = append(out, lq)
						continue
					}
				}
			}
			return nil, nil
		}
	}
	return out, nil
}

func stringInFunction(args [][]any) ([]any, error) {
	if len(args) < 2 || len(args[0]) == 0 || len(args[1]) == 0 {
		return nil, nil
	}
	needle, nok := unwrapPrimitive(args[0][0]).(string)
	hay, hok := unwrapPrimitive(args[1][0]).(string)
	if !nok || !hok {
		return nil, nil
	}
	return []any{strings.Contains(hay, needle)}, nil
}

func truncateQuantity(args [][]any) ([]any, error) {
	item, ok := singletonArg(args)
	if !ok {
		return nil, nil
	}
	if q, ok := asQuantity(item); ok {
		q.Value = math.Trunc(q.Value)
		return []any{q}, nil
	}
	if f, ok := asFloat(item); ok {
		return []any{math.Trunc(f)}, nil
	}
	return nil, nil
}

func convertsToRatio(args [][]any) ([]any, error) {
	item, ok := singletonArg(args)
	if !ok {
		return nil, nil
	}
	if _, ok := asRatio(item); ok {
		return []any{true}, nil
	}
	return []any{false}, nil
}

func pointFromInterval(iv Interval) ([]any, error) {
	start := iv.Low
	if start != nil && !iv.LowClosed {
		next, ok := successorValue(start, false)
		if !ok {
			return nil, nil
		}
		start = next
	}
	end := iv.High
	if end != nil && !iv.HighClosed {
		prev, ok := successorValue(end, true)
		if !ok {
			return nil, nil
		}
		end = prev
	}
	if start == nil || end == nil {
		return nil, nil
	}
	if !cqlEqual(start, end) {
		return nil, nil
	}
	return []any{start}, nil
}

func valuePrecision(v any) (int, bool) {
	v = unwrapPrimitive(v)
	if t, ok := asTime(v); ok {
		if !isDateOnlyTime(t) {
			if t.Nanosecond() != 0 {
				return 17, true
			}
			if t.Second() != 0 {
				return 14, true
			}
			if t.Minute() != 0 {
				return 12, true
			}
			return 10, true
		}
		if t.Day() != 1 {
			return 8, true
		}
		if t.Month() != 1 {
			return 6, true
		}
		return 4, true
	}
	if isIntLike(v) {
		return 0, true
	}
	if f, ok := asFloat(v); ok {
		return decimalPrecision(f), true
	}
	if s, ok := v.(string); ok {
		if i := strings.IndexByte(s, '.'); i >= 0 {
			return len(s) - i - 1, true
		}
		return 0, true
	}
	return 0, false
}

func decimalPrecision(f float64) int {
	s := strconv.FormatFloat(f, 'f', -1, 64)
	i := strings.IndexByte(s, '.')
	if i < 0 {
		return 0
	}
	return len(s) - i - 1
}

func boundaryValue(args [][]any, high bool) ([]any, error) {
	item, ok := singletonArg(args)
	if !ok {
		return nil, nil
	}
	prec := -1
	if len(args) > 1 && len(args[1]) > 0 {
		if n, ok := asInt(args[1][0]); ok {
			prec = int(n)
		}
	}
	if iv, ok := asInterval(item); ok {
		v := iv.Low
		if high {
			v = iv.High
		}
		if v == nil {
			return nil, nil
		}
		if prec < 0 {
			return []any{v}, nil
		}
		item = v
	}
	if t, ok := asTime(item); ok && prec >= 0 {
		return []any{temporalBoundary(t, prec, high)}, nil
	}
	if f, ok := asFloat(item); ok {
		if prec < 0 {
			return []any{item}, nil
		}
		return []any{decimalBoundary(f, prec, high, isIntLike(item))}, nil
	}
	return []any{item}, nil
}

func decimalBoundary(f float64, prec int, high, intLike bool) any {
	if prec < 0 || decimalPrecision(f) <= prec {
		if intLike && f == float64(int64(f)) {
			return int64(f)
		}
		return f
	}
	scale := math.Pow(10, float64(prec))
	var out float64
	if high {
		out = math.Ceil(f*scale-1e-10) / scale
	} else {
		out = math.Floor(f*scale+1e-10) / scale
	}
	return out
}

func temporalBoundary(t time.Time, prec int, high bool) time.Time {
	y, m, d := t.Year(), t.Month(), t.Day()
	hh, mm, ss, ns := t.Hour(), t.Minute(), t.Second(), t.Nanosecond()
	loc := t.Location()
	if loc == nil {
		loc = time.UTC
	}
	max := high
	switch {
	case prec <= 4:
		if max {
			m, d, hh, mm, ss, ns = 12, 31, 23, 59, 59, 999000000
		} else {
			m, d, hh, mm, ss, ns = 1, 1, 0, 0, 0, 0
		}
	case prec <= 6:
		if max {
			d = daysInMonth(y, m)
			hh, mm, ss, ns = 23, 59, 59, 999000000
		} else {
			d, hh, mm, ss, ns = 1, 0, 0, 0, 0
		}
	case prec <= 8:
		if max {
			hh, mm, ss, ns = 23, 59, 59, 999000000
		} else {
			hh, mm, ss, ns = 0, 0, 0, 0
		}
	case prec <= 10:
		if max {
			mm, ss, ns = 59, 59, 999000000
		} else {
			mm, ss, ns = 0, 0, 0
		}
	case prec <= 12:
		if max {
			ss, ns = 59, 999000000
		} else {
			ss, ns = 0, 0
		}
	case prec <= 14:
		if max {
			ns = 999000000
		} else {
			ns = 0
		}
	}
	out := time.Date(y, m, d, hh, mm, ss, ns, loc)
	if isDateOnlyTime(t) && prec <= 8 {
		return time.Date(out.Year(), out.Month(), out.Day(), 0, 0, 0, 0, dateOnlyLoc)
	}
	return out
}

func daysInMonth(year int, month time.Month) int {
	return time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

func quantityFromValue(v any) (Quantity, bool) {
	if q, ok := asQuantity(v); ok {
		return q, true
	}
	if f, ok := asFloat(v); ok {
		return Quantity{Value: f, Unit: "1"}, true
	}
	if s, ok := unwrapPrimitive(v).(string); ok {
		return parseQuantityString(s)
	}
	return Quantity{}, false
}

func parseQuantityString(s string) (Quantity, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Quantity{}, false
	}
	i := 0
	if s[0] == '+' || s[0] == '-' {
		i++
	}
	dot := false
	for i < len(s) {
		c := s[i]
		if c >= '0' && c <= '9' {
			i++
			continue
		}
		if c == '.' && !dot {
			dot = true
			i++
			continue
		}
		break
	}
	if i == 0 || (i == 1 && (s[0] == '+' || s[0] == '-')) {
		return Quantity{}, false
	}
	f, err := strconv.ParseFloat(s[:i], 64)
	if err != nil {
		return Quantity{}, false
	}
	rest := strings.TrimSpace(s[i:])
	unit := "1"
	if rest != "" {
		if strings.HasPrefix(rest, "'") {
			end := strings.Index(rest[1:], "'")
			if end < 0 {
				return Quantity{}, false
			}
			unit = rest[1 : 1+end]
			if strings.TrimSpace(rest[2+end:]) != "" {
				return Quantity{}, false
			}
		} else {
			unit = rest
		}
	}
	if unit == "" {
		unit = "1"
	}
	return Quantity{Value: f, Unit: unit}, true
}

func booleanFromValue(v any) (bool, bool) {
	if b := asBool([]any{v}); b != nil {
		return *b, true
	}
	if s, ok := unwrapPrimitive(v).(string); ok {
		switch strings.ToLower(strings.TrimSpace(s)) {
		case "true", "t", "yes", "y", "1":
			return true, true
		case "false", "f", "no", "n", "0":
			return false, true
		}
		return false, false
	}
	if isIntLike(v) {
		n, _ := asInt(v)
		if n == 1 {
			return true, true
		}
		if n == 0 {
			return false, true
		}
		return false, false
	}
	if f, ok := asFloat(v); ok {
		if f == 1 {
			return true, true
		}
		if f == 0 {
			return false, true
		}
	}
	return false, false
}

func convertsToResult(name string, args [][]any) ([]any, error) {
	if len(args) == 0 || args[0] == nil || len(args[0]) == 0 {
		return nil, nil
	}
	if len(args[0]) != 1 {
		return []any{false}, nil
	}
	item := args[0][0]
	if unwrapPrimitive(item) == nil {
		return nil, nil
	}
	ok := false
	switch strings.TrimPrefix(name, "convertsto") {
	case "integer", "long":
		_, ok = intFromValue(item)
	case "decimal":
		_, ok = floatFromValue(item)
	case "boolean":
		_, ok = booleanFromValue(item)
	case "string":
		_, ok = cqlToString(item)
	case "quantity":
		_, ok = quantityFromValue(item)
	case "date", "datetime":
		_, ok = timeFromValue(item, false)
	case "time":
		_, ok = timeFromValue(item, true)
	}
	return []any{ok}, nil
}

func intFromValue(v any) (int64, bool) {
	if n, ok := asInt(v); ok {
		return n, true
	}
	if s, ok := unwrapPrimitive(v).(string); ok {
		n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
		return n, err == nil
	}
	return 0, false
}

func floatFromValue(v any) (float64, bool) {
	if f, ok := asFloat(v); ok {
		return f, true
	}
	if s, ok := unwrapPrimitive(v).(string); ok {
		f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
		return f, err == nil
	}
	return 0, false
}

func timeFromValue(v any, asTimeOfDay bool) (time.Time, bool) {
	if t, ok := asTime(v); ok {
		return t, true
	}
	s, ok := unwrapPrimitive(v).(string)
	if !ok {
		return time.Time{}, false
	}
	if asTimeOfDay {
		if t, pok := parseELMTimeString(s); pok {
			return t, true
		}
	}
	t, err := parseCQLDate(s)
	return t, err == nil
}

func toConceptValue(v any) []any {
	if v == nil {
		return nil
	}
	if list, ok := v.([]any); ok {
		var out []any
		for _, el := range list {
			out = append(out, toConceptValue(el)...)
		}
		return out
	}
	if c, ok := v.(Code); ok {
		return []any{c}
	}
	if c, ok := asCodeLike(v); ok {
		return []any{Code{Code: c.Code, System: c.System, Display: c.Display}}
	}
	return nil
}

func typeMinMaxValue(args [][]any, min bool) ([]any, error) {
	item, ok := singletonArg(args)
	if !ok {
		return nil, nil
	}
	s, ok := unwrapPrimitive(item).(string)
	if !ok {
		s = elmTypeBare(elmString(item))
	}
	name := strings.ToLower(elmTypeBare(s))
	switch name {
	case "integer":
		if min {
			return []any{int64(math.MinInt32)}, nil
		}
		return []any{int64(math.MaxInt32)}, nil
	case "long":
		if min {
			return []any{int64(math.MinInt64)}, nil
		}
		return []any{int64(math.MaxInt64)}, nil
	case "decimal":
		if min {
			return []any{-1e28}, nil
		}
		return []any{1e28}, nil
	case "date":
		if min {
			return []any{time.Date(1, 1, 1, 0, 0, 0, 0, dateOnlyLoc)}, nil
		}
		return []any{time.Date(9999, 12, 31, 0, 0, 0, 0, dateOnlyLoc)}, nil
	case "datetime":
		if min {
			return []any{time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC)}, nil
		}
		return []any{time.Date(9999, 12, 31, 23, 59, 59, 999000000, time.UTC)}, nil
	case "time":
		if min {
			return []any{time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC)}, nil
		}
		return []any{time.Date(1, 1, 1, 23, 59, 59, 999000000, time.UTC)}, nil
	case "quantity":
		if min {
			return []any{Quantity{Value: -1e28, Unit: "1"}}, nil
		}
		return []any{Quantity{Value: 1e28, Unit: "1"}}, nil
	}
	return nil, nil
}

func structureChildrenArgs(args [][]any) ([]any, error) {
	item, ok := singletonArg(args)
	if !ok {
		if len(args) > 0 && args[0] != nil {
			return structureChildren(args[0]), nil
		}
		return nil, nil
	}
	return structureChildren(item), nil
}

func structureDescendantsArgs(args [][]any) ([]any, error) {
	item, ok := singletonArg(args)
	if !ok {
		if len(args) > 0 && args[0] != nil {
			return structureDescendants(args[0]), nil
		}
		return nil, nil
	}
	return structureDescendants(item), nil
}

func structureChildren(v any) []any {
	if v == nil {
		return nil
	}
	if iv, ok := asInterval(v); ok {
		var out []any
		if iv.Low != nil {
			out = append(out, iv.Low)
		}
		if iv.High != nil {
			out = append(out, iv.High)
		}
		return out
	}
	if list, ok := v.([]any); ok {
		var out []any
		for _, el := range list {
			out = append(out, structureChildren(el)...)
		}
		return out
	}
	obj, ok := asObject(v)
	if !ok {
		return nil
	}
	keys := make([]string, 0, len(obj))
	for k := range obj {
		if k == "resourceType" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var out []any
	for _, k := range keys {
		out = append(out, flattenJSON(obj[k])...)
	}
	return out
}

func structureDescendants(v any) []any {
	kids := structureChildren(v)
	var out []any
	for _, k := range kids {
		out = append(out, k)
		out = append(out, structureDescendants(k)...)
	}
	return out
}

func listFloats(args [][]any) ([]float64, bool, bool) {
	if len(args) == 0 || args[0] == nil {
		return nil, false, false
	}
	nums := make([]float64, 0, len(args[0]))
	intLike := true
	for _, item := range args[0] {
		if unwrapPrimitive(item) == nil {
			continue
		}
		f, ok := asFloat(item)
		if !ok {
			return nil, false, false
		}
		if !isIntLike(item) {
			intLike = false
		}
		nums = append(nums, f)
	}
	return nums, intLike, true
}

func listMedian(args [][]any) ([]any, error) {
	nums, intLike, ok := listFloats(args)
	if !ok || len(nums) == 0 {
		return nil, nil
	}
	sort.Float64s(nums)
	n := len(nums)
	var mid float64
	if n%2 == 1 {
		mid = nums[n/2]
	} else {
		mid = (nums[n/2-1] + nums[n/2]) / 2
	}
	if intLike && mid == float64(int64(mid)) {
		return []any{int64(mid)}, nil
	}
	return []any{mid}, nil
}

func listMode(args [][]any) ([]any, error) {
	if len(args) == 0 || args[0] == nil {
		return nil, nil
	}
	type pair struct {
		v any
		n int
	}
	var items []pair
	for _, item := range args[0] {
		if unwrapPrimitive(item) == nil {
			continue
		}
		found := false
		for i := range items {
			if cqlEqual(items[i].v, item) {
				items[i].n++
				found = true
				break
			}
		}
		if !found {
			items = append(items, pair{v: item, n: 1})
		}
	}
	if len(items) == 0 {
		return nil, nil
	}
	best := items[0]
	for _, it := range items[1:] {
		if it.n > best.n {
			best = it
		}
	}
	return []any{best.v}, nil
}

func listProduct(args [][]any) ([]any, error) {
	nums, intLike, ok := listFloats(args)
	if !ok || len(nums) == 0 {
		return nil, nil
	}
	prod := 1.0
	for _, n := range nums {
		prod *= n
	}
	if intLike && prod == float64(int64(prod)) {
		return []any{int64(prod)}, nil
	}
	return []any{prod}, nil
}

func listGeometricMean(args [][]any) ([]any, error) {
	nums, _, ok := listFloats(args)
	if !ok || len(nums) == 0 {
		return nil, nil
	}
	sum := 0.0
	for _, n := range nums {
		if n <= 0 {
			return nil, nil
		}
		sum += math.Log(n)
	}
	out := math.Exp(sum / float64(len(nums)))
	if math.IsNaN(out) || math.IsInf(out, 0) {
		return nil, nil
	}
	return []any{out}, nil
}

func listVariance(args [][]any) ([]any, error) {
	nums, _, ok := listFloats(args)
	if !ok || len(nums) < 2 {
		return nil, nil
	}
	mean := 0.0
	for _, n := range nums {
		mean += n
	}
	mean /= float64(len(nums))
	sum := 0.0
	for _, n := range nums {
		d := n - mean
		sum += d * d
	}
	return []any{sum / float64(len(nums)-1)}, nil
}

func listStdDev(args [][]any) ([]any, error) {
	v, err := listVariance(args)
	if err != nil || len(v) == 0 {
		return v, err
	}
	f, ok := asFloat(v[0])
	if !ok || f < 0 {
		return nil, nil
	}
	return []any{math.Sqrt(f)}, nil
}

func listPopulationVariance(args [][]any) ([]any, error) {
	nums, _, ok := listFloats(args)
	if !ok || len(nums) < 2 {
		return nil, nil
	}
	mean := 0.0
	for _, n := range nums {
		mean += n
	}
	mean /= float64(len(nums))
	sum := 0.0
	for _, n := range nums {
		d := n - mean
		sum += d * d
	}
	return []any{sum / float64(len(nums))}, nil
}

func listPopulationStdDev(args [][]any) ([]any, error) {
	v, err := listPopulationVariance(args)
	if err != nil || len(v) == 0 {
		return v, err
	}
	f, ok := asFloat(v[0])
	if !ok || f < 0 {
		return nil, nil
	}
	return []any{math.Sqrt(f)}, nil
}
