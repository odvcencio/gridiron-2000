package league

import (
	"net/http"
	"path/filepath"
	"testing"
	"time"
)

// validGridironShape is a known-valid RosterOverride (the gridiron-house
// preset's own shape), reused across this file's tests as a baseline.
func validGridironShape() RosterOverride {
	return RosterOverride{
		Slots: map[string]int{
			"QB": 1, "RB": 2, "WR": 2, "TE": 1, "FLEX": 1,
			"SUPERFLEX": 1, "K": 1, "P": 1, "DST": 1,
		},
		Bench: 6,
	}
}

// TestValidateRosterOverride pins the roster-shape editor's validation
// matrix (roster-ops spec section 10 numbers): allowed slot keys, per-slot
// count 0-4, QB >= 1, RB+WR >= 2, starters >= 6, bench 0-10, and total
// (starters+bench) 10-25.
func TestValidateRosterOverride(t *testing.T) {
	base := validGridironShape()

	cases := []struct {
		name    string
		mutate  func(o *RosterOverride)
		wantErr bool
	}{
		{"valid shape", func(o *RosterOverride) {}, false},
		{"unknown slot key", func(o *RosterOverride) { o.Slots["LB"] = 1 }, true},
		{"slot count negative", func(o *RosterOverride) { o.Slots["K"] = -1 }, true},
		{"slot count above 4", func(o *RosterOverride) { o.Slots["RB"] = 5 }, true},
		{"no QB", func(o *RosterOverride) { delete(o.Slots, "QB") }, true},
		{"RB+WR below 2", func(o *RosterOverride) {
			o.Slots = map[string]int{"QB": 1, "RB": 1, "K": 1, "DST": 1, "TE": 1, "FLEX": 1}
		}, true},
		{"starters below 6", func(o *RosterOverride) {
			o.Slots = map[string]int{"QB": 1, "RB": 2, "WR": 2}
			o.Bench = 6
		}, true},
		{"bench negative", func(o *RosterOverride) { o.Bench = -1 }, true},
		{"bench above 10", func(o *RosterOverride) { o.Bench = 11 }, true},
		{"total below 10", func(o *RosterOverride) {
			o.Slots = map[string]int{"QB": 1, "RB": 2, "WR": 2, "K": 1}
			o.Bench = 0
		}, true},
		{"total above 25", func(o *RosterOverride) {
			o.Slots = map[string]int{"QB": 4, "RB": 4, "WR": 4, "TE": 4, "FLEX": 4, "DST": 1}
			o.Bench = 10
		}, true},
		{"minimum valid shape (6 starters, 4 bench, 10 total)", func(o *RosterOverride) {
			o.Slots = map[string]int{"QB": 1, "RB": 2, "WR": 2, "K": 1}
			o.Bench = 4
		}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			shape := validGridironShape()
			_ = base // keep base referenced for readability of intent
			tc.mutate(&shape)
			err := validateRosterOverride(shape)
			if tc.wantErr && err == nil {
				t.Fatalf("shape %+v: want an error, got none", shape)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("shape %+v: want no error, got %v", shape, err)
			}
		})
	}
}

// TestSetRosterOverrideLocksAfterFirstPick mirrors TestSetDraftOrder: once
// a pick exists, SetRosterOverride and ClearRosterOverride both reject with
// the roster-shape lock message.
func TestSetRosterOverrideLocksAfterFirstPick(t *testing.T) {
	store := newTestStore(t)

	if err := store.SetRosterOverride(validGridironShape()); err != nil {
		t.Fatalf("valid override before any pick must be accepted: %v", err)
	}

	if _, err := store.MakePick("team-1", "p-01", "manager", time.Now(), time.Time{}); err != nil {
		t.Fatal(err)
	}

	err := store.SetRosterOverride(validGridironShape())
	if err == nil {
		t.Fatal("SetRosterOverride must reject once a pick exists")
	}
	if got := err.Error(); got != "the roster shape locks once the draft starts" {
		t.Fatalf("SetRosterOverride error = %q, want the lock message", got)
	}

	err = store.ClearRosterOverride()
	if err == nil {
		t.Fatal("ClearRosterOverride must reject once a pick exists")
	}
	if got := err.Error(); got != "the roster shape locks once the draft starts" {
		t.Fatalf("ClearRosterOverride error = %q, want the lock message", got)
	}
}

// TestRosterOverrideSurvivesReload mirrors TestSetTeamName's reload
// precedent: a persisted override is still there after a fresh Store reads
// the same file.
func TestRosterOverrideSurvivesReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := NewStore(path)

	shape := validGridironShape()
	if err := store.SetRosterOverride(shape); err != nil {
		t.Fatal(err)
	}

	reloaded := NewStore(path)
	got := reloaded.Snapshot().RosterOverride
	if got == nil {
		t.Fatal("override lost on reload")
	}
	if got.Bench != shape.Bench || len(got.Slots) != len(shape.Slots) {
		t.Fatalf("reloaded override = %+v, want %+v", got, shape)
	}
	for key, count := range shape.Slots {
		if got.Slots[key] != count {
			t.Fatalf("reloaded slot %s = %d, want %d", key, got.Slots[key], count)
		}
	}

	if err := store.ClearRosterOverride(); err != nil {
		t.Fatal(err)
	}
	reloaded = NewStore(path)
	if got := reloaded.Snapshot().RosterOverride; got != nil {
		t.Fatalf("cleared override reappeared on reload: %+v", got)
	}
}

