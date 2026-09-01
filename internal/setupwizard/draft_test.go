package setupwizard

import (
	"path/filepath"
	"testing"
	"time"

	"gridiron-2000/internal/league"
)

func TestNeutralSeedValidatesCleanly(t *testing.T) {
	_, _, err := Validate(neutralSeed())
	if err != nil {
		t.Fatalf("the neutral seed must validate on its own: %v", err)
	}
}

func TestNewStateStartsWithEveryStepTodo(t *testing.T) {
	state := NewState()
	for _, step := range Steps {
		if got := state.Status.StatusFor(step.Slug, "identity"); step.Slug != "identity" && got != StepTodo {
			t.Errorf("StatusFor(%q) = %q, want TODO", step.Slug, got)
		}
	}
	if got := state.Status.StatusFor("identity", "identity"); got != StepCurrent {
		t.Errorf("StatusFor(identity, identity) = %q, want CURRENT", got)
	}
}

func TestApplyStepValidSaveMarksDone(t *testing.T) {
	state := NewState()
	candidate := state.Draft.Config
	candidate.League.Name = "My League"
	warnings, err := state.ApplyStep("identity", candidate)
	if err != nil {
		t.Fatalf("ApplyStep: %v", err)
	}
	_ = warnings
	if state.Status["identity"] != StepDone {
		t.Fatalf("status[identity] = %q, want DONE", state.Status["identity"])
	}
	if state.Draft.Config.League.Name != "My League" {
		t.Fatal("the candidate config was not kept after a successful ApplyStep")
	}
}

func TestApplyStepInvalidSaveOnThisStepReturnsErrorAndKeepsPriorDraft(t *testing.T) {
	state := NewState()
	before := state.Draft.Config
	candidate := state.Draft.Config
	candidate.League.Name = "" // identity's own field: league.name is required
	_, err := state.ApplyStep("identity", candidate)
	if err == nil {
		t.Fatal("expected a validation error for an empty league name")
	}
	if state.Status["identity"] == StepDone {
		t.Fatal("a failed step must not be marked DONE")
	}
	if state.Draft.Config.League.Name != before.League.Name {
		t.Fatal("a failed ApplyStep must not mutate the draft")
	}
}

// TestApplyStepDefersLaterStepErrors proves design section 4.2's rule: "an
// error for a later step's field is deferred, not shown early." Saving
// step "roster" with a shape whose total no longer matches draft.rounds
// (still holding its neutral placeholder) must not block the roster step
// — draft.rounds belongs to "roster" itself per this package's field-owner
// table, so this also exercises the same-step (not "later") branch
// directly; StepStale marks the roster step consistent going forward.
func TestApplyStepRosterOwnsDraftRoundsMismatch(t *testing.T) {
	state := NewState()
	candidate := state.Draft.Config
	candidate.Roster = league.RosterBlock{Preset: "gridiron-house"} // needs 17, placeholder rounds is 15
	_, err := state.ApplyStep("roster", candidate)
	if err == nil {
		t.Fatal("expected the roster/rounds mismatch to surface as an error on the roster step itself")
	}
	if FieldStepOwner(err.Error()) != "roster" {
		t.Fatalf("FieldStepOwner(%q) = %q, want roster", err.Error(), FieldStepOwner(err.Error()))
	}
}

func TestFieldStepOwnerMapsKnownFields(t *testing.T) {
	cases := []struct {
		message string
		want    string
	}{
		{`league config: league.name is required`, "identity"},
		{`league config: teams must number 4 to 14; got 2`, "teams"},
		{`league config: scoring_format must be one of half_ppr, ppr, standard`, "scoring"},
		{`league config: roster.bench must be 0 to 10`, "roster"},
		{`league config: draft.rounds must be 1 to 30`, "roster"},
		{`league config: draft.pick_clock_seconds must be 10 to 600`, "draft"},
		{`league config: waivers.mode must be one of perf-priority, faab`, "waivers"},
		{`league config: trades.veto must be one of commissioner, vote, both, none`, "trades"},
		{`league config: membership.allowed_domain "x" is not a valid domain`, "membership"},
		{`league config: nonsense nobody wrote`, ""},
	}
	for _, tc := range cases {
		if got := FieldStepOwner(tc.message); got != tc.want {
			t.Errorf("FieldStepOwner(%q) = %q, want %q", tc.message, got, tc.want)
		}
	}
}

