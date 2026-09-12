package league

import (
	"context"
	"fmt"
	"testing"
)

func TestStarterWheelLiveBindingsMatchEachTeamsSegments(t *testing.T) {
	svc, now := featuredMatchupFixture(t)
	for _, final := range []bool{false, true} {
		if final {
			svc.SetLiveStatusSource(func() LiveStatus {
				return LiveStatus{Enabled: true, Games: map[string]LiveGameState{
					"BUF": {Final: true}, "BAL": {Final: true}, "PHI": {Final: true}, "SEA": {Final: true},
				}}
			})
		}
		live, err := (scheduleProvider{svc: svc}).SnapshotWeek(context.Background(), now, 1)
		if err != nil {
			t.Fatal(err)
		}
		view := svc.LiveScoresView(context.Background())
		status, _ := svc.liveStatus()
		assert := func(key, id, want string) {
			t.Helper()
			values, ok := view[key].(map[string]string)
			if !ok {
				t.Fatalf("%s is not a binding map: %#v", key, view[key])
			}
			if got, exists := values[id]; !exists || got != want {
				t.Fatalf("%s.%s = %q (exists %v), want %q", key, id, got, exists, want)
			}
		}
		for _, matchup := range live.Matchups {
			for side, rows := range map[string][]StarterLedgerRow{"home": matchup.Home.StarterLedger, "away": matchup.Away.StarterLedger} {
				segments := starterProgressSegmentsForTeam(rows, status)
				id := matchup.ID + "-" + side
				assert("starterProgressComplete", id, fmt.Sprintf("%d/%d", starterProgressCompleted(segments), len(segments)))
				assert("starterProgressSummary", id, starterProgressSummary(segments))
				for _, segment := range segments {
					key := starterProgressTeamBindKey(matchup.ID, side, segment.Index)
					assert("starterProgressLabel", key, segment.ProgressLabel)
					assert("starterProgressPlayers", key, segment.PlayerNames)
					for quarter := 1; quarter <= 4; quarter++ {
						assert(fmt.Sprintf("starterProgressQ%d", quarter), key, starterProgressMark(segment.Progress, quarter))
					}
				}
			}
		}
	}
}
