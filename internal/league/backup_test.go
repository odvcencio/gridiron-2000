package league

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// backupFixtureConfigJSON is a complete, valid league.json (the shape
// config/league.json.example ships) so backupTestService's Service loads a
// real config file, not a synthesized Config literal — the archive's
// league.json inclusion path reads the file from disk by its recorded
// Source path (see WriteBackupArchive), so a test fixture needs an actual
// file too.
const backupFixtureConfigJSON = `{
  "version": 1,
  "league": {
    "name": "Backup Test League",
    "short_code": "BTL",
    "tagline": "Fantasy Football League",
    "mode_label": "DYNASTY",
    "url": "http://localhost:8080",
    "timezone": "America/New_York",
    "season": 2026
  },
  "teams": [
    { "id": "team-1", "name": "East 1", "abbreviation": "E1", "division": "East", "tone": "cyan" },
    { "id": "team-2", "name": "East 2", "abbreviation": "E2", "division": "East", "tone": "blue" },
    { "id": "team-3", "name": "East 3", "abbreviation": "E3", "division": "East", "tone": "violet" },
    { "id": "team-4", "name": "East 4", "abbreviation": "E4", "division": "East", "tone": "lime" }
  ],
  "draft": {
    "at": "2099-01-01T00:00:00Z",
    "rounds": 15,
    "pick_clock_seconds": 120,
    "format_label": ""
  },
  "season_start_at": "2099-01-08T00:00:00Z",
  "scoring_format": "half_ppr",
  "copy": {
    "hero_kicker": "",
    "footer_line": "",
    "venue_line": "",
    "invite_blurb": ""
  },
  "membership": {
    "allowed_domain": ""
  },
  "roster": {
    "preset": "standard",
    "reserve": {},
    "ir": 0,
    "limits": {}
  },
  "waivers": {
    "mode": "perf-priority",
    "season_weight_pct": 60,
    "faab_budget": 100,
    "clear_days": 2,
    "process_time": "09:00"
  },
  "trades": {
    "deadline": "",
    "veto": "commissioner",
    "review_hours": 24
  },
  "postseason": {
    "teamCount": 4,
    "startWeek": 15,
    "roundLengthWeeks": 1,
    "qualification": "division-winners-wildcards",
    "tiebreakOrder": ["record", "head-to-head", "points-for", "pickem", "seeded-draw"],
    "byes": 0,
    "divisionWinnersFirst": true,
    "reseed": true,
    "consolation": false,
    "toiletBowl": false
  }
}
`

