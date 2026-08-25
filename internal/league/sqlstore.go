package league

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite driver; no CGo (dependency-purity line)
)

// ---------------------------------------------------------------------
// The SQLite engine behind Store.
//
// The authoritative league state lives in one SQLite database file per
// league (data/league.db). The in-memory PersistedState and the RWMutex
// discipline are unchanged: SQLite replaces persistLocked's whole-file
// JSON write, not the concurrency model.
//
// Three rules drive the layout below.
//
//  1. Real tables, one row per record. Every collection maps to a table
//     whose primary key is the record's own identity (a pick number, a
//     member email, a team/week/slot triple). Only genuinely nested
//     values serialize as a JSON column inside their own row — a
//     transaction's player list, a schedule week's matchups, the playoff
//     bracket. Nothing stores the whole state as one blob.
//  2. Incremental writes. A mutator declares which collections it
//     touched (see persistLocked's cols argument). persistLocked emits
//     only those collections' rows, diffs them against the shadow index
//     of what the database already holds, and writes only the rows that
//     actually changed, plus deletes for rows that disappeared. One
//     transaction covers the whole persist.
//  3. Durability first. WAL journal, synchronous=FULL, foreign_keys on,
//     busy_timeout set. A commit is fsynced; a crash mid-write rolls
//     back to the last commit. There is no whole-file rename race and no
//     partial-write window.
// ---------------------------------------------------------------------

// dbFileName is the league database's name inside the data directory. The
// Store's file path names the legacy JSON state file; the database is its
// sibling, so one data directory holds exactly one league database.
const dbFileName = "league.db"

// importedSuffix is appended to the legacy JSON state file after a
// verified import. The original bytes survive under the new name; nothing
// is ever deleted by the migration.
const importedSuffix = ".imported"

// currentDBVersion is the SQLite schema generation this binary knows,
// recorded in PRAGMA user_version. It equals len(dbMigrations). A database
// whose user_version exceeds it refuses to open (errSchemaTooNew): a newer
// binary's tables must not be silently half-read by an older one.
var currentDBVersion = len(dbMigrations)

// dbMigrations is the ordered schema migration list. Index i holds the
// step that lifts user_version from i to i+1. Steps are append-only:
// never edit a shipped step, always add a new one.
var dbMigrations = []func(*sql.Tx) error{
	migrate001Initial,
	migrate002AvatarRefs,
	migrate003DraftLifecycle,
	migrate004PickemMarkets,
	migrate005PickemEnteredAt,
	migrate006WaiverReceipts,
}

// sqlitePersistVerify turns on the read-back check inside persistLocked:
// after every successful commit the whole state is re-read from the
// database and compared with the in-memory model. It is off in production
// and on for the package's own tests (see TestMain in sqlstore_test.go),
// where it proves that every mutator declares every collection it touched.
var sqlitePersistVerify bool

