package pattern

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// fixedNow pins time so tests across machines / time-of-day stay
// deterministic. Tests that touch dates set this in setUp.
func fixedNow(t *testing.T, when string) func() {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, when)
	require.NoError(t, err)
	prev := nowFn
	nowFn = func() time.Time { return parsed }
	return func() { nowFn = prev }
}

func TestParseSinglePredicate(t *testing.T) {
	got, err := Parse("~f bob@acme.com")
	require.NoError(t, err)
	pred, ok := got.(Predicate)
	require.True(t, ok)
	require.Equal(t, FieldFrom, pred.Field)
	sv := pred.Value.(StringValue)
	require.Equal(t, "bob@acme.com", sv.Raw)
	require.Equal(t, MatchExact, sv.Match)
}

func TestParseWildcardKinds(t *testing.T) {
	cases := []struct {
		src   string
		match MatchKind
		raw   string
	}{
		{"~f newsletter@*", MatchPrefix, "newsletter@"},
		{"~f *@vendor.com", MatchSuffix, "@vendor.com"},
		{"~f *spam*", MatchContains, "spam"},
		{"~s a*b*c", MatchContains, "abc"}, // multi-* degrades to contains
	}
	for _, c := range cases {
		t.Run(c.src, func(t *testing.T) {
			got, err := Parse(c.src)
			require.NoError(t, err)
			pred := got.(Predicate)
			sv := pred.Value.(StringValue)
			require.Equal(t, c.match, sv.Match)
			require.Equal(t, c.raw, sv.Raw)
		})
	}
}

func TestParseQuotedSubject(t *testing.T) {
	got, err := Parse(`~s "Q4 review"`)
	require.NoError(t, err)
	pred := got.(Predicate)
	require.Equal(t, FieldSubject, pred.Field)
	require.Equal(t, "Q4 review", pred.Value.(StringValue).Raw)
}

func TestParseImplicitAnd(t *testing.T) {
	got, err := Parse("~f bob ~s budget")
	require.NoError(t, err)
	and, ok := got.(And)
	require.True(t, ok)
	require.Equal(t, FieldFrom, and.L.(Predicate).Field)
	require.Equal(t, FieldSubject, and.R.(Predicate).Field)
}

func TestParseExplicitAndOr(t *testing.T) {
	got, err := Parse("~f bob | ~f alice")
	require.NoError(t, err)
	or := got.(Or)
	require.Equal(t, FieldFrom, or.L.(Predicate).Field)
	require.Equal(t, FieldFrom, or.R.(Predicate).Field)

	got, err = Parse("~f bob & ~A")
	require.NoError(t, err)
	and := got.(And)
	require.Equal(t, FieldFrom, and.L.(Predicate).Field)
	require.Equal(t, FieldHasAttachments, and.R.(Predicate).Field)
}

func TestParsePrecedenceAndGrouping(t *testing.T) {
	// `~f bob | ~f alice ~A` — implicit AND between the OR's RHS and ~A
	// produces (bob OR (alice AND ~A)).
	got, err := Parse("~f bob | ~f alice ~A")
	require.NoError(t, err)
	or := got.(Or)
	require.Equal(t, FieldFrom, or.L.(Predicate).Field)
	rhs := or.R.(And)
	require.Equal(t, FieldFrom, rhs.L.(Predicate).Field)
	require.Equal(t, FieldHasAttachments, rhs.R.(Predicate).Field)

	// Parens flip precedence.
	got, err = Parse("(~f bob | ~f alice) ~A")
	require.NoError(t, err)
	root := got.(And)
	innerOr := root.L.(Or)
	require.Equal(t, FieldFrom, innerOr.L.(Predicate).Field)
	require.Equal(t, FieldFrom, innerOr.R.(Predicate).Field)
	require.Equal(t, FieldHasAttachments, root.R.(Predicate).Field)
}

func TestParseNot(t *testing.T) {
	got, err := Parse("! ~N")
	require.NoError(t, err)
	notN := got.(Not)
	require.Equal(t, FieldUnread, notN.X.(Predicate).Field)
}

func TestParseNoArgOperators(t *testing.T) {
	for _, src := range []string{"~A", "~N", "~F", "~U"} {
		got, err := Parse(src)
		require.NoError(t, err, src)
		pred := got.(Predicate)
		_, ok := pred.Value.(EmptyValue)
		require.True(t, ok, "%s value should be EmptyValue", src)
	}
}

func TestParseRejectsUnknownOperator(t *testing.T) {
	_, err := Parse("~Z foo")
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown operator")
}

func TestParseRejectsMissingArgument(t *testing.T) {
	_, err := Parse("~f")
	require.Error(t, err)
	require.Contains(t, err.Error(), "requires an argument")
}

func TestParseRejectsUnclosedQuote(t *testing.T) {
	_, err := Parse(`~s "unterminated`)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unterminated")
}

func TestParseRejectsTrailingTokens(t *testing.T) {
	_, err := Parse("~A )")
	require.Error(t, err)
}

// ---------- Date parsing -----------

func TestParseDateDuration(t *testing.T) {
	defer fixedNow(t, "2026-04-28T12:00:00Z")()
	got, err := Parse("~d <30d")
	require.NoError(t, err)
	dv := got.(Predicate).Value.(DateValue)
	require.Equal(t, DateWithinLast, dv.Op)
	want := time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC).Add(-30 * 24 * time.Hour)
	require.True(t, dv.At.Equal(want), "got %v want %v", dv.At, want)
}

func TestParseDateOlderThan(t *testing.T) {
	defer fixedNow(t, "2026-04-28T12:00:00Z")()
	got, err := Parse("~d >180d")
	require.NoError(t, err)
	dv := got.(Predicate).Value.(DateValue)
	require.Equal(t, DateBefore, dv.Op)
}

