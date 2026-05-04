package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/eugenelim/inkwell/internal/action"
	"github.com/eugenelim/inkwell/internal/store"
)

// newFolderCmd is the spec 18 §6 singular `inkwell folder` group.
// Three subcommands: new, rename, delete. Outputs match the
// pattern from cmd_filter.go (text default; --output json on
// flagged commands).
func newFolderCmd(rc *rootContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "folder",
		Short: "Create / rename / delete mail folders",
		Long: `Manage mail folders without leaving inkwell.

Examples:
  inkwell folder new "Vendor Quotes"
  inkwell folder new "Vendor Quotes/2026"   # nested via slash
  inkwell folder rename "Vendor Quotes" "Vendor"
  inkwell folder delete "Vendor Quotes" --yes
`,
	}
	cmd.AddCommand(newFolderNewCmd(rc), newFolderRenameCmd(rc), newFolderDeleteCmd(rc))
	cmd.AddCommand(newFolderShowCmd(rc))
	cmd.AddCommand(newFolderSubscribeCmd(rc))
	cmd.AddCommand(newFolderUnsubscribeCmd(rc))
	cmd.AddCommand(newFolderTreeCmd(rc))
	return cmd
}

// resolveFolderByNameCtx looks up a cached folder by display name
// (case insensitive). The slash-path syntax supports nested lookup:
// "Parent/Child" finds Child whose parent's display name is Parent.
// Returns the folder ID + the resolved parent ID + the canonical
// displayName.
func resolveFolderByNameCtx(ctx context.Context, app *headlessApp, name string) (id string, parentID string, displayName string, err error) {
	all, listErr := app.store.ListFolders(ctx, app.account.ID)
	if listErr != nil {
		return "", "", "", fmt.Errorf("list folders: %w", listErr)
	}
	parts := strings.Split(name, "/")
	leaf := parts[len(parts)-1]
	parentName := ""
	if len(parts) > 1 {
		parentName = parts[len(parts)-2]
	}
	parentMatchID := ""
	if parentName != "" {
		for _, f := range all {
			if strings.EqualFold(f.DisplayName, parentName) {
				parentMatchID = f.ID
				break
			}
		}
		if parentMatchID == "" {
			return "", "", "", fmt.Errorf("parent folder %q not found", parentName)
		}
	}
	for _, f := range all {
		if !strings.EqualFold(f.DisplayName, leaf) {
			continue
		}
		if parentMatchID != "" && f.ParentFolderID != parentMatchID {
			continue
		}
		return f.ID, f.ParentFolderID, f.DisplayName, nil
	}
	return "", "", "", fmt.Errorf("folder %q not found", name)
}

func newFolderNewCmd(rc *rootContext) *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:   "new <name>",
		Short: "Create a new folder",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			ctx := c.Context()
			app, err := buildHeadlessApp(ctx, rc)
			if err != nil {
				return err
			}
			defer app.Close()

			path := args[0]
			parentID := ""
			leaf := path
			// Slash-path: "Parent/Child" creates Child under Parent.
			if i := strings.LastIndexByte(path, '/'); i > 0 {
				parentName := path[:i]
				leaf = path[i+1:]
				pid, _, _, perr := resolveFolderByNameCtx(ctx, app, parentName)
				if perr != nil {
					return fmt.Errorf("parent: %w", perr)
				}
				parentID = pid
			}

			exec := action.New(app.store, app.graph, app.logger)
			res, err := exec.CreateFolder(ctx, app.account.ID, parentID, leaf)
			if err != nil {
				return err
			}
			if output == "json" {
				return json.NewEncoder(os.Stdout).Encode(map[string]string{
					"id":             res.ID,
					"displayName":    res.DisplayName,
					"parentFolderId": res.ParentFolderID,
				})
			}
			fmt.Fprintf(os.Stdout, "✓ created folder %q (id=%s)\n", res.DisplayName, res.ID)
			return nil
		},
	}
	cmd.Flags().StringVar(&output, "output", "text", "output format: text|json")
	return cmd
}

func newFolderRenameCmd(rc *rootContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rename <name> <new-name>",
		Short: "Rename an existing folder",
		Args:  cobra.ExactArgs(2),
		RunE: func(c *cobra.Command, args []string) error {
			ctx := c.Context()
			app, err := buildHeadlessApp(ctx, rc)
			if err != nil {
				return err
			}
			defer app.Close()

			id, _, _, err := resolveFolderByNameCtx(ctx, app, args[0])
			if err != nil {
				return err
			}
			exec := action.New(app.store, app.graph, app.logger)
			if err := exec.RenameFolder(ctx, id, args[1]); err != nil {
				return err
			}
			fmt.Fprintf(os.Stdout, "✓ renamed %q → %q\n", args[0], args[1])
			return nil
		},
	}
	return cmd
}

