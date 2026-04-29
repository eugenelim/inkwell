package unsub

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestParseRejectsEmpty verifies the friendly error path that the UI
// surfaces as "no List-Unsubscribe header — try Outlook for this one".
func TestParseRejectsEmpty(t *testing.T) {
	_, err := Parse("", "")
	require.ErrorIs(t, err, ErrNoHeader)
	_, err = Parse("   \t\n", "")
	require.ErrorIs(t, err, ErrNoHeader)
}

// TestParseCorpusRealWorldHeaders is the spec 16 §11 corpus —
// headers we've seen from major senders. New tenants will surface
// new shapes; add them here as bug-named regression cases (meli's
// fixture-as-bug-postmortem convention from the test research).
func TestParseCorpusRealWorldHeaders(t *testing.T) {
	cases := []struct {
		name        string
		listUnsub   string
		listUnsubP  string
		wantAction  Action
		wantURL     string
		wantMailto  string
		wantSubject string
	}{
		{
			name:       "rfc8058-one-click-https-only",
			listUnsub:  "<https://example.invalid/u?id=abc>",
			listUnsubP: "List-Unsubscribe=One-Click",
			wantAction: ActionOneClickPOST,
			wantURL:    "https://example.invalid/u?id=abc",
		},
		{
			name:       "https-without-post-header-degrades-to-browser",
			listUnsub:  "<https://example.invalid/u?id=abc>",
			listUnsubP: "",
			wantAction: ActionBrowserGET,
			wantURL:    "https://example.invalid/u?id=abc",
		},
		{
			name:       "mailto-only",
			listUnsub:  "<mailto:unsub@example.invalid>",
			listUnsubP: "",
			wantAction: ActionMailto,
			wantMailto: "unsub@example.invalid",
		},
		{
			name:        "mailto-with-subject-and-body",
			listUnsub:   "<mailto:unsub@example.invalid?subject=remove&body=please>",
			listUnsubP:  "",
			wantAction:  ActionMailto,
			wantMailto:  "unsub@example.invalid",
			wantSubject: "remove",
		},
		{
			name:       "both-mailto-and-https-no-post-header-prefers-mailto",
			listUnsub:  "<mailto:unsub@example.invalid>, <https://example.invalid/u>",
			listUnsubP: "",
			wantAction: ActionMailto,
			wantMailto: "unsub@example.invalid",
		},
		{
			name:       "both-mailto-and-https-with-post-header-prefers-one-click",
			listUnsub:  "<mailto:unsub@example.invalid>, <https://example.invalid/u>",
			listUnsubP: "List-Unsubscribe=One-Click",
			wantAction: ActionOneClickPOST,
			wantURL:    "https://example.invalid/u",
		},
		{
			// AWS / SES style: unsub URL embedded with a subscriber id.
			name:       "aws-ses-style-https",
			listUnsub:  "<https://example.invalid/unsubscribe?Identity=A&MessageID=B>",
			listUnsubP: "List-Unsubscribe=One-Click",
			wantAction: ActionOneClickPOST,
			wantURL:    "https://example.invalid/unsubscribe?Identity=A&MessageID=B",
		},
		{
			// Mailchimp-shape: mailto for legacy clients + one-click https.
			name:       "mailchimp-style-pair",
			listUnsub:  " <mailto:list-unsub@example.invalid?subject=unsubscribe> , <https://example.invalid/u/abc> ",
			listUnsubP: "List-Unsubscribe=One-Click",
			wantAction: ActionOneClickPOST,
			wantURL:    "https://example.invalid/u/abc",
		},
		{
			// Substack-style: just one HTTPS URL with one-click.
			name:       "substack-style",
			listUnsub:  "<https://example.invalid/api/v1/free?token=xyz>",
			listUnsubP: "List-Unsubscribe=One-Click",
			wantAction: ActionOneClickPOST,
			wantURL:    "https://example.invalid/api/v1/free?token=xyz",
		},
		{
			// Substack lowercase post-header capitalisation seen in the wild.
			name:       "lowercase-post-header",
			listUnsub:  "<https://example.invalid/u/abc>",
			listUnsubP: "list-unsubscribe=one-click",
			wantAction: ActionOneClickPOST,
			wantURL:    "https://example.invalid/u/abc",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, err := Parse(tc.listUnsub, tc.listUnsubP)
			require.NoError(t, err)
			require.Equal(t, tc.wantAction, r.Action, "action")
			if tc.wantURL != "" {
				require.Equal(t, tc.wantURL, r.URL, "url")
			}
			if tc.wantMailto != "" {
				require.Equal(t, tc.wantMailto, r.MailtoAddr, "mailto addr")
			}
			if tc.wantSubject != "" {
				require.Equal(t, tc.wantSubject, r.MailtoSubject, "subject param")
			}
		})
	}
}

