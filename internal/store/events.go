package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// PutEvent upserts a single calendar event. Idempotent — repeated
// calls overwrite the cached fields with the latest values.
func (s *store) PutEvent(ctx context.Context, e Event) error {
	return s.PutEvents(ctx, []Event{e})
}

// PutEvents upserts a batch of events in one transaction. Used by
// the calendar fetch path to persist /me/calendarView results.
func (s *store) PutEvents(ctx context.Context, events []Event) error {
	if len(events) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	stmt, err := tx.PrepareContext(ctx, upsertEventSQL)
	if err != nil {
		return err
	}
	defer func() { _ = stmt.Close() }()
	now := time.Now()
	for _, e := range events {
		if e.CachedAt.IsZero() {
			e.CachedAt = now
		}
		if _, err := stmt.ExecContext(ctx,
			e.ID, e.AccountID,
			nullStr(e.Subject), nullStr(e.OrganizerName), nullStr(e.OrganizerAddress),
			e.Start.Unix(), e.End.Unix(), boolToInt(e.IsAllDay),
			nullStr(e.Location), nullStr(e.OnlineMeetingURL),
			nullStr(e.ShowAs), nullStr(e.ResponseStatus), nullStr(e.WebLink),
			e.CachedAt.Unix(),
		); err != nil {
			return fmt.Errorf("upsert event %s: %w", e.ID, err)
		}
	}
	return tx.Commit()
}

// ListEvents returns cached events matching the supplied query.
// Ordered by Start ASC. Used by the :cal modal to render the
// next-N events without a Graph round-trip.
func (s *store) ListEvents(ctx context.Context, q EventQuery) ([]Event, error) {
	var (
		clauses []string
		args    []any
	)
	clauses = append(clauses, "account_id = ?")
	args = append(args, q.AccountID)
	if !q.Start.IsZero() {
		clauses = append(clauses, "end_at > ?")
		args = append(args, q.Start.Unix())
	}
	if !q.End.IsZero() {
		clauses = append(clauses, "start_at < ?")
		args = append(args, q.End.Unix())
	}
	limit := q.Limit
	if limit <= 0 {
		limit = 200
	}
	args = append(args, limit)
	// #nosec G202 — clauses come from a fixed set of column names; args bind via `?`.
	query := "SELECT " + eventColumns + " FROM events WHERE " +
		strings.Join(clauses, " AND ") +
		" ORDER BY start_at ASC LIMIT ?"
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Event
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *e)
	}
	return out, rows.Err()
}

// DeleteEventsBefore removes cached events whose start_at is before
// the supplied timestamp. Used by the window-slide pass to drop
// stale yesterday entries on a midnight rollover.
func (s *store) DeleteEventsBefore(ctx context.Context, accountID int64, before time.Time) error {
	_, err := s.db.ExecContext(ctx,
		"DELETE FROM events WHERE account_id = ? AND start_at < ?",
		accountID, before.Unix())
	return err
}

// DeleteEvent removes a single event by ID. Used by the delta-sync
// @removed path so Graph-deleted events don't linger in the cache.
func (s *store) DeleteEvent(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM events WHERE id = ?", id)
	return err
}

const eventColumns = `
	id, account_id, COALESCE(subject, ''),
	COALESCE(organizer_name, ''), COALESCE(organizer_address, ''),
	start_at, end_at, is_all_day,
	COALESCE(location, ''), COALESCE(online_meeting_url, ''),
	COALESCE(show_as, ''), COALESCE(response_status, ''), COALESCE(web_link, ''),
	cached_at
`

const upsertEventSQL = `
INSERT INTO events (
	id, account_id, subject, organizer_name, organizer_address,
	start_at, end_at, is_all_day, location, online_meeting_url,
	show_as, response_status, web_link, cached_at
) VALUES (?,?,?,?,?, ?,?,?,?,?, ?,?,?,?)
ON CONFLICT(id) DO UPDATE SET
	account_id = excluded.account_id,
	subject = excluded.subject,
	organizer_name = excluded.organizer_name,
	organizer_address = excluded.organizer_address,
	start_at = excluded.start_at,
	end_at = excluded.end_at,
	is_all_day = excluded.is_all_day,
	location = excluded.location,
	online_meeting_url = excluded.online_meeting_url,
	show_as = excluded.show_as,
	response_status = excluded.response_status,
	web_link = excluded.web_link,
	cached_at = excluded.cached_at
`

// PutEventAttendees replaces all attendees for eventID atomically.
// DELETE + INSERT in one transaction so partial states never persist.
func (s *store) PutEventAttendees(ctx context.Context, eventID string, attendees []EventAttendee) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, "DELETE FROM event_attendees WHERE event_id = ?", eventID); err != nil {
		return err
	}
	if len(attendees) > 0 {
		stmt, err := tx.PrepareContext(ctx, `
			INSERT INTO event_attendees (event_id, address, name, type, status)
			VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(event_id, address) DO UPDATE SET
				name = excluded.name,
				type = excluded.type,
				status = excluded.status`)
		if err != nil {
			return err
		}
		defer func() { _ = stmt.Close() }()
		for _, a := range attendees {
			if _, err := stmt.ExecContext(ctx, eventID, a.Address, nullStr(a.Name), nullStr(a.Type), nullStr(a.Status)); err != nil {
				return fmt.Errorf("insert attendee %s: %w", a.Address, err)
			}
		}
	}
	return tx.Commit()
}

// ListEventAttendees returns cached attendees for eventID, ordered by
// address. Returns nil (not an error) when none are cached yet.
func (s *store) ListEventAttendees(ctx context.Context, eventID string) ([]EventAttendee, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT event_id, address, COALESCE(name,''), COALESCE(type,''), COALESCE(status,'')
		FROM event_attendees WHERE event_id = ? ORDER BY address`, eventID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []EventAttendee
	for rows.Next() {
		var a EventAttendee
		if err := rows.Scan(&a.EventID, &a.Address, &a.Name, &a.Type, &a.Status); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func scanEvent(row rowScanner) (*Event, error) {
	var e Event
	var startAt, endAt, cachedAt int64
	var isAllDay int
	if err := row.Scan(
		&e.ID, &e.AccountID, &e.Subject,
		&e.OrganizerName, &e.OrganizerAddress,
		&startAt, &endAt, &isAllDay,
		&e.Location, &e.OnlineMeetingURL,
		&e.ShowAs, &e.ResponseStatus, &e.WebLink,
		&cachedAt,
	); err != nil {
		return nil, err
	}
	e.Start = time.Unix(startAt, 0).UTC()
	e.End = time.Unix(endAt, 0).UTC()
	e.IsAllDay = isAllDay != 0
	e.CachedAt = time.Unix(cachedAt, 0)
	return &e, nil
}