// openDB opens (creating when absent) the league database at path and
// applies the pragmas the durability bar requires.
//
//	journal_mode=WAL   readers never block the writer; a commit appends.
//	synchronous=FULL   every commit fsyncs before it reports success.
//	                   NORMAL would leave a committed transaction in the
//	                   OS page cache; that is the exact durability hole
//	                   this migration closes, so the cost is accepted.
//	foreign_keys=ON    referential integrity is enforced, not advisory.
//	busy_timeout=5000  a second writer waits instead of failing at once.
//	_txlock=immediate  a write transaction takes its lock up front, so two
//	                   writers can never deadlock on a lock upgrade.
//
// The pool is capped at one connection: the Store already serializes every
// access behind its RWMutex, so a second connection would only add lock
// contention inside SQLite.
func openDB(path string) (*sql.DB, error) {
	dsn := "file:" + path + "?" + strings.Join([]string{
		"_pragma=journal_mode(WAL)",
		"_pragma=synchronous(FULL)",
		"_pragma=foreign_keys(1)",
		"_pragma=busy_timeout(5000)",
		"_txlock=immediate",
	}, "&")
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

// migrateDB applies every pending step from dbMigrations and stamps
// PRAGMA user_version. A database from a newer binary (user_version above
// currentDBVersion) is refused with errSchemaTooNew, the same rule the
// JSON state file's SchemaVersion already carried.
func migrateDB(db *sql.DB) error {
	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version > currentDBVersion {
		return fmt.Errorf("%w: database is user_version %d, this binary supports up to %d",
			errSchemaTooNew, version, currentDBVersion)
	}
	for step := version; step < currentDBVersion; step++ {
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		if err := dbMigrations[step](tx); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("schema migration %d: %w", step+1, err)
		}
		// PRAGMA user_version takes no bound parameter; step+1 is an int
		// from this binary's own migration list, never external input.
		if _, err := tx.Exec("PRAGMA user_version = " + strconv.Itoa(step+1)); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// migrate001Initial creates the whole table set. Column choices per table
// are documented at collectionSpecs.
func migrate001Initial(tx *sql.Tx) error {
	stmts := []string{
		`CREATE TABLE kv (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
		`CREATE TABLE picks (number INTEGER PRIMARY KEY, round INTEGER NOT NULL, team_id TEXT NOT NULL,
			player_id TEXT NOT NULL, made_at TEXT NOT NULL, made_by TEXT NOT NULL)`,
		`CREATE TABLE ready (team_id TEXT PRIMARY KEY, ready INTEGER NOT NULL)`,
		`CREATE TABLE members (key TEXT PRIMARY KEY, team_id TEXT NOT NULL, name TEXT NOT NULL,
			email TEXT NOT NULL, role TEXT NOT NULL)`,
		`CREATE TABLE co_invites (email TEXT PRIMARY KEY, team_id TEXT NOT NULL)`,
		`CREATE TABLE invites (ord INTEGER PRIMARY KEY, email TEXT NOT NULL)`,
		`CREATE TABLE boards (owner TEXT NOT NULL, ord INTEGER NOT NULL, player_id TEXT NOT NULL,
			PRIMARY KEY (owner, ord))`,
		`CREATE TABLE board_owners (owner TEXT PRIMARY KEY)`,
		`CREATE TABLE team_names (team_id TEXT PRIMARY KEY, name TEXT NOT NULL)`,
		`CREATE TABLE draft_order (ord INTEGER PRIMARY KEY, team_id TEXT NOT NULL)`,
		`CREATE TABLE scoring (key TEXT PRIMARY KEY, points REAL NOT NULL)`,
		`CREATE TABLE pickems (owner TEXT NOT NULL, game_id TEXT NOT NULL, team TEXT NOT NULL,
			PRIMARY KEY (owner, game_id))`,
		`CREATE TABLE pickem_owners (owner TEXT PRIMARY KEY)`,
		`CREATE TABLE blitz_entries (owner TEXT NOT NULL, slate TEXT NOT NULL, players TEXT NOT NULL,
			updated_at TEXT NOT NULL, PRIMARY KEY (owner, slate))`,
		`CREATE TABLE blitz_owners (owner TEXT PRIMARY KEY)`,
		`CREATE TABLE autopick (team_id TEXT PRIMARY KEY, enabled INTEGER NOT NULL)`,
		`CREATE TABLE sent_log (key TEXT PRIMARY KEY, sent_at TEXT NOT NULL)`,
		`CREATE TABLE notify_prefs (email TEXT NOT NULL, category TEXT NOT NULL, enabled INTEGER NOT NULL,
			PRIMARY KEY (email, category))`,
		`CREATE TABLE notify_pref_owners (email TEXT PRIMARY KEY)`,
		`CREATE TABLE badge_claims (team_id TEXT PRIMARY KEY, motif TEXT NOT NULL)`,
		`CREATE TABLE announcements (ord INTEGER PRIMARY KEY, id TEXT NOT NULL, body TEXT NOT NULL,
			posted_at TEXT NOT NULL, posted_by TEXT NOT NULL)`,
		`CREATE TABLE lineups (team_id TEXT NOT NULL, week INTEGER NOT NULL, slot TEXT NOT NULL,
			player_id TEXT NOT NULL, PRIMARY KEY (team_id, week, slot))`,
		`CREATE TABLE lineup_weeks (team_id TEXT NOT NULL, week INTEGER NOT NULL, PRIMARY KEY (team_id, week))`,
		`CREATE TABLE lineup_teams (team_id TEXT PRIMARY KEY)`,
		`CREATE TABLE transactions (ord INTEGER PRIMARY KEY, id TEXT NOT NULL, season INTEGER NOT NULL,
			week INTEGER NOT NULL, type TEXT NOT NULL, team_id TEXT NOT NULL, other_team_id TEXT NOT NULL,
			adds TEXT NOT NULL, drops TEXT NOT NULL, bid INTEGER NOT NULL, position INTEGER NOT NULL,
			offer_id TEXT NOT NULL, by TEXT NOT NULL, note TEXT NOT NULL, at TEXT NOT NULL)`,
		`CREATE TABLE waiver_claims (ord INTEGER PRIMARY KEY, id TEXT NOT NULL, team_id TEXT NOT NULL,
			add_id TEXT NOT NULL, drop_id TEXT NOT NULL, bid INTEGER NOT NULL, priority INTEGER NOT NULL,
			filed_at TEXT NOT NULL)`,
		`CREATE TABLE trade_offers (ord INTEGER PRIMARY KEY, id TEXT NOT NULL, from_team_id TEXT NOT NULL,
			to_team_id TEXT NOT NULL, give TEXT NOT NULL, get TEXT NOT NULL, picks TEXT NOT NULL,
			note TEXT NOT NULL, status TEXT NOT NULL, parent_id TEXT NOT NULL, vetoes TEXT NOT NULL,
			fail_reason TEXT NOT NULL, created_at TEXT NOT NULL, accepted_at TEXT NOT NULL,
			resolved_at TEXT NOT NULL)`,
		`CREATE TABLE roster_zones (team_id TEXT NOT NULL, player_id TEXT NOT NULL, zone TEXT NOT NULL,
			position TEXT NOT NULL, placed_at TEXT NOT NULL, PRIMARY KEY (team_id, player_id))`,
		`CREATE TABLE roster_zone_teams (team_id TEXT PRIMARY KEY)`,
		`CREATE TABLE schedule (ord INTEGER PRIMARY KEY, week INTEGER NOT NULL, data TEXT NOT NULL)`,
		`CREATE TABLE playoffs (id INTEGER PRIMARY KEY, data TEXT NOT NULL)`,
	}
	for _, stmt := range stmts {
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("%s: %w", firstLine(stmt), err)
		}
	}
	return nil
}

// migrate002AvatarRefs adds the authoritative custom-avatar identity table.
// The image bytes live in immutable content-addressed files; this table is
// the only durable mapping from a team to one such blob. Existing databases
// upgrade in place, while a pre-commit crash leaves at most an unreferenced
// blob that the serving path cannot discover.
func migrate002AvatarRefs(tx *sql.Tx) error {
	if _, err := tx.Exec(`CREATE TABLE avatar_refs (team_id TEXT PRIMARY KEY, ref TEXT NOT NULL CHECK (length(ref) = 64 AND ref NOT GLOB '*[^0-9a-f]*'))`); err != nil {
		return fmt.Errorf("CREATE TABLE avatar_refs: %w", err)
	}
	// This migration lifts the logical PersistedState schema in lockstep with
	// SQLite's user_version. Keeping both markers current makes backups and
	// direct database inspection tell the same upgrade story.
	if _, err := tx.Exec(`INSERT OR REPLACE INTO kv (key, value) VALUES ('schema_version', '3')`); err != nil {
		return fmt.Errorf("stamp schema_version 3: %w", err)
	}
	return nil
}

func migrate003DraftLifecycle(tx *sql.Tx) error {
	var pickCount int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM picks`).Scan(&pickCount); err != nil {
		return fmt.Errorf("count legacy picks: %w", err)
	}
	if pickCount > 0 {
		startedAt := ""
		_ = tx.QueryRow(`SELECT made_at FROM picks WHERE made_at <> '' ORDER BY number LIMIT 1`).Scan(&startedAt)
		for key, value := range map[string]string{"draft_started": "1", "draft_started_at": startedAt} {
			if _, err := tx.Exec(`INSERT OR REPLACE INTO kv (key, value) VALUES (?, ?)`, key, value); err != nil {
				return fmt.Errorf("persist %s: %w", key, err)
			}
		}
	}
	if _, err := tx.Exec(`INSERT OR REPLACE INTO kv (key, value) VALUES ('schema_version', '4')`); err != nil {
		return fmt.Errorf("stamp schema_version 4: %w", err)
	}
	return nil
}

func migrate004PickemMarkets(tx *sql.Tx) error {
	if _, err := tx.Exec(`CREATE TABLE pickem_markets (game_id TEXT PRIMARY KEY, data TEXT NOT NULL)`); err != nil {
		return fmt.Errorf("CREATE TABLE pickem_markets: %w", err)
	}
	if _, err := tx.Exec(`INSERT OR REPLACE INTO kv (key, value) VALUES ('schema_version', '5')`); err != nil {
		return fmt.Errorf("stamp schema_version 5: %w", err)
	}
	return nil
}

func migrate005PickemEnteredAt(tx *sql.Tx) error {
	if _, err := tx.Exec(`CREATE TABLE pickem_entries (owner TEXT PRIMARY KEY, entered_at TEXT NOT NULL)`); err != nil {
		return fmt.Errorf("CREATE TABLE pickem_entries: %w", err)
	}
	if _, err := tx.Exec(`INSERT OR REPLACE INTO kv (key, value) VALUES ('schema_version', '6')`); err != nil {
		return fmt.Errorf("stamp schema_version 6: %w", err)
	}
	return nil
}

func migrate006WaiverReceipts(tx *sql.Tx) error {
	// Older stores allowed priority gaps after cancel and could accumulate
	// duplicate priorities after a later filing. Canonicalize once, before the
	// move controls make this field manager-visible and authoritative.
	if _, err := tx.Exec(`WITH ranked AS (
		SELECT ord, ROW_NUMBER() OVER (
			PARTITION BY team_id
			ORDER BY CASE WHEN priority > 0 THEN priority ELSE 2147483647 END, filed_at, id, ord
		) AS normalized_priority FROM waiver_claims
	)
	UPDATE waiver_claims SET priority = (
		SELECT normalized_priority FROM ranked WHERE ranked.ord = waiver_claims.ord
	)`); err != nil {
		return fmt.Errorf("normalize waiver claim priorities: %w", err)
	}
	if _, err := tx.Exec(`CREATE TABLE waiver_receipts (ord INTEGER PRIMARY KEY, claim_id TEXT NOT NULL,
		season INTEGER NOT NULL, week INTEGER NOT NULL, team_id TEXT NOT NULL, add_player TEXT NOT NULL,
		drops TEXT NOT NULL, bid INTEGER NOT NULL, submitted_priority INTEGER NOT NULL,
		waiver_position INTEGER NOT NULL, waiver_team_count INTEGER NOT NULL, mode TEXT NOT NULL,
		outcome TEXT NOT NULL, winning_team_id TEXT NOT NULL, winning_bid INTEGER NOT NULL,
		winning_bid_known INTEGER NOT NULL, reason TEXT NOT NULL,
		filed_at TEXT NOT NULL, resolved_at TEXT NOT NULL)`); err != nil {
		return fmt.Errorf("CREATE TABLE waiver_receipts: %w", err)
	}
	if _, err := tx.Exec(`INSERT OR REPLACE INTO kv (key, value) VALUES ('schema_version', '7')`); err != nil {
		return fmt.Errorf("stamp schema_version 7: %w", err)
	}
	return nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '('); i > 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}

// ---------------------------------------------------------------------
// Collections: the dirty-tracking unit
// ---------------------------------------------------------------------

// collectionID names one dirty-tracking unit. A mutator passes the
// collections it touched to persistLocked, and only those collections'
// rows are emitted, diffed, and written.
type collectionID int

const (
	colScalars collectionID = iota // the kv table: clock, phase, roster override, schedule header
	colPicks
	colReady
	colMembers
	colCoInvites
	colInvites
	colBoards
	colTeamNames
	colDraftOrder
	colScoring
	colPickems
	colPickemMarkets
	colBlitzEntries
	colAutopick
	colSentLog
	colNotifyPrefs
	colBadgeClaims
	colAvatarRefs
	colAnnouncements
	colLineups
	colTransactions
	colWaiverClaims
	colWaiverReceipts
	colTradeOffers
	colRosterZones
	colSchedule
	colPlayoffs
	collectionCount
)

// allCollections is every collection, for a full write (import, or the
// first write into a fresh database).
func allCollections() []collectionID {
	out := make([]collectionID, 0, collectionCount)
	for id := collectionID(0); id < collectionCount; id++ {
		out = append(out, id)
	}
	return out
}

// tableDef describes one physical table: its primary-key columns and its
// value columns, in the order emit produces them.
type tableDef struct {
	name    string
	keyCols []string
	valCols []string
}

// collectionSpec binds a collectionID to the tables it owns and the
// function that emits its rows from a state snapshot.
//
// Granularity, per collection (the "row-granular is the bar" choice):
//
//	scalars        one kv row per scalar field. A clock arm rewrites one row.
//	picks          one row per pick, fully column-normalized. A pick writes
//	               one row; an undo deletes one row.
//	ready          one row per team.
//	members        one row per member email.
//	co_invites     one row per pending co-manager email.
//	invites        one row per list position (ord). Removing from the middle
//	               rewrites the tail; the list is one entry per manager.
//	boards         one row per (owner, position). A reorder rewrites only
//	               the moved range. board_owners records an owner whose
//	               board is present but empty.
//	team_names     one row per team.
//	draft_order    one row per position; the order is redrawn as a whole.
//	scoring        one row per rule key.
//	pickems        one row per (owner, game), plus one immutable first-entry
//	               row per participating owner. pickem_owners as for boards.
//	blitz_entries  one row per (owner, slate); the 5-player slate is a JSON
//	               column, since the entry is always written whole.
//	autopick       one row per team.
//	sent_log       one row per idempotency key. A send writes one row.
//	notify_prefs   one row per (email, category).
//	badge_claims   one row per team.
//	announcements  one row per feed position. A post prepends, so the feed
//	               (capped at 20) rewrites; the cap bounds the cost.
//	lineups        one row per (team, week, slot). One slot change writes
//	               one row. lineup_teams/lineup_weeks record present but
//	               empty containers.
//	transactions   one row per log entry, append-only, so an append writes
//	               exactly one row. The add/drop player lists are JSON
//	               columns inside that row.
//	waiver_claims  one row per open claim.
//	waiver_receipts one row per resolved claim, season-scoped and append-only.
//	trade_offers   one row per offer; a status change rewrites that one
//	               row. Give/Get/Picks/Vetoes are JSON columns.
//	roster_zones   one row per (team, player). roster_zone_teams as above.
//	schedule       one row per week, the week's matchups a JSON column, so
//	               a week close writes one row out of eighteen. The
//	               schedule header (season, seed, start week, generated at)
//	               lives in the kv table.
//	playoffs       one row, the bracket a JSON column: its only mutator
//	               (SetPlayoffs) replaces the bracket whole.
type collectionSpec struct {
	tables []tableDef
	emit   func(*PersistedState, *rowSink)
}

// dbRow is one emitted row: primary-key values then value-column values,
// matching its tableDef's column order.
type dbRow struct {
	key  []any
	vals []any
}

// rowSink collects emitted rows per table. err holds the first encoding
// failure (only a JSON column can fail), which aborts the persist rather
// than writing a truncated value.
type rowSink struct {
	rows map[string][]dbRow
	err  error
}

func newRowSink() *rowSink {
	return &rowSink{rows: map[string][]dbRow{}}
}

func (s *rowSink) add(table string, key []any, vals ...any) {
	s.rows[table] = append(s.rows[table], dbRow{key: key, vals: vals})
}

// jsonValue encodes a nested value for a JSON column. A failure is
// recorded and surfaces as a persist error; it never writes a partial
// value.
func (s *rowSink) jsonValue(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		if s.err == nil {
			s.err = err
		}
		return "null"
	}
	return string(raw)
}

