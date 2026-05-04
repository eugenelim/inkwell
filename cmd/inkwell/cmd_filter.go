package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/eugenelim/inkwell/internal/action"
	"github.com/eugenelim/inkwell/internal/pattern"
	"github.com/eugenelim/inkwell/internal/store"
)

func newFilterCmd(rc *rootContext) *cobra.Command {
	var (
		actionType string
		apply      bool
		yes        bool
		output     string
		limit      int
		allFolders bool
	)
	cmd := &cobra.Command{
		Use:   "filter <pattern>",
		Short: "Match messages by pattern; dry-run by default",
		Long: `Compile a spec 08 pattern, run it against the local cache, and print
the matched message envelopes.

By default the command is dry-run: it prints matches and exits.
Pass --action <verb> --apply to dispatch the bulk operation through
Microsoft Graph $batch (chunked at 20 sub-requests per call).

Verbs: delete | archive | mark-read

Examples:
  inkwell filter '~f newsletter@*'
  inkwell filter '~f newsletter@* & ~d <30d' --action delete --apply
  inkwell filter '~G Newsletters' --action archive --apply
  inkwell filter '~U & ~A' --output json | jq '.messages[].id'`,
		Args: cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			ctx := c.Context()
			app, err := buildHeadlessApp(ctx, rc)
			if err != nil {
				return err
			}
			defer app.Close()

			src := args[0]
			msgs, err := runFilterListing(ctx, app, src, "", limit)
			if err != nil {
				return err
			}

			if output == "json" {
				_ = json.NewEncoder(os.Stdout).Encode(struct {
					Pattern  string          `json:"pattern"`
					Matched  int             `json:"matched"`
					Messages []store.Message `json:"messages"`
				}{src, len(msgs), msgs})
			} else {
				fmt.Fprintf(os.Stderr, "matched %d message(s)\n", len(msgs))
				printMessageList(msgs)
			}

			if !apply {
				return nil
			}
			if actionType == "" {
				return fmt.Errorf("--apply requires --action <delete|archive|mark-read>")
			}
			if !yes && actionType == "delete" {
				if !confirm(c, fmt.Sprintf("Delete %d messages?", len(msgs))) {
					return nil
				}
			}
			ids := make([]string, 0, len(msgs))
			for _, m := range msgs {
				ids = append(ids, m.ID)
			}
			exec := action.New(app.store, app.graph, app.logger)
			var results []action.BatchResult
			switch actionType {
			case "delete":
				results, err = exec.BulkSoftDelete(ctx, app.account.ID, ids)
			case "archive":
				results, err = exec.BulkArchive(ctx, app.account.ID, ids)
			case "mark-read":
				results, err = exec.BulkMarkRead(ctx, app.account.ID, ids)
			default:
				return fmt.Errorf("unknown action %q (use delete | archive | mark-read)", actionType)
			}
			if err != nil {
				return fmt.Errorf("bulk: %w", err)
			}
			ok, fail := 0, 0
			for _, r := range results {
				if r.Err != nil {
					fail++
				} else {
					ok++
				}
			}
			fmt.Fprintf(os.Stderr, "✓ %s: %d succeeded, %d failed\n", actionType, ok, fail)
			if output == "json" {
				return json.NewEncoder(os.Stdout).Encode(struct {
					Action    string `json:"action"`
					Succeeded int    `json:"succeeded"`
					Failed    int    `json:"failed"`
				}{actionType, ok, fail})
			}
			if fail > 0 {
				return fmt.Errorf("%d action(s) failed", fail)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&actionType, "action", "", "delete | archive | mark-read (with --apply)")
	cmd.Flags().BoolVar(&apply, "apply", false, "dispatch the action (without it the command is dry-run)")
	cmd.Flags().BoolVar(&yes, "yes", false, "skip the destructive-action confirmation prompt")
	cmd.Flags().IntVar(&limit, "limit", 1000, "max matches to consider")
	cmd.Flags().StringVar(&output, "output", "text", "output format: text|json")
	cmd.Flags().BoolVar(&allFolders, "all", false, "search across all subscribed folders (default behaviour; flag is explicit)")
	return cmd
}

// runFilterListing parses a pattern, compiles it locally, and queries
// the store. Used by both `inkwell messages --filter` and
// `inkwell filter`. folderID may be empty (no folder scope).
func runFilterListing(ctx context.Context, app *headlessApp, src, folderID string, limit int) ([]store.Message, error) {
	src = strings.TrimSpace(src)
	if !strings.Contains(src, "~") {
		src = "~B " + src
	}
	root, err := pattern.Parse(src)
	if err != nil {
		return nil, fmt.Errorf("filter: %w", err)
	}
	clause, err := pattern.CompileLocal(root)
	if err != nil {
		return nil, fmt.Errorf("filter: %w", err)
	}
	where := clause.Where
	args := clause.Args
	if folderID != "" {
		where = "(" + where + ") AND folder_id = ?"
		args = append(args, folderID)
	}
	timed, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return app.store.SearchByPredicate(timed, app.account.ID, where, args, limit)
}
