package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/eugenelim/inkwell/internal/render"
	"github.com/eugenelim/inkwell/internal/store"
)

func newMessagesCmd(rc *rootContext) *cobra.Command {
	var (
		folder string
		limit  int
		unread bool
		output string
		filter string
	)
	cmd := &cobra.Command{
		Use:   "messages",
		Short: "List messages from the local cache",
		Long: `Print message envelopes (from / received / subject) for one folder
or a pattern. Pulls from the local SQLite cache; for a fresh server
state, run ` + "`inkwell sync`" + ` first.

Examples:
  inkwell messages --folder Inbox --limit 50
  inkwell messages --folder Inbox --unread
  inkwell messages --filter '~f bob' --limit 20
  inkwell messages --folder Inbox --output json | jq '.[].subject'`,
		RunE: func(c *cobra.Command, _ []string) error {
			ctx := c.Context()
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
			if output == "json" {
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
	cmd.Flags().StringVar(&output, "output", "text", "output format: text|json")

	cmd.AddCommand(newMessageShowCmd(rc))
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
