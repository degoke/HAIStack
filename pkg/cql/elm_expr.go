package cql

import (
	"encoding/json"
	"strconv"
	"strings"
)

func parseELMExpr(obj map[string]any) (Node, error) {
	if obj == nil {
		return nil, errf("%w: empty ELM expression", ErrUnsupported)
	}
	typ := elmType(obj)
	switch typ {
	case "", "Expression":
		if inner, ok := asObject(obj["expression"]); ok {
			return parseELMExpr(inner)
		}
		return nil, errf("%w: ELM expression is missing type", ErrUnsupported)
	case "Literal":
		return parseELMLiteral(obj)
	case "Null":
		return &litNode{}, nil
	case "Quantity":
		return &quantityNode{value: elmNumber(obj["value"]), unit: elmString(obj["unit"])}, nil
	case "Ratio":
		num, err := parseELMChild(obj, "numerator")
		if err != nil {
			return nil, err
		}
		den, err := parseELMChild(obj, "denominator")
		if err != nil {
			return nil, err
		}
		return &binaryNode{op: "/", left: num, right: den}, nil
	case "Date", "DateTime", "Time":
		return parseELMTemporal(obj, typ)
	case "Interval":
		low, err := parseELMOptional(obj, "low")
		if err != nil {
			return nil, err
		}
		high, err := parseELMOptional(obj, "high")
		if err != nil {
			return nil, err
		}
		lowClosed, highClosed := true, true
		if v, ok := obj["lowClosed"]; ok {
			lowClosed = elmBool(v)
		}
		if v, ok := obj["highClosed"]; ok {
			highClosed = elmBool(v)
		}
		return &intervalNode{low: low, high: high, lowClosed: lowClosed, highClosed: highClosed}, nil
	case "List":
		var elems []Node
		for _, el := range elmList(obj["element"]) {
			n, err := parseELMExpr(el)
			if err != nil {
				return nil, err
			}
			elems = append(elems, n)
		}
		return &listNode{elems: elems}, nil
	case "Tuple", "Instance":
		var fields []tupleField
		for _, el := range elmList(obj["element"]) {
			name := elmString(el["name"])
			valObj, ok := asObject(el["value"])
			if name == "" || !ok {
				continue
			}
			v, err := parseELMExpr(valObj)
			if err != nil {
				return nil, err
			}
			fields = append(fields, tupleField{name: name, value: v})
		}
		return &tupleNode{fields: fields}, nil
	case "Code":
		system := elmString(obj["system"])
		if cs, ok := asObject(obj["system"]); ok {
			system = firstNonEmpty(elmString(cs["name"]), elmString(cs["id"]))
		}
		return &codeLitNode{code: elmString(obj["code"]), system: system, display: elmString(obj["display"])}, nil
	case "Concept":
		codes := elmList(obj["codes"])
		if len(codes) > 0 {
			return parseELMExpr(codes[0])
		}
		return &litNode{}, nil
	case "IdentifierRef", "ExpressionRef", "ParameterRef", "OperandRef", "AliasRef", "ValueSetRef", "CodeRef", "CodeSystemRef", "ConceptRef":
		return parseELMRef(obj)
	case "Property":
		return parseELMProperty(obj)
	case "Indexer":
		ops, err := parseELMOperands(obj)
		if err != nil {
			return nil, err
		}
		if len(ops) < 2 {
			return nil, errf("%w: ELM Indexer requires two operands", ErrUnsupported)
		}
		return &indexNode{x: ops[0], index: ops[1]}, nil
	case "Retrieve":
		return parseELMRetrieve(obj)
	case "Query":
		return parseELMQuery(obj)
	case "If":
		cond, err := parseELMChild(obj, "condition")
		if err != nil {
			return nil, err
		}
		thenN, err := parseELMChild(obj, "then")
		if err != nil {
			return nil, err
		}
		elseN, err := parseELMOptional(obj, "else")
		if err != nil {
			return nil, err
		}
		return &ifNode{cond: cond, thenN: thenN, elseN: elseN}, nil
	case "Case":
		return parseELMCase(obj)
	case "FunctionRef":
		return parseELMFunctionRef(obj)
	case "As", "Convert":
		x, err := firstELMOperand(obj)
		if err != nil {
			return nil, err
		}
		target := firstNonEmpty(elmTypeName(obj["asTypeSpecifier"]), elmTypeName(obj["toTypeSpecifier"]), elmTypeBare(elmString(obj["asType"])), elmTypeBare(elmString(obj["toType"])))
		if target == "" {
			return x, nil
		}
		return &convertNode{x: x, target: target}, nil
	case "Is":
		x, err := firstELMOperand(obj)
		if err != nil {
			return nil, err
		}
		target := firstNonEmpty(elmTypeName(obj["isTypeSpecifier"]), elmTypeBare(elmString(obj["isType"])), "null")
		return &isNode{x: x, target: target}, nil
	case "IsNull":
		x, err := firstELMOperand(obj)
		if err != nil {
			return nil, err
		}
		return &isNode{x: x, target: "null"}, nil
	case "IsTrue":
		x, err := firstELMOperand(obj)
		if err != nil {
			return nil, err
		}
		return &binaryNode{op: "=", left: x, right: &litNode{value: true}}, nil
	case "IsFalse":
		x, err := firstELMOperand(obj)
		if err != nil {
			return nil, err
		}
		return &binaryNode{op: "=", left: x, right: &litNode{value: false}}, nil
	case "Not":
		x, err := firstELMOperand(obj)
		if err != nil {
			return nil, err
		}
		return &unaryNode{op: "not", x: x}, nil
	case "Negate", "UnaryMinus":
		x, err := firstELMOperand(obj)
		if err != nil {
			return nil, err
		}
		return &unaryNode{op: "-", x: x}, nil
	case "Exists":
		x, err := firstELMOperand(obj)
		if err != nil {
			return nil, err
		}
		return &unaryNode{op: "exists", x: x}, nil
	case "SingletonFrom":
		x, err := firstELMOperand(obj)
		if err != nil {
			return nil, err
		}
		return &unaryNode{op: "singleton", x: x}, nil
	case "Flatten":
		x, err := firstELMOperand(obj)
		if err != nil {
			return nil, err
		}
		return &unaryNode{op: "flatten", x: x}, nil
	case "Start":
		x, err := firstELMOperand(obj)
		if err != nil {
			return nil, err
		}
		return &unaryNode{op: "start", x: x}, nil
	case "End":
		x, err := firstELMOperand(obj)
		if err != nil {
			return nil, err
		}
		return &unaryNode{op: "end", x: x}, nil
	case "Width":
		x, err := firstELMOperand(obj)
		if err != nil {
			return nil, err
		}
		return &unaryNode{op: "width", x: x}, nil
	case "ToList":
		return firstELMOperand(obj)
	case "Message":
		if src, err := parseELMChild(obj, "source"); err == nil {
			return src, nil
		}
		return firstELMOperand(obj)
	case "Filter":
		src, err := parseELMChild(obj, "source")
		if err != nil {
			return nil, err
		}
		cond, err := parseELMChild(obj, "condition")
		if err != nil {
			return nil, err
		}
		return &callNode{callee: &memberNode{x: src, name: "where"}, args: []Node{cond}}, nil
	case "ForEach":
		src, err := parseELMChild(obj, "source")
		if err != nil {
			return nil, err
		}
		elem, err := parseELMChild(obj, "element")
		if err != nil {
			return nil, err
		}
		return &callNode{callee: &memberNode{x: src, name: "select"}, args: []Node{elem}}, nil
	case "DurationBetween", "DifferenceBetween":
		ops, err := parseELMOperands(obj)
		if err != nil {
			return nil, err
		}
		if len(ops) < 2 {
			return nil, errf("%w: ELM %s requires two operands", ErrUnsupported, typ)
		}
		return &durationNode{unit: strings.ToLower(elmString(obj["precision"])), left: ops[0], right: ops[1], difference: typ == "DifferenceBetween"}, nil
	case "CalculateAge", "CalculateAgeAt":
		return parseELMAge(obj, typ)
	case "InValueSet":
		return parseELMInValueSet(obj)
	case "InCodeSystem":
		ops, err := parseELMOperands(obj)
		if err != nil {
			return nil, err
		}
		if len(ops) < 2 {
			return nil, errf("%w: ELM InCodeSystem requires two operands", ErrUnsupported)
		}
		return &binaryNode{op: "in", left: ops[0], right: ops[1]}, nil
	}
	if op, ok := elmBinaryOp(typ); ok {
		ops, err := parseELMOperands(obj)
		if err != nil {
			return nil, err
		}
		if len(ops) < 2 {
			return nil, errf("%w: ELM operator %s requires two operands", ErrUnsupported, typ)
		}
		n := Node(ops[0])
		for i := 1; i < len(ops); i++ {
			n = &binaryNode{op: op, left: n, right: ops[i]}
		}
		return n, nil
	}
	return parseELMCallFallback(obj, typ)
}

