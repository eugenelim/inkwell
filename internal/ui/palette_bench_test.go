package ui

import (
	"fmt"
	"testing"
	"time"
)

// BenchmarkPaletteFilter measures per-keystroke filter+rerank cost
// across the standard 200-row mailbox shape and the 5000-row
// extreme. Spec 22 §6 budget: <2ms p95 at 200 rows; <15ms p95 at
// 5000 rows; the test fails if a single iteration exceeds 22ms
// (50% headroom per CLAUDE.md §6).
func BenchmarkPaletteFilter(b *testing.B) {
	for _, size := range []int{200, 5000} {
		b.Run(fmt.Sprintf("rows=%d", size), func(b *testing.B) {
			rows := generatePaletteFixture(size)
			caches := buildRowCaches(rows)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				start := time.Now()
				_ = matchAndScore(rows, caches, "arc", nil)
				if d := time.Since(start); d > 22*time.Millisecond {
					b.Fatalf("filter+rerank took %s on %d rows; budget is 22ms", d, size)
				}
			}
		})
	}
}

// BenchmarkPaletteOpen measures the cost of buildRowCaches over the
// same shapes. The Open path also runs collectPaletteRows, but that
// pulls Model state — measured via BenchmarkPaletteFilter as a
// proxy for the matcher hot loop.
func BenchmarkPaletteOpen(b *testing.B) {
	rows := generatePaletteFixture(200)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = buildRowCaches(rows)
	}
}

// generatePaletteFixture synthesises n PaletteRow entries: 80
// commands + (n-100) folders + 20 saved searches. No binary blobs
// are committed (spec 22 §6).
func generatePaletteFixture(n int) []PaletteRow {
	rows := make([]PaletteRow, 0, n)
	commandTitles := []string{
		"Archive message", "Delete message", "Mark read", "Mark unread",
		"Toggle flag", "Move to folder", "Add category", "Remove category",
		"Undo last action", "Unsubscribe", "Mute thread", "Archive thread",
		"Delete thread", "Mark thread read", "Reply", "Reply-all", "Forward",
		"New message", "Filter", "Filter (all folders)", "Clear filter",
		"Apply action to filtered", "Open URL", "Yank URL", "Fullscreen body",
		"Jump to folder", "New folder", "Rename folder", "Delete folder",
		"Search messages", "Backfill older messages", "Sync now",
		"Open calendar", "Out-of-office: turn on", "Out-of-office: turn off",
		"Out-of-office: schedule", "Mailbox settings",
		"Saved search: save current filter", "Saved search: list",
		"Saved search: edit", "Saved search: delete", "Sign in", "Sign out",
		"Help", "Quit",
	}
	for i := 0; i < 80 && i < n; i++ {
		title := commandTitles[i%len(commandTitles)]
		rows = append(rows, PaletteRow{
			ID:        fmt.Sprintf("cmd_%d", i),
			Title:     title,
			Section:   sectionCommands,
			Available: Availability{OK: true},
		})
	}
	folderCount := n - 80 - 20
	if folderCount < 0 {
		folderCount = 0
	}
	for i := 0; i < folderCount; i++ {
		rows = append(rows, PaletteRow{
			ID:        fmt.Sprintf("folder:%d", i),
			Title:     fmt.Sprintf("Inbox / Project / Q%d / Folder %d", i%4+1, i),
			Section:   sectionFolders,
			Available: Availability{OK: true},
		})
	}
	rest := n - len(rows)
	for i := 0; i < rest; i++ {
		rows = append(rows, PaletteRow{
			ID:        fmt.Sprintf("rule:%d", i),
			Title:     fmt.Sprintf("Saved search %d", i),
			Section:   sectionSavedSearches,
			Available: Availability{OK: true},
		})
	}
	return rows
}
