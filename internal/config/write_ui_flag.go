package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// WriteUIFlag updates a single boolean key in the [ui] section of the
// user's config TOML at the given path. Atomic via temp-file +
// rename; mode 0600. Preserves other sections, comments, and key
// order verbatim.
//
// Specs 11 §5.4, 23 §5.9, and 28 §5.3.1 / §5.3.2 reference an "auto-
// write pattern" for one-shot UI flags (hint dismissals, gate-flip
// markers). Spec 28 is the first whose correctness depends on the
// writer existing — without it, the marker resets every launch and
// the §5.3.1 modal re-fires forever. This is the minimal viable
// writer scoped to bool keys under [ui]; non-bool / non-[ui] keys
// fall outside its remit.
//
// If the file is missing the function creates it with the single
// `[ui]` section containing the key. If the file exists but has no
// `[ui]` section, it appends a `[ui]\n<key> = <value>` block at the
// end. If the file exists and has the section but not the key, the
// key is appended as the last line of the section. If the file has
// both, the existing line is rewritten in place.
//
// Caller is responsible for path resolution (~ expansion etc.) —
// the config loader's `defaultConfigPath` is the canonical source.
func WriteUIFlag(path, key string, value bool) error {
	if key == "" {
		return fmt.Errorf("config.WriteUIFlag: key must not be empty")
	}
	if strings.ContainsAny(key, " \t\n[]\"'=#") {
		return fmt.Errorf("config.WriteUIFlag: invalid key %q", key)
	}
	if path == "" {
		return fmt.Errorf("config.WriteUIFlag: path must not be empty")
	}

	val := strconv.FormatBool(value)

	// Read existing content. Missing file → write a fresh [ui] block.
	// #nosec G304 — path is the user's config.toml (rc.configPath).
	// Same trust model as the config loader's os.Open: single-user
	// desktop tool, the user owns the path.
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("config.WriteUIFlag: read %s: %w", path, err)
		}
		fresh := fmt.Sprintf("[ui]\n%s = %s\n", key, val)
		return atomicWriteFile(path, []byte(fresh), 0o600)
	}

	updated, changed := rewriteUIFlag(string(data), key, val)
	if !changed {
		// Nothing to do; the file already carries the desired value.
		return nil
	}
	return atomicWriteFile(path, []byte(updated), 0o600)
}

// rewriteUIFlag returns (newContent, changed). It scans for the
// `[ui]` section header and either rewrites an existing `<key> = …`
// line inside it, appends the key as the last line of the existing
// section, or appends a new `[ui]` block at end-of-file.
func rewriteUIFlag(content, key, val string) (string, bool) {
	scanner := bufio.NewScanner(strings.NewReader(content))
	// Allow long lines (TOML multi-line strings, etc.); 1MB cap.
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)

	var (
		out          strings.Builder
		inUI         bool
		uiSeen       bool
		uiEndIdx     = -1 // line index where the [ui] section ends
		lines        []string
		keyLineIdx   = -1 // line index of an existing `<key> = …` inside [ui]
		desiredLine  = key + " = " + val
		lineHasValue = func(s string) bool {
			t := strings.TrimSpace(s)
			if !strings.HasPrefix(t, key) {
				return false
			}
			rest := strings.TrimSpace(t[len(key):])
			return strings.HasPrefix(rest, "=")
		}
	)
	_ = out

	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			if inUI {
				inUI = false
				uiEndIdx = i // section ended at the next header
			}
			if trimmed == "[ui]" {
				inUI = true
				uiSeen = true
			}
			continue
		}
		if inUI && lineHasValue(line) {
			keyLineIdx = i
		}
	}
	// If the [ui] section was open at EOF, its end is len(lines).
	if inUI && uiEndIdx == -1 {
		uiEndIdx = len(lines)
	}

	switch {
	case keyLineIdx >= 0:
		// Rewrite the existing line. Preserve leading whitespace.
		raw := lines[keyLineIdx]
		ws := raw[:len(raw)-len(strings.TrimLeft(raw, " \t"))]
		newLine := ws + desiredLine
		if newLine == raw {
			return content, false
		}
		lines[keyLineIdx] = newLine
	case uiSeen:
		// Insert just before the section's end (which is either the
		// next-header line or len(lines)).
		insertAt := uiEndIdx
		// Trim any trailing blank lines inside the [ui] section so
		// the new key appears with the rest, not after a gap.
		for insertAt > 0 && strings.TrimSpace(lines[insertAt-1]) == "" {
			insertAt--
		}
		newLines := make([]string, 0, len(lines)+1)
		newLines = append(newLines, lines[:insertAt]...)
		newLines = append(newLines, desiredLine)
		newLines = append(newLines, lines[insertAt:]...)
		lines = newLines
	default:
		// No [ui] section: append a fresh block.
		if len(lines) > 0 && lines[len(lines)-1] != "" {
			lines = append(lines, "")
		}
		lines = append(lines, "[ui]", desiredLine)
	}

	return joinLinesPreservingTrailingNewline(lines, content), true
}

// joinLinesPreservingTrailingNewline rejoins the line slice and
// preserves whether the original content ended in a newline.
func joinLinesPreservingTrailingNewline(lines []string, original string) string {
	out := strings.Join(lines, "\n")
	if strings.HasSuffix(original, "\n") || original == "" {
		out += "\n"
	}
	return out
}

// atomicWriteFile writes data to path via a sibling temp file +
// rename so a crash mid-write cannot leave a half-formed config
// file. Mode is honoured on the final file via fchmod before
// rename; Go's os.CreateTemp creates with 0600 by default but we
// re-chmod for clarity.
func atomicWriteFile(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if dir == "" {
		dir = "."
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("config.WriteUIFlag: mkdir %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".inkwell-config-*.tmp")
	if err != nil {
		return fmt.Errorf("config.WriteUIFlag: temp file: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpPath) }
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("config.WriteUIFlag: write temp: %w", err)
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("config.WriteUIFlag: chmod temp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("config.WriteUIFlag: fsync temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("config.WriteUIFlag: close temp: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		cleanup()
		return fmt.Errorf("config.WriteUIFlag: rename %s → %s: %w", tmpPath, path, err)
	}
	return nil
}
