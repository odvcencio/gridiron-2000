package league

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func newSchemaSevenStore(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "state.json")
	initial := NewStore(path)
	initial.draftLifecycleBypass = true
	if err := initial.StartupError(); err != nil {
		t.Fatal(err)
	}
	if err := initial.Close(); err != nil {
		t.Fatal(err)
	}
	setLogicalSchemaMarker(t, path, 7)
	return openSchemaSevenStore(t, path), path
}

func openSchemaSevenStore(t *testing.T, path string) *Store {
	t.Helper()
	store := NewStore(path)
	store.draftLifecycleBypass = true
	t.Cleanup(func() { _ = store.Close() })
	if err := store.StartupError(); err != nil {
		t.Fatal(err)
	}
	compatibility := store.StateSchemaCompatibility()
	if compatibility.PersistedVersion != 7 ||
		compatibility.PersistedDatabaseVersion != currentDBVersion ||
		!compatibility.Compatible {
		t.Fatalf("schema-7 fixture compatibility = %+v", compatibility)
	}
	if got := store.Snapshot().SchemaVersion; got != currentSchemaVersion {
		t.Fatalf("normalized fixture schema = %d, want %d", got, currentSchemaVersion)
	}
	return store
}

func setLogicalSchemaMarker(t *testing.T, path string, version int) {
	t.Helper()
	db, err := openDB(filepath.Join(filepath.Dir(path), dbFileName))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`UPDATE kv SET value = ? WHERE key = ?`, version, kvSchemaVersion); err != nil {
		t.Fatal(err)
	}
}

func persistedSchemaMarker(t *testing.T, store *Store) int {
	t.Helper()
	version, known, err := logicalSchemaVersion(store.db)
	if err != nil {
		t.Fatal(err)
	}
	if !known {
		t.Fatal("logical schema marker is unknown")
	}
	return version
}

func seedPreviewSchemaSeven(t *testing.T, teamCount int) (*Store, PlayoffState) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "state.json")
	seed := NewStore(path)
	seed.draftLifecycleBypass = true
	preview := ps1Preview(t, teamCount)
	when := time.Date(2026, time.December, 9, 12, 0, 0, 0, time.UTC)
	if err := seed.SetPlayoffPreview(preview, "commissioner", when); err != nil {
		t.Fatal(err)
	}
	if err := seed.Close(); err != nil {
		t.Fatal(err)
	}
	setLogicalSchemaMarker(t, path, 7)
	return openSchemaSevenStore(t, path), preview
}

func assertSchemaNineAndRejectsV8(t *testing.T, store *Store) {
	t.Helper()
	compatibility := store.StateSchemaCompatibility()
	if compatibility.PersistedVersion != currentSchemaVersion ||
		compatibility.SupportedVersion != currentSchemaVersion ||
		compatibility.PersistedDatabaseVersion != currentDBVersion ||
		compatibility.SupportedDatabaseVersion != currentDBVersion ||
		!compatibility.Compatible {
		t.Fatalf("schema-9 compatibility = %+v", compatibility)
	}
	if got := persistedSchemaMarker(t, store); got != currentSchemaVersion {
		t.Fatalf("persisted logical schema = %d, want %d", got, currentSchemaVersion)
	}
	if err := checkLogicalSchemaVersionAt(store.db, currentSchemaVersion-1); !errors.Is(err, errSchemaTooNew) {
		t.Fatalf("simulated schema-8 startup error = %v, want schema-too-new", err)
	}
}

func seedPublishedSchemaSeven(t *testing.T, teamCount int) (*Store, PlayoffState) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "state.json")
	seed := NewStore(path)
	seed.draftLifecycleBypass = true
	preview := ps1Preview(t, teamCount)
	when := time.Date(2026, time.December, 10, 12, 0, 0, 0, time.UTC)
	if err := seed.SetPlayoffPreview(preview, "commissioner", when); err != nil {
		t.Fatal(err)
	}
	if _, err := seed.PublishPlayoffPreview(preview.PreviewID, PlayoffPublishConfirmation, "commissioner", when); err != nil {
		t.Fatal(err)
	}
	if err := seed.Close(); err != nil {
		t.Fatal(err)
	}
	setLogicalSchemaMarker(t, path, 7)
	return openSchemaSevenStore(t, path), preview
}

