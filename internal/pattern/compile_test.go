package pattern

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestCompileStrategySelection is the spec 08 §17 "≥30 patterns
// strategy selection" gate. Each row is a pattern + the strategy
// the planner should pick + (optionally) substrings the rendered
// query strings should contain.
func TestCompileStrategySelection(t *testing.T) {
	cases := []struct {
		name              string
		src               string
		opts              CompileOptions
		wantStrategy      ExecutionStrategy
		wantFilterSubstr  string
		wantSearchSubstr  string
		wantLocalSubstr   string
		wantNotesContains string
	}{
		// Local-only forced (offline path).
		{
			name:            "local_only_forced",
			src:             "~f bob",
			opts:            CompileOptions{LocalOnly: true},
			wantStrategy:    StrategyLocalOnly,
			wantLocalSubstr: "from_address",
		},
		{
			name:              "local_only_force_with_unsupported_predicate_errors",
			src:               "~h list-id:newsletter",
			opts:              CompileOptions{LocalOnly: true},
			wantStrategy:      0,
			wantNotesContains: "",
		},
		// Server-filter eligible: structural-only patterns.
		{
			name:             "from_exact_goes_to_filter",
			src:              "~f bob@vendor.invalid",
			wantStrategy:     StrategyServerFilter,
			wantFilterSubstr: "from/emailAddress/address eq 'bob@vendor.invalid'",
		},
		{
			name:             "from_prefix_goes_to_filter",
			src:              "~f newsletter@*",
			wantStrategy:     StrategyServerFilter,
			wantFilterSubstr: "startswith(from/emailAddress/address,'newsletter@')",
		},
		{
			name:             "from_suffix_goes_to_filter",
			src:              "~f *@vendor.invalid",
			wantStrategy:     StrategyServerFilter,
			wantFilterSubstr: "endswith(from/emailAddress/address,'@vendor.invalid')",
		},
		{
			name:             "unread_goes_to_filter",
			src:              "~N",
			wantStrategy:     StrategyServerFilter,
			wantFilterSubstr: "isRead eq false",
		},
		{
			name:             "flagged_goes_to_filter",
			src:              "~F",
			wantStrategy:     StrategyServerFilter,
			wantFilterSubstr: "flag/flagStatus eq 'flagged'",
		},
		{
			name:             "has_attachments_goes_to_filter",
			src:              "~A",
			wantStrategy:     StrategyServerFilter,
			wantFilterSubstr: "hasAttachments eq true",
		},
		{
			name:             "category_goes_to_filter",
			src:              "~G Work",
			wantStrategy:     StrategyServerFilter,
			wantFilterSubstr: "categories/any(c:c eq 'Work')",
		},
		{
			name:             "importance_goes_to_filter",
			src:              "~i high",
			wantStrategy:     StrategyServerFilter,
			wantFilterSubstr: "importance eq 'high'",
		},
		{
			name:             "subject_goes_to_filter",
			src:              "~s budget",
			wantStrategy:     StrategyServerFilter,
			wantFilterSubstr: "contains(subject,'budget')",
		},
		{
			name:             "to_recipient_goes_to_filter",
			src:              "~t alice@example.invalid",
			wantStrategy:     StrategyServerFilter,
			wantFilterSubstr: "toRecipients/any(r:r/emailAddress/address eq 'alice@example.invalid')",
		},
		{
			name:             "and_composition_goes_to_filter",
			src:              "~f newsletter@* & ~N",
			wantStrategy:     StrategyServerFilter,
			wantFilterSubstr: " and ",
		},
		{
			name:             "or_composition_goes_to_filter",
			src:              "~f bob | ~f alice",
			wantStrategy:     StrategyServerFilter,
			wantFilterSubstr: " or ",
		},
		{
			name:             "not_composition_goes_to_filter",
			src:              "! ~A",
			wantStrategy:     StrategyServerFilter,
			wantFilterSubstr: "(not hasAttachments eq true)",
		},
		{
			name:             "date_within_last_goes_to_filter",
			src:              "~d <30d",
			wantStrategy:     StrategyServerFilter,
			wantFilterSubstr: "receivedDateTime ge ",
		},
		// Server-search forced (body / header).
		{
			name:             "body_predicate_goes_to_search",
			src:              `~b "action required"`,
			wantStrategy:     StrategyServerSearch,
			wantSearchSubstr: `body:"action required"`,
		},
		{
			name:             "subject_or_body_goes_to_search",
			src:              "~B forecast",
			wantStrategy:     StrategyServerSearch,
			wantSearchSubstr: "forecast",
		},
		{
			name:             "header_goes_to_search",
			src:              "~h list-id:newsletter",
			wantStrategy:     StrategyServerSearch,
			wantSearchSubstr: "list-id:newsletter",
		},
		// TwoStage: server-only predicate AND local-only refinement.
		// LocalSQL is empty for TwoStage — refinement runs via
		// EvaluateInMemory against the cached envelope, not via
		// SQLite.
		{
			name:             "body_and_flagged_goes_to_two_stage",
			src:              `~b "magic phrase" & ~F`,
			wantStrategy:     StrategyTwoStage,
			wantSearchSubstr: `body:"magic phrase"`,
		},
		{
			name:             "body_and_importance_goes_to_two_stage",
			src:              `~b deck & ~i high`,
			wantStrategy:     StrategyTwoStage,
			wantSearchSubstr: "body:deck",
		},
		// PreferLocal: structurally-eligible-for-server but
		// local execution wins.
		{
			name:            "prefer_local_routes_to_local",
			src:             "~N",
			opts:            CompileOptions{PreferLocal: true},
			wantStrategy:    StrategyLocalOnly,
			wantLocalSubstr: "is_read",
		},
		// Notes content for --explain.
		{
			name:              "explain_filter_notes_present",
			src:               "~N",
			wantStrategy:      StrategyServerFilter,
			wantNotesContains: "All predicates satisfiable",
		},
		{
			name:              "explain_search_notes_present",
			src:               "~b deck",
			wantStrategy:      StrategyServerSearch,
			wantNotesContains: "Graph $search",
		},
		{
			name:              "explain_two_stage_notes_present",
			src:               "~b deck & ~N",
			wantStrategy:      StrategyTwoStage,
			wantNotesContains: "local refinement",
		},
		// Field with wildcards for recipient.
		{
			name:             "recipient_or_branches_to_filter",
			src:              "~r bob@vendor.invalid",
			wantStrategy:     StrategyServerFilter,
			wantFilterSubstr: "toRecipients/any",
		},
		// Date range maps to AND'd ge/lt.
		{
			name:             "date_on_today_goes_to_filter",
			src:              "~d today",
			wantStrategy:     StrategyServerFilter,
			wantFilterSubstr: "receivedDateTime ge ",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			compiled, err := Compile(c.src, c.opts)
			if c.name == "local_only_force_with_unsupported_predicate_errors" {
				require.Error(t, err)
				require.True(t, errors.Is(err, ErrPatternUnsupported),
					"expect ErrPatternUnsupported, got %v", err)
				return
			}
			require.NoError(t, err, "src=%q", c.src)
			require.Equal(t, c.wantStrategy, compiled.Strategy,
				"strategy mismatch; src=%q got plan=%s", c.src, compiled.Explain())
			if c.wantFilterSubstr != "" {
				require.Contains(t, compiled.Plan.GraphFilter, c.wantFilterSubstr,
					"$filter rendering; src=%q", c.src)
			}
			if c.wantSearchSubstr != "" {
				require.Contains(t, compiled.Plan.GraphSearch, c.wantSearchSubstr,
					"$search rendering; src=%q", c.src)
			}
			if c.wantLocalSubstr != "" {
				require.Contains(t, compiled.Plan.LocalSQL, c.wantLocalSubstr,
					"local SQL rendering; src=%q", c.src)
			}
			if c.wantNotesContains != "" {
				notes := strings.Join(compiled.Plan.Notes, " | ")
				require.Contains(t, notes, c.wantNotesContains,
					"notes for --explain; src=%q got=%q", c.src, notes)
			}
		})
	}
}

