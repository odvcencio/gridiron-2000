package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"gridiron-2000/internal/league"
)

func TestBlitzHealthPayloadIsSafeAndActionable(t *testing.T) {
	attempt := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	payload := blitzHealthPayload(league.BlitzDependencyHealth{Source: league.BlitzHealth{
		Enabled: true, State: league.BlitzStateDegraded, LastAttempt: attempt,
		LastSuccess: attempt.Add(-time.Hour), SafeError: "source temporarily unavailable",
		ExpectedGames: 4, FetchedGames: 3, Complete: false,
		Slates: map[string]league.BlitzSlateHealth{"pre2": {State: league.BlitzStateDegraded, ExpectedGames: 4, FetchedGames: 3}},
	}, Pre1: league.BlitzPre1Health{State: league.BlitzStateDegraded, ExpectedGames: 2, FetchedGames: 1}})
	if payload["state"] != league.BlitzStateDegraded || payload["expectedGames"] != 4 || payload["fetchedGames"] != 3 {
		t.Fatalf("payload = %+v", payload)
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"api_key", "secret", "https://", "tank.example"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("health payload leaked %q: %s", forbidden, encoded)
		}
	}
}
