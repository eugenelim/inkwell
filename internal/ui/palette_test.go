package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/require"
)

// makeRows builds a small test row table without going through
// collectPaletteRows so the matcher tests don't depend on Model.
func makeRows(specs []rowSpec) ([]PaletteRow, []paletteRowCache) {
	rows := make([]PaletteRow, len(specs))
	for i, s := range specs {
		rows[i] = PaletteRow{
			ID: s.id, Title: s.title, Section: s.section,
			Synonyms: s.synonyms, Available: Availability{OK: s.ok || !s.unavail},
		}
		if s.unavail {
			rows[i].Available = Availability{OK: false, Why: "test"}
		}
	}
	return rows, buildRowCaches(rows)
}

type rowSpec struct {
	id       string
	title    string
	section  string
	synonyms []string
	ok       bool
	unavail  bool
}

func TestMatchSubsequence(t *testing.T) {
	rows, caches := makeRows([]rowSpec{
		{id: "archive", title: "Archive message", section: sectionCommands, ok: true},
		{id: "unsubscribe", title: "Unsubscribe (RFC 8058)", section: sectionCommands, ok: true},
	})
	got := matchAndScore(rows, caches, "arc", nil)
	require.NotEmpty(t, got)
	require.Equal(t, "archive", got[0].row.ID)
}

func TestMatchExcludesNonMatch(t *testing.T) {
	rows, caches := makeRows([]rowSpec{
		{id: "archive", title: "Archive message", section: sectionCommands, ok: true},
	})
	got := matchAndScore(rows, caches, "xyzq", nil)
	require.Empty(t, got)
}

func TestMatchPrefixBonus(t *testing.T) {
	rows, caches := makeRows([]rowSpec{
		{id: "archive", title: "Archive", section: sectionCommands, ok: true},
		{id: "search", title: "Search messages", section: sectionCommands, ok: true},
	})
	// "arc" must rank the prefix-matching row first.
	got := matchAndScore(rows, caches, "arc", nil)
	require.GreaterOrEqual(t, len(got), 1)
	require.Equal(t, "archive", got[0].row.ID)
}

func TestMatchTitleOutranksSynonym(t *testing.T) {
	// A query that hits both a Title rune and a Synonym rune should
	// score higher when the match falls in the title (verifies the
	// titleEnd boundary contributes the +10 in-title bonus).
	rows, caches := makeRows([]rowSpec{
		{id: "archive", title: "Archive message", section: sectionCommands,
			synonyms: []string{"trash"}, ok: true},
		{id: "delete", title: "Delete message", section: sectionCommands,
			synonyms: []string{"archive"}, ok: true},
	})
	got := matchAndScore(rows, caches, "arch", nil)
	require.NotEmpty(t, got)
	require.Equal(t, "archive", got[0].row.ID,
		"title hit must outrank synonym hit (got order: %v)", idsOf(got))
}

func TestMatchConsecutiveBonus(t *testing.T) {
	rows, caches := makeRows([]rowSpec{
		{id: "ab", title: "abxxx", section: sectionCommands, ok: true},
		{id: "cd", title: "axbxx", section: sectionCommands, ok: true},
	})
	got := matchAndScore(rows, caches, "ab", nil)
	require.NotEmpty(t, got)
	require.Equal(t, "ab", got[0].row.ID, "consecutive hit must outrank skipped hit")
}

func TestMatchUnavailablePenalty(t *testing.T) {
	rows, caches := makeRows([]rowSpec{
		{id: "a", title: "test command", section: sectionCommands, unavail: true},
		{id: "b", title: "test command", section: sectionCommands, ok: true},
	})
	got := matchAndScore(rows, caches, "test", nil)
	require.Len(t, got, 2)
	require.Equal(t, "b", got[0].row.ID, "available row must outrank unavailable")
}

func TestRecencyBoost(t *testing.T) {
	rows, caches := makeRows([]rowSpec{
		{id: "a", title: "alpha", section: sectionCommands, ok: true},
		{id: "b", title: "alpha beta", section: sectionCommands, ok: true},
	})
	got := matchAndScore(rows, caches, "a", []string{"b"})
	require.NotEmpty(t, got)
	// recents puts "b" ahead despite the prefix bonus going to "a".
	require.Equal(t, "b", got[0].row.ID,
		"recents must outrank fresh prefix match (got: %v)", idsOf(got))
}

func TestRecordRecentMRU(t *testing.T) {
	var p PaletteModel
	for _, id := range []string{"a", "b", "c", "a"} {
		p.recordRecent(id)
	}
	require.Equal(t, []string{"a", "c", "b"}, p.Recents(),
		"recordRecent must move-to-front and dedup")
}

func TestRecordRecentCap(t *testing.T) {
	var p PaletteModel
	ids := []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"}
	for _, id := range ids {
		p.recordRecent(id)
	}
	require.Len(t, p.Recents(), paletteRecentsCap)
	require.Equal(t, "j", p.Recents()[0], "newest entry goes first")
}

