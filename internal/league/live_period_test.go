package league

import (
	"context"
	"fmt"
	"testing"
)

// Live Tank01 periods are ordinal labels, not the Q-prefixed values our
// synthetic fixtures used. Both SSR and the targeted live bindings must
// advance the same wheel without waiting for the game to become final.
func TestStarterWheelOrdinalPeriodsAdvanceLiveBindings(t *testing.T) {
	svc, _ := featuredMatchupFixture(t)
	for quarter, period := range []string{"1st", "2nd", "3rd", "4th", "Final"} {
		t.Run(period, func(t *testing.T) {
			want := min(quarter+1, 4)
			status := LiveStatus{Enabled: true, Games: map[string]LiveGameState{
				"BUF": {Period: period, Clock: "12:04", InProgress: period != "Final", Final: period == "Final"},
				"BAL": {Period: period, Clock: "12:04", InProgress: period != "Final", Final: period == "Final"},
				"PHI": {Period: period, Clock: "12:04", InProgress: period != "Final", Final: period == "Final"},
				"SEA": {Period: period, Clock: "12:04", InProgress: period != "Final", Final: period == "Final"},
			}}
			svc.SetLiveStatusSource(func() LiveStatus { return status })
			view := svc.LiveScoresView(context.Background())
			checked := 0
			labels := view["starterProgressLabel"].(map[string]string)
			players := view["starterProgressPlayers"].(map[string]string)
			for key, name := range players {
				if name != "Josh Allen" && name != "Saquon Barkley" {
					continue
				}
				checked++
				if got := labels[key]; got != progressLabel(want) {
					t.Errorf("%s label = %q, want %q", key, got, progressLabel(want))
				}
				for q := 1; q <= 4; q++ {
					marks := view[fmt.Sprintf("starterProgressQ%d", q)].(map[string]string)
					if got := marks[key]; got != starterProgressMark(want, q) {
						t.Errorf("%s Q%d mark = %q for %s", key, q, got, period)
					}
				}
			}
			if checked != 2 {
				t.Fatalf("checked %d starters, want 2", checked)
			}
		})
	}
}

func TestStarterProgressPeriodAliases(t *testing.T) {
	for period, want := range map[string]int{
		"1st": 1, "2nd": 2, "3rd": 3, "4th": 4,
		" 2nd 12:04 ": 2, "Q1 8:12": 1, "q3": 3,
		"Halftime": 2, "HALF TIME": 2, "OT": 4, "Overtime": 4,
		"": 0, "Scheduled": 0, "Delayed": 0, "Q10": 0,
	} {
		if got := progressQuarterFromPeriod(period); got != want {
			t.Errorf("%q progress = %d, want %d", period, got, want)
		}
		row := StarterLedgerRow{PlayerID: "p-09", NFLTeam: "BUF", GameState: period}
		if got := starterProgressQuarter(row, LiveStatus{}); got != want {
			t.Errorf("%q row fallback = %d, want %d", period, got, want)
		}
	}
}

func TestRemainingFractionOrdinalPeriods(t *testing.T) {
	for period, want := range map[string]float64{
		"1st": .875, "2nd": .625, "3rd": .375, "4th": .125,
		" q2 ": .625, "2nd 12:04": .625, "Overtime": .125,
	} {
		state := LiveGameState{Period: period, InProgress: true}
		if got := remainingFraction(state, true); got != want {
			t.Errorf("%q remaining = %v, want %v", period, got, want)
		}
		if got := remainingFraction(state, false); got != 1 {
			t.Errorf("%q unknown game remaining = %v, want 1", period, got)
		}
	}
}