func parseELMInValueSet(obj map[string]any) (Node, error) {
	code, err := parseELMNamedOrOperand(obj, "code")
	if err != nil {
		return nil, err
	}
	if ref, ok := asObject(obj["valueset"]); ok {
		vs, err := parseELMExpr(ref)
		if err != nil {
			return nil, err
		}
		return &binaryNode{op: "in", left: code, right: vs}, nil
	}
	ops, err := parseELMOperands(obj)
	if err != nil || len(ops) < 2 {
		return nil, errf("%w: ELM InValueSet requires a valueset", ErrUnsupported)
	}
	return &binaryNode{op: "in", left: ops[0], right: ops[1]}, nil
}

func parseELMLiteral(obj map[string]any) (Node, error) {
	valueType := elmTypeBare(elmString(obj["valueType"]))
	v := obj["value"]
	switch strings.ToLower(valueType) {
	case "boolean":
		return &litNode{value: elmBool(v)}, nil
	case "integer":
		if n, ok := elmInt(v); ok {
			return &litNode{value: n}, nil
		}
	case "decimal", "long":
		return &litNode{value: elmNumber(v)}, nil
	case "string":
		return &litNode{value: elmString(v)}, nil
	case "date":
		if tm, ok := parseELMDateString(elmString(v), false); ok {
			return &litNode{value: tm}, nil
		}
	case "datetime":
		if tm, ok := parseELMDateString(elmString(v), true); ok {
			return &litNode{value: tm}, nil
		}
	case "time":
		if tm, ok := parseELMTimeString(elmString(v)); ok {
			return &litNode{value: tm}, nil
		}
	}
	switch x := v.(type) {
	case bool:
		return &litNode{value: x}, nil
	case float64:
		if x == float64(int64(x)) {
			return &litNode{value: int64(x)}, nil
		}
		return &litNode{value: x}, nil
	case json.Number:
		if i, err := x.Int64(); err == nil {
			return &litNode{value: i}, nil
		}
		f, _ := x.Float64()
		return &litNode{value: f}, nil
	case string:
		if b, ok := parseBoolString(x); ok {
			return &litNode{value: b}, nil
		}
		if i, err := strconv.ParseInt(x, 10, 64); err == nil {
			return &litNode{value: i}, nil
		}
		if f, err := strconv.ParseFloat(x, 64); err == nil {
			return &litNode{value: f}, nil
		}
		return &litNode{value: x}, nil
	case nil:
		return &litNode{}, nil
	}
	return &litNode{value: elmString(v)}, nil
}

func parseELMTemporal(obj map[string]any, typ string) (Node, error) {
	name := typ
	var args []Node
	for _, field := range []string{"year", "month", "day", "hour", "minute", "second", "millisecond"} {
		if typ == "Date" && (field == "hour" || field == "minute" || field == "second" || field == "millisecond") {
			continue
		}
		if typ == "Time" && (field == "year" || field == "month" || field == "day") {
			continue
		}
		child, ok := obj[field]
		if !ok {
			continue
		}
		if m, ok := asObject(child); ok {
			n, err := parseELMExpr(m)
			if err != nil {
				return nil, err
			}
			args = append(args, n)
			continue
		}
		if n, ok := elmInt(child); ok {
			args = append(args, &litNode{value: n})
			continue
		}
		args = append(args, &litNode{value: elmNumber(child)})
	}
	if len(args) == 0 {
		if s := elmString(obj["value"]); s != "" {
			if typ == "Time" {
				if tm, ok := parseELMTimeString(s); ok {
					return &litNode{value: tm}, nil
				}
			}
			if tm, ok := parseELMDateString(s, typ != "Date"); ok {
				return &litNode{value: tm}, nil
			}
		}
	}
	return &callNode{callee: &identNode{name: name}, args: args}, nil
}

func parseELMRef(obj map[string]any) (Node, error) {
	name := firstNonEmpty(elmString(obj["name"]), elmString(obj["id"]))
	if name == "" {
		return nil, errf("%w: ELM reference is missing a name", ErrUnsupported)
	}
	libName := elmString(obj["libraryName"])
	n := Node(&identNode{name: name})
	if libName != "" {
		n = &memberNode{x: &identNode{name: libName}, name: name}
	}
	return n, nil
}

