package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"gridiron-2000/internal/league"
)

// defaultBackupOffhostKeep mirrors defaultBackupKeep (backup_scheduler.go):
// seven rotated off-host snapshots is the same "roughly a week" retention
// window, applied independently of the local directory's own BACKUP_KEEP.
const defaultBackupOffhostKeep = 7

// backupSink copies one already-written local snapshot archive to a
// location outside this host. A sink failure must never block the app or
// the nightly loop: runBackupSnapshotOnce logs it loudly and records it in
// backupHealthState; the next scheduled tick tries again regardless.
type backupSink interface {
	// Name identifies the sink in logs and in the /api/health payload.
	Name() string
	// Copy places srcPath's already-written archive at the sink's own
	// destination and applies the sink's own retention.
	Copy(ctx context.Context, srcPath string) error
}

// directoryBackupSink copies each snapshot archive into a second
// configured directory — typically a mounted network volume, so the bytes
// land on different physical storage than the live data directory (a
// Hetzner Storage Box mounted over SMB or SSHFS, an NFS mount, or any
// other path outside the app's own PVC). It is the sink every deployment
// can use with no new dependency.
//
// An S3-compatible sink (Backblaze B2, Cloudflare R2, ...) is deliberately
// not built into this binary: it would need either a new SDK dependency or
// a hand-rolled SigV4 signer, and this deployment does not need one yet.
// Run rclone in a sidecar container against this same directory instead —
// see docs/backup-restore.md's off-host section for the recipe.
type directoryBackupSink struct {
	dir  string
	keep int
}

func (d *directoryBackupSink) Name() string { return "directory:" + d.dir }

func (d *directoryBackupSink) Copy(ctx context.Context, srcPath string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(d.dir, 0o750); err != nil {
		return fmt.Errorf("create off-host backup directory: %w", err)
	}
	destPath := filepath.Join(d.dir, filepath.Base(srcPath))
	if err := copyFileAtomic(srcPath, destPath); err != nil {
		return err
	}
	if d.keep > 0 {
		if _, err := league.RotateBackups(d.dir, d.keep); err != nil {
			return fmt.Errorf("rotate off-host backups: %w", err)
		}
	}
	return nil
}

// copyFileAtomic copies srcPath to destPath through a same-directory temp
// file plus rename, so a reader of destPath — a later restore, or a
// concurrent rclone sidecar — never observes a partially written file.
// That matters most once destPath is a network mount, where a copy can be
// slow or interrupted mid-write.
func copyFileAtomic(srcPath, destPath string) error {
	src, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("open snapshot for off-host copy: %w", err)
	}
	defer src.Close()
	tmp, err := os.CreateTemp(filepath.Dir(destPath), ".gridiron-offhost-*.tmp")
	if err != nil {
		return fmt.Errorf("create off-host staging file: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := io.Copy(tmp, src); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("copy off-host archive: %w", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("secure off-host archive: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("finish off-host archive: %w", err)
	}
	if err := os.Rename(tmpPath, destPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("place off-host archive: %w", err)
	}
	return nil
}

