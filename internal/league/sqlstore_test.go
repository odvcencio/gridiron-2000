package league

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestMain turns the persist read-back check on for the whole package
// test binary. Every persist in every test then re-reads the database and
// compares it with the in-memory model, so a mutator that changes a
// collection it did not declare to persistLocked fails the test that
// exercised it. Production never sets this.
//
// The check roughly doubles the suite's runtime, so "go test -short"
// leaves it off for a fast edit loop. Every full run has it on.
func TestMain(m *testing.M) {
	flag.Parse()
	sqlitePersistVerify = !testing.Short()
	os.Exit(m.Run())
}

// ---------------------------------------------------------------------
// Test seams
// ---------------------------------------------------------------------

// errInjectedPersist is the failure failThisStorePersist injects.
var errInjectedPersist = errors.New("injected persist failure")

// failThisStorePersist makes every later persist on this store fail
// inside its transaction, so nothing commits. It replaces the JSON
// engine's "make the directory unwritable" trick, which no longer fails a
// write to an already-open database.
func failThisStorePersist(s *Store) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.persistHook = func() error { return errInjectedPersist }
}

// killThisStorePersist makes the next persist kill this process from
// inside its open transaction: the crash-consistency seam.
func killThisStorePersist(s *Store) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.persistHook = func() error {
		proc, err := os.FindProcess(os.Getpid())
		if err != nil {
			return err
		}
		_ = proc.Signal(os.Kill)
		time.Sleep(30 * time.Second) // never reached: the signal lands first
		return nil
	}
}

