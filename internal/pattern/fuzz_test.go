package pattern

import "testing"

// FuzzParse asserts the parser never panics on arbitrary input. Any
// crash found here gets committed to testdata/fuzz/FuzzParse/ so it
// becomes a permanent regression test.
//
// Run locally with:
//
//	go test -fuzz=FuzzParse -fuzztime=30s ./internal/pattern/...
func FuzzParse(f *testing.F) {
	seeds := []string{
		"",
		"~f bob",
		"~A",
		"~A & ~N",
		"~f bob | ~f alice",
		"(~f a | ~f b) ~A",
		"! ~N",
		"~s \"Q4 review\"",
		"~d <30d",
		"~d 2026-01-01..2026-04-01",
		"~f *@vendor.com",
		"~f *foo*",
		"~h list-id:newsletter",
		// Pathological inputs the parser must NOT crash on:
		"~",
		"~f",
		"~Z bad",
		"((((",
		"))))",
		"~s \"unterminated",
		"~f bob ~f alice ~A ~N ~F ~U",
		// Spec 23 routing operator seed corpus.
		"~o feed",
		"~o none",
		"~o paper_trail",
		"~o screener & ~A",
		"~o feed | ~o paper_trail",
		"!~o feed",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, src string) {
		_, _ = Parse(src) // must not panic on any input
	})
}

// FuzzCompileLocal additionally exercises the SQL evaluator. Any
// pattern that parses without error must also compile to a SQLClause
// without panicking.
func FuzzCompileLocal(f *testing.F) {
	seeds := []string{
		"~f bob",
		"~A",
		"(~f a | ~f b) & ! ~N",
		"~d <30d",
		"~s *budget*",
		"~G Newsletters",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, src string) {
		root, err := Parse(src)
		if err != nil || root == nil {
			return
		}
		_, _ = CompileLocal(root) // must not panic
	})
}
