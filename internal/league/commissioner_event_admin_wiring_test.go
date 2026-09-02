// Wave-2 commissioner-console audit trail: one happy-path test per
// remaining commissioner-gated Admin* mutation (admin.go, rosterops.go's
// AdminRunWaivers and season.go's AdminCloseWeek have their own dedicated
// tests colocated with their existing fixtures). Each test asserts the
// action leaves exactly one CommissionerEvent with the expected kind and,
// where the action is naturally scoped to one entity, the expected Refs.
package league

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAdminGenerateScheduleRecordsCommissionerEvent(t *testing.T) {
	service := newTestService(t, true)
	request, _ := http.NewRequest(http.MethodGet, "/admin", nil)
	if _, err := service.AdminGenerateSchedule(request, 14, 1, 42); err != nil {
		t.Fatal(err)
	}
	events := service.store.Snapshot().CommissionerEvents
	if len(events) != 1 || events[0].Kind != "schedule.generate" {
		t.Fatalf("commissioner events = %+v, want one schedule.generate row", events)
	}
}

func TestAdminRegenerateScheduleRecordsCommissionerEvent(t *testing.T) {
	service := newTestService(t, true)
	request, _ := http.NewRequest(http.MethodGet, "/admin", nil)
	if _, err := service.AdminGenerateSchedule(request, 14, 1, 111); err != nil {
		t.Fatal(err)
	}
	if _, err := service.AdminRegenerateSchedule(request, 0, 0); err != nil {
		t.Fatal(err)
	}
	events := service.store.Snapshot().CommissionerEvents
	if len(events) != 2 || events[1].Kind != "schedule.regenerate" {
		t.Fatalf("commissioner events = %+v, want [schedule.generate, schedule.regenerate]", events)
	}
}

func TestAdminAddInviteRecordsCommissionerEvent(t *testing.T) {
	service := newTestService(t, true)
	request, _ := http.NewRequest(http.MethodGet, "/admin", nil)
	if err := service.AdminAddInvite(request, "invitee@example.com"); err != nil {
		t.Fatal(err)
	}
	events := service.store.Snapshot().CommissionerEvents
	if len(events) != 1 || events[0].Kind != "invite.add" {
		t.Fatalf("commissioner events = %+v, want one invite.add row", events)
	}
}

func TestAdminSendInviteRecordsCommissionerEvent(t *testing.T) {
	service := newTestService(t, true)
	request, _ := http.NewRequest(http.MethodGet, "/admin", nil)
	if _, err := service.AdminSendInvite(request, "invitee-send@example.com"); err != nil {
		t.Fatal(err)
	}
	events := service.store.Snapshot().CommissionerEvents
	if len(events) != 1 || events[0].Kind != "invite.send" {
		t.Fatalf("commissioner events = %+v, want one invite.send row", events)
	}
}

func TestAdminRemoveInviteRecordsCommissionerEvent(t *testing.T) {
	service := newTestService(t, true)
	request, _ := http.NewRequest(http.MethodGet, "/admin", nil)
	if err := service.AdminAddInvite(request, "remove-me@example.com"); err != nil {
		t.Fatal(err)
	}
	if err := service.AdminRemoveInvite(request, "remove-me@example.com"); err != nil {
		t.Fatal(err)
	}
	events := service.store.Snapshot().CommissionerEvents
	if len(events) != 2 || events[1].Kind != "invite.remove" {
		t.Fatalf("commissioner events = %+v, want [invite.add, invite.remove]", events)
	}
}

func TestAdminReleaseSeatRecordsCommissionerEvent(t *testing.T) {
	service := newTestService(t, true)
	request, _ := http.NewRequest(http.MethodPost, "/admin", nil)
	if _, _, err := service.store.AssignMember("release-target@example.com", "Target"); err != nil {
		t.Fatal(err)
	}
	team := service.Teams()[0]
	token := seatReleaseToken(service.store.Snapshot(), team.ID, team.Name)
	if _, err := service.AdminReleaseSeat(request, team.ID, seatReleaseConfirmation(team.ID, team.Name), token); err != nil {
		t.Fatal(err)
	}
	events := service.store.Snapshot().CommissionerEvents
	if len(events) != 1 || events[0].Kind != "seat.release" || events[0].Refs.TeamID != team.ID {
		t.Fatalf("commissioner events = %+v, want one seat.release row for %s", events, team.ID)
	}
}

