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

// punterHistIndex is the embedded asset, parsed once at package init into a
// team+last-name lookup keyed by punterHistKey.
var punterHistIndex = loadPunterHistIndex()

// loadPunterHistIndex parses puntersHistRaw into the lookup index. A parse
// failure (only possible from a corrupted build) yields an empty index
// rather than a panic: a missing hist line is a cosmetic gap, never a
// startup crash — the same fail-quiet discipline punterHistLine's mismatch
// path follows.
func loadPunterHistIndex() map[string]string {
	var entries []punterHistEntry
	if err := json.Unmarshal(puntersHistRaw, &entries); err != nil {
		return map[string]string{}
	}
	index := make(map[string]string, len(entries))
	for _, entry := range entries {
		if entry.Hist == "" {
			continue
		}
		index[punterHistKey(entry.Team, entry.LastName)] = entry.Hist
	}
	return index
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
	line, ok := punterHistIndex[punterHistKey(team, lastName)]
	if !ok {
		return "", false
	}
	return line, true
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
