package league

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestIdentityPreflightProjectsCompatibleMigrationWithoutMutationOrPII(t *testing.T) {
	older := time.Date(2026, 8, 22, 1, 0, 0, 0, time.UTC)
	state := PersistedState{
		Members: map[string]Member{
			identityAliasEmail: {
				TeamID: "team-private", Email: identityAliasEmail, Role: "co",
			},
			identityCanonicalEmail: {
				TeamID: "team-private", Email: identityCanonicalEmail, Role: "co",
			},
		},
		CoInvites: map[string]string{
			identityAliasEmail:     "team-private",
			identityCanonicalEmail: "team-private",
		},
		Boards: map[string][]string{
			identityAliasEmail:     {"player-private-a"},
			identityCanonicalEmail: {"player-private-b"},
		},
		Pickems: map[string]map[string]string{
			identityAliasEmail:     {"game-private-a": "BUF"},
			identityCanonicalEmail: {"game-private-b": "MIA"},
		},
		BlitzEntries: map[string]map[string]BlitzEntry{
			identityAliasEmail: {
				"pre2": {Players: []string{"player-private-a"}, UpdatedAt: older},
			},
			identityCanonicalEmail: {
				"pre3": {Players: []string{"player-private-b"}, UpdatedAt: older},
			},
		},
		NotifyPrefs: map[string]map[string]bool{
			identityAliasEmail:     {categoryDraftLive: true},
			identityCanonicalEmail: {categoryTransactions: false},
		},
		Announcements: []Announcement{{
			ID: "announcement-private", PostedBy: identityAliasEmail,
		}},
		SentLog: map[string]time.Time{
			"broadcast:announcement-private:" + identityAliasEmail:     older,
			"broadcast:announcement-private:" + identityCanonicalEmail: older.Add(time.Minute),
		},
	}
	normalizeState(&state)
	original, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}

	report := PreflightIdentityAliases(state, testIdentityResolver(t))
	if !report.Ready || !report.WouldChange || report.ConflictCategory != "" {
		t.Fatalf("report = %+v, want ready migration", report)
	}
	if report.AliasMappings != 1 {
		t.Fatalf("alias mappings = %d, want 1", report.AliasMappings)
	}
	if report.Before.Members != 2 || report.After.Members != 1 ||
		report.Before.TeamSeats != 2 || report.After.TeamSeats != 1 ||
		report.Before.CoManagerRoles != 2 || report.After.CoManagerRoles != 1 {
		t.Fatalf("member/seat/role counts = before %+v after %+v", report.Before, report.After)
	}
	if report.Before.CoManagerInvites != 2 || report.After.CoManagerInvites != 1 ||
		report.Before.BoardOwners != 2 || report.After.BoardOwners != 1 ||
		report.After.BoardPlayers != 2 ||
		report.Before.PickemOwners != 2 || report.After.PickemOwners != 1 ||
		report.After.PickemPicks != 2 ||
		report.Before.BlitzOwners != 2 || report.After.BlitzOwners != 1 ||
		report.After.BlitzEntries != 2 ||
		report.Before.NotificationOwners != 2 || report.After.NotificationOwners != 1 ||
		report.After.NotificationPreferences != 2 ||
		report.Before.SentNotifications != 2 || report.After.SentNotifications != 1 {
		t.Fatalf("owned-state counts = before %+v after %+v", report.Before, report.After)
	}
	after, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, original) {
		t.Fatal("preflight mutated its input snapshot")
	}

	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{
		identityAliasEmail, identityCanonicalEmail, "team-private",
		"player-private", "game-private", "announcement-private",
	} {
		if strings.Contains(string(raw), private) {
			t.Fatalf("report leaked private value %q: %s", private, raw)
		}
	}
}

func TestIdentityPreflightJSONSnapshotRejectsFutureSchemaWithoutMutation(t *testing.T) {
	state := PersistedState{
		SchemaVersion: currentSchemaVersion + 1,
		Members: map[string]Member{
			identityAliasEmail: {TeamID: "private-team", Email: identityAliasEmail},
		},
	}
	original, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	report := PreflightIdentityAliasesFromJSONSnapshot(state, testIdentityResolver(t))
	if report.Ready || report.WouldChange || report.ConflictCategory != "snapshot_schema" {
		t.Fatalf("report = %+v, want bounded future-schema refusal", report)
	}
	after, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, original) {
		t.Fatal("future-schema JSON input changed during rejected preflight")
	}
}

