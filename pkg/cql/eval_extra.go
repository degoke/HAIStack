package cql

import (
	"fmt"
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
	if !ok || i < 0 || int(i) >= len(base) {
		return nil, nil
	}
	return []any{base[i]}, nil
}

func (st *evalState) evalIndexer(args [][]any) ([]any, error) {
	if len(args) < 2 || len(args[0]) == 0 || len(args[1]) == 0 {
		return nil, nil
	}
	i, ok := asInt(args[1][0])
	if !ok {
		return nil, nil
	}
	if s, ok := unwrapPrimitive(args[0][0]).(string); ok {
		if i < 0 || int(i) >= len(s) {
			return nil, nil
		}
		return []any{string(s[i])}, nil
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
		return []any{fmt.Sprint(unwrapPrimitive(item))}, nil
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
	if len(left) == 0 || len(right) == 0 {
		return nil, nil
	}
	lt, lok := asTime(left[0])
	rt, rok := asTime(right[0])
	if !lok || !rok {
		return []any{cqlEqual(left[0], right[0])}, nil
	}
	switch n.op {
	case "same year as":
		return []any{lt.Year() == rt.Year()}, nil
	case "same month as":
		return []any{lt.Year() == rt.Year() && lt.Month() == rt.Month()}, nil
	case "same day as":
		return []any{lt.Year() == rt.Year() && lt.Month() == rt.Month() && lt.Day() == rt.Day()}, nil
	case "same hour as":
		return []any{lt.Year() == rt.Year() && lt.Month() == rt.Month() && lt.Day() == rt.Day() && lt.Hour() == rt.Hour()}, nil
	case "same minute as":
		return []any{lt.Truncate(time.Minute).Equal(rt.Truncate(time.Minute))}, nil
	}
	return []any{lt.Equal(rt)}, nil
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
		if iv, ok := asInterval(item); ok {
			ivs = append(ivs, iv)
		}
	}
	if len(ivs) == 0 {
		return v
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

func constructDate(args [][]any, dateTime bool) ([]any, error) {
	parts := make([]int, 0, 7)
	for _, a := range args {
		if len(a) == 0 {
			continue
		}
		n, ok := asInt(a[0])
		if !ok {
			return nil, nil
		}
		parts = append(parts, int(n))
	}
	if len(parts) == 0 {
		return nil, nil
	}
	y, m, d, h, min, s := parts[0], 1, 1, 0, 0, 0
	if len(parts) > 1 {
		m = parts[1]
	}
	if len(parts) > 2 {
		d = parts[2]
	}
	if dateTime && len(parts) > 3 {
		h = parts[3]
	}
	if dateTime && len(parts) > 4 {
		min = parts[4]
	}
	if dateTime && len(parts) > 5 {
		s = parts[5]
	}
	loc := time.UTC
	if !dateTime {
		loc = dateOnlyLoc
	}
	return []any{time.Date(y, time.Month(m), d, h, min, s, 0, loc)}, nil
}

func (st *evalState) constructTime(args [][]any) ([]any, error) {
	h, m, s := 0, 0, 0
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
	now := time.Now()
	if st != nil && !st.now.IsZero() {
		now = clockInZone(st.now)
	}
	loc := now.Location()
	if loc == nil {
		loc = time.UTC
	}
	return []any{time.Date(now.Year(), now.Month(), now.Day(), h, m, s, 0, loc)}, nil
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
	if len(args) < 3 || len(args[0]) == 0 {
		return nil, nil
	}
	s := fmt.Sprint(unwrapPrimitive(args[0][0]))
	old, newv := "", ""
	if len(args[1]) > 0 {
		old = fmt.Sprint(unwrapPrimitive(args[1][0]))
	}
	if len(args[2]) > 0 {
		newv = fmt.Sprint(unwrapPrimitive(args[2][0]))
	}
	return []any{strings.ReplaceAll(s, old, newv)}, nil
}

func stringSplit(args [][]any) ([]any, error) {
	if len(args) == 0 || len(args[0]) == 0 {
		return nil, nil
	}
	s := fmt.Sprint(unwrapPrimitive(args[0][0]))
	sep := ""
	if len(args) > 1 && len(args[1]) > 0 {
		sep = fmt.Sprint(unwrapPrimitive(args[1][0]))
	}
	parts := strings.Split(s, sep)
	out := make([]any, 0, len(parts))
	for _, p := range parts {
		out = append(out, p)
	}
	return out, nil
}

func stringCombine(args [][]any) ([]any, error) {
	if len(args) == 0 {
		return []any{""}, nil
	}
	sep := ""
	if len(args) > 1 && len(args[1]) > 0 {
		sep = fmt.Sprint(unwrapPrimitive(args[1][0]))
	}
	var parts []string
	for _, el := range args[0] {
		parts = append(parts, fmt.Sprint(unwrapPrimitive(el)))
	}
	return []any{strings.Join(parts, sep)}, nil
}

func stringCase(args [][]any, upper bool) ([]any, error) {
	if len(args) == 0 || len(args[0]) == 0 {
		return nil, nil
	}
	s := fmt.Sprint(unwrapPrimitive(args[0][0]))
	if upper {
		return []any{strings.ToUpper(s)}, nil
	}
	return []any{strings.ToLower(s)}, nil
}

func stringSubstring(args [][]any) ([]any, error) {
	if len(args) == 0 || len(args[0]) == 0 {
		return nil, nil
	}
	s := fmt.Sprint(unwrapPrimitive(args[0][0]))
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
