package league

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ---------------------------------------------------------------------
// One-click backup/restore (data sovereignty).
//
// A backup archive is a tar+gzip file holding exactly three entries: a
// consistent SQLite snapshot (league.db, produced by VACUUM INTO, never a
// raw copy of the live WAL-mode database), the league.json this process
// actually loaded (when one was found), and manifest.json — the
// machine-readable record of both schema markers, the app version, the
// snapshot timestamp, and the database's SHA-256.
//
// WriteBackupArchive is the write side (an admin download, or the nightly
// scheduler). ExtractBackupArchive is the read side; the only supported
// restore path is the offline cmd/leaguerestore CLI — see its doc comment
// for why a web-facing upload/restore is not offered.
// ---------------------------------------------------------------------

// BackupDBEntryName, BackupConfigEntryName, and BackupManifestEntryName are
// the fixed tar entry names inside every backup archive this package
// writes or reads. cmd/leaguerestore never needs to know these directly —
// ExtractBackupArchive is the sole reader — but they are exported so a
// caller inspecting an archive with a plain tar tool knows what to expect.
const (
	BackupDBEntryName       = "league.db"
	BackupConfigEntryName   = "league.json"
	BackupManifestEntryName = "manifest.json"
)

// ErrBackupSchemaTooNew means an archive's manifest declares a logical or
// physical schema version this binary does not support. It mirrors the
// launch checklist's rollback doctrine (docs/launch-checklist.md, "Rollback
// criteria and commands"): a binary must never read state a newer one
// wrote. ExtractBackupArchive still writes the archive's files to destDir
// before returning this error — see its doc comment.
var ErrBackupSchemaTooNew = errors.New("league: backup archive schema is newer than this binary supports")

// ErrBackupHashMismatch means the extracted league.db does not match the
// manifest's recorded SHA-256: the archive is corrupt or was tampered with
// after it was written.
var ErrBackupHashMismatch = errors.New("league: backup archive database hash does not match its manifest")

// BackupManifest is the machine-readable summary written as manifest.json
// inside every backup archive. It carries no league secrets or member
// data: schema markers, an app version, a creation timestamp, and the
// database snapshot's content hash.
type BackupManifest struct {
	CreatedAt time.Time `json:"createdAt"`
	// AppVersion is the Gridiron release that wrote this archive (main's
	// appVersion — "dev" outside a release build).
	AppVersion string `json:"appVersion"`
	// PersistedVersion is the logical PersistedState schema the snapshot
	// carries (Store.StateSchemaCompatibility's PersistedVersion).
	PersistedVersion int `json:"persistedVersion"`
	// PersistedDatabaseVersion is SQLite's PRAGMA user_version migration
	// marker the snapshot carries (Store.StateSchemaCompatibility's
	// PersistedDatabaseVersion) — the physical schema generation.
	PersistedDatabaseVersion int    `json:"persistedDatabaseVersion"`
	DBFileName               string `json:"dbFileName"`
	DBSHA256                 string `json:"dbSha256"`
	LeagueConfigIncluded     bool   `json:"leagueConfigIncluded"`
	LeagueConfigFileName     string `json:"leagueConfigFileName,omitempty"`
}

