package cql

import (
	"strconv"
	"strings"
	"time"
)

type parser struct {
	lex *lexer
	src string
}

func parseLibrary(src string) (*Library, error) {
	src = strings.TrimSpace(src)
	if src == "" {
		return nil, ErrEmptyExpression
	}
	p := &parser{lex: newLexer(src), src: src}
	lib := &Library{Source: src, Context: "Patient", Defines: nil}
	if !p.acceptKeyword("library") {
		return nil, parseError(src, p.lex.lookahead().pos, "expected library declaration")
	}
	name, err := p.requireName("library name")
	if err != nil {
		return nil, err
	}
	lib.Name = name
	if p.acceptKeyword("version") {
		v, err := p.requireString("library version")
		if err != nil {
			return nil, err
		}
		lib.Version = v
	}
	for {
		switch {
		case p.acceptKeyword("using"):
			model, err := p.requireIdent("using model")
			if err != nil {
				return nil, err
			}
			lib.Using = model
			if p.acceptKeyword("version") {
				if _, err := p.requireString("model version"); err != nil {
					return nil, err
				}
			}
		case p.acceptKeyword("include"):
			inc, err := p.parseInclude()
			if err != nil {
				return nil, err
			}
			lib.Includes = append(lib.Includes, inc)
		case p.acceptKeyword("codesystem"):
			cs, err := p.parseCodeSystem()
			if err != nil {
				return nil, err
			}
			lib.CodeSystems = append(lib.CodeSystems, cs)
		case p.acceptKeyword("valueset"):
			vs, err := p.parseValueSet()
			if err != nil {
				return nil, err
			}
			lib.ValueSets = append(lib.ValueSets, vs)
		case p.acceptKeyword("code"):
			code, err := p.parseCode()
			if err != nil {
				return nil, err
			}
			lib.Codes = append(lib.Codes, code)
		case p.acceptKeyword("concept"):
			concept, err := p.parseConcept()
			if err != nil {
				return nil, err
			}
			lib.Concepts = append(lib.Concepts, concept)
		case p.acceptKeyword("parameter"):
			param, err := p.parseParameter()
			if err != nil {
				return nil, err
			}
			lib.Parameters = append(lib.Parameters, param)
		case p.acceptKeyword("context"):
			ctxName, err := p.requireIdent("context")
			if err != nil {
				return nil, err
			}
			lib.Context = ctxName
		case p.acceptKeyword("define"):
			def, fn, err := p.parseDefine()
			if err != nil {
				return nil, err
			}
			if fn != nil {
				lib.Functions = append(lib.Functions, *fn)
			} else {
				lib.Defines = append(lib.Defines, def)
			}
		default:
			if p.lex.lookahead().kind == tEOF {
				if len(lib.Defines) == 0 && len(lib.Functions) == 0 {
					return nil, parseError(src, p.lex.lookahead().pos, "library has no define statements")
				}
				resolveTerminologyDecls(lib)
				return lib, nil
			}
			return nil, parseError(src, p.lex.lookahead().pos, "unexpected token %q", p.lex.lookahead().text)
		}
	}
}

func parseExpression(src string) (Node, error) {
	src = strings.TrimSpace(src)
	if src == "" {
		return nil, ErrEmptyExpression
	}
	p := &parser{lex: newLexer(src), src: src}
	n, err := p.tryParseBracketQuery()
	if err != nil {
		return nil, err
	}
	if n == nil {
		n, err = p.parseExpr()
		if err != nil {
			return nil, err
		}
	}
	if p.lex.lookahead().kind != tEOF {
		return nil, parseError(src, p.lex.lookahead().pos, "unexpected token %q after expression", p.lex.lookahead().text)
	}
	return n, nil
}

// tryParseBracketQuery parses expression-level queries such as [Observation] O where ...
// without treating them as queries inside let operands or other parseExpr contexts.
func (p *parser) tryParseBracketQuery() (Node, error) {
	br := p.lex.lookahead()
	if br.kind != tLBrack {
		return nil, nil
	}
	cp := br.pos
	n, err := p.parseRetrieve()
	if err != nil {
		return nil, err
	}
	t := p.lex.lookahead()
	if t.kind == tQuotedIdent || (t.kind == tIdent && !isReservedAlias(t.text)) {
		p.lex.next()
		return p.parseQuery(querySource{expr: n, alias: t.text})
	}
	p.lex.rewind(cp)
	return nil, nil
}

func (p *parser) parseInclude() (Include, error) {
	name, err := p.requireName("include library")
	if err != nil {
		return Include{}, err
	}
	inc := Include{Name: name}
	if p.acceptKeyword("version") {
		v, err := p.requireString("include version")
		if err != nil {
			return Include{}, err
		}
		inc.Version = v
	}
	if p.acceptKeyword("called") {
		called, err := p.requireName("include alias")
		if err != nil {
			return Include{}, err
		}
		inc.Called = called
	}
	return inc, nil
}

func (p *parser) parseDefine() (Define, *Function, error) {
	access := "public"
	fluent := false
	if p.acceptKeyword("public") || p.acceptKeyword("private") || p.acceptKeyword("fluent") {
		access = strings.ToLower(p.lex.last.text)
		if access == "fluent" {
			fluent = true
			access = "public"
			if p.acceptKeyword("public") || p.acceptKeyword("private") {
				access = strings.ToLower(p.lex.last.text)
			}
		}
	}
	if p.acceptKeyword("fluent") {
		fluent = true
	}
	if p.acceptKeyword("function") {
		fn, err := p.parseFunction(access, fluent)
		if err != nil {
			return Define{}, nil, err
		}
		return Define{}, &fn, nil
	}
	name, err := p.requireName("define name")
	if err != nil {
		return Define{}, nil, err
	}
	if !p.acceptKind(tColon) {
		return Define{}, nil, parseError(p.src, p.lex.lookahead().pos, "expected ':' after define %s", name)
	}
	start := p.lex.lookahead().pos
	expr, err := p.parseExpr()
	if err != nil {
		return Define{}, nil, err
	}
	end := p.lex.lookahead().pos
	if end < start {
		end = start
	}
	if end > len(p.src) {
		end = len(p.src)
	}
	return Define{Name: name, Access: access, Expression: expr, Source: strings.TrimSpace(p.src[start:end])}, nil, nil
}

