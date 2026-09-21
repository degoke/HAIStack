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
			if err := p.skipNamedDeclaration(); err != nil {
				return nil, err
			}
		case p.acceptKeyword("valueset"):
			if err := p.skipNamedDeclaration(); err != nil {
				return nil, err
			}
		case p.acceptKeyword("code"):
			if err := p.skipNamedDeclaration(); err != nil {
				return nil, err
			}
		case p.acceptKeyword("concept"):
			if err := p.skipNamedDeclaration(); err != nil {
				return nil, err
			}
		case p.acceptKeyword("parameter"):
			if err := p.skipParameter(); err != nil {
				return nil, err
			}
		case p.acceptKeyword("context"):
			ctxName, err := p.requireIdent("context")
			if err != nil {
				return nil, err
			}
			lib.Context = ctxName
		case p.acceptKeyword("define"):
			def, err := p.parseDefine()
			if err != nil {
				return nil, err
			}
			lib.Defines = append(lib.Defines, def)
		default:
			if p.lex.lookahead().kind == tEOF {
				if len(lib.Defines) == 0 {
					return nil, parseError(src, p.lex.lookahead().pos, "library has no define statements")
				}
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
	n, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if p.lex.lookahead().kind != tEOF {
		return nil, parseError(src, p.lex.lookahead().pos, "unexpected token %q after expression", p.lex.lookahead().text)
	}
	return n, nil
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

func (p *parser) parseDefine() (Define, error) {
	access := "public"
	if p.acceptKeyword("public") || p.acceptKeyword("private") || p.acceptKeyword("fluent") {
		access = strings.ToLower(p.lex.last.text)
		if access == "fluent" {
			p.acceptKeyword("public")
			p.acceptKeyword("private")
		}
	}
	if p.acceptKeyword("function") {
		name, _ := p.requireName("function")
		return Define{}, errf("%w: define function %q", ErrUnsupported, name)
	}
	name, err := p.requireName("define name")
	if err != nil {
		return Define{}, err
	}
	if !p.acceptKind(tColon) {
		return Define{}, parseError(p.src, p.lex.lookahead().pos, "expected ':' after define %s", name)
	}
	start := p.lex.lookahead().pos
	expr, err := p.parseExpr()
	if err != nil {
		return Define{}, err
	}
	end := p.lex.lookahead().pos
	if end < start {
		end = start
	}
	if end > len(p.src) {
		end = len(p.src)
	}
	return Define{Name: name, Access: access, Expression: expr, Source: strings.TrimSpace(p.src[start:end])}, nil
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

func (p *parser) skipParameter() error {
	if _, err := p.requireName("parameter name"); err != nil {
		return err
	}
	// Optional type: Ident or Ident<Ident>
	if p.lex.lookahead().kind == tIdent && !isExprStartKeyword(p.lex.lookahead().text) {
		p.lex.next()
		if p.acceptKind(tLt) {
			if _, err := p.requireIdent("type argument"); err != nil {
				return err
			}
			if !p.acceptKind(tGt) {
				return parseError(p.src, p.lex.lookahead().pos, "expected '>' after type argument")
			}
		}
	}
	if p.acceptKeyword("default") {
		if _, err := p.parseExpr(); err != nil {
			return err
		}
	}
	return nil
}

func isExprStartKeyword(text string) bool {
	switch strings.ToLower(text) {
	case "library", "using", "include", "codesystem", "valueset", "code", "concept", "parameter", "context", "define", "and", "or", "not":
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
	if p.acceptKeyword("not") {
		x, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		return &unaryNode{nodeBase: nodeBase{src: p.src}, op: "not", x: x}, nil
	}
	if p.acceptKeyword("exists") {
		x, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		return &unaryNode{nodeBase: nodeBase{src: p.src}, op: "exists", x: x}, nil
	}
	return p.parseIn()
}

func (p *parser) parseIn() (Node, error) {
	left, err := p.parseComparison()
	if err != nil {
		return nil, err
	}
	for {
		op := ""
		if p.acceptKeyword("in") {
			op = "in"
		} else if p.acceptKeyword("contains") {
			op = "contains"
		} else {
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
	left, err := p.parseMul()
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
		default:
			if p.acceptKeyword("union") {
				op = "|"
			} else {
				return left, nil
			}
		}
		right, err := p.parseMul()
		if err != nil {
			return nil, err
		}
		left = &binaryNode{nodeBase: nodeBase{src: p.src}, op: op, left: left, right: right}
	}
}

func (p *parser) parseMul() (Node, error) {
	left, err := p.parseUnary()
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
		right, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		left = &binaryNode{nodeBase: nodeBase{src: p.src}, op: op, left: left, right: right}
	}
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
	return p.parsePostfix()
}

func (p *parser) parsePostfix() (Node, error) {
	n, err := p.parsePrimary()
	if err != nil {
		return nil, err
	}
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
		default:
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
				if _, err := p.requireName("type name"); err != nil {
					return nil, err
				}
				// as Type is treated as identity in this subset.
				continue
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
	if p.acceptKind(tColon) {
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
	return &retrieveNode{nodeBase: nodeBase{src: name}, resourceType: name, terminology: term}, nil
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

func parseCQLDate(text string) (time.Time, error) {
	text = strings.TrimSpace(text)
	formats := []string{
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02",
		"2006-01",
		"2006",
	}
	for _, layout := range formats {
		if tm, err := time.Parse(layout, text); err == nil {
			return tm.UTC(), nil
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
