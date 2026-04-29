package pattern

import (
	"fmt"
	"strings"
)

// Parse turns pattern source into an AST. Returns the root [Node] or
// a typed error pointing at the offending byte offset.
//
// Grammar (precedence low → high):
//
//	expr    := orExpr
//	orExpr  := andExpr ('|' andExpr)*
//	andExpr := unary ( ('&' | implicit-AND) unary )*
//	unary   := '!' unary | atom
//	atom    := '(' expr ')' | predicate
//	predicate := operator argument?
//
// Implicit-AND fires whenever two predicate-producing constructs sit
// adjacent without an explicit operator in between (e.g. `~f bob ~s
// budget`). Inside the parser this is detected by the absence of `&`,
// `|`, or `)` between two atoms.
func Parse(src string) (Node, error) {
	toks, err := lex(src)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks}
	root, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if p.peek().kind != tkEOF {
		return nil, fmt.Errorf("pattern: trailing tokens at offset %d", p.peek().pos)
	}
	if root == nil {
		return nil, fmt.Errorf("pattern: empty expression")
	}
	return root, nil
}

type parser struct {
	toks []token
	idx  int
}

func (p *parser) peek() token { return p.toks[p.idx] }
func (p *parser) eat() token  { t := p.toks[p.idx]; p.idx++; return t }

func (p *parser) parseExpr() (Node, error) { return p.parseOr() }

func (p *parser) parseOr() (Node, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.peek().kind == tkOr {
		p.eat()
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = Or{L: left, R: right}
	}
	return left, nil
}

func (p *parser) parseAnd() (Node, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for {
		switch p.peek().kind {
		case tkAnd:
			p.eat()
			right, err := p.parseUnary()
			if err != nil {
				return nil, err
			}
			left = And{L: left, R: right}
		case tkOperator, tkNot, tkLParen:
			// Implicit AND between adjacent atoms.
			right, err := p.parseUnary()
			if err != nil {
				return nil, err
			}
			left = And{L: left, R: right}
		default:
			return left, nil
		}
	}
}

func (p *parser) parseUnary() (Node, error) {
	if p.peek().kind == tkNot {
		p.eat()
		x, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return Not{X: x}, nil
	}
	return p.parseAtom()
}

func (p *parser) parseAtom() (Node, error) {
	t := p.peek()
	switch t.kind {
	case tkLParen:
		p.eat()
		inner, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if p.peek().kind != tkRParen {
			return nil, fmt.Errorf("pattern: missing ) at offset %d", p.peek().pos)
		}
		p.eat()
		return inner, nil
	case tkOperator:
		return p.parsePredicate()
	}
	return nil, fmt.Errorf("pattern: expected predicate or '(' at offset %d, got %v", t.pos, t.kind)
}

func (p *parser) parsePredicate() (Node, error) {
	op := p.eat() // tkOperator
	field, ok := fieldForOp(op.val)
	if !ok {
		return nil, fmt.Errorf("pattern: unknown operator ~%s at offset %d", op.val, op.pos)
	}
	if !opTakesArgument(op.val) {
		return Predicate{Field: field, Value: EmptyValue{}}, nil
	}
	if p.peek().kind != tkArgument {
		return nil, fmt.Errorf("pattern: operator ~%s requires an argument at offset %d", op.val, op.pos)
	}
	arg := p.eat()
	val, err := buildPredicateValue(field, arg.val)
	if err != nil {
		return nil, fmt.Errorf("pattern: ~%s: %w", op.val, err)
	}
	return Predicate{Field: field, Value: val}, nil
}

// buildPredicateValue converts a raw argument string into the typed
// PredicateValue for the field's family (string vs date).
func buildPredicateValue(f Field, raw string) (PredicateValue, error) {
	switch f {
	case FieldDateReceived, FieldDateSent:
		return parseDateValue(raw)
	case FieldHasAttachments, FieldUnread, FieldFlagged, FieldRead:
		// Should never reach here — opTakesArgument filters these out.
		return EmptyValue{}, nil
	}
	// String family — wildcard handling.
	return parseStringValue(raw), nil
}

// parseStringValue extracts the wildcard kind and stripped raw value.
// `*foo` → suffix, `foo*` → prefix, `*foo*` → contains, `foo` → exact.
// Internal `*` (foo*bar) is treated as contains over the post-strip raw.
func parseStringValue(raw string) StringValue {
	hasL := strings.HasPrefix(raw, "*")
	hasR := strings.HasSuffix(raw, "*")
	stripped := raw
	if hasL {
		stripped = stripped[1:]
	}
	if hasR {
		stripped = stripped[:len(stripped)-1]
	}
	if strings.Contains(stripped, "*") {
		// Multiple-internal-star — degrade to contains over the original
		// minus the outer stars.
		stripped = strings.ReplaceAll(stripped, "*", "")
		return StringValue{Raw: stripped, Match: MatchContains}
	}
	switch {
	case hasL && hasR:
		return StringValue{Raw: stripped, Match: MatchContains}
	case hasL:
		return StringValue{Raw: stripped, Match: MatchSuffix}
	case hasR:
		return StringValue{Raw: stripped, Match: MatchPrefix}
	}
	return StringValue{Raw: stripped, Match: MatchExact}
}
