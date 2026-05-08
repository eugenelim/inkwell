package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/eugenelim/inkwell/internal/store"
)

// Sentinel folder IDs for the spec 25 inkwell stacks. Same
// double-underscore convention as the spec 19 muted sentinel and the
// spec 23 stream sentinels.
const (
	replyLaterSentinelID = "__reply_later__"
	setAsideSentinelID   = "__set_aside__"
)

// IsStackSentinelID reports whether id is one of the two spec 25
// stack virtual-folder sentinels.
func IsStackSentinelID(id string) bool {
	switch id {
	case replyLaterSentinelID, setAsideSentinelID:
		return true
	}
	return false
}

// stackCategoryFromID returns the matching reserved category for a
// stack sentinel ID, or "" when id is not a stack sentinel.
func stackCategoryFromID(id string) string {
	switch id {
	case replyLaterSentinelID:
		return store.CategoryReplyLater
	case setAsideSentinelID:
		return store.CategorySetAside
	}
	return ""
}

// stackDisplayName returns the user-facing label for a stack
// sentinel. Mirrors `streamDisplayLabelForDestination`.
func stackDisplayName(category string) string {
	switch category {
	case store.CategoryReplyLater:
		return "Reply Later"
	case store.CategorySetAside:
		return "Set Aside"
	}
	return ""
}

// stackGlyph returns the row glyph for a category, honouring the
// theme override. Spec 25 §5.2.
func stackGlyph(category string, t Theme) string {
	switch category {
	case store.CategoryReplyLater:
		if t.ReplyLaterIndicator != "" {
			return t.ReplyLaterIndicator
		}
		return "↩"
	case store.CategorySetAside:
		if t.SetAsideIndicator != "" {
			return t.SetAsideIndicator
		}
		return "📌"
	}
	return ""
}

// stackToggleMsg arrives after a single-message L / P dispatch.
type stackToggleMsg struct {
	category string
	added    bool
	address  string // toast string fragments
	subject  string
	err      error
}

// stackCountsUpdatedMsg carries the refreshed sidebar badge counts
// for the two stacks (spec 25 §10.4).
type stackCountsUpdatedMsg struct {
	replyLater int
	setAside   int
}

// toggleStackCmd dispatches AddCategory or RemoveCategory based on
// the focused message's current Categories slice.
func toggleStackCmd(triage TriageExecutor, accountID int64, msg store.Message, category string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if store.IsInCategory(msg.Categories, category) {
			if err := triage.RemoveCategory(ctx, accountID, msg.ID, category); err != nil {
				return stackToggleMsg{category: category, added: false, err: err, subject: msg.Subject}
			}
			return stackToggleMsg{category: category, added: false, subject: msg.Subject}
		}
		if err := triage.AddCategory(ctx, accountID, msg.ID, category); err != nil {
			return stackToggleMsg{category: category, added: true, err: err, subject: msg.Subject}
		}
		return stackToggleMsg{category: category, added: true, subject: msg.Subject}
	}
}

// startStackToggle resolves the focused message and dispatches a
// stack toggle. Used by both list and viewer dispatchers.
func (m Model) startStackToggle(category string) (Model, tea.Cmd) {
	if m.deps.Triage == nil {
		m.lastError = fmt.Errorf("stack: not wired (CLI mode or unsigned)")
		return m, nil
	}
	var focused *store.Message
	if cur := m.viewer.current; cur != nil && m.focused == ViewerPane {
		focused = cur
	} else if sel, ok := m.list.SelectedMessage(); ok {
		s := sel
		focused = &s
	}
	if focused == nil {
		m.lastError = fmt.Errorf("stack: no message focused")
		return m, nil
	}
	var accountID int64
	if m.deps.Account != nil {
		accountID = m.deps.Account.ID
	}
	return m, toggleStackCmd(m.deps.Triage, accountID, *focused, category)
}

// formatStackToggleToast builds the user-facing toast for a stack
// toggle result (spec 25 §5.6).
func formatStackToggleToast(t Theme, msg stackToggleMsg) string {
	g := stackGlyph(msg.category, t)
	name := stackDisplayName(msg.category)
	subj := msg.subject
	if msg.err != nil {
		return fmt.Sprintf("stack: %s failed: %v", name, msg.err)
	}
	verb := "added to"
	if !msg.added {
		verb = "removed from"
	}
	if msg.added && subj != "" {
		return fmt.Sprintf("✓ %s %s %s (subject: %s)", g, verb, name, truncateSubject(subj, 40))
	}
	return fmt.Sprintf("✓ %s %s %s", g, verb, name)
}

func truncateSubject(s string, max int) string {
	if len([]rune(s)) <= max {
		return s
	}
	rs := []rune(s)
	return string(rs[:max-1]) + "…"
}

// refreshStackCountsCmd queries CountMessagesInCategory for both
// stacks and returns a stackCountsUpdatedMsg. Spec 25 §10.4.
func (m Model) refreshStackCountsCmd() tea.Cmd {
	if m.deps.Store == nil {
		return nil
	}
	var accountID int64
	if m.deps.Account != nil {
		accountID = m.deps.Account.ID
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		rl, err := m.deps.Store.CountMessagesInCategory(ctx, accountID, store.CategoryReplyLater)
		if err != nil {
			return nil
		}
		sa, err := m.deps.Store.CountMessagesInCategory(ctx, accountID, store.CategorySetAside)
		if err != nil {
			return nil
		}
		return stackCountsUpdatedMsg{replyLater: rl, setAside: sa}
	}
}

