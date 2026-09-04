package league

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func findAction(actions []ActionCenterAction, id string) (ActionCenterAction, bool) {
	for _, action := range actions {
		if action.ID == id {
			return action, true
		}
	}
	return ActionCenterAction{}, false
}

func TestBuildActionCenterEntryAndPredraftMeetingTruth(t *testing.T) {
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	draftAt := now.Add(24 * time.Hour)

	entry := BuildActionCenter(ActionCenterFacts{
		Now: now, EntryState: PublicEntryCoManagerPending,
		EntryStateLabel:  "ADMITTED · CO-MANAGER INVITE",
		EntryHeadline:    "COMPLETE YOUR SHARED SEAT.",
		EntryActionHref:  "/auth/google/start?next=%2Fteam",
		EntryActionLabel: "Complete co-manager sign-in →",
		EntryDetail:      "You are invited to co-manage East 1.",
	})
	if entry.Stage != ActionCenterEntry ||
		entry.StageLabel != "ADMITTED · CO-MANAGER INVITE" ||
		entry.Heading != "COMPLETE YOUR SHARED SEAT." {
		t.Fatalf("entry = %+v", entry)
	}
	if action, ok := findAction(entry.Actions, "entry"); !ok || action.Href != "/auth/google/start?next=%2Fteam" || !action.NativeNavigation {
		t.Fatalf("entry action = %+v", entry.Actions)
	}

	predraft := BuildActionCenter(ActionCenterFacts{
		Now: now, Location: time.UTC, Admitted: true, HasSeat: true,
		DraftAt: draftAt, BoardCount: 0, Ready: false,
	})
	if predraft.Stage != ActionCenterPreDraft {
		t.Fatalf("predraft stage = %q", predraft.Stage)
	}
	for _, id := range []string{"draft-board", "draft-ready"} {
		action, ok := findAction(predraft.Actions, id)
		if !ok || !action.HasDueAt || !action.DueAt.Equal(draftAt) || action.DueLabel != "DRAFT MEETING" {
			t.Fatalf("%s action = %+v", id, action)
		}
		if !strings.Contains(action.Detail, "Draft meeting:") ||
			!strings.Contains(action.Detail, "meeting point; the commissioner starts the room intentionally") {
			t.Fatalf("%s detail = %q", id, action.Detail)
		}
		if action.NativeNavigation {
			t.Fatalf("ordinary internal action %s unexpectedly requires native navigation", id)
		}
	}

	ready := BuildActionCenter(ActionCenterFacts{
		Now: now, Location: time.UTC, Admitted: true, HasSeat: true,
		DraftAt: draftAt, BoardCount: 3, Ready: true,
	})
	action, ok := findAction(ready.Actions, "draft-info")
	if !ok || !action.HasDueAt || !action.DueAt.Equal(draftAt) || !strings.Contains(action.Detail, "Draft meeting:") {
		t.Fatalf("ready predraft action = %+v", ready.Actions)
	}
}

func TestActionCenterUsesRescheduledDraftMeeting(t *testing.T) {
	service := newTestService(t, false)
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	override := now.Add(7 * 24 * time.Hour)
	teamID := service.Teams()[0].ID
	email := "manager@example.com"
	state := service.store.Snapshot()
	state.DraftAtOverride = override
	state.Members[email] = Member{Email: email, Name: "Manager", TeamID: teamID}
	viewer := map[string]any{
		"signed_in": true,
		"email":     email,
		"has_seat":  true,
		"team_id":   teamID,
	}
	request, err := http.NewRequest(http.MethodGet, "/", nil)
	if err != nil {
		t.Fatal(err)
	}
	data := service.actionCenterDataForSnapshot(request, state, viewer, map[string]any{}, now)
	actions, _ := data["actions"].([]map[string]any)
	for _, action := range actions {
		if action["id"] != "draft-board" && action["id"] != "draft-ready" && action["id"] != "draft-info" {
			continue
		}
		if got := action["due_at"]; got != override.Format(time.RFC3339) {
			t.Fatalf("draft action due_at = %v, want rescheduled meeting %s", got, override.Format(time.RFC3339))
		}
		return
	}
	t.Fatalf("no predraft action in %#v", actions)
}

