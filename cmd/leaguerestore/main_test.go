package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gridiron-2000/internal/league"
	_ "modernc.org/sqlite"
)

// buildArchiveFile produces a real, restorable backup archive using only
// league's exported surface (Store, VacuumSnapshot, BackupManifest, and the
// entry-name constants) — the same building blocks WriteBackupArchive uses
// internally, exercised here from outside the package the way
// cmd/leaguerestore's only supported input actually arrives.
func buildArchiveFile(t *testing.T, dir string, mutate func(persistedVersion, dbVersion int) (int, int)) string {
	t.Helper()
	store := league.NewStore(filepath.Join(dir, "league-state.json"))
	t.Cleanup(func() { _ = store.Close() })
	if err := store.StartupError(); err != nil {
		t.Fatalf("fixture store: %v", err)
	}
	if _, err := store.ToggleReady("team-1"); err != nil {
		t.Fatalf("seed a row: %v", err)
	}

	dbSnapshotPath := filepath.Join(dir, "snapshot.db")
	if err := store.VacuumSnapshot(context.Background(), dbSnapshotPath); err != nil {
		t.Fatalf("VacuumSnapshot: %v", err)
	}
	dbBytes, err := os.ReadFile(dbSnapshotPath)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(dbBytes)

	compat := store.StateSchemaCompatibility()
	persistedVersion, dbVersion := compat.PersistedVersion, compat.PersistedDatabaseVersion
	if mutate != nil {
		persistedVersion, dbVersion = mutate(persistedVersion, dbVersion)
	}
	manifest := league.BackupManifest{
		CreatedAt:                time.Date(2026, 8, 31, 3, 0, 0, 0, time.UTC),
		AppVersion:               "test-cli",
		PersistedVersion:         persistedVersion,
		PersistedDatabaseVersion: dbVersion,
		DBFileName:               league.BackupDBEntryName,
		DBSHA256:                 hex.EncodeToString(hash[:]),
		LeagueConfigIncluded:     true,
		LeagueConfigFileName:     league.BackupConfigEntryName,
	}
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}

	archivePath := filepath.Join(dir, "archive.tar.gz")
	archiveFile, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	defer archiveFile.Close()
	gz := gzip.NewWriter(archiveFile)
	tw := tar.NewWriter(gz)
	writeEntry := func(name string, data []byte) {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(data))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	writeEntry(league.BackupDBEntryName, dbBytes)
	writeEntry(league.BackupConfigEntryName, []byte(`{"version":1}`))
	writeEntry(league.BackupManifestEntryName, manifestBytes)
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return archivePath
}

func TestRunRestoresIntoEmptyTargetDirectory(t *testing.T) {
	dir := t.TempDir()
	archive := buildArchiveFile(t, dir, nil)
	target := filepath.Join(dir, "target")

	var stdout, stderr bytes.Buffer
	code := run([]string{"--archive", archive, "--target", target}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run code = %d; stderr: %s", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Errorf("unexpected stderr: %s", stderr.String())
	}
	for _, want := range []string{"Restored league.db", "Restored league.json", "Next steps:", "Start Gridiron again"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("stdout missing %q:\n%s", want, stdout.String())
		}
	}
	if _, err := os.Stat(filepath.Join(target, "league.db")); err != nil {
		t.Errorf("league.db not restored: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "league.json")); err != nil {
		t.Errorf("league.json not restored: %v", err)
	}

	// The restored database is a real, independently openable SQLite file
	// carrying the seeded row, not merely bytes that happened to pass the
	// hash check.
	restoredDB, err := sql.Open("sqlite", "file:"+filepath.Join(target, "league.db")+"?mode=ro")
	if err != nil {
		t.Fatalf("open restored league.db: %v", err)
	}
	defer restoredDB.Close()
	var readyCount int
	if err := restoredDB.QueryRow(`SELECT COUNT(*) FROM ready WHERE team_id = 'team-1' AND ready = 1`).Scan(&readyCount); err != nil {
		t.Fatalf("query restored league.db: %v", err)
	}
	if readyCount != 1 {
		t.Errorf("restored ready row count = %d, want 1 (the seeded ToggleReady row)", readyCount)
	}
}

