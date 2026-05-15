package pattern

import (
	"context"
	"fmt"
	"regexp"
	"regexp/syntax"
	"strings"
	"time"
)

// RegexPlan is the spec-35 §9.4 plan for a StrategyLocalRegex query.
// The caller (Execute / bulk-filter wiring) calls
// [RegexPlan.ExecuteAgainst] with the live [store.Store] handle —
// keeping the pattern package free of a store import preserves the
// CONVENTIONS §2 layering.
type RegexPlan struct {
	// Compiled regex per predicate. For ~B the same regex appears
	// twice — once tagged for subject, once for body — so the
	// post-filter applies the right field each time.
	Predicates []RegexPredicate
	// Literals are the mandatory ≥3-char substrings extracted across
	// every regex predicate, ANDed into the trigram narrow query.
	Literals []string
	// FolderID scopes the candidate query (optional).
	FolderID string
	// MaxCandidates caps the trigram-narrowed result set.
	MaxCandidates int
	// PostFilterTimeout caps the Go regex post-filter wall-clock.
	PostFilterTimeout time.Duration
	// StructuralWhere / StructuralArgs carry structural predicates
	// (~F / ~m / ~d etc.) so the candidate query can be a single
	// round-trip instead of a join + in-memory intersection.
	StructuralWhere string
	StructuralArgs  []any
}

// RegexPredicate is one regex predicate from the AST, paired with the
// field it targets so the post-filter knows whether to match against
// the candidate's body or the candidate's subject.
type RegexPredicate struct {
	Field    Field
	Compiled *regexp.Regexp
}

// CompileLocalRegex builds a [RegexPlan] from a regex-bearing AST.
// Returns:
//   - ErrRegexUnboundedScan when no regex predicate has a ≥3-char
//     mandatory literal substring.
//   - ErrRegexNotSupportedOnHeader when the AST somehow carries a
//     RegexValue on FieldHeader (the parser already rejects this).
//
// The structural-predicate strip mirrors astWithoutLocalOnly but
// inverts the polarity: we keep the structural part (anything that
// isn't a regex) for the candidate query's WHERE clause, and we
// keep the regex predicates separately for the Go-side post-filter.
//
// Spec 35 §9.2 / §9.4.
func CompileLocalRegex(root Node, opts CompileOptions) (*RegexPlan, error) {
	if root == nil {
		return nil, fmt.Errorf("CompileLocalRegex: nil AST")
	}
	plan := &RegexPlan{
		MaxCandidates:     opts.MaxRegexCandidates,
		PostFilterTimeout: 5 * time.Second,
	}
	if plan.MaxCandidates <= 0 {
		plan.MaxCandidates = 2000
	}

	var regexPreds []RegexPredicate
	var literals []string
	hasBodyRegex := false
	walk(root, func(n Node) {
		p, ok := n.(Predicate)
		if !ok {
			return
		}
		rv, ok := p.Value.(RegexValue)
		if !ok {
			return
		}
		if p.Field == FieldHeader {
			return // caller already errored at parse time; defence in depth
		}
		regexPreds = append(regexPreds, RegexPredicate{Field: p.Field, Compiled: rv.Compiled})
		lit := mandatoryLiterals(rv.Compiled)
		// Spec 35 §3.3 / §9.3: every regex must contribute at least
		// one ≥3-char literal. Multiple predicates compose via AND,
		// so we collect all of them and let the candidate query
		// AND-conjoin.
		literals = append(literals, lit...)
		if p.Field == FieldBody || p.Field == FieldSubjectOrBody {
			hasBodyRegex = true
		}
	})

	if len(regexPreds) == 0 {
		return nil, fmt.Errorf("CompileLocalRegex: AST has no regex predicates")
	}

	// Filter literals to those of length ≥ 3 and de-dup case-folded.
	uniq := make(map[string]struct{}, len(literals))
	var keepLits []string
	for _, lit := range literals {
		if len(lit) < 3 {
			continue
		}
		key := strings.ToLower(lit)
		if _, seen := uniq[key]; seen {
			continue
		}
		uniq[key] = struct{}{}
		keepLits = append(keepLits, lit)
	}
	// Need at least one literal per regex predicate or the trigram
	// narrow can't bound the candidate set.
	if len(keepLits) < len(regexPreds) {
		return nil, ErrRegexUnboundedScan
	}

	// Spec 35 §9.2 step 0: body regex needs the index on; the strategy
	// selector already enforced this, but a fresh caller that hand-
	// builds a plan deserves the same defence.
	if hasBodyRegex && !opts.BodyIndexEnabled {
		return nil, ErrRegexRequiresLocalIndex
	}

	plan.Predicates = regexPreds
	plan.Literals = keepLits

	// Carry the structural part of the AST as the SQL filter on
	// `messages m`. Strip leaves all regex predicates with nil; And
	// branches collapse; Or with a stripped leaf can't safely strip
	// (would change the result set).
	structural := astStripRegex(root)
	if structural != nil {
		c, err := CompileLocalWithOpts(structural, opts)
		if err != nil {
			return nil, fmt.Errorf("CompileLocalRegex: structural compile failed: %w", err)
		}
		if c.Where != "" {
			plan.StructuralWhere = c.Where
			plan.StructuralArgs = c.Args
		}
	}

	return plan, nil
}

