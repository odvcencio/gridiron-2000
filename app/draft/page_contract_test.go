package draft

import (
	"os"
	"strings"
	"testing"
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

func TestCommissionerAutopickControlsRequireClaimedSeats(t *testing.T) {
	controls := draftSeatControlProps([]map[string]any{
		{"id": "team-1", "name": "Claimed", "claimed": true},
		{"id": "team-2", "name": "Open", "claimed": false},
	})
	if len(controls) != 1 || controls[0].TeamID != "team-1" {
		t.Fatalf("controls = %+v, want only claimed team-1", controls)
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