func TestSigilDetection(t *testing.T) {
	cases := []struct {
		buf  string
		want paletteScope
	}{
		{"", scopeMixed},
		{"abc", scopeMixed},
		{"#inbox", scopeFolders},
		{"@news", scopeSavedSearches},
		{">archive", scopeCommands},
		{"/literal", scopeMixed},
	}
	var p PaletteModel
	for _, c := range cases {
		require.Equal(t, c.want, p.detectScope(c.buf), "buf=%q", c.buf)
	}
}

func TestSigilSwitchResetsCursor(t *testing.T) {
	rows, _ := makeRows([]rowSpec{
		{id: "a", title: "alpha command", section: sectionCommands, ok: true},
		{id: "b", title: "beta command", section: sectionCommands, ok: true},
		{id: "f1", title: "Inbox", section: sectionFolders, ok: true},
		{id: "f2", title: "Archive", section: sectionFolders, ok: true},
	})
	var p PaletteModel
	p.rows = rows
	p.caches = buildRowCaches(rows)
	p.refilter()
	p.cursor = 1
	p.AppendRunes([]rune("#"))
	require.Equal(t, 0, p.Cursor(), "cursor must reset after sigil switch")
	require.Equal(t, scopeFolders, p.Scope())
}

func TestBackspacePastSigilReturnsToMixed(t *testing.T) {
	rows, _ := makeRows([]rowSpec{
		{id: "a", title: "alpha", section: sectionCommands, ok: true},
		{id: "f1", title: "Inbox", section: sectionFolders, ok: true},
	})
	var p PaletteModel
	p.rows = rows
	p.caches = buildRowCaches(rows)
	p.refilter()
	p.AppendRunes([]rune("#"))
	require.Equal(t, scopeFolders, p.Scope())
	p.Backspace()
	require.Equal(t, "", p.Buffer())
	require.Equal(t, scopeMixed, p.Scope())
}

func TestBackspaceAtEmptyBufferIsNoop(t *testing.T) {
	var p PaletteModel
	rows, _ := makeRows([]rowSpec{{id: "a", title: "alpha", section: sectionCommands, ok: true}})
	p.rows = rows
	p.caches = buildRowCaches(rows)
	p.refilter()
	before := p.Buffer()
	p.Backspace()
	require.Equal(t, before, p.Buffer())
	require.Equal(t, scopeMixed, p.Scope())
}

func TestSigilGreaterScopesToCommandsOnly(t *testing.T) {
	rows, _ := makeRows([]rowSpec{
		{id: "archive", title: "Archive", section: sectionCommands, ok: true},
		{id: "f1", title: "Archive folder", section: sectionFolders, ok: true},
	})
	var p PaletteModel
	p.rows = rows
	p.caches = buildRowCaches(rows)
	p.AppendRunes([]rune(">archive"))
	for _, sr := range p.Filtered() {
		require.Equal(t, sectionCommands, sr.row.Section,
			"`>` sigil must yield commands-only results, got section %q", sr.row.Section)
	}
}

func TestSlashIsLiteral(t *testing.T) {
	rows, _ := makeRows([]rowSpec{
		{id: "a", title: "Inbox / Project / Q4", section: sectionFolders, ok: true},
	})
	var p PaletteModel
	p.rows = rows
	p.caches = buildRowCaches(rows)
	p.AppendRunes([]rune("/"))
	require.Equal(t, "/", p.Buffer())
	require.Equal(t, scopeMixed, p.Scope())
	// "/" matches the rune in the path label.
	require.NotEmpty(t, p.Filtered())
}

func TestEmptyBufferShowsRecentsFirst(t *testing.T) {
	rows, _ := makeRows([]rowSpec{
		{id: "a", title: "alpha", section: sectionCommands, ok: true},
		{id: "b", title: "beta", section: sectionCommands, ok: true},
		{id: "c", title: "gamma", section: sectionCommands, ok: true},
	})
	var p PaletteModel
	p.rows = rows
	p.caches = buildRowCaches(rows)
	p.recents = []string{"c", "a"}
	p.refilter()
	require.GreaterOrEqual(t, len(p.Filtered()), 2)
	require.Equal(t, "c", p.Filtered()[0].row.ID)
	require.Equal(t, "a", p.Filtered()[1].row.ID)
}

func TestPaletteWindow(t *testing.T) {
	cases := []struct {
		cursor, total, max int
		wantStart, wantEnd int
	}{
		{0, 5, 10, 0, 5},
		{0, 20, 5, 0, 5},
		{4, 20, 5, 0, 5},
		{5, 20, 5, 1, 6},
		{19, 20, 5, 15, 20},
	}
	for _, c := range cases {
		s, e := paletteWindow(c.cursor, c.total, c.max)
		require.Equal(t, c.wantStart, s, "case=%+v", c)
		require.Equal(t, c.wantEnd, e, "case=%+v", c)
	}
}

func TestTruncateToWidth(t *testing.T) {
	require.Equal(t, "hello", truncateToWidth("hello", 10))
	out := truncateToWidth("abcdefghij", 5)
	require.LessOrEqual(t, lipgloss.Width(out), 5)
	require.True(t, strings.HasSuffix(out, "…"))
}

func idsOf(rows []scoredRow) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.row.ID
	}
	return out
}