func TestSaveAndLoadStateRoundTrip(t *testing.T) {
	dir := t.TempDir()
	store := league.NewStore(filepath.Join(dir, "league-state.json"))
	t.Cleanup(func() { _ = store.Close() })
	if err := store.StartupError(); err != nil {
		t.Fatal(err)
	}

	if _, found, err := store.LoadSetupDraft(); err != nil || found {
		t.Fatalf("fresh store: found=%v err=%v", found, err)
	}

	state := NewState()
	state.Draft.Config.League.Name = "Persisted League"
	state.Draft.MemberEmails = []string{"a@example.com", "b@example.com"}
	state.Draft.CommissionerEmail = "commish@example.com"
	state.Draft.IdentityAliases = []IdentityAlias{{Alias: "alt@example.com", Canonical: "commish@example.com"}}
	state.Status.MarkDone("identity")
	now := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	if err := state.Save(store, now); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := LoadState(store)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if loaded.Draft.Config.League.Name != "Persisted League" {
		t.Fatalf("League.Name = %q, want Persisted League", loaded.Draft.Config.League.Name)
	}
	if len(loaded.Draft.MemberEmails) != 2 {
		t.Fatalf("MemberEmails = %v, want 2 entries", loaded.Draft.MemberEmails)
	}
	if loaded.Draft.CommissionerEmail != "commish@example.com" {
		t.Fatalf("CommissionerEmail = %q", loaded.Draft.CommissionerEmail)
	}
	if len(loaded.Draft.IdentityAliases) != 1 || loaded.Draft.IdentityAliases[0].Alias != "alt@example.com" {
		t.Fatalf("IdentityAliases = %v", loaded.Draft.IdentityAliases)
	}
	if loaded.Status["identity"] != StepDone {
		t.Fatalf("status[identity] = %q, want DONE", loaded.Status["identity"])
	}
}

func TestLoadStateWithNoSavedDraftReturnsFreshState(t *testing.T) {
	dir := t.TempDir()
	store := league.NewStore(filepath.Join(dir, "league-state.json"))
	t.Cleanup(func() { _ = store.Close() })
	if err := store.StartupError(); err != nil {
		t.Fatal(err)
	}
	state, err := LoadState(store)
	if err != nil {
		t.Fatal(err)
	}
	if state.Draft.Config.League.Name == "" {
		t.Fatal("a fresh state must still carry the neutral seed's name")
	}
	if len(state.Status) != 0 {
		t.Fatalf("Status = %v, want empty", state.Status)
	}
}

func TestFirstIncompleteStepAndAllDone(t *testing.T) {
	status := StatusMap{}
	if got := status.FirstIncompleteStep(); got != "identity" {
		t.Fatalf("FirstIncompleteStep() = %q, want identity", got)
	}
	for _, step := range Steps {
		if step.Slug == "review" {
			continue
		}
		status.MarkDone(step.Slug)
	}
	if !status.AllDone() {
		t.Fatal("AllDone() = false, want true once every step but review is DONE")
	}
	if got := status.FirstIncompleteStep(); got != "review" {
		t.Fatalf("FirstIncompleteStep() = %q, want review", got)
	}
}

func TestMarkStaleOnlyAffectsDoneSteps(t *testing.T) {
	status := StatusMap{}
	status.MarkStale("roster") // never visited: no-op
	if status["roster"] == StepStale {
		t.Fatal("MarkStale must not stale a step that was never DONE")
	}
	status.MarkDone("roster")
	status.MarkStale("roster")
	if status["roster"] != StepStale {
		t.Fatalf("status[roster] = %q, want STALE", status["roster"])
	}
}

func TestStepOrderingHelpers(t *testing.T) {
	if FirstStepSlug() != "identity" {
		t.Fatalf("FirstStepSlug() = %q, want identity", FirstStepSlug())
	}
	if NextStepSlug("identity") != "teams" {
		t.Fatalf("NextStepSlug(identity) = %q, want teams", NextStepSlug("identity"))
	}
	if PrevStepSlug("teams") != "identity" {
		t.Fatalf("PrevStepSlug(teams) = %q, want identity", PrevStepSlug("teams"))
	}
	if PrevStepSlug("identity") != "" {
		t.Fatalf("PrevStepSlug(identity) = %q, want empty", PrevStepSlug("identity"))
	}
	if NextStepSlug("review") != "" {
		t.Fatalf("NextStepSlug(review) = %q, want empty", NextStepSlug("review"))
	}
	if ValidStep("nonsense") {
		t.Fatal("ValidStep(nonsense) = true, want false")
	}
	if !ValidStep("review") {
		t.Fatal("ValidStep(review) = false, want true")
	}
}
