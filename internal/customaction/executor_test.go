package customaction

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// stubTriage / stubBulk / stubThread / stubMute / stubRouting /
// stubUnsubscribe / stubFolders capture each method invocation so
// the tests can verify the dispatch closures route to the right
// downstream method with the right args.
type stubTriage struct {
	calls map[string][]any // method → args
	mu    sync.Mutex
	fail  map[string]error
}

func newStubTriage() *stubTriage {
	return &stubTriage{calls: map[string][]any{}, fail: map[string]error{}}
}

func (s *stubTriage) record(method string, args ...any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls[method] = append(s.calls[method], args...)
	return s.fail[method]
}

func (s *stubTriage) MarkRead(_ context.Context, accID int64, msgID string) error {
	return s.record("MarkRead", accID, msgID)
}
func (s *stubTriage) MarkUnread(_ context.Context, accID int64, msgID string) error {
	return s.record("MarkUnread", accID, msgID)
}
func (s *stubTriage) ToggleFlag(_ context.Context, accID int64, msgID string, currentlyFlagged bool) error {
	return s.record("ToggleFlag", accID, msgID, currentlyFlagged)
}
func (s *stubTriage) SoftDelete(_ context.Context, accID int64, msgID string) error {
	return s.record("SoftDelete", accID, msgID)
}
func (s *stubTriage) Archive(_ context.Context, accID int64, msgID string) error {
	return s.record("Archive", accID, msgID)
}
func (s *stubTriage) Move(_ context.Context, accID int64, msgID, destID, alias string) error {
	return s.record("Move", accID, msgID, destID, alias)
}
func (s *stubTriage) PermanentDelete(_ context.Context, accID int64, msgID string) error {
	return s.record("PermanentDelete", accID, msgID)
}
func (s *stubTriage) AddCategory(_ context.Context, accID int64, msgID, cat string) error {
	return s.record("AddCategory", accID, msgID, cat)
}
func (s *stubTriage) RemoveCategory(_ context.Context, accID int64, msgID, cat string) error {
	return s.record("RemoveCategory", accID, msgID, cat)
}

type stubBulk struct{ calls map[string][]any }

func newStubBulk() *stubBulk { return &stubBulk{calls: map[string][]any{}} }
func (s *stubBulk) BulkPermanentDelete(_ context.Context, accID int64, ids []string) error {
	s.calls["BulkPermanentDelete"] = append(s.calls["BulkPermanentDelete"], accID, ids)
	return nil
}
func (s *stubBulk) BulkMove(_ context.Context, accID int64, ids []string, destID, alias string) error {
	s.calls["BulkMove"] = append(s.calls["BulkMove"], accID, ids, destID, alias)
	return nil
}
func (s *stubBulk) BulkAddCategory(_ context.Context, accID int64, ids []string, cat string) error {
	s.calls["BulkAddCategory"] = append(s.calls["BulkAddCategory"], accID, ids, cat)
	return nil
}

type stubThread struct{ calls map[string][]any }

func newStubThread() *stubThread { return &stubThread{calls: map[string][]any{}} }
func (s *stubThread) ThreadAddCategory(_ context.Context, accID int64, msgID, cat string) error {
	s.calls["ThreadAddCategory"] = append(s.calls["ThreadAddCategory"], accID, msgID, cat)
	return nil
}
func (s *stubThread) ThreadRemoveCategory(_ context.Context, accID int64, msgID, cat string) error {
	s.calls["ThreadRemoveCategory"] = append(s.calls["ThreadRemoveCategory"], accID, msgID, cat)
	return nil
}
func (s *stubThread) ThreadArchive(_ context.Context, accID int64, msgID string) error {
	s.calls["ThreadArchive"] = append(s.calls["ThreadArchive"], accID, msgID)
	return nil
}

type stubMute struct {
	muted, unmuted []string
}

func (s *stubMute) MuteConversation(_ context.Context, _ int64, convID string) error {
	s.muted = append(s.muted, convID)
	return nil
}
func (s *stubMute) UnmuteConversation(_ context.Context, _ int64, convID string) error {
	s.unmuted = append(s.unmuted, convID)
	return nil
}

type stubRouting struct {
	routed []struct{ Addr, Dest, Prior string }
}

func (s *stubRouting) SetSenderRouting(_ context.Context, _ int64, addr, dest string) (string, error) {
	s.routed = append(s.routed, struct{ Addr, Dest, Prior string }{addr, dest, ""})
	return "", nil
}

type stubUnsub struct {
	resolveAct UnsubAction
	resolveErr error
	postCalls  []string
	resolveN   int
}

