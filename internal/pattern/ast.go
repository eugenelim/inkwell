// Package pattern implements the Mutt-inspired pattern language used
// to select messages for bulk operations and saved searches. See
// docs/specs/08-pattern-language/spec.md.
//
// v0.5.0 shipped the lexer, parser, AST, and a local-SQL evaluator;
// v0.18.0 (PR 9 of audit-drain) shipped the Compile / Execute API +
// strategy selector + Graph $filter / $search evaluators + the
// in-memory evaluator used by TwoStage refinement. Spec 08 is now
// at CI-shipped status pending the 100k-message bench + 10k-AST
// property test.
package pattern

import "time"

// Field enumerates the operator-targeted fields. Each ~x operator
// produces a Predicate node bound to one Field.
type Field int

const (
	// String / address fields.
	FieldFrom Field = iota
	FieldTo
	FieldCc
	FieldRecipient // ~r — to OR cc
	FieldSubject
	FieldBody
	FieldSubjectOrBody // ~B
	FieldHeader        // ~h (server-only)
	FieldFolder        // ~m
	FieldCategory      // ~G
	FieldImportance    // ~i — low|normal|high
	FieldInferenceCls  // ~y — focused|other
	FieldConversation  // ~v
	FieldRouting       // ~o — imbox|feed|paper_trail|screener|none (spec 23)

	// Date fields.
	FieldDateReceived // ~d
	FieldDateSent     // ~D

	// Boolean / no-arg fields.
	FieldHasAttachments // ~A
	FieldUnread         // ~N
	FieldRead           // ~U (the negation, kept as its own field for symmetry)
	FieldFlagged        // ~F
)

// MatchKind selects how a string predicate's argument is matched.
type MatchKind int

const (
	MatchExact    MatchKind = iota // no wildcards
	MatchPrefix                    // value*
	MatchSuffix                    // *value
	MatchContains                  // *value* or value with wildcard in middle
)

// DateOp selects which side of a moment a date predicate matches.
type DateOp int

const (
	DateBefore     DateOp = iota // <
	DateBeforeEq                 // <=
	DateAfter                    // >
	DateAfterEq                  // >=
	DateOn                       // on day (today, yesterday, exact date)
	DateRange                    // a..b
	DateWithinLast               // <Nd is read as "received >= now - Nd"; we tag it here for clarity
)

// Node is the marker interface implemented by every AST node.
type Node interface{ isNode() }

// And is a binary AND.
type And struct{ L, R Node }

// Or is a binary OR.
type Or struct{ L, R Node }

// Not is unary negation.
type Not struct{ X Node }

// Predicate matches a Field against an argument. Concrete shape varies
// by Field family — string, date, or no-arg — captured in the
// PredicateValue union below.
type Predicate struct {
	Field Field
	Value PredicateValue
}

// PredicateValue is the typed argument carried by a Predicate.
type PredicateValue interface{ isValue() }

// StringValue is the argument for from / to / cc / subject / body
// etc. Match is the wildcard kind; Raw is the literal value the user
// typed (without surrounding quotes).
type StringValue struct {
	Raw   string
	Match MatchKind
}

// DateValue is the argument for ~d / ~D.
type DateValue struct {
	Op DateOp
	// At is the anchor time (UTC). For DateRange, At is the start and
	// End is the end. For DateWithinLast, At is the threshold (now -
	// duration), and a "received >= At" clause is generated.
	At  time.Time
	End time.Time // only set for DateRange
}

// EmptyValue is the argument shape for no-arg predicates (~A, ~N, ~F, ~U).
type EmptyValue struct{}

// RoutingValue is the typed argument carried by a `~o` predicate
// (spec 23 §4.3). Destination is one of "imbox" / "feed" /
// "paper_trail" / "screener" / "none". The "none" sentinel matches
// senders with NO row in sender_routing (i.e., unrouted) and
// compiles to a NOT EXISTS form.
type RoutingValue struct {
	Destination string
}

func (And) isNode()       {}
func (Or) isNode()        {}
func (Not) isNode()       {}
func (Predicate) isNode() {}

func (StringValue) isValue()  {}
func (DateValue) isValue()    {}
func (EmptyValue) isValue()   {}
func (RoutingValue) isValue() {}
