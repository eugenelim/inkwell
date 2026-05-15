package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/eugenelim/inkwell/internal/render"
	"github.com/eugenelim/inkwell/internal/store"
)

// newIndexCmd is the spec 35 §11 `inkwell index` parent. Four
// subcommands manage the opt-in body index:
//
//	inkwell index status
//	inkwell index rebuild [--folder NAME] [--limit N] [--force]
//	inkwell index evict   [--older-than DUR] [--folder NAME] [--message-id ID]
//	inkwell index disable [--yes]
//
// All four respect the global --json flag for scripting.
func newIndexCmd(rc *rootContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "index",
		Short: "Manage the local body index (spec 35)",
		Long: `Inspect and manage the opt-in local body index.

Examples:
  inkwell index status
  inkwell index rebuild --folder='Clients/TIAA' --limit=2000
  inkwell index evict --older-than=90d
  inkwell index disable

The body index is off by default. Enable it via
[body_index].enabled = true in your config.toml; then either reopen
inkwell (bodies are indexed as you view them) or run 'inkwell index
rebuild' to backfill from the locally-cached LRU.`,
	}
	cmd.AddCommand(newIndexStatusCmd(rc))
	cmd.AddCommand(newIndexRebuildCmd(rc))
	cmd.AddCommand(newIndexEvictCmd(rc))
	cmd.AddCommand(newIndexDisableCmd(rc))
	return cmd
}

// indexStatusReport is the JSON shape emitted by `inkwell index
// status --json` (or when [cli].default_output = "json"). Fields
// match the human-readable layout for consistency.
type indexStatusReport struct {
	Enabled         bool   `json:"enabled"`
	Rows            int64  `json:"rows"`
	Bytes           int64  `json:"bytes"`
	MaxCount        int    `json:"max_count"`
	MaxBytes        int64  `json:"max_bytes"`
	BodyLRURows     int64  `json:"body_lru_rows"`
	BodyLRUBytes    int64  `json:"body_lru_bytes"`
	TruncatedRows   int64  `json:"truncated_rows"`
	MaxBodyBytes    int64  `json:"max_body_bytes"`
	OldestIndexedAt string `json:"oldest_indexed_at,omitempty"`
	NewestIndexedAt string `json:"newest_indexed_at,omitempty"`
	FolderAllowlist []string `json:"folder_allowlist,omitempty"`
}

func newIndexStatusCmd(rc *rootContext) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show body-index size, caps, and last indexed times",
		RunE: func(c *cobra.Command, _ []string) error {
			ctx := c.Context()
			app, err := buildHeadlessApp(ctx, rc)
			if err != nil {
				return err
			}
			defer app.Close()

			stats, err := app.store.BodyIndexStats(ctx)
			if err != nil {
				return fmt.Errorf("body index stats: %w", err)
			}
			lru := bodyLRUSnapshot(ctx, app.store)

			report := indexStatusReport{
				Enabled:         app.cfg.BodyIndex.Enabled,
				Rows:            stats.Rows,
				Bytes:           stats.Bytes,
				MaxCount:        app.cfg.BodyIndex.MaxCount,
				MaxBytes:        app.cfg.BodyIndex.MaxBytes,
				BodyLRURows:     lru.rows,
				BodyLRUBytes:    lru.bytes,
				TruncatedRows:   stats.Truncated,
				MaxBodyBytes:    app.cfg.BodyIndex.MaxBodyBytes,
				FolderAllowlist: app.cfg.BodyIndex.FolderAllowlist,
			}
			if !stats.OldestIndexedAt.IsZero() {
				report.OldestIndexedAt = stats.OldestIndexedAt.Format(time.RFC3339)
			}
			if !stats.NewestIndexedAt.IsZero() {
				report.NewestIndexedAt = stats.NewestIndexedAt.Format(time.RFC3339)
			}

			if effectiveOutput(rc, app.cfg) == "json" {
				return json.NewEncoder(os.Stdout).Encode(report)
			}
			printIndexStatus(report)
			return nil
		},
	}
}

type bodyLRUStats struct {
	rows  int64
	bytes int64
}

func bodyLRUSnapshot(ctx context.Context, _ store.Store) bodyLRUStats {
	// No direct stat method on Store today; the maintenance loop is
	// the only consumer of EvictBodies. v1 keeps this best-effort
	// and returns zeroes when we don't have a tally helper. The
	// scaffolding stays in place so future iterations can light it
	// up without churning the report shape.
	_ = ctx
	return bodyLRUStats{}
}

