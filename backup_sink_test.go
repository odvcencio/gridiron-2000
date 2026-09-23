package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gridiron-2000/internal/league"
)

func TestBackupSinksFromEnvEmptyWhenUnconfigured(t *testing.T) {
	t.Setenv("BACKUP_OFFHOST_DIR", "")
	t.Setenv("BACKUP_OFFHOST_KEEP", "")
	t.Setenv("BACKUP_GCS_BUCKET", "")
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "")
	if sinks := backupSinksFromEnv(); len(sinks) != 0 {
		t.Fatalf("sinks = %v, want none when no sink env var is set", sinks)
	}
}

// TestBackupSinksFromEnvGCSRequiresBothVariables covers the "either, both,
// or neither" contract: BACKUP_GCS_BUCKET and GOOGLE_APPLICATION_CREDENTIALS
// must both be present, or the GCS sink stays disabled — a bucket name
// with no credential config (or a credential config with no bucket) is a
// misconfiguration, not a half-enabled sink.
func TestBackupSinksFromEnvGCSRequiresBothVariables(t *testing.T) {
	t.Setenv("BACKUP_OFFHOST_DIR", "")
	t.Setenv("BACKUP_GCS_BUCKET", "m31labs-gridiron-backups")
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "")
	if sinks := backupSinksFromEnv(); len(sinks) != 0 {
		t.Fatalf("sinks = %v, want none when GOOGLE_APPLICATION_CREDENTIALS is unset", sinks)
	}

	t.Setenv("BACKUP_GCS_BUCKET", "")
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "/var/run/secrets/gcp/config.json")
	if sinks := backupSinksFromEnv(); len(sinks) != 0 {
		t.Fatalf("sinks = %v, want none when BACKUP_GCS_BUCKET is unset", sinks)
	}
}

// TestBackupSinksFromEnvGCSConstructsWhenConfigured covers the opt-in
// path with a real, parseable credential config. It uses a plain
// "service_account"-shaped key (google.FindDefaultCredentials/
// CredentialsFromJSON parse that entirely locally, no network round
// trip) rather than a real external_account/Workload Identity Federation
// config: this test only needs to prove backupSinksFromEnv's own
// wiring — env vars in, a *gcsBackupSink out — not re-verify
// golang.org/x/oauth2/google's own, separately tested, WIF resolution.
func TestBackupSinksFromEnvGCSConstructsWhenConfigured(t *testing.T) {
	t.Setenv("BACKUP_OFFHOST_DIR", "")
	t.Setenv("BACKUP_GCS_BUCKET", "m31labs-gridiron-backups")
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", gcsTestServiceAccountCredentialFile(t))

	sinks := backupSinksFromEnv()
	if len(sinks) != 1 {
		t.Fatalf("sinks = %v, want exactly one GCS sink", sinks)
	}
	gcsSink, ok := sinks[0].(*gcsBackupSink)
	if !ok {
		t.Fatalf("sink = %T, want *gcsBackupSink", sinks[0])
	}
	if got := gcsSink.Name(); got != "gcs:m31labs-gridiron-backups" {
		t.Errorf("Name() = %q, want %q", got, "gcs:m31labs-gridiron-backups")
	}
}

// TestBackupSinksFromEnvGCSUnresolvableCredentialSkipsSilently covers the
// never-block-boot discipline: a bucket configured with an unreadable or
// unresolvable credential config logs (backupSinksFromEnv's own doc
// comment) and is skipped, rather than panicking or fatal-erroring the
// whole process.
func TestBackupSinksFromEnvGCSUnresolvableCredentialSkipsSilently(t *testing.T) {
	t.Setenv("BACKUP_OFFHOST_DIR", "")
	t.Setenv("BACKUP_GCS_BUCKET", "m31labs-gridiron-backups")
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", filepath.Join(t.TempDir(), "missing.json"))

	if sinks := backupSinksFromEnv(); len(sinks) != 0 {
		t.Fatalf("sinks = %v, want none when the GCS credential config cannot be read", sinks)
	}
}

func TestBackupSinksFromEnvDirectoryDefaultsAndOverrides(t *testing.T) {
	t.Setenv("BACKUP_OFFHOST_DIR", "/mnt/offhost")
	t.Setenv("BACKUP_OFFHOST_KEEP", "")
	sinks := backupSinksFromEnv()
	if len(sinks) != 1 {
		t.Fatalf("sinks = %v, want exactly one directory sink", sinks)
	}
	dirSink, ok := sinks[0].(*directoryBackupSink)
	if !ok {
		t.Fatalf("sink = %T, want *directoryBackupSink", sinks[0])
	}
	if dirSink.dir != "/mnt/offhost" || dirSink.keep != defaultBackupOffhostKeep {
		t.Errorf("dir=%q keep=%d, want /mnt/offhost, %d", dirSink.dir, dirSink.keep, defaultBackupOffhostKeep)
	}
	if !strings.Contains(dirSink.Name(), "/mnt/offhost") {
		t.Errorf("Name() = %q, want it to name the directory", dirSink.Name())
	}

	t.Setenv("BACKUP_OFFHOST_KEEP", "3")
	sinks = backupSinksFromEnv()
	if got := sinks[0].(*directoryBackupSink).keep; got != 3 {
		t.Errorf("keep = %d, want 3 from BACKUP_OFFHOST_KEEP", got)
	}
}

