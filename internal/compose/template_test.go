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
