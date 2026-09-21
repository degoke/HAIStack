package cql

import (
	"strings"
	"time"
)

func (st *evalState) evalQuantity(n *quantityNode) ([]any, error) {
	f, ok := asFloat(n.value)
	if !ok {
		return nil, errf("CQL quantity requires a number")
	}
	return []any{Quantity{Value: f, Unit: n.unit}}, nil
}

func (st *evalState) evalInterval(n *intervalNode) ([]any, error) {
	low, err := st.eval(n.low)
	if err != nil {
		return nil, err
	}
	high, err := st.eval(n.high)
	if err != nil {
		return nil, err
	}
	return []any{Interval{
		Low:        singletonOrList(low),
		High:       singletonOrList(high),
		LowClosed:  n.lowClosed,
		HighClosed: n.highClosed,
	}}, nil
}

func (st *evalState) evalBetween(n *betweenNode) ([]any, error) {
	x, err := st.eval(n.x)
	if err != nil {
		return nil, err
	}
	low, err := st.eval(n.low)
	if err != nil {
		return nil, err
	}
	high, err := st.eval(n.high)
	if err != nil {
		return nil, err
	}
	if len(x) == 0 {
		return nil, nil
	}
	iv := Interval{Low: singletonOrList(low), High: singletonOrList(high), LowClosed: true, HighClosed: true}
	return []any{intervalContains(iv, x[0], false)}, nil
}

func (st *evalState) evalDuration(n *durationNode) ([]any, error) {
	var start, end any
	if n.of != nil {
		v, err := st.eval(n.of)
		if err != nil {
			return nil, err
		}
		if len(v) == 0 {
			return nil, nil
		}
		if iv, ok := asInterval(v[0]); ok {
			start, end = iv.Low, iv.High
		} else {
			return nil, nil
		}
	} else {
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
		start, end = left[0], right[0]
	}
	years, ok := durationInUnit(start, end, n.unit)
	if !ok {
		return nil, nil
	}
	return []any{years}, nil
}

func (st *evalState) evalIntervalRel(n *binaryNode) ([]any, error) {
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
	li, lok := asInterval(left[0])
	ri, rok := asInterval(right[0])
	switch n.op {
	case "includes", "properly includes":
		if lok && rok {
			return []any{intervalIncludes(li, ri, strings.HasPrefix(n.op, "properly"))}, nil
		}
		if lok {
			return []any{intervalContains(li, right[0], strings.HasPrefix(n.op, "properly"))}, nil
		}
	case "included in", "during", "properly included in", "properly during":
		proper := strings.HasPrefix(n.op, "properly")
		if lok && rok {
			return []any{intervalIncludes(ri, li, proper)}, nil
		}
		if rok {
			return []any{intervalContains(ri, left[0], proper)}, nil
		}
	case "overlaps":
		if lok && rok {
			return []any{intervalOverlaps(li, ri)}, nil
		}
	case "starts":
		if lok && rok {
			return []any{intervalStarts(li, ri)}, nil
		}
	case "ends":
		if lok && rok {
			return []any{intervalEnds(li, ri)}, nil
		}
	case "meets":
		if lok && rok {
			return []any{intervalMeets(li, ri)}, nil
		}
	case "before":
		if lok && rok {
			cmp, ok := cqlCompare(li.High, ri.Low)
			return []any{ok && (cmp < 0 || (!li.HighClosed && cmp == 0) || (!ri.LowClosed && cmp == 0))}, nil
		}
		cmp, ok := cqlCompare(left[0], right[0])
		if !ok {
			return nil, nil
		}
		return []any{cmp < 0}, nil
	case "after":
		if lok && rok {
			cmp, ok := cqlCompare(li.Low, ri.High)
			return []any{ok && (cmp > 0 || (!li.LowClosed && cmp == 0) || (!ri.HighClosed && cmp == 0))}, nil
		}
		cmp, ok := cqlCompare(left[0], right[0])
		if !ok {
			return nil, nil
		}
		return []any{cmp > 0}, nil
	}
	return nil, nil
}

func asQuantity(v any) (Quantity, bool) {
	switch x := v.(type) {
	case Quantity:
		return x, true
	case *Quantity:
		if x == nil {
			return Quantity{}, false
		}
		return *x, true
	}
	obj, ok := asObject(v)
	if !ok {
		return Quantity{}, false
	}
	raw, has := obj["value"]
	if !has {
		return Quantity{}, false
	}
	f, ok := asFloat(raw)
	if !ok {
		return Quantity{}, false
	}
	unit, _ := obj["unit"].(string)
	if unit == "" {
		if c, ok := obj["code"].(string); ok {
			unit = c
		}
	}
	if unit == "" {
		return Quantity{}, false
	}
	return Quantity{Value: f, Unit: unit}, true
}

