package ui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/eugenelim/inkwell/internal/customaction"
	"github.com/eugenelim/inkwell/internal/store"
)

// customActionDoneMsg arrives when a custom-action invocation
// completes (full success, full failure, or pause-on-prompt). The
// Update handler renders the toast and parks any continuation on
// m.customActionContinuation.
type customActionDoneMsg struct {
	result customaction.Result
	err    error
}

// runCustomActionByName dispatches the custom action whose Name is
// `name`. Builds a Context from the focused message (or empty when
// invoked without a focus, e.g. from `:actions run`). Returns the
// pair Bubble Tea expects.
func (m Model) runCustomActionByName(name string) (tea.Model, tea.Cmd) {
	if m.customActions == nil {
		m.lastError = fmt.Errorf("no custom actions configured")
		return m, nil
	}
	a, ok := m.customActions.ByName[name]
	if !ok {
		m.lastError = fmt.Errorf("custom action %q not found", name)
		return m, nil
	}
	if m.customActionRunning != "" {
		m.lastError = fmt.Errorf("custom action already in flight")
		return m, nil
	}
	if m.customActionDeps.Triage == nil {
		m.lastError = fmt.Errorf("custom actions: dispatch surface not wired")
		return m, nil
	}

	cctx := m.buildCustomActionContext(a)
	if cctx.MessageID == "" && requiresFocusedMessage(a) {
		m.lastError = fmt.Errorf("custom action %q: no focused message", name)
		return m, nil
	}

	m.customActionRunning = name
	deps := m.customActionDeps
	if deps.NowFn == nil {
		deps.NowFn = time.Now
	}
	return m, func() tea.Msg {
		res, err := customaction.Run(context.Background(), a, cctx, deps)
		return customActionDoneMsg{result: res, err: err}
	}
}

// resumeCustomAction submits the user's prompt_value modal entry to
// the in-flight continuation. Returns the dispatched Cmd.
func (m Model) resumeCustomAction(userInput string) (tea.Model, tea.Cmd) {
	if m.customActionContinuation == nil {
		return m, nil
	}
	cont := m.customActionContinuation
	m.customActionContinuation = nil
	m.customActionPromptBuf = ""
	m.customActionPromptHeader = ""
	m.mode = NormalMode
	return m, func() tea.Msg {
		res, err := customaction.Resume(context.Background(), cont, userInput)
		return customActionDoneMsg{result: res, err: err}
	}
}

// cancelCustomAction drops the in-flight continuation and clears the
// prompt modal.
func (m Model) cancelCustomAction(reason string) (tea.Model, tea.Cmd) {
	name := m.customActionRunning
	m.customActionContinuation = nil
	m.customActionPromptBuf = ""
	m.customActionPromptHeader = ""
	m.customActionRunning = ""
	m.mode = NormalMode
	if name != "" {
		if reason == "" {
			reason = "cancelled"
		}
		m.engineActivity = fmt.Sprintf("custom action %q: %s", name, reason)
	}
	return m, nil
}

// buildCustomActionContext snapshots the focused-message data into a
// customaction.Context. SelectionIDs are populated from the active
// filter when the action's first step is `filter` or a *_filtered
// variant.
func (m Model) buildCustomActionContext(a *customaction.Action) customaction.Context {
	cctx := customaction.Context{
		AccountID:     m.customActionDeps.AccountID,
		SelectionKind: "single",
	}
	if m.deps.Account != nil {
		cctx.AccountID = m.deps.Account.ID
	}
	var msg *store.Message
	if cur := m.viewer.current; cur != nil && m.focused == ViewerPane {
		msg = cur
	} else if sel, ok := m.list.SelectedMessage(); ok {
		msg = &sel
	}
	if msg != nil {
		cctx.From = strings.ToLower(strings.TrimSpace(msg.FromAddress))
		cctx.FromName = msg.FromName
		if at := strings.LastIndex(cctx.From, "@"); at >= 0 {
			cctx.SenderDomain = cctx.From[at+1:]
		}
		cctx.Subject = msg.Subject
		cctx.ConversationID = msg.ConversationID
		cctx.MessageID = msg.ID
		cctx.IsRead = msg.IsRead
		cctx.FlagStatus = msg.FlagStatus
		cctx.Date = msg.ReceivedAt
		if msg.ToAddresses != nil && len(msg.ToAddresses) > 0 {
			cctx.To = msg.ToAddresses[0].Address
		}
	}
	// Selection IDs: prefer the active filter result; fall back to
	// the focused message ID for thread-style sequences.
	if m.filterActive && len(m.filterIDs) > 0 && firstStepIsFilteredOrFilter(a) {
		cctx.SelectionIDs = append([]string(nil), m.filterIDs...)
		cctx.SelectionKind = "filtered"
	}
	return cctx
}