// astStripRegex returns a copy of the AST with regex predicates
// removed (mirror of astWithoutLocalOnly but the polarity is
// inverted — we keep what isn't regex).
func astStripRegex(n Node) Node {
	switch v := n.(type) {
	case Predicate:
		if _, ok := v.Value.(RegexValue); ok {
			return nil
		}
		return v
	case And:
		l := astStripRegex(v.L)
		r := astStripRegex(v.R)
		switch {
		case l == nil && r == nil:
			return nil
		case l == nil:
			return r
		case r == nil:
			return l
		}
		return And{L: l, R: r}
	case Or:
		l := astStripRegex(v.L)
		r := astStripRegex(v.R)
		if l == nil || r == nil {
			// Can't safely strip: dropping a branch changes the
			// result set. Treat as "no structural filter".
			return nil
		}
		return Or{L: l, R: r}
	case Not:
		inner := astStripRegex(v.X)
		if inner == nil {
			return nil
		}
		return Not{X: inner}
	}
	return n
}

// mandatoryLiterals walks the regex syntax tree and returns every
// literal substring that MUST appear in any matching input. The
// extraction is conservative: only literals reachable from the root
// via [syntax.OpConcat] / single-branch [syntax.OpAlternate] / [Op*+]
// 1+-required quantifiers are kept. Alternation with multiple
// branches contributes a literal only when every branch begins with
// the same literal prefix.
//
// Returns the longest run of consecutive literal runes encountered
// (one per concat path). The caller filters to length ≥ 3 and
// de-duplicates.
func mandatoryLiterals(re *regexp.Regexp) []string {
	if re == nil {
		return nil
	}
	parsed, err := syntax.Parse(re.String(), syntax.Perl)
	if err != nil {
		return nil
	}
	return collectLiterals(parsed)
}

func collectLiterals(r *syntax.Regexp) []string {
	if r == nil {
		return nil
	}
	switch r.Op {
	case syntax.OpLiteral:
		if len(r.Rune) == 0 {
			return nil
		}
		return []string{string(r.Rune)}
	case syntax.OpConcat:
		// Concatenate adjacent literal sub-runs.
		var lits []string
		var cur strings.Builder
		flush := func() {
			if cur.Len() > 0 {
				lits = append(lits, cur.String())
				cur.Reset()
			}
		}
		for _, sub := range r.Sub {
			switch sub.Op {
			case syntax.OpLiteral:
				cur.WriteString(string(sub.Rune))
			case syntax.OpCapture:
				// Recurse; if the capture is a pure literal, fold it in.
				inner := collectLiterals(sub)
				if len(inner) == 1 && !strings.ContainsAny(inner[0], ".*+?") {
					// not actually checking dot, but inner is a literal
					// run because OpLiteral collapses
					cur.WriteString(inner[0])
				} else {
					flush()
					lits = append(lits, inner...)
				}
			case syntax.OpPlus:
				// "x+" — at least one of the inner pattern. If the
				// inner is a literal, include one copy.
				if len(sub.Sub) == 1 && sub.Sub[0].Op == syntax.OpLiteral {
					cur.WriteString(string(sub.Sub[0].Rune))
				} else {
					flush()
				}
			default:
				flush()
				lits = append(lits, collectLiterals(sub)...)
			}
		}
		flush()
		return lits
	case syntax.OpAlternate:
		// Every branch must contribute the same prefix for it to be
		// mandatory across the alternation. Conservative for v1 —
		// return literals only when every branch begins identically.
		if len(r.Sub) == 0 {
			return nil
		}
		// Easy case: pull out per-branch literals and keep only the
		// longest common prefix that appears in every branch.
		branchLits := make([][]string, len(r.Sub))
		for i, sub := range r.Sub {
			branchLits[i] = collectLiterals(sub)
		}
		// Find a literal that appears in every branch (string-set
		// intersection).
		if len(branchLits[0]) == 0 {
			return nil
		}
		var common []string
		for _, lit := range branchLits[0] {
			present := true
			for _, others := range branchLits[1:] {
				found := false
				for _, o := range others {
					if o == lit {
						found = true
						break
					}
				}
				if !found {
					present = false
					break
				}
			}
			if present {
				common = append(common, lit)
			}
		}
		return common
	case syntax.OpCapture:
		if len(r.Sub) == 1 {
			return collectLiterals(r.Sub[0])
		}
	case syntax.OpPlus:
		// At least one of the inner pattern is required.
		if len(r.Sub) == 1 {
			return collectLiterals(r.Sub[0])
		}
	}
	return nil
}

