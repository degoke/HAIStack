package cql

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

type kind int

const (
	tEOF kind = iota
	tIdent
	tQuotedIdent
	tString
	tNumber
	tDate
	tLParen
	tRParen
	tLBrack
	tRBrack
	tLBrace
	tRBrace
	tDot
	tComma
	tColon
	tPipe
	tEq
	tNeq
	tLt
	tGt
	tLe
	tGe
	tTilde
	tNTilde
	tPlus
	tMinus
	tStar
	tSlash
	tCaret
	tAmp
)

type token struct {
	kind kind
	text string
	pos  int
}

type lexer struct {
	src  string
	pos  int
	last token
	peek *token
}

func newLexer(src string) *lexer {
	return &lexer{src: src}
}

func (l *lexer) next() token {
	if l.peek != nil {
		t := *l.peek
		l.peek = nil
		l.last = t
		return t
	}
	l.last = l.scan()
	return l.last
}

func (l *lexer) lookahead() token {
	if l.peek != nil {
		return *l.peek
	}
	t := l.scan()
	l.peek = &t
	return t
}

func (l *lexer) checkpoint() int {
	return l.pos
}

func (l *lexer) rewind(pos int) {
	l.pos = pos
	l.peek = nil
}

func (l *lexer) scan() token {
	l.skipIgnored()
	if l.pos >= len(l.src) {
		return token{kind: tEOF, pos: l.pos}
	}
	pos := l.pos
	r, w := utf8.DecodeRuneInString(l.src[l.pos:])
	switch r {
	case '(':
		l.pos += w
		return token{kind: tLParen, text: "(", pos: pos}
	case ')':
		l.pos += w
		return token{kind: tRParen, text: ")", pos: pos}
	case '[':
		l.pos += w
		return token{kind: tLBrack, text: "[", pos: pos}
	case ']':
		l.pos += w
		return token{kind: tRBrack, text: "]", pos: pos}
	case '{':
		l.pos += w
		return token{kind: tLBrace, text: "{", pos: pos}
	case '}':
		l.pos += w
		return token{kind: tRBrace, text: "}", pos: pos}
	case '.':
		l.pos += w
		return token{kind: tDot, text: ".", pos: pos}
	case ',':
		l.pos += w
		return token{kind: tComma, text: ",", pos: pos}
	case ':':
		l.pos += w
		return token{kind: tColon, text: ":", pos: pos}
	case '|':
		l.pos += w
		return token{kind: tPipe, text: "|", pos: pos}
	case '+':
		l.pos += w
		return token{kind: tPlus, text: "+", pos: pos}
	case '-':
		l.pos += w
		return token{kind: tMinus, text: "-", pos: pos}
	case '*':
		l.pos += w
		return token{kind: tStar, text: "*", pos: pos}
	case '/':
		l.pos += w
		return token{kind: tSlash, text: "/", pos: pos}
	case '^':
		l.pos += w
		return token{kind: tCaret, text: "^", pos: pos}
	case '&':
		l.pos += w
		return token{kind: tAmp, text: "&", pos: pos}
	case '=':
		l.pos += w
		return token{kind: tEq, text: "=", pos: pos}
	case '~':
		l.pos += w
		return token{kind: tTilde, text: "~", pos: pos}
	case '<':
		l.pos += w
		if l.consume('=') {
			return token{kind: tLe, text: "<=", pos: pos}
		}
		return token{kind: tLt, text: "<", pos: pos}
	case '>':
		l.pos += w
		if l.consume('=') {
			return token{kind: tGe, text: ">=", pos: pos}
		}
		return token{kind: tGt, text: ">", pos: pos}
	case '!':
		l.pos += w
		if l.consume('=') {
			return token{kind: tNeq, text: "!=", pos: pos}
		}
		if l.consume('~') {
			return token{kind: tNTilde, text: "!~", pos: pos}
		}
		return token{kind: tIdent, text: "!", pos: pos}
	case '\'':
		return l.scanString(pos)
	case '"':
		return l.scanQuotedIdent(pos)
	case '@':
		return l.scanDate(pos)
	}
	if unicode.IsDigit(r) {
		return l.scanNumber(pos)
	}
	if isIdentStart(r) {
		return l.scanIdent(pos)
	}
	l.pos += w
	return token{kind: tIdent, text: string(r), pos: pos}
}

