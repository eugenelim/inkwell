package pattern

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/eugenelim/inkwell/internal/store"
)

// stubLocal implements LocalSearcher with canned responses.
type stubLocal struct {
	predResults    []store.Message
	predLastWhere  string
	predLastArgs   []any
	predLastLimit  int
	predLastAcct   int64
	predErr        error
	getMessageByID map[string]*store.Message
}

func (s *stubLocal) SearchByPredicate(_ context.Context, accountID int64, where string, args []any, limit int) ([]store.Message, error) {
	s.predLastAcct = accountID
	s.predLastWhere = where
	s.predLastArgs = args
	s.predLastLimit = limit
	return s.predResults, s.predErr
}
func (s *stubLocal) GetMessage(_ context.Context, id string) (*store.Message, error) {
	if s.getMessageByID == nil {
		return nil, store.ErrNotFound
	}
	if m, ok := s.getMessageByID[id]; ok {
		return m, nil
	}
	return nil, store.ErrNotFound
}

// stubGraph implements GraphService with canned responses for
// $search and $filter.
type stubGraph struct {
	searchResults []store.Message
	searchErr     error
	searchCalls   int
	searchLastQ   ServerQuery

	filterResults []store.Message
	filterErr     error
	filterCalls   int
	filterLastQ   ServerQuery
}

func (s *stubGraph) SearchMessages(_ context.Context, q ServerQuery) ([]store.Message, error) {
	s.searchCalls++
	s.searchLastQ = q
	return s.searchResults, s.searchErr
}
func (s *stubGraph) FilterMessages(_ context.Context, q ServerQuery) ([]store.Message, error) {
	s.filterCalls++
	s.filterLastQ = q
	return s.filterResults, s.filterErr
}

// TestExecuteLocalOnlyDispatchesSearchByPredicate covers the
// happy path for the local-only strategy: SearchByPredicate gets
// the rendered SQL + args, returns matching messages, IDs flow
// out.
func TestExecuteLocalOnlyDispatchesSearchByPredicate(t *testing.T) {
	c, err := Compile("~A", CompileOptions{LocalOnly: true})
	require.NoError(t, err)
	require.Equal(t, StrategyLocalOnly, c.Strategy)

	local := &stubLocal{predResults: []store.Message{{ID: "m-1"}, {ID: "m-2"}}}
	ids, err := Execute(context.Background(), c, local, nil, ExecuteOptions{AccountID: 42, LocalMatchLimit: 1000})
	require.NoError(t, err)
	require.Equal(t, []string{"m-1", "m-2"}, ids)
	require.Equal(t, int64(42), local.predLastAcct)
	require.Contains(t, local.predLastWhere, "has_attachments")
	require.Equal(t, 1000, local.predLastLimit)
}

// TestExecuteServerFilterDispatchesFilterMessages mirrors the
// LocalOnly test but for the StrategyServerFilter path.
func TestExecuteServerFilterDispatchesFilterMessages(t *testing.T) {
	c, err := Compile("~U", CompileOptions{})
	require.NoError(t, err)
	require.Equal(t, StrategyServerFilter, c.Strategy)

	graph := &stubGraph{filterResults: []store.Message{{ID: "m-srv-1"}}}
	ids, err := Execute(context.Background(), c, nil, graph, ExecuteOptions{ServerCandidateLimit: 250})
	require.NoError(t, err)
	require.Equal(t, []string{"m-srv-1"}, ids)
	require.Equal(t, 1, graph.filterCalls)
	require.Equal(t, "isRead eq true", graph.filterLastQ.Filter)
	require.Equal(t, 250, graph.filterLastQ.Top)
}

// TestExecuteServerSearchDispatchesSearchMessages — same shape
// for $search.
func TestExecuteServerSearchDispatchesSearchMessages(t *testing.T) {
	c, err := Compile(`~b "action required"`, CompileOptions{})
	require.NoError(t, err)
	require.Equal(t, StrategyServerSearch, c.Strategy)

	graph := &stubGraph{searchResults: []store.Message{{ID: "m-srv-1"}, {ID: "m-srv-2"}}}
	ids, err := Execute(context.Background(), c, nil, graph, ExecuteOptions{})
	require.NoError(t, err)
	require.Equal(t, []string{"m-srv-1", "m-srv-2"}, ids)
	require.Equal(t, 1, graph.searchCalls)
	require.Equal(t, `body:"action required"`, graph.searchLastQ.Search)
}

