package customaction

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// BenchmarkLoadCatalogue50Actions stresses the typical-power-user
// catalogue size: 50 actions, average 4 steps each (≈200 steps total).
// Spec 27 §8 budget: ≤30ms p95, ≤45ms gate.
func BenchmarkLoadCatalogue50Actions(b *testing.B) {
	path := writeBenchActions(b, 50, 4)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := LoadCatalogue(context.Background(), path, defaultDeps())
		if err != nil {
			b.Fatal(err)
		}
	}
	avgMs := float64(b.Elapsed().Microseconds()) / float64(b.N) / 1000.0
	if avgMs > 45 {
		b.Errorf("BenchmarkLoadCatalogue50Actions: avg %.2fms exceeds 45ms gate", avgMs)
	}
}

// BenchmarkLoadCatalogueAtCap exercises the spec 27 §3.1 cap:
// 256 actions × 32 steps = 8192 steps. Budget ≤500ms p95, ≤750ms gate.
func BenchmarkLoadCatalogueAtCap(b *testing.B) {
	path := writeBenchActions(b, 256, 32)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := LoadCatalogue(context.Background(), path, defaultDeps())
		if err != nil {
			b.Fatal(err)
		}
	}
	avgMs := float64(b.Elapsed().Microseconds()) / float64(b.N) / 1000.0
	if avgMs > 750 {
		b.Errorf("BenchmarkLoadCatalogueAtCap: avg %.2fms exceeds 750ms gate", avgMs)
	}
}

// BenchmarkResolveAction measures the resolve+dispatch cost of a
// 4-step action against an in-memory Context. Budget ≤2ms p95,
// ≤3ms gate.
func BenchmarkResolveAction(b *testing.B) {
	path := writeBenchActions(b, 1, 4)
	cat, err := LoadCatalogue(context.Background(), path, defaultDeps())
	if err != nil {
		b.Fatal(err)
	}
	a := &cat.Actions[0]
	deps, _, _, _, _, _, _, _, _ := depsForTest()
	ctx := newTestContext()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = Run(context.Background(), a, ctx, deps)
	}
	avgUs := float64(b.Elapsed().Nanoseconds()) / float64(b.N) / 1000.0
	const budgetUs = 3000 // 3ms gate (50% headroom on 2ms budget)
	if avgUs > budgetUs {
		b.Errorf("BenchmarkResolveAction: avg %.2fµs exceeds %dµs gate", avgUs, budgetUs)
	}
}

// writeBenchActions generates a synthetic actions.toml with N actions,
// each with stepsPerAction steps drawn from a small rotating op set.
func writeBenchActions(tb testing.TB, n, stepsPerAction int) string {
	tb.Helper()
	dir := tb.TempDir()
	path := filepath.Join(dir, "actions.toml")
	var sb strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&sb, "\n[[custom_action]]\nname = \"a%s\"\ndescription = \"benchmark\"\nsequence = [", addrSuffix(i))
		for s := 0; s < stepsPerAction; s++ {
			if s > 0 {
				sb.WriteString(", ")
			}
			switch s % 4 {
			case 0:
				sb.WriteString("{ op = \"mark_read\" }")
			case 1:
				sb.WriteString("{ op = \"archive\" }")
			case 2:
				sb.WriteString("{ op = \"add_category\", category = \"bench\" }")
			case 3:
				sb.WriteString("{ op = \"flag\" }")
			}
		}
		sb.WriteString("]\n")
	}
	if err := os.WriteFile(path, []byte(sb.String()), 0o600); err != nil {
		tb.Fatal(err)
	}
	return path
}
