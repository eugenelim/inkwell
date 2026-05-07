package pattern

import (
	"context"
	"fmt"
	"strings"

	"github.com/eugenelim/inkwell/internal/store"
)

// LocalSearcher narrows store.Store to the predicate path used
// by [Execute]. Only the SearchByPredicate / GetMessage methods
// the executor actually calls show up here so tests can stub
// without hauling in a full store.
type LocalSearcher interface {
	SearchByPredicate(ctx context.Context, accountID int64, where string, args []any, limit int) ([]store.Message, error)
	GetMessage(ctx context.Context, id string) (*store.Message, error)
}

// RoutingLookup is the optional surface a LocalSearcher may
// implement so the TwoStage refinement path can satisfy `~o`
// (routing) predicates against the in-memory candidate set.
// store.Store satisfies this; test stubs that don't implement it
// silently degrade — `~o` evaluates to false during refinement
// and the TwoStage result skips routing-matched messages, which
// is acceptable for the tests that don't exercise routing.
type RoutingLookup interface {
	GetSenderRouting(ctx context.Context, accountID int64, emailAddress string) (string, error)
}

// GraphService is the consumer-side seam to the Graph backend
// for Execute. internal/graph satisfies this via a small adapter
// so the pattern package's tests don't need to import graph.
//
// SearchMessages is the same shape as the spec 06 Searcher's
// Graph branch — Query is the rendered $search expression,
// FolderID is optional URL-path scope, Top caps the page.
//
// FilterMessages is the OData $filter side. Filter is the
// rendered expression; FolderID is optional URL-path scope; Top
// caps the page. Implementations may paginate; the executor
// expects them to return the full result set under the cap.
type GraphService interface {
	SearchMessages(ctx context.Context, q ServerQuery) ([]store.Message, error)
	FilterMessages(ctx context.Context, q ServerQuery) ([]store.Message, error)
}

// ServerQuery is the typed envelope passed to GraphService for
// either a $search or $filter call. Filter / Search carry their
// rendered expressions; FolderID is the optional URL-path scope.
type ServerQuery struct {
	Filter   string
	Search   string
	FolderID string
	Top      int
}

// ExecuteOptions tunes Execute. ServerCandidateLimit caps the
// TwoStage server fetch; LocalMatchLimit caps the LocalOnly
// SQL output.
type ExecuteOptions struct {
	AccountID            int64
	LocalMatchLimit      int
	ServerCandidateLimit int
}

// Execute runs c against the supplied backends and returns the
// matching message IDs. Spec 08 §6 / §11.
//
// The local + graph args are interfaces so callers (UI, tests)
// can stub without dragging in production wiring. Either may be
// nil when the chosen strategy doesn't need that backend
// (StrategyLocalOnly with graph=nil; StrategyServerSearch with
// local=nil); the executor double-checks before dispatching.
func Execute(ctx context.Context, c *Compiled, local LocalSearcher, graph GraphService, opts ExecuteOptions) ([]string, error) {
	if c == nil {
		return nil, fmt.Errorf("Execute: nil Compiled")
	}
	if opts.LocalMatchLimit <= 0 {
		opts.LocalMatchLimit = 5000
	}
	if opts.ServerCandidateLimit <= 0 {
		opts.ServerCandidateLimit = 1000
	}
	switch c.Strategy {
	case StrategyLocalOnly:
		return executeLocal(ctx, c, local, opts)
	case StrategyServerFilter:
		return executeServerFilter(ctx, c, graph, opts)
	case StrategyServerSearch:
		return executeServerSearch(ctx, c, graph, opts)
	case StrategyServerHybrid:
		return executeServerHybrid(ctx, c, graph, opts)
	case StrategyTwoStage:
		return executeTwoStage(ctx, c, local, graph, opts)
	}
	return nil, fmt.Errorf("Execute: unknown strategy %v", c.Strategy)
}

func executeLocal(ctx context.Context, c *Compiled, local LocalSearcher, opts ExecuteOptions) ([]string, error) {
	if local == nil {
		return nil, fmt.Errorf("Execute: LocalSearcher is required for %s", c.Strategy)
	}
	msgs, err := local.SearchByPredicate(ctx, opts.AccountID, c.Plan.LocalSQL, c.Plan.LocalArgs, opts.LocalMatchLimit)
	if err != nil {
		return nil, fmt.Errorf("execute local: %w", err)
	}
	return idsOf(msgs), nil
}

func executeServerFilter(ctx context.Context, c *Compiled, graph GraphService, opts ExecuteOptions) ([]string, error) {
	if graph == nil {
		return nil, fmt.Errorf("Execute: GraphService is required for %s", c.Strategy)
	}
	msgs, err := graph.FilterMessages(ctx, ServerQuery{
		Filter:   c.Plan.GraphFilter,
		FolderID: c.Plan.GraphFolderID,
		Top:      opts.ServerCandidateLimit,
	})
	if err != nil {
		return nil, fmt.Errorf("execute server-filter: %w", err)
	}
	return idsOf(msgs), nil
}

