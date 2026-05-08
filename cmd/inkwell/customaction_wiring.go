package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/eugenelim/inkwell/internal/action"
	"github.com/eugenelim/inkwell/internal/config"
	"github.com/eugenelim/inkwell/internal/customaction"
	"github.com/eugenelim/inkwell/internal/pattern"
	"github.com/eugenelim/inkwell/internal/store"
	"github.com/eugenelim/inkwell/internal/ui"
)

// resolveActionsPath returns the actions.toml path to load. Order:
// explicit [custom_actions].file (expanding ~), else
// ~/.config/inkwell/actions.toml. Empty when neither exists.
func resolveActionsPath(cfg *config.Config) string {
	if cfg.CustomActions.File != "" {
		return expandHome(cfg.CustomActions.File)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "inkwell", "actions.toml")
}

// loadCustomActions reads actions.toml + builds the executor's
// dispatch surface. Returns (catalogue, deps, nil) on success;
// (nil, deps, err) on validation failure (caller decides whether to
// startup-abort).
func loadCustomActions(ctx context.Context, cfg *config.Config, app *headlessApp, logger *slog.Logger) (*customaction.Catalogue, customaction.ExecDeps, error) {
	deps := buildCustomActionDeps(app, logger, cfg)
	path := resolveActionsPath(cfg)
	cat, err := customaction.LoadCatalogue(ctx, path, customaction.Deps{
		PatternCompile: pattern.Compile,
		Now:            time.Now,
		Logger:         logger,
	})
	return cat, deps, err
}

// buildCustomActionDeps wraps the existing executor + service stack
// in the customaction.ExecDeps interface set. Reuses the same
// adapters the UI Deps use; the customaction package's interfaces
// are deliberately the same shape as ui.TriageExecutor / etc.
func buildCustomActionDeps(app *headlessApp, logger *slog.Logger, cfg *config.Config) customaction.ExecDeps {
	exec := action.New(app.store, app.graph, logger)
	folders := &cliFolderResolver{app: app}
	muteWriter := storeMuter{store: app.store}
	routingWriter := storeRoutingWriter{store: app.store}
	deps := customaction.ExecDeps{
		Triage:           caTriage{exec: exec},
		Bulk:             caBulk{exec: exec},
		Thread:           caThread{exec: exec},
		Mute:             muteWriter,
		Routing:          routingWriter,
		Unsubscribe:      nil, // CLI doesn't ship unsubscribe; UI overrides this in ui-side wiring.
		Folders:          folders,
		PatternCompile:   pattern.Compile,
		OpenURL:          openURLLocal,
		NowFn:            time.Now,
		Logger:           logger,
		ConfirmThreshold: cfg.Triage.ConfirmThreshold,
		AccountID:        app.account.ID,
	}
	return deps
}

// cliFolderResolver wraps resolveFolderByNameCtx for the customaction
// FolderResolver interface.
type cliFolderResolver struct {
	app *headlessApp
}

func (r *cliFolderResolver) Resolve(ctx context.Context, _ int64, pathOrName string) (string, string, error) {
	id, _, _, err := resolveFolderByNameCtx(ctx, r.app, pathOrName)
	if err != nil {
		return "", "", err
	}
	return id, "", nil
}

// caTriage adapts *action.Executor to customaction.Triage.
type caTriage struct{ exec *action.Executor }

