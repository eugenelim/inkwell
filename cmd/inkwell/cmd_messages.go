package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/eugenelim/inkwell/internal/action"
	"github.com/eugenelim/inkwell/internal/compose"
	"github.com/eugenelim/inkwell/internal/render"
	"github.com/eugenelim/inkwell/internal/store"
)

func newMessagesCmd(rc *rootContext) *cobra.Command {
	var (
		folder         string
		limit          int
		unread         bool
		output         string
		filter         string
		rule           string
		view           string
		allFolders     bool
		watch          bool
		watchInterval  time.Duration
		watchInitial   int
		includeUpdated bool
		watchCount     int
		watchFor       time.Duration
	)
	cmd := &cobra.Command{
		Use:   "messages",
		Short: "List messages from the local cache",
		Long: `Print message envelopes (from / received / subject) for one folder
or a pattern. Pulls from the local SQLite cache; for a fresh server
state, run ` + "`inkwell sync`" + ` first.

Use --watch to continuously stream new matches as they arrive
(spec 29 §1). Watch mode requires --filter or --rule.

Examples:
  inkwell messages --folder Inbox --limit 50
  inkwell messages --folder Inbox --unread
  inkwell messages --filter '~f bob' --limit 20
  inkwell messages --folder Inbox --output json | jq '.[].subject'

  # Watch mode (spec 29):
  inkwell messages --filter '~U & ~f vip@*' --watch
  inkwell messages --rule VIPs --watch --output json | jq '.Subject'
  inkwell messages --filter '~U' --watch --initial=10 --count 5`,
		RunE: func(c *cobra.Command, _ []string) error {
			ctx := c.Context()
			// Spec 31 §8.1 — --view is sugar for `--filter '~y <view>'
			// --folder Inbox`. Validate via a pure helper so tests can
			// exercise the error paths without os.Exit.
			if view != "" {
				newFolder, newFilter, vErr := applyMessagesViewFlag(view, folder, filter)
				if vErr != nil {
					fmt.Fprintln(c.ErrOrStderr(), vErr.Error())
					os.Exit(2)
				}
				folder = newFolder
				filter = newFilter
			}
			if watch {
				return runWatchFromFlags(ctx, rc, watchOpts{
					folder:         folder,
					filter:         filter,
					rule:           rule,
					all:            allFolders,
					output:         output,
					interval:       watchInterval,
					initial:        watchInitial,
					includeUpdated: includeUpdated,
					count:          watchCount,
					forDuration:    watchFor,
				})
			}
			app, err := buildHeadlessApp(ctx, rc)
			if err != nil {
				return err
			}
			defer app.Close()
			if limit <= 0 {
				limit = 50
			}
			folderID, err := resolveFolder(ctx, app, folder)
			if err != nil {
				return err
			}
			var msgs []store.Message
			if allFolders {
				folderID = "" // ignore --folder when --all is set
			}
			if filter != "" {
				msgs, err = runFilterListing(ctx, app, filter, folderID, limit)
			} else {
				q := store.MessageQuery{
					AccountID: app.account.ID,
					FolderID:  folderID,
					Limit:     limit,
				}
				if unread {
					q.UnreadOnly = true
				}
				msgs, err = app.store.ListMessages(ctx, q)
			}
			if err != nil {
				return err
			}
			cfg, _ := rc.loadConfig()
			if effectiveOutput(rc, cfg) == "json" || output == "json" {
				return json.NewEncoder(os.Stdout).Encode(msgs)
			}
			printMessageList(msgs)
			return nil
		},
	}
	cmd.Flags().StringVar(&folder, "folder", "", "folder display-name (e.g. Inbox), well-known name, or empty for all")
	cmd.Flags().IntVar(&limit, "limit", 50, "max rows to return")
	cmd.Flags().BoolVar(&unread, "unread", false, "only unread messages")
	cmd.Flags().StringVar(&filter, "filter", "", "spec 08 pattern; overrides --folder/--unread when set")
	cmd.Flags().StringVar(&rule, "rule", "", "saved search name (spec 11); mutually exclusive with --filter")
	cmd.Flags().StringVar(&output, "output", "", "output format: text|json (overrides [cli].default_output)")
	cmd.Flags().StringVar(&view, "view", "", "spec 31: \"focused\" or \"other\" — Inbox sub-strip view (sugar for --filter '~y <view>' --folder Inbox)")
	cmd.Flags().BoolVar(&allFolders, "all", false, "ignore --folder and search all folders (requires --filter)")
	cmd.Flags().BoolVar(&watch, "watch", false, "stream new matches like tail -f (spec 29; requires --filter or --rule)")
	cmd.Flags().DurationVar(&watchInterval, "interval", 0, "watch: re-eval cadence (default = engine foreground interval; min 5s)")
	cmd.Flags().IntVar(&watchInitial, "initial", 0, "watch: print N most-recent matches at startup (default 0 = silent)")
	cmd.Flags().BoolVar(&includeUpdated, "include-updated", false, "watch: re-emit on last_modified_at advance")
	cmd.Flags().IntVar(&watchCount, "count", 0, "watch: exit 0 after N new matches (default unbounded)")
	cmd.Flags().DurationVar(&watchFor, "for", 0, "watch: exit 0 after this wall-clock duration (default unbounded)")
	cmd.MarkFlagsMutuallyExclusive("folder", "all")
	cmd.MarkFlagsMutuallyExclusive("filter", "rule")
	cmd.MarkFlagsMutuallyExclusive("watch", "limit")
	cmd.MarkFlagsMutuallyExclusive("watch", "unread")

	cmd.AddCommand(newMessageShowCmd(rc))
	cmd.AddCommand(newMessageReadCmd(rc))
	cmd.AddCommand(newMessageUnreadCmd(rc))
	cmd.AddCommand(newMessageFlagCmd(rc))
	cmd.AddCommand(newMessageUnflagCmd(rc))
	cmd.AddCommand(newMessageMoveCmd(rc))
	cmd.AddCommand(newMessageDeleteCmd(rc))
	cmd.AddCommand(newMessagePermanentDeleteCmd(rc))
	cmd.AddCommand(newMessageAttachmentsCmd(rc))
	cmd.AddCommand(newMessageSaveAttachmentCmd(rc))
	cmd.AddCommand(newMessageReplyCmd(rc))
	cmd.AddCommand(newMessageReplyAllCmd(rc))
	cmd.AddCommand(newMessageForwardCmd(rc))
	return cmd
}

