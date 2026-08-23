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

	poller.fetchBoxScore(context.Background(), game, false, now)
	if got := poller.Snapshot().Health.State; got != league.BlitzStateDegraded {
		t.Fatalf("failed fetch state = %q, want degraded", got)
	}
	poller.fetchBoxScore(context.Background(), game, false, now.Add(time.Minute))
	final := poller.Snapshot()
	if final.Health.State != league.BlitzStateReady || final.Health.SafeError != "" {
		t.Fatalf("recovered fetch health = %+v, want ready without error", final.Health)
	}
}