// reloadStoredState opens a second store on the same data directory and
// returns what the database holds. Tests that once read the JSON state
// file's bytes read the stored state through this instead.
func reloadStoredState(t *testing.T, path string) PersistedState {
	t.Helper()
	store := NewStore(path)
	if err := store.StartupError(); err != nil {
		t.Fatalf("reloading %s: %v", path, err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store.Snapshot()
}

// ---------------------------------------------------------------------
// The realistic fixture: every collection, non-empty
// ---------------------------------------------------------------------

// realisticFixture builds a state that exercises every collection the
// store persists, including a trade offer in every lifecycle status, a
// transaction of every type, both roster zones, and a partly played
// schedule. The migration round-trip test writes it as a JSON state file
// and checks that importing it reproduces it exactly.
func realisticFixture() PersistedState {
	at := func(day, hour int) time.Time {
		return time.Date(2026, 9, day, hour, 30, 0, 0, time.UTC)
	}
	state := PersistedState{
		SchemaVersion: currentSchemaVersion,
		Ready:         map[string]bool{"team-1": true, "team-2": false, "team-5": true},
		Members: map[string]Member{
			"one@example.com":  {TeamID: "team-1", Name: "One", Email: "one@example.com"},
			"two@example.com":  {TeamID: "team-2", Name: "Two", Email: "two@example.com"},
			"co@example.com":   {TeamID: "team-1", Name: "Co", Email: "co@example.com", Role: "co"},
			"free@example.com": {TeamID: "", Name: "Seatless", Email: "free@example.com"},
		},
		Invites:   []string{"one@example.com", "two@example.com", "pending@example.com"},
		CoInvites: map[string]string{"pending@example.com": "team-3"},
		Boards: map[string][]string{
			"one@example.com": {"p-01", "p-02", "p-03"},
			"two@example.com": {},
		},
		TeamNames:      map[string]string{"team-1": "Rolling Thunder", "team-4": "Fourth Wall"},
		DraftOrder:     []string{"team-3", "team-1", "team-2", "team-4", "team-5", "team-6", "team-7", "team-8"},
		Scoring:        map[string]float64{"passing_td": 6, "reception": 0.5, "fumble_lost": -2.5},
		DraftStarted:   true,
		DraftStartedAt: at(1, 16),
		Pickems: map[string]map[string]string{
			"one@example.com": {"2026-w01-KC-BAL": "KC", "2026-w01-DAL-PHI": "PHI"},
			"two@example.com": {},
		},
		PickemEnteredAt: map[string]time.Time{
			"one@example.com": at(1, 8),
		},
		BlitzEntries: map[string]map[string]BlitzEntry{
			"one@example.com": {
				"pre2": {Players: []string{"p-01", "p-02", "p-03", "p-04", "p-05"}, UpdatedAt: at(1, 9)},
				"pre3": {Players: []string{}, UpdatedAt: at(2, 9)},
			},
			"two@example.com": {},
		},
		ClockDeadline:     at(3, 18),
		ClockPaused:       true,
		ClockRemainingSec: 42,
		ClockDurationSec:  90,
		Autopick:          map[string]bool{"team-6": true, "team-7": true},
		SentLog: map[string]time.Time{
			"onclock:team-1:12":                  at(3, 12),
			"draftdone:abcd1234:one@example.com": at(3, 20),
			"waiver:clm-0001":                    at(4, 6),
		},
		NotifyPrefs: map[string]map[string]bool{
			"one@example.com": {"draft": true, "waivers": false},
			"two@example.com": {},
		},
		ScoringChangedAt: at(2, 11),
		Phase:            PhaseRegularSeason,
		BadgeClaims:      map[string]string{"team-1": "bolt", "team-2": "fireball"},
		// A valid custom ref exercises JSON import, cloneState, and the
		// SQLite round-trip without inventing an object-file fixture.
		AvatarRefs: map[string]string{"team-3": strings.Repeat("c", 64)},
		RosterOverride: &RosterOverride{
			Slots:   map[string]int{"QB": 1, "RB": 2, "WR": 3, "TE": 1, "FLEX": 1, "K": 1, "DST": 1},
			Bench:   6,
			Reserve: map[string]int{"QB": 1},
			IR:      2,
			Limits:  map[string]int{"QB": 3},
		},
		Announcements: []Announcement{
			{ID: "ann-aaaa1111", Body: "Draft moved to Sunday.", PostedAt: at(1, 10), PostedBy: "commissioner"},
			{ID: "ann-bbbb2222", Body: "Waivers run nightly.", PostedAt: at(1, 9), PostedBy: "commissioner"},
		},
		Lineups: map[string]map[int]map[string]string{
			"team-1": {
				1: {"QB": "p-06", "RB1": "p-02", "WR1": "p-01"},
				2: {"QB": "p-09"},
			},
			"team-2": {3: {}},
			"team-3": {},
		},
		WaiversProcessedThrough: at(4, 3),
		RosterZones: map[string]map[string]ZoneAssignment{
			"team-1": {
				"p-11": {Zone: zoneReserve, Position: "RB", PlacedAt: at(2, 14)},
				"p-12": {Zone: zoneIR, Position: "TE", PlacedAt: at(2, 15)},
			},
			"team-2": {},
		},
	}

	state.Picks = []DraftPick{
		{Number: 1, Round: 1, TeamID: "team-3", PlayerID: "p-01", MadeAt: at(1, 16), MadeBy: "manager"},
		{Number: 2, Round: 1, TeamID: "team-1", PlayerID: "p-02", MadeAt: at(1, 16), MadeBy: "auto"},
		{Number: 3, Round: 1, TeamID: "team-2", PlayerID: "p-03", MadeAt: at(1, 17), MadeBy: "commissioner"},
		{Number: 4, Round: 1, TeamID: "team-4", PlayerID: "p-04", MadeAt: at(1, 17)}, // no provenance: old data
	}

	player := func(id, name, pos, team string) TransactionPlayer {
		return TransactionPlayer{PlayerID: id, Name: name, Position: pos, NFLTeam: team}
	}
	state.Transactions = []Transaction{
		{ID: "txn-0001", Season: 2026, Week: 1, Type: "add", TeamID: "team-1",
			Adds: []TransactionPlayer{player("p-20", "Add One", "WR", "KC")}, By: "manager", At: at(2, 9)},
		{ID: "txn-0002", Season: 2026, Week: 1, Type: "drop", TeamID: "team-1",
			Drops: []TransactionPlayer{player("p-21", "Drop One", "RB", "SF")}, By: "manager",
			Note: "roster crunch", At: at(2, 10)},
		{ID: "txn-0003", Season: 2026, Week: 2, Type: "claim", TeamID: "team-2",
			Adds:  []TransactionPlayer{player("p-22", "Claim One", "TE", "LV")},
			Drops: []TransactionPlayer{player("p-23", "Claim Drop", "K", "DEN")},
			Bid:   17, Position: 3, By: "manager", At: at(3, 4)},
		{ID: "txn-0004", Season: 2026, Week: 2, Type: "trade", TeamID: "team-1", OtherTeamID: "team-2",
			Adds:    []TransactionPlayer{player("p-24", "Trade In", "WR", "MIA")},
			Drops:   []TransactionPlayer{player("p-25", "Trade Out", "RB", "NYJ")},
			OfferID: "trd-executed", By: "manager", At: at(3, 12)},
		{ID: "txn-0005", Season: 2026, Week: 3, Type: "auto-drop", TeamID: "team-3",
			Drops: []TransactionPlayer{player("p-26", "Healed IR", "QB", "GB")}, By: "system", At: at(4, 5)},
	}

	state.WaiverClaims = []WaiverClaim{
		{ID: "clm-0001", TeamID: "team-1", AddID: "p-30", DropID: "p-31", Bid: 12, Priority: 1, FiledAt: at(4, 1)},
		{ID: "clm-0002", TeamID: "team-2", AddID: "p-30", Priority: 1, FiledAt: at(4, 2)},
	}
	state.WaiverReceipts = []WaiverReceipt{{
		ClaimID: "clm-resolved", Season: 2026, Week: 2, TeamID: "team-1",
		Add:   player("p-27", "Receipt Add", "WR", "MIA"),
		Drops: []TransactionPlayer{player("p-28", "Receipt Drop", "RB", "NYJ")},
		Bid:   11, SubmittedPriority: 2, WaiverPosition: 4, WaiverTeamCount: 8,
		Mode: "faab", Outcome: "beaten", WinningTeamID: "team-2", WinningBid: 17, WinningBidKnown: true,
		Reason:  "Another team acquired this player before this claim resolved.",
		FiledAt: at(3, 21), ResolvedAt: at(4, 3),
	}}

	offer := func(id, status string, resolved bool) TradeOffer {
		o := TradeOffer{
			ID: id, FromTeamID: "team-1", ToTeamID: "team-2",
			Give: []string{"p-40"}, Get: []string{"p-41"}, Picks: []string{},
			Status: status, Vetoes: []string{}, CreatedAt: at(3, 8),
		}
		if resolved {
			o.ResolvedAt = at(3, 9)
		}
		return o
	}
	accepted := offer("trd-accepted", TradeStatusAccepted, false)
	accepted.AcceptedAt = at(3, 9)
	accepted.Vetoes = []string{"team-5"}
	executed := offer("trd-executed", TradeStatusExecuted, true)
	executed.AcceptedAt = at(3, 10)
	failed := offer("trd-failed", TradeStatusFailed, true)
	failed.FailReason = "that player is no longer on the roster"
	countered := offer("trd-countered", TradeStatusCountered, true)
	counter := offer("trd-counter", TradeStatusOpen, false)
	counter.ParentID = "trd-countered"
	counter.Note = "how about this instead"
	state.TradeOffers = []TradeOffer{
		offer("trd-open", TradeStatusOpen, false),
		accepted,
		executed,
		offer("trd-declined", TradeStatusDeclined, true),
		offer("trd-withdrawn", TradeStatusWithdrawn, true),
		countered,
		counter,
		offer("trd-vetoed", TradeStatusVetoed, true),
		offer("trd-expired", TradeStatusExpired, true),
		failed,
	}

	schedule, err := GenerateSchedule(ScheduleParams{
		Season: 2026, TeamIDs: defaultTeamIDs(), StartWeek: 1, Weeks: 3, Seed: 7,
	})
	if err != nil {
		panic(err)
	}
	schedule.GeneratedAt = at(1, 8)
	for i := range schedule.Weeks[0].Matchups {
		schedule.Weeks[0].Matchups[i].Final = true
		schedule.Weeks[0].Matchups[i].HomeScore = 101.5
		schedule.Weeks[0].Matchups[i].AwayScore = 99.25
	}
	state.Schedule = &schedule

	state.Playoffs = &PlayoffState{
		Config: PlayoffConfig{TeamCount: 4, StartWeek: 15, RoundLengthWeeks: 1},
		Seeds: []PlayoffSeed{
			{Seed: 1, TeamID: "team-1"}, {Seed: 2, TeamID: "team-2"},
			{Seed: 3, TeamID: "team-3"}, {Seed: 4, TeamID: "team-4"},
		},
		Matchups: []PlayoffMatchup{
			{ID: "po-r1-1", Round: 1, HomeTeamID: "team-1", AwayTeamID: "team-4"},
			{ID: "po-r1-2", Round: 1, HomeTeamID: "team-2", AwayTeamID: "team-3"},
		},
		ChampionTeamID: "",
	}
	return state
}

// TestImportRoundTripsRealisticState is the migration bar: a state file
// exercising every collection imports into SQLite, reads back deep-equal,
// and leaves the original bytes on disk under the .imported name.
func TestImportRoundTripsRealisticState(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "league-state.json")
	fixture := realisticFixture()
	raw, err := json.MarshalIndent(fixture, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	store := NewStore(path)
	t.Cleanup(func() { _ = store.Close() })
	if err := store.StartupError(); err != nil {
		t.Fatalf("importing a realistic state file must succeed: %v", err)
	}

	want := cloneState(fixture)
	got := store.Snapshot()
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("the imported state does not deep-equal the file's state:\n%s", diffStates(t, want, got))
	}

	// The original bytes survive under the new name, untouched.
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("the imported state file must be renamed, not left in place (err = %v)", err)
	}
	kept, err := os.ReadFile(path + importedSuffix)
	if err != nil {
		t.Fatalf("the original state file must survive: %v", err)
	}
	if string(kept) != string(raw) {
		t.Error("the imported state file's bytes must be untouched")
	}

	// A restart reads the same state back out of the database alone.
	reloaded := reloadStoredState(t, path)
	if !reflect.DeepEqual(want, reloaded) {
		t.Fatalf("the reopened state does not deep-equal the imported state:\n%s", diffStates(t, want, reloaded))
	}
}

