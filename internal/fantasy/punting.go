package fantasy

import (
	"math"
	"strconv"
	"strings"
)

// addLivePuntingStats reads the provider's per-player aggregates, not its
// team-level totals. A valid punts count retains a real zero-stat row too.
// Aggregates cannot reconstruct every 40+/50+ punt or its landing position:
// only the known scoring components are emitted, then week-close PBP settles
// the remaining bonuses. In a one-punt game, matching yards and longest
// distance identify that individual punt exactly.
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
		if longest >= 50 {
			stats["puntLong50"] = 1 // at least this longest punt is confirmed
		}
		if punts == 1 && longest == yards && yards >= 40 {
			stats["puntYards"] = yards
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