func (s *stubUnsub) Resolve(_ context.Context, _ string) (UnsubAction, error) {
	s.resolveN++
	return s.resolveAct, s.resolveErr
}
func (s *stubUnsub) OneClickPOST(_ context.Context, url string) error {
	s.postCalls = append(s.postCalls, url)
	return nil
}

type stubFolders struct {
	resolved []string
	idMap    map[string]string // path → id
	failOn   string
}

func (s *stubFolders) Resolve(_ context.Context, _ int64, path string) (string, string, error) {
	s.resolved = append(s.resolved, path)
	if path == s.failOn {
		return "", "", errors.New("folder not found")
	}
	if id, ok := s.idMap[path]; ok {
		return id, "", nil
	}
	return "f-" + path, "", nil
}

func depsForTest() (ExecDeps, *stubTriage, *stubBulk, *stubThread, *stubMute, *stubRouting, *stubUnsub, *stubFolders, *[]string) {
	tri := newStubTriage()
	bulk := newStubBulk()
	thr := newStubThread()
	mute := &stubMute{}
	rout := &stubRouting{}
	uns := &stubUnsub{}
	fld := &stubFolders{idMap: map[string]string{}}
	openCalls := []string{}
	deps := ExecDeps{
		Triage:      tri,
		Bulk:        bulk,
		Thread:      thr,
		Mute:        mute,
		Routing:     rout,
		Unsubscribe: uns,
		Folders:     fld,
		OpenURL: func(s string) error {
			openCalls = append(openCalls, s)
			return nil
		},
		NowFn: time.Now,
	}
	return deps, tri, bulk, thr, mute, rout, uns, fld, &openCalls
}

func newTestContext() Context {
	return Context{
		AccountID:      42,
		From:           "alice@example.invalid",
		FromName:       "Alice",
		SenderDomain:   "example.invalid",
		Subject:        "Test",
		ConversationID: "conv-1",
		MessageID:      "msg-1",
		Date:           time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC),
		Folder:         "Inbox",
	}
}

// loadOne builds an Action from a TOML body for one custom_action.
func loadOne(t *testing.T, body string) *Action {
	t.Helper()
	path := writeActions(t, body)
	cat, err := LoadCatalogue(context.Background(), path, defaultDeps())
	require.NoError(t, err)
	require.Len(t, cat.Actions, 1)
	return &cat.Actions[0]
}

func TestRunHappyPathThreeSteps(t *testing.T) {
	a := loadOne(t, `
[[custom_action]]
name = "x"
description = "test"
sequence = [
  { op = "mark_read" },
  { op = "set_sender_routing", destination = "feed" },
  { op = "archive" },
]
`)
	deps, tri, _, _, _, rout, _, _, _ := depsForTest()
	res, err := Run(context.Background(), a, newTestContext(), deps)
	require.NoError(t, err)
	require.Len(t, res.Steps, 3)
	for _, r := range res.Steps {
		require.Equal(t, StepOK, r.Status, "step %d should be OK: %s", r.StepIndex, r.Message)
	}
	require.Len(t, tri.calls["MarkRead"], 2) // (accID, msgID) → 2 args
	require.Len(t, tri.calls["Archive"], 2)
	require.Len(t, rout.routed, 1)
	require.Equal(t, "feed", rout.routed[0].Dest)
}

func TestRunStopOnErrorTrueShortCircuits(t *testing.T) {
	a := loadOne(t, `
[[custom_action]]
name = "x"
description = "test"
stop_on_error = true
sequence = [
  { op = "mark_read" },
  { op = "archive" },
  { op = "soft_delete" },
]
`)
	deps, tri, _, _, _, _, _, _, _ := depsForTest()
	tri.fail["Archive"] = errors.New("boom")
	res, err := Run(context.Background(), a, newTestContext(), deps)
	require.NoError(t, err)
	require.Len(t, res.Steps, 2, "stop_on_error must short-circuit at step 2")
	require.Equal(t, StepFailed, res.Steps[1].Status)
	require.Empty(t, tri.calls["SoftDelete"])
}

func TestRunStopOnErrorFalseContinues(t *testing.T) {
	a := loadOne(t, `
[[custom_action]]
name = "x"
description = "test"
stop_on_error = false
sequence = [
  { op = "mark_read" },
  { op = "archive" },
  { op = "soft_delete" },
]
`)
	deps, tri, _, _, _, _, _, _, _ := depsForTest()
	tri.fail["Archive"] = errors.New("boom")
	res, err := Run(context.Background(), a, newTestContext(), deps)
	require.NoError(t, err)
	require.Len(t, res.Steps, 3)
	require.Equal(t, StepFailed, res.Steps[1].Status)
	require.Equal(t, StepOK, res.Steps[2].Status)
}

