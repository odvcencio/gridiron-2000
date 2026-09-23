package main

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gridiron-2000/internal/league"
	"gridiron-2000/internal/loopguard"
)

// defaultBackupKeep is BACKUP_KEEP's default: seven rotated local
// snapshots, roughly a week of nightly recovery points.
const defaultBackupKeep = 7

// backupSchedulerInterval is how often the nightly snapshot loop runs. It
// is deliberately not a precise midnight scheduler: the loop fires once
// shortly after startup, then every 24 hours from that instant. That is
// simple, testable, and enough for a nightly safety net — not a cron
// replacement.
const backupSchedulerInterval = 24 * time.Hour

// backupSchedulerConfig is the nightly snapshot loop's resolved settings.
type backupSchedulerConfig struct {
	Enabled bool
	Keep    int
	Dir     string
	// Sinks copies each successful local snapshot off this host. Empty
	// means no off-host sink is configured yet (BACKUP_OFFHOST_DIR unset).
	Sinks []backupSink
}

// backupSchedulerConfigFromEnv reads BACKUP_ENABLED (default true),
// BACKUP_KEEP (default defaultBackupKeep), and every configured off-host
// sink (backupSinksFromEnv). dataDir is the directory holding league.db
// (league.Default().DataDir()); local snapshots land in its "backups"
// subdirectory, never a shared system temp path.
func backupSchedulerConfigFromEnv(dataDir string) backupSchedulerConfig {
	enabled := true
	if raw := strings.TrimSpace(os.Getenv("BACKUP_ENABLED")); raw != "" {
		if parsed, err := strconv.ParseBool(raw); err == nil {
			enabled = parsed
		}
	}
	keep := defaultBackupKeep
	if raw := strings.TrimSpace(os.Getenv("BACKUP_KEEP")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed >= 0 {
			keep = parsed
		}
	}
	dir := ""
	if dataDir != "" {
		dir = filepath.Join(dataDir, "backups")
	}
	return backupSchedulerConfig{Enabled: enabled, Keep: keep, Dir: dir, Sinks: backupSinksFromEnv()}
}

// startBackupScheduler runs the nightly local snapshot loop: one VACUUM
// INTO archive per backupSchedulerInterval, saved under cfg.Dir, with
// anything beyond cfg.Keep removed after each successful run. Off-host
// copying remains the operator's job (docs/backup-restore.md) — this loop
// only ever writes to the local data volume, and never blocks the writer
// longer than the VACUUM INTO snapshot itself requires (see
// Store.VacuumSnapshot's doc comment).
func startBackupScheduler(ctx context.Context, service *league.Service, cfg backupSchedulerConfig, appVersion string) {
	if !cfg.Enabled {
		log.Printf("scheduled backups: disabled (BACKUP_ENABLED=false)")
		return
	}
	if cfg.Dir == "" {
		log.Printf("scheduled backups: no data directory configured; skipping")
		return
	}
	go func() {
		// loopguard.Tick: a panic inside one snapshot attempt (a VACUUM
		// error path, a sink implementation bug) must not silently end
		// nightly backups for the rest of the process's life (audit item
		// 12) — the next scheduled tick still runs.
		loopguard.Tick("backupScheduler", 0, func() { runBackupSnapshotOnce(ctx, service, cfg, appVersion) })
		ticker := time.NewTicker(backupSchedulerInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				loopguard.Tick("backupScheduler", 0, func() { runBackupSnapshotOnce(ctx, service, cfg, appVersion) })
			}
		}
	}()
}

// runBackupSnapshotOnce writes one local snapshot, rotates cfg.Dir to
// cfg.Keep, and then copies the fresh snapshot to every configured
// off-host sink. Every step's outcome is recorded in backupHealthState for
// /api/health, and a failure at any step — local write, local rotation, or
// any one sink — is logged loudly. A failure never panics, never stops the
// loop, and a sink failure never skips the other configured sinks: the
// next scheduled tick, and the next sink, always get their own try.
func runBackupSnapshotOnce(ctx context.Context, service *league.Service, cfg backupSchedulerConfig, appVersion string) {
	path, _, err := service.WriteBackupSnapshotFile(ctx, cfg.Dir, time.Now(), appVersion)
	backupHealthState.recordLocal(err)
	if err != nil {
		log.Printf("scheduled backup: failed: %v", err)
		return
	}
	log.Printf("scheduled backup: wrote %s", path)
	removed, err := league.RotateBackups(cfg.Dir, cfg.Keep)
	if err != nil {
		log.Printf("scheduled backup: rotation failed: %v", err)
	} else if len(removed) > 0 {
		log.Printf("scheduled backup: rotated out %d old snapshot(s)", len(removed))
	}

	for _, sink := range cfg.Sinks {
		sinkErr := sink.Copy(ctx, path)
		backupHealthState.recordSink(sink.Name(), sinkErr)
		if sinkErr != nil {
			log.Printf("scheduled backup: off-host sink %s failed: %v", sink.Name(), sinkErr)
			continue
		}
		log.Printf("scheduled backup: off-host sink %s copied %s", sink.Name(), filepath.Base(path))
	}
}
