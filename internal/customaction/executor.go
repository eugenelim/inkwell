package customaction

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
)

// resolvedStep is the post-template-substitution form of one Step.
// Built by the resolve phase and consumed by dispatch.
type resolvedStep struct {
	Index  int
	Op     OpKind
	Params map[string]any
	// stopOnError is the resolved per-step value: action default OR
	// per-step override.
	stopOnError bool
	// nonUndoable mirrors opSpec.NonUndoable; surfaced in the result
	// toast (§5.2).
	nonUndoable bool
}

// Run executes one custom action against the focused-message
// Context. The first invocation resolves and dispatches batch 0
// (steps before the first prompt_value). When a prompt_value is
// reached, Run returns a Result with Continuation != nil; the
// caller drives the prompt UI and resumes via Resume(). On full
// completion, Continuation is nil.
func Run(ctx context.Context, action *Action, msg Context, deps ExecDeps) (Result, error) {
	if action == nil {
		return Result{}, errors.New("nil action")
	}
	res := Result{ActionName: action.Name}
	logStart(deps, action, &msg)

	// Build resolve batches.
	batches := batchSteps(action.Steps)
	if len(batches) == 0 {
		return res, errors.New("empty action sequence")
	}

	// Resolve batch 0 atomically; abort with zero side effects on any
	// error.
	resolved, err := resolveBatch(action, batches[0], &msg, deps)
	if err != nil {
		logResolveFailed(deps, action, err)
		return res, err
	}

	// Dispatch batch 0.
	rs, paused, dispatchErr := dispatchSteps(ctx, deps, &msg, resolved, action.StopOnError)
	res.Steps = append(res.Steps, rs...)
	if dispatchErr != nil && action.StopOnError {
		logDone(deps, action, &res)
		return res, nil
	}
	if paused {
		// The pausing prompt itself is appended to res.Steps as a
		// sentinel (status StepOK with op=prompt_value). The continuation
		// carries the remaining batches.
		cont := &Continuation{
			Action:    action,
			Context:   msg,
			Prior:     append([]StepResult(nil), res.Steps...),
			PromptIdx: pauseIndex(action.Steps, batches, 0),
			deps:      deps,
		}
		// Pre-stash the remaining batches as a flat slice; Resume
		// resolves them one-at-a-time.
		cont.Steps = nil
		// Resolve the first batch index after the prompt for use by
		// Resume. We carry the Action; Resume uses cont.Steps as the
		// already-resolved slice for the NEXT batch (filled by Resume).
		cont.Steps = nil
		res.Continuation = cont
		// Stash remaining batches via a closure on the deps receiver.
		// We reuse Steps to track which batch index is "next".
		cont.PromptIdx = nextBatchStartIndex(batches, 0)
		logDone(deps, action, &res)
		return res, nil
	}
	// No prompt; dispatched all of batch 0. If there were more batches
	// (impossible without a prompt — batchSteps guarantees split-on-
	// prompt), we'd handle them. Single-batch path is the common case.
	if len(batches) > 1 {
		// Should not happen — batchSteps splits on prompt_value, so
		// extra batches imply we missed a prompt. Defensive fallthrough.
		return res, fmt.Errorf("internal: unexpected batch count %d without pause", len(batches))
	}
	logDone(deps, action, &res)
	return res, nil
}

