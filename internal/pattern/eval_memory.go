package pattern

import (
	"strings"

	"github.com/eugenelim/inkwell/internal/store"
)

// EvalEnv carries optional context the in-memory evaluator
// consults for store-backed predicates. Today only `~o` (routing)
// uses it — Routing is the lowercased+trimmed sender address →
// destination map for the candidate set. nil / empty map means
// every routing lookup misses, so `~o feed` evaluates to false
// (and `~o none` to true) — the in-memory path cannot satisfy
// `~o` without help from the caller.
type EvalEnv struct {
	Routing map[string]string
}

// EvaluateInMemory walks the AST against a single in-memory
// message. Used by [Execute]'s TwoStage path: the server returns
// a candidate set; the structural predicates (~N / ~U / ~F /
// ~i / ~y) refine locally. Spec 08 §11.
//
// Returns true when every predicate matches — same boolean shape
// as the SQL evaluator but evaluated in Go.
//
// Predicates the in-memory path cannot evaluate (~h header
// lookup) return false — TwoStage only refines using fields
// already on the cached envelope. `~o` evaluates against an
// empty EvalEnv; use [EvaluateInMemoryEnv] to thread routing.
func EvaluateInMemory(root Node, m *store.Message) bool {
	return EvaluateInMemoryEnv(root, m, EvalEnv{})
}

// EvaluateInMemoryEnv is [EvaluateInMemory] with a context for
// store-backed predicates. Spec 23 §4.3 / §6: TwoStage refinement
// preloads sender_routing into env.Routing, then this evaluator
// can satisfy `~o feed` against the cached envelope without a DB
// round-trip per message.
func EvaluateInMemoryEnv(root Node, m *store.Message, env EvalEnv) bool {
	if root == nil || m == nil {
		return false
	}
	return evalMem(root, m, env)
}

func evalMem(n Node, m *store.Message, env EvalEnv) bool {
	switch v := n.(type) {
	case And:
		return evalMem(v.L, m, env) && evalMem(v.R, m, env)
	case Or:
		return evalMem(v.L, m, env) || evalMem(v.R, m, env)
	case Not:
		return !evalMem(v.X, m, env)
	case Predicate:
		return evalMemPredicate(v, m, env)
	}
	return false
}

func evalMemPredicate(p Predicate, m *store.Message, env EvalEnv) bool {
	switch p.Field {
	case FieldHasAttachments:
		return m.HasAttachments
	case FieldUnread:
		return !m.IsRead
	case FieldRead:
		return m.IsRead
	case FieldFlagged:
		return m.FlagStatus == "flagged"
	}

	switch v := p.Value.(type) {
	case StringValue:
		return evalMemString(p.Field, v, m)
	case DateValue:
		return evalMemDate(p.Field, v, m)
	case RoutingValue:
		return evalMemRouting(v, m, env)
	}
	return false
}

// evalMemRouting evaluates `~o <dest>` against env.Routing. The
// candidate's lowercased+trimmed from_address is the lookup key.
// `~o none` matches when the address is unrouted (no map entry or
// empty value). Other destinations match exact string equality
// against the map's value.
func evalMemRouting(v RoutingValue, m *store.Message, env EvalEnv) bool {
	addr := strings.ToLower(strings.TrimSpace(m.FromAddress))
	dest := env.Routing[addr]
	if v.Destination == "none" {
		return dest == ""
	}
	return dest == v.Destination
}

func evalMemString(f Field, v StringValue, m *store.Message) bool {
	switch f {
	case FieldFrom:
		return matchString(m.FromAddress, v) || matchString(m.FromName, v)
	case FieldTo:
		return anyAddrMatches(m.ToAddresses, v)
	case FieldCc:
		return anyAddrMatches(m.CcAddresses, v)
	case FieldRecipient:
		return anyAddrMatches(m.ToAddresses, v) || anyAddrMatches(m.CcAddresses, v)
	case FieldSubject:
		return matchString(m.Subject, v)
	case FieldBody:
		return matchString(m.BodyPreview, v)
	case FieldSubjectOrBody:
		return matchString(m.Subject, v) || matchString(m.BodyPreview, v)
	case FieldFolder:
		return strings.EqualFold(m.FolderID, v.Raw)
	case FieldCategory:
		for _, c := range m.Categories {
			if matchString(c, v) {
				return true
			}
		}
		return false
	case FieldImportance:
		return strings.EqualFold(m.Importance, v.Raw)
	case FieldInferenceCls:
		return strings.EqualFold(m.InferenceClass, v.Raw)
	case FieldConversation:
		return m.ConversationID == v.Raw
	case FieldHeader:
		// Headers aren't on the cached envelope; in-memory
		// evaluation can't satisfy this. Spec 08 §11: TwoStage
		// refinement skips ~h.
		return false
	}
	return false
}

func evalMemDate(f Field, v DateValue, m *store.Message) bool {
	t := m.ReceivedAt
	if f == FieldDateSent {
		t = m.SentAt
	}
	switch v.Op {
	case DateBefore:
		return t.Before(v.At)
	case DateBeforeEq:
		return t.Before(v.At) || t.Equal(v.At)
	case DateAfter:
		return t.After(v.At)
	case DateAfterEq:
		return t.After(v.At) || t.Equal(v.At)
	case DateOn, DateRange:
		return (t.After(v.At) || t.Equal(v.At)) && t.Before(v.End)
	case DateWithinLast:
		return t.After(v.At) || t.Equal(v.At)
	}
	return false
}

// matchString applies the wildcard kind to the haystack. Match
// is case-insensitive (mirrors Graph's $filter semantics for
// contains/startswith/endswith). Empty haystack with empty
// needle is a hit; empty haystack with non-empty needle is a
// miss.
func matchString(haystack string, v StringValue) bool {
	h := strings.ToLower(haystack)
	n := strings.ToLower(v.Raw)
	switch v.Match {
	case MatchExact:
		return h == n
	case MatchPrefix:
		return strings.HasPrefix(h, n)
	case MatchSuffix:
		return strings.HasSuffix(h, n)
	case MatchContains:
		return strings.Contains(h, n)
	}
	return false
}

func anyAddrMatches(addrs []store.EmailAddress, v StringValue) bool {
	for _, a := range addrs {
		if matchString(a.Address, v) || matchString(a.Name, v) {
			return true
		}
	}
	return false
}
