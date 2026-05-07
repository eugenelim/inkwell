package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/eugenelim/inkwell/internal/store"
)

// streamChordTimeoutMsg cancels an in-progress S<dest> chord when no
// second key arrives within the timeout window (spec 23 §5.3 / §5.1).
// The token field allows stale timeout messages to be discarded —
// each new `S` press bumps the token; the old timeout's token no
// longer matches the model's, and the handler ignores it.
type streamChordTimeoutMsg struct{ token uint64 }

// streamChordTimeout returns a Cmd that fires streamChordTimeoutMsg
// after 3 seconds if no second key is pressed. Same shape as
// threadChordTimeout (spec 20).
func streamChordTimeout(token uint64) tea.Cmd {
	return func() tea.Msg {
		<-time.After(3 * time.Second)
		return streamChordTimeoutMsg{token: token}
	}
}

// routeErrMsg, routedMsg, and routeNoopMsg are the typed messages the
// routing dispatch path emits. None of these implement String() /
// Error() — the toast renderer reads the fields directly so subject
// lines and PII never leak into slog (spec 23 §5.6 toast-vs-log
// boundary).
type routeErrMsg struct {
	err error
}

type routedMsg struct {
	address   string
	dest      string // empty when this was a `S c` (clear)
	priorDest string
}

type routeNoopMsg struct {
	address string
	kind    string // "already" | "unrouted"
	dest    string // populated when kind == "already"
}

// streamCountsUpdatedMsg carries the refreshed per-destination
// routing counts for the sidebar badge (spec 23 §5.4).
type streamCountsUpdatedMsg struct{ counts map[string]int }

// routeCmd dispatches a single routing assignment (or clear when
// destination == ""). Returns a tea.Msg via the typed routing
// message types — never an Error string that could land in slog.
//
// Per spec 19 §6 ctx-capture warning: tea.Cmd goroutines must not
// capture a context from the synchronous Update call. Each Cmd
// builds its own bounded context.
func routeCmd(st store.Store, accountID int64, fromAddress, destination string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if destination == "" {
			prior, err := st.ClearSenderRouting(ctx, accountID, fromAddress)
			if err != nil {
				return routeErrMsg{err: err}
			}
			if prior == "" {
				return routeNoopMsg{address: fromAddress, kind: "unrouted"}
			}
			return routedMsg{address: fromAddress, dest: "", priorDest: prior}
		}
		prior, err := st.SetSenderRouting(ctx, accountID, fromAddress, destination)
		if err != nil {
			return routeErrMsg{err: err}
		}
		if prior == destination {
			return routeNoopMsg{address: fromAddress, kind: "already", dest: destination}
		}
		return routedMsg{address: fromAddress, dest: destination, priorDest: prior}
	}
}

// refreshStreamCountsCmd queries CountMessagesByRoutingAll and
// returns a streamCountsUpdatedMsg so the sidebar's stream badges
// stay accurate. Spec 23 §5.4: refreshed on initial sidebar load,
// after every routing assignment, and on the spec 11 background
// refresh tick.
func (m Model) refreshStreamCountsCmd() tea.Cmd {
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
		counts, err := m.deps.Store.CountMessagesByRoutingAll(ctx, accountID, true)
		if err != nil {
			// Non-fatal — bucket badges just won't refresh.
			return nil
		}
		return streamCountsUpdatedMsg{counts: counts}
	}
}

// startStreamChord sets the stream-chord-pending flag, bumps the
// token, paints the status hint, and returns the timeout Cmd. Used
// by both the list-pane and viewer-pane S keypress paths.
func (m Model) startStreamChord() (Model, tea.Cmd) {
	m.streamChordToken++
	m.streamChordPending = true
	m.engineActivity = "stream: i/f/p/k/c  esc cancel"
	return m, streamChordTimeout(m.streamChordToken)
}