// resolveFolder maps a user-supplied folder name to a real folder ID.
// Empty string means "all folders" (returns ""). Matches against
// well-known names first, then case-insensitive display names.
func resolveFolder(ctx context.Context, app *headlessApp, name string) (string, error) {
	if name == "" {
		return "", nil
	}
	if f, err := app.store.GetFolderByWellKnown(ctx, app.account.ID, strings.ToLower(name)); err == nil && f != nil {
		return f.ID, nil
	}
	all, err := app.store.ListFolders(ctx, app.account.ID)
	if err != nil {
		return "", fmt.Errorf("resolve folder: %w", err)
	}
	for _, f := range all {
		if strings.EqualFold(f.DisplayName, name) {
			return f.ID, nil
		}
	}
	return "", fmt.Errorf("folder %q not found locally — try `inkwell folders` to list, or `inkwell sync` first", name)
}

func printMessageList(msgs []store.Message) {
	fmt.Fprintf(os.Stdout, "%-19s %-26s %s\n", "RECEIVED", "FROM", "SUBJECT")
	for _, m := range msgs {
		from := m.FromName
		if from == "" {
			from = m.FromAddress
		}
		fmt.Fprintf(os.Stdout, "%-19s %-26s %s\n",
			m.ReceivedAt.Format("2006-01-02 15:04"),
			truncCLI(from, 26), m.Subject)
	}
}

