package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// dispatchIndexCmdBar handles `:index <subverb>` from the cmd-bar.
//
// Spec 35 §10.3. v1 routes every subverb to a status-bar hint
// pointing the user at the CLI equivalent — same shape as
// `:rules`. The four subverbs map to the four cmd_index.go
// commands shipped in slice 6:
//
//	:index status   → inkwell index status
//	:index rebuild  → inkwell index rebuild
//	:index evict    → inkwell index evict --older-than=…
//	:index disable  → inkwell index disable
//
// A future iteration can build an in-TUI status modal that reads
// `store.BodyIndexStats` and paints inline; for now the cmd-bar
// + palette discovery surface is the v1 deliverable. Both depend
// only on the existing CLI behaviour shipped in slice 6, so the
// stub is honest about what the keystroke does today.
func (m Model) dispatchIndexCmdBar(args []string) (tea.Model, tea.Cmd) {
	verb := ""
	if len(args) > 0 {
		verb = strings.ToLower(strings.TrimSpace(args[0]))
	}
	switch verb {
	case "", "status", "rebuild", "evict", "disable":
		// All accepted subverbs route to the same hint for v1.
	default:
		m.lastError = fmt.Errorf("index: unknown subverb %q; try :index status|rebuild|evict|disable", verb)
		return m, nil
	}
	suffix := verb
	if suffix == "" {
		suffix = "status"
	}
	m.lastError = fmt.Errorf("index: use `inkwell index %s` from a shell — the in-TUI status modal is a follow-up iteration", suffix)
	return m, nil
}
