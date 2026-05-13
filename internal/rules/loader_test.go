package rules

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eugenelim/inkwell/internal/customaction"
	"github.com/stretchr/testify/require"
)

func TestLoadCatalogueValidExample(t *testing.T) {
	body := []byte(`
[[rule]]
name = "Newsletters → Feed"
sequence = 10
enabled = true

  [rule.when]
  sender_contains = ["newsletter@", "no-reply@"]
  header_contains = ["List-Unsubscribe"]

  [rule.then]
  move = "Folders/Newsletters"
  mark_read = true
  stop = true

[[rule]]
name = "Receipts"
sequence = 20

  [rule.when]
  from = [
    { address = "receipts@amazon.com" },
    { address = "billing@stripe.com" },
  ]

  [rule.then]
  move = "Folders/Paper Trail"
  add_categories = ["Receipt"]
`)
	cat, err := parseCatalogue("rules.toml", body)
	require.NoError(t, err)
	require.Len(t, cat.Rules, 2)

	r1 := cat.Rules[0]
	require.Equal(t, "Newsletters → Feed", r1.Name)
	require.Equal(t, 10, r1.Sequence)
	require.True(t, r1.Enabled)
	require.Equal(t, []string{"newsletter@", "no-reply@"}, r1.When.SenderContains)
	require.Equal(t, "Folders/Newsletters", r1.Then.MoveToFolder)
	require.NotNil(t, r1.Then.MarkAsRead)
	require.True(t, *r1.Then.MarkAsRead)
	require.NotNil(t, r1.Then.StopProcessingRules)

	r2 := cat.Rules[1]
	require.Equal(t, "Receipts", r2.Name)
	require.True(t, r2.Enabled, "enabled defaults to true when omitted")
	require.Len(t, r2.When.FromAddresses, 2)
	require.Equal(t, "receipts@amazon.com", r2.When.FromAddresses[0].EmailAddress.Address)
	require.Equal(t, "Folders/Paper Trail", r2.Then.MoveToFolder)
	require.Equal(t, []string{"Receipt"}, r2.Then.AssignCategories)
}

func TestLoadCatalogueRejectsDeferredPredicate(t *testing.T) {
	body := []byte(`
[[rule]]
name = "VM"
sequence = 1

  [rule.when]
  is_voicemail = true

  [rule.then]
  mark_read = true
`)
	_, err := parseCatalogue("rules.toml", body)
	require.Error(t, err)
	require.Contains(t, err.Error(), "is_voicemail")
	require.Contains(t, err.Error(), "unknown key")
}

func TestLoadCatalogueRejectsForwardAction(t *testing.T) {
	body := []byte(`
[[rule]]
name = "Send to assistant"
sequence = 1

  [rule.when]
  sender_contains = ["boss@"]

  [rule.then]
  forward_to = ["assistant@example.invalid"]
`)
	_, err := parseCatalogue("rules.toml", body)
	require.Error(t, err)
	require.Contains(t, err.Error(), "forward_to")
}

func TestLoadCatalogueRejectsDeleteWithoutConfirm(t *testing.T) {
	body := []byte(`
[[rule]]
name = "Trash spam"
sequence = 1

  [rule.when]
  sender_contains = ["spam@"]

  [rule.then]
  delete = true
`)
	_, err := parseCatalogue("rules.toml", body)
	require.Error(t, err)
	require.Contains(t, err.Error(), "delete")
	require.Contains(t, err.Error(), "confirm")
}

func TestLoadCatalogueRejectsConfirmNeverOnDestructive(t *testing.T) {
	body := []byte(`
[[rule]]
name = "Trash spam"
sequence = 1
confirm = "never"

  [rule.when]
  sender_contains = ["spam@"]

  [rule.then]
  delete = true
`)
	_, err := parseCatalogue("rules.toml", body)
	require.Error(t, err)
	require.Contains(t, err.Error(), "never")
}

func TestLoadCatalogueAcceptsDeleteWithConfirmAlways(t *testing.T) {
	body := []byte(`
[[rule]]
name = "Trash spam"
sequence = 1
confirm = "always"

  [rule.when]
  sender_contains = ["spam@"]

  [rule.then]
  delete = true
`)
	cat, err := parseCatalogue("rules.toml", body)
	require.NoError(t, err)
	require.Len(t, cat.Rules, 1)
	require.Equal(t, customaction.ConfirmAlways, cat.Rules[0].Confirm)
}

func TestLoadCatalogueRejectsDuplicateNameNoID(t *testing.T) {
	body := []byte(`
[[rule]]
name = "Same"
sequence = 1
  [rule.then]
  mark_read = true

[[rule]]
name = "Same"
sequence = 2
  [rule.then]
  mark_read = true
`)
	_, err := parseCatalogue("rules.toml", body)
	require.Error(t, err)
	require.Contains(t, err.Error(), "duplicate")
}

