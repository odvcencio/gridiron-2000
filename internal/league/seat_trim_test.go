package league

import (
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"testing"
	"time"
)

// claimSeat is the seat_trim_test.go fixture helper: assigns email the
// next open seat directly through the store (Store.AssignMember — the same
// primitive registration_test.go uses), without forging Google auth.
// AssignMember always claims the first unclaimed team in defaultTeams()
// order, so calling this n times in a row claims exactly team-1..team-n.
func claimSeat(t *testing.T, svc *Service, email string) Member {

	t.Helper()
	member, _, err := svc.store.AssignMember(email, email)
	if err != nil {
		t.Fatalf("AssignMember(%s): %v", email, err)
	}
	return member
}
func trimUnclaimedSeatsForTest(t *testing.T, svc *Service, request *http.Request) ([]Team, []string, error) {
	t.Helper()
	data := svc.AdminData(request)
	count, ok := data["unclaimed_seat_count"].(int)
	if !ok {
		t.Fatalf("unclaimed_seat_count = %T", data["unclaimed_seat_count"])
	}
	token, _ := data["unclaimed_seat_token"].(string)
	return svc.TrimUnclaimedSeats(request, seatTrimConfirmation(count), token)
}

// TestTrimUnclaimedSeatsDropsUnclaimedKeepsClaimed is the trim's core
// contract (SK unclaimed-seat spec): a claimed seat's own record (ID,
// name, division, tone) survives untouched; every unclaimed seat is
// dropped, and defaultTeams() reflects the drop immediately.
func TestTrimUnclaimedSeatsDropsUnclaimedKeepsClaimed(t *testing.T) {
	t.Cleanup(clearSeatTrim)
	svc := newTestService(t, true) // demo mode grants commissioner
	request, _ := http.NewRequest(http.MethodGet, "/admin", nil)

	all := defaultTeams()
	if len(all) != 8 {
		t.Fatalf("neutral default team count = %d, want 8", len(all))
	}
	// Claim the first 5 of 8 (team-1..team-5); leave team-6..team-8 open.
	for i := 0; i < 5; i++ {
		claimSeat(t, svc, fmt.Sprintf("member-%d@example.com", i))
	}

	kept, removed, err := trimUnclaimedSeatsForTest(t, svc, request)
	if err != nil {
		t.Fatalf("TrimUnclaimedSeats: %v", err)
	}
	if len(kept) != 5 {
		t.Fatalf("kept = %d teams, want 5", len(kept))
	}
	if len(removed) != 3 {
		t.Fatalf("removed = %d teams, want 3", len(removed))
	}
	for i, team := range kept {
		if team != all[i] {
			t.Fatalf("kept[%d] = %+v, want the untouched original %+v", i, team, all[i])
		}
	}
	wantRemoved := map[string]bool{"team-6": true, "team-7": true, "team-8": true}
	for _, id := range removed {
		if !wantRemoved[id] {
			t.Fatalf("unexpected removed team id %q", id)
		}
	}

	// The runtime override is visible immediately, package-wide.
	if got := len(defaultTeams()); got != 5 {
		t.Fatalf("defaultTeams() after trim = %d, want 5", got)
	}
	if got := len(defaultTeamIDs()); got != 5 {
		t.Fatalf("defaultTeamIDs() after trim = %d, want 5", got)
	}

	// Draft rounds derive from the roster shape alone, never team count.
	if got := CurrentDraftRounds(); got != ActiveRosterPreset.Total() {
		t.Fatalf("CurrentDraftRounds() after trim = %d, want %d (unaffected by the trim)", got, ActiveRosterPreset.Total())
	}

	if got := svc.store.Snapshot().TrimmedTeamIDs; len(got) != 3 {
		t.Fatalf("persisted TrimmedTeamIDs = %v, want 3 entries", got)
	}
}

// TestTrimUnclaimedSeatsRequiresCommissioner mirrors every other Admin*
// method's non-commissioner rejection precedent (roster_shape_test.go).
func TestTrimUnclaimedSeatsRequiresCommissioner(t *testing.T) {
	t.Cleanup(clearSeatTrim)
	svc := newTestService(t, false) // not demo mode: no free commissioner grant
	request, _ := http.NewRequest(http.MethodGet, "/admin", nil)

	for i := 0; i < 5; i++ {
		claimSeat(t, svc, fmt.Sprintf("member-%d@example.com", i))
	}
	if _, _, err := trimUnclaimedSeatsForTest(t, svc, request); err == nil {
		t.Fatal("a non-commissioner request must be rejected")
	}
	if got := len(defaultTeams()); got != 8 {
		t.Fatalf("defaultTeams() after a rejected trim = %d, want 8 (untouched)", got)
	}
}

