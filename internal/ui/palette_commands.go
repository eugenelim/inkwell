package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/eugenelim/inkwell/internal/store"
)

// collectPaletteRows returns the static + dynamic palette row index
// resolved against the live Model snapshot. Called once per palette
// Open so live state (focused message, focused folder, deps) is
// captured for the open session — re-open to refresh.
//
// The order is: static commands, then folders, then saved searches.
// Empty-buffer ordering is handled by matchEmpty in palette_match.go.
func collectPaletteRows(m *Model) []PaletteRow {
	rows := make([]PaletteRow, 0, 80)
	rows = append(rows, buildStaticPaletteRows(m)...)
	rows = append(rows, buildFolderPaletteRows(m)...)
	rows = append(rows, buildSavedSearchPaletteRows(m)...)
	return rows
}

// availTrue is the always-available Availability literal. Many rows
// share it; defining it here keeps the row table compact.
var availTrue = Availability{OK: true}

// notWired returns an unavailable Availability with the supplied why
// string, mirroring the dispatcher's "X: not wired" toasts.
func notWired(why string) Availability { return Availability{OK: false, Why: why} }

// focusedMessage returns the message the palette should act on:
// viewer.current when the viewer is focused, otherwise the list
// selection. Returns nil when no message is in scope.
func focusedMessage(m *Model) *store.Message {
	if cur := m.viewer.current; cur != nil && m.focused == ViewerPane {
		return cur
	}
	if sel, ok := m.list.SelectedMessage(); ok {
		s := sel
		return &s
	}
	return nil
}

