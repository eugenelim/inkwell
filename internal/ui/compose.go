package ui

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/eugenelim/inkwell/internal/store"
)

// lipglossPlace centres the modal on the screen. Tiny wrapper so the
// import sits next to the code that uses it.
func lipglossPlace(s string, w, h int) string {
	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, s)
}

// draftSavedMsg fires after the Graph round-trip completes.
// On success, .webLink is set so the user can press `s` to open the
// draft in Outlook. On failure, .err carries the reason.
type draftSavedMsg struct {
	webLink string
	err     error
}

// saveComposeCmd dispatches the in-modal form's snapshot through
// the action queue. Recipient recovery (spec 15 v2 §6 — fall back
// to the source's FromAddress when the form's To is empty) lives
// here so the same safety net the editor flow had still applies.
//
// The Ctrl+S / Esc save path lands here. On success the modal
// closes, status bar shows "✓ draft saved · press s to open in
// Outlook." On failure the form state stays in m.compose so the
// user can correct + retry.
func (m Model) saveComposeCmd(snap ComposeSnapshot) tea.Cmd {
	var accountID int64
	if m.deps.Account != nil {
		accountID = m.deps.Account.ID
	}
	st := m.deps.Store
	return func() tea.Msg {
		toList := splitAddressList(snap.To)
		// Recipient recovery: empty To + reply to a known source →
		// use source.FromAddress. Mirrors the legacy saveDraftCmd
		// behaviour. Without this, replying to an email with no
		// edits to the To line would error as "no recipient" even
		// though the source's sender is the obvious target.
		if len(toList) == 0 && snap.SourceID != "" {
			if fallback := lookupSourceFromAddress(st, snap.SourceID); fallback != "" {
				toList = []string{fallback}
			}
		}
		if len(toList) == 0 {
			return draftSavedMsg{
				err: fmt.Errorf("draft has no recipient (set To: in the compose form)"),
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		ref, err := m.deps.Drafts.CreateDraftReply(ctx, accountID, snap.SourceID,
			snap.Body, toList, splitAddressList(snap.Cc), nil, snap.Subject)
		if err != nil {
			return draftSavedMsg{err: err}
		}
		return draftSavedMsg{webLink: ref.WebLink}
	}
}

// splitAddressList turns "a@x, b@y" into ["a@x", "b@y"]. Empty
// entries are skipped so trailing commas / blank fields don't
// produce phantom recipients. Mirrors compose.splitAddrs from the
// legacy parse path; replicated here so the in-modal flow doesn't
// need to import the legacy compose package.
func splitAddressList(s string) []string {
	var out []string
	cur := ""
	flush := func() {
		v := trimSpaces(cur)
		if v != "" {
			out = append(out, v)
		}
		cur = ""
	}
	for _, r := range s {
		if r == ',' || r == ';' {
			flush()
			continue
		}
		cur += string(r)
	}
	flush()
	return out
}

// trimSpaces strips ASCII whitespace from both ends. Tiny helper
// so we don't pull in strings just for this.
func trimSpaces(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t' || s[0] == '\n' || s[0] == '\r') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t' || s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}

// lookupSourceFromAddress reads sourceID's `FromAddress` from the
// store. Returns "" if the row isn't found or the FromAddress is
// empty — the caller treats either as "no fallback available".
func lookupSourceFromAddress(s store.Store, sourceID string) string {
	if s == nil || sourceID == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	msg, err := s.GetMessage(ctx, sourceID)
	if err != nil || msg == nil {
		return ""
	}
	return msg.FromAddress
}

// openInBrowser opens url via the OS-default handler. macOS uses
// `open`; Linux/BSD uses `xdg-open`. Best-effort; errors are silently
// swallowed because the user already has the link in the status bar
// and can copy it manually if this fails.
func openInBrowser(url string) {
	var args []string
	switch runtime.GOOS {
	case "darwin":
		args = []string{"open", url}
	case "linux", "freebsd", "netbsd", "openbsd":
		args = []string{"xdg-open", url}
	default:
		return
	}
	// #nosec G204 — args[0] is "open" or "xdg-open" (constant per OS); args[1] is a URL drawn from a Graph webLink the server gave us. No shell, no concatenation, no user-controlled binary.
	_ = exec.Command(args[0], args[1:]...).Run()
}
