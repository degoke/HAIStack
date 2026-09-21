package cql

import "strings"

// compareCtx threads Engine UCUM settings through compare/equivalence helpers.
type compareCtx struct {
	conv UCUMConverter
}

func (st *evalState) compareContext() compareCtx {
	if st == nil {
		return compareCtx{}
	}
	return compareCtx{conv: st.ucumConv()}
}

func (st *evalState) ucumConv() UCUMConverter {
	if st != nil && st.engine != nil {
		return st.engine.ucum()
	}
	return defaultUCUM
}

func (c compareCtx) ucum() UCUMConverter {
	if c.conv != nil {
		return c.conv
	}
	return defaultUCUM
}

func (c compareCtx) Compare(a, b any) (int, bool) {
	return cqlCompareUCUM(a, b, c.ucum())
}

func (c compareCtx) Equivalent(a, b any) bool {
	return cqlEquivalentUCUM(a, b, c.ucum())
}

func cqlCompareUCUM(a, b any, conv UCUMConverter) (int, bool) {
	if conv == nil {
		conv = defaultUCUM
	}
	a, b = unwrapPrimitive(a), unwrapPrimitive(b)
	if ra, ok := asRatio(a); ok {
		if rb, ok := asRatio(b); ok {
			return ratioCompare(ra, rb, conv)
		}
	}
	if qa, ok := asQuantity(a); ok {
		if qb, ok := asQuantity(b); ok {
			va, vb, ok := quantityValuesComparable(qa, qb, conv)
			if ok {
				if va < vb {
					return -1, true
				}
				if va > vb {
					return 1, true
				}
				return 0, true
			}
			if sameUnit(qa.Unit, qb.Unit) || (isTimeUnit(qa.Unit) && isTimeUnit(qb.Unit)) {
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
	}
	if ta, aok := asTemporal(a, temporalLocation(b)); aok {
		if tb, bok := asTemporal(b, temporalLocation(a)); bok {
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
	sa, aok := a.(string)
	sb, bok := b.(string)
	if aok && bok {
		if sa < sb {
			return -1, true
		}
		if sa > sb {
			return 1, true
		}
		return 0, true
	}
	return 0, false
}

func cqlEquivalentUCUM(a, b any, conv UCUMConverter) bool {
	if conv == nil {
		conv = defaultUCUM
	}
	a, b = unwrapPrimitive(a), unwrapPrimitive(b)
	if cqlEqual(a, b) {
		return true
	}
	if ra, ok := asRatio(a); ok {
		if rb, ok := asRatio(b); ok {
			if ratioCrossEqual(ra, rb, conv) {
				return true
			}
			return cqlEquivalentUCUM(ra.Numerator, rb.Numerator, conv) &&
				cqlEquivalentUCUM(ra.Denominator, rb.Denominator, conv)
		}
	}
	if qa, ok := asQuantity(a); ok {
		if qb, ok := asQuantity(b); ok {
			va, vb, ok := quantityValuesComparable(qa, qb, conv)
			if ok && va == vb {
				return true
			}
		}
	}
	if ca, ok := asCodeLike(a); ok {
		if cb, ok := asCodeLike(b); ok {
			return codesEquivalent(ca, cb)
		}
	}
	sa, aok := a.(string)
	sb, bok := b.(string)
	if aok && bok {
		return strings.EqualFold(sa, sb)
	}
	return false
}
