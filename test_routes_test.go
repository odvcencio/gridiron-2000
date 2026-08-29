package main

import (
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"gridiron-2000/internal/league"
)

func TestTestRoutesAreAbsentWithoutHarnessFlag(t *testing.T) {
	hermeticEnv(t)
	cfg, _ := AppConfigFromEnv()
	app, _, err := BuildApp(cfg)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(app.Build())
	defer srv.Close()
	res, err := http.Get(srv.URL + "/test/clock?advance=30s")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusOK {
		t.Fatal("test route must not exist without GRIDIRON_TEST_AUTH")
	}
}

func TestTestClockAdvancesAndDraftStateIsJSON(t *testing.T) {
	hermeticEnv(t)
	t.Setenv("GRIDIRON_TEST_AUTH", "1")
	cfg, _ := AppConfigFromEnv()
	app, _, err := BuildApp(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { league.Default().SetClockForTest(nil) }) // the singleton outlives this test
	srv := httptest.NewServer(app.Build())
	defer srv.Close()
	before := time.Now()
	res, err := http.Get(srv.URL + "/test/clock?advance=30s")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("clock = %d", res.StatusCode)
	}
	var clock struct {
		Now time.Time `json:"now"`
	}
	if err := json.NewDecoder(res.Body).Decode(&clock); err != nil {
		t.Fatal(err)
	}
	if clock.Now.Before(before.Add(29 * time.Second)) {
		t.Fatalf("clock did not advance: %v", clock.Now)
	}
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/test/draft", nil)
	req.Header.Set("X-Test-User", "commish@sim.test|Commish")
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var state struct {
		Started   bool             `json:"started"`
		OnClockID string           `json:"on_clock_id"`
		Picks     []map[string]any `json:"picks"`
		Available []map[string]any `json:"available"`
		Teams     []map[string]any `json:"teams"`
	}
	if err := json.NewDecoder(res.Body).Decode(&state); err != nil {
		t.Fatal(err)
	}
	if state.Started || len(state.Teams) == 0 {
		t.Fatalf("fresh league: started=%v teams=%d", state.Started, len(state.Teams))
	}
}

func TestTestSigninSetsCookieAndRejectsProtocolRelativeRedirect(t *testing.T) {
	hermeticEnv(t)
	t.Setenv("GRIDIRON_TEST_AUTH", "1")
	cfg, _ := AppConfigFromEnv()
	app, _, err := BuildApp(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { league.Default().SetClockForTest(nil) })
	srv := httptest.NewServer(app.Build())
	defer srv.Close()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{
		Jar: jar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	identity := url.QueryEscape("browser@sim.test|Browser")
	res, err := client.Get(srv.URL + "/test/signin?user=" + identity + "&to=/draft")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("signin = %d, want 303", res.StatusCode)
	}
	if loc := res.Header.Get("Location"); loc != "/draft" {
		t.Fatalf("signin redirect Location = %q, want /draft", loc)
	}

	followClient := &http.Client{Jar: jar}
	res, err = followClient.Get(srv.URL + "/draft")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET /draft with cookie only = %d, want 200", res.StatusCode)
	}

	res, err = client.Get(srv.URL + "/test/signin?user=" + identity + "&to=//evil.example")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("signin (evil redirect) = %d, want 303", res.StatusCode)
	}
	if loc := res.Header.Get("Location"); loc != "/draft" {
		t.Fatalf("protocol-relative redirect target = %q, want it rejected to /draft", loc)
	}

	// A browser normalizes a backslash to a forward slash, so "/\evil.example"
	// becomes the same "//evil.example" open redirect at render time even
	// though it does not literally start with "//" here.
	res, err = client.Get(srv.URL + "/test/signin?user=" + identity + "&to=" + url.QueryEscape(`/\evil.example`))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("signin (backslash redirect) = %d, want 303", res.StatusCode)
	}
	if loc := res.Header.Get("Location"); loc != "/draft" {
		t.Fatalf("backslash redirect target = %q, want it rejected to /draft", loc)
	}
}

// TestTestClockRejectsPartialUpdateOnInvalidAdvance guards the ordering bug:
// a well-formed set must not commit when the same request's advance is
// malformed. The handler must parse both parameters before mutating any
// state, so a 400 always means the clock did not move at all.
func TestTestClockRejectsPartialUpdateOnInvalidAdvance(t *testing.T) {
	hermeticEnv(t)
	t.Setenv("GRIDIRON_TEST_AUTH", "1")
	cfg, _ := AppConfigFromEnv()
	app, _, err := BuildApp(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { league.Default().SetClockForTest(nil) })
	srv := httptest.NewServer(app.Build())
	defer srv.Close()

	before := time.Now()
	res, err := http.Get(srv.URL + "/test/clock?set=2030-01-01T00:00:00Z&advance=not-a-duration")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("clock (bad advance) = %d, want 400", res.StatusCode)
	}

	res, err = http.Get(srv.URL + "/test/clock")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("clock read-back = %d, want 200", res.StatusCode)
	}
	var clock struct {
		Now time.Time `json:"now"`
	}
	if err := json.NewDecoder(res.Body).Decode(&clock); err != nil {
		t.Fatal(err)
	}
	if clock.Now.Year() == 2030 {
		t.Fatalf("clock = %v; the rejected request's set must not have committed", clock.Now)
	}
	if clock.Now.Before(before) || clock.Now.After(time.Now().Add(time.Minute)) {
		t.Fatalf("clock = %v, want it still tracking wall time near %v", clock.Now, before)
	}
}