func (p *parser) parseFunction(access string, fluent bool) (Function, error) {
	name, err := p.requireName("function")
	if err != nil {
		return Function{}, err
	}
	if !p.acceptKind(tLParen) {
		return Function{}, parseError(p.src, p.lex.lookahead().pos, "expected '(' after function %s", name)
	}
	var params []FunctionParam
	if !p.acceptKind(tRParen) {
		for {
			pname, err := p.requireName("function parameter")
			if err != nil {
				return Function{}, err
			}
			ptype := p.parseOptionalTypeName()
			params = append(params, FunctionParam{Name: pname, Type: ptype})
			if p.acceptKind(tComma) {
				continue
			}
			if !p.acceptKind(tRParen) {
				return Function{}, parseError(p.src, p.lex.lookahead().pos, "expected ')' after function parameters")
			}
			break
		}
	}
	if !p.acceptKind(tColon) {
		return Function{}, parseError(p.src, p.lex.lookahead().pos, "expected ':' after function %s", name)
	}
	start := p.lex.lookahead().pos
	body, err := p.parseExpr()
	if err != nil {
		return Function{}, err
	}
	end := p.lex.lookahead().pos
	if end < start {
		end = start
	}
	if end > len(p.src) {
		end = len(p.src)
	}
	return Function{
		Name:   name,
		Access: access,
		Fluent: fluent,
		Params: params,
		Body:   body,
		Source: strings.TrimSpace(p.src[start:end]),
	}, nil
}

func (p *parser) parseCodeSystem() (CodeSystem, error) {
	name, url, err := p.parseNamedURL("codesystem")
	if err != nil {
		return CodeSystem{}, err
	}
	return CodeSystem{Name: name, URL: url}, nil
}

func (p *parser) parseValueSet() (ValueSet, error) {
	name, url, err := p.parseNamedURL("valueset")
	if err != nil {
		return ValueSet{}, err
	}
	if p.acceptKeyword("codesystems") {
		if !p.acceptKind(tLBrace) {
			return ValueSet{}, parseError(p.src, p.lex.lookahead().pos, "expected '{' after codesystems")
		}
		if !p.acceptKind(tRBrace) {
			for {
				if _, err := p.requireName("codesystem reference"); err != nil {
					return ValueSet{}, err
				}
				if p.acceptKind(tComma) {
					continue
				}
				break
			}
			if !p.acceptKind(tRBrace) {
				return ValueSet{}, parseError(p.src, p.lex.lookahead().pos, "expected '}' after codesystems")
			}
		}
	}
	return ValueSet{Name: name, URL: url}, nil
}

func (p *parser) parseCode() (Code, error) {
	name, err := p.requireName("code name")
	if err != nil {
		return Code{}, err
	}
	if !p.acceptKind(tColon) {
		return Code{}, parseError(p.src, p.lex.lookahead().pos, "expected ':' in code declaration")
	}
	code, err := p.requireString("code value")
	if err != nil {
		return Code{}, err
	}
	out := Code{Name: name, Code: code}
	if p.acceptKeyword("from") {
		system, err := p.requireName("codesystem")
		if err != nil {
			return Code{}, err
		}
		out.System = system
	}
	if p.acceptKeyword("display") {
		display, err := p.requireString("code display")
		if err != nil {
			return Code{}, err
		}
		out.Display = display
	}
	return out, nil
}

func (p *parser) parseConcept() (Concept, error) {
	name, err := p.requireName("concept name")
	if err != nil {
		return Concept{}, err
	}
	if !p.acceptKind(tColon) {
		return Concept{}, parseError(p.src, p.lex.lookahead().pos, "expected ':' in concept declaration")
	}
	out := Concept{Name: name}
	if p.acceptKind(tLBrace) {
		if !p.acceptKind(tRBrace) {
			for {
				cname, err := p.requireName("concept code")
				if err != nil {
					return Concept{}, err
				}
				out.Codes = append(out.Codes, cname)
				if p.acceptKind(tComma) {
					continue
				}
				break
			}
			if !p.acceptKind(tRBrace) {
				return Concept{}, parseError(p.src, p.lex.lookahead().pos, "expected '}' after concept codes")
			}
		}
	}
	if p.acceptKeyword("display") {
		display, err := p.requireString("concept display")
		if err != nil {
			return Concept{}, err
		}
		out.Display = display
	}
	return out, nil
}

func (p *parser) parseNamedURL(kind string) (name, url string, err error) {
	name, err = p.requireName(kind + " name")
	if err != nil {
		return "", "", err
	}
	if !p.acceptKind(tColon) {
		return "", "", parseError(p.src, p.lex.lookahead().pos, "expected ':' in %s declaration", kind)
	}
	url, err = p.requireString(kind + " url")
	if err != nil {
		return "", "", err
	}
	if p.acceptKeyword("version") {
		if _, err := p.requireString(kind + " version"); err != nil {
			return "", "", err
		}
	}
	return name, url, nil
}

func (p *parser) skipNamedDeclaration() error {
	if _, err := p.requireName("declaration name"); err != nil {
		return err
	}
	if !p.acceptKind(tColon) {
		return parseError(p.src, p.lex.lookahead().pos, "expected ':' in declaration")
	}
	if p.lex.lookahead().kind == tString {
		p.lex.next()
	} else if _, err := p.requireName("declaration value"); err != nil {
		return err
	}
	_ = p.acceptKeyword("from")
	if p.lex.lookahead().kind == tQuotedIdent || p.lex.lookahead().kind == tIdent {
		p.lex.next()
	}
	if p.acceptKeyword("display") {
		if p.lex.lookahead().kind == tString {
			p.lex.next()
		}
	}
	return nil
}

func resolveTerminologyDecls(lib *Library) {
	if lib == nil {
		return
	}
	csByName := map[string]string{}
	for _, cs := range lib.CodeSystems {
		if cs.Name != "" && cs.URL != "" {
			csByName[cs.Name] = cs.URL
		}
	}
	for i, c := range lib.Codes {
		if c.System == "" || strings.Contains(c.System, "://") {
			continue
		}
		if url, ok := csByName[c.System]; ok {
			lib.Codes[i].System = url
		}
	}
}

func (p *parser) parseParameter() (Parameter, error) {
	name, err := p.requireName("parameter name")
	if err != nil {
		return Parameter{}, err
	}
	param := Parameter{Name: name}
	if p.lex.lookahead().kind == tIdent && !isExprStartKeyword(p.lex.lookahead().text) && !keywordEq(p.lex.lookahead().text, "default") {
		param.Type = p.parseOptionalTypeName()
	}
	if p.acceptKeyword("default") {
		expr, err := p.parseExpr()
		if err != nil {
			return Parameter{}, err
		}
		param.Default = expr
	}
	return param, nil
}

func (p *parser) parseOptionalTypeName() string {
	t := p.lex.lookahead()
	if t.kind != tIdent && t.kind != tQuotedIdent {
		return ""
	}
	if isExprStartKeyword(t.text) || keywordEq(t.text, "default") {
		return ""
	}
	p.lex.next()
	name := t.text
	if p.acceptKind(tLt) {
		inner, err := p.requireName("type argument")
		if err != nil {
			return name
		}
		_ = p.acceptKind(tGt)
		return name + "<" + inner + ">"
	}
	return name
}

