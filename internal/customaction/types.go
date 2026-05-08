// Package customaction implements the spec 27 framework: user-
// authored TOML recipes that chain primitive mail operations
// (mark_read, archive, set_sender_routing, etc.) into one named
// verb bound to a key, a palette row, and a CLI subcommand.
//
// The package owns three concerns: load-time validation
// (LoadCatalogue, loader.go), the op catalogue + dispatch table
// (ops.go), and the runtime executor (executor.go). Consumer-defined
// interfaces (Triage, Bulk, Thread, Muter, RoutingWriter,
// Unsubscriber, FolderResolver) keep this package free of
// internal/ui imports — see CLAUDE.md §2 layering.
package customaction

import (
	"context"
	"log/slog"
	"text/template"
	"time"

	"github.com/eugenelim/inkwell/internal/pattern"
)

// Action is one user-defined recipe loaded from actions.toml.
// Pure data + compiled-template / pattern caches; no runtime state.
//
// Scope and ConfirmPolicy are package-local so the spec 27 types
// don't collide with internal/ui's existing Pane / ConfirmMode
// identifiers.
type Action struct {
	Name        string
	Key         string
	Description string
	When        []Scope
	Confirm     ConfirmPolicy
	StopOnError bool
	// AllowFolderTpl gates the §4.3 destination-template safety
	// guard: a step whose move/move_filtered destination references
	// message-derived data (e.g. {{.SenderDomain}}) is rejected at
	// load unless this flag is true on the action.
	AllowFolderTpl bool
	// AllowURLTpl gates the §4.3 open_url PII guard. Without it a
	// recipe that templates {{.From}} into an http URL is a supply-
	// chain exfil vector; with it the user has explicitly opted in.
	AllowURLTpl bool
	Steps       []Step
	// RequiresMessageContext is true if any step references a per-
	// message template variable. Set by the loader and consulted by
	// the CLI to reject `--filter` invocations.
	RequiresMessageContext bool
}

// Step is one entry in an action's sequence. Params is the typed
// projection of the TOML inline table; the executor reads it through
// the op-specific schema.
type Step struct {
	Op           OpKind
	Params       map[string]any
	PatternC     *pattern.Compiled
	Templated    map[string]*template.Template
	StopOnError  *bool
	requiresMsg  bool // computed by loader; true if any template references per-message data
	requiresUser bool // true if any template references {{.UserInput}}
}

// Catalogue is the full loaded table of custom actions. ByName /
// ByKey are immutable post-load O(1) lookups for `:actions` and
// keymap dispatch.
type Catalogue struct {
	Actions []Action
	ByName  map[string]*Action
	ByKey   map[string]*Action
}

// Scope is the pane-set in which an action is bindable.
type Scope int

// Scope values.
const (
	ScopeList Scope = iota + 1
	ScopeViewer
	ScopeFolders
)

// ConfirmPolicy is the auto/always/never knob.
type ConfirmPolicy int

// ConfirmPolicy values.
const (
	ConfirmAuto ConfirmPolicy = iota
	ConfirmAlways
	ConfirmNever
)

// OpKind discriminates the 22 primitive operations.
type OpKind string

// OpKind constants — the v1.1 closed catalogue.
const (
	OpMarkRead                OpKind = "mark_read"
	OpMarkUnread              OpKind = "mark_unread"
	OpFlag                    OpKind = "flag"
	OpUnflag                  OpKind = "unflag"
	OpArchive                 OpKind = "archive"
	OpSoftDelete              OpKind = "soft_delete"
	OpPermanentDelete         OpKind = "permanent_delete"
	OpMove                    OpKind = "move"
	OpAddCategory             OpKind = "add_category"
	OpRemoveCategory          OpKind = "remove_category"
	OpSetSenderRouting        OpKind = "set_sender_routing"
	OpSetThreadMuted          OpKind = "set_thread_muted"
	OpThreadAddCategory       OpKind = "thread_add_category"
	OpThreadRemoveCategory    OpKind = "thread_remove_category"
	OpThreadArchive           OpKind = "thread_archive"
	OpUnsubscribe             OpKind = "unsubscribe"
	OpFilter                  OpKind = "filter"
	OpMoveFiltered            OpKind = "move_filtered"
	OpPermanentDeleteFiltered OpKind = "permanent_delete_filtered"
	OpPromptValue             OpKind = "prompt_value"
	OpAdvanceCursor           OpKind = "advance_cursor"
	OpOpenURL                 OpKind = "open_url"
)

// deferredOps are the §3.6 op strings reserved for future specs.
// The loader rejects them at parse time with a friendly redirect.
var deferredOps = map[string]string{
	"block_sender": "deferred — server-side mailbox rules need their own spec",
	"shell":        "deferred — sandboxing review pending",
	"forward":      "deferred — drafts only, never send (PRD §3.1)",
}

// Context is the per-invocation data the templating layer reads and
// the executor passes step-to-step. Mutated only by prompt_value,
// which binds UserInput.
type Context struct {
	AccountID      int64
	From           string
	FromName       string
	SenderDomain   string
	To             string
	Subject        string
	ConversationID string
	MessageID      string
	IsRead         bool
	FlagStatus     string
	Date           time.Time
	Folder         string
	UserInput      string
	SelectionIDs   []string
	SelectionKind  string // "single" | "thread" | "filtered"
}

