package join

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gridiron-2000/internal/league"
	"m31labs.dev/gosx"
	"m31labs.dev/gosx/auth"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
)

// renderJoinPage drives a real HTTP GET through the actual file router
// against this package's page.gsx and page.server.go as they sit on disk.
// It mirrors app/matchups/page_render_test.go's harness; see that file for
// why "." is the route root and why this is not a render-every-page rig.
func renderJoinPage(t *testing.T) string {
	t.Helper()
	t.Setenv("DATA_FILE", filepath.Join(t.TempDir(), "league-state.json"))
	t.Setenv("DEMO_MODE", "false")
	t.Setenv("GOOGLE_CLIENT_ID", "")
	service := league.Default()
	const email = "join-render@example.com"
	if _, err := service.EnsureMember(email, "Join Render"); err != nil {
		t.Fatalf("EnsureMember: %v", err)
	}

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
	authn := auth.New(nil, auth.Options{
		Provider: auth.ProviderFunc(func(*http.Request) (auth.User, bool) {
			return auth.User{ID: email, Email: email, Name: "Join Render"}, true
		}),
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Test-User", email)
	rec := httptest.NewRecorder()
	authn.Middleware(handler).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET / (join page) = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

// motifRadio matches one rendered badge radio with all of its attributes, so
// the tests below can assert on what the browser actually receives.
var motifRadio = regexp.MustCompile(`<input[^>]*class="badge-option__radio"[^>]*>`)

// TestJoinPageBadgeRadioIsSubmittable is the regression guard for the bug
// the league owner hit on the live /join link: the badge tiles could not be
// selected and "Claim your seat" did nothing.
//
// The cause was HTML, not Go. Each badge radio carries required="required"
// while CSS keeps it visually hidden, and a browser refuses to submit a form
// containing a required control it cannot focus to report the error on —
// Chrome logs "An invalid form control with name='motif' is not focusable"
// and cancels the submit. Every click and every keypress therefore appeared
// to do nothing, with no server request and no visible message.
//
// Nothing in the suite would have caught it. internal/league's
// registration_test.go covers claimFantasySeat thoroughly, but the form
// never reached that code: the submit died in the browser. The defect lived
// only in rendered markup, so only a render-level assertion can hold it
// down. The empty-motif case is a server-side error message instead (see
// claimFantasySeat), which is reachable precisely because the control is no
// longer required at the HTML layer.
func TestJoinPageBadgeRadioIsSubmittable(t *testing.T) {
	body := renderJoinPage(t)

	radios := motifRadio.FindAllString(body, -1)
	if len(radios) == 0 {
		t.Fatalf("join page rendered no badge radios at all; body: %s", body)
	}
	for _, radio := range radios {
		if strings.Contains(radio, "required") {
			t.Errorf("badge radio is visually hidden but carries required, which blocks form submit entirely: %s", radio)
		}
	}
}

// TestJoinPageOffersEveryBadge pins the owner's ask that a manager picks
// from all 16 motifs, with the claim gate enforced server-side rather than
// by hiding options.
func TestJoinPageOffersEveryBadge(t *testing.T) {
	body := renderJoinPage(t)

	if got, want := len(motifRadio.FindAllString(body, -1)), 16; got != want {
		t.Errorf("join page offered %d badge radios, want %d", got, want)
	}
	if !strings.Contains(body, "Claim your seat") {
		t.Errorf("join page rendered no claim submit button; body: %s", body)
	}
}

// TestJoinPageClaimFormCarriesCSRFAndFeedbackTargets protects the live
// managed action contract. GoSX's session middleware rejects a mutating
// managed request without csrf_token, and its browser projection only has a
// visible destination when the form exposes a status or field-error node.
// Without either contract the button appears inert: the action never reaches
// ClaimFantasySeat, or a validation response changes only hidden runtime
// state.
func TestJoinPageClaimFormCarriesCSRFAndFeedbackTargets(t *testing.T) {
	body := renderJoinPage(t)

	if !strings.Contains(body, `name="csrf_token"`) {
		t.Fatalf("claim form omitted csrf_token; managed POST would be rejected: %s", body)
	}
	if !strings.Contains(body, `class="error-message form-error signup-form__error" data-gosx-field-error="team_name"`) {
		t.Fatalf("claim form omitted team-name error target: %s", body)
	}
	if !strings.Contains(body, `class="error-message form-error signup-form__error" data-gosx-field-error="motif"`) {
		t.Fatalf("claim form omitted motif error target: %s", body)
	}
	if !strings.Contains(body, `id="signup-form-status" class="form-status signup-form__status"`) {
		t.Fatalf("claim form omitted visible action status target: %s", body)
	}
}

func TestJoinPageStartsWithOneAvailableBadgeSelected(t *testing.T) {
	body := renderJoinPage(t)

	radios := motifRadio.FindAllString(body, -1)
	checked := 0
	for _, radio := range radios {
		if strings.Contains(radio, "checked") {
			checked++
		}
	}
	if checked != 1 {
		t.Errorf("join page rendered %d checked badge radios, want exactly 1 safe default; radios: %v", checked, radios)
	}
}

func TestJoinPageExplainsThePathAfterClaim(t *testing.T) {
	body := renderJoinPage(t)

	for _, want := range []string{
		"Secure your seat",
		"Build your board",
		"Mark yourself ready",
		"commissioner starts the draft intentionally",
		"You can rename the team later",
		"uploading a custom team image releases the badge",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("join page missing onboarding guidance %q; body: %s", want, body)
		}
	}
}

func TestJoinPageNonClaimableEntryUsesCanonicalProjection(t *testing.T) {
	source, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	page := string(source)
	for _, want := range []string{
		"data.public_entry.state_label",
		"data.public_entry.detail",
		"data.public_entry.action_label",
		"data.public_entry.action_href",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("join page does not render canonical non-claimable field %q", want)
		}
	}
	if strings.Contains(page, "/auth/google/start?next=%2Fteam") || strings.Contains(page, "Complete co-manager sign-in") {
		t.Fatalf("join page hardcodes stale co-manager reauthentication instead of canonical public_entry: %s", page)
	}
}

// TestJoinPageH1BindsToHeadlineWhenSeatlessAndFull (F4): the h1 read
// "CLAIM YOUR FRANCHISE." even for a member the very next sentence told
// "every configured franchise is currently assigned" — a promise the
// page immediately withdrew. It forks a subprocess (following
// app/trades/page_render_test.go's own
// TestTradesSeatlessBannerFixtureProcess precedent) because
// league.Default() is a process-wide singleton: filling every seat in
// this same test binary would corrupt every sibling test in this package
// that still expects an open seat.
func TestJoinPageH1BindsToHeadlineWhenSeatlessAndFull(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestJoinAdmittedSeatlessFullFixtureProcess$")
	cmd.Env = append(os.Environ(),
		"JOIN_SEATLESS_FULL_FIXTURE=1",
		"DATA_FILE="+filepath.Join(t.TempDir(), "league-state.json"),
		"DEMO_MODE=false",
		"GOOGLE_CLIENT_ID=",
		"APP_ENV=",
		"LEAGUE_FILE=",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("join seatless-full fixture: %v\n%s", err, output)
	}
	body := string(output)

	if !strings.Contains(body, "ADMITTED · WAITING FOR A SEAT.") {
		t.Fatalf("fixture did not reach the seatless-and-full-league state: %s", body)
	}
	h1Start := strings.Index(body, "<h1")
	if h1Start < 0 {
		t.Fatalf("could not find an h1: %s", body)
	}
	h1End := strings.Index(body[h1Start:], "</h1>")
	if h1End < 0 {
		t.Fatalf("could not find the h1's closing tag: %s", body)
	}
	h1 := body[h1Start : h1Start+h1End]
	if strings.Contains(h1, "CLAIM YOUR") {
		t.Fatalf("h1 still promises a claim the member cannot make: %s", h1)
	}
	if !strings.Contains(h1, "ADMITTED") || !strings.Contains(h1, "WAITING FOR A SEAT") {
		t.Fatalf("h1 did not bind to public_entry.headline: %s", h1)
	}

	// F16: the same seatless-full fixture's own primary button ("Open
	// Pick'em HQ →") is the one place action_label's own trailing arrow
	// was doubled by a redundant <span aria-hidden="true">→</span>.
	if strings.Count(body, "→") != 1 {
		t.Fatalf("join page's primary button renders %d arrow glyphs, want exactly 1: %s", strings.Count(body, "→"), body)
	}
}

// TestJoinAdmittedSeatlessFullFixtureProcess is
// TestJoinPageH1BindsToHeadlineWhenSeatlessAndFull's own subprocess body;
// it never runs under a normal `go test` invocation (the guard below
// skips it), only when the parent test re-execs the test binary with
// JOIN_SEATLESS_FULL_FIXTURE set.
func TestJoinAdmittedSeatlessFullFixtureProcess(t *testing.T) {
	if os.Getenv("JOIN_SEATLESS_FULL_FIXTURE") == "" {
		t.Skip("fixture helper")
	}
	service := league.Default()
	for i, team := range service.Teams() {
		if _, err := service.AssignManager(fmt.Sprintf("seat-%d@example.com", i), team.Name); err != nil {
			t.Fatalf("AssignManager seat %d: %v", i, err)
		}
	}
	const seatlessEmail = "join-seatless-full@example.com"
	if _, err := service.EnsureMember(seatlessEmail, "Full League Applicant"); err != nil {
		t.Fatalf("EnsureMember seatless viewer: %v", err)
	}

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
	authn := auth.New(nil, auth.Options{
		Provider: auth.ProviderFunc(func(*http.Request) (auth.User, bool) {
			return auth.User{ID: seatlessEmail, Email: seatlessEmail, Name: "Full League Applicant"}, true
		}),
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	authn.Middleware(handler).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET / (join page, seatless full) = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	fmt.Print(rec.Body.String())
}
