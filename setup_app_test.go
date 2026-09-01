package main

import (
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"gridiron-2000/internal/league"
)

// newSetupTestApp builds a SETUP-state app with a hermetic Store and a
// token sink the test can read from. It returns the running test server, a
// cookie-jar client (so the encrypted session round-trips across requests
// the way a real browser's would), and a channel of every raw token minted.
func newSetupTestApp(t *testing.T) (server *httptest.Server, client *http.Client, tokens chan string) {
	t.Helper()
	t.Setenv("APP_ENV", "test")
	cfg, err := AppConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	store := league.NewStore(filepath.Join(t.TempDir(), "league-state.json"))
	t.Cleanup(func() { _ = store.Close() })
	if err := store.StartupError(); err != nil {
		t.Fatal(err)
	}
	tokens = make(chan string, 8)
	app, _, err := buildSetupAppWithTokenSink(cfg, store, func(token string) {
		select {
		case tokens <- token:
		default:
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	server = httptest.NewServer(app.Build())
	t.Cleanup(server.Close)
	client = &http.Client{Jar: jar}
	return server, client, tokens
}

func mustReadToken(t *testing.T, tokens chan string) string {
	t.Helper()
	select {
	case token := <-tokens:
		return token
	default:
		t.Fatal("no setup token was minted at boot")
		return ""
	}
}

func getBody(t *testing.T, client *http.Client, target string) (int, string) {
	t.Helper()
	response, err := client.Get(target)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, string(body)
}

func TestSetupAppServesTokenEntryFormWhenUnauthorized(t *testing.T) {
	server, client, tokens := newSetupTestApp(t)
	_ = mustReadToken(t, tokens)
	status, body := getBody(t, client, server.URL+"/setup")
	if status != http.StatusOK {
		t.Fatalf("GET /setup = %d, want 200", status)
	}
	if !strings.Contains(body, "Enter the console token") {
		t.Fatalf("unauthorized /setup did not render the token entry form:\n%s", body)
	}
}

func TestSetupAppHealthReportsSetupState(t *testing.T) {
	server, client, _ := newSetupTestApp(t)
	status, body := getBody(t, client, server.URL+"/api/health")
	if status != http.StatusOK {
		t.Fatalf("GET /api/health = %d, want 200", status)
	}
	if !strings.Contains(body, `"state":"setup"`) {
		t.Fatalf("health payload did not report state=setup:\n%s", body)
	}
	if !strings.Contains(body, `"readiness":false`) {
		t.Fatalf("health payload must report readiness=false in setup state:\n%s", body)
	}
}

func TestSetupAppLivenessAlwaysOK(t *testing.T) {
	server, client, _ := newSetupTestApp(t)
	status, _ := getBody(t, client, server.URL+"/api/live")
	if status != http.StatusOK {
		t.Fatalf("GET /api/live = %d, want 200", status)
	}
}

// TestSetupAppTokenClaimViaFullTokenizedURL covers the boot banner's
// second printed line (design section 3.3: "plus the full tokenized URL
// for copy-paste"): visiting /setup?token=<token> directly claims the
// token exactly like submitting the form, and does so exactly once.
func TestSetupAppTokenClaimViaFullTokenizedURL(t *testing.T) {
	server, client, tokens := newSetupTestApp(t)
	token := mustReadToken(t, tokens)

	status, body := getBody(t, client, server.URL+"/setup?token="+token)
	if status != http.StatusOK || !strings.Contains(body, "/setup/identity") {
		t.Fatalf("the tokenized URL must authorize and bounce to the first step; status=%d body:\n%s", status, body)
	}

	// A second visitor (fresh cookie jar) using the exact same URL must
	// not claim it a second time.
	secondJar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	second := &http.Client{Jar: secondJar}
	status, body = getBody(t, second, server.URL+"/setup?token="+token)
	if status != http.StatusOK || strings.Contains(body, "/setup/identity") {
		t.Fatalf("a second visit to the same tokenized URL must not authorize a new session; status=%d body:\n%s", status, body)
	}
	if !strings.Contains(body, "Enter the console token") {
		t.Fatalf("the second visitor should see the token entry form:\n%s", body)
	}
}

// TestSetupAppTokenSingleClaimEndToEnd is the design's slice-2 acceptance
// criterion: "token single-claim proven by test," exercised over real HTTP
// with real encrypted session cookies (not just the in-process guard unit
// test in setup_token_test.go).
func TestSetupAppTokenSingleClaimEndToEnd(t *testing.T) {
	server, client, tokens := newSetupTestApp(t)
	token := mustReadToken(t, tokens)

	// A wrong token leaves the visitor unauthorized.
	postClaim(t, client, server.URL, "not-the-token")
	if _, body := getBody(t, client, server.URL+"/setup"); !strings.Contains(body, "Enter the console token") {
		t.Fatalf("a wrong token must not authorize /setup:\n%s", body)
	}

	// The correct token authorizes this session: /setup now bounces (via
	// meta-refresh — see metaRefreshNode's doc comment) to the first
	// wizard step instead of showing the token form.
	postClaim(t, client, server.URL, token)
	status, body := getBody(t, client, server.URL+"/setup")
	if status != http.StatusOK || !strings.Contains(body, "/setup/identity") {
		t.Fatalf("the correct token must authorize /setup and bounce to the first step; status=%d body:\n%s", status, body)
	}

	// A second browser (its own cookie jar, no session) presenting the same
	// raw token must see the truthful "already claimed" outcome, not a
	// second successful claim.
	secondJar, err := cookiejarNew()
	if err != nil {
		t.Fatal(err)
	}
	second := &http.Client{Jar: secondJar}
	postClaim(t, second, server.URL, token)
	status, body = getBody(t, second, server.URL+"/setup")
	if strings.Contains(body, "/setup/identity") {
		t.Fatal("a second session claimed an already-claimed token")
	}
	if !strings.Contains(body, "Enter the console token") {
		t.Fatalf("the second session should still see the token entry form:\n%s", body)
	}
}

func cookiejarNew() (*cookiejar.Jar, error) {
	return cookiejar.New(nil)
}

func postClaim(t *testing.T, client *http.Client, base, token string) {
	t.Helper()
	// Fetch the token entry page first so the session carries a CSRF token
	// this client can echo back — sessions.Protect requires it for POST.
	_, body := getBody(t, client, base+"/setup")
	csrf := extractCSRFToken(t, body)
	form := url.Values{"token": {token}, "csrf_token": {csrf}}
	response, err := client.PostForm(base+"/setup", form)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	io.Copy(io.Discard, response.Body)
}

func extractCSRFToken(t *testing.T, body string) string {
	t.Helper()
	const marker = `name="csrf_token" value="`
	idx := strings.Index(body, marker)
	if idx < 0 {
		t.Fatalf("no csrf_token field found in body:\n%s", body)
	}
	rest := body[idx+len(marker):]
	end := strings.Index(rest, `"`)
	if end < 0 {
		t.Fatalf("malformed csrf_token field in body:\n%s", body)
	}
	return rest[:end]
}