func printIndexStatus(r indexStatusReport) {
	enabled := "disabled"
	if r.Enabled {
		enabled = "enabled"
	}
	fmt.Printf("Status:           %s\n", enabled)
	fmt.Printf("Indexed:          %s messages (cap %s), %s plaintext (cap %s)\n",
		formatInt(r.Rows), formatIntCap(r.MaxCount),
		formatBytes(r.Bytes), formatInt64Bytes(r.MaxBytes))
	if r.BodyLRURows > 0 || r.BodyLRUBytes > 0 {
		fmt.Printf("Body LRU:         %s messages, %s  (rebuild can only re-decode what the LRU currently caches)\n",
			formatInt(r.BodyLRURows), formatBytes(r.BodyLRUBytes))
	}
	if r.OldestIndexedAt != "" {
		fmt.Printf("Oldest:           %s\n", r.OldestIndexedAt)
	}
	if r.NewestIndexedAt != "" {
		fmt.Printf("Newest:           %s\n", r.NewestIndexedAt)
	}
	fmt.Printf("Truncated bodies: %s (cap: %s per message)\n",
		formatInt(r.TruncatedRows), formatInt64Bytes(r.MaxBodyBytes))
	if len(r.FolderAllowlist) == 0 {
		fmt.Println("Folder allow-list: (empty — all subscribed folders)")
	} else {
		fmt.Printf("Folder allow-list: %s\n", strings.Join(r.FolderAllowlist, ", "))
	}
}

func newIndexRebuildCmd(rc *rootContext) *cobra.Command {
	var folder string
	var limit int
	var force bool
	cmd := &cobra.Command{
		Use:   "rebuild",
		Short: "Backfill the body index from cached bodies (no Graph round-trips)",
		Long: `Walk the local bodies table and re-decode each through render.DecodeForIndex.

Bounded by the configured body LRU — bodies that have already been
evicted from the cache require the user to re-open the message in the
viewer before they're available to rebuild.`,
		RunE: func(c *cobra.Command, _ []string) error {
			ctx := c.Context()
			app, err := buildHeadlessApp(ctx, rc)
			if err != nil {
				return err
			}
			defer app.Close()

			if !app.cfg.BodyIndex.Enabled {
				return fmt.Errorf("body index is disabled — set [body_index].enabled = true in config.toml first")
			}

			n, totalBytes, err := rebuildFromBodyLRU(ctx, app.store, app.cfg.BodyIndex.MaxBodyBytes, app.account.ID, folder, limit, force)
			if err != nil {
				return err
			}
			if effectiveOutput(rc, app.cfg) == "json" {
				return json.NewEncoder(os.Stdout).Encode(map[string]any{
					"indexed_rows":  n,
					"indexed_bytes": totalBytes,
				})
			}
			fmt.Printf("Indexed %s messages (%s).\n", formatInt(int64(n)), formatBytes(totalBytes))
			return nil
		},
	}
	cmd.Flags().StringVar(&folder, "folder", "", "scope to a folder by display_name (matches `folders.display_name`)")
	cmd.Flags().IntVar(&limit, "limit", 0, "cap the rebuild at N messages")
	cmd.Flags().BoolVar(&force, "force", false, "re-decode messages already in the index")
	return cmd
}

// rebuildFromBodyLRU walks every cached body row, runs DecodeForIndex,
// and writes the result to body_text. Returns the number of indexed
// rows and the total bytes written. Folder + limit + force shape the
// walk.
func rebuildFromBodyLRU(
	ctx context.Context,
	st store.Store,
	maxBodyBytes int64,
	accountID int64,
	folder string,
	limit int,
	force bool,
) (int, int64, error) {
	// We don't have a Store.ListBodies API; reach into the DB
	// directly via the explicit query interface. The body LRU has
	// hundreds of rows on default config so the simple SELECT is
	// well under the perf budget.
	type bodyRow struct {
		messageID string
		content   string
		ctype     string
	}
	internal, ok := st.(interface {
		ListBodyMessageIDs(ctx context.Context) ([]string, error)
	})
	_ = internal
	_ = ok

	// Fall back to a Message-by-Message walk: ListMessages by folder
	// (or all folders), call GetBody, decode, IndexBody.
	q := store.MessageQuery{AccountID: accountID, Limit: 100000}
	if folder != "" {
		f, err := st.GetFolderByPath(ctx, accountID, folder)
		if err != nil {
			return 0, 0, fmt.Errorf("folder %q: %w", folder, err)
		}
		q.FolderID = f.ID
	}
	msgs, err := st.ListMessages(ctx, q)
	if err != nil {
		return 0, 0, err
	}

	indexed := 0
	var totalBytes int64
	for _, m := range msgs {
		if limit > 0 && indexed >= limit {
			break
		}
		body, err := st.GetBody(ctx, m.ID)
		if err != nil {
			// Not in the LRU — skip silently. The user can re-open
			// the message to warm it.
			continue
		}
		if !force {
			if _, err := st.GetBodyText(ctx, m.ID); err == nil {
				continue
			}
		}
		decoded, err := render.DecodeForIndex(body.Content)
		if err != nil {
			continue
		}
		if decoded == "" {
			continue
		}
		truncated := false
		if maxBodyBytes > 0 && int64(len(decoded)) > maxBodyBytes {
			decoded = decoded[:maxBodyBytes]
			truncated = true
		}
		if err := st.IndexBody(ctx, store.BodyIndexEntry{
			MessageID: m.ID,
			AccountID: m.AccountID,
			FolderID:  m.FolderID,
			Content:   decoded,
			Truncated: truncated,
		}); err != nil {
			continue
		}
		indexed++
		totalBytes += int64(len(decoded))
	}
	return indexed, totalBytes, nil
}

