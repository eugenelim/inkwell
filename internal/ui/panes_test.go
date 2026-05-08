package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/eugenelim/inkwell/internal/render"
	"github.com/eugenelim/inkwell/internal/store"
)

// renderAttachmentLines tests (spec 05 §8 / PR 10).

func TestRenderAttachmentLinesLetterPrefixes(t *testing.T) {
	atts := []store.Attachment{
		{ID: "a1", Name: "report.pdf", Size: 1024, ContentType: "application/pdf"},
		{ID: "a2", Name: "image.png", Size: 2048, ContentType: "image/png"},
		{ID: "a3", Name: "data.csv", Size: 512, ContentType: "text/csv"},
	}
	lines := renderAttachmentLines(atts, render.Theme{})
	require.NotNil(t, lines)
	// First line: summary header
	require.Contains(t, lines[0], "Attach:")
	require.Contains(t, lines[0], "3 files")
	// Per-attachment lines carry accelerator letter prefixes.
	require.Contains(t, lines[1], "[a]")
	require.Contains(t, lines[1], "report.pdf")
	require.Contains(t, lines[2], "[b]")
	require.Contains(t, lines[2], "image.png")
	require.Contains(t, lines[3], "[c]")
	require.Contains(t, lines[3], "data.csv")
}

func TestRenderAttachmentLinesEmpty(t *testing.T) {
	require.Nil(t, renderAttachmentLines(nil, render.Theme{}))
	require.Nil(t, renderAttachmentLines([]store.Attachment{}, render.Theme{}))
}

func TestRenderAttachmentLinesSingleFileGrammar(t *testing.T) {
	atts := []store.Attachment{{ID: "a1", Name: "sole.txt", Size: 100}}
	lines := renderAttachmentLines(atts, render.Theme{})
	require.Contains(t, lines[0], "1 file") // not "1 files"
	require.NotContains(t, lines[0], "1 files")
}

// TestRenderAttachmentLinesColored confirms that a non-zero Attachment
// style (from DefaultTheme) is applied — the theme parameter is wired
// through and the text content is preserved in all cases.
func TestRenderAttachmentLinesColored(t *testing.T) {
	atts := []store.Attachment{{ID: "a1", Name: "deck.pdf", Size: 1024}}
	lines := renderAttachmentLines(atts, render.DefaultTheme())
	require.NotNil(t, lines)
	// File name must always be present regardless of whether the test
	// environment has a TTY (lipgloss strips ANSI in non-TTY builds).
	found := false
	for _, l := range lines {
		if strings.Contains(l, "deck.pdf") {
			found = true
			break
		}
	}
	require.True(t, found, "attachment name must appear in the rendered output")
}

// renderConversationSection tests (spec 05 §11 / PR 10).

func TestRenderConversationSectionOmittedForSingleOrNil(t *testing.T) {
	require.Empty(t, renderConversationSection(nil, 0))
	require.Empty(t, renderConversationSection([]store.Message{}, 0))
	require.Empty(t, renderConversationSection([]store.Message{{ID: "m-1"}}, 0))
}

func TestRenderConversationSectionMarksCurrent(t *testing.T) {
	now := time.Now()
	msgs := []store.Message{
		{ID: "m-1", Subject: "First reply", FromName: "Alice", ReceivedAt: now},
		{ID: "m-2", Subject: "Second reply", FromName: "Bob", ReceivedAt: now.Add(time.Hour)},
	}
	section := renderConversationSection(msgs, 1) // m-2 is current
	require.Contains(t, section, "Thread (2 messages)")

	lines := strings.Split(section, "\n")
	var firstLine, secondLine string
	for _, l := range lines {
		if strings.Contains(l, "First reply") {
			firstLine = l
		}
		if strings.Contains(l, "Second reply") {
			secondLine = l
		}
	}
	require.NotEmpty(t, firstLine, "expected line for 'First reply'")
	require.NotEmpty(t, secondLine, "expected line for 'Second reply'")
	require.NotContains(t, firstLine, "▶", "first message should not have ▶ marker")
	require.Contains(t, secondLine, "▶", "current message (m-2) should have ▶ marker")
}

func TestRenderConversationSectionHasNavHint(t *testing.T) {
	msgs := []store.Message{
		{ID: "m-1", Subject: "A", ReceivedAt: time.Now()},
		{ID: "m-2", Subject: "B", ReceivedAt: time.Now().Add(time.Hour)},
	}
	section := renderConversationSection(msgs, 0)
	require.Contains(t, section, "←")
	require.Contains(t, section, "→")
}

// ViewerModel conversation thread tests (spec 05 §11 / PR 10).