// requiresFocusedMessage reports whether the action's first step
// needs a focused-message Context. Filter / *_filtered actions can
// start without one.
func requiresFocusedMessage(a *customaction.Action) bool {
	if a == nil || len(a.Steps) == 0 {
		return false
	}
	switch a.Steps[0].Op {
	case customaction.OpFilter, customaction.OpMoveFiltered, customaction.OpPermanentDeleteFiltered:
		return false
	}
	return true
}

// firstStepIsFilteredOrFilter reports whether the action's first
// step is a *_filtered op or filter.
func firstStepIsFilteredOrFilter(a *customaction.Action) bool {
	if a == nil || len(a.Steps) == 0 {
		return false
	}
	switch a.Steps[0].Op {
	case customaction.OpFilter, customaction.OpMoveFiltered, customaction.OpPermanentDeleteFiltered:
		return true
	}
	return false
}

// renderCustomActionToast formats the §5.2 result toast. Single line
// on full success when every step is queue-routed (undoable);
// multi-line otherwise.
func renderCustomActionToast(name string, res customaction.Result) string {
	ok, failed, skipped := 0, 0, 0
	nonUndoable := []string{}
	for _, r := range res.Steps {
		switch r.Status {
		case customaction.StepOK:
			ok++
			if r.NonUndoable {
				nonUndoable = append(nonUndoable, string(r.Op))
			}
		case customaction.StepFailed:
			failed++
		case customaction.StepSkipped:
			skipped++
		}
	}
	if failed == 0 && skipped == 0 {
		base := fmt.Sprintf("✓ %s: %d step(s) OK", name, ok)
		if len(nonUndoable) > 0 {
			base += fmt.Sprintf(" (%d not reversible by `u`: %s)", len(nonUndoable), strings.Join(nonUndoable, ", "))
		}
		return base
	}
	// Multi-line: header + one row per step.
	var b strings.Builder
	fmt.Fprintf(&b, "custom action %q:", name)
	for _, r := range res.Steps {
		b.WriteString("\n  ")
		switch r.Status {
		case customaction.StepOK:
			b.WriteString("✓ ")
		case customaction.StepFailed:
			b.WriteString("✗ ")
		case customaction.StepSkipped:
			b.WriteString("– ")
		}
		b.WriteString(string(r.Op))
		if r.Message != "" {
			b.WriteString(": ")
			b.WriteString(r.Message)
		}
		if r.NonUndoable {
			b.WriteString(" [non-undoable]")
		}
	}
	return b.String()
}

// keyMsgString converts a tea.KeyMsg into the canonical string form
// the customaction loader's keyRegex (and key.NewBinding) expects:
// a literal rune, ctrl+<rune>, or alt+<rune>. Returns "" for keys
// outside that surface so the caller falls through.
func keyMsgString(msg tea.KeyMsg) string {
	if msg.Type == tea.KeyRunes && len(msg.Runes) == 1 {
		// Plain rune — handle alt-modifier (Bubble Tea sets msg.Alt).
		if msg.Alt {
			return "alt+" + string(msg.Runes[0])
		}
		return string(msg.Runes[0])
	}
	// Bubble Tea encodes ctrl+<letter> as msg.Type == tea.KeyCtrlA … KeyCtrlZ.
	// String() returns "ctrl+a" etc.; reuse it.
	s := msg.String()
	if strings.HasPrefix(s, "ctrl+") || strings.HasPrefix(s, "alt+") {
		return s
	}
	return ""
}

// dispatchCustomActionKey scans the catalogue's ByKey index for a
// match against the supplied tea.KeyMsg. Returns (model, cmd, true)
// when a match dispatches; (m, nil, false) otherwise.
func (m Model) dispatchCustomActionKey(keyStr string) (tea.Model, tea.Cmd, bool) {
	if m.customActions == nil || keyStr == "" {
		return m, nil, false
	}
	a, ok := m.customActions.ByKey[keyStr]
	if !ok {
		return m, nil, false
	}
	// Pane-scope check: only dispatch if the action's When list
	// includes the current pane.
	scope := scopeForPane(m.focused)
	if !actionAllowsScope(a, scope) {
		return m, nil, false
	}
	mm, cmd := m.runCustomActionByName(a.Name)
	return mm, cmd, true
}

// scopeForPane maps the runtime focused pane into the customaction
// Scope enum used by the When list.
func scopeForPane(p Pane) customaction.Scope {
	switch p {
	case ListPane:
		return customaction.ScopeList
	case ViewerPane:
		return customaction.ScopeViewer
	case FoldersPane:
		return customaction.ScopeFolders
	}
	return customaction.ScopeList
}

func actionAllowsScope(a *customaction.Action, s customaction.Scope) bool {
	for _, w := range a.When {
		if w == s {
			return true
		}
	}
	return false
}