// encodeTime renders an instant for a TEXT column. A zero instant stores
// as the empty string so it decodes back to exactly time.Time{}.
func encodeTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339Nano)
}

// decodeTime is encodeTime's inverse.
func decodeTime(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	return time.Parse(time.RFC3339Nano, s)
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// kv keys for the scalars collection.
const (
	kvSchemaVersion           = "schema_version"
	kvPersistenceAuthority    = "persistence_authority"
	kvClockDeadline           = "clock_deadline"
	kvClockPaused             = "clock_paused"
	kvClockRemainingSec       = "clock_remaining_sec"
	kvClockDurationSec        = "clock_duration_sec"
	kvDraftStarted            = "draft_started"
	kvDraftStartedAt          = "draft_started_at"
	kvDraftAtOverride         = "draft_at_override"
	kvScoringChangedAt        = "scoring_changed_at"
	kvPhase                   = "phase"
	kvWaiversProcessedThrough = "waivers_processed_through"
	kvRosterOverride          = "roster_override"
	kvScheduleHeader          = "schedule_header"
	kvTrimmedTeamIDs          = "trimmed_team_ids"
	kvSeatRevisions           = "seat_revisions"
)

// scheduleHeader is the SeasonSchedule minus its weeks: the part that has
// no natural row of its own. It lives in one kv row so the schedule table
// stays one row per week.
type scheduleHeader struct {
	Season      int       `json:"season"`
	Seed        int64     `json:"seed"`
	GeneratedAt time.Time `json:"generatedAt"`
	StartWeek   int       `json:"startWeek"`
}

// collectionSpecs is the whole persistence map, indexed by collectionID.
var collectionSpecs = [collectionCount]collectionSpec{
	colScalars: {
		tables: []tableDef{{name: "kv", keyCols: []string{"key"}, valCols: []string{"value"}}},
		emit: func(st *PersistedState, sink *rowSink) {
			put := func(key, value string) { sink.add("kv", []any{key}, value) }
			put(kvSchemaVersion, strconv.Itoa(st.SchemaVersion))
			if st.persistenceAuthority != "" {
				put(kvPersistenceAuthority, st.persistenceAuthority)
			}
			put(kvClockDeadline, encodeTime(st.ClockDeadline))
			put(kvClockPaused, strconv.Itoa(boolToInt(st.ClockPaused)))
			put(kvClockRemainingSec, strconv.Itoa(st.ClockRemainingSec))
			put(kvClockDurationSec, strconv.Itoa(st.ClockDurationSec))
			put(kvDraftStarted, strconv.Itoa(boolToInt(st.DraftStarted)))
			put(kvDraftStartedAt, encodeTime(st.DraftStartedAt))
			put(kvDraftAtOverride, encodeTime(st.DraftAtOverride))
			put(kvScoringChangedAt, encodeTime(st.ScoringChangedAt))
			put(kvPhase, st.Phase)
			put(kvWaiversProcessedThrough, encodeTime(st.WaiversProcessedThrough))
			// A nil override and a nil schedule write no row at all, so
			// "absent" and "present but empty" stay distinguishable.
			if st.RosterOverride != nil {
				put(kvRosterOverride, sink.jsonValue(st.RosterOverride))
			}
			if st.Schedule != nil {
				put(kvScheduleHeader, sink.jsonValue(scheduleHeader{
					Season:      st.Schedule.Season,
					Seed:        st.Schedule.Seed,
					GeneratedAt: st.Schedule.GeneratedAt,
					StartWeek:   st.Schedule.StartWeek,
				}))
			}
			// An empty TrimmedTeamIDs writes no row, matching the override/
			// schedule "absent" convention above: no trim has run.
			if len(st.TrimmedTeamIDs) > 0 {
				put(kvTrimmedTeamIDs, sink.jsonValue(st.TrimmedTeamIDs))
			}
			if len(st.SeatRevisions) > 0 {
				put(kvSeatRevisions, sink.jsonValue(st.SeatRevisions))
			}
		},
	},
	colPicks: {
		tables: []tableDef{{
			name:    "picks",
			keyCols: []string{"number"},
			valCols: []string{"round", "team_id", "player_id", "made_at", "made_by"},
		}},
		emit: func(st *PersistedState, sink *rowSink) {
			for _, p := range st.Picks {
				sink.add("picks", []any{p.Number}, p.Round, p.TeamID, p.PlayerID, encodeTime(p.MadeAt), p.MadeBy)
			}
		},
	},
	colReady: {
		tables: []tableDef{{name: "ready", keyCols: []string{"team_id"}, valCols: []string{"ready"}}},
		emit: func(st *PersistedState, sink *rowSink) {
			for teamID, ready := range st.Ready {
				sink.add("ready", []any{teamID}, boolToInt(ready))
			}
		},
	},
	colMembers: {
		tables: []tableDef{{
			name:    "members",
			keyCols: []string{"key"},
			valCols: []string{"team_id", "name", "email", "role"},
		}},
		emit: func(st *PersistedState, sink *rowSink) {
			for key, m := range st.Members {
				sink.add("members", []any{key}, m.TeamID, m.Name, m.Email, m.Role)
			}
		},
	},
	colCoInvites: {
		tables: []tableDef{{name: "co_invites", keyCols: []string{"email"}, valCols: []string{"team_id"}}},
		emit: func(st *PersistedState, sink *rowSink) {
			for email, teamID := range st.CoInvites {
				sink.add("co_invites", []any{email}, teamID)
			}
		},
	},
	colInvites: {
		tables: []tableDef{{name: "invites", keyCols: []string{"ord"}, valCols: []string{"email"}}},
		emit: func(st *PersistedState, sink *rowSink) {
			for i, email := range st.Invites {
				sink.add("invites", []any{i}, email)
			}
		},
	},
	colBoards: {
		tables: []tableDef{
			{name: "boards", keyCols: []string{"owner", "ord"}, valCols: []string{"player_id"}},
			{name: "board_owners", keyCols: []string{"owner"}},
		},
		emit: func(st *PersistedState, sink *rowSink) {
			for owner, board := range st.Boards {
				sink.add("board_owners", []any{owner})
				for i, playerID := range board {
					sink.add("boards", []any{owner, i}, playerID)
				}
			}
		},
	},
	colTeamNames: {
		tables: []tableDef{{name: "team_names", keyCols: []string{"team_id"}, valCols: []string{"name"}}},
		emit: func(st *PersistedState, sink *rowSink) {
			for teamID, name := range st.TeamNames {
				sink.add("team_names", []any{teamID}, name)
			}
		},
	},
	colDraftOrder: {
		tables: []tableDef{{name: "draft_order", keyCols: []string{"ord"}, valCols: []string{"team_id"}}},
		emit: func(st *PersistedState, sink *rowSink) {
			for i, teamID := range st.DraftOrder {
				sink.add("draft_order", []any{i}, teamID)
			}
		},
	},
	colScoring: {
		tables: []tableDef{{name: "scoring", keyCols: []string{"key"}, valCols: []string{"points"}}},
		emit: func(st *PersistedState, sink *rowSink) {
			for key, points := range st.Scoring {
				sink.add("scoring", []any{key}, points)
			}
		},
	},
	colPickems: {
		tables: []tableDef{
			{name: "pickems", keyCols: []string{"owner", "game_id"}, valCols: []string{"team"}},
			{name: "pickem_owners", keyCols: []string{"owner"}},
			{name: "pickem_entries", keyCols: []string{"owner"}, valCols: []string{"entered_at"}},
		},
		emit: func(st *PersistedState, sink *rowSink) {
			for owner, picks := range st.Pickems {
				sink.add("pickem_owners", []any{owner})
				for gameID, team := range picks {
					sink.add("pickems", []any{owner, gameID}, team)
				}
			}
			for owner, enteredAt := range st.PickemEnteredAt {
				if !enteredAt.IsZero() {
					sink.add("pickem_entries", []any{owner}, encodeTime(enteredAt))
				}
			}
		},
	},
	colPickemMarkets: {
		tables: []tableDef{{name: "pickem_markets", keyCols: []string{"game_id"}, valCols: []string{"data"}}},
		emit: func(st *PersistedState, sink *rowSink) {
			for gameID, market := range st.PickemMarkets {
				sink.add("pickem_markets", []any{gameID}, sink.jsonValue(market))
			}
		},
	},
	colBlitzEntries: {
		tables: []tableDef{
			{name: "blitz_entries", keyCols: []string{"owner", "slate"}, valCols: []string{"players", "updated_at"}},
			{name: "blitz_owners", keyCols: []string{"owner"}},
		},
		emit: func(st *PersistedState, sink *rowSink) {
			for owner, bySlate := range st.BlitzEntries {
				sink.add("blitz_owners", []any{owner})
				for slate, entry := range bySlate {
					players := entry.Players
					if players == nil {
						players = []string{}
					}
					sink.add("blitz_entries", []any{owner, slate}, sink.jsonValue(players), encodeTime(entry.UpdatedAt))
				}
			}
		},
	},
	colAutopick: {
		tables: []tableDef{{name: "autopick", keyCols: []string{"team_id"}, valCols: []string{"enabled"}}},
		emit: func(st *PersistedState, sink *rowSink) {
			for teamID, on := range st.Autopick {
				sink.add("autopick", []any{teamID}, boolToInt(on))
			}
		},
	},
	colSentLog: {
		tables: []tableDef{{name: "sent_log", keyCols: []string{"key"}, valCols: []string{"sent_at"}}},
		emit: func(st *PersistedState, sink *rowSink) {
			for key, sentAt := range st.SentLog {
				sink.add("sent_log", []any{key}, encodeTime(sentAt))
			}
		},
	},
	colNotifyPrefs: {
		tables: []tableDef{
			{name: "notify_prefs", keyCols: []string{"email", "category"}, valCols: []string{"enabled"}},
			{name: "notify_pref_owners", keyCols: []string{"email"}},
		},
		emit: func(st *PersistedState, sink *rowSink) {
			for email, prefs := range st.NotifyPrefs {
				sink.add("notify_pref_owners", []any{email})
				for category, enabled := range prefs {
					sink.add("notify_prefs", []any{email, category}, boolToInt(enabled))
				}
			}
		},
	},
	colBadgeClaims: {
		tables: []tableDef{{name: "badge_claims", keyCols: []string{"team_id"}, valCols: []string{"motif"}}},
		emit: func(st *PersistedState, sink *rowSink) {
			for teamID, motif := range st.BadgeClaims {
				sink.add("badge_claims", []any{teamID}, motif)
			}
		},
	},
	colAvatarRefs: {
		tables: []tableDef{{name: "avatar_refs", keyCols: []string{"team_id"}, valCols: []string{"ref"}}},
		emit: func(st *PersistedState, sink *rowSink) {
			for teamID, ref := range st.AvatarRefs {
				sink.add("avatar_refs", []any{teamID}, ref)
			}
		},
	},
	colAnnouncements: {
		tables: []tableDef{{
			name:    "announcements",
			keyCols: []string{"ord"},
			valCols: []string{"id", "body", "posted_at", "posted_by"},
		}},
		emit: func(st *PersistedState, sink *rowSink) {
			for i, a := range st.Announcements {
				sink.add("announcements", []any{i}, a.ID, a.Body, encodeTime(a.PostedAt), a.PostedBy)
			}
		},
	},
	colLineups: {
		tables: []tableDef{
			{name: "lineups", keyCols: []string{"team_id", "week", "slot"}, valCols: []string{"player_id"}},
			{name: "lineup_weeks", keyCols: []string{"team_id", "week"}},
			{name: "lineup_teams", keyCols: []string{"team_id"}},
		},
		emit: func(st *PersistedState, sink *rowSink) {
			for teamID, byWeek := range st.Lineups {
				sink.add("lineup_teams", []any{teamID})
				for week, slots := range byWeek {
					sink.add("lineup_weeks", []any{teamID, week})
					for slot, playerID := range slots {
						sink.add("lineups", []any{teamID, week, slot}, playerID)
					}
				}
			}
		},
	},
	colTransactions: {
		tables: []tableDef{{
			name:    "transactions",
			keyCols: []string{"ord"},
			valCols: []string{"id", "season", "week", "type", "team_id", "other_team_id", "adds", "drops",
				"bid", "position", "offer_id", "by", "note", "at"},
		}},
		emit: func(st *PersistedState, sink *rowSink) {
			for i, txn := range st.Transactions {
				sink.add("transactions", []any{i},
					txn.ID, txn.Season, txn.Week, txn.Type, txn.TeamID, txn.OtherTeamID,
					sink.jsonValue(txn.Adds), sink.jsonValue(txn.Drops),
					txn.Bid, txn.Position, txn.OfferID, txn.By, txn.Note, encodeTime(txn.At))
			}
		},
	},
	colWaiverClaims: {
		tables: []tableDef{{
			name:    "waiver_claims",
			keyCols: []string{"ord"},
			valCols: []string{"id", "team_id", "add_id", "drop_id", "bid", "priority", "filed_at"},
		}},
		emit: func(st *PersistedState, sink *rowSink) {
			for i, c := range st.WaiverClaims {
				sink.add("waiver_claims", []any{i},
					c.ID, c.TeamID, c.AddID, c.DropID, c.Bid, c.Priority, encodeTime(c.FiledAt))
			}
		},
	},
	colWaiverReceipts: {
		tables: []tableDef{{
			name:    "waiver_receipts",
			keyCols: []string{"ord"},
			valCols: []string{"claim_id", "season", "week", "team_id", "add_player", "drops", "bid",
				"submitted_priority", "waiver_position", "waiver_team_count", "mode", "outcome",
				"winning_team_id", "winning_bid", "winning_bid_known", "reason", "filed_at", "resolved_at"},
		}},
		emit: func(st *PersistedState, sink *rowSink) {
			for i, receipt := range st.WaiverReceipts {
				sink.add("waiver_receipts", []any{i}, receipt.ClaimID, receipt.Season, receipt.Week,
					receipt.TeamID, sink.jsonValue(receipt.Add), sink.jsonValue(receipt.Drops), receipt.Bid,
					receipt.SubmittedPriority, receipt.WaiverPosition, receipt.WaiverTeamCount, receipt.Mode,
					receipt.Outcome, receipt.WinningTeamID, receipt.WinningBid, boolToInt(receipt.WinningBidKnown), receipt.Reason,
					encodeTime(receipt.FiledAt), encodeTime(receipt.ResolvedAt))
			}
		},
	},
	colTradeOffers: {
		tables: []tableDef{{
			name:    "trade_offers",
			keyCols: []string{"ord"},
			valCols: []string{"id", "from_team_id", "to_team_id", "give", "get", "picks", "note", "status",
				"parent_id", "vetoes", "fail_reason", "created_at", "accepted_at", "resolved_at"},
		}},
		emit: func(st *PersistedState, sink *rowSink) {
			for i, o := range st.TradeOffers {
				sink.add("trade_offers", []any{i},
					o.ID, o.FromTeamID, o.ToTeamID,
					sink.jsonValue(o.Give), sink.jsonValue(o.Get), sink.jsonValue(o.Picks),
					o.Note, o.Status, o.ParentID, sink.jsonValue(o.Vetoes), o.FailReason,
					encodeTime(o.CreatedAt), encodeTime(o.AcceptedAt), encodeTime(o.ResolvedAt))
			}
		},
	},
	colRosterZones: {
		tables: []tableDef{
			{name: "roster_zones", keyCols: []string{"team_id", "player_id"},
				valCols: []string{"zone", "position", "placed_at"}},
			{name: "roster_zone_teams", keyCols: []string{"team_id"}},
		},
		emit: func(st *PersistedState, sink *rowSink) {
			for teamID, zones := range st.RosterZones {
				sink.add("roster_zone_teams", []any{teamID})
				for playerID, za := range zones {
					sink.add("roster_zones", []any{teamID, playerID}, za.Zone, za.Position, encodeTime(za.PlacedAt))
				}
			}
		},
	},
	colSchedule: {
		tables: []tableDef{{name: "schedule", keyCols: []string{"ord"}, valCols: []string{"week", "data"}}},
		emit: func(st *PersistedState, sink *rowSink) {
			if st.Schedule == nil {
				return
			}
			for i, week := range st.Schedule.Weeks {
				sink.add("schedule", []any{i}, week.Week, sink.jsonValue(week))
			}
		},
	},
	colPlayoffs: {
		tables: []tableDef{{name: "playoffs", keyCols: []string{"id"}, valCols: []string{"data"}}},
		emit: func(st *PersistedState, sink *rowSink) {
			if st.Playoffs == nil {
				return
			}
			sink.add("playoffs", []any{1}, sink.jsonValue(st.Playoffs))
		},
	},
}

// ---------------------------------------------------------------------
// Shadow index and the incremental write
// ---------------------------------------------------------------------

// shadowRow remembers one written row: its key values (so a delete can
// name them) and the canonical rendering of its value columns. The
// rendering is stored in full rather than hashed: a hash collision would
// silently drop a write, which is exactly the class of failure this
// migration exists to remove.
type shadowRow struct {
	key  []any
	vals string
}

// shadowIndex maps table name -> row key -> the row as last committed.
type shadowIndex map[string]map[string]shadowRow

// rowKeyString renders a row's primary key as a map key. The unit
// separator can never appear in a team ID, email, slot, or game ID.
func rowKeyString(key []any) string {
	var b strings.Builder
	for i, part := range key {
		if i > 0 {
			b.WriteByte(0x1f)
		}
		fmt.Fprintf(&b, "%v", part)
	}
	return b.String()
}

// rowValsString renders a row's value columns canonically, for the change
// comparison.
func rowValsString(vals []any) string {
	var b strings.Builder
	for _, part := range vals {
		fmt.Fprintf(&b, "%v\x1f", part)
	}
	return b.String()
}

// upsertSQL and deleteSQL build the two statements a table needs. Both
// persistDisposition describes how much is known about a failed write.
// NotCommitted is safe for a candidate mutation to discard. Committed means
// SQLite committed and only the post-commit verification reported a problem.
// Unknown is reserved for a Commit error whose outcome the driver cannot
// prove; callers must read the authoritative database before publishing a
// candidate identity.
type persistDisposition uint8

const (
	persistNotCommitted persistDisposition = iota
	persistCommitted
	persistUnknown
)

type persistError struct {
	err         error
	disposition persistDisposition
}

func (e *persistError) Error() string { return e.err.Error() }

func (e *persistError) Unwrap() error { return e.err }

func persistDispositionOf(err error) persistDisposition {
	var pe *persistError
	if errors.As(err, &pe) {
		return pe.disposition
	}
	return persistNotCommitted
}

// persistFailure is writeDirtyLocked's single error return path: every
// failure it reports (a SQLite driver error, a diff/emit error, a hook
// error, or the post-commit verify) passes through here, so wrapping err
// with ErrInternal here marks all of them without touching writeDirtyLocked's
// own branches.
func persistFailure(err error, disposition persistDisposition) error {
	if err == nil {
		return nil
	}
	return &persistError{err: fmt.Errorf("%w: %w", ErrInternal, err), disposition: disposition}
}

// quote every identifier, so a column named "by" or "type" is safe.
func upsertSQL(def tableDef) string {

	cols := append(append([]string{}, def.keyCols...), def.valCols...)
	quoted := make([]string, len(cols))
	holders := make([]string, len(cols))
	for i, c := range cols {
		quoted[i] = `"` + c + `"`
		holders[i] = "?"
	}
	return fmt.Sprintf(`INSERT OR REPLACE INTO "%s" (%s) VALUES (%s)`,
		def.name, strings.Join(quoted, ", "), strings.Join(holders, ", "))
}

func deleteSQL(def tableDef) string {
	where := make([]string, len(def.keyCols))
	for i, c := range def.keyCols {
		where[i] = `"` + c + `" = ?`
	}
	return fmt.Sprintf(`DELETE FROM "%s" WHERE %s`, def.name, strings.Join(where, " AND "))
}

// writeDirtyLocked writes every dirty collection's changed rows in one
// transaction. The caller must hold s.mu.
//
// The shadow index only advances after the commit reports success, so a
// failed or crashed write leaves both the database and the index at the
// last committed state, and the dirty marks stay set for the next attempt.
func (s *Store) writeDirtyLocked() error {
	// Reset this per-attempt metric before opening the transaction. A failed
	// write or an empty diff must never inherit the row count from an earlier
	// successful persist and falsely clear a retryable persistence health
	// error on the caller's next check.
	s.lastPersistRows = 0
	// The logical schema marker is part of every durable state transition, not
	// only scalar mutators. Collection-only writes (for example, a persisted
	// playoff preview) can contain fields introduced by this binary; leaving
	// kv.schema_version behind would let an older binary open and overwrite
	// those fields on rollback. Include the marker in this same transaction so
	// a committed collection write and its schema claim are inseparable. A
	// failed transaction leaves the dirty bit set for the retry path.
	advanceSchemaMarker := !s.persistedSchemaKnown || s.persistedSchemaVersion != currentSchemaVersion
	s.state.SchemaVersion = currentSchemaVersion
	s.dirty |= 1 << uint(colScalars)
	// A load normalizes the in-memory state to the current schema before
	// rebuilding the shadow. Remove the marker from that normalized shadow so
	// the persisted value is always upserted when this transaction claims the
	// current logical schema. This closes the old-marker/new-collection gap
	// without making callers remember to dirty colScalars themselves.
	if advanceSchemaMarker {
		if table := s.shadow["kv"]; table != nil {
			delete(table, rowKeyString([]any{kvSchemaVersion}))
		}
	}
	type change struct {
		table  string
		key    string
		row    shadowRow
		delete bool
	}
	var changes []change

	tx, err := s.db.Begin()
	if err != nil {
		return persistFailure(err, persistNotCommitted)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	for id := collectionID(0); id < collectionCount; id++ {
		if s.dirty&(1<<uint(id)) == 0 {
			continue
		}
		spec := collectionSpecs[id]
		sink := newRowSink()
		spec.emit(&s.state, sink)
		if sink.err != nil {
			return persistFailure(sink.err, persistNotCommitted)
		}
		for _, def := range spec.tables {
			upsert := upsertSQL(def)
			remove := deleteSQL(def)
			seen := make(map[string]struct{}, len(sink.rows[def.name]))
			for _, row := range sink.rows[def.name] {
				key := rowKeyString(row.key)
				seen[key] = struct{}{}
				vals := rowValsString(row.vals)
				if prev, ok := s.shadow[def.name][key]; ok && prev.vals == vals {
					continue
				}
				args := append(append([]any{}, row.key...), row.vals...)
				if _, err := tx.Exec(upsert, args...); err != nil {
					return persistFailure(fmt.Errorf("write %s: %w", def.name, err), persistNotCommitted)
				}
				changes = append(changes, change{table: def.name, key: key,
					row: shadowRow{key: row.key, vals: vals}})
			}
			for key, prev := range s.shadow[def.name] {
				if _, ok := seen[key]; ok {
					continue
				}
				if _, err := tx.Exec(remove, prev.key...); err != nil {
					return persistFailure(fmt.Errorf("delete from %s: %w", def.name, err), persistNotCommitted)
				}
				changes = append(changes, change{table: def.name, key: key, delete: true})
			}
		}
	}

	// persistHook is the crash-injection seam: tests use it to fail or to
	// kill the process with the transaction open. It is nil in production.
	if s.persistHook != nil {
		if err := s.persistHook(); err != nil {
			return persistFailure(err, persistNotCommitted)
		}
	}

	commit := tx.Commit
	if s.commitTx != nil {
		commit = func() error { return s.commitTx(tx) }
	}
	if err := commit(); err != nil {
		return persistFailure(err, persistUnknown)
	}
	committed = true
	if s.persistAfterCommitHook != nil {
		disposition, hookErr := s.persistAfterCommitHook()
		if hookErr != nil {
			return persistFailure(hookErr, disposition)
		}
	}
	// Read the marker after commit so the release projection reports the
	// schema actually persisted, rather than the normalized in-memory state.
	// A read failure does not turn a successful mutation into a write failure;
	// the last known marker remains the truthful evidence available to health.
	if version, known, readErr := logicalSchemaVersion(s.db); readErr == nil && known {
		s.persistedSchemaVersion = version
		s.persistedSchemaKnown = true
	}

	for _, c := range changes {
		if c.delete {
			delete(s.shadow[c.table], c.key)
			continue
		}
		if s.shadow[c.table] == nil {
			s.shadow[c.table] = map[string]shadowRow{}
		}
		s.shadow[c.table][c.key] = c.row
	}
	s.dirty = 0
	s.lastPersistRows = len(changes)

	if sqlitePersistVerify {
		return persistFailure(s.verifyPersistedLocked(), persistCommitted)
	}
	return nil
}

// rebuildShadowLocked recomputes the shadow index from the in-memory
// state. It runs right after a load or an import, when the database holds
// exactly the rows this state emits.
func (s *Store) rebuildShadowLocked() error {
	shadow := shadowIndex{}
	for id := collectionID(0); id < collectionCount; id++ {
		spec := collectionSpecs[id]
		sink := newRowSink()
		spec.emit(&s.state, sink)
		if sink.err != nil {
			return sink.err
		}
		for _, def := range spec.tables {
			table := shadow[def.name]
			if table == nil {
				table = map[string]shadowRow{}
				shadow[def.name] = table
			}
			for _, row := range sink.rows[def.name] {
				table[rowKeyString(row.key)] = shadowRow{key: row.key, vals: rowValsString(row.vals)}
			}
		}
	}
	s.shadow = shadow
	return nil
}

// persistedStatesEqual compares the canonical JSON form used by persistence
// verification. Reset reconciliation must tolerate harmless nil/empty and
// time-location representation details while still requiring every persisted
// collection and scalar to match the authoritative database.
func persistedStatesEqual(a, b PersistedState) bool {
	left, err := json.Marshal(cloneState(a))
	if err != nil {
		return false
	}
	right, err := json.Marshal(cloneState(b))
	if err != nil {
		return false
	}
	return string(left) == string(right)
}

// verifyPersistedLocked re-reads the whole state from the database and
// compares it with the in-memory model. Any difference means a mutator
// changed a collection it did not declare, so the write was skipped. The
// comparison runs over the canonical JSON encoding of both states, which
// is the exact fidelity the JSON engine guaranteed and which ignores
// harmless representation details (a monotonic clock reading, two equal
// fixed time zones with different addresses).
func (s *Store) verifyPersistedLocked() error {
	stored, err := loadStateFromDB(s.db)
	if err != nil {
		return fmt.Errorf("persist verification could not re-read the database: %w", err)
	}
	want, err := json.Marshal(cloneState(s.state))
	if err != nil {
		return err
	}
	got, err := json.Marshal(cloneState(stored))
	if err != nil {
		return err
	}
	if string(want) != string(got) {
		return fmt.Errorf("persist verification failed: the database does not match the in-memory state\n in-memory: %s\n stored:    %s", want, got)
	}
	return nil
}

// ---------------------------------------------------------------------
// Load
// ---------------------------------------------------------------------

// loadStateFromDB reads the whole state back. It is called once per boot,
// and once per persist under the verification switch.
func loadStateFromDB(db *sql.DB) (PersistedState, error) {
	return loadStateFromDBMode(db, true)
}

// loadStateFromDBUnrepaired is the read-only compatibility path used while
// proving an old import that committed before its legacy-file rename. It
// must not run the identity cleanup transaction before the JSON-vs-database
// comparison has established which bytes are authoritative.
func loadStateFromDBUnrepaired(db *sql.DB) (PersistedState, error) {
	return loadStateFromDBMode(db, false)
}

func loadStateFromDBMode(db *sql.DB, repairIdentity bool) (PersistedState, error) {
	var state PersistedState
	normalizeState(&state)
	state.SchemaVersion = currentSchemaVersion
	// Refuse a logically newer database before the clean-break repair can
	// mutate any identity rows. PRAGMA user_version protects migrations; this
	// read-only kv check protects the logical state schema independently.
	if err := checkLogicalSchemaVersion(db); err != nil {
		return state, err
	}
	authority, err := readPersistenceAuthority(db)
	if err != nil {
		return state, err
	}
	state.persistenceAuthority = authority
	if repairIdentity {
		if err := repairIdentityRows(db); err != nil {
			return state, err
		}
	}

	scalars := map[string]string{}
	if err := queryRows(db, `SELECT "key", "value" FROM kv`, func(rows *sql.Rows) error {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return err
		}
		scalars[key] = value
		return nil
	}); err != nil {
		return state, err
	}
	if raw, ok := scalars[kvSchemaVersion]; ok {
		version, err := strconv.Atoi(raw)
		if err != nil {
			return state, fmt.Errorf("kv %s: %w", kvSchemaVersion, err)
		}
		if version > currentSchemaVersion {
			return state, fmt.Errorf("%w: stored state is version %d, this binary supports up to %d",
				errSchemaTooNew, version, currentSchemaVersion)
		}
		// Migrate forward, exactly as the JSON loader did: every field
		// added since is additive with a nil-safe zero value, so the
		// version is simply stamped current.
		state.SchemaVersion = currentSchemaVersion
	}
	if state.ClockDeadline, err = decodeTime(scalars[kvClockDeadline]); err != nil {
		return state, err
	}
	state.ClockPaused = scalars[kvClockPaused] == "1"
	state.ClockRemainingSec, _ = strconv.Atoi(scalars[kvClockRemainingSec])
	state.ClockDurationSec, _ = strconv.Atoi(scalars[kvClockDurationSec])
	state.DraftStarted = scalars[kvDraftStarted] == "1"
	if state.DraftStartedAt, err = decodeTime(scalars[kvDraftStartedAt]); err != nil {
		return state, err
	}
	if state.DraftAtOverride, err = decodeTime(scalars[kvDraftAtOverride]); err != nil {
		return state, err
	}
	if state.ScoringChangedAt, err = decodeTime(scalars[kvScoringChangedAt]); err != nil {
		return state, err
	}
	state.Phase = scalars[kvPhase]
	if state.WaiversProcessedThrough, err = decodeTime(scalars[kvWaiversProcessedThrough]); err != nil {
		return state, err
	}
	if raw, ok := scalars[kvRosterOverride]; ok {
		var override RosterOverride
		if err := json.Unmarshal([]byte(raw), &override); err != nil {
			return state, fmt.Errorf("kv %s: %w", kvRosterOverride, err)
		}
		state.RosterOverride = &override
	}
	if raw, ok := scalars[kvTrimmedTeamIDs]; ok {
		var trimmed []string
		if err := json.Unmarshal([]byte(raw), &trimmed); err != nil {
			return state, fmt.Errorf("kv %s: %w", kvTrimmedTeamIDs, err)
		}
		state.TrimmedTeamIDs = trimmed
	}
	if raw, ok := scalars[kvSeatRevisions]; ok {
		var revisions map[string]uint64
		if err := json.Unmarshal([]byte(raw), &revisions); err != nil {
			return state, fmt.Errorf("kv %s: %w", kvSeatRevisions, err)
		}
		state.SeatRevisions = revisions
	}

	if err := queryRows(db, `SELECT "number", "round", "team_id", "player_id", "made_at", "made_by" FROM picks ORDER BY "number"`,
		func(rows *sql.Rows) error {
			var p DraftPick
			var madeAt string
			if err := rows.Scan(&p.Number, &p.Round, &p.TeamID, &p.PlayerID, &madeAt, &p.MadeBy); err != nil {
				return err
			}
			var err error
			if p.MadeAt, err = decodeTime(madeAt); err != nil {
				return err
			}
			state.Picks = append(state.Picks, p)
			return nil
		}); err != nil {
		return state, err
	}

	if err := queryRows(db, `SELECT "team_id", "ready" FROM ready`, func(rows *sql.Rows) error {
		var teamID string
		var ready int
		if err := rows.Scan(&teamID, &ready); err != nil {
			return err
		}
		state.Ready[teamID] = ready == 1
		return nil
	}); err != nil {
		return state, err
	}

	if err := queryRows(db, `SELECT "key", "team_id", "name", "email", "role" FROM members`, func(rows *sql.Rows) error {
		var key string
		var m Member
		if err := rows.Scan(&key, &m.TeamID, &m.Name, &m.Email, &m.Role); err != nil {
			return err
		}
		state.Members[key] = m
		return nil
	}); err != nil {
		return state, err
	}

	if err := queryRows(db, `SELECT "email", "team_id" FROM co_invites`, func(rows *sql.Rows) error {
		var email, teamID string
		if err := rows.Scan(&email, &teamID); err != nil {
			return err
		}
		state.CoInvites[email] = teamID
		return nil
	}); err != nil {
		return state, err
	}

	if err := queryRows(db, `SELECT "email" FROM invites ORDER BY "ord"`, func(rows *sql.Rows) error {
		var email string
		if err := rows.Scan(&email); err != nil {
			return err
		}
		state.Invites = append(state.Invites, email)
		return nil
	}); err != nil {
		return state, err
	}

	if err := queryRows(db, `SELECT "owner" FROM board_owners`, func(rows *sql.Rows) error {
		var owner string
		if err := rows.Scan(&owner); err != nil {
			return err
		}
		state.Boards[owner] = []string{}
		return nil
	}); err != nil {
		return state, err
	}
	if err := queryRows(db, `SELECT "owner", "player_id" FROM boards ORDER BY "owner", "ord"`, func(rows *sql.Rows) error {
		var owner, playerID string
		if err := rows.Scan(&owner, &playerID); err != nil {
			return err
		}
		state.Boards[owner] = append(state.Boards[owner], playerID)
		return nil
	}); err != nil {
		return state, err
	}

	if err := queryRows(db, `SELECT "team_id", "name" FROM team_names`, func(rows *sql.Rows) error {
		var teamID, name string
		if err := rows.Scan(&teamID, &name); err != nil {
			return err
		}
		state.TeamNames[teamID] = name
		return nil
	}); err != nil {
		return state, err
	}

	if err := queryRows(db, `SELECT "team_id" FROM draft_order ORDER BY "ord"`, func(rows *sql.Rows) error {
		var teamID string
		if err := rows.Scan(&teamID); err != nil {
			return err
		}
		state.DraftOrder = append(state.DraftOrder, teamID)
		return nil
	}); err != nil {
		return state, err
	}

	if err := queryRows(db, `SELECT "key", "points" FROM scoring`, func(rows *sql.Rows) error {
		var key string
		var points float64
		if err := rows.Scan(&key, &points); err != nil {
			return err
		}
		state.Scoring[key] = points
		return nil
	}); err != nil {
		return state, err
	}

	if err := queryRows(db, `SELECT "owner" FROM pickem_owners`, func(rows *sql.Rows) error {
		var owner string
		if err := rows.Scan(&owner); err != nil {
			return err
		}
		state.Pickems[owner] = map[string]string{}
		return nil
	}); err != nil {
		return state, err
	}
	if err := queryRows(db, `SELECT "owner", "game_id", "team" FROM pickems`, func(rows *sql.Rows) error {
		var owner, gameID, team string
		if err := rows.Scan(&owner, &gameID, &team); err != nil {
			return err
		}
		if state.Pickems[owner] == nil {
			state.Pickems[owner] = map[string]string{}
		}
		state.Pickems[owner][gameID] = team
		return nil
	}); err != nil {
		return state, err
	}
	if err := queryRows(db, `SELECT "owner", "entered_at" FROM pickem_entries`, func(rows *sql.Rows) error {
		var owner, raw string
		if err := rows.Scan(&owner, &raw); err != nil {
			return err
		}
		enteredAt, err := decodeTime(raw)
		if err != nil {
			return fmt.Errorf("pickem_entries entered_at: %w", err)
		}
		if !enteredAt.IsZero() {
			state.PickemEnteredAt[owner] = enteredAt
		}
		return nil
	}); err != nil {
		return state, err
	}
	if err := queryRows(db, `SELECT "game_id", "data" FROM pickem_markets`, func(rows *sql.Rows) error {
		var gameID, raw string
		if err := rows.Scan(&gameID, &raw); err != nil {
			return err
		}
		var market PickemMarket
		if err := json.Unmarshal([]byte(raw), &market); err != nil {
			return fmt.Errorf("pickem_markets data: %w", err)
		}
		state.PickemMarkets[gameID] = market
		return nil
	}); err != nil {
		return state, err
	}

	if err := queryRows(db, `SELECT "owner" FROM blitz_owners`, func(rows *sql.Rows) error {
		var owner string
		if err := rows.Scan(&owner); err != nil {
			return err
		}
		state.BlitzEntries[owner] = map[string]BlitzEntry{}
		return nil
	}); err != nil {
		return state, err
	}
	if err := queryRows(db, `SELECT "owner", "slate", "players", "updated_at" FROM blitz_entries`, func(rows *sql.Rows) error {
		var owner, slate, players, updatedAt string
		if err := rows.Scan(&owner, &slate, &players, &updatedAt); err != nil {
			return err
		}
		var entry BlitzEntry
		if err := json.Unmarshal([]byte(players), &entry.Players); err != nil {
			return fmt.Errorf("blitz_entries players: %w", err)
		}
		var err error
		if entry.UpdatedAt, err = decodeTime(updatedAt); err != nil {
			return err
		}
		if state.BlitzEntries[owner] == nil {
			state.BlitzEntries[owner] = map[string]BlitzEntry{}
		}
		state.BlitzEntries[owner][slate] = entry
		return nil
	}); err != nil {
		return state, err
	}

	if err := queryRows(db, `SELECT "team_id", "enabled" FROM autopick`, func(rows *sql.Rows) error {
		var teamID string
		var enabled int
		if err := rows.Scan(&teamID, &enabled); err != nil {
			return err
		}
		state.Autopick[teamID] = enabled == 1
		return nil
	}); err != nil {
		return state, err
	}

	if err := queryRows(db, `SELECT "key", "sent_at" FROM sent_log`, func(rows *sql.Rows) error {
		var key, sentAt string
		if err := rows.Scan(&key, &sentAt); err != nil {
			return err
		}
		at, err := decodeTime(sentAt)
		if err != nil {
			return err
		}
		state.SentLog[key] = at
		return nil
	}); err != nil {
		return state, err
	}

	if err := queryRows(db, `SELECT "email" FROM notify_pref_owners`, func(rows *sql.Rows) error {
		var email string
		if err := rows.Scan(&email); err != nil {
			return err
		}
		state.NotifyPrefs[email] = map[string]bool{}
		return nil
	}); err != nil {
		return state, err
	}
	if err := queryRows(db, `SELECT "email", "category", "enabled" FROM notify_prefs`, func(rows *sql.Rows) error {
		var email, category string
		var enabled int
		if err := rows.Scan(&email, &category, &enabled); err != nil {
			return err
		}
		if state.NotifyPrefs[email] == nil {
			state.NotifyPrefs[email] = map[string]bool{}
		}
		state.NotifyPrefs[email][category] = enabled == 1
		return nil
	}); err != nil {
		return state, err
	}

	if err := queryRows(db, `SELECT "team_id", "motif" FROM badge_claims`, func(rows *sql.Rows) error {
		var teamID, motif string
		if err := rows.Scan(&teamID, &motif); err != nil {
			return err
		}
		state.BadgeClaims[teamID] = motif
		return nil
	}); err != nil {
		return state, err
	}

	if err := queryRows(db, `SELECT "team_id", "ref" FROM avatar_refs`, func(rows *sql.Rows) error {
		var teamID, ref string
		if err := rows.Scan(&teamID, &ref); err != nil {
			return err
		}
		state.AvatarRefs[teamID] = ref
		return nil
	}); err != nil {
		return state, err
	}
	if err := queryRows(db, `SELECT "id", "body", "posted_at", "posted_by" FROM announcements ORDER BY "ord"`,

		func(rows *sql.Rows) error {
			var a Announcement
			var postedAt string
			if err := rows.Scan(&a.ID, &a.Body, &postedAt, &a.PostedBy); err != nil {
				return err
			}
			var err error
			if a.PostedAt, err = decodeTime(postedAt); err != nil {
				return err
			}
			state.Announcements = append(state.Announcements, a)
			return nil
		}); err != nil {
		return state, err
	}

	if err := queryRows(db, `SELECT "team_id" FROM lineup_teams`, func(rows *sql.Rows) error {
		var teamID string
		if err := rows.Scan(&teamID); err != nil {
			return err
		}
		state.Lineups[teamID] = map[int]map[string]string{}
		return nil
	}); err != nil {
		return state, err
	}
	if err := queryRows(db, `SELECT "team_id", "week" FROM lineup_weeks`, func(rows *sql.Rows) error {
		var teamID string
		var week int
		if err := rows.Scan(&teamID, &week); err != nil {
			return err
		}
		if state.Lineups[teamID] == nil {
			state.Lineups[teamID] = map[int]map[string]string{}
		}
		state.Lineups[teamID][week] = map[string]string{}
		return nil
	}); err != nil {
		return state, err
	}
	if err := queryRows(db, `SELECT "team_id", "week", "slot", "player_id" FROM lineups`, func(rows *sql.Rows) error {
		var teamID, slot, playerID string
		var week int
		if err := rows.Scan(&teamID, &week, &slot, &playerID); err != nil {
			return err
		}
		if state.Lineups[teamID] == nil {
			state.Lineups[teamID] = map[int]map[string]string{}
		}
		if state.Lineups[teamID][week] == nil {
			state.Lineups[teamID][week] = map[string]string{}
		}
		state.Lineups[teamID][week][slot] = playerID
		return nil
	}); err != nil {
		return state, err
	}

	if err := queryRows(db, `SELECT "id", "season", "week", "type", "team_id", "other_team_id", "adds", "drops",
		"bid", "position", "offer_id", "by", "note", "at" FROM transactions ORDER BY "ord"`,
		func(rows *sql.Rows) error {
			var txn Transaction
			var adds, drops, at string
			if err := rows.Scan(&txn.ID, &txn.Season, &txn.Week, &txn.Type, &txn.TeamID, &txn.OtherTeamID,
				&adds, &drops, &txn.Bid, &txn.Position, &txn.OfferID, &txn.By, &txn.Note, &at); err != nil {
				return err
			}
			if err := json.Unmarshal([]byte(adds), &txn.Adds); err != nil {
				return fmt.Errorf("transactions adds: %w", err)
			}
			if err := json.Unmarshal([]byte(drops), &txn.Drops); err != nil {
				return fmt.Errorf("transactions drops: %w", err)
			}
			var err error
			if txn.At, err = decodeTime(at); err != nil {
				return err
			}
			state.Transactions = append(state.Transactions, txn)
			return nil
		}); err != nil {
		return state, err
	}

	if err := queryRows(db, `SELECT "id", "team_id", "add_id", "drop_id", "bid", "priority", "filed_at"
		FROM waiver_claims ORDER BY "ord"`,
		func(rows *sql.Rows) error {
			var c WaiverClaim
			var filedAt string
			if err := rows.Scan(&c.ID, &c.TeamID, &c.AddID, &c.DropID, &c.Bid, &c.Priority, &filedAt); err != nil {
				return err
			}
			var err error
			if c.FiledAt, err = decodeTime(filedAt); err != nil {
				return err
			}
			state.WaiverClaims = append(state.WaiverClaims, c)
			return nil
		}); err != nil {
		return state, err
	}

	if err := queryRows(db, `SELECT "claim_id", "season", "week", "team_id", "add_player", "drops", "bid",
		"submitted_priority", "waiver_position", "waiver_team_count", "mode", "outcome",
		"winning_team_id", "winning_bid", "winning_bid_known", "reason", "filed_at", "resolved_at" FROM waiver_receipts ORDER BY "ord"`,
		func(rows *sql.Rows) error {
			var receipt WaiverReceipt
			var add, drops, filedAt, resolvedAt string
			var winningBidKnown int
			if err := rows.Scan(&receipt.ClaimID, &receipt.Season, &receipt.Week, &receipt.TeamID,
				&add, &drops, &receipt.Bid, &receipt.SubmittedPriority, &receipt.WaiverPosition,
				&receipt.WaiverTeamCount, &receipt.Mode, &receipt.Outcome, &receipt.WinningTeamID,
				&receipt.WinningBid, &winningBidKnown,
				&receipt.Reason, &filedAt, &resolvedAt); err != nil {
				return err
			}
			receipt.WinningBidKnown = winningBidKnown != 0
			if err := json.Unmarshal([]byte(add), &receipt.Add); err != nil {
				return fmt.Errorf("waiver_receipts add: %w", err)
			}
			if err := json.Unmarshal([]byte(drops), &receipt.Drops); err != nil {
				return fmt.Errorf("waiver_receipts drops: %w", err)
			}
			var err error
			if receipt.FiledAt, err = decodeTime(filedAt); err != nil {
				return err
			}
			if receipt.ResolvedAt, err = decodeTime(resolvedAt); err != nil {
				return err
			}
			state.WaiverReceipts = append(state.WaiverReceipts, receipt)
			return nil
		}); err != nil {
		return state, err
	}

	if err := queryRows(db, `SELECT "id", "from_team_id", "to_team_id", "give", "get", "picks", "note", "status",
		"parent_id", "vetoes", "fail_reason", "created_at", "accepted_at", "resolved_at"
		FROM trade_offers ORDER BY "ord"`,
		func(rows *sql.Rows) error {
			var o TradeOffer
			var give, get, picks, vetoes, createdAt, acceptedAt, resolvedAt string
			if err := rows.Scan(&o.ID, &o.FromTeamID, &o.ToTeamID, &give, &get, &picks, &o.Note, &o.Status,
				&o.ParentID, &vetoes, &o.FailReason, &createdAt, &acceptedAt, &resolvedAt); err != nil {
				return err
			}
			for _, pair := range []struct {
				raw  string
				dest *[]string
			}{{give, &o.Give}, {get, &o.Get}, {picks, &o.Picks}, {vetoes, &o.Vetoes}} {
				if err := json.Unmarshal([]byte(pair.raw), pair.dest); err != nil {
					return fmt.Errorf("trade_offers list: %w", err)
				}
			}
			var err error
			if o.CreatedAt, err = decodeTime(createdAt); err != nil {
				return err
			}
			if o.AcceptedAt, err = decodeTime(acceptedAt); err != nil {
				return err
			}
			if o.ResolvedAt, err = decodeTime(resolvedAt); err != nil {
				return err
			}
			state.TradeOffers = append(state.TradeOffers, o)
			return nil
		}); err != nil {
		return state, err
	}

	if err := queryRows(db, `SELECT "team_id" FROM roster_zone_teams`, func(rows *sql.Rows) error {
		var teamID string
		if err := rows.Scan(&teamID); err != nil {
			return err
		}
		state.RosterZones[teamID] = map[string]ZoneAssignment{}
		return nil
	}); err != nil {
		return state, err
	}
	if err := queryRows(db, `SELECT "team_id", "player_id", "zone", "position", "placed_at" FROM roster_zones`,
		func(rows *sql.Rows) error {
			var teamID, playerID string
			var za ZoneAssignment
			var placedAt string
			if err := rows.Scan(&teamID, &playerID, &za.Zone, &za.Position, &placedAt); err != nil {
				return err
			}
			var err error
			if za.PlacedAt, err = decodeTime(placedAt); err != nil {
				return err
			}
			if state.RosterZones[teamID] == nil {
				state.RosterZones[teamID] = map[string]ZoneAssignment{}
			}
			state.RosterZones[teamID][playerID] = za
			return nil
		}); err != nil {
		return state, err
	}

	if raw, ok := scalars[kvScheduleHeader]; ok {
		var header scheduleHeader
		if err := json.Unmarshal([]byte(raw), &header); err != nil {
			return state, fmt.Errorf("kv %s: %w", kvScheduleHeader, err)
		}
		schedule := &SeasonSchedule{
			Season:      header.Season,
			Seed:        header.Seed,
			GeneratedAt: header.GeneratedAt,
			StartWeek:   header.StartWeek,
		}
		if err := queryRows(db, `SELECT "data" FROM schedule ORDER BY "ord"`, func(rows *sql.Rows) error {
			var data string
			if err := rows.Scan(&data); err != nil {
				return err
			}
			var week ScheduleWeek
			if err := json.Unmarshal([]byte(data), &week); err != nil {
				return fmt.Errorf("schedule week: %w", err)
			}
			schedule.Weeks = append(schedule.Weeks, week)
			return nil
		}); err != nil {
			return state, err
		}
		state.Schedule = schedule
	}

	if err := queryRows(db, `SELECT "data" FROM playoffs WHERE "id" = 1`, func(rows *sql.Rows) error {
		var data string
		if err := rows.Scan(&data); err != nil {
			return err
		}
		var playoffs PlayoffState
		if err := json.Unmarshal([]byte(data), &playoffs); err != nil {
			return fmt.Errorf("playoffs: %w", err)
		}
		state.Playoffs = &playoffs
		return nil
	}); err != nil {
		return state, err
	}

	// v1 had no catalog-enforced identity boundary. Strip retired/unknown
	// motifs and keep only one holder for each current canonical art before
	// this state becomes the Store's read model. repairIdentityRows already
	// removed those rows durably before this read; every reload therefore sees
	// the same repaired identity collections and rebuilds its shadow from them.
	normalizeScoringValues(state.Scoring)
	normalizeIdentityCollections(&state)
	return state, nil
}

// logicalSchemaVersion is deliberately read-only. It distinguishes a
// missing marker on a pre-migration database from a real persisted marker so
// callers can report unknown evidence without inventing a version.
func logicalSchemaVersion(db *sql.DB) (version int, known bool, err error) {
	if db == nil {
		return 0, false, errors.New("logical schema version requires an open database")
	}
	var raw string
	err = db.QueryRow(`SELECT "value" FROM kv WHERE "key" = ?`, kvSchemaVersion).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	// A database at PRAGMA user_version=0 has no tables yet. This is the
	// normal pre-migration state for a newly-created file, and is not a
	// logical schema claim that needs to be rejected.
	if err != nil && strings.Contains(err.Error(), "no such table") {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	version, err = strconv.Atoi(raw)
	if err != nil {
		return 0, false, fmt.Errorf("kv %s: %w", kvSchemaVersion, err)
	}
	return version, true, nil
}

// checkLogicalSchemaVersion is deliberately read-only. A database may carry
// a supported SQLite migration number while its logical state schema was
// written by a newer binary; that case must be rejected before repair or any
// other startup action that could change durable rows.
func checkLogicalSchemaVersion(db *sql.DB) error {
	return checkLogicalSchemaVersionAt(db, currentSchemaVersion)
}

// checkLogicalSchemaVersionAt is the same read-only compatibility check with
// an explicit upper bound. Production always uses currentSchemaVersion; the
// explicit bound lets migration tests model an older deployed binary and
// prove that a collection-only v9 write is refused by a v8 reader.
func checkLogicalSchemaVersionAt(db *sql.DB, supportedVersion int) error {
	version, known, err := logicalSchemaVersion(db)
	if err != nil {
		return err
	}
	if !known {
		return nil
	}
	if version > supportedVersion {
		return fmt.Errorf("%w: stored state is version %d, this binary supports up to %d",
			errSchemaTooNew, version, supportedVersion)
	}
	return nil
}

// repairIdentityRows is the durable v1->v2 clean-break boundary for the two
// identity tables. It runs before every authoritative load, including the
// initial startup load, so the Store's shadow is built from the repaired
// database rather than from rows that only an in-memory normalizer hid.
//
// Valid custom-avatar refs win a historical same-team badge/avatar conflict,
// matching the upload transition's "custom image releases badge" rule. For
// duplicate valid badge motifs, the lexically first known team ID wins. Every
// invalid team, retired/unknown motif, invalid ref, duplicate loser, and
// badge row shadowed by a valid avatar is physically deleted in one durable
// SQLite transaction.
func repairIdentityRows(db *sql.DB) error {
	if db == nil {
		return errors.New("identity repair requires an open database")
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	rollback := true
	defer func() {
		if rollback {
			_ = tx.Rollback()
		}
	}()

	type avatarRow struct {
		teamID string
		ref    string
	}
	rows, err := tx.Query(`SELECT "team_id", "ref" FROM avatar_refs ORDER BY "team_id", "ref"`)
	if err != nil {
		return err
	}
	var avatars []avatarRow
	var invalidAvatarTeams []string
	for rows.Next() {
		var row avatarRow
		if err := rows.Scan(&row.teamID, &row.ref); err != nil {
			_ = rows.Close()
			return err
		}
		if !knownTeam(row.teamID) || !validAvatarRef(row.ref) {
			invalidAvatarTeams = append(invalidAvatarTeams, row.teamID)
			continue
		}
		avatars = append(avatars, row)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	validAvatarTeams := make(map[string]struct{}, len(avatars))
	for _, row := range avatars {
		validAvatarTeams[row.teamID] = struct{}{}
	}

	rows, err = tx.Query(`SELECT "team_id", "motif" FROM badge_claims ORDER BY "team_id", "motif"`)
	if err != nil {
		return err
	}
	seenMotifs := map[string]struct{}{}
	type badgeRow struct {
		teamID string
		motif  string
	}
	var removeBadges []badgeRow
	for rows.Next() {
		var row badgeRow
		if err := rows.Scan(&row.teamID, &row.motif); err != nil {
			_ = rows.Close()
			return err
		}
		if !knownTeam(row.teamID) || !knownMotif(row.motif) {
			removeBadges = append(removeBadges, row)
			continue
		}
		if _, hasAvatar := validAvatarTeams[row.teamID]; hasAvatar {
			removeBadges = append(removeBadges, row)
			continue
		}
		if _, duplicate := seenMotifs[row.motif]; duplicate {
			removeBadges = append(removeBadges, row)
			continue
		}
		seenMotifs[row.motif] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}

	for _, teamID := range invalidAvatarTeams {
		if _, err := tx.Exec(`DELETE FROM avatar_refs WHERE "team_id" = ?`, teamID); err != nil {
			return err
		}
	}
	for _, row := range removeBadges {
		if _, err := tx.Exec(`DELETE FROM badge_claims WHERE "team_id" = ? AND "motif" = ?`, row.teamID, row.motif); err != nil {
			return err
		}
	}
	if len(invalidAvatarTeams) == 0 && len(removeBadges) == 0 {
		return tx.Rollback()
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	rollback = false
	return nil
}

// queryRows runs one query and hands each row to scan.
func queryRows(db *sql.DB, query string, scan func(*sql.Rows) error) error {
	rows, err := db.Query(query)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := scan(rows); err != nil {
			return err
		}
	}
	return rows.Err()
}

// normalizeState fills every nil collection with an empty, non-nil value.
// It is the one place the "an absent collection reads as empty, never nil"
// contract lives; both the database loader and the JSON importer call it,
// exactly as the old JSON loader's nil-map guards did.
func normalizeState(state *PersistedState) {
	if state.Ready == nil {
		state.Ready = map[string]bool{}
	}
	if state.Picks == nil {
		state.Picks = []DraftPick{}
	}
	if state.Members == nil {
		state.Members = map[string]Member{}
	}
	if state.Invites == nil {
		state.Invites = []string{}
	}
	if state.Boards == nil {
		state.Boards = map[string][]string{}
	}
	if state.TeamNames == nil {
		state.TeamNames = map[string]string{}
	}
	if state.DraftOrder == nil {
		state.DraftOrder = []string{}
	}
	if state.Scoring == nil {
		state.Scoring = map[string]float64{}
	}
	normalizeScoringValues(state.Scoring)
	if state.Pickems == nil {
		state.Pickems = map[string]map[string]string{}
	}
	if state.PickemEnteredAt == nil {
		state.PickemEnteredAt = map[string]time.Time{}
	}
	if state.PickemMarkets == nil {
		state.PickemMarkets = map[string]PickemMarket{}
	}
	if state.BlitzEntries == nil {
		state.BlitzEntries = map[string]map[string]BlitzEntry{}
	}
	if state.Autopick == nil {
		state.Autopick = map[string]bool{}
	}
	if state.SentLog == nil {
		state.SentLog = map[string]time.Time{}
	}
	if state.NotifyPrefs == nil {
		state.NotifyPrefs = map[string]map[string]bool{}
	}
	if state.BadgeClaims == nil {
		state.BadgeClaims = map[string]string{}
	}
	if state.AvatarRefs == nil {
		state.AvatarRefs = map[string]string{}
	}
	if state.Announcements == nil {
		state.Announcements = []Announcement{}
	}
	if state.Lineups == nil {
		state.Lineups = map[string]map[int]map[string]string{}
	}
	if state.Transactions == nil {
		state.Transactions = []Transaction{}
	}
	if state.WaiverClaims == nil {
		state.WaiverClaims = []WaiverClaim{}
	}
	if state.WaiverReceipts == nil {
		state.WaiverReceipts = []WaiverReceipt{}
	}
	normalizeAllClaimPriorities(state.WaiverClaims)
	if state.TradeOffers == nil {
		state.TradeOffers = []TradeOffer{}
	}
	if state.RosterZones == nil {
		state.RosterZones = map[string]map[string]ZoneAssignment{}
	}
	if state.CoInvites == nil {
		state.CoInvites = map[string]string{}
	}
	if state.SeatRevisions == nil {
		state.SeatRevisions = map[string]uint64{}
	}
	if state.TrimmedTeamIDs == nil {
		state.TrimmedTeamIDs = []string{}
	}
	normalizeIdentityCollections(state)
}

// normalizeIdentityCollections applies the same canonical identity boundary
// to in-memory imports/candidates that repairIdentityRows applies durably to
// SQLite. Invalid avatar refs are stripped first; a remaining valid avatar
// wins a historical same-team badge conflict, then duplicate canonical
// motifs are resolved deterministically by normalizeBadgeClaims.
func normalizeIdentityCollections(state *PersistedState) {
	for teamID, ref := range state.AvatarRefs {
		if !knownTeam(teamID) || !validAvatarRef(ref) {
			delete(state.AvatarRefs, teamID)
		}
	}
	for teamID := range state.BadgeClaims {
		if _, hasAvatar := state.AvatarRefs[teamID]; hasAvatar {
			delete(state.BadgeClaims, teamID)
		}
	}
	normalizeBadgeClaims(state)
}

// normalizeBadgeClaims is the pre-v1 clean break for persisted badge
// identity. Retired aliases are not translated, and arbitrary/unknown team
// IDs are not allowed to occupy a canonical motif. Sorting team IDs makes a
// hand-edited database with duplicate canonical claims deterministic: the
// lexically first known team keeps the claim and later duplicates are
// dropped from the read model.
func normalizeBadgeClaims(state *PersistedState) {
	if len(state.BadgeClaims) < 1 {
		return
	}
	teamIDs := make([]string, 0, len(state.BadgeClaims))
	for teamID := range state.BadgeClaims {
		teamIDs = append(teamIDs, teamID)
	}
	sort.Strings(teamIDs)
	seen := make(map[string]struct{}, len(teamIDs))
	for _, teamID := range teamIDs {
		motif := state.BadgeClaims[teamID]
		if !knownTeam(teamID) || !knownMotif(motif) {
			delete(state.BadgeClaims, teamID)
			continue
		}
		if _, duplicate := seen[motif]; duplicate {
			delete(state.BadgeClaims, teamID)
			continue
		}
		seen[motif] = struct{}{}
	}
}