func TestIdentityPreflightReadsOfflineSQLiteSnapshotWithoutWriting(t *testing.T) {
	dir := t.TempDir()
	legacyPath := filepath.Join(dir, "league-state.json")
	raw, err := json.Marshal(PersistedState{
		SchemaVersion: currentSchemaVersion,
		Members: map[string]Member{
			identityAliasEmail: {TeamID: "team-private", Email: identityAliasEmail},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacyPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewStore(legacyPath)
	if err := store.StartupError(); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	databasePath := filepath.Join(dir, dbFileName)
	before, err := os.ReadFile(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	report := PreflightIdentityAliasesFromSQLiteSnapshot(databasePath, testIdentityResolver(t))
	if !report.Ready || !report.WouldChange || report.Before.Members != 1 || report.After.Members != 1 {
		t.Fatalf("report = %+v, want safe projected alias rewrite", report)
	}
	after, err := os.ReadFile(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, before) {
		t.Fatal("SQLite snapshot bytes changed during preflight")
	}

	db, err := openDB(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE kv SET value = ? WHERE key = ?`, currentSchemaVersion+1, kvSchemaVersion); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	futureBefore, err := os.ReadFile(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	report = PreflightIdentityAliasesFromSQLiteSnapshot(databasePath, testIdentityResolver(t))
	if report.Ready || report.ConflictCategory != "snapshot_schema" {
		t.Fatalf("future SQLite report = %+v, want schema parity with JSON", report)
	}
	futureAfter, err := os.ReadFile(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(futureAfter, futureBefore) {
		t.Fatal("future-schema SQLite snapshot bytes changed during preflight")
	}
}

func TestIdentityPreflightConflictCategoriesFailClosedWithoutMutation(t *testing.T) {
	tooLargeBoard := make([]string, boardLimit+1)
	for i := range tooLargeBoard {
		tooLargeBoard[i] = fmt.Sprintf("player-%d", i)
	}
	tests := []struct {
		name     string
		state    PersistedState
		category string
	}{
		{
			name: "seat",
			state: PersistedState{Members: map[string]Member{
				identityAliasEmail:     {TeamID: "team-a", Email: identityAliasEmail},
				identityCanonicalEmail: {TeamID: "team-b", Email: identityCanonicalEmail},
			}},
			category: "seat",
		},
		{
			name: "role",
			state: PersistedState{Members: map[string]Member{
				identityAliasEmail:     {TeamID: "team-a", Email: identityAliasEmail, Role: "co"},
				identityCanonicalEmail: {TeamID: "team-a", Email: identityCanonicalEmail},
			}},
			category: "role",
		},
		{
			name: "co manager",
			state: PersistedState{CoInvites: map[string]string{
				identityAliasEmail: "team-a", identityCanonicalEmail: "team-b",
			}},
			category: "co_manager",
		},
		{
			name: "board",
			state: PersistedState{Boards: map[string][]string{
				identityAliasEmail: tooLargeBoard,
			}},
			category: "board",
		},
		{
			name: "pickem",
			state: PersistedState{Pickems: map[string]map[string]string{
				identityAliasEmail: {"game": "BUF"}, identityCanonicalEmail: {"game": "MIA"},
			}},
			category: "pickem",
		},
		{
			name: "blitz",
			state: PersistedState{BlitzEntries: map[string]map[string]BlitzEntry{
				identityAliasEmail:     {"pre2": {Players: []string{"a"}}},
				identityCanonicalEmail: {"pre2": {Players: []string{"b"}}},
			}},
			category: "blitz",
		},
		{
			name: "notification",
			state: PersistedState{NotifyPrefs: map[string]map[string]bool{
				identityAliasEmail:     {categoryDraftLive: true},
				identityCanonicalEmail: {categoryDraftLive: false},
			}},
			category: "notification",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			normalizeState(&tt.state)
			original, err := json.Marshal(tt.state)
			if err != nil {
				t.Fatal(err)
			}
			report := PreflightIdentityAliases(tt.state, testIdentityResolver(t))
			if report.Ready || report.WouldChange || report.ConflictCategory != tt.category {
				t.Fatalf("report = %+v, want fail-closed category %q", report, tt.category)
			}
			if report.After != (IdentityPreflightCounts{}) {
				t.Fatalf("projected counts = %+v, want withheld on conflict", report.After)
			}
			after, err := json.Marshal(tt.state)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(after, original) {
				t.Fatal("conflict preflight mutated its input snapshot")
			}
		})
	}
}