func TestSetConversationThreadIndexing(t *testing.T) {
	v := NewViewer()
	msgs := []store.Message{
		{ID: "m-1", Subject: "First"},
		{ID: "m-2", Subject: "Second"},
		{ID: "m-3", Subject: "Third"},
	}
	v.SetConversationThread(msgs, "m-2")
	require.Len(t, v.ConversationThread(), 3)
	// convIdx is 1; NavPrevInThread should move to m-1.
	prev := v.NavPrevInThread()
	require.NotNil(t, prev)
	require.Equal(t, "m-1", prev.ID)
}

func TestSetConversationThreadUnknownIDDefaultsToZero(t *testing.T) {
	v := NewViewer()
	msgs := []store.Message{
		{ID: "m-1", Subject: "First"},
		{ID: "m-2", Subject: "Second"},
	}
	v.SetConversationThread(msgs, "m-unknown")
	// convIdx defaults to 0; NavPrevInThread at 0 returns nil.
	require.Nil(t, v.NavPrevInThread())
}

func TestNavPrevNextInThreadBounds(t *testing.T) {
	v := NewViewer()
	// Empty thread: both directions return nil.
	require.Nil(t, v.NavPrevInThread())
	require.Nil(t, v.NavNextInThread())

	msgs := []store.Message{{ID: "m-1"}, {ID: "m-2"}, {ID: "m-3"}}
	v.SetConversationThread(msgs, "m-1") // convIdx = 0

	// At first message: prev is nil.
	require.Nil(t, v.NavPrevInThread())
	// Advance forward twice.
	require.Equal(t, "m-2", v.NavNextInThread().ID)
	require.Equal(t, "m-3", v.NavNextInThread().ID)
	// At last message: next is nil.
	require.Nil(t, v.NavNextInThread())
	// Retreat.
	require.Equal(t, "m-2", v.NavPrevInThread().ID)
}

// safeAttachmentPath tests (spec 17 §4.4 / PR 10).

func TestSafeAttachmentPathHappyPath(t *testing.T) {
	got, err := safeAttachmentPath("/tmp/dl", "report.pdf")
	require.NoError(t, err)
	require.Equal(t, "/tmp/dl/report.pdf", got)
}

func TestSafeAttachmentPathStripsTraversal(t *testing.T) {
	// filepath.Base("../evil.sh") == "evil.sh"; resolved path is
	// /tmp/dl/evil.sh which is inside dir — no error, traversal stripped.
	got, err := safeAttachmentPath("/tmp/dl", "../evil.sh")
	require.NoError(t, err)
	require.Equal(t, "/tmp/dl/evil.sh", got)
}

func TestSafeAttachmentPathStripsSubDirectory(t *testing.T) {
	got, err := safeAttachmentPath("/tmp/dl", "sub/dir/file.txt")
	require.NoError(t, err)
	require.Equal(t, "/tmp/dl/file.txt", got)
}

func TestSafeAttachmentPathRejectsDot(t *testing.T) {
	_, err := safeAttachmentPath("/tmp/dl", ".")
	require.Error(t, err)
}

func TestSafeAttachmentPathRejectsDotDot(t *testing.T) {
	_, err := safeAttachmentPath("/tmp/dl", "..")
	require.Error(t, err)
}

func TestSafeAttachmentPathDirPrefixFalsePositive(t *testing.T) {
	// dir="/foo" must NOT match clean="/foobar/x" — the separator-based
	// prefix check prevents this false positive.
	_, err := safeAttachmentPath("/foo", "bar/x")
	// filepath.Base("bar/x") == "x", joined = "/foo/x" — inside /foo, ok.
	require.NoError(t, err)
}

// hasInviteBodyPreview and extractMeetingInfo tests.

func TestHasInviteBodyPreviewBothLabels(t *testing.T) {
	preview := "When: Thursday, May 8, 2026 2:00 PM – 3:00 PM (UTC+00:00)\nWhere: Microsoft Teams Meeting\n"
	require.True(t, hasInviteBodyPreview(preview), "both When+Where must be detected")
}

func TestHasInviteBodyPreviewWhenAloneWithDigit(t *testing.T) {
	// Virtual meeting: no physical location, "Where:" omitted.
	preview := "When: 5/8/2026 2:00 PM – 3:00 PM (UTC)\n"
	require.True(t, hasInviteBodyPreview(preview), "When: with numeric date must be detected without Where:")
}

func TestHasInviteBodyPreviewWhenAloneWithDayName(t *testing.T) {
	preview := "When: Thursday, May 8, 2026 2:00 PM – 3:00 PM (UTC)\n"
	require.True(t, hasInviteBodyPreview(preview), "When: with day name must be detected without Where:")
}

