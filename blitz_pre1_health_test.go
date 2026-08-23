package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestComputeBlitzPre1RetainsPartialStatsWithDegradedHealth(t *testing.T) {
	boxScore, err := os.ReadFile(filepath.Join("internal", "fantasy", "testdata", "preseason-boxscore-sample.json"))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/getNFLGamesForWeek":
			if r.URL.Query().Get("week") == "1" {
				_, _ = w.Write([]byte(`{"statusCode":200,"body":[
					{"gameID":"g1","gameWeek":"Preseason Week 1","away":"ARI","home":"LV","gameStatus":"Completed","gameStatusCode":"2"},
					{"gameID":"g2","gameWeek":"Preseason Week 1","away":"KC","home":"DEN","gameStatus":"Completed","gameStatusCode":"2"}
				]}`))
				return
			}
			_, _ = w.Write([]byte(`{"statusCode":200,"body":[]}`))
		case "/getNFLBoxScore":
			if r.URL.Query().Get("gameID") == "g2" {
				w.WriteHeader(http.StatusBadGateway)
				_, _ = w.Write([]byte(`upstream failure`))
				return
			}
			_, _ = w.Write(boxScore)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	p := newBlitzPre1TestPool(t, server)
	result, err := computeBlitzPre1Detailed(context.Background(), p, time.Now())
	if err != nil {
		t.Fatalf("partial fetch should remain usable, got error: %v", err)
	}
	if result.health.State != "degraded" || result.health.Complete {
		t.Fatalf("partial health = %+v, want degraded/incomplete", result.health)
	}
	if result.health.ExpectedGames != 2 || result.health.FetchedGames != 1 {
		t.Fatalf("partial counts = expected %d fetched %d, want 2/1", result.health.ExpectedGames, result.health.FetchedGames)
	}
	if _, ok := result.stats["4038524"]; !ok {
		t.Fatal("successful game's stats were not retained")
	}
}