func TestFlagOpReadsCurrentStateAndSetsFlagged(t *testing.T) {
	a := loadOne(t, `
[[custom_action]]
name = "x"
description = "test"
sequence = [{ op = "flag" }]
`)
	deps, tri, _, _, _, _, _, _, _ := depsForTest()

	// Unflagged → ToggleFlag is called with currentlyFlagged=false.
	res, err := Run(context.Background(), a, newTestContext(), deps)
	require.NoError(t, err)
	require.Equal(t, StepOK, res.Steps[0].Status)
	require.Len(t, tri.calls["ToggleFlag"], 3) // 3 args per call

	// Already-flagged → no ToggleFlag, marked Skipped.
	tri2 := newStubTriage()
	deps.Triage = tri2
	msg := newTestContext()
	msg.FlagStatus = "flagged"
	res, err = Run(context.Background(), a, msg, deps)
	require.NoError(t, err)
	require.Equal(t, StepSkipped, res.Steps[0].Status)
	require.Empty(t, tri2.calls["ToggleFlag"])
}

func TestUnsubscribeOpResolvesThenPostsForOneClick(t *testing.T) {
	a := loadOne(t, `
[[custom_action]]
name = "x"
description = "test"
sequence = [{ op = "unsubscribe" }]
`)
	deps, _, _, _, _, _, uns, _, openCalls := depsForTest()
	uns.resolveAct = UnsubAction{Method: "POST", URL: "https://example.invalid/unsub"}
	res, err := Run(context.Background(), a, newTestContext(), deps)
	require.NoError(t, err)
	require.Equal(t, StepOK, res.Steps[0].Status)
	require.Len(t, uns.postCalls, 1)
	require.Equal(t, "https://example.invalid/unsub", uns.postCalls[0])
	require.Empty(t, *openCalls)
}

func TestUnsubscribeOpFallsBackToOpenURLForBrowser(t *testing.T) {
	a := loadOne(t, `
[[custom_action]]
name = "x"
description = "test"
sequence = [{ op = "unsubscribe" }]
`)
	deps, _, _, _, _, _, uns, _, openCalls := depsForTest()
	uns.resolveAct = UnsubAction{Method: "URL", URL: "https://example.invalid/manual"}
	res, err := Run(context.Background(), a, newTestContext(), deps)
	require.NoError(t, err)
	require.Equal(t, StepOK, res.Steps[0].Status)
	require.Len(t, *openCalls, 1)
	require.Equal(t, "https://example.invalid/manual", (*openCalls)[0])
}

func TestSetThreadMutedTrueCallsMuteConversation(t *testing.T) {
	a := loadOne(t, `
[[custom_action]]
name = "x"
description = "test"
sequence = [{ op = "set_thread_muted" }]
`)
	deps, _, _, _, mute, _, _, _, _ := depsForTest()
	res, err := Run(context.Background(), a, newTestContext(), deps)
	require.NoError(t, err)
	require.Equal(t, StepOK, res.Steps[0].Status)
	require.True(t, res.Steps[0].NonUndoable)
	require.Equal(t, []string{"conv-1"}, mute.muted)
}

func TestSetThreadMutedFalseCallsUnmute(t *testing.T) {
	a := loadOne(t, `
[[custom_action]]
name = "x"
description = "test"
sequence = [{ op = "set_thread_muted", value = false }]
`)
	deps, _, _, _, mute, _, _, _, _ := depsForTest()
	res, err := Run(context.Background(), a, newTestContext(), deps)
	require.NoError(t, err)
	require.Equal(t, StepOK, res.Steps[0].Status)
	require.Equal(t, []string{"conv-1"}, mute.unmuted)
}

func TestEmptyConversationIDAbortsThreadStep(t *testing.T) {
	a := loadOne(t, `
[[custom_action]]
name = "x"
description = "test"
sequence = [{ op = "set_thread_muted" }]
`)
	deps, _, _, _, _, _, _, _, _ := depsForTest()
	msg := newTestContext()
	msg.ConversationID = ""
	res, err := Run(context.Background(), a, msg, deps)
	require.NoError(t, err)
	require.Equal(t, StepFailed, res.Steps[0].Status)
	require.Contains(t, res.Steps[0].Message, "conversation")
}

