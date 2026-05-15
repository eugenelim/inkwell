//go:build e2e && aifuzz

// AI exploratory fuzz harness — frame producer.
//
// This file does NOT call an LLM. It drives the real ui.Model with a
// curated fuzz alphabet of keystrokes and dumps the rendered frame after
// every action plus a unified diff against the previous frame. A Claude
// Code session reads the artifacts and acts as the oracle.
//
// How it fits together:
//
//   1. `scripts/ai-fuzz.sh` (or `make ai-fuzz`) runs this test.
//   2. Test writes to .context/ai-fuzz/run-<unix-ts>/:
//        - seed.txt          — PRNG seed (replay with INKWELL_FUZZ_SEED)
//        - actions.log       — one keystroke label per line
//        - frames/step-NN-<label>.txt  — ANSI-stripped framebuffer
//        - diffs/step-NN.diff          — `diff -u` vs previous frame
//        - REVIEW.md         — index + reviewer instructions
//   3. Claude Code Reads REVIEW.md and the per-step files, judges what
//      looks broken, reports back in chat.
//
// Knobs (env vars):
//   INKWELL_FUZZ_STEPS  - default 8
//   INKWELL_FUZZ_SEED   - default time-based
//
// Run with: go test -tags='e2e aifuzz' -run='^TestAIFuzzExplore$' -timeout=5m ./internal/ui

package ui

import (
	"bytes"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"
	"github.com/stretchr/testify/require"
)

// recorderModel wraps a tea.Model and stores the latest View() output in
// a shared slot so the test goroutine can read the current frame without
// racing the program's render loop.
type recorderModel struct {
	inner tea.Model
	slot  *atomic.Pointer[string]
}

func (r recorderModel) Init() tea.Cmd { return r.inner.Init() }
func (r recorderModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	newInner, cmd := r.inner.Update(msg)
	return recorderModel{inner: newInner, slot: r.slot}, cmd
}
func (r recorderModel) View() string {
	v := r.inner.View()
	s := v
	r.slot.Store(&s)
	return v
}

var ansiCSI = regexp.MustCompile("\x1b\\[[0-9;?]*[ -/]*[@-~]")

func stripANSI(s string) string { return ansiCSI.ReplaceAllString(s, "") }

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
func envInt64(key string, def int64) int64 {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	return def
}

type fuzzAction struct {
	label string
	msg   tea.Msg
}

func fuzzAlphabet() []fuzzAction {
	rune1 := func(label, r string) fuzzAction {
		return fuzzAction{label: label, msg: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(r)}}
	}
	key := func(label string, k tea.KeyType) fuzzAction {
		return fuzzAction{label: label, msg: tea.KeyMsg{Type: k}}
	}
	return []fuzzAction{
		rune1("focus-folders", "1"),
		rune1("focus-list", "2"),
		rune1("focus-viewer", "3"),
		rune1("nav-j", "j"),
		rune1("nav-k", "k"),
		rune1("nav-h", "h"),
		rune1("nav-l", "l"),
		rune1("expand-o", "o"),
		rune1("flag-f", "f"),
		rune1("archive-a", "a"),
		rune1("delete-d", "d"),
		rune1("undo-u", "u"),
		rune1("help-?", "?"),
		rune1("search-slash", "/"),
		rune1("cmd-colon", ":"),
		key("palette-ctrlk", tea.KeyCtrlK),
		rune1("fullscreen-z", "z"),
		rune1("calendar-c", "c"),
		rune1("settings-comma", ","),
		key("enter", tea.KeyEnter),
		key("esc", tea.KeyEsc),
		key("tab", tea.KeyTab),
		key("backspace", tea.KeyBackspace),
		key("ctrl-r", tea.KeyCtrlR),
		key("up", tea.KeyUp),
		key("down", tea.KeyDown),
		key("pgup", tea.KeyPgUp),
		key("pgdown", tea.KeyPgDown),
		key("home", tea.KeyHome),
		key("end", tea.KeyEnd),
		rune1("text-burst-a", "alice"),
		rune1("text-burst-num", "12345"),
		rune1("text-burst-sym", "@!#"),
		rune1("text-unicode", "日本"),
		{label: "resize-narrow", msg: tea.WindowSizeMsg{Width: 60, Height: 20}},
		{label: "resize-wide", msg: tea.WindowSizeMsg{Width: 200, Height: 50}},
		{label: "resize-tiny", msg: tea.WindowSizeMsg{Width: 30, Height: 10}},
		{label: "resize-default", msg: tea.WindowSizeMsg{Width: 120, Height: 30}},
	}
}