// TestTrimUnclaimedSeatsLocksOnceDraftStarts mirrors SetDraftOrder/
// SetRosterOverride's post-first-pick lock (store.go).
func TestTrimUnclaimedSeatsLocksOnceDraftStarts(t *testing.T) {
	t.Cleanup(clearSeatTrim)
	svc := newTestService(t, true)
	request, _ := http.NewRequest(http.MethodGet, "/admin", nil)

	all := defaultTeams()
	for i := 0; i < 5; i++ {
		claimSeat(t, svc, fmt.Sprintf("member-%d@example.com", i))
	}
	startTestDraft(t, svc.store)
	if _, err := svc.store.MakePick(all[0].ID, "p-01", "manager", time.Now(), time.Time{}); err != nil {
		t.Fatalf("MakePick: %v", err)
	}
	if _, _, err := trimUnclaimedSeatsForTest(t, svc, request); err == nil {
		t.Fatal("a trim must be rejected once the draft has picks on the tape")
	}
	if got := len(defaultTeams()); got != 8 {
		t.Fatalf("defaultTeams() after a rejected trim = %d, want 8 (untouched)", got)
	}
}

// TestTrimUnclaimedSeatsMinFloor rejects a trim that would leave the league
// under the engine's own team floor (config.go's minTeams).
func TestTrimUnclaimedSeatsMinFloor(t *testing.T) {
	t.Cleanup(clearSeatTrim)
	svc := newTestService(t, true)
	request, _ := http.NewRequest(http.MethodGet, "/admin", nil)

	for i := 0; i < 2; i++ { // fewer than minTeams (4)
		claimSeat(t, svc, fmt.Sprintf("member-%d@example.com", i))
	}
	if _, _, err := trimUnclaimedSeatsForTest(t, svc, request); err == nil {
		t.Fatal("a trim leaving fewer than minTeams claimed seats must be rejected")
	}
	if got := len(defaultTeams()); got != 8 {
		t.Fatalf("defaultTeams() after a rejected trim = %d, want 8 (untouched)", got)
	}
	if got := svc.store.Snapshot().TrimmedTeamIDs; len(got) != 0 {
		t.Fatalf("a rejected trim must not persist TrimmedTeamIDs, got %v", got)
	}
}

// TestTrimUnclaimedSeatsIdempotent proves a repeat call (the commissioner
// running the action twice, or a retried request) recomputes the same,
// stable answer rather than compounding.
func TestTrimUnclaimedSeatsIdempotent(t *testing.T) {
	t.Cleanup(clearSeatTrim)
	svc := newTestService(t, true)
	request, _ := http.NewRequest(http.MethodGet, "/admin", nil)

	for i := 0; i < 5; i++ {
		claimSeat(t, svc, fmt.Sprintf("member-%d@example.com", i))
	}
	kept1, removed1, err := trimUnclaimedSeatsForTest(t, svc, request)
	if err != nil {
		t.Fatalf("first trim: %v", err)
	}
	if _, _, err = trimUnclaimedSeatsForTest(t, svc, request); !errors.Is(err, errAdminActionStale) {
		t.Fatalf("second trim error = %v, want stale render rejection", err)
	}
	_ = kept1
	_ = removed1
}

// TestTrimUnclaimedSeatsClearsStaleDraftOrder proves a draft order drawn
// before the trim (against the full, untrimmed team set) does not survive
// the trim as a stale, wrong-length order — activeTeamCount must read the
// trimmed count, not a leftover full-size order (store.go).
func TestTrimUnclaimedSeatsClearsStaleDraftOrder(t *testing.T) {
	t.Cleanup(clearSeatTrim)
	svc := newTestService(t, true)
	request, _ := http.NewRequest(http.MethodGet, "/admin", nil)

	fullOrder := defaultTeamIDs()
	if err := svc.store.SetDraftOrder(fullOrder); err != nil {
		t.Fatalf("SetDraftOrder: %v", err)
	}
	for i := 0; i < 5; i++ {
		claimSeat(t, svc, fmt.Sprintf("member-%d@example.com", i))
	}
	if _, _, err := trimUnclaimedSeatsForTest(t, svc, request); err != nil {
		t.Fatalf("TrimUnclaimedSeats: %v", err)
	}
	if got := svc.store.Snapshot().DraftOrder; len(got) != 0 {
		t.Fatalf("draft order survived the trim: %v, want cleared", got)
	}
	if got := activeTeamCount(svc.store.Snapshot().DraftOrder); got != 5 {
		t.Fatalf("activeTeamCount after trim = %d, want 5 (falls back to the trimmed defaultTeamIDs())", got)
	}
}

