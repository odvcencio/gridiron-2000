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

func TestDraftActionSuccessRefreshesManagedRegionsWithoutNavigation(t *testing.T) {
	managedRequest := httptest.NewRequest(http.MethodPost, "/draft/__actions/test", nil)
	managedRequest.Header.Set("Accept", "application/json")
	managed := httptest.NewRecorder()
	action.ServeHandler(managed, managedRequest, func(ctx *action.Context) error {
		return draftActionSuccess(ctx, "/draft?pos=RB", "Draft state updated.")
	})
	if managed.Code != http.StatusOK || managed.Header().Get("Location") != "" {
		t.Fatalf("managed action = %d location=%q body=%s", managed.Code, managed.Header().Get("Location"), managed.Body.String())
	}
	var result struct {
		OK      bool            `json:"ok"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(managed.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	var data map[string]string
	if err := json.Unmarshal(result.Data, &data); err != nil {
		t.Fatal(err)
	}
	if !result.OK || result.Message != "Draft state updated." || data["value"] != "refresh" {
		t.Fatalf("managed result = %+v data=%v", result, data)
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