func TestAdminRenameTeamRecordsCommissionerEvent(t *testing.T) {
	service := newTestService(t, true)
	request, _ := http.NewRequest(http.MethodGet, "/admin", nil)
	if _, err := service.AdminRenameTeam(request, "team-1", "Renamed Franchise"); err != nil {
		t.Fatal(err)
	}
	events := service.store.Snapshot().CommissionerEvents
	if len(events) != 1 || events[0].Kind != "seat.rename" || events[0].Refs.TeamID != "team-1" {
		t.Fatalf("commissioner events = %+v, want one seat.rename row for team-1", events)
	}
}

func TestAdminResetDraftRecordsCommissionerEvent(t *testing.T) {
	service := newTestService(t, true)
	request, _ := http.NewRequest(http.MethodGet, "/admin", nil)
	if err := service.AdminResetDraft(request, ResetDraftConfirmation); err != nil {
		t.Fatal(err)
	}
	events := service.store.Snapshot().CommissionerEvents
	if len(events) != 1 || events[0].Kind != "draft.reset" {
		t.Fatalf("commissioner events = %+v, want one draft.reset row", events)
	}
}

func TestAdminUndoPickRecordsCommissionerEvent(t *testing.T) {
	service := newTestService(t, true)
	request, _ := http.NewRequest(http.MethodGet, "/admin", nil)
	team := teamOnClock(nil, 1)
	if _, err := service.store.MakePick(team, "p-01", "manager", service.clock(), time.Time{}); err != nil {
		t.Fatal(err)
	}
	if err := service.AdminUndoPick(request, draftPreviousPickToken(service.store.Snapshot())); err != nil {
		t.Fatal(err)
	}
	events := service.store.Snapshot().CommissionerEvents
	if len(events) != 1 || events[0].Kind != "draft.undo_pick" || events[0].Refs.TeamID != team || events[0].Refs.PlayerID != "p-01" {
		t.Fatalf("commissioner events = %+v, want one draft.undo_pick row for %s/p-01", events, team)
	}
}

