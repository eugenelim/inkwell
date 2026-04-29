package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func newFoldersCmd(rc *rootContext) *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:   "folders",
		Short: "List mail folders from the local cache",
		Long: `Print every cached folder for the signed-in account.

The list comes from the local SQLite cache; sync runs separately
(` + "`inkwell sync`" + `) and updates what's available here.

Examples:
  inkwell folders
  inkwell folders --output json`,
		RunE: func(c *cobra.Command, _ []string) error {
			ctx := c.Context()
			app, err := buildHeadlessApp(ctx, rc)
			if err != nil {
				return err
			}
			defer app.Close()
			fs, err := app.store.ListFolders(ctx, app.account.ID)
			if err != nil {
				return fmt.Errorf("list folders: %w", err)
			}
			if output == "json" {
				type row struct {
					ID            string `json:"id"`
					DisplayName   string `json:"displayName"`
					WellKnownName string `json:"wellKnownName,omitempty"`
					ParentID      string `json:"parentId,omitempty"`
					Total         int    `json:"totalCount"`
					Unread        int    `json:"unreadCount"`
				}
				out := make([]row, 0, len(fs))
				for _, f := range fs {
					out = append(out, row{
						ID: f.ID, DisplayName: f.DisplayName,
						WellKnownName: f.WellKnownName, ParentID: f.ParentFolderID,
						Total: f.TotalCount, Unread: f.UnreadCount,
					})
				}
				return json.NewEncoder(os.Stdout).Encode(out)
			}
			fmt.Fprintf(os.Stdout, "%-40s %5s %5s\n", "FOLDER", "TOTAL", "UNREAD")
			for _, f := range fs {
				name := f.DisplayName
				if f.WellKnownName != "" {
					name += "  (" + f.WellKnownName + ")"
				}
				fmt.Fprintf(os.Stdout, "%-40s %5d %5d\n", name, f.TotalCount, f.UnreadCount)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&output, "output", "text", "output format: text|json")
	return cmd
}
