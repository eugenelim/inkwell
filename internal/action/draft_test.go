package action

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/eugenelim/inkwell/internal/store"
)

// --- safeReadFile tests ---

// TestSafeReadFileSuccess verifies a regular, size-within-limit file
// is read successfully.
func TestSafeReadFileSuccess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")
	require.NoError(t, os.WriteFile(path, []byte("hello"), 0600))

	data, err := safeReadFile(path, 1024)
	require.NoError(t, err)
	require.Equal(t, []byte("hello"), data)
}

// TestSafeReadFileRelativePathRejected ensures relative paths are
// rejected (spec 17 §4.4 path-traversal guard).
func TestSafeReadFileRelativePathRejected(t *testing.T) {
	_, err := safeReadFile("relative/path/file.txt", 1024)
	require.Error(t, err)
	require.Contains(t, err.Error(), "absolute")
}

// TestSafeReadFilePathTraversalRejected ensures ".." components are
// rejected even in an absolute path. filepath.Join cleans paths, so
// we build the dirty path with string concatenation to preserve "..".
func TestSafeReadFilePathTraversalRejected(t *testing.T) {
	dir := t.TempDir()
	// Build a path that filepath.Clean would alter: dir/subdir/../file.txt
	traversal := dir + "/subdir/../file.txt"
	_, err := safeReadFile(traversal, 1024)
	require.Error(t, err)
	require.Contains(t, err.Error(), "traversal")
}

// TestSafeReadFileSymlinkRejected verifies symlinks are rejected.
// os.Lstat detects them without following the link.
func TestSafeReadFileSymlinkRejected(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real.txt")
	link := filepath.Join(dir, "link.txt")
	require.NoError(t, os.WriteFile(target, []byte("data"), 0600))
	require.NoError(t, os.Symlink(target, link))

	_, err := safeReadFile(link, 1024)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not a regular file")
}

// TestSafeReadFileTooLarge verifies the size gate rejects files
// exceeding maxBytes.
func TestSafeReadFileTooLarge(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.bin")
	require.NoError(t, os.WriteFile(path, make([]byte, 100), 0600))

	_, err := safeReadFile(path, 50)
	require.Error(t, err)
	require.Contains(t, err.Error(), "exceeds")
}

// --- DiscardDraft tests ---

// TestDiscardDraftSuccess verifies DiscardDraft issues DELETE to
// /me/messages/{id} and records the action as Done.
func TestDiscardDraftSuccess(t *testing.T) {
	exec, st, accID, srv := newTestExec(t)
	var deletedID string
	srv.Config.Handler.(*http.ServeMux).HandleFunc("/me/messages/draft-del", func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodDelete, r.Method)
		deletedID = "draft-del"
		w.WriteHeader(http.StatusNoContent)
	})

	err := exec.DiscardDraft(context.Background(), accID, "draft-del")
	require.NoError(t, err)
	require.Equal(t, "draft-del", deletedID)

	// Action row must be Done.
	actions, err := st.PendingActions(context.Background())
	require.NoError(t, err)
	require.Empty(t, actions, "done discard_draft must not stay Pending")
}

// TestDiscardDraftNotFoundIsSuccess verifies 404 from Graph is
// treated as success (idempotent: draft already gone).
func TestDiscardDraftNotFoundIsSuccess(t *testing.T) {
	exec, _, accID, srv := newTestExec(t)
	srv.Config.Handler.(*http.ServeMux).HandleFunc("/me/messages/draft-gone", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"error":{"code":"ErrorItemNotFound"}}`)
	})

	require.NoError(t, exec.DiscardDraft(context.Background(), accID, "draft-gone"))
}

// TestDiscardDraftGraphFailureRecordsActionFailed verifies a Graph
// error causes the action row to be marked Failed and the error
// surfaces to the caller.
func TestDiscardDraftGraphFailureRecordsActionFailed(t *testing.T) {
	exec, st, accID, srv := newTestExec(t)
	srv.Config.Handler.(*http.ServeMux).HandleFunc("/me/messages/draft-fail", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"error":{"code":"InternalServerError"}}`)
	})

	err := exec.DiscardDraft(context.Background(), accID, "draft-fail")
	require.Error(t, err)

	// Action row must be Failed (not Pending or Done).
	rows, dbErr := st.ListActionsByType(context.Background(), store.ActionDiscardDraft)
	require.NoError(t, dbErr)
	require.Len(t, rows, 1)
	require.Equal(t, store.StatusFailed, rows[0].Status)
}

// TestDiscardDraftEmptyIDReturnsError verifies that an empty draftID
// is rejected locally without any Graph call.
func TestDiscardDraftEmptyIDReturnsError(t *testing.T) {
	exec, _, accID, _ := newTestExec(t)
	err := exec.DiscardDraft(context.Background(), accID, "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "empty draft id")
}

// TestUploadAttachmentsSuccess verifies that uploadAttachments posts
// each file's bytes to /me/messages/{id}/attachments.
func TestUploadAttachmentsSuccess(t *testing.T) {
	exec, _, _, srv := newTestExec(t)
	dir := t.TempDir()

	// Write two small test files.
	file1 := filepath.Join(dir, "a.txt")
	file2 := filepath.Join(dir, "b.txt")
	require.NoError(t, os.WriteFile(file1, []byte("alpha"), 0600))
	require.NoError(t, os.WriteFile(file2, []byte("beta"), 0600))

	var postCount int
	srv.Config.Handler.(*http.ServeMux).HandleFunc("/me/messages/draftX/attachments", func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		postCount++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = fmt.Fprintf(w, `{"id":"att-%d"}`, postCount)
	})

	err := exec.uploadAttachments(context.Background(), "draftX", []AttachmentRef{
		{LocalPath: file1, Name: "a.txt", SizeBytes: 5},
		{LocalPath: file2, Name: "b.txt", SizeBytes: 4},
	})
	require.NoError(t, err)
	require.Equal(t, 2, postCount, "one POST per attachment")
}