func asInterval(v any) (Interval, bool) {
	switch x := v.(type) {
	case Interval:
		return x, true
	case *Interval:
		if x == nil {
			return Interval{}, false
		}
		return *x, true
	}
	obj, ok := asObject(v)
	if !ok {
		return Interval{}, false
	}
	if low, has := obj["low"]; has {
		high := obj["high"]
		return Interval{Low: unwrapPrimitive(low), High: unwrapPrimitive(high), LowClosed: true, HighClosed: true}, true
	}
	if start, has := obj["start"]; has {
		end := obj["end"]
		return Interval{
			Low:        unwrapPrimitive(start),
			High:       unwrapPrimitive(end),
			LowClosed:  true,
			HighClosed: true,
		}, true
	}
	return Interval{}, false
}

func intervalContains(iv Interval, point any, properly bool) bool {
	if point == nil {
		return false
	}
	if iv.Low != nil {
		cmp, ok := cqlCompare(point, iv.Low)
		if !ok {
			return false
		}
		if cmp < 0 || (cmp == 0 && (!iv.LowClosed || properly)) {
			return false
		}
	}
	if iv.High != nil {
		cmp, ok := cqlCompare(point, iv.High)
		if !ok {
			return false
		}
		if cmp > 0 || (cmp == 0 && (!iv.HighClosed || properly)) {
			return false
		}
	}
	return true
}

func intervalIncludes(outer, inner Interval, properly bool) bool {
	if !intervalBoundIncludesLow(outer, inner) || !intervalBoundIncludesHigh(outer, inner) {
		return false
	}
	if properly && intervalBoundsEqual(outer, inner) {
		return false
	}
	return true
}

func intervalBoundIncludesLow(outer, inner Interval) bool {
	if inner.Low == nil {
		return outer.Low == nil
	}
	if outer.Low == nil {
		return true
	}
	cmp, ok := cqlCompare(inner.Low, outer.Low)
	if !ok {
		return false
	}
	if cmp > 0 {
		return true
	}
	if cmp == 0 {
		return outer.LowClosed || !inner.LowClosed
	}
	return false
}

func intervalBoundIncludesHigh(outer, inner Interval) bool {
	if inner.High == nil {
		return outer.High == nil
	}
	if outer.High == nil {
		return true
	}
	cmp, ok := cqlCompare(inner.High, outer.High)
	if !ok {
		return false
	}
	if cmp < 0 {
		return true
	}
	if cmp == 0 {
		return outer.HighClosed || !inner.HighClosed
	}
	return false
}

func intervalBoundsEqual(a, b Interval) bool {
	return cqlEqual(a.Low, b.Low) && cqlEqual(a.High, b.High) && a.LowClosed == b.LowClosed && a.HighClosed == b.HighClosed
}

func intervalOverlaps(a, b Interval) bool {
	if a.High != nil && b.Low != nil {
		cmp, ok := cqlCompare(a.High, b.Low)
		if ok && (cmp < 0 || (cmp == 0 && (!a.HighClosed || !b.LowClosed))) {
			return false
		}
	}
	if b.High != nil && a.Low != nil {
		cmp, ok := cqlCompare(b.High, a.Low)
		if ok && (cmp < 0 || (cmp == 0 && (!b.HighClosed || !a.LowClosed))) {
			return false
		}
	}
	return true
}

func intervalStarts(a, b Interval) bool {
	return cqlEqual(a.Low, b.Low) && intervalIncludes(b, a, false)
}

func intervalEnds(a, b Interval) bool {
	return cqlEqual(a.High, b.High) && intervalIncludes(b, a, false)
}

func intervalMeets(a, b Interval) bool {
	if a.High != nil && b.Low != nil && cqlEqual(a.High, b.Low) {
		return a.HighClosed != b.LowClosed || (a.HighClosed && b.LowClosed)
	}
	if b.High != nil && a.Low != nil && cqlEqual(b.High, a.Low) {
		return b.HighClosed != a.LowClosed || (b.HighClosed && a.LowClosed)
	}
	return false
}

func intervalIntersect(a, b Interval) (Interval, bool) {
	if !intervalOverlaps(a, b) {
		return Interval{}, false
	}
	out := Interval{LowClosed: true, HighClosed: true}
	if a.Low == nil {
		out.Low, out.LowClosed = b.Low, b.LowClosed
	} else if b.Low == nil {
		out.Low, out.LowClosed = a.Low, a.LowClosed
	} else {
		cmp, ok := cqlCompare(a.Low, b.Low)
		if !ok {
			return Interval{}, false
		}
		if cmp > 0 {
			out.Low, out.LowClosed = a.Low, a.LowClosed
		} else if cmp < 0 {
			out.Low, out.LowClosed = b.Low, b.LowClosed
		} else {
			out.Low = a.Low
			out.LowClosed = a.LowClosed && b.LowClosed
		}
	}
	if a.High == nil {
		out.High, out.HighClosed = b.High, b.HighClosed
	} else if b.High == nil {
		out.High, out.HighClosed = a.High, a.HighClosed
	} else {
		cmp, ok := cqlCompare(a.High, b.High)
		if !ok {
			return Interval{}, false
		}
		if cmp < 0 {
			out.High, out.HighClosed = a.High, a.HighClosed
		} else if cmp > 0 {
			out.High, out.HighClosed = b.High, b.HighClosed
		} else {
			out.High = a.High
			out.HighClosed = a.HighClosed && b.HighClosed
		}
	}
	return out, true
}

