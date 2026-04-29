package ui

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/eugenelim/inkwell/internal/compose"
	"github.com/eugenelim/inkwell/internal/store"
)

// lipglossPlace centres the modal on the screen. Tiny wrapper so the
// import sits next to the code that uses it.
func lipglossPlace(s string, w, h int) string {
	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, s)
}

// composeStartedMsg is the result of preparing the tempfile + skeleton.
// On success, .tempfile + .sourceID are populated and Bubble Tea will
// then run the editor via tea.ExecProcess (cmd is non-nil). On error,
// .err is set.
type composeStartedMsg struct {
	tempfile string
	sourceID string
	editor   *exec.Cmd
	err      error
}

// composeEditedMsg fires after the user's editor exits (regardless of
// exit code). The body sits on disk at the tempfile we created.
type composeEditedMsg struct {
	tempfile string
	sourceID string
	err      error
}

// draftSavedMsg fires after the Graph round-trip completes.
// On success, .webLink is set so the user can press `s` to open the
// draft in Outlook. On failure, .err carries the reason and
// .tempfile (when set) is the path to the preserved draft file.
type draftSavedMsg struct {
	webLink  string
	tempfile string
	err      error
}

// startReplyCmd builds the reply skeleton, writes a tempfile, and
// returns a Cmd that produces composeStartedMsg. The editor is NOT
// run here — that's the next stage, after Update sees the started
// msg and dispatches tea.ExecProcess.
//
// Two-stage so the failure path (skeleton/tempfile error) doesn't
// leak through tea.ExecProcess's terminal-suspend dance.
func (m Model) startReplyCmd(src store.Message) tea.Cmd {
	return func() tea.Msg {
		// We don't have the rendered body in hand here; pass the
		// body_preview which is what the store has. Future iter:
		// fetch + render the full body so the quote chain is
		// complete.
		skeleton := compose.ReplySkeleton(src, src.BodyPreview)
		path, err := compose.WriteTempfile(skeleton)
		if err != nil {
			return composeStartedMsg{err: fmt.Errorf("compose: %w", err)}
		}
		ec, err := compose.EditorCmd(path)
		if err != nil {
			compose.CleanupTempfile(path)
			return composeStartedMsg{err: err}
		}
		return composeStartedMsg{tempfile: path, sourceID: src.ID, editor: ec}
	}
}

// runEditorCmd invokes tea.ExecProcess on the prepared editor command.
// On exit (success or otherwise), composeEditedMsg lands in Update.
func runEditorCmd(tempfile, sourceID string, editor *exec.Cmd) tea.Cmd {
	return tea.ExecProcess(editor, func(err error) tea.Msg {
		return composeEditedMsg{tempfile: tempfile, sourceID: sourceID, err: err}
	})
}

// saveDraftCmd parses the post-edit tempfile and dispatches the
// CreateDraftReply action. The tempfile is cleaned up ONLY on
// success or on a parse error that means the user discarded
// (ErrEmpty / ErrNoRecipients). Graph round-trip failures leave the
// file on disk so the user doesn't lose their work — they can copy
// the path from the log and finish in Outlook directly.
func (m Model) saveDraftCmd(tempfile, sourceID string) tea.Cmd {
	return func() tea.Msg {
		parsed, err := compose.Parse(tempfile)
		if err != nil {
			// Discard cases — the file is intentionally being thrown
			// away; safe to clean up.
			compose.CleanupTempfile(tempfile)
			return draftSavedMsg{err: err}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		ref, err := m.deps.Drafts.CreateDraftReply(ctx, sourceID, parsed.Body, parsed.To, parsed.Cc, parsed.Bcc, parsed.Subject)
		if err != nil {
			// Preserve the tempfile so the user has a copy of their
			// work. Surface the path so they can recover.
			return draftSavedMsg{err: err, tempfile: tempfile}
		}
		compose.CleanupTempfile(tempfile)
		return draftSavedMsg{webLink: ref.WebLink}
	}
}

// renderComposeConfirm draws the post-edit confirm pane. The
// modal lists the three choices clearly so the user never wonders
// "did pressing :q! save my draft?" — they pick the action
// explicitly.
func (m Model) renderComposeConfirm() string {
	title := m.theme.Bold.Render("✉️  Draft ready")
	body := []string{
		title,
		"",
		"Your editor closed. Pick what to do with this draft:",
		"",
		"  " + m.theme.HelpKey.Render("s") + "  " + m.theme.Help.Render("save draft (lands in your Outlook Drafts folder)"),
		"  " + m.theme.HelpKey.Render("e") + "  " + m.theme.Help.Render("re-edit (re-opens the same file in your editor)"),
		"  " + m.theme.HelpKey.Render("d") + "  " + m.theme.Help.Render("discard (delete the file; nothing sent or saved)"),
		"",
		m.theme.Dim.Render("Esc stays on this prompt — destructive choices need an explicit key."),
	}
	box := m.theme.Modal.Render(strings.Join(body, "\n"))
	return placeCenter(box, m.width, m.height)
}

// placeCenter is a tiny wrapper around lipgloss.Place to keep the
// import discipline in app.go from sprawling.
func placeCenter(s string, w, h int) string { return lipglossPlace(s, w, h) }

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