func executeServerSearch(ctx context.Context, c *Compiled, graph GraphService, opts ExecuteOptions) ([]string, error) {
	if graph == nil {
		return nil, fmt.Errorf("Execute: GraphService is required for %s", c.Strategy)
	}
	msgs, err := graph.SearchMessages(ctx, ServerQuery{
		Search:   c.Plan.GraphSearch,
		FolderID: c.Plan.GraphFolderID,
		Top:      opts.ServerCandidateLimit,
	})
	if err != nil {
		return nil, fmt.Errorf("execute server-search: %w", err)
	}
	return idsOf(msgs), nil
}

// executeServerHybrid runs both $filter and $search, then
// INTERSECTs the IDs in memory. Graph rejects $filter+$search on
// /me/messages, so we fan out and combine. Spec 08 §7.3.
func executeServerHybrid(ctx context.Context, c *Compiled, graph GraphService, opts ExecuteOptions) ([]string, error) {
	if graph == nil {
		return nil, fmt.Errorf("Execute: GraphService is required for %s", c.Strategy)
	}
	filtered, err := graph.FilterMessages(ctx, ServerQuery{
		Filter:   c.Plan.GraphFilter,
		FolderID: c.Plan.GraphFolderID,
		Top:      opts.ServerCandidateLimit,
	})
	if err != nil {
		return nil, fmt.Errorf("execute hybrid (filter): %w", err)
	}
	searched, err := graph.SearchMessages(ctx, ServerQuery{
		Search:   c.Plan.GraphSearch,
		FolderID: c.Plan.GraphFolderID,
		Top:      opts.ServerCandidateLimit,
	})
	if err != nil {
		return nil, fmt.Errorf("execute hybrid (search): %w", err)
	}
	return intersectIDs(idsOf(filtered), idsOf(searched)), nil
}

// executeTwoStage runs server $search to get a candidate set,
// then refines locally via in-memory evaluation against the
// cached envelopes. Spec 08 §11.
//
// Candidates not in the local cache (deep archive) are silently
// dropped — the spec calls this out as a documented limitation;
// users :backfill the relevant folder to widen the cache.
func executeTwoStage(ctx context.Context, c *Compiled, local LocalSearcher, graph GraphService, opts ExecuteOptions) ([]string, error) {
	if graph == nil {
		return nil, fmt.Errorf("Execute: GraphService is required for %s", c.Strategy)
	}
	if local == nil {
		return nil, fmt.Errorf("Execute: LocalSearcher is required for %s refinement", c.Strategy)
	}
	candidates, err := graph.SearchMessages(ctx, ServerQuery{
		Search:   c.Plan.GraphSearch,
		FolderID: c.Plan.GraphFolderID,
		Top:      opts.ServerCandidateLimit,
	})
	if err != nil {
		return nil, fmt.Errorf("execute two-stage (server): %w", err)
	}
	if len(candidates) > opts.ServerCandidateLimit {
		return nil, fmt.Errorf("two-stage: %d candidates exceeds limit %d; refine the pattern", len(candidates), opts.ServerCandidateLimit)
	}

	// Pre-load routing for every distinct sender in the candidate
	// set when the AST contains a `~o` predicate (spec 23 §4.3) AND
	// the LocalSearcher implements RoutingLookup. This keeps the
	// refinement loop allocation-light and avoids one DB call per
	// candidate. Stubs that don't implement RoutingLookup leave the
	// map empty — `~o feed` predicates then evaluate to false and
	// those rows drop from the refined set.
	env := EvalEnv{}
	if astHasRouting(c.AST) {
		if rl, ok := local.(RoutingLookup); ok {
			env.Routing = make(map[string]string, len(candidates))
			for _, cand := range candidates {
				addr := strings.ToLower(strings.TrimSpace(cand.FromAddress))
				if addr == "" {
					continue
				}
				if _, seen := env.Routing[addr]; seen {
					continue
				}
				dest, err := rl.GetSenderRouting(ctx, opts.AccountID, addr)
				if err != nil {
					return nil, fmt.Errorf("two-stage routing lookup: %w", err)
				}
				env.Routing[addr] = dest
			}
		}
	}

	// Refine: pull each candidate from the local cache and
	// evaluate the AST against the in-memory envelope. Misses
	// (deep archive) are dropped silently per spec 08 §11.1.
	out := make([]string, 0, len(candidates))
	for _, cand := range candidates {
		m, err := local.GetMessage(ctx, cand.ID)
		if err != nil || m == nil {
			continue
		}
		if EvaluateInMemoryEnv(c.AST, m, env) {
			out = append(out, m.ID)
		}
	}
	return out, nil
}

// astHasRouting reports whether the AST contains any FieldRouting
// predicate. Used by executeTwoStage to skip the routing pre-load
// when the pattern doesn't need it.
func astHasRouting(n Node) bool {
	found := false
	walk(n, func(node Node) {
		if p, ok := node.(Predicate); ok && p.Field == FieldRouting {
			found = true
		}
	})
	return found
}

func idsOf(ms []store.Message) []string {
	out := make([]string, 0, len(ms))
	for _, m := range ms {
		out = append(out, m.ID)
	}
	return out
}

// intersectIDs returns the order-preserved intersection of a and
// b. Order follows a (the $filter result is the structural
// "primary" set; $search refines).
func intersectIDs(a, b []string) []string {
	if len(a) == 0 || len(b) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(b))
	for _, id := range b {
		set[id] = struct{}{}
	}
	out := make([]string, 0, len(a))
	for _, id := range a {
		if _, ok := set[id]; ok {
			out = append(out, id)
		}
	}
	return out
}
