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
	lowClosed, err := st.evalClosed(n.lowClosed, n.lowClosedExpr)
	if err != nil {
		return nil, err
	}
	highClosed, err := st.evalClosed(n.highClosed, n.highClosedExpr)
	if err != nil {
		return nil, err
	}
	return []any{Interval{
		Low:        singletonOrList(low),
		High:       singletonOrList(high),
		LowClosed:  lowClosed,
		HighClosed: highClosed,
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
	if x == nil {
		return nil, nil
	}
	iv := Interval{Low: singletonOrList(low), High: singletonOrList(high), LowClosed: true, HighClosed: true}
	cmp := st.compareContext()
	unknown := false
	for _, item := range x {
		ok, comparable := intervalContains(iv, item, false, cmp)
		if !comparable {
			unknown = true
			continue
		}
		if !ok {
			return []any{false}, nil
		}
	}
	if unknown {
		return nil, nil
	}
	return []any{true}, nil
}

func (st *evalState) evalDuration(n *durationNode) ([]any, error) {
	var start, end any
	if n.of != nil {
		v, err := st.eval(n.of)
		if err != nil {
			return nil, err
		}
		if len(v) != 1 {
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
		if len(left) != 1 || len(right) != 1 {
			return nil, nil
		}
		start, end = left[0], right[0]
	}
	var nval int64
	var ok bool
	if n.difference {
		nval, ok = differenceInUnit(start, end, n.unit)
	} else {
		nval, ok = durationInUnit(start, end, n.unit)
	}
	if !ok {
		return nil, nil
	}
	return []any{nval}, nil
}

func (st *evalState) evalIntervalRel(n *binaryNode) ([]any, error) {
	base, unit := splitTimingOp(n.op)
	if unit == "" && st.isListValued(n.left) && st.isListValued(n.right) {
		switch base {
		case "includes", "properly includes":
			left, err := st.eval(n.left)
			if err != nil {
				return nil, err
			}
			right, err := st.eval(n.right)
			if err != nil {
				return nil, err
			}
			return st.listSubsetIncludes(left, right, strings.HasPrefix(base, "properly")), nil
		case "included in", "properly included in":
			left, err := st.eval(n.left)
			if err != nil {
				return nil, err
			}
			right, err := st.eval(n.right)
			if err != nil {
				return nil, err
			}
			return st.listSubsetIncludes(right, left, strings.HasPrefix(base, "properly")), nil
		}
	}
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
	left = applyPrecisionList(left, unit)
	right = applyPrecisionList(right, unit)
	unknown := false
	for _, lv := range left {
		for _, rv := range right {
			res := intervalRelOne(base, lv, rv, st.compareContext())
			if res == nil {
				unknown = true
				continue
			}
			if res[0] != true {
				return []any{false}, nil
			}
		}
	}
	if unknown {
		return nil, nil
	}
	return []any{true}, nil
}

func intervalRelOne(base string, lv, rv any, vcmp compareCtx) []any {
	li, lok := asInterval(lv)
	ri, rok := asInterval(rv)
	switch base {
	case "includes", "properly includes":
		proper := strings.HasPrefix(base, "properly")
		if lok && rok {
			return []any{intervalIncludes(li, ri, proper, vcmp)}
		}
		if lok {
			ok, comparable := intervalContains(li, rv, proper, vcmp)
			if !comparable {
				return nil
			}
			return []any{ok}
		}
	case "included in", "during", "properly included in", "properly during":
		proper := strings.HasPrefix(base, "properly")
		if lok && rok {
			return []any{intervalIncludes(ri, li, proper, vcmp)}
		}
		if rok {
			ok, comparable := intervalContains(ri, lv, proper, vcmp)
			if !comparable {
				return nil
			}
			return []any{ok}
		}
	case "overlaps":
		if lok && rok {
			return []any{intervalOverlaps(li, ri, vcmp)}
		}
	case "overlaps before":
		if lok && rok {
			return []any{intervalOverlapsBefore(li, ri, vcmp)}
		}
	case "overlaps after":
		if lok && rok {
			return []any{intervalOverlapsAfter(li, ri, vcmp)}
		}
	case "starts":
		if lok && rok {
			return []any{intervalStarts(li, ri, vcmp)}
		}
	case "ends":
		if lok && rok {
			return []any{intervalEnds(li, ri, vcmp)}
		}
	case "meets":
		if lok && rok {
			return []any{intervalMeets(li, ri, vcmp)}
		}
	case "meets before":
		if lok && rok {
			return []any{intervalMeetsBefore(li, ri, vcmp)}
		}
	case "meets after":
		if lok && rok {
			return []any{intervalMeetsAfter(li, ri, vcmp)}
		}
	case "before":
		if lok && rok {
			ord, ok := vcmp.Compare(li.High, ri.Low)
			return []any{ok && (ord < 0 || (!li.HighClosed && ord == 0) || (!ri.LowClosed && ord == 0))}
		}
		ord, ok := vcmp.Compare(lv, rv)
		if !ok {
			return nil
		}
		return []any{ord < 0}
	case "after":
		if lok && rok {
			ord, ok := vcmp.Compare(li.Low, ri.High)
			return []any{ok && (ord > 0 || (!li.LowClosed && ord == 0) || (!ri.HighClosed && ord == 0))}
		}
		ord, ok := vcmp.Compare(lv, rv)
		if !ok {
			return nil
		}
		return []any{ord > 0}
	case "contains":
		if lok {
			ok, comparable := intervalContains(li, rv, false, vcmp)
			if !comparable {
				return nil
			}
			return []any{ok}
		}
	}
	return nil
}

func intervalRelOp(op string) bool {
	base, unit := splitTimingOp(op)
	switch base {
	case "includes", "properly includes", "included in", "properly included in",
		"during", "properly during", "overlaps", "overlaps before", "overlaps after",
		"starts", "ends", "meets", "meets before", "meets after", "before", "after":
		return true
	case "contains":
		return unit != ""
	}
	return false
}

func splitTimingOp(op string) (base, unit string) {
	op = strings.ToLower(strings.TrimSpace(op))
	if strings.HasSuffix(op, " of") {
		rest := strings.TrimSpace(strings.TrimSuffix(op, " of"))
		if i := strings.LastIndex(rest, " "); i >= 0 {
			if u := timeUnitName(rest[i+1:]); u != "" {
				return strings.TrimSpace(rest[:i]), u
			}
		}
	}
	return op, ""
}

func (st *evalState) listSubsetIncludes(superset, subset []any, proper bool) []any {
	cmp := st.compareContext()
	for _, item := range subset {
		if !listContainsMember(superset, item, cmp) {
			return []any{false}
		}
	}
	if !proper {
		return []any{true}
	}
	for _, item := range superset {
		if !listContainsMember(subset, item, cmp) {
			return []any{true}
		}
	}
	return []any{false}
}

func listContainsMember(list []any, item any, cmp compareCtx) bool {
	for _, el := range list {
		if cmp.MemberEqual(el, item) {
			return true
		}
	}
	return false
}

func applyPrecisionList(vals []any, unit string) []any {
	if unit == "" || len(vals) == 0 {
		return vals
	}
	out := make([]any, len(vals))
	for i, v := range vals {
		out[i] = applyPrecisionValue(v, unit)
	}
	return out
}

func applyPrecisionValue(v any, unit string) any {
	if iv, ok := asInterval(v); ok {
		iv.Low = truncateToPrecision(iv.Low, unit)
		iv.High = truncateToPrecision(iv.High, unit)
		return iv
	}
	return truncateToPrecision(v, unit)
}

func truncateToPrecision(v any, unit string) any {
	tm, ok := asTime(v)
	if !ok {
		return v
	}
	loc := tm.Location()
	if loc == nil {
		loc = time.UTC
	}
	switch unit {
	case "year":
		return time.Date(tm.Year(), 1, 1, 0, 0, 0, 0, loc)
	case "month":
		return time.Date(tm.Year(), tm.Month(), 1, 0, 0, 0, 0, loc)
	case "week":
		wd := int(tm.Weekday())
		if wd == 0 {
			wd = 7
		}
		day := tm.AddDate(0, 0, 1-wd)
		return time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, loc)
	case "day":
		return time.Date(tm.Year(), tm.Month(), tm.Day(), 0, 0, 0, 0, loc)
	case "hour":
		return time.Date(tm.Year(), tm.Month(), tm.Day(), tm.Hour(), 0, 0, 0, loc)
	case "minute":
		return time.Date(tm.Year(), tm.Month(), tm.Day(), tm.Hour(), tm.Minute(), 0, 0, loc)
	case "second":
		return time.Date(tm.Year(), tm.Month(), tm.Day(), tm.Hour(), tm.Minute(), tm.Second(), 0, loc)
	}
	return tm
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

func isCQLInterval(v any) (Interval, bool) {
	switch x := v.(type) {
	case Interval:
		return x, true
	case *Interval:
		if x == nil {
			return Interval{}, false
		}
		return *x, true
	}
	return Interval{}, false
}

func asInterval(v any) (Interval, bool) {
	if iv, ok := isCQLInterval(v); ok {
		return iv, true
	}
	obj, ok := asObject(v)
	if !ok {
		return Interval{}, false
	}
	if low, has := obj["low"]; has {
		high := obj["high"]
		return Interval{
			Low:        intervalBound(low),
			High:       intervalBound(high),
			LowClosed:  intervalBoundClosed(obj, "lowClosed", true),
			HighClosed: intervalBoundClosed(obj, "highClosed", true),
		}, true
	}
	if start, has := obj["start"]; has {
		end := obj["end"]
		return Interval{
			Low:        intervalBound(start),
			High:       intervalBound(end),
			LowClosed:  intervalBoundClosed(obj, "lowClosed", true),
			HighClosed: intervalBoundClosed(obj, "highClosed", true),
		}, true
	}
	return Interval{}, false
}

func intervalBoundClosed(obj map[string]any, key string, def bool) bool {
	if raw, ok := obj[key]; ok {
		if b, ok := raw.(bool); ok {
			return b
		}
	}
	return def
}

func intervalBound(v any) any {
	v = unwrapPrimitive(v)
	if s, ok := v.(string); ok {
		if tm, err := parseCQLDate(s); err == nil {
			return tm
		}
	}
	return v
}

func intervalContainsAll(iv Interval, points []any, vcmp compareCtx) ([]any, error) {
	unknown := false
	for _, item := range points {
		ok, comparable := intervalContains(iv, item, false, vcmp)
		if !comparable {
			unknown = true
			continue
		}
		if !ok {
			return []any{false}, nil
		}
	}
	if unknown {
		return nil, nil
	}
	return []any{true}, nil
}

func intervalContains(iv Interval, point any, properly bool, vcmp compareCtx) (bool, bool) {
	if point == nil {
		return false, false
	}
	if iv.Low != nil {
		ord, ok := vcmp.Compare(point, iv.Low)
		if !ok {
			return false, false
		}
		if ord < 0 || (ord == 0 && (!iv.LowClosed || properly)) {
			return false, true
		}
	}
	if iv.High != nil {
		ord, ok := vcmp.Compare(point, iv.High)
		if !ok {
			return false, false
		}
		if ord > 0 || (ord == 0 && (!iv.HighClosed || properly)) {
			return false, true
		}
	}
	return true, true
}

func intervalIncludes(outer, inner Interval, properly bool, vcmp compareCtx) bool {
	if !intervalBoundIncludesLow(outer, inner, vcmp) || !intervalBoundIncludesHigh(outer, inner, vcmp) {
		return false
	}
	if properly && intervalBoundsEqual(outer, inner, vcmp) {
		return false
	}
	return true
}

func intervalBoundIncludesLow(outer, inner Interval, vcmp compareCtx) bool {
	if inner.Low == nil {
		return outer.Low == nil
	}
	if outer.Low == nil {
		return true
	}
	ord, ok := vcmp.Compare(inner.Low, outer.Low)
	if !ok {
		return false
	}
	if ord > 0 {
		return true
	}
	if ord == 0 {
		return outer.LowClosed || !inner.LowClosed
	}
	return false
}

func intervalBoundIncludesHigh(outer, inner Interval, vcmp compareCtx) bool {
	if inner.High == nil {
		return outer.High == nil
	}
	if outer.High == nil {
		return true
	}
	ord, ok := vcmp.Compare(inner.High, outer.High)
	if !ok {
		return false
	}
	if ord < 0 {
		return true
	}
	if ord == 0 {
		return outer.HighClosed || !inner.HighClosed
	}
	return false
}

func intervalBoundsEqual(a, b Interval, vcmp compareCtx) bool {
	return boundEqual(a.Low, b.Low, vcmp) && boundEqual(a.High, b.High, vcmp) &&
		a.LowClosed == b.LowClosed && a.HighClosed == b.HighClosed
}

func intervalOverlaps(a, b Interval, vcmp compareCtx) bool {
	if a.High != nil && b.Low != nil {
		ord, ok := vcmp.Compare(a.High, b.Low)
		if ok && (ord < 0 || (ord == 0 && (!a.HighClosed || !b.LowClosed))) {
			return false
		}
	}
	if b.High != nil && a.Low != nil {
		ord, ok := vcmp.Compare(b.High, a.Low)
		if ok && (ord < 0 || (ord == 0 && (!b.HighClosed || !a.LowClosed))) {
			return false
		}
	}
	return true
}

func intervalOverlapsBefore(a, b Interval, vcmp compareCtx) bool {
	return intervalOverlaps(a, b, vcmp) && intervalStartsBefore(a, b, vcmp)
}

func intervalOverlapsAfter(a, b Interval, vcmp compareCtx) bool {
	return intervalOverlaps(a, b, vcmp) && intervalEndsAfter(a, b, vcmp)
}

func intervalStartsBefore(a, b Interval, vcmp compareCtx) bool {
	if a.Low == nil {
		return b.Low != nil
	}
	if b.Low == nil {
		return false
	}
	ord, ok := vcmp.Compare(a.Low, b.Low)
	if !ok {
		return false
	}
	if ord < 0 {
		return true
	}
	return ord == 0 && a.LowClosed && !b.LowClosed
}

func intervalEndsAfter(a, b Interval, vcmp compareCtx) bool {
	if a.High == nil {
		return b.High != nil
	}
	if b.High == nil {
		return false
	}
	ord, ok := vcmp.Compare(a.High, b.High)
	if !ok {
		return false
	}
	if ord > 0 {
		return true
	}
	return ord == 0 && a.HighClosed && !b.HighClosed
}

func intervalStarts(a, b Interval, vcmp compareCtx) bool {
	return boundEqual(a.Low, b.Low, vcmp) && intervalIncludes(b, a, false, vcmp)
}

func intervalEnds(a, b Interval, vcmp compareCtx) bool {
	return boundEqual(a.High, b.High, vcmp) && intervalIncludes(b, a, false, vcmp)
}

func intervalMeets(a, b Interval, vcmp compareCtx) bool {
	return intervalMeetsBefore(a, b, vcmp) || intervalMeetsAfter(a, b, vcmp)
}

func intervalMeetsBefore(a, b Interval, vcmp compareCtx) bool {
	if a.High != nil && b.Low != nil && boundEqual(a.High, b.Low, vcmp) {
		return a.HighClosed != b.LowClosed || (a.HighClosed && b.LowClosed)
	}
	return false
}

func intervalMeetsAfter(a, b Interval, vcmp compareCtx) bool {
	if b.High != nil && a.Low != nil && boundEqual(b.High, a.Low, vcmp) {
		return b.HighClosed != a.LowClosed || (b.HighClosed && a.LowClosed)
	}
	return false
}

func intervalExcept(a, b Interval, vcmp compareCtx) []any {
	if !intervalOverlaps(a, b, vcmp) {
		return []any{a}
	}
	if intervalIncludes(b, a, false, vcmp) {
		return nil
	}
	inter, ok := intervalIntersect(a, b, vcmp)
	if !ok {
		return []any{a}
	}
	leftGap := false
	rightGap := false
	if a.Low == nil && inter.Low != nil {
		leftGap = true
	} else if a.Low != nil && inter.Low != nil {
		ord, ok := vcmp.Compare(a.Low, inter.Low)
		if ok && (ord < 0 || (ord == 0 && a.LowClosed && !inter.LowClosed)) {
			leftGap = true
		}
	}
	if a.High == nil && inter.High != nil {
		rightGap = true
	} else if a.High != nil && inter.High != nil {
		ord, ok := vcmp.Compare(a.High, inter.High)
		if ok && (ord > 0 || (ord == 0 && a.HighClosed && !inter.HighClosed)) {
			rightGap = true
		}
	}
	var out []any
	if leftGap {
		out = append(out, Interval{Low: a.Low, High: inter.Low, LowClosed: a.LowClosed, HighClosed: !inter.LowClosed})
	}
	if rightGap {
		out = append(out, Interval{Low: inter.High, High: a.High, LowClosed: !inter.HighClosed, HighClosed: a.HighClosed})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func expandValues(v []any, per *Quantity, vcmp compareCtx) []any {
	var out []any
	expanded := false
	for _, item := range v {
		iv, ok := asInterval(item)
		if !ok {
			out = append(out, item)
			continue
		}
		expanded = true
		out = append(out, expandInterval(iv, per, vcmp)...)
	}
	if !expanded {
		return v
	}
	return out
}

func expandInterval(iv Interval, per *Quantity, vcmp compareCtx) []any {
	if iv.Low == nil || iv.High == nil {
		return nil
	}
	if lf, ok := asInt(iv.Low); ok {
		if hf, ok := asInt(iv.High); ok {
			step := int64(1)
			if per != nil && per.Value > 0 {
				step = int64(per.Value)
			}
			if step <= 0 {
				return nil
			}
			start := lf
			if !iv.LowClosed {
				start += step
			}
			end := hf
			if !iv.HighClosed {
				end--
			}
			var out []any
			for i := start; i <= end; i += step {
				out = append(out, Interval{Low: i, High: i, LowClosed: true, HighClosed: true})
				if len(out) > 10000 {
					break
				}
			}
			return out
		}
	}
	if _, ok := asTime(iv.Low); ok {
		if _, ok := asTime(iv.High); ok {
			return expandTimeInterval(iv, per, vcmp)
		}
	}
	return []any{iv}
}

func expandTimeInterval(iv Interval, per *Quantity, vcmp compareCtx) []any {
	start, ok := asTime(iv.Low)
	if !ok {
		return nil
	}
	end, ok := asTime(iv.High)
	if !ok {
		return nil
	}
	step := Quantity{Value: 1, Unit: "day"}
	if per != nil {
		step = *per
	}
	t := start
	if !iv.LowClosed {
		next, ok := addDuration(t, step, "+")
		if !ok {
			return nil
		}
		t = next
	}
	var out []any
	for {
		ord, ok := vcmp.Compare(t, end)
		if !ok {
			break
		}
		if ord > 0 || (ord == 0 && !iv.HighClosed) {
			break
		}
		out = append(out, Interval{Low: t, High: t, LowClosed: true, HighClosed: true})
		if len(out) > 10000 {
			break
		}
		next, ok := addDuration(t, step, "+")
		if !ok || !next.After(t) {
			break
		}
		t = next
	}
	return out
}

func intervalIntersect(a, b Interval, vcmp compareCtx) (Interval, bool) {
	if !intervalOverlaps(a, b, vcmp) {
		return Interval{}, false
	}
	out := Interval{LowClosed: true, HighClosed: true}
	if a.Low == nil {
		out.Low, out.LowClosed = b.Low, b.LowClosed
	} else if b.Low == nil {
		out.Low, out.LowClosed = a.Low, a.LowClosed
	} else {
		ord, ok := vcmp.Compare(a.Low, b.Low)
		if !ok {
			return Interval{}, false
		}
		if ord > 0 {
			out.Low, out.LowClosed = a.Low, a.LowClosed
		} else if ord < 0 {
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
		ord, ok := vcmp.Compare(a.High, b.High)
		if !ok {
			return Interval{}, false
		}
		if ord < 0 {
			out.High, out.HighClosed = a.High, a.HighClosed
		} else if ord > 0 {
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

func alignQuantitiesForArith(op string, a, b Quantity, conv UCUMConverter) (Quantity, Quantity, bool) {
	if sameUnit(a.Unit, b.Unit) || isTimeUnit(a.Unit) {
		return a, b, true
	}
	switch op {
	case "+", "-", "*", "/":
		if !quantitySameDimension(a.Unit, b.Unit, conv) {
			return a, b, false
		}
		if convB, ok := convertQuantityValue(b, a.Unit, conv); ok {
			return a, convB, true
		}
		if convA, ok := convertQuantityValue(a, b.Unit, conv); ok {
			return convA, b, true
		}
		return a, b, false
	default:
		return a, b, false
	}
}

func evalQuantityArith(op string, a, b Quantity, conv UCUMConverter) ([]any, error) {
	if conv == nil {
		conv = defaultUCUM
	}
	sameUnitBefore := sameUnit(a.Unit, b.Unit)
	if !sameUnitBefore && !isTimeUnit(a.Unit) {
		var ok bool
		a, b, ok = alignQuantitiesForArith(op, a, b, conv)
		if !ok {
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
	unit := quantityArithResultUnit(op, a, b, sameUnitBefore, conv)
	return []any{Quantity{Value: out, Unit: unit}}, nil
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

func differenceInUnit(start, end any, unit string) (int64, bool) {
	st, ok := asTime(start)
	if !ok {
		return 0, false
	}
	et, ok := asTime(end)
	if !ok {
		return 0, false
	}
	if et.Before(st) {
		n, ok := differenceInUnit(end, start, unit)
		return -n, ok
	}
	unit = timeUnitName(unit)
	if unit == "" {
		return 0, false
	}
	switch unit {
	case "year":
		return int64(et.Year() - st.Year()), true
	case "month":
		return int64((et.Year()-st.Year())*12 + int(et.Month()-st.Month())), true
	case "week":
		sw, _ := truncateToPrecision(st, "week").(time.Time)
		ew, _ := truncateToPrecision(et, "week").(time.Time)
		return int64(ew.Sub(sw).Hours() / 24 / 7), true
	case "day":
		sd := calendarTruncate(st, "day")
		ed := calendarTruncate(et, "day")
		return int64(ed.Sub(sd).Hours() / 24), true
	case "hour":
		sh := calendarTruncate(st, "hour")
		eh := calendarTruncate(et, "hour")
		return int64(eh.Sub(sh).Hours()), true
	case "minute":
		sm := calendarTruncate(st, "minute")
		em := calendarTruncate(et, "minute")
		return int64(em.Sub(sm).Minutes()), true
	case "second":
		ss := calendarTruncate(st, "second")
		es := calendarTruncate(et, "second")
		return int64(es.Sub(ss).Seconds()), true
	case "millisecond":
		return et.Sub(st).Milliseconds(), true
	}
	return 0, false
}

func calendarTruncate(t time.Time, unit string) time.Time {
	v := truncateToPrecision(t, unit)
	if tm, ok := v.(time.Time); ok {
		return tm
	}
	return t
}