// backupTestService builds a Service around a fixture-populated SQLite
// store (realisticFixture, imported the same way TestImportRoundTripsRealisticState
// does) plus a real league.json on disk, so a backup test exercises both
// archive members, not just the database.
func backupTestService(t *testing.T) (*Service, string) {
	t.Helper()
	dir := t.TempDir()
	statePath := filepath.Join(dir, "league-state.json")
	fixture := realisticFixture()
	raw, err := json.MarshalIndent(fixture, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewStore(statePath)
	t.Cleanup(func() { _ = store.Close() })
	if err := store.StartupError(); err != nil {
		t.Fatalf("fixture store failed to open: %v", err)
	}

	configPath := filepath.Join(dir, "league.json")
	if err := os.WriteFile(configPath, []byte(backupFixtureConfigJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := LoadConfigFile(configPath)
	if err != nil {
		t.Fatalf("load fixture league.json: %v", err)
	}

	svc := &Service{store: store, cfg: cfg, teams: defaultTeams(), players: defaultPlayers()}
	return svc, dir
}

// countAllRows sums every user table's row count, keyed by table name, so a
// snapshot can be proven row-count-equal to its source without hardcoding
// the schema's table list here.
func countAllRows(t *testing.T, db *sql.DB) map[string]int {
	t.Helper()
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'`)
	if err != nil {
		t.Fatal(err)
	}
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		tables = append(tables, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	counts := make(map[string]int, len(tables))
	for _, table := range tables {
		var count int
		if err := db.QueryRow(`SELECT COUNT(*) FROM "` + table + `"`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		counts[table] = count
	}
	return counts
}

// TestWriteBackupArchiveProducesValidOpenableSnapshotWithEqualRowCounts is
// the core VACUUM INTO evidence: a fixture-populated store's backup, once
// extracted, opens as an ordinary SQLite database whose every table holds
// exactly as many rows as the live database it was snapshotted from.
func TestWriteBackupArchiveProducesValidOpenableSnapshotWithEqualRowCounts(t *testing.T) {
	svc, _ := backupTestService(t)
	originalCounts := countAllRows(t, svc.store.db)

	var archive bytes.Buffer
	now := time.Date(2026, 8, 31, 3, 0, 0, 0, time.UTC)
	manifest, err := svc.WriteBackupArchive(context.Background(), &archive, now, "test-1.2.3")
	if err != nil {
		t.Fatalf("WriteBackupArchive: %v", err)
	}
	t.Logf("archive size: %d bytes", archive.Len())
	if manifest.AppVersion != "test-1.2.3" {
		t.Errorf("manifest.AppVersion = %q, want test-1.2.3", manifest.AppVersion)
	}
	if manifest.PersistedVersion != currentSchemaVersion {
		t.Errorf("manifest.PersistedVersion = %d, want %d", manifest.PersistedVersion, currentSchemaVersion)
	}
	if manifest.PersistedDatabaseVersion != currentDBVersion {
		t.Errorf("manifest.PersistedDatabaseVersion = %d, want %d", manifest.PersistedDatabaseVersion, currentDBVersion)
	}
	if !manifest.LeagueConfigIncluded {
		t.Error("manifest.LeagueConfigIncluded = false, want true (a real league.json was loaded)")
	}

	destDir := t.TempDir()
	extracted, err := ExtractBackupArchive(bytes.NewReader(archive.Bytes()), destDir)
	if err != nil {
		t.Fatalf("ExtractBackupArchive: %v", err)
	}
	if extracted.DBSHA256 != manifest.DBSHA256 {
		t.Errorf("extracted manifest DBSHA256 = %s, want %s", extracted.DBSHA256, manifest.DBSHA256)
	}
	if _, err := os.Stat(filepath.Join(destDir, BackupConfigEntryName)); err != nil {
		t.Errorf("expected extracted %s: %v", BackupConfigEntryName, err)
	}

	snapshotDB, err := sql.Open("sqlite", "file:"+filepath.Join(destDir, BackupDBEntryName)+"?mode=ro")
	if err != nil {
		t.Fatalf("open extracted snapshot: %v", err)
	}
	defer snapshotDB.Close()
	if err := snapshotDB.Ping(); err != nil {
		t.Fatalf("extracted snapshot must be a valid, openable SQLite database: %v", err)
	}
	snapshotCounts := countAllRows(t, snapshotDB)
	if !reflect.DeepEqual(originalCounts, snapshotCounts) {
		t.Fatalf("row counts differ:\noriginal: %+v\nsnapshot: %+v", originalCounts, snapshotCounts)
	}
	total := 0
	for _, count := range originalCounts {
		total += count
	}
	if total == 0 {
		t.Fatal("fixture store has zero rows across every table; the row-count comparison is vacuous")
	}
}

// TestWriteBackupArchiveWithoutConfigFile covers the "defaults" league (no
// league.json ever loaded): the archive still succeeds, and honestly
// reports the config as not included instead of inventing one.
func TestWriteBackupArchiveWithoutConfigFile(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(filepath.Join(dir, "league-state.json"))
	t.Cleanup(func() { _ = store.Close() })
	svc := &Service{store: store, cfg: DefaultConfig()}
	svc.cfg.Source = "defaults"

	var archive bytes.Buffer
	manifest, err := svc.WriteBackupArchive(context.Background(), &archive, time.Now(), "dev")
	if err != nil {
		t.Fatalf("WriteBackupArchive: %v", err)
	}
	if manifest.LeagueConfigIncluded {
		t.Error("manifest.LeagueConfigIncluded = true, want false (no league.json was ever loaded)")
	}
	destDir := t.TempDir()
	if _, err := ExtractBackupArchive(bytes.NewReader(archive.Bytes()), destDir); err != nil {
		t.Fatalf("ExtractBackupArchive: %v", err)
	}
	if _, err := os.Stat(filepath.Join(destDir, BackupConfigEntryName)); !os.IsNotExist(err) {
		t.Errorf("expected no extracted %s, stat err = %v", BackupConfigEntryName, err)
	}
}

// buildTestArchive constructs a minimal, fully custom tar+gzip archive so
// ExtractBackupArchive's own validation (hash, schema bound) can be tested
// independently of WriteBackupArchive's production path.
func buildTestArchive(t *testing.T, dbContent []byte, manifest BackupManifest, includeManifest bool) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	writeEntry := func(name string, data []byte) {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(data))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	writeEntry(BackupDBEntryName, dbContent)
	if includeManifest {
		manifestBytes, err := json.Marshal(manifest)
		if err != nil {
			t.Fatal(err)
		}
		writeEntry(BackupManifestEntryName, manifestBytes)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestExtractBackupArchiveVerifiesManifestHash(t *testing.T) {
	dbContent := []byte("not a real database, only hash verification is under test")
	manifest := BackupManifest{
		AppVersion:               "test",
		PersistedVersion:         currentSchemaVersion,
		PersistedDatabaseVersion: currentDBVersion,
		DBFileName:               BackupDBEntryName,
		DBSHA256:                 "0000000000000000000000000000000000000000000000000000000000000000",
	}
	archive := buildTestArchive(t, dbContent, manifest, true)
	if _, err := ExtractBackupArchive(bytes.NewReader(archive), t.TempDir()); !errors.Is(err, ErrBackupHashMismatch) {
		t.Fatalf("err = %v, want ErrBackupHashMismatch", err)
	}
}

func TestExtractBackupArchiveRefusesNewerPhysicalSchema(t *testing.T) {
	dbContent := []byte("db bytes")
	hash := sha256Hex(dbContent)
	manifest := BackupManifest{
		AppVersion:               "test",
		PersistedVersion:         currentSchemaVersion,
		PersistedDatabaseVersion: currentDBVersion + 1,
		DBFileName:               BackupDBEntryName,
		DBSHA256:                 hash,
	}
	archive := buildTestArchive(t, dbContent, manifest, true)
	_, err := ExtractBackupArchive(bytes.NewReader(archive), t.TempDir())
	if !errors.Is(err, ErrBackupSchemaTooNew) {
		t.Fatalf("err = %v, want ErrBackupSchemaTooNew", err)
	}
}

func TestExtractBackupArchiveRefusesNewerLogicalSchema(t *testing.T) {
	dbContent := []byte("db bytes")
	hash := sha256Hex(dbContent)
	manifest := BackupManifest{
		AppVersion:               "test",
		PersistedVersion:         currentSchemaVersion + 1,
		PersistedDatabaseVersion: currentDBVersion,
		DBFileName:               BackupDBEntryName,
		DBSHA256:                 hash,
	}
	archive := buildTestArchive(t, dbContent, manifest, true)
	_, err := ExtractBackupArchive(bytes.NewReader(archive), t.TempDir())
	if !errors.Is(err, ErrBackupSchemaTooNew) {
		t.Fatalf("err = %v, want ErrBackupSchemaTooNew", err)
	}
}

func TestExtractBackupArchiveRequiresManifest(t *testing.T) {
	archive := buildTestArchive(t, []byte("db bytes"), BackupManifest{}, false)
	if _, err := ExtractBackupArchive(bytes.NewReader(archive), t.TempDir()); err == nil {
		t.Fatal("expected an error for an archive with no manifest.json")
	}
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func TestRotateBackupsKeepsN(t *testing.T) {
	dir := t.TempDir()
	names := []string{
		"gridiron-snapshot-20260825-030000.tar.gz",
		"gridiron-snapshot-20260826-030000.tar.gz",
		"gridiron-snapshot-20260827-030000.tar.gz",
		"gridiron-snapshot-20260828-030000.tar.gz",
		"gridiron-snapshot-20260829-030000.tar.gz",
		"gridiron-snapshot-20260830-030000.tar.gz",
		"gridiron-snapshot-20260831-030000.tar.gz",
		"gridiron-snapshot-20260901-030000.tar.gz",
		"gridiron-snapshot-20260902-030000.tar.gz",
	}
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// A hand-saved admin download in the same directory must survive
	// rotation untouched: only the scheduled-snapshot naming pattern is
	// ever subject to RotateBackups.
	adminDownload := "gridiron-backup-btl-20260831.tar.gz"
	if err := os.WriteFile(filepath.Join(dir, adminDownload), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	removed, err := RotateBackups(dir, 7)
	if err != nil {
		t.Fatalf("RotateBackups: %v", err)
	}
	wantRemoved := []string{names[0], names[1]}
	if !reflect.DeepEqual(removed, wantRemoved) {
		t.Fatalf("removed = %v, want %v", removed, wantRemoved)
	}

	remaining, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 8 { // 7 kept snapshots + the admin download
		t.Fatalf("remaining directory entries = %d, want 8", len(remaining))
	}
	if _, err := os.Stat(filepath.Join(dir, adminDownload)); err != nil {
		t.Errorf("admin download must survive rotation: %v", err)
	}
	for _, name := range names[2:] {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("expected %s to survive rotation: %v", name, err)
		}
	}
	for _, name := range names[:2] {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Errorf("expected %s to be removed, stat err = %v", name, err)
		}
	}
}

func TestRotateBackupsOnMissingDirectory(t *testing.T) {
	removed, err := RotateBackups(filepath.Join(t.TempDir(), "does-not-exist"), 7)
	if err != nil {
		t.Fatalf("RotateBackups on a missing directory must not error: %v", err)
	}
	if removed != nil {
		t.Errorf("removed = %v, want nil", removed)
	}
}

func TestWriteBackupSnapshotFileWritesUnderDir(t *testing.T) {
	svc, dir := backupTestService(t)
	backupsDir := filepath.Join(dir, "backups")
	now := time.Date(2026, 8, 31, 3, 0, 0, 0, time.UTC)
	path, manifest, err := svc.WriteBackupSnapshotFile(context.Background(), backupsDir, now, "dev")
	if err != nil {
		t.Fatalf("WriteBackupSnapshotFile: %v", err)
	}
	if filepath.Dir(path) != backupsDir {
		t.Errorf("path = %s, want a file directly under %s", path, backupsDir)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat snapshot file: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("snapshot file mode = %v, want 0600 (owner-only)", info.Mode().Perm())
	}
	if manifest.DBSHA256 == "" {
		t.Error("manifest.DBSHA256 is empty")
	}
}

func TestBackupArchiveFileName(t *testing.T) {
	svc := &Service{cfg: Config{ShortCode: "BTL", Name: "Backup Test League"}}
	when := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	got := svc.BackupArchiveFileName(when)
	want := "gridiron-backup-btl-20260831.tar.gz"
	if got != want {
		t.Errorf("BackupArchiveFileName = %q, want %q", got, want)
	}

	svc2 := &Service{cfg: Config{}}
	got2 := svc2.BackupArchiveFileName(when)
	want2 := "gridiron-backup-league-20260831.tar.gz"
	if got2 != want2 {
		t.Errorf("BackupArchiveFileName (empty config) = %q, want %q", got2, want2)
	}
}