func TestDirectoryBackupSinkCopiesAndRotates(t *testing.T) {
	srcDir := t.TempDir()
	destDir := filepath.Join(t.TempDir(), "offhost")
	sink := &directoryBackupSink{dir: destDir, keep: 1}

	writeSnapshot := func(name, body string) string {
		path := filepath.Join(srcDir, name)
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}

	first := writeSnapshot("gridiron-snapshot-20260101-000000.tar.gz", "first")
	if err := sink.Copy(context.Background(), first); err != nil {
		t.Fatalf("Copy (first): %v", err)
	}
	second := writeSnapshot("gridiron-snapshot-20260102-000000.tar.gz", "second")
	if err := sink.Copy(context.Background(), second); err != nil {
		t.Fatalf("Copy (second): %v", err)
	}

	entries, err := os.ReadDir(destDir)
	if err != nil {
		t.Fatalf("read dest dir: %v", err)
	}
	if len(entries) != 1 {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Fatalf("dest entries = %v, want exactly 1 kept (keep=1)", names)
	}
	if entries[0].Name() != "gridiron-snapshot-20260102-000000.tar.gz" {
		t.Errorf("kept file = %q, want the newer snapshot", entries[0].Name())
	}
	// No leftover .tmp staging file from the atomic-copy-then-rename.
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".tmp") {
			t.Errorf("leftover staging file: %s", entry.Name())
		}
	}
	body, err := os.ReadFile(filepath.Join(destDir, "gridiron-snapshot-20260102-000000.tar.gz"))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "second" {
		t.Errorf("copied body = %q, want %q", body, "second")
	}
}

func TestDirectoryBackupSinkCopyFailureIsReportedNotPanicked(t *testing.T) {
	srcDir := t.TempDir()
	srcPath := filepath.Join(srcDir, "gridiron-snapshot-20260101-000000.tar.gz")
	if err := os.WriteFile(srcPath, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A destination "directory" that is actually a regular file: MkdirAll
	// fails, Copy must return an error rather than panic.
	blocked := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocked, []byte("occupied"), 0o600); err != nil {
		t.Fatal(err)
	}
	sink := &directoryBackupSink{dir: filepath.Join(blocked, "nested"), keep: 1}
	if err := sink.Copy(context.Background(), srcPath); err == nil {
		t.Fatal("expected an error copying into an unwritable destination")
	}
}

// fakeBackupSink is a test-only backupSink whose Copy outcome and observed
// calls are controlled directly, so runBackupSnapshotOnce's sink loop can
// be proven both loud (records the failure) and non-blocking (still
// completes, and still tries every other configured sink).
type fakeBackupSink struct {
	name  string
	err   error
	calls int
}

func (f *fakeBackupSink) Name() string { return f.name }
func (f *fakeBackupSink) Copy(ctx context.Context, srcPath string) error {
	f.calls++
	return f.err
}

func TestRunBackupSnapshotOnceRunsEverySinkAndNeverBlocksOnFailure(t *testing.T) {
	backupsDir := filepath.Join(t.TempDir(), "backups")
	failing := &fakeBackupSink{name: "failing", err: errors.New("network mount unavailable")}
	succeeding := &fakeBackupSink{name: "succeeding"}
	cfg := backupSchedulerConfig{Enabled: true, Keep: defaultBackupKeep, Dir: backupsDir, Sinks: []backupSink{failing, succeeding}}

	runBackupSnapshotOnce(context.Background(), league.Default(), cfg, "test")

	if failing.calls != 1 {
		t.Errorf("failing sink calls = %d, want 1", failing.calls)
	}
	if succeeding.calls != 1 {
		t.Errorf("succeeding sink calls = %d, want 1 (a failing sink must not skip the next one)", succeeding.calls)
	}

	health := backupHealthState.payload()
	local, _ := health["local"].(map[string]any)
	if local["healthy"] != true {
		t.Errorf("local health = %+v, want healthy after a successful local write", local)
	}
	sinks, _ := health["sinks"].(map[string]any)
	failingStatus, _ := sinks["failing"].(map[string]any)
	if failingStatus["healthy"] != false || failingStatus["lastError"] == "" {
		t.Errorf("failing sink health = %+v, want unhealthy with a recorded error", failingStatus)
	}
	succeedingStatus, _ := sinks["succeeding"].(map[string]any)
	if succeedingStatus["healthy"] != true {
		t.Errorf("succeeding sink health = %+v, want healthy", succeedingStatus)
	}
}