func isExprStartKeyword(text string) bool {
	switch strings.ToLower(text) {
	case "library", "using", "include", "codesystem", "valueset", "code", "concept", "parameter", "context", "define",
		"and", "or", "not", "xor", "implies", "default", "function", "fluent",
		"where", "return", "sort", "with", "without", "let", "aggregate":
		return true
	}
	return false
}

func isQueryClauseKeyword(text string) bool {
	switch strings.ToLower(text) {
	case "where", "return", "sort", "with", "without", "let", "aggregate", "such", "from":
		return true
	}
	return false
}

func isReservedAlias(text string) bool {
	if isExprStartKeyword(text) || isQueryClauseKeyword(text) {
		return true
	}
	switch strings.ToLower(text) {
	case "then", "else", "is", "as", "in", "contains", "union", "intersect", "except",
		"div", "mod", "true", "false", "null", "if", "case", "end", "when", "between",
		"during", "includes", "overlaps", "starts", "ends", "meets", "before", "after",
		"of", "duration", "difference", "asc", "desc", "all", "distinct", "minimum", "maximum", "year", "years",
		"month", "months", "week", "weeks", "day", "days", "hour", "hours", "minute",
		"minutes", "second", "seconds", "millisecond", "milliseconds":
		return true
	}
	return false
}

func (p *parser) parseExpr() (Node, error) {
	return p.parseImplies()
}

func (p *parser) parseImplies() (Node, error) {
	left, err := p.parseOr()
	if err != nil {
		return nil, err
	}
	for p.acceptKeyword("implies") {
		right, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		left = &binaryNode{nodeBase: nodeBase{src: p.src}, op: "implies", left: left, right: right}
	}
	return left, nil
}

func (p *parser) parseOr() (Node, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for {
		op := ""
		if p.acceptKeyword("or") {
			op = "or"
		} else if p.acceptKeyword("xor") {
			op = "xor"
		} else {
			break
		}
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = &binaryNode{nodeBase: nodeBase{src: p.src}, op: op, left: left, right: right}
	}
	return left, nil
}

func (p *parser) parseAnd() (Node, error) {
	left, err := p.parseNot()
	if err != nil {
		return nil, err
	}
	for p.acceptKeyword("and") {
		right, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		left = &binaryNode{nodeBase: nodeBase{src: p.src}, op: "and", left: left, right: right}
	}
	return left, nil
}

func (p *parser) parseNot() (Node, error) {
	n, err := p.parseNotExpression()
	if err != nil {
		return nil, err
	}
	return p.parseTypeSuffix(n)
}

func (p *parser) parseNotExpression() (Node, error) {
	if p.acceptKeyword("not") {
		x, err := p.parseNotExpression()
		if err != nil {
			return nil, err
		}
		return &unaryNode{nodeBase: nodeBase{src: p.src}, op: "not", x: x}, nil
	}
	if p.acceptKeyword("exists") {
		x, err := p.parseNotExpression()
		if err != nil {
			return nil, err
		}
		return &unaryNode{nodeBase: nodeBase{src: p.src}, op: "exists", x: x}, nil
	}
	return p.parseIn()
}

func (p *parser) parseTypeSuffix(n Node) (Node, error) {
	for {
		if p.acceptKeyword("is") {
			not := p.acceptKeyword("not")
			target := "null"
			if p.acceptKeyword("null") {
				target = "null"
			} else {
				tname, err := p.requireName("type name")
				if err != nil {
					return nil, err
				}
				target = tname
			}
			n = &isNode{nodeBase: nodeBase{src: p.src}, x: n, not: not, target: target}
			continue
		}
		if p.acceptKeyword("as") {
			tname, err := p.requireName("type name")
			if err != nil {
				return nil, err
			}
			n = &asNode{nodeBase: nodeBase{src: p.src}, x: n, target: tname}
			continue
		}
		break
	}
	return n, nil
}

func (p *parser) parseIn() (Node, error) {
	left, err := p.parseComparison()
	if err != nil {
		return nil, err
	}
	for {
		if p.acceptKeyword("between") {
			low, err := p.parseComparison()
			if err != nil {
				return nil, err
			}
			if !p.acceptKeyword("and") {
				return nil, parseError(p.src, p.lex.lookahead().pos, "expected 'and' after between")
			}
			high, err := p.parseComparison()
			if err != nil {
				return nil, err
			}
			left = &betweenNode{nodeBase: nodeBase{src: p.src}, x: left, low: low, high: high}
			continue
		}
		op := p.parseMembershipOp()
		if op == "" {
			break
		}
		right, err := p.parseComparison()
		if err != nil {
			return nil, err
		}
		left = &binaryNode{nodeBase: nodeBase{src: p.src}, op: op, left: left, right: right}
	}
	return left, nil
}

func (p *parser) parseMembershipOp() string {
	if p.peekKeywordPair("all", "in") {
		p.lex.next()
		p.lex.next()
		return "all in"
	}
	if p.peekKeywordPair("any", "in") {
		p.lex.next()
		p.lex.next()
		return "any in"
	}
	if p.acceptKeyword("properly") {
		switch {
		case p.acceptKeyword("includes"):
			return "properly includes"
		case p.acceptKeyword("included"):
			_ = p.acceptKeyword("in")
			return "properly included in"
		case p.acceptKeyword("during"):
			return "properly during"
		case p.acceptKeyword("subsumes"):
			return "properly subsumes"
		case p.acceptKeyword("subsumed"):
			_ = p.acceptKeyword("by")
			return "properly subsumed by"
		default:
			return "properly"
		}
	}
	if p.acceptKeyword("included") {
		_ = p.acceptKeyword("in")
		return "included in"
	}
	if p.acceptKeyword("subsumes") {
		return "subsumes"
	}
	if p.acceptKeyword("subsumed") {
		_ = p.acceptKeyword("by")
		return "subsumed by"
	}
	if p.acceptKeyword("occurs") {
		if p.acceptKeyword("during") {
			return "during"
		}
		if p.acceptKeyword("before") {
			return "before"
		}
		if p.acceptKeyword("after") {
			return "after"
		}
	}
	if p.acceptKeyword("same") {
		unit := ""
		if u := timeUnitName(p.lex.lookahead().text); u != "" {
			p.lex.next()
			unit = u
		}
		if p.acceptKeyword("or") {
			rel := ""
			if p.acceptKeyword("before") {
				rel = "before"
			} else if p.acceptKeyword("after") {
				rel = "after"
			}
			if rel != "" {
				if unit != "" {
					return "same " + unit + " or " + rel
				}
				return "same or " + rel
			}
		}
		_ = p.acceptKeyword("as")
		if unit != "" {
			return "same " + unit + " as"
		}
		return "same as"
	}
	switch {
	case p.acceptKeyword("in"):
		return "in"
	case p.acceptKeyword("contains"):
		return "contains" + p.timingPrecision()
	case p.acceptKeyword("includes"):
		return "includes" + p.timingPrecision()
	case p.acceptKeyword("during"):
		return "during" + p.timingPrecision()
	case p.acceptKeyword("overlaps"):
		op := "overlaps"
		if p.acceptKeyword("before") {
			op = "overlaps before"
		} else if p.acceptKeyword("after") {
			op = "overlaps after"
		}
		return op + p.timingPrecision()
	case p.acceptKeyword("starts"):
		return "starts" + p.timingPrecision()
	case p.acceptKeyword("ends"):
		return "ends" + p.timingPrecision()
	case p.acceptKeyword("meets"):
		op := "meets"
		if p.acceptKeyword("before") {
			op = "meets before"
		} else if p.acceptKeyword("after") {
			op = "meets after"
		}
		return op + p.timingPrecision()
	case p.acceptKeyword("before"):
		return "before" + p.timingPrecision()
	case p.acceptKeyword("after"):
		return "after" + p.timingPrecision()
	}
	return ""
}

