package ui

import (
	"context"
	"encoding/base64"
	"fmt"
	"os/exec"
	"runtime"
	"time"
)

// osc52Sequence builds the OSC 52 escape sequence that asks the
// terminal to write `data` to the system clipboard. Format:
//
//	\x1b]52;c;<base64-data>\x07
//
// Most modern terminals (iTerm2, WezTerm, Kitty, Ghostty, foot,
// Alacritty, Windows Terminal, recent VTE/GNOME Terminal) honour
// this. Apple Terminal.app does NOT — `pbcopyFallback` (only on
// darwin) covers that case.
//
// Spec source: xterm CSI control reference §"Operating System
// Commands", OSC 52 ("Manipulate Selection Data").
func osc52Sequence(data string) string {
	return "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte(data)) + "\x07"
}

// yank copies `data` to the user's clipboard. Returns nil on
// best-effort success — true detection isn't possible because the
// OSC 52 read-back form is widely disabled for security.
//
// The two-pronged approach:
//  1. Always emit OSC 52. This is the only path that works when the
//     user is connected over SSH — clipboard tools running on the
//     remote host can't reach the local clipboard.
//  2. On macOS, additionally pipe to `pbcopy` so users on
//     Apple Terminal (no OSC 52 support) still get the yank locally.
//
// On Linux/BSD without OSC 52 support, the user can configure their
// terminal's clipboard passthrough or run inside tmux with
// `set -g set-clipboard on`. Documented in the user reference.
//
// `osc52Writer` is the destination for the escape sequence; in
// production it's os.Stdout (Bubble Tea writes through the same
// FD), but tests inject a buffer.
type yanker struct {
	writeOSC52   func(string) error
	pbcopyDarwin bool // disable in tests so we don't fork a real process
}

func newYanker(writeOSC52 func(string) error) *yanker {
	return &yanker{
		writeOSC52:   writeOSC52,
		pbcopyDarwin: runtime.GOOS == "darwin",
	}
}

// Yank performs the copy. Returns the human-readable destination
// label ("OSC 52", "pbcopy", "OSC 52 + pbcopy") for the status bar.
// Errors from either path are joined and returned — but the
// non-error label still tells the user something happened, since a
// "no terminal supports OSC 52" failure is silent and we can't
// detect it anyway.
func (y *yanker) Yank(data string) (string, error) {
	if data == "" {
		return "", fmt.Errorf("yank: empty data")
	}
	var (
		emittedOSC bool
		emittedMac bool
		errs       []error
	)
	if y.writeOSC52 != nil {
		if err := y.writeOSC52(osc52Sequence(data)); err != nil {
			errs = append(errs, fmt.Errorf("osc52: %w", err))
		} else {
			emittedOSC = true
		}
	}
	if y.pbcopyDarwin {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		// #nosec G204 — pbcopy is a fixed system binary; data is
		// piped via stdin, never via argv. No injection vector.
		cmd := exec.CommandContext(ctx, "pbcopy")
		stdin, err := cmd.StdinPipe()
		if err != nil {
			errs = append(errs, fmt.Errorf("pbcopy stdin: %w", err))
		} else if err := cmd.Start(); err != nil {
			errs = append(errs, fmt.Errorf("pbcopy start: %w", err))
		} else {
			_, _ = stdin.Write([]byte(data))
			_ = stdin.Close()
			if err := cmd.Wait(); err != nil {
				errs = append(errs, fmt.Errorf("pbcopy wait: %w", err))
			} else {
				emittedMac = true
			}
		}
	}
	label := ""
	switch {
	case emittedOSC && emittedMac:
		label = "OSC 52 + pbcopy"
	case emittedOSC:
		label = "OSC 52"
	case emittedMac:
		label = "pbcopy"
	}
	if len(errs) > 0 && label == "" {
		return "", fmt.Errorf("yank failed: %v", errs)
	}
	// Non-fatal partial failure: prefer the success label and
	// swallow the per-path error (the user got SOMETHING on the
	// clipboard).
	return label, nil
}
