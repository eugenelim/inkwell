// Package rules owns the spec 32 server-side mail-rules surface:
// loading the user's rules.toml authoring file, pulling the current
// rule set from Graph, computing the apply diff, executing it (and
// rewriting rules.toml atomically afterwards), and surfacing rule
// metadata to the TUI / CLI.
//
// Layering: this is a mid-tier package (above `internal/store` and
// `internal/graph`; consumed by `internal/ui` and `cmd/inkwell`).
// It depends on the spec 27 `internal/customaction` package only for
// the `ConfirmPolicy` tri-state — borrowing the same vocabulary the
// custom-actions framework exposes (spec 32 §6.4).
package rules

import (
	"encoding/json"
	"time"

	"github.com/eugenelim/inkwell/internal/customaction"
	"github.com/eugenelim/inkwell/internal/store"
)

// Rule is the inkwell-side representation of a single mail rule.
// Carries both the v1-catalogue typed shape (When / Then / Except)
// and the raw Graph JSON for round-trip preservation of non-v1
// fields. Created by [LoadCatalogue] and by [Pull].
type Rule struct {
	// ID is the Graph rule id. Empty on a freshly-authored rule that
	// hasn't been applied yet; assigned by the server on first apply.
	ID string

	Name     string
	Sequence int
	Enabled  bool

	// Confirm controls per-rule prompting at apply time. Reuses the
	// spec 27 ConfirmPolicy tri-state (auto / always / never).
	Confirm customaction.ConfirmPolicy

	When   store.MessagePredicates
	Then   store.MessageActions
	Except store.MessagePredicates

	// Server-set flags. IsReadOnly / HasError are visible on rules
	// pulled from Graph; they're always false on rules authored in
	// rules.toml. The loader does not allow users to set them.
	IsReadOnly bool
	HasError   bool

	// Raw bytes preserve non-v1 predicate / action keys verbatim so
	// updates round-trip cleanly through the apply pipeline (spec 32
	// §4.3 / §5.3).
	RawConditions json.RawMessage
	RawActions    json.RawMessage
	RawExceptions json.RawMessage

	// LastPulledAt is set on rules surfaced by [Pull] / from the
	// local mirror. Zero on freshly-authored rules.
	LastPulledAt time.Time

	// SourcePos is the line number in rules.toml where this rule's
	// `[[rule]]` block starts. Used in loader error messages. Zero
	// for rules pulled from Graph.
	SourcePos int
}

// Catalogue is the result of loading a rules.toml file.
type Catalogue struct {
	Path  string // absolute path of the file loaded
	Rules []Rule
}

// DiffOp classifies what apply will do to a single rule.
type DiffOp int

// DiffOp values.
const (
	DiffNoop   DiffOp = iota // server and TOML agree
	DiffCreate               // TOML has it, server doesn't
	DiffUpdate               // TOML and server differ
	DiffDelete               // server has it, TOML doesn't
	DiffSkip                 // server is_read_only — apply leaves it alone
)

// DiffEntry is one classified rule in the apply diff.
type DiffEntry struct {
	Op            DiffOp
	Rule          Rule               // the desired-state rule (TOML side) for create/update/noop
	Prior         *store.MessageRule // the local mirror's view for update/delete/noop
	Warning       string             // optional one-line warning, e.g. retargeted folder ID
	IsDestructive bool               // any action where Delete = true
}
