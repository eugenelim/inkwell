package compose

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/eugenelim/inkwell/internal/store"
)

func TestReplySkeletonPopulatesHeaders(t *testing.T) {
	src := store.Message{
		Subject:     "Q4 forecast",
		FromName:    "Bob",
		FromAddress: "bob@vendor.invalid",
		SentAt:      time.Date(2026, 4, 29, 14, 32, 0, 0, time.UTC),
	}
	got := ReplySkeleton(src, "")
	require.Contains(t, got, "To: bob@vendor.invalid")
	require.Contains(t, got, "Cc:")
	require.Contains(t, got, "Subject: Re: Q4 forecast")
}

func TestReplySkeletonPreservesExistingRePrefix(t *testing.T) {
	src := store.Message{
		Subject:     "Re: Q4 forecast",
		FromAddress: "bob@vendor.invalid",
	}
	got := ReplySkeleton(src, "")
	require.Contains(t, got, "Subject: Re: Q4 forecast")
	require.NotContains(t, got, "Re: Re:", "must not double-prefix")
}

func TestReplySkeletonQuotesBody(t *testing.T) {
	src := store.Message{
		Subject:     "x",
		FromAddress: "b@x",
	}
	got := ReplySkeleton(src, "Hey team,\nsee attached.\n")
	require.Contains(t, got, "> Hey team,")
	require.Contains(t, got, "> see attached.")
}

func TestReplySkeletonEmptyBody(t *testing.T) {
	src := store.Message{Subject: "x", FromAddress: "b@x"}
	got := ReplySkeleton(src, "")
	require.NotContains(t, got, "> ", "no quote prefix when body is empty")
}

// Used by parse tests to sanity-check the round-trip shape.
func TestSkeletonHasBlankLineBeforeBody(t *testing.T) {
	src := store.Message{Subject: "x", FromAddress: "b@x"}
	got := ReplySkeleton(src, "hi")
	require.Contains(t, got, "\n\n", "headers and body separated by blank line")

	headers := got[:strings.Index(got, "\n\n")]
	require.Contains(t, headers, "To:")
	require.Contains(t, headers, "Subject:")
	require.NotContains(t, headers, ">")
}

// TestReplyAllSkeletonPopulatesAllRecipients covers the spec 15
// §6.2 reply-all rule: To carries source.From + remaining To
// recipients; Cc carries source.Cc; the user's own UPN is
// filtered out so they don't email themselves.
func TestReplyAllSkeletonPopulatesAllRecipients(t *testing.T) {
	src := store.Message{
		Subject:     "Q4 forecast",
		FromName:    "Bob",
		FromAddress: "bob@vendor.invalid",
		ToAddresses: []store.EmailAddress{
			{Address: "alice@example.invalid"},
			{Address: "self@example.invalid"},
		},
		CcAddresses: []store.EmailAddress{
			{Address: "carol@example.invalid"},
		},
	}
	got := ReplyAllSkeleton(src, "", "self@example.invalid")
	require.Contains(t, got, "bob@vendor.invalid", "From becomes part of To")
	require.Contains(t, got, "alice@example.invalid", "remaining To recipients carry over")
	require.NotContains(t, got, "self@example.invalid", "user's own UPN filtered out")
	require.Contains(t, got, "carol@example.invalid", "Cc carries over")
	require.Contains(t, got, "Subject: Re: Q4 forecast")
}

// TestReplyAllSkeletonDedupesAddresses confirms duplicate
// addresses across To/Cc are collapsed to a single entry.
func TestReplyAllSkeletonDedupesAddresses(t *testing.T) {
	src := store.Message{
		FromAddress: "bob@vendor.invalid",
		ToAddresses: []store.EmailAddress{
			{Address: "alice@example.invalid"},
			{Address: "alice@example.invalid"},
		},
	}
	got := ReplyAllSkeleton(src, "", "")
	require.Equal(t, 1, strings.Count(got, "alice@example.invalid"),
		"duplicate To addresses must dedup")
}

// TestForwardSkeletonHasForwardHeaderBlock verifies the canonical
// forward shape: Subject prefixed "Fwd:", To/Cc empty, body opens
// with the "Forwarded message" header block + source body.
func TestForwardSkeletonHasForwardHeaderBlock(t *testing.T) {
	src := store.Message{
		Subject:     "Q4 forecast",
		FromName:    "Bob",
		FromAddress: "bob@vendor.invalid",
		SentAt:      time.Date(2026, 4, 29, 14, 32, 0, 0, time.UTC),
	}
	got := ForwardSkeleton(src, "the original body\n")
	require.Contains(t, got, "Subject: Fwd: Q4 forecast")
	require.Contains(t, got, "---------- Forwarded message ----------")
	require.Contains(t, got, "From:    Bob <bob@vendor.invalid>")
	require.Contains(t, got, "Date:    Wed 2026-04-29 14:32")
	require.Contains(t, got, "the original body")
	// Body must NOT be quote-prefixed (forwards show source verbatim).
	require.NotContains(t, got, "> the original body")
}

// TestForwardSkeletonPreservesExistingFwdPrefix accepts both
// "Fwd:" and "Fw:" forms and normalises the OUTER subject to
// "Fwd:" without stacking. The forwarded-block's inner "Subject:"
// preserves the source verbatim (so receivers see the original
// subject as it was), so a "Fwd: x" source produces two distinct
// occurrences of "Fwd:" — one outer normalised, one inner verbatim.
func TestForwardSkeletonPreservesExistingFwdPrefix(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Fwd: x", "Subject: Fwd: x"},
		{"Fw: x", "Subject: Fwd: x"},
		{"x", "Subject: Fwd: x"},
	}
	for _, c := range cases {
		src := store.Message{Subject: c.in, FromAddress: "b@x"}
		got := ForwardSkeleton(src, "")
		require.Contains(t, got, c.want, "in=%q", c.in)
		require.NotContains(t, got, "Fwd: Fwd:", "no Fwd: stacking; in=%q", c.in)
		require.NotContains(t, got, "Fwd: Fw:", "no Fwd: stacking; in=%q", c.in)
	}
}

// TestNewSkeletonIsBlank confirms the new-message path produces
// only empty header lines + the blank separator.
func TestNewSkeletonIsBlank(t *testing.T) {
	got := NewSkeleton()
	require.Contains(t, got, "To:\n")
	require.Contains(t, got, "Cc:\n")
	require.Contains(t, got, "Subject:\n")
	require.NotContains(t, got, ">", "no quote chain in a new draft")
}
