// Package filter parses and evaluates the tag filter expressions typed at the
// `f` prompt.
//
// The grammar is small on purpose, so it can be typed from memory:
//
//	expr    := or
//	or      := and ( ("||" | "or") and )*
//	and     := unary ( ("&&" | "and") unary | unary )*   // juxtaposition = AND
//	unary   := ("!" | "not") unary | primary
//	primary := "(" expr ")" | tag | quoted
//	tag     := any run of characters that is not an operator or a space
//	quoted  := '"' ... '"' | "'" ... "'"   // may contain spaces and operators
//
// Symbols and words are interchangeable, and two operands next to each other
// mean AND, so all of these are the same query:
//
//	LLM && !DB
//	LLM and not DB
//	LLM !DB
//	"LLM" && not "DB"
//
// A tag name is a bare word. It is matched against the global tag table
// case-insensitively; a name that is not in the table matches nothing and is
// reported by UnknownTags, so a typo shows up as a warning instead of a list
// that is mysteriously empty.
package filter

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
)

// Node is one node of a parsed expression.
type Node interface {
	// Eval reports whether the tag set satisfies the node.
	Eval(has func(string) bool) bool
	// String renders the node back to a normalised expression.
	String() string
}

// Tag is a single tag name test.
type Tag struct {
	Name string // as typed, without quotes
}

// Not is a negation.
type Not struct{ X Node }

// And is a conjunction (n-ary, so "a b c" needs no nesting).
type And struct{ Xs []Node }

// Or is a disjunction (n-ary for the same reason).
type Or struct{ Xs []Node }

// Eval implements Node.
func (t Tag) Eval(has func(string) bool) bool { return has(t.Name) }

// Eval implements Node.
func (n Not) Eval(has func(string) bool) bool { return !n.X.Eval(has) }

// Eval implements Node.
func (a And) Eval(has func(string) bool) bool {
	for _, x := range a.Xs {
		if !x.Eval(has) {
			return false
		}
	}
	return true
}

// Eval implements Node.
func (o Or) Eval(has func(string) bool) bool {
	for _, x := range o.Xs {
		if x.Eval(has) {
			return true
		}
	}
	return false
}

