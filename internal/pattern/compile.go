package pattern

import (
	"fmt"
)

// ExecutionStrategy enumerates how Execute will run a Compiled
// pattern. Spec 08 §6 / §7. The strategy is selected at Compile
// time after analysing which predicates the AST contains and
// which backends can satisfy them.
type ExecutionStrategy int

const (
	// StrategyLocalOnly evaluates the pattern entirely against
	// the local SQLite cache. Fastest path; doesn't see anything
	// outside the cache (deep archive needs :backfill first).
	StrategyLocalOnly ExecutionStrategy = iota
	// StrategyServerFilter renders the AST as a Graph $filter
	// expression. Server sees the full mailbox.
	StrategyServerFilter
	// StrategyServerSearch renders the AST as a Graph $search
	// expression. Used when the AST contains body / header
	// predicates that $filter can't express.
	StrategyServerSearch
	// StrategyServerHybrid runs $filter and $search separately
	// then INTERSECTs the IDs. Graph rejects $filter+$search on
	// /me/messages, so we have to fan out and combine in memory.
	StrategyServerHybrid
	// StrategyTwoStage runs a server query for the candidate
	// set, then evaluates the structural predicates locally on
	// the cached envelopes. Used when the AST mixes a server-
	// only predicate (~b) with a local-only one (~N / ~F / ~i).
	StrategyTwoStage
	// StrategyLocalRegex narrows a regex match via the spec 35
	// trigram index, then post-filters with Go regexp.
	// Selected when the AST contains a [RegexValue] and the
	// caller has [body_index] enabled. Spec 35 §9.2 / §9.4.
	StrategyLocalRegex
)

// String returns the human-readable strategy name (used by
// --explain output).
func (s ExecutionStrategy) String() string {
	switch s {
	case StrategyLocalOnly:
		return "StrategyLocalOnly"
	case StrategyServerFilter:
		return "StrategyServerFilter"
	case StrategyServerSearch:
		return "StrategyServerSearch"
	case StrategyServerHybrid:
		return "StrategyServerHybrid"
	case StrategyTwoStage:
		return "StrategyTwoStage"
	case StrategyLocalRegex:
		return "StrategyLocalRegex"
	}
	return fmt.Sprintf("StrategyUnknown(%d)", int(s))
}

// CompilationPlan carries the rendered query strings + a human-
// readable explanation. Spec 08 §6. Empty fields signal that the
// strategy doesn't use that backend (StrategyLocalOnly leaves
// GraphFilter / GraphSearch empty; StrategyServerSearch leaves
// LocalSQL empty).
type CompilationPlan struct {
	LocalSQL      string
	LocalArgs     []any
	GraphFilter   string
	GraphSearch   string
	GraphFolderID string
	// Notes is the user-facing strategy explanation surfaced via
	// `:filter --explain <expr>`. One human-readable line per
	// design decision the planner made.
	Notes []string
}

// Compiled is the output of [Compile]: the parsed AST + the
// strategy choice + the rendered backend queries. Spec 10 / 11
// consume Compiled as a black box.
type Compiled struct {
	AST      Node
	Strategy ExecutionStrategy
	Plan     CompilationPlan
}

// CompileOptions tunes the Compile pass. Spec 08 §6.
type CompileOptions struct {
	// DefaultFolderID scopes a pattern that doesn't carry its
	// own ~m clause. Empty means "all subscribed folders".
	DefaultFolderID string
	// IncludeArchive widens the local FTS scope to include the
	// archive folder; passed through to evaluator if the
	// strategy lands locally.
	IncludeArchive bool
	// LocalOnly forces StrategyLocalOnly even when a server
	// strategy would be available. Used by the offline-mode
	// short-circuit.
	LocalOnly bool
	// PreferLocal biases the planner toward local execution
	// when the choice between local and server is a wash. Spec
	// 08 §7.2 — useful for big-list patterns where the user
	// prefers cache hits over a 2s server round-trip.
	PreferLocal bool
	// BodyIndexEnabled mirrors `[body_index].enabled`. When true,
	// ~b / ~B route against `body_text` (the spec 35 index) and
	// regex predicates are admitted on ~s / ~b / ~B. When false,
	// ~b / ~B keep the legacy `body_preview` LIKE behaviour and a
	// regex on ~b / ~B returns [ErrRegexRequiresLocalIndex] at
	// compile time. Spec 35 §9.2.
	BodyIndexEnabled bool
	// MaxRegexCandidates caps the trigram-narrowed candidate set
	// fed to the Go regex post-filter. Mirrors
	// `[body_index].max_regex_candidates`. 0 → 2000 (spec 35 §7).
	MaxRegexCandidates int
}