func (p *parser) timingPrecision() string {
	if u := timeUnitName(p.lex.lookahead().text); u != "" {
		p.lex.next()
		_ = p.acceptKeyword("of")
		return " " + u + " of"
	}
	return ""
}

func (p *parser) parseComparison() (Node, error) {
	left, err := p.parseConcat()
	if err != nil {
		return nil, err
	}
	op := ""
	switch p.lex.lookahead().kind {
	case tEq:
		op = "="
	case tNeq:
		op = "!="
	case tLt:
		op = "<"
	case tGt:
		op = ">"
	case tLe:
		op = "<="
	case tGe:
		op = ">="
	case tTilde:
		op = "~"
	case tNTilde:
		op = "!~"
	}
	if op == "" {
		return left, nil
	}
	p.lex.next()
	right, err := p.parseConcat()
	if err != nil {
		return nil, err
	}
	return &binaryNode{nodeBase: nodeBase{src: p.src}, op: op, left: left, right: right}, nil
}

func (p *parser) parseConcat() (Node, error) {
	left, err := p.parseAdd()
	if err != nil {
		return nil, err
	}
	for p.acceptKind(tAmp) {
		right, err := p.parseAdd()
		if err != nil {
			return nil, err
		}
		left = &binaryNode{nodeBase: nodeBase{src: p.src}, op: "&", left: left, right: right}
	}
	return left, nil
}

func (p *parser) parseAdd() (Node, error) {
	left, err := p.parseRatioMul()
	if err != nil {
		return nil, err
	}
	for {
		op := ""
		switch p.lex.lookahead().kind {
		case tPlus:
			op = "+"
			p.lex.next()
		case tMinus:
			op = "-"
			p.lex.next()
		case tPipe:
			op = "|"
			p.lex.next()
		default:
			if p.acceptKeyword("union") {
				op = "|"
			} else if p.acceptKeyword("intersect") {
				op = "intersect"
			} else if p.acceptKeyword("except") {
				op = "except"
			} else {
				return left, nil
			}
		}
		right, err := p.parseRatioMul()
		if err != nil {
			return nil, err
		}
		left = &binaryNode{nodeBase: nodeBase{src: p.src}, op: op, left: left, right: right}
	}
}

func (p *parser) parseRatioMul() (Node, error) {
	left, err := p.parseRatio()
	if err != nil {
		return nil, err
	}
	for {
		op := ""
		switch p.lex.lookahead().kind {
		case tStar:
			op = "*"
			p.lex.next()
		case tSlash:
			op = "/"
			p.lex.next()
		default:
			return left, nil
		}
		right, err := p.parseRatio()
		if err != nil {
			return nil, err
		}
		left = &binaryNode{nodeBase: nodeBase{src: p.src}, op: op, left: left, right: right}
	}
}

func (p *parser) parseRatio() (Node, error) {
	left, err := p.parseMul()
	if err != nil {
		return nil, err
	}
	if !p.peekRatioRightAfterColon() {
		return left, nil
	}
	p.acceptKind(tColon)
	right, err := p.parsePower()
	if err != nil {
		return nil, err
	}
	return &binaryNode{nodeBase: nodeBase{src: p.src}, op: ":", left: left, right: right}, nil
}

func (p *parser) peekRatioRightAfterColon() bool {
	t := p.lex.lookahead()
	if t.kind != tColon {
		return false
	}
	rest := p.src[t.pos:]
	lx := newLexer(rest)
	_ = lx.next()
	next := lx.next()
	switch next.kind {
	case tNumber, tString, tLParen:
		return true
	case tIdent:
		after := lx.next()
		if after.kind == tString {
			return true
		}
		if timeUnitName(next.text) != "" {
			return true
		}
	}
	return false
}

func (p *parser) parseMul() (Node, error) {
	left, err := p.parsePower()
	if err != nil {
		return nil, err
	}
	for {
		op := ""
		switch p.lex.lookahead().kind {
		case tStar:
			op = "*"
			p.lex.next()
		case tSlash:
			op = "/"
			p.lex.next()
		default:
			if p.acceptKeyword("div") {
				op = "div"
			} else if p.acceptKeyword("mod") {
				op = "mod"
			} else {
				return left, nil
			}
		}
		right, err := p.parsePower()
		if err != nil {
			return nil, err
		}
		left = &binaryNode{nodeBase: nodeBase{src: p.src}, op: op, left: left, right: right}
	}
}

func (p *parser) parsePower() (Node, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	if !p.acceptKind(tCaret) {
		return left, nil
	}
	right, err := p.parsePower()
	if err != nil {
		return nil, err
	}
	return &binaryNode{nodeBase: nodeBase{src: p.src}, op: "^", left: left, right: right}, nil
}

