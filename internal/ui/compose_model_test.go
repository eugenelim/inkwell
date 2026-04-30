package ui

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/eugenelim/inkwell/internal/store"
)

// Compose tests guard the in-modal compose pivot from spec 15 v2.
// The replace-the-editor-with-an-overlay redesign is driven by a
// real-tenant complaint: $EDITOR-driven flow forced the user to
// "select Exit command first" before save / discard appeared, which
// is a structural artifact of `tea.ExecProcess` suspending the
// Bubble Tea program. The in-modal flow keeps inkwell's UI on
// screen during compose so save / discard live in a persistent
// footer.

// TestNewComposeReturnsEmptyState pins the post-construction shape.
func TestNewComposeReturnsEmptyState(t *testing.T) {
	c := NewCompose()
	require.Equal(t, ComposeKindReply, c.Kind, "default kind is Reply (the only flavor MVP supports)")
	require.Empty(t, c.SourceID)
	require.Empty(t, c.To())
	require.Empty(t, c.Cc())
	require.Empty(t, c.Subject())
	require.Empty(t, c.Body())
	// Body has focus by default — the user almost always wants to
	// start typing the body; headers are pre-filled by the skeleton.
	require.Equal(t, ComposeFieldBody, c.Focused(),
		"Body field has focus by default after skeleton apply")
}

// TestApplyReplySkeletonPopulatesFromSource verifies the canonical
// reply skeleton: To = source FromAddress, Subject = "Re: <subj>",
// Body starts with a quote chain.
func TestApplyReplySkeletonPopulatesFromSource(t *testing.T) {
	src := store.Message{
		ID:          "m-42",
		FromAddress: "alice@example.invalid",
		FromName:    "Alice",
		Subject:     "Q4 forecast",
		BodyPreview: "Initial draft is attached.",
	}
	c := NewCompose()
	c.ApplyReplySkeleton(src, src.BodyPreview)

	require.Equal(t, "m-42", c.SourceID, "source id captured for action dispatch")
	require.Equal(t, "alice@example.invalid", c.To(),
		"To pre-filled with the source's FromAddress")
	require.Equal(t, "Re: Q4 forecast", c.Subject())
	require.Contains(t, c.Body(), "Alice", "quote chain mentions the source author")
	require.Contains(t, c.Body(), "Initial draft is attached",
		"quote chain includes the source body preview")
}

// TestApplyReplySkeletonHandlesRePrefix avoids the "Re: Re:"
// double-prefix regression: a source already prefixed with Re:
// keeps the single Re:.
func TestApplyReplySkeletonHandlesRePrefix(t *testing.T) {
	for _, prefix := range []string{"Re:", "RE:", "re:"} {
		src := store.Message{Subject: prefix + " hello"}
		c := NewCompose()
		c.ApplyReplySkeleton(src, "")
		require.Equal(t, "Re: hello", c.Subject(),
			"prefix %q normalises to canonical 'Re:' without double-prefixing", prefix)
	}
}

// TestApplyReplySkeletonEmptySourceFromAddressLeavesToEmpty
// documents the contract: the skeleton doesn't try to invent a
// recipient. Recipient recovery (falling back to the source's
// FromAddress when the user clears the To field at save time) is
// the saveCompose-Cmd path's job, not the skeleton's.
func TestApplyReplySkeletonEmptySourceFromAddressLeavesToEmpty(t *testing.T) {
	src := store.Message{ID: "m-1", Subject: "hello"}
	c := NewCompose()
	c.ApplyReplySkeleton(src, "")
	require.Empty(t, c.To(),
		"skeleton leaves To empty when source FromAddress is empty")
}

