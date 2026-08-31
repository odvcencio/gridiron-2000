package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// tank01GamesForWeek builds a getNFLGamesForWeek-shaped envelope body:
// one entry per (gameID, away, home, date, status) tuple.
type gameEntry struct {
	ID, Away, Home, Date, Status, StatusCode, Week string
}

func tank01GamesEnvelope(entries []gameEntry) []byte {
	body := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		body = append(body, map[string]any{
			"gameID": e.ID, "gameWeek": e.Week, "away": e.Away, "home": e.Home,
			"gameDate": e.Date, "gameStatus": e.Status, "gameStatusCode": e.StatusCode,
			"gameTime": "1:00p",
		})
	}
	encoded, _ := json.Marshal(map[string]any{"statusCode": 200, "body": body})
	return encoded
}

// tank01BoxEnvelope builds a getNFLBoxScore-shaped envelope body for one
// completed game with one player and one DST unit — enough for
// fantasy.ParseBoxScore to report Final=true with non-empty Players/DST.
func tank01BoxEnvelope(gameID, away, home string) []byte {
	body := map[string]any{
		"gameID": gameID, "away": away, "home": home, "awayPts": "24", "homePts": "17",
		"gameStatus": "Completed", "gameStatusCode": "2", "currentPeriod": "Final", "gameClock": "",
		"playerStats": map[string]any{
			"1": map[string]any{
				"longName": "Sample Runner", "teamAbv": away,
				"Rushing": map[string]any{"rushYds": "80", "rushTD": "1", "carries": "15"},
			},
		},
		"DST": map[string]any{
			"away": map[string]any{"teamAbv": away, "sacks": "2", "defensiveInterceptions": "1", "fumblesRecovered": "0", "defTD": "0", "safeties": "0", "ptsAllowed": "17"},
			"home": map[string]any{"teamAbv": home, "sacks": "1", "defensiveInterceptions": "0", "fumblesRecovered": "0", "defTD": "0", "safeties": "0", "ptsAllowed": "24"},
		},
	}
	encoded, _ := json.Marshal(map[string]any{"statusCode": 200, "body": body})
	return encoded
}

// newFixtureRelay serves the Tank01 shapes this probe reads: a
// regular-season week-1 listing, a preseason week listing (one completed
// game), and that game's box score.
func newFixtureRelay(t *testing.T, regListing, preListing, box []byte) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/getNFLGamesForWeek":
			if r.URL.Query().Get("seasonType") == "pre" {
				w.Write(preListing)
				return
			}
			w.Write(regListing)
		case "/getNFLBoxScore":
			w.Write(box)
		default:
			http.NotFound(w, r)
		}
	}))
}