// String implements Node. A name that cannot be written bare is quoted with
// its quotes and backslashes escaped, so the rendered form always lexes back to
// the same tag.
func (t Tag) String() string {
	if isBare(t.Name) {
		return t.Name
	}
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range t.Name {
		if r == '"' || r == '\\' {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	b.WriteByte('"')
	return b.String()
}

// String implements Node.
func (n Not) String() string { return "!" + n.X.String() }

// String implements Node.
func (a And) String() string { return joinNodes(a.Xs, " && ") }

// String implements Node.
func (o Or) String() string { return joinNodes(o.Xs, " || ") }

func joinNodes(xs []Node, sep string) string {
	parts := make([]string, len(xs))
	for i, x := range xs {
		// A nested Or inside an And (or the reverse) needs parentheses to keep
		// the rendered form parseable back to the same tree.
		if needsParens(x) {
			parts[i] = "(" + x.String() + ")"
		} else {
			parts[i] = x.String()
		}
	}
	return strings.Join(parts, sep)
}

func needsParens(n Node) bool {
	switch x := n.(type) {
	case And:
		return len(x.Xs) > 1
	case Or:
		return len(x.Xs) > 1
	}
	return false
}

// isBare reports whether a name can be written without quotes. Names holding a
// quote or a backslash must be quoted, or the rendered form would not lex back
// to the same tag.
func isBare(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if unicode.IsSpace(r) || isOperatorRune(r) || r == '!' ||
			r == '"' || r == '\'' || r == '\\' {
			return false
		}
	}
	switch strings.ToLower(s) {
	case "and", "or", "not":
		return false
	}
	return true
}

func isOperatorRune(r rune) bool {
	switch r {
	case '&', '|', '(', ')':
		return true
	}
	return false
}

// ParseError explains where an expression went wrong. Pos is a byte offset into
// the input, so a caller can point at the offending character.
type ParseError struct {
	Msg string
	Pos int
}

// Error implements error.
func (e *ParseError) Error() string {
	return fmt.Sprintf("%s at position %d", e.Msg, e.Pos+1)
}

// Parse turns an expression into a tree. The empty expression parses to nil,
// which means "no filter".
func Parse(s string) (Node, error) {
	toks, err := lex(s)
	if err != nil {
		return nil, err
	}
	if len(toks) == 0 {
		return nil, nil
	}
	p := &parser{toks: toks, src: s}
	n, err := p.parseOr()
	if err != nil {
		return nil, err
	}
	if !p.done() {
		t := p.peek()
		return nil, &ParseError{Msg: "unexpected " + t.describe(), Pos: t.pos}
	}
	return n, nil
}

// ---- lexer ---------------------------------------------------------------

type tokKind int

const (
	tokTag tokKind = iota
	tokAnd
	tokOr
	tokNot
	tokOpen
	tokClose
)

type token struct {
	kind tokKind
	text string
	pos  int
}

func (t token) describe() string {
	switch t.kind {
	case tokAnd:
		return `"&&"`
	case tokOr:
		return `"||"`
	case tokNot:
		return `"!"`
	case tokOpen:
		return `"("`
	case tokClose:
		return `")"`
	}
	return "tag " + t.text
}

// lex splits the input into tokens. Words that are exactly and/or/not become
// operators; every other run of characters is a tag, with backslash escaping
// so a tag can contain a space or an operator character.
func lex(s string) ([]token, error) {
	var toks []token
	i := 0
	for i < len(s) {
		c := s[i]
		switch c {
		case ' ', '\t', '\n':
			i++
		case '&':
			// "&&" is the AND operator; a lone "&" is an ordinary character, so
			// a tag like "R&D" needs no quoting.
			if i+1 < len(s) && s[i+1] == '&' {
				toks = append(toks, token{tokAnd, "&&", i})
				i += 2
				continue
			}
			text, next := lexWord(s, i)
			toks = append(toks, token{tokTag, text, i})
			i = next
		case '|':
			if i+1 < len(s) && s[i+1] == '|' {
				toks = append(toks, token{tokOr, "||", i})
				i += 2
				continue
			}
			return nil, &ParseError{Msg: `stray "|", use "||"`, Pos: i}
		case '!':
			toks = append(toks, token{tokNot, "!", i})
			i++
		case '(':
			toks = append(toks, token{tokOpen, "(", i})
			i++
		case ')':
			toks = append(toks, token{tokClose, ")", i})
			i++
		case '"', '\'':
			text, next, err := lexQuoted(s, i)
			if err != nil {
				return nil, err
			}
			toks = append(toks, token{tokTag, text, i})
			i = next
		default:
			text, next := lexWord(s, i)
			switch strings.ToLower(text) {
			case "and":
				toks = append(toks, token{tokAnd, text, i})
			case "or":
				toks = append(toks, token{tokOr, text, i})
			case "not":
				toks = append(toks, token{tokNot, text, i})
			case "":
				// Nothing consumed (an operator rune mid-word); report it.
				return nil, &ParseError{Msg: fmt.Sprintf("unexpected %q", string(s[i])), Pos: i}
			default:
				toks = append(toks, token{tokTag, text, i})
			}
			i = next
		}
	}
	return toks, nil
}

// lexQuoted reads a quoted string starting at s[i]. A backslash escapes the
// next character, so a tag may contain the quote character.
func lexQuoted(s string, i int) (string, int, error) {
	quote := s[i]
	var b strings.Builder
	j := i + 1
	for j < len(s) {
		c := s[j]
		switch c {
		case '\\':
			if j+1 < len(s) {
				b.WriteByte(s[j+1])
				j += 2
				continue
			}
			return "", 0, &ParseError{Msg: "trailing backslash", Pos: j}
		case quote:
			if b.Len() == 0 {
				return "", 0, &ParseError{Msg: "empty tag name", Pos: i}
			}
			return b.String(), j + 1, nil
		default:
			b.WriteByte(c)
			j++
		}
	}
	return "", 0, &ParseError{Msg: "unterminated quote", Pos: i}
}

// lexWord reads an unquoted tag name ending at whitespace or an operator.
func lexWord(s string, i int) (string, int) {
	var b strings.Builder
	j := i
	for j < len(s) {
		c := s[j]
		if c == ' ' || c == '\t' || c == '\n' || c == '(' || c == ')' || c == '|' {
			break
		}
		if c == '&' {
			// A pair starts the AND operator; a lone one belongs to the name.
			if j+1 < len(s) && s[j+1] == '&' {
				break
			}
			b.WriteByte(c)
			j++
			continue
		}
		if c == '!' && j > i {
			// "a!b" is unusual; stop so the "!" starts a negation and the
			// error message points at the real problem.
			break
		}
		if c == '\\' && j+1 < len(s) {
			b.WriteByte(s[j+1])
			j += 2
			continue
		}
		b.WriteByte(c)
		j++
	}
	return b.String(), j
}

// ---- parser --------------------------------------------------------------

type parser struct {
	toks []token
	i    int
	src  string
}

func (p *parser) done() bool { return p.i >= len(p.toks) }

func (p *parser) peek() token {
	if p.done() {
		return token{kind: tokClose, pos: len(p.src)}
	}
	return p.toks[p.i]
}

func (p *parser) next() token {
	t := p.peek()
	if p.i < len(p.toks) {
		p.i++
	}
	return t
}

// parseOr parses a disjunction.
func (p *parser) parseOr() (Node, error) {
	first, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	xs := []Node{first}
	for !p.done() && p.peek().kind == tokOr {
		p.next()
		if p.done() || p.peek().kind == tokClose {
			t := p.peek()
			return nil, &ParseError{Msg: `"||" needs an expression on its right`, Pos: t.pos}
		}
		n, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		xs = append(xs, n)
	}
	if len(xs) == 1 {
		return xs[0], nil
	}
	return Or{Xs: xs}, nil
}

func (p *parser) parseAnd() (Node, error) {
	first, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	xs := []Node{first}
	for !p.done() {
		switch p.peek().kind {
		case tokAnd:
			p.next()
			if p.done() || p.peek().kind == tokClose {
				t := p.peek()
				return nil, &ParseError{Msg: `"&&" needs an expression on its right`, Pos: t.pos}
			}
			n, err := p.parseUnary()
			if err != nil {
				return nil, err
			}
			xs = append(xs, n)
		case tokOr, tokClose:
			return andNode(xs), nil
		default:
			// Juxtaposition: "LLM DB" means "LLM && DB".
			n, err := p.parseUnary()
			if err != nil {
				return nil, err
			}
			xs = append(xs, n)
		}
	}
	return andNode(xs), nil
}

// andNode returns the single child when there is only one, so a one-term query
// does not produce a one-element And node.
func andNode(xs []Node) Node {
	if len(xs) == 1 {
		return xs[0]
	}
	return And{Xs: xs}
}

func (p *parser) parseUnary() (Node, error) {
	if !p.done() && p.peek().kind == tokNot {
		t := p.next()
		if p.done() || p.peek().kind == tokClose {
			return nil, &ParseError{Msg: `"!" needs an expression after it`, Pos: t.pos}
		}
		n, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return Not{X: n}, nil
	}
	return p.parsePrimary()
}

func (p *parser) parsePrimary() (Node, error) {
	if p.done() {
		return nil, &ParseError{Msg: "unexpected end of expression", Pos: len(p.src)}
	}
	t := p.next()
	switch t.kind {
	case tokTag:
		return Tag{Name: t.text}, nil
	case tokOpen:
		if !p.done() && p.peek().kind == tokClose {
			return nil, &ParseError{Msg: "empty parentheses", Pos: t.pos}
		}
		n, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		if p.done() || p.peek().kind != tokClose {
			return nil, &ParseError{Msg: `missing ")"`, Pos: t.pos}
		}
		p.next()
		return n, nil
	case tokClose:
		return nil, &ParseError{Msg: `unexpected ")"`, Pos: t.pos}
	case tokAnd:
		return nil, &ParseError{Msg: `"&&" needs an expression on its left`, Pos: t.pos}
	case tokOr:
		return nil, &ParseError{Msg: `"||" needs an expression on its left`, Pos: t.pos}
	}
	return nil, &ParseError{Msg: "unexpected " + t.describe(), Pos: t.pos}
}

// ---- helpers -------------------------------------------------------------

// Tags returns every tag name the expression mentions, in order of appearance.
func Tags(n Node) []string {
	var out []string
	walk(n, func(t Tag) {
		if t.Name != "" {
			out = append(out, t.Name)
		}
	})
	return out
}

// UnknownTags returns the mentioned tags that are missing from known, sorted
// and de-duplicated. A quoted or unquoted name is compared case-insensitively.
func UnknownTags(n Node, known func(string) bool) []string {
	seen := map[string]bool{}
	var out []string
	for _, name := range Tags(n) {
		key := strings.ToLower(name)
		if seen[key] {
			continue
		}
		seen[key] = true
		if !known(name) {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

func walk(n Node, fn func(Tag)) {
	switch x := n.(type) {
	case Tag:
		fn(x)
	case Not:
		walk(x.X, fn)
	case And:
		for _, c := range x.Xs {
			walk(c, fn)
		}
	case Or:
		for _, c := range x.Xs {
			walk(c, fn)
		}
	}
}