// CandidateFetcher is the seam to [store.Store.SearchBodyTrigramCandidates]
// — declared at the consumer site so this package stays free of the
// store import. Tests substitute a fake.
type CandidateFetcher interface {
	SearchBodyTrigramCandidates(ctx context.Context, q TrigramQuery) ([]Candidate, error)
	// MessageFields lifts the structural columns needed by the post-
	// filter that touches subject. nil-safe: missing rows are dropped.
	MessageSubject(ctx context.Context, messageID string) (string, error)
}

// TrigramQuery mirrors store.BodyTrigramQuery (defined here so the
// pattern package stays free of a store import; the cmd layer
// translates between the two).
type TrigramQuery struct {
	AccountID       int64
	FolderID        string
	Literals        []string
	StructuralWhere string
	StructuralArgs  []any
	Limit           int
}

// Candidate mirrors store.BodyCandidate at the pattern-package
// boundary.
type Candidate struct {
	MessageID string
	Content   string
}

// ExecuteAgainst runs the trigram narrow + Go regex post-filter and
// returns the matching message ids. Per-call wall-clock is bounded
// by [RegexPlan.PostFilterTimeout]; the regex loop checks the
// timeout between iterations. The Go `regexp` engine does not
// honour ctx natively — see CONVENTIONS §16 ledger.
//
// AccountID is the scope; the caller is responsible for passing the
// resolved account id from its session context.
func (rp *RegexPlan) ExecuteAgainst(ctx context.Context, accountID int64, f CandidateFetcher) ([]string, error) {
	if rp == nil {
		return nil, fmt.Errorf("RegexPlan.ExecuteAgainst: nil plan")
	}
	q := TrigramQuery{
		AccountID:       accountID,
		FolderID:        rp.FolderID,
		Literals:        rp.Literals,
		StructuralWhere: rp.StructuralWhere,
		StructuralArgs:  rp.StructuralArgs,
		Limit:           rp.MaxCandidates,
	}
	cands, err := f.SearchBodyTrigramCandidates(ctx, q)
	if err != nil {
		return nil, err
	}
	if len(cands) > rp.MaxCandidates {
		return nil, fmt.Errorf("pattern: too many candidates (%d); narrow with a folder or a more specific literal", len(cands))
	}

	deadline := time.Now().Add(rp.PostFilterTimeout)
	var matches []string
	for _, c := range cands {
		if time.Now().After(deadline) {
			return matches, fmt.Errorf("pattern: regex post-filter exceeded %s; returned %d partial matches", rp.PostFilterTimeout, len(matches))
		}
		if rp.matches(ctx, c, f) {
			matches = append(matches, c.MessageID)
		}
	}
	return matches, nil
}

// matches applies every regex predicate in the plan against c. All
// predicates must match (AND semantics).
func (rp *RegexPlan) matches(ctx context.Context, c Candidate, f CandidateFetcher) bool {
	for _, p := range rp.Predicates {
		switch p.Field {
		case FieldBody:
			if !p.Compiled.MatchString(c.Content) {
				return false
			}
		case FieldSubject:
			subject, err := f.MessageSubject(ctx, c.MessageID)
			if err != nil {
				return false
			}
			if !p.Compiled.MatchString(subject) {
				return false
			}
		case FieldSubjectOrBody:
			subject, _ := f.MessageSubject(ctx, c.MessageID)
			if !p.Compiled.MatchString(c.Content) && !p.Compiled.MatchString(subject) {
				return false
			}
		default:
			return false
		}
	}
	return true
}
