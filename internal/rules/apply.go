package rules

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/eugenelim/inkwell/internal/customaction"
	"github.com/eugenelim/inkwell/internal/graph"
	"github.com/eugenelim/inkwell/internal/store"
)

// ApplyOptions tunes the apply pipeline behaviour.
type ApplyOptions struct {
	// DryRun produces the Diff without issuing any Graph writes.
	DryRun bool
	// Yes skips interactive confirmation prompts (for `--yes` /
	// non-interactive shells).
	Yes bool
	// ConfirmDestructive is the global belt-and-suspenders gate from
	// [rules].confirm_destructive. When true, every destructive
	// (delete=true) rule prompts regardless of its per-rule
	// `confirm` value. Per-rule `confirm = "never"` is already
	// rejected by the loader for destructive rules.
	ConfirmDestructive bool
	// Confirmer is invoked for each rule that needs a Y/N prompt
	// during a non-dry-run apply. Return false to skip that rule
	// (the apply marches on with the next entry; the skipped rule
	// counts as 'failed' for reporting). Nil → behaves like the
	// caller said No (defensive default).
	Confirmer func(d DiffEntry) bool
}

// ApplyResult is what Apply returns to its caller.
type ApplyResult struct {
	Diff    []DiffEntry
	Created int
	Updated int
	Deleted int
	Skipped int
	Failed  int
	Errors  []ApplyError
	Path    string // path of the (re-)written rules.toml on full success
}

// ApplyError carries a per-rule failure that did not stop the apply.
type ApplyError struct {
	RuleName string
	RuleID   string
	Op       DiffOp
	Err      error
}

func (e ApplyError) Error() string {
	if e.RuleID != "" {
		return fmt.Sprintf("rule %q (%s): %v", e.RuleName, e.RuleID, e.Err)
	}
	return fmt.Sprintf("rule %q: %v", e.RuleName, e.Err)
}

// Apply diffs the supplied catalogue against the local mirror,
// optionally prompts for destructive rules, and (unless DryRun)
// issues the corresponding Graph PATCH / POST / DELETE calls.
// Folder paths in move/copy actions are resolved via the store's
// folders cache; unresolved paths fail their own rule but allow
// other rules to proceed (per-rule sequential failure).
//
// On full success the local mirror is re-pulled and rules.toml is
// atomically rewritten with the canonical server state.
func Apply(ctx context.Context, gc GraphClient, s store.Store, accountID int64, cat *Catalogue, opts ApplyOptions) (*ApplyResult, error) {
	start := time.Now()
	// 1. Refresh the local mirror by pulling from Graph; narrows the
	// conflict window per spec 32 §5.4.
	res, err := Pull(ctx, gc, s, accountID, cat.Path)
	if err != nil {
		// On pre-flight pull failure the result is incomplete; don't
		// touch Graph.
		return nil, fmt.Errorf("apply: pre-flight pull: %w", err)
	}
	_ = res // diff classification reads from the mirror, not the pull return value

	// 2. Compute the diff against the freshly-pulled mirror.
	mirror, err := s.ListMessageRules(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("apply: load mirror: %w", err)
	}
	diff, err := computeDiff(ctx, s, accountID, cat.Rules, mirror)
	if err != nil {
		return nil, err
	}

	out := &ApplyResult{Diff: diff, Path: cat.Path}
	if opts.DryRun {
		return out, nil
	}

	// 3. Execute. Order: delete first, then create, then update — for
	// idempotency / debuggability, not server-side uniqueness.
	ordered := orderForApply(diff)
	for _, d := range ordered {
		if d.Op == DiffNoop || d.Op == DiffSkip {
			if d.Op == DiffSkip {
				out.Skipped++
			}
			continue
		}
		if d.IsDestructive && needsConfirm(d, opts) {
			if opts.Confirmer == nil || !opts.Confirmer(d) {
				out.Failed++
				out.Errors = append(out.Errors, ApplyError{
					RuleName: d.Rule.Name,
					RuleID:   d.Rule.ID,
					Op:       d.Op,
					Err:      errors.New("declined by user"),
				})
				continue
			}
		}
		if err := executeOne(ctx, gc, s, accountID, d); err != nil {
			out.Failed++
			out.Errors = append(out.Errors, ApplyError{
				RuleName: d.Rule.Name,
				RuleID:   d.Rule.ID,
				Op:       d.Op,
				Err:      err,
			})
			// Stop on first failure per spec 32 §6.5 step 6: surface
			// the partial-apply state; subsequent rules are
			// unprocessed.
			break
		}
		switch d.Op {
		case DiffCreate:
			out.Created++
		case DiffUpdate:
			out.Updated++
		case DiffDelete:
			out.Deleted++
		}
	}

	// 4. Re-pull and re-write rules.toml on full success so the file
	// reflects reality (matches Terraform's apply contract). On
	// partial success we still re-pull the mirror so callers can
	// inspect the actual state; we DO NOT overwrite rules.toml in
	// that case (the user's hand-edits stay intact).
	if out.Failed == 0 {
		if _, err := Pull(ctx, gc, s, accountID, cat.Path); err != nil {
			return out, fmt.Errorf("apply: post-write re-pull: %w", err)
		}
	} else {
		if _, err := Pull(ctx, gc, s, accountID, cat.Path+".sync"); err != nil {
			// Best-effort; don't shadow the partial-apply errors.
			_ = err
		}
	}

	// Spec 32 §12.1 — one INFO line summarising the apply at exit.
	// Counts only; predicate / display-name values are NOT logged
	// at this level (covered by §12.2).
	slog.Info("rules.apply",
		"account_id", accountID,
		"created", out.Created,
		"updated", out.Updated,
		"deleted", out.Deleted,
		"skipped", out.Skipped,
		"failed", out.Failed,
		"dry_run", opts.DryRun,
		"duration_ms", time.Since(start).Milliseconds(),
	)
	return out, nil
}

