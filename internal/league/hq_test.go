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

// TestBuildActionCenterAnnouncesOpenWaiverDeskWithNoClaims pins J3 F9: a
// manager with zero claims filed still needs to hear waivers are open and
// when they next run — before this fix waiverAction returned nil unless
// OpenClaims > 0, so the home page never mentioned waivers at all until a
// claim already existed.
func TestBuildActionCenterAnnouncesOpenWaiverDeskWithNoClaims(t *testing.T) {
	now := time.Date(2026, 9, 15, 8, 0, 0, 0, time.UTC)
	nextRun := now.Add(9 * time.Hour)
	facts := ActionCenterFacts{
		Now: now, Location: time.UTC, Admitted: true, HasSeat: true, DraftComplete: true,
		SeasonPhase: PhaseRegularSeason,
		Waivers:     ActionCenterWaiverFacts{OpenClaims: 0, DeskNextRun: nextRun, HasDeskNextRun: true},
	}
	action, ok := findAction(BuildActionCenter(facts).Actions, "waiver-open")
	if !ok {
		t.Fatalf("waiver-open action missing with an open desk and no claims filed")
	}
	if action.Href != "/players" {
		t.Fatalf("waiver-open href = %q, want /players", action.Href)
	}
	if !strings.Contains(action.Detail, "Waivers run") || !strings.Contains(action.Detail, "in 9 hours") {
		t.Fatalf("waiver-open detail = %q, want the run time and a relative phrase", action.Detail)
	}

	// A claim already filed keeps the existing waiver-claims card, not a
	// duplicate waiver-open card.
	facts.Waivers.OpenClaims = 1
	if _, ok := findAction(BuildActionCenter(facts).Actions, "waiver-open"); ok {
		t.Fatalf("waiver-open action present alongside a filed claim; want waiver-claims only")
	}
}

// TestActionCenterDataWiresWaiverDeskFromThePerfPriorityConfig pins the
// service-level wiring for J3 F9 against the league's real perf-priority
// waiver config (DefaultConfig's mode and 09:00 process time): a seated
// manager with no claim filed still gets a waiver-open card naming the
// desk's next scheduled run.
func TestActionCenterDataWiresWaiverDeskFromThePerfPriorityConfig(t *testing.T) {
	svc := newTestService(t, false)
	if svc.cfg.Waivers.Mode != "perf-priority" {
		t.Fatalf("test fixture waivers mode = %q, want perf-priority", svc.cfg.Waivers.Mode)
	}
	teamID := svc.Teams()[0].ID
	email := "manager@example.com"
	now := time.Date(2026, 9, 15, 8, 0, 0, 0, time.UTC)

	state := svc.store.Snapshot()
	state.Members[email] = Member{Email: email, Name: "Manager", TeamID: teamID}
	total := len(defaultTeams()) * CurrentDraftRounds()
	for i := 0; i < total; i++ {
		state.Picks = append(state.Picks, DraftPick{Number: i + 1, TeamID: defaultTeamIDs()[i%len(defaultTeamIDs())]})
	}
	viewer := map[string]any{"signed_in": true, "email": email, "has_seat": true, "team_id": teamID}
	request, err := http.NewRequest(http.MethodGet, "/", nil)
	if err != nil {
		t.Fatal(err)
	}
	data := svc.actionCenterDataForSnapshot(request, state, viewer, map[string]any{}, now)
	actions, _ := data["actions"].([]map[string]any)
	var open map[string]any
	for _, action := range actions {
		if action["id"] == "waiver-open" {
			open = action
		}
	}
	if open == nil {
		t.Fatalf("actions = %#v, want a waiver-open card with no claims filed", actions)
	}
	if open["href"] != "/players" {
		t.Fatalf("waiver-open href = %v, want /players", open["href"])
	}
	detail, _ := open["detail"].(string)
	if !strings.Contains(detail, "Waivers run") {
		t.Fatalf("waiver-open detail = %q, want it to open with \"Waivers run\"", detail)
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
