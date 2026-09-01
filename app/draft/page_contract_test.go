package draft

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"m31labs.dev/gosx/action"
)

func TestAutopickTimingCopyMatchesPersistedClockSemantics(t *testing.T) {
	sourceBytes, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	source := string(sourceBytes)

	for _, truth := range []string{
		"uses your Big Board, then best available",
		"does not reset this turn's grace",
		"if grace has elapsed, the next clock tick may pick",
		"Manual control keeps the full pick clock",
		"If it expires, auto-select uses your Big Board first",
		"Presence is observational. AUTO is authority.",
		"HERE, IDLE, and AWAY retain the normal pick clock",
		"NOT SEEN may receive the short safety clock only after the two-minute boot grace",
	} {
		if !strings.Contains(source, truth) {
			t.Errorf("draft autopick copy omits truthful engine behavior %q", truth)
		}
	}

	for _, falsePromise := range []string{
		"picks after a short grace",
		"keep the full pick clock",
		"starts a fresh grace",
		"resets the grace",
		"HERE, IDLE, AWAY, and NOT SEEN never shorten a pick",
	} {
		if strings.Contains(source, falsePromise) {
			t.Errorf("draft autopick copy still promises %q", falsePromise)
		}
	}
}

func TestParseSeatAutopickIsLiteral(t *testing.T) {
	for _, raw := range []string{"", "1", "TRUE", " true ", "false\n"} {
		if _, err := parseSeatAutopick(raw); err == nil {
			t.Errorf("parseSeatAutopick(%q) unexpectedly succeeded", raw)
		}
	}
	for _, raw := range []string{"true", "false"} {
		if _, err := parseSeatAutopick(raw); err != nil {
			t.Errorf("parseSeatAutopick(%q) = %v", raw, err)
		}
	}
}

func TestCommissionerReadyAndAutopickControlsRequireClaimedSeats(t *testing.T) {
	controls := draftSeatControlProps([]map[string]any{
		{"id": "team-1", "name": "Claimed", "claimed": true},
		{"id": "team-2", "name": "Open", "claimed": false},
	})
	if len(controls) != 1 || controls[0].TeamID != "team-1" {
		t.Fatalf("controls = %+v, want only claimed team-1", controls)
	}
	if controls[0].ReadyAction != draftActionPath("seat-ready") || controls[0].Action != draftActionPath("seat-autopick") {
		t.Fatalf("claimed seat actions = ready %q, AUTO %q; want both commissioner controls", controls[0].ReadyAction, controls[0].Action)
	}
}

func TestDraftTeamProjectionKeepsOpenSeatsOutOfReadiness(t *testing.T) {
	cards := draftTeamProps([]map[string]any{
		{"id": "team-1", "name": "Claimed", "claimed": true, "ready": false},
		{"id": "team-2", "name": "Open", "claimed": false, "ready": false},
	})
	if len(cards) != 2 || !cards[0].Claimed || cards[1].Claimed {
		t.Fatalf("team claim projection = %+v, want claimed and open cards", cards)
	}
	sourceBytes, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	source := string(sourceBytes)
	for _, truth := range []string{
		"<If cond={props.Claimed == false}>",
		"OPEN SEAT",
		"<If cond={props.Claimed}>",
		"<If cond={props.Ready}>",
		"<If cond={props.Ready == false}>",
	} {
		if !strings.Contains(source, truth) {
			t.Errorf("draft team readiness contract omits %q", truth)
		}
	}
}

func TestCompletedDraftReplacesMutationControlsWithNextActions(t *testing.T) {
	sourceBytes, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	source := string(sourceBytes)
	for _, truth := range []string{
		"DRAFT CLOSED · ALL PICKS LOCKED",
		"DraftComplete={props.Data.draft_complete}",
		"Open team terminal →",
		"Open player pool →",
		"Roster need",
		"props.Data.draft.complete == false",
	} {
		if !strings.Contains(source, truth) {
			t.Errorf("completed-draft contract missing %q", truth)
		}
	}
}