// diffStates renders the first differing collection of two states, so a
// failure names the field instead of dumping the whole model twice.
func diffStates(t *testing.T, want, got PersistedState) string {
	t.Helper()
	wantValue := reflect.ValueOf(want)
	gotValue := reflect.ValueOf(got)
	var b strings.Builder
	for i := 0; i < wantValue.NumField(); i++ {
		if !wantValue.Field(i).CanInterface() {
			continue
		}
		if reflect.DeepEqual(wantValue.Field(i).Interface(), gotValue.Field(i).Interface()) {
			continue
		}
		fmt.Fprintf(&b, "field %s:\n  want %+v\n  got  %+v\n",
			wantValue.Type().Field(i).Name, wantValue.Field(i).Interface(), gotValue.Field(i).Interface())
	}
	if b.Len() == 0 {
		return "no field differs (the difference is in an unexported detail)"
	}
	return b.String()
}

// TestImportRefusesTooNewFileAndLeavesNoDatabase checks the failure mode:
// a state file this binary cannot read blocks startup, keeps the file
// exactly as it was, and leaves no half-built database behind for the
// next boot to mistake for the record of truth.
func TestImportRefusesTooNewFileAndLeavesNoDatabase(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "league-state.json")
	body := fmt.Sprintf(`{"schemaVersion": %d, "picks": []}`, currentSchemaVersion+1)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	store := NewStore(path)
	err := store.StartupError()
	if err == nil {
		t.Fatal("a too-new state file must refuse startup")
	}
	if !errors.Is(err, errSchemaTooNew) {
		t.Errorf("StartupError = %v, want a schema-too-new error", err)
	}
	if err := store.SetTeamName("team-1", "Nope"); err == nil {
		t.Error("every write must fail while the startup error is set")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the state file must be left untouched: %v", err)
	}
	if string(raw) != body {
		t.Errorf("the state file's bytes changed:\ngot:  %s\nwant: %s", raw, body)
	}
	if _, err := os.Stat(filepath.Join(dir, dbFileName)); !os.IsNotExist(err) {
		t.Errorf("a refused import must leave no database behind (err = %v)", err)
	}
}

