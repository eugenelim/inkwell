package ui

import (
	"sort"
	"strings"
	"unicode"
)

// titleSynonymSep is the rune sentinel placed between a row's Title
// and its joined Synonyms inside the matcher's flattened search
// string. U+00A6 is unlikely to appear in any title or query; the
// matcher excludes it explicitly so a user-typed query rune never
// matches the boundary.
const titleSynonymSep = '¦'

// paletteRowCache holds per-row data the matcher recomputes only when
// the row list is rebuilt (palette Open). Stored as a sibling slice
// keyed by row index so PaletteRow stays a plain data record.
type paletteRowCache struct {
	runes      []rune // lowercased Title + sep + lowercased Synonyms
	origRunes  []rune // case-preserving Title + sep + Synonyms (for uppercase bonus)
	titleEnd   int    // index in runes immediately after the last Title rune (sep position)
	titleRunes int    // rune count of the Title alone
}

// scoredRow is the matcher output: the underlying row plus its score
// and (for future inline highlighting) the title-rune indices that
// matched. Sorted descending by score, then ascending by Title.
type scoredRow struct {
	row   PaletteRow
	score int
	hits  []int
}

// buildRowCaches returns one paletteRowCache per row in rows. Called
// once per palette Open; cached on the model alongside rows.
func buildRowCaches(rows []PaletteRow) []paletteRowCache {
	caches := make([]paletteRowCache, len(rows))
	for i, r := range rows {
		title := r.Title
		var b strings.Builder
		b.WriteString(title)
		if len(r.Synonyms) > 0 {
			b.WriteRune(' ')
			b.WriteRune(titleSynonymSep)
			b.WriteRune(' ')
			b.WriteString(strings.Join(r.Synonyms, " "))
		}
		orig := []rune(b.String())
		lower := []rune(strings.ToLower(b.String()))
		titleRunes := len([]rune(title))
		// titleEnd is the index of the sep rune (or len(runes) when no
		// synonyms are present). Matches strictly before titleEnd
		// belong to the title.
		titleEnd := len(lower)
		for j, r := range lower {
			if r == titleSynonymSep {
				titleEnd = j
				break
			}
		}
		caches[i] = paletteRowCache{
			runes:      lower,
			origRunes:  orig,
			titleEnd:   titleEnd,
			titleRunes: titleRunes,
		}
	}
	return caches
}

// matchAndScore runs the fuzzy matcher across rows + caches against
// the lowercased query q, returning rows that contained every query
// rune as a subsequence. recents is consulted for the frecency
// boost — earlier index = higher boost.
func matchAndScore(rows []PaletteRow, caches []paletteRowCache, q string, recents []string) []scoredRow {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return matchEmpty(rows, recents)
	}
	qr := []rune(q)
	out := make([]scoredRow, 0, len(rows))
	for i, row := range rows {
		c := caches[i]
		score, hits, ok := scoreRow(qr, c)
		if !ok {
			continue
		}
		score += recencyBoost(row.ID, recents)
		if !row.Available.OK {
			score -= 50
		}
		out = append(out, scoredRow{row: row, score: score, hits: hits})
	}
	sort.SliceStable(out, func(a, b int) bool {
		if out[a].score != out[b].score {
			return out[a].score > out[b].score
		}
		// On score ties, prefer commands over folders / saved
		// searches — the palette is action-oriented.
		aSec := sectionOrder(out[a].row.Section)
		bSec := sectionOrder(out[b].row.Section)
		if aSec != bSec {
			return aSec < bSec
		}
		// Shorter titles outrank longer ones on the same score —
		// "Filter…" beats "Filter (all folders)…" for query "filter".
		aLen := len(out[a].row.Title)
		bLen := len(out[b].row.Title)
		if aLen != bLen {
			return aLen < bLen
		}
		return strings.ToLower(out[a].row.Title) < strings.ToLower(out[b].row.Title)
	})
	return out
}

// scoreRow walks q over c.runes left-to-right awarding bonuses per
// §4.4 of spec 22. Returns score + hit indices, or ok=false when any
// query rune has no match.
func scoreRow(qr []rune, c paletteRowCache) (int, []int, bool) {
	hits := make([]int, 0, len(qr))
	score := 0
	prevIdx := -1
	for qi, qrune := range qr {
		// Find the next matching rune in c.runes after prevIdx,
		// skipping the title/synonym sentinel.
		idx := -1
		for j := prevIdx + 1; j < len(c.runes); j++ {
			r := c.runes[j]
			if r == titleSynonymSep {
				continue
			}
			if r == qrune {
				idx = j
				break
			}
		}
		if idx < 0 {
			return 0, nil, false
		}
		// Penalty for skipped runes between matches.
		if prevIdx >= 0 {
			gap := idx - prevIdx - 1
			if gap > 0 {
				score -= gap
			}
		}
		// Per-rune bonuses.
		bonus := 1
		if idx == 0 && qi == 0 {
			bonus += 30 // full prefix on title
		}
		if idx > 0 {
			prev := c.runes[idx-1]
			if prev == ' ' || prev == '_' || prev == ':' || prev == '/' || prev == '-' {
				bonus += 12 // start-of-word
			}
		}
		// Uppercase in original (case-preserving) string.
		if idx < len(c.origRunes) {
			or := c.origRunes[idx]
			if unicode.IsUpper(or) {
				bonus += 8
			}
		}
		// Consecutive run bonus.
		if prevIdx >= 0 && idx == prevIdx+1 {
			bonus += 6
		}
		// In-title bonus: match falls strictly inside the Title rune
		// range, not in the synonym tail.
		if idx < c.titleEnd {
			bonus += 10
		}
		score += bonus
		hits = append(hits, idx)
		prevIdx = idx
	}
	return score, hits, true
}

// recencyBoost returns the frecency contribution for id given the MRU
// list. Index 0 (most recent) → +60, decays by 8 per slot. Not in
// recents → 0.
func recencyBoost(id string, recents []string) int {
	for i, rid := range recents {
		if rid == id {
			b := 60 - 8*i
			if b < 0 {
				b = 0
			}
			return b
		}
	}
	return 0
}

// matchEmpty returns rows in the empty-buffer order: recents (MRU)
// first, then everything else by Section, then Title. Used when the
// user has typed nothing yet.
func matchEmpty(rows []PaletteRow, recents []string) []scoredRow {
	byID := make(map[string]PaletteRow, len(rows))
	for _, r := range rows {
		byID[r.ID] = r
	}
	out := make([]scoredRow, 0, len(rows))
	seen := make(map[string]bool, len(recents))
	for i, id := range recents {
		r, ok := byID[id]
		if !ok {
			continue
		}
		out = append(out, scoredRow{row: r, score: 1000 - i})
		seen[id] = true
	}
	rest := make([]PaletteRow, 0, len(rows)-len(seen))
	for _, r := range rows {
		if seen[r.ID] {
			continue
		}
		rest = append(rest, r)
	}
	sort.SliceStable(rest, func(i, j int) bool {
		if rest[i].Section != rest[j].Section {
			return sectionOrder(rest[i].Section) < sectionOrder(rest[j].Section)
		}
		return strings.ToLower(rest[i].Title) < strings.ToLower(rest[j].Title)
	})
	for _, r := range rest {
		out = append(out, scoredRow{row: r, score: 0})
	}
	return out
}

// sectionOrder ranks sections so the empty-buffer view shows
// commands first, then folders, then saved searches.
func sectionOrder(s string) int {
	switch s {
	case sectionCommands:
		return 0
	case sectionFolders:
		return 1
	case sectionSavedSearches:
		return 2
	}
	return 3
}