func parseELMProperty(obj map[string]any) (Node, error) {
	path := elmString(obj["path"])
	var base Node
	if src, ok := asObject(obj["source"]); ok {
		n, err := parseELMExpr(src)
		if err != nil {
			return nil, err
		}
		base = n
	} else if scope := elmString(obj["scope"]); scope != "" {
		base = &identNode{name: scope}
	} else {
		base = &identNode{name: "$this"}
	}
	if path == "" {
		return base, nil
	}
	n := base
	for _, part := range strings.Split(path, ".") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		n = &memberNode{x: n, name: part}
	}
	return n, nil
}

func parseELMRetrieve(obj map[string]any) (Node, error) {
	rt := elmTypeBare(firstNonEmpty(elmString(obj["dataType"]), elmString(obj["templateId"])))
	n := &retrieveNode{resourceType: rt, comparator: "in"}
	if cmp := elmString(obj["codeComparator"]); cmp != "" {
		n.comparator = cmp
	}
	n.codePath = firstNonEmpty(elmString(obj["codeProperty"]), elmString(obj["codePath"]))
	if codes, ok := asObject(obj["codes"]); ok {
		term, cmp := elmRetrieveCodes(codes, n.comparator)
		n.terminology = term
		n.comparator = cmp
	} else if list := elmList(obj["codes"]); len(list) > 0 {
		term, cmp := elmRetrieveCodes(list[0], n.comparator)
		n.terminology = term
		n.comparator = cmp
	}
	return n, nil
}

func elmRetrieveCodes(codes map[string]any, cmp string) (string, string) {
	if codes == nil {
		return "", cmp
	}
	switch elmType(codes) {
	case "ValueSetRef":
		if cmp == "" {
			cmp = "in"
		}
		return firstNonEmpty(elmString(codes["name"]), elmString(codes["id"])), cmp
	case "CodeRef", "ConceptRef":
		if cmp == "in" {
			cmp = "="
		}
		return firstNonEmpty(elmString(codes["name"]), elmString(codes["id"])), cmp
	case "ToList":
		if inner, ok := asObject(codes["operand"]); ok {
			return elmRetrieveCodes(inner, cmp)
		}
		ops := elmList(codes["operand"])
		if len(ops) > 0 {
			return elmRetrieveCodes(ops[0], cmp)
		}
	case "List":
		elems := elmList(codes["element"])
		if len(elems) > 0 {
			return elmRetrieveCodes(elems[0], cmp)
		}
	default:
		if name := firstNonEmpty(elmString(codes["name"]), elmString(codes["id"])); name != "" {
			return name, cmp
		}
		elems := elmList(codes["element"])
		if len(elems) > 0 {
			return elmRetrieveCodes(elems[0], cmp)
		}
	}
	return "", cmp
}

