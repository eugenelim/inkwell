package auth

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClassifyAuthError_Nil(t *testing.T) {
	require.NoError(t, ClassifyAuthError(nil))
}

func TestClassifyAuthError_NoMatch(t *testing.T) {
	base := errors.New("something went wrong")
	got := ClassifyAuthError(base)
	assert.Equal(t, base, got, "unrecognised error should pass through unchanged")
}

func TestClassifyAuthError_WrapsOriginal(t *testing.T) {
	base := fmt.Errorf("AADSTS530003: compliant device required")
	got := ClassifyAuthError(base)
	require.Error(t, got)
	assert.True(t, errors.Is(got, base), "original error must be preserved via %%w")
}

func TestClassifyAuthError_DeviceCompliance(t *testing.T) {
	for _, code := range []string{"AADSTS530003", "AADSTS530002"} {
		t.Run(code, func(t *testing.T) {
			got := ClassifyAuthError(fmt.Errorf("%s: device compliance required", code))
			require.Error(t, got)
			assert.Contains(t, got.Error(), "compliant device")
			assert.Contains(t, got.Error(), "--device-code")
		})
	}
}

func TestClassifyAuthError_ConsentRequired(t *testing.T) {
	for _, code := range []string{"AADSTS65001", "AADSTS50105", "AADSTS530004"} {
		t.Run(code, func(t *testing.T) {
			got := ClassifyAuthError(fmt.Errorf("%s: consent required", code))
			require.Error(t, got)
			assert.Contains(t, got.Error(), "IT admin")
		})
	}
}

func TestClassifyAuthError_InvalidScope(t *testing.T) {
	got := ClassifyAuthError(fmt.Errorf("AADSTS70011: invalid scope"))
	require.Error(t, got)
	assert.Contains(t, got.Error(), "scopes are not permitted")
}

func TestClassifyAuthError_ClockSkew(t *testing.T) {
	msgs := []string{
		"clock_skew detected",
		"clock skew is too large",
		"token is not valid yet",
		"not yet valid",
		"issued in the future",
		"AADSTS500133",
	}
	for _, msg := range msgs {
		t.Run(msg, func(t *testing.T) {
			got := ClassifyAuthError(fmt.Errorf("auth error: %s", msg))
			require.Error(t, got)
			assert.Contains(t, got.Error(), "clock")
		})
	}
}

func TestIsClockSkewError_Nil(t *testing.T) {
	assert.False(t, IsClockSkewError(nil))
}

func TestIsClockSkewError_NoMatch(t *testing.T) {
	assert.False(t, IsClockSkewError(errors.New("AADSTS530003: unrelated")))
}

func TestIsClockSkewError_Match(t *testing.T) {
	cases := []string{
		"clock_skew too large",
		"Clock Skew Exceeded",
		"token is not valid yet",
		"not yet valid",
		"issued in the future",
		"AADSTS500133",
	}
	for _, c := range cases {
		t.Run(c, func(t *testing.T) {
			assert.True(t, IsClockSkewError(errors.New(c)))
		})
	}
}

func TestClassifyAuthError_HintLineFormat(t *testing.T) {
	got := ClassifyAuthError(fmt.Errorf("AADSTS530003: device compliance required"))
	require.Error(t, got)
	lines := strings.Split(got.Error(), "\n")
	require.GreaterOrEqual(t, len(lines), 2)
	assert.True(t, strings.HasPrefix(lines[len(lines)-1], "hint: "), "last line must start with 'hint: '")
}
