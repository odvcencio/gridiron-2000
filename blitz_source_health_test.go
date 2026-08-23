package main

import (
	"context"
	"gridiron-2000/internal/league"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestBlitzPollerReprobesUnresolvedSlateAfterBootFailure(t *testing.T) {
	var week2Calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/getNFLGamesForWeek" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		switch r.URL.Query().Get("week") {
		case "2":
			if week2Calls.Add(1) == 1 {
				w.WriteHeader(http.StatusBadGateway)
				_, _ = w.Write([]byte(`upstream boot failure`))
				return
			}
			_, _ = w.Write([]byte(`{"statusCode":200,"body":[{"gameID":"g2","gameWeek":"Preseason Week 2","away":"KC","home":"DEN","gameTime_epoch":"1787270400","gameStatus":"Scheduled"}]}`))
		default:
			_, _ = w.Write([]byte(`{"statusCode":200,"body":[]}`))
		}
	}))
	defer server.Close()
	p := newBlitzPre1TestPool(t, server)
	poller := newBlitzPoller(p, nil)
	poller.probeSlates(context.Background())
	first := poller.Snapshot()
	if first.Health.State != "error" || first.Health.Slates["pre2"].State != "error" {
		t.Fatalf("boot health = %+v, want unresolved error", first.Health)
	}
	if len(first.Games) != 0 {
		t.Fatalf("failed boot probe must not publish empty/partial games: %+v", first.Games)
	}

	poller.mu.Lock()
	poller.lastSchedule["pre2"] = time.Now().Add(-25 * time.Hour)
	poller.mu.Unlock()
	poller.refreshSchedulesIfDue(context.Background(), time.Now())
	second := poller.Snapshot()
	status := second.Health.Slates["pre2"]
	if status.State != "ready" || !status.Complete || status.ExpectedGames != 1 || status.FetchedGames != 1 {
		t.Fatalf("recovery health = %+v, want ready complete 1/1", second.Health)
	}
	if len(second.Games) != 1 || second.Games[0].ID != "g2" {
		t.Fatalf("recovery games = %+v", second.Games)
	}
}

