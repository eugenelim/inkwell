package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

// runBundleCmd builds a `bundle <verb>` cobra command and runs it
// against the supplied headlessApp by replacing RunE with a direct
// store call. Mirrors runMuteCmd in cmd_mute_test.go.
func runBundleCmd(t *testing.T, app *headlessApp, verb string, args []string, asJSON bool) string {
	t.Helper()
	rc := &rootContext{cfg: nil}
	if asJSON {
		rc.output = "json"
	}
	parent := newBundleCmd(rc)
	var sub *cobra.Command
	for _, c := range parent.Commands() {
		if c.Name() == verb {
			sub = c
			break
		}
	}
	require.NotNil(t, sub, "subcommand %q not found", verb)
	sub.RunE = func(c *cobra.Command, a []string) error {
		// Re-implement the RunE logic against the supplied app, skipping
		// buildHeadlessApp's MSAL probe.
		ctx := c.Context()
		switch verb {
		case "add":
			addr := strings.ToLower(strings.TrimSpace(a[0]))
			if err := validateBareAddress(addr); err != nil {
				return usageErr(err)
			}
			if err := app.store.AddBundledSender(ctx, app.account.ID, addr); err != nil {
				return err
			}
			if effectiveOutput(rc, rc.cfg) == "json" {
				return json.NewEncoder(c.OutOrStdout()).Encode(struct {
					Bundled bool   `json:"bundled"`
					Address string `json:"address"`
				}{true, addr})
			}
			c.OutOrStdout().Write([]byte("✓ bundled " + addr + "\n"))
		case "remove":
			addr := strings.ToLower(strings.TrimSpace(a[0]))
			if err := validateBareAddress(addr); err != nil {
				return usageErr(err)
			}
			if err := app.store.RemoveBundledSender(ctx, app.account.ID, addr); err != nil {
				return err
			}
			if effectiveOutput(rc, rc.cfg) == "json" {
				return json.NewEncoder(c.OutOrStdout()).Encode(struct {
					Bundled bool   `json:"bundled"`
					Address string `json:"address"`
				}{false, addr})
			}
			c.OutOrStdout().Write([]byte("✓ unbundled " + addr + "\n"))
		case "list":
			rows, err := app.store.ListBundledSenders(ctx, app.account.ID)
			if err != nil {
				return err
			}
			if effectiveOutput(rc, rc.cfg) == "json" {
				type row struct {
					Address string `json:"address"`
					AddedAt int64  `json:"added_at"`
				}
				out := make([]row, 0, len(rows))
				for _, r := range rows {
					out = append(out, row{r.Address, r.AddedAt.Unix()})
				}
				return json.NewEncoder(c.OutOrStdout()).Encode(out)
			}
			if len(rows) == 0 {
				c.OutOrStdout().Write([]byte("(no bundled senders)\n"))
				return nil
			}
			c.OutOrStdout().Write([]byte("ADDRESS\tADDED\n"))
			for _, r := range rows {
				c.OutOrStdout().Write([]byte(r.Address + "\t" + r.AddedAt.Format("2006-01-02T15:04:05Z07:00") + "\n"))
			}
		}
		return nil
	}
	var out bytes.Buffer
	parent.SetOut(&out)
	// Pass verb + args via the parent so cobra's dispatch resolves the
	// subcommand the same way the real command line does.
	full := append([]string{verb}, args...)
	parent.SetArgs(full)
	require.NoError(t, parent.ExecuteContext(context.Background()))
	return out.String()
}

func TestBundleCLIAddLowercases(t *testing.T) {
	app := newCLITestApp(t)
	runBundleCmd(t, app, "add", []string{"Bob@Acme.com"}, false)
	rows, err := app.store.ListBundledSenders(context.Background(), app.account.ID)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "bob@acme.com", rows[0].Address, "address must be stored lowercased")
}

func TestBundleCLIRemoveByCanonicalAddr(t *testing.T) {
	app := newCLITestApp(t)
	runBundleCmd(t, app, "add", []string{"news@acme.com"}, false)
	runBundleCmd(t, app, "remove", []string{"NEWS@acme.com"}, false)
	rows, err := app.store.ListBundledSenders(context.Background(), app.account.ID)
	require.NoError(t, err)
	require.Empty(t, rows)
}

func TestBundleCLIListJSONShape(t *testing.T) {
	app := newCLITestApp(t)
	runBundleCmd(t, app, "add", []string{"a@x.com"}, false)
	runBundleCmd(t, app, "add", []string{"b@x.com"}, false)
	out := runBundleCmd(t, app, "list", nil, true)
	var rows []map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &rows))
	require.Len(t, rows, 2)
	for _, r := range rows {
		require.Contains(t, r, "address")
		require.Contains(t, r, "added_at")
	}
}

func TestBundleCLIListTextShape(t *testing.T) {
	app := newCLITestApp(t)
	runBundleCmd(t, app, "add", []string{"news@acme.com"}, false)
	out := runBundleCmd(t, app, "list", nil, false)
	require.True(t, strings.HasPrefix(out, "ADDRESS\tADDED\n"), "header must be ADDRESS<TAB>ADDED, got: %q", out)
	require.Contains(t, out, "news@acme.com")
}

func TestBundleCLIAddIdempotent(t *testing.T) {
	app := newCLITestApp(t)
	runBundleCmd(t, app, "add", []string{"news@acme.com"}, false)
	runBundleCmd(t, app, "add", []string{"news@acme.com"}, false)
	rows, err := app.store.ListBundledSenders(context.Background(), app.account.ID)
	require.NoError(t, err)
	require.Len(t, rows, 1, "second add must be a no-op (no duplicate row)")
}

func TestBundleCLIRejectsDisplayNameAddress(t *testing.T) {
	// validateBareAddress (shared with cmd_route) rejects display-name
	// forms. The bundle CLI calls it before any store access, so the
	// validation is the same shape.
	cases := []struct {
		in    string
		valid bool
	}{
		{"news@acme.com", true},
		{`"News" <news@acme.com>`, false},
		{"", false},
		{"not-an-email", false},
	}
	for _, c := range cases {
		err := validateBareAddress(c.in)
		if c.valid {
			require.NoError(t, err, "in=%q", c.in)
		} else {
			require.Error(t, err, "in=%q", c.in)
		}
	}
}