// buildStaticPaletteRows returns the canonical command rows. Each
// row's Available is resolved against the live Model; RunFn / ArgFn
// close over identifiers (folder ID, etc.) — never over Model.
func buildStaticPaletteRows(m *Model) []PaletteRow {
	km := m.keymap
	msg := focusedMessage(m)

	hasMsg := availTrue
	if msg == nil {
		hasMsg = notWired("no message focused")
	}
	hasMsgWithThread := availTrue
	if msg == nil {
		hasMsgWithThread = notWired("no message focused")
	} else if msg.ConversationID == "" {
		hasMsgWithThread = notWired("focused message has no conversation id")
	}
	threadAvail := availTrue
	if m.deps.Thread == nil {
		threadAvail = notWired("thread: not wired")
	} else if msg == nil {
		threadAvail = notWired("no message focused")
	}
	draftsAvail := availTrue
	if msg == nil {
		draftsAvail = notWired("no message focused")
	} else if m.deps.Drafts == nil {
		draftsAvail = notWired("drafts: not wired (CLI mode or unsigned)")
	}
	composeAvail := availTrue
	if m.deps.Drafts == nil {
		composeAvail = notWired("compose: not wired (CLI mode or unsigned)")
	}
	calendarAvail := availTrue
	if m.deps.Calendar == nil {
		calendarAvail = notWired("calendar: not wired (CLI mode or unsigned)")
	}
	mailboxAvail := availTrue
	if m.deps.Mailbox == nil {
		mailboxAvail = notWired("mailbox: not wired (CLI mode or unsigned)")
	}
	backfillAvail := availTrue
	if m.deps.Engine == nil {
		backfillAvail = notWired("backfill: not wired")
	} else if strings.HasPrefix(m.list.FolderID, "filter:") || m.list.FolderID == "" {
		backfillAvail = notWired("backfill: focus a folder (not a filter view) first")
	}
	filteredApplyAvail := availTrue
	if !m.filterActive || len(m.filterIDs) == 0 {
		filteredApplyAvail = notWired(";: requires an active filter (run :filter <pattern>)")
	}
	urlAvail := availTrue
	if len(m.viewer.Links()) == 0 {
		urlAvail = notWired("no links in current message")
	}

	rows := []PaletteRow{
		{
			ID: "archive", Title: "Archive message",
			Binding: keysOf(km.Archive), Section: sectionCommands,
			Synonyms:  []string{"done", "file"},
			Available: hasMsg,
			RunFn: func(mm Model) (tea.Model, tea.Cmd) {
				if msg == nil {
					return mm, nil
				}
				return mm.runTriage("archive", *msg, ListPane, func(ctx context.Context, accID int64, src store.Message) error {
					return mm.deps.Triage.Archive(ctx, accID, src.ID)
				})
			},
		},
		{
			ID: "delete", Title: "Delete (move to Deleted Items)",
			Binding: keysOf(km.Delete), Section: sectionCommands,
			Synonyms:  []string{"trash", "remove"},
			Available: hasMsg,
			RunFn: func(mm Model) (tea.Model, tea.Cmd) {
				if msg == nil {
					return mm, nil
				}
				return mm.runTriage("soft_delete", *msg, ListPane, func(ctx context.Context, accID int64, src store.Message) error {
					return mm.deps.Triage.SoftDelete(ctx, accID, src.ID)
				})
			},
		},
		{
			ID: "permanent_delete", Title: "Permanent delete (skip Deleted Items)",
			Binding: keysOf(km.PermanentDelete), Section: sectionCommands,
			Synonyms:  []string{"purge", "rm", "destroy"},
			Available: hasMsg,
			RunFn: func(mm Model) (tea.Model, tea.Cmd) {
				if msg == nil {
					return mm, nil
				}
				return mm.startPermanentDelete(*msg)
			},
		},
		{
			ID: "mark_read", Title: "Mark read",
			Binding: keysOf(km.MarkRead), Section: sectionCommands,
			Synonyms:  []string{"read"},
			Available: hasMsg,
			RunFn: func(mm Model) (tea.Model, tea.Cmd) {
				if msg == nil {
					return mm, nil
				}
				return mm.runTriage("mark_read", *msg, ListPane, func(ctx context.Context, accID int64, src store.Message) error {
					return mm.deps.Triage.MarkRead(ctx, accID, src.ID)
				})
			},
		},
		{
			ID: "mark_unread", Title: "Mark unread",
			Binding: keysOf(km.MarkUnread), Section: sectionCommands,
			Available: hasMsg,
			RunFn: func(mm Model) (tea.Model, tea.Cmd) {
				if msg == nil {
					return mm, nil
				}
				return mm.runTriage("mark_unread", *msg, ListPane, func(ctx context.Context, accID int64, src store.Message) error {
					return mm.deps.Triage.MarkUnread(ctx, accID, src.ID)
				})
			},
		},
		{
			ID: "toggle_flag", Title: "Toggle flag",
			Binding: keysOf(km.ToggleFlag), Section: sectionCommands,
			Available: hasMsg,
			RunFn: func(mm Model) (tea.Model, tea.Cmd) {
				if msg == nil {
					return mm, nil
				}
				return mm.runTriage("toggle_flag", *msg, ListPane, func(ctx context.Context, accID int64, src store.Message) error {
					return mm.deps.Triage.ToggleFlag(ctx, accID, src.ID, src.FlagStatus == "flagged")
				})
			},
		},
		{
			ID: "move", Title: "Move to folder…", Subtitle: "opens folder picker",
			Binding: keysOf(km.Move), Section: sectionCommands,
			NeedsArg: true, Available: hasMsg,
			RunFn: func(mm Model) (tea.Model, tea.Cmd) {
				if msg == nil {
					return mm, nil
				}
				return mm.startMove(*msg)
			},
			ArgFn: func(mm Model) (tea.Model, tea.Cmd) {
				if msg == nil {
					return mm, nil
				}
				return mm.startMove(*msg)
			},
		},
		{
			ID: "add_category", Title: "Add category…",
			Binding: keysOf(km.AddCategory), Section: sectionCommands,
			NeedsArg: true, Available: hasMsg,
			RunFn: func(mm Model) (tea.Model, tea.Cmd) {
				if msg == nil {
					return mm, nil
				}
				return mm.startCategoryInput("add", *msg)
			},
			ArgFn: func(mm Model) (tea.Model, tea.Cmd) {
				if msg == nil {
					return mm, nil
				}
				return mm.startCategoryInput("add", *msg)
			},
		},
		{
			ID: "remove_category", Title: "Remove category…",
			Binding: keysOf(km.RemoveCategory), Section: sectionCommands,
			NeedsArg: true, Available: hasMsg,
			RunFn: func(mm Model) (tea.Model, tea.Cmd) {
				if msg == nil {
					return mm, nil
				}
				return mm.startCategoryInput("remove", *msg)
			},
			ArgFn: func(mm Model) (tea.Model, tea.Cmd) {
				if msg == nil {
					return mm, nil
				}
				return mm.startCategoryInput("remove", *msg)
			},
		},
		{
			ID: "undo", Title: "Undo last action",
			Binding: keysOf(km.Undo), Section: sectionCommands,
			Available: availTrue,
			RunFn: func(mm Model) (tea.Model, tea.Cmd) {
				return mm.runUndo()
			},
		},
		{
			ID: "unsubscribe", Title: "Unsubscribe (RFC 8058)",
			Binding: keysOf(km.Unsubscribe), Section: sectionCommands,
			Synonyms:  []string{"unsub", "list-unsub"},
			Available: hasMsg,
			RunFn: func(mm Model) (tea.Model, tea.Cmd) {
				if msg == nil {
					return mm, nil
				}
				return mm.startUnsubscribe(msg.ID)
			},
		},
		{
			ID: "mute_thread", Title: muteRowTitle(m, msg),
			Binding: keysOf(km.MuteThread), Section: sectionCommands,
			Synonyms:  []string{"silence"},
			Available: hasMsgWithThread,
			RunFn: func(mm Model) (tea.Model, tea.Cmd) {
				return mm.startMute()
			},
		},
		{
			ID: "thread_archive", Title: "Archive thread",
			Binding: "T " + keysOf(km.Archive), Section: sectionCommands,
			Available: threadAvail,
			RunFn: func(mm Model) (tea.Model, tea.Cmd) {
				if msg == nil {
					return mm, nil
				}
				return mm, mm.runThreadMoveCmd("archive", msg.ID, "", "archive")
			},
		},
		{
			ID: "thread_delete", Title: "Delete thread",
			Binding: "T " + keysOf(km.Delete), Section: sectionCommands,
			Available: threadAvail,
			RunFn: func(mm Model) (tea.Model, tea.Cmd) {
				if msg == nil {
					return mm, nil
				}
				return mm, mm.threadPreFetchCmd("soft_delete", msg.ID)
			},
		},
		{
			ID: "thread_permanent_delete", Title: "Permanent delete thread",
			Binding: "T " + keysOf(km.PermanentDelete), Section: sectionCommands,
			Available: threadAvail,
			RunFn: func(mm Model) (tea.Model, tea.Cmd) {
				if msg == nil {
					return mm, nil
				}
				return mm, mm.threadPreFetchCmd("permanent_delete", msg.ID)
			},
		},
		{
			ID: "thread_mark_read", Title: "Mark thread read",
			Binding: "T " + keysOf(km.MarkRead), Section: sectionCommands,
			Available: threadAvail,
			RunFn: func(mm Model) (tea.Model, tea.Cmd) {
				if msg == nil {
					return mm, nil
				}
				return mm, mm.runThreadExecuteCmd("mark read", store.ActionMarkRead, msg.ID, nil)
			},
		},
		{
			ID: "thread_mark_unread", Title: "Mark thread unread",
			Binding: "T " + keysOf(km.MarkUnread), Section: sectionCommands,
			Available: threadAvail,
			RunFn: func(mm Model) (tea.Model, tea.Cmd) {
				if msg == nil {
					return mm, nil
				}
				return mm, mm.runThreadExecuteCmd("mark unread", store.ActionMarkUnread, msg.ID, nil)
			},
		},
		{
			ID: "thread_flag", Title: "Flag thread",
			Binding: "T " + keysOf(km.ToggleFlag), Section: sectionCommands,
			Available: threadAvail,
			RunFn: func(mm Model) (tea.Model, tea.Cmd) {
				if msg == nil {
					return mm, nil
				}
				return mm, mm.runThreadExecuteCmd("flag", store.ActionFlag, msg.ID, nil)
			},
		},
		{
			ID: "thread_unflag", Title: "Unflag thread",
			Binding: "T F", Section: sectionCommands,
			Available: threadAvail,
			RunFn: func(mm Model) (tea.Model, tea.Cmd) {
				if msg == nil {
					return mm, nil
				}
				return mm, mm.runThreadExecuteCmd("unflag", store.ActionUnflag, msg.ID, nil)
			},
		},
		{
			ID: "thread_move", Title: "Move thread to folder…",
			Binding: "T " + keysOf(km.Move), Section: sectionCommands,
			NeedsArg: true, Available: threadAvail,
			RunFn: func(mm Model) (tea.Model, tea.Cmd) {
				return startThreadMove(mm, msg)
			},
			ArgFn: func(mm Model) (tea.Model, tea.Cmd) {
				return startThreadMove(mm, msg)
			},
		},
		{
			ID: "reply", Title: "Reply",
			Binding: "r (viewer)", Section: sectionCommands,
			Available: draftsAvail,
			RunFn: func(mm Model) (tea.Model, tea.Cmd) {
				if msg == nil {
					return mm, nil
				}
				return mm.startCompose(*msg)
			},
		},
		{
			ID: "reply_all", Title: "Reply-all",
			Binding: "R (viewer)", Section: sectionCommands,
			Available: draftsAvail,
			RunFn: func(mm Model) (tea.Model, tea.Cmd) {
				if msg == nil {
					return mm, nil
				}
				return mm.startComposeReplyAll(*msg)
			},
		},
		{
			ID: "forward", Title: "Forward",
			Binding: "f (viewer)", Section: sectionCommands,
			Available: draftsAvail,
			RunFn: func(mm Model) (tea.Model, tea.Cmd) {
				if msg == nil {
					return mm, nil
				}
				return mm.startComposeForward(*msg)
			},
		},
		{
			ID: "compose", Title: "New message",
			Binding: ":compose", Section: sectionCommands,
			Synonyms:  []string{"new", "write"},
			Available: composeAvail,
			RunFn: func(mm Model) (tea.Model, tea.Cmd) {
				return mm.startComposeNew()
			},
		},
		{
			ID: "filter", Title: "Filter…",
			Binding: keysOf(km.Filter) + " / :filter", Section: sectionCommands,
			Synonyms: []string{"narrow"},
			NeedsArg: true, Available: availTrue,
			RunFn: func(mm Model) (tea.Model, tea.Cmd) {
				return prefillCmdBar(mm, "filter ")
			},
			ArgFn: func(mm Model) (tea.Model, tea.Cmd) {
				return prefillCmdBar(mm, "filter ")
			},
		},
		{
			ID: "filter_all", Title: "Filter (all folders)…",
			Binding: ":filter --all", Section: sectionCommands,
			NeedsArg: true, Available: availTrue,
			RunFn: func(mm Model) (tea.Model, tea.Cmd) {
				return prefillCmdBar(mm, "filter --all ")
			},
			ArgFn: func(mm Model) (tea.Model, tea.Cmd) {
				return prefillCmdBar(mm, "filter --all ")
			},
		},
		{
			ID: "unfilter", Title: "Clear filter",
			Binding: keysOf(km.ClearFilter) + " / :unfilter", Section: sectionCommands,
			Available: availTrue,
			RunFn: func(mm Model) (tea.Model, tea.Cmd) {
				return mm.dispatchCommand("unfilter")
			},
		},
		{
			ID: "apply_to_filtered", Title: "Apply action to filtered…",
			Binding: keysOf(km.ApplyToFiltered), Section: sectionCommands,
			Synonyms: []string{"bulk"},
			NeedsArg: true, Available: filteredApplyAvail,
			RunFn: func(mm Model) (tea.Model, tea.Cmd) {
				mm.bulkPending = true
				mm.engineActivity = "bulk: press d (delete) or a (archive) — esc to cancel"
				return mm, nil
			},
			ArgFn: func(mm Model) (tea.Model, tea.Cmd) {
				mm.bulkPending = true
				mm.engineActivity = "bulk: press d (delete) or a (archive) — esc to cancel"
				return mm, nil
			},
		},
		{
			ID: "open_url", Title: "Open URL…",
			Binding: keysOf(km.OpenURL), Section: sectionCommands,
			Available: urlAvail,
			RunFn: func(mm Model) (tea.Model, tea.Cmd) {
				mm.mode = URLPickerMode
				return mm, nil
			},
		},
		{
			ID: "yank_url", Title: "Yank URL",
			Binding: keysOf(km.Yank), Section: sectionCommands,
			Available: urlAvail,
			RunFn: func(mm Model) (tea.Model, tea.Cmd) {
				links := mm.viewer.Links()
				if len(links) == 1 {
					return mm.yankURL(links[0].URL)
				}
				mm.mode = URLPickerMode
				return mm, nil
			},
		},
		{
			ID: "fullscreen_body", Title: "Fullscreen body",
			Binding: keysOf(km.FullscreenBody), Section: sectionCommands,
			Synonyms:  []string{"zoom"},
			Available: availTrue,
			RunFn: func(mm Model) (tea.Model, tea.Cmd) {
				mm.mode = FullscreenBodyMode
				return mm, nil
			},
		},
		{
			ID: "folder_jump", Title: "Jump to folder…",
			Binding: ":folder", Section: sectionCommands,
			NeedsArg: true, Available: availTrue,
			RunFn: func(mm Model) (tea.Model, tea.Cmd) {
				return prefillCmdBar(mm, "folder ")
			},
			ArgFn: func(mm Model) (tea.Model, tea.Cmd) {
				return prefillCmdBar(mm, "folder ")
			},
		},
		{
			ID: "folder_new", Title: "New folder…",
			Binding: keysOf(km.NewFolder), Section: sectionCommands,
			NeedsArg: true, Available: availTrue,
			RunFn: func(mm Model) (tea.Model, tea.Cmd) {
				parent := ""
				if f, ok := mm.folders.Selected(); ok {
					parent = f.ID
				}
				return mm.startFolderNameInput("new", "", parent)
			},
			ArgFn: func(mm Model) (tea.Model, tea.Cmd) {
				parent := ""
				if f, ok := mm.folders.Selected(); ok {
					parent = f.ID
				}
				return mm.startFolderNameInput("new", "", parent)
			},
		},
		{
			ID: "search", Title: "Search messages…",
			Binding: keysOf(km.Search), Section: sectionCommands,
			NeedsArg: true, Available: availTrue,
			RunFn: func(mm Model) (tea.Model, tea.Cmd) {
				mm.mode = SearchMode
				mm.searchBuf = ""
				if !mm.searchActive {
					mm.priorFolderID = mm.list.FolderID
				}
				return mm, nil
			},
			ArgFn: func(mm Model) (tea.Model, tea.Cmd) {
				mm.mode = SearchMode
				mm.searchBuf = ""
				if !mm.searchActive {
					mm.priorFolderID = mm.list.FolderID
				}
				return mm, nil
			},
		},
		{
			ID: "backfill", Title: "Backfill older messages",
			Binding: ":backfill", Section: sectionCommands,
			Available: backfillAvail,
			RunFn: func(mm Model) (tea.Model, tea.Cmd) {
				return mm.dispatchCommand("backfill")
			},
		},
		{
			ID: "sync", Title: "Sync now",
			Binding: keysOf(km.Refresh), Section: sectionCommands,
			Synonyms:  []string{"refresh", "fetch"},
			Available: availTrue,
			RunFn: func(mm Model) (tea.Model, tea.Cmd) {
				if mm.deps.Engine != nil {
					mm.deps.Engine.Wake()
					mm.engineActivity = "syncing…"
				}
				return mm, nil
			},
		},
		{
			ID: "calendar", Title: "Open calendar",
			Binding: ":cal", Section: sectionCommands,
			Available: calendarAvail,
			RunFn: func(mm Model) (tea.Model, tea.Cmd) {
				return mm.dispatchCommand("calendar")
			},
		},
		{
			ID: "ooo_on", Title: "Out-of-office: turn on",
			Binding: ":ooo on", Section: sectionCommands,
			Available: mailboxAvail,
			RunFn: func(mm Model) (tea.Model, tea.Cmd) {
				return mm.dispatchCommand("ooo on")
			},
		},
		{
			ID: "ooo_off", Title: "Out-of-office: turn off",
			Binding: ":ooo off", Section: sectionCommands,
			Available: mailboxAvail,
			RunFn: func(mm Model) (tea.Model, tea.Cmd) {
				return mm.dispatchCommand("ooo off")
			},
		},
		{
			ID: "ooo_schedule", Title: "Out-of-office: schedule…",
			Binding: ":ooo schedule", Section: sectionCommands,
			Available: mailboxAvail,
			RunFn: func(mm Model) (tea.Model, tea.Cmd) {
				return mm.dispatchCommand("ooo schedule")
			},
		},
		{
			ID: "settings", Title: "Mailbox settings",
			Binding: ":settings", Section: sectionCommands,
			Available: mailboxAvail,
			RunFn: func(mm Model) (tea.Model, tea.Cmd) {
				return mm.dispatchCommand("settings")
			},
		},
	}
	rows = append(rows, buildRoutingPaletteRows(m, msg)...)
	rows = append(rows, buildScreenerPaletteRows(m, msg)...)
	rows = append(rows, []PaletteRow{
		{
			ID: "rule_save", Title: "Saved search: save current filter…",
			Binding: ":save", Section: sectionCommands,
			NeedsArg: true, Available: availTrue,
			RunFn: func(mm Model) (tea.Model, tea.Cmd) {
				return prefillCmdBar(mm, "save ")
			},
			ArgFn: func(mm Model) (tea.Model, tea.Cmd) {
				return prefillCmdBar(mm, "save ")
			},
		},
		{
			ID: "rule_list", Title: "Saved search: list",
			Binding: ":rule list", Section: sectionCommands,
			Available: availTrue,
			RunFn: func(mm Model) (tea.Model, tea.Cmd) {
				return mm.dispatchCommand("rule list")
			},
		},
		{
			ID: "rule_edit", Title: "Saved search: edit…",
			Binding: ":rule edit", Section: sectionCommands,
			NeedsArg: true, Available: availTrue,
			RunFn: func(mm Model) (tea.Model, tea.Cmd) {
				return prefillCmdBar(mm, "rule edit ")
			},
			ArgFn: func(mm Model) (tea.Model, tea.Cmd) {
				return prefillCmdBar(mm, "rule edit ")
			},
		},
		{
			ID: "rule_delete", Title: "Saved search: delete…",
			Binding: ":rule delete", Section: sectionCommands,
			NeedsArg: true, Available: availTrue,
			RunFn: func(mm Model) (tea.Model, tea.Cmd) {
				return prefillCmdBar(mm, "rule delete ")
			},
			ArgFn: func(mm Model) (tea.Model, tea.Cmd) {
				return prefillCmdBar(mm, "rule delete ")
			},
		},
		{
			ID: "signin", Title: "Sign in",
			Binding: ":signin", Section: sectionCommands,
			Available: availTrue,
			RunFn: func(mm Model) (tea.Model, tea.Cmd) {
				return mm.dispatchCommand("signin")
			},
		},
		{
			ID: "signout", Title: "Sign out and clear credentials",
			Binding: ":signout", Section: sectionCommands,
			Available: availTrue,
			RunFn: func(mm Model) (tea.Model, tea.Cmd) {
				return mm.dispatchCommand("signout")
			},
		},
		{
			ID: "help", Title: "Help (every binding)",
			Binding: keysOf(km.Help), Section: sectionCommands,
			Available: availTrue,
			RunFn: func(mm Model) (tea.Model, tea.Cmd) {
				mm.mode = HelpMode
				return mm, nil
			},
		},
		{
			ID: "quit", Title: "Quit",
			Binding: keysOf(km.Quit), Section: sectionCommands,
			Available: availTrue,
			RunFn: func(mm Model) (tea.Model, tea.Cmd) {
				return mm, tea.Quit
			},
		},
	}...)
	return rows
}