func seedCompletedSchemaSeven(t *testing.T) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "state.json")
	seed := NewStore(path)
	seed.draftLifecycleBypass = true
	preview := ps1Preview(t, 2)
	when := time.Date(2026, time.December, 11, 12, 0, 0, 0, time.UTC)
	if err := seed.SetPlayoffPreview(preview, "commissioner", when); err != nil {
		t.Fatal(err)
	}
	if _, err := seed.PublishPlayoffPreview(preview.PreviewID, PlayoffPublishConfirmation, "commissioner", when); err != nil {
		t.Fatal(err)
	}
	matchup := matchupsForRound(preview.Matchups, "championship", 1)[0]
	if _, err := seed.AdvancePublishedPlayoffRound([]PlayoffRoundResult{{
		MatchupID: matchup.ID, HomeScore: 110, AwayScore: 100,
		Final: true, Authoritative: true, SourceState: "final",
		Source: "scores", ObservedAt: when,
	}}); err != nil {
		t.Fatal(err)
	}
	if err := seed.Close(); err != nil {
		t.Fatal(err)
	}
	setLogicalSchemaMarker(t, path, 7)
	return openSchemaSevenStore(t, path)
}

func TestPS1CollectionWritesAdvanceLogicalSchemaAndRefuseOldBinary(t *testing.T) {
	t.Run("preview", func(t *testing.T) {
		store, _ := newSchemaSevenStore(t)
		if err := store.SetPlayoffPreview(ps1Preview(t, 4), "commissioner", time.Now()); err != nil {
			t.Fatal(err)
		}
		assertSchemaNineAndRejectsV8(t, store)
	})

	t.Run("publish", func(t *testing.T) {
		store, preview := seedPreviewSchemaSeven(t, 4)
		when := time.Date(2026, time.December, 12, 12, 0, 0, 0, time.UTC)
		published, err := store.PublishPlayoffPreview(preview.PreviewID, PlayoffPublishConfirmation, "commissioner", when)
		if err != nil {
			t.Fatal(err)
		}
		if published.Status != PlayoffStatusPublished {
			t.Fatalf("published status = %q", published.Status)
		}
		second, err := store.PublishPlayoffPreview(preview.PreviewID, PlayoffPublishConfirmation, "commissioner", when.Add(time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		if second.Revision != published.Revision || len(second.Audit) != len(published.Audit) {
			t.Fatalf("idempotent publish changed state: first=%+v second=%+v", published, second)
		}
		assertSchemaNineAndRejectsV8(t, store)
	})

	t.Run("nonterminal advancement", func(t *testing.T) {
		store, preview := seedPublishedSchemaSeven(t, 4)
		active := matchupsForRound(preview.Matchups, "championship", 1)
		when := time.Date(2026, time.December, 13, 12, 0, 0, 0, time.UTC)
		results := make([]PlayoffRoundResult, 0, len(active))
		for index, matchup := range active {
			results = append(results, PlayoffRoundResult{
				MatchupID: matchup.ID, HomeScore: float64(110 + index),
				AwayScore: 90, Final: true, Authoritative: true,
				SourceState: "final", Source: "scores", ObservedAt: when,
			})
		}
		advanced, err := store.AdvancePublishedPlayoffRound(results)
		if err != nil {
			t.Fatal(err)
		}
		if advanced.ChampionTeamID != "" {
			t.Fatalf("nonterminal advancement produced champion %q", advanced.ChampionTeamID)
		}
		assertSchemaNineAndRejectsV8(t, store)
	})

	t.Run("correction", func(t *testing.T) {
		store := seedCompletedSchemaSeven(t)
		truth := store.PlayoffTruth()
		matchup := matchupsForRound(truth.Matchups, "championship", 1)[0]
		when := time.Date(2026, time.December, 14, 12, 0, 0, 0, time.UTC)
		corrected, err := store.CorrectPublishedPlayoff(PlayoffCorrection{
			MatchupID: matchup.ID, WinnerTeamID: matchup.AwayTeamID,
			HomeScore: 90, AwayScore: 120, ScoresProvided: true,
			Actor: "commissioner", Reason: "audited stat correction",
			Confirmation: PlayoffCorrectionConfirmation, At: when,
		})
		if err != nil {
			t.Fatal(err)
		}
		if corrected.Matchups[0].AwayScore != 120 {
			t.Fatalf("corrected away score = %v", corrected.Matchups[0].AwayScore)
		}
		assertSchemaNineAndRejectsV8(t, store)
	})

	t.Run("SetPlayoffs", func(t *testing.T) {
		store, _ := newSchemaSevenStore(t)
		if err := store.SetPlayoffs(ps1Preview(t, 4)); err != nil {
			t.Fatal(err)
		}
		assertSchemaNineAndRejectsV8(t, store)
	})
}

func TestPS1CollectionSchemaMarkerCommitIsAtomicAndRetryable(t *testing.T) {
	store, path := newSchemaSevenStore(t)
	preview := ps1Preview(t, 4)
	injected := errors.New("injected playoff persist failure")
	store.mu.Lock()
	store.persistHook = func() error { return injected }
	store.mu.Unlock()

	if err := store.SetPlayoffPreview(preview, "commissioner", time.Now()); !errors.Is(err, injected) {
		t.Fatalf("failed preview error = %v, want %v", err, injected)
	}
	if got := persistedSchemaMarker(t, store); got != 7 {
		t.Fatalf("logical marker after rolled-back write = %d, want 7", got)
	}
	if got := store.PlayoffTruth(); got != nil {
		t.Fatalf("rolled-back preview remained in memory: %+v", got)
	}

	store.mu.Lock()
	store.persistHook = nil
	store.mu.Unlock()
	if err := store.SetPlayoffPreview(preview, "commissioner", time.Now()); err != nil {
		t.Fatal(err)
	}
	if got := persistedSchemaMarker(t, store); got != currentSchemaVersion {
		t.Fatalf("logical marker after retry = %d, want %d", got, currentSchemaVersion)
	}
	restarted := NewStore(path)
	t.Cleanup(func() { _ = restarted.Close() })
	if err := restarted.StartupError(); err != nil {
		t.Fatal(err)
	}
	if got := persistedSchemaMarker(t, restarted); got != currentSchemaVersion {
		t.Fatalf("logical marker after restart = %d, want %d", got, currentSchemaVersion)
	}
}

func TestPS1CollectionSchemaMarkerCrashLeavesTransactionUncommitted(t *testing.T) {
	store, path := newSchemaSevenStore(t)
	preview := ps1Preview(t, 4)
	store.mu.Lock()
	store.persistHook = func() error { panic("crash inside playoff persist transaction") }
	store.mu.Unlock()

	func() {
		defer func() {
			if recover() == nil {
				t.Error("the injected crash must reach the caller")
			}
		}()
		_ = store.SetPlayoffPreview(preview, "commissioner", time.Now())
	}()

	restarted := openSchemaSevenStore(t, path)
	if got := restarted.PlayoffTruth(); got != nil {
		t.Fatalf("crashed preview committed: %+v", got)
	}

	store.mu.Lock()
	store.persistHook = nil
	store.mu.Unlock()
	preview.PreviewID = preview.PreviewID + "-retry"
	if err := store.SetPlayoffPreview(preview, "commissioner", time.Now()); err != nil {
		t.Fatal(err)
	}
	assertSchemaNineAndRejectsV8(t, store)
}