var unsafePath = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

func sanitize(s string) string { return unsafePath.ReplaceAllString(s, "_") }

// modalFingerprint matches Lipgloss rounded-corner glyphs used by the
// inkwell modal style (Help overlay, Settings, OOF, Confirm, etc.).
// When the previous frame contains one of these, the picker biases hard
// toward `esc` so the fuzz doesn't waste steps bouncing keys off a
// modal that swallows them. Found via the first smoke run, where 6 of
// 8 steps were no-ops because the help overlay opened on step 2.
var modalFingerprint = regexp.MustCompile(`[╭╰╮╯]`)

// pickAction biases toward `esc` with 50% probability when the previous
// frame looks like a modal; otherwise picks uniformly from the alphabet.
func pickAction(rng *rand.Rand, alphabet []fuzzAction, prevFrame string) fuzzAction {
	if modalFingerprint.MatchString(prevFrame) && rng.Float64() < 0.5 {
		for _, a := range alphabet {
			if a.label == "esc" {
				return a
			}
		}
	}
	return alphabet[rng.Intn(len(alphabet))]
}

// writeUnifiedDiff shells out to `diff -u` to produce a unified diff. If
// diff is unavailable, writes a placeholder rather than failing the test.
func writeUnifiedDiff(prevPath, newPath, outPath string) {
	cmd := exec.Command("diff", "-u", prevPath, newPath)
	out, _ := cmd.CombinedOutput()
	if len(out) == 0 {
		out = []byte("(no change vs previous frame)\n")
	}
	_ = os.WriteFile(outPath, out, 0o644)
}

