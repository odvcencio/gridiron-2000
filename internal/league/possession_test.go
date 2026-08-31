package league

import (
	"testing"
	"time"
)

// newRelevanceFixtureService builds a Service with two teams' worth of
// draft picks, backing TestTeamRelevanceForReadsCurrentEffectiveStarters:
// team-1 gets p-09 (Josh Allen, QB, BUF) and team-2 gets a synthetic
// Ravens D/ST (DST, BAL) — defaultPlayers carries no DST entry at all, so
// this fixture adds one. Both picks auto-fill into their team's starting
// lineup with no competition for the slot.
func newRelevanceFixtureService(t *testing.T) (*Service, string) {
	t.Helper()
	svc := newTestService(t, true)
	svc.players = append(append([]Player(nil), defaultPlayers()...), Player{
		ID: "p-dst-bal", Name: "Ravens D/ST", Position: "DST", NFLTeam: "BAL", Projection: 7.0, Status: "Available",
	})
	svc.store.draftLifecycleBypass = true
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	if _, err := svc.store.MakePick("team-1", "p-09", "manager", now, time.Time{}); err != nil {
		t.Fatalf("pick 1 (team-1, p-09): %v", err)
	}
	if _, err := svc.store.MakePick("team-2", "p-dst-bal", "manager", now, time.Time{}); err != nil {
		t.Fatalf("pick 2 (team-2, p-dst-bal): %v", err)
	}
	return svc, "team-1"
}

// TestStarterPossessionLabelKnownOffensiveStarter covers the chip's
// positive case: a starter whose own NFL team is known to hold the ball.
func TestStarterPossessionLabelKnownOffensiveStarter(t *testing.T) {
	player := Player{Name: "Josh Allen", Position: "QB", NFLTeam: "BUF"}
	live := LiveStatus{Games: map[string]LiveGameState{
		"BUF": {Possession: "BUF", PossessionKnown: true},
		"BAL": {Possession: "BUF", PossessionKnown: true},
	}}
	if got := starterPossessionLabel(player, live, true); got != "ON OFFENSE" {
		t.Fatalf("starterPossessionLabel = %q, want ON OFFENSE", got)
	}
}

// TestStarterPossessionLabelDSTInvertsTheCondition covers the DST-only
// inverted rule: a started DST is relevant while its OPPONENT holds the
// ball, never its own team.
func TestStarterPossessionLabelDSTInvertsTheCondition(t *testing.T) {
	dst := Player{Name: "Bills D/ST", Position: "DST", NFLTeam: "BUF"}
	// BAL (the opponent) has the ball: BUF's own defense is on the field.
	opponentHasBall := LiveStatus{Games: map[string]LiveGameState{
		"BUF": {Possession: "BAL", PossessionKnown: true},
		"BAL": {Possession: "BAL", PossessionKnown: true},
	}}
	if got := starterPossessionLabel(dst, opponentHasBall, true); got != "DEFENSE ON FIELD" {
		t.Fatalf("DST while the opponent has the ball = %q, want DEFENSE ON FIELD", got)
	}
	// BUF itself has the ball: its own DST is off the field, not relevant.
	ownTeamHasBall := LiveStatus{Games: map[string]LiveGameState{
		"BUF": {Possession: "BUF", PossessionKnown: true},
		"BAL": {Possession: "BUF", PossessionKnown: true},
	}}
	if got := starterPossessionLabel(dst, ownTeamHasBall, true); got != "" {
		t.Fatalf("DST while its own team has the ball = %q, want \"\" (not relevant)", got)
	}
}

