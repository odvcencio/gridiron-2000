package fantasy

import (
	"math"
	"strconv"
	"strings"
)

// PuntQualifiesForYardsBonus is the single, shared 40+-yard puntYards
// bonus threshold (WP-R2). A blocked punt never qualifies, regardless of
// its recorded distance — its landing spot is not a real kick. Every
// punting-stat producer in this codebase (main.go's
// addPuntingStatsFromPBP and addPuntingStatsFromBoxScore,
// addLivePuntingStats below) must call this function rather than
// hardcoding the 40-yard threshold itself: the audit that found this
// duplicated across two independently maintained mappers (2026-09-23,
// "## Correctness" item 6) is the reason it now lives in exactly one
// place.
func PuntQualifiesForYardsBonus(distance float64, blocked bool) bool {
	return !blocked && distance >= 40
}

// PuntQualifiesForLong50 is the single, shared 50+-yard "punt long"
// bonus threshold — the other constant duplicated across producers
// (audit item 6, alongside PuntQualifiesForYardsBonus above). A blocked
// punt never qualifies.
func PuntQualifiesForLong50(distance float64, blocked bool) bool {
	return !blocked && distance >= 50
}

// addLivePuntingStats reads the provider's per-player aggregates, not its
// team-level totals. A valid punts count retains a real zero-stat row too.
// Aggregates cannot reconstruct every 40+/50+ punt or its landing position:
// only the known scoring components are emitted, then week-close PBP settles
// the remaining bonuses. The longest distance identifies one individual
// punt, so it contributes verified qualifying yards even in a multi-punt
// game. This is exact for one punt and conservative for multiple punts.
func addLivePuntingStats(stats map[string]float64, entry map[string]any) bool {
	group, ok := entry["Punting"].(map[string]any)
	if !ok {
		return false
	}
	punts, known := nonnegativePuntingStat(group["punts"])
	if !known {
		return false
	}
	for raw, normalized := range map[string]string{"puntsin20": "puntIn20", "puntTouchBacks": "puntTouchback"} {
		if count, valid := nonnegativePuntingStat(group[raw]); valid && count > 0 && count <= punts {
			stats[normalized] = count
		}
	}
	longest, longKnown := nonnegativePuntingStat(group["puntLong"])
	yards, yardsKnown := nonnegativePuntingStat(group["puntYds"])
	if punts > 0 && longKnown && yardsKnown && longest <= yards {
		// The live aggregate carries no per-punt blocked flag for the
		// longest punt specifically, so blocked=false here — matching
		// this function's existing, unchanged fallback behavior.
		if PuntQualifiesForLong50(longest, false) {
			stats["puntLong50"] = 1 // at least this longest punt is confirmed
		}
		if PuntQualifiesForYardsBonus(longest, false) && (punts != 1 || longest == yards) {
			stats["puntYards"] = longest
		}
	}
	return true
}

func nonnegativePuntingStat(raw any) (float64, bool) {
	if raw == nil {
		return 0, false
	}
	value, err := strconv.ParseFloat(strings.TrimSpace(flexString(raw)), 64)
	return value, err == nil && !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0 && math.Trunc(value) == value
}