// printMessageListWithFolder prints messages with an additional FOLDER column.
func printMessageListWithFolder(msgs []store.Message, nameByID map[string]string) {
	fmt.Fprintf(os.Stdout, "%-19s %-20s %-16s %s\n", "RECEIVED", "FROM", "FOLDER", "SUBJECT")
	for _, m := range msgs {
		from := m.FromName
		if from == "" {
			from = m.FromAddress
		}
		folder := nameByID[m.FolderID]
		if folder == "" {
			folder = "???"
		}
		fmt.Fprintf(os.Stdout, "%-19s %-20s %-16s %s\n",
			m.ReceivedAt.Format("2006-01-02 15:04"),
			truncCLI(from, 20), truncCLI(folder, 16), m.Subject)
	}
}

func truncCLI(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func newMessageShowCmd(rc *rootContext) *cobra.Command {
	var (
		output  string
		headers bool
	)
	cmd := &cobra.Command{
		Use:   "show <id>",
		Short: "Show a message in full (headers + body)",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			ctx := c.Context()
			app, err := buildHeadlessApp(ctx, rc)
			if err != nil {
				return err
			}
			defer app.Close()
			id := args[0]
			m, err := app.store.GetMessage(ctx, id)
			if err != nil || m == nil {
				return fmt.Errorf("get message %q: %w", id, err)
			}
			r := render.New(app.store, render.NewGraphBodyFetcher(app.graph))
			view, err := r.Body(ctx, m, render.BodyOpts{Width: 100, Theme: render.DefaultTheme()})
			if err != nil {
				return fmt.Errorf("render body: %w", err)
			}
			if view.State == render.BodyFetching {
				if f, ok := r.(interface {
					FetchBodyAsync(context.Context, *store.Message, render.BodyOpts) (render.BodyView, error)
				}); ok {
					view, err = f.FetchBodyAsync(ctx, m, render.BodyOpts{Width: 100, Theme: render.DefaultTheme()})
					if err != nil {
						return err
					}
				}
			}
			if output == "json" {
				return json.NewEncoder(os.Stdout).Encode(struct {
					Message *store.Message `json:"message"`
					Body    string         `json:"body"`
				}{m, view.Text})
			}
			from := m.FromName
			if from == "" {
				from = m.FromAddress
			}
			fmt.Printf("From:    %s <%s>\n", from, m.FromAddress)
			fmt.Printf("Date:    %s\n", m.ReceivedAt.Format("Mon 2006-01-02 15:04 -0700"))
			fmt.Printf("Subject: %s\n", m.Subject)
			if headers {
				fmt.Printf("To:      %s\n", joinCLIAddrs(m.ToAddresses))
				if len(m.CcAddresses) > 0 {
					fmt.Printf("Cc:      %s\n", joinCLIAddrs(m.CcAddresses))
				}
				if len(m.BccAddresses) > 0 {
					fmt.Printf("Bcc:     %s\n", joinCLIAddrs(m.BccAddresses))
				}
			} else {
				fmt.Printf("To:      %s%s\n", joinCLIAddrsSummary(m.ToAddresses, 3),
					moreNote(len(m.ToAddresses), len(m.CcAddresses), len(m.BccAddresses), 3))
			}
			fmt.Println()
			fmt.Println(view.Text)
			return nil
		},
	}
	cmd.Flags().StringVar(&output, "output", "text", "output format: text|json")
	cmd.Flags().BoolVar(&headers, "headers", false, "include all headers (To/Cc/Bcc lists)")
	return cmd
}

func joinCLIAddrs(rs []store.EmailAddress) string {
	parts := make([]string, 0, len(rs))
	for _, a := range rs {
		if a.Name != "" {
			parts = append(parts, a.Name+" <"+a.Address+">")
		} else {
			parts = append(parts, a.Address)
		}
	}
	if len(parts) == 0 {
		return "—"
	}
	return strings.Join(parts, ", ")
}

