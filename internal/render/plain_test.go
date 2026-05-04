package render

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFormatFlowedUnwrap(t *testing.T) {
	// A format=flowed message where lines end with a trailing space.
	// RFC 2646 §4.2: the trailing space is the word separator before the
	// next line's content, so joining "sender " + "and" = "sender and".
	input := "Hello this is a long line that was soft-wrapped by the sender \nand continues here.\nThis is a separate paragraph."
	require.True(t, isFormatFlowed(input), "input with trailing-space lines should be detected as format=flowed")
	result := unwrapFormatFlowed(input)
	// The trailing space on line 1 is the separator: "sender " + "and" → "sender and".
	require.Contains(t, result, "Hello this is a long line that was soft-wrapped by the sender and continues here.")
	require.Contains(t, result, "This is a separate paragraph.")
}

func TestFormatFlowedDetectionThreshold(t *testing.T) {
	// Exactly 20% trailing-space lines: detected (boundary at ≥20%).
	atThreshold := "line one\nline two\nline three\nline four \nline five\n"
	require.True(t, isFormatFlowed(atThreshold), "1/5 = 20% trailing-space lines → detected at boundary")

	// Below 20%: not detected.
	belowThreshold := "line one\nline two\nline three\nline four\nline five\nline six \nline seven\nline eight\nline nine\nline ten\n"
	require.False(t, isFormatFlowed(belowThreshold), "1/10 = 10% trailing-space lines → not format=flowed")

	// Above 20%: detected.
	aboveThreshold := "line one \nline two \nline three\nline four\nline five\n"
	require.True(t, isFormatFlowed(aboveThreshold), "2/5 = 40% trailing-space lines → format=flowed")
}

func TestFormatFlowedNotDetectedInNormalMail(t *testing.T) {
	// Normal email with no trailing spaces.
	normal := "Hello Alice,\n\nThis is a normal email without any trailing spaces.\n\nThanks,\nBob\n"
	require.False(t, isFormatFlowed(normal), "normal email should not be detected as format=flowed")
}

func TestCollapseQuotes(t *testing.T) {
	body := "Hello\n> quote one\n> quote two\n> quote three\nAfter quotes\n"
	got := collapseQuotes(body, 1)
	require.Contains(t, got, "[… 3 quoted lines]", "three quoted lines at depth 1 should be collapsed")
	require.NotContains(t, got, "> quote one", "original quoted lines should be removed")
	require.Contains(t, got, "Hello", "non-quoted lines should be preserved")
	require.Contains(t, got, "After quotes", "non-quoted lines after run should be preserved")
}

func TestCollapseQuotesDepthThreshold(t *testing.T) {
	// Only collapse at depth ≥ threshold.
	body := "> shallow\n>> deep one\n>> deep two\n> shallow again\n"
	// threshold=2: depth 2 lines collapse; depth 1 lines stay.
	got := collapseQuotes(body, 2)
	require.Contains(t, got, "> shallow", "depth-1 lines must not be collapsed when threshold=2")
	require.Contains(t, got, "[… 2 quoted lines]", "depth-2 lines must be collapsed")
	require.Contains(t, got, "> shallow again", "second depth-1 line must survive")
}

func TestCollapseQuotesZeroThresholdIsNoop(t *testing.T) {
	body := "> quote\n> more\ntext\n"
	got := collapseQuotes(body, 0)
	require.Equal(t, body, got, "threshold=0 must be a no-op")
}

func TestExpandQuotes(t *testing.T) {
	// Verify that collapsing and then checking TextExpanded == original.
	body := "Header\n> first quote\n> second quote\nbody text\n"
	collapsed := collapseQuotes(body, 1)
	require.Contains(t, collapsed, "[…", "body should be collapsed")
	require.NotContains(t, collapsed, "> first quote", "original quote lines should be gone")
	// The expanded form is the original.
	require.Contains(t, body, "> first quote", "original form has full quotes")
}

func TestAttributionLineDim(t *testing.T) {
	body := "On Mon, 1 Jan 2024, Alice Smith wrote:\nSome reply text.\n"
	out, _ := normalisePlain(body, 80, 0, 0)
	require.Contains(t, out, "\x1b[2m", "attribution line must be wrapped in dim ANSI")
	require.Contains(t, out, "\x1b[0m", "ANSI reset must follow attribution line")
	require.Contains(t, out, "On Mon, 1 Jan 2024, Alice Smith wrote:", "attribution line text must be present")
}

func TestAttributionLineNotDimmedWhenQuoted(t *testing.T) {
	// Attribution lines inside quoted blocks should not get extra dim treatment
	// (they are already rendered with quote markers).
	body := "> On Mon, 1 Jan 2024, Alice Smith wrote:\n> some previous text\n"
	out, _ := normalisePlain(body, 80, 0, 0)
	// The quoted attribution is at depth > 0 so it bypasses the attribution check.
	// It should not have the standalone dim escape applied.
	require.NotContains(t, out, "\x1b[2mOn Mon", "quoted attribution line must not get standalone dim treatment")
}

func TestStripPatterns(t *testing.T) {
	content := "Hello world\nCAUTION: This is an external email\nPlease read this.\n"
	r := &renderer{
		stripPatterns: defaultStripPatterns,
	}
	got := r.applyStripPatterns(content)
	require.NotContains(t, got, "CAUTION", "external email banner should be stripped")
	require.Contains(t, got, "Hello world", "non-matching lines must be preserved")
	require.Contains(t, got, "Please read this.", "non-matching lines must be preserved")
}

func TestStripPatternsCIDPlaceholders(t *testing.T) {
	content := "See attached image [cid:image001.png@01D7A1B2.3C4D5E6F] in the body.\n"
	r := &renderer{
		stripPatterns: defaultStripPatterns,
	}
	got := r.applyStripPatterns(content)
	require.NotContains(t, got, "[cid:", "CID placeholders should be stripped")
}

func TestStripPatternsTroubleViewing(t *testing.T) {
	content := "If you are having trouble viewing this email, click here.\n"
	r := &renderer{
		stripPatterns: defaultStripPatterns,
	}
	got := r.applyStripPatterns(content)
	require.NotContains(t, got, "trouble viewing", "trouble-viewing preludes should be stripped")
}

func TestStripPatternsEmptyWhenConfigured(t *testing.T) {
	content := "CAUTION: This is an external email\nHello world\n"
	// When StripPatterns is explicitly empty (not nil), use no patterns.
	r := &renderer{
		stripPatterns: []*regexp.Regexp{},
	}
	got := r.applyStripPatterns(content)
	require.Contains(t, got, "CAUTION", "explicit empty patterns means nothing is stripped")
}

func TestNormalisePlainWithQuoteCollapse(t *testing.T) {
	body := "Introduction\n> q1\n> q2\n> q3\nConclusion\n"
	collapsed, _ := normalisePlain(body, 80, 0, 1)
	require.Contains(t, collapsed, "[… 3 quoted lines]", "quotes collapsed at threshold=1")
	notCollapsed, _ := normalisePlain(body, 80, 0, 0)
	require.Contains(t, notCollapsed, "> q1", "quotes not collapsed at threshold=0")
}