func TestRunRefusesNonEmptyTargetWithoutForce(t *testing.T) {
	dir := t.TempDir()
	archive := buildArchiveFile(t, dir, nil)
	target := filepath.Join(dir, "target")
	if err := os.MkdirAll(target, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "keep.txt"), []byte("pre-existing"), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"--archive", archive, "--target", target}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("run code = %d, want 1; stdout: %s", code, stdout.String())
	}
	if !strings.Contains(stderr.String(), "not empty") || !strings.Contains(stderr.String(), "--force") {
		t.Errorf("stderr = %q, want a not-empty / --force refusal", stderr.String())
	}
	if _, err := os.Stat(filepath.Join(target, "league.db")); !os.IsNotExist(err) {
		t.Error("league.db must not be written when the restore is refused")
	}

	// --force must proceed and leave the pre-existing file alone.
	var stdout2, stderr2 bytes.Buffer
	code = run([]string{"--archive", archive, "--target", target, "--force"}, &stdout2, &stderr2)
	if code != 0 {
		t.Fatalf("run --force code = %d; stderr: %s", code, stderr2.String())
	}
	if _, err := os.Stat(filepath.Join(target, "league.db")); err != nil {
		t.Errorf("league.db not restored under --force: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "keep.txt")); err != nil {
		t.Errorf("--force must not remove unrelated existing files: %v", err)
	}
}

func TestRunRefusesNewerPhysicalSchema(t *testing.T) {
	dir := t.TempDir()
	bounds := (*league.Store)(nil).StateSchemaCompatibility()
	archive := buildArchiveFile(t, dir, func(persistedVersion, dbVersion int) (int, int) {
		return persistedVersion, bounds.SupportedDatabaseVersion + 1
	})
	target := filepath.Join(dir, "target")

	var stdout, stderr bytes.Buffer
	code := run([]string{"--archive", archive, "--target", target}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("run code = %d, want 1; stdout: %s", code, stdout.String())
	}
	if !strings.Contains(stderr.String(), "newer than this binary supports") {
		t.Errorf("stderr = %q, want a schema-too-new refusal", stderr.String())
	}
	if _, err := os.Stat(filepath.Join(target, "league.db")); !os.IsNotExist(err) {
		t.Error("league.db must not be written when the schema check fails")
	}
}

func TestRunRefusesNewerLogicalSchema(t *testing.T) {
	dir := t.TempDir()
	bounds := (*league.Store)(nil).StateSchemaCompatibility()
	archive := buildArchiveFile(t, dir, func(persistedVersion, dbVersion int) (int, int) {
		return bounds.SupportedVersion + 1, dbVersion
	})
	target := filepath.Join(dir, "target")

	var stdout, stderr bytes.Buffer
	code := run([]string{"--archive", archive, "--target", target}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("run code = %d, want 1; stdout: %s", code, stdout.String())
	}
	if !strings.Contains(stderr.String(), "newer than this binary supports") {
		t.Errorf("stderr = %q, want a schema-too-new refusal", stderr.String())
	}
}

func TestRunRequiresArchiveAndTarget(t *testing.T) {
	for _, args := range [][]string{nil, {"--archive", "x.tar.gz"}, {"--target", "dir"}} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != 2 {
			t.Errorf("run(%v) code = %d, want 2", args, code)
		}
	}
}

func TestRunFailsClosedOnMissingArchive(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--archive", filepath.Join(t.TempDir(), "does-not-exist.tar.gz"), "--target", t.TempDir()}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("run code = %d, want 1; stderr: %s", code, stderr.String())
	}
}