func joinCLIAddrsSummary(rs []store.EmailAddress, max int) string {
	if len(rs) == 0 {
		return "—"
	}
	parts := make([]string, 0, max)
	for i, a := range rs {
		if i >= max {
			break
		}
		if a.Name != "" {
			parts = append(parts, a.Name)
		} else {
			parts = append(parts, a.Address)
		}
	}
	return strings.Join(parts, ", ")
}

func moreNote(toN, ccN, bccN, shownTo int) string {
	more := toN - shownTo
	if more < 0 {
		more = 0
	}
	more += ccN + bccN
	if more <= 0 {
		return ""
	}
	return fmt.Sprintf("  + %d more (--headers to expand)", more)
}

func newMessageReadCmd(rc *rootContext) *cobra.Command {
	return &cobra.Command{
		Use:   "read <id>",
		Short: "Mark a message as read",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			ctx := c.Context()
			app, err := buildHeadlessApp(ctx, rc)
			if err != nil {
				return err
			}
			defer app.Close()
			exec := action.New(app.store, app.graph, app.logger)
			if err := exec.MarkRead(ctx, app.account.ID, args[0]); err != nil {
				return err
			}
			fmt.Fprintf(os.Stdout, "marked read: %s\n", args[0])
			return nil
		},
	}
}

func newMessageUnreadCmd(rc *rootContext) *cobra.Command {
	return &cobra.Command{
		Use:   "unread <id>",
		Short: "Mark a message as unread",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			ctx := c.Context()
			app, err := buildHeadlessApp(ctx, rc)
			if err != nil {
				return err
			}
			defer app.Close()
			exec := action.New(app.store, app.graph, app.logger)
			if err := exec.MarkUnread(ctx, app.account.ID, args[0]); err != nil {
				return err
			}
			fmt.Fprintf(os.Stdout, "marked unread: %s\n", args[0])
			return nil
		},
	}
}

func newMessageFlagCmd(rc *rootContext) *cobra.Command {
	return &cobra.Command{
		Use:   "flag <id>",
		Short: "Flag a message",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			ctx := c.Context()
			app, err := buildHeadlessApp(ctx, rc)
			if err != nil {
				return err
			}
			defer app.Close()
			m, err := app.store.GetMessage(ctx, args[0])
			if err != nil || m == nil {
				return fmt.Errorf("get message %q: %w", args[0], err)
			}
			exec := action.New(app.store, app.graph, app.logger)
			if err := exec.ToggleFlag(ctx, app.account.ID, args[0], m.FlagStatus == "flagged"); err != nil {
				return err
			}
			fmt.Fprintf(os.Stdout, "flagged: %s\n", args[0])
			return nil
		},
	}
}

func newMessageUnflagCmd(rc *rootContext) *cobra.Command {
	return &cobra.Command{
		Use:   "unflag <id>",
		Short: "Remove flag from a message",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			ctx := c.Context()
			app, err := buildHeadlessApp(ctx, rc)
			if err != nil {
				return err
			}
			defer app.Close()
			m, err := app.store.GetMessage(ctx, args[0])
			if err != nil || m == nil {
				return fmt.Errorf("get message %q: %w", args[0], err)
			}
			exec := action.New(app.store, app.graph, app.logger)
			// Pass currentlyFlagged=true so ToggleFlag applies an unflag action.
			if err := exec.ToggleFlag(ctx, app.account.ID, args[0], true); err != nil {
				return err
			}
			fmt.Fprintf(os.Stdout, "unflagged: %s\n", args[0])
			return nil
		},
	}
}

