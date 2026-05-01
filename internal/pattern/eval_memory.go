package pattern

import (
	"strings"

	"github.com/eugenelim/inkwell/internal/store"
)

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
// already on the cached envelope.
func EvaluateInMemory(root Node, m *store.Message) bool {
	if root == nil || m == nil {
		return false
	}
	return evalMem(root, m)
}

func evalMem(n Node, m *store.Message) bool {
	switch v := n.(type) {
	case And:
		return evalMem(v.L, m) && evalMem(v.R, m)
	case Or:
		return evalMem(v.L, m) || evalMem(v.R, m)
	case Not:
		return !evalMem(v.X, m)
	case Predicate:
		return evalMemPredicate(v, m)
	}
	return false
}

func evalMemPredicate(p Predicate, m *store.Message) bool {
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
	}
	return false
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