// buildRoutingPaletteRows returns the spec 23 §13 routing rows
// (route_imbox / route_feed / route_paper_trail / route_screener /
// route_clear / route_show). Each routing row's Available is OK only
// when a message is focused with a non-empty from_address —
// mirroring the chord's `S` precondition. route_show is the only
// NeedsArg row; it defers to the cmd-bar.
func buildRoutingPaletteRows(m *Model, msg *store.Message) []PaletteRow {
	hasFrom := availTrue
	addr := ""
	if msg != nil {
		addr = strings.TrimSpace(msg.FromAddress)
	}
	if addr == "" {
		hasFrom = notWired("route: focused message has no from-address")
	}
	storeAvail := availTrue
	if m.deps.Store == nil {
		storeAvail = notWired("route: not wired (CLI mode or unsigned)")
	}
	combined := hasFrom
	if storeAvail.OK == false {
		combined = storeAvail
	}
	var accountID int64
	if m.deps.Account != nil {
		accountID = m.deps.Account.ID
	}
	st := m.deps.Store
	mkRow := func(id, title, dest string) PaletteRow {
		return PaletteRow{
			ID: id, Title: title,
			Binding:   "S " + chordKeyForDestination(dest),
			Section:   sectionCommands,
			Available: combined,
			RunFn: func(mm Model) (tea.Model, tea.Cmd) {
				if addr == "" || st == nil {
					return mm, nil
				}
				return mm, routeCmd(st, accountID, addr, dest)
			},
		}
	}
	return []PaletteRow{
		mkRow("route_imbox", "Route sender → Imbox", "imbox"),
		mkRow("route_feed", "Route sender → Feed", "feed"),
		mkRow("route_paper_trail", "Route sender → Paper Trail", "paper_trail"),
		mkRow("route_screener", "Route sender → Screener", "screener"),
		{
			ID: "route_clear", Title: "Clear sender routing",
			Binding:   "S c",
			Section:   sectionCommands,
			Available: combined,
			RunFn: func(mm Model) (tea.Model, tea.Cmd) {
				if addr == "" || st == nil {
					return mm, nil
				}
				return mm, routeCmd(st, accountID, addr, "")
			},
		},
		{
			ID: "route_show", Title: "Show routing for sender…",
			Binding:   ":route show",
			Section:   sectionCommands,
			NeedsArg:  true,
			Available: storeAvail,
			RunFn: func(mm Model) (tea.Model, tea.Cmd) {
				return prefillCmdBar(mm, "route show ")
			},
			ArgFn: func(mm Model) (tea.Model, tea.Cmd) {
				return prefillCmdBar(mm, "route show ")
			},
		},
	}
}