func newMessageMoveCmd(rc *rootContext) *cobra.Command {
	var to string
	cmd := &cobra.Command{
		Use:   "move <id>",
		Short: "Move a message to a folder",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			if to == "" {
				return fmt.Errorf("--to <folder> is required")
			}
			ctx := c.Context()
			app, err := buildHeadlessApp(ctx, rc)
			if err != nil {
				return err
			}
			defer app.Close()
			destID, err := resolveFolder(ctx, app, to)
			if err != nil {
				return err
			}
			exec := action.New(app.store, app.graph, app.logger)
			if err := exec.Move(ctx, app.account.ID, args[0], destID, ""); err != nil {
				return err
			}
			fmt.Fprintf(os.Stdout, "moved %s to %q\n", args[0], to)
			return nil
		},
	}
	cmd.Flags().StringVar(&to, "to", "", "destination folder name (required)")
	return cmd
}

func newMessageDeleteCmd(rc *rootContext) *cobra.Command {
	return &cobra.Command{
		Use:   "delete <id>",
		Short: "Move a message to Deleted Items",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			ctx := c.Context()
			app, err := buildHeadlessApp(ctx, rc)
			if err != nil {
				return err
			}
			defer app.Close()
			exec := action.New(app.store, app.graph, app.logger)
			if err := exec.SoftDelete(ctx, app.account.ID, args[0]); err != nil {
				return err
			}
			fmt.Fprintf(os.Stdout, "deleted: %s\n", args[0])
			return nil
		},
	}
}

func newMessagePermanentDeleteCmd(rc *rootContext) *cobra.Command {
	return &cobra.Command{
		Use:   "permanent-delete <id>",
		Short: "Permanently delete a message (irreversible)",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			ctx := c.Context()
			app, err := buildHeadlessApp(ctx, rc)
			if err != nil {
				return err
			}
			defer app.Close()
			if !rc.yes {
				if !confirm(c, fmt.Sprintf("Permanently delete message %s? This cannot be undone.", args[0])) {
					return nil
				}
			}
			exec := action.New(app.store, app.graph, app.logger)
			if err := exec.PermanentDelete(ctx, app.account.ID, args[0]); err != nil {
				return err
			}
			fmt.Fprintf(os.Stdout, "permanently deleted: %s\n", args[0])
			return nil
		},
	}
}

func newMessageAttachmentsCmd(rc *rootContext) *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:   "attachments <id>",
		Short: "List attachments on a message",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			ctx := c.Context()
			app, err := buildHeadlessApp(ctx, rc)
			if err != nil {
				return err
			}
			defer app.Close()
			atts, err := app.store.ListAttachments(ctx, args[0])
			if err != nil {
				return fmt.Errorf("list attachments: %w", err)
			}
			if output == "json" {
				return json.NewEncoder(os.Stdout).Encode(atts)
			}
			if len(atts) == 0 {
				fmt.Fprintln(os.Stdout, "(no attachments)")
				return nil
			}
			fmt.Fprintf(os.Stdout, "%-36s  %-40s  %s\n", "ID", "NAME", "SIZE")
			for _, a := range atts {
				fmt.Fprintf(os.Stdout, "%-36s  %-40s  %d\n", a.ID, truncCLI(a.Name, 40), a.Size)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&output, "output", "text", "output format: text|json")
	return cmd
}

func newMessageSaveAttachmentCmd(rc *rootContext) *cobra.Command {
	var toDir string
	cmd := &cobra.Command{
		Use:   "save-attachment <message-id> <attachment-id>",
		Short: "Download and save an attachment to disk",
		Args:  cobra.ExactArgs(2),
		RunE: func(c *cobra.Command, args []string) error {
			ctx := c.Context()
			app, err := buildHeadlessApp(ctx, rc)
			if err != nil {
				return err
			}
			defer app.Close()
			msgID := args[0]
			attID := args[1]

			// Look up the attachment name from the local cache.
			atts, err := app.store.ListAttachments(ctx, msgID)
			if err != nil {
				return fmt.Errorf("list attachments: %w", err)
			}
			name := attID
			for _, a := range atts {
				if a.ID == attID {
					name = a.Name
					break
				}
			}

			data, err := app.graph.GetAttachment(ctx, msgID, attID)
			if err != nil {
				return fmt.Errorf("download attachment: %w", err)
			}

			if toDir == "" {
				toDir = "."
			}
			dest, err := safeAttachmentDest(toDir, name)
			if err != nil {
				return fmt.Errorf("attachment path: %w", err)
			}
			// #nosec G306 — attachment saved to user-controlled directory with standard permissions.
			if err := os.WriteFile(dest, data, 0o644); err != nil {
				return fmt.Errorf("write %s: %w", dest, err)
			}
			fmt.Fprintf(os.Stdout, "saved to %s\n", dest)
			return nil
		},
	}
	cmd.Flags().StringVar(&toDir, "to", "", "destination directory (default: current directory)")
	return cmd
}

