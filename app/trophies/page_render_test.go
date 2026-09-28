package trophies

import (
	"html"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"gridiron-2000/internal/league"
	"m31labs.dev/gosx"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
)

func trophiesHandler(t *testing.T) http.Handler {
	t.Helper()
	router := route.NewRouter()
	router.SetLayout(func(ctx *route.RouteContext, body gosx.Node) gosx.Node {
		ctx.SetLanguage("en")
		return server.HTMLDocument(ctx.Document("Test", body))
	})
	if err := router.AddDir(".", route.FileRoutesOptions{}); err != nil {
		t.Fatalf("AddDir: %v", err)
	}
	handler, err := router.BuildChecked()
	if err != nil {
		t.Fatalf("BuildChecked: %v", err)
	}
	return handler
}

func get(t *testing.T, handler http.Handler, target string) (string, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d; body: %s", target, rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, "WENT DARK") || strings.Contains(body, "render strict component") {
		t.Fatalf("GET %s rendered the error page: %s", target, body)
	}
	return body, strings.Join(strings.Fields(html.UnescapeString(body)), " ")
}

func setupLeague(t *testing.T) {
	t.Helper()
	t.Setenv("DATA_FILE", filepath.Join(t.TempDir(), "league-state.json"))
	t.Setenv("DEMO_MODE", "true")
	t.Setenv("GOOGLE_CLIENT_ID", "")
}

var everyTrophy = []string{"High Score", "Nailbiter", "Blowout", "Toilet Bowl", "Pick'em Winner", "Perfect Week", "Upset Hunter", "Contrarian", "Points Leader", "Hot Streak", "Best Picker", "Best Weekly Record"}

// An empty league still renders the whole page: the empty states, the view
// toggle, the manager picker, and every trophy's rule as visible text.
func TestTrophiesPageEmptyCase(t *testing.T) {
	setupLeague(t)
	now := time.Date(2026, time.June, 8, 15, 0, 0, 0, time.UTC)
	league.Default().SetClockForTest(func() time.Time { return now })
	t.Cleanup(func() { league.Default().SetClockForTest(nil) })
	league.Default().SetScheduleSource(func() []league.GameInfo { return nil })

	body, compact := get(t, trophiesHandler(t), "/")
	for _, want := range []string{"NO TROPHIES YET", "NO AWARDS THIS WEEK", "NOT AWARDED YET", "By week", "By manager", "Every trophy and how ties work", "Exact ties share it", "Ties go to win percentage, then wins"} {
		if !strings.Contains(compact, want) {
			t.Fatalf("empty trophy case missing %q: %s", want, body)
		}
	}
	for _, title := range everyTrophy {
		if !strings.Contains(compact, title) {
			t.Fatalf("rules list missing trophy %q: %s", title, body)
		}
	}
	if strings.Contains(body, "@") && strings.Contains(body, "mailto:") {
		t.Fatalf("page must not expose email addresses: %s", body)
	}
}

func seedSettledPickemWeek(t *testing.T) {
	t.Helper()
	now := time.Date(2026, time.June, 8, 15, 0, 0, 0, time.UTC)
	league.Default().SetClockForTest(func() time.Time { return now })
	t.Cleanup(func() { league.Default().SetClockForTest(nil) })
	games := []league.GameInfo{
		{ID: "g-final", Week: 1, Kickoff: now.Add(time.Hour), Away: "BUF", Home: "MIA", SpreadLinePresent: true, SpreadLineTenths: 35, SourceObservedAt: now.Add(-14 * 24 * time.Hour), SourceURL: "https://github.com/nflverse"},
		{ID: "g-next", Week: 2, Kickoff: now.Add(7 * 24 * time.Hour), Away: "KC", Home: "DEN", SpreadLinePresent: true, SpreadLineTenths: -25, SourceObservedAt: now.Add(-14 * 24 * time.Hour), SourceURL: "https://github.com/nflverse"},
	}
	league.Default().SetScheduleSource(func() []league.GameInfo { return games })
	req := httptest.NewRequest(http.MethodGet, "/pickem", nil)
	if _, err := league.Default().PickemSet(req, "g-final", "BUF"); err != nil {
		t.Fatalf("seed pick: %v", err)
	}
	games[0].Kickoff = now.Add(-72 * time.Hour)
	games[0].Final = true
	games[0].ScoresPresent = true
	games[0].AwayScore = 24
	games[0].HomeScore = 17
	// PickemData reconciles and freezes the market, exactly as a page GET does.
	league.Default().PickemData(httptest.NewRequest(http.MethodGet, "/pickem?week=1", nil))
}

func TestTrophiesPageWeekAndManagerViewsFromSettledPickem(t *testing.T) {
	setupLeague(t)
	seedSettledPickemWeek(t)
	handler := trophiesHandler(t)

	// Default: the latest awarded week, with the winner linked to their case.
	body, compact := get(t, handler, "/")
	for _, want := range []string{"WEEK 1", "Perfect Week", "1-0", "Best Picker", "Best Weekly Record", "Season · leader so far", "Every contested Pick'em game"} {
		if !strings.Contains(compact, want) {
			t.Fatalf("week view missing %q: %s", want, body)
		}
	}
	match := regexp.MustCompile(`href="(/trophies\?manager=m-[0-9a-f]+)"`).FindStringSubmatch(body)
	if match == nil {
		t.Fatalf("week view must link the winner to a manager view: %s", body)
	}

	// The deep link shows that manager's trophies, the week of each, and a count.
	body, compact = get(t, handler, strings.Replace(match[1], "/trophies", "/", 1)) // the test router mounts the page at "/"
	for _, want := range []string{"BY MANAGER", "trophies this season", "Week 1", "× 1", "Trophies won, by kind", "Every trophy won, with its week"} {
		if !strings.Contains(compact, want) {
			t.Fatalf("manager view missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, `id="by-week"`) {
		t.Fatalf("manager view should not render the week section: %s", body)
	}

	// A week with no award and an unknown manager both render honest empty states.
	_, compact = get(t, handler, "/?week=9")
	if !strings.Contains(compact, "NO AWARDS THIS WEEK") {
		t.Fatalf("week 9 should say nothing was awarded: %s", compact)
	}
	_, compact = get(t, handler, "/?manager=m-nobody")
	if !strings.Contains(compact, "MANAGER NOT FOUND") {
		t.Fatalf("unknown manager should say so: %s", compact)
	}
}