// computeDiff walks the TOML side and the mirror to classify each
// rule into create / update / noop / delete / skip.
func computeDiff(ctx context.Context, s store.Store, accountID int64, tomlRules []Rule, mirror []store.MessageRule) ([]DiffEntry, error) {
	mirrorByID := make(map[string]store.MessageRule, len(mirror))
	mirrorByName := make(map[string]store.MessageRule, len(mirror))
	for _, m := range mirror {
		mirrorByID[m.RuleID] = m
		mirrorByName[m.DisplayName] = m
	}

	matched := make(map[string]bool, len(mirror)) // mirror rule_id → matched by TOML
	out := make([]DiffEntry, 0, len(tomlRules)+len(mirror))

	for _, t := range tomlRules {
		// Resolve folder paths.
		resolved, retarget, err := resolveFolderTargets(ctx, s, accountID, t)
		if err != nil {
			out = append(out, DiffEntry{
				Op:      DiffCreate, // tentative — will be reported as error at apply-time
				Rule:    t,
				Warning: err.Error(),
			})
			continue
		}
		t = resolved
		// Match by ID first; fall back to name for ID-less.
		var prior *store.MessageRule
		switch {
		case t.ID != "":
			if m, ok := mirrorByID[t.ID]; ok {
				prior = &m
				matched[m.RuleID] = true
			}
		default:
			if m, ok := mirrorByName[t.Name]; ok {
				prior = &m
				matched[m.RuleID] = true
				t.ID = m.RuleID // adopt the server ID
			}
		}

		entry := DiffEntry{Rule: t, Prior: prior}
		// Skip read-only rules entirely.
		if prior != nil && prior.IsReadOnly {
			entry.Op = DiffSkip
			out = append(out, entry)
			continue
		}
		if prior == nil {
			entry.Op = DiffCreate
		} else if rulesEqual(t, *prior) {
			entry.Op = DiffNoop
		} else {
			entry.Op = DiffUpdate
		}
		entry.IsDestructive = t.Then.Delete != nil && *t.Then.Delete
		if retarget != "" {
			entry.Warning = retarget
		}
		out = append(out, entry)
	}

	// Mirror rows not matched → DELETE (unless read-only).
	for _, m := range mirror {
		if matched[m.RuleID] {
			continue
		}
		op := DiffDelete
		if m.IsReadOnly {
			op = DiffSkip
		}
		prior := m
		out = append(out, DiffEntry{
			Op:    op,
			Rule:  Rule{ID: m.RuleID, Name: m.DisplayName},
			Prior: &prior,
		})
	}
	return out, nil
}

// rulesEqual returns true when applying the TOML rule on top of the
// mirror would produce no observable server-side change. We compare
// the v1-catalogue fields via canonical JSON to avoid spurious
// updates from key-order differences.
func rulesEqual(t Rule, m store.MessageRule) bool {
	if t.Name != m.DisplayName {
		return false
	}
	if t.Sequence != m.Sequence {
		return false
	}
	if t.Enabled != m.IsEnabled {
		return false
	}
	tc, _ := graph.CanonicalJSON(t.When)
	mc, _ := graph.CanonicalJSON(m.Conditions)
	if string(tc) != string(mc) {
		return false
	}
	ta, _ := graph.CanonicalJSON(t.Then)
	ma, _ := graph.CanonicalJSON(m.Actions)
	if string(ta) != string(ma) {
		return false
	}
	te, _ := graph.CanonicalJSON(t.Except)
	me, _ := graph.CanonicalJSON(m.Exceptions)
	return string(te) == string(me)
}