func TestAdminRescheduleDraftRecordsCommissionerEvent(t *testing.T) {
	service := newTestService(t, true)
	now := time.Date(2026, time.August, 23, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	request, _ := http.NewRequest(http.MethodPost, "/admin", nil)
	if err := service.AdminRescheduleDraft(request, "2026-08-24T11:00"); err != nil {
		t.Fatal(err)
	}
	events := service.store.Snapshot().CommissionerEvents
	if len(events) != 1 || events[0].Kind != "draft.reschedule" {
		t.Fatalf("commissioner events = %+v, want one draft.reschedule row", events)
	}
}

func TestAdminResetLeagueRecordsCommissionerEvent(t *testing.T) {
	service := newTestService(t, true)
	request := httptest.NewRequest(http.MethodPost, "/admin", nil)
	if err := service.AdminResetLeague(request, ResetLeagueConfirmation); err != nil {
		t.Fatal(err)
	}
	events := service.store.Snapshot().CommissionerEvents
	if len(events) != 1 || events[0].Kind != "league.reset" {
		t.Fatalf("commissioner events = %+v, want one league.reset row", events)
	}
}

func TestTrimUnclaimedSeatsRecordsCommissionerEvent(t *testing.T) {
	t.Cleanup(clearSeatTrim)
	service := newTestService(t, true)
	request, _ := http.NewRequest(http.MethodPost, "/admin", nil)
	// The trim floor (minTeams) requires at least 4 claimed seats, the
	// same fixture shape TestTrimUnclaimedSeatsMinFloor uses.
	for i := 0; i < 4; i++ {
		claimSeat(t, service, fmt.Sprintf("trim-claim-%d@example.com", i))
	}
	if _, _, err := trimUnclaimedSeatsForTest(t, service, request); err != nil {
		t.Fatal(err)
	}
	events := service.store.Snapshot().CommissionerEvents
	if len(events) != 1 || events[0].Kind != "seat.trim" {
		t.Fatalf("commissioner events = %+v, want one seat.trim row", events)
	}
}

func TestAdminRandomizeDraftOrderRecordsCommissionerEvent(t *testing.T) {
	service := newTestService(t, true)
	request, _ := http.NewRequest(http.MethodGet, "/admin", nil)
	if _, _, err := service.AdminRandomizeDraftOrder(request, ""); err != nil {
		t.Fatal(err)
	}
	events := service.store.Snapshot().CommissionerEvents
	if len(events) != 1 || events[0].Kind != "draft.order_randomize" {
		t.Fatalf("commissioner events = %+v, want one draft.order_randomize row", events)
	}
}

func TestAdminPauseAndResumeClockRecordCommissionerEvents(t *testing.T) {
	service := newTestService(t, true)
	request, _ := http.NewRequest(http.MethodPost, "/admin", nil)
	service.store.state.DraftStarted = true
	if err := service.store.ArmClock(service.clock().Add(90 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := service.AdminPauseClock(request); err != nil {
		t.Fatal(err)
	}
	if err := service.AdminResumeClock(request); err != nil {
		t.Fatal(err)
	}
	events := service.store.Snapshot().CommissionerEvents
	if len(events) != 2 || events[0].Kind != "clock.pause" || events[1].Kind != "clock.resume" {
		t.Fatalf("commissioner events = %+v, want [clock.pause, clock.resume]", events)
	}
}

func TestAdminForceAutopickRecordsCommissionerEvent(t *testing.T) {
	service := newTestService(t, true)
	service.SetPlayerSource(func() ([]Player, int64, string) { return testPool(20), 1, "live" })
	request, _ := http.NewRequest(http.MethodGet, "/admin", nil)
	if err := service.store.ArmClock(service.clock().Add(90 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := service.AdminPauseClock(request); err != nil {
		t.Fatal(err)
	}
	pick, player, team, err := service.AdminForceAutopick(request, ForceCurrentPickConfirmation, draftCurrentPickToken(service.store.Snapshot()))
	if err != nil {
		t.Fatal(err)
	}
	events := service.store.Snapshot().CommissionerEvents
	if len(events) != 2 || events[1].Kind != "clock.force_autopick" || events[1].Refs.TeamID != team.ID || events[1].Refs.PlayerID != player.ID {
		t.Fatalf("commissioner events = %+v, want [clock.pause, clock.force_autopick] with pick %+v refs", events, pick)
	}
}

func TestAdminExtendClockRecordsCommissionerEvent(t *testing.T) {
	service := newTestService(t, true)
	service.store.draftLifecycleBypass = false
	service.SetPlayerSource(func() ([]Player, int64, string) { return testPool(200), 1, "live" })
	request := httptest.NewRequest(http.MethodPost, "/admin", nil)
	if started, err := service.AdminStartDraft(request); err != nil || !started {
		t.Fatalf("start draft: started=%v err=%v", started, err)
	}
	token := draftCurrentPickToken(service.store.Snapshot())
	if err := service.AdminExtendClock(request, 30, token); err != nil {
		t.Fatal(err)
	}
	events := service.store.Snapshot().CommissionerEvents
	if len(events) != 2 || events[1].Kind != "clock.extend" {
		t.Fatalf("commissioner events = %+v, want [draft.start, clock.extend]", events)
	}
}

func TestAdminSetClockSecondsRecordsCommissionerEventOnlyOnChange(t *testing.T) {
	service := newTestService(t, true)
	request, _ := http.NewRequest(http.MethodGet, "/admin", nil)
	// Establish a known, already-clamped duration first — the fresh
	// service's zero-value ClockDurationSec is itself outside
	// [MinPickClock, MaxPickClock], so re-submitting it verbatim would
	// misreport a real clamp-driven change as the "no-op" case this test
	// checks.
	if err := service.AdminSetClockSeconds(request, 45); err != nil {
		t.Fatal(err)
	}
	before := service.store.Snapshot().ClockDurationSec
	if err := service.AdminSetClockSeconds(request, before); err != nil {
		t.Fatal(err)
	}
	if got := service.store.Snapshot().CommissionerEvents; len(got) != 1 {
		t.Fatalf("commissioner events after a no-op duration re-set = %+v, want exactly the first row", got)
	}
	if err := service.AdminSetClockSeconds(request, before+30); err != nil {
		t.Fatal(err)
	}
	events := service.store.Snapshot().CommissionerEvents
	if len(events) != 2 || events[1].Kind != "clock.set_duration" {
		t.Fatalf("commissioner events = %+v, want a second clock.set_duration row after the real change", events)
	}
}

func TestAdminSetAutopickRecordsCommissionerEventOnlyOnChange(t *testing.T) {
	service := newTestService(t, true)
	request := httptest.NewRequest(http.MethodPost, "/admin", nil)
	if _, _, err := service.store.AssignMember("autopick-claim@example.com", "Claimed"); err != nil {
		t.Fatal(err)
	}
	if err := service.AdminSetAutopick(request, "team-1", false); err != nil {
		t.Fatal(err)
	}
	if got := service.store.Snapshot().CommissionerEvents; len(got) != 0 {
		t.Fatalf("commissioner events after a no-op autopick set = %+v, want none", got)
	}
	if err := service.AdminSetAutopick(request, "team-1", true); err != nil {
		t.Fatal(err)
	}
	events := service.store.Snapshot().CommissionerEvents
	if len(events) != 1 || events[0].Kind != "clock.set_autopick" || events[0].Refs.TeamID != "team-1" {
		t.Fatalf("commissioner events = %+v, want one clock.set_autopick row for team-1", events)
	}
}

func TestAdminSetReadyRecordsCommissionerEventOnlyOnChange(t *testing.T) {
	service := newTestService(t, true)
	request, _ := http.NewRequest(http.MethodGet, "/admin", nil)
	claimReadyTestSeats(t, service, 2)
	if err := service.AdminSetReady(request, "team-2", false); err != nil {
		t.Fatal(err)
	}
	if got := service.store.Snapshot().CommissionerEvents; len(got) != 0 {
		t.Fatalf("commissioner events after a no-op ready set = %+v, want none", got)
	}
	if err := service.AdminSetReady(request, "team-2", true); err != nil {
		t.Fatal(err)
	}
	events := service.store.Snapshot().CommissionerEvents
	if len(events) != 1 || events[0].Kind != "clock.set_ready" || events[0].Refs.TeamID != "team-2" {
		t.Fatalf("commissioner events = %+v, want one clock.set_ready row for team-2", events)
	}
}

func TestAdminStartDraftRecordsCommissionerEvent(t *testing.T) {
	service := newTestService(t, true)
	service.store.draftLifecycleBypass = false
	required := len(service.Teams()) * CurrentDraftRounds()
	service.SetPlayerSource(func() ([]Player, int64, string) { return testPool(required), 1, "demo" })
	request, _ := http.NewRequest(http.MethodPost, "/draft", nil)
	started, err := service.AdminStartDraft(request)
	if err != nil || !started {
		t.Fatalf("start = %v, %v", started, err)
	}
	// A second, idempotent start (draft already live) must not duplicate
	// the original start's event.
	if started, err = service.AdminStartDraft(request); err != nil || started {
		t.Fatalf("idempotent start = %v, %v", started, err)
	}
	events := service.store.Snapshot().CommissionerEvents
	if len(events) != 1 || events[0].Kind != "draft.start" {
		t.Fatalf("commissioner events = %+v, want exactly one draft.start row", events)
	}
}

func TestAdminSetRosterShapeAndResetRecordCommissionerEvents(t *testing.T) {
	t.Cleanup(clearRosterShape)
	service := newTestService(t, true)
	request, _ := http.NewRequest(http.MethodGet, "/admin", nil)
	if _, err := service.AdminSetRosterShape(request, validGridironShape()); err != nil {
		t.Fatal(err)
	}
	if err := service.AdminResetRosterShape(request); err != nil {
		t.Fatal(err)
	}
	events := service.store.Snapshot().CommissionerEvents
	if len(events) != 2 || events[0].Kind != "roster.shape_set" || events[1].Kind != "roster.shape_reset" {
		t.Fatalf("commissioner events = %+v, want [roster.shape_set, roster.shape_reset]", events)
	}
}

func TestAdminPostAndDeleteAnnouncementRecordCommissionerEvents(t *testing.T) {
	service := newAnnouncementAdminService(t)
	request, _ := http.NewRequest(http.MethodGet, "/admin", nil)
	posted, _, err := service.AdminPostAnnouncement(request, "Draft starts Saturday.", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.AdminDeleteAnnouncement(request, posted.ID); err != nil {
		t.Fatal(err)
	}
	events := service.store.Snapshot().CommissionerEvents
	if len(events) != 2 || events[0].Kind != "announcement.post" || events[1].Kind != "announcement.delete" {
		t.Fatalf("commissioner events = %+v, want [announcement.post, announcement.delete]", events)
	}
}

func TestAdminSetScoringAndResetRecordCommissionerEvents(t *testing.T) {
	service := newTestService(t, true)
	request, _ := http.NewRequest(http.MethodGet, "/admin", nil)
	if _, err := service.AdminSetScoring(request, "passYards", "0.06"); err != nil {
		t.Fatal(err)
	}
	if err := service.AdminResetScoring(request); err != nil {
		t.Fatal(err)
	}
	events := service.store.Snapshot().CommissionerEvents
	if len(events) != 2 || events[0].Kind != "scoring.set" || events[1].Kind != "scoring.reset" {
		t.Fatalf("commissioner events = %+v, want [scoring.set, scoring.reset]", events)
	}
}