func parseELMQuery(obj map[string]any) (Node, error) {
	q := &queryNode{}
	sources := elmList(obj["source"])
	if len(sources) == 0 {
		if src, ok := asObject(obj["source"]); ok {
			sources = []map[string]any{src}
		}
	}
	for _, src := range sources {
		exprObj, ok := asObject(src["expression"])
		if !ok {
			return nil, errf("%w: ELM query source is missing an expression", ErrUnsupported)
		}
		expr, err := parseELMExpr(exprObj)
		if err != nil {
			return nil, err
		}
		q.sources = append(q.sources, querySource{expr: expr, alias: elmString(src["alias"])})
	}
	for _, let := range elmList(obj["let"]) {
		name := firstNonEmpty(elmString(let["identifier"]), elmString(let["name"]))
		exprObj, ok := asObject(let["expression"])
		if name == "" || !ok {
			continue
		}
		expr, err := parseELMExpr(exprObj)
		if err != nil {
			return nil, err
		}
		q.lets = append(q.lets, letClause{name: name, expr: expr})
	}
	for _, rel := range elmList(obj["relationship"]) {
		exprObj, ok := asObject(rel["expression"])
		if !ok {
			continue
		}
		expr, err := parseELMExpr(exprObj)
		if err != nil {
			return nil, err
		}
		var such Node
		if suchObj, ok := asObject(rel["suchThat"]); ok {
			such, err = parseELMExpr(suchObj)
			if err != nil {
				return nil, err
			}
		}
		q.related = append(q.related, relatedClause{
			source:  querySource{expr: expr, alias: elmString(rel["alias"])},
			such:    such,
			without: strings.EqualFold(elmType(rel), "Without"),
		})
	}
	if where, ok := asObject(obj["where"]); ok {
		n, err := parseELMExpr(where)
		if err != nil {
			return nil, err
		}
		q.where = n
	}
	if ret, ok := asObject(obj["return"]); ok {
		q.distinct = elmBool(ret["distinct"])
		if expr, ok := asObject(ret["expression"]); ok {
			n, err := parseELMExpr(expr)
			if err != nil {
				return nil, err
			}
			q.ret = n
		}
	}
	if sort, ok := asObject(obj["sort"]); ok {
		for _, by := range elmList(sort["by"]) {
			desc := strings.EqualFold(elmString(by["direction"]), "desc")
			var expr Node
			switch elmType(by) {
			case "ByColumn":
				expr = &identNode{name: elmString(by["path"])}
			case "ByExpression":
				if e, ok := asObject(by["expression"]); ok {
					n, err := parseELMExpr(e)
					if err != nil {
						return nil, err
					}
					expr = n
				}
			}
			if expr != nil {
				q.sort = append(q.sort, sortItem{expr: expr, desc: desc})
			}
		}
	}
	if agg, ok := asObject(obj["aggregate"]); ok {
		body, err := parseELMChild(agg, "expression")
		if err != nil {
			return nil, err
		}
		var starting Node
		if _, ok := asObject(agg["starting"]); ok {
			starting, err = parseELMChild(agg, "starting")
			if err != nil {
				return nil, err
			}
		}
		q.agg = &aggregateNode{
			name:     firstNonEmpty(elmString(agg["identifier"]), elmString(agg["name"])),
			starting: starting,
			body:     body,
			distinct: elmBool(agg["distinct"]),
		}
	}
	if len(q.sources) == 0 {
		return nil, errf("%w: ELM query has no source", ErrUnsupported)
	}
	return q, nil
}

func parseELMCase(obj map[string]any) (Node, error) {
	n := &caseNode{}
	if test, ok := asObject(obj["comparand"]); ok {
		t, err := parseELMExpr(test)
		if err != nil {
			return nil, err
		}
		n.test = t
	}
	for _, item := range elmList(obj["caseItem"]) {
		when, err := parseELMChild(item, "when")
		if err != nil {
			return nil, err
		}
		thenN, err := parseELMChild(item, "then")
		if err != nil {
			return nil, err
		}
		n.whens = append(n.whens, caseWhen{when: when, then: thenN})
	}
	if elseObj, ok := asObject(obj["else"]); ok {
		e, err := parseELMExpr(elseObj)
		if err != nil {
			return nil, err
		}
		n.elseN = e
	}
	return n, nil
}

func parseELMFunctionRef(obj map[string]any) (Node, error) {
	name := elmString(obj["name"])
	if name == "" {
		return nil, errf("%w: ELM FunctionRef is missing a name", ErrUnsupported)
	}
	args, err := parseELMOperands(obj)
	if err != nil {
		return nil, err
	}
	var callee Node = &identNode{name: name}
	if libName := elmString(obj["libraryName"]); libName != "" {
		callee = &memberNode{x: &identNode{name: libName}, name: name}
	}
	return &callNode{callee: callee, args: args}, nil
}

func parseELMAge(obj map[string]any, typ string) (Node, error) {
	precision := strings.ToLower(elmString(obj["precision"]))
	if precision == "" {
		precision = "year"
	}
	ops, err := parseELMOperands(obj)
	if err != nil {
		return nil, err
	}
	var birth, at Node
	if len(ops) > 0 {
		birth = ops[0]
	}
	if typ == "CalculateAgeAt" && len(ops) >= 2 {
		at = ops[1]
	}
	if birth == nil || isELMPatientBirthDate(birth) {
		if at != nil {
			return &callNode{callee: &identNode{name: "AgeInYearsAt"}, args: []Node{at}}, nil
		}
		return &callNode{callee: &identNode{name: "AgeInYears"}, args: nil}, nil
	}
	asOf := at
	if asOf == nil {
		asOf = &callNode{callee: &identNode{name: "Today"}, args: nil}
	}
	return &durationNode{unit: precision, left: birth, right: asOf}, nil
}

