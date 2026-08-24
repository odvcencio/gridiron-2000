package league

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestStateSchemaCompatibilityReportsAuthoritativePersistedMarker(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "league-state.json")
	store := NewStore(path)
	if err := store.StartupError(); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	// Simulate a supported older persisted marker without changing the
	// in-memory model's normalized current schema. The accessor must report
	// the SQLite marker, not Snapshot().SchemaVersion.
	db, err := openDB(filepath.Join(dir, dbFileName))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE kv SET value = ? WHERE key = ?`, currentSchemaVersion-1, kvSchemaVersion); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	reloaded := NewStore(path)
	t.Cleanup(func() { _ = reloaded.Close() })
	if err := reloaded.StartupError(); err != nil {
		t.Fatal(err)
	}
	got := reloaded.StateSchemaCompatibility()
	if got.PersistedVersion != currentSchemaVersion-1 || got.SupportedVersion != currentSchemaVersion || !got.Compatible {
		t.Fatalf("state schema compatibility = %+v, want persisted=%d supported=%d compatible=true", got, currentSchemaVersion-1, currentSchemaVersion)
	}
	if snapshot := reloaded.Snapshot(); snapshot.SchemaVersion != currentSchemaVersion {
		t.Fatalf("normalized snapshot schema = %d, want %d", snapshot.SchemaVersion, currentSchemaVersion)
	}
}

func TestStateSchemaCompatibilityPreservesTooNewJSONEvidence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "league-state.json")
	future := currentSchemaVersion + 1
	if err := os.WriteFile(path, []byte(fmt.Sprintf(`{"schemaVersion":%d}`, future)), 0o600); err != nil {
		t.Fatal(err)
	}

	store := NewStore(path)
	if err := store.StartupError(); !errors.Is(err, errSchemaTooNew) {
		t.Fatalf("StartupError = %v, want schema-too-new", err)
	}
	got := store.StateSchemaCompatibility()
	if got.PersistedVersion != future || got.SupportedVersion != currentSchemaVersion || got.Compatible {
		t.Fatalf("too-new JSON compatibility = %+v, want persisted=%d supported=%d compatible=false", got, future, currentSchemaVersion)
	}
}

func TestStateSchemaCompatibilityPreservesTooNewSQLiteEvidence(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, dbFileName)
	db, err := openDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := migrateDB(db); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	future := currentSchemaVersion + 1
	if _, err := db.Exec(`UPDATE kv SET value = ? WHERE key = ?`, future, kvSchemaVersion); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	store := NewStore(filepath.Join(dir, "league-state.json"))
	t.Cleanup(func() { _ = store.Close() })
	if err := store.StartupError(); !errors.Is(err, errSchemaTooNew) {
		t.Fatalf("StartupError = %v, want schema-too-new", err)
	}
	got := store.StateSchemaCompatibility()
	if got.PersistedVersion != future || got.SupportedVersion != currentSchemaVersion || got.Compatible {
		t.Fatalf("too-new SQLite compatibility = %+v, want persisted=%d supported=%d compatible=false", got, future, currentSchemaVersion)
	}
}
