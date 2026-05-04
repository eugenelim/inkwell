package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/eugenelim/inkwell/internal/store"
)

func newExportCmd(rc *rootContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export messages from the local cache",
		Long: `Export cached messages to a file.

Examples:
  inkwell export messages --folder Inbox --format json --to inbox.jsonl
  inkwell export messages --folder Inbox --format json`,
	}
	cmd.AddCommand(newExportMessagesCmd(rc))
	return cmd
}

func newExportMessagesCmd(rc *rootContext) *cobra.Command {
	var folder, format, toFile string
	var limit int
	cmd := &cobra.Command{
		Use:   "messages",
		Short: "Export messages to JSON (line-delimited) or mbox",
		RunE: func(c *cobra.Command, _ []string) error {
			if format == "" {
				format = "json"
			}
			if format != "json" && format != "mbox" {
				return fmt.Errorf("--format must be json or mbox")
			}
			ctx := c.Context()
			app, err := buildHeadlessApp(ctx, rc)
			if err != nil {
				return err
			}
			defer app.Close()

			folderID, err := resolveFolder(ctx, app, folder)
			if err != nil {
				return err
			}
			if limit <= 0 {
				limit = 10000
			}
			msgs, err := app.store.ListMessages(ctx, store.MessageQuery{
				AccountID: app.account.ID,
				FolderID:  folderID,
				Limit:     limit,
			})
			if err != nil {
				return fmt.Errorf("list messages: %w", err)
			}

			var out *os.File
			if toFile != "" {
				// #nosec G304 — user-supplied export path
				f, err := os.Create(toFile)
				if err != nil {
					return fmt.Errorf("create %s: %w", toFile, err)
				}
				defer f.Close()
				out = f
			} else {
				out = os.Stdout
			}

			switch format {
			case "json":
				enc := json.NewEncoder(out)
				for _, m := range msgs {
					if err := enc.Encode(m); err != nil {
						return fmt.Errorf("encode: %w", err)
					}
				}
			case "mbox":
				for _, m := range msgs {
					writeMboxEntry(out, m)
				}
			}

			if toFile != "" && !rc.quiet {
				fmt.Fprintf(os.Stderr, "exported %d message(s) to %s\n", len(msgs), toFile)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&folder, "folder", "", "folder name (empty = all folders)")
	cmd.Flags().StringVar(&format, "format", "json", "output format: json|mbox")
	cmd.Flags().StringVar(&toFile, "to", "", "output file path (default: stdout)")
	cmd.Flags().IntVar(&limit, "limit", 10000, "max messages to export")
	return cmd
}

// writeMboxEntry writes a minimal mbox-formatted entry for a message.
// Full body fetching is not performed — this uses envelope fields only.
func writeMboxEntry(f *os.File, m store.Message) {
	from := m.FromAddress
	if from == "" {
		from = "unknown@example.invalid"
	}
	fmt.Fprintf(f, "From %s %s\n", from, m.ReceivedAt.Format("Mon Jan _2 15:04:05 2006"))
	fmt.Fprintf(f, "From: %s <%s>\n", m.FromName, m.FromAddress)
	fmt.Fprintf(f, "Subject: %s\n", m.Subject)
	fmt.Fprintf(f, "Date: %s\n", m.ReceivedAt.Format("Mon, 02 Jan 2006 15:04:05 -0700"))
	fmt.Fprintln(f)
	fmt.Fprintln(f, m.BodyPreview)
	fmt.Fprintln(f)
}
