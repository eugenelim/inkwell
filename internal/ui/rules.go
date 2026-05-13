package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// dispatchRulesCmdBar handles `:rules <subverb>` from the cmd-bar.
//
// v1 scope (spec 32 §8.3 cmd-bar parity, conservative cut): every
// subverb surfaces a status-bar hint pointing the user at the CLI
// equivalent. The full in-TUI manager modal is a follow-up iteration
// — landing it now would balloon this spec well past the budget and
// the value-per-effort is highest for the cmd-bar pointer + palette
// discoverability, both of which this file delivers.
//
// The cmd-bar entry exists today so that:
//   - Users discover the feature via `:` + tab-completion (the
//     `case "rules"` dispatch in app.go).
//   - Palette rows (spec_32_palette_commands.go) render with a
//     binding string that types the user-facing command for them.
//   - Future iterations can replace the stub with a real modal
//     without changing the surface contract.
func (m Model) dispatchRulesCmdBar(args []string) (tea.Model, tea.Cmd) {
	verb := ""
	if len(args) > 0 {
		verb = strings.ToLower(strings.TrimSpace(args[0]))
	}
	switch verb {
	case "", "list", "pull", "apply", "edit", "new",
		"delete", "enable", "disable", "move", "get":
		// All accepted subverbs route to the same hint for v1.
	default:
		m.lastError = fmt.Errorf("rules: unknown subverb %q; try :rules list|pull|apply", verb)
		return m, nil
	}
	// Surface via lastError (same channel as routing / screener
	// dispatchers) — the user sees a single-line hint and the next
	// real error clears it on its own when the user dismisses.
	m.lastError = fmt.Errorf("rules: use `inkwell rules %s` from a shell — the in-TUI manager modal is a follow-up iteration", dispatchedVerb(verb))
	return m, nil
}

func dispatchedVerb(v string) string {
	if v == "" {
		return "list"
	}
	return v
}
