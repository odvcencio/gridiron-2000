package league

import (
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
	if action, ok := findAction(entry.Actions, "entry"); !ok || action.Href != "/auth/google/start?next=%2Fteam" {
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
