package replay

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"gridiron-2000/internal/fantasy"
)

func TestServerAdvancesOnAScheduleAndListsTheGame(t *testing.T) {
	game := loadGame(t)
	eastern, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 13, 17, 0, 0, 0, time.UTC)
	server := Serve(game, 10*time.Second, func() time.Time { return now })
	defer server.Close()
	// NewBoxScoreClient's maxBodyBytes parameter (added by Task 1, after
	// this plan step was drafted) is 0 here, which falls back to its own
	// default cap.
	client, err := fantasy.NewBoxScoreClient(server.URL(), 2026, http.DefaultClient, 0)
	if err != nil {
		t.Fatal(err)
	}
	first, err := client.FetchBoxScore(context.Background(), "20250907_BAL@BUF")
	if err != nil || first.Period != "" {
		t.Fatalf("frame 0 = %+v %v", first, err)
	}
	now = now.Add(25 * time.Second)
	third, err := client.FetchBoxScore(context.Background(), "20250907_BAL@BUF")
	if err != nil || third.Period != "Q1" || server.ServedIndex() != 2 || server.ServedAt(2).IsZero() {
		t.Fatalf("after 25 s = %+v index=%d err=%v", third, server.ServedIndex(), err)
	}
	listings, err := client.FetchGamesForWeek(context.Background(), "reg", "1")
	wantDate := server.Start().In(eastern).Format("20060102")
	if err != nil || len(listings) != 1 || listings[0].ID != "20250907_BAL@BUF" || listings[0].Date != wantDate {
		t.Fatalf("listings = %+v %v (want date %s)", listings, err, wantDate)
	}
	games := server.ScheduleSource()()
	if len(games) != 1 || games[0].Week != 1 || games[0].Away != "BAL" || games[0].Home != "BUF" || !games[0].Kickoff.Equal(server.Start()) {
		t.Fatalf("schedule = %+v", games)
	}
}

// TestServerBoxScoreRejectsAnotherGamesID covers coordinator review
// finding 7 (commit 698ec54): this replay serves exactly one game, and a
// request naming any other gameID must 404, not silently return this
// game's frame, and must not move ServedIndex.
func TestServerBoxScoreRejectsAnotherGamesID(t *testing.T) {
	game := loadGame(t)
	now := time.Date(2026, 9, 13, 17, 0, 0, 0, time.UTC)
	server := Serve(game, 10*time.Second, func() time.Time { return now })
	defer server.Close()
	client, err := fantasy.NewBoxScoreClient(server.URL(), 2026, http.DefaultClient, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.FetchBoxScore(context.Background(), "20250907_SEA@ARI")
	var statusErr *fantasy.HTTPStatusError
	if !errors.As(err, &statusErr) || statusErr.Status != http.StatusNotFound {
		t.Fatalf("mismatched gameID = %v, want a 404 HTTPStatusError", err)
	}
	if server.ServedIndex() != 0 {
		t.Fatalf("a rejected request must not move ServedIndex: %d", server.ServedIndex())
	}
}