// TestStarterPossessionLabelUnknownRendersNothing covers the truthful-
// state rule: unknown possession, no live poller, and no game entry for
// the team must all render nothing — never a placeholder, never a
// negative claim.
func TestStarterPossessionLabelUnknownRendersNothing(t *testing.T) {
	player := Player{Name: "Josh Allen", Position: "QB", NFLTeam: "BUF"}
	cases := map[string]struct {
		live    LiveStatus
		hasLive bool
	}{
		"no live poller wired":       {LiveStatus{}, false},
		"no game entry for the team": {LiveStatus{Games: map[string]LiveGameState{"BAL": {Possession: "BAL", PossessionKnown: true}}}, true},
		"possession itself unknown":  {LiveStatus{Games: map[string]LiveGameState{"BUF": {PossessionKnown: false}}}, true},
		"possession known but empty": {LiveStatus{Games: map[string]LiveGameState{"BUF": {Possession: "", PossessionKnown: true}}}, true},
		"known but the other team has it": {LiveStatus{Games: map[string]LiveGameState{
			"BUF": {Possession: "BAL", PossessionKnown: true},
			"BAL": {Possession: "BAL", PossessionKnown: true},
		}}, true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := starterPossessionLabel(player, tc.live, tc.hasLive); got != "" {
				t.Fatalf("%s: starterPossessionLabel = %q, want \"\"", name, got)
			}
		})
	}
}

// TestStarterRowMapsCarriesPossessionOnlyForOccupiedSlots covers the
// Team view's own possession chip seam (starterRowMaps, lineup.go): an
// occupied slot whose player's team is known to hold the ball gets
// has_possession=true and the matching label; an empty slot never
// carries either key at all (no placeholder, never true).
func TestStarterRowMapsCarriesPossessionOnlyForOccupiedSlots(t *testing.T) {
	svc := newTestService(t, true)
	svc.SetLiveStatusSource(func() LiveStatus {
		return LiveStatus{Games: map[string]LiveGameState{
			"BUF": {Possession: "BUF", PossessionKnown: true},
			"BAL": {Possession: "BUF", PossessionKnown: true},
		}}
	})
	lineup := EffectiveLineup{Week: 1, Slots: []SlotAssignment{
		{Slot: SlotInstance{ID: "QB", Def: SlotDef{Eligible: []string{"QB"}}}, HasPlayer: true,
			Player: Player{ID: "p-09", Name: "Josh Allen", Position: "QB", NFLTeam: "BUF"}},
		{Slot: SlotInstance{ID: "QB2", Def: SlotDef{Eligible: []string{"QB"}}}, HasPlayer: true,
			Player: Player{ID: "p-06", Name: "Lamar Jackson", Position: "QB", NFLTeam: "BAL"}},
		{Slot: SlotInstance{ID: "BENCH1", Def: SlotDef{Eligible: []string{"QB"}}}, HasPlayer: false},
	}}
	rows := svc.starterRowMaps(lineup, nil, nil, time.Now(), nil)
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3", len(rows))
	}
	if rows[0]["has_possession"] != true || rows[0]["possession_label"] != "ON OFFENSE" {
		t.Fatalf("BUF starter (own team has the ball) row = %+v, want has_possession=true possession_label=ON OFFENSE", rows[0])
	}
	if rows[1]["has_possession"] != false || rows[1]["possession_label"] != "" {
		t.Fatalf("BAL starter (opponent has the ball) row = %+v, want has_possession=false", rows[1])
	}
	if _, ok := rows[2]["has_possession"]; ok {
		t.Fatalf("empty slot carries a possession key at all: %+v", rows[2])
	}
}

// TestTeamRelevanceForReadsCurrentEffectiveStarters covers GC-2b's
// adaptive-cadence seam against a real Service/store fixture: a team
// with a started offensive player reports OffensiveStarter, a team whose
// D/ST is started reports DSTStarter, and a team nobody in the league
// starts reports neither.
func TestTeamRelevanceForReadsCurrentEffectiveStarters(t *testing.T) {
	svc, teamID := newRelevanceFixtureService(t)
	relevance := svc.TeamRelevanceFor("BUF")
	if !relevance.OffensiveStarter {
		t.Fatalf("BUF (started QB) TeamRelevance = %+v, want OffensiveStarter=true", relevance)
	}
	dst := svc.TeamRelevanceFor("BAL")
	if !dst.DSTStarter {
		t.Fatalf("BAL (started DST) TeamRelevance = %+v, want DSTStarter=true", dst)
	}
	none := svc.TeamRelevanceFor("KC")
	if none.OffensiveStarter || none.DSTStarter {
		t.Fatalf("KC (nobody starts a KC player) TeamRelevance = %+v, want both false", none)
	}
	_ = teamID
}
