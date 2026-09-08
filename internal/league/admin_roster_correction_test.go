package league

import (
	"net/http"
	"strings"
	"testing"
)

func TestAdminRosterCorrectionRequiresCommissioner(t *testing.T) {
	svc, _ := newPlayersTestService(t)
	svc.demoMode = false
	request, _ := http.NewRequest(http.MethodPost, "/admin", nil)
	_, err := svc.AdminRosterCorrection(request, "team-1", "rb-open", "fa-open", "fix a bad autopick")
	want := "commissioner access is required"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
	if owner := rosterOwner(currentRosters(svc.store.Snapshot())); owner["rb-open"] != "team-1" {
		t.Fatal("a rejected correction changed the roster")
	}
}

func TestAdminRosterCorrectionUnknownTeam(t *testing.T) {
	svc, _ := newPlayersTestService(t)
	request, _ := http.NewRequest(http.MethodPost, "/admin", nil)
	_, err := svc.AdminRosterCorrection(request, "ghost-team", "", "fa-open", "fix a bad autopick")
	want := "choose a team"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

func TestAdminRosterCorrectionRequiresAtLeastOneSide(t *testing.T) {
	svc, _ := newPlayersTestService(t)
	request, _ := http.NewRequest(http.MethodPost, "/admin", nil)
	_, err := svc.AdminRosterCorrection(request, "team-1", "", "", "fix a bad autopick")
	want := "choose a player to drop, add, or both"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

func TestAdminRosterCorrectionRequiresReason(t *testing.T) {
	svc, _ := newPlayersTestService(t)
	request, _ := http.NewRequest(http.MethodPost, "/admin", nil)
	_, err := svc.AdminRosterCorrection(request, "team-1", "rb-open", "fa-open", "")
	want := "a reason is required"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

func TestAdminRosterCorrectionReasonTooLong(t *testing.T) {
	svc, _ := newPlayersTestService(t)
	request, _ := http.NewRequest(http.MethodPost, "/admin", nil)
	reason := strings.Repeat("a", 241)
	_, err := svc.AdminRosterCorrection(request, "team-1", "rb-open", "fa-open", reason)
	want := "reason must be 240 characters or fewer"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

func TestAdminRosterCorrectionDropNotOnRoster(t *testing.T) {
	svc, _ := newPlayersTestService(t)
	request, _ := http.NewRequest(http.MethodPost, "/admin", nil)
	_, err := svc.AdminRosterCorrection(request, "team-1", "fa-open", "", "fix a bad autopick")
	want := lineupNotOnRosterMessage
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

func TestAdminRosterCorrectionDropLocked(t *testing.T) {
	svc, _ := newPlayersTestService(t)
	request, _ := http.NewRequest(http.MethodPost, "/admin", nil)
	_, err := svc.AdminRosterCorrection(request, "team-1", "rb-locked", "", "fix a bad autopick")
	want := "Locked Rusher is locked and cannot be dropped until the week closes"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

func TestAdminRosterCorrectionAddAlreadyRostered(t *testing.T) {
	svc, _ := newPlayersTestService(t)
	request, _ := http.NewRequest(http.MethodPost, "/admin", nil)
	_, err := svc.AdminRosterCorrection(request, "team-1", "", "other-team-player", "fix a bad autopick")
	want := "Other Team Player is already on a roster"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

func TestAdminRosterCorrectionAddOnWaivers(t *testing.T) {
	svc, now := newWaiversTestService(t)
	_ = now
	request, _ := http.NewRequest(http.MethodPost, "/admin", nil)
	_, err := svc.AdminRosterCorrection(request, "team-1", "", "wv-open", "fix a bad autopick")
	if err == nil || !strings.Contains(err.Error(), "on waivers") {
		t.Fatalf("err = %v, want an on-waivers rejection", err)
	}
}

func TestAdminRosterCorrectionAddOnlyRosterFull(t *testing.T) {
	svc, _ := newPlayersTestService(t)
	request, _ := http.NewRequest(http.MethodPost, "/admin", nil)
	_, err := svc.AdminRosterCorrection(request, "team-1", "", "fa-open", "fix a bad autopick")
	want := "the roster is full; choose a player to drop"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

// TestAdminRosterCorrectionSwapSucceeds pins the build brief's own
// motivating case: the commissioner drops a locked-in-error quarterback
// and adds a wide receiver on a NAMED team's behalf, with a required
// reason, and every write commits as one atomic correction.
func TestAdminRosterCorrectionSwapSucceeds(t *testing.T) {
	svc, now := newPlayersTestService(t)
	request, _ := http.NewRequest(http.MethodPost, "/admin", nil)
	result, err := svc.AdminRosterCorrection(request, "team-1", "rb-open", "fa-open", "team ended up with too many RBs off an autopick")
	if err != nil {
		t.Fatal(err)
	}
	if result.TeamID != "team-1" || result.AddName != "Free Agent Open" || result.DropName != "Open Rusher" {
		t.Fatalf("result = %+v", result)
	}
	wantSummary := "corrects East 1: adds Free Agent Open (RB), drops Open Rusher (RB) — reason: team ended up with too many RBs off an autopick"
	if result.Summary != wantSummary {
		t.Fatalf("summary = %q, want %q", result.Summary, wantSummary)
	}

	state := svc.store.Snapshot()
	owner := rosterOwner(currentRosters(state))
	if owner["fa-open"] != "team-1" {
		t.Fatal("fa-open must now belong to team-1")
	}
	if _, stillRostered := owner["rb-open"]; stillRostered {
		t.Fatal("rb-open must no longer belong to any roster")
	}

	if len(state.Transactions) != 1 {
		t.Fatalf("len(Transactions) = %d, want 1 (one atomic correction record)", len(state.Transactions))
	}
	txn := state.Transactions[0]
	if txn.Type != "commissioner_correction" || txn.By != "commissioner" || txn.TeamID != "team-1" {
		t.Fatalf("txn = %+v, want a commissioner_correction record for team-1", txn)
	}
	if len(txn.Adds) != 1 || txn.Adds[0].PlayerID != "fa-open" {
		t.Fatalf("txn.Adds = %+v", txn.Adds)
	}
	if len(txn.Drops) != 1 || txn.Drops[0].PlayerID != "rb-open" {
		t.Fatalf("txn.Drops = %+v", txn.Drops)
	}
	if txn.Note != "team ended up with too many RBs off an autopick" {
		t.Fatalf("txn.Note = %q", txn.Note)
	}

	if len(state.CommissionerEvents) != 1 {
		t.Fatalf("CommissionerEvents = %+v, want 1", state.CommissionerEvents)
	}
	event := state.CommissionerEvents[0]
	if event.Kind != "roster.correction" || event.Refs.TeamID != "team-1" {
		t.Fatalf("event = %+v", event)
	}
	if event.Summary != wantSummary {
		t.Fatalf("event.Summary = %q, want %q", event.Summary, wantSummary)
	}
	if event.ActorEmail == "" || event.ActorName == "" {
		t.Fatalf("event actor identity is blank: %+v", event)
	}

	notice, ok := state.RosterCorrectionNotices["team-1"]
	if !ok {
		t.Fatal("no pending roster-correction notice for team-1")
	}
	wantNotice := "The commissioner corrected your roster: adds Free Agent Open (RB), drops Open Rusher (RB) — reason: team ended up with too many RBs off an autopick"
	if notice.Summary != wantNotice {
		t.Fatalf("notice.Summary = %q, want %q", notice.Summary, wantNotice)
	}
	if notice.TeamID != "team-1" || !notice.At.Equal(now) {
		t.Fatalf("notice = %+v", notice)
	}

	poppedNotice, ok, err := svc.store.ConsumeRosterCorrectionNotice("team-1")
	if err != nil || !ok || poppedNotice.Summary != wantNotice {
		t.Fatalf("ConsumeRosterCorrectionNotice = %+v, %v, %v", poppedNotice, ok, err)
	}
	if _, stillPending := svc.store.Snapshot().RosterCorrectionNotices["team-1"]; stillPending {
		t.Fatal("notice must be gone after being consumed once")
	}
	if _, ok, err := svc.store.ConsumeRosterCorrectionNotice("team-1"); ok || err != nil {
		t.Fatal("a second consume must find nothing left")
	}
}

// TestAdminRosterCorrectionDropOnlySucceeds pins the build brief's "either
// side may be empty" contract: a drop with no simultaneous add.
func TestAdminRosterCorrectionDropOnlySucceeds(t *testing.T) {
	svc, _ := newPlayersTestService(t)
	request, _ := http.NewRequest(http.MethodPost, "/admin", nil)
	result, err := svc.AdminRosterCorrection(request, "team-1", "wr-open", "", "cutting a player who should not have been kept")
	if err != nil {
		t.Fatal(err)
	}
	if result.AddName != "" || result.DropName != "Open Wideout" {
		t.Fatalf("result = %+v", result)
	}
	if owner := rosterOwner(currentRosters(svc.store.Snapshot())); owner["wr-open"] != "" {
		t.Fatal("wr-open must be a free agent after the drop-only correction")
	}
}

// TestAdminRosterCorrectionAddOnlySucceedsWithRoomOnRoster pins the
// add-only path against a roster with an open spot: every fixture team is
// drafted to the tiny 3-spot cap, so this frees one seat first (an
// ordinary drop, not the correction under test) before proving an
// add-only correction succeeds once room exists.
func TestAdminRosterCorrectionAddOnlySucceedsWithRoomOnRoster(t *testing.T) {
	svc, _ := newPlayersTestService(t)
	request, _ := http.NewRequest(http.MethodPost, "/admin", nil)
	if _, err := svc.DropPlayer(request, "team-2", "other-team-player", playerDropConfirmation); err != nil {
		t.Fatalf("seed a free roster spot: %v", err)
	}
	result, err := svc.AdminRosterCorrection(request, "team-2", "", "fa-open", "seat needed a second body before week 1")
	if err != nil {
		t.Fatal(err)
	}
	if result.DropName != "" || result.AddName != "Free Agent Open" {
		t.Fatalf("result = %+v", result)
	}
	if owner := rosterOwner(currentRosters(svc.store.Snapshot())); owner["fa-open"] != "team-2" {
		t.Fatal("fa-open must now belong to team-2")
	}
}

// TestAdminRosterCorrectionPreviewDoesNotWrite pins the review step's own
// contract: it resolves the same names and summary AdminRosterCorrection
// would commit, but never writes a transaction, a commissioner event, or
// a /team notice.
func TestAdminRosterCorrectionPreviewDoesNotWrite(t *testing.T) {
	svc, _ := newPlayersTestService(t)
	request, _ := http.NewRequest(http.MethodPost, "/admin", nil)
	preview, err := svc.AdminRosterCorrectionPreview(request, "team-1", "rb-open", "fa-open", "double check before committing")
	if err != nil {
		t.Fatal(err)
	}
	wantSummary := "corrects East 1: adds Free Agent Open (RB), drops Open Rusher (RB) — reason: double check before committing"
	if preview.Summary != wantSummary {
		t.Fatalf("preview.Summary = %q, want %q", preview.Summary, wantSummary)
	}
	state := svc.store.Snapshot()
	if len(state.Transactions) != 0 {
		t.Fatalf("Transactions = %+v, want none from a preview call", state.Transactions)
	}
	if len(state.CommissionerEvents) != 0 {
		t.Fatalf("CommissionerEvents = %+v, want none from a preview call", state.CommissionerEvents)
	}
	if len(state.RosterCorrectionNotices) != 0 {
		t.Fatalf("RosterCorrectionNotices = %+v, want none from a preview call", state.RosterCorrectionNotices)
	}
	if owner := rosterOwner(currentRosters(state)); owner["rb-open"] != "team-1" {
		t.Fatal("a preview call changed the roster")
	}
}

// TestAdminRosterCorrectionPreviewSurfacesTheSameErrors pins that the
// review step and the commit step reject the exact same bad input, so a
// commissioner never sees a clean review followed by a surprise failure.
func TestAdminRosterCorrectionPreviewSurfacesTheSameErrors(t *testing.T) {
	svc, _ := newPlayersTestService(t)
	request, _ := http.NewRequest(http.MethodPost, "/admin", nil)
	_, err := svc.AdminRosterCorrectionPreview(request, "team-1", "rb-locked", "", "fix a bad autopick")
	want := "Locked Rusher is locked and cannot be dropped until the week closes"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

// TestAdminRosterCorrectionDataListsTeamsAndDisablesLockedDrops pins the
// console panel's own read-only projection: every team by real name, the
// selected team's roster as drop candidates with a locked player disabled
// and its reason folded into the option label, and the free-agent pool as
// add candidates sorted by projection (highest first).
func TestAdminRosterCorrectionDataListsTeamsAndDisablesLockedDrops(t *testing.T) {
	svc, _ := newPlayersTestService(t)
	request, _ := http.NewRequest(http.MethodGet, "/admin?section=roster&correction_team=team-1", nil)
	data := svc.AdminRosterCorrectionData(request)

	teamOptions, _ := data["team_options"].([]map[string]any)
	if len(teamOptions) == 0 {
		t.Fatal("team_options must list every team")
	}
	foundTeamOne := false
	for _, option := range teamOptions {
		if option["id"] == "team-1" {
			foundTeamOne = true
			if option["label"] != "East 1" || option["selected"] != true {
				t.Fatalf("team-1 option = %+v", option)
			}
		}
	}
	if !foundTeamOne {
		t.Fatal("team-1 missing from team_options")
	}
	if data["has_team"] != true || data["selected_team_name"] != "East 1" {
		t.Fatalf("has_team/selected_team_name = %v/%v", data["has_team"], data["selected_team_name"])
	}

	dropOptions, _ := data["drop_options"].([]map[string]any)
	if len(dropOptions) != 3 {
		t.Fatalf("drop_options = %+v, want 3 (team-1's whole roster)", dropOptions)
	}
	foundLocked := false
	for _, option := range dropOptions {
		if option["id"] == "rb-locked" {
			foundLocked = true
			if option["disabled"] != true {
				t.Fatalf("rb-locked option = %+v, want disabled", option)
			}
			label, _ := option["label"].(string)
			if !strings.Contains(label, "DROP LOCKED") {
				t.Fatalf("locked option label = %q, want an inline lock reason", label)
			}
		}
		if option["id"] == "rb-open" && option["disabled"] == true {
			t.Fatalf("rb-open must not be disabled: %+v", option)
		}
	}
	if !foundLocked {
		t.Fatal("rb-locked missing from drop_options")
	}

	addOptions, _ := data["add_options"].([]map[string]any)
	if len(addOptions) != 2 {
		t.Fatalf("add_options = %+v, want the fixture's 2 free agents", addOptions)
	}
	// fa-open (projection 9) must rank ahead of fa-two (projection 8).
	if addOptions[0]["id"] != "fa-open" || addOptions[1]["id"] != "fa-two" {
		t.Fatalf("add_options order = %+v, want fa-open before fa-two by projection", addOptions)
	}
}

// TestAdminRosterCorrectionDataFiltersAddOptionsByPosition pins the plain
// GET position filter (?correction_pos=) against the add list.
func TestAdminRosterCorrectionDataFiltersAddOptionsByPosition(t *testing.T) {
	svc, _ := newPlayersTestService(t)
	request, _ := http.NewRequest(http.MethodGet, "/admin?section=roster&correction_team=team-1&correction_pos=WR", nil)
	data := svc.AdminRosterCorrectionData(request)
	addOptions, _ := data["add_options"].([]map[string]any)
	if len(addOptions) != 1 || addOptions[0]["id"] != "fa-two" {
		t.Fatalf("add_options = %+v, want only the WR free agent", addOptions)
	}
}

// TestAdminRosterCorrectionDataNoTeamSelected pins the before-any-choice
// state: every team is offered, but no roster is shown yet.
func TestAdminRosterCorrectionDataNoTeamSelected(t *testing.T) {
	svc, _ := newPlayersTestService(t)
	request, _ := http.NewRequest(http.MethodGet, "/admin?section=roster", nil)
	data := svc.AdminRosterCorrectionData(request)
	if data["has_team"] != false {
		t.Fatalf("has_team = %v, want false with no team chosen", data["has_team"])
	}
	dropOptions, _ := data["drop_options"].([]map[string]any)
	addOptions, _ := data["add_options"].([]map[string]any)
	if len(dropOptions) != 0 || len(addOptions) != 0 {
		t.Fatalf("drop/add options must be empty with no team chosen: %+v / %+v", dropOptions, addOptions)
	}
}

// TestTeamDataShowsAndConsumesRosterCorrectionNoticeOnce pins the /team
// one-time flash (build brief item 3): the affected team's own next full
// page load shows "The commissioner corrected your roster: ..." exactly
// once, and a second load shows nothing.
func TestTeamDataShowsAndConsumesRosterCorrectionNoticeOnce(t *testing.T) {
	svc, _ := newPlayersTestService(t)
	adminRequest, _ := http.NewRequest(http.MethodPost, "/admin", nil)
	if _, err := svc.AdminRosterCorrection(adminRequest, "team-1", "rb-open", "fa-open", "fix a bad autopick"); err != nil {
		t.Fatal(err)
	}

	teamRequest, _ := http.NewRequest(http.MethodGet, "/team", nil)
	first := svc.TeamData(teamRequest)
	if first["has_roster_correction_notice"] != true {
		t.Fatalf("has_roster_correction_notice = %v, want true on the first load after a correction", first["has_roster_correction_notice"])
	}
	wantNotice := "The commissioner corrected your roster: adds Free Agent Open (RB), drops Open Rusher (RB) — reason: fix a bad autopick"
	if first["roster_correction_notice"] != wantNotice {
		t.Fatalf("roster_correction_notice = %q, want %q", first["roster_correction_notice"], wantNotice)
	}

	second := svc.TeamData(teamRequest)
	if second["has_roster_correction_notice"] != false || second["roster_correction_notice"] != "" {
		t.Fatalf("second load = %v/%q, want the notice gone after being shown once", second["has_roster_correction_notice"], second["roster_correction_notice"])
	}
}

// TestTeamDataReadOnlyDoesNotConsumeRosterCorrectionNotice pins the
// read-only fragment-poll boundary: polling /team/fragment must never pop
// the notice before the manager's real, full-page load ever sees it.
func TestTeamDataReadOnlyDoesNotConsumeRosterCorrectionNotice(t *testing.T) {
	svc, _ := newPlayersTestService(t)
	adminRequest, _ := http.NewRequest(http.MethodPost, "/admin", nil)
	if _, err := svc.AdminRosterCorrection(adminRequest, "team-1", "rb-open", "fa-open", "fix a bad autopick"); err != nil {
		t.Fatal(err)
	}

	teamRequest, _ := http.NewRequest(http.MethodGet, "/team", nil)
	polled := svc.TeamDataReadOnly(teamRequest)
	if polled["has_roster_correction_notice"] != false {
		t.Fatal("a read-only poll must not surface or consume the pending notice")
	}

	full := svc.TeamData(teamRequest)
	if full["has_roster_correction_notice"] != true {
		t.Fatal("the notice must still be pending for the first real full-page load")
	}
}

// TestAdminRosterCorrectionRejectsBeforeDraftCompletes mirrors AddPlayer/
// DropPlayer's own pre-draft gate: a correction only ever makes sense once
// real rosters exist.
func TestAdminRosterCorrectionRejectsBeforeDraftCompletes(t *testing.T) {
	svc := newInProgressPlayersTestService(t)
	request, _ := http.NewRequest(http.MethodPost, "/admin", nil)
	_, err := svc.AdminRosterCorrection(request, "team-1", "rb-open", "", "fix a bad autopick")
	want := "free agency opens once the draft is complete"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}
