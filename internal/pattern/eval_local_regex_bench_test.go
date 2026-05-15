package pattern

import (
	"context"
	"fmt"
	"math/rand"
	"regexp"
	"strings"
	"testing"
	"time"
)

// fakeCandidateFetcher returns pre-built candidates from memory.
// Spec-35-bench seam: lets us measure the post-filter loop without
// the store / SQLite round-trip cost on the path.
type fakeCandidateFetcher struct {
	cands    []Candidate
	subjects map[string]string
}

func (f *fakeCandidateFetcher) SearchBodyTrigramCandidates(_ context.Context, q TrigramQuery) ([]Candidate, error) {
	_ = q
	return f.cands, nil
}

func (f *fakeCandidateFetcher) MessageSubject(_ context.Context, id string) (string, error) {
	return f.subjects[id], nil
}

// synthBody produces n bytes of pseudo-English prose seeded from i.
// Roughly half of the bodies contain the literal "auth-token=" so
// the regex `auth.*token=[a-f0-9]+` matches a realistic fraction.
func synthBody(i, n int) string {
	r := rand.New(rand.NewSource(int64(i)))
	words := []string{
		"the", "quick", "brown", "fox", "jumps", "lazy", "dog",
		"please", "click", "here", "review", "budget", "meeting",
		"alpha", "bravo", "charlie", "delta", "echo", "foxtrot",
	}
	var b strings.Builder
	b.Grow(n)
	if i%2 == 0 {
		b.WriteString("Subject mentions auth-token=abc12345 in the body. ")
	}
	for b.Len() < n {
		b.WriteString(words[r.Intn(len(words))])
		b.WriteByte(' ')
	}
	return b.String()[:n]
}

// BenchmarkRegexPostFilter_200x5KB covers spec 35 §14 row 6
// (target <120 ms p95). Pure post-filter cost: 200 candidate bodies
// of 5 KB each, regex `auth.*token=[a-f0-9]+`. Measures
// RegexPlan.ExecuteAgainst with a fake CandidateFetcher so the
// SQLite trigram-narrow round-trip is excluded from the timing —
// exactly the cost the spec budget targets.
func BenchmarkRegexPostFilter_200x5KB(b *testing.B) {
	re := regexp.MustCompile(`auth.*token=[a-f0-9]+`)
	const n = 200
	cands := make([]Candidate, n)
	for i := 0; i < n; i++ {
		cands[i] = Candidate{MessageID: fmt.Sprintf("m-%d", i), Content: synthBody(i, 5*1024)}
	}
	fetcher := &fakeCandidateFetcher{cands: cands}
	plan := &RegexPlan{
		Predicates: []RegexPredicate{{Field: FieldBody, Compiled: re}},
		Literals:   []string{"auth", "token="},
		// Cap the post-filter at the budget so the bench fails loudly
		// if a future change blows the wall-clock.
		PostFilterTimeout: 120 * time.Millisecond,
		MaxCandidates:     n,
	}
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		matches, err := plan.ExecuteAgainst(ctx, 1, fetcher)
		if err != nil {
			b.Fatalf("ExecuteAgainst: %v", err)
		}
		if len(matches) == 0 {
			b.Fatalf("expected at least one match")
		}
	}
}

// BenchmarkRegexPostFilter_200x5KB_NoMatches measures the
// fast-fail path: every candidate is rejected. Verifies the
// post-filter doesn't degenerate when the regex never matches
// (catastrophic-backtracking guard).
func BenchmarkRegexPostFilter_200x5KB_NoMatches(b *testing.B) {
	re := regexp.MustCompile(`ZZZZZZ_neverappears_[A-Z]+`)
	const n = 200
	cands := make([]Candidate, n)
	for i := 0; i < n; i++ {
		cands[i] = Candidate{MessageID: fmt.Sprintf("m-%d", i), Content: synthBody(i, 5*1024)}
	}
	fetcher := &fakeCandidateFetcher{cands: cands}
	plan := &RegexPlan{
		Predicates:        []RegexPredicate{{Field: FieldBody, Compiled: re}},
		Literals:          []string{"ZZZZZZ"},
		PostFilterTimeout: 120 * time.Millisecond,
		MaxCandidates:     n,
	}
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		matches, err := plan.ExecuteAgainst(ctx, 1, fetcher)
		if err != nil {
			b.Fatalf("ExecuteAgainst: %v", err)
		}
		if len(matches) != 0 {
			b.Fatalf("expected zero matches, got %d", len(matches))
		}
	}
}

// BenchmarkRegexSearchEndToEnd_5kCorpus is a directional proxy for
// spec 35 §14 row 7 (full end-to-end at 50 k bodies, budget <300 ms
// p95). We measure CompileLocalRegex + ExecuteAgainst against a
// 5 k in-memory candidate set — same shape as production, smaller
// corpus. The 50 k full-scale run is a follow-up tagged for the
// v0.64 measurement pass.
func BenchmarkRegexSearchEndToEnd_5kCorpus(b *testing.B) {
	const n = 5_000
	cands := make([]Candidate, n)
	for i := 0; i < n; i++ {
		cands[i] = Candidate{MessageID: fmt.Sprintf("m-%d", i), Content: synthBody(i, 4*1024)}
	}
	fetcher := &fakeCandidateFetcher{cands: cands}
	root, err := Parse(`~b /auth.*token=[a-f0-9]+/`)
	if err != nil {
		b.Fatalf("Parse: %v", err)
	}
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		plan, err := CompileLocalRegex(root, CompileOptions{BodyIndexEnabled: true, MaxRegexCandidates: n})
		if err != nil {
			b.Fatalf("CompileLocalRegex: %v", err)
		}
		plan.PostFilterTimeout = 300 * time.Millisecond
		plan.MaxCandidates = n
		if _, err := plan.ExecuteAgainst(ctx, 1, fetcher); err != nil {
			b.Fatalf("ExecuteAgainst: %v", err)
		}
	}
}