// DataDir returns the directory holding the league database, or "" when
// the store runs without persistence (an in-memory test store). Backup and
// restore staging always use this directory (or a fresh subdirectory of
// it), never the shared system temp directory, so a snapshot never leaves
// the volume the operator already trusts with league state.
func (s *Store) DataDir() string {
	if s == nil {
		return ""
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.dbPath == "" {
		return ""
	}
	return filepath.Dir(s.dbPath)
}

// VacuumSnapshot writes a consistent, point-in-time copy of the league
// database to destPath using SQLite's VACUUM INTO, run on the store's own
// live connection. This is deliberately not a filesystem copy of
// data/league.db: a raw copy of a WAL-mode database can capture torn,
// inconsistent pages mid-write, while VACUUM INTO always produces a
// complete, internally consistent snapshot as of one instant.
//
// destPath's parent directory must already exist; destPath itself must not
// — SQLite refuses to VACUUM INTO a file that is already there. The
// store's connection pool holds exactly one physical connection (openDB's
// doc comment), so this call queues behind any write already in flight and
// blocks later ones only for its own duration — the same serialization
// every other Store operation already accepts, never more.
func (s *Store) VacuumSnapshot(ctx context.Context, destPath string) error {
	if s == nil {
		return errors.New("league: nil store has no database to snapshot")
	}
	s.mu.RLock()
	db := s.db
	s.mu.RUnlock()
	if db == nil {
		return errors.New("league: database is not open")
	}
	if _, err := db.ExecContext(ctx, "VACUUM INTO ?", destPath); err != nil {
		return fmt.Errorf("VACUUM INTO: %w", err)
	}
	return nil
}

// DataDir returns the directory holding the league database, or "" when
// the service runs without persistence. main.go's nightly backup scheduler
// uses this to place data/backups/ beside data/league.db, never in a
// shared system temp directory.
func (s *Service) DataDir() string {
	if s == nil {
		return ""
	}
	return s.store.DataDir()
}

// BackupArchiveFileName renders the dated download filename an admin
// backup or a nightly snapshot uses: gridiron-backup-<league>-<date>.tar.gz.
// <league> is the configured short code, lowercased and stripped to
// [a-z0-9]; an empty result (no league.json, or an exotic short code) falls
// back to "league" rather than producing a malformed name.
func (s *Service) BackupArchiveFileName(when time.Time) string {
	slug := backupSlug(s.cfg.ShortCode)
	if slug == "" {
		slug = backupSlug(s.cfg.Name)
	}
	if slug == "" {
		slug = "league"
	}
	return fmt.Sprintf("gridiron-backup-%s-%s.tar.gz", slug, when.UTC().Format("20060102"))
}

func backupSlug(raw string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(raw)) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// snapshotFileName renders the nightly/rotated snapshot filename: sorted
// ascending by name is sorted ascending by time, which RotateBackups
// relies on. Unlike BackupArchiveFileName (one per calendar day, admin
// download), this carries seconds so consecutive nightly runs — or a
// manual extra run the same day — never collide.
func snapshotFileName(when time.Time) string {
	return fmt.Sprintf("gridiron-snapshot-%s.tar.gz", when.UTC().Format("20060102-150405"))
}

// WriteBackupArchive streams a complete, restorable league backup to w: the
// live database's consistent VACUUM INTO snapshot, the league.json this
// process actually loaded (when one was found), and manifest.json. now is
// the caller's clock (time.Now in production; a fixed instant in tests).
//
// The archive never carries secrets, environment values, or any Signal
// Wire / Open Stats cache — those refetch on the next boot. It is a
// read-only export: WriteBackupArchive never mutates the store. The only
// supported way back in is cmd/leaguerestore, run offline against a
// stopped app; see its doc comment.
func (s *Service) WriteBackupArchive(ctx context.Context, w io.Writer, now time.Time, appVersion string) (BackupManifest, error) {
	var manifest BackupManifest
	if s == nil || s.store == nil {
		return manifest, errors.New("league: service has no store to back up")
	}
	compat := s.store.StateSchemaCompatibility()
	if !compat.Compatible {
		return manifest, errors.New("league: store schema is not in a known-compatible state; refusing to back up")
	}
	stageParent := s.store.DataDir()
	if stageParent == "" {
		return manifest, errors.New("league: backup requires a persistent database (no data directory)")
	}
	stageDir, err := os.MkdirTemp(stageParent, ".gridiron-backup-*")
	if err != nil {
		return manifest, fmt.Errorf("stage backup temp dir: %w", err)
	}
	defer os.RemoveAll(stageDir)

	dbSnapshotPath := filepath.Join(stageDir, BackupDBEntryName)
	if err := s.store.VacuumSnapshot(ctx, dbSnapshotPath); err != nil {
		return manifest, err
	}
	if err := os.Chmod(dbSnapshotPath, 0o600); err != nil {
		return manifest, fmt.Errorf("secure backup snapshot: %w", err)
	}
	dbFile, err := os.Open(dbSnapshotPath)
	if err != nil {
		return manifest, err
	}
	defer dbFile.Close()
	dbInfo, err := dbFile.Stat()
	if err != nil {
		return manifest, err
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, dbFile); err != nil {
		return manifest, err
	}
	if _, err := dbFile.Seek(0, io.SeekStart); err != nil {
		return manifest, err
	}

	var configBytes []byte
	configIncluded := false
	if configPath, ok := strings.CutPrefix(s.cfg.Source, "file:"); ok {
		raw, err := os.ReadFile(configPath)
		if err != nil {
			return manifest, fmt.Errorf("read league config for backup: %w", err)
		}
		configBytes = raw
		configIncluded = true
	}

	manifest = BackupManifest{
		CreatedAt:                now.UTC(),
		AppVersion:               appVersion,
		PersistedVersion:         compat.PersistedVersion,
		PersistedDatabaseVersion: compat.PersistedDatabaseVersion,
		DBFileName:               BackupDBEntryName,
		DBSHA256:                 hex.EncodeToString(hash.Sum(nil)),
		LeagueConfigIncluded:     configIncluded,
	}
	if configIncluded {
		manifest.LeagueConfigFileName = BackupConfigEntryName
	}
	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return manifest, err
	}

	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)
	if err := writeTarFile(tw, BackupDBEntryName, dbInfo.ModTime(), dbFile, dbInfo.Size()); err != nil {
		return manifest, err
	}
	if configIncluded {
		if err := writeTarBytes(tw, BackupConfigEntryName, now, configBytes); err != nil {
			return manifest, err
		}
	}
	if err := writeTarBytes(tw, BackupManifestEntryName, now, manifestBytes); err != nil {
		return manifest, err
	}
	if err := tw.Close(); err != nil {
		return manifest, err
	}
	if err := gz.Close(); err != nil {
		return manifest, err
	}
	return manifest, nil
}

