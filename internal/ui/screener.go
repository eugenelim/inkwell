package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/eugenelim/inkwell/internal/config"
	"github.com/eugenelim/inkwell/internal/store"
)

// writeUIFlagBool wraps config.WriteUIFlag for the screener path.
// Centralising the call site makes it cheap to swap implementations
// in tests (e.g. via a Deps-supplied writer) without spreading the
// config-package import.
func writeUIFlagBool(path, key string, value bool) error {
	return config.WriteUIFlag(path, key, value)
}

// pendingSendersLoadedMsg carries the rendered Screener queue rows
// for the per-sender grouping path (spec 28 §5.1).
type pendingSendersLoadedMsg struct {
	FolderID string // always screenerSentinelID
	Senders  []store.PendingSender
}

// screenerSidebarUpdatedMsg refreshes the per-screener sidebar
// counts (pending senders + screened-out messages) so the gate-on
// sidebar matches the live store. Spec 28 §5.1 / §5.2.
type screenerSidebarUpdatedMsg struct {
	Pending     int
	ScreenedOut int
}

// loadPendingSendersCmd reads ListPendingSenders for the current
// account and returns a pendingSendersLoadedMsg. Used when the user
// selects __screener__ with [screener].grouping = "sender".
func (m Model) loadPendingSendersCmd() tea.Cmd {
	if m.deps.Store == nil {
		return nil
	}
	limit := m.list.LoadLimit()
	cap := m.screenerMaxCountPerSender
	excludeMuted := m.screenerExcludeMuted
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		var accountID int64
		if m.deps.Account != nil {
			accountID = m.deps.Account.ID
		}
		rows, err := m.deps.Store.ListPendingSenders(ctx, accountID, limit, cap, excludeMuted)
		if err != nil {
			return ErrorMsg{Err: err}
		}
		return pendingSendersLoadedMsg{FolderID: screenerSentinelID, Senders: rows}
	}
}

// loadPendingMessagesCmd reads ListPendingMessages and emits the
// regular MessagesLoadedMsg envelope so the list pane renders one
// row per message (per-message grouping mode, spec 28 §5.1).
func (m Model) loadPendingMessagesCmd() tea.Cmd {
	if m.deps.Store == nil {
		return nil
	}
	limit := m.list.LoadLimit()
	excludeMuted := m.screenerExcludeMuted
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		var accountID int64
		if m.deps.Account != nil {
			accountID = m.deps.Account.ID
		}
		msgs, err := m.deps.Store.ListPendingMessages(ctx, accountID, limit, excludeMuted)
		if err != nil {
			return ErrorMsg{Err: err}
		}
		return MessagesLoadedMsg{FolderID: screenerSentinelID, Messages: msgs}
	}
}

// loadScreenedOutMessagesCmd reads the spec 28 §5.2 Screened-Out
// virtual folder. Sentinel ID screenedOutSentinelID is set on the
// envelope so the list pane recognises it.
func (m Model) loadScreenedOutMessagesCmd() tea.Cmd {
	if m.deps.Store == nil {
		return nil
	}
	limit := m.list.LoadLimit()
	excludeMuted := m.screenerExcludeMuted
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		var accountID int64
		if m.deps.Account != nil {
			accountID = m.deps.Account.ID
		}
		msgs, err := m.deps.Store.ListScreenedOutMessages(ctx, accountID, limit, excludeMuted)
		if err != nil {
			return ErrorMsg{Err: err}
		}
		return MessagesLoadedMsg{FolderID: screenedOutSentinelID, Messages: msgs}
	}
}

// refreshScreenerSidebarCmd queries CountPendingSenders +
// CountScreenedOutMessages and emits a screenerSidebarUpdatedMsg so
// the sidebar's __screener__ and __screened_out__ badges stay
// accurate. Called only when the gate is on (CLAUDE.md §3 invariant
// 2: UI never blocks on I/O, so the gate-on count source replaces
// the spec 23 path; gate-off path keeps the spec 23 behaviour).
func (m Model) refreshScreenerSidebarCmd() tea.Cmd {
	if m.deps.Store == nil {
		return nil
	}
	if !m.screenerEnabled {
		return nil
	}
	excludeMuted := m.screenerExcludeMuted
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		var accountID int64
		if m.deps.Account != nil {
			accountID = m.deps.Account.ID
		}
		pending, err := m.deps.Store.CountPendingSenders(ctx, accountID, excludeMuted)
		if err != nil {
			return nil
		}
		screened, err := m.deps.Store.CountScreenedOutMessages(ctx, accountID, excludeMuted)
		if err != nil {
			return nil
		}
		return screenerSidebarUpdatedMsg{Pending: pending, ScreenedOut: screened}
	}
}

