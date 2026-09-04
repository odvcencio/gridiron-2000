package players

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gridiron-2000/internal/league"
	"m31labs.dev/gosx"
	"m31labs.dev/gosx/auth"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
)

func renderPlayersForUser(t *testing.T, handler http.Handler, email string) string {
	t.Helper()
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Test-User", email)
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET /players for %s = %d, want 200; body: %s", email, recorder.Code, recorder.Body.String())
	}
	return recorder.Body.String()
}

func TestWaiverDeskManagedFormsAndPrivateReceiptCopyContract(t *testing.T) {
	page, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	serverSource, err := os.ReadFile("page.server.go")
	if err != nil {
		t.Fatal(err)
	}
	pageText := string(page)
	for _, want := range []string{
		`actionPath("claim-move") + "#waivers"`,
		`actionPath("claim-cancel") + "#waivers"`,
		`actionPath("claim-file") + "#waivers"`,
		`data-gosx-managed="true"`, `name="csrf_token"`,
		`Move up`, `Move down`, `Claim order`, `Team waiver position`,
		`PRIVATE RECEIPTS`, `NO WAIVER RECEIPTS YET`, `THIS TEAM ONLY`,
		`Higher FAAB bids run first`,
		`drop_locked`, `RESOLVES`, `RESOLUTION OVERDUE`,
		`LOCKED UNTIL WAIVERS RUN`, `RESOLUTION DEGRADED`, `RESOLUTION UNKNOWN`,
		`pool_unavailable`, `PLAYER DATA UNAVAILABLE`, `WAIVER ACTIONS PAUSED`,
		`roster-capacity-breakdown`, `GENERAL`, `RESERVE`, `IR · OUTSIDE CAP`,
		`Reserve counts toward draftable capacity`,
	} {
		if !strings.Contains(pageText, want) {
			t.Errorf("page.gsx missing waiver desk contract %q", want)
		}
	}
	for _, want := range []string{`"claim-move"`, `waiverRedirectTarget`, `+ "#waivers"`} {
		if !strings.Contains(string(serverSource), want) {
			t.Errorf("page.server.go missing waiver route contract %q", want)
		}
	}
}

func buildPlayersAuthenticatedHandler(t *testing.T, currentEmail *string) http.Handler {
	t.Helper()
	authn := auth.New(nil, auth.Options{
		Provider: auth.ProviderFunc(func(r *http.Request) (auth.User, bool) {
			email := *currentEmail
			if header := r.Header.Get("X-Test-User"); header != "" {
				email = header
			}
			return auth.User{ID: email, Email: email, Name: "Render Fixture"}, true
		}),
	})
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
	return authn.Middleware(handler)
}

func TestPlayersPageSeatlessHidesRowActionsButKeepsBrowsing(t *testing.T) {
	t.Setenv("DATA_FILE", filepath.Join(t.TempDir(), "league-state.json"))
	t.Setenv("DEMO_MODE", "false")
	t.Setenv("GOOGLE_CLIENT_ID", "")

	service := league.Default()
	const seatlessEmail = "seatless-render@example.com"
	const seatedEmail = "seated-render@example.com"
	if _, err := service.EnsureMember(seatlessEmail, "Seatless Render"); err != nil {
		t.Fatalf("EnsureMember: %v", err)
	}
	if _, err := service.AssignManager(seatedEmail, "Seated Render"); err != nil {
		t.Fatalf("AssignManager: %v", err)
	}

	currentEmail := seatlessEmail
	handler := buildPlayersAuthenticatedHandler(t, &currentEmail)
	seatless := renderPlayersForUser(t, handler, seatlessEmail)
	for _, want := range []string{"ADMITTED · FRANCHISE OPEN", "pool-row", "FREE AGENT"} {
		if !strings.Contains(seatless, want) {
			t.Fatalf("seatless players page missing %q: %s", want, seatless)
		}
	}
	for _, forbidden := range []string{"player-add", "claim-file", "player-drop", `class="control-locked"`} {
		if strings.Contains(seatless, forbidden) {
			t.Errorf("seatless players page rendered forbidden row control %q: %s", forbidden, seatless)
		}
	}

	seated := renderPlayersForUser(t, handler, seatedEmail)
	// The honest disabled SIGN control now carries its plain-language
	// reason adjacent to the control (contract: disabled styling is not
	// the explanation; 2026-09-01 UX audit finding 7). The label itself
	// is SIGN, not the generic "Add" — gap-audit item 4 (wave 4):
	// /players' roster-add action changes league state (signing a free
	// agent), so it carries the accent SIGN/CLAIM verbs, not the private
	// list's neutral RANK.
	if !strings.Contains(seated, `disabled="disabled" title="Roster moves open after the draft.">SIGN`) {
		t.Errorf("seated pre-draft page lost its honest disabled SIGN state: %s", seated)
	}
	if !strings.Contains(seated, `<small class="control-locked__reason">Roster moves open after the draft.</small>`) {
		t.Errorf("seated pre-draft disabled Add lost its adjacent reason: %s", seated)
	}
	for _, want := range []string{
		`<details class="stat-tip">`,
		`stat-tip__summary`,
		`class="stat-tip__panel"`,
		"Projection",
		"Availability",
	} {
		if !strings.Contains(seated, want) {
			t.Errorf("players page missing accessible detail context %q: %s", want, seated)
		}
	}
	if strings.Contains(seated, `role="tooltip"`) || strings.Contains(seated, `stat-tip" tabindex="0"`) {
		t.Errorf("players page rendered a legacy tooltip trigger: %s", seated)
	}
	if !strings.Contains(seated, "<h1>PLAYER POOL</h1>") {
		t.Errorf("seated players page missing the PLAYER POOL page-name h1: %s", seated)
	}
}

// TestPlayersHeadlineNamesThePageNotTheWire is gap-audit item 3 (wave 4 —
// linden): "wire" meant two things — this page's old h1 "THE WIRE OPENS
// HERE." and /wire's own "SIGNAL WIRE." headline. The h1 on every page
// this worker owns is the page NAME, with any slogan demoted to a
// subhead; "wire" language belongs to /wire alone now.
func TestPlayersHeadlineNamesThePageNotTheWire(t *testing.T) {
	page, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	source := string(page)
	if !strings.Contains(source, "<h1>PLAYER POOL</h1>") {
		t.Errorf("players page.gsx h1 is not the plain page name %q", "PLAYER POOL")
	}
	if strings.Contains(source, "THE WIRE") {
		t.Errorf("players page.gsx still claims the WIRE name /wire owns")
	}
}

// TestPlayersSignClaimVocabularySourceContract is gap-audit item 4 (wave
// 4 — linden): the roster-add action reads SIGN for a free agent and
// CLAIM for a waiver row — both change league state, so both stay in the
// accent .draft-button style, unlike /board and /draft's neutral-state
// RANK. Asserted against the template source, not one render fixture:
// CLAIM only ever renders for a waiver-eligible row post-draft, which the
// seatless/seated pre-draft fixtures above do not construct.
func TestPlayersSignClaimVocabularySourceContract(t *testing.T) {
	page, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	source := string(page)
	for _, want := range []string{
		`<button class="draft-button" type="submit">SIGN</button>`,
		`<button class="draft-button" type="submit">CLAIM</button>`,
	} {
		if !strings.Contains(source, want) {
			t.Errorf("players page.gsx missing SIGN/CLAIM contract %q", want)
		}
	}
	if strings.Contains(source, `type="submit">Add</button>`) || strings.Contains(source, `type="submit">Claim</button>`) {
		t.Error("players page.gsx retained the old generic Add/Claim labels")
	}
}
