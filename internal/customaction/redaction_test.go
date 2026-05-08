package customaction

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestCustomActionLogsRedactPII asserts the executor's log lines
// (custom_action_run / custom_action_done / custom_action_resolve_failed)
// do not leak From, Subject, or MessageID. The user's typed
// prompt_value is also out of scope.
func TestCustomActionLogsRedactPII(t *testing.T) {
	a := loadOne(t, `
[[custom_action]]
name = "x"
description = "test"
sequence = [{ op = "mark_read" }, { op = "archive" }]
`)
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	deps, _, _, _, _, _, _, _, _ := depsForTest()
	deps.Logger = logger

	msg := newTestContext()
	// Loaded values that must NOT appear verbatim in any log line.
	msg.From = "alice@news.example"
	msg.Subject = "URGENT MEETING"
	msg.MessageID = "m-2026-test-001"
	_, err := Run(context.Background(), a, msg, deps)
	require.NoError(t, err)

	logs := buf.String()
	require.NotEmpty(t, logs)
	for _, leak := range []string{"alice@news.example", "URGENT MEETING", "m-2026-test-001"} {
		require.False(t, strings.Contains(logs, leak),
			"log output must not contain %q (PII per CLAUDE.md §7.3)\n--- log ---\n%s\n", leak, logs)
	}
}

// TestPromptValueInputNotLogged verifies that the user's typed
// prompt_value response never appears in any log line, even at
// DEBUG. The prompt template's resolved form is also withheld
// (§7 prose: visible on the modal, never logged).
func TestPromptValueInputNotLogged(t *testing.T) {
	a := loadOne(t, `
[[custom_action]]
name = "x"
description = "test"
allow_folder_template = true
sequence = [
  { op = "prompt_value", prompt = "Move to:" },
  { op = "move", destination = "{{.UserInput}}" },
]
`)
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	deps, _, _, _, _, _, _, _, _ := depsForTest()
	deps.Logger = logger

	res, err := Run(context.Background(), a, newTestContext(), deps)
	require.NoError(t, err)
	require.NotNil(t, res.Continuation)

	// Resume with a sentinel string we'll grep for.
	const userInput = "PIIPIIPII-secret-folder-name"
	_, err = Resume(context.Background(), res.Continuation, userInput)
	require.NoError(t, err)

	logs := buf.String()
	require.False(t, strings.Contains(logs, userInput),
		"prompt_value input must never reach the log\n--- log ---\n%s\n", logs)
}
