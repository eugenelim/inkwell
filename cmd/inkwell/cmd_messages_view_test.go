package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Spec 31 §8.1 — applyMessagesViewFlag is the pure helper that
// translates --view into (folder, filter). Cobra wires the helper +
// os.Exit(2) on error; unit tests below exercise the helper directly.

func TestMessagesViewFocused(t *testing.T) {
	folder, filter, err := applyMessagesViewFlag("focused", "", "")
	require.NoError(t, err)
	require.Equal(t, "Inbox", folder)
	require.Equal(t, "~y focused", filter)
}

func TestMessagesViewOther(t *testing.T) {
	folder, filter, err := applyMessagesViewFlag("other", "", "")
	require.NoError(t, err)
	require.Equal(t, "Inbox", folder)
	require.Equal(t, "~y other", filter)
}

func TestMessagesViewRejectsUnknownValue(t *testing.T) {
	for _, bad := range []string{"FOCUSED", "all", "spam", "primary"} {
		_, _, err := applyMessagesViewFlag(bad, "", "")
		require.Error(t, err, "value %q must be rejected", bad)
		require.Contains(t, err.Error(), "--view must be one of")
	}
}

func TestMessagesViewWithNonInboxFolderErrors(t *testing.T) {
	_, _, err := applyMessagesViewFlag("focused", "Sent", "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "requires --folder Inbox")
	require.Contains(t, strings.ToLower(err.Error()), "got")
}

func TestMessagesViewWithInboxFolderOk(t *testing.T) {
	for _, ok := range []string{"Inbox", "inbox", "INBOX"} {
		folder, filter, err := applyMessagesViewFlag("focused", ok, "")
		require.NoError(t, err)
		require.Equal(t, "Inbox", folder)
		require.Equal(t, "~y focused", filter)
	}
}

func TestMessagesViewCombinesWithFilter(t *testing.T) {
	_, filter, err := applyMessagesViewFlag("focused", "", "~d <7d")
	require.NoError(t, err)
	require.Equal(t, "(~y focused) & (~d <7d)", filter)
}