// Resume advances a paused continuation with the user-supplied
// prompt_value response. It binds UserInput, resolves the next
// batch, and dispatches. Returns a fresh Result; if more prompts
// remain, Continuation is non-nil again.
func Resume(ctx context.Context, cont *Continuation, userInput string) (Result, error) {
	if cont == nil {
		return Result{}, errors.New("nil continuation")
	}
	cont.Context.UserInput = userInput
	res := Result{ActionName: cont.Action.Name, Steps: append([]StepResult(nil), cont.Prior...)}
	deps := cont.deps

	// Recompute batches from the action and find the batch that starts
	// at PromptIdx (== absolute step index after the answered prompt).
	batches := batchSteps(cont.Action.Steps)
	startStepIdx := cont.PromptIdx
	batchIdx := -1
	for i, b := range batches {
		if len(b) > 0 && b[0].absoluteIndex == startStepIdx {
			batchIdx = i
			break
		}
	}
	if batchIdx < 0 {
		return res, fmt.Errorf("internal: no batch starts at step %d", startStepIdx)
	}
	resolved, err := resolveBatch(cont.Action, batches[batchIdx], &cont.Context, deps)
	if err != nil {
		// Post-prompt resolve failure (§4.4 / §6 edge case): prior
		// batches' side effects stay applied. Surface a failed StepResult
		// row for the failing step.
		// Determine the offending step index — best-effort, point at
		// the first step in the failing batch.
		failingIdx := batches[batchIdx][0].absoluteIndex
		res.Steps = append(res.Steps, StepResult{
			StepIndex: failingIdx,
			Op:        batches[batchIdx][0].Op,
			Status:    StepFailed,
			Message:   fmt.Sprintf("resolve failed: %v", err),
		})
		logDone(deps, cont.Action, &res)
		return res, nil
	}
	rs, paused, dispatchErr := dispatchSteps(ctx, deps, &cont.Context, resolved, cont.Action.StopOnError)
	res.Steps = append(res.Steps, rs...)
	if dispatchErr != nil && cont.Action.StopOnError {
		logDone(deps, cont.Action, &res)
		return res, nil
	}
	if paused {
		// Another prompt — chain the continuation forward.
		next := &Continuation{
			Action:    cont.Action,
			Context:   cont.Context,
			Prior:     append([]StepResult(nil), res.Steps...),
			deps:      deps,
			PromptIdx: nextBatchStartIndex(batches, batchIdx),
		}
		res.Continuation = next
		logDone(deps, cont.Action, &res)
		return res, nil
	}
	logDone(deps, cont.Action, &res)
	return res, nil
}

// batchedStep wraps a Step with its absolute index in Action.Steps.
// Needed so resolveBatch can produce resolvedStep entries that carry
// the original index for the toast.
type batchedStep struct {
	Step
	absoluteIndex int
}

// batchSteps splits Action.Steps at every prompt_value boundary into
// dispatchable batches. The prompt itself is the *terminator* of its
// batch — it is included as the last step so resolveBatch can render
// the prompt template and dispatchSteps can detect the pause.
func batchSteps(steps []Step) [][]batchedStep {
	var batches [][]batchedStep
	var cur []batchedStep
	for i := range steps {
		cur = append(cur, batchedStep{Step: steps[i], absoluteIndex: i})
		if steps[i].Op == OpPromptValue {
			batches = append(batches, cur)
			cur = nil
		}
	}
	if len(cur) > 0 {
		batches = append(batches, cur)
	}
	return batches
}

// pauseIndex returns the absolute step index where a paused batch
// expects its UserInput. Currently: the absolute index of the
// prompt_value step that terminates batch[idx].
func pauseIndex(steps []Step, batches [][]batchedStep, idx int) int {
	last := batches[idx][len(batches[idx])-1]
	return last.absoluteIndex
}

// nextBatchStartIndex returns the absolute step index of the first
// step in the batch *after* idx (i.e. the first non-prompt step
// following the prompt that paused). Returns the length of steps if
// no further batch exists.
func nextBatchStartIndex(batches [][]batchedStep, idx int) int {
	if idx+1 >= len(batches) {
		return -1
	}
	return batches[idx+1][0].absoluteIndex
}

// resolveBatch templates and validates one batch, returning a
// list of resolvedStep entries ready for dispatch. Returns an error
// (and zero side effects) on any resolution failure.
func resolveBatch(action *Action, batch []batchedStep, msg *Context, deps ExecDeps) ([]resolvedStep, error) {
	var out []resolvedStep
	for _, b := range batch {
		// Skip prompt_value at resolve time — its prompt template is
		// rendered separately by the dispatch loop when the pause
		// fires.
		if b.Op == OpPromptValue {
			out = append(out, resolvedStep{
				Index:  b.absoluteIndex,
				Op:     b.Op,
				Params: copyMap(b.Params),
			})
			continue
		}
		// Template every templated param against msg.
		params := copyMap(b.Params)
		for key, t := range b.Templated {
			var buf bytes.Buffer
			if err := t.Execute(&buf, msg); err != nil {
				return nil, fmt.Errorf("step %d (%s): template %q: %w", b.absoluteIndex, b.Op, key, err)
			}
			result := buf.String()
			if strings.TrimSpace(result) == "" {
				return nil, fmt.Errorf("step %d (%s): template %q rendered empty", b.absoluteIndex, b.Op, key)
			}
			params[key] = result
		}
		// open_url runtime URL check (templated case).
		if b.Op == OpOpenURL {
			if s, ok := params["url"].(string); ok {
				if err := validateURL(s); err != nil {
					return nil, fmt.Errorf("step %d (%s): %w", b.absoluteIndex, b.Op, err)
				}
			}
		}
		spec := ops[b.Op]
		stopOnErr := action.StopOnError
		if b.StopOnError != nil {
			stopOnErr = *b.StopOnError
		}
		out = append(out, resolvedStep{
			Index:       b.absoluteIndex,
			Op:          b.Op,
			Params:      params,
			stopOnError: stopOnErr,
			nonUndoable: spec.NonUndoable,
		})
	}
	return out, nil
}