// safeAttachmentDest returns the absolute path where rawName should be saved
// inside toDir. It strips directory components and rejects names that resolve
// to "." or ".." (path-traversal guard, spec 17 §4.4 / ASVS V12.1.1).
func safeAttachmentDest(toDir, rawName string) (string, error) {
	base := filepath.Base(rawName)
	if base == "." || base == ".." {
		return "", fmt.Errorf("attachment name %q is not a valid filename", rawName)
	}
	dest := filepath.Join(toDir, base)
	absDir, err := filepath.Abs(toDir)
	if err != nil {
		return "", fmt.Errorf("resolve target dir: %w", err)
	}
	absDest, err := filepath.Abs(dest)
	if err != nil {
		return "", fmt.Errorf("resolve attachment dest: %w", err)
	}
	rel, err := filepath.Rel(absDir, absDest)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("attachment %q would escape target directory", rawName)
	}
	return dest, nil
}

func newMessageReplyCmd(rc *rootContext) *cobra.Command {
	var bodyText, bodyFile, subject string
	var useEditor bool
	cmd := &cobra.Command{
		Use:   "reply <id>",
		Short: "Create a draft reply to a message",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			ctx := c.Context()
			app, err := buildHeadlessApp(ctx, rc)
			if err != nil {
				return err
			}
			defer app.Close()
			body, err := resolveBody(bodyText, bodyFile, useEditor)
			if err != nil {
				return err
			}
			exec := action.New(app.store, app.graph, app.logger)
			res, err := exec.CreateDraftReply(ctx, app.account.ID, args[0], compose.DraftBody{Content: body, ContentType: "text"}, nil, nil, nil, subject, nil)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stdout, "draft created: id=%s\nwebLink: %s\n", res.ID, res.WebLink)
			return nil
		},
	}
	cmd.Flags().StringVar(&bodyText, "body", "", "reply body text")
	cmd.Flags().StringVar(&bodyFile, "body-from-file", "", "read body from file path")
	cmd.Flags().StringVar(&subject, "subject", "", "override subject line")
	cmd.Flags().BoolVar(&useEditor, "editor", false, "open $EDITOR to compose body")
	return cmd
}

func newMessageReplyAllCmd(rc *rootContext) *cobra.Command {
	var bodyText, bodyFile, subject string
	var useEditor bool
	cmd := &cobra.Command{
		Use:   "reply-all <id>",
		Short: "Create a draft reply-all to a message",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			ctx := c.Context()
			app, err := buildHeadlessApp(ctx, rc)
			if err != nil {
				return err
			}
			defer app.Close()
			body, err := resolveBody(bodyText, bodyFile, useEditor)
			if err != nil {
				return err
			}
			exec := action.New(app.store, app.graph, app.logger)
			res, err := exec.CreateDraftReplyAll(ctx, app.account.ID, args[0], compose.DraftBody{Content: body, ContentType: "text"}, nil, nil, nil, subject, nil)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stdout, "draft created: id=%s\nwebLink: %s\n", res.ID, res.WebLink)
			return nil
		},
	}
	cmd.Flags().StringVar(&bodyText, "body", "", "reply body text")
	cmd.Flags().StringVar(&bodyFile, "body-from-file", "", "read body from file path")
	cmd.Flags().StringVar(&subject, "subject", "", "override subject line")
	cmd.Flags().BoolVar(&useEditor, "editor", false, "open $EDITOR to compose body")
	return cmd
}

