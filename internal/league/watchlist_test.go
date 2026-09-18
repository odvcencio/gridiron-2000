package league

import (
	"net/http"
	"path/filepath"
	"testing"
	"time"
)

// TestSetWatchListsAndUnlistsForOneMember pins the store contract: a
// member's watchlist is keyed by their email, a toggle on adds and off
// removes, an unknown unwatch is a no-op, and the set survives a reload
// through the additive kv set (no schema move).
func TestSetWatchListsAndUnlistsForOneMember(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := NewStore(path)
	now := time.Date(2026, time.September, 18, 12, 0, 0, 0, time.UTC)
	if err := store.SetWatch("ash@example.com", "wr-1", true, now); err != nil {
		t.Fatal(err)
	}
	if err := store.SetWatch("ash@example.com", "rb-2", true, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := store.SetWatch("zoe@example.com", "wr-1", true, now); err != nil {
		t.Fatal(err)
	}
	if got := store.Snapshot().Watchlists["ash@example.com"]; len(got) != 2 || got["wr-1"].IsZero() || got["rb-2"].IsZero() {
		t.Fatalf("ash watchlist = %+v", got)
	}
	if err := store.SetWatch("ash@example.com", "nobody", false, now); err != nil {
		t.Fatalf("unwatching an unwatched player must be a no-op, got %v", err)
	}
	if err := store.SetWatch("ash@example.com", "wr-1", false, now); err != nil {
		t.Fatal(err)
	}
	reloaded := NewStore(path).Snapshot()
	if got := reloaded.Watchlists["ash@example.com"]; len(got) != 1 || got["rb-2"].IsZero() {
		t.Fatalf("ash watchlist after reload = %+v, want only rb-2", got)
	}
	if got := reloaded.Watchlists["zoe@example.com"]; len(got) != 1 {
		t.Fatalf("zoe watchlist after reload = %+v, want wr-1 untouched", got)
	}
}

// TestToggleWatchMarksPoolRowsAndFiltersTheWatchlistTab pins the service
// contract: a signed-in member toggles a pool player onto their watchlist,
// every pool row carries watched, the Watchlist tab filters the pool to
// watched players, and the count feeds the tab label.
func TestToggleWatchMarksPoolRowsAndFiltersTheWatchlistTab(t *testing.T) {
	svc, _ := newPlayersTestService(t)
	request, _ := http.NewRequest(http.MethodGet, "/players", nil)

	if _, err := svc.ToggleWatch(request, "no-such-player", true); err == nil {
		t.Fatal("watched a player who is not in the pool")
	}
	message, err := svc.ToggleWatch(request, "fa-open", true)
	if err != nil {
		t.Fatalf("watch fa-open: %v", err)
	}
	if message == "" {
		t.Fatal("watch returned no confirmation")
	}

	data := svc.PlayersData(request)
	if data["can_watch"] != true || data["watch_count"] != 1 {
		t.Fatalf("can_watch = %v, watch_count = %v", data["can_watch"], data["watch_count"])
	}
	rows, _ := data["players"].([]map[string]any)
	watchedRows := 0
	for _, row := range rows {
		if row["watched"] == true {
			watchedRows++
			if row["id"] != "fa-open" {
				t.Fatalf("watched row = %v, want fa-open", row["id"])
			}
		}
	}
	if watchedRows != 1 {
		t.Fatalf("watched rows = %d, want 1", watchedRows)
	}

	filtered, _ := http.NewRequest(http.MethodGet, "/players?avail=watch", nil)
	data = svc.PlayersData(filtered)
	rows, _ = data["players"].([]map[string]any)
	if data["avail"] != "watch" || len(rows) != 1 || rows[0]["id"] != "fa-open" {
		t.Fatalf("watchlist tab: avail = %v, rows = %d", data["avail"], len(rows))
	}

	if _, err := svc.ToggleWatch(request, "fa-open", false); err != nil {
		t.Fatal(err)
	}
	if again := svc.PlayersData(request); again["watch_count"] != 0 {
		t.Fatalf("watch_count after unwatch = %v, want 0", again["watch_count"])
	}
}
