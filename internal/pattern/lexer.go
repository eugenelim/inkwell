package pattern

import (
	"fmt"
	"unicode"
)

// tokenKind enumerates lexer outputs.
type tokenKind int

const (
	tkEOF      tokenKind = iota
	tkOperator           // ~f, ~s, ~A, …
	tkArgument           // bare-word or quoted argument to an operator
	tkAnd                // & (or implicit between adjacent predicates)
	tkOr                 // |
	tkNot                // !
	tkLParen             // (
	tkRParen             // )
)

type token struct {
	kind tokenKind
	val  string // operator letter (without ~), argument text, etc.
	pos  int    // byte offset in source for diagnostics
}

// lex tokenises the pattern source. Whitespace separates tokens; the
// parser is responsible for inserting implicit ANDs between adjacent
// predicate-producing tokens.
func lex(src string) ([]token, error) {
	var out []token
	i := 0
	for i < len(src) {
		c := src[i]
		switch {
		case unicode.IsSpace(rune(c)):
			i++
			continue
		case c == '~':
			if i+1 >= len(src) {
				return nil, fmt.Errorf("pattern: dangling ~ at offset %d", i)
			}
			letter := src[i+1]
			if !isOpLetter(letter) {
				return nil, fmt.Errorf("pattern: unknown operator ~%c at offset %d", letter, i)
			}
			out = append(out, token{kind: tkOperator, val: string(letter), pos: i})
			i += 2
			// Argument? Some operators take none — the parser decides
			// based on the operator letter. Greedy-read the next bare-
			// word or quoted string IF one is present.
			j := i
			for j < len(src) && unicode.IsSpace(rune(src[j])) {
				j++
			}
			if j >= len(src) {
				continue
			}
			next := src[j]
			if next == '&' || next == '|' || next == '!' || next == '(' || next == ')' || next == '~' {
				continue // no argument
			}
			if next == '"' {
				k := j + 1
				start := k
				for k < len(src) && src[k] != '"' {
					k++
				}
				if k >= len(src) {
					return nil, fmt.Errorf("pattern: unterminated quoted argument starting at %d", j)
				}
				out = append(out, token{kind: tkArgument, val: src[start:k], pos: j})
				i = k + 1
				continue
			}
			// Bare word: consume up to whitespace or boolean punctuation.
			k := j
			for k < len(src) && !unicode.IsSpace(rune(src[k])) &&
				src[k] != '&' && src[k] != '|' && src[k] != '(' && src[k] != ')' {
				k++
			}
			out = append(out, token{kind: tkArgument, val: src[j:k], pos: j})
			i = k
			continue
		case c == '&':
			out = append(out, token{kind: tkAnd, pos: i})
			i++
		case c == '|':
			out = append(out, token{kind: tkOr, pos: i})
			i++
		case c == '!':
			out = append(out, token{kind: tkNot, pos: i})
			i++
		case c == '(':
			out = append(out, token{kind: tkLParen, pos: i})
			i++
		case c == ')':
			out = append(out, token{kind: tkRParen, pos: i})
			i++
		default:
			return nil, fmt.Errorf("pattern: unexpected character %q at offset %d", c, i)
		}
	}
	out = append(out, token{kind: tkEOF, pos: i})
	return out, nil
}

func isOpLetter(c byte) bool {
	switch c {
	case 'f', 't', 'c', 'r', 's', 'b', 'B', 'd', 'D', 'A', 'N', 'F', 'U', 'G', 'i', 'y', 'v', 'm', 'h':
		return true
	}
	return false
}

// fieldForOp maps an operator letter to its [Field]. Returns false for
// unknown operators (defensive — lexer shouldn't emit them).
func fieldForOp(letter string) (Field, bool) {
	switch letter {
	case "f":
		return FieldFrom, true
	case "t":
		return FieldTo, true
	case "c":
		return FieldCc, true
	case "r":
		return FieldRecipient, true
	case "s":
		return FieldSubject, true
	case "b":
		return FieldBody, true
	case "B":
		return FieldSubjectOrBody, true
	case "d":
		return FieldDateReceived, true
	case "D":
		return FieldDateSent, true
	case "A":
		return FieldHasAttachments, true
	case "N":
		return FieldUnread, true
	case "F":
		return FieldFlagged, true
	case "U":
		return FieldRead, true
	case "G":
		return FieldCategory, true
	case "i":
		return FieldImportance, true
	case "y":
		return FieldInferenceCls, true
	case "v":
		return FieldConversation, true
	case "m":
		return FieldFolder, true
	case "h":
		return FieldHeader, true
	}
	return 0, false
}

// opTakesArgument reports whether an operator letter expects a value.
func opTakesArgument(letter string) bool {
	switch letter {
	case "A", "N", "F", "U":
		return false
	}
	return true
}