// ExecDeps is the dispatch surface. Consumer-defined here so this
// package does not import internal/ui (CLAUDE.md §2 layering).
type ExecDeps struct {
	Triage      Triage
	Bulk        Bulk
	Thread      Thread
	Mute        Muter
	Routing     RoutingWriter
	Unsubscribe Unsubscriber
	Folders     FolderResolver
	OpenURL     func(url string) error
	NowFn       func() time.Time
	Logger      *slog.Logger
	// PatternCompile compiles a runtime pattern (used by *_filtered
	// ops; the loader pre-compiles literal patterns but templated
	// patterns must compile at run-time after substitution). Wire to
	// pattern.Compile.
	PatternCompile func(string, pattern.CompileOptions) (*pattern.Compiled, error)
	// ConfirmThreshold mirrors [triage].confirm_threshold; the
	// confirm-auto branch fires when a sequence touches > N
	// messages. 0 disables the threshold check.
	ConfirmThreshold int
	// AccountID is the signed-in account ID. Cached here so callers
	// don't have to thread it through every Run invocation.
	AccountID int64
}

// Triage mirrors internal/ui.TriageExecutor (the per-message
// triage surface from spec 07). Consumer-defined to break the
// import cycle with internal/ui.
type Triage interface {
	MarkRead(ctx context.Context, accountID int64, messageID string) error
	MarkUnread(ctx context.Context, accountID int64, messageID string) error
	ToggleFlag(ctx context.Context, accountID int64, messageID string, currentlyFlagged bool) error
	SoftDelete(ctx context.Context, accountID int64, messageID string) error
	Archive(ctx context.Context, accountID int64, messageID string) error
	Move(ctx context.Context, accountID int64, messageID, destFolderID, destAlias string) error
	PermanentDelete(ctx context.Context, accountID int64, messageID string) error
	AddCategory(ctx context.Context, accountID int64, messageID, category string) error
	RemoveCategory(ctx context.Context, accountID int64, messageID, category string) error
}

// Bulk mirrors internal/ui.BulkExecutor. Only the methods custom
// actions invoke are listed.
type Bulk interface {
	BulkPermanentDelete(ctx context.Context, accountID int64, messageIDs []string) error
	BulkMove(ctx context.Context, accountID int64, messageIDs []string, destFolderID, destAlias string) error
	BulkAddCategory(ctx context.Context, accountID int64, messageIDs []string, category string) error
}

// Thread mirrors internal/ui.ThreadExecutor + the conversation-IDs
// helper from store. Custom actions read both surfaces; one local
// interface keeps the executor wiring clean.
type Thread interface {
	ThreadAddCategory(ctx context.Context, accountID int64, focusedMessageID, category string) error
	ThreadRemoveCategory(ctx context.Context, accountID int64, focusedMessageID, category string) error
	ThreadArchive(ctx context.Context, accountID int64, focusedMessageID string) error
}

// Muter writes spec 19 mute state directly to the store (synchronous;
// not action-queue routed).
type Muter interface {
	MuteConversation(ctx context.Context, accountID int64, conversationID string) error
	UnmuteConversation(ctx context.Context, accountID int64, conversationID string) error
}

// RoutingWriter writes spec 23 routing destinations directly to the
// store (synchronous; not undoable via `u`).
type RoutingWriter interface {
	SetSenderRouting(ctx context.Context, accountID int64, address, destination string) (string, error)
}

// Unsubscriber mirrors the spec 16 unsubscribe surface.
type Unsubscriber interface {
	Resolve(ctx context.Context, messageID string) (UnsubAction, error)
	OneClickPOST(ctx context.Context, url string) error
}

// UnsubAction mirrors internal/ui.UnsubscribeAction. Method is
// "POST" for one-click, "URL" for browser, "MAILTO" for mailto: links.
type UnsubAction struct {
	Method string
	URL    string
	Mailto string
}

// FolderResolver maps a user-typed folder path (e.g. "Clients/TIAA")
// to a folder ID + alias, using the existing CLI helper. Wired by
// cmd_run.go.
type FolderResolver interface {
	Resolve(ctx context.Context, accountID int64, pathOrName string) (id, alias string, err error)
}

// Result summarises a Run() invocation. Steps is one row per step
// dispatched (or skipped) — the UI layer formats it into the §5.2
// toast.
type Result struct {
	ActionName string
	Steps      []StepResult
	// Continuation is non-nil when the run paused on a prompt_value;
	// the UI layer captures it and resumes via Resume() once the
	// modal returns. Nil on completion or full failure.
	Continuation *Continuation
}

// StepResult is one row of the result toast.
type StepResult struct {
	StepIndex   int
	Op          OpKind
	Status      StepStatus
	Message     string // human-readable; rendered in the toast
	NonUndoable bool   // true for set_sender_routing / set_thread_muted (§5.2)
}

// StepStatus discriminates the StepResult row glyph (✓ / ✗ / –).
type StepStatus int

// StepStatus values.
const (
	StepOK StepStatus = iota + 1
	StepFailed
	StepSkipped // already-flagged + flag, etc.
)

// Continuation carries the in-flight state across a prompt_value
// boundary. Internal to the package; the UI layer holds an opaque
// pointer and invokes Resume(continuation, userInput) when the
// modal returns Enter, or DropContinuation(continuation) on Esc.
type Continuation struct {
	Action    *Action
	Context   Context
	Steps     []resolvedStep // remaining (post-prompt) steps still to dispatch
	Prior     []StepResult   // already-applied step results (for the toast)
	PromptIdx int            // index of the pausing prompt_value step in Action.Steps
	deps      ExecDeps
}