// chordKeyForDestination maps a destination to its `S <letter>`
// chord second key. Mnemonic: i = imbox, f = feed, p = paper trail,
// k = screener (spec 23 §5.1).
func chordKeyForDestination(dest string) string {
	switch dest {
	case "imbox":
		return "i"
	case "feed":
		return "f"
	case "paper_trail":
		return "p"
	case "screener":
		return "k"
	}
	return ""
}

// muteRowTitle resolves the mute_thread row title based on the
// focused message's current mute state. Spec 19 §6: the M key flips
// behaviour, so the palette label flips too.
func muteRowTitle(m *Model, msg *store.Message) string {
	if msg == nil || msg.ConversationID == "" || m.deps.Store == nil {
		return "Mute / unmute thread"
	}
	var accountID int64
	if m.deps.Account != nil {
		accountID = m.deps.Account.ID
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	muted, err := m.deps.Store.IsConversationMuted(ctx, accountID, msg.ConversationID)
	if err != nil {
		return "Mute / unmute thread"
	}
	if muted {
		return "Unmute thread"
	}
	return "Mute thread"
}

// startThreadMove is the spec 22 §3.5 / spec 20 thread-move opener
// shared by Tab + Enter on the `thread_move` row.
func startThreadMove(m Model, msg *store.Message) (tea.Model, tea.Cmd) {
	if msg == nil {
		return m, nil
	}
	if m.deps.Thread == nil {
		m.lastError = fmt.Errorf("thread move: not wired")
		return m, nil
	}
	folders := m.folders.raw
	if len(folders) == 0 {
		m.lastError = fmt.Errorf("thread move: no folders synced yet")
		return m, nil
	}
	m.pendingThreadMove = true
	src := *msg
	m.pendingMoveMsg = &src
	m.folderPicker.Reset(folders, m.recentFolderIDs)
	m.mode = FolderPickerMode
	return m, nil
}

// prefillCmdBar transitions to CommandMode with the supplied prefix
// already in the cmd buffer. Used by Tab on argument-needing rows
// (Filter, :folder, :save, :rule edit/delete) so the existing
// cmd-bar dispatcher stays authoritative for argument parsing.
func prefillCmdBar(m Model, prefix string) (tea.Model, tea.Cmd) {
	m.mode = CommandMode
	m.cmd.Activate()
	m.cmd.buf = prefix
	return m, nil
}

// buildFolderPaletteRows returns one row per folder in the live model
// snapshot. Selecting a row jumps the list pane to that folder.
func buildFolderPaletteRows(m *Model) []PaletteRow {
	folders := m.folders.raw
	if len(folders) == 0 {
		return nil
	}
	pathByID := buildFolderPaths(folders)
	rows := make([]PaletteRow, 0, len(folders))
	for _, f := range folders {
		fid := f.ID
		title := pathByID[fid]
		if title == "" {
			title = f.DisplayName
		}
		rows = append(rows, PaletteRow{
			ID:        "folder:" + fid,
			Title:     title,
			Section:   sectionFolders,
			Available: availTrue,
			RunFn: func(mm Model) (tea.Model, tea.Cmd) {
				mm.list.FolderID = fid
				mm.focused = ListPane
				return mm, mm.loadMessagesCmd(fid)
			},
		})
	}
	return rows
}

// buildSavedSearchPaletteRows returns one row per saved search.
// Selecting a row runs the saved search's pattern as a filter.
func buildSavedSearchPaletteRows(m *Model) []PaletteRow {
	rows := make([]PaletteRow, 0, len(m.savedSearches))
	for _, ss := range m.savedSearches {
		name := ss.Name
		pattern := ss.Pattern
		rows = append(rows, PaletteRow{
			ID:        "rule:" + name,
			Title:     name,
			Subtitle:  pattern,
			Section:   sectionSavedSearches,
			Available: availTrue,
			RunFn: func(mm Model) (tea.Model, tea.Cmd) {
				return mm, mm.runFilterCmd(pattern)
			},
		})
	}
	return rows
}
