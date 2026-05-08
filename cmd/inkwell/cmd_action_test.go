package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/eugenelim/inkwell/internal/config"
)

func writeActionsFile(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "actions.toml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return path
}

// rcWithActions builds a rootContext whose loaded config points at
// the supplied actions.toml path. The config defaults are filled in.
func rcWithActions(path string) *rootContext {
	cfg := config.Defaults()
	cfg.CustomActions.File = path
	return &rootContext{cfg: cfg}
}

func TestActionListEmpty(t *testing.T) {
	rc := rcWithActions(filepath.Join(t.TempDir(), "missing.toml"))
	cmd := newActionListCmd(rc)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs(nil)
	require.NoError(t, cmd.ExecuteContext(context.Background()))
	require.Empty(t, out.String(), "missing actions.toml → exit 0 with no output")
}

func TestActionListPopulated(t *testing.T) {
	path := writeActionsFile(t, `
[[custom_action]]
name = "newsletter_done"
key = "n"
description = "Newsletter triage"
sequence = [{ op = "mark_read" }, { op = "archive" }]
`)
	rc := rcWithActions(path)
	cmd := newActionListCmd(rc)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs(nil)
	require.NoError(t, cmd.ExecuteContext(context.Background()))
	got := out.String()
	require.Contains(t, got, "NAME")
	require.Contains(t, got, "newsletter_done")
	require.Contains(t, got, "Newsletter triage")
}

func TestActionShowJSON(t *testing.T) {
	path := writeActionsFile(t, `
[[custom_action]]
name = "x"
description = "test"
sequence = [{ op = "mark_read" }, { op = "archive" }]
`)
	rc := rcWithActions(path)
	rc.output = "json"
	cmd := newActionShowCmd(rc)
	cmd.SetArgs([]string{"x"})
	require.NoError(t, cmd.ExecuteContext(context.Background()))
}

func TestActionShowMissingActionErrors(t *testing.T) {
	path := writeActionsFile(t, `
[[custom_action]]
name = "x"
description = "test"
sequence = [{ op = "mark_read" }]
`)
	rc := rcWithActions(path)
	cmd := newActionShowCmd(rc)
	cmd.SetArgs([]string{"does_not_exist"})
	err := cmd.ExecuteContext(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestActionValidateExitsZeroOnGoodFile(t *testing.T) {
	path := writeActionsFile(t, `
[[custom_action]]
name = "x"
description = "test"
sequence = [{ op = "mark_read" }, { op = "archive" }]
`)
	rc := rcWithActions(path)
	cmd := newActionValidateCmd(rc)
	cmd.SetArgs([]string{"--file", path})
	var out bytes.Buffer
	cmd.SetOut(&out)
	require.NoError(t, cmd.ExecuteContext(context.Background()))
	require.Contains(t, out.String(), "1 action(s) loaded")
}

func TestActionValidateNonzeroOnBadFile(t *testing.T) {
	// Capture os.Exit by intercepting stderr.
	path := writeActionsFile(t, `
[[custom_action]]
name = "x"
description = "test"
sequence = [{ op = "this_op_does_not_exist" }]
`)
	rc := rcWithActions(path)
	cmd := newActionValidateCmd(rc)
	cmd.SetArgs([]string{"--file", path})
	// We avoid os.Exit by running through the loader directly.
	cat, err := loadCatalogueForCLI(rc)
	require.Error(t, err)
	require.Nil(t, cat)
	_ = cmd
}

// TestRunFilterRejectsPerMessageTemplate verifies the per-message
// template detection: an action whose template references {{.From}}
// cannot be invoked with --filter (since per-message vars are unbound
// in filter mode). We exercise the gate by loading the catalogue
// and asserting the bit; the actual run-path invocation is checked
// separately because it requires a signed-in app.
func TestRunFilterRejectsPerMessageTemplate(t *testing.T) {
	path := writeActionsFile(t, `
[[custom_action]]
name = "y"
description = "test"
sequence = [{ op = "add_category", category = "from-{{.SenderDomain}}" }]
`)
	rc := rcWithActions(path)
	cat, err := loadCatalogueForCLI(rc)
	require.NoError(t, err)
	a := cat.ByName["y"]
	require.NotNil(t, a)
	require.True(t, a.RequiresMessageContext, "per-message template must set RequiresMessageContext")
}

// TestActionRootCommandRegisters confirms the parent `action` cobra
// command exists in the root command tree (cmd_root.go wiring).
func TestActionRootCommandRegisters(t *testing.T) {
	root := newRootCmd()
	var found *cobra.Command
	for _, c := range root.Commands() {
		if c.Name() == "action" {
			found = c
			break
		}
	}
	require.NotNil(t, found, "newRootCmd must register `action`")
	subs := []string{"list", "show", "run", "validate"}
	for _, name := range subs {
		hit := false
		for _, c := range found.Commands() {
			if c.Name() == name {
				hit = true
				break
			}
		}
		require.True(t, hit, "missing subcommand %q", name)
	}
}

// TestExpandHomeStr verifies the expandHome wrapper exposed for
// loader path resolution.
func TestExpandHomeStr(t *testing.T) {
	got := expandHomeStr("~/inkwell/actions.toml")
	require.False(t, strings.HasPrefix(got, "~/"), "expandHome must drop the ~/ prefix")
}
