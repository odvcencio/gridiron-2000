package league

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gridiron-2000/internal/commissionerhq"
)

func TestEffectiveDraftAtUsesOverrideAndConfigFallback(t *testing.T) {
	service := newTestService(t, true)
	configured := service.cfg.DraftAt
	if got := service.EffectiveDraftAt(PersistedState{}); !got.Equal(service.draftAt) {
		t.Fatalf("fallback = %v, want service fallback %v", got, service.draftAt)
	}
	override := configured.Add(48 * time.Hour)
	if got := service.EffectiveDraftAt(PersistedState{DraftAtOverride: override}); !got.Equal(override) {
		t.Fatalf("override = %v, want %v", got, override)
	}
}

func TestParseDraftMeetingLocalRejectsDSTGapsAndFolds(t *testing.T) {
	location, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parseDraftMeetingLocal("2026-03-08T02:30", location); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("DST gap error = %v", err)
	}
	if _, err := parseDraftMeetingLocal("2026-11-01T01:30", location); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("DST fold error = %v", err)
	}
	got, err := parseDraftMeetingLocal("2026-11-01T03:30", location)
	if err != nil || got.In(location).Format(draftMeetingInputLayout) != "2026-11-01T03:30" {
		t.Fatalf("ordinary local time = %v, %v", got, err)
	}
	if _, err := parseDraftMeetingLocal("not-a-date", location); err == nil {
		t.Fatal("malformed local time unexpectedly accepted")
	}
}

func TestAdminRescheduleDraftRequiresCommissionerAndFutureTime(t *testing.T) {
	service := newTestService(t, false)
	now := time.Date(2026, time.August, 23, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	request, _ := http.NewRequest(http.MethodPost, "/admin", nil)
	if err := service.AdminRescheduleDraft(request, "2026-08-24T12:00"); err == nil || !strings.Contains(err.Error(), "commissioner") {
		t.Fatalf("non-commissioner error = %v", err)
	}
	service.demoMode = true
	if err := service.AdminRescheduleDraft(request, "2026-08-23T08:00"); err == nil || !strings.Contains(err.Error(), "future") {
		t.Fatalf("past meeting error = %v", err)
	}
	want := time.Date(2026, time.August, 24, 15, 0, 0, 0, time.UTC)
	if err := service.AdminRescheduleDraft(request, "2026-08-24T11:00"); err != nil {
		t.Fatalf("future reschedule: %v", err)
	}
	if got := service.DraftAt(); !got.Equal(want) {
		t.Fatalf("effective meeting = %v, want %v", got, want)
	}
	service.store.state.DraftStarted = true
	if err := service.AdminRescheduleDraft(request, "2026-08-25T11:00"); err == nil || !strings.Contains(err.Error(), "after the draft starts") {
		t.Fatalf("post-start error = %v", err)
	}
}

func TestDraftMeetingPersistsAcrossJSONAndSQLiteRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := NewStore(path)
	override := time.Date(2026, time.August, 30, 15, 0, 0, 0, time.UTC)
	encoded, err := json.Marshal(PersistedState{DraftAtOverride: override})
	if err != nil {
		t.Fatal(err)
	}
	var decoded PersistedState
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if !decoded.DraftAtOverride.Equal(override) {
		t.Fatalf("JSON round-trip override = %v, want %v", decoded.DraftAtOverride, override)
	}
	if err := store.SetDraftAtOverride(override); err != nil {
		t.Fatal(err)
	}
	if got := reloadStoredState(t, path).DraftAtOverride; !got.Equal(override) {
		t.Fatalf("restart override = %v, want %v", got, override)
	}
}

func TestDraftMeetingResetSemantics(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "state.json"))
	override := time.Date(2026, time.August, 30, 15, 0, 0, 0, time.UTC)
	if err := store.SetDraftAtOverride(override); err != nil {
		t.Fatal(err)
	}
	store.state.Picks = []DraftPick{{Number: 1, TeamID: "team-1", PlayerID: "p-1", MadeAt: override}}
	if err := store.UndoLastPick(time.Time{}); err != nil {
		t.Fatal(err)
	}
	if got := store.Snapshot().DraftAtOverride; !got.Equal(override) {
		t.Fatalf("undo cleared override: %v", got)
	}
	store.state.Picks = []DraftPick{{Number: 1, TeamID: "team-1", PlayerID: "p-1", MadeAt: override}}
	if err := store.ResetDraft(); err != nil {
		t.Fatal(err)
	}
	if got := store.Snapshot().DraftAtOverride; !got.Equal(override) {
		t.Fatalf("draft reset cleared override: %v", got)
	}
	if err := store.ResetLeague(); err != nil {
		t.Fatal(err)
	}
	if got := store.Snapshot().DraftAtOverride; !got.IsZero() {
		t.Fatalf("league reset retained override: %v", got)
	}
}

func TestDraftMeetingSurfacesUseOneEffectiveInstant(t *testing.T) {
	service := newTestService(t, true)
	now := time.Date(2026, time.August, 23, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	override := now.Add(72 * time.Hour)
	service.store.state.DraftAtOverride = override
	summary := service.draftSummary(now)
	if summary["at"] != override.Format(time.RFC3339) || summary["overridden"] != true {
		t.Fatalf("draft summary = %#v", summary)
	}
	location := parseDraftTZ(service.cfg.Timezone)
	rules := service.rulesIdentityMap(now, location)
	if rules["draft_date"] != override.In(location).Format("Monday, January 2, 2006") {
		t.Fatalf("rules draft date = %#v", rules["draft_date"])
	}
	commissioner := service.CommissionerSummary("test", commissionerhq.Runtime{Ready: true}, commissionerhq.Pool{Mode: "live", Actual: 1000, Target: 1000})
	if !commissioner.Draft.ScheduledAt.Equal(override) {
		t.Fatalf("HQ scheduled = %v, want %v", commissioner.Draft.ScheduledAt, override)
	}
	if got := service.boundaryDigest(now, nil); !strings.Contains(got, "draft:0") {
		t.Fatalf("boundary before meeting = %q", got)
	}
}

func TestDraftReminderOverrideRearmsOldKeyAndUsesNewMeetingCopy(t *testing.T) {
	oldAt := time.Date(2026, time.August, 24, 16, 0, 0, 0, time.UTC)
	service, _ := newNotifyTestService(t, oldAt, oldAt)
	if _, _, err := service.store.AssignMember("manager@example.com", "Manager"); err != nil {
		t.Fatal(err)
	}
	service.store.state.SentLog = map[string]time.Time{}
	state := service.store.Snapshot()
	oldKey := keyDraftReminder(24, oldAt, "manager@example.com")
	state.SentLog = map[string]time.Time{oldKey: oldAt.Add(-time.Hour)}
	service.store.state.SentLog[oldKey] = oldAt.Add(-time.Hour)
	newAt := oldAt.Add(48 * time.Hour)
	state.DraftAtOverride = newAt
	service.evalDraftReminders(state, newAt.Add(-24*time.Hour))
	newState := service.store.Snapshot()
	newKey := keyDraftReminder(24, newAt, "manager@example.com")
	if _, ok := newState.SentLog[newKey]; !ok {
		t.Fatalf("rescheduled reminder key %q was not recorded", newKey)
	}
	if _, ok := newState.SentLog[oldKey]; !ok {
		t.Fatal("the old reminder key was unexpectedly removed")
	}
	notification := service.buildDraftReminder(state, state.Members["manager@example.com"], reminderLeads[0])
	if !strings.Contains(notification.Text, "AUG 26") {
		t.Fatalf("reminder copy did not use the new meeting date: %s", notification.Text)
	}
}