func TestBuildActionCenterSeasonCompleteSuppressesWeeklyTasks(t *testing.T) {
	got := BuildActionCenter(ActionCenterFacts{
		Now:      time.Date(2026, 12, 1, 12, 0, 0, 0, time.UTC),
		Admitted: true, HasSeat: true, DraftComplete: true, SeasonPhase: PhaseSeasonComplete,
		Lineup:  ActionCenterLineupFacts{Week: 15, Problems: 4, HasFirstKickoff: true},
		Pickem:  ActionCenterPickemFacts{Week: 15, GameCount: 4, OpenUnpicked: 2, LockedUnpicked: 1},
		Trades:  ActionCenterTradeFacts{IncomingOpen: 1, AcceptedReview: 1, OutgoingOpen: 1},
		Waivers: ActionCenterWaiverFacts{OpenClaims: 1, HasNextRun: true, NextRun: time.Date(2026, 12, 1, 13, 0, 0, 0, time.UTC)},
	})
	if got.Stage != ActionCenterSeasonComplete {
		t.Fatalf("stage = %q", got.Stage)
	}
	for _, action := range got.Actions {
		if action.ID == "lineup" || action.ID == "lineup-review" ||
			strings.HasPrefix(action.ID, "pickem-") ||
			strings.HasPrefix(action.ID, "trade-") || action.ID == "waiver-claims" {
			t.Fatalf("fabricated season-complete task: %+v", action)
		}
	}
	if _, ok := findAction(got.Actions, "record-info"); !ok {
		t.Fatalf("record-info missing: %+v", got.Actions)
	}
}

func TestBuildActionCenterTradeDeadlinesRemainDistinct(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	got := BuildActionCenter(ActionCenterFacts{
		Now: now, Admitted: true, HasSeat: true, DraftComplete: true, SeasonPhase: PhaseRegularSeason,
		Trades: ActionCenterTradeFacts{
			IncomingOpen: 1, AcceptedReview: 1, OutgoingOpen: 1,
			HasReviewDeadline: true, NextReviewDeadline: now.Add(2 * time.Hour),
			HasTradeDeadline: true, TradeDeadline: now.Add(24 * time.Hour),
		},
	})
	for _, id := range []string{"trade-review", "trade-inbox", "trade-deadline", "trade-outbox"} {
		if _, ok := findAction(got.Actions, id); !ok {
			t.Fatalf("missing %s in %+v", id, got.Actions)
		}
	}
	if got.Actions[0].ID != "trade-review" {
		t.Fatalf("accepted review was not first: %+v", got.Actions)
	}
	if action, _ := findAction(got.Actions, "trade-review"); !action.Urgent || !action.HasDueAt {
		t.Fatalf("review deadline not surfaced: %+v", got.Actions)
	}
	predraft := BuildActionCenter(ActionCenterFacts{
		Now: now, Admitted: true, HasSeat: true,
		Trades: ActionCenterTradeFacts{IncomingOpen: 1, HasTradeDeadline: true, TradeDeadline: now.Add(time.Hour)},
	})
	if _, ok := findAction(predraft.Actions, "trade-deadline"); ok {
		t.Fatalf("predraft trade deadline leaked: %+v", predraft.Actions)
	}
}

// TestBuildActionCenterStableTasksUsePlainOnTrackOrNeedsYouLabels covers
// wave-8 audit item 7: the four ActionCenterPriorityStable actions used
// to all share the shouted "STABLE TASK" PriorityLabel; each now reads a
// plain word instead — "On track" for a task the viewer has nothing
// pending to act on (a clean lineup, a Pick'em week with nothing open or
// missed, a waiver claim already filed and only waiting on the league's
// own schedule) and "Needs you" for one that is actually waiting on the
// viewer's own decision (a trade offer sitting in their inbox).
func TestBuildActionCenterStableTasksUsePlainOnTrackOrNeedsYouLabels(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	got := BuildActionCenter(ActionCenterFacts{
		Now: now, Location: time.UTC, Admitted: true, HasSeat: true,
		DraftComplete: true, SeasonPhase: PhaseRegularSeason,
		Lineup:  ActionCenterLineupFacts{Week: 3, Problems: 0},
		Pickem:  ActionCenterPickemFacts{Week: 3, GameCount: 4, OpenUnpicked: 0, LockedUnpicked: 0},
		Trades:  ActionCenterTradeFacts{IncomingOpen: 1},
		Waivers: ActionCenterWaiverFacts{OpenClaims: 1, HasNextRun: false},
	})
	onTrack := []string{"lineup-review", "pickem-review", "waiver-claims"}
	for _, id := range onTrack {
		action, ok := findAction(got.Actions, id)
		if !ok {
			t.Fatalf("%s missing from %+v", id, got.Actions)
		}
		if action.PriorityLabel != actionCenterLabelOnTrack {
			t.Errorf("%s priority label = %q, want %q", id, action.PriorityLabel, actionCenterLabelOnTrack)
		}
		if action.Priority != ActionCenterPriorityStable {
			t.Errorf("%s priority = %q, want %q", id, action.Priority, ActionCenterPriorityStable)
		}
	}
	inbox, ok := findAction(got.Actions, "trade-inbox")
	if !ok {
		t.Fatalf("trade-inbox missing from %+v", got.Actions)
	}
	if inbox.PriorityLabel != actionCenterLabelNeedsYou {
		t.Errorf("trade-inbox priority label = %q, want %q", inbox.PriorityLabel, actionCenterLabelNeedsYou)
	}
	for _, action := range got.Actions {
		if action.PriorityLabel == "STABLE TASK" {
			t.Fatalf("action %q still carries the retired STABLE TASK label", action.ID)
		}
	}
}