// resolveFolderTargets replaces TOML-side folder slash-paths in
// move / copy actions with the resolved Graph folder ID. Returns
// the rewritten rule and an optional retarget warning when the
// resolved ID differs from the prior mirror's stored ID.
func resolveFolderTargets(ctx context.Context, s store.Store, accountID int64, t Rule) (Rule, string, error) {
	var warnings []string
	resolve := func(path string) (string, error) {
		if path == "" || !looksLikePath(path) {
			return path, nil
		}
		f, err := s.GetFolderByPath(ctx, accountID, path)
		if err != nil {
			return "", fmt.Errorf("folder %q not found: %w", path, err)
		}
		return f.ID, nil
	}
	if t.Then.MoveToFolder != "" {
		id, err := resolve(t.Then.MoveToFolder)
		if err != nil {
			return t, "", err
		}
		t.Then.MoveToFolder = id
	}
	if t.Then.CopyToFolder != "" {
		id, err := resolve(t.Then.CopyToFolder)
		if err != nil {
			return t, "", err
		}
		t.Then.CopyToFolder = id
	}
	return t, strings.Join(warnings, "; "), nil
}

// looksLikePath returns true when s looks like a slash-path (contains
// at least one `/`) rather than a bare Graph folder ID. Graph folder
// IDs are opaque base64-ish blobs and never contain `/`.
func looksLikePath(s string) bool {
	return strings.Contains(s, "/")
}

func orderForApply(diff []DiffEntry) []DiffEntry {
	out := make([]DiffEntry, 0, len(diff))
	for _, op := range []DiffOp{DiffDelete, DiffCreate, DiffUpdate, DiffNoop, DiffSkip} {
		for _, d := range diff {
			if d.Op == op {
				out = append(out, d)
			}
		}
	}
	// Sort each bucket by name for deterministic output.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Op != out[j].Op {
			return out[i].Op < out[j].Op
		}
		return out[i].Rule.Name < out[j].Rule.Name
	})
	return out
}

func needsConfirm(d DiffEntry, opts ApplyOptions) bool {
	if opts.Yes {
		return false
	}
	if !d.IsDestructive {
		// Non-destructive operations don't gate on confirm.
		return false
	}
	if opts.ConfirmDestructive {
		return true
	}
	// Per-rule confirm.
	return d.Rule.Confirm == customaction.ConfirmAlways
}

func executeOne(ctx context.Context, gc GraphClient, s store.Store, accountID int64, d DiffEntry) error {
	switch d.Op {
	case DiffCreate:
		gr := graph.MessageRule{
			DisplayName: d.Rule.Name,
			Sequence:    d.Rule.Sequence,
			IsEnabled:   d.Rule.Enabled,
			Conditions:  graphPredicatesFromStore(d.Rule.When),
			Actions:     graphActionsFromStore(d.Rule.Then),
		}
		if !isEmptyPredicates(d.Rule.Except) {
			gr.Exceptions = graphPredicatesFromStore(d.Rule.Except)
		}
		_, err := gc.CreateMessageRule(ctx, gr)
		return err
	case DiffUpdate:
		body, err := buildPatchBody(d)
		if err != nil {
			return err
		}
		_, err = gc.UpdateMessageRule(ctx, d.Rule.ID, body)
		return err
	case DiffDelete:
		return gc.DeleteMessageRule(ctx, d.Rule.ID)
	}
	return nil
}

// buildPatchBody constructs the PATCH JSON, merging the prior
// mirror's raw conditions / actions / exceptions with the v1-typed
// edit so non-v1 fields survive (spec 32 §5.3).
func buildPatchBody(d DiffEntry) (json.RawMessage, error) {
	editConditions, err := json.Marshal(graphPredicatesFromStore(d.Rule.When))
	if err != nil {
		return nil, err
	}
	editActions, err := json.Marshal(graphActionsFromStore(d.Rule.Then))
	if err != nil {
		return nil, err
	}
	editExceptions, err := json.Marshal(graphPredicatesFromStore(d.Rule.Except))
	if err != nil {
		return nil, err
	}

	var priorC, priorA, priorE json.RawMessage
	if d.Prior != nil {
		priorC = d.Prior.RawConditions
		priorA = d.Prior.RawActions
		priorE = d.Prior.RawExceptions
	}
	mergedC, err := graph.MergeRuleSubObject(priorC, editConditions)
	if err != nil {
		return nil, err
	}
	mergedA, err := graph.MergeRuleSubObject(priorA, editActions)
	if err != nil {
		return nil, err
	}
	mergedE, err := graph.MergeRuleSubObject(priorE, editExceptions)
	if err != nil {
		return nil, err
	}

	body := map[string]any{
		"displayName": d.Rule.Name,
		"sequence":    d.Rule.Sequence,
		"isEnabled":   d.Rule.Enabled,
		"conditions":  mergedC,
		"actions":     mergedA,
	}
	if !isEmptyPredicates(d.Rule.Except) {
		body["exceptions"] = mergedE
	}
	return json.Marshal(body)
}
