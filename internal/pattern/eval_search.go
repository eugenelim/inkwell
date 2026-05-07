package pattern

import (
	"fmt"
	"strings"
)

// EmitSearch renders the AST as a Graph $search expression. Spec
// 08 §10. Returns [ErrUnsupported] when the AST contains a
// predicate $search can't express (~N / ~U / ~F / ~i / ~y /
// negation of structural fields).
//
// Boolean composition: AND / OR / NOT (uppercase per the Graph
// $search spec; spaces between tokens are implicit AND but we
// emit explicit AND for clarity). The whole expression goes
// inside a single quoted value at the call site
// (`?$search="..."`); this generator produces the value, not the
// surrounding quotes.
func EmitSearch(root Node) (string, error) {
	if root == nil {
		return "", fmt.Errorf("EmitSearch: nil AST")
	}
	return emitSearch(root)
}

func emitSearch(n Node) (string, error) {
	switch v := n.(type) {
	case And:
		l, err := emitSearch(v.L)
		if err != nil {
			return "", err
		}
		r, err := emitSearch(v.R)
		if err != nil {
			return "", err
		}
		return "(" + l + " AND " + r + ")", nil
	case Or:
		l, err := emitSearch(v.L)
		if err != nil {
			return "", err
		}
		r, err := emitSearch(v.R)
		if err != nil {
			return "", err
		}
		return "(" + l + " OR " + r + ")", nil
	case Not:
		s, err := emitSearch(v.X)
		if err != nil {
			return "", err
		}
		return "(NOT " + s + ")", nil
	case Predicate:
		return emitSearchPredicate(v)
	}
	return "", fmt.Errorf("emitSearch: unknown node %T", n)
}

func emitSearchPredicate(p Predicate) (string, error) {
	switch p.Field {
	case FieldHasAttachments:
		return "hasattachment:true", nil
	case FieldUnread, FieldRead, FieldFlagged:
		// $search has no isread:false / flag-status field. Spec
		// 08 §10.1: these flow via TwoStage (server returns
		// candidates; local refines).
		return "", fmt.Errorf("%w: ~N / ~U / ~F not expressible in Graph $search", ErrUnsupported)
	}

	switch v := p.Value.(type) {
	case StringValue:
		return emitSearchString(p.Field, v)
	case DateValue:
		return emitSearchDate(p.Field, v)
	case RoutingValue:
		// ~o is local-only — no Graph equivalent. Spec 23 §4.3.
		return "", fmt.Errorf("%w: ~o routing is local-only (no Graph equivalent)", ErrUnsupported)
	}
	return "", fmt.Errorf("%w: unsupported value type %T for field %v", ErrUnsupported, p.Value, p.Field)
}

func emitSearchString(f Field, v StringValue) (string, error) {
	switch f {
	case FieldFrom:
		return "from:" + searchTerm(v.Raw), nil
	case FieldTo:
		return "to:" + searchTerm(v.Raw), nil
	case FieldCc:
		return "cc:" + searchTerm(v.Raw), nil
	case FieldRecipient:
		return "(to:" + searchTerm(v.Raw) + " OR cc:" + searchTerm(v.Raw) + ")", nil
	case FieldSubject:
		return "subject:" + searchTerm(v.Raw), nil
	case FieldBody:
		return "body:" + searchTerm(v.Raw), nil
	case FieldSubjectOrBody:
		// Default-field $search hits subject + body; no field
		// prefix means "anywhere in the indexed text".
		return searchTerm(v.Raw), nil
	case FieldCategory:
		return "category:" + searchTerm(v.Raw), nil
	case FieldHeader:
		// ~h list-id:newsletter → list-id:newsletter (raw).
		// The user typed `name:value` as the argument, so we
		// emit it verbatim.
		return v.Raw, nil
	case FieldFolder:
		// Folder scope rides on the URL, not the search expression.
		// Same shape as $filter.
		return "", fmt.Errorf("%w: folder scope is encoded in the URL path, not $search", ErrUnsupported)
	case FieldImportance, FieldInferenceCls, FieldConversation:
		return "", fmt.Errorf("%w: ~i / ~y / ~v not expressible in Graph $search", ErrUnsupported)
	}
	return "", fmt.Errorf("%w: emitSearchString field %v", ErrUnsupported, f)
}

// searchTerm wraps the value in double quotes when it contains
// whitespace or shell-y punctuation that Graph's $search would
// otherwise tokenise on. Email-shaped values are auto-quoted to
// keep the `@` and `.` from splitting the term.
func searchTerm(raw string) string {
	if raw == "" {
		return raw
	}
	if strings.HasPrefix(raw, `"`) && strings.HasSuffix(raw, `"`) {
		return raw
	}
	if needsSearchQuote(raw) {
		return `"` + strings.ReplaceAll(raw, `"`, ``) + `"`
	}
	return raw
}

// needsSearchQuote reports whether a raw term needs double-quote
// wrapping. Anything outside ASCII alphanum + `*` + `-` + `_`
// gets quoted so Graph treats it as a phrase. The quote rule is
// pragmatic, not exact (Graph's exact rules are
// version-dependent); over-quoting is safer than under.
func needsSearchQuote(t string) bool {
	for i := 0; i < len(t); i++ {
		c := t[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' {
			continue
		}
		switch c {
		case '*', '-', '_':
			continue
		}
		return true
	}
	return false
}

// emitSearchDate renders date predicates using Graph's
// `received>=YYYY-MM-DD` shorthand. $search supports only the
// date portion (no time); we truncate. Bounded ranges become an
// AND'd pair.
func emitSearchDate(f Field, v DateValue) (string, error) {
	field := "received"
	if f == FieldDateSent {
		field = "sent"
	}
	atDay := v.At.UTC().Format("2006-01-02")
	endDay := v.End.UTC().Format("2006-01-02")
	switch v.Op {
	case DateBefore:
		return field + "<" + atDay, nil
	case DateBeforeEq:
		return field + "<=" + atDay, nil
	case DateAfter:
		return field + ">" + atDay, nil
	case DateAfterEq:
		return field + ">=" + atDay, nil
	case DateOn:
		return "(" + field + ">=" + atDay + " AND " + field + "<" + endDay + ")", nil
	case DateRange:
		return "(" + field + ">=" + atDay + " AND " + field + "<" + endDay + ")", nil
	case DateWithinLast:
		return field + ">=" + atDay, nil
	}
	return "", fmt.Errorf("%w: unknown date op %v", ErrUnsupported, v.Op)
}