func newMessageForwardCmd(rc *rootContext) *cobra.Command {
	var bodyText, bodyFile, subject string
	var toAddrs []string
	var useEditor bool
	cmd := &cobra.Command{
		Use:   "forward <id>",
		Short: "Create a draft forward of a message",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			ctx := c.Context()
			app, err := buildHeadlessApp(ctx, rc)
			if err != nil {
				return err
			}
			defer app.Close()
			body, err := resolveBody(bodyText, bodyFile, useEditor)
			if err != nil {
				return err
			}
			exec := action.New(app.store, app.graph, app.logger)
			res, err := exec.CreateDraftForward(ctx, app.account.ID, args[0], compose.DraftBody{Content: body, ContentType: "text"}, toAddrs, nil, nil, subject, nil)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stdout, "draft created: id=%s\nwebLink: %s\n", res.ID, res.WebLink)
			return nil
		},
	}
	cmd.Flags().StringVar(&bodyText, "body", "", "body text")
	cmd.Flags().StringVar(&bodyFile, "body-from-file", "", "read body from file path")
	cmd.Flags().StringVar(&subject, "subject", "", "override subject line")
	cmd.Flags().StringArrayVar(&toAddrs, "to", nil, "recipient addresses (may repeat)")
	cmd.Flags().BoolVar(&useEditor, "editor", false, "open $EDITOR to compose body")
	return cmd
}

// resolveBody returns the body string from the first non-empty source:
// --body, --body-from-file, or $EDITOR (temp file).
func resolveBody(bodyText, bodyFile string, useEditor bool) (string, error) {
	if bodyText != "" {
		return bodyText, nil
	}
	if bodyFile != "" {
		data, err := os.ReadFile(bodyFile) // #nosec G304 — user-supplied path for message body
		if err != nil {
			return "", fmt.Errorf("read body file: %w", err)
		}
		return string(data), nil
	}
	if useEditor {
		return openEditor()
	}
	return "", nil
}

// openEditor creates a temp file, opens $EDITOR on it, and returns the
// contents the user saved. Falls back to vi if $EDITOR is unset.
func openEditor() (string, error) {
	f, err := os.CreateTemp("", "inkwell-compose-*.txt")
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	name := f.Name()
	_ = f.Close()

	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}
	cmd := exec.Command(editor, name) // #nosec G204 G702 — editor is $EDITOR (user-controlled by the operator) or "vi"; this is intentional shell-out to the user's chosen editor, identical to how git/mutt/etc invoke $EDITOR
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("editor: %w", err)
	}
	data, err := os.ReadFile(name) // #nosec G304 — path created by os.CreateTemp above
	if err != nil {
		return "", fmt.Errorf("read temp file: %w", err)
	}
	_ = os.Remove(name)
	return string(data), nil
}

// applyMessagesViewFlag implements the spec 31 §8.1 `--view` flag
// translation. Returns the resolved (folder, filter) pair, or an error
// describing exactly the user-visible message to print on stderr
// before exiting with code 2. Helper is pure so unit tests can exercise
// both error paths.
func applyMessagesViewFlag(view, folder, filter string) (string, string, error) {
	switch view {
	case "focused", "other":
	default:
		return "", "", fmt.Errorf("messages: --view must be one of \"focused\", \"other\"")
	}
	if folder != "" && strings.ToLower(folder) != "inbox" {
		return "", "", fmt.Errorf("messages: --view requires --folder Inbox (or no --folder); got %q", folder)
	}
	if filter == "" {
		filter = "~y " + view
	} else {
		filter = "(~y " + view + ") & (" + filter + ")"
	}
	return "Inbox", filter, nil
}