// TestDraftActionSuccessRedirectsManagedAndNative is the wave-1 stale-state
// fix's own contract pin for the draft room (replacing this test's former
// "...WithoutNavigation" shape, which asserted the bug: a managed reply
// carrying only {ok:true, data:{value:"refresh"}} that GoSX's managed-form
// runtime never acts on, so a room mutation never left the pre-mutation
// document on screen). draftActionSuccess must answer a managed caller with
// the same 303-plus-redirect-field JSON shape a native caller gets via
// Location — see mutation_response_shape_test.go for the fuller per-action
// coverage and the no-Location-header rationale (a plain HTTP client, such
// as gridiron-sim's Bot, must read the JSON body rather than being silently
// redirected away from it).
func TestDraftActionSuccessRedirectsManagedAndNative(t *testing.T) {
	managedRequest := httptest.NewRequest(http.MethodPost, "/draft/__actions/test", nil)
	managedRequest.Header.Set("Accept", "application/json")
	managed := httptest.NewRecorder()
	action.ServeHandler(managed, managedRequest, func(ctx *action.Context) error {
		return draftActionSuccess(ctx, "/draft?pos=RB", "Draft state updated.")
	})
	if managed.Code != http.StatusSeeOther || managed.Header().Get("Location") != "" {
		t.Fatalf("managed action = %d location=%q body=%s, want 303 with no Location header", managed.Code, managed.Header().Get("Location"), managed.Body.String())
	}
	var result struct {
		OK       bool   `json:"ok"`
		Message  string `json:"message"`
		Redirect string `json:"redirect"`
	}
	if err := json.Unmarshal(managed.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !result.OK || result.Message != "Draft state updated." || result.Redirect != "/draft?pos=RB" {
		t.Fatalf("managed result = %+v, want ok with the redirect target", result)
	}

	nativeRequest := httptest.NewRequest(http.MethodPost, "/draft/__actions/test", nil)
	native := httptest.NewRecorder()
	action.ServeHandler(native, nativeRequest, func(ctx *action.Context) error {
		return draftActionSuccess(ctx, "/draft?pos=RB", "Draft state updated.")
	})
	if native.Code != http.StatusSeeOther || native.Header().Get("Location") != "/draft?pos=RB" {
		t.Fatalf("native action = %d location=%q body=%s", native.Code, native.Header().Get("Location"), native.Body.String())
	}
}

func TestPresenceDotsCoverNormalizedAndDisplayCase(t *testing.T) {
	styles, err := os.ReadFile("../../public/styles.css")
	if err != nil {
		t.Fatal(err)
	}
	source := string(styles)
	for _, selector := range []string{
		".presence-dot[data-presence=\"idle\"]",
		".presence-dot[data-presence=\"IDLE\"]",
		".presence-dot[data-presence=\"away\"]",
		".presence-dot[data-presence=\"AWAY\"]",
	} {
		if !strings.Contains(source, selector) {
			t.Errorf("presence dot styles omit %q", selector)
		}
	}
}

// TestDraftPoolHeaderExplainsRKPROJVSADP is P1-7's own render test (UI
// pass 2026-08-30): the pool header's RK/PROJ/VS ADP jargon must carry
// either an <abbr title> or the collapsible legend — a newbie drafting
// on a phone should never have to guess what a column header means.
func TestDraftPoolHeaderExplainsRKPROJVSADP(t *testing.T) {
	sourceBytes, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	source := string(sourceBytes)
	if !strings.Contains(source, `class="pool-legend"`) {
		t.Error("draft pool header omitted the RK/PROJ/VS ADP legend (.pool-legend)")
	}
	for _, want := range []string{"RK", "PROJ", "VS ADP"} {
		if !strings.Contains(source, want) {
			t.Errorf("draft pool header lost the %q column label entirely", want)
		}
	}
	if !strings.Contains(source, `<abbr title="rank by draft market`) {
		t.Error("RK header carries no <abbr title>")
	}
	if !strings.Contains(source, `<abbr title="projected points per game">PROJ</abbr>`) {
		t.Error("PROJ header carries no <abbr title>")
	}
	if !strings.Contains(source, `<abbr title="value if drafted right now`) {
		t.Error("VS ADP header carries no <abbr title>")
	}
	if !strings.Contains(source, `H### — this league's own rank, from your scoring and roster rules.`) {
		t.Error("pool legend omitted the H### house-rank explanation")
	}
}

// TestTapeRowManagerDropsTeamNameDuplication is P3-22's own unit test
// (UI pass 2026-08-30): "Big Endians · Big Endians Manager" read as one
// duplicated, run-on fact — the harness's own bot naming (sim_draft_test.go,
// teamName+" Manager") is exactly the shape a real league's own manager
// name could just as easily take.
func TestTapeRowManagerDropsTeamNameDuplication(t *testing.T) {
	cases := []struct {
		name, team, manager, want string
	}{
		{"exact match", "Big Endians", "Big Endians", ""},
		{"case-insensitive exact match", "Big Endians", "big endians", ""},
		{"team-prefixed manager", "Big Endians", "Big Endians Manager", ""},
		{"distinct manager name", "Big Endians", "Priya Shah", "Priya Shah"},
		{"manager name merely starts similarly, no space boundary", "Big End", "Big Endians", "Big Endians"},
		{"empty manager", "Big Endians", "", ""},
		{"empty team", "", "Some Manager", "Some Manager"},
		// Finding 12 (2026-08-31 review): the old implementation sliced
		// manager by len(team) BYTES, not runes. "\u212a-Town" (KELVIN
		// SIGN, 3 bytes) makes team 8 bytes but only 6 runes, so that
		// byte slice landed mid-word in manager's plain-ASCII "k-town"
		// prefix instead of on the actual 6-rune boundary — a case a
		// byte-slicing bug can hit on any team name carrying so much as
		// one multi-byte character, not only exotic ones like this.
		{"multi-byte team name (byte length != rune count)", "\u212a-Town", "k-town Manager", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := tapeRowManager(c.team, c.manager); got != c.want {
				t.Errorf("tapeRowManager(%q, %q) = %q, want %q", c.team, c.manager, got, c.want)
			}
		})
	}
}