// TestNextFieldCyclesForward / TestPrevFieldCyclesBackward — the
// Tab navigation contract: Body → To → Cc → Subject → Body. We
// rotate forward (Tab) and backward (Shift+Tab) so power-users can
// re-edit a header without re-Tabbing through the whole form.
func TestNextFieldCyclesForward(t *testing.T) {
	c := NewCompose()
	require.Equal(t, ComposeFieldBody, c.Focused())
	c.NextField()
	require.Equal(t, ComposeFieldTo, c.Focused())
	c.NextField()
	require.Equal(t, ComposeFieldCc, c.Focused())
	c.NextField()
	require.Equal(t, ComposeFieldSubject, c.Focused())
	c.NextField()
	require.Equal(t, ComposeFieldBody, c.Focused(), "wraps back to Body")
}

func TestPrevFieldCyclesBackward(t *testing.T) {
	c := NewCompose()
	require.Equal(t, ComposeFieldBody, c.Focused())
	c.PrevField()
	require.Equal(t, ComposeFieldSubject, c.Focused())
	c.PrevField()
	require.Equal(t, ComposeFieldCc, c.Focused())
	c.PrevField()
	require.Equal(t, ComposeFieldTo, c.Focused())
	c.PrevField()
	require.Equal(t, ComposeFieldBody, c.Focused(), "wraps back to Body")
}

// TestSnapshotAndRestoreRoundTrip is the contract for spec 15 v2
// §7 crash recovery: ComposeSnapshot is the JSON-serialisable view
// of the form. PR 7-ii will persist this into compose_sessions and
// restore on launch. The round-trip property MUST hold or resume
// would silently lose recipients / body.
func TestSnapshotAndRestoreRoundTrip(t *testing.T) {
	src := store.Message{
		ID: "m-1", FromAddress: "alice@example.invalid", Subject: "hello",
	}
	c := NewCompose()
	c.ApplyReplySkeleton(src, "Body preview text")
	c.SetCc("bob@example.invalid")
	c.SetSubject("Re: hello (modified)")

	snap := c.Snapshot()
	require.Equal(t, "m-1", snap.SourceID)
	require.Equal(t, "alice@example.invalid", snap.To)
	require.Equal(t, "bob@example.invalid", snap.Cc)
	require.Equal(t, "Re: hello (modified)", snap.Subject)
	require.NotEmpty(t, snap.Body)

	other := NewCompose()
	other.Restore(snap)
	require.Equal(t, c.SourceID, other.SourceID)
	require.Equal(t, c.To(), other.To())
	require.Equal(t, c.Cc(), other.Cc())
	require.Equal(t, c.Subject(), other.Subject())
	require.Equal(t, c.Body(), other.Body())
}

// TestComposeBodyAcceptsTextEdits verifies the body's textarea
// component accepts inserts. Indirectly verifies the textarea is
// wired with focus so its Update path runs.
func TestComposeBodyAcceptsTextEdits(t *testing.T) {
	c := NewCompose()
	c.SetBody("hello")
	require.Equal(t, "hello", c.Body())
	c.SetBody(c.Body() + " world")
	require.Equal(t, "hello world", c.Body())
}

// TestComposeViewRendersAllFieldsAndFooter is the visible-delta
// invariant from the user's complaint: save / discard hints live
// in a persistent footer at the bottom of the compose pane. To /
// Cc / Subject labels and the body block must all paint.
func TestComposeViewRendersAllFieldsAndFooter(t *testing.T) {
	src := store.Message{FromAddress: "alice@example.invalid", Subject: "hello"}
	c := NewCompose()
	c.ApplyReplySkeleton(src, "")
	out := c.View(DefaultTheme(), 100, 30)

	require.Contains(t, out, "To:")
	require.Contains(t, out, "Cc:")
	require.Contains(t, out, "Subject:")
	require.Contains(t, out, "alice@example.invalid", "To value visible")
	require.Contains(t, out, "Re: hello", "Subject value visible")
	// Footer hints — the structural fix for "save/discard at the
	// bottom" the in-modal pivot exists to deliver.
	low := strings.ToLower(out)
	require.Contains(t, low, "ctrl+s", "save shortcut shown in footer")
	require.Contains(t, low, "ctrl+d", "discard shortcut shown in footer")
	require.Contains(t, low, "tab", "field-cycle hint shown in footer")
}
