package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gridiron-2000/internal/league"
)

func TestBackupSchedulerConfigFromEnvDefaults(t *testing.T) {
	t.Setenv("BACKUP_ENABLED", "")
	t.Setenv("BACKUP_KEEP", "")
	cfg := backupSchedulerConfigFromEnv("/data")
	if !cfg.Enabled {
		t.Error("Enabled default must be true")
	}
	if cfg.Keep != defaultBackupKeep {
		t.Errorf("Keep = %d, want default %d", cfg.Keep, defaultBackupKeep)
	}
	if cfg.Dir != filepath.Join("/data", "backups") {
		t.Errorf("Dir = %q, want /data/backups", cfg.Dir)
	}
}

func TestBackupSchedulerConfigFromEnvOverrides(t *testing.T) {
	t.Setenv("BACKUP_ENABLED", "false")
	t.Setenv("BACKUP_KEEP", "3")
	cfg := backupSchedulerConfigFromEnv("/data")
	if cfg.Enabled {
		t.Error("BACKUP_ENABLED=false must disable the scheduler")
	}
	if cfg.Keep != 3 {
		t.Errorf("Keep = %d, want 3", cfg.Keep)
	}
}

func TestBackupSchedulerConfigFromEnvIgnoresMalformedKeep(t *testing.T) {
	t.Setenv("BACKUP_ENABLED", "")
	t.Setenv("BACKUP_KEEP", "not-a-number")
	cfg := backupSchedulerConfigFromEnv("/data")
	if cfg.Keep != defaultBackupKeep {
		t.Errorf("Keep = %d, want the default %d for a malformed BACKUP_KEEP", cfg.Keep, defaultBackupKeep)
	}
}

func TestBackupSchedulerConfigFromEnvNoDataDir(t *testing.T) {
	cfg := backupSchedulerConfigFromEnv("")
	if cfg.Dir != "" {
		t.Errorf("Dir = %q, want empty when no data directory is configured", cfg.Dir)
	}
}

func TestStartBackupSchedulerDisabledDoesNothing(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// A nil service would panic if the disabled scheduler tried to use it;
	// this proves the disabled path returns before touching service at all.
	startBackupScheduler(ctx, nil, backupSchedulerConfig{Enabled: false}, "test")
}

// TestRunBackupSnapshotOnceWritesAndRotates exercises the loop's body
// directly (not the ticker) against league.Default() — TestMain already
// gives this process-wide singleton its own state directory (see
// testmain_test.go), the same seam every other root-package test that
// needs a real Service already relies on.
func TestRunBackupSnapshotOnceWritesAndRotates(t *testing.T) {
	backupsDir := filepath.Join(t.TempDir(), "backups")
	cfg := backupSchedulerConfig{Enabled: true, Keep: 1, Dir: backupsDir}

	runBackupSnapshotOnce(context.Background(), league.Default(), cfg, "test")
	time.Sleep(1100 * time.Millisecond) // snapshotFileName carries seconds; force a distinct name
	runBackupSnapshotOnce(context.Background(), league.Default(), cfg, "test")

	entries, err := os.ReadDir(backupsDir)
	if err != nil {
		t.Fatalf("read backups dir: %v", err)
	}
	if len(entries) != 1 {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Fatalf("snapshots remaining = %v, want exactly 1 kept (Keep=1)", names)
	}
}