func TestBlitzPollerCleanUnpublishedSlateStaysLoading(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"statusCode":200,"body":[]}`))
	}))
	defer server.Close()
	poller := newBlitzPoller(newBlitzPre1TestPool(t, server), nil)
	poller.probeSlates(context.Background())
	snapshot := poller.Snapshot()
	if snapshot.Health.State != "loading" {
		t.Fatalf("clean unpublished source = %q, want loading", snapshot.Health.State)
	}
	for slate, status := range snapshot.Health.Slates {
		if status.State != "loading" || status.Complete || status.VerifiedZero {
			t.Fatalf("%s health = %+v, want unknown/loading", slate, status)
		}
	}
}

func TestBlitzPollerBoxScoreRecoveryRestoresReadyState(t *testing.T) {
	boxScore, err := os.ReadFile(filepath.Join("internal", "fantasy", "testdata", "preseason-boxscore-sample.json"))
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(`temporary box score failure`))
			return
		}
		_, _ = w.Write(boxScore)
	}))
	defer server.Close()
	poller := newBlitzPoller(newBlitzPre1TestPool(t, server), nil)
	now := time.Now()
	game := league.BlitzGame{ID: "g1", Slate: "pre2", Away: "ARI", Home: "LV", Kickoff: now.Add(-time.Hour)}
	poller.mu.Lock()
	poller.games = []league.BlitzGame{game}
	poller.health.Slates = map[string]league.BlitzSlateHealth{
		"pre2": {State: league.BlitzStateReady, LastSuccess: now, ExpectedGames: 1, FetchedGames: 1, Complete: true},
		"pre3": {State: league.BlitzStateReady, LastSuccess: now, ExpectedGames: 1, FetchedGames: 1, FinalGames: 1, Complete: true, Final: true},
	}
	poller.health.State = league.BlitzStateReady
	poller.health.Enabled = true
	poller.mu.Unlock()
	poller.enteredTeamsFn = func() map[string]bool { return map[string]bool{"ARI": true} }

	poller.fetchBoxScore(context.Background(), game, false, now)
	failed := poller.Snapshot()
	if got := failed.Health.State; got != league.BlitzStateDegraded {
		t.Fatalf("failed fetch state = %q, want degraded", got)
	}
	if got := failed.Health.Slates["pre2"].State; got != league.BlitzStateDegraded {
		t.Fatalf("failed selected slate state = %q, want degraded", got)
	}
	if failed.Health.Slates["pre2"].Error == "" {
		t.Fatal("failed selected slate must retain safe error copy")
	}
	poller.fetchBoxScore(context.Background(), game, false, now.Add(time.Minute))
	final := poller.Snapshot()
	if final.Health.State != league.BlitzStateReady || final.Health.SafeError != "" || !final.Health.Slates["pre2"].ScoringComplete {
		t.Fatalf("recovered fetch health = %+v, want ready without error", final.Health)
	}
	if final.Health.Slates["pre2"].ExpectedScoringGames != 1 || final.Health.Slates["pre2"].FetchedScoringGames != 1 {
		t.Fatalf("recovered scoring health = %+v, want 1/1", final.Health.Slates["pre2"])
	}
}

func TestBlitzPollerFinalRelevantGameWithoutCacheIsFetchEligible(t *testing.T) {
	poller := newBlitzPoller(nil, nil)
	poller.enteredTeamsFn = func() map[string]bool { return map[string]bool{"KC": true} }
	now := time.Now().UTC()
	game := league.BlitzGame{ID: "final-kc", Slate: "pre2", Away: "KC", Home: "DEN", Kickoff: now.Add(-24 * time.Hour), Final: true}
	poller.mu.Lock()
	poller.games = []league.BlitzGame{game}
	poller.mu.Unlock()
	target, catchUp := poller.selectFetchTarget(now)
	if target.ID != game.ID || !catchUp {
		t.Fatalf("final relevant game target = %+v, catch-up=%v; want fetch despite empty cache", target, catchUp)
	}
}

func TestBlitzPollerNoEntrantsMarkFinalScoringComplete(t *testing.T) {
	poller := newBlitzPoller(nil, nil)
	poller.enteredTeamsFn = func() map[string]bool { return nil }
	now := time.Now().UTC()
	poller.mu.Lock()
	poller.games = []league.BlitzGame{
		{ID: "final-pre2", Slate: "pre2", Away: "KC", Home: "DEN", Kickoff: now.Add(-24 * time.Hour), Final: true},
		{ID: "final-pre3", Slate: "pre3", Away: "BUF", Home: "MIA", Kickoff: now.Add(-24 * time.Hour), Final: true},
	}
	poller.health.Slates = map[string]league.BlitzSlateHealth{
		"pre2": {State: league.BlitzStateReady, ExpectedGames: 1, FetchedGames: 1, FinalGames: 1, Complete: true, Final: true},
		"pre3": {State: league.BlitzStateReady, ExpectedGames: 1, FetchedGames: 1, FinalGames: 1, Complete: true, Final: true},
	}
	poller.health.Enabled = true
	poller.recomputeHealthLocked(now)
	poller.mu.Unlock()
	snapshot := poller.Snapshot()
	if !snapshot.Health.ScoringComplete || snapshot.Health.ExpectedScoringGames != 0 || snapshot.Health.FetchedScoringGames != 0 {
		t.Fatalf("no-entrant scoring health = %+v, want complete zero expectation", snapshot.Health)
	}
	for slate, status := range snapshot.Health.Slates {
		if !status.ScoringComplete || status.ExpectedScoringGames != 0 || status.FetchedScoringGames != 0 {
			t.Fatalf("%s no-entrant scoring health = %+v", slate, status)
		}
	}
}