// TestRosterAccessorRoundTrip checks CurrentRoster/CurrentDraftRounds
// against setRosterShape/clearRosterShape: an applied override is visible
// immediately, and clearing it restores the config baseline
// (ActiveRosterPreset/DraftRounds).
func TestRosterAccessorRoundTrip(t *testing.T) {
	t.Cleanup(clearRosterShape)

	if got := CurrentRoster().Name; got != ActiveRosterPreset.Name {
		t.Fatalf("CurrentRoster() before any override = %q, want the config baseline %q", got, ActiveRosterPreset.Name)
	}
	if got := CurrentDraftRounds(); got != DraftRounds {
		t.Fatalf("CurrentDraftRounds() before any override = %d, want DraftRounds (%d)", got, DraftRounds)
	}

	custom := rosterOverridePreset(validGridironShape())
	setRosterShape(custom)
	if got := CurrentRoster().Total(); got != custom.Total() {
		t.Fatalf("CurrentRoster().Total() after setRosterShape = %d, want %d", got, custom.Total())
	}
	if got := CurrentDraftRounds(); got != custom.Total() {
		t.Fatalf("CurrentDraftRounds() after setRosterShape = %d, want %d", got, custom.Total())
	}

	clearRosterShape()
	if got := CurrentRoster().Name; got != ActiveRosterPreset.Name {
		t.Fatalf("CurrentRoster() after clearRosterShape = %q, want the config baseline %q", got, ActiveRosterPreset.Name)
	}
	if got := CurrentDraftRounds(); got != DraftRounds {
		t.Fatalf("CurrentDraftRounds() after clearRosterShape = %d, want DraftRounds (%d)", got, DraftRounds)
	}
}

// TestCurrentDraftRoundsAlwaysMatchesRosterTotal pins the "draft rounds
// derive from the roster shape, never set independently" rule across
// several different overrides.
func TestCurrentDraftRoundsAlwaysMatchesRosterTotal(t *testing.T) {
	t.Cleanup(clearRosterShape)

	shapes := []RosterOverride{
		validGridironShape(),
		{Slots: map[string]int{"QB": 1, "RB": 2, "WR": 2, "K": 1}, Bench: 4},
		{Slots: map[string]int{"QB": 2, "RB": 3, "WR": 3, "TE": 2, "FLEX": 2, "DST": 1}, Bench: 8},
	}
	for _, shape := range shapes {
		preset := rosterOverridePreset(shape)
		setRosterShape(preset)
		if got := CurrentDraftRounds(); got != CurrentRoster().Total() {
			t.Fatalf("shape %+v: CurrentDraftRounds() = %d, want CurrentRoster().Total() (%d)", shape, got, CurrentRoster().Total())
		}
	}
}

// TestAdminSetRosterShapeWiring checks the service-level Admin methods:
// commissioner-only, applies the runtime accessor immediately on success,
// and AdminResetRosterShape reverts it.
func TestAdminSetRosterShapeWiring(t *testing.T) {
	t.Cleanup(clearRosterShape)

	svc := newTestService(t, true) // demo mode grants commissioner
	request, _ := http.NewRequest(http.MethodGet, "/admin", nil)

	preset, err := svc.AdminSetRosterShape(request, validGridironShape())
	if err != nil {
		t.Fatal(err)
	}
	if got := CurrentRoster().Total(); got != preset.Total() {
		t.Fatalf("CurrentRoster().Total() = %d, want %d", got, preset.Total())
	}
	if got := svc.store.Snapshot().RosterOverride; got == nil {
		t.Fatal("AdminSetRosterShape did not persist the override")
	}

	if err := svc.AdminResetRosterShape(request); err != nil {
		t.Fatal(err)
	}
	if got := svc.store.Snapshot().RosterOverride; got != nil {
		t.Fatalf("AdminResetRosterShape left an override persisted: %+v", got)
	}
	if got := CurrentRoster().Name; got != ActiveRosterPreset.Name {
		t.Fatalf("CurrentRoster() after AdminResetRosterShape = %q, want the config baseline", got)
	}
}

// TestAdminSetRosterShapeRequiresCommissioner checks the non-commissioner
// rejection path (requireCommissioner precedent every other Admin* method
// follows).
func TestAdminSetRosterShapeRequiresCommissioner(t *testing.T) {
	svc := newTestService(t, false) // not demo mode: no free commissioner grant
	request, _ := http.NewRequest(http.MethodGet, "/admin", nil)

	if _, err := svc.AdminSetRosterShape(request, validGridironShape()); err == nil {
		t.Fatal("a non-commissioner request must be rejected")
	}
	if err := svc.AdminResetRosterShape(request); err == nil {
		t.Fatal("a non-commissioner request must be rejected")
	}
}