func (p *parser) parseUnary() (Node, error) {
	if p.acceptKind(tMinus) {
		x, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &unaryNode{nodeBase: nodeBase{src: p.src}, op: "-", x: x}, nil
	}
	if p.acceptKind(tPlus) {
		return p.parseUnary()
	}
	if p.acceptKeyword("singleton") {
		_ = p.acceptKeyword("from")
		x, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &unaryNode{nodeBase: nodeBase{src: p.src}, op: "singleton", x: x}, nil
	}
	if p.acceptKeyword("flatten") {
		x, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &unaryNode{nodeBase: nodeBase{src: p.src}, op: "flatten", x: x}, nil
	}
	if p.peekKeywordPair("point", "from") {
		p.lex.next()
		p.lex.next()
		x, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &unaryNode{nodeBase: nodeBase{src: p.src}, op: "point from", x: x}, nil
	}
	if p.peekKeywordPair("precision", "of") {
		p.lex.next()
		p.lex.next()
		x, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &unaryNode{nodeBase: nodeBase{src: p.src}, op: "precision", x: x}, nil
	}
	if p.peekKeywordPair("high", "boundary") || p.peekKeywordPair("low", "boundary") {
		op := "highboundary"
		if keywordEq(p.lex.lookahead().text, "low") {
			op = "lowboundary"
		}
		p.lex.next()
		p.lex.next()
		var prec Node
		if p.lex.lookahead().kind == tNumber {
			t := p.lex.next()
			prec = &litNode{nodeBase: nodeBase{src: p.src}, value: parseNumber(t.text)}
		}
		if !p.acceptKeyword("of") {
			return nil, parseError(p.src, p.lex.lookahead().pos, "expected 'of' after boundary")
		}
		x, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		args := []Node{x}
		if prec != nil {
			args = append(args, prec)
		}
		return &callNode{nodeBase: nodeBase{src: p.src}, callee: &identNode{name: op}, args: args}, nil
	}
	if p.acceptKeyword("minimum") || p.acceptKeyword("maximum") {
		name := "minvalue"
		if keywordEq(p.lex.last.text, "maximum") {
			name = "maxvalue"
		}
		typ, err := p.requireIdent("type name")
		if err != nil {
			return nil, err
		}
		return &callNode{nodeBase: nodeBase{src: p.src}, callee: &identNode{name: name}, args: []Node{&litNode{nodeBase: nodeBase{src: p.src}, value: typ}}}, nil
	}
	if startOf, op := p.peekStartEndWidth(); startOf {
		p.lex.next()
		_ = p.acceptKeyword("of")
		x, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &unaryNode{nodeBase: nodeBase{src: p.src}, op: op, x: x}, nil
	}
	if p.acceptKeyword("convert") {
		x, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		if !p.acceptKeyword("to") {
			return nil, parseError(p.src, p.lex.lookahead().pos, "expected 'to' after convert")
		}
		target, err := p.requireName("convert type")
		if err != nil {
			return nil, err
		}
		return &convertNode{nodeBase: nodeBase{src: p.src}, x: x, target: target}, nil
	}
	if p.acceptKeyword("collapse") || p.acceptKeyword("expand") {
		op := strings.ToLower(p.lex.last.text)
		x, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		if p.acceptKeyword("per") {
			per, err := p.parseUnary()
			if err != nil {
				return nil, err
			}
			return &callNode{nodeBase: nodeBase{src: p.src}, callee: &identNode{name: op}, args: []Node{x, per}}, nil
		}
		return &unaryNode{nodeBase: nodeBase{src: p.src}, op: op, x: x}, nil
	}
	if unit, ok := p.peekExtractorFrom(); ok {
		p.lex.next()
		if !p.acceptKeyword("from") {
			return nil, parseError(p.src, p.lex.lookahead().pos, "expected from")
		}
		x, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &unaryNode{nodeBase: nodeBase{src: p.src}, op: unit + " from", x: x}, nil
	}
	return p.parsePostfix()
}

func (p *parser) peekKeywordPair(first, second string) bool {
	t := p.lex.lookahead()
	if t.kind != tIdent || !keywordEq(t.text, first) {
		return false
	}
	rest := p.src[t.pos:]
	lx := newLexer(rest)
	_ = lx.next()
	next := lx.next()
	return next.kind == tIdent && keywordEq(next.text, second)
}

func (p *parser) peekExtractorFrom() (string, bool) {
	t := p.lex.lookahead()
	if t.kind != tIdent {
		return "", false
	}
	name := strings.ToLower(t.text)
	switch name {
	case "date", "time", "timezoneoffset", "timezone",
		"year", "month", "week", "day", "hour", "minute", "second", "millisecond":
	default:
		return "", false
	}
	rest := p.src[t.pos:]
	lx := newLexer(rest)
	_ = lx.next()
	next := lx.next()
	if next.kind == tIdent && keywordEq(next.text, "from") {
		if name == "timezone" {
			name = "timezoneoffset"
		}
		return name, true
	}
	return "", false
}

func (p *parser) peekStartEndWidth() (bool, string) {
	t := p.lex.lookahead()
	if t.kind != tIdent {
		return false, ""
	}
	op := strings.ToLower(t.text)
	if op != "start" && op != "end" && op != "width" && op != "successor" && op != "predecessor" {
		return false, ""
	}
	rest := p.src[t.pos:]
	lx := newLexer(rest)
	_ = lx.next()
	next := lx.next()
	if next.kind == tIdent && keywordEq(next.text, "of") {
		return true, op
	}
	return false, ""
}

func (p *parser) parsePostfix() (Node, error) {
	n, err := p.parsePrimary()
	if err != nil {
		return nil, err
	}
	n = p.parseQuantitySuffix(n)
	for {
		switch p.lex.lookahead().kind {
		case tDot:
			p.lex.next()
			name, err := p.requireName("member name")
			if err != nil {
				return nil, err
			}
			n = &memberNode{nodeBase: nodeBase{src: p.src}, x: n, name: name}
			if p.acceptKind(tLParen) {
				args, err := p.parseArgList()
				if err != nil {
					return nil, err
				}
				n = &callNode{nodeBase: nodeBase{src: p.src}, callee: n, args: args}
			}
		case tLParen:
			p.lex.next()
			args, err := p.parseArgList()
			if err != nil {
				return nil, err
			}
			n = &callNode{nodeBase: nodeBase{src: p.src}, callee: n, args: args}
		case tLBrack:
			p.lex.next()
			idx, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			if !p.acceptKind(tRBrack) {
				return nil, parseError(p.src, p.lex.lookahead().pos, "expected ']' after indexer")
			}
			n = &indexNode{nodeBase: nodeBase{src: p.src}, x: n, index: idx}
		default:
			if lit, ok := n.(*litNode); ok {
				if s, ok := lit.value.(string); ok && p.acceptKeyword("from") {
					sys, err := p.requireName("codesystem")
					if err != nil {
						return nil, err
					}
					n = &codeLitNode{nodeBase: nodeBase{src: p.src}, code: s, system: sys}
					continue
				}
			}
			return n, nil
		}
	}
}

func (p *parser) parseArgList() ([]Node, error) {
	if p.acceptKind(tRParen) {
		return nil, nil
	}
	var args []Node
	for {
		arg, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		args = append(args, arg)
		if p.acceptKind(tComma) {
			continue
		}
		if !p.acceptKind(tRParen) {
			return nil, parseError(p.src, p.lex.lookahead().pos, "expected ')' after arguments")
		}
		return args, nil
	}
}

