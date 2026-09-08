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
// data.arrival_strip_shown (arrivalStripShown, page.server.go —
// TeamHasSavedLineupThisWeek and ArrivalStripDismissed's own
// service-level contracts are covered directly in internal/league),
// offers the three named next actions, and dismisses through a plain
// link — this page's own template must stay link-only (no <form>
// element, TestHomepageActionCenterTypedAdapterRendersLinkOnly).
func TestArrivalStripMarkupGatesOnServerFlagAndOffersThreeActions(t *testing.T) {
	source, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	page := string(source)
	for _, want := range []string{
		`<If cond={data.arrival_strip_shown}>`,
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
