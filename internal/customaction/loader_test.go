package customaction

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/eugenelim/inkwell/internal/pattern"
)

// stubPatternCompile is a no-op pattern.Compile that returns a non-nil
// Compiled. Used by tests that don't need pattern semantics.
func stubPatternCompile(s string, opts pattern.CompileOptions) (*pattern.Compiled, error) {
	if s == "BAD-PATTERN" {
		return nil, errors.New("syntax error")
	}
	return &pattern.Compiled{}, nil
}

func writeActions(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "actions.toml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return path
}

func defaultDeps() Deps {
	return Deps{PatternCompile: stubPatternCompile}
}

func TestLoadCatalogueEmpty(t *testing.T) {
	cat, err := LoadCatalogue(context.Background(), filepath.Join(t.TempDir(), "missing.toml"), defaultDeps())
	require.NoError(t, err)
	require.NotNil(t, cat)
	require.Empty(t, cat.Actions)
}

func TestLoadCatalogueValidatesOpName(t *testing.T) {
	path := writeActions(t, `
[[custom_action]]
name = "x"
description = "test"
sequence = [{ op = "this_op_does_not_exist" }]
`)
	_, err := LoadCatalogue(context.Background(), path, defaultDeps())
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown op")
}

func TestLoadCatalogueRejectsDeferredOp(t *testing.T) {
	path := writeActions(t, `
[[custom_action]]
name = "x"
description = "test"
sequence = [{ op = "shell", url = "x" }]
`)
	_, err := LoadCatalogue(context.Background(), path, defaultDeps())
	require.Error(t, err)
	require.Contains(t, err.Error(), "deferred")
}

func TestLoadCatalogueRequiresParams(t *testing.T) {
	path := writeActions(t, `
[[custom_action]]
name = "x"
description = "test"
sequence = [{ op = "move" }]
`)
	_, err := LoadCatalogue(context.Background(), path, defaultDeps())
	require.Error(t, err)
	require.Contains(t, err.Error(), "destination")
}

func TestLoadCatalogueValidatesPattern(t *testing.T) {
	path := writeActions(t, `
[[custom_action]]
name = "x"
description = "test"
sequence = [{ op = "filter", pattern = "BAD-PATTERN" }]
`)
	_, err := LoadCatalogue(context.Background(), path, defaultDeps())
	require.Error(t, err)
	require.Contains(t, err.Error(), "pattern")
}

func TestLoadCatalogueValidatesTemplate(t *testing.T) {
	path := writeActions(t, `
[[custom_action]]
name = "x"
description = "test"
sequence = [{ op = "add_category", category = "{{.NotAField" }]
`)
	_, err := LoadCatalogue(context.Background(), path, defaultDeps())
	require.Error(t, err)
}

func TestLoadCatalogueRoadmapAliasRewrite(t *testing.T) {
	path := writeActions(t, `
[[custom_action]]
name = "x"
description = "test"
allow_folder_template = true
sequence = [{ op = "move", destination = "Clients/{sender_domain}" }]
`)
	cat, err := LoadCatalogue(context.Background(), path, defaultDeps())
	require.NoError(t, err)
	require.Len(t, cat.Actions, 1)
	got := cat.Actions[0].Steps[0].Params["destination"].(string)
	require.Equal(t, "Clients/{{.SenderDomain}}", got, "single-brace alias must rewrite")
}

func TestLoadCatalogueRejectsPermDeleteWithConfirmNever(t *testing.T) {
	path := writeActions(t, `
[[custom_action]]
name = "x"
description = "test"
confirm = "never"
sequence = [{ op = "permanent_delete" }]
`)
	_, err := LoadCatalogue(context.Background(), path, defaultDeps())
	require.Error(t, err)
	require.Contains(t, err.Error(), "destructive op forbidden")
}

func TestLoadCatalogueRejectsDuplicateName(t *testing.T) {
	path := writeActions(t, `
[[custom_action]]
name = "x"
description = "test"
sequence = [{ op = "mark_read" }]

[[custom_action]]
name = "x"
description = "test 2"
sequence = [{ op = "archive" }]
`)
	_, err := LoadCatalogue(context.Background(), path, defaultDeps())
	require.Error(t, err)
	require.Contains(t, err.Error(), "duplicate name")
}

func TestLoadCatalogueRejectsDuplicateKey(t *testing.T) {
	path := writeActions(t, `
[[custom_action]]
name = "x"
key = "n"
description = "test"
sequence = [{ op = "mark_read" }]

[[custom_action]]
name = "y"
key = "n"
description = "test 2"
sequence = [{ op = "archive" }]
`)
	_, err := LoadCatalogue(context.Background(), path, defaultDeps())
	require.Error(t, err)
	require.Contains(t, err.Error(), "already bound")
}

