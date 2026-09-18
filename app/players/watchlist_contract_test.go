package players

import (
	"os"
	"strings"
	"testing"
)

// TestPlayerPoolWatchlistContract pins the watchlist surface: a Watchlist
// tab beside Free agents / All players, one managed star form per pool
// row for any signed-in member (seat or not), posting player_id and the
// next state to the "watch-toggle" action, and the action registered
// against league.Service.ToggleWatch.
func TestPlayerPoolWatchlistContract(t *testing.T) {
	page, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	source := string(page)
	for _, want := range []string{
		`href={data.avail_watch_href}`, `aria-current={data.avail == "watch"}`,
		`<If cond={data.can_watch}>`,
		`action={actionPath("watch-toggle")}`,
		`name="player_id" value={player.id}`,
		`name="watched" value={player.watch_toggle_value}`,
		`aria-pressed={player.watched}`,
		`{player.watch_glyph}`,
	} {
		if !strings.Contains(source, want) {
			t.Errorf("page.gsx is missing the watchlist piece %q", want)
		}
	}
	if strings.Count(source, `action={actionPath("watch-toggle")}`) != 1 {
		t.Errorf("the star form must render from exactly one place (the shared pool row), found %d", strings.Count(source, `action={actionPath("watch-toggle")}`))
	}
	server, err := os.ReadFile("page.server.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(server), `"watch-toggle": func(ctx *action.Context) error {`) {
		t.Error(`page.server.go does not register a "watch-toggle" action`)
	}
	if !strings.Contains(string(server), `ToggleWatch(ctx.Request, ctx.FormData["player_id"], ctx.FormData["watched"] == "1")`) {
		t.Error("the watch-toggle action must call ToggleWatch with the player and the requested state")
	}
}
