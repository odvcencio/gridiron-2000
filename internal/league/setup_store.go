// Setup-wizard persistence: the setup_state completion marker (setup-wizard
// design section 3.2). It is deliberately independent of PersistedState's
// collection/shadow-diff system (sqlstore.go): it is a one-row, write-once
// table read at boot, before any Service exists, and written exactly once,
// inside the wizard's atomic commit transaction.
package league

import (
	"database/sql"
	"fmt"
	"time"
)

// SetupCompletion is the setup_state table's one row (design section 3.2).
type SetupCompletion struct {
	CompletedAt  time.Time
	CompletedBy  string
	ConfigSHA256 string
	AppVersion   string
}

// SetupCompletion reports the durable setup_state marker, if one has been
// written. found is false on a database that has never completed setup.
func (s *Store) SetupCompletion() (completion SetupCompletion, found bool, err error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.db == nil {
		return SetupCompletion{}, false, nil
	}
	var completedAt, completedBy, configSHA, appVersion string
	row := s.db.QueryRow(`SELECT completed_at, completed_by, config_sha256, app_version FROM setup_state WHERE id = 1`)
	switch err := row.Scan(&completedAt, &completedBy, &configSHA, &appVersion); {
	case err == sql.ErrNoRows:
		return SetupCompletion{}, false, nil
	case err != nil:
		return SetupCompletion{}, false, fmt.Errorf("%w: read setup_state: %w", ErrInternal, err)
	}
	parsed, _ := time.Parse(time.RFC3339, completedAt)
	return SetupCompletion{
		CompletedAt:  parsed,
		CompletedBy:  completedBy,
		ConfigSHA256: configSHA,
		AppVersion:   appVersion,
	}, true, nil
}

// MarkSetupComplete writes the one-row setup_state marker (design section
// 4.5, commit step 5: after the league.json rename, the actual state flip).
// It is called at most once in the life of a database; a second call
// overwrites the single id=1 row, which only a deliberate re-run of setup
// against an already-completed database would ever do (not a path the
// wizard's own router exposes — CONFIGURED instances never mount /setup).
func (s *Store) MarkSetupComplete(completion SetupCompletion) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeErrorLocked(); err != nil {
		return err
	}
	if s.db == nil {
		return fmt.Errorf("%w: no database open", ErrInternal)
	}
	_, err := s.db.Exec(`INSERT OR REPLACE INTO setup_state (id, completed_at, completed_by, config_sha256, app_version) VALUES (1, ?, ?, ?, ?)`,
		completion.CompletedAt.UTC().Format(time.RFC3339), completion.CompletedBy, completion.ConfigSHA256, completion.AppVersion)
	if err != nil {
		return fmt.Errorf("%w: write setup_state: %w", ErrInternal, err)
	}
	return nil
}