// ErrPatternUnsupported is returned by Compile when the supplied
// AST contains predicates that no backend can satisfy. The error
// message names the offender (e.g. `~h` outside server mode).
var ErrPatternUnsupported = fmt.Errorf("pattern: unsupported predicate combination")

// ErrUnsupported is returned by EmitFilter / EmitSearch when the
// supplied AST contains a predicate that backend can't render.
// The strategy selector consumes this signal to pick a fallback
// path (TwoStage / StrategyLocalOnly).
var ErrUnsupported = fmt.Errorf("pattern: backend cannot express this predicate")

// Spec 35 §9.3 sentinels surfaced by Compile when a regex predicate
// can't be evaluated under the current configuration.
var (
	// ErrRegexUnboundedScan is returned when a regex predicate has no
	// mandatory literal of length ≥ 3 — the trigram narrow can't run
	// and a full-scan post-filter would violate perf budgets.
	ErrRegexUnboundedScan = fmt.Errorf("pattern: regex requires at least one 3-character literal substring; add a literal anchor or scope to a folder")
	// ErrRegexRequiresLocalIndex is returned when a regex predicate
	// touches body fields (~b / ~B) but [body_index] is disabled.
	ErrRegexRequiresLocalIndex = fmt.Errorf("pattern: regex body / subject-or-body search needs [body_index].enabled = true; run 'inkwell index rebuild' first")
	// ErrRegexNotSupportedOnHeader is returned when a regex predicate
	// is attached to ~h. Graph $search has no regex; we refuse rather
	// than silently lose semantics.
	ErrRegexNotSupportedOnHeader = fmt.Errorf("pattern: ~h does not support regex; Graph $search is token-based — use a literal value or run a folder-scoped search")
)

// Compile parses src, walks the AST to classify each predicate
// against the backend matrix (spec 08 §7.1), and produces a
// Compiled with the chosen strategy + rendered query strings.
//
// On parse error → returns the parser error verbatim so callers
// can highlight the column. On a pattern that no backend can
// handle (e.g., `~h` with LocalOnly forced) → ErrPatternUnsupported.
func Compile(src string, opts CompileOptions) (*Compiled, error) {
	root, err := Parse(src)
	if err != nil {
		return nil, err
	}
	return CompileNode(root, opts)
}

// CompileNode is Compile starting from an already-parsed AST.
// Useful for tests that hand-craft ASTs without round-tripping
// through the lexer.
func CompileNode(root Node, opts CompileOptions) (*Compiled, error) {
	if root == nil {
		return nil, fmt.Errorf("CompileNode: nil AST")
	}
	cap := analyse(root)
	strategy, plan, err := selectStrategy(root, cap, opts)
	if err != nil {
		return nil, err
	}
	return &Compiled{AST: root, Strategy: strategy, Plan: plan}, nil
}

// astCapability summarises what the AST contains so the strategy
// selector can pick a backend without re-walking the tree per
// option. Each field counts the predicates of that flavour
// reachable from the root (counts, not booleans, so `--explain`
// can name "2 unsupported predicates").
type astCapability struct {
	hasBody         bool // ~b / ~B
	hasHeader       bool // ~h
	hasReadFlag     bool // ~N / ~U / ~F
	hasImportance   bool // ~i / ~y
	hasRouting      bool // ~o (spec 23) — local-only, like ~i / ~y
	hasFolderScope  string
	hasNonLocalPred bool // any predicate that local SQL rejects today
	predicateCount  int
	// Spec 35 §9.2: regex predicates and the fields they touch.
	hasRegex            bool
	regexOnBody         bool // ~b / ~B with RegexValue
	regexOnHeader       bool // ~h with RegexValue (already rejected at parse time; defensive)
	regexOnSubjectOnly  bool // only ~s carries regex
	regexPredicateCount int
}

