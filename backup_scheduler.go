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
}

// backupSchedulerConfigFromEnv reads BACKUP_ENABLED (default true) and
// BACKUP_KEEP (default defaultBackupKeep). dataDir is the directory
// holding league.db (league.Default().DataDir()); snapshots land in its
// "backups" subdirectory, never a shared system temp path.
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
	return backupSchedulerConfig{Enabled: enabled, Keep: keep, Dir: dir}
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
		runBackupSnapshotOnce(ctx, service, cfg, appVersion)
		ticker := time.NewTicker(backupSchedulerInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				runBackupSnapshotOnce(ctx, service, cfg, appVersion)
			}
		}
	}()
}

// runBackupSnapshotOnce writes one snapshot and rotates cfg.Dir to
// cfg.Keep. A failure at either step is logged and never panics or stops
// the loop; the next scheduled tick tries again.
func runBackupSnapshotOnce(ctx context.Context, service *league.Service, cfg backupSchedulerConfig, appVersion string) {
	path, _, err := service.WriteBackupSnapshotFile(ctx, cfg.Dir, time.Now(), appVersion)
	if err != nil {
		log.Printf("scheduled backup: failed: %v", err)
		return
	}
	log.Printf("scheduled backup: wrote %s", path)
	removed, err := league.RotateBackups(cfg.Dir, cfg.Keep)
	if err != nil {
		log.Printf("scheduled backup: rotation failed: %v", err)
		return
	}
	if len(removed) > 0 {
		log.Printf("scheduled backup: rotated out %d old snapshot(s)", len(removed))
	}
}
