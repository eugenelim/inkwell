package pattern

import (
	"fmt"
	"strings"
)

// EmitFilter renders the AST as an OData `$filter` expression
// suitable for /me/messages?$filter=... Spec 08 §9. Returns
// [ErrUnsupported] when the AST contains a predicate $filter
// can't express (the strategy selector then falls back to
// $search / TwoStage / LocalOnly).
//
// Boolean composition: AND / OR / NOT lower-cased per OData; the
// generator wraps subtrees in parentheses when needed for
// precedence safety. String literals are single-quote-escaped
// (single-quote doubled) per OData v4.
func EmitFilter(root Node) (string, error) {
	if root == nil {
		return "", fmt.Errorf("EmitFilter: nil AST")
	}
	return emitFilter(root)
}

func emitFilter(n Node) (string, error) {
	switch v := n.(type) {
	case And:
		l, err := emitFilter(v.L)
		if err != nil {
			return "", err
		}
		r, err := emitFilter(v.R)
		if err != nil {
			return "", err
		}
		return "(" + l + " and " + r + ")", nil
	case Or:
		l, err := emitFilter(v.L)
		if err != nil {
			return "", err
		}
		r, err := emitFilter(v.R)
		if err != nil {
			return "", err
		}
		return "(" + l + " or " + r + ")", nil
	case Not:
		s, err := emitFilter(v.X)
		if err != nil {
			return "", err
		}
		return "(not " + s + ")", nil
	case Predicate:
		return emitFilterPredicate(v)
	}
	return "", fmt.Errorf("emitFilter: unknown node %T", n)
}

func emitFilterPredicate(p Predicate) (string, error) {
	switch p.Field {
	case FieldHasAttachments:
		return "hasAttachments eq true", nil
	case FieldUnread:
		return "isRead eq false", nil
	case FieldRead:
		return "isRead eq true", nil
	case FieldFlagged:
		return "flag/flagStatus eq 'flagged'", nil
	}

	switch v := p.Value.(type) {
	case StringValue:
		return emitFilterString(p.Field, v)
	case DateValue:
		return emitFilterDate(p.Field, v)
	}
	return "", fmt.Errorf("%w: unsupported value type %T for field %v", ErrUnsupported, p.Value, p.Field)
}

// emitFilterString renders the Graph $filter shape for string-
// valued fields. Spec 08 §9 table.
func emitFilterString(f Field, v StringValue) (string, error) {
	switch f {
	case FieldFrom:
		return filterAddrField("from/emailAddress/address", "from/emailAddress/name", v)
	case FieldTo:
		return filterCollection("toRecipients", v)
	case FieldCc:
		return filterCollection("ccRecipients", v)
	case FieldRecipient:
		// to/any() OR cc/any() — full audience match.
		toExpr, err := filterCollection("toRecipients", v)
		if err != nil {
			return "", err
		}
		ccExpr, err := filterCollection("ccRecipients", v)
		if err != nil {
			return "", err
		}
		return "(" + toExpr + " or " + ccExpr + ")", nil
	case FieldSubject:
		return filterScalarString("subject", v)
	case FieldCategory:
		return "categories/any(c:c eq " + odataLit(v.Raw) + ")", nil
	case FieldImportance:
		return "importance eq " + odataLit(strings.ToLower(v.Raw)), nil
	case FieldInferenceCls:
		return "inferenceClassification eq " + odataLit(strings.ToLower(v.Raw)), nil
	case FieldConversation:
		return "conversationId eq " + odataLit(v.Raw), nil
	case FieldFolder:
		// Spec 08 §9 — folder scope rides on the URL path
		// (/me/mailFolders/{id}/messages), NOT inside $filter.
		// EmitFilter returns ErrUnsupported here so the planner
		// understands the predicate is being satisfied by URL
		// scoping rather than the expression.
		return "", fmt.Errorf("%w: folder scope is encoded in the URL path, not $filter", ErrUnsupported)
	case FieldBody, FieldSubjectOrBody, FieldHeader:
		// Body / subject-or-body / header are server-only via
		// $search (spec 08 §7.1). $filter rejects them.
		return "", fmt.Errorf("%w: ~b / ~B / ~h require Graph $search, not $filter", ErrUnsupported)
	}
	return "", fmt.Errorf("%w: emitFilterString field %v", ErrUnsupported, f)
}