func newFolderDeleteCmd(rc *rootContext) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a folder",
		Long: `Delete a folder. Children + messages cascade to Deleted Items
server-side; you can recover from Outlook's Deleted Items folder
within the tenant retention window. Without --yes, the command
prints what would be deleted and exits.
`,
		Args: cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			ctx := c.Context()
			app, err := buildHeadlessApp(ctx, rc)
			if err != nil {
				return err
			}
			defer app.Close()

			id, _, displayName, err := resolveFolderByNameCtx(ctx, app, args[0])
			if err != nil {
				return err
			}
			if !yes {
				fmt.Fprintf(os.Stderr,
					"Would delete folder %q (id=%s). Re-run with --yes to confirm.\n",
					displayName, id)
				return nil
			}
			exec := action.New(app.store, app.graph, app.logger)
			if err := exec.DeleteFolder(ctx, id); err != nil {
				return err
			}
			fmt.Fprintf(os.Stdout, "✓ deleted folder %q\n", displayName)
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "skip the confirmation prompt")
	return cmd
}

func newFolderShowCmd(rc *rootContext) *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:   "show <name>",
		Short: "Show details for a single folder",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			ctx := c.Context()
			app, err := buildHeadlessApp(ctx, rc)
			if err != nil {
				return err
			}
			defer app.Close()
			id, _, _, err := resolveFolderByNameCtx(ctx, app, args[0])
			if err != nil {
				return err
			}
			all, err := app.store.ListFolders(ctx, app.account.ID)
			if err != nil {
				return fmt.Errorf("list folders: %w", err)
			}
			var f *store.Folder
			for i := range all {
				if all[i].ID == id {
					f = &all[i]
					break
				}
			}
			if f == nil {
				return fmt.Errorf("folder not found after resolve")
			}
			if output == "json" {
				return json.NewEncoder(os.Stdout).Encode(f)
			}
			fmt.Fprintf(os.Stdout, "ID:           %s\n", f.ID)
			fmt.Fprintf(os.Stdout, "DisplayName:  %s\n", f.DisplayName)
			fmt.Fprintf(os.Stdout, "WellKnown:    %s\n", f.WellKnownName)
			fmt.Fprintf(os.Stdout, "ParentID:     %s\n", f.ParentFolderID)
			fmt.Fprintf(os.Stdout, "TotalCount:   %d\n", f.TotalCount)
			fmt.Fprintf(os.Stdout, "UnreadCount:  %d\n", f.UnreadCount)
			return nil
		},
	}
	cmd.Flags().StringVar(&output, "output", "text", "output format: text|json")
	return cmd
}

func newFolderSubscribeCmd(_ *rootContext) *cobra.Command {
	return &cobra.Command{
		Use:   "subscribe <name>",
		Short: "Mark a folder as subscribed (manage via [sync].subscribed_well_known in config)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			fmt.Fprintf(os.Stdout, "Subscription management is via the [sync].subscribed_well_known config key.\n")
			fmt.Fprintf(os.Stdout, "Add %q to that list and restart inkwell.\n", args[0])
			return nil
		},
	}
}

func newFolderUnsubscribeCmd(_ *rootContext) *cobra.Command {
	return &cobra.Command{
		Use:   "unsubscribe <name>",
		Short: "Mark a folder as unsubscribed (manage via [sync].subscribed_well_known in config)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			fmt.Fprintf(os.Stdout, "Subscription management is via the [sync].subscribed_well_known config key.\n")
			fmt.Fprintf(os.Stdout, "Remove %q from that list and restart inkwell.\n", args[0])
			return nil
		},
	}
}

func newFolderTreeCmd(rc *rootContext) *cobra.Command {
	return &cobra.Command{
		Use:   "tree",
		Short: "Print folder hierarchy as an indented tree",
		RunE: func(c *cobra.Command, _ []string) error {
			ctx := c.Context()
			app, err := buildHeadlessApp(ctx, rc)
			if err != nil {
				return err
			}
			defer app.Close()
			all, err := app.store.ListFolders(ctx, app.account.ID)
			if err != nil {
				return fmt.Errorf("list folders: %w", err)
			}
			printFolderTree(os.Stdout, all)
			return nil
		},
	}
}

// printFolderTree renders all folders as a depth-indented tree sorted
// by display name within each level.
func printFolderTree(w *os.File, folders []store.Folder) {
	byParent := make(map[string][]store.Folder)
	for _, f := range folders {
		byParent[f.ParentFolderID] = append(byParent[f.ParentFolderID], f)
	}
	// Sort within each parent group.
	for k := range byParent {
		sort.Slice(byParent[k], func(i, j int) bool {
			return byParent[k][i].DisplayName < byParent[k][j].DisplayName
		})
	}
	// Collect root folder IDs (no parent or parent not in our list).
	knownIDs := make(map[string]bool, len(folders))
	for _, f := range folders {
		knownIDs[f.ID] = true
	}
	roots := byParent[""]
	for _, f := range folders {
		if f.ParentFolderID != "" && !knownIDs[f.ParentFolderID] {
			roots = append(roots, f)
		}
	}
	sort.Slice(roots, func(i, j int) bool {
		return roots[i].DisplayName < roots[j].DisplayName
	})
	var walk func(f store.Folder, depth int)
	walk = func(f store.Folder, depth int) {
		indent := strings.Repeat("  ", depth)
		unread := ""
		if f.UnreadCount > 0 {
			unread = fmt.Sprintf(" (%d)", f.UnreadCount)
		}
		fmt.Fprintf(w, "%s%s%s\n", indent, f.DisplayName, unread)
		for _, child := range byParent[f.ID] {
			walk(child, depth+1)
		}
	}
	for _, r := range roots {
		walk(r, 0)
	}
}
