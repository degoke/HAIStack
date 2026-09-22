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

// Equal implements CQL = with engine UCUM settings (notably ratio cross-unit equality).
func (c compareCtx) Equal(a, b any) bool {
	return cqlEqualWithUCUM(a, b, c.ucum())
}

// MemberEqual implements list membership / distinct / intersect using CQL ~ semantics.
func (c compareCtx) MemberEqual(a, b any) bool {
	return c.Equivalent(a, b)
}

func boundEqual(a, b any, vcmp compareCtx) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	ord, ok := vcmp.Compare(a, b)
	if ok {
		return ord == 0
	}
	return vcmp.Equal(a, b)
}

func cqlEqualWithUCUM(a, b any, conv UCUMConverter) bool {
	if conv == nil {
		conv = defaultUCUM
	}
	a, b = unwrapPrimitive(a), unwrapPrimitive(b)
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	if ra, ok := resourceIdentity(a); ok {
		if rb, ok := resourceIdentity(b); ok {
			return ra == rb
		}
	}
	if ra, ok := asRatio(a); ok {
		if rb, ok := asRatio(b); ok {
			return ratioEqualWithUCUM(ra, rb, conv)
		}
	}
	if qa, ok := asQuantity(a); ok {
		if qb, ok := asQuantity(b); ok {
			return qa.Value == qb.Value && sameUnit(qa.Unit, qb.Unit)
		}
	}
	if ia, ok := isCQLInterval(a); ok {
		if ib, ok := isCQLInterval(b); ok {
			vcmp := compareCtx{conv: conv}
			return boundEqual(ia.Low, ib.Low, vcmp) &&
				boundEqual(ia.High, ib.High, vcmp) &&
				ia.LowClosed == ib.LowClosed && ia.HighClosed == ib.HighClosed
		}
	}
	if ta, aok := asTemporal(a, temporalLocation(b)); aok {
		if tb, bok := asTemporal(b, temporalLocation(a)); bok {
			return ta.Equal(tb)
		}
	}
	if fa, ok := asFloat(a); ok {
		if fb, ok := asFloat(b); ok {
			return fa == fb
		}
	}
	if la, ok := a.([]any); ok {
		lb, ok := b.([]any)
		if !ok || len(la) != len(lb) {
			return false
		}
		for i := range la {
			if !cqlEqualWithUCUM(la[i], lb[i], conv) {
				return false
			}
		}
		return true
	}
	if ma, ok := asObject(a); ok {
		if mb, ok := asObject(b); ok {
			if len(ma) != len(mb) {
				return false
			}
			for k, va := range ma {
				vb, ok := mb[k]
				if !ok || !cqlEqualWithUCUM(va, vb, conv) {
					return false
				}
			}
			return true
		}
	}
	if ca, ok := a.(Code); ok {
		if cb, ok := b.(Code); ok {
			return ca.System == cb.System && ca.Code == cb.Code
		}
		return false
	}
	if sa, ok := a.(string); ok {
		sb, ok := b.(string)
		return ok && sa == sb
	}
	if ba, ok := a.(bool); ok {
		bb, ok := b.(bool)
		return ok && ba == bb
	}
	return false
}

// cqlEquivalentStructuralEqual handles ~ for types where strict = matches ~; quantity and ratio
// are handled below; intervals use boundEqual with engine UCUM.
func cqlEquivalentStructuralEqual(a, b any, conv UCUMConverter) bool {
	if _, ok := asQuantity(a); ok {
		if _, ok := asQuantity(b); ok {
			return false
		}
	}
	if _, ok := asRatio(a); ok {
		if _, ok := asRatio(b); ok {
			return false
		}
	}
	if ia, ok := isCQLInterval(a); ok {
		if ib, ok := isCQLInterval(b); ok {
			vcmp := compareCtx{conv: conv}
			return boundEqual(ia.Low, ib.Low, vcmp) &&
				boundEqual(ia.High, ib.High, vcmp) &&
				ia.LowClosed == ib.LowClosed && ia.HighClosed == ib.HighClosed
		}
	}
	return cqlEqualWithUCUM(a, b, conv)
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
	if cqlEquivalentStructuralEqual(a, b, conv) {
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
