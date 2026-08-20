package league

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"database/sql"
)

// ---------------------------------------------------------------------
// Boot: open the database, and import a legacy JSON state file once.
// ---------------------------------------------------------------------

// openLocked prepares the store's database and loads the state into
// memory. It runs once, from NewStore, before the Store is published to
// any caller.
//
// An empty file path keeps the pre-database "no persistence" behavior: the
// store runs from memory alone and every persist is a no-op.
//
// The database is the data directory's league.db, a sibling of the legacy
// JSON state file the caller names. When the database does not exist yet
// and a JSON state file does, this call imports it (see importJSONLocked).
func (s *Store) openLocked() error {
	if s.filePath == "" {
		return nil
	}
	dir := filepath.Dir(s.filePath)
	s.dbPath = filepath.Join(dir, dbFileName)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	_, statErr := os.Stat(s.dbPath)
	fresh := errors.Is(statErr, os.ErrNotExist)
	if statErr != nil && !fresh {
		return statErr
	}

	db, err := openDB(s.dbPath)
	if err != nil {
		return err
	}
	s.db = db

	// This check must precede pending SQLite migrations. A database can have
	// a lower PRAGMA user_version while its logical state marker was written
	// by a newer binary; migrating first could overwrite that marker (or
	// repair identity rows) before the downgrade is refused.
	if err := checkLogicalSchemaVersion(db); err != nil {
		return s.abandonLocked(fresh, err)
	}
	if err := migrateDB(db); err != nil {
		return s.abandonLocked(fresh, err)
	}
	authority, err := readPersistenceAuthority(db)
	if err != nil {
		return s.abandonLocked(fresh, err)
	}
	legacyRaw, legacyExists, err := readLegacyStateFile(s.filePath)
	if err != nil {
		return s.abandonLocked(fresh, err)
	}
	if authority != "" {
		if err := s.reconcileMarkedAuthority(authority, legacyRaw, legacyExists); err != nil {
			return s.abandonLocked(fresh, err)
		}
	} else if legacyExists {
		populated, err := databaseHasStateRows(db)
		if err != nil {
			return s.abandonLocked(fresh, err)
		}
		if populated {
			// This is the recovery path for databases produced by the old
			// importer, which committed state before the JSON rename but had
			// no authority marker. Compare every byte-derived field before
			// adopting the database; a divergent file must never be allowed
			// to overwrite or silently coexist with stored state.
			if err := s.recoverUnmarkedImport(legacyRaw); err != nil {
				return s.abandonLocked(fresh, err)
			}
		} else {
			imported, err := s.importJSONLocked()
			if err != nil {
				return s.abandonLocked(fresh, err)
			}
			if imported {
				return nil
			}
		}
	}
	initializeNative := false
	if !legacyExists && authority == "" {
		populated, err := databaseHasStateRows(db)
		if err != nil {
			return s.abandonLocked(fresh, err)
		}
		initializeNative = !populated
	}
	if initializeNative {
		// A brand-new league with nothing to import: write the empty
		// state's scalar rows once, so the file describes itself (its
		// schema version above all) from the moment it exists instead of
		// waiting for the first mutation. The native marker also makes a
		// later, unexpectedly reappearing JSON file fail closed rather than
		// being mistaken for a first import.
		s.state.persistenceAuthority = authorityNative
		for _, id := range allCollections() {
			s.dirty |= 1 << uint(id)
		}
		if err := s.writeDirtyLocked(); err != nil {
			return s.abandonLocked(fresh, err)
		}
	}
	state, err := loadStateFromDB(db)
	if err != nil {
		return s.abandonLocked(fresh, err)
	}
	s.state = state
	if err := s.rebuildShadowLocked(); err != nil {
		return s.abandonLocked(fresh, err)
	}
	return nil
}

const (
	authorityNative       = "sqlite-native-v1"
	authorityLegacyPrefix = "legacy-json-sha256:"
)

// validateAuthorityMarker accepts only markers this binary understands. A
// malformed marker is an operationally ambiguous database, not an invitation
// to fall back to blank state.
func validateAuthorityMarker(raw string) (string, error) {
	if raw == authorityNative {
		return raw, nil
	}
	if !strings.HasPrefix(raw, authorityLegacyPrefix) {
		return "", fmt.Errorf("unknown persistence authority marker %q", raw)
	}
	digest := strings.TrimPrefix(raw, authorityLegacyPrefix)
	if len(digest) != sha256.Size*2 || strings.ToLower(digest) != digest {
		return "", fmt.Errorf("invalid persistence authority marker %q", raw)
	}
	if _, err := hex.DecodeString(digest); err != nil {
		return "", fmt.Errorf("invalid persistence authority marker %q: %w", raw, err)
	}
	return raw, nil
}