// analyse walks the AST once and tallies per-backend support
// signals.
func analyse(root Node) astCapability {
	var cap astCapability
	regexSubjectOnly := true
	regexCount := 0
	walk(root, func(n Node) {
		p, ok := n.(Predicate)
		if !ok {
			return
		}
		cap.predicateCount++
		// Spec 35 §9.2 regex detection runs alongside the existing
		// per-field tallies.
		if _, isRegex := p.Value.(RegexValue); isRegex {
			cap.hasRegex = true
			regexCount++
			switch p.Field {
			case FieldBody, FieldSubjectOrBody:
				cap.regexOnBody = true
				regexSubjectOnly = false
			case FieldHeader:
				cap.regexOnHeader = true
				regexSubjectOnly = false
			case FieldSubject:
				// subject-only stays true unless a non-subject regex appears
			default:
				regexSubjectOnly = false
			}
		}
		switch p.Field {
		case FieldBody, FieldSubjectOrBody:
			cap.hasBody = true
		case FieldHeader:
			cap.hasHeader = true
			cap.hasNonLocalPred = true
		case FieldUnread, FieldRead, FieldFlagged:
			cap.hasReadFlag = true
		case FieldImportance, FieldInferenceCls:
			cap.hasImportance = true
		case FieldRouting:
			cap.hasRouting = true
		case FieldFolder:
			if sv, ok := p.Value.(StringValue); ok {
				cap.hasFolderScope = sv.Raw
			}
		}
	})
	cap.regexPredicateCount = regexCount
	if cap.hasRegex {
		cap.regexOnSubjectOnly = regexSubjectOnly
	}
	return cap
}

// astWithoutLocalOnly returns a rewritten AST with leaves that
// Graph $search can't express stripped from AND'd subtrees so
// the result renders cleanly via [EmitSearch]. Used by the
// TwoStage planner — server runs the strict superset; the
// in-memory evaluator refines against the full AST. Spec 08
// §11 / §11.1.
//
// Strip rules:
//   - Predicate with a local-only field (~N / ~U / ~F / ~i / ~y /
//     ~v) → return nil (the predicate is "removable").
//   - And: rewrite each branch; nil branches drop, the surviving
//     branch becomes the parent. Both nil → return nil.
//   - Or: dropping a branch from an OR would change the result
//     set (could miss matches), so any nil branch propagates as
//     nil to signal "can't safely strip".
//   - Not: a Not over a strippable leaf is itself strippable
//     because the over-broad superset stays sound (refinement
//     still excludes false positives).
func astWithoutLocalOnly(n Node) Node {
	switch v := n.(type) {
	case Predicate:
		if isLocalOnlyField(v.Field) {
			return nil
		}
		return v
	case And:
		l := astWithoutLocalOnly(v.L)
		r := astWithoutLocalOnly(v.R)
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
		l := astWithoutLocalOnly(v.L)
		r := astWithoutLocalOnly(v.R)
		if l == nil || r == nil {
			return nil
		}
		return Or{L: l, R: r}
	case Not:
		x := astWithoutLocalOnly(v.X)
		if x == nil {
			return nil
		}
		return Not{X: x}
	}
	return n
}

// isLocalOnlyField reports whether a Predicate's field is one
// the server $search dialect can't express. The matrix mirrors
// EmitSearchPredicate's ErrUnsupported branches.
func isLocalOnlyField(f Field) bool {
	switch f {
	case FieldUnread, FieldRead, FieldFlagged,
		FieldImportance, FieldInferenceCls, FieldConversation,
		FieldRouting:
		return true
	}
	return false
}

// walk visits every node in the AST in pre-order. fn is invoked
// once per node.
func walk(n Node, fn func(Node)) {
	if n == nil {
		return
	}
	fn(n)
	switch v := n.(type) {
	case And:
		walk(v.L, fn)
		walk(v.R, fn)
	case Or:
		walk(v.L, fn)
		walk(v.R, fn)
	case Not:
		walk(v.X, fn)
	}
}