// screenerGateFlipModalMsg fires at Init when the gate flipped
// false→true since the last launch (spec 28 §5.3.1). The handler
// renders a Confirm modal naming the message-from-pending-senders
// count so the user knows what's about to vanish from their Inbox.
type screenerGateFlipModalMsg struct {
	MessagesFromPending int
	PendingSenders      int
}

// detectScreenerGateFlipCmd runs once at boot. If
// cfg.Screener.Enabled is true AND
// [ui].screener_last_seen_enabled is false, count the messages
// affected and fire the §5.3.1 modal. When the count is zero, the
// marker is advanced silently and no modal renders. When the gate
// is off but the marker is true, the marker is reset (disable path
// non-destructive). Otherwise no-op.
func (m Model) detectScreenerGateFlipCmd() tea.Cmd {
	if m.deps.Store == nil || m.deps.Account == nil {
		return nil
	}
	enabled := m.screenerEnabled
	lastSeen := m.screenerLastSeenEnabled
	if enabled == lastSeen {
		return nil // no transition
	}
	configPath := m.screenerConfigPath
	excludeMuted := m.screenerExcludeMuted
	accountID := m.deps.Account.ID
	store := m.deps.Store
	logger := m.deps.Logger
	if !enabled && lastSeen {
		// Disable path: reset the marker silently. Mail re-appears in
		// default folder views by virtue of m.screenerEnabled = false.
		return func() tea.Msg {
			if configPath != "" {
				if err := writeUIFlagBool(configPath, "screener_last_seen_enabled", false); err != nil && logger != nil {
					logger.Warn("screener: reset last-seen marker", "err", err)
				}
			}
			return nil
		}
	}
	// enabled && !lastSeen: count messages from pending senders to
	// decide whether the §5.3.1 modal should render.
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		count, err := store.CountMessagesFromPendingSenders(ctx, accountID, excludeMuted)
		if err != nil {
			if logger != nil {
				logger.Warn("screener: count pending messages", "err", err)
			}
			return nil
		}
		if count == 0 {
			// Skip modal; advance the marker so future launches don't
			// re-prompt; the post-enable hint still fires per usual.
			if configPath != "" {
				if err := writeUIFlagBool(configPath, "screener_last_seen_enabled", true); err != nil && logger != nil {
					logger.Warn("screener: persist last-seen marker", "err", err)
				}
			}
			return screenerGateConfirmedSilentlyMsg{}
		}
		senders, err := store.CountPendingSenders(ctx, accountID, excludeMuted)
		if err != nil {
			senders = 0
		}
		return screenerGateFlipModalMsg{MessagesFromPending: count, PendingSenders: senders}
	}
}

// screenerGateConfirmedSilentlyMsg is dispatched when the gate-flip
// detection finds zero pending mail — the marker was advanced
// silently and the §5.3.2 hint should fire on the first list-pane
// render.
type screenerGateConfirmedSilentlyMsg struct{}

// reloadStreamCmd picks the correct loader for the currently
// displayed stream-sentinel folder, accounting for the spec 28
// gate flip on __screener__ and the new __screened_out__ sentinel.
// Returns nil when the displayed folder is not a stream sentinel.
func (m Model) reloadStreamCmd() tea.Cmd {
	switch m.list.FolderID {
	case screenerSentinelID:
		if m.screenerEnabled {
			if m.screenerGrouping == "message" {
				return m.loadPendingMessagesCmd()
			}
			return m.loadPendingSendersCmd()
		}
		return m.loadByRoutingCmd("screener")
	case screenedOutSentinelID:
		return m.loadScreenedOutMessagesCmd()
	}
	if dest := streamDestinationFromID(m.list.FolderID); dest != "" {
		return m.loadByRoutingCmd(dest)
	}
	return nil
}

