//go:build e2e

package ui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"
)

// TestRegexSearchPrefix_SurfacesIndexRequirement is spec 35 §13.6
// row 1 in the index-disabled state (the e2e store has body_index
// off by default): typing `/regex:auth.*` should surface the
// documented [body_index].enabled = true requirement directly in
// the status bar, not silently fail. Visible-delta rule: the user
// sees the path forward without consulting docs.
//
// The index-enabled `[regex local-only]` indicator branch fires
// when the filter actually runs; that path is covered by the
// pattern-package strategy tests rather than re-asserted here.
func TestRegexSearchPrefix_SurfacesIndexRequirement(t *testing.T) {
	m, _ := newE2EModel(t)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(120, 30))

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return contains(string(out), "Inbox")
	}, teatest.WithDuration(2*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	tm.Type("regex:auth.*token")
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		s := string(out)
		// The actionable cue must mention the config flag the user
		// needs to flip. Spec 35 §9.3 sentinel message.
		return contains(s, "body_index") && contains(s, "enabled")
	}, teatest.WithDuration(2*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))
}

// TestIndexCmdBar_ShowsCLIPointerToast is spec 35 §13.6 row 3 (the
// `:index status` opens cmd-bar + emits toast variant). v1 of the
// dispatcher routes every subverb to a `lastError`-channel hint
// pointing at the CLI; the toast must mention `inkwell index status`
// so a user discovering the feature through the cmd-bar gets the
// path forward.
func TestIndexCmdBar_ShowsCLIPointerToast(t *testing.T) {
	m, _ := newE2EModel(t)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(120, 30))

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return contains(string(out), "Inbox")
	}, teatest.WithDuration(2*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(":")})
	tm.Type("index status")
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return contains(string(out), "inkwell index status")
	}, teatest.WithDuration(2*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))
}

// TestIndexCmdBar_UnknownSubverb hits the dispatcher's negative
// path: `:index garbage` surfaces the documented `try :index status|
// rebuild|evict|disable` hint.
func TestIndexCmdBar_UnknownSubverb(t *testing.T) {
	m, _ := newE2EModel(t)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(120, 30))

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return contains(string(out), "Inbox")
	}, teatest.WithDuration(2*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(":")})
	tm.Type("index garbage")
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return contains(string(out), "unknown subverb")
	}, teatest.WithDuration(2*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))
}