func (p *parser) parsePrimary() (Node, error) {
	t := p.lex.lookahead()
	switch t.kind {
	case tIdent:
		if keywordEq(t.text, "true") {
			p.lex.next()
			return &litNode{nodeBase: nodeBase{src: t.text}, value: true}, nil
		}
		if keywordEq(t.text, "false") {
			p.lex.next()
			return &litNode{nodeBase: nodeBase{src: t.text}, value: false}, nil
		}
		if keywordEq(t.text, "null") {
			p.lex.next()
			return &litNode{nodeBase: nodeBase{src: t.text}, value: nil}, nil
		}
		if keywordEq(t.text, "if") {
			return p.parseIf()
		}
		if keywordEq(t.text, "from") {
			p.lex.next()
			return p.parseFromQuery()
		}
		if keywordEq(t.text, "Interval") {
			return p.parseInterval()
		}
		if keywordEq(t.text, "case") {
			return p.parseCase()
		}
		if keywordEq(t.text, "duration") || keywordEq(t.text, "difference") {
			return p.parseDuration(keywordEq(t.text, "difference"))
		}
		if unit := timeUnitName(t.text); unit != "" && p.looksLikeUnitBetween() {
			return p.parseUnitBetween()
		}
		p.lex.next()
		return &identNode{nodeBase: nodeBase{src: t.text}, name: t.text}, nil
	case tQuotedIdent:
		p.lex.next()
		return &identNode{nodeBase: nodeBase{src: t.text}, name: t.text, quoted: true}, nil
	case tString:
		p.lex.next()
		return &litNode{nodeBase: nodeBase{src: t.text}, value: t.text}, nil
	case tNumber:
		p.lex.next()
		return &litNode{nodeBase: nodeBase{src: t.text}, value: parseNumber(t.text)}, nil
	case tDate:
		p.lex.next()
		tm, err := parseCQLDate(t.text)
		if err != nil {
			return nil, parseError(p.src, t.pos, "%s", err.Error())
		}
		return &litNode{nodeBase: nodeBase{src: t.text}, value: tm}, nil
	case tLParen:
		p.lex.next()
		n, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if !p.acceptKind(tRParen) {
			return nil, parseError(p.src, p.lex.lookahead().pos, "expected ')'")
		}
		return n, nil
	case tLBrack:
		return p.parseRetrieve()
	case tLBrace:
		return p.parseBrace()
	}
	return nil, parseError(p.src, t.pos, "expected expression, got %q", t.text)
}

func (p *parser) parseIf() (Node, error) {
	p.lex.next() // if
	cond, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if !p.acceptKeyword("then") {
		return nil, parseError(p.src, p.lex.lookahead().pos, "expected then")
	}
	thenN, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if !p.acceptKeyword("else") {
		return nil, parseError(p.src, p.lex.lookahead().pos, "expected else")
	}
	elseN, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	return &ifNode{nodeBase: nodeBase{src: p.src}, cond: cond, thenN: thenN, elseN: elseN}, nil
}

func (p *parser) parseRetrieve() (Node, error) {
	p.lex.next() // [
	name, err := p.requireIdent("retrieve type")
	if err != nil {
		return nil, err
	}
	term := ""
	comp := ""
	codePath := ""
	if p.acceptKind(tColon) {
		t := p.lex.lookahead()
		if t.kind == tIdent || t.kind == tQuotedIdent {
			rest := p.src[t.pos:]
			lx := newLexer(rest)
			_ = lx.next()
			next := lx.next()
			switch {
			case next.kind == tIdent && keywordEq(next.text, "in"):
				codePath = p.lex.next().text
				p.acceptKeyword("in")
				comp = "in"
			case next.kind == tEq:
				codePath = p.lex.next().text
				p.lex.next()
				comp = "="
			case next.kind == tTilde:
				codePath = p.lex.next().text
				p.lex.next()
				comp = "~"
			}
		}
		switch p.lex.lookahead().kind {
		case tString:
			term = p.lex.next().text
		case tQuotedIdent, tIdent:
			term = p.lex.next().text
		default:
			return nil, parseError(p.src, p.lex.lookahead().pos, "expected terminology in retrieve")
		}
	}
	if !p.acceptKind(tRBrack) {
		return nil, parseError(p.src, p.lex.lookahead().pos, "expected ']' after retrieve")
	}
	return &retrieveNode{nodeBase: nodeBase{src: name}, resourceType: name, terminology: term, comparator: comp, codePath: codePath, dateLowClosed: true, dateHighClosed: true}, nil
}

func (p *parser) parseQuantitySuffix(n Node) Node {
	lit, ok := n.(*litNode)
	if !ok {
		return n
	}
	if _, isNum := asParseFloat(lit.value); !isNum {
		return n
	}
	t := p.lex.lookahead()
	unit := ""
	switch t.kind {
	case tString:
		p.lex.next()
		unit = t.text
	case tIdent:
		if u := timeUnitName(t.text); u != "" {
			p.lex.next()
			unit = u
		}
	}
	if unit == "" {
		return n
	}
	return &quantityNode{nodeBase: nodeBase{src: p.src}, value: lit.value, unit: unit}
}

func asParseFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case float64:
		return x, true
	}
	return 0, false
}

func timeUnitName(text string) string {
	switch strings.ToLower(text) {
	case "year", "years":
		return "year"
	case "month", "months":
		return "month"
	case "week", "weeks":
		return "week"
	case "day", "days":
		return "day"
	case "hour", "hours":
		return "hour"
	case "minute", "minutes":
		return "minute"
	case "second", "seconds":
		return "second"
	case "millisecond", "milliseconds":
		return "millisecond"
	}
	return ""
}

func (p *parser) looksLikeUnitBetween() bool {
	t := p.lex.lookahead()
	rest := p.src[t.pos:]
	lx := newLexer(rest)
	_ = lx.next() // unit
	next := lx.next()
	return next.kind == tIdent && keywordEq(next.text, "between")
}

func (p *parser) parseUnitBetween() (Node, error) {
	unitTok := p.lex.next()
	unit := timeUnitName(unitTok.text)
	if !p.acceptKeyword("between") {
		return nil, parseError(p.src, p.lex.lookahead().pos, "expected between")
	}
	left, err := p.parseComparison()
	if err != nil {
		return nil, err
	}
	if !p.acceptKeyword("and") {
		return nil, parseError(p.src, p.lex.lookahead().pos, "expected 'and' after between")
	}
	right, err := p.parseComparison()
	if err != nil {
		return nil, err
	}
	return &durationNode{nodeBase: nodeBase{src: p.src}, unit: unit, left: left, right: right}, nil
}

