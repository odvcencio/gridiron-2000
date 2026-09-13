package league

import (
	"fmt"
	"strings"
)

// A partial source forecast is useful for display, but is not a complete
// forecast for probability calculations. Never fill gaps with actual scores.
type originalProjectionSummary struct {
	Total    float64
	Known    int
	Starters int
	Missing  []string
}

func summarizeOriginalProjections(rows []StarterLedgerRow, byID map[string]Player) originalProjectionSummary {
	var summary originalProjectionSummary
	for _, row := range rows {
		if row.PlayerID == "" {
			continue
		}
		summary.Starters++
		value, known := originalStarterProjectedTotal(row, byID)
		if known {
			summary.Known++
			summary.Total += value
			continue
		}
		name := strings.TrimSpace(row.PlayerName)
		if name == "" {
			name = row.Slot
		}
		if name == "" {
			name = "Unnamed starter"
		}
		summary.Missing = append(summary.Missing, name)
	}
	return summary
}

func (s originalProjectionSummary) complete() bool {
	return s.Starters > 0 && s.Known == s.Starters
}

func (s originalProjectionSummary) text() string {
	if s.Known == 0 {
		return winProbabilityDashText
	}
	text := fmt.Sprintf("%.1f", s.Total)
	if !s.complete() {
		// Keep the marker in the existing value binding too: already-open
		// clients must not mistake a subtotal for a complete forecast.
		text += "*"
	}
	return text
}

func (s originalProjectionSummary) coverage() string {
	if s.Known > 0 && !s.complete() {
		return "Partial"
	}
	return ""
}

func (s originalProjectionSummary) note() string {
	if s.complete() {
		return "Weekly source forecasts; actual scores are not substituted."
	}
	if s.Starters == 0 {
		return "Set a starting lineup to see its weekly source projection."
	}
	names := s.Missing
	if len(names) > 3 {
		names = names[:3]
	}
	missing := strings.Join(names, ", ")
	if len(s.Missing) > len(names) {
		count := len(s.Missing) - len(names)
		label := "others"
		if count == 1 {
			label = "other"
		}
		missing += fmt.Sprintf(" and %d %s", count, label)
	}
	return fmt.Sprintf("%d of %d starter forecasts available. Missing forecast: %s. Unknown forecasts are excluded, not treated as zero; actual scores are not substituted.", s.Known, s.Starters, missing)
}
