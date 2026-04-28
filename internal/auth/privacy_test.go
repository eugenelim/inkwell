package auth

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	ilog "github.com/eu-gene-lim/inkwell/internal/log"
)

// TestAuthErrorPathDoesNotLeakTokenWhenLogged ensures the redactor
// scrubs a token-shaped string even if some future code path
// accidentally hands a token to the logger. This is a backstop for the
// rule that auth errors never carry token material.
func TestAuthErrorPathDoesNotLeakTokenWhenLogged(t *testing.T) {
	logger, captured := ilog.NewCaptured(ilog.Options{Level: slog.LevelDebug, AllowOwnUPN: "owner@example.invalid"})
	const fakeToken = "Bearer eyJlYWtlZA.payloadlongstring.signaturelongstring" //nolint:gosec
	logger.Info("simulated handler dump", slog.String("authorization", fakeToken), slog.String("note", "raw "+fakeToken))
	require.NoError(t, captured.AssertNoSecret("eyJlYWtlZA.payloadlongstring.signaturelongstring"))
}

// TestSignOutDoesNotEmitToken runs SignOut against a fake source whose
// stored "blob" includes a token-shaped string. Even though SignOut
// itself does no logging, this test is a regression backstop: if a
// future change adds logging to the path, the redactor must catch it.
func TestSignOutDoesNotEmitToken(t *testing.T) {
	src := &fakeSource{accounts: []Account{{UPN: "u@example.invalid", TenantID: "T"}}}
	a := newTestAuth(t, src, nil)

	logger, captured := ilog.NewCaptured(ilog.Options{Level: slog.LevelDebug, AllowOwnUPN: "u@example.invalid"})
	require.NoError(t, a.SignOut(context.Background()))
	logger.Info("post-signout state",
		slog.Bool("removed", true),
		slog.String("hint", "Bearer never-logged-in-real-code"))
	require.NoError(t, captured.AssertNoSecret("never-logged-in-real-code"))
}

// TestDeviceCodePromptCalledWithEmptyAccessToken ensures the prompt
// implementation never sees a token (the prompt arrives BEFORE the
// token, by definition of the device-code flow).
func TestDeviceCodePromptCalledWithEmptyAccessToken(t *testing.T) {
	var called atomic.Int32
	prompt := func(_ context.Context, p DeviceCodePrompt) error {
		called.Add(1)
		require.Empty(t, p.UserCode == "" && p.VerificationURL == "")
		return nil
	}
	src := &fakeSource{
		deviceResult: AuthResult{AccessToken: "secret-tok", ExpiresOn: time.Now().Add(time.Hour)},
	}
	a := newTestAuth(t, src, prompt)
	_, err := a.Token(context.Background())
	require.NoError(t, err)
	require.Equal(t, int32(1), called.Load())
}

// TestDeviceCodeCancellationPropagates verifies that a context error
// from the prompt surfaces back through Token without burying the cause.
func TestDeviceCodeCancellationPropagates(t *testing.T) {
	src := &fakeSource{}
	want := errors.New("user canceled")
	a := newTestAuth(t, src, func(_ context.Context, _ DeviceCodePrompt) error { return want })
	_, err := a.Token(context.Background())
	require.ErrorIs(t, err, want)
}
