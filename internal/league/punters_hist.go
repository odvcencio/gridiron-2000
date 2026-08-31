package league

import (
	_ "embed"
	"encoding/json"
	"strings"
)

// punters_2025_hist.json is a WP-R0 asset (roster-ops spec section 4.1.2):
// 35 punters' 2025 lines ({lastName, team, games, hist, totalPts})
// reconstructed from 2025 play-by-play under the league's refined PUNTING
// scoring rules (scoring.go's defaultScoringRules). nflverse's
// season-summary mirror — the primary HistoricalSource (main.go) — carries
// no punting columns at all, so a Position "P" player never matches there;
// withHistorical (service.go) falls back to this embedded index. Embedding
// follows the internal/wire precedent (classifier.go's go:embed
// signal_rules.arb): the asset ships inside the binary, no runtime file
// dependency, no gitignored data/ directory involved.
//
//go:embed punters_2025_hist.json
var puntersHistRaw []byte

// punterHistEntry is one embedded punter's reconstructed 2025 line.
type punterHistEntry struct {
	LastName string  `json:"lastName"`
	Team     string  `json:"team"`
	Games    int     `json:"games"`
	Hist     string  `json:"hist"`
	TotalPts float64 `json:"totalPts"`
}

// punterHistStore is the embedded asset's parsed index, built once at
// package init. byTeamName backs punterHistLine's original team+last-name
// display match, unchanged. byLastName backs PunterProjection's
// last-name-primary match: keyed on the upper-cased last name alone, with
// every asset entry sharing that last name kept (usually exactly one — see
// PunterProjection's doc comment for the uniqueness verification).
type punterHistStore struct {
	byTeamName map[string]string
	byLastName map[string][]punterHistEntry
}

// punterIndex is the embedded asset, parsed once at package init.
var punterIndex = loadPunterHistIndex()

// loadPunterHistIndex parses puntersHistRaw into the lookup index. A parse
// failure (only possible from a corrupted build) yields an empty index
// rather than a panic: a missing hist line or projection is a cosmetic
// gap, never a startup crash — the same fail-quiet discipline
// punterHistLine's and PunterProjection's mismatch paths follow.
func loadPunterHistIndex() punterHistStore {
	var entries []punterHistEntry
	if err := json.Unmarshal(puntersHistRaw, &entries); err != nil {
		return punterHistStore{byTeamName: map[string]string{}, byLastName: map[string][]punterHistEntry{}}
	}
	byTeamName := make(map[string]string, len(entries))
	byLastName := make(map[string][]punterHistEntry, len(entries))
	for _, entry := range entries {
		if entry.Hist != "" {
			byTeamName[punterHistKey(entry.Team, entry.LastName)] = entry.Hist
		}
		key := strings.ToUpper(strings.TrimSpace(entry.LastName))
		if key == "" {
			continue
		}
		byLastName[key] = append(byLastName[key], entry)
	}
	return punterHistStore{byTeamName: byTeamName, byLastName: byLastName}
}

// punterHistKey normalizes a team abbreviation and a last name into the
// index's lookup key: upper-cased team, lower-cased last name.
func punterHistKey(team, lastName string) string {
	return strings.ToUpper(strings.TrimSpace(team)) + "|" + strings.ToLower(strings.TrimSpace(lastName))
}

// punterHistLine looks up a punter's embedded 2025 line by team and the
// last (space-separated) word of the player's full name, case-insensitive
// (roster-ops spec section 4.1.2 / WP-R0's exact matching rule). A
// mismatch — unknown team, unknown last name, or an empty name — returns
// ("", false): fail quiet, attach nothing, never a wrong attribution.
func punterHistLine(name, team string) (string, bool) {
	lastName := lastWord(name)
	if lastName == "" {
		return "", false
	}
	line, ok := punterIndex.byTeamName[punterHistKey(team, lastName)]
	if !ok {
		return "", false
	}
	return line, true
}

// PunterProjection resolves an embedded punter's 2025 per-game fantasy
// projection (TotalPts/Games, the same rescoring the punting-history line
// is built from) for the fantasy pool's punter-enrichment hook
// (fantasy.Service.SetPunterProjections) — the live Tank01 feed carries no
// punter projections at all.
//
// Punters change teams between seasons more often than skill players, so
// the match is primarily by LAST NAME: the last space-separated word of
// name, case-insensitive. All 35 entries in the embedded 2025 asset carry
// a unique last name (verified; see punters_hist_test.go), so last-name
// matching alone resolves every case unambiguously today. A team match is
// required ONLY when two or more asset entries share a last name, so a
// future roster addition that collides on last name can never silently
// misattribute a projection to the wrong punter. An unmatched name, or a
// same-last-name collision with no team match, returns (0, false) —
// fail quiet, the same discipline punterHistLine's mismatch path follows.
func PunterProjection(name, team string) (perGame float64, ok bool) {
	lastName := lastWord(name)
	if lastName == "" {
		return 0, false
	}
	return punterProjectionFrom(punterIndex, lastName, team)
}

// punterProjectionFrom is PunterProjection's testable core: it takes the
// index as a parameter so the last-name-collision disambiguation rule can
// be pinned against a synthetic store, independent of the embedded asset's
// current (collision-free) shape.
func punterProjectionFrom(store punterHistStore, lastName, team string) (float64, bool) {
	candidates := store.byLastName[strings.ToUpper(strings.TrimSpace(lastName))]
	switch len(candidates) {
	case 0:
		return 0, false
	case 1:
		return punterPerGame(candidates[0])
	default:
		wantTeam := strings.ToUpper(strings.TrimSpace(team))
		for _, entry := range candidates {
			if strings.ToUpper(strings.TrimSpace(entry.Team)) == wantTeam {
				return punterPerGame(entry)
			}
		}
		return 0, false
	}
}

// punterPerGame divides a matched entry's TotalPts by Games. A non-positive
// Games (should not occur in the embedded asset; guarded defensively) also
// returns (0, false) rather than dividing by zero or a negative count.
func punterPerGame(entry punterHistEntry) (float64, bool) {
	if entry.Games <= 0 {
		return 0, false
	}
	return entry.TotalPts / float64(entry.Games), true
}

// lastWord returns the last space-separated token of name, or "" for an
// empty or all-whitespace name.
func lastWord(name string) string {
	fields := strings.Fields(name)
	if len(fields) == 0 {
		return ""
	}
	return fields[len(fields)-1]
}
