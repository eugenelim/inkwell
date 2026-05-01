package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/eugenelim/inkwell/internal/store"
)

// renderAttachmentLines tests (spec 05 §8 / PR 10).

func TestRenderAttachmentLinesLetterPrefixes(t *testing.T) {
	atts := []store.Attachment{
		{ID: "a1", Name: "report.pdf", Size: 1024, ContentType: "application/pdf"},
		{ID: "a2", Name: "image.png", Size: 2048, ContentType: "image/png"},
		{ID: "a3", Name: "data.csv", Size: 512, ContentType: "text/csv"},
	}
	lines := renderAttachmentLines(atts)
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
	require.Nil(t, renderAttachmentLines(nil))
	require.Nil(t, renderAttachmentLines([]store.Attachment{}))
}

func TestRenderAttachmentLinesSingleFileGrammar(t *testing.T) {
	atts := []store.Attachment{{ID: "a1", Name: "sole.txt", Size: 100}}
	lines := renderAttachmentLines(atts)
	require.Contains(t, lines[0], "1 file") // not "1 files"
	require.NotContains(t, lines[0], "1 files")
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