func (a caTriage) MarkRead(ctx context.Context, accID int64, msgID string) error {
	return a.exec.MarkRead(ctx, accID, msgID)
}
func (a caTriage) MarkUnread(ctx context.Context, accID int64, msgID string) error {
	return a.exec.MarkUnread(ctx, accID, msgID)
}
func (a caTriage) ToggleFlag(ctx context.Context, accID int64, msgID string, currentlyFlagged bool) error {
	return a.exec.ToggleFlag(ctx, accID, msgID, currentlyFlagged)
}
func (a caTriage) SoftDelete(ctx context.Context, accID int64, msgID string) error {
	return a.exec.SoftDelete(ctx, accID, msgID)
}
func (a caTriage) Archive(ctx context.Context, accID int64, msgID string) error {
	return a.exec.Archive(ctx, accID, msgID)
}
func (a caTriage) Move(ctx context.Context, accID int64, msgID, destFolderID, destAlias string) error {
	return a.exec.Move(ctx, accID, msgID, destFolderID, destAlias)
}
func (a caTriage) PermanentDelete(ctx context.Context, accID int64, msgID string) error {
	return a.exec.PermanentDelete(ctx, accID, msgID)
}
func (a caTriage) AddCategory(ctx context.Context, accID int64, msgID, cat string) error {
	return a.exec.AddCategory(ctx, accID, msgID, cat)
}
func (a caTriage) RemoveCategory(ctx context.Context, accID int64, msgID, cat string) error {
	return a.exec.RemoveCategory(ctx, accID, msgID, cat)
}

// caBulk drops the per-row BulkResult slice; custom actions only
// care about whether the batch as a whole succeeded.
type caBulk struct{ exec *action.Executor }

func (b caBulk) BulkPermanentDelete(ctx context.Context, accID int64, ids []string) error {
	_, err := b.exec.BulkPermanentDelete(ctx, accID, ids)
	return err
}
func (b caBulk) BulkMove(ctx context.Context, accID int64, ids []string, destFolderID, destAlias string) error {
	_, err := b.exec.BulkMove(ctx, accID, ids, destFolderID, destAlias)
	return err
}
func (b caBulk) BulkAddCategory(ctx context.Context, accID int64, ids []string, cat string) error {
	_, err := b.exec.BulkAddCategory(ctx, accID, ids, cat)
	return err
}

// caThread adapts the existing thread executor to the simpler
// customaction.Thread surface. ThreadAddCategory and
// ThreadRemoveCategory route through ThreadExecute with the right
// store.ActionType. ThreadArchive routes through ThreadMove.
type caThread struct{ exec *action.Executor }

func (t caThread) ThreadAddCategory(ctx context.Context, accID int64, msgID, cat string) error {
	_, _, err := t.exec.ThreadExecute(ctx, accID, store.ActionAddCategory, msgID, map[string]any{"category": cat})
	return err
}
func (t caThread) ThreadRemoveCategory(ctx context.Context, accID int64, msgID, cat string) error {
	_, _, err := t.exec.ThreadExecute(ctx, accID, store.ActionRemoveCategory, msgID, map[string]any{"category": cat})
	return err
}
func (t caThread) ThreadArchive(ctx context.Context, accID int64, msgID string) error {
	_, _, err := t.exec.ThreadMove(ctx, accID, msgID, "", "archive")
	return err
}

// caUnsubAdapter wraps the existing UnsubscribeService into the
// customaction.Unsubscriber surface. The Method field translation
// is direct (UnsubscribeOneClickPOST → "POST", BrowserGET → "URL",
// Mailto → "MAILTO").
type caUnsubAdapter struct{ svc *unsubAdapter }

func (a caUnsubAdapter) Resolve(ctx context.Context, msgID string) (customaction.UnsubAction, error) {
	if a.svc == nil {
		return customaction.UnsubAction{}, errors.New("unsubscribe service not wired")
	}
	act, err := a.svc.Resolve(ctx, msgID)
	if err != nil {
		return customaction.UnsubAction{}, err
	}
	out := customaction.UnsubAction{URL: act.URL, Mailto: act.Mailto}
	switch act.Kind {
	case ui.UnsubscribeOneClickPOST:
		out.Method = "POST"
	case ui.UnsubscribeBrowserGET:
		out.Method = "URL"
	case ui.UnsubscribeMailto:
		out.Method = "MAILTO"
	default:
		out.Method = ""
	}
	return out, nil
}

func (a caUnsubAdapter) OneClickPOST(ctx context.Context, postURL string) error {
	if a.svc == nil {
		return errors.New("unsubscribe service not wired")
	}
	return a.svc.OneClickPOST(ctx, postURL)
}

// storeMuter / storeRoutingWriter wrap the store for the synchronous
// non-undoable ops (set_thread_muted, set_sender_routing).
type storeMuter struct{ store store.Store }