// backupSinksFromEnv builds every configured off-host sink. BACKUP_OFFHOST_DIR
// names a directory sink; empty (the default) means no directory sink is
// configured. BACKUP_GCS_BUCKET plus GOOGLE_APPLICATION_CREDENTIALS, both
// non-empty, name a Google Cloud Storage sink authenticated by keyless
// Workload Identity Federation (backup_gcs_sink.go's gcsBackupSink —
// owner decision 2026-09-23, ops-drift hardening; see
// docs/backup-restore.md's "Google Cloud Storage" section).
// GOOGLE_APPLICATION_CREDENTIALS is Google's own standard variable
// (golang.org/x/oauth2/google.FindDefaultCredentials' first search
// step), not an app-specific one, so it is checked only for presence
// here — resolution itself, and any error in it, happens inside
// newGCSBackupSink. Either, both, or neither of the two sinks may be
// configured; an unset BACKUP_OFFHOST_KEEP defaults to
// defaultBackupOffhostKeep, mirroring BACKUP_KEEP's own default. A GCS
// credential that fails to resolve is logged once, here, and skipped
// rather than failing the whole process — the same never-block-boot
// discipline every other off-host sink already follows (see this file's
// own package comment on backupSink.Copy).
func backupSinksFromEnv() []backupSink {
	var sinks []backupSink
	if dir := strings.TrimSpace(os.Getenv("BACKUP_OFFHOST_DIR")); dir != "" {
		keep := defaultBackupOffhostKeep
		if raw := strings.TrimSpace(os.Getenv("BACKUP_OFFHOST_KEEP")); raw != "" {
			if parsed, err := strconv.Atoi(raw); err == nil && parsed >= 0 {
				keep = parsed
			}
		}
		sinks = append(sinks, &directoryBackupSink{dir: dir, keep: keep})
	}
	bucket := strings.TrimSpace(os.Getenv("BACKUP_GCS_BUCKET"))
	credConfig := strings.TrimSpace(os.Getenv("GOOGLE_APPLICATION_CREDENTIALS"))
	if bucket != "" && credConfig != "" {
		sink, err := newGCSBackupSink(context.Background(), bucket, league.Default(), &http.Client{Timeout: 30 * time.Second}, time.Now)
		if err != nil {
			log.Printf("backup: GCS sink not started: %v", err)
		} else {
			sinks = append(sinks, sink)
		}
	}
	return sinks
}

// ---------------------------------------------------------------------
// Backup health: a loud, non-blocking readout of the last local snapshot
// and every configured sink's last attempt. /api/health reads this
// (app_build.go) so a failed off-host copy is visible to an operator or a
// monitor without ever flipping process readiness or failing a request —
// runBackupSnapshotOnce and each sink's Copy already guarantee that.
// ---------------------------------------------------------------------

// backupAttemptStatus is one sink's (or the local loop's) last-outcome
// record. The zero value means "never attempted yet."
type backupAttemptStatus struct {
	LastAttempt time.Time
	LastSuccess time.Time
	LastError   string
}

func (b backupAttemptStatus) healthy() bool {
	return b.LastAttempt.IsZero() || b.LastError == ""
}

func (b backupAttemptStatus) payload() map[string]any {
	return map[string]any{
		"lastAttempt": formatBackupHealthTime(b.LastAttempt),
		"lastSuccess": formatBackupHealthTime(b.LastSuccess),
		"lastError":   b.LastError,
		"healthy":     b.healthy(),
	}
}

func formatBackupHealthTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// backupHealth is the process-wide, thread-safe last-outcome record the
// nightly backup loop keeps. One backupHealth serves the whole process
// (backupHealthState below), the same singleton-readout shape as the
// existing wireStatus/openStatus/poolStatus health sources app_build.go's
// /api/health already reads.
type backupHealth struct {
	mu    sync.RWMutex
	local backupAttemptStatus
	sinks map[string]backupAttemptStatus
}

var backupHealthState = &backupHealth{sinks: map[string]backupAttemptStatus{}}

func (h *backupHealth) recordLocal(err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.local.LastAttempt = time.Now()
	if err != nil {
		h.local.LastError = err.Error()
		return
	}
	h.local.LastSuccess = h.local.LastAttempt
	h.local.LastError = ""
}

func (h *backupHealth) recordSink(name string, err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	status := h.sinks[name]
	status.LastAttempt = time.Now()
	if err != nil {
		status.LastError = err.Error()
	} else {
		status.LastSuccess = status.LastAttempt
		status.LastError = ""
	}
	h.sinks[name] = status
}

// payload renders the whole readout for /api/health. It always returns a
// non-nil "sinks" map (rather than JSON null) so a caller with no sinks
// configured yet sees an explicit empty object.
func (h *backupHealth) payload() map[string]any {
	h.mu.RLock()
	defer h.mu.RUnlock()
	sinks := make(map[string]any, len(h.sinks))
	for name, status := range h.sinks {
		sinks[name] = status.payload()
	}
	return map[string]any{
		"local": h.local.payload(),
		"sinks": sinks,
	}
}
