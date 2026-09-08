package league

import (
	"net/http"
	"testing"
)

// TestTeamHasSavedLineupThisWeek pins J5 F37's own arrival-strip gate: a
// team with no explicitly saved lineup slot for the current week reads
// false (pre-draft, or a first session that has never touched /team) and
// true the instant SetLineup writes any slot — the raw Store.Lineups map,
// never the auto-filled EffectiveLineup a manager who has saved nothing
// still sees.
func TestTeamHasSavedLineupThisWeek(t *testing.T) {
	svc, week, _ := pinTestService(t)

	if got := svc.TeamHasSavedLineupThisWeek("team-1"); got {
		t.Fatal("TeamHasSavedLineupThisWeek = true before any lineup was ever saved")
	}
	if got := svc.TeamHasSavedLineupThisWeek(""); got {
		t.Error("TeamHasSavedLineupThisWeek(\"\") = true, want false for an empty team id")
	}

	request, _ := http.NewRequest(http.MethodPost, "/team", nil)
	if _, err := svc.SetLineup(request, "team-1", week, "QB", "qb-open"); err != nil {
		t.Fatal(err)
	}
	if got := svc.TeamHasSavedLineupThisWeek("team-1"); !got {
		t.Fatal("TeamHasSavedLineupThisWeek = false right after SetLineup saved a slot")
	}

	// A different, untouched team still reads false: the gate is per
	// team, not a league-wide flag.
	if got := svc.TeamHasSavedLineupThisWeek("team-2"); got {
		t.Error("TeamHasSavedLineupThisWeek(\"team-2\") = true; another team's saved slot leaked across teams")
	}
}
