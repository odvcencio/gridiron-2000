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

// MinPunterGamesForRank is the fewest games (from the embedded 2025
// asset) a punter must have played to qualify for enrichment and rank
// (owner decision, punter-rankings review): a 5-game small sample can
// out-average a full 17-game season on a handful of good punts —
// Gillikin's 5-game 9.46/game outranking Townsend's 17-game 8.75, for
// example — which is not a meaningful "this punter is better" signal.
// Below this floor, PunterProjection returns a miss, the same as an
// unmatched name; see punterPerGame, the one place this qualifier and the
// non-positive-points qualifier both apply.
const MinPunterGamesForRank = 8

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
// required whenever two or more asset entries share a last name (an
// asset-side collision, none exist today), and ALSO whenever requireTeam
// is true: the live fantasy pool's own enrichment walk sets requireTeam
// when more than one live Position "P" player shares this last name (a
// live-pool collision — the embedded asset's 35 surnames are unique, but
// common surnames like Taylor or Martin genuinely recur among real live
// punters), so a second live punter sharing a surname can never inherit
// the first one's projection and rank. An unmatched name, a same-last-name
// collision with no team match, or a live-pool collision matched to the
// wrong team all return (0, false) — fail quiet, the same discipline
// punterHistLine's mismatch path follows.
func PunterProjection(name, team string, requireTeam bool) (perGame float64, ok bool) {
	lastName := lastWord(name)
	if lastName == "" {
		return 0, false
	}
	return punterProjectionFrom(punterIndex, lastName, team, requireTeam)
}

// punterProjectionFrom is PunterProjection's testable core: it takes the
// index as a parameter so the last-name-collision disambiguation rule can
// be pinned against a synthetic store, independent of the embedded asset's
// current (collision-free) shape.
func punterProjectionFrom(store punterHistStore, lastName, team string, requireTeam bool) (float64, bool) {
	candidates := store.byLastName[strings.ToUpper(strings.TrimSpace(lastName))]
	switch len(candidates) {
	case 0:
		return 0, false
	case 1:
		entry := candidates[0]
		if requireTeam && !strings.EqualFold(strings.TrimSpace(entry.Team), strings.TrimSpace(team)) {
			return 0, false
		}
		return punterPerGame(entry)
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

// punterPerGame divides a matched entry's TotalPts by Games — the ONE
// place both enrichment qualifiers apply: fewer than MinPunterGamesForRank
// games returns a miss (see its doc comment), and so does a non-positive
// TotalPts, indistinguishable from a punter who never actually scored.
// Games < MinPunterGamesForRank also covers a non-positive Games count
// defensively (should not occur in the embedded asset), so neither can
// ever divide by zero or a negative count.
func punterPerGame(entry punterHistEntry) (float64, bool) {
	if entry.Games < MinPunterGamesForRank || entry.TotalPts <= 0 {
		return 0, false
	}
	return entry.TotalPts / float64(entry.Games), true
}

// punterSuffixes lists the trailing generational/name suffixes lastWord
// strips before extracting a last name, with or without a trailing period
// (for example both "Jr." and "Jr"), case-insensitive — so "Michael
// Dickson Jr." keys on "Dickson", not "Jr.". This is the EXACT mirror of
// internal/fantasy/service.go's own punterSuffixes var and punterSurname
// loop: internal/fantasy has no dependency on this package, so the two
// copies are hand-kept in lockstep (finding 1 of the punter-rankings
// review); TestLastWordAgreesWithPunterSurnameSuffixTable, below, and
// fantasy's TestPunterSurnameStripsGenerationalSuffix pin both against the
// same table literal.
var punterSuffixes = map[string]bool{
	"JR": true, "SR": true, "II": true, "III": true, "IV": true,
}

// lastWord returns the last space-separated token of name, skipping a
// trailing generational suffix (see punterSuffixes), or "" for an empty,
// all-whitespace, or suffix-only name. Shared by punterHistLine's display
// match and PunterProjection's ranking match, so both follow this same
// suffix-stripping rule.
func lastWord(name string) string {
	fields := strings.Fields(name)
	for len(fields) > 0 {
		last := strings.ToUpper(strings.TrimSuffix(fields[len(fields)-1], "."))
		if !punterSuffixes[last] {
			break
		}
		fields = fields[:len(fields)-1]
	}
	if len(fields) == 0 {
		return ""
	}
	return fields[len(fields)-1]
}
