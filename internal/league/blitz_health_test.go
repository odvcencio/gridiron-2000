package league

import (
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestBlitzHealthEmptySnapshotIsLoadingNotVerifiedZero(t *testing.T) {
	health := BlitzHealthFromSnapshot(BlitzHealth{}, nil)
	if health.State != BlitzStateLoading {
		t.Fatalf("empty source state = %q, want loading", health.State)
	}
	if health.Complete || health.VerifiedZero || health.Final {
		t.Fatalf("empty source must not claim terminal truth: %+v", health)
	}
}

func TestBlitzHealthLegacyGamesRequireBothSlatesForArchiveTruth(t *testing.T) {
	now := time.Now()
	pre2 := BlitzGame{ID: "pre2-game", Slate: "pre2", Away: "KC", Home: "DEN", Kickoff: now.Add(-time.Hour), Final: true}
	pre3 := BlitzGame{ID: "pre3-game", Slate: "pre3", Away: "BUF", Home: "MIA", Kickoff: now.Add(-time.Hour), Final: true}
	one := BlitzSnapshot{Games: []BlitzGame{pre2}}
	if blitzArchiveTruthReady(one) {
		t.Fatal("a snapshot missing pre3 must not be archive-ready")
	}
	both := BlitzSnapshot{Games: []BlitzGame{pre2, pre3}}
	if !blitzArchiveTruthReady(both) {
		t.Fatal("complete final legacy fixture should remain archive-ready")
	}
}

func TestSafeBlitzErrorDoesNotExposeProviderURLOrSecret(t *testing.T) {
	err := errors.New("GET https://tank.example.test/getNFLGamesForWeek?api_key=secret-token returned 503")
	message := SafeBlitzError(err)
	if message == "" || message == err.Error() {
		t.Fatalf("safe error = %q, want bounded classification", message)
	}
	for _, forbidden := range []string{"tank.example.test", "secret-token", "api_key", "https://"} {
		if contains(message, forbidden) {
			t.Fatalf("safe error %q leaked %q", message, forbidden)
		}
	}
	if got := SafeBlitzErrorText("https://tank.example.test/path?key=secret"); got == "" || contains(got, "tank.example") {
		t.Fatalf("safe URL text = %q", got)
	}
}

func TestBlitzValidationStillUsesSourceGamesWhenHealthDegraded(t *testing.T) {
	service := newTestService(t, true)
	now := time.Now()
	service.now = func() time.Time { return now }
	service.SetPlayerSource(func() ([]Player, int64, string) {
		return []Player{{ID: "p1", Name: "Player One", Position: "QB", NFLTeam: "KC"}}, 1, "live"
	})
	service.SetBlitzSource(func() BlitzSnapshot {
		return BlitzSnapshot{
			Games: []BlitzGame{{ID: "g1", Slate: "pre2", Away: "KC", Home: "DEN", Kickoff: now.Add(time.Hour)}},
			Health: BlitzHealth{State: BlitzStateDegraded, Slates: map[string]BlitzSlateHealth{
				"pre2": {State: BlitzStateDegraded, ExpectedGames: 2, FetchedGames: 1, Complete: false},
			}},
		}
	})
	request, _ := http.NewRequest(http.MethodGet, "/blitz?slate=pre2", nil)
	data := service.BlitzData(request)
	if data["archived"] == true || data["slate_closed"] == true {
		t.Fatalf("degraded partial source must not claim terminal truth: %+v", data)
	}
	if data["blitz_state"] != BlitzStateDegraded {
		t.Fatalf("blitz_state = %v, want degraded", data["blitz_state"])
	}
}

func TestBlitzDataUsesSelectedSlateHealth(t *testing.T) {
	service := newTestService(t, true)
	now := time.Now().UTC().Truncate(time.Second)
	service.now = func() time.Time { return now }
	lastSuccess := now.Add(-5 * time.Minute)
	service.SetBlitzSource(func() BlitzSnapshot {
		return BlitzSnapshot{
			Games: []BlitzGame{{ID: "pre2-game", Slate: "pre2", Away: "KC", Home: "DEN", Kickoff: now.Add(time.Hour)}},
			Health: BlitzHealth{
				State:     BlitzStateDegraded,
				SafeError: "source has not published Preseason Week 3",
				Slates: map[string]BlitzSlateHealth{
					"pre2": {State: BlitzStateReady, LastSuccess: lastSuccess, ExpectedGames: 1, FetchedGames: 1, Complete: true, Final: true},
					"pre3": {State: BlitzStateLoading},
				},
			},
		}
	})
	request, _ := http.NewRequest(http.MethodGet, "/blitz?slate=pre2", nil)
	data := service.BlitzData(request)
	if data["blitz_state"] != BlitzStateReady || data["blitz_loading"] == true || data["blitz_degraded"] == true || data["blitz_recovery"] == true {
		t.Fatalf("healthy selected slate inherited another slate's state: %+v", data)
	}
	if data["blitz_source_error"] != "" || data["blitz_as_of"] != lastSuccess.Format(time.RFC3339) {
		t.Fatalf("selected slate provenance = error %v, as-of %v", data["blitz_source_error"], data["blitz_as_of"])
	}
	if data["blitz_source_complete"] != true || data["blitz_source_final"] != true {
		t.Fatalf("selected slate completeness = %v/%v, want true/true", data["blitz_source_complete"], data["blitz_source_final"])
	}
}

func TestBlitzPre1PartialDoesNotCallAbsentPlayersNoSnaps(t *testing.T) {
	service := newTestService(t, true)
	now := time.Now()
	pool := playerPool{players: []Player{
		{ID: "present", Name: "Present", Position: "WR", NFLTeam: "KC"},
		{ID: "missing", Name: "Missing", Position: "RB", NFLTeam: "DEN"},
	}}
	games := []BlitzGame{{ID: "g1", Slate: "pre2", Away: "KC", Home: "DEN", Kickoff: now.Add(time.Hour)}}
	rows := service.blitzEligiblePlayersWithHealth(pool, games, nil,
		map[string]map[string]float64{"present": {"recYds": 20}},
		BlitzPre1Health{State: BlitzStateDegraded, ExpectedGames: 2, FetchedGames: 1, Complete: false}, now)
	for _, row := range rows {
		if row["id"] == "missing" {
			if row["pre1_summary"] == "no pre1 snaps" {
				t.Fatalf("partial evidence mislabeled absent player as verified no snaps: %+v", row)
			}
			if row["pre1_summary"] != "PRE1 evidence incomplete — no fetched snaps" {
				t.Fatalf("partial evidence copy = %v", row["pre1_summary"])
			}
		}
	}
}

func contains(value, needle string) bool {
	for index := 0; index+len(needle) <= len(value); index++ {
		if value[index:index+len(needle)] == needle {
			return true
		}
	}
	return false
}