func isELMPatientBirthDate(n Node) bool {
	var parts []string
	cur := n
	for {
		mem, ok := cur.(*memberNode)
		if !ok {
			break
		}
		parts = append([]string{mem.name}, parts...)
		cur = mem.x
	}
	id, ok := cur.(*identNode)
	if !ok || !strings.EqualFold(id.name, "Patient") || len(parts) == 0 {
		return false
	}
	if !strings.EqualFold(parts[0], "birthDate") {
		return false
	}
	if len(parts) == 1 {
		return true
	}
	return len(parts) == 2 && strings.EqualFold(parts[1], "value")
}

func parseELMCallFallback(obj map[string]any, typ string) (Node, error) {
	if typ == "" {
		return nil, errf("%w: ELM expression is missing type", ErrUnsupported)
	}
	args, err := parseELMOperands(obj)
	if err != nil {
		return nil, err
	}
	if len(args) == 0 {
		if src, err := parseELMChild(obj, "source"); err == nil {
			args = []Node{src}
		}
	}
	return &callNode{callee: &identNode{name: typ}, args: args}, nil
}

func parseELMChild(obj map[string]any, key string) (Node, error) {
	child, ok := asObject(obj[key])
	if !ok {
		return nil, errf("%w: ELM %s is missing", ErrUnsupported, key)
	}
	return parseELMExpr(child)
}

func parseELMOptional(obj map[string]any, key string) (Node, error) {
	v, ok := obj[key]
	if !ok || v == nil {
		return nil, nil
	}
	child, ok := asObject(v)
	if !ok {
		return nil, errf("%w: ELM %s is invalid", ErrUnsupported, key)
	}
	return parseELMExpr(child)
}

func parseELMNamedOrOperand(obj map[string]any, key string) (Node, error) {
	if child, ok := asObject(obj[key]); ok {
		return parseELMExpr(child)
	}
	ops, err := parseELMOperands(obj)
	if err != nil {
		return nil, err
	}
	if len(ops) == 0 {
		return nil, errf("%w: ELM %s is missing", ErrUnsupported, key)
	}
	return ops[0], nil
}

func firstELMOperand(obj map[string]any) (Node, error) {
	ops, err := parseELMOperands(obj)
	if err != nil {
		return nil, err
	}
	if len(ops) == 0 {
		if src, ok := asObject(obj["source"]); ok {
			return parseELMExpr(src)
		}
		return nil, errf("%w: ELM operator %s is missing an operand", ErrUnsupported, elmType(obj))
	}
	return ops[0], nil
}

func parseELMOperands(obj map[string]any) ([]Node, error) {
	var raw []map[string]any
	switch v := obj["operand"].(type) {
	case map[string]any:
		raw = []map[string]any{v}
	case []any:
		for _, el := range v {
			if m, ok := asObject(el); ok {
				raw = append(raw, m)
			}
		}
	}
	if len(raw) == 0 {
		if src, ok := asObject(obj["source"]); ok {
			raw = []map[string]any{src}
		}
	}
	out := make([]Node, 0, len(raw))
	for _, el := range raw {
		n, err := parseELMExpr(el)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, nil
}

func elmBinaryOp(typ string) (string, bool) {
	ops := map[string]string{
		"And": "and", "Or": "or", "Xor": "xor", "Implies": "implies",
		"Equal": "=", "Equivalent": "~", "NotEqual": "!=", "NotEquivalent": "!~",
		"Less": "<", "Greater": ">", "LessOrEqual": "<=", "GreaterOrEqual": ">=",
		"Add": "+", "Subtract": "-", "Multiply": "*", "Divide": "/",
		"TruncatedDivide": "div", "Modulo": "mod",
		"Concatenate": "&", "Concat": "&",
		"Union": "|", "Intersect": "intersect", "Except": "except",
		"In": "in", "Contains": "contains",
		"Includes": "includes", "IncludedIn": "included in",
		"ProperIncludes": "properly includes", "ProperIncludedIn": "properly included in",
		"ProperIn": "properly included in", "ProperContains": "properly includes",
		"During": "during", "ProperDuring": "properly during",
		"Overlaps": "overlaps", "OverlapsBefore": "overlaps", "OverlapsAfter": "overlaps",
		"Starts": "starts", "Ends": "ends", "Meets": "meets",
		"Before": "before", "After": "after",
		"SameAs": "same as", "SameOrBefore": "same or before", "SameOrAfter": "same or after",
	}
	op, ok := ops[typ]
	return op, ok
}
