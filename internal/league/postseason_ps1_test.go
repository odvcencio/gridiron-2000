package league

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func ps1Standings(n int) []Standing {
	standings := make([]Standing, n)
	for i := range standings {
		standings[i] = Standing{TeamID: "team-" + string(rune('1'+i)), Rank: i + 1, Wins: n - i}
	}
	return standings
}

func ps1Divisions(n int) map[string]string {
	divisions := make(map[string]string, n)
	for i := 0; i < n; i++ {
		if i < n/2 {
			divisions["team-"+string(rune('1'+i))] = "East"
		} else {
			divisions["team-"+string(rune('1'+i))] = "West"
		}
	}
	return divisions
}

func ps1Preview(t *testing.T, teamCount int) PlayoffState {
	t.Helper()
	standings := ps1Standings(8)
	when := time.Date(2026, time.November, 30, 12, 0, 0, 0, time.UTC)
	cfg := PlayoffConfig{TeamCount: teamCount, StartWeek: 15, RoundLengthWeeks: 1, Reseed: true}
	provenance, err := NewPlayoffProvenance(standings, 17, when, nil)
	if err != nil {
		t.Fatal(err)
	}
	state, err := BuildPlayoffPreview(standings, ps1Divisions(8), cfg, provenance)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func TestPS1PreviewIsDeterministicAndExplainsSeedsAndByes(t *testing.T) {
	standings := ps1Standings(8)
	when := time.Date(2026, time.November, 30, 12, 0, 0, 0, time.UTC)
	cfg := PlayoffConfig{
		TeamCount: 3, StartWeek: 15, RoundLengthWeeks: 1, Reseed: true,
		Qualification: "top-record", TiebreakOrder: append([]string(nil), DefaultTiebreakChain...),
	}
	provenance, err := NewPlayoffProvenance(standings, 17, when, cfg.TiebreakOrder)
	if err != nil {
		t.Fatal(err)
	}
	reversed := append([]Standing(nil), standings...)
	for left, right := 0, len(reversed)-1; left < right; left, right = left+1, right-1 {
		reversed[left], reversed[right] = reversed[right], reversed[left]
	}
	reversedProvenance, err := NewPlayoffProvenance(reversed, 17, when, cfg.TiebreakOrder)
	if err != nil {
		t.Fatal(err)
	}
	if provenance.SnapshotID != reversedProvenance.SnapshotID {
		t.Fatalf("snapshot hash changed with input order: %q vs %q", provenance.SnapshotID, reversedProvenance.SnapshotID)
	}
	first, err := BuildPlayoffPreview(standings, ps1Divisions(8), cfg, provenance)
	if err != nil {
		t.Fatal(err)
	}
	second, err := BuildPlayoffPreview(reversed, ps1Divisions(8), cfg, reversedProvenance)
	if err != nil {
		t.Fatal(err)
	}
	if first.PreviewID != second.PreviewID {
		t.Fatalf("preview IDs differ for the same final snapshot: %q vs %q", first.PreviewID, second.PreviewID)
	}
	if first.Status != PlayoffStatusPreview || first.Provenance.SnapshotID == "" {
		t.Fatalf("preview lifecycle/provenance = %+v", first)
	}
	if len(first.Seeds) != 3 || first.Seeds[0].TieBreakExplanation == "" {
		t.Fatalf("seed explanations missing: %+v", first.Seeds)
	}
	byes := 0
	for _, matchup := range first.Matchups {
		if matchup.AwayTeamID == "" {
			byes++
			if !matchup.Final || matchup.TieBreakExplanation == "" {
				t.Fatalf("bye lacks persisted explanation: %+v", matchup)
			}
		}
	}
	if byes != 1 {
		t.Fatalf("bye count = %d, want 1", byes)
	}
}

func TestPS1PreviewPublishIsExplicitAndIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := NewStore(path)
	store.draftLifecycleBypass = true
	t.Cleanup(func() { _ = store.Close() })
	preview := ps1Preview(t, 4)
	when := time.Date(2026, time.December, 1, 12, 0, 0, 0, time.UTC)
	if err := store.SetPlayoffPreview(preview, "commissioner", when); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PublishPlayoffPreview(preview.PreviewID, "", "commissioner", when); err == nil {
		t.Fatal("publish without confirmation succeeded")
	}
	published, err := store.PublishPlayoffPreview(preview.PreviewID, PlayoffPublishConfirmation, "commissioner", when)
	if err != nil {
		t.Fatal(err)
	}
	if published.Status != PlayoffStatusPublished || len(published.Audit) != 2 {
		t.Fatalf("published bracket = %+v", published)
	}
	second, err := store.PublishPlayoffPreview(preview.PreviewID, PlayoffPublishConfirmation, "commissioner", when.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if !second.PublishedAt.Equal(published.PublishedAt) || len(second.Audit) != len(published.Audit) {
		t.Fatalf("idempotent publish changed state: first=%+v second=%+v", published, second)
	}
	truth := store.PlayoffTruth()
	if truth == nil || truth.Status != PlayoffStatusPublished {
		t.Fatalf("authoritative truth = %+v", truth)
	}
	truth.Matchups[0].HomeTeamID = "mutated"
	if store.PlayoffTruth().Matchups[0].HomeTeamID == "mutated" {
		t.Fatal("PlayoffTruth returned mutable store state")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	restarted := NewStore(path)
	t.Cleanup(func() { _ = restarted.Close() })
	if err := restarted.StartupError(); err != nil {
		t.Fatal(err)
	}
	if got := restarted.PlayoffTruth(); got == nil || got.Status != PlayoffStatusPublished || len(got.Audit) != 2 || got.Provenance.SnapshotID == "" {
		t.Fatalf("restarted playoff truth = %+v", got)
	}
}

func TestPS1StrictAdvancementRejectsPartialAndPersistsTieEvidence(t *testing.T) {
	state := ps1Preview(t, 4)
	state.Status = PlayoffStatusPublished
	active := matchupsForRound(state.Matchups, "championship", 1)
	if len(active) != 2 {
		t.Fatalf("championship round one = %+v", active)
	}
	when := time.Date(2026, time.December, 2, 12, 0, 0, 0, time.UTC)
	partial := PlayoffRoundResult{MatchupID: active[0].ID, Final: true, Authoritative: true, SourceState: "final", Source: "scores", ObservedAt: when}
	if _, err := AdvanceAuthoritativePlayoffRound(state, []PlayoffRoundResult{partial}); err == nil {
		t.Fatal("partial playoff results advanced")
	}
	degraded := []PlayoffRoundResult{partial, {
		MatchupID: active[1].ID, Final: true, Authoritative: true, SourceState: "degraded", Source: "scores", ObservedAt: when,
	}}
	if _, err := AdvanceAuthoritativePlayoffRound(state, degraded); err == nil {
		t.Fatal("degraded playoff results advanced")
	}
	valid := []PlayoffRoundResult{
		{MatchupID: active[0].ID, HomeScore: 100, AwayScore: 100, HomeBestWeek: 60, AwayBestWeek: 60, Final: true, Authoritative: true, SourceState: "final", Source: "scores", ObservedAt: when},
		{MatchupID: active[1].ID, HomeScore: 102, AwayScore: 98, Final: true, Authoritative: true, SourceState: "final", Source: "scores", ObservedAt: when},
	}
	advanced, err := AdvanceAuthoritativePlayoffRound(state, valid)
	if err != nil {
		t.Fatal(err)
	}
	for _, matchup := range advanced.Matchups {
		if matchup.Round == 1 && matchup.Bracket == "championship" && matchup.AwayTeamID != "" {
			if matchup.TieBreakExplanation == "" || matchup.ResultProvenance == nil || !matchup.ResultProvenance.Authoritative {
				t.Fatalf("missing tie/source evidence: %+v", matchup)
			}
		}
	}
}

func TestPS1PublishedCorrectionIsConfirmedAuditedAndBounded(t *testing.T) {
	store := newTestStore(t)
	preview := ps1Preview(t, 2)
	when := time.Date(2026, time.December, 3, 12, 0, 0, 0, time.UTC)
	if err := store.SetPlayoffPreview(preview, "commissioner", when); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PublishPlayoffPreview(preview.PreviewID, PlayoffPublishConfirmation, "commissioner", when); err != nil {
		t.Fatal(err)
	}
	matchup := matchupsForRound(preview.Matchups, "championship", 1)[0]
	result := PlayoffRoundResult{MatchupID: matchup.ID, HomeScore: 110, AwayScore: 100, Final: true, Authoritative: true, SourceState: "final", Source: "scores", ObservedAt: when}
	if _, err := store.AdvancePublishedPlayoffRound([]PlayoffRoundResult{result}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CorrectPublishedPlayoff(PlayoffCorrection{MatchupID: matchup.ID, WinnerTeamID: matchup.AwayTeamID, Actor: "commissioner", Reason: "audited stat correction", Confirmation: "", At: when}); err == nil {
		t.Fatal("correction without confirmation succeeded")
	}
	corrected, err := store.CorrectPublishedPlayoff(PlayoffCorrection{MatchupID: matchup.ID, WinnerTeamID: matchup.AwayTeamID, HomeScore: 90, AwayScore: 120, ScoresProvided: true, Actor: "commissioner", Reason: "audited stat correction", Confirmation: PlayoffCorrectionConfirmation, At: when})
	if err != nil {
		t.Fatal(err)
	}
	if corrected.ChampionTeamID != matchup.AwayTeamID || corrected.Matchups[0].HomeScore != 90 || corrected.Matchups[0].AwayScore != 120 || len(corrected.Audit) != 4 {
		t.Fatalf("corrected bracket = %+v", corrected)
	}
	last := corrected.Audit[len(corrected.Audit)-1]
	if last.Action != "correction" || last.Reason == "" || last.PreviousWinnerTeamID == "" || last.WinnerTeamID != matchup.AwayTeamID {
		t.Fatalf("correction audit = %+v", last)
	}
}

func TestPS1LegacyBracketRemainsReadableAfterSchemaMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	legacy := PersistedState{
		SchemaVersion: currentSchemaVersion - 1,
		Playoffs: &PlayoffState{
			Config:   PlayoffConfig{TeamCount: 2, StartWeek: 15, RoundLengthWeeks: 1},
			Seeds:    []PlayoffSeed{{Seed: 1, TeamID: "team-1"}, {Seed: 2, TeamID: "team-2"}},
			Matchups: []PlayoffMatchup{{ID: "legacy-final", Bracket: "championship", Round: 1, HomeTeamID: "team-1", AwayTeamID: "team-2"}},
		},
	}
	raw, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewStore(path)
	if err := store.StartupError(); err != nil {
		t.Fatal(err)
	}
	if got := store.Snapshot().SchemaVersion; got != currentSchemaVersion {
		t.Fatalf("schema version = %d, want %d", got, currentSchemaVersion)
	}
	truth := store.PlayoffTruth()
	if truth == nil || truth.Status != PlayoffStatusPublished || truth.Matchups[0].ID != "legacy-final" {
		t.Fatalf("legacy truth = %+v", truth)
	}
}

func TestPS1ConcurrentPublishIsIdempotent(t *testing.T) {
	store := newTestStore(t)
	preview := ps1Preview(t, 4)
	when := time.Date(2026, time.December, 4, 12, 0, 0, 0, time.UTC)
	if err := store.SetPlayoffPreview(preview, "commissioner", when); err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			_, err := store.PublishPlayoffPreview(preview.PreviewID, PlayoffPublishConfirmation, "commissioner", when)
			errs <- err
		}()
	}
	group.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := len(store.PlayoffTruth().Audit); got != 2 {
		t.Fatalf("concurrent publish audit entries = %d, want 2", got)
	}
}