// dispatchStreamChord handles the second keypress of an S<dest>
// chord (or Esc to cancel). Returns ok=false when the second key
// is unrecognised — the caller surfaces the original key as a
// non-routing fall-through is rejected per spec 23 §5.1
// chord-pending discipline ("any unrecognised key cancels and the
// original action does NOT fire"). The 'T' second key cancels
// stream chord without starting thread chord (cross-chord
// cancel, §5.1).
func (m Model) dispatchStreamChord(msg tea.KeyMsg, focused *store.Message) (Model, tea.Cmd) {
	m.streamChordPending = false
	m.engineActivity = ""
	if msg.Type == tea.KeyEsc {
		m.engineActivity = "stream chord cancelled"
		return m, nil
	}
	// Cross-chord cancel: the chord prefix of the *other* chord ('T'
	// while stream-pending; spec 23 §5.1) cancels without entering
	// the new chord. Symmetric self-cancel ('S' while stream-pending)
	// also lands here as an unrecognised second key.
	if msg.Type == tea.KeyRunes && len(msg.Runes) == 1 {
		switch msg.Runes[0] {
		case 'T', 'S':
			m.engineActivity = "stream chord cancelled"
			return m, nil
		}
	}
	if focused == nil {
		m.lastError = fmt.Errorf("stream: no message focused")
		return m, nil
	}
	addr := strings.TrimSpace(focused.FromAddress)
	if addr == "" {
		m.lastError = fmt.Errorf("route: focused message has no from-address")
		return m, nil
	}
	var accountID int64
	if m.deps.Account != nil {
		accountID = m.deps.Account.ID
	}
	if msg.Type != tea.KeyRunes || len(msg.Runes) != 1 {
		// Unrecognised key — cancel silently per §5.1.
		return m, nil
	}
	switch msg.Runes[0] {
	case 'i':
		return m, routeCmd(m.deps.Store, accountID, addr, "imbox")
	case 'f':
		return m, routeCmd(m.deps.Store, accountID, addr, "feed")
	case 'p':
		return m, routeCmd(m.deps.Store, accountID, addr, "paper_trail")
	case 'k':
		return m, routeCmd(m.deps.Store, accountID, addr, "screener")
	case 'c':
		return m, routeCmd(m.deps.Store, accountID, addr, "")
	}
	// Unrecognised second key — cancel silently.
	return m, nil
}

// loadByRoutingCmd loads messages for one of the four spec 23 routing
// virtual folders (Imbox / Feed / Paper Trail / Screener). Returns a
// MessagesLoadedMsg with FolderID set to the routing destination's
// sentinel ID so the list pane can identify the view.
func (m Model) loadByRoutingCmd(destination string) tea.Cmd {
	if m.deps.Store == nil {
		return nil
	}
	limit := m.list.LoadLimit()
	sentinel := streamSentinelIDForDestination(destination)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		var accountID int64
		if m.deps.Account != nil {
			accountID = m.deps.Account.ID
		}
		msgs, err := m.deps.Store.ListMessagesByRouting(ctx, accountID, destination, limit, true)
		if err != nil {
			return ErrorMsg{Err: err}
		}
		return MessagesLoadedMsg{FolderID: sentinel, Messages: msgs}
	}
}

// formatRoutedToast builds the user-facing toast string for a
// successful routing assignment (spec 23 §5.6). Address is the
// lowercased+trimmed sender; dest empty means a clear; priorDest
// drives the "(was Imbox)" reassign hint.
func formatRoutedToast(t Theme, address, dest, priorDest string) string {
	addr := strings.ToLower(strings.TrimSpace(address))
	if dest == "" {
		return "↩ cleared routing for " + addr
	}
	glyph := streamGlyphForDestination(dest, t)
	label := streamDisplayLabelForDestination(dest)
	out := fmt.Sprintf("%s routed %s → %s", glyph, addr, label)
	if priorDest != "" {
		out += " (was " + streamDisplayLabelForDestination(priorDest) + ")"
	}
	return out
}

// formatRouteNoopToast builds the user-facing toast string for a
// no-op routing call (spec 23 §5.6).
func formatRouteNoopToast(address, kind, dest string) string {
	addr := strings.ToLower(strings.TrimSpace(address))
	if kind == "unrouted" {
		return "route: " + addr + " is not routed"
	}
	return "route: " + addr + " already → " + streamDisplayLabelForDestination(dest)
}

