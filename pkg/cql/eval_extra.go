package cql

import (
	"math"
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
	from, fok := quantitySIFactor(q.Unit)
	to, tok := quantitySIFactor(unit)
	if !fok || !tok || to == 0 {
		return Quantity{}, false
	}
	return Quantity{Value: q.Value * from / to, Unit: unit}, true
}

func quantitySIFactor(unit string) (float64, bool) {
	switch strings.ToLower(strings.TrimSpace(unit)) {
	case "g", "gm", "gram", "grams":
		return 1, true
	case "mg":
		return 0.001, true
	case "kg":
		return 1000, true
	case "mcg", "ug":
		return 1e-6, true
	case "m", "meter", "meters":
		return 1, true
	case "cm":
		return 0.01, true
	case "mm":
		return 0.001, true
	case "km":
		return 1000, true
	case "l", "liter", "liters":
		return 1, true
	case "ml":
		return 0.001, true
	case "dl":
		return 0.1, true
	}
	return 0, false
}