// buildScreenerPaletteRows returns the four spec 28 §5.9 palette
// rows: screener_accept / screener_reject / screener_open /
// screener_history. Available() honours the spec — accept/reject
// require a focused message with a from_address; history only
// renders when the gate is on.
//
// When the gate is off, accept/reject titles rewrite to a more
// honest "Route focused sender" form so the row reads correctly
// in a Screener-less context.
func buildScreenerPaletteRows(m *Model, msg *store.Message) []PaletteRow {
	hasFrom := availTrue
	addr := ""
	if msg != nil {
		addr = strings.TrimSpace(msg.FromAddress)
	}
	if addr == "" {
		hasFrom = notWired("screener: focused message has no from-address")
	}
	storeAvail := availTrue
	if m.deps.Store == nil {
		storeAvail = notWired("screener: not wired (CLI mode or unsigned)")
	}
	combined := hasFrom
	if !storeAvail.OK {
		combined = storeAvail
	}
	var accountID int64
	if m.deps.Account != nil {
		accountID = m.deps.Account.ID
	}
	st := m.deps.Store

	acceptTitle := "Admit focused sender to Imbox"
	rejectTitle := "Screen out focused sender"
	if !m.screenerEnabled {
		acceptTitle = "Route focused sender → Imbox (Y)"
		rejectTitle = "Screen out focused sender (N)"
	}

	rows := []PaletteRow{
		{
			ID: "screener_accept", Title: acceptTitle,
			Binding: "Y", Section: sectionCommands, Available: combined,
			RunFn: func(mm Model) (tea.Model, tea.Cmd) {
				if addr == "" || st == nil {
					return mm, nil
				}
				return mm, routeCmd(st, accountID, addr, "imbox")
			},
		},
		{
			ID: "screener_reject", Title: rejectTitle,
			Binding: "N", Section: sectionCommands, Available: combined,
			RunFn: func(mm Model) (tea.Model, tea.Cmd) {
				if addr == "" || st == nil {
					return mm, nil
				}
				return mm, routeCmd(st, accountID, addr, "screener")
			},
		},
		{
			ID: "screener_open", Title: "Open Screener queue",
			Binding: ":screener list", Section: sectionCommands,
			Available: storeAvail,
			RunFn: func(mm Model) (tea.Model, tea.Cmd) {
				return mm.dispatchScreener([]string{"list"})
			},
		},
	}
	if m.screenerEnabled {
		rows = append(rows, PaletteRow{
			ID: "screener_history", Title: "Open Screened-Out history",
			Binding: ":screener history", Section: sectionCommands,
			Available: storeAvail,
			RunFn: func(mm Model) (tea.Model, tea.Cmd) {
				return mm.dispatchScreener([]string{"history"})
			},
		})
	}
	return rows
}

// dispatchScreener handles the `:screener <verb>` cmd-bar form.
// Verbs (per spec 28 §7.1):
//
//	accept <addr> [--to imbox|feed|paper_trail]
//	reject <addr>
//	list                     — navigate to __screener__
//	history                  — navigate to __screened_out__
//	status                   — toast with the current state
func (m Model) dispatchScreener(args []string) (tea.Model, tea.Cmd) {
	if len(args) == 0 {
		m.lastError = fmt.Errorf("screener: usage `:screener accept|reject|list|history|status`")
		return m, nil
	}
	verb := args[0]
	rest := args[1:]
	switch verb {
	case "list":
		m.list.FolderID = screenerSentinelID
		m.list.ResetLimit()
		m.focused = ListPane
		m.searchActive = false
		m.searchQuery = ""
		return m, m.reloadStreamCmd()
	case "history":
		if !m.screenerEnabled {
			m.lastError = fmt.Errorf("screener: history requires [screener].enabled = true")
			return m, nil
		}
		m.list.FolderID = screenedOutSentinelID
		m.list.ResetLimit()
		m.focused = ListPane
		m.searchActive = false
		m.searchQuery = ""
		return m, m.loadScreenedOutMessagesCmd()
	case "status":
		m.engineActivity = fmt.Sprintf("screener: enabled=%v grouping=%s exclude_muted=%v",
			m.screenerEnabled, m.screenerGrouping, m.screenerExcludeMuted)
		return m, nil
	case "accept":
		if len(rest) == 0 {
			m.lastError = fmt.Errorf("screener accept: usage `:screener accept <address> [--to imbox|feed|paper_trail]`")
			return m, nil
		}
		addr, dest, err := parseScreenerAcceptArgs(rest)
		if err != nil {
			m.lastError = err
			return m, nil
		}
		if m.deps.Store == nil || m.deps.Account == nil {
			m.lastError = fmt.Errorf("screener: store not wired")
			return m, nil
		}
		return m, routeCmd(m.deps.Store, m.deps.Account.ID, addr, dest)
	case "reject":
		if len(rest) == 0 {
			m.lastError = fmt.Errorf("screener reject: usage `:screener reject <address>`")
			return m, nil
		}
		addr := normalizeFromAddress(rest[0])
		if !looksLikeBareAddress(addr) {
			m.lastError = fmt.Errorf("screener reject: %q is not a bare address (no display name / brackets)", rest[0])
			return m, nil
		}
		if m.deps.Store == nil || m.deps.Account == nil {
			m.lastError = fmt.Errorf("screener: store not wired")
			return m, nil
		}
		return m, routeCmd(m.deps.Store, m.deps.Account.ID, addr, "screener")
	}
	m.lastError = fmt.Errorf("screener: unknown verb %q (try list / history / accept / reject / status)", verb)
	return m, nil
}