func TestAIFuzzExplore(t *testing.T) {
	steps := envInt("INKWELL_FUZZ_STEPS", 8)
	seed := envInt64("INKWELL_FUZZ_SEED", time.Now().UnixNano())
	rng := rand.New(rand.NewSource(seed))

	// .context is the Conductor workspace-scratch directory (gitignored at
	// the workspace level). Resolve it to an absolute path from the repo
	// root so the run dir is easy to find from chat.
	repoRoot, err := repoRootDir()
	require.NoError(t, err)
	runDir := filepath.Join(repoRoot, ".context", "ai-fuzz", fmt.Sprintf("run-%d", time.Now().Unix()))
	framesDir := filepath.Join(runDir, "frames")
	diffsDir := filepath.Join(runDir, "diffs")
	require.NoError(t, os.MkdirAll(framesDir, 0o755))
	require.NoError(t, os.MkdirAll(diffsDir, 0o755))

	require.NoError(t, os.WriteFile(filepath.Join(runDir, "seed.txt"), []byte(strconv.FormatInt(seed, 10)+"\n"), 0o644))
	actionsLog, err := os.Create(filepath.Join(runDir, "actions.log"))
	require.NoError(t, err)
	defer actionsLog.Close()

	t.Logf("ai-fuzz run dir: %s", runDir)
	t.Logf("ai-fuzz seed: %d", seed)
	t.Logf("ai-fuzz steps: %d", steps)

	base, _ := newE2EModel(t)
	slot := &atomic.Pointer[string]{}
	rec := recorderModel{inner: base, slot: slot}
	tm := teatest.NewTestModel(t, rec, teatest.WithInitialTermSize(120, 30))

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return contains(string(out), "Inbox")
	}, teatest.WithDuration(2*time.Second))

	readFrame := func() string {
		deadline := time.Now().Add(200 * time.Millisecond)
		var last string
		for time.Now().Before(deadline) {
			if p := slot.Load(); p != nil {
				last = *p
			}
			time.Sleep(20 * time.Millisecond)
		}
		return stripANSI(last)
	}

	alphabet := fuzzAlphabet()

	initialPath := filepath.Join(framesDir, "step-00-initial.txt")
	prev := readFrame()
	require.NoError(t, os.WriteFile(initialPath, []byte(prev), 0o644))
	prevPath := initialPath

	type stepRow struct {
		Step      int
		Label     string
		FramePath string
		DiffPath  string
	}
	var rows []stepRow

	for i := 1; i <= steps; i++ {
		act := pickAction(rng, alphabet, prev)
		tm.Send(act.msg)
		fmt.Fprintln(actionsLog, act.label)

		frame := readFrame()
		framePath := filepath.Join(framesDir, fmt.Sprintf("step-%02d-%s.txt", i, sanitize(act.label)))
		require.NoError(t, os.WriteFile(framePath, []byte(frame), 0o644))

		diffPath := filepath.Join(diffsDir, fmt.Sprintf("step-%02d.diff", i))
		writeUnifiedDiff(prevPath, framePath, diffPath)

		rows = append(rows, stepRow{
			Step:      i,
			Label:     act.label,
			FramePath: relPath(runDir, framePath),
			DiffPath:  relPath(runDir, diffPath),
		})

		prev = frame
		prevPath = framePath
	}

	tm.Send(tea.QuitMsg{})
	tm.WaitFinished(t, teatest.WithFinalTimeout(3*time.Second))

	// Emit a Claude-Code-friendly review document.
	var b bytes.Buffer
	fmt.Fprintf(&b, "# ai-fuzz run\n\n")
	fmt.Fprintf(&b, "- seed: %d (replay with `INKWELL_FUZZ_SEED=%d scripts/ai-fuzz.sh %d`)\n", seed, seed, steps)
	fmt.Fprintf(&b, "- steps: %d\n", steps)
	fmt.Fprintf(&b, "- initial frame: `frames/step-00-initial.txt`\n\n")
	fmt.Fprintf(&b, "## Reviewer instructions (read this carefully)\n\n")
	fmt.Fprintf(&b, "You are the oracle. The harness above randomly drove keystrokes into\n")
	fmt.Fprintf(&b, "the real ui.Model and captured the rendered framebuffer after each\n")
	fmt.Fprintf(&b, "action. Your job:\n\n")
	fmt.Fprintf(&b, "1. Read the diffs in order. They are small; most steps will be ok.\n")
	fmt.Fprintf(&b, "2. Open the corresponding frame file ONLY when the diff suggests\n")
	fmt.Fprintf(&b, "   something looks wrong (or when you need surrounding context).\n")
	fmt.Fprintf(&b, "3. Flag anomalies — examples:\n")
	fmt.Fprintf(&b, "   - garbled / overlapping / truncated text\n")
	fmt.Fprintf(&b, "   - broken pane borders or box-drawing chars\n")
	fmt.Fprintf(&b, "   - a modal that renders empty or has no close affordance\n")
	fmt.Fprintf(&b, "   - stuck `loading…` with no progression\n")
	fmt.Fprintf(&b, "   - status-bar showing an unexpected error toast\n")
	fmt.Fprintf(&b, "   - focus marker (▌) disappearing entirely\n")
	fmt.Fprintf(&b, "   - layout collapses or content overflows the terminal box\n")
	fmt.Fprintf(&b, "   - any sign of panic / runtime error in the visible buffer\n")
	fmt.Fprintf(&b, "4. Do NOT flag: empty panes with no data, modals opening on `:` or\n")
	fmt.Fprintf(&b, "   `/`, cursor movement, `no message selected` placeholders, resize\n")
	fmt.Fprintf(&b, "   re-layouts, ignored keys when nothing is in focus.\n")
	fmt.Fprintf(&b, "5. Output a short findings list back in chat: step #, action,\n")
	fmt.Fprintf(&b, "   severity (low/med/high), one-sentence reasoning, and a pointer\n")
	fmt.Fprintf(&b, "   to the frame so the human can confirm.\n\n")
	fmt.Fprintf(&b, "## Steps\n\n")
	fmt.Fprintf(&b, "| # | action | frame | diff |\n|---|---|---|---|\n")
	for _, r := range rows {
		fmt.Fprintf(&b, "| %02d | `%s` | `%s` | `%s` |\n", r.Step, r.Label, r.FramePath, r.DiffPath)
	}
	require.NoError(t, os.WriteFile(filepath.Join(runDir, "REVIEW.md"), b.Bytes(), 0o644))

	t.Logf("ai-fuzz wrote %d steps; review with: cat %s/REVIEW.md", steps, runDir)
}

// repoRootDir resolves the repo root by walking up from CWD until it
// finds a go.mod. Used so frames land in <repo>/.context/ai-fuzz/...
// regardless of which subdir `go test` was invoked from.
func repoRootDir() (string, error) {
	d, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return d, nil
		}
		parent := filepath.Dir(d)
		if parent == d {
			return "", fmt.Errorf("go.mod not found from %s", d)
		}
		d = parent
	}
}

func relPath(base, target string) string {
	r, err := filepath.Rel(base, target)
	if err != nil {
		return target
	}
	return r
}