func legacyAuthorityMarker(raw []byte) string {
	digest := sha256.Sum256(raw)
	return authorityLegacyPrefix + hex.EncodeToString(digest[:])
}

// readPersistenceAuthority is intentionally read-only. It is called before
// migrations and before identity repair so a logical authority claim can
// never be overwritten while deciding whether a database is safe to load.
func readPersistenceAuthority(db *sql.DB) (string, error) {
	if db == nil {
		return "", errors.New("persistence authority requires an open database")
	}
	var raw string
	err := db.QueryRow(`SELECT "value" FROM kv WHERE "key" = ?`, kvPersistenceAuthority).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil && strings.Contains(err.Error(), "no such table") {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return validateAuthorityMarker(raw)
}

func readLegacyStateFile(path string) ([]byte, bool, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return raw, true, nil
}

// databaseHasStateRows distinguishes a schema-only database left by a crash
// before the import transaction committed from a populated database whose
// authority must be proved before any legacy file is considered.
func databaseHasStateRows(db *sql.DB) (bool, error) {
	for _, table := range []string{
		"kv", "picks", "ready", "members", "co_invites", "invites", "boards",
		"board_owners", "team_names", "draft_order", "scoring", "pickems",
		"pickem_owners", "blitz_entries", "blitz_owners", "autopick", "sent_log",
		"notify_prefs", "notify_pref_owners", "badge_claims", "avatar_refs",
		"announcements", "lineups", "lineup_weeks", "lineup_teams", "transactions",
		"waiver_claims", "trade_offers", "roster_zones", "roster_zone_teams", "schedule",
		"playoffs",
	} {
		var present int
		query := `SELECT 1 FROM "` + table + `" LIMIT 1`
		if table == "kv" {
			// migrate002 stamps the supported logical schema version even
			// before any league state exists; that migration row alone does
			// not make a database authoritative over a legacy JSON file.
			query = `SELECT 1 FROM "kv" WHERE "key" != ? LIMIT 1`
		}
		var err error
		if table == "kv" {
			err = db.QueryRow(query, kvSchemaVersion).Scan(&present)
		} else {
			err = db.QueryRow(query).Scan(&present)
		}
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return false, err
		}
		return present == 1, nil
	}
	return false, nil
}

func (s *Store) reconcileMarkedAuthority(authority string, raw []byte, exists bool) error {
	marker, err := validateAuthorityMarker(authority)
	if err != nil {
		return err
	}
	if marker == authorityNative {
		if exists {
			return fmt.Errorf("persistence authority is native SQLite but legacy state file %s still exists", s.filePath)
		}
		return nil
	}
	if exists && legacyAuthorityMarker(raw) != marker {
		// A completed import keeps the original bytes under .imported. If
		// an operator later drops a different JSON file at the old path,
		// the preserved copy proves it is unrelated stale input; leave it
		// untouched and continue serving the already-authoritative DB.
		kept, keptErr := os.ReadFile(s.filePath + importedSuffix)
		if keptErr == nil && legacyAuthorityMarker(kept) == marker {
			return nil
		}
		return fmt.Errorf("legacy state file %s differs from the committed SQLite import; refusing to choose an authority", s.filePath)
	}
	if !exists {
		return nil
	}
	return s.finishLegacyImportFile(raw)
}