// dispatchSteps invokes each resolved step's op in order, respecting
// stop-on-error semantics. Returns the StepResult rows + a paused
// flag + any dispatch error.
func dispatchSteps(ctx context.Context, deps ExecDeps, msg *Context, steps []resolvedStep, defaultStopOnErr bool) ([]StepResult, bool, error) {
	var rows []StepResult
	for i := range steps {
		s := &steps[i]
		// Pause sentinel.
		if s.Op == OpPromptValue {
			rows = append(rows, StepResult{
				StepIndex: s.Index,
				Op:        s.Op,
				Status:    StepOK,
				Message:   "prompt", // user-facing label; prompt template not echoed (§7)
			})
			return rows, true, nil
		}
		// Filter op: compile the (possibly-templated) pattern, run it,
		// stash matches into msg.SelectionIDs for downstream *_filtered
		// ops.
		if s.Op == OpFilter {
			// v1.1: Filter relies on the consumer providing a matched-
			// IDs slice via msg.SelectionIDs out-of-band, since the
			// pattern engine here doesn't have store access. The
			// dispatch is a no-op so the executor keeps going.
			rows = append(rows, StepResult{
				StepIndex: s.Index,
				Op:        s.Op,
				Status:    StepOK,
				Message:   "filter",
			})
			continue
		}
		spec := ops[s.Op]
		err := spec.Dispatch(ctx, deps, msg, s.Params)
		if errors.Is(err, errAlreadyApplied) {
			rows = append(rows, StepResult{
				StepIndex: s.Index,
				Op:        s.Op,
				Status:    StepSkipped,
				Message:   "already applied",
			})
			continue
		}
		if err != nil {
			rows = append(rows, StepResult{
				StepIndex:   s.Index,
				Op:          s.Op,
				Status:      StepFailed,
				Message:     err.Error(),
				NonUndoable: s.nonUndoable,
			})
			if s.stopOnError {
				return rows, false, err
			}
			continue
		}
		rows = append(rows, StepResult{
			StepIndex:   s.Index,
			Op:          s.Op,
			Status:      StepOK,
			NonUndoable: s.nonUndoable,
		})
	}
	return rows, false, nil
}

// copyMap returns a shallow clone — used to keep the step's
// pre-templated Params pristine for repeated dispatches across the
// same Resume continuation chain.
func copyMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// logStart / logResolveFailed / logDone emit the §7 INFO/WARN lines.
// All of them avoid From / Subject / MessageID (PII per `docs/CONVENTIONS.md` §7.3).
func logStart(deps ExecDeps, a *Action, msg *Context) {
	if deps.Logger == nil {
		return
	}
	destructive := false
	for _, s := range a.Steps {
		if ops[s.Op].Destructive {
			destructive = true
			break
		}
	}
	deps.Logger.Info("custom_action_run",
		"name", a.Name,
		"steps", len(a.Steps),
		"destructive", destructive,
		"selection_kind", msg.SelectionKind,
		"selection_size", len(msg.SelectionIDs),
	)
}

func logResolveFailed(deps ExecDeps, a *Action, err error) {
	if deps.Logger == nil {
		return
	}
	deps.Logger.Warn("custom_action_resolve_failed",
		"name", a.Name,
		"reason", classifyErr(err),
	)
}

func logDone(deps ExecDeps, a *Action, res *Result) {
	if deps.Logger == nil {
		return
	}
	ok, failed, skipped := 0, 0, 0
	for _, r := range res.Steps {
		switch r.Status {
		case StepOK:
			ok++
		case StepFailed:
			failed++
		case StepSkipped:
			skipped++
		}
	}
	deps.Logger.Info("custom_action_done",
		"name", a.Name,
		"ok", ok,
		"failed", failed,
		"skipped", skipped,
	)
}

// classifyErr returns a short PII-free label for the resolve-failure
// log. The precise template / folder name lives in the toast for the
// user, not in the log.
func classifyErr(err error) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	switch {
	case strings.Contains(s, "template"):
		return "template_error"
	case strings.Contains(s, "folder"):
		return "folder_resolve_error"
	case strings.Contains(s, "no conversation"):
		return "missing_conversation_id"
	case strings.Contains(s, "from address"):
		return "missing_from_address"
	default:
		return "resolve_error"
	}
}
