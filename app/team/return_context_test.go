package team

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestTeamLineupHelpHrefPreservesSelectedTaskContext(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/team?team=team-2&week=3", nil)
	data := prepareTeamData(map[string]any{}, request)
	got, ok := data["lineup_help_href"].(string)
	if !ok {
		t.Fatalf("lineup_help_href has type %T, want string", data["lineup_help_href"])
	}
	want := "/help/lineups-locks-matchups-and-scoring?return_to=%2Fteam%3Fteam%3Dteam-2%26week%3D3%23lineup"
	if got != want {
		t.Fatalf("full-page contextual help href = %q, want %q", got, want)
	}
}

func TestTeamLineupHelpHrefNormalizesFragmentRefresh(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/team/fragment?team=team-2&week=3", nil)
	got := teamLineupHelpHref(request)
	want := "/help/lineups-locks-matchups-and-scoring?return_to=%2Fteam%3Fteam%3Dteam-2%26week%3D3%23lineup"
	if got != want {
		t.Fatalf("fragment contextual help href = %q, want %q", got, want)
	}
}

func TestTeamLineupMarkupUsesContextualHelpHref(t *testing.T) {
	page, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	source := string(page)
	if !strings.Contains(source, "href={data.lineup_help_href} data-gosx-link") {
		t.Fatal("Team lineup markup does not use its server-provided contextual help href")
	}
	if !strings.Contains(source, "Read lineup, lock, and projection guidance →") {
		t.Fatal("Team lineup markup lost its contextual help label")
	}
}