// recoverUnmarkedImport adopts only the old importer state that exactly
// round-trips from the still-present JSON file. The new importer never needs
// this path because its marker is in the same SQLite transaction as state;
// this compatibility recovery closes the historical post-commit/pre-rename
// window without ever overwriting divergent data.
func (s *Store) recoverUnmarkedImport(raw []byte) error {
	var decoded PersistedState
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return fmt.Errorf("recover import %s: %w", s.filePath, err)
	}
	if decoded.SchemaVersion > currentSchemaVersion {
		return fmt.Errorf("recover import %s: file is version %d, this binary supports up to %d",
			s.filePath, decoded.SchemaVersion, currentSchemaVersion)
	}
	decoded.SchemaVersion = currentSchemaVersion
	normalizeState(&decoded)
	stored, err := loadStateFromDBUnrepaired(s.db)
	if err != nil {
		return fmt.Errorf("recover import %s: read back: %w", s.filePath, err)
	}
	if err := compareImportedState(decoded, stored); err != nil {
		return fmt.Errorf("recover import %s: %w", s.filePath, err)
	}
	marker := legacyAuthorityMarker(raw)
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT OR REPLACE INTO kv (key, value) VALUES (?, ?)`, kvPersistenceAuthority, marker); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return s.finishLegacyImportFile(raw)
}

// finishLegacyImportFile makes the post-commit rename restart-safe. It never
// overwrites an existing .imported copy: equal bytes mean a prior rename
// completed and the source can be removed, while different bytes are an
// authority conflict that must stop startup.
func (s *Store) finishLegacyImportFile(raw []byte) error {
	if len(raw) == 0 {
		// Empty is valid input to this helper only when the source has already
		// disappeared; an empty JSON file is rejected by json.Unmarshal in the
		// importer before reaching here.
		return nil
	}
	current, err := os.ReadFile(s.filePath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !bytes.Equal(current, raw) {
		return fmt.Errorf("legacy state file %s changed after the SQLite import; refusing to rename it", s.filePath)
	}
	backupPath := s.filePath + importedSuffix
	kept, keptErr := os.ReadFile(backupPath)
	if keptErr == nil {
		if !bytes.Equal(kept, raw) {
			return fmt.Errorf("existing imported state %s differs from the committed import", backupPath)
		}
		if err := os.Remove(s.filePath); err != nil {
			return err
		}
		return syncImportDirectoryHook(filepath.Dir(s.filePath))
	}
	if !errors.Is(keptErr, os.ErrNotExist) {
		return keptErr
	}
	if err := os.Rename(s.filePath, backupPath); err != nil {
		return err
	}
	return syncImportDirectoryHook(filepath.Dir(s.filePath))
}

func syncImportDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	if err := dir.Sync(); err != nil {
		_ = dir.Close()
		return err
	}
	return dir.Close()
}

// syncImportDirectoryHook is a test seam for the post-rename directory
// durability boundary. Production always uses syncImportDirectory; a test
// can force the error that must retain a committed database for restart.
var syncImportDirectoryHook = syncImportDirectory

// abandonLocked closes the database after a failed boot and, when this
// boot created only an unmarked/half-built file, removes it. Once the
// authority marker is durable, the database is retained even if a later
// rename or directory-sync barrier failed: restart can safely finish from
// the marker instead of stranding the imported state.
// The returned error is the caller's, unchanged.
func (s *Store) abandonLocked(created bool, cause error) error {
	preserveAuthority := false
	if created && s.db != nil {
		// A committed import/native initialization is authoritative even if a
		// later post-commit filesystem barrier reports an error. Removing that
		// database would strand the only copy of the imported state and let the
		// next boot create a blank native database beside only .imported bytes.
		// Leave it in place so restart can reconcile from its marker; only a
		// schema-only/unmarked database is safe to remove here.
		if authority, err := readPersistenceAuthority(s.db); err == nil && authority != "" {
			preserveAuthority = true
		}
	}
	if s.db != nil {
		_ = s.db.Close()
		s.db = nil
	}
	if created && !preserveAuthority && s.dbPath != "" {
		_ = os.Remove(s.dbPath)
		_ = os.Remove(s.dbPath + "-wal")
		_ = os.Remove(s.dbPath + "-shm")
	}
	return cause
}

// importJSONLocked migrates a legacy JSON state file into the fresh
// database, once, and reports whether it did. The steps are, in order:
//
//  1. Read and decode the JSON file. An absent file means "nothing to
//     import" and is not an error. A malformed file is an error, exactly
//     as the JSON engine treated it, so a corrupt file is never silently
//     replaced by blank state.
//  2. Refuse a file whose SchemaVersion exceeds this binary's
//     (errSchemaTooNew), leaving the file untouched.
//  3. Write the whole state, every collection, in one transaction.
//  4. Read the state back out of the database and compare it with the
//     decoded state. Only an exact round trip is accepted.
//  5. Rename the JSON file to <name>.imported. The original bytes survive
//     under the new name; nothing is deleted.
//
// Any failure returns an error, which becomes the Store's StartupError and
// blocks every write. An unmarked partial database is removed by openLocked,
// so the JSON file stays authoritative; once the marker transaction has
// committed, the database is retained so a restart can finish the rename
// without ever creating blank state beside the durable import.
func (s *Store) importJSONLocked() (bool, error) {
	raw, err := os.ReadFile(s.filePath)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var decoded PersistedState
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return false, fmt.Errorf("import %s: %w", s.filePath, err)
	}
	if decoded.SchemaVersion > currentSchemaVersion {
		return false, fmt.Errorf("%w: file is version %d, this binary supports up to %d",
			errSchemaTooNew, decoded.SchemaVersion, currentSchemaVersion)
	}
	// Migrate forward, exactly as the JSON loader did: a missing version
	// decodes as 0 ("version 1"), every field added since is additive with
	// a nil-safe zero value, so the version is stamped current.
	decoded.SchemaVersion = currentSchemaVersion
	normalizeState(&decoded)
	// This marker is emitted by colScalars in the same writeDirtyLocked
	// transaction as every imported collection. A restart can therefore
	// distinguish a committed import from a database created before the
	// import transaction committed.
	decoded.persistenceAuthority = legacyAuthorityMarker(raw)

	s.state = decoded
	s.shadow = shadowIndex{}
	for _, id := range allCollections() {
		s.dirty |= 1 << uint(id)
	}
	if err := s.writeDirtyLocked(); err != nil {
		return false, fmt.Errorf("import %s: %w", s.filePath, err)
	}

	stored, err := loadStateFromDB(s.db)
	if err != nil {
		return false, fmt.Errorf("import %s: read back: %w", s.filePath, err)
	}
	if err := compareImportedState(decoded, stored); err != nil {
		return false, fmt.Errorf("import %s: %w", s.filePath, err)
	}
	if err := s.finishLegacyImportFile(raw); err != nil {
		return false, fmt.Errorf("import %s: %w", s.filePath, err)
	}
	// Fold the import's pages out of the write-ahead log into the
	// database file itself, so an operator who inspects or copies
	// league.db right after the migration sees the whole league in it.
	// A failure here costs nothing: the pages stay in the log, which the
	// next checkpoint (or the last connection's close) folds in.
	_, _ = s.db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`)
	return true, nil
}