// TestImportRefusesMalformedFile checks the other refusal: a state file
// that is not JSON is never silently replaced by blank state.
func TestImportRefusesMalformedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "league-state.json")
	if err := os.WriteFile(path, []byte("{this is not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewStore(path)
	if store.StartupError() == nil {
		t.Fatal("a malformed state file must refuse startup")
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("the malformed file must stay in place: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, dbFileName)); !os.IsNotExist(err) {
		t.Errorf("a refused import must leave no database behind (err = %v)", err)
	}
}

// TestImportRunsOnlyOnce checks that a state file dropped back into the
// data directory after a completed import never overwrites the database:
// once the database exists it is the record of truth.
func TestImportRunsOnlyOnce(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "league-state.json")
	if err := os.WriteFile(path, []byte(`{"schemaVersion":2,"teamNames":{"team-1":"From File"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	first := NewStore(path)
	if err := first.StartupError(); err != nil {
		t.Fatal(err)
	}
	if got := first.Snapshot().TeamNames["team-1"]; got != "From File" {
		t.Fatalf("imported team name = %q, want From File", got)
	}
	if err := first.SetTeamName("team-1", "From Database"); err != nil {
		t.Fatal(err)
	}
	_ = first.Close()

	if err := os.WriteFile(path, []byte(`{"schemaVersion":2,"teamNames":{"team-1":"Stale File"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	second := NewStore(path)
	t.Cleanup(func() { _ = second.Close() })
	if err := second.StartupError(); err != nil {
		t.Fatal(err)
	}
	if got := second.Snapshot().TeamNames["team-1"]; got != "From Database" {
		t.Fatalf("team name = %q, want From Database (the stale file must not import twice)", got)
	}
}

func writeUnmarkedLegacyDatabase(t *testing.T, path string, state PersistedState) []byte {
	t.Helper()
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	db, err := openDB(filepath.Join(filepath.Dir(path), dbFileName))
	if err != nil {
		t.Fatal(err)
	}
	if err := migrateDB(db); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	normalizeState(&state)
	state.SchemaVersion = currentSchemaVersion
	legacy := &Store{db: db, state: state, shadow: shadowIndex{}}
	for _, id := range allCollections() {
		legacy.dirty |= 1 << uint(id)
	}
	if err := legacy.writeDirtyLocked(); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestImportAuthorityMarkerCommitsWithImportedState(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "league-state.json")
	state := PersistedState{SchemaVersion: currentSchemaVersion, TeamNames: map[string]string{"team-1": "Imported"}}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewStore(path)
	t.Cleanup(func() { _ = store.Close() })
	if err := store.StartupError(); err != nil {
		t.Fatal(err)
	}
	var marker string
	if err := store.db.QueryRow(`SELECT "value" FROM kv WHERE "key" = ?`, kvPersistenceAuthority).Scan(&marker); err != nil {
		t.Fatal(err)
	}
	if marker != legacyAuthorityMarker(raw) {
		t.Fatalf("authority marker = %q, want %q", marker, legacyAuthorityMarker(raw))
	}
	if got := store.Snapshot().TeamNames["team-1"]; got != "Imported" {
		t.Fatalf("imported state = %q, want Imported", got)
	}
}

func TestLegacyImportRecoveryAfterCommitBeforeRename(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "league-state.json")
	raw := writeUnmarkedLegacyDatabase(t, path, PersistedState{
		SchemaVersion: currentSchemaVersion,
		TeamNames:     map[string]string{"team-1": "Committed"},
	})
	store := NewStore(path)
	t.Cleanup(func() { _ = store.Close() })
	if err := store.StartupError(); err != nil {
		t.Fatal(err)
	}
	if got := store.Snapshot().TeamNames["team-1"]; got != "Committed" {
		t.Fatalf("recovered team name = %q, want Committed", got)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("recovered source file still exists: %v", err)
	}
	kept, err := os.ReadFile(path + importedSuffix)
	if err != nil || !bytes.Equal(kept, raw) {
		t.Fatalf("recovered .imported bytes = %q, err %v; want original", kept, err)
	}
	var marker string
	if err := store.db.QueryRow(`SELECT "value" FROM kv WHERE "key" = ?`, kvPersistenceAuthority).Scan(&marker); err != nil {
		t.Fatal(err)
	}
	if marker != legacyAuthorityMarker(raw) {
		t.Fatalf("recovery marker = %q, want %q", marker, legacyAuthorityMarker(raw))
	}
}

func TestLegacyImportRecoveryRejectsDivergentFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "league-state.json")
	_ = writeUnmarkedLegacyDatabase(t, path, PersistedState{
		SchemaVersion: currentSchemaVersion,
		TeamNames:     map[string]string{"team-1": "Database"},
	})
	if err := os.WriteFile(path, []byte(`{"schemaVersion":3,"teamNames":{"team-1":"Divergent file"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewStore(path)
	t.Cleanup(func() { _ = store.Close() })
	if err := store.StartupError(); err == nil {
		t.Fatal("divergent JSON must refuse startup")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("divergent JSON was removed: %v", err)
	}
	db, err := openDB(filepath.Join(dir, dbFileName))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	stored, err := loadStateFromDB(db)
	if err != nil {
		t.Fatal(err)
	}
	if got := stored.TeamNames["team-1"]; got != "Database" {
		t.Fatalf("database after divergent recovery = %q, want Database", got)
	}
}

func TestLogicalSchemaTooNewRefusesBeforePendingMigration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "league-state.json")
	dbPath := filepath.Join(dir, dbFileName)
	db, err := openDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := migrateDB(db); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT OR REPLACE INTO kv (key, value) VALUES (?, ?)`, kvSchemaVersion, currentSchemaVersion+1); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if _, err := db.Exec(fmt.Sprintf("PRAGMA user_version = %d", currentDBVersion-1)); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	store := NewStore(path)
	t.Cleanup(func() { _ = store.Close() })
	if err := store.StartupError(); !errors.Is(err, errSchemaTooNew) {
		t.Fatalf("StartupError = %v, want logical schema-too-new before migration", err)
	}
	db, err = openDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var version string
	if err := db.QueryRow(`SELECT "value" FROM kv WHERE "key" = ?`, kvSchemaVersion).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != fmt.Sprint(currentSchemaVersion+1) {
		t.Fatalf("logical schema marker after refusal = %q, want %d", version, currentSchemaVersion+1)
	}
}

func TestAbandonPreservesCommittedAuthorityAfterPostCommitFailure(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, dbFileName)
	db, err := openDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := migrateDB(db); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO kv (key, value) VALUES (?, ?)`, kvPersistenceAuthority, authorityNative); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	store := &Store{db: db, dbPath: dbPath}
	want := errors.New("post-commit directory barrier failed")
	if got := store.abandonLocked(true, want); !errors.Is(got, want) {
		t.Fatalf("abandon error = %v, want %v", got, want)
	}
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("committed authoritative database was removed: %v", err)
	}
	reopened, err := openDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	marker, err := readPersistenceAuthority(reopened)
	if err != nil || marker != authorityNative {
		t.Fatalf("retained authority marker = %q, err %v; want %q", marker, err, authorityNative)
	}
}

func TestImportRetainsCommittedDatabaseWhenRenameSyncFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "league-state.json")
	raw := []byte(`{"schemaVersion":3,"teamNames":{"team-1":"Durable import"}}`)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	originalSync := syncImportDirectoryHook
	syncErr := errors.New("injected import directory sync failure")
	syncImportDirectoryHook = func(string) error { return syncErr }
	t.Cleanup(func() { syncImportDirectoryHook = originalSync })
	failed := NewStore(path)
	if err := failed.StartupError(); !errors.Is(err, syncErr) {
		t.Fatalf("post-rename sync StartupError = %v, want %v", err, syncErr)
	}
	if _, err := os.Stat(filepath.Join(dir, dbFileName)); err != nil {
		t.Fatalf("committed database was removed after rename sync failure: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("source file after successful rename = err %v, want absent", err)
	}
	if _, err := os.Stat(path + importedSuffix); err != nil {
		t.Fatalf("imported backup after successful rename = %v", err)
	}
	_ = failed.Close()
	syncImportDirectoryHook = originalSync

	restarted := NewStore(path)
	t.Cleanup(func() { _ = restarted.Close() })
	if err := restarted.StartupError(); err != nil {
		t.Fatalf("restart after post-rename sync failure = %v", err)
	}
	if got := restarted.Snapshot().TeamNames["team-1"]; got != "Durable import" {
		t.Fatalf("restart team name = %q, want Durable import", got)
	}
}

func TestV1SQLiteMigratesToV2WithCanonicalIdentityOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "league-state.json")
	dbPath := filepath.Join(dir, dbFileName)
	db, err := openDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if err := migrate001Initial(tx); err != nil {
		_ = tx.Rollback()
		_ = db.Close()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if _, err := db.Exec(`PRAGMA user_version = 1`); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO badge_claims (team_id, motif) VALUES ('team-1', 'fireball')`); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	for _, args := range [][]any{
		{0, "legacy-late", "team-1", "p-2", "", 0, 9, "2026-09-01T10:00:00Z"},
		{1, "legacy-early", "team-1", "p-1", "", 0, 9, "2026-09-01T09:00:00Z"},
	} {
		if _, err := db.Exec(`INSERT INTO waiver_claims (ord, id, team_id, add_id, drop_id, bid, priority, filed_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, args...); err != nil {
			_ = db.Close()
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	store := NewStore(path)
	if err := store.StartupError(); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	var gotVersion int
	if err := store.db.QueryRow(`PRAGMA user_version`).Scan(&gotVersion); err != nil || gotVersion != currentDBVersion {
		_ = store.Close()
		t.Fatalf("migrated user_version = %d (err %v), want %d", gotVersion, err, currentDBVersion)
	}
	var tableName string
	if err := store.db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'avatar_refs'`).Scan(&tableName); err != nil {
		_ = store.Close()
		t.Fatalf("avatar_refs table missing after v1->v2 migration: %v", err)
	}
	got := store.Snapshot()
	if got.BadgeClaims["team-1"] != "fireball" {
		_ = store.Close()
		t.Fatalf("migrated canonical badge = %q, want fireball", got.BadgeClaims["team-1"])
	}
	if len(got.AvatarRefs) != 0 {
		_ = store.Close()
		t.Fatalf("v1 migration invented avatar refs: %#v", got.AvatarRefs)
	}
	claimPriority := map[string]int{}
	for _, claim := range got.WaiverClaims {
		claimPriority[claim.ID] = claim.Priority
	}
	if len(got.WaiverClaims) != 2 || claimPriority["legacy-early"] != 1 || claimPriority["legacy-late"] != 2 {
		_ = store.Close()
		t.Fatalf("migrated claim priorities = %+v, want deterministic 1..2 order", got.WaiverClaims)
	}
	var receiptTable string
	if err := store.db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'waiver_receipts'`).Scan(&receiptTable); err != nil {
		_ = store.Close()
		t.Fatalf("waiver_receipts table missing after migration: %v", err)
	}
	want := got
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reloaded := NewStore(path)
	t.Cleanup(func() { _ = reloaded.Close() })
	if err := reloaded.StartupError(); err != nil {
		t.Fatal(err)
	}
	if got := reloaded.Snapshot(); !reflect.DeepEqual(got, want) {
		t.Fatalf("reloaded v1->v2 state differs:\n%s", diffStates(t, want, got))
	}
}

func TestV6SQLiteMigratesWaiverReceiptsToV7(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), dbFileName)
	db, err := openDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// DB user_version 5 is logical state schema 6: Pick'em entry authority
	// exists, while waiver receipts and canonical private claim order do not.
	for step := 0; step < 5; step++ {
		tx, err := db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		if err := dbMigrations[step](tx); err != nil {
			_ = tx.Rollback()
			t.Fatal(err)
		}
		if _, err := tx.Exec("PRAGMA user_version = " + strconv.Itoa(step+1)); err != nil {
			_ = tx.Rollback()
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]any{
		{0, "late", "team-1", "p-2", "", 0, 9, "2026-09-01T10:00:00Z"},
		{1, "early", "team-1", "p-1", "", 0, 9, "2026-09-01T09:00:00Z"},
	} {
		if _, err := db.Exec(`INSERT INTO waiver_claims (ord, id, team_id, add_id, drop_id, bid, priority, filed_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, args...); err != nil {
			t.Fatal(err)
		}
	}

	if err := migrateDB(db); err != nil {
		t.Fatal(err)
	}
	// migrateDB always runs every pending step, not only migrate006: since
	// the 2026-08-30 review added migrate007WaiverClaimDeferral and its
	// round-2 follow-up migrate008WaiverClaimDeferralTiming, a fixture
	// seeded at user_version 5 lands at currentDBVersion (8), not 6.
	var userVersion int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&userVersion); err != nil || userVersion != currentDBVersion {
		t.Fatalf("user_version = %d (err %v), want %d", userVersion, err, currentDBVersion)
	}
	var logicalVersion string
	if err := db.QueryRow(`SELECT value FROM kv WHERE key = ?`, kvSchemaVersion).Scan(&logicalVersion); err != nil || logicalVersion != "9" {
		t.Fatalf("logical schema = %q (err %v), want 9", logicalVersion, err)
	}
	var receiptTable string
	if err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'waiver_receipts'`).Scan(&receiptTable); err != nil {
		t.Fatalf("waiver_receipts table missing after v6->v7 migration: %v", err)
	}
	rows, err := db.Query(`SELECT id, priority FROM waiver_claims ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	priorities := map[string]int{}
	for rows.Next() {
		var id string
		var priority int
		if err := rows.Scan(&id, &priority); err != nil {
			t.Fatal(err)
		}
		priorities[id] = priority
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if priorities["early"] != 1 || priorities["late"] != 2 {
		t.Fatalf("normalized priorities = %#v, want early=1 late=2", priorities)
	}
}

func TestLogicalSchemaTooNewRefusesBeforeIdentityRepair(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "league-state.json")
	dbPath := filepath.Join(dir, dbFileName)
	db, err := openDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := migrateDB(db); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	var pragmaVersion int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&pragmaVersion); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if pragmaVersion != currentDBVersion {
		_ = db.Close()
		t.Fatalf("fixture user_version = %d, want supported version %d", pragmaVersion, currentDBVersion)
	}
	if _, err := db.Exec(`PRAGMA ignore_check_constraints = ON`); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT OR REPLACE INTO avatar_refs (team_id, ref) VALUES ('team-4', 'not-a-ref')`); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT OR REPLACE INTO badge_claims (team_id, motif) VALUES ('team-4', 'flame')`); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT OR REPLACE INTO kv (key, value) VALUES (?, ?)`, kvSchemaVersion, currentSchemaVersion+1); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}

	readRows := func(open *sql.DB) (avatars, badges []string) {
		rows, err := open.Query(`SELECT "team_id", "ref" FROM avatar_refs ORDER BY "team_id", "ref"`)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var teamID, ref string
			if err := rows.Scan(&teamID, &ref); err != nil {
				_ = rows.Close()
				t.Fatal(err)
			}
			avatars = append(avatars, teamID+"\x00"+ref)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			t.Fatal(err)
		}
		if err := rows.Close(); err != nil {
			t.Fatal(err)
		}
		rows, err = open.Query(`SELECT "team_id", "motif" FROM badge_claims ORDER BY "team_id", "motif"`)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var teamID, motif string
			if err := rows.Scan(&teamID, &motif); err != nil {
				_ = rows.Close()
				t.Fatal(err)
			}
			badges = append(badges, teamID+"\x00"+motif)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			t.Fatal(err)
		}
		if err := rows.Close(); err != nil {
			t.Fatal(err)
		}
		return avatars, badges
	}
	beforeAvatars, beforeBadges := readRows(db)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	reopened := NewStore(path)
	if err := reopened.StartupError(); !errors.Is(err, errSchemaTooNew) {
		_ = reopened.Close()
		t.Fatalf("logical schema startup error = %v, want errSchemaTooNew", err)
	}
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}

	db, err = openDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	afterAvatars, afterBadges := readRows(db)
	if !reflect.DeepEqual(afterAvatars, beforeAvatars) || !reflect.DeepEqual(afterBadges, beforeBadges) {
		t.Fatalf("logical schema refusal repaired identity rows: avatars before=%#v after=%#v; badges before=%#v after=%#v",
			beforeAvatars, afterAvatars, beforeBadges, afterBadges)
	}
}

// TestDatabaseRefusesNewerUserVersion is the old-binary safety check: a
// database written by a newer binary (a higher PRAGMA user_version)
// refuses to open and refuses every write, so a rollback to this binary
// can never half-read or clobber it.
func TestDatabaseRefusesNewerUserVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "league-state.json")
	store := NewStore(path)
	if err := store.SetTeamName("team-1", "Before"); err != nil {
		t.Fatal(err)
	}
	_ = store.Close()

	dbPath := filepath.Join(dir, dbFileName)
	db, err := openDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(fmt.Sprintf("PRAGMA user_version = %d", currentDBVersion+1)); err != nil {
		t.Fatal(err)
	}
	if got, err := dbUserVersion(db); err != nil || got != currentDBVersion+1 {
		t.Fatalf("user_version = %d (err %v), want %d", got, err, currentDBVersion+1)
	}
	_ = db.Close()

	reopened := NewStore(path)
	t.Cleanup(func() { _ = reopened.Close() })
	err = reopened.StartupError()
	if err == nil {
		t.Fatal("a database from a newer binary must refuse startup")
	}
	if !errors.Is(err, errSchemaTooNew) {
		t.Errorf("StartupError = %v, want a schema-too-new error", err)
	}
	if err := reopened.SetTeamName("team-1", "After"); err == nil {
		t.Error("every write must fail while the startup error is set")
	}

	// Put the version back and check the stored row was never touched.
	db, err = openDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(fmt.Sprintf("PRAGMA user_version = %d", currentDBVersion)); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	if got := reloadStoredState(t, path).TeamNames["team-1"]; got != "Before" {
		t.Fatalf("stored team name = %q, want Before (the refused binary must not have written)", got)
	}
}

// TestPersistFailureLeavesStoredStateIntact checks that a write that
// fails inside its transaction commits nothing at all: the stored state
// still reads exactly as it did before the attempt.
func TestPersistFailureLeavesStoredStateIntact(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "league-state.json")
	store := NewStore(path)
	store.draftLifecycleBypass = true
	t.Cleanup(func() { _ = store.Close() })
	now := time.Date(2026, 8, 22, 16, 0, 0, 0, time.UTC)
	if _, err := store.MakePick(teamOnClock(nil, 1), "p-01", "manager", now, time.Time{}); err != nil {
		t.Fatal(err)
	}

	failThisStorePersist(store)
	if _, err := store.MakePick(teamOnClock(nil, 2), "p-02", "manager", now, time.Time{}); !errors.Is(err, errInjectedPersist) {
		t.Fatalf("MakePick error = %v, want the injected persist failure", err)
	}

	stored := reloadStoredState(t, path)
	if len(stored.Picks) != 1 {
		t.Fatalf("stored picks = %d, want 1 (the failed write must commit nothing)", len(stored.Picks))
	}
	if stored.Picks[0].PlayerID != "p-01" {
		t.Fatalf("stored pick = %q, want p-01", stored.Picks[0].PlayerID)
	}

	// The live store's own in-memory copy must agree with disk: a caller
	// told MakePick failed must never find the rejected pick still
	// recorded in Snapshot(), and the next pick attempt must see the same
	// on-clock team and pick number this one did (draft-concurrency
	// review finding: MakePick used to append to Picks before persisting
	// and never rolled the append back on a persist error).
	inMemory := store.Snapshot()
	if len(inMemory.Picks) != 1 {
		t.Fatalf("in-memory picks after a failed MakePick = %d, want 1 (the failed pick must not stick in memory)", len(inMemory.Picks))
	}
}

// TestMakePickRollsBackOnPersistFailure is the direct regression test for
// the finding above: MakePick must undo its own append to Picks and its
// own ClockDeadline write when persistLocked fails, mirroring the
// FirstSend/FirstSendBatch rollback precedent (see those tests). Without
// the rollback, the pick count and on-clock team silently advance in
// memory even though the caller was told the pick failed.
func TestMakePickRollsBackOnPersistFailure(t *testing.T) {
	store := newTestStore(t)
	now := time.Date(2026, 8, 22, 16, 0, 0, 0, time.UTC)
	team1 := teamOnClock(nil, 1)
	staleDeadline := now.Add(90 * time.Second)
	if err := store.ArmClock(staleDeadline); err != nil {
		t.Fatal(err)
	}

	failThisStorePersist(store)
	nextDeadline := now.Add(90 * time.Second)
	if _, err := store.MakePick(team1, "p-01", "manager", now, nextDeadline); !errors.Is(err, errInjectedPersist) {
		t.Fatalf("MakePick error = %v, want the injected persist failure", err)
	}

	state := store.Snapshot()
	if len(state.Picks) != 0 {
		t.Fatalf("picks after a failed persist = %d, want 0 (the append must roll back)", len(state.Picks))
	}
	if !state.ClockDeadline.Equal(staleDeadline) {
		t.Fatalf("ClockDeadline after a failed persist = %v, want the pre-pick deadline %v", state.ClockDeadline, staleDeadline)
	}

	// A retry with the store healthy again must land pick 1 for team1,
	// exactly as if the failed attempt never happened.
	store.mu.Lock()
	store.persistHook = nil
	store.mu.Unlock()
	pick, err := store.MakePick(team1, "p-01", "manager", now, nextDeadline)
	if err != nil {
		t.Fatalf("retry after rollback: %v", err)
	}
	if pick.Number != 1 || pick.TeamID != team1 {
		t.Fatalf("retry pick = %+v, want number 1 for %s", pick, team1)
	}
}

// TestAutoPickRollsBackOnPersistFailure is AutoPick's counterpart to
// TestMakePickRollsBackOnPersistFailure: the same rollback rule applies to
// the clock-driven pick path.
func TestAutoPickRollsBackOnPersistFailure(t *testing.T) {
	store := newTestStore(t)
	now := time.Date(2026, 8, 22, 16, 0, 0, 0, time.UTC)
	team1 := teamOnClock(nil, 1)
	deadline := now.Add(90 * time.Second)
	if err := store.ArmClock(deadline); err != nil {
		t.Fatal(err)
	}

	failThisStorePersist(store)
	if _, err := store.AutoPick(team1, "p-01", "auto", 1, deadline, now, now.Add(90*time.Second)); !errors.Is(err, errInjectedPersist) {
		t.Fatalf("AutoPick error = %v, want the injected persist failure", err)
	}

	state := store.Snapshot()
	if len(state.Picks) != 0 {
		t.Fatalf("picks after a failed persist = %d, want 0 (the append must roll back)", len(state.Picks))
	}
	if !state.ClockDeadline.Equal(deadline) {
		t.Fatalf("ClockDeadline after a failed persist = %v, want the pre-pick deadline %v", state.ClockDeadline, deadline)
	}
}

// TestUndoLastPickRollsBackOnPersistFailure checks the same rule on the
// removal path: a failed persist must not leave a pick "gone" in memory
// while the commissioner was told the undo failed, or a retry on the
// resulting stale count would drop a second pick instead of retrying the
// same one.
func TestUndoLastPickRollsBackOnPersistFailure(t *testing.T) {
	store := newTestStore(t)
	now := time.Date(2026, 8, 22, 16, 0, 0, 0, time.UTC)
	team1 := teamOnClock(nil, 1)
	if _, err := store.MakePick(team1, "p-01", "manager", now, now.Add(90*time.Second)); err != nil {
		t.Fatal(err)
	}

	failThisStorePersist(store)
	if err := store.UndoLastPick(now.Add(90 * time.Second)); !errors.Is(err, errInjectedPersist) {
		t.Fatalf("UndoLastPick error = %v, want the injected persist failure", err)
	}

	state := store.Snapshot()
	if len(state.Picks) != 1 {
		t.Fatalf("picks after a failed undo persist = %d, want 1 (the removal must roll back)", len(state.Picks))
	}
	if state.Picks[0].PlayerID != "p-01" {
		t.Fatalf("surviving pick = %q, want p-01", state.Picks[0].PlayerID)
	}
}

// TestPanicMidTransactionKeepsPreMutationState checks the other crash
// shape: a panic raised inside the write transaction. The transaction
// rolls back while the panic unwinds, the lock is released, and both the
// live store and a fresh one read the pre-mutation state.
func TestPanicMidTransactionKeepsPreMutationState(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "league-state.json")
	store := NewStore(path)
	t.Cleanup(func() { _ = store.Close() })
	if err := store.SetTeamName("team-1", "Before"); err != nil {
		t.Fatal(err)
	}

	store.mu.Lock()
	store.persistHook = func() error { panic("crash inside the persist transaction") }
	store.mu.Unlock()

	func() {
		defer func() {
			if recover() == nil {
				t.Error("the injected panic must reach the caller")
			}
		}()
		_ = store.SetTeamName("team-2", "Never Committed")
	}()

	store.mu.Lock()
	store.persistHook = nil
	store.mu.Unlock()

	stored := reloadStoredState(t, path)
	if got := stored.TeamNames["team-1"]; got != "Before" {
		t.Fatalf("stored team-1 name = %q, want Before", got)
	}
	if got, exists := stored.TeamNames["team-2"]; exists {
		t.Fatalf("stored team-2 name = %q, want absent", got)
	}
	// The store still works: the lock was released, and the next write
	// covers everything the failed one left marked. The in-memory model
	// still carries the name the failed write set — no store method has
	// ever rolled a mutation back out of memory except FirstSend — so
	// that name reaches the database on this retry. The dirty mark
	// surviving the failure is what makes the retry complete.
	if err := store.SetTeamName("team-3", "After"); err != nil {
		t.Fatalf("the store must still write after a rolled-back panic: %v", err)
	}
	stored = reloadStoredState(t, path)
	if got := stored.TeamNames["team-3"]; got != "After" {
		t.Fatalf("stored team-3 name = %q, want After", got)
	}
	if got := stored.TeamNames["team-2"]; got != "Never Committed" {
		t.Fatalf("stored team-2 name = %q, want the retry to carry the still-marked change", got)
	}
}

// TestCrashMidTransactionKeepsPreMutationState kills a real process with
// its write transaction open, then reopens the database and checks the
// state is exactly what the last committed write left. This is the
// durability claim the whole migration exists for, tested with an actual
// SIGKILL rather than a simulated one.
func TestCrashMidTransactionKeepsPreMutationState(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "league-state.json")

	cmd := exec.Command(os.Args[0], "-test.run=TestCrashHelperProcess", "-test.v")
	cmd.Env = append(os.Environ(), "GRIDIRON_CRASH_HELPER=1", "GRIDIRON_CRASH_PATH="+path)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("the helper process must not exit cleanly; output:\n%s", out)
	}
	if !strings.Contains(string(out), "helper: committed the pre-crash write") {
		t.Fatalf("the helper never committed its first write; output:\n%s", out)
	}
	// A signal-terminated process reports exit code -1. Anything else
	// means the helper returned by itself, so the test would be proving
	// nothing about a crash.
	if code := cmd.ProcessState.ExitCode(); code != -1 {
		t.Fatalf("the helper exited with code %d, want death by signal; output:\n%s", code, out)
	}

	stored := reloadStoredState(t, path)
	if got := stored.TeamNames["team-1"]; got != "Survives" {
		t.Fatalf("stored team name = %q, want Survives (the committed write must survive the crash)", got)
	}
	if got, exists := stored.TeamNames["team-2"]; exists {
		t.Fatalf("team-2 name = %q, want absent (the crashed write must not commit)", got)
	}
	if len(stored.Picks) != 1 || stored.Picks[0].PlayerID != "p-01" {
		t.Fatalf("stored picks = %+v, want only the committed pick p-01", stored.Picks)
	}
}

// TestCrashHelperProcess is the child half of
// TestCrashMidTransactionKeepsPreMutationState. It commits one write,
// then dies inside the next write's transaction.
func TestCrashHelperProcess(t *testing.T) {
	if os.Getenv("GRIDIRON_CRASH_HELPER") != "1" {
		t.Skip("helper process for TestCrashMidTransactionKeepsPreMutationState")
	}
	path := os.Getenv("GRIDIRON_CRASH_PATH")
	store := NewStore(path)
	store.draftLifecycleBypass = true
	if err := store.StartupError(); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 22, 16, 0, 0, 0, time.UTC)
	if err := store.SetTeamName("team-1", "Survives"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.MakePick(teamOnClock(nil, 1), "p-01", "manager", now, time.Time{}); err != nil {
		t.Fatal(err)
	}
	fmt.Println("helper: committed the pre-crash write")

	killThisStorePersist(store)
	_ = store.SetTeamName("team-2", "Never Committed")
	t.Fatal("helper: the process must not survive its own persist")
}

// TestConcurrentMutatorsAndSnapshots hammers the store with parallel
// writers of different collections and parallel readers, then checks that
// every write landed and that the stored state matches the in-memory one.
func TestConcurrentMutatorsAndSnapshots(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "league-state.json")
	store := NewStore(path)
	t.Cleanup(func() { _ = store.Close() })
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

	const rounds = 12
	var wg sync.WaitGroup
	fail := make(chan error, 64)
	run := func(fn func(i int) error) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < rounds; i++ {
				if err := fn(i); err != nil {
					fail <- err
					return
				}
			}
		}()
	}

	run(func(i int) error { return store.SetTeamName("team-1", fmt.Sprintf("Name %d", i)) })
	run(func(i int) error { return store.BoardAdd("one@example.com", fmt.Sprintf("p-%02d", i)) })
	run(func(i int) error { return store.SetPickem("two@example.com", fmt.Sprintf("g-%02d", i), "KC", now) })
	run(func(i int) error { return store.SetLineupSlot("team-2", 1+i, "QB", "p-09", now) })
	run(func(i int) error {
		_, err := store.FirstSend(fmt.Sprintf("kickoff:2026:%d@example.com", i), now)
		return err
	})
	run(func(i int) error {
		return store.SetNotifyPref("one@example.com", notificationPreferenceCategories[i%len(notificationPreferenceCategories)], i%2 == 0)
	})
	run(func(i int) error {
		_, err := store.PostAnnouncement(fmt.Sprintf("Announcement %d", i), "commissioner", now.Add(time.Duration(i)*time.Second))
		return err
	})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < rounds*20; i++ {
			snapshot := store.Snapshot()
			_ = len(snapshot.Picks) + len(snapshot.Boards) + len(snapshot.SentLog)
		}
	}()
	wg.Wait()
	close(fail)
	for err := range fail {
		t.Fatalf("concurrent mutation failed: %v", err)
	}

	live := store.Snapshot()
	if len(live.Boards["one@example.com"]) != rounds {
		t.Fatalf("board entries = %d, want %d", len(live.Boards["one@example.com"]), rounds)
	}
	if len(live.SentLog) != rounds {
		t.Fatalf("sent log entries = %d, want %d", len(live.SentLog), rounds)
	}
	stored := reloadStoredState(t, path)
	if !reflect.DeepEqual(live, stored) {
		t.Fatalf("the stored state does not match the in-memory state after the hammer:\n%s", diffStates(t, live, stored))
	}
}

// TestPersistWritesOnlyChangedRows pins the incremental-write contract:
// one more pick into a long draft writes its own row plus the clock, not
// the whole draft, and a no-op mutation writes nothing at all.
func TestPersistWritesOnlyChangedRows(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "league-state.json")
	store := NewStore(path)
	store.draftLifecycleBypass = true
	t.Cleanup(func() { _ = store.Close() })
	now := time.Date(2026, 8, 22, 16, 0, 0, 0, time.UTC)

	for i := 0; i < 40; i++ {
		team := teamOnClock(nil, i+1)
		if _, err := store.MakePick(team, fmt.Sprintf("p-%03d", i), "manager", now, now.Add(time.Minute)); err != nil {
			t.Fatalf("pick %d: %v", i+1, err)
		}
	}

	team := teamOnClock(nil, 41)
	if _, err := store.MakePick(team, "p-041", "manager", now, now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	// One pick row, plus the one kv row holding the clock deadline.
	if got := store.lastPersistRows; got != 2 {
		t.Errorf("a pick after 40 picks wrote %d rows, want 2 (its own row and the clock)", got)
	}

	if err := store.SetTeamName("team-1", "Same"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetTeamName("team-1", "Same"); err != nil {
		t.Fatal(err)
	}
	if got := store.lastPersistRows; got != 0 {
		t.Errorf("rewriting an unchanged name wrote %d rows, want 0", got)
	}
}

// TestRarelyExercisedMutatorsDeclareTheirCollections drives the three
// mutators the rest of the suite never reaches (ClearClock, BoardMoveTo,
// SetPlayoffs) with the read-back check forced on, so every persist call
// site in the package is proven to declare the collections it touches.
func TestRarelyExercisedMutatorsDeclareTheirCollections(t *testing.T) {
	was := sqlitePersistVerify
	sqlitePersistVerify = true
	t.Cleanup(func() { sqlitePersistVerify = was })

	dir := t.TempDir()
	path := filepath.Join(dir, "league-state.json")
	store := NewStore(path)
	store.draftLifecycleBypass = true
	t.Cleanup(func() { _ = store.Close() })
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

	if err := store.ArmClock(now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := store.ClearClock(); err != nil {
		t.Fatal(err)
	}
	if got := store.Snapshot().ClockDeadline; !got.IsZero() {
		t.Fatalf("ClockDeadline = %v, want the zero instant", got)
	}

	for _, playerID := range []string{"p-01", "p-02", "p-03"} {
		if err := store.BoardAdd("one@example.com", playerID); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.BoardMoveTo("one@example.com", "p-03", 0); err != nil {
		t.Fatal(err)
	}
	if got := store.Snapshot().Boards["one@example.com"][0]; got != "p-03" {
		t.Fatalf("board head = %q, want p-03", got)
	}

	bracket := PlayoffState{
		Config: PlayoffConfig{TeamCount: 2, StartWeek: 15, RoundLengthWeeks: 1},
		Seeds:  []PlayoffSeed{{Seed: 1, TeamID: "team-1"}, {Seed: 2, TeamID: "team-2"}},
		Matchups: []PlayoffMatchup{
			{ID: "po-final", Round: 1, HomeTeamID: "team-1", AwayTeamID: "team-2"},
		},
	}
	if err := store.SetPlayoffs(bracket); err != nil {
		t.Fatal(err)
	}

	stored := reloadStoredState(t, path)
	if !reflect.DeepEqual(store.Snapshot(), stored) {
		t.Fatalf("the stored state does not match the in-memory state:\n%s", diffStates(t, store.Snapshot(), stored))
	}
}

// ---------------------------------------------------------------------
// Benchmarks: persist latency for a representative mutation
// ---------------------------------------------------------------------

// benchStore builds a store carrying a full draft and a season's worth of
// log entries, so the benchmarks measure a persist against a realistic
// state rather than an empty one.
func benchStore(b *testing.B) (*Store, string) {
	b.Helper()
	dir := b.TempDir()
	path := filepath.Join(dir, "league-state.json")
	fixture := realisticFixture()
	// Grow the collections a persist has to walk to league-season size.
	for i := 0; i < 400; i++ {
		fixture.SentLog[fmt.Sprintf("onclock:team-%d:%d", i%8+1, i)] = time.Now().UTC()
	}
	for i := len(fixture.Picks); i < len(defaultTeams())*CurrentDraftRounds(); i++ {
		fixture.Picks = append(fixture.Picks, DraftPick{
			Number: i + 1, Round: i/len(defaultTeams()) + 1,
			TeamID: teamOnClock(fixture.DraftOrder, i+1), PlayerID: fmt.Sprintf("bp-%03d", i),
			MadeAt: time.Now().UTC(), MadeBy: "manager",
		})
	}
	raw, err := json.Marshal(fixture)
	if err != nil {
		b.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		b.Fatal(err)
	}
	store := NewStore(path)
	if err := store.StartupError(); err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = store.Close() })
	return store, path
}

// BenchmarkPersistPick measures the write path a live draft runs: append
// one pick, re-arm the clock, commit.
func BenchmarkPersistPick(b *testing.B) {
	sqlitePersistVerify = false
	defer func() { sqlitePersistVerify = true }()
	store, _ := benchStore(b)
	now := time.Now().UTC()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		store.mu.Lock()
		store.state.Picks = append(store.state.Picks, DraftPick{
			Number: len(store.state.Picks) + 1, Round: 1, TeamID: "team-1",
			PlayerID: fmt.Sprintf("bench-%d", i), MadeAt: now, MadeBy: "manager",
		})
		store.state.ClockDeadline = now.Add(time.Duration(i) * time.Second)
		err := store.persistLocked(colPicks, colScalars)
		store.mu.Unlock()
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkPersistTradeExecution measures the heaviest ordinary write: a
// trade's status change plus the transaction it appends plus both sides'
// zone clears.
func BenchmarkPersistTradeExecution(b *testing.B) {
	sqlitePersistVerify = false
	defer func() { sqlitePersistVerify = true }()
	store, _ := benchStore(b)
	now := time.Now().UTC()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		store.mu.Lock()
		store.state.TradeOffers[0].Status = TradeStatusExecuted
		store.state.TradeOffers[0].ResolvedAt = now.Add(time.Duration(i) * time.Second)
		store.state.Transactions = append(store.state.Transactions, Transaction{
			ID: fmt.Sprintf("txn-bench-%d", i), Season: 2026, Week: 3, Type: "trade",
			TeamID: "team-1", OtherTeamID: "team-2",
			Adds:  []TransactionPlayer{{PlayerID: "p-90", Name: "In", Position: "WR", NFLTeam: "KC"}},
			Drops: []TransactionPlayer{{PlayerID: "p-91", Name: "Out", Position: "RB", NFLTeam: "SF"}},
			By:    "manager", At: now,
		})
		err := store.persistLocked(colTradeOffers, colTransactions, colRosterZones)
		store.mu.Unlock()
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkPersistPickLegacyJSONFile is the "before" number: the JSON
// engine's whole-file persist, reproduced exactly (marshal the entire
// state, write a temporary file, rename over the state file) against the
// same fixture the SQLite benchmarks use. It is kept as a benchmark, not
// as production code, so the comparison stays reproducible.
//
// Note what it does not do: it never calls Sync. The old engine reported
// success as soon as the rename returned, with the file's contents still
// in the operating system's cache. That missing fsync is a large part of
// why it looks fast here and why it was not durable.
func BenchmarkPersistPickLegacyJSONFile(b *testing.B) {
	sqlitePersistVerify = false
	defer func() { sqlitePersistVerify = true }()
	store, _ := benchStore(b)
	dir := b.TempDir()
	path := filepath.Join(dir, "league-state.json")
	now := time.Now().UTC()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		store.mu.Lock()
		store.state.Picks = append(store.state.Picks, DraftPick{
			Number: len(store.state.Picks) + 1, Round: 1, TeamID: "team-1",
			PlayerID: fmt.Sprintf("bench-%d", i), MadeAt: now, MadeBy: "manager",
		})
		store.state.ClockDeadline = now.Add(time.Duration(i) * time.Second)
		raw, err := json.MarshalIndent(store.state, "", "  ")
		store.mu.Unlock()
		if err != nil {
			b.Fatal(err)
		}
		tmp, err := os.CreateTemp(dir, ".league-state-*.json")
		if err != nil {
			b.Fatal(err)
		}
		if err := tmp.Chmod(0o600); err != nil {
			b.Fatal(err)
		}
		if _, err := tmp.Write(raw); err != nil {
			b.Fatal(err)
		}
		if err := tmp.Close(); err != nil {
			b.Fatal(err)
		}
		if err := os.Rename(tmp.Name(), path); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkBackupSnapshot measures the rolling backup every draft pick
// takes before it mutates: one VACUUM INTO of the whole database.
func BenchmarkBackupSnapshot(b *testing.B) {
	sqlitePersistVerify = false
	defer func() { sqlitePersistVerify = true }()
	store, _ := benchStore(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		store.mu.Lock()
		err := store.backupSnapshotLocked()
		store.mu.Unlock()
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkSnapshot pins the read path, which never touches the database.
func BenchmarkSnapshot(b *testing.B) {
	store, _ := benchStore(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = store.Snapshot()
	}
}