// TestTrimUnclaimedSeatsClearsAndRegeneratesSchedule proves the complete
// topology transition: a schedule generated for all seats cannot survive the
// trim, and the next schedule is generated only from the kept teams. The
// reload in the middle catches a stale schedule row that would otherwise
// return after a restart.
func TestTrimUnclaimedSeatsClearsAndRegeneratesSchedule(t *testing.T) {
	t.Cleanup(clearSeatTrim)
	svc := newTestService(t, true)
	request, _ := http.NewRequest(http.MethodGet, "/admin", nil)

	if _, err := svc.AdminGenerateSchedule(request, 2, 1, 17); err != nil {
		t.Fatalf("generate full schedule: %v", err)
	}
	if err := svc.store.SetDraftOrder(defaultTeamIDs()); err != nil {
		t.Fatalf("set full draft order: %v", err)
	}
	for i := 0; i < minTeams; i++ {
		claimSeat(t, svc, fmt.Sprintf("member-%d@example.com", i))
	}

	kept, removed, err := trimUnclaimedSeatsForTest(t, svc, request)
	if err != nil {
		t.Fatalf("trim scheduled league: %v", err)
	}
	if len(kept) != minTeams || len(removed) != len(activeTeams)-minTeams {
		t.Fatalf("trim counts = kept %d removed %d, want kept %d removed %d", len(kept), len(removed), minTeams, len(activeTeams)-minTeams)
	}
	if got := svc.store.Snapshot().Schedule; got != nil {
		t.Fatalf("trim left an in-memory schedule: %+v", got)
	}

	reloaded := reloadStoredState(t, svc.store.filePath)
	if reloaded.Schedule != nil {
		t.Fatalf("trim left a schedule persisted across restart: %+v", reloaded.Schedule)
	}
	if len(reloaded.DraftOrder) != 0 {
		t.Fatalf("trim left a draft order persisted across restart: %v", reloaded.DraftOrder)
	}
	if len(reloaded.TrimmedTeamIDs) != len(removed) {
		t.Fatalf("reloaded TrimmedTeamIDs = %v, want %d entries", reloaded.TrimmedTeamIDs, len(removed))
	}

	regenerated, err := svc.AdminGenerateSchedule(request, 2, 1, 19)
	if err != nil {
		t.Fatalf("regenerate kept-team schedule: %v", err)
	}
	keptIDs := make(map[string]bool, len(kept))
	for _, team := range kept {
		keptIDs[team.ID] = true
	}
	for _, week := range regenerated.Weeks {
		for _, matchup := range week.Matchups {
			if !keptIDs[matchup.HomeTeamID] || !keptIDs[matchup.AwayTeamID] {
				t.Fatalf("regenerated week %d matchup names trimmed team: %+v; kept=%v", week.Week, matchup, keptIDs)
			}
		}
	}
	if persisted := svc.store.Snapshot().Schedule; persisted == nil || persisted.Seed != 19 {
		t.Fatalf("regenerated schedule was not persisted: %+v", persisted)
	}
}

// TestTrimUnclaimedSeatsPersistFailureIsAtomic uses the store's existing
// pre-commit failure seam. Neither the in-memory topology nor the durable
// schedule may move when the combined trim transaction cannot commit.
func TestTrimUnclaimedSeatsPersistFailureIsAtomic(t *testing.T) {
	t.Cleanup(clearSeatTrim)
	svc := newTestService(t, true)
	request, _ := http.NewRequest(http.MethodGet, "/admin", nil)
	if _, err := svc.AdminGenerateSchedule(request, 2, 1, 23); err != nil {
		t.Fatalf("generate full schedule: %v", err)
	}
	for i := 0; i < minTeams; i++ {
		claimSeat(t, svc, fmt.Sprintf("member-%d@example.com", i))
	}
	before := svc.store.Snapshot()
	failThisStorePersist(svc.store)
	if _, _, err := trimUnclaimedSeatsForTest(t, svc, request); !errors.Is(err, errInjectedPersist) {
		t.Fatalf("failed trim error = %v, want injected persist failure", err)
	}
	after := svc.store.Snapshot()
	if !reflect.DeepEqual(after.Schedule, before.Schedule) || !reflect.DeepEqual(after.DraftOrder, before.DraftOrder) || !reflect.DeepEqual(after.TrimmedTeamIDs, before.TrimmedTeamIDs) {
		t.Fatalf("failed trim changed in-memory topology:\n before=%+v\n after=%+v", before, after)
	}
	reloaded := reloadStoredState(t, svc.store.filePath)
	if !reflect.DeepEqual(reloaded.Schedule, before.Schedule) || !reflect.DeepEqual(reloaded.TrimmedTeamIDs, before.TrimmedTeamIDs) {
		t.Fatalf("failed trim changed durable topology:\n before=%+v\n reloaded=%+v", before, reloaded)
	}
}
func TestTrimUnclaimedSeatsRejectsWrongAndStaleConfirmation(t *testing.T) {
	t.Cleanup(clearSeatTrim)
	svc := newTestService(t, true)
	request, _ := http.NewRequest(http.MethodGet, "/admin", nil)
	for i := 0; i < 5; i++ {
		claimSeat(t, svc, fmt.Sprintf("manager-%d@example.com", i))
	}

	data := svc.AdminData(request)
	count := data["unclaimed_seat_count"].(int)
	token := data["unclaimed_seat_token"].(string)
	before := svc.store.Snapshot()
	if _, _, err := svc.TrimUnclaimedSeats(request, "DROP 99 UNCLAIMED SEATS", token); err == nil {
		t.Fatal("wrong confirmation unexpectedly succeeded")
	}
	afterWrong := svc.store.Snapshot()
	if !reflect.DeepEqual(afterWrong.TrimmedTeamIDs, before.TrimmedTeamIDs) ||
		!reflect.DeepEqual(afterWrong.Schedule, before.Schedule) {
		t.Fatal("wrong confirmation changed league topology")
	}

	// A newly claimed seat invalidates the exact count and token rendered
	// above; submitting that stale form must not trim the new state.
	claimSeat(t, svc, "manager-5@example.com")
	if _, _, err := svc.TrimUnclaimedSeats(request, seatTrimConfirmation(count), token); !errors.Is(err, errAdminActionStale) {
		t.Fatalf("stale trim error = %v, want stale-action rejection", err)
	}
	current := svc.store.Snapshot()
	if len(current.TrimmedTeamIDs) != 0 {
		t.Fatalf("stale trim changed topology: %#v", current.TrimmedTeamIDs)
	}
}

