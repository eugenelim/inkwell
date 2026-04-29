package pattern

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// nowFn is the clock the date parser uses. Tests override it.
var nowFn = time.Now

// parseDateValue understands the v0.5.0-supported date forms:
//
//	<30d   <=24h        within last N units (DateWithinLast)
//	>30d   >=12h        older than N units  (DateAfter / DateAfterEq, inverted)
//	<2026-01-01         absolute before
//	>=2026-01-01        absolute on-or-after
//	2026-03..2026-04    range (DateRange)
//	today | yesterday   named days (DateOn)
//
// The richer forms (this-week, last-month, etc.) land later when bulk
// operations actually need them. The minimum viable set above covers
// the most common cleanup patterns ("everything older than 90 days").
func parseDateValue(raw string) (DateValue, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return DateValue{}, fmt.Errorf("empty date argument")
	}
	now := nowFn()

	// Named days.
	switch raw {
	case "today":
		start := startOfDay(now)
		return DateValue{Op: DateOn, At: start, End: start.Add(24 * time.Hour)}, nil
	case "yesterday":
		end := startOfDay(now)
		start := end.Add(-24 * time.Hour)
		return DateValue{Op: DateOn, At: start, End: end}, nil
	}

	// Range: a..b
	if i := strings.Index(raw, ".."); i > 0 {
		a, errA := parseAbsoluteDate(raw[:i])
		b, errB := parseAbsoluteDate(raw[i+2:])
		if errA != nil {
			return DateValue{}, fmt.Errorf("range start: %w", errA)
		}
		if errB != nil {
			return DateValue{}, fmt.Errorf("range end: %w", errB)
		}
		// Inclusive on day boundaries: end is the day-after-b at 00:00.
		return DateValue{Op: DateRange, At: a, End: b.Add(24 * time.Hour)}, nil
	}

	// Comparison forms: <=, >=, <, >, then either a duration or an
	// absolute date.
	op, rest := DateOn, raw
	switch {
	case strings.HasPrefix(raw, "<="):
		op, rest = DateBeforeEq, raw[2:]
	case strings.HasPrefix(raw, ">="):
		op, rest = DateAfterEq, raw[2:]
	case strings.HasPrefix(raw, "<"):
		op, rest = DateBefore, raw[1:]
	case strings.HasPrefix(raw, ">"):
		op, rest = DateAfter, raw[1:]
	}

	// Duration form? Only meaningful with one of the comparison ops.
	if op != DateOn {
		if dur, ok, err := parseDuration(rest); ok {
			if err != nil {
				return DateValue{}, err
			}
			anchor := now.Add(-dur)
			// `<30d` ("within last 30 days") is most natural read as
			// "received_at >= now-30d". Tag with DateWithinLast so the
			// evaluator knows to flip the comparison.
			if op == DateBefore || op == DateBeforeEq {
				return DateValue{Op: DateWithinLast, At: anchor}, nil
			}
			// `>30d` ("older than 30 days") = received_at < now-30d.
			return DateValue{Op: DateBefore, At: anchor}, nil
		}
	}

	// Absolute YYYY-MM-DD.
	t, err := parseAbsoluteDate(rest)
	if err != nil {
		return DateValue{}, err
	}
	if op == DateOn {
		return DateValue{Op: DateOn, At: t, End: t.Add(24 * time.Hour)}, nil
	}
	return DateValue{Op: op, At: t}, nil
}

// parseDuration recognises NNNu where u is s|m|h|d|w|mo|y. The minutes
// suffix is `m` (single letter), months is `mo`. Returns (dur, true,
// nil) on hit, (0, false, nil) if the input clearly isn't a duration,
// (0, true, err) when it looked like a duration but parsing failed.
func parseDuration(s string) (time.Duration, bool, error) {
	if s == "" {
		return 0, false, nil
	}
	// Find unit: scan from the right while we have letters.
	i := len(s)
	for i > 0 && !isDigit(s[i-1]) {
		i--
	}
	if i == 0 || i == len(s) {
		return 0, false, nil
	}
	num, unit := s[:i], s[i:]
	n, err := strconv.Atoi(num)
	if err != nil {
		return 0, false, nil
	}
	switch unit {
	case "s":
		return time.Duration(n) * time.Second, true, nil
	case "m":
		return time.Duration(n) * time.Minute, true, nil
	case "h":
		return time.Duration(n) * time.Hour, true, nil
	case "d":
		return time.Duration(n) * 24 * time.Hour, true, nil
	case "w":
		return time.Duration(n) * 7 * 24 * time.Hour, true, nil
	case "mo":
		return time.Duration(n) * 30 * 24 * time.Hour, true, nil // approximation
	case "y":
		return time.Duration(n) * 365 * 24 * time.Hour, true, nil
	}
	return 0, true, fmt.Errorf("unknown duration unit %q", unit)
}

func parseAbsoluteDate(s string) (time.Time, error) {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}, fmt.Errorf("expected YYYY-MM-DD, got %q", s)
	}
	return t.UTC(), nil
}

func startOfDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }
