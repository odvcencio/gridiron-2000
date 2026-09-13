// Package gameclock interprets NFL period labels shared by live-feed
// ingestion and league presentation. It does not infer game finality.
package gameclock

import "strings"

// NormalizePeriod maps provider ordinal periods to canonical quarter labels.
// A row-level display may include a trailing clock ("2nd 12:04"); only the
// exact first token is interpreted. Unknown labels remain visible rather
// than inventing progress, and overtime remains distinct from regulation.
func NormalizePeriod(period string) string {
	period = strings.TrimSpace(period)
	parts := strings.Fields(period)
	if len(parts) == 0 {
		return ""
	}
	switch strings.ToUpper(parts[0]) {
	case "Q1", "1", "1ST", "FIRST":
		return "Q1"
	case "Q2", "2", "2ND", "SECOND":
		return "Q2"
	case "Q3", "3", "3RD", "THIRD":
		return "Q3"
	case "Q4", "4", "4TH", "FOURTH":
		return "Q4"
	case "HALF", "HALFTIME":
		return "HALF"
	case "OT", "1OT", "2OT", "OVERTIME":
		return "OT"
	case "FINAL":
		return "Final"
	default:
		return period
	}
}
