package app

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"m31labs.dev/gosx/route"
)

// TestArrivalStripMarkupGatesOnServerFlagAndOffersThreeActions pins J5
// F37's page.gsx shape: the strip renders only behind
// props.ArrivalStripShown (threaded from data.arrival_strip_shown —
// arrivalStripShown, page.server.go; TeamHasSavedLineupThisWeek and
// ArrivalStripDismissed's own service-level contracts are covered
// directly in internal/league), offers the three named next actions,
// and dismisses through a plain link — this page's own template must
// stay link-only (no <form> element,
// TestHomepageActionCenterTypedAdapterRendersLinkOnly).
//
// Coordinator follow-up: the strip used to sit as Page()'s own sibling
// ahead of <ActionCenterPanel>, pushing home's masthead h1 217px below
// #main-content while every other route's masthead sits ~170px below —
// TestBrowserMastheadLeadContract's own <= 16px spread contract. It now
// renders inside ActionCenterPanel itself, between the masthead
// <header> (which still carries the h1 first) and the task list, so the
// masthead's own position never moves; this test also pins that order
// directly.
func TestArrivalStripMarkupGatesOnServerFlagAndOffersThreeActions(t *testing.T) {
	source, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	page := string(source)
	for _, want := range []string{
		`<If cond={props.ArrivalStripShown}>`,
		`<section class="score-command arrival-strip" aria-labelledby="home-arrival-heading">`,
		`<h2 id="home-arrival-heading">Three things before kickoff</h2>`,
		`<a href="/arrival-strip/dismiss" data-gosx-link class="access-link" aria-label="Dismiss this strip">Dismiss</a>`,
		`<a href="/team" data-gosx-link>Set your lineup →</a>`,
		`<a href="/players#waivers" data-gosx-link>Check waivers →</a>`,
		`<a href="/guide" data-gosx-link>Read the rules →</a>`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("home page.gsx missing %q", want)
		}
	}
	if strings.Contains(page, "<form") {
		t.Fatal("homepage template must remain link-only; the arrival strip's own dismiss control introduced a <form>")
	}
	if strings.Contains(page, "data.arrival_strip_shown") {
		t.Error("page.gsx still reads data.arrival_strip_shown directly; the strip must render inside ActionCenterPanel via props.ArrivalStripShown so it cannot sit ahead of the masthead again")
	}

	headingAt := strings.Index(page, `<h1 id="home-action-center-heading">`)
	stripAt := strings.Index(page, `<If cond={props.ArrivalStripShown}>`)
	bodyAt := strings.Index(page, `<div class="home-action-center__body">`)
	if headingAt < 0 || stripAt < 0 || bodyAt < 0 {
		t.Fatal("could not locate the masthead h1, the arrival strip gate, and the task-list body to check their order")
	}
	if !(headingAt < stripAt && stripAt < bodyAt) {
		t.Fatalf("ActionCenterPanel order = heading:%d strip:%d body:%d, want the masthead h1 first, then the arrival strip, then the task-list body", headingAt, stripAt, bodyAt)
	}
}

// TestArrivalStripShownComputesFromViewerRosterAndDismissal exercises
// arrivalStripShown directly: a seatless or demo viewer never sees the
// strip regardless of the other flags.
func TestArrivalStripShownComputesFromViewerRosterAndDismissal(t *testing.T) {
	ctx := &route.RouteContext{Request: httptest.NewRequest(http.MethodGet, "/", nil)}
	if got := arrivalStripShown(ctx, map[string]any{"team_id": "team-1"}, false, true); got {
		t.Error("arrivalStripShown = true for a signed-out viewer")
	}
	if got := arrivalStripShown(ctx, map[string]any{"team_id": "team-1"}, true, false); got {
		t.Error("arrivalStripShown = true for a seatless viewer")
	}
	if got := arrivalStripShown(ctx, map[string]any{"team_id": "team-1", "demo": true}, true, true); got {
		t.Error("arrivalStripShown = true for a demo viewer")
	}
	if got := arrivalStripShown(ctx, map[string]any{"team_id": ""}, true, true); got {
		t.Error("arrivalStripShown = true with an empty team_id")
	}
}