func newIndexEvictCmd(rc *rootContext) *cobra.Command {
	var olderThan time.Duration
	var folder string
	var messageID string
	cmd := &cobra.Command{
		Use:   "evict",
		Short: "Evict rows from the body index",
		Long: `Remove rows from the body index. Filters compose (all-of):

  --older-than DURATION    drop rows whose last_accessed_at is
                           older than the supplied duration (e.g. 90d, 24h).
  --folder NAME            drop rows in the named folder.
  --message-id ID          drop a single message's row.`,
		RunE: func(c *cobra.Command, _ []string) error {
			ctx := c.Context()
			app, err := buildHeadlessApp(ctx, rc)
			if err != nil {
				return err
			}
			defer app.Close()

			opts := store.EvictBodyIndexOpts{
				MessageID: messageID,
			}
			if olderThan > 0 {
				opts.OlderThan = time.Now().Add(-olderThan)
			}
			if folder != "" {
				f, err := app.store.GetFolderByPath(ctx, app.account.ID, folder)
				if err != nil {
					return fmt.Errorf("folder %q: %w", folder, err)
				}
				opts.FolderID = f.ID
			}
			evicted, err := app.store.EvictBodyIndex(ctx, opts)
			if err != nil {
				return err
			}
			if effectiveOutput(rc, app.cfg) == "json" {
				return json.NewEncoder(os.Stdout).Encode(map[string]any{
					"evicted_rows": evicted,
				})
			}
			fmt.Printf("Evicted %s rows.\n", formatInt(int64(evicted)))
			return nil
		},
	}
	cmd.Flags().DurationVar(&olderThan, "older-than", 0, "drop rows whose last_accessed_at is older than DURATION (e.g. 90h, 720h)")
	cmd.Flags().StringVar(&folder, "folder", "", "drop rows in the named folder")
	cmd.Flags().StringVar(&messageID, "message-id", "", "drop a single message's row")
	return cmd
}

func newIndexDisableCmd(rc *rootContext) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "disable",
		Short: "Purge the body index (destructive)",
		Long: `Drop every row in the body index. The config flag
[body_index].enabled remains where it was; flip it to false to stop
future indexing on body fetches.

The body index is not re-derivable without re-opening cached
messages. Disable when you no longer want plaintext bodies on disk.`,
		RunE: func(c *cobra.Command, _ []string) error {
			ctx := c.Context()
			app, err := buildHeadlessApp(ctx, rc)
			if err != nil {
				return err
			}
			defer app.Close()

			stats, err := app.store.BodyIndexStats(ctx)
			if err != nil {
				return err
			}
			if stats.Rows == 0 {
				fmt.Println("Body index is already empty.")
				return nil
			}
			if !yes {
				fmt.Printf("This will delete the local body index (%s across %s messages)\n",
					formatBytes(stats.Bytes), formatInt(stats.Rows))
				fmt.Print("Proceed? [y/N]: ")
				reader := bufio.NewReader(os.Stdin)
				ans, _ := reader.ReadString('\n')
				if ans = strings.ToLower(strings.TrimSpace(ans)); ans != "y" && ans != "yes" {
					fmt.Println("Cancelled.")
					return nil
				}
			}
			if err := app.store.PurgeBodyIndex(ctx); err != nil {
				return err
			}
			if effectiveOutput(rc, app.cfg) == "json" {
				return json.NewEncoder(os.Stdout).Encode(map[string]any{
					"purged_rows":  stats.Rows,
					"purged_bytes": stats.Bytes,
				})
			}
			fmt.Println("Index purged.")
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "skip the destructive-op confirmation prompt")
	return cmd
}

// formatInt prints n with thousands separators.
func formatInt(n int64) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	return b.String()
}

func formatIntCap(n int) string {
	if n <= 0 {
		return "no cap"
	}
	return formatInt(int64(n))
}

func formatBytes(n int64) string {
	switch {
	case n >= 1024*1024*1024:
		return fmt.Sprintf("%.1f GB", float64(n)/(1024*1024*1024))
	case n >= 1024*1024:
		return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
	case n >= 1024:
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	}
	return fmt.Sprintf("%d B", n)
}

func formatInt64Bytes(n int64) string {
	if n <= 0 {
		return "no cap"
	}
	return formatBytes(n)
}