// TestCompileExplainOutput pins the multi-line --explain shape.
// Real users see this from `:filter --explain <expr>`; the test
// keeps the layout stable so docs / muscle memory don't drift.
//
// Note: per ast.go, `~N` is FieldUnread (`isRead eq false`) and
// `~U` is FieldRead (`isRead eq true`); the spec table at §3.1
// is consistent with that mapping. Test uses `~N` to anchor the
// "unread" assertion clearly.
func TestCompileExplainOutput(t *testing.T) {
	c, err := Compile("~f newsletter@* & ~N", CompileOptions{})
	require.NoError(t, err)
	out := c.Explain()
	require.Contains(t, out, "Strategy: StrategyServerFilter")
	require.Contains(t, out, "Graph $filter:")
	require.Contains(t, out, "startswith(from/emailAddress/address,'newsletter@')")
	require.Contains(t, out, "isRead eq false")
}

// TestEmitFilterSnapshotShapes covers the spec 08 §9 mapping
// table. Each row asserts the rendered $filter EXACTLY (no
// substring) so a rendering regression breaks the test loud.
func TestEmitFilterSnapshotShapes(t *testing.T) {
	cases := []struct {
		src  string
		want string
	}{
		{src: "~f bob@acme.invalid", want: "(from/emailAddress/address eq 'bob@acme.invalid' or from/emailAddress/name eq 'bob@acme.invalid')"},
		{src: "~f newsletter@*", want: "(startswith(from/emailAddress/address,'newsletter@') or startswith(from/emailAddress/name,'newsletter@'))"},
		{src: "~f *@vendor.invalid", want: "(endswith(from/emailAddress/address,'@vendor.invalid') or endswith(from/emailAddress/name,'@vendor.invalid'))"},
		{src: "~A", want: "hasAttachments eq true"},
		{src: "~U", want: "isRead eq true"},
		{src: "~N", want: "isRead eq false"},
		{src: "~F", want: "flag/flagStatus eq 'flagged'"},
		{src: "~G Work", want: "categories/any(c:c eq 'Work')"},
		{src: "~i high", want: "importance eq 'high'"},
		{src: "~s budget", want: "contains(subject,'budget')"},
	}
	for _, c := range cases {
		root, err := Parse(c.src)
		require.NoError(t, err, "src=%q", c.src)
		got, err := EmitFilter(root)
		require.NoError(t, err, "src=%q", c.src)
		require.Equal(t, c.want, got, "src=%q", c.src)
	}
}

