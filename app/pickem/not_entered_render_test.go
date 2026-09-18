package pickem

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestPickemSeasonBoardNamesMembersWhoNeverEntered pins the season board's
// "Not yet entered" line: a member with no pick at all is omitted from the
// ranked board by design, and the board says who is missing instead of
// leaving their absence to be read as a bug. has_not_entered guards the
// line; not_entered_names carries the sorted display names
// (pickemNotEnteredNames, internal/league/pickem.go).
func TestPickemSeasonBoardNamesMembersWhoNeverEntered(t *testing.T) {
	base := func(hasNotEntered bool, names string) map[string]any {
		return map[string]any{
			"week": 1, "picked_count": 0, "games_empty": true,
			"week_options": []any{}, "leaderboard": []any{}, "week_leaderboard": []any{},
			"games":             []map[string]any{},
			"has_not_entered":   hasNotEntered,
			"not_entered_names": names,
		}
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	html, err := pickemFragmentRender(base(true, "Ash Quiet, Bo Silent"), req)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html, "Not yet entered:") || !strings.Contains(html, "Ash Quiet, Bo Silent") {
		t.Fatalf("season board does not name the members who never entered: %s", html)
	}

	html, err = pickemFragmentRender(base(false, ""), req)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(html, "Not yet entered") {
		t.Fatal("season board renders the not-entered line when every member has entered")
	}
}