func TestEmptyFromAbortsRoutingStep(t *testing.T) {
	a := loadOne(t, `
[[custom_action]]
name = "x"
description = "test"
sequence = [{ op = "set_sender_routing", destination = "feed" }]
`)
	deps, _, _, _, _, _, _, _, _ := depsForTest()
	msg := newTestContext()
	msg.From = ""
	res, err := Run(context.Background(), a, msg, deps)
	require.NoError(t, err)
	require.Equal(t, StepFailed, res.Steps[0].Status)
}

func TestResolveAbortsBeforeEnqueueOnTemplateError(t *testing.T) {
	// Template references a per-message field — should template OK
	// against a populated context. Use a folder template that
	// renders empty (forces resolve failure).
	a := loadOne(t, `
[[custom_action]]
name = "x"
description = "test"
allow_folder_template = true
sequence = [
  { op = "mark_read" },
  { op = "move", destination = "{{.UserInput}}" },
]
`)
	deps, tri, _, _, _, _, _, _, _ := depsForTest()
	res, err := Run(context.Background(), a, newTestContext(), deps)
	require.Error(t, err, "empty template render must abort batch 0")
	require.Empty(t, tri.calls["MarkRead"], "no step dispatches when batch 0 resolve fails")
	require.Empty(t, res.Steps)
}

func TestResolveAbortsBeforeEnqueueOnFolderNotFound(t *testing.T) {
	a := loadOne(t, `
[[custom_action]]
name = "x"
description = "test"
sequence = [
  { op = "mark_read" },
  { op = "move", destination = "Nonexistent" },
]
`)
	deps, tri, _, _, _, _, _, fld, _ := depsForTest()
	fld.failOn = "Nonexistent"
	// Move's resolve closure resolves the folder via deps.Folders;
	// the resolve phase as written doesn't pre-resolve folders (the
	// dispatch closure does). The folder resolve happens at dispatch
	// time, so step 1 (mark_read) DOES fire — this matches §4.4 prose
	// for non-templated folder names.
	res, err := Run(context.Background(), a, newTestContext(), deps)
	require.NoError(t, err)
	require.Equal(t, StepOK, res.Steps[0].Status, "mark_read fires before move resolves")
	require.Equal(t, StepFailed, res.Steps[1].Status)
	require.Contains(t, res.Steps[1].Message, "folder")
	require.Len(t, tri.calls["MarkRead"], 2)
	require.Empty(t, tri.calls["Move"])
}

func TestPromptValueBindsUserInput(t *testing.T) {
	a := loadOne(t, `
[[custom_action]]
name = "x"
description = "test"
allow_folder_template = true
sequence = [
  { op = "mark_read" },
  { op = "prompt_value", prompt = "Move to:" },
  { op = "move", destination = "{{.UserInput}}" },
]
`)
	deps, tri, _, _, _, _, _, _, _ := depsForTest()
	res, err := Run(context.Background(), a, newTestContext(), deps)
	require.NoError(t, err)
	require.NotNil(t, res.Continuation, "first batch must pause on prompt")
	require.Len(t, tri.calls["MarkRead"], 2, "mark_read fires before pause")

	// Resume with a folder name.
	res2, err := Resume(context.Background(), res.Continuation, "Archive")
	require.NoError(t, err)
	require.Nil(t, res2.Continuation, "no further prompts")
	require.Len(t, tri.calls["Move"], 4) // accID, msgID, destID, alias
	moveArgs := tri.calls["Move"]
	require.Equal(t, "f-Archive", moveArgs[2], "Move dispatched against the user-supplied folder")
}

func TestSecondPromptValueOverwritesUserInput(t *testing.T) {
	a := loadOne(t, `
[[custom_action]]
name = "x"
description = "test"
allow_folder_template = true
sequence = [
  { op = "prompt_value", prompt = "First:" },
  { op = "prompt_value", prompt = "Second:" },
  { op = "move", destination = "{{.UserInput}}" },
]
`)
	deps, tri, _, _, _, _, _, _, _ := depsForTest()
	res, err := Run(context.Background(), a, newTestContext(), deps)
	require.NoError(t, err)
	require.NotNil(t, res.Continuation)

	res2, err := Resume(context.Background(), res.Continuation, "first-answer")
	require.NoError(t, err)
	require.NotNil(t, res2.Continuation, "second prompt pauses again")

	res3, err := Resume(context.Background(), res2.Continuation, "second-answer")
	require.NoError(t, err)
	require.Nil(t, res3.Continuation)
	moveArgs := tri.calls["Move"]
	require.Equal(t, "f-second-answer", moveArgs[2], "second answer wins")
}