func TestBuildActionCenterWaiverResolutionTruth(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	facts := ActionCenterFacts{
		Now: now, Location: time.UTC, Admitted: true, HasSeat: true,
		Waivers: ActionCenterWaiverFacts{OpenClaims: 1, HasNextRun: true, NextRun: now.Add(2 * time.Hour)},
	}
	future, ok := findAction(BuildActionCenter(facts).Actions, "waiver-claims")
	if !ok || future.Priority != ActionCenterPriorityDeadline || future.Urgent ||
		!strings.Contains(future.Detail, "Processing is scheduled") {
		t.Fatalf("future waiver = %+v", future)
	}
	facts.Now = now.Add(3 * time.Hour)
	due, ok := findAction(BuildActionCenter(facts).Actions, "waiver-claims")
	if !ok || !due.Urgent || !strings.Contains(due.Detail, "processing is due now") {
		t.Fatalf("due waiver = %+v", due)
	}
}

// TestBuildActionCenterPromotesCommissionerPreDraftTaskToTheMainList pins
// F11 (gap-audit J2): two days before the draft, the home page's primary
// slot went to a manager's own pick'em review — the commissioner's real
// job (seat readiness, starting the draft) lived only in the always-
// secondary "COMMISSIONER OVERLAY" aside. The commissioner's own task now
// also appears in the SAME sorted list a manager's tasks share, at
// deadline priority with the draft's own start time as its DueAt, so it
// sorts ahead of a manager's lower-priority preparation task under the
// existing, unmodified time-remaining sort. It must not appear once the
// draft has started or completed — the "COMMISSIONER OVERLAY" aside
// already carries the commissioner's task for those stages.
func TestBuildActionCenterPromotesCommissionerPreDraftTaskToTheMainList(t *testing.T) {
	draftAt := time.Date(2026, 9, 6, 20, 5, 0, 0, time.UTC) // a Sunday
	predraft := BuildActionCenter(ActionCenterFacts{
		Now: time.Now(), Location: time.UTC, Admitted: true, HasSeat: true, Commissioner: true,
		DraftAt: draftAt, SeatCapacity: 8, ClaimedSeats: 8, ReadySeats: 4,
		BoardCount: 1, Ready: true, // the commissioner's own manager prep is already done
	})
	if predraft.Stage != ActionCenterPreDraft {
		t.Fatalf("stage = %q, want predraft", predraft.Stage)
	}
	commissionerTask, ok := findAction(predraft.Actions, "commissioner-start-draft")
	if !ok {
		t.Fatalf("commissioner-start-draft missing from the main Actions list: %+v", predraft.Actions)
	}
	if commissionerTask.Priority != ActionCenterPriorityDeadline {
		t.Fatalf("commissioner-start-draft priority = %q, want %q", commissionerTask.Priority, ActionCenterPriorityDeadline)
	}
	if !commissionerTask.HasDueAt || !commissionerTask.DueAt.Equal(draftAt) {
		t.Fatalf("commissioner-start-draft DueAt = %+v, want %v", commissionerTask, draftAt)
	}
	if want := "Start the draft Sunday"; commissionerTask.Label != want {
		t.Fatalf("commissioner-start-draft label = %q, want %q", commissionerTask.Label, want)
	}
	if want := "4 of 8 checked in · open the runbook."; commissionerTask.Detail != want {
		t.Fatalf("commissioner-start-draft detail = %q, want %q", commissionerTask.Detail, want)
	}
	// Ahead of a lower (preparation) priority task, unmodified sort rule.
	if len(predraft.Actions) == 0 || predraft.Actions[0].ID != "commissioner-start-draft" {
		t.Fatalf("commissioner-start-draft did not sort first: %+v", predraft.Actions)
	}

	live := BuildActionCenter(ActionCenterFacts{
		Now: time.Now(), Admitted: true, HasSeat: true, Commissioner: true, DraftStarted: true, DraftAt: draftAt,
	})
	if _, ok := findAction(live.Actions, "commissioner-start-draft"); ok {
		t.Fatalf("commissioner-start-draft must not appear once the draft has started: %+v", live.Actions)
	}

	complete := BuildActionCenter(ActionCenterFacts{
		Now: time.Now(), Admitted: true, HasSeat: true, Commissioner: true, DraftComplete: true, DraftAt: draftAt,
		SeasonPhase: PhaseRegularSeason,
	})
	if _, ok := findAction(complete.Actions, "commissioner-start-draft"); ok {
		t.Fatalf("commissioner-start-draft must not appear once the draft is complete: %+v", complete.Actions)
	}

	nonCommissioner := BuildActionCenter(ActionCenterFacts{
		Now: time.Now(), Admitted: true, HasSeat: true, DraftAt: draftAt,
	})
	if _, ok := findAction(nonCommissioner.Actions, "commissioner-start-draft"); ok {
		t.Fatalf("commissioner-start-draft must not appear for a non-commissioner viewer: %+v", nonCommissioner.Actions)
	}
}