// renderActionShow formats the body of `:actions show <name>` — used
// by the cmd-bar verb and the confirm modal. literalTemplates=true
// echoes templates verbatim; false renders against the focused-
// message context (if any).
func (m Model) renderActionShow(a *customaction.Action, literalTemplates bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s — %s", a.Name, a.Description)
	if a.Key != "" {
		fmt.Fprintf(&b, " [%s]", a.Key)
	}
	for i, s := range a.Steps {
		fmt.Fprintf(&b, "\n  %d. %s", i+1, s.Op)
		if dest, ok := s.Params["destination"].(string); ok {
			fmt.Fprintf(&b, " → %s", dest)
		}
		if cat, ok := s.Params["category"].(string); ok {
			fmt.Fprintf(&b, " [%s]", cat)
		}
		if pat, ok := s.Params["pattern"].(string); ok {
			fmt.Fprintf(&b, " %s", pat)
		}
	}
	_ = literalTemplates // Reserved for the resolved-templates path; v1.1 echoes literal.
	return b.String()
}

// renderPromptHeader renders the prompt template that paused the
// sequence. The customaction.Continuation carries Action.Steps; the
// pause's prompt_value step has the templated prompt string.
// Returns a fallback "Custom action — input:" when the prompt
// template can't be located (defensive).
func renderPromptHeader(res customaction.Result, ctx *customaction.Context) string {
	// The pausing step is the last OK row in res.Steps with op=prompt_value.
	for i := len(res.Steps) - 1; i >= 0; i-- {
		if res.Steps[i].Op == customaction.OpPromptValue && res.Steps[i].Status == customaction.StepOK {
			// Look up the prompt string from the continuation's Action.
			if res.Continuation != nil && res.Steps[i].StepIndex < len(res.Continuation.Action.Steps) {
				step := res.Continuation.Action.Steps[res.Steps[i].StepIndex]
				if t, ok := step.Templated["prompt"]; ok {
					var b strings.Builder
					if err := t.Execute(&b, ctx); err == nil {
						return b.String()
					}
				}
				if p, ok := step.Params["prompt"].(string); ok {
					return p
				}
			}
		}
	}
	return "Custom action — input:"
}

// renderCustomActionPromptModal renders the modal body shown while
// CustomActionPromptMode is active. Single-line input with the
// resolved prompt template as the header.
func renderCustomActionPromptModal(theme Theme, header, buf string) string {
	var b strings.Builder
	b.WriteString(theme.Bold.Render("Custom action — prompt"))
	b.WriteString("\n\n")
	if header != "" {
		b.WriteString(header)
		b.WriteString("\n")
	}
	b.WriteString("> " + buf + "_")
	b.WriteString("\n\n")
	b.WriteString(theme.Dim.Render("Enter to confirm · Esc to cancel"))
	return b.String()
}

// dispatchActions handles the `:actions` cmd-bar verb (spec 27 §4.10).
// Forms: `:actions` / `:actions list`, `:actions show <name>`,
// `:actions run <name>`. v1.1 deliberately omits `reload` (CLAUDE.md
// §9 — no hot reload).
func (m Model) dispatchActions(args []string) (tea.Model, tea.Cmd) {
	if len(args) == 0 || args[0] == "list" {
		m.engineActivity = m.listCustomActionsForCmdBar()
		return m, nil
	}
	switch args[0] {
	case "show":
		if len(args) < 2 {
			m.lastError = fmt.Errorf("actions show: usage :actions show <name>")
			return m, nil
		}
		name := args[1]
		if m.customActions == nil {
			m.lastError = fmt.Errorf("custom actions: catalogue not loaded")
			return m, nil
		}
		a, ok := m.customActions.ByName[name]
		if !ok {
			m.lastError = fmt.Errorf("custom action %q not found", name)
			return m, nil
		}
		m.engineActivity = m.renderActionShow(a, true)
		return m, nil
	case "run":
		if len(args) < 2 {
			m.lastError = fmt.Errorf("actions run: usage :actions run <name>")
			return m, nil
		}
		return m.runCustomActionByName(args[1])
	default:
		m.lastError = fmt.Errorf("actions: unknown subcommand %q (try list / show / run)", args[0])
		return m, nil
	}
}

// listCustomActionsForCmdBar formats the `:actions list` overlay body.
func (m Model) listCustomActionsForCmdBar() string {
	if m.customActions == nil || len(m.customActions.Actions) == 0 {
		return "no custom actions configured — see `docs/user/how-to.md#custom-actions`"
	}
	names := make([]string, 0, len(m.customActions.Actions))
	for _, a := range m.customActions.Actions {
		names = append(names, a.Name)
	}
	sort.Strings(names)
	var b strings.Builder
	b.WriteString("Custom actions:")
	for _, n := range names {
		a := m.customActions.ByName[n]
		b.WriteString("\n  ")
		if a.Key != "" {
			fmt.Fprintf(&b, "[%s] ", a.Key)
		}
		b.WriteString(a.Name)
		if a.Description != "" {
			b.WriteString(" — ")
			b.WriteString(a.Description)
		}
	}
	return b.String()
}
