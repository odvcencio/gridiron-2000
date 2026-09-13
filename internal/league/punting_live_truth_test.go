package league

import (
	"strings"
	"testing"
	"time"
)

func TestMissingLivePunterStatsArePendingNotConfirmedZero(t *testing.T) {
	now := time.Date(2026, 9, 13, 18, 0, 0, 0, time.UTC)
	player := Player{ID: "test-p", Name: "Test Punter", Position: "P", NFLTeam: "CIN"}
	snapshot := matchupStatsSnapshot{hasLive: true, known: true, live: LiveStatus{Enabled: true, Games: map[string]LiveGameState{"CIN": {InProgress: true, Period: "2nd"}}}, lines: []WeekStatLine{{Key: "other|QB", Stats: map[string]float64{"passYards": 10}}}}
	if starterGameKnownZeroSoFar(player, 1, snapshot, now) {
		t.Fatal("missing punting source was called a known zero")
	}
	if got := weeklyPlayerPointsText(player, snapshot, nil, weekStatLinesByKey(snapshot.lines), now); got != "—" {
		t.Fatalf("missing punter points = %q, want pending dash", got)
	}
	row := StarterLedgerRow{Position: "P", JoinState: "missing-join"}
	ledgerPlayerDetail(&row)
	if !strings.Contains(row.Detail, "not a confirmed zero") {
		t.Fatalf("missing punter detail = %q", row.Detail)
	}
}

func TestPunterLedgerScoresKnownComponentsAndDisclosesProvisionalData(t *testing.T) {
	setRosterShape(rosterPresets["gridiron-house"])
	t.Cleanup(clearRosterShape)
	svc := newTestService(t, true)
	now := time.Date(2026, 9, 13, 18, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	player := Player{ID: "test-p", Name: "Test Punter", Position: "P", NFLTeam: "CIN", Projection: 8}
	svc.SetPlayerSource(func() ([]Player, int64, string) { return []Player{player}, 1, "test" })
	if _, err := svc.store.MakePick("team-1", player.ID, "manager", now, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if err := svc.store.SetLineupSlot("team-1", 1, "P", player.ID, now); err != nil {
		t.Fatal(err)
	}
	svc.SetLiveStatusSource(func() LiveStatus {
		return LiveStatus{Enabled: true, Games: map[string]LiveGameState{"CIN": {InProgress: true, Period: "2nd"}}}
	})
	svc.SetWeekStatsSource(func(int) []WeekStatLine {
		return []WeekStatLine{{Key: "testpunter|P", Stats: map[string]float64{"puntYards": 51, "puntLong50": 1}, Source: StatSourceLive}}
	})
	ledger := svc.teamWeekLedger(svc.store.Snapshot(), "team-1", 1)
	if ledger.Total != 2.02 || !ledger.Known {
		t.Fatalf("punter ledger total=%v known=%v", ledger.Total, ledger.Known)
	}
	for _, row := range ledger.Rows {
		if row.PlayerID == player.ID {
			if row.PointsText != "2.0" || !strings.Contains(row.Detail, "provisional") || row.JoinState != "matched" {
				t.Fatalf("punter display = %+v", row)
			}
			return
		}
	}
	t.Fatal("punter absent from ledger")
}