// compareImportedState checks that the state read back out of the freshly
// written database matches the state decoded from the JSON file.
//
// The primary check is a deep equality of the two clones. It can report a
// difference for two genuinely equal instants when the source JSON carried
// a numeric zone offset, because each parse builds its own time.Location
// value; the canonical JSON encoding of both states is then the tie
// breaker, and it is the exact fidelity the JSON engine itself
// guaranteed. A difference under both checks fails the import.
func compareImportedState(decoded, stored PersistedState) error {
	want := cloneState(decoded)
	got := cloneState(stored)
	if reflect.DeepEqual(want, got) {
		return nil
	}
	wantJSON, err := json.Marshal(want)
	if err != nil {
		return err
	}
	gotJSON, err := json.Marshal(got)
	if err != nil {
		return err
	}
	if string(wantJSON) == string(gotJSON) {
		return nil
	}
	return fmt.Errorf("the imported state does not match the state file; the file is unchanged\n from file: %s\n from database: %s",
		wantJSON, gotJSON)
}

// backupSnapshotLocked writes a consistent copy of the whole database to
// <database>.bak, replacing the previous copy atomically. It is the
// SQLite-native successor to the JSON engine's rolling .bak file, and the
// draft-mutating paths (MakePick, AutoPick, UndoLastPick) still call it
// best-effort before they mutate.
//
// The mechanism is VACUUM INTO, not a file copy. Under WAL a plain copy of
// the database file alone is not a valid snapshot: the newest committed
// pages may still live in the -wal file, so the copy would silently lose
// them. VACUUM INTO runs inside a read transaction and writes a complete,
// self-contained, already-checkpointed database — the honest snapshot.
// The result lands on a unique temporary name first, then renames over
// the previous backup, so a crash mid-backup can never leave a truncated
// .bak in place of a good one.
func (s *Store) backupSnapshotLocked() error {
	if s.db == nil {
		return nil
	}
	dir := filepath.Dir(s.dbPath)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".league-db-bak-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if err := tmp.Close(); err != nil {
		return err
	}
	// VACUUM INTO refuses an existing target; the temp file only reserved
	// the name.
	if err := os.Remove(tmpPath); err != nil {
		return err
	}
	defer os.Remove(tmpPath)
	if _, err := s.db.Exec(`VACUUM INTO ?`, tmpPath); err != nil {
		return err
	}
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		return err
	}
	return os.Rename(tmpPath, s.dbPath+".bak")
}

// openBackup opens the rolling backup written by backupSnapshotLocked and
// returns the state it holds. Tests and recovery tooling read a backup
// through this call rather than by decoding a file by hand.
func openBackup(dbPath string) (PersistedState, error) {
	path := dbPath + ".bak"
	if _, err := os.Stat(path); err != nil {
		return PersistedState{}, err
	}
	db, err := openDB(path)
	if err != nil {
		return PersistedState{}, err
	}
	defer db.Close()
	return loadStateFromDB(db)
}

// readStateFromFile decodes a legacy JSON state file. It backs the import
// path's verification and the tests that assert what an imported file
// held.
func readStateFromFile(path string) (PersistedState, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return PersistedState{}, err
	}
	var state PersistedState
	if err := json.Unmarshal(raw, &state); err != nil {
		return PersistedState{}, err
	}
	normalizeState(&state)
	return state, nil
}

// dbUserVersion reports a database file's PRAGMA user_version. It backs
// the old-binary-safety test, which bumps the version to prove an older
// binary refuses a newer schema.
func dbUserVersion(db *sql.DB) (int, error) {
	var version int
	err := db.QueryRow("PRAGMA user_version").Scan(&version)
	return version, err
}