func TestBuildActionCenterCommissionerAnchorsAndPostseasonCopy(t *testing.T) {
	predraft := BuildActionCenter(ActionCenterFacts{
		Now: time.Now(), Admitted: true, HasSeat: true, Commissioner: true,
		SeatCapacity: 14, ClaimedSeats: 3, ReadySeats: 2, DraftOrderSet: true,
		DraftPoolCount: 210, DraftPoolTarget: 210,
	})
	commissioner, ok := findAction(predraft.CommissionerActions, "commissioner-start")
	if !ok || commissioner.Href != "/admin?section=draft-control#admin-draft-control" {
		t.Fatalf("commissioner action = %+v", predraft.CommissionerActions)
	}
	for _, want := range []string{"3/14 seats claimed", "2/3 managers ready", "draft order is set", "210/210 players in pool"} {
		if !strings.Contains(commissioner.Detail, want) {
			t.Fatalf("commissioner detail missing %q: %s", want, commissioner.Detail)
		}
	}

	live := BuildActionCenter(ActionCenterFacts{
		Now: time.Now(), Admitted: true, HasSeat: true, Commissioner: true, DraftStarted: true,
	})
	clock, ok := findAction(live.CommissionerActions, "commissioner-clock")
	if !ok || clock.Href != "/admin?section=clock#admin-clock" {
		t.Fatalf("commissioner clock = %+v", live.CommissionerActions)
	}

	playoffs := BuildActionCenter(ActionCenterFacts{
		Now: time.Now(), Admitted: true, HasSeat: true, DraftComplete: true, SeasonPhase: PhasePlayoffs,
	})
	if strings.Contains(playoffs.Heading, "BRACKET") || strings.Contains(strings.ToLower(playoffs.Summary), "bracket") {
		t.Fatalf("playoff copy invented bracket truth: %+v", playoffs)
	}
}

func TestBuildActionCenterCommissionerWeekCloseActionability(t *testing.T) {
	base := ActionCenterFacts{
		Now: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC), Admitted: true, HasSeat: true,
		Commissioner: true, DraftComplete: true, SeasonPhase: PhaseRegularSeason,
		ScheduleExists: true, WeekCloseWeek: 1, WeekCloseGamesFinal: 8, WeekCloseGamesTotal: 8,
		WeekCloseReady: true,
	}
	ready := BuildActionCenter(base)
	action, ok := findAction(ready.CommissionerActions, "commissioner-week-close")
	if !ok || action.Href != "/admin?section=week-close#admin-week-close" || !action.Urgent || !action.Primary {
		t.Fatalf("ready week-close action = %+v", ready.CommissionerActions)
	}
	if !strings.Contains(action.Detail, "normal close") || !strings.Contains(action.Detail, "forced close") {
		t.Fatalf("week-close action collapsed the override distinction: %q", action.Detail)
	}

	waiting := base
	waiting.WeekCloseReady = false
	waiting.WeekCloseGamesFinal = 2
	if len(BuildActionCenter(waiting).CommissionerActions) != 0 {
		t.Fatalf("still-playing week fabricated a commissioner action: %+v", BuildActionCenter(waiting).CommissionerActions)
	}

	final := base
	final.WeekCloseFinal = true
	final.WeekCloseReady = true
	if len(BuildActionCenter(final).CommissionerActions) != 0 {
		t.Fatalf("final week retained a commissioner action: %+v", BuildActionCenter(final).CommissionerActions)
	}
}
