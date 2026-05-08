package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// SenderRouting is one row of the sender_routing table — a per-sender,
// per-account assignment to one of the four routing destinations.
type SenderRouting struct {
	EmailAddress string
	AccountID    int64
	Destination  string
	AddedAt      time.Time
}

// Routing destination string constants. The four values are a fixed
// contract with the UI, CLI, and pattern operator — `destination IN
// (…)` CHECK constraint in migration 011_sender_routing.sql enforces
// the same set at the SQLite layer.
const (
	RoutingImbox      = "imbox"
	RoutingFeed       = "feed"
	RoutingPaperTrail = "paper_trail"
	RoutingScreener   = "screener"
)

// ErrInvalidDestination is returned by SetSenderRouting when the
// destination string is outside the allowed four-value set.
var ErrInvalidDestination = errors.New("store: invalid routing destination")

// ErrInvalidAddress is returned when the email_address would violate
// the migration's `length > 0` CHECK after NormalizeEmail. Empty,
// whitespace-only, and display-name-form (`"Bob" <bob@…>`) addresses
// land here — callers (CLI especially) must pass the bare address.
var ErrInvalidAddress = errors.New("store: invalid email address")

// NormalizeEmail lowercases + trims an email address for use as a
// sender_routing key. ASCII-equivalent only — `strings.ToLower`
// passes non-ASCII through unchanged, so a user routing
// `user@münchen.de` will not match the punycode form
// `user@xn--mnchen-3ya.de` or vice versa. Documented as a known v1
// limit per spec 23 §8; full IDNA / RFC 5895 normalization is a
// follow-up.
//
// `messages.from_address` is NOT normalised at write time (Graph
// returns whatever case the sender chose). The routing JOIN uses
// `lower(trim(from_address))` to bridge the asymmetry; the
// `idx_messages_from_lower` expression index covers it.
func NormalizeEmail(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// validRoutingDestination reports whether dest is one of the four
// allowed values.
func validRoutingDestination(dest string) bool {
	switch dest {
	case RoutingImbox, RoutingFeed, RoutingPaperTrail, RoutingScreener:
		return true
	}
	return false
}

// SetSenderRouting upserts a (account, sender) → destination row. The
// emailAddress is normalised via NormalizeEmail before write; the
// destination must be one of the four allowed strings. Returns the
// PRIOR destination ("" when the sender was unrouted) so callers can
// disambiguate "newly routed" / "reassigned" / "no-op (already
// routed here)" without a separate query.
//
// Read-then-write protocol (spec 23 §5.7): a GetSenderRouting first
// short-circuits when prior == destination — no SQL write, no
// added_at bump, and the dispatch caller can skip the list-pane
// reload (visible flicker for no semantic change). Do NOT simplify
// to a single `INSERT … ON CONFLICT DO UPDATE`; the read-first is
// intentional.
func (s *store) SetSenderRouting(ctx context.Context, accountID int64, emailAddress, destination string) (string, error) {
	if !validRoutingDestination(destination) {
		return "", fmt.Errorf("%w: %q", ErrInvalidDestination, destination)
	}
	addr := NormalizeEmail(emailAddress)
	if addr == "" || strings.ContainsAny(addr, "<>\"") {
		return "", fmt.Errorf("%w: %q", ErrInvalidAddress, emailAddress)
	}
	prior, err := s.GetSenderRouting(ctx, accountID, addr)
	if err != nil {
		return "", err
	}
	if prior == destination {
		// No-op: same destination. Skip SQL + added_at bump per §5.7.
		return prior, nil
	}
	if prior == "" {
		_, err := s.db.ExecContext(ctx,
			`INSERT INTO sender_routing (email_address, account_id, destination, added_at)
			 VALUES (?, ?, ?, ?)`,
			addr, accountID, destination, time.Now().Unix())
		if err != nil {
			return "", err
		}
		return "", nil
	}
	_, err = s.db.ExecContext(ctx,
		`UPDATE sender_routing SET destination = ?, added_at = ?
		 WHERE email_address = ? AND account_id = ?`,
		destination, time.Now().Unix(), addr, accountID)
	if err != nil {
		return "", err
	}
	return prior, nil
}

// ClearSenderRouting removes the (account, sender) routing row.
// Returns the PRIOR destination ("" when the sender was unrouted; in
// that case the call is a successful no-op).
func (s *store) ClearSenderRouting(ctx context.Context, accountID int64, emailAddress string) (string, error) {
	addr := NormalizeEmail(emailAddress)
	if addr == "" {
		return "", fmt.Errorf("%w: %q", ErrInvalidAddress, emailAddress)
	}
	prior, err := s.GetSenderRouting(ctx, accountID, addr)
	if err != nil {
		return "", err
	}
	if prior == "" {
		return "", nil
	}
	_, err = s.db.ExecContext(ctx,
		`DELETE FROM sender_routing WHERE email_address = ? AND account_id = ?`,
		addr, accountID)
	if err != nil {
		return "", err
	}
	return prior, nil
}

// GetSenderRouting returns the destination for a sender, or "" when
// the sender is unrouted.
func (s *store) GetSenderRouting(ctx context.Context, accountID int64, emailAddress string) (string, error) {
	addr := NormalizeEmail(emailAddress)
	if addr == "" {
		return "", nil
	}
	var dest string
	err := s.db.QueryRowContext(ctx,
		`SELECT destination FROM sender_routing WHERE email_address = ? AND account_id = ?`,
		addr, accountID).Scan(&dest)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	return dest, nil
}

// ListSenderRoutings returns all rows for the account, optionally
// filtered to a single destination. Empty destination returns all.
// Ordered by destination then email_address.
func (s *store) ListSenderRoutings(ctx context.Context, accountID int64, destination string) ([]SenderRouting, error) {
	var (
		rows *sql.Rows
		err  error
	)
	if destination == "" {
		rows, err = s.db.QueryContext(ctx,
			`SELECT email_address, account_id, destination, added_at
			 FROM sender_routing WHERE account_id = ?
			 ORDER BY destination, email_address`,
			accountID)
	} else {
		if !validRoutingDestination(destination) {
			return nil, fmt.Errorf("%w: %q", ErrInvalidDestination, destination)
		}
		rows, err = s.db.QueryContext(ctx,
			`SELECT email_address, account_id, destination, added_at
			 FROM sender_routing WHERE account_id = ? AND destination = ?
			 ORDER BY email_address`,
			accountID, destination)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SenderRouting
	for rows.Next() {
		var r SenderRouting
		var added int64
		if err := rows.Scan(&r.EmailAddress, &r.AccountID, &r.Destination, &added); err != nil {
			return nil, err
		}
		r.AddedAt = time.Unix(added, 0)
		out = append(out, r)
	}
	return out, rows.Err()
}

// ListMessagesByRouting returns messages whose sender is routed to
// `destination` for the account, ordered by received_at DESC. The
// JOIN normalises messages.from_address via lower(trim(...)) so
// existing rows (which are stored as Graph returned them) match the
// lowercased+trimmed sender_routing.email_address. Honours
// excludeMuted matching spec 19 §5.3 default-folder behaviour.
//
// Argument shape: positional toggle rather than a Query struct
// because there is only one toggle today; promote to a struct if a
// second one (e.g. excludeRead) is added.
func (s *store) ListMessagesByRouting(ctx context.Context, accountID int64, destination string, limit int, excludeMuted bool) ([]Message, error) {
	if !validRoutingDestination(destination) {
		return nil, fmt.Errorf("%w: %q", ErrInvalidDestination, destination)
	}
	if limit <= 0 {
		limit = 100
	}
	// Walk the partial index idx_messages_account_received_routed
	// backward (received_at DESC), filter rows by the routing IN
	// predicate, and stop at LIMIT N — avoiding a full
	// matching-set sort. The matching `from_address IS NOT NULL`
	// predicate qualifies the partial index so SQLite picks it.
	// Verified with EXPLAIN QUERY PLAN in TestListMessagesByRoutingUsesIndex.
	q := `SELECT ` + messageColumns + `
		  FROM messages INDEXED BY idx_messages_account_received_routed
		  WHERE account_id = ?
		    AND from_address IS NOT NULL AND length(trim(from_address)) > 0
		    AND lower(trim(from_address)) IN (
		        SELECT email_address FROM sender_routing
		        WHERE account_id = ? AND destination = ?
		    )`
	if excludeMuted {
		q += `
		    AND (
		        messages.conversation_id IS NULL
		        OR messages.conversation_id = ''
		        OR NOT EXISTS (
		            SELECT 1 FROM muted_conversations mc
		            WHERE mc.conversation_id = messages.conversation_id
		              AND mc.account_id = messages.account_id
		        )
		    )`
	}
	q += `
		  ORDER BY received_at DESC
		  LIMIT ?`
	rows, err := s.db.QueryContext(ctx, q, accountID, accountID, destination, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		msg, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *msg)
	}
	return out, rows.Err()
}

// CountMessagesByRouting returns the count of messages whose sender
// is routed to destination. Used by one-off lookups (CLI, `inkwell
// route show`); the sidebar uses CountMessagesByRoutingAll instead.
func (s *store) CountMessagesByRouting(ctx context.Context, accountID int64, destination string, excludeMuted bool) (int, error) {
	if !validRoutingDestination(destination) {
		return 0, fmt.Errorf("%w: %q", ErrInvalidDestination, destination)
	}
	// IN-subquery form: the planner uses idx_messages_from_lower to
	// scan messages once and bloom-filter against the sender_routing
	// shortlist. JOIN-driven attempts didn't pick the right plan
	// even with INDEXED BY hints, leaving all-messages scans on
	// either side.
	q := `SELECT COUNT(*) FROM messages
		  WHERE account_id = ?
		    AND lower(trim(from_address)) IN (
		        SELECT email_address FROM sender_routing
		        WHERE account_id = ? AND destination = ?
		    )`
	if excludeMuted {
		q += `
		    AND (
		        messages.conversation_id IS NULL
		        OR messages.conversation_id = ''
		        OR NOT EXISTS (
		            SELECT 1 FROM muted_conversations mc
		            WHERE mc.conversation_id = messages.conversation_id
		              AND mc.account_id = messages.account_id
		        )
		    )`
	}
	var n int
	err := s.db.QueryRowContext(ctx, q, accountID, accountID, destination).Scan(&n)
	if err != nil {
		return 0, err
	}
	return n, nil
}

// CountMessagesByRoutingAll returns counts for all four destinations
// in one batched GROUP BY query. Map keys are the destination
// strings; missing keys mean zero. Used by the sidebar refresh path
// (spec 23 §9 / BenchmarkSidebarBucketRefresh).
func (s *store) CountMessagesByRoutingAll(ctx context.Context, accountID int64, excludeMuted bool) (map[string]int, error) {
	// One CountMessagesByRouting call per destination. Four calls is
	// faster than a single GROUP BY because the IN-subquery plan
	// (covering idx_messages_from_lower with a bloom filter on
	// sender_routing for one destination) wins over the JOIN-form
	// GROUP BY, where the planner scans messages once per
	// sender_routing row. Spec 23 §9 is OK with the cumulative cost.
	out := map[string]int{
		RoutingImbox:      0,
		RoutingFeed:       0,
		RoutingPaperTrail: 0,
		RoutingScreener:   0,
	}
	for _, dest := range []string{RoutingImbox, RoutingFeed, RoutingPaperTrail, RoutingScreener} {
		n, err := s.CountMessagesByRouting(ctx, accountID, dest, excludeMuted)
		if err != nil {
			return nil, err
		}
		out[dest] = n
	}
	return out, nil
}