// loadStackMessagesCmd loads the message list for a stack virtual
// folder. Returns a MessagesLoadedMsg whose FolderID is the
// matching sentinel so the list pane can identify the view.
func (m Model) loadStackMessagesCmd(category, sentinel string) tea.Cmd {
	if m.deps.Store == nil {
		return nil
	}
	limit := m.list.LoadLimit()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		var accountID int64
		if m.deps.Account != nil {
			accountID = m.deps.Account.ID
		}
		msgs, err := m.deps.Store.ListMessagesInCategory(ctx, accountID, category, limit)
		if err != nil {
			return ErrorMsg{Err: err}
		}
		return MessagesLoadedMsg{FolderID: sentinel, Messages: msgs}
	}
}

// stackChordVerb returns the human-friendly verb string for a
// thread-chord category mutation (spec 25 §5.6 toast).
func stackChordVerb(category string, removing bool) string {
	name := stackDisplayName(category)
	if removing {
		return "removed thread from " + name
	}
	return "added thread to " + name
}

// focusQueueLoadedMsg is delivered when :focus has pre-fetched the
// Reply Later queue. The model transitions into focus mode and
// opens compose for index 0 (or the requested 1-based offset minus
// one).
type focusQueueLoadedMsg struct {
	ids        []string
	startIndex int // 0-based
	prevFolder string
	err        error
}

// focusOpenIndexMsg drives the open-message-N step. Carries the
// store ID resolved from focusQueueIDs[focusIndex].
type focusOpenIndexMsg struct {
	id string
}

// startFocusMode parses :focus [N] and pre-fetches the queue.
func (m Model) startFocusMode(args []string) (tea.Model, tea.Cmd) {
	if m.deps.Store == nil {
		m.lastError = fmt.Errorf("focus: not wired (CLI mode or unsigned)")
		return m, nil
	}
	if m.deps.Drafts == nil {
		m.lastError = fmt.Errorf("focus: drafts not wired (compose layer required)")
		return m, nil
	}
	startIdx := 0
	if len(args) >= 1 {
		var n int
		if _, err := fmt.Sscanf(args[0], "%d", &n); err != nil {
			m.lastError = fmt.Errorf("focus: invalid index (must be a positive integer)")
			return m, nil
		}
		if n < 1 {
			m.lastError = fmt.Errorf("focus: invalid index (must be ≥ 1)")
			return m, nil
		}
		startIdx = n - 1
	}
	limit := 200
	if m.deps.FocusQueueLimit > 0 {
		limit = m.deps.FocusQueueLimit
	}
	prevFolder := m.list.FolderID
	var accountID int64
	if m.deps.Account != nil {
		accountID = m.deps.Account.ID
	}
	st := m.deps.Store
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		msgs, err := st.ListMessagesInCategory(ctx, accountID, store.CategoryReplyLater, limit)
		if err != nil {
			return focusQueueLoadedMsg{err: err}
		}
		ids := make([]string, 0, len(msgs))
		for _, x := range msgs {
			ids = append(ids, x.ID)
		}
		return focusQueueLoadedMsg{ids: ids, startIndex: startIdx, prevFolder: prevFolder}
	}
}

// focusActivate enters focus mode at focusIndex and opens compose
// for the current message. Used both at startup and when the
// compose-exit observer advances the queue.
func (m Model) focusActivate() (Model, tea.Cmd) {
	if m.focusIndex < 0 || m.focusIndex >= len(m.focusQueueIDs) {
		return m.focusEnd()
	}
	id := m.focusQueueIDs[m.focusIndex]
	if m.deps.Store == nil {
		return m.focusEnd()
	}
	st := m.deps.Store
	cmd := func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		msg, err := st.GetMessage(ctx, id)
		if err != nil || msg == nil {
			return focusOpenIndexMsg{id: ""}
		}
		return focusOpenIndexMsg{id: id}
	}
	return m, cmd
}

// focusEnd exits focus mode, restores the prior folder, and clears
// all focus-mode model fields. Spec 25 §5.7 step 7.
func (m Model) focusEnd() (Model, tea.Cmd) {
	processed := len(m.focusQueueIDs)
	if processed > 0 {
		m.engineActivity = fmt.Sprintf("focus: queue cleared (%d messages processed)", processed)
	}
	prev := m.focusReturnFolderID
	m.focusModeActive = false
	m.focusQueueIDs = nil
	m.focusIndex = 0
	m.focusReturnFolderID = ""
	m.focusComposePending = false
	m.focusPrevMode = NormalMode
	if prev != "" && prev != m.list.FolderID {
		m.list.FolderID = prev
		return m, m.loadMessagesCmd(prev)
	}
	return m, m.clearTransientCmd()
}

// focusStatusBarHint renders the spec 25 §5.7.1 [focus i/N] mode
// indicator. Returns "" when focus mode is inactive.
func focusStatusBarHint(m Model) string {
	if !m.focusModeActive {
		return ""
	}
	return fmt.Sprintf("[focus %d/%d]", m.focusIndex+1, len(m.focusQueueIDs))
}

var _ = strings.TrimSpace // keep strings import alive