func TestParseDateAbsolute(t *testing.T) {
	got, err := Parse("~d >=2026-01-01")
	require.NoError(t, err)
	dv := got.(Predicate).Value.(DateValue)
	require.Equal(t, DateAfterEq, dv.Op)
	require.Equal(t, "2026-01-01T00:00:00Z", dv.At.Format(time.RFC3339))
}

func TestParseDateRange(t *testing.T) {
	got, err := Parse("~d 2026-03-01..2026-04-01")
	require.NoError(t, err)
	dv := got.(Predicate).Value.(DateValue)
	require.Equal(t, DateRange, dv.Op)
	require.Equal(t, "2026-03-01T00:00:00Z", dv.At.Format(time.RFC3339))
	require.Equal(t, "2026-04-02T00:00:00Z", dv.End.Format(time.RFC3339), "end is exclusive day-after")
}

func TestParseDateNamed(t *testing.T) {
	defer fixedNow(t, "2026-04-28T15:30:00Z")()
	got, err := Parse("~d today")
	require.NoError(t, err)
	dv := got.(Predicate).Value.(DateValue)
	require.Equal(t, DateOn, dv.Op)
}

// ---------- Local SQL evaluator -----------

func TestCompileLocalFromSimple(t *testing.T) {
	root, err := Parse("~f bob@acme.com")
	require.NoError(t, err)
	c, err := CompileLocal(root)
	require.NoError(t, err)
	// from_address = ? OR from_name = ?
	require.Contains(t, c.Where, "from_address = ?")
	require.Contains(t, c.Where, "from_name = ?")
	require.Equal(t, []any{"bob@acme.com", "bob@acme.com"}, c.Args)
}

func TestCompileLocalFromPrefixWildcard(t *testing.T) {
	root, err := Parse("~f newsletter@*")
	require.NoError(t, err)
	c, err := CompileLocal(root)
	require.NoError(t, err)
	require.Contains(t, c.Where, "from_address LIKE ?")
	require.Equal(t, "newsletter@%", c.Args[0])
}

func TestCompileLocalAndOrNot(t *testing.T) {
	root, err := Parse("(~f bob | ~f alice) & ! ~N")
	require.NoError(t, err)
	c, err := CompileLocal(root)
	require.NoError(t, err)
	// Loose structural assertion — exact placement of parens matters
	// less than the semantic content.
	require.Contains(t, c.Where, "OR")
	require.Contains(t, c.Where, "AND")
	require.Contains(t, c.Where, "NOT")
	require.Contains(t, c.Where, "is_read = 0")
}

func TestCompileLocalNoArgPredicates(t *testing.T) {
	cases := map[string]string{
		"~A": "has_attachments = 1",
		"~N": "is_read = 0",
		"~U": "is_read = 1",
		"~F": "flag_status = 'flagged'",
	}
	for src, want := range cases {
		t.Run(src, func(t *testing.T) {
			root, err := Parse(src)
			require.NoError(t, err)
			c, err := CompileLocal(root)
			require.NoError(t, err)
			require.Equal(t, want, c.Where)
			require.Empty(t, c.Args)
		})
	}
}

func TestCompileLocalDateWithinLast(t *testing.T) {
	defer fixedNow(t, "2026-04-28T12:00:00Z")()
	root, err := Parse("~d <30d")
	require.NoError(t, err)
	c, err := CompileLocal(root)
	require.NoError(t, err)
	require.Equal(t, "received_at >= ?", c.Where)
	want := time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC).Add(-30 * 24 * time.Hour).Unix()
	require.Equal(t, []any{want}, c.Args)
}

func TestCompileLocalDateRange(t *testing.T) {
	root, err := Parse("~d 2026-03-01..2026-04-01")
	require.NoError(t, err)
	c, err := CompileLocal(root)
	require.NoError(t, err)
	require.Contains(t, c.Where, "received_at >= ?")
	require.Contains(t, c.Where, "received_at < ?")
	require.Len(t, c.Args, 2)
}

func TestCompileLocalRejectsHeaderField(t *testing.T) {
	root, err := Parse("~h list-id:newsletter")
	require.NoError(t, err)
	_, err = CompileLocal(root)
	require.Error(t, err)
	require.Contains(t, err.Error(), "server-only")
}

func TestCompileLocalSubjectContains(t *testing.T) {
	root, err := Parse(`~s budget`)
	require.NoError(t, err)
	c, err := CompileLocal(root)
	require.NoError(t, err)
	require.Equal(t, "subject = ?", c.Where, "no wildcard → exact match")
	require.Equal(t, []any{"budget"}, c.Args)

	root, err = Parse(`~s *budget*`)
	require.NoError(t, err)
	c, err = CompileLocal(root)
	require.NoError(t, err)
	require.Contains(t, c.Where, "LIKE")
	require.Equal(t, "%budget%", c.Args[0])
}

func TestCompileLocalEscapesWildcardLiterals(t *testing.T) {
	// User searches for a literal "%" — our LIKE arg must escape it.
	root, err := Parse(`~s 50%off*`)
	require.NoError(t, err)
	c, err := CompileLocal(root)
	require.NoError(t, err)
	// `%` at index 2 must be escaped to `\%`; the trailing `*` becomes
	// the LIKE wildcard `%`.
	require.True(t, strings.HasSuffix(c.Args[0].(string), "%"))
	require.Contains(t, c.Args[0].(string), `\%`)
	// And the SQL must declare the escape character — without
	// `ESCAPE '\'`, SQLite treats `\` as literal text and the
	// previous escape pass silently produces a query that matches
	// no rows. Real-tenant bug class.
	require.Contains(t, c.Where, `ESCAPE '\'`)
}