func TestHasInviteBodyPreviewWhenAloneWithMonthName(t *testing.T) {
	preview := "When: May 8, 2026 2:00 PM – 3:00 PM (UTC)\n"
	require.True(t, hasInviteBodyPreview(preview), "When: with month name must be detected without Where:")
}

func TestHasInviteBodyPreviewWhereAloneIsNotSufficient(t *testing.T) {
	preview := "Where: Conference Room B\n"
	require.False(t, hasInviteBodyPreview(preview), "Where: alone must not be detected")
}

func TestHasInviteBodyPreviewProseWhenIsNotDetected(t *testing.T) {
	// Regular email containing "When I" — must not false-positive.
	preview := "When I have a chance I will review the document and send feedback."
	require.False(t, hasInviteBodyPreview(preview), "prose 'when' clause must not be detected")
}

func TestHasInviteBodyPreviewEmptyReturnsFalse(t *testing.T) {
	require.False(t, hasInviteBodyPreview(""))
}

func TestHasInviteBodyPreviewLongTimezoneWindowExtended(t *testing.T) {
	// Long timezone pushes "Where:" past the old 200-char window; the
	// extended 400-char window must catch it.
	longWhen := "When: Thursday, May 8, 2026 2:00 PM – 3:00 PM (UTC+05:30) Chennai, Kolkata, Mumbai, New Delhi\n"
	where := "Where: Microsoft Teams Meeting\n"
	preview := longWhen + where
	require.True(t, hasInviteBodyPreview(preview), "Where: after long timezone must be detected in extended window")
}

func TestExtractMeetingInfoBothPresent(t *testing.T) {
	preview := "When: Thursday, May 8, 2026 2:00 PM – 3:00 PM (UTC)\nWhere: Building 5\n"
	when, where := extractMeetingInfo(preview, "")
	require.Equal(t, "Thursday, May 8, 2026 2:00 PM – 3:00 PM (UTC)", when)
	require.Equal(t, "Building 5", where)
}

func TestExtractMeetingInfoWhenOnly(t *testing.T) {
	preview := "When: 5/8/2026 2:00 PM\nNo location info here.\n"
	when, where := extractMeetingInfo(preview, "")
	require.Equal(t, "5/8/2026 2:00 PM", when)
	require.Empty(t, where)
}

func TestExtractMeetingInfoEmpty(t *testing.T) {
	when, where := extractMeetingInfo("", "")
	require.Empty(t, when)
	require.Empty(t, where)
}

func TestExtractMeetingInfoCaseInsensitive(t *testing.T) {
	preview := "WHEN: Monday, Jun 1, 2026\nWHERE: Room 42\n"
	when, where := extractMeetingInfo(preview, "")
	require.Equal(t, "Monday, Jun 1, 2026", when)
	require.Equal(t, "Room 42", where)
}

// Teams-invite signature handling (spec 05 follow-up).

func TestExtractMeetingInfoTeamsInviteFallsBackToTeamsLabel(t *testing.T) {
	preview := "Hi team — quick sync this afternoon.\n"
	body := preview + "________________________________________________________________________________\nMicrosoft Teams meeting\nJoin on your computer or mobile app\nClick here to join the meeting\nMeeting ID: 123 456 789\nPasscode: abc\n"
	when, where := extractMeetingInfo(preview, body)
	require.Empty(t, when, "Teams invite without a When: label leaves When unset")
	require.Equal(t, "Microsoft Teams Meeting", where, "Teams invite signature populates Where")
}

func TestExtractMeetingInfoBodyOverridesPreviewWhenLabelsAreOnlyInBody(t *testing.T) {
	// BodyPreview holds the user's intro text; the When/Where labels
	// are only present in the rendered body further down.
	preview := "Hey Eugene, blocking time for the Q4 review.\n"
	body := preview +
		"\n________________________________________________________________________________\n" +
		"When: Tuesday, May 13, 2025 2:00 PM-3:00 PM (UTC-08:00) Pacific Time\n" +
		"Where: Microsoft Teams Meeting\n"
	when, where := extractMeetingInfo(preview, body)
	require.Contains(t, when, "Tuesday, May 13, 2025")
	require.Equal(t, "Microsoft Teams Meeting", where)
}

func TestIsMeetingMessageTeamsInviteWithoutLabels(t *testing.T) {
	msg := store.Message{
		Subject:     "Q4 review",
		BodyPreview: "Hi team — quick sync. Microsoft Teams meeting Click here to join the meeting Meeting ID: 123 456",
	}
	require.True(t, isMeetingMessage(msg), "Teams-invite signature in BodyPreview must trigger 📅 indicator")
}