// TestExecuteTwoStageRefinesAgainstCachedEnvelopes is the spec
// 08 §11 invariant: server returns candidates, in-memory eval
// against the cached envelope filters by the predicates $search
// can't see.
//
// `~b *deck*` is intentionally wildcarded so the in-memory
// matcher's MatchContains branch fires; bare `~b deck` parses
// as MatchExact and would fail the substring test against a
// non-equal body — which is the same behaviour as the local
// SQL `body_preview = 'deck'` shape today.
func TestExecuteTwoStageRefinesAgainstCachedEnvelopes(t *testing.T) {
	c, err := Compile(`~b *deck* & ~F`, CompileOptions{})
	require.NoError(t, err)
	require.Equal(t, StrategyTwoStage, c.Strategy)

	cachedFlagged := store.Message{ID: "m-1", FlagStatus: "flagged", BodyPreview: "the deck has it"}
	cachedNotFlagged := store.Message{ID: "m-2", FlagStatus: "", BodyPreview: "the deck has it"}
	cachedAlsoFlagged := store.Message{ID: "m-3", FlagStatus: "flagged", BodyPreview: "the deck has it"}

	local := &stubLocal{getMessageByID: map[string]*store.Message{
		"m-1": &cachedFlagged,
		"m-2": &cachedNotFlagged,
		"m-3": &cachedAlsoFlagged,
	}}
	graph := &stubGraph{searchResults: []store.Message{
		{ID: "m-1"},
		{ID: "m-2"},
		{ID: "m-3"},
		{ID: "m-deep-archive"}, // not in local cache → silently dropped per §11.1
	}}

	ids, err := Execute(context.Background(), c, local, graph, ExecuteOptions{ServerCandidateLimit: 500})
	require.NoError(t, err)
	require.Equal(t, []string{"m-1", "m-3"}, ids,
		"only flagged candidates pass the in-memory refinement; deep-archive miss is dropped")
}

// TestExecuteServerHybridIntersectsTwoQueries covers the spec
// 08 §7.3 hybrid path: $filter and $search run separately and
// the IDs INTERSECT in memory.
func TestExecuteServerHybridIntersectsTwoQueries(t *testing.T) {
	// Hand-craft a Compiled with StrategyServerHybrid since the
	// planner doesn't pick this strategy organically yet — the
	// path exists for future patterns that need it.
	c := &Compiled{
		AST:      Predicate{Field: FieldHasAttachments, Value: EmptyValue{}},
		Strategy: StrategyServerHybrid,
		Plan: CompilationPlan{
			GraphFilter: "hasAttachments eq true",
			GraphSearch: "subject:deck",
		},
	}
	graph := &stubGraph{
		filterResults: []store.Message{{ID: "m-1"}, {ID: "m-2"}, {ID: "m-3"}},
		searchResults: []store.Message{{ID: "m-2"}, {ID: "m-3"}, {ID: "m-99"}},
	}
	ids, err := Execute(context.Background(), c, nil, graph, ExecuteOptions{})
	require.NoError(t, err)
	require.Equal(t, []string{"m-2", "m-3"}, ids,
		"intersect preserves $filter result order")
}

// TestExecuteRejectsServerErrorPropagation verifies graph
// failures surface to the caller (not silently swallowed).
func TestExecuteRejectsServerErrorPropagation(t *testing.T) {
	c, err := Compile("~U", CompileOptions{})
	require.NoError(t, err)
	graph := &stubGraph{filterErr: errors.New("graph boom")}
	_, err = Execute(context.Background(), c, nil, graph, ExecuteOptions{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "graph boom")
}

// TestExecuteServerFilterRequiresGraphService — calling with
// nil graph for a server-strategy compiled is a programming
// error, not a fall-through-to-local.
func TestExecuteServerFilterRequiresGraphService(t *testing.T) {
	c, err := Compile("~U", CompileOptions{})
	require.NoError(t, err)
	_, err = Execute(context.Background(), c, nil, nil, ExecuteOptions{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "GraphService")
}

// TestEvaluateInMemoryHandlesAllPredicateShapes is a sanity-
// check sweep over every Field family; the in-memory evaluator
// is the TwoStage refinement engine and must agree with the
// SQL evaluator on the canonical envelope cases.
func TestEvaluateInMemoryHandlesAllPredicateShapes(t *testing.T) {
	now := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	m := &store.Message{
		ID:             "m-1",
		FromAddress:    "bob@vendor.invalid",
		FromName:       "Bob Acme",
		Subject:        "Q4 budget review",
		BodyPreview:    "the deck is attached",
		IsRead:         false,
		FlagStatus:     "flagged",
		HasAttachments: true,
		Categories:     []string{"Work"},
		Importance:     "high",
		ToAddresses:    []store.EmailAddress{{Address: "alice@example.invalid"}},
		ReceivedAt:     now,
	}
	cases := []struct {
		src  string
		want bool
	}{
		{"~A", true},
		{"~N", true},
		{"~U", false},
		{"~F", true},
		{"~i high", true},
		{"~i low", false},
		{"~G Work", true},
		{"~G Personal", false},
		{"~f bob@vendor.invalid", true},
		{"~f *@vendor.invalid", true},
		{"~f *@other.invalid", false},
		// Exact-match: bare `~s X` parses as MatchExact, so it
		// only matches when the subject IS exactly "X" (mirrors
		// the local SQL `subject = ?` shape). Wildcarded forms
		// (`~s *X*`) hit MatchContains.
		{"~s budget", false}, // "Q4 budget review" != "budget"
		{"~s *budget*", true},
		{"~s Q4*", true},
		{"~s nothing-matches", false},
		{"~b *deck*", true},
		{"~b unicorn", false},
		{"~B *forecast*", false},
		{"~B *budget*", true},
		{"~t alice@example.invalid", true},
		{"~r alice@example.invalid", true},
		{"~A & ~F", true},
		{"~A & ~U", false},
		{"~A | ~U", true},
		{"! ~U", true},
	}
	for _, c := range cases {
		root, err := Parse(c.src)
		require.NoError(t, err, "src=%q", c.src)
		got := EvaluateInMemory(root, m)
		require.Equal(t, c.want, got, "src=%q", c.src)
	}
}
