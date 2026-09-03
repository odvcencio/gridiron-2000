package draft

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCSRFTokenParsesHiddenField(t *testing.T) {
	if got := csrfTokenFrom(`<form><input type="hidden" name="csrf_token" value="abc123"></form>`); got != "abc123" {
		t.Fatalf("csrf = %q", got)
	}
}

func TestMotifParsesFirstOption(t *testing.T) {
	if got := firstMotifFrom(`<input type="radio" name="motif" value="astronaut"><input type="radio" name="motif" value="rocket">`); got != "astronaut" {
		t.Fatalf("motif = %q", got)
	}
}

// TestCSRFTokenParsesValueBeforeName and TestMotifParsesValueBeforeName
// guard attribute-order tolerance: a renderer is free to emit an <input>'s
// value attribute before its name attribute, and the parser must still
// find it.
func TestCSRFTokenParsesValueBeforeName(t *testing.T) {
	if got := csrfTokenFrom(`<input type="hidden" value="abc123" name="csrf_token">`); got != "abc123" {
		t.Fatalf("csrf = %q", got)
	}
}

func TestMotifParsesValueBeforeName(t *testing.T) {
	if got := firstMotifFrom(`<input type="radio" value="astronaut" name="motif">`); got != "astronaut" {
		t.Fatalf("motif = %q", got)
	}
}

func TestBotSendsIdentityHeaderAndCSRF(t *testing.T) {
	headers := make(chan http.Header, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers <- r.Header.Clone()
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	bot := New(srv.URL, "east1@sim.test", "East One")
	bot.csrf = "tok"
	if _, err := bot.postAction("/draft/__actions/toggle-ready", map[string]string{"team_id": "east-1"}); err != nil {
		t.Fatal(err)
	}
	seen := <-headers
	if seen.Get("X-Test-User") != "east1@sim.test|East One" || seen.Get("X-CSRF-Token") != "tok" || seen.Get("Accept") != "application/json" {
		t.Fatalf("headers = %v", seen)
	}
}

func TestEligiblePickSkipsIneligibleRows(t *testing.T) {
	state := DraftState{Available: []map[string]any{
		{"id": "a", "draft_eligible": false},
		{"id": "b", "draft_eligible": true},
	}}
	if got := state.EligiblePick(); got != "b" {
		t.Fatalf("eligible = %q", got)
	}
}

// TestJoinSetsTeamIDFromViewerTeamID guards the only way a scenario learns
// its own seat: the server ignores a submitted team_id form field, so Join
// must re-read /test/draft's viewer_team_id after a successful claim.
func TestJoinSetsTeamIDFromViewerTeamID(t *testing.T) {
	var claimed bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/join":
			_, _ = w.Write([]byte(`<input type="hidden" name="csrf_token" value="tok"><input type="radio" name="motif" value="astronaut">`))
		case r.Method == http.MethodPost && r.URL.Path == "/join/__actions/signup-claim":
			claimed = true
			_, _ = w.Write([]byte(`{"ok":true}`))
		case r.Method == http.MethodGet && r.URL.Path == "/test/draft":
			_, _ = w.Write([]byte(`{"viewer_team_id":"east-3"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	bot := New(srv.URL, "east3@sim.test", "East Three")
	if err := bot.Join("East Three Squad"); err != nil {
		t.Fatal(err)
	}
	if !claimed {
		t.Fatal("signup-claim was never called")
	}
	if bot.TeamID != "east-3" {
		t.Fatalf("TeamID = %q, want east-3", bot.TeamID)
	}
}

// TestStateDoesNotOverrideAlreadySetTeamID guards State's other half: it
// must adopt viewer_team_id only when TeamID is still unset, never
// overwrite a value a caller (or an earlier Join) already established.
func TestStateDoesNotOverrideAlreadySetTeamID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"viewer_team_id":"east-9"}`))
	}))
	defer srv.Close()
	bot := New(srv.URL, "east9@sim.test", "East Nine")
	bot.TeamID = "east-legacy"
	if _, err := bot.State(); err != nil {
		t.Fatal(err)
	}
	if bot.TeamID != "east-legacy" {
		t.Fatalf("TeamID = %q, want it left alone", bot.TeamID)
	}
}

// TestNextPickErrorsWhenNotOnTheClock guards the pre-scan check: a draft
// that has not started (or has no seat on the clock) must fail fast with a
// clear error instead of returning some other team's draft_eligible row.
func TestNextPickErrorsWhenNotOnTheClock(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"started":false,"on_clock_id":""}`))
	}))
	defer srv.Close()
	bot := New(srv.URL, "east4@sim.test", "East Four")
	if _, err := bot.NextPick(); err == nil {
		t.Fatal("want an error when the draft has not started")
	}
}

// TestNextPickFallsBackToPunterPage guards the gridiron-house late-round
// case a bare K/DST fallback missed: a team whose only open starter slot
// is P sees no draft_eligible row on the default page (punters rank far
// below the page-1 cutoff) and none on pos=K or pos=DST either, so
// NextPick must also try pos=P before giving up — otherwise a rehearsal
// stalls at the exact pick a real gridiron-house league would make too.
func TestNextPickFallsBackToPunterPage(t *testing.T) {
	var sawPunterPage bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("pos") {
		case "P":
			sawPunterPage = true
			_, _ = w.Write([]byte(`{"started":true,"on_clock_id":"team-6","available":[{"id":"p-punter","draft_eligible":true}]}`))
		case "K":
			_, _ = w.Write([]byte(`{"started":true,"on_clock_id":"team-6","available":[{"id":"p-kicker","draft_eligible":false}]}`))
		case "DST":
			_, _ = w.Write([]byte(`{"started":true,"on_clock_id":"team-6","available":[{"id":"p-dst","draft_eligible":false}]}`))
		default:
			_, _ = w.Write([]byte(`{"started":true,"on_clock_id":"team-6","available":[{"id":"p-other","draft_eligible":false}]}`))
		}
	}))
	defer srv.Close()
	bot := New(srv.URL, "east5@sim.test", "East Five")
	got, err := bot.NextPick()
	if err != nil {
		t.Fatal(err)
	}
	if !sawPunterPage {
		t.Fatal("NextPick never requested pos=P")
	}
	if got != "p-punter" {
		t.Fatalf("NextPick = %q, want p-punter", got)
	}
}

// TestPostActionDecodeErrorIncludesBody guards the same truncated-body
// diagnostic get already carries: a non-JSON action response (an HTML
// error page, for instance) must not collapse into a bare decode error.
func TestPostActionDecodeErrorIncludesBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("<html>internal server error, this is not json</html>"))
	}))
	defer srv.Close()
	bot := New(srv.URL, "east5@sim.test", "East Five")
	bot.csrf = "tok"
	_, err := bot.postAction("/draft/__actions/toggle-ready", map[string]string{"team_id": "east-5"})
	if err == nil {
		t.Fatal("want a decode error for a non-JSON response")
	}
	if !strings.Contains(err.Error(), "internal server error") {
		t.Fatalf("error = %v, want it to include the response body", err)
	}
}