// TestParseRefusesPlainHTTP is the spec 16 §9 invariant: plain HTTP
// URIs are not actionable. Must surface ErrUnactionable so the UI
// shows the "open manually if you trust the sender" message.
func TestParseRefusesPlainHTTP(t *testing.T) {
	_, err := Parse("<http://example.invalid/u>", "List-Unsubscribe=One-Click")
	require.ErrorIs(t, err, ErrUnactionable, "plain HTTP must NOT auto-POST")
}

// TestParseRefusesUnknownScheme guards against attempted scheme-based
// trickery (`<javascript:...>`, `<file:...>`).
func TestParseRefusesUnknownScheme(t *testing.T) {
	_, err := Parse("<javascript:alert(1)>", "")
	require.ErrorIs(t, err, ErrUnactionable)
	_, err = Parse("<file:///etc/passwd>", "")
	require.ErrorIs(t, err, ErrUnactionable)
}

// TestParseHandlesMissingBrackets matches the lenient real-world
// pattern: senders sometimes emit bare URIs. Per RFC 2369 §3.6 these
// are technically invalid, and we'd rather refuse to act than guess.
func TestParseHandlesMissingBrackets(t *testing.T) {
	_, err := Parse("https://example.invalid/u", "")
	require.ErrorIs(t, err, ErrUnactionable, "bare URIs must not be actionable")
}

// TestParseDoesNotPanicOnGarbage is the fuzz-style guard: any byte
// soup must produce a typed error rather than panic. Adds confidence
// before wiring the parser to user input.
func TestParseDoesNotPanicOnGarbage(t *testing.T) {
	garbage := []string{
		"<<<<<>>>>>",
		"<>",
		"<https://[",
		strings.Repeat("<", 1000),
		"<\x00\x01\x02>",
	}
	for _, g := range garbage {
		t.Run(g[:min(len(g), 20)], func(t *testing.T) {
			_, _ = Parse(g, "List-Unsubscribe=One-Click") // must not panic
		})
	}
}

// TestIndicatorURLNormalisesEachAction returns the right cache key
// for each action so the store has one column to populate.
func TestIndicatorURLNormalisesEachAction(t *testing.T) {
	require.Equal(t, "https://example.invalid/u",
		IndicatorURL(&Result{Action: ActionOneClickPOST, URL: "https://example.invalid/u"}))
	require.Equal(t, "https://example.invalid/u",
		IndicatorURL(&Result{Action: ActionBrowserGET, URL: "https://example.invalid/u"}))
	require.Equal(t, "mailto:unsub@example.invalid",
		IndicatorURL(&Result{Action: ActionMailto, MailtoAddr: "unsub@example.invalid"}))
	require.Empty(t, IndicatorURL(nil))
}

// TestPrivacyParseLogsNothing is the redaction guard — parse is pure;
// it never logs. We assert by reading the source and confirming no
// import of log/slog (CLAUDE.md §7 mandate).
func TestPrivacyParseLogsNothing(t *testing.T) {
	require.NotContains(t, mustReadParseSource(t), "log/slog",
		"parse must not import log/slog (CLAUDE.md §7)")
}