func TestLoadCatalogueRejectsKeyCollidingWithDefault(t *testing.T) {
	path := writeActions(t, `
[[custom_action]]
name = "x"
key = "a"
description = "test"
sequence = [{ op = "mark_read" }]
`)
	deps := defaultDeps()
	deps.ReservedKeys = map[string]string{"a": "Archive"}
	_, err := LoadCatalogue(context.Background(), path, deps)
	require.Error(t, err)
	require.Contains(t, err.Error(), "Archive")
}

func TestLoadCatalogueAcceptsKeyAfterRebindingDefault(t *testing.T) {
	path := writeActions(t, `
[[custom_action]]
name = "x"
key = "a"
description = "test"
sequence = [{ op = "mark_read" }]
`)
	deps := defaultDeps()
	deps.ReservedKeys = map[string]string{} // user re-bound Archive elsewhere
	_, err := LoadCatalogue(context.Background(), path, deps)
	require.NoError(t, err)
}

func TestLoadCatalogueRejectsExtraTomlFields(t *testing.T) {
	path := writeActions(t, `
[[custom_action]]
name = "x"
description = "test"
seqeunce = [{ op = "mark_read" }]
`)
	_, err := LoadCatalogue(context.Background(), path, defaultDeps())
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown key")
}

func TestLoadCatalogueAcceptsAllowFolderTemplateOptIn(t *testing.T) {
	// Without opt-in: rejected.
	path := writeActions(t, `
[[custom_action]]
name = "x"
description = "test"
sequence = [{ op = "move", destination = "Clients/{{.SenderDomain}}" }]
`)
	_, err := LoadCatalogue(context.Background(), path, defaultDeps())
	require.Error(t, err)
	require.Contains(t, err.Error(), "allow_folder_template")

	// With opt-in: accepted.
	path = writeActions(t, `
[[custom_action]]
name = "x"
description = "test"
allow_folder_template = true
sequence = [{ op = "move", destination = "Clients/{{.SenderDomain}}" }]
`)
	cat, err := LoadCatalogue(context.Background(), path, defaultDeps())
	require.NoError(t, err)
	require.Len(t, cat.Actions, 1)
	require.True(t, cat.Actions[0].AllowFolderTpl)
}

func TestLoadCatalogueAcceptsAllowURLTemplateOptIn(t *testing.T) {
	// Without opt-in: rejected.
	path := writeActions(t, `
[[custom_action]]
name = "x"
description = "test"
sequence = [{ op = "open_url", url = "https://example.invalid/?leak={{.From}}" }]
`)
	_, err := LoadCatalogue(context.Background(), path, defaultDeps())
	require.Error(t, err)
	require.Contains(t, err.Error(), "allow_url_template")

	// With opt-in: accepted.
	path = writeActions(t, `
[[custom_action]]
name = "x"
description = "test"
allow_url_template = true
sequence = [{ op = "open_url", url = "https://example.invalid/?q={{.From}}" }]
`)
	_, err = LoadCatalogue(context.Background(), path, defaultDeps())
	require.NoError(t, err)
}

func TestLoadCatalogueRejectsChordKey(t *testing.T) {
	path := writeActions(t, `
[[custom_action]]
name = "x"
key = "<C-x> n"
description = "test"
sequence = [{ op = "mark_read" }]
`)
	_, err := LoadCatalogue(context.Background(), path, defaultDeps())
	require.Error(t, err)
	require.Contains(t, err.Error(), "chord")
}

func TestLoadCatalogueRejectsAddCategoryReplyLater(t *testing.T) {
	path := writeActions(t, `
[[custom_action]]
name = "x"
description = "test"
sequence = [{ op = "add_category", category = "Inkwell/ReplyLater" }]
`)
	_, err := LoadCatalogue(context.Background(), path, defaultDeps())
	require.Error(t, err)
	require.Contains(t, err.Error(), "thread_add_category")
}

func TestLoadCatalogueAllowsTemplatedCategoryReservedName(t *testing.T) {
	// A templated category that *might* render to ReplyLater is allowed —
	// we can't know at load time.
	path := writeActions(t, `
[[custom_action]]
name = "x"
description = "test"
sequence = [{ op = "add_category", category = "{{.Subject}}" }]
`)
	_, err := LoadCatalogue(context.Background(), path, defaultDeps())
	require.NoError(t, err)
}