// filterAddrField renders the from-field expression — exact
// matches as `=`, prefix/suffix/contains via OData functions.
// Both address and display-name columns are OR'd because a user
// typing `~f bob` typically means "Bob, regardless of which
// column carries the value".
func filterAddrField(addrCol, nameCol string, v StringValue) (string, error) {
	switch v.Match {
	case MatchExact:
		return "(" + addrCol + " eq " + odataLit(v.Raw) + " or " + nameCol + " eq " + odataLit(v.Raw) + ")", nil
	case MatchPrefix:
		return "(startswith(" + addrCol + "," + odataLit(v.Raw) + ") or startswith(" + nameCol + "," + odataLit(v.Raw) + "))", nil
	case MatchSuffix:
		return "(endswith(" + addrCol + "," + odataLit(v.Raw) + ") or endswith(" + nameCol + "," + odataLit(v.Raw) + "))", nil
	case MatchContains:
		return "(contains(" + addrCol + "," + odataLit(v.Raw) + ") or contains(" + nameCol + "," + odataLit(v.Raw) + "))", nil
	}
	return "", fmt.Errorf("%w: unknown match kind %v", ErrUnsupported, v.Match)
}

// filterScalarString renders a single-string-column predicate
// (subject, etc).
func filterScalarString(col string, v StringValue) (string, error) {
	switch v.Match {
	case MatchExact:
		// $filter doesn't have an equality operator for free-
		// form text, so exact match is treated as `contains` on
		// the whole value. This matches the existing local
		// behaviour (`subject = 'Q4'` is also approximate when
		// users mean phrase-match).
		return "contains(" + col + "," + odataLit(v.Raw) + ")", nil
	case MatchPrefix:
		return "startswith(" + col + "," + odataLit(v.Raw) + ")", nil
	case MatchSuffix:
		return "endswith(" + col + "," + odataLit(v.Raw) + ")", nil
	case MatchContains:
		return "contains(" + col + "," + odataLit(v.Raw) + ")", nil
	}
	return "", fmt.Errorf("%w: unknown match kind %v", ErrUnsupported, v.Match)
}

// filterCollection renders a recipient-collection predicate
// using OData's `any()` lambda. Wildcards on collection fields
// fall through to startswith/endswith/contains within the
// any(). Empty value rejected.
func filterCollection(col string, v StringValue) (string, error) {
	if v.Raw == "" {
		return "", fmt.Errorf("%w: empty value for %s", ErrUnsupported, col)
	}
	addr := col + "/any(r:r/emailAddress/address"
	switch v.Match {
	case MatchExact:
		return addr + " eq " + odataLit(v.Raw) + ")", nil
	case MatchPrefix:
		return col + "/any(r:startswith(r/emailAddress/address," + odataLit(v.Raw) + "))", nil
	case MatchSuffix:
		return col + "/any(r:endswith(r/emailAddress/address," + odataLit(v.Raw) + "))", nil
	case MatchContains:
		return col + "/any(r:contains(r/emailAddress/address," + odataLit(v.Raw) + "))", nil
	}
	return "", fmt.Errorf("%w: unknown match kind %v", ErrUnsupported, v.Match)
}

// emitFilterDate renders date predicates as OData ge / le
// inclusive bounds. Times are normalised to UTC ISO-8601 and
// quoted as datetimeoffset literals (Graph accepts the
// quoted-RFC3339 shape).
func emitFilterDate(f Field, v DateValue) (string, error) {
	col := "receivedDateTime"
	if f == FieldDateSent {
		col = "sentDateTime"
	}
	at := v.At.UTC().Format("2006-01-02T15:04:05Z")
	end := v.End.UTC().Format("2006-01-02T15:04:05Z")
	switch v.Op {
	case DateBefore:
		return col + " lt " + at, nil
	case DateBeforeEq:
		return col + " le " + at, nil
	case DateAfter:
		return col + " gt " + at, nil
	case DateAfterEq:
		return col + " ge " + at, nil
	case DateOn, DateRange:
		return "(" + col + " ge " + at + " and " + col + " lt " + end + ")", nil
	case DateWithinLast:
		return col + " ge " + at, nil
	}
	return "", fmt.Errorf("%w: unknown date op %v", ErrUnsupported, v.Op)
}

// odataLit single-quote-escapes per OData v4 (single quotes
// double up). The result includes the surrounding quotes.
func odataLit(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
