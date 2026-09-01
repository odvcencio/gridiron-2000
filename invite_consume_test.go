package main

import (
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"gridiron-2000/internal/league"
	"gridiron-2000/internal/sim/draft"
)

// TestInviteConsumeBindsPendingCoManagerInviteExactlyLikeGoogle is the
// design's slice-3 acceptance criterion: "co-manager invite binds on
// invite-link first sign-in exactly as the Google path test does" — the
// same property test_routes_test.go's TestTestSigninBindsPendingCoManagerInvite
// proves for the harness sign-in path, exercised here over the real
// /auth/invite/{token} HTTP route instead. Both routes now call the same
// extracted completeSignIn (main.go), so this is a structural guarantee,
// not a coincidence — this test proves it holds in practice too.
func TestInviteConsumeBindsPendingCoManagerInviteExactlyLikeGoogle(t *testing.T) {
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

	primary := draft.New(srv.URL, "invite-consume-primary@sim.test", "Invite Primary")
	if err := primary.Join("Invite Consume Squad"); err != nil {
		t.Fatal(err)
	}
	if primary.TeamID == "" {
		t.Fatal("primary.TeamID was not set after Join")
	}
	const coInviteeEmail = "invite-consume-co@sim.test"
	if err := primary.InviteCoManager(coInviteeEmail); err != nil {
		t.Fatal(err)
	}

	token, err := league.Default().MintInviteLinkForTest(coInviteeEmail)
	if err != nil {
		t.Fatal(err)
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar}
	consumeInviteLinkOverHTTP(t, client, srv.URL, token)

	member, ok := league.Default().MemberByEmailForTest(coInviteeEmail)
	if !ok {
		t.Fatal("co-invitee did not become a member after consuming the invite link")
	}
	if member.TeamID != primary.TeamID {
		t.Fatalf("co-invitee TeamID = %q, want %q (the primary's team)", member.TeamID, primary.TeamID)
	}
	if member.Role == "" {
		t.Fatal("co-invitee bound with an empty Role; expected the co-manager role, not the primary slot")
	}
}

// TestInviteConsumeSeatlessWhenNoCoInvitePending covers the ordinary path:
// no pending co-invite means EnsureMember's plain, seatless membership,
// exactly like a first Google sign-in with no invite.
func TestInviteConsumeSeatlessWhenNoCoInvitePending(t *testing.T) {
	hermeticEnv(t)
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

	const email = "invite-consume-seatless@sim.test"
	token, err := league.Default().MintInviteLinkForTest(email)
	if err != nil {
		t.Fatal(err)
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar}
	consumeInviteLinkOverHTTP(t, client, srv.URL, token)

	member, ok := league.Default().MemberByEmailForTest(email)
	if !ok {
		t.Fatal("expected a plain membership after consuming an invite link with no pending co-invite")
	}
	if member.TeamID != "" {
		t.Fatalf("TeamID = %q, want empty (no seat claimed by sign-in alone)", member.TeamID)
	}
}

// TestInviteConsumeSingleUseOverHTTP proves the single-use contract end to
// end: a second visit to the exact same link, after a first successful
// consume, must not silently re-admit — it must render the truthful
// "already used" state and must not be able to consume again.
func TestInviteConsumeSingleUseOverHTTP(t *testing.T) {
	hermeticEnv(t)
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

	const email = "invite-consume-single-use@sim.test"
	token, err := league.Default().MintInviteLinkForTest(email)
	if err != nil {
		t.Fatal(err)
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar}
	consumeInviteLinkOverHTTP(t, client, srv.URL, token)

	// A second GET (fresh cookie jar, as an unrelated visitor would arrive)
	// must show the truthful "already used" state, not the confirmation.
	second := &http.Client{Jar: mustCookieJar(t)}
	response, err := second.Get(srv.URL + "/auth/invite/" + token)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "already used") {
		t.Fatalf("second visit to a consumed link did not show the truthful already-used state:\n%s", body)
	}
}

func mustCookieJar(t *testing.T) *cookiejar.Jar {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return jar
}

// consumeInviteLinkOverHTTP performs the real two-step consume a browser
// would: GET the confirmation page for its CSRF token, then POST the
// consume action, following the redirect.
func consumeInviteLinkOverHTTP(t *testing.T, client *http.Client, base, token string) {
	t.Helper()
	getResponse, err := client.Get(base + "/auth/invite/" + token)
	if err != nil {
		t.Fatal(err)
	}
	defer getResponse.Body.Close()
	body, err := io.ReadAll(getResponse.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "ENTER LEAGUE") {
		t.Fatalf("GET /auth/invite/%s did not render the confirmation button:\n%s", token, body)
	}
	csrf := extractCSRFToken(t, string(body))
	form := url.Values{"csrf_token": {csrf}}
	postResponse, err := client.PostForm(base+"/auth/invite/"+token, form)
	if err != nil {
		t.Fatal(err)
	}
	defer postResponse.Body.Close()
	io.Copy(io.Discard, postResponse.Body)
	if postResponse.StatusCode != http.StatusOK {
		t.Fatalf("POST /auth/invite/%s (after redirect) = %d, want 200", token, postResponse.StatusCode)
	}
}