// WriteBackupSnapshotFile builds one backup archive and saves it under dir
// (created if missing, owner-only) as snapshotFileName(now). It is the
// nightly scheduler's and any future "run one now" caller's single entry
// point: dir stays local (BACKUP_ENABLED/BACKUP_KEEP, off-host copying
// remains the operator's job — see docs/backup-restore.md).
func (s *Service) WriteBackupSnapshotFile(ctx context.Context, dir string, now time.Time, appVersion string) (path string, manifest BackupManifest, err error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", manifest, fmt.Errorf("create backup directory: %w", err)
	}
	path = filepath.Join(dir, snapshotFileName(now))
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", manifest, fmt.Errorf("create backup file: %w", err)
	}
	manifest, err = s.WriteBackupArchive(ctx, file, now, appVersion)
	closeErr := file.Close()
	if err != nil {
		_ = os.Remove(path)
		return "", manifest, err
	}
	if closeErr != nil {
		_ = os.Remove(path)
		return "", manifest, closeErr
	}
	return path, manifest, nil
}

// RotateBackups keeps the keep most recent gridiron-snapshot-*.tar.gz files
// in dir (by name, which sorts chronologically — see snapshotFileName) and
// removes the rest. keep <= 0 is treated as "keep nothing produced by
// scheduled snapshots," which is never called with production defaults
// (BACKUP_KEEP defaults to 7) but is valid for a caller that wants to
// disable retention outright. Any file in dir that does not match the
// scheduled-snapshot naming pattern is left untouched — an admin download
// saved into the same directory by hand, say. Returns the removed
// filenames (not full paths) for logging.
func RotateBackups(dir string, keep int) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, "gridiron-snapshot-") && strings.HasSuffix(name, ".tar.gz") {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	if keep < 0 {
		keep = 0
	}
	if len(names) <= keep {
		return nil, nil
	}
	toRemove := names[:len(names)-keep]
	removed := make([]string, 0, len(toRemove))
	for _, name := range toRemove {
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			return removed, err
		}
		removed = append(removed, name)
	}
	return removed, nil
}

