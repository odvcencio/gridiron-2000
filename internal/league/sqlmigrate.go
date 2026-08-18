package league

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"

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

	if err := migrateDB(db); err != nil {
		return s.abandonLocked(fresh, err)
	}
	if fresh {
		imported, err := s.importJSONLocked()
		if err != nil {
			return s.abandonLocked(fresh, err)
		}
		if imported {
			return nil
		}
		// A brand-new league with nothing to import: write the empty
		// state's scalar rows once, so the file describes itself (its
		// schema version above all) from the moment it exists instead of
		// waiting for the first mutation.
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

// abandonLocked closes the database after a failed boot and, when this
// boot created the file, removes it. A half-built database must never
// survive to be mistaken for the authoritative state on the next boot,
// which would strand the JSON state file that is still the real record.
// The returned error is the caller's, unchanged.
func (s *Store) abandonLocked(created bool, cause error) error {
	if s.db != nil {
		_ = s.db.Close()
		s.db = nil
	}
	if created && s.dbPath != "" {
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
// blocks every write. The caller (openLocked) then removes the partial
// database, so the JSON file stays authoritative and the operator can fix
// the cause and boot again.
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
	if err := os.Rename(s.filePath, s.filePath+importedSuffix); err != nil {
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