func (p *parser) parseDuration(difference bool) (Node, error) {
	p.lex.next() // duration | difference
	if !p.acceptKeyword("in") {
		return nil, parseError(p.src, p.lex.lookahead().pos, "expected 'in' after duration")
	}
	unitTok := p.lex.lookahead()
	unit := timeUnitName(unitTok.text)
	if unit == "" {
		name, err := p.requireName("duration unit")
		if err != nil {
			return nil, err
		}
		unit = name
	} else {
		p.lex.next()
	}
	n := &durationNode{nodeBase: nodeBase{src: p.src}, unit: unit, difference: difference}
	if p.acceptKeyword("of") {
		x, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		n.of = x
		return n, nil
	}
	if p.acceptKeyword("between") {
		left, err := p.parseComparison()
		if err != nil {
			return nil, err
		}
		if !p.acceptKeyword("and") {
			return nil, parseError(p.src, p.lex.lookahead().pos, "expected 'and' after between")
		}
		right, err := p.parseComparison()
		if err != nil {
			return nil, err
		}
		n.left, n.right = left, right
		return n, nil
	}
	return nil, parseError(p.src, p.lex.lookahead().pos, "expected 'of' or 'between' after duration in %s", unit)
}

func (p *parser) parseInterval() (Node, error) {
	p.lex.next() // Interval
	lowClosed := false
	switch p.lex.lookahead().kind {
	case tLBrack:
		p.lex.next()
		lowClosed = true
	case tLParen:
		p.lex.next()
		lowClosed = false
	default:
		return nil, parseError(p.src, p.lex.lookahead().pos, "expected '[' or '(' after Interval")
	}
	low, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if !p.acceptKind(tComma) {
		return nil, parseError(p.src, p.lex.lookahead().pos, "expected ',' in Interval")
	}
	high, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	highClosed := false
	switch p.lex.lookahead().kind {
	case tRBrack:
		p.lex.next()
		highClosed = true
	case tRParen:
		p.lex.next()
		highClosed = false
	default:
		return nil, parseError(p.src, p.lex.lookahead().pos, "expected ']' or ')' after Interval")
	}
	return &intervalNode{nodeBase: nodeBase{src: p.src}, low: low, high: high, lowClosed: lowClosed, highClosed: highClosed}, nil
}

func (p *parser) parseCase() (Node, error) {
	p.lex.next() // case
	n := &caseNode{nodeBase: nodeBase{src: p.src}}
	if !keywordEq(p.lex.lookahead().text, "when") {
		test, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		n.test = test
	}
	for p.acceptKeyword("when") {
		when, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if !p.acceptKeyword("then") {
			return nil, parseError(p.src, p.lex.lookahead().pos, "expected then")
		}
		thenN, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		n.whens = append(n.whens, caseWhen{when: when, then: thenN})
	}
	if p.acceptKeyword("else") {
		elseN, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		n.elseN = elseN
	}
	if !p.acceptKeyword("end") {
		return nil, parseError(p.src, p.lex.lookahead().pos, "expected end")
	}
	return n, nil
}

func (p *parser) parseFromQuery() (Node, error) {
	src, err := p.parseAliasedSource()
	if err != nil {
		return nil, err
	}
	return p.parseQuery(src)
}

func (p *parser) parseAliasedSource() (querySource, error) {
	expr, err := p.parseQuerySourceExpr()
	if err != nil {
		return querySource{}, err
	}
	src := querySource{expr: expr}
	t := p.lex.lookahead()
	if t.kind == tQuotedIdent || (t.kind == tIdent && !isReservedAlias(t.text)) {
		p.lex.next()
		src.alias = t.text
	}
	return src, nil
}

func (p *parser) parseQuerySourceExpr() (Node, error) {
	t := p.lex.lookahead()
	if t.kind == tLBrack {
		return p.parseRetrieve()
	}
	if t.kind == tLParen {
		p.lex.next()
		n, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if !p.acceptKind(tRParen) {
			return nil, parseError(p.src, p.lex.lookahead().pos, "expected ')'")
		}
		return n, nil
	}
	return p.parsePostfix()
}

func (p *parser) parseQuery(first querySource) (Node, error) {
	q := &queryNode{nodeBase: nodeBase{src: p.src}, sources: []querySource{first}}
	for p.acceptKind(tComma) {
		src, err := p.parseAliasedSource()
		if err != nil {
			return nil, err
		}
		q.sources = append(q.sources, src)
	}
	for p.acceptKeyword("let") {
		name, err := p.requireName("let name")
		if err != nil {
			return nil, err
		}
		if !p.acceptKind(tColon) {
			return nil, parseError(p.src, p.lex.lookahead().pos, "expected ':' after let %s", name)
		}
		expr, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		q.lets = append(q.lets, letClause{name: name, expr: expr})
	}
	for {
		without := false
		if p.acceptKeyword("with") {
			without = false
		} else if p.acceptKeyword("without") {
			without = true
		} else {
			break
		}
		rel, err := p.parseRelated(without)
		if err != nil {
			return nil, err
		}
		q.related = append(q.related, rel)
	}
	if p.acceptKeyword("where") {
		where, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		q.where = where
	}
	if p.acceptKeyword("return") {
		if p.acceptKeyword("distinct") {
			q.distinct = true
		} else {
			_ = p.acceptKeyword("all")
		}
		ret, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		q.ret = ret
	}
	if p.acceptKeyword("aggregate") {
		if q.ret != nil {
			return nil, parseError(p.src, p.lex.last.pos, "CQL query cannot have both return and aggregate")
		}
		agg := &aggregateNode{nodeBase: nodeBase{src: p.src}}
		if p.acceptKeyword("distinct") {
			agg.distinct = true
		} else {
			_ = p.acceptKeyword("all")
		}
		name, err := p.requireName("aggregate name")
		if err != nil {
			return nil, err
		}
		agg.name = name
		if p.acceptKeyword("starting") {
			start, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			agg.starting = start
		}
		if !p.acceptKind(tColon) {
			return nil, parseError(p.src, p.lex.lookahead().pos, "expected ':' after aggregate")
		}
		body, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		agg.body = body
		q.agg = agg
	}
	if p.acceptKeyword("sort") {
		_ = p.acceptKeyword("by")
		for {
			expr, err := p.parseComparison()
			if err != nil {
				return nil, err
			}
			item := sortItem{expr: expr}
			if p.acceptKeyword("desc") {
				item.desc = true
			} else {
				_ = p.acceptKeyword("asc")
			}
			q.sort = append(q.sort, item)
			if !p.acceptKind(tComma) {
				break
			}
		}
	}
	return q, nil
}

func (p *parser) parseRelated(without bool) (relatedClause, error) {
	src, err := p.parseAliasedSource()
	if err != nil {
		return relatedClause{}, err
	}
	if !p.acceptKeyword("such") || !p.acceptKeyword("that") {
		return relatedClause{}, parseError(p.src, p.lex.lookahead().pos, "expected 'such that'")
	}
	such, err := p.parseExpr()
	if err != nil {
		return relatedClause{}, err
	}
	return relatedClause{source: src, such: such, without: without}, nil
}

