package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/eugenelim/inkwell/internal/search"
)

func newSearchCmd(rc *rootContext) *cobra.Command {
	var (
		folder    string
		all       bool
		localOnly bool
		sortRelev bool
		limit     int
		output    string
	)
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Hybrid search (local FTS5 + Graph $search)",
		Long: `Run a hybrid search: local FTS5 index and Microsoft Graph $search
execute in parallel; results are merged and deduplicated by message ID.

By default the command searches across all folders. Use --folder to
scope to a single folder. Field-prefix syntax works as in the TUI:

  inkwell search "q4 budget"
  inkwell search --folder Inbox "from:alice proposal"
  inkwell search --sort=relevance "annual review"
  inkwell search --local-only "draft notes"
  inkwell search --output json "team standup" | jq '.[].subject'`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			query := strings.Join(args, " ")
			ctx := c.Context()

			cfg, err := rc.loadConfig()
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			app, err := buildHeadlessApp(ctx, rc)
			if err != nil {
				return err
			}
			defer app.Close()

			// Resolve folder scope. --all is the default when no
			// --folder is given (CLI has no "current folder" concept).
			folderID := ""
			if !all && folder != "" {
				folderID, err = resolveFolder(ctx, app, folder)
				if err != nil {
					return err
				}
			}

			if limit <= 0 {
				limit = cfg.Search.DefaultResultLimit
			}

			srv := search.ServerSearcher(graphServerSearcher{gc: app.graph})
			if localOnly {
				srv = nil
			}
			s := search.New(app.store, srv, search.Options{
				EmitThrottle:  cfg.Search.MergeEmitThrottle,
				ServerTimeout: cfg.Search.ServerSearchTimeout,
				DefaultLimit:  cfg.Search.DefaultResultLimit,
				AccountID:     app.account.ID,
			})

			stream := s.Search(ctx, search.Query{
				Text:            query,
				FolderID:        folderID,
				Limit:           limit,
				LocalOnly:       localOnly,
				SortByRelevance: sortRelev,
			})
			defer stream.Cancel()

			// Drain all updates; take the last (most complete) snapshot.
			var last []search.Result
			for snap := range stream.Updates() {
				last = snap
			}

			if effectiveOutput(rc, cfg) == "json" || output == "json" {
				type jsonMsg struct {
					ID       string `json:"id"`
					Subject  string `json:"subject"`
					From     string `json:"from"`
					Received string `json:"received"`
					Folder   string `json:"folder_id"`
					Source   string `json:"source"`
					Snippet  string `json:"snippet,omitempty"`
				}
				out := make([]jsonMsg, 0, len(last))
				for _, r := range last {
					from := r.Message.FromName
					if from == "" {
						from = r.Message.FromAddress
					}
					out = append(out, jsonMsg{
						ID:       r.Message.ID,
						Subject:  r.Message.Subject,
						From:     from,
						Received: r.Message.ReceivedAt.Format("2006-01-02T15:04:05Z"),
						Folder:   r.Message.FolderID,
						Source:   sourceName(r.Source),
						Snippet:  r.Snippet,
					})
				}
				return json.NewEncoder(os.Stdout).Encode(out)
			}

			if len(last) == 0 {
				fmt.Fprintln(os.Stdout, "no matches.")
				return nil
			}
			fmt.Fprintf(os.Stdout, "%-19s %-26s %s\n", "RECEIVED", "FROM", "SUBJECT")
			for _, r := range last {
				from := r.Message.FromName
				if from == "" {
					from = r.Message.FromAddress
				}
				fmt.Fprintf(os.Stdout, "%-19s %-26s %s\n",
					r.Message.ReceivedAt.Format("2006-01-02 15:04"),
					truncCLI(from, 26), r.Message.Subject)
			}
			fmt.Fprintf(os.Stderr, "%d result(s) [%s]\n", len(last), searchSourceSummary(last))
			return nil
		},
	}
	cmd.Flags().StringVar(&folder, "folder", "", "scope to a specific folder (display-name or well-known)")
	cmd.Flags().BoolVar(&all, "all", false, "search all folders (default; overrides --folder)")
	cmd.Flags().BoolVar(&localOnly, "local-only", false, "skip Graph $search; use FTS5 cache only")
	cmd.Flags().BoolVar(&sortRelev, "sort-relevance", false, "sort by BM25 relevance instead of received-date DESC")
	cmd.Flags().IntVar(&limit, "limit", 0, "max results (0 = config default)")
	cmd.Flags().StringVar(&output, "output", "", "output format: text|json")
	return cmd
}

func sourceName(s search.ResultSource) string {
	switch s {
	case search.SourceLocal:
		return "local"
	case search.SourceServer:
		return "server"
	default:
		return "both"
	}
}

func searchSourceSummary(rs []search.Result) string {
	var local, server, both int
	for _, r := range rs {
		switch r.Source {
		case search.SourceLocal:
			local++
		case search.SourceServer:
			server++
		default:
			both++
		}
	}
	return fmt.Sprintf("local %d  server %d  both %d", local, server, both)
}