func intervalWidth(iv Interval) ([]any, error) {
	if iv.Low == nil || iv.High == nil {
		return nil, nil
	}
	if lf, ok := asFloat(iv.Low); ok {
		if hf, ok := asFloat(iv.High); ok {
			return []any{hf - lf}, nil
		}
	}
	if n, ok := durationInUnit(iv.Low, iv.High, "day"); ok {
		return []any{n}, nil
	}
	return nil, nil
}

func evalQuantityArith(op string, a, b Quantity) ([]any, error) {
	if !sameUnit(a.Unit, b.Unit) && !isTimeUnit(a.Unit) {
		if op == "+" || op == "-" {
			return nil, nil
		}
	}
	ua, ub := a.Value, b.Value
	if isTimeUnit(a.Unit) && isTimeUnit(b.Unit) {
		ua = toSeconds(a)
		ub = toSeconds(b)
		a.Unit = "s"
	}
	var out float64
	switch op {
	case "+":
		out = ua + ub
	case "-":
		out = ua - ub
	case "*":
		out = ua * ub
	case "/":
		if ub == 0 {
			return nil, nil
		}
		out = ua / ub
	default:
		return nil, nil
	}
	return []any{Quantity{Value: out, Unit: a.Unit}}, nil
}

func sameUnit(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

func isTimeUnit(unit string) bool {
	return timeUnitName(unit) != "" || unit == "s" || unit == "ms"
}

func toSeconds(q Quantity) float64 {
	switch timeUnitName(q.Unit) {
	case "year":
		return q.Value * 365.25 * 24 * 3600
	case "month":
		return q.Value * 30.4375 * 24 * 3600
	case "week":
		return q.Value * 7 * 24 * 3600
	case "day":
		return q.Value * 24 * 3600
	case "hour":
		return q.Value * 3600
	case "minute":
		return q.Value * 60
	case "second":
		return q.Value
	case "millisecond":
		return q.Value / 1000
	}
	if q.Unit == "s" {
		return q.Value
	}
	if q.Unit == "ms" {
		return q.Value / 1000
	}
	return q.Value
}

func addDuration(t time.Time, q Quantity, op string) (time.Time, bool) {
	n := int(q.Value)
	if op == "-" {
		n = -n
	} else if op != "+" {
		return time.Time{}, false
	}
	unit := timeUnitName(q.Unit)
	if unit == "" {
		unit = strings.ToLower(q.Unit)
	}
	switch unit {
	case "year":
		return t.AddDate(n, 0, 0), true
	case "month":
		return t.AddDate(0, n, 0), true
	case "week":
		return t.AddDate(0, 0, n*7), true
	case "day":
		return t.AddDate(0, 0, n), true
	case "hour":
		return t.Add(time.Duration(n) * time.Hour), true
	case "minute":
		return t.Add(time.Duration(n) * time.Minute), true
	case "second":
		return t.Add(time.Duration(n) * time.Second), true
	case "millisecond":
		return t.Add(time.Duration(n) * time.Millisecond), true
	}
	return time.Time{}, false
}

func durationInUnit(start, end any, unit string) (int64, bool) {
	st, ok := asTime(start)
	if !ok {
		return 0, false
	}
	et, ok := asTime(end)
	if !ok {
		return 0, false
	}
	if et.Before(st) {
		n, ok := durationInUnit(end, start, unit)
		return -n, ok
	}
	unit = timeUnitName(unit)
	if unit == "" {
		return 0, false
	}
	switch unit {
	case "year":
		years := et.Year() - st.Year()
		ann := time.Date(et.Year(), st.Month(), st.Day(), st.Hour(), st.Minute(), st.Second(), st.Nanosecond(), time.UTC)
		if et.Before(ann) {
			years--
		}
		return int64(years), true
	case "month":
		months := (et.Year()-st.Year())*12 + int(et.Month()-st.Month())
		if et.Day() < st.Day() {
			months--
		}
		return int64(months), true
	case "week":
		return int64(et.Sub(st).Hours() / 24 / 7), true
	case "day":
		return int64(et.Sub(st).Hours() / 24), true
	case "hour":
		return int64(et.Sub(st).Hours()), true
	case "minute":
		return int64(et.Sub(st).Minutes()), true
	case "second":
		return int64(et.Sub(st).Seconds()), true
	case "millisecond":
		return et.Sub(st).Milliseconds(), true
	}
	return 0, false
}
