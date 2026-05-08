package customaction

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// opSpec is one row in the package-level op registration table. Each
// op carries its load-time validator, dispatch closure, and bits used
// by the executor (Destructive, NeedsBulk).
type opSpec struct {
	Name        OpKind
	Destructive bool
	NeedsBulk   bool
	NonUndoable bool // true for set_sender_routing / set_thread_muted (§5.2)
	// requiresFocusedMessage is consulted by the palette availability
	// check + CLI --filter rejection. true for ops that need a single
	// focused-message context (everything except *_filtered + filter).
	requiresFocusedMessage bool
	// Validate is the load-time param check. raw is the populated
	// step.Params after the loader's per-key projection.
	Validate func(raw map[string]any) error
	// Dispatch executes one resolved step at runtime. msg is the
	// snapshot Context built by the executor's resolve phase; params
	// is the post-template-substitution map.
	Dispatch func(ctx context.Context, deps ExecDeps, msg *Context, params map[string]any) error
}

// validRoutingDestinations are the four spec 23 stream values.
var validRoutingDestinations = map[string]struct{}{
	"imbox":       {},
	"feed":        {},
	"paper_trail": {},
	"screener":    {},
}

// requireString verifies key is present, a non-empty string, and
// returns the value.
func requireString(raw map[string]any, key, op string) (string, error) {
	v, ok := raw[key]
	if !ok {
		return "", fmt.Errorf("op %q: %q is required", op, key)
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("op %q: %q must be a string", op, key)
	}
	if strings.TrimSpace(s) == "" {
		return "", fmt.Errorf("op %q: %q must be non-empty", op, key)
	}
	return s, nil
}

// noParams is the validator for ops that take no params.
func noParams(raw map[string]any) error {
	if len(raw) > 0 {
		return fmt.Errorf("no params expected; got %d", len(raw))
	}
	return nil
}