// TestEmitFilterRejectsServerOnlyPredicates is the
// ErrUnsupported invariant: ~b / ~B / ~h must not be silently
// emitted to $filter.
func TestEmitFilterRejectsServerOnlyPredicates(t *testing.T) {
	for _, src := range []string{`~b "action"`, "~B forecast", "~h list-id:x"} {
		root, err := Parse(src)
		require.NoError(t, err, "src=%q", src)
		_, err = EmitFilter(root)
		require.Error(t, err, "src=%q should return ErrUnsupported", src)
		require.True(t, errors.Is(err, ErrUnsupported), "src=%q got=%v", src, err)
	}
}

// TestEmitSearchSnapshotShapes covers the spec 08 §10 mapping
// table.
func TestEmitSearchSnapshotShapes(t *testing.T) {
	cases := []struct {
		src  string
		want string
	}{
		{src: "~s budget", want: "subject:budget"},
		{src: `~b "action required"`, want: `body:"action required"`},
		{src: "~B forecast", want: "forecast"},
		{src: "~f bob@acme.invalid", want: `from:"bob@acme.invalid"`},
		{src: "~A", want: "hasattachment:true"},
		{src: "~G Work", want: "category:Work"},
		{src: "~h list-id:newsletter", want: "list-id:newsletter"},
	}
	for _, c := range cases {
		root, err := Parse(c.src)
		require.NoError(t, err, "src=%q", c.src)
		got, err := EmitSearch(root)
		require.NoError(t, err, "src=%q", c.src)
		require.Equal(t, c.want, got, "src=%q", c.src)
	}
}

// TestEmitSearchRejectsReadFlag is the spec 08 §10.1 invariant:
// ~N / ~U / ~F can't be expressed in $search.
func TestEmitSearchRejectsReadFlag(t *testing.T) {
	for _, src := range []string{"~N", "~U", "~F"} {
		root, err := Parse(src)
		require.NoError(t, err, "src=%q", src)
		_, err = EmitSearch(root)
		require.Error(t, err, "src=%q", src)
		require.True(t, errors.Is(err, ErrUnsupported), "src=%q got=%v", src, err)
	}
}
