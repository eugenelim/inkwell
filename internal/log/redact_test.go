package log

import (
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRedactorScrubsBearerTokens(t *testing.T) {
	logger, c := NewCaptured(Options{Level: slog.LevelDebug})
	logger.Info("calling graph", slog.String("auth", "Bearer eyJabc.payload.signature"))
	require.NoError(t, c.AssertNoSecret("eyJabc.payload.signature"))
	require.Contains(t, c.String(), "Bearer <redacted>")
}

func TestRedactorScrubsSensitiveKeys(t *testing.T) {
	logger, c := NewCaptured(Options{Level: slog.LevelDebug})
	logger.Info("token issued",
		slog.String("access_token", "secret-token-value"),
		slog.String("refresh_token", "secret-refresh"),
		slog.String("body", "this is a private email body"),
	)
	require.NoError(t, c.AssertNoSecret("secret-token-value", "secret-refresh", "private email body"))
}

// TestRedactorScrubsComposeSnapshot covers the spec 15 §7 / PR
// 7-ii defense: a snapshot blob carries body + subject + To/Cc
// content. The redaction handler must scrub the entire blob if a
// log site emits it as a single string under the `snapshot` key.
func TestRedactorScrubsComposeSnapshot(t *testing.T) {
	logger, c := NewCaptured(Options{Level: slog.LevelDebug})
	blob := `{"kind":1,"source_id":"m-1","to":"alice@example.invalid","subject":"PRIVATE Q4","body":"PRIVATE BODY CONTENT"}`
	logger.Info("compose snapshot", slog.String("snapshot", blob))
	require.NoError(t, c.AssertNoSecret("PRIVATE BODY CONTENT", "PRIVATE Q4"))
}

func TestRedactorTokenisesEmails(t *testing.T) {
	logger, c := NewCaptured(Options{Level: slog.LevelDebug, AllowOwnUPN: "owner@example.invalid"})
	logger.Info("processing", slog.String("from", "alice@example.invalid"), slog.String("to", "owner@example.invalid"))
	out := c.String()
	require.NotContains(t, out, "alice@example.invalid")
	require.Contains(t, out, "owner@example.invalid", "own UPN preserved")
	require.Regexp(t, `<email-\d+>`, out)
}

func TestRedactorPersistsEmailMappingAcrossRecords(t *testing.T) {
	logger, c := NewCaptured(Options{Level: slog.LevelDebug})
	logger.Info("first", slog.String("from", "bob@example.invalid"))
	logger.Info("second", slog.String("from", "bob@example.invalid"))
	logger.Info("third", slog.String("from", "carol@example.invalid"))
	out := c.String()
	require.Equal(t, 2, strings.Count(out, "<email-0>"))
	require.Contains(t, out, "<email-1>")
}

func TestRedactorRedactsSubjectAboveDebug(t *testing.T) {
	logger, c := NewCaptured(Options{Level: slog.LevelInfo})
	logger.Info("got message", slog.String("subject", "Q4 forecast"))
	require.NotContains(t, c.String(), "Q4 forecast")
	require.Contains(t, c.String(), "<redacted>")
}

func TestRedactorAllowsSubjectAtDebug(t *testing.T) {
	logger, c := NewCaptured(Options{Level: slog.LevelDebug})
	logger.Debug("got message", slog.String("subject", "Q4 forecast"))
	require.Contains(t, c.String(), "Q4 forecast")
}

func TestRedactorScrubsJWTLikeStrings(t *testing.T) {
	logger, c := NewCaptured(Options{Level: slog.LevelDebug})
	logger.Info("inspect", slog.String("note", "found eyJabcdefghijk.payloadabcdefghijk.signatureabcdefghijk in cache"))
	require.NoError(t, c.AssertNoSecret("eyJabcdefghijk.payloadabcdefghijk.signatureabcdefghijk"))
	require.Contains(t, c.String(), "<redacted-jwt>")
}

func TestRedactorRaceFree(t *testing.T) {
	logger, c := NewCaptured(Options{Level: slog.LevelDebug})
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				logger.Info("from", slog.String("addr", "alice@example.invalid"), slog.String("auth", "Bearer abc.def.ghi"))
			}
		}()
	}
	wg.Wait()
	require.NoError(t, c.AssertNoSecret("alice@example.invalid", "abc.def.ghi"))
}

// TestRedactorScrubsInviteFetchError covers the spec-34 §6.1
// soft-fail log site `invite: event fetch failed` which carries
// the wrapped *graph.GraphError as the `err` field. Graph error
// messages can embed user addresses (e.g. "User alice@... cannot
// access ..."); the redactor must tokenise them through the
// generic email-in-string pattern.
func TestRedactorScrubsInviteFetchError(t *testing.T) {
	logger, c := NewCaptured(Options{Level: slog.LevelDebug})
	logger.Warn("invite: event fetch failed",
		slog.String("msg_id", "AAMkADExample=="),
		slog.String("err", "graph: ErrorAccessDenied: User alice@example.invalid cannot access resource /me/messages/abc/event (status 403)"),
	)
	require.NoError(t, c.AssertNoSecret("alice@example.invalid"))
	out := c.String()
	require.Contains(t, out, "<email-",
		"redactor must tokenise the email address embedded in the Graph error")
}

// TestRedact_HashMessageID_OneWayAndStable is spec 35 §8.5's
// canonical assertion. The hash must be deterministic across calls,
// irreversible (no substring of the input survives), and stable on
// the empty input.
func TestRedact_HashMessageID_OneWayAndStable(t *testing.T) {
	id := "AAMkADExample0123456789abcdefABCDEF=="
	h1 := HashMessageID(id)
	h2 := HashMessageID(id)
	require.Equal(t, h1, h2, "HashMessageID must be deterministic")
	require.Len(t, h1, 16, "HashMessageID must return 16 hex characters")

	// No 4+-char substring of the input may appear in the output.
	for i := 0; i+4 <= len(id); i++ {
		sub := id[i : i+4]
		require.NotContains(t, h1, sub, "hash leaks input substring %q", sub)
	}

	// Empty id sentinel.
	require.Empty(t, HashMessageID(""))

	// Different ids hash to different digests (collision is allowed
	// in principle for 16-hex truncation, but two close inputs should
	// not collide in practice).
	require.NotEqual(t, h1, HashMessageID("AAMkADDifferentInput=="))
}