// parseScreenerAcceptArgs validates the `accept <address> [--to <dest>]`
// form. Defaults to imbox; rejects --to screener (use `:screener
// reject` for that).
func parseScreenerAcceptArgs(args []string) (addr, dest string, err error) {
	dest = "imbox"
	pos := []string{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--to" || a == "-t" {
			if i+1 >= len(args) {
				return "", "", fmt.Errorf("screener accept: --to requires a destination")
			}
			i++
			dest = args[i]
			continue
		}
		if strings.HasPrefix(a, "--to=") {
			dest = strings.TrimPrefix(a, "--to=")
			continue
		}
		pos = append(pos, a)
	}
	if len(pos) != 1 {
		return "", "", fmt.Errorf("screener accept: expected one address, got %d", len(pos))
	}
	addr = normalizeFromAddress(pos[0])
	if !looksLikeBareAddress(addr) {
		return "", "", fmt.Errorf("screener accept: %q is not a bare address", pos[0])
	}
	switch dest {
	case "imbox", "feed", "paper_trail":
	case "screener":
		return "", "", fmt.Errorf("screener accept: --to=screener rejected; use `:screener reject` for screening-out")
	default:
		return "", "", fmt.Errorf("screener accept: unknown destination %q (imbox|feed|paper_trail)", dest)
	}
	return addr, dest, nil
}

// looksLikeBareAddress is a cheap shape check so display-name input
// like "Bob <bob@x>" surfaces a friendly error rather than running
// through the lower/trim path and getting normalised into garbage.
func looksLikeBareAddress(s string) bool {
	if s == "" {
		return false
	}
	if strings.ContainsAny(s, "<>\"'") {
		return false
	}
	if !strings.Contains(s, "@") {
		return false
	}
	return true
}

// dispatchScreenerVerb performs the §5.4 Y/N pane-scoped action.
// destination is "imbox" (Y → admit) or "screener" (N → screen
// out). Captures the focused address synchronously per §5.4
// concurrent-decision semantics; rapid keypresses dispatch each
// against its own captured address with no debounce. The action
// queue is bypassed (spec 23 §6); the SQLite write lock plus the
// (account_id, email_address) PK conflict-target serialise
// concurrent upserts.
func (m Model) dispatchScreenerVerb(destination string) (tea.Model, tea.Cmd) {
	addr := m.focusedScreenerAddress()
	if addr == "" {
		m.engineActivity = "screener: focused sender has no from-address"
		return m, nil
	}
	if m.deps.Store == nil || m.deps.Account == nil {
		return m, nil
	}
	return m, routeCmd(m.deps.Store, m.deps.Account.ID, addr, destination)
}

// focusedScreenerAddress returns the lowercased from_address of the
// currently-focused list-pane row for use by the §5.4 Y/N
// shortcuts. Returns "" when the focus has no actionable sender —
// Y/N will toast "screener: focused sender has no from-address".
//
// In per-sender grouping mode the list pane renders one row per
// PendingSender; SelectedMessage returns the latest representative
// message (whose FromAddress is the sender's address). In per-
// message mode each row is already a regular message. Either way
// SelectedMessage().FromAddress is the address to act on.
func (m Model) focusedScreenerAddress() string {
	msg, ok := m.list.SelectedMessage()
	if !ok {
		return ""
	}
	return normalizeFromAddress(msg.FromAddress)
}

// normalizeFromAddress lowers and trims the From address. Mirrors
// store.NormalizeEmail without the package-level import (we already
// have it via routing helpers, but a local helper keeps the
// screener path self-contained).
func normalizeFromAddress(s string) string {
	out := make([]byte, 0, len(s))
	// Trim whitespace; lowercase letters; preserve everything else.
	start, end := 0, len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t') {
		end--
	}
	for i := start; i < end; i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		out = append(out, c)
	}
	return string(out)
}