func (l *lexer) skipIgnored() {
	for l.pos < len(l.src) {
		r, w := utf8.DecodeRuneInString(l.src[l.pos:])
		if unicode.IsSpace(r) {
			l.pos += w
			continue
		}
		if r == '/' && l.pos+1 < len(l.src) {
			switch l.src[l.pos+1] {
			case '/':
				l.pos += 2
				for l.pos < len(l.src) && l.src[l.pos] != '\n' {
					l.pos++
				}
				continue
			case '*':
				l.pos += 2
				for l.pos+1 < len(l.src) && !(l.src[l.pos] == '*' && l.src[l.pos+1] == '/') {
					l.pos++
				}
				if l.pos+1 < len(l.src) {
					l.pos += 2
				}
				continue
			}
		}
		return
	}
}

func (l *lexer) consume(b byte) bool {
	if l.pos < len(l.src) && l.src[l.pos] == b {
		l.pos++
		return true
	}
	return false
}

func (l *lexer) scanIdent(pos int) token {
	i := pos
	for i < len(l.src) {
		r, w := utf8.DecodeRuneInString(l.src[i:])
		if !isIdentPart(r) {
			break
		}
		i += w
	}
	l.pos = i
	return token{kind: tIdent, text: l.src[pos:i], pos: pos}
}

func (l *lexer) scanNumber(pos int) token {
	i := pos
	for i < len(l.src) && l.src[i] >= '0' && l.src[i] <= '9' {
		i++
	}
	if i < len(l.src) && l.src[i] == '.' && i+1 < len(l.src) && l.src[i+1] >= '0' && l.src[i+1] <= '9' {
		i++
		for i < len(l.src) && l.src[i] >= '0' && l.src[i] <= '9' {
			i++
		}
	}
	if i < len(l.src) && (l.src[i] == 'e' || l.src[i] == 'E') {
		j := i + 1
		if j < len(l.src) && (l.src[j] == '+' || l.src[j] == '-') {
			j++
		}
		k := j
		for k < len(l.src) && l.src[k] >= '0' && l.src[k] <= '9' {
			k++
		}
		if k > j {
			i = k
		}
	}
	l.pos = i
	return token{kind: tNumber, text: l.src[pos:i], pos: pos}
}

func (l *lexer) scanString(pos int) token {
	l.pos++ // opening quote
	var b strings.Builder
	for l.pos < len(l.src) {
		if l.src[l.pos] == '\'' {
			if l.pos+1 < len(l.src) && l.src[l.pos+1] == '\'' {
				b.WriteByte('\'')
				l.pos += 2
				continue
			}
			l.pos++
			return token{kind: tString, text: b.String(), pos: pos}
		}
		b.WriteByte(l.src[l.pos])
		l.pos++
	}
	return token{kind: tString, text: b.String(), pos: pos}
}

func (l *lexer) scanQuotedIdent(pos int) token {
	l.pos++
	var b strings.Builder
	for l.pos < len(l.src) {
		if l.src[l.pos] == '"' {
			if l.pos+1 < len(l.src) && l.src[l.pos+1] == '"' {
				b.WriteByte('"')
				l.pos += 2
				continue
			}
			l.pos++
			return token{kind: tQuotedIdent, text: b.String(), pos: pos}
		}
		b.WriteByte(l.src[l.pos])
		l.pos++
	}
	return token{kind: tQuotedIdent, text: b.String(), pos: pos}
}

func (l *lexer) scanDate(pos int) token {
	l.pos++ // @
	i := l.pos
	for i < len(l.src) {
		c := l.src[i]
		if (c >= '0' && c <= '9') || c == '-' || c == ':' || c == 'T' || c == '+' || c == 'Z' || c == '.' {
			i++
			continue
		}
		break
	}
	text := l.src[l.pos:i]
	l.pos = i
	return token{kind: tDate, text: text, pos: pos}
}

func isIdentStart(r rune) bool {
	return unicode.IsLetter(r) || r == '_'
}

func isIdentPart(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
}

func keywordEq(text, kw string) bool {
	return strings.EqualFold(text, kw)
}

func parseError(src string, pos int, format string, args ...any) error {
	msg := fmt.Sprintf(format, args...)
	line, col := lineCol(src, pos)
	return errf("CQL parse error at line %d column %d: %s", line, col, msg)
}

func lineCol(src string, pos int) (int, int) {
	if pos < 0 {
		pos = 0
	}
	if pos > len(src) {
		pos = len(src)
	}
	line, col := 1, 1
	for i := 0; i < pos; i++ {
		if src[i] == '\n' {
			line++
			col = 1
			continue
		}
		col++
	}
	return line, col
}
