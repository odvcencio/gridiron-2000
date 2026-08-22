package join

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"m31labs.dev/gosx"
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
	t.Setenv("DEMO_MODE", "true")
	t.Setenv("GOOGLE_CLIENT_ID", "")

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

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

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
