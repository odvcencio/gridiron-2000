package main

import (
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gridiron-2000/internal/league"
)

// wizardE2EHarness bundles what the full-flow test needs: a running SETUP
// server, a cookie-jar client already authorized past the token gate, and
// the data directory the commit will write into.
type wizardE2EHarness struct {
	server  *httptest.Server
	client  *http.Client
	dataDir string
	rt      *SetupRuntime
}

func newWizardE2EHarness(t *testing.T) *wizardE2EHarness {
	t.Helper()
	dataDir := commitTestEnv(t)
	t.Setenv("APP_ENV", "test")
	cfg, err := AppConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	store := league.NewStore(filepath.Join(dataDir, "league-state.json"))
	t.Cleanup(func() { _ = store.Close() })
	if err := store.StartupError(); err != nil {
		t.Fatal(err)
	}
	tokens := make(chan string, 8)
	app, rt, err := buildSetupAppWithTokenSink(cfg, store, func(token string) {
		select {
		case tokens <- token:
		default:
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	// Tests must never let the real process-exit side effect run — see
	// osExit's doc comment (wizard_review.go).
	rt.Restart = func() {}

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(app.Build())
	t.Cleanup(server.Close)
	client := &http.Client{Jar: jar}

	token := mustReadToken(t, tokens)
	postClaim(t, client, server.URL, token)

	return &wizardE2EHarness{server: server, client: client, dataDir: dataDir, rt: rt}
}

// postStep GETs slug's page for a CSRF token, then POSTs values to it,
// following the redirect (http.Client's default policy) and returning the
// final response body and status.
func (h *wizardE2EHarness) postStep(t *testing.T, slug string, values url.Values) (int, string) {
	t.Helper()
	_, body := getBody(t, h.client, h.server.URL+"/setup/"+slug)
	values.Set("csrf_token", extractCSRFToken(t, body))
	response, err := h.client.PostForm(h.server.URL+"/setup/"+slug, values)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	final, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, string(final)
}

// TestWizardFullFlowCommitsAValidLeagueJSON is the design's slice-3
// acceptance criterion: "a scripted run produces a league.json that
// leaguecheck passes byte-identically." It walks every step over real
// HTTP with real cookies/CSRF, exactly as a browser would, then verifies
// the committed file, the minted invite links, and the post-commit
// completion state.
func TestWizardFullFlowCommitsAValidLeagueJSON(t *testing.T) {
	h := newWizardE2EHarness(t)

	status, body := h.postStep(t, "identity", url.Values{
		"name": {"E2E League"}, "short_code": {"E2E"}, "tagline": {"Testing the wizard"},
		"mode_label": {"DYNASTY"}, "url": {"https://e2e.example.com"},
		"timezone": {"America/New_York"}, "season": {"2026"},
	})
	if status != http.StatusOK {
		t.Fatalf("identity step: status=%d body:\n%s", status, body)
	}
	if !strings.Contains(body, "Teams and divisions") {
		t.Fatalf("identity step did not advance to teams:\n%s", body)
	}

	status, body = h.postStep(t, "teams", url.Values{
		"teams": {"team-1,Alpha,ALP\nteam-2,Bravo,BRV\nteam-3,Charlie,CHA\nteam-4,Delta,DEL"},
	})
	if status != http.StatusOK || !strings.Contains(body, "Scoring format") {
		t.Fatalf("teams step did not advance to scoring: status=%d body:\n%s", status, body)
	}

	status, body = h.postStep(t, "scoring", url.Values{"scoring_format": {"half_ppr"}})
	if status != http.StatusOK || !strings.Contains(body, "Roster") {
		t.Fatalf("scoring step did not advance to roster: status=%d body:\n%s", status, body)
	}

	status, body = h.postStep(t, "roster", url.Values{"preset": {"standard"}, "bench": {"0"}, "ir": {"0"}})
	if status != http.StatusOK || !strings.Contains(body, "Draft meeting") {
		t.Fatalf("roster step did not advance to draft: status=%d body:\n%s", status, body)
	}

	status, body = h.postStep(t, "draft", url.Values{
		"at": {"2099-01-01T00:00:00Z"}, "pick_clock_seconds": {"0"}, "format_label": {"Snake"},
	})
	if status != http.StatusOK || !strings.Contains(body, "Waivers") {
		t.Fatalf("draft step did not advance to waivers: status=%d body:\n%s", status, body)
	}

	status, body = h.postStep(t, "waivers", url.Values{
		"mode": {"perf-priority"}, "season_weight_pct": {"60"}, "faab_budget": {"100"},
		"clear_days": {"2"}, "process_time": {"09:00"},
	})
	if status != http.StatusOK || !strings.Contains(body, "Trades") {
		t.Fatalf("waivers step did not advance to trades: status=%d body:\n%s", status, body)
	}

	status, body = h.postStep(t, "trades", url.Values{
		"veto": {"commissioner"}, "review_hours": {"24"}, "deadline": {""},
	})
	if status != http.StatusOK || !strings.Contains(body, "Membership") {
		t.Fatalf("trades step did not advance to membership: status=%d body:\n%s", status, body)
	}

	status, body = h.postStep(t, "membership", url.Values{
		"allowed_domain": {""}, "member_emails": {"member1@example.com\nmember2@example.com"},
	})
	if status != http.StatusOK || !strings.Contains(body, "Commissioner account") {
		t.Fatalf("membership step did not advance to commissioner: status=%d body:\n%s", status, body)
	}

	status, body = h.postStep(t, "commissioner", url.Values{
		"commissioner_email": {"commish@example.com"}, "aliases": {""},
	})
	if status != http.StatusOK || !strings.Contains(body, "Review and confirm") {
		t.Fatalf("commissioner step did not advance to review: status=%d body:\n%s", status, body)
	}
	if !strings.Contains(body, "E2E League") || !strings.Contains(body, "half_ppr") {
		t.Fatalf("review page does not render the assembled league.json:\n%s", body)
	}

	// A wrong confirmation phrase must refuse the commit.
	status, body = h.postStep(t, "review", url.Values{"confirm": {"NOPE"}})
	if status != http.StatusOK || !strings.Contains(body, "Type the exact league short code") {
		t.Fatalf("wrong confirmation phrase should refuse commit: status=%d body:\n%s", status, body)
	}
	if _, err := os.Stat(filepath.Join(h.dataDir, "league.json")); err == nil {
		t.Fatal("league.json must not exist after a refused commit")
	}

	status, body = h.postStep(t, "review", url.Values{"confirm": {"E2E"}})
	if status != http.StatusOK {
		t.Fatalf("commit: status=%d body:\n%s", status, body)
	}
	if !strings.Contains(body, "Your league is configured") {
		t.Fatalf("commit did not render the completion page:\n%s", body)
	}
	if !strings.Contains(body, "member1@example.com") || !strings.Contains(body, "member2@example.com") {
		t.Fatalf("completion page must display both minted invite links once:\n%s", body)
	}

	raw, err := os.ReadFile(filepath.Join(h.dataDir, "league.json"))
	if err != nil {
		t.Fatalf("league.json was not written: %v", err)
	}
	if _, _, err := league.LoadConfigFileBytes(filepath.Join(h.dataDir, "league.json"), raw); err != nil {
		t.Fatalf("committed league.json fails leaguecheck's own LoadConfigBytes path: %v", err)
	}

	completion, found, err := h.rt.Store.SetupCompletion()
	if err != nil || !found {
		t.Fatalf("setup_state marker missing: found=%v err=%v", found, err)
	}
	if completion.CompletedBy != "commish@example.com" {
		t.Fatalf("CompletedBy = %q, want commish@example.com", completion.CompletedBy)
	}

	links, err := h.rt.Store.InviteLinksForEmail("member1@example.com")
	if err != nil || len(links) != 1 {
		t.Fatalf("expected exactly one invite link for member1: %v (err=%v)", links, err)
	}

	// Every /setup route now renders the completion state, regardless of
	// which step it targeted — the wizard's job is over.
	status, body = getBody(t, h.client, h.server.URL+"/setup/identity")
	if status != http.StatusOK || !strings.Contains(body, "Your league is configured") {
		t.Fatalf("post-commit /setup/identity must render the completion page: status=%d body:\n%s", status, body)
	}
}

// TestWizardReviewRefusesUntilEveryStepIsDone covers the design's "The
// review step requires a clean pass with no placeholders": visiting
// /setup/review before finishing every earlier step bounces to the first
// incomplete one instead of rendering a still-placeholder-filled review.
func TestWizardReviewRefusesUntilEveryStepIsDone(t *testing.T) {
	h := newWizardE2EHarness(t)
	status, body := getBody(t, h.client, h.server.URL+"/setup/review")
	if status != http.StatusOK || !strings.Contains(body, "/setup/identity") {
		t.Fatalf("an all-TODO wizard must bounce review to the first step: status=%d body:\n%s", status, body)
	}
}

// TestWizardBackNavigationRevisitsADoneStep covers design section 4.4:
// "Any DONE step is revisitable."
func TestWizardBackNavigationRevisitsADoneStep(t *testing.T) {
	h := newWizardE2EHarness(t)
	status, body := h.postStep(t, "identity", url.Values{
		"name": {"Back Nav League"}, "short_code": {"BNL"}, "tagline": {""},
		"mode_label": {"DYNASTY"}, "url": {"https://bnl.example.com"},
		"timezone": {"America/New_York"}, "season": {"2026"},
	})
	if status != http.StatusOK {
		t.Fatalf("identity save: status=%d body:\n%s", status, body)
	}
	// Now on the teams page (identity is DONE); revisit identity directly.
	status, body = getBody(t, h.client, h.server.URL+"/setup/identity")
	if status != http.StatusOK || !strings.Contains(body, "Back Nav League") {
		t.Fatalf("revisiting a DONE step should show its saved value: status=%d body:\n%s", status, body)
	}
}

// runFullWizardHappyPath drives every step to completion and commits,
// using shortCode/leagueName so a caller can run it more than once (e.g.
// once per GRIDIRON_SUPERVISED value) without a short-code collision
// across independent harnesses. It stops the test immediately on any
// unexpected status/body, exactly like the inline flow in
// TestWizardFullFlowCommitsAValidLeagueJSON, which this does not replace —
// that test's inline form independently documents each step's exact
// expected transition; this helper exists for tests that only care about
// reaching a successful commit.
func runFullWizardHappyPath(t *testing.T, h *wizardE2EHarness, shortCode, leagueName string) {
	t.Helper()
	steps := []struct {
		slug   string
		values url.Values
	}{
		{"identity", url.Values{
			"name": {leagueName}, "short_code": {shortCode}, "tagline": {""},
			"mode_label": {"DYNASTY"}, "url": {"https://" + strings.ToLower(shortCode) + ".example.com"},
			"timezone": {"America/New_York"}, "season": {"2026"},
		}},
		{"teams", url.Values{"teams": {"team-1,Alpha,ALP\nteam-2,Bravo,BRV\nteam-3,Charlie,CHA\nteam-4,Delta,DEL"}}},
		{"scoring", url.Values{"scoring_format": {"half_ppr"}}},
		{"roster", url.Values{"preset": {"standard"}, "bench": {"0"}, "ir": {"0"}}},
		{"draft", url.Values{"at": {"2099-01-01T00:00:00Z"}, "pick_clock_seconds": {"0"}, "format_label": {"Snake"}}},
		{"waivers", url.Values{"mode": {"perf-priority"}, "season_weight_pct": {"60"}, "faab_budget": {"100"}, "clear_days": {"2"}, "process_time": {"09:00"}}},
		{"trades", url.Values{"veto": {"commissioner"}, "review_hours": {"24"}, "deadline": {""}}},
		{"membership", url.Values{"allowed_domain": {""}, "member_emails": {"member1@example.com"}}},
		{"commissioner", url.Values{"commissioner_email": {"commish@example.com"}, "aliases": {""}}},
	}
	for _, step := range steps {
		status, body := h.postStep(t, step.slug, step.values)
		if status != http.StatusOK {
			t.Fatalf("step %s: status=%d body:\n%s", step.slug, status, body)
		}
	}
	status, body := h.postStep(t, "review", url.Values{"confirm": {shortCode}})
	if status != http.StatusOK || !strings.Contains(body, "Your league is configured") {
		t.Fatalf("commit: status=%d body:\n%s", status, body)
	}
}
