package action

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/eugenelim/inkwell/internal/store"
)

// TestInverseRoundTripsToggleActions verifies the symmetric pairs:
// mark_read ↔ mark_unread, flag ↔ unflag, add_category ↔
// remove_category. Pressing `u` after one of these must produce its
// twin.
func TestInverseRoundTripsToggleActions(t *testing.T) {
	cases := []struct {
		name     string
		original store.ActionType
		expected store.ActionType
	}{
		{"mark_read→mark_unread", store.ActionMarkRead, store.ActionMarkUnread},
		{"mark_unread→mark_read", store.ActionMarkUnread, store.ActionMarkRead},
		{"flag→unflag", store.ActionFlag, store.ActionUnflag},
		{"unflag→flag", store.ActionUnflag, store.ActionFlag},
		{"add_category→remove_category", store.ActionAddCategory, store.ActionRemoveCategory},
		{"remove_category→add_category", store.ActionRemoveCategory, store.ActionAddCategory},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := store.Action{
				Type:       tc.original,
				MessageIDs: []string{"m-1"},
				Params:     map[string]any{"category": "Q4"},
			}
			entry, ok := Inverse(a, &store.Message{ID: "m-1"})
			require.True(t, ok, "%s must be reversible", tc.name)
			require.Equal(t, tc.expected, entry.ActionType)
			require.Equal(t, []string{"m-1"}, entry.MessageIDs)
		})
	}
}

// TestInverseMoveRestoresSourceFolder is the spec 07 §11 invariant
// for move / soft_delete: the inverse uses the snapshot's FolderID
// to pin the destination, so undoing a delete restores to the
// original folder regardless of where the user moved next.
func TestInverseMoveRestoresSourceFolder(t *testing.T) {
	pre := &store.Message{ID: "m-1", FolderID: "f-inbox"}
	a := store.Action{
		Type:       store.ActionSoftDelete,
		MessageIDs: []string{"m-1"},
		Params:     map[string]any{"destination_folder_id": "f-deleted"},
	}
	entry, ok := Inverse(a, pre)
	require.True(t, ok)
	require.Equal(t, store.ActionMove, entry.ActionType)
	require.Equal(t, "f-inbox", entry.Params["destination_folder_id"],
		"inverse of soft_delete must restore to the snapshot's source folder")
	require.Equal(t, "deleted", entry.Label)
}

// TestInverseMoveRequiresSnapshot verifies the safety: without a
// pre-snapshot we don't know where to move back to, so the inverse
// is non-reversible. The executor must NOT push such an entry.
func TestInverseMoveRequiresSnapshot(t *testing.T) {
	a := store.Action{Type: store.ActionMove, MessageIDs: []string{"m-1"}}
	_, ok := Inverse(a, nil)
	require.False(t, ok, "move without pre-snapshot is not reversible")
}

// TestInversePermanentDeleteIsNotReversible is the spec 07 §11
// invariant: once Graph honours permanent delete the message is
// gone from the tenant. Undo must NOT pretend to roll it back.
func TestInversePermanentDeleteIsNotReversible(t *testing.T) {
	a := store.Action{Type: store.ActionPermanentDelete, MessageIDs: []string{"m-1"}}
	_, ok := Inverse(a, &store.Message{ID: "m-1"})
	require.False(t, ok, "permanent_delete must not be reversible")
}

// TestInverseUnknownActionIsNotReversible covers defensive defaults —
// any future action not yet in the switch returns ok=false rather
// than panicking or pushing garbage.
func TestInverseUnknownActionIsNotReversible(t *testing.T) {
	a := store.Action{Type: store.ActionType("not_a_real_action"), MessageIDs: []string{"m-1"}}
	_, ok := Inverse(a, nil)
	require.False(t, ok)
}