func writeMirrorGamesCSV(t *testing.T, root string, rows string) {
	t.Helper()
	header := "game_id,season,game_type,week,gameday,gametime,away_team,away_score,home_team,home_score\n"
	if err := os.WriteFile(filepath.Join(root, "games.csv"), []byte(header+rows), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestRunAllChecksPassAgainstMatchingFixtures(t *testing.T) {
	reg := tank01GamesEnvelope([]gameEntry{
		{ID: "20260910_KC@BUF", Away: "KC", Home: "BUF", Date: "20260910", Status: "Scheduled", StatusCode: "0", Week: "1"},
	})
	pre := tank01GamesEnvelope([]gameEntry{
		{ID: "20260813_ARI@LV", Away: "ARI", Home: "LV", Date: "20260813", Status: "Completed", StatusCode: "2", Week: "Preseason Week 1"},
	})
	box := tank01BoxEnvelope("20260813_ARI@LV", "ARI", "LV")
	server := newFixtureRelay(t, reg, pre, box)
	defer server.Close()

	root := t.TempDir()
	writeMirrorGamesCSV(t, root, "2026_01_KC_BUF,2026,REG,1,2026-09-10,20:15,KC,,BUF,,\n")
	captureDir := t.TempDir()

	results, err := Run(context.Background(), ProbeConfig{
		Tank01BaseURL: server.URL, OpenStatsRoot: root, Season: 2026, Week: 1, PreseasonWeek: "1", CaptureDir: captureDir,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, result := range results {
		if !result.Pass {
			t.Errorf("check %q failed: %s", result.Name, result.Detail)
		}
	}
	if len(results) == 0 {
		t.Fatal("Run produced no results")
	}
	entries, err := os.ReadDir(captureDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("capture dir has %d files, want 2 (games list + box score)", len(entries))
	}
}

// TestRunDetectsAScheduleMismatch covers the diff check's failure path:
// a mirrored games.csv row whose team pair does not match Tank01's own
// listing for the same date must FAIL, not silently pass.
func TestRunDetectsAScheduleMismatch(t *testing.T) {
	reg := tank01GamesEnvelope([]gameEntry{
		{ID: "20260910_KC@BUF", Away: "KC", Home: "BUF", Date: "20260910", Status: "Scheduled", StatusCode: "0", Week: "1"},
	})
	pre := tank01GamesEnvelope([]gameEntry{
		{ID: "20260813_ARI@LV", Away: "ARI", Home: "LV", Date: "20260813", Status: "Completed", StatusCode: "2", Week: "Preseason Week 1"},
	})
	box := tank01BoxEnvelope("20260813_ARI@LV", "ARI", "LV")
	server := newFixtureRelay(t, reg, pre, box)
	defer server.Close()

	root := t.TempDir()
	// Mirror disagrees: DAL@PHI instead of KC@BUF on the same date.
	writeMirrorGamesCSV(t, root, "2026_01_DAL_PHI,2026,REG,1,2026-09-10,20:15,DAL,,PHI,,\n")

	results, err := Run(context.Background(), ProbeConfig{
		Tank01BaseURL: server.URL, OpenStatsRoot: root, Season: 2026, Week: 1, PreseasonWeek: "1",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	found := false
	for _, result := range results {
		if result.Name == "diff team pairs + Eastern dates (Tank01 vs mirrored games.csv)" {
			found = true
			if result.Pass {
				t.Fatalf("mismatched schedule reported PASS: %+v", result)
			}
		}
	}
	if !found {
		t.Fatal("diff check did not run")
	}
}

// TestRunFailsWhenTank01BaseURLIsMissing covers the setup-error path: a
// probe with no relay to talk to must return an error, not silently
// report every check as passed.
func TestRunFailsWhenTank01BaseURLIsMissing(t *testing.T) {
	if _, err := Run(context.Background(), ProbeConfig{OpenStatsRoot: t.TempDir()}); err == nil {
		t.Fatal("Run with no Tank01BaseURL did not error")
	}
}

// TestRunFailsWhenNoCompletedPreseasonGameExists covers the "pick a
// completed preseason game" check's own failure path.
func TestRunFailsWhenNoCompletedPreseasonGameExists(t *testing.T) {
	reg := tank01GamesEnvelope([]gameEntry{
		{ID: "20260910_KC@BUF", Away: "KC", Home: "BUF", Date: "20260910", Status: "Scheduled", StatusCode: "0", Week: "1"},
	})
	pre := tank01GamesEnvelope([]gameEntry{
		{ID: "20260813_ARI@LV", Away: "ARI", Home: "LV", Date: "20260813", Status: "Scheduled", StatusCode: "0", Week: "Preseason Week 1"},
	})
	server := newFixtureRelay(t, reg, pre, nil)
	defer server.Close()

	root := t.TempDir()
	writeMirrorGamesCSV(t, root, "2026_01_KC_BUF,2026,REG,1,2026-09-10,20:15,KC,,BUF,,\n")

	results, err := Run(context.Background(), ProbeConfig{
		Tank01BaseURL: server.URL, OpenStatsRoot: root, Season: 2026, Week: 1, PreseasonWeek: "1",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, result := range results {
		if result.Name == "pick a completed preseason game" && result.Pass {
			t.Fatal("a preseason week with no completed game reported PASS")
		}
	}
}

// TestCLIExitsNonZeroOnFailure covers the CLI's own exit-code contract.
func TestCLIExitsNonZeroOnFailure(t *testing.T) {
	reg := tank01GamesEnvelope([]gameEntry{
		{ID: "20260910_KC@BUF", Away: "KC", Home: "BUF", Date: "20260910", Status: "Scheduled", StatusCode: "0", Week: "1"},
	})
	pre := tank01GamesEnvelope(nil)
	server := newFixtureRelay(t, reg, pre, nil)
	defer server.Close()

	root := t.TempDir()
	writeMirrorGamesCSV(t, root, "2026_01_KC_BUF,2026,REG,1,2026-09-10,20:15,KC,,BUF,,\n")

	var stdout, stderr fakeWriter
	code := run([]string{
		"--tank01-base-url", server.URL,
		"--open-stats-root", root,
		"--season", "2026", "--week", "1", "--preseason-week", "1",
	}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("run() exited 0 with an empty preseason listing; stdout=%s", stdout.String())
	}
}

// TestCLIExitsZeroOnSuccess mirrors TestRunAllChecksPassAgainstMatchingFixtures at the CLI layer.
func TestCLIExitsZeroOnSuccess(t *testing.T) {
	reg := tank01GamesEnvelope([]gameEntry{
		{ID: "20260910_KC@BUF", Away: "KC", Home: "BUF", Date: "20260910", Status: "Scheduled", StatusCode: "0", Week: "1"},
	})
	pre := tank01GamesEnvelope([]gameEntry{
		{ID: "20260813_ARI@LV", Away: "ARI", Home: "LV", Date: "20260813", Status: "Completed", StatusCode: "2", Week: "Preseason Week 1"},
	})
	box := tank01BoxEnvelope("20260813_ARI@LV", "ARI", "LV")
	server := newFixtureRelay(t, reg, pre, box)
	defer server.Close()

	root := t.TempDir()
	writeMirrorGamesCSV(t, root, "2026_01_KC_BUF,2026,REG,1,2026-09-10,20:15,KC,,BUF,,\n")

	var stdout, stderr fakeWriter
	code := run([]string{
		"--tank01-base-url", server.URL,
		"--open-stats-root", root,
		"--season", "2026", "--week", "1", "--preseason-week", "1",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run() = %d, want 0; stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
}

type fakeWriter struct{ buf []byte }

func (w *fakeWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	return len(p), nil
}
func (w *fakeWriter) String() string { return string(w.buf) }
