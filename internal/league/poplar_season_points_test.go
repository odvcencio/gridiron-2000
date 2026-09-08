package league

import "testing"

// TestSeasonPointsTextSumsClosedWeeksOnly is J3 F31 residue (wave E): the
// trade composer's own "season points" column read a single week's live
// score (weeklyPlayerPointsText) under a field literally named
// SeasonPoints — a real number for one week, not a season total. This
// pins the new SeasonPointsText: two closed weeks with different scores
// for the same player sum correctly, and no week closed at all reads as
// the honest "—", never a claimed "0.0".
func TestSeasonPointsTextSumsClosedWeeksOnly(t *testing.T) {
	svc := schedulerTestService(t)
	player := Player{ID: "p-01", Name: "Ja'Marr Chase", Position: "WR"}

	svc.SetWeekStatsSource(func(week int) []WeekStatLine {
		switch week {
		case 1:
			// 1 recTD, 6 points at the default scoring values (matching
			// TestCloseWeekScoresAndMarksFinal's own fixture assumption).
			return []WeekStatLine{{Key: normalizePlayerKey("Ja'Marr Chase", "WR"), Stats: map[string]float64{"recTD": 1}}}
		case 2:
			// 2 recTDs, 12 points.
			return []WeekStatLine{{Key: normalizePlayerKey("Ja'Marr Chase", "WR"), Stats: map[string]float64{"recTD": 2}}}
		}
		return nil
	})

	weeks := svc.store.Snapshot().Schedule.Weeks
	if len(weeks) != 2 {
		t.Fatalf("fixture schedule has %d weeks, want 2", len(weeks))
	}

	// Before any week closes: honest "—", never a claimed score.
	if got := svc.SeasonPointsText(svc.store.Snapshot(), player); got != "—" {
		t.Fatalf("SeasonPointsText before any close = %q, want %q", got, "—")
	}

	now := svc.clock()
	if _, _, err := svc.closeWeek(weeks[0].Week, now); err != nil {
		t.Fatalf("close week %d: %v", weeks[0].Week, err)
	}
	if got, want := svc.SeasonPointsText(svc.store.Snapshot(), player), "6.0"; got != want {
		t.Fatalf("SeasonPointsText after one closed week = %q, want %q", got, want)
	}

	if _, _, err := svc.closeWeek(weeks[1].Week, now); err != nil {
		t.Fatalf("close week %d: %v", weeks[1].Week, err)
	}
	if got, want := svc.SeasonPointsText(svc.store.Snapshot(), player), "18.0"; got != want {
		t.Fatalf("SeasonPointsText after two closed weeks = %q, want %q (6.0 + 12.0)", got, want)
	}

	// A player with no posted line in either closed week still sums as a
	// real, honest zero across two real closed weeks — never "—" once at
	// least one week has closed.
	unposted := Player{ID: "p-99", Name: "Nobody Ledgered", Position: "WR"}
	if got, want := svc.SeasonPointsText(svc.store.Snapshot(), unposted), "0.0"; got != want {
		t.Fatalf("SeasonPointsText for a player with no posted line = %q, want %q", got, want)
	}
}
