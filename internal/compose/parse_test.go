package compose

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "draft.eml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func TestParseRoundTripsHeadersAndBody(t *testing.T) {
	p := writeTemp(t, "To: alice@x, bob@x\nCc: ceo@x\nSubject: hi\n\nBody line 1\nBody line 2\n")
	got, err := Parse(p)
	require.NoError(t, err)
	require.Equal(t, []string{"alice@x", "bob@x"}, got.To)
	require.Equal(t, []string{"ceo@x"}, got.Cc)
	require.Equal(t, "hi", got.Subject)
	require.Equal(t, "Body line 1\nBody line 2\n", got.Body)
}

func TestParseEmptyToReturnsErrNoRecipients(t *testing.T) {
	p := writeTemp(t, "To:\nSubject: hi\n\nbody\n")
	got, err := Parse(p)
	require.ErrorIs(t, err, ErrNoRecipients)
	require.NotNil(t, got, "result still returned so caller can re-open editor")
	require.Empty(t, got.To)
}

func TestParseTrailingCommaStrippedFromTo(t *testing.T) {
	p := writeTemp(t, "To: alice@x, , bob@x,\nSubject: x\n\nbody\n")
	got, err := Parse(p)
	require.NoError(t, err)
	require.Equal(t, []string{"alice@x", "bob@x"}, got.To)
}

func TestParseEmptyFileIsErrEmpty(t *testing.T) {
	p := writeTemp(t, "   \n\n  ")
	_, err := Parse(p)
	require.ErrorIs(t, err, ErrEmpty)
}

func TestParseUnknownHeadersIgnored(t *testing.T) {
	p := writeTemp(t, "X-Custom: weird\nTo: a@x\nSubject: x\n\nbody\n")
	got, err := Parse(p)
	require.NoError(t, err)
	require.Equal(t, []string{"a@x"}, got.To)
}

func TestParseGarbageLineSkipped(t *testing.T) {
	p := writeTemp(t, "no colon line\nTo: a@x\nSubject: x\n\nbody\n")
	got, err := Parse(p)
	require.NoError(t, err)
	require.Equal(t, []string{"a@x"}, got.To)
}
