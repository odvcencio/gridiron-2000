package league

import (
	"fmt"
	"strings"
	"time"
)

const draftMeetingInputLayout = "2006-01-02T15:04"

// EffectiveDraftAt is the single runtime resolver for the announced draft
// meeting. The persisted override wins; a zero override deliberately falls
// back to the configured league.json instant. This meeting never opens the
// draft—DraftStarted remains the lifecycle authority.
func (s *Service) EffectiveDraftAt(state PersistedState) time.Time {
	if !state.DraftAtOverride.IsZero() {
		return state.DraftAtOverride
	}
	return s.draftAt
}

// parseDraftMeetingLocal accepts exactly the value emitted by a
// datetime-local control and interprets it in the league's configured
// timezone. Nonexistent and ambiguous wall-clock times are rejected instead
// of silently shifting or choosing one side of a DST transition.
func parseDraftMeetingLocal(raw string, location *time.Location) (time.Time, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return time.Time{}, fmt.Errorf("choose a new draft meeting time")
	}
	if location == nil {
		location = time.UTC
	}
	parsed, err := time.ParseInLocation(draftMeetingInputLayout, value, location)
	if err != nil {
		return time.Time{}, fmt.Errorf("draft meeting must use the date and time shown by the calendar control")
	}
	if parsed.In(location).Format(draftMeetingInputLayout) != value {
		return time.Time{}, fmt.Errorf("that local time does not exist in %s because of a daylight-saving transition", location)
	}
	for delta := -4 * time.Hour; delta <= 4*time.Hour; delta += time.Minute {
		if delta == 0 {
			continue
		}
		if parsed.Add(delta).In(location).Format(draftMeetingInputLayout) == value {
			return time.Time{}, fmt.Errorf("that local time is ambiguous in %s because of a daylight-saving transition; choose another time", location)
		}
	}
	return parsed, nil
}

func draftMeetingInputValue(at time.Time, location *time.Location) string {
	if at.IsZero() {
		return ""
	}
	if location == nil {
		location = time.UTC
	}
	return at.In(location).Format(draftMeetingInputLayout)
}