func (p *parser) parseBrace() (Node, error) {
	p.lex.next() // {
	if p.acceptKind(tRBrace) {
		return &listNode{nodeBase: nodeBase{src: p.src}}, nil
	}
	// Tuple { name: expr } vs list { expr, expr }
	firstName, isTuple := p.peekTupleField()
	if isTuple {
		var fields []tupleField
		name := firstName
		p.lex.next()
		if p.lex.lookahead().kind == tQuotedIdent || p.lex.lookahead().kind == tIdent {
			p.lex.next()
		}
		if !p.acceptKind(tColon) {
			return nil, parseError(p.src, p.lex.lookahead().pos, "expected ':' in tuple")
		}
		val, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		fields = append(fields, tupleField{name: name, value: val})
		for p.acceptKind(tComma) {
			fname, err := p.requireName("tuple field")
			if err != nil {
				return nil, err
			}
			if !p.acceptKind(tColon) {
				return nil, parseError(p.src, p.lex.lookahead().pos, "expected ':' in tuple")
			}
			fval, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			fields = append(fields, tupleField{name: fname, value: fval})
		}
		if !p.acceptKind(tRBrace) {
			return nil, parseError(p.src, p.lex.lookahead().pos, "expected '}'")
		}
		return &tupleNode{nodeBase: nodeBase{src: p.src}, fields: fields}, nil
	}
	var elems []Node
	for {
		el, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		elems = append(elems, el)
		if p.acceptKind(tComma) {
			continue
		}
		if !p.acceptKind(tRBrace) {
			return nil, parseError(p.src, p.lex.lookahead().pos, "expected '}'")
		}
		return &listNode{nodeBase: nodeBase{src: p.src}, elems: elems}, nil
	}
}

func (p *parser) peekTupleField() (string, bool) {
	t := p.lex.lookahead()
	if t.kind != tIdent && t.kind != tQuotedIdent {
		return "", false
	}
	// Look at the following token without consuming the ident. The lexer only
	// has one-token lookahead, so parse a tiny nested lexer from remaining src.
	rest := p.src[t.pos:]
	lx := newLexer(rest)
	_ = lx.next() // name
	next := lx.next()
	return t.text, next.kind == tColon
}

func (p *parser) acceptKeyword(kw string) bool {
	t := p.lex.lookahead()
	if t.kind == tIdent && keywordEq(t.text, kw) {
		p.lex.next()
		return true
	}
	return false
}

func (p *parser) acceptKind(k kind) bool {
	if p.lex.lookahead().kind == k {
		p.lex.next()
		return true
	}
	return false
}

func (p *parser) requireName(what string) (string, error) {
	t := p.lex.lookahead()
	if t.kind == tIdent || t.kind == tQuotedIdent {
		p.lex.next()
		return t.text, nil
	}
	return "", parseError(p.src, t.pos, "expected %s", what)
}

func (p *parser) requireIdent(what string) (string, error) {
	t := p.lex.lookahead()
	if t.kind == tIdent {
		p.lex.next()
		return t.text, nil
	}
	if t.kind == tQuotedIdent {
		p.lex.next()
		return t.text, nil
	}
	return "", parseError(p.src, t.pos, "expected %s", what)
}

func (p *parser) requireString(what string) (string, error) {
	t := p.lex.lookahead()
	if t.kind == tString {
		p.lex.next()
		return t.text, nil
	}
	return "", parseError(p.src, t.pos, "expected %s string", what)
}

func parseNumber(text string) any {
	if strings.ContainsAny(text, ".eE") {
		f, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return text
		}
		return f
	}
	n, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		f, ferr := strconv.ParseFloat(text, 64)
		if ferr != nil {
			return text
		}
		return f
	}
	return n
}

var dateOnlyLoc = time.FixedZone("CQL-DATE", 0)
var naiveDateTimeLoc = time.FixedZone("CQL-DATETIME", 0)
var timeOnlyLoc = time.FixedZone("CQL-TIME", 0)

func isDateOnlyString(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" || strings.ContainsAny(text, "Tt") {
		return false
	}
	switch len(text) {
	case 4, 7, 10:
		return true
	}
	return false
}

func isNaiveDateTimeString(text string) bool {
	text = strings.TrimSpace(text)
	i := strings.IndexAny(text, "Tt")
	if i < 0 {
		return false
	}
	rest := text[i+1:]
	if strings.ContainsAny(rest, "Zz") {
		return false
	}
	for j, r := range rest {
		if r == '+' {
			return false
		}
		if r == '-' && j >= 2 {
			return false
		}
	}
	return true
}

func isDateOnlyTime(t time.Time) bool {
	if t.Location() == nil {
		return false
	}
	return t.Location() == dateOnlyLoc || t.Location().String() == "CQL-DATE"
}

func isTimeOnlyTime(t time.Time) bool {
	if t.Location() == nil {
		return false
	}
	return t.Location().String() == "CQL-TIME"
}

func isNaiveDateTimeTime(t time.Time) bool {
	if t.Location() == nil {
		return false
	}
	return t.Location() == naiveDateTimeLoc || t.Location().String() == "CQL-DATETIME"
}

func isFloatingTime(t time.Time) bool {
	return isDateOnlyTime(t) || isNaiveDateTimeTime(t)
}

func parseCQLDate(text string) (time.Time, error) {
	text = strings.TrimSpace(text)
	formats := []string{
		time.RFC3339,
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02T15:04:05",
		"2006-01-02",
		"2006-01",
		"2006",
	}
	for _, layout := range formats {
		if tm, err := time.Parse(layout, text); err == nil {
			if isDateOnlyString(text) {
				return time.Date(tm.Year(), tm.Month(), tm.Day(), 0, 0, 0, 0, dateOnlyLoc), nil
			}
			if isNaiveDateTimeString(text) {
				return time.Date(tm.Year(), tm.Month(), tm.Day(), tm.Hour(), tm.Minute(), tm.Second(), tm.Nanosecond(), naiveDateTimeLoc), nil
			}
			return tm, nil
		}
	}
	return time.Time{}, errf("invalid CQL date @%s", text)
}

func isSimpleIdentifier(src string) bool {
	src = strings.TrimSpace(src)
	if src == "" {
		return false
	}
	p := &parser{lex: newLexer(src), src: src}
	t := p.lex.next()
	if t.kind != tIdent && t.kind != tQuotedIdent {
		return false
	}
	return p.lex.lookahead().kind == tEOF
}

func identifierName(src string) string {
	src = strings.TrimSpace(src)
	p := &parser{lex: newLexer(src), src: src}
	t := p.lex.next()
	return t.text
}

func defineNameFromExpression(src string) string {
	src = strings.TrimSpace(src)
	if src == "" {
		return src
	}
	if isSimpleIdentifier(src) {
		return identifierName(src)
	}
	if len(src) >= 2 && src[0] == '"' && src[len(src)-1] == '"' {
		inner := src[1 : len(src)-1]
		return strings.ReplaceAll(inner, `""`, `"`)
	}
	return src
}