func TestLoadCatalogueAllowsDuplicateNameWithDistinctIDs(t *testing.T) {
	body := []byte(`
[[rule]]
id = "x1"
name = "Same"
sequence = 1
  [rule.then]
  mark_read = true

[[rule]]
id = "x2"
name = "Same"
sequence = 2
  [rule.then]
  mark_read = true
`)
	cat, err := parseCatalogue("rules.toml", body)
	require.NoError(t, err)
	require.Len(t, cat.Rules, 2)
}

func TestLoadCatalogueAcceptsShorthandFromString(t *testing.T) {
	body := []byte(`
[[rule]]
name = "Shorthand"
sequence = 1

  [rule.when]
  from = ["a@x", "b@y"]

  [rule.then]
  mark_read = true
`)
	cat, err := parseCatalogue("rules.toml", body)
	require.NoError(t, err)
	require.Len(t, cat.Rules[0].When.FromAddresses, 2)
	require.Equal(t, "a@x", cat.Rules[0].When.FromAddresses[0].EmailAddress.Address)
	require.Equal(t, "b@y", cat.Rules[0].When.FromAddresses[1].EmailAddress.Address)
}

func TestLoadCatalogueMissingFileIsEmpty(t *testing.T) {
	dir := t.TempDir()
	cat, err := LoadCatalogue(filepath.Join(dir, "does-not-exist.toml"))
	require.NoError(t, err)
	require.Empty(t, cat.Rules)
}

func TestLoadCatalogueLoadsFromDisk(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rules.toml")
	require.NoError(t, os.WriteFile(path, []byte(`
[[rule]]
name = "X"
sequence = 1
  [rule.then]
  mark_read = true
`), 0o600))
	cat, err := LoadCatalogue(path)
	require.NoError(t, err)
	require.Len(t, cat.Rules, 1)
	require.Equal(t, path, cat.Path)
}

func TestLoadCatalogueRejectsUnknownField(t *testing.T) {
	body := []byte(`
[[rule]]
name = "Typo"
sequence = 1

  [rule.when]
  form_contains = ["x"]  # typo of from / sender_contains

  [rule.then]
  mark_read = true
`)
	_, err := parseCatalogue("rules.toml", body)
	require.Error(t, err)
	require.Contains(t, err.Error(), "form_contains")
}

func TestLoadCatalogueRejectsBadFlagValue(t *testing.T) {
	body := []byte(`
[[rule]]
name = "Bad flag"
sequence = 1

  [rule.when]
  flag = "not_a_real_flag"

  [rule.then]
  mark_read = true
`)
	_, err := parseCatalogue("rules.toml", body)
	require.Error(t, err)
	require.Contains(t, err.Error(), "flag")
	require.Contains(t, err.Error(), "any")
}

func TestLoadCatalogueRejectsBadImportance(t *testing.T) {
	body := []byte(`
[[rule]]
name = "Bad imp"
sequence = 1

  [rule.when]
  importance = "super-high"

  [rule.then]
  mark_read = true
`)
	_, err := parseCatalogue("rules.toml", body)
	require.Error(t, err)
	require.Contains(t, err.Error(), "importance")
}

func TestLoadCatalogueRejectsEmptyName(t *testing.T) {
	body := []byte(`
[[rule]]
name = ""
sequence = 1
  [rule.then]
  mark_read = true
`)
	_, err := parseCatalogue("rules.toml", body)
	require.Error(t, err)
	require.Contains(t, err.Error(), "name is required")
}

func TestLoadCatalogueRejectsNegativeSequence(t *testing.T) {
	body := []byte(`
[[rule]]
name = "Neg"
sequence = -1
  [rule.then]
  mark_read = true
`)
	_, err := parseCatalogue("rules.toml", body)
	require.Error(t, err)
	require.Contains(t, err.Error(), "sequence")
}

func TestLoadCatalogueRejectsEmptyThen(t *testing.T) {
	body := []byte(`
[[rule]]
name = "No actions"
sequence = 1
  [rule.when]
  sender_contains = ["x"]
`)
	_, err := parseCatalogue("rules.toml", body)
	require.Error(t, err)
	require.Contains(t, err.Error(), "[rule.then]")
}

func TestLoadCatalogueRejectsBadSizeRange(t *testing.T) {
	body := []byte(`
[[rule]]
name = "Bad size"
sequence = 1
  [rule.when]
  size_min_kb = 100
  size_max_kb = 10
  [rule.then]
  mark_read = true
`)
	_, err := parseCatalogue("rules.toml", body)
	require.Error(t, err)
	require.Contains(t, err.Error(), "size_min_kb")
	require.Contains(t, err.Error(), "size_max_kb")
}

func TestLoadCatalogueAcceptsSizeRange(t *testing.T) {
	body := []byte(`
[[rule]]
name = "Size OK"
sequence = 1
  [rule.when]
  size_min_kb = 10
  size_max_kb = 1000
  [rule.then]
  mark_read = true
`)
	cat, err := parseCatalogue("rules.toml", body)
	require.NoError(t, err)
	require.NotNil(t, cat.Rules[0].When.WithinSizeRange)
	require.Equal(t, 10, cat.Rules[0].When.WithinSizeRange.MinimumSize)
	require.Equal(t, 1000, cat.Rules[0].When.WithinSizeRange.MaximumSize)
}
