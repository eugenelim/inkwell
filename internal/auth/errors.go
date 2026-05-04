package auth

import (
	"fmt"
	"strings"
)

// ClassifyAuthError wraps an MSAL/AAD error with a human-readable hint
// line when the error contains a recognised AADSTS code or known
// pattern (clock skew, device compliance, consent required). Callers
// should pass the error through before surfacing it to the user.
//
// The original error is preserved via %w so errors.Is/As chains work.
// Returns nil unchanged.
func ClassifyAuthError(err error) error {
	if err == nil {
		return nil
	}
	hint := classifyAAD(err.Error())
	if hint == "" {
		return err
	}
	return fmt.Errorf("%w\nhint: %s", err, hint)
}

// IsClockSkewError returns true when err looks like an AAD clock-skew
// rejection. Exported for tests and for UI code that wants to render
// a dedicated "please sync your clock" message instead of a generic
// auth error.
func IsClockSkewError(err error) bool {
	if err == nil {
		return false
	}
	return isClockSkewMsg(err.Error())
}

// classifyAAD maps AADSTS codes and known error patterns to short
// user-readable hint strings. Returns "" when no match is found.
func classifyAAD(msg string) string {
	switch {
	case containsAny(msg, "AADSTS530003", "AADSTS530002"):
		return "This tenant requires a compliant device. Run `inkwell signin` without --device-code so the system browser can carry the device-attestation signal."
	case containsAny(msg, "AADSTS65001", "AADSTS50105", "AADSTS530004"):
		return "Your tenant blocks the Microsoft Graph Command Line Tools app — ask your IT admin to allow it or grant user-consent for first-party Microsoft apps."
	case strings.Contains(msg, "AADSTS70011"):
		return "One or more requested scopes are not permitted by your tenant. Please contact your admin."
	case isClockSkewMsg(msg):
		return "System clock is off by more than 5 minutes; please sync your clock."
	}
	return ""
}

// isClockSkewMsg returns true when msg contains patterns associated
// with an AAD clock-skew rejection.
func isClockSkewMsg(msg string) bool {
	lower := strings.ToLower(msg)
	return strings.Contains(lower, "clock_skew") ||
		(strings.Contains(lower, "clock") && strings.Contains(lower, "skew")) ||
		strings.Contains(lower, "token is not valid yet") ||
		strings.Contains(lower, "not yet valid") ||
		strings.Contains(lower, "issued in the future") ||
		strings.Contains(lower, "aadsts500133")
}

// containsAny returns true if s contains any of the substrings.
func containsAny(s string, substrings ...string) bool {
	for _, sub := range substrings {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
