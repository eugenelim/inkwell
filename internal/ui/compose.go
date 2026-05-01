package ui

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os/exec"
	"runtime"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/eugenelim/inkwell/internal/store"
)

// composeResumePrompt builds the user-facing confirm modal text
// for an unconfirmed session. Includes the source subject (when
// available; loaded from the store via SourceID) and a short age
// hint so the user can tell which crashed draft they're being
// asked about.
func composeResumePrompt(sess store.ComposeSession) string {
	age := time.Since(sess.UpdatedAt).Round(time.Minute)
	if age < time.Minute {
		age = time.Minute
	}
	switch sess.Kind {
	case "reply", "reply_all":
		return fmt.Sprintf("Resume reply draft from %s ago?", humanAge(age))
	case "forward":
		return fmt.Sprintf("Resume forward draft from %s ago?", humanAge(age))
	case "new":
		return fmt.Sprintf("Resume new-message draft from %s ago?", humanAge(age))
	}
	return fmt.Sprintf("Resume draft from %s ago?", humanAge(age))
}

// humanAge renders a `5 min` / `2 h` / `1 day` style duration.
// Designed for the resume prompt so the user can decide whether
// the crashed draft is still relevant.
func humanAge(d time.Duration) string {
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%d min", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d h", int(d.Hours()))
	}
	days := int(d.Hours() / 24)
	if days == 1 {
		return "1 day"
	}
	return fmt.Sprintf("%d days", days)
}

// resumeCompose hydrates a fresh ComposeModel from a stored
// snapshot and enters ComposeMode. Called from the
// ConfirmResultMsg branch when the user accepted the resume
// prompt. The session id is preserved so subsequent persistence /
// confirm writes hit the same row (no orphaned snapshots).
//
// Snapshot decode errors fall back to a friendly status message;
// the row is confirmed to prevent infinite resume loops.
func (m Model) resumeCompose(sess store.ComposeSession) (tea.Model, tea.Cmd) {
	var snap ComposeSnapshot
	if err := json.Unmarshal([]byte(sess.Snapshot), &snap); err != nil {
		if m.deps.Logger != nil {
			m.deps.Logger.Warn("compose: resume snapshot decode failed", "err", err.Error())
		}
		m.lastError = fmt.Errorf("resume: snapshot corrupt; discarded")
		m.confirmComposeSessionInline(sess.SessionID)
		return m, nil
	}
	m.compose = NewCompose()
	m.compose.SessionID = sess.SessionID
	m.compose.Restore(snap)
	m.mode = ComposeMode
	m.engineActivity = "resumed draft"
	return m, nil
}

// composeResumeMsg fires after the launch-time scan finds an
// unconfirmed compose session in the store. The Update handler
// hands it to the resume modal which asks the user whether to
// continue editing or discard.
type composeResumeMsg struct {
	Session store.ComposeSession
	Err     error
}

// composeResumeNoneMsg fires when the launch-time scan finds no
// unconfirmed sessions. Wired so the Init Cmd's batch always
// receives a deterministic message (test harnesses can assert).
type composeResumeNoneMsg struct{}

// newComposeSessionID returns a 16-byte hex id prefixed with
// `cs-`. crypto/rand keeps test runs (which spawn parallel models)
// from colliding on time-based ids.
func newComposeSessionID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		// Fallback to a time-based id if the system is starved of
		// entropy (extremely rare on modern OSes; never block the
		// compose flow on it).
		return fmt.Sprintf("cs-%d", time.Now().UnixNano())
	}
	return "cs-" + hex.EncodeToString(b[:])
}

// composeKindToString maps the UI's ComposeKind enum to the string
// stored in compose_sessions.kind. The table column is text rather
// than int so a future spec can add a new kind without a migration.
func composeKindToString(k ComposeKind) string {
	switch k {
	case ComposeKindReply:
		return "reply"
	case ComposeKindReplyAll:
		return "reply_all"
	case ComposeKindForward:
		return "forward"
	case ComposeKindNew:
		return "new"
	}
	return "reply"
}

