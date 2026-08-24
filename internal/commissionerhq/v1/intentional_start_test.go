package v1

import (
	"encoding/json"
	"testing"
)

func TestDraftLifecycleIsIndependentOfScheduledMeetingTime(t *testing.T) {
	t.Parallel()

	var scheduled Summary
	if err := json.Unmarshal(fixture(t, "healthy_dynasty_null_digest.json"), &scheduled); err != nil {
		t.Fatal(err)
	}
	scheduled.ProducedAt = "2026-08-30T20:30:05Z"
	if err := scheduled.Validate(); err != nil {
		t.Fatalf("missed meeting must remain a valid scheduled draft until commissioner start: %v", err)
	}

	var open Summary
	if err := json.Unmarshal(fixture(t, "healthy_dynasty_null_digest.json"), &open); err != nil {
		t.Fatal(err)
	}
	open.ProducedAt = "2026-08-28T20:30:05Z"
	open.Competition.Phase = "draft"
	open.Draft.State = "open"
	onClock := "team-1"
	open.Draft.OnClockTeamID = &onClock
	if err := open.Validate(); err != nil {
		t.Fatalf("commissioner must be able to start before the meeting time: %v", err)
	}
}

func TestExpectedDraftTeamsMayIncludeOpenActiveSeat(t *testing.T) {
	t.Parallel()

	var summary Summary
	if err := json.Unmarshal(fixture(t, "healthy_dynasty_null_digest.json"), &summary); err != nil {
		t.Fatal(err)
	}
	*summary.Competition.Teams.Occupied = 7
	*summary.Competition.Teams.Vacant = 1
	*summary.Membership.ClaimedTeams = 7
	*summary.Membership.OpenTeams = 1
	*summary.Membership.PrimaryManagers = 7
	if err := summary.Validate(); err != nil {
		t.Fatalf("active open seat must remain in expected draft capacity: %v", err)
	}
}