func (m storeMuter) MuteConversation(ctx context.Context, accID int64, convID string) error {
	return m.store.MuteConversation(ctx, accID, convID)
}
func (m storeMuter) UnmuteConversation(ctx context.Context, accID int64, convID string) error {
	return m.store.UnmuteConversation(ctx, accID, convID)
}

type storeRoutingWriter struct{ store store.Store }

func (r storeRoutingWriter) SetSenderRouting(ctx context.Context, accID int64, addr, dest string) (string, error) {
	return r.store.SetSenderRouting(ctx, accID, addr, dest)
}

// openURLLocal is the fallback OpenURL helper when the UI doesn't
// override (CLI path). Cross-platform `open` / `xdg-open`.
func openURLLocal(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "mailto" {
		return errors.New("URL must use http, https, or mailto")
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		// #nosec G204 — argument vector built from a validated URL.
		cmd = exec.Command("open", rawURL)
	case "linux":
		// #nosec G204 — same.
		cmd = exec.Command("xdg-open", rawURL)
	default:
		return fmt.Errorf("open URL: unsupported platform %q", runtime.GOOS)
	}
	return cmd.Start()
}

// expandHomeStr expands a leading ~/ to the user's home directory.
// Mirrors expandHome but exposed for tests.
func expandHomeStr(s string) string { return expandHome(s) }

// keyMapBindingsForLoader returns the post-override KeyMap binding
// strings (one entry per single-key field) so the loader can detect
// custom-action keys colliding with built-in actions. Empty when
// the KeyMap is nil — used by the CLI `validate` subcommand.
func keyMapBindingsForLoader(reserved map[string]string) map[string]string {
	if reserved == nil {
		return map[string]string{}
	}
	out := make(map[string]string, len(reserved))
	for k, v := range reserved {
		out[k] = v
	}
	return out
}

// rawKeyMapBindings returns the (key → KeyMap field name) reverse
// index for every BindingsConfig entry. Used by cmd_run to seed
// the loader's ReservedKeys table.
func rawKeyMapBindings(b config.BindingsConfig) map[string]string {
	pairs := []struct {
		field string
		keys  string
	}{
		{"Quit", b.Quit}, {"Help", b.Help}, {"Cmd", b.Cmd},
		{"Search", b.Search}, {"Refresh", b.Refresh},
		{"FocusFolders", b.FocusFolders}, {"FocusList", b.FocusList}, {"FocusViewer", b.FocusViewer},
		{"NextPane", b.NextPane}, {"PrevPane", b.PrevPane},
		{"PageUp", b.PageUp}, {"PageDown", b.PageDown},
		{"Home", b.Home}, {"End", b.End}, {"Open", b.Open},
		{"MarkRead", b.MarkRead}, {"MarkUnread", b.MarkUnread},
		{"ToggleFlag", b.ToggleFlag},
		{"Delete", b.Delete}, {"PermanentDelete", b.PermanentDelete},
		{"Archive", b.Archive}, {"Move", b.Move},
		{"AddCategory", b.AddCategory}, {"RemoveCategory", b.RemoveCategory},
		{"Undo", b.Undo},
		{"Filter", b.Filter}, {"ApplyToFiltered", b.ApplyToFiltered},
		{"Unsubscribe", b.Unsubscribe},
		{"MuteThread", b.MuteThread},
		{"ThreadChord", b.ThreadChord},
		{"Palette", b.Palette},
		{"StreamChord", b.StreamChord},
		{"NextTab", b.NextTab}, {"PrevTab", b.PrevTab},
		{"ReplyLaterToggle", b.ReplyLaterToggle}, {"SetAsideToggle", b.SetAsideToggle},
		{"BundleToggle", b.BundleToggle},
	}
	out := map[string]string{}
	for _, p := range pairs {
		if p.keys == "" {
			continue
		}
		for _, k := range strings.Split(p.keys, ",") {
			k = strings.TrimSpace(k)
			if k != "" {
				out[k] = p.field
			}
		}
	}
	return out
}