// dispatchRoute handles `:route assign|clear|show|list …` from the
// cmd-bar (spec 23 §7.1). The CLI wrapper does richer JSON / table
// output; the cmd-bar surfaces results via the status-bar toast and
// :route list opens a modal. v1 keeps :route list as a status-bar
// summary (count per destination) — a dedicated modal can land in a
// follow-up; spec §7.1 says "opens a modal that shows the routing
// table" which is non-blocking but deferred for v1 brevity.
func (m Model) dispatchRoute(args []string) (tea.Model, tea.Cmd) {
	if len(args) == 0 {
		m.lastError = fmt.Errorf("route: usage :route assign|clear|show|list …")
		return m, nil
	}
	if m.deps.Store == nil {
		m.lastError = fmt.Errorf("route: not wired (CLI mode or unsigned)")
		return m, nil
	}
	var accountID int64
	if m.deps.Account != nil {
		accountID = m.deps.Account.ID
	}
	switch args[0] {
	case "assign":
		if len(args) < 3 {
			m.lastError = fmt.Errorf("route assign: usage :route assign <address> <destination>")
			return m, nil
		}
		addr := args[1]
		dest := args[2]
		if !validRoutingDestinationStr(dest) {
			m.lastError = fmt.Errorf(`route: unknown destination %q; expected one of imbox, feed, paper_trail, screener`, dest)
			return m, nil
		}
		if err := validateBareRouteAddress(addr); err != nil {
			m.lastError = err
			return m, nil
		}
		return m, routeCmd(m.deps.Store, accountID, addr, dest)
	case "clear":
		if len(args) < 2 {
			m.lastError = fmt.Errorf("route clear: usage :route clear <address>")
			return m, nil
		}
		addr := args[1]
		if err := validateBareRouteAddress(addr); err != nil {
			m.lastError = err
			return m, nil
		}
		return m, routeCmd(m.deps.Store, accountID, addr, "")
	case "show":
		if len(args) < 2 {
			m.lastError = fmt.Errorf("route show: usage :route show <address>")
			return m, nil
		}
		addr := args[1]
		if err := validateBareRouteAddress(addr); err != nil {
			m.lastError = err
			return m, nil
		}
		return m, routeShowCmd(m.deps.Store, accountID, addr)
	case "list":
		return m, routeListSummaryCmd(m.deps.Store, accountID)
	}
	m.lastError = fmt.Errorf(`route: unknown subcommand %q; expected assign|clear|show|list`, args[0])
	return m, nil
}

// validRoutingDestinationStr is a UI-side mirror of
// store.validRoutingDestination (which is unexported). Kept in sync
// via the routingDestinations slice (covered by a unit test in
// routing_test.go).
func validRoutingDestinationStr(dest string) bool {
	switch dest {
	case "imbox", "feed", "paper_trail", "screener":
		return true
	}
	return false
}

// validateBareRouteAddress mirrors the CLI bare-address check so
// `:route assign "Bob" <bob@…> feed` from the cmd-bar gives the same
// rejection as `inkwell route assign "Bob" <bob@…> feed`.
func validateBareRouteAddress(addr string) error {
	trimmed := strings.TrimSpace(addr)
	if trimmed == "" {
		return fmt.Errorf("route: address is empty")
	}
	if strings.ContainsAny(trimmed, "<>\"") {
		return fmt.Errorf("route: address must be bare; got %q", addr)
	}
	if !strings.Contains(trimmed, "@") {
		return fmt.Errorf("route: address must contain '@'; got %q", addr)
	}
	return nil
}

// routeShowToastMsg carries the result of `:route show <addr>` so
// the dispatch layer can surface it via the engineActivity toast.
type routeShowToastMsg struct {
	address string
	dest    string
}

// routeListSummaryMsg carries the `:route list` summary counts.
type routeListSummaryMsg struct {
	counts map[string]int
}

func routeShowCmd(st store.Store, accountID int64, address string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		dest, err := st.GetSenderRouting(ctx, accountID, address)
		if err != nil {
			return routeErrMsg{err: err}
		}
		return routeShowToastMsg{address: store.NormalizeEmail(address), dest: dest}
	}
}

func routeListSummaryCmd(st store.Store, accountID int64) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		rows, err := st.ListSenderRoutings(ctx, accountID, "")
		if err != nil {
			return routeErrMsg{err: err}
		}
		counts := map[string]int{}
		for _, r := range rows {
			counts[r.Destination]++
		}
		return routeListSummaryMsg{counts: counts}
	}
}
