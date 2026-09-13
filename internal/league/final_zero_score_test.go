package league

import (
	"testing"
	"time"

	"gridiron-2000/internal/fantasy"
)

func TestTeamWeekLedgerPreservesParserConfirmedFinalZero(t *testing.T) {
	for _, tc := range []struct {
		name, zeroPlayer string
		wantKnown        bool
		wantTotalText    string
		wantZeroJoin     string
	}{
		{
			name:          "explicit final zero keeps earned team total",
			zeroPlayer:    `,"p-02":{"longName":"Bijan Robinson","teamAbv":"ATL","Rushing":{"rushYds":"0","rushTD":"0"}}`,
			wantKnown:     true,
			wantTotalText: "6.0",
			wantZeroJoin:  "matched",
		},
		{
			name:          "absent final row keeps team total unknown",
			wantTotalText: "—",
			wantZeroJoin:  "missing-join",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := newTestService(t, true)
			now := time.Date(2026, 9, 13, 21, 0, 0, 0, time.UTC)
			svc.now = func() time.Time { return now }
			if _, err := svc.store.MakePick("team-1", "p-01", "manager", now, time.Time{}); err != nil {
				t.Fatal(err)
			}
			draftSecondPickForTeam1(t, svc, now, "p-02")

			box := fantasy.ParseBoxScore([]byte(`{"body":{"gameID":"20260913_CIN@ATL","away":"CIN","home":"ATL","gameStatus":"Completed","gameStatusCode":"2","currentPeriod":"Final/OT","gameClock":"","playerStats":{"p-01":{"longName":"Ja'Marr Chase","teamAbv":"CIN","Receiving":{"recTD":"1"}}` + tc.zeroPlayer + `}}}`))
			if !box.Final {
				t.Fatal("fixture must parse as a final game")
			}
			var lines []WeekStatLine
			for id, row := range box.Players {
				player, ok := svc.ResolveLivePlayer(id, row.Name)
				if !ok {
					t.Fatalf("parsed player %q (%s) did not resolve", id, row.Name)
				}
				lines = append(lines, WeekStatLine{
					Key:    normalizePlayerKey(player.Name, player.Position),
					Stats:  RuleStatsFromTank01(row.Stats, box.Final),
					Source: StatSourceLiveFinal,
				})
			}
			svc.SetWeekStatsSource(func(int) []WeekStatLine { return lines })
			svc.SetLiveStatusSource(func() LiveStatus {
				game := LiveGameState{GameID: box.GameID, Away: box.Away, Home: box.Home,
					Period: box.Period, Clock: box.Clock, Final: box.Final, Kickoff: now.Add(-4 * time.Hour)}
				return LiveStatus{Enabled: true, Games: map[string]LiveGameState{"CIN": game, "ATL": game}}
			})

			ledger := svc.teamWeekLedger(svc.store.Snapshot(), "team-1", 1)
			if ledger.Known != tc.wantKnown || ledger.TotalText != tc.wantTotalText || ledger.Total != 6 {
				t.Fatalf("final ledger = %+v, want known=%v total text=%q and earned subtotal 6", ledger, tc.wantKnown, tc.wantTotalText)
			}
			var sawEarned, sawZero bool
			for _, row := range ledger.Rows {
				switch row.PlayerID {
				case "p-01":
					sawEarned = true
					if row.JoinState != "matched" || row.Points != 6 || !row.GameFinal {
						t.Fatalf("earned final row = %+v, want matched 6 points", row)
					}
				case "p-02":
					sawZero = true
					if row.JoinState != tc.wantZeroJoin || row.Points != 0 || !row.GameFinal || row.ZeroSoFarKnown {
						t.Fatalf("scoreless final row = %+v, want join=%q and no in-progress zero assumption", row, tc.wantZeroJoin)
					}
				}
			}
			if !sawEarned || !sawZero {
				t.Fatalf("both drafted starters must remain in ledger: %+v", ledger.Rows)
			}
		})
	}
}