// ops is the package-level registration table for the 22 v1.1 ops.
// Spec 27 §4.5 mandates a `var` literal — no init().
var ops = map[OpKind]opSpec{
	OpMarkRead: {
		Name:                   OpMarkRead,
		Validate:               noParams,
		requiresFocusedMessage: true,
		Dispatch: func(ctx context.Context, deps ExecDeps, msg *Context, _ map[string]any) error {
			return deps.Triage.MarkRead(ctx, msg.AccountID, msg.MessageID)
		},
	},
	OpMarkUnread: {
		Name:                   OpMarkUnread,
		Validate:               noParams,
		requiresFocusedMessage: true,
		Dispatch: func(ctx context.Context, deps ExecDeps, msg *Context, _ map[string]any) error {
			return deps.Triage.MarkUnread(ctx, msg.AccountID, msg.MessageID)
		},
	},
	OpFlag: {
		Name:                   OpFlag,
		Validate:               noParams,
		requiresFocusedMessage: true,
		Dispatch: func(ctx context.Context, deps ExecDeps, msg *Context, _ map[string]any) error {
			if msg.FlagStatus == "flagged" {
				return errAlreadyApplied
			}
			return deps.Triage.ToggleFlag(ctx, msg.AccountID, msg.MessageID, false)
		},
	},
	OpUnflag: {
		Name:                   OpUnflag,
		Validate:               noParams,
		requiresFocusedMessage: true,
		Dispatch: func(ctx context.Context, deps ExecDeps, msg *Context, _ map[string]any) error {
			if msg.FlagStatus != "flagged" {
				return errAlreadyApplied
			}
			return deps.Triage.ToggleFlag(ctx, msg.AccountID, msg.MessageID, true)
		},
	},
	OpArchive: {
		Name:                   OpArchive,
		Validate:               noParams,
		requiresFocusedMessage: true,
		Dispatch: func(ctx context.Context, deps ExecDeps, msg *Context, _ map[string]any) error {
			return deps.Triage.Archive(ctx, msg.AccountID, msg.MessageID)
		},
	},
	OpSoftDelete: {
		Name:                   OpSoftDelete,
		Validate:               noParams,
		requiresFocusedMessage: true,
		Dispatch: func(ctx context.Context, deps ExecDeps, msg *Context, _ map[string]any) error {
			return deps.Triage.SoftDelete(ctx, msg.AccountID, msg.MessageID)
		},
	},
	OpPermanentDelete: {
		Name:                   OpPermanentDelete,
		Destructive:            true,
		Validate:               noParams,
		requiresFocusedMessage: true,
		Dispatch: func(ctx context.Context, deps ExecDeps, msg *Context, _ map[string]any) error {
			return deps.Triage.PermanentDelete(ctx, msg.AccountID, msg.MessageID)
		},
	},
	OpMove: {
		Name:                   OpMove,
		requiresFocusedMessage: true,
		Validate: func(raw map[string]any) error {
			_, err := requireString(raw, "destination", "move")
			return err
		},
		Dispatch: func(ctx context.Context, deps ExecDeps, msg *Context, params map[string]any) error {
			dest, _ := params["destination"].(string)
			id, alias, err := deps.Folders.Resolve(ctx, msg.AccountID, dest)
			if err != nil {
				return fmt.Errorf("folder %q: %w", dest, err)
			}
			return deps.Triage.Move(ctx, msg.AccountID, msg.MessageID, id, alias)
		},
	},
	OpAddCategory: {
		Name:                   OpAddCategory,
		requiresFocusedMessage: true,
		Validate: func(raw map[string]any) error {
			_, err := requireString(raw, "category", "add_category")
			return err
		},
		Dispatch: func(ctx context.Context, deps ExecDeps, msg *Context, params map[string]any) error {
			cat, _ := params["category"].(string)
			return deps.Triage.AddCategory(ctx, msg.AccountID, msg.MessageID, cat)
		},
	},
	OpRemoveCategory: {
		Name:                   OpRemoveCategory,
		requiresFocusedMessage: true,
		Validate: func(raw map[string]any) error {
			_, err := requireString(raw, "category", "remove_category")
			return err
		},
		Dispatch: func(ctx context.Context, deps ExecDeps, msg *Context, params map[string]any) error {
			cat, _ := params["category"].(string)
			return deps.Triage.RemoveCategory(ctx, msg.AccountID, msg.MessageID, cat)
		},
	},
	OpSetSenderRouting: {
		Name:                   OpSetSenderRouting,
		NonUndoable:            true,
		requiresFocusedMessage: true,
		Validate: func(raw map[string]any) error {
			dest, err := requireString(raw, "destination", "set_sender_routing")
			if err != nil {
				return err
			}
			if strings.Contains(dest, "{{") {
				return fmt.Errorf("destination must be a literal — templates not allowed")
			}
			if _, ok := validRoutingDestinations[dest]; !ok {
				return fmt.Errorf("destination %q must be one of imbox|feed|paper_trail|screener", dest)
			}
			return nil
		},
		Dispatch: func(ctx context.Context, deps ExecDeps, msg *Context, params map[string]any) error {
			dest, _ := params["destination"].(string)
			if strings.TrimSpace(msg.From) == "" {
				return errors.New("from address missing")
			}
			_, err := deps.Routing.SetSenderRouting(ctx, msg.AccountID, msg.From, dest)
			return err
		},
	},
	OpSetThreadMuted: {
		Name:                   OpSetThreadMuted,
		NonUndoable:            true,
		requiresFocusedMessage: true,
		Validate: func(raw map[string]any) error {
			if v, ok := raw["value"]; ok {
				if _, isBool := v.(bool); !isBool {
					return fmt.Errorf("value must be a bool")
				}
			}
			return nil
		},
		Dispatch: func(ctx context.Context, deps ExecDeps, msg *Context, params map[string]any) error {
			if msg.ConversationID == "" {
				return errors.New("no conversation ID")
			}
			value := true
			if v, ok := params["value"].(bool); ok {
				value = v
			}
			if value {
				return deps.Mute.MuteConversation(ctx, msg.AccountID, msg.ConversationID)
			}
			return deps.Mute.UnmuteConversation(ctx, msg.AccountID, msg.ConversationID)
		},
	},
	OpThreadAddCategory: {
		Name:                   OpThreadAddCategory,
		requiresFocusedMessage: true,
		Validate: func(raw map[string]any) error {
			_, err := requireString(raw, "category", "thread_add_category")
			return err
		},
		Dispatch: func(ctx context.Context, deps ExecDeps, msg *Context, params map[string]any) error {
			cat, _ := params["category"].(string)
			return deps.Thread.ThreadAddCategory(ctx, msg.AccountID, msg.MessageID, cat)
		},
	},
	OpThreadRemoveCategory: {
		Name:                   OpThreadRemoveCategory,
		requiresFocusedMessage: true,
		Validate: func(raw map[string]any) error {
			_, err := requireString(raw, "category", "thread_remove_category")
			return err
		},
		Dispatch: func(ctx context.Context, deps ExecDeps, msg *Context, params map[string]any) error {
			cat, _ := params["category"].(string)
			return deps.Thread.ThreadRemoveCategory(ctx, msg.AccountID, msg.MessageID, cat)
		},
	},
	OpThreadArchive: {
		Name:                   OpThreadArchive,
		Validate:               noParams,
		requiresFocusedMessage: true,
		Dispatch: func(ctx context.Context, deps ExecDeps, msg *Context, _ map[string]any) error {
			return deps.Thread.ThreadArchive(ctx, msg.AccountID, msg.MessageID)
		},
	},
	OpUnsubscribe: {
		Name:                   OpUnsubscribe,
		Validate:               noParams,
		requiresFocusedMessage: true,
		Dispatch: func(ctx context.Context, deps ExecDeps, msg *Context, _ map[string]any) error {
			if deps.Unsubscribe == nil {
				return errors.New("unsubscribe service not wired")
			}
			act, err := deps.Unsubscribe.Resolve(ctx, msg.MessageID)
			if err != nil {
				return err
			}
			switch strings.ToUpper(act.Method) {
			case "POST":
				return deps.Unsubscribe.OneClickPOST(ctx, act.URL)
			case "URL":
				if deps.OpenURL == nil {
					return errors.New("open_url helper not wired")
				}
				return deps.OpenURL(act.URL)
			case "MAILTO":
				if deps.OpenURL == nil {
					return errors.New("open_url helper not wired")
				}
				return deps.OpenURL(act.Mailto)
			default:
				return fmt.Errorf("unsubscribe: no actionable URL")
			}
		},
	},
	OpFilter: {
		Name: OpFilter,
		Validate: func(raw map[string]any) error {
			_, err := requireString(raw, "pattern", "filter")
			return err
		},
		Dispatch: func(ctx context.Context, deps ExecDeps, msg *Context, params map[string]any) error {
			// Filter is a no-op at the dispatcher level — its result
			// (the matched IDs) is captured into msg.SelectionIDs by
			// the executor before downstream *_filtered ops dispatch.
			return nil
		},
	},
	OpMoveFiltered: {
		NeedsBulk: true,
		Name:      OpMoveFiltered,
		Validate: func(raw map[string]any) error {
			if _, err := requireString(raw, "pattern", "move_filtered"); err != nil {
				return err
			}
			_, err := requireString(raw, "destination", "move_filtered")
			return err
		},
		Dispatch: func(ctx context.Context, deps ExecDeps, msg *Context, params map[string]any) error {
			dest, _ := params["destination"].(string)
			id, alias, err := deps.Folders.Resolve(ctx, msg.AccountID, dest)
			if err != nil {
				return fmt.Errorf("folder %q: %w", dest, err)
			}
			ids := msg.SelectionIDs
			if len(ids) == 0 {
				return errors.New("no messages matched filter")
			}
			return deps.Bulk.BulkMove(ctx, msg.AccountID, ids, id, alias)
		},
	},
	OpPermanentDeleteFiltered: {
		NeedsBulk:   true,
		Destructive: true,
		Name:        OpPermanentDeleteFiltered,
		Validate: func(raw map[string]any) error {
			_, err := requireString(raw, "pattern", "permanent_delete_filtered")
			return err
		},
		Dispatch: func(ctx context.Context, deps ExecDeps, msg *Context, params map[string]any) error {
			ids := msg.SelectionIDs
			if len(ids) == 0 {
				return errors.New("no messages matched filter")
			}
			return deps.Bulk.BulkPermanentDelete(ctx, msg.AccountID, ids)
		},
	},
	OpPromptValue: {
		Name: OpPromptValue,
		Validate: func(raw map[string]any) error {
			_, err := requireString(raw, "prompt", "prompt_value")
			return err
		},
		// Dispatch for prompt_value is special — the executor handles
		// it directly (pauses + creates a Continuation). The closure
		// is never called; nil-checks in executor.go guard us.
		Dispatch: nil,
	},
	OpAdvanceCursor: {
		Name:     OpAdvanceCursor,
		Validate: noParams,
		// Pure-UI; executor pushes a sentinel StepResult and the UI
		// layer advances the cursor when it sees it.
		Dispatch: func(ctx context.Context, deps ExecDeps, msg *Context, _ map[string]any) error {
			return nil
		},
	},
	OpOpenURL: {
		Name: OpOpenURL,
		Validate: func(raw map[string]any) error {
			s, err := requireString(raw, "url", "open_url")
			if err != nil {
				return err
			}
			if strings.Contains(s, "{{") {
				// Templated URL — runtime check after substitution.
				return nil
			}
			return validateURL(s)
		},
		Dispatch: func(ctx context.Context, deps ExecDeps, msg *Context, params map[string]any) error {
			s, _ := params["url"].(string)
			if err := validateURL(s); err != nil {
				return err
			}
			if deps.OpenURL == nil {
				return errors.New("open_url helper not wired")
			}
			return deps.OpenURL(s)
		},
	},
}

// errAlreadyApplied is the sentinel returned by flag/unflag when the
// op is a no-op for the current state. The executor maps it to a
// StepSkipped row in the result toast (§3.5 row 3, §6 edge case).
var errAlreadyApplied = errors.New("already applied")

// validateURL enforces http(s) only — the §4.3 rule. mailto: URLs
// flow through the unsubscribe op, not open_url.
func validateURL(s string) error {
	u, err := url.Parse(s)
	if err != nil {
		return fmt.Errorf("invalid URL %q: %w", s, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("URL %q must use http or https", s)
	}
	if u.Host == "" {
		return fmt.Errorf("URL %q has no host", s)
	}
	return nil
}
