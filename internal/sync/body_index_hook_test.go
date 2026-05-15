package sync

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	ilog "github.com/eugenelim/inkwell/internal/log"
	"github.com/eugenelim/inkwell/internal/store"
)

// TestMaybeIndexBody_RedactsNoSensitiveFields enforces spec 35 §8.5:
// the body-index write-error log site must not emit message id,
// folder id, or content under any level. We trigger the failure path
// by passing an entry that violates the IndexBody contract (empty
// MessageID) and assert the captured log line carries none of the
// sensitive substrings.
func TestMaybeIndexBody_RedactsNoSensitiveFields(t *testing.T) {
	logger, c := ilog.NewCaptured(ilog.Options{Level: slog.LevelDebug})

	// A real store would refuse this entry — that's the point. The
	// hook should swallow the err in a log line that contains the
	// error message but NONE of the sensitive fields.
	st := &errIndexStore{err: stubIndexErr{}}
	e := &engine{
		st:     st,
		logger: logger,
		opts: Options{
			BodyIndexEnabled: true,
		},
	}
	msg := &store.Message{
		ID:       "AAMkADExample==",
		FolderID: "AAMkAFolderSecretName==",
	}
	e.MaybeIndexBody(context.Background(), msg, "Body containing a secret-looking token=abcdef")

	require.NoError(t, c.AssertNoSecret(
		"AAMkADExample==",
		"AAMkAFolderSecretName==",
		"secret-looking token=abcdef",
	), "indexer log site must not emit msg id / folder id / content")
}

// stubIndexErr is a sentinel returned by errIndexStore.IndexBody.
type stubIndexErr struct{}

func (stubIndexErr) Error() string { return "stub: IndexBody refused" }

// errIndexStore makes only the methods MaybeIndexBody actually
// touches (IndexBody + the body-index-allowed helpers) — folder
// allow-list defaults to "any" so we hit the IndexBody call.
type errIndexStore struct {
	store.Store
	err error
}

func (s *errIndexStore) IndexBody(ctx context.Context, e store.BodyIndexEntry) error {
	return s.err
}

// silence unused-import warnings when this file builds without
// triggering bytes.Buffer.
var _ = bytes.MinRead