func TestPostPromptResolveFailureKeepsPriorBatch(t *testing.T) {
	a := loadOne(t, `
[[custom_action]]
name = "x"
description = "test"
allow_folder_template = true
sequence = [
  { op = "mark_read" },
  { op = "prompt_value", prompt = "Move to:" },
  { op = "move", destination = "{{.UserInput}}" },
]
`)
	deps, tri, _, _, _, _, _, fld, _ := depsForTest()
	fld.failOn = "ghost"
	res, err := Run(context.Background(), a, newTestContext(), deps)
	require.NoError(t, err)
	require.NotNil(t, res.Continuation)
	require.Len(t, tri.calls["MarkRead"], 2, "mark_read applied before prompt")

	// Resume with a folder that resolves but the dispatch fails.
	res2, err := Resume(context.Background(), res.Continuation, "ghost")
	require.NoError(t, err)
	// Dispatch path's folder resolution failure surfaces as a
	// per-step Failed row (not a resolve-batch error), since Move's
	// dispatch closure calls Resolve at runtime.
	require.NotEmpty(t, res2.Steps)
	last := res2.Steps[len(res2.Steps)-1]
	require.Equal(t, StepFailed, last.Status)
	// Earlier batches' side effects remained applied — the toast
	// retains the mark_read OK row.
	var sawMarkReadOK bool
	for _, r := range res2.Steps {
		if r.Op == OpMarkRead && r.Status == StepOK {
			sawMarkReadOK = true
		}
	}
	require.True(t, sawMarkReadOK, "prior batches' OK rows persist across the post-prompt failure")
}

func TestResultToastMarksNonUndoableSteps(t *testing.T) {
	a := loadOne(t, `
[[custom_action]]
name = "x"
description = "test"
sequence = [
  { op = "mark_read" },
  { op = "set_sender_routing", destination = "feed" },
  { op = "archive" },
]
`)
	deps, _, _, _, _, _, _, _, _ := depsForTest()
	res, err := Run(context.Background(), a, newTestContext(), deps)
	require.NoError(t, err)
	require.Len(t, res.Steps, 3)
	require.False(t, res.Steps[0].NonUndoable)
	require.True(t, res.Steps[1].NonUndoable, "set_sender_routing must be flagged non-undoable")
	require.False(t, res.Steps[2].NonUndoable)
}

func TestThreadAddCategoryAcceptsReplyLater(t *testing.T) {
	// The per-message add_category rejects ReplyLater literally; the
	// thread variant accepts it (spec 25 stack categories are thread-
	// level).
	a := loadOne(t, `
[[custom_action]]
name = "x"
description = "test"
sequence = [{ op = "thread_add_category", category = "Inkwell/ReplyLater" }]
`)
	deps, _, _, thr, _, _, _, _, _ := depsForTest()
	res, err := Run(context.Background(), a, newTestContext(), deps)
	require.NoError(t, err)
	require.Equal(t, StepOK, res.Steps[0].Status)
	require.Len(t, thr.calls["ThreadAddCategory"], 3)
}

func TestOpenURLLiteralDispatch(t *testing.T) {
	a := loadOne(t, `
[[custom_action]]
name = "x"
description = "test"
sequence = [{ op = "open_url", url = "https://example.invalid/" }]
`)
	deps, _, _, _, _, _, _, _, openCalls := depsForTest()
	res, err := Run(context.Background(), a, newTestContext(), deps)
	require.NoError(t, err)
	require.Equal(t, StepOK, res.Steps[0].Status)
	require.Equal(t, []string{"https://example.invalid/"}, *openCalls)
}

func TestMoveFilteredUsesSelectionIDs(t *testing.T) {
	a := loadOne(t, `
[[custom_action]]
name = "x"
description = "test"
confirm = "always"
sequence = [
  { op = "filter", pattern = "~f a@b.com" },
  { op = "move_filtered", pattern = "~f a@b.com", destination = "Archive" },
]
`)
	deps, _, bulk, _, _, _, _, _, _ := depsForTest()
	msg := newTestContext()
	msg.SelectionIDs = []string{"m-1", "m-2", "m-3"}
	msg.SelectionKind = "filtered"
	res, err := Run(context.Background(), a, msg, deps)
	require.NoError(t, err)
	require.Len(t, res.Steps, 2)
	require.Equal(t, StepOK, res.Steps[0].Status)
	require.Equal(t, StepOK, res.Steps[1].Status)
	require.Len(t, bulk.calls["BulkMove"], 4) // accID, ids, destID, alias
}