// selectStrategy picks an ExecutionStrategy + builds the
// CompilationPlan. Spec 08 §7.2 decision tree.
func selectStrategy(root Node, cap astCapability, opts CompileOptions) (ExecutionStrategy, CompilationPlan, error) {
	notes := []string{}

	// Spec 35 §9.2 step 0: regex predicates route locally via the
	// trigram index, or fail explicitly. Runs before every other
	// branch so the user sees a pointed error rather than e.g.
	// StrategyServerSearch trying to render a regex into $search.
	if cap.hasRegex {
		if cap.regexOnHeader {
			return 0, CompilationPlan{}, ErrRegexNotSupportedOnHeader
		}
		if cap.regexOnBody && !opts.BodyIndexEnabled {
			return 0, CompilationPlan{}, ErrRegexRequiresLocalIndex
		}
		// Mandatory-literal extraction happens at emit time, in
		// eval_local_regex; a "no literal" failure surfaces as
		// ErrRegexUnboundedScan from CompileLocalRegex. Subject-only
		// regex is admitted regardless of BodyIndexEnabled — subjects
		// live on `messages` and don't need body_text.
		notes = append(notes, fmt.Sprintf("Strategy: %s", StrategyLocalRegex))
		notes = append(notes, "Reason: regex predicate; trigram narrow + Go regexp post-filter.")
		return StrategyLocalRegex, CompilationPlan{
			GraphFolderID: cap.hasFolderScope,
			Notes:         notes,
		}, nil
	}

	// Step 0: LocalOnly forced — try the local evaluator
	// directly. If CompileLocal accepts the AST (which today
	// includes ~b / ~B against body_preview, falling short of
	// full-body but matching the existing UX), use it; if not
	// (only ~h triggers this), surface ErrPatternUnsupported.
	// This is checked BEFORE the body/header server route so
	// the user's LocalOnly opt-in wins over the planner's
	// server-preference.
	if opts.LocalOnly {
		c, err := CompileLocal(root)
		if err != nil {
			return 0, CompilationPlan{}, fmt.Errorf("%w: %v", ErrPatternUnsupported, err)
		}
		notes = append(notes, fmt.Sprintf("Strategy: %s", StrategyLocalOnly))
		notes = append(notes, "Reason: LocalOnly forced (offline mode or explicit --local).")
		return StrategyLocalOnly, CompilationPlan{
			LocalSQL:  c.Where,
			LocalArgs: c.Args,
			Notes:     notes,
		}, nil
	}

	// Step 1: body / header → MUST go via server $search.
	if cap.hasBody || cap.hasHeader {
		// If the AST also contains predicates $search can't
		// express (~N / ~U / ~F / ~i / ~y), peel them off the
		// $search subtree; refinement runs in-memory against
		// the full AST. Spec 08 §11.
		if cap.hasReadFlag || cap.hasImportance || cap.hasRouting {
			stripped := astWithoutLocalOnly(root)
			if stripped == nil {
				notes = append(notes, fmt.Sprintf("Strategy: %s", StrategyLocalOnly))
				notes = append(notes, "Reason: pattern OR's a body / header predicate with a local-only one — TwoStage can't safely strip; falling back to local cache.")
				if c, err := CompileLocal(root); err == nil {
					return StrategyLocalOnly, CompilationPlan{
						LocalSQL:  c.Where,
						LocalArgs: c.Args,
						Notes:     notes,
					}, nil
				}
				return 0, CompilationPlan{}, fmt.Errorf("%w: pattern combines body / header with local-only predicates in an unsplittable shape", ErrPatternUnsupported)
			}
			searchExpr, err := EmitSearch(stripped)
			if err != nil {
				return 0, CompilationPlan{}, fmt.Errorf("%w: %v", ErrPatternUnsupported, err)
			}
			notes = append(notes, fmt.Sprintf("Strategy: %s", StrategyTwoStage))
			notes = append(notes, "Reason: ~b / ~B / ~h must run server-side via $search; the local refinement filters by ~N / ~U / ~F / ~i / ~y on the cached envelopes.")
			return StrategyTwoStage, CompilationPlan{
				GraphSearch:   searchExpr,
				GraphFolderID: cap.hasFolderScope,
				Notes:         notes,
			}, nil
		}
		searchExpr, err := EmitSearch(root)
		if err != nil {
			// $search couldn't express the AST — but we already
			// know the pattern needs server-only predicates, so
			// nothing else can satisfy it.
			return 0, CompilationPlan{}, fmt.Errorf("%w: %v", ErrPatternUnsupported, err)
		}
		notes = append(notes, fmt.Sprintf("Strategy: %s", StrategyServerSearch))
		notes = append(notes, "Reason: ~b / ~B / ~h require Graph $search.")
		return StrategyServerSearch, CompilationPlan{
			GraphSearch:   searchExpr,
			GraphFolderID: cap.hasFolderScope,
			Notes:         notes,
		}, nil
	}

	// Step 2: PreferLocal AND all predicates have a local
	// execution path.
	if opts.PreferLocal {
		if c, err := CompileLocal(root); err == nil {
			notes = append(notes, fmt.Sprintf("Strategy: %s", StrategyLocalOnly))
			notes = append(notes, "Reason: PreferLocal set and all predicates expressible locally.")
			return StrategyLocalOnly, CompilationPlan{
				LocalSQL:  c.Where,
				LocalArgs: c.Args,
				Notes:     notes,
			}, nil
		}
	}

	// Step 3: try $filter — covers most structural patterns
	// (read/flag/importance/category/recipient/date/attachment)
	// and gives the user full-mailbox visibility, not just cache.
	filterExpr, ferr := EmitFilter(root)
	if ferr == nil {
		notes = append(notes, fmt.Sprintf("Strategy: %s", StrategyServerFilter))
		notes = append(notes, "Reason: All predicates satisfiable by Graph $filter; server sees the full mailbox.")
		return StrategyServerFilter, CompilationPlan{
			GraphFilter:   filterExpr,
			GraphFolderID: cap.hasFolderScope,
			Notes:         notes,
		}, nil
	}

	// Step 4: $filter rejected → fall back to local. Step 5
	// (ServerHybrid for mixed structural + text) is conceptually
	// here, but in practice patterns that mix `~s` text with
	// structural predicates compile cleanly through $filter
	// already (`contains(subject, 'X')` is part of $filter). The
	// remaining shapes that needed Hybrid all involve body /
	// header — those took the Step 1 branch above. So Step 4
	// collapses to LocalOnly fallback.
	c, err := CompileLocal(root)
	if err != nil {
		return 0, CompilationPlan{}, fmt.Errorf("%w: filter rejected (%v) and local rejected (%v)", ErrPatternUnsupported, ferr, err)
	}
	notes = append(notes, fmt.Sprintf("Strategy: %s", StrategyLocalOnly))
	notes = append(notes, fmt.Sprintf("Reason: Graph $filter cannot express this AST (%v); falling back to local cache.", ferr))
	return StrategyLocalOnly, CompilationPlan{
		LocalSQL:  c.Where,
		LocalArgs: c.Args,
		Notes:     notes,
	}, nil
}

// Explain renders a Compiled's plan as a multi-line human-readable
// string. Spec 08 §7.2 example output. Used by `:filter --explain`.
func (c *Compiled) Explain() string {
	if c == nil {
		return ""
	}
	out := ""
	for _, n := range c.Plan.Notes {
		out += n + "\n"
	}
	if c.Plan.LocalSQL != "" {
		out += "Local SQL: WHERE " + c.Plan.LocalSQL + "\n"
	}
	if c.Plan.GraphFilter != "" {
		out += "Graph $filter: " + c.Plan.GraphFilter + "\n"
	}
	if c.Plan.GraphSearch != "" {
		out += "Graph $search: \"" + c.Plan.GraphSearch + "\"\n"
	}
	if c.Plan.GraphFolderID != "" {
		out += "Folder scope: " + c.Plan.GraphFolderID + "\n"
	}
	return out
}
