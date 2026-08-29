package main

import (
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"gridiron-2000/internal/sim/draft"
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
	app, rt, err := BuildApp(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(rt.Close)
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
		Started      bool             `json:"started"`
		OnClockID    string           `json:"on_clock_id"`
		ViewerTeamID string           `json:"viewer_team_id"`
		Picks        []map[string]any `json:"picks"`
		Available    []map[string]any `json:"available"`
		Teams        []map[string]any `json:"teams"`
	}
	if err := json.NewDecoder(res.Body).Decode(&state); err != nil {
		t.Fatal(err)
	}
	if state.Started || len(state.Teams) == 0 {
		t.Fatalf("fresh league: started=%v teams=%d", state.Started, len(state.Teams))
	}
	// commish@sim.test has no seat here: it only ever signed in via the
	// X-Test-User header, never claimed a team.
	if state.ViewerTeamID != "" {
		t.Fatalf("viewer_team_id = %q, want empty for a seatless viewer", state.ViewerTeamID)
	}
}

// TestTestClockReset guards the explicit escape hatch: reset=1 must return
// the clock to wall time and clear whatever fixed instant or offset an
// earlier request left behind.
func TestTestClockReset(t *testing.T) {
	hermeticEnv(t)
	t.Setenv("GRIDIRON_TEST_AUTH", "1")
	cfg, _ := AppConfigFromEnv()
	app, rt, err := BuildApp(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(rt.Close)
	srv := httptest.NewServer(app.Build())
	defer srv.Close()

	res, err := http.Get(srv.URL + "/test/clock?set=2030-01-01T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("set clock = %d", res.StatusCode)
	}

	before := time.Now()
	res, err = http.Get(srv.URL + "/test/clock?reset=1")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("reset clock = %d", res.StatusCode)
	}
	var clock struct {
		Now time.Time `json:"now"`
	}
	if err := json.NewDecoder(res.Body).Decode(&clock); err != nil {
		t.Fatal(err)
	}
	if clock.Now.Year() == 2030 {
		t.Fatalf("clock = %v; reset=1 must clear the fixed instant", clock.Now)
	}
	if clock.Now.Before(before) || clock.Now.After(time.Now().Add(time.Minute)) {
		t.Fatalf("clock = %v, want it back on wall time near %v", clock.Now, before)
	}
}

func TestTestSigninSetsCookieAndRejectsProtocolRelativeRedirect(t *testing.T) {
	hermeticEnv(t)
	t.Setenv("GRIDIRON_TEST_AUTH", "1")
	cfg, _ := AppConfigFromEnv()
	app, rt, err := BuildApp(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(rt.Close)
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

// TestTestSigninBindsPendingCoManagerInvite mirrors main.go's Google
// callback membership sequencing: a co-manager invite pending for this
// email must bind on the harness sign-in that follows, exactly as it would
// bind on that identity's first real Google sign-in, instead of leaving
// the invitee seatless.
func TestTestSigninBindsPendingCoManagerInvite(t *testing.T) {
	hermeticEnv(t)
	t.Setenv("GRIDIRON_TEST_AUTH", "1")
	cfg, err := AppConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	app, rt, err := BuildApp(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(rt.Close)
	srv := httptest.NewServer(app.Build())
	defer srv.Close()

	primary := draft.New(srv.URL, "co-primary@sim.test", "Co Primary")
	if err := primary.Join("Co Invite Squad"); err != nil {
		t.Fatal(err)
	}
	if primary.TeamID == "" {
		t.Fatal("primary.TeamID was not set after Join")
	}
	if err := primary.InviteCoManager("co-invitee@sim.test"); err != nil {
		t.Fatal(err)
	}

	// The bind itself is a plain GET; no cookie jar is needed to observe
	// its effect — the store mutation, not session propagation, is what
	// this test checks. CheckRedirect stops before the client follows the
	// redirect, since a plain http.Get would otherwise report the final
	// landing page's own 200.
	noFollow := &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	res, err := noFollow.Get(srv.URL + "/test/signin?user=" + url.QueryEscape("co-invitee@sim.test|Co Invitee"))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("signin (co-invitee) = %d, want 303", res.StatusCode)
	}

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/test/draft", nil)
	req.Header.Set("X-Test-User", "co-invitee@sim.test|Co Invitee")
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var state struct {
		ViewerTeamID string `json:"viewer_team_id"`
	}
	if err := json.NewDecoder(res.Body).Decode(&state); err != nil {
		t.Fatal(err)
	}
	if state.ViewerTeamID != primary.TeamID {
		t.Fatalf("co-invitee viewer_team_id = %q, want %q (the bind must land the invite on sign-in)", state.ViewerTeamID, primary.TeamID)
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
	app, rt, err := BuildApp(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(rt.Close)
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

// TestTestRoutesRejectNonLoopbackRemoteAddr guards the network-exposure
// boundary: main.go binds every interface, so a RemoteAddr outside
// 127.0.0.0/8 and ::1 must be refused before any harness handler runs, not
// just admitted because GRIDIRON_TEST_AUTH happens to be set.
func TestTestRoutesRejectNonLoopbackRemoteAddr(t *testing.T) {
	hermeticEnv(t)
	t.Setenv("GRIDIRON_TEST_AUTH", "1")
	cfg, _ := AppConfigFromEnv()
	app, rt, err := BuildApp(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(rt.Close)
	handler := app.Build()

	for _, path := range []string{"/test/clock", "/test/draft", "/test/signin?user=x@sim.test"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.RemoteAddr = "10.0.0.5:1234"
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		if recorder.Code != http.StatusForbidden {
			t.Errorf("GET %s from 10.0.0.5 = %d, want 403", path, recorder.Code)
		}
	}
}