// persistComposeSnapshotCmd writes the current ComposeModel state
// into compose_sessions. Called on entry to ComposeMode and on
// each focus change (Tab / Shift+Tab) so the resume scan finds
// the user's most-recent input. Per-keystroke persistence is
// deliberately NOT done — too noisy for the disk and per-Tab
// already captures every field-completion the user makes.
//
// Persist failures log via the model logger but do NOT surface
// to the user; a failed snapshot doesn't block the compose flow,
// it just means crash recovery would have nothing to resume from
// (the legacy "no recovery at all" behaviour).
func (m Model) persistComposeSnapshotCmd() tea.Cmd {
	if m.deps.Store == nil || m.compose.SessionID == "" {
		return nil
	}
	st := m.deps.Store
	logger := m.deps.Logger
	sessID := m.compose.SessionID
	snap := m.compose.Snapshot()
	return func() tea.Msg {
		blob, err := json.Marshal(snap)
		if err != nil {
			if logger != nil {
				logger.Warn("compose: snapshot marshal failed", "err", err.Error())
			}
			return nil
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		err = st.PutComposeSession(ctx, store.ComposeSession{
			SessionID: sessID,
			Kind:      composeKindToString(snap.Kind),
			SourceID:  snap.SourceID,
			Snapshot:  string(blob),
		})
		if err != nil && logger != nil {
			logger.Warn("compose: snapshot persist failed", "err", err.Error())
		}
		return nil
	}
}

// scanComposeSessionsCmd runs the launch-time resume scan. Returns
// a composeResumeMsg with the most-recent unconfirmed session if
// found; composeResumeNoneMsg otherwise. The companion GC pass for
// confirmed sessions older than 24h runs in the same Cmd so we
// don't churn through the table twice on launch.
func (m Model) scanComposeSessionsCmd() tea.Cmd {
	if m.deps.Store == nil {
		return func() tea.Msg { return composeResumeNoneMsg{} }
	}
	st := m.deps.Store
	logger := m.deps.Logger
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		// GC first — confirmed-older-than-24h. Failure here is
		// benign (the table just stays a bit bigger).
		if _, err := st.GCConfirmedComposeSessions(ctx, time.Now().Add(-24*time.Hour)); err != nil && logger != nil {
			logger.Warn("compose: GC failed", "err", err.Error())
		}
		rows, err := st.ListUnconfirmedComposeSessions(ctx)
		if err != nil {
			return composeResumeMsg{Err: err}
		}
		if len(rows) == 0 {
			return composeResumeNoneMsg{}
		}
		return composeResumeMsg{Session: rows[0]}
	}
}

// draftSavedMsg fires after the Graph round-trip completes.
// On success, .webLink is set so the user can press `s` to open the
// draft in Outlook. On failure, .err carries the reason.
type draftSavedMsg struct {
	webLink string
	err     error
}

// confirmComposeSessionInline writes confirmed_at synchronously.
// Used by the Ctrl+D discard path which has no other goroutine to
// hang the write off; SQLite WAL keeps this sub-millisecond on
// local disk. Failure is logged + ignored — worst case the resume
// scan offers a discarded draft on next launch and the user
// discards again.
func (m Model) confirmComposeSessionInline(sessionID string) {
	if m.deps.Store == nil || sessionID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := m.deps.Store.ConfirmComposeSession(ctx, sessionID); err != nil && m.deps.Logger != nil {
		m.deps.Logger.Warn("compose: confirm-on-discard failed", "err", err.Error())
	}
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
//
// Spec 15 §7 / PR 7-ii: the ConfirmComposeSession write happens
// inside this goroutine after the Graph round-trip resolves
// (success OR failure) — the user explicitly pressed save, so the
// resume scan should not re-offer this row regardless of how the
// Graph call ended. Failure to write confirmed_at is logged but
// not surfaced (the worst case is one duplicate resume offer).
func (m Model) saveComposeCmd(snap ComposeSnapshot, sessionID string) tea.Cmd {
	var accountID int64
	if m.deps.Account != nil {
		accountID = m.deps.Account.ID
	}
	st := m.deps.Store
	logger := m.deps.Logger
	confirm := func() {
		if st == nil || sessionID == "" {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := st.ConfirmComposeSession(ctx, sessionID); err != nil && logger != nil {
			logger.Warn("compose: confirm-on-save failed", "err", err.Error())
		}
	}
	return func() tea.Msg {
		toList := splitAddressList(snap.To)
		// Recipient recovery: empty To + reply to a known source →
		// use source.FromAddress. Mirrors the legacy saveDraftCmd
		// behaviour. Without this, replying to an email with no
		// edits to the To line would error as "no recipient" even
		// though the source's sender is the obvious target. Only
		// applies to Reply / ReplyAll / Forward — for a brand-new
		// draft there's no source to fall back to.
		if len(toList) == 0 && snap.SourceID != "" && snap.Kind != ComposeKindNew {
			if fallback := lookupSourceFromAddress(st, snap.SourceID); fallback != "" {
				toList = []string{fallback}
			}
		}
		if len(toList) == 0 {
			confirm()
			return draftSavedMsg{
				err: fmt.Errorf("draft has no recipient (set To: in the compose form)"),
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		ccList := splitAddressList(snap.Cc)
		var ref *DraftRef
		var err error
		switch snap.Kind {
		case ComposeKindReplyAll:
			ref, err = m.deps.Drafts.CreateDraftReplyAll(ctx, accountID, snap.SourceID,
				snap.Body, toList, ccList, nil, snap.Subject)
		case ComposeKindForward:
			ref, err = m.deps.Drafts.CreateDraftForward(ctx, accountID, snap.SourceID,
				snap.Body, toList, ccList, nil, snap.Subject)
		case ComposeKindNew:
			ref, err = m.deps.Drafts.CreateNewDraft(ctx, accountID,
				snap.Body, toList, ccList, nil, snap.Subject)
		default:
			ref, err = m.deps.Drafts.CreateDraftReply(ctx, accountID, snap.SourceID,
				snap.Body, toList, ccList, nil, snap.Subject)
		}
		confirm()
		if err != nil {
			if ref != nil {
				// Stage 2 (PATCH) failed but stage 1 produced a draft.
				// Existing spec-15 contract: surface the error AND the
				// webLink so the user can finish in Outlook.
				return draftSavedMsg{webLink: ref.WebLink, err: err}
			}
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