// ExtractBackupArchive reads a backup archive from r, writes league.db (and
// league.json, when present) into destDir (created if missing), and
// returns the parsed manifest. It verifies the extracted database's
// SHA-256 against the manifest (ErrBackupHashMismatch on failure) and
// refuses a manifest declaring a newer logical or physical schema than
// this binary supports (ErrBackupSchemaTooNew) — the same rollback
// doctrine docs/launch-checklist.md applies to a live rollout.
//
// destDir receives the extracted files even when this function returns
// ErrBackupSchemaTooNew or ErrBackupHashMismatch: a caller that needs
// atomicity (cmd/leaguerestore) extracts into a fresh staging directory
// and only moves the result into the live data directory after a nil
// error.
func ExtractBackupArchive(r io.Reader, destDir string) (BackupManifest, error) {
	var manifest BackupManifest
	if err := os.MkdirAll(destDir, 0o750); err != nil {
		return manifest, fmt.Errorf("create restore staging directory: %w", err)
	}
	gz, err := gzip.NewReader(r)
	if err != nil {
		return manifest, fmt.Errorf("open archive: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)

	var manifestFound, dbFound bool
	var manifestBytes []byte
	var dbHash string
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return manifest, fmt.Errorf("read archive: %w", err)
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}
		switch header.Name {
		case BackupManifestEntryName:
			manifestBytes, err = io.ReadAll(tr)
			if err != nil {
				return manifest, fmt.Errorf("read manifest.json: %w", err)
			}
			manifestFound = true
		case BackupDBEntryName:
			dbHash, err = extractTarEntryWithHash(tr, filepath.Join(destDir, BackupDBEntryName))
			if err != nil {
				return manifest, err
			}
			dbFound = true
		case BackupConfigEntryName:
			if _, err := extractTarEntryWithHash(tr, filepath.Join(destDir, BackupConfigEntryName)); err != nil {
				return manifest, err
			}
		default:
			// Forward compatibility: an unknown entry from a newer archive
			// format is ignored, not fatal, here. The schema check below is
			// what actually decides whether this binary may act on it.
		}
	}
	if !manifestFound {
		return manifest, errors.New("league: backup archive has no manifest.json")
	}
	if !dbFound {
		return manifest, errors.New("league: backup archive has no league.db")
	}
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return manifest, fmt.Errorf("parse manifest.json: %w", err)
	}
	if dbHash != manifest.DBSHA256 {
		return manifest, fmt.Errorf("%w: manifest %s, extracted %s", ErrBackupHashMismatch, manifest.DBSHA256, dbHash)
	}
	if manifest.PersistedDatabaseVersion > currentDBVersion {
		return manifest, fmt.Errorf("%w: archive database schema %d, this binary supports up to %d",
			ErrBackupSchemaTooNew, manifest.PersistedDatabaseVersion, currentDBVersion)
	}
	if manifest.PersistedVersion > currentSchemaVersion {
		return manifest, fmt.Errorf("%w: archive state schema %d, this binary supports up to %d",
			ErrBackupSchemaTooNew, manifest.PersistedVersion, currentSchemaVersion)
	}
	return manifest, nil
}

func extractTarEntryWithHash(r io.Reader, destPath string) (string, error) {
	file, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return "", fmt.Errorf("create %s: %w", filepath.Base(destPath), err)
	}
	hash := sha256.New()
	_, copyErr := io.Copy(io.MultiWriter(file, hash), r)
	closeErr := file.Close()
	if copyErr != nil {
		return "", fmt.Errorf("write %s: %w", filepath.Base(destPath), copyErr)
	}
	if closeErr != nil {
		return "", fmt.Errorf("write %s: %w", filepath.Base(destPath), closeErr)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func writeTarBytes(tw *tar.Writer, name string, modTime time.Time, data []byte) error {
	header := &tar.Header{
		Name:    name,
		Mode:    0o600,
		Size:    int64(len(data)),
		ModTime: modTime,
	}
	if err := tw.WriteHeader(header); err != nil {
		return fmt.Errorf("write %s header: %w", name, err)
	}
	if _, err := tw.Write(data); err != nil {
		return fmt.Errorf("write %s: %w", name, err)
	}
	return nil
}

func writeTarFile(tw *tar.Writer, name string, modTime time.Time, r io.Reader, size int64) error {
	header := &tar.Header{
		Name:    name,
		Mode:    0o600,
		Size:    size,
		ModTime: modTime,
	}
	if err := tw.WriteHeader(header); err != nil {
		return fmt.Errorf("write %s header: %w", name, err)
	}
	if _, err := io.Copy(tw, r); err != nil {
		return fmt.Errorf("write %s: %w", name, err)
	}
	return nil
}