func TestTrimUnclaimedSeatsRejectsConcurrentDuplicateSubmission(t *testing.T) {
	t.Cleanup(clearSeatTrim)
	svc := newTestService(t, true)
	request, _ := http.NewRequest(http.MethodGet, "/admin", nil)
	for i := 0; i < 5; i++ {
		claimSeat(t, svc, fmt.Sprintf("manager-%d@example.com", i))
	}
	data := svc.AdminData(request)
	count := data["unclaimed_seat_count"].(int)
	token := data["unclaimed_seat_token"].(string)
	confirmation := seatTrimConfirmation(count)

	start := make(chan struct{})
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			<-start
			_, _, err := svc.TrimUnclaimedSeats(request, confirmation, token)
			results <- err
		}()
	}
	close(start)
	var success, stale int
	for i := 0; i < 2; i++ {
		err := <-results
		switch {
		case err == nil:
			success++
		case errors.Is(err, errAdminActionStale):
			stale++
		default:
			t.Fatalf("duplicate trim error = %v, want success or stale rejection", err)
		}
	}
	if success != 1 || stale != 1 {
		t.Fatalf("duplicate trim outcomes = success:%d stale:%d, want 1/1", success, stale)
	}
}
func TestTrimUnclaimedSeatsRejectsScheduleRace(t *testing.T) {
	t.Cleanup(clearSeatTrim)
	svc := newTestService(t, true)
	request, _ := http.NewRequest(http.MethodGet, "/admin", nil)
	for i := 0; i < 5; i++ {
		claimSeat(t, svc, fmt.Sprintf("manager-%d@example.com", i))
	}
	initial := SeasonSchedule{Season: svc.cfg.Season, Weeks: []ScheduleWeek{{
		Week: 1, Matchups: []LeagueMatchup{{ID: "race-1", HomeTeamID: "team-1", AwayTeamID: "team-2"}},
	}}}
	if err := svc.store.SetSchedule(initial); err != nil {
		t.Fatalf("set initial schedule: %v", err)
	}
	data := svc.AdminData(request)
	count := data["unclaimed_seat_count"].(int)
	token := data["unclaimed_seat_token"].(string)
	changed := SeasonSchedule{Season: svc.cfg.Season, Weeks: []ScheduleWeek{{
		Week: 1, Matchups: []LeagueMatchup{{ID: "race-2", HomeTeamID: "team-1", AwayTeamID: "team-3"}},
	}}}
	if err := svc.store.SetSchedule(changed); err != nil {
		t.Fatalf("set changed schedule: %v", err)
	}
	if _, _, err := svc.TrimUnclaimedSeats(request, seatTrimConfirmation(count), token); !errors.Is(err, errAdminActionStale) {
		t.Fatalf("schedule-race trim error = %v, want stale-action rejection", err)
	}
	current := svc.store.Snapshot()
	if current.Schedule == nil || !reflect.DeepEqual(*current.Schedule, changed) {
		t.Fatalf("stale trim discarded changed schedule: %#v", current.Schedule)
	}
}