func TestLoadCatalogueComputesRequiresMessageContext(t *testing.T) {
	path := writeActions(t, `
[[custom_action]]
name = "needs_msg"
description = "test"
sequence = [{ op = "add_category", category = "from-{{.SenderDomain}}" }]

[[custom_action]]
name = "literal_only"
description = "test"
sequence = [{ op = "mark_read" }]
`)
	cat, err := LoadCatalogue(context.Background(), path, defaultDeps())
	require.NoError(t, err)
	require.True(t, cat.ByName["needs_msg"].RequiresMessageContext)
	require.False(t, cat.ByName["literal_only"].RequiresMessageContext)
}

func TestLoadCatalogueParsesStepLevelStopOnError(t *testing.T) {
	path := writeActions(t, `
[[custom_action]]
name = "x"
description = "test"
stop_on_error = true
sequence = [
  { op = "mark_read" },
  { op = "archive", stop_on_error = false },
]
`)
	cat, err := LoadCatalogue(context.Background(), path, defaultDeps())
	require.NoError(t, err)
	require.Len(t, cat.Actions[0].Steps, 2)
	require.Nil(t, cat.Actions[0].Steps[0].StopOnError)
	require.NotNil(t, cat.Actions[0].Steps[1].StopOnError)
	require.False(t, *cat.Actions[0].Steps[1].StopOnError)
}

func TestLoadCatalogueSetSenderRoutingStaticEnumOnly(t *testing.T) {
	path := writeActions(t, `
[[custom_action]]
name = "x"
description = "test"
sequence = [{ op = "set_sender_routing", destination = "{{.From}}" }]
`)
	_, err := LoadCatalogue(context.Background(), path, defaultDeps())
	require.Error(t, err)
	require.Contains(t, err.Error(), "literal")
}

func TestLoadCatalogueSetSenderRoutingValidEnum(t *testing.T) {
	path := writeActions(t, `
[[custom_action]]
name = "x"
description = "test"
sequence = [{ op = "set_sender_routing", destination = "feed" }]
`)
	cat, err := LoadCatalogue(context.Background(), path, defaultDeps())
	require.NoError(t, err)
	require.Len(t, cat.Actions, 1)
}

func TestLoadCatalogueSetSenderRoutingRejectsBadEnum(t *testing.T) {
	path := writeActions(t, `
[[custom_action]]
name = "x"
description = "test"
sequence = [{ op = "set_sender_routing", destination = "primary" }]
`)
	_, err := LoadCatalogue(context.Background(), path, defaultDeps())
	require.Error(t, err)
	require.Contains(t, err.Error(), "must be one of")
}

func TestLoadCatalogueOpenURLLiteralValidation(t *testing.T) {
	// Literal http URL — validates at load.
	path := writeActions(t, `
[[custom_action]]
name = "x"
description = "test"
sequence = [{ op = "open_url", url = "ftp://nope" }]
`)
	_, err := LoadCatalogue(context.Background(), path, defaultDeps())
	require.Error(t, err)
	require.Contains(t, err.Error(), "http")
}

func TestLoadCataloguePromptConfirmAlias(t *testing.T) {
	path := writeActions(t, `
[[custom_action]]
name = "x"
description = "test"
prompt_confirm = true
sequence = [{ op = "mark_read" }]
`)
	cat, err := LoadCatalogue(context.Background(), path, defaultDeps())
	require.NoError(t, err)
	require.Equal(t, ConfirmAlways, cat.Actions[0].Confirm)
}

func TestLoadCatalogueRejectsTooManyActions(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < 257; i++ {
		sb.WriteString(`
[[custom_action]]
name = "a`)
		// Generate a deterministic 32-char-bound name; "a"+i is fine for 257 entries.
		sb.WriteString(addrSuffix(i))
		sb.WriteString(`"
description = "t"
sequence = [{ op = "mark_read" }]
`)
	}
	path := writeActions(t, sb.String())
	_, err := LoadCatalogue(context.Background(), path, defaultDeps())
	require.Error(t, err)
	require.Contains(t, err.Error(), "exceeds the cap")
}

// addrSuffix is the short alphanumeric suffix helper, copied from
// store/bundled_senders_test.go (avoids cross-package test imports).
func addrSuffix(i int) string {
	const digits = "0123456789abcdefghijklmnopqrstuvwxyz"
	if i == 0 {
		return "0"
	}
	var buf [16]byte
	n := 0
	for i > 0 {
		buf[n] = digits[i%36]
		i /= 36
		n++
	}
	out := make([]byte, n)
	for k := 0; k < n; k++ {
		out[k] = buf[n-1-k]
	}
	return string(out)
}
