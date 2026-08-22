package league

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"gridiron-2000/internal/identity"
)

// ErrPersistenceIndeterminate means an identity transaction's commit result
// could not be reconciled with the authoritative database. Identity reads
// fail closed while this Store is unhealthy; the process should be restarted
// or repaired rather than guessing which badge/avatar pair won.
var ErrPersistenceIndeterminate = errors.New("avatar identity persistence outcome is indeterminate")

var ErrLeagueFull = errors.New("all league seats are already assigned")

// errStaleAutoPick means an optimistic re-validation inside AutoPick failed:
// a human pick, a pause, or a clock event raced in between the ticker's
// snapshot and the write. The caller drops the pick; the next tick
// re-evaluates from a fresh snapshot. See AutoPick.
var errStaleAutoPick = errors.New("auto-pick is stale")

// currentSchemaVersion is the state file schema version this binary writes
// and the highest version it accepts on load. See PersistedState's
// SchemaVersion doc comment and Store.load.
const currentSchemaVersion = 4

// errSchemaTooNew is returned by NewStore/load when the state file's
// SchemaVersion exceeds currentSchemaVersion: an older binary must not
// silently drop fields a newer one wrote (section 6.3).
var errSchemaTooNew = errors.New("state file schema version is newer than this binary supports")

// Store holds the league's authoritative state. The record of truth is a
// SQLite database, one file per league (data/league.db); the in-memory
// PersistedState below is the working copy every read serves, and every
// mutator writes its own rows through in one transaction. Its methods are
// safe for concurrent requests.
//
// One process owns one league database, as it always has: the deployment
// runs a single replica against a ReadWriteOnce volume, with the Recreate
// rollout strategy, so no two processes ever hold the same file. A second
// writing process would keep its own working copy and its own record of
// what it last wrote, so the two copies would drift — the same
// single-writer rule the JSON state file carried, now backed by a real
// transaction boundary for the one writer that exists.
type Store struct {
	mu sync.RWMutex
	// draftLifecycleBypass is a package-test seam for legacy unit fixtures
	// that exercise unrelated store behavior without constructing a full
	// draft lifecycle. Production never sets it.
	draftLifecycleBypass bool
	// filePath names the legacy JSON state file. It still anchors the data
	// directory (the database is its sibling, dbFileName) and it is the
	// one-time import source; see openLocked. An empty path means "no
	// persistence": the store runs from memory and every persist is a
	// no-op.
	filePath         string
	dbPath           string
	db               *sql.DB
	identityResolver identity.Resolver
	state            PersistedState
	// shadow remembers every row as last committed, so a persist writes
	// only the rows that actually changed. dirty is the bitmask of
	// collections a mutator declared; see persistLocked.
	shadow shadowIndex
	dirty  uint32
	// lastPersistRows counts the rows the current/latest persistence attempt
	// wrote or deleted. It backs the incremental-write test and is useful
	// when diagnosing write amplification.
	lastPersistRows   int
	identityUnhealthy bool
	// identityPreflightHook is a test-only seam that runs after the fast
	// authority read and before the final Store lock, allowing seat churn to
	// be exercised deterministically. It is nil in production.
	identityPreflightHook func()
	// identityReconcileReadHook is a test-only seam for the read that follows
	// an unknown/committed persist outcome. Production always reads the
	// authoritative database through loadStateFromDB; tests can make that
	// read fail to prove the Store restores its pre-candidate state before
	// poisoning all later persistence.
	identityReconcileReadHook func(*sql.DB) (PersistedState, error)
	// persistencePoison is a fatal runtime persistence stop. Once an
	// identity outcome cannot be reconciled, every later DB write returns
	// this error and restores poisonedState/poisonedDirty before the caller
	// can publish another in-memory candidate.
	persistencePoison error
	// persistenceWriteError is the latest ordinary write failure. Unlike a
	// poison it remains retryable, but readiness stays withdrawn until a later
	// durable write or explicit reconciliation succeeds. It is deliberately
	// separate from loadErr and the combined constructor/runtime health
	// returned by StartupError, so health reflects a failure that happened
	// after startup as well.
	persistenceWriteError error
	poisonedState         PersistedState
	poisonedDirty         uint32
	// loadErr holds a boot failure the constructor could not recover from: a
	// state whose schema version is newer than this binary supports (section
	// 6.3), a database that cannot be opened, or a legacy state file that
	// failed to import. persistLocked refuses to write while it is set, so a
	// downgraded or broken binary can never clobber good stored state with
	// blank in-memory state; see StartupError.
	loadErr error
	// persistHook runs inside persistLocked's transaction, just before
	// the commit. It is nil in production; tests set it to fail a write
	// or to kill the process with the transaction still open.
	persistHook func() error
	// persistAfterCommitHook is a test-only seam for errors reported after
	// SQLite has committed. Its disposition distinguishes a known committed
	// write from an outcome the caller must reconcile by reading the DB.
	persistAfterCommitHook func() (persistDisposition, error)
	// commitTx is a test-only seam for SQLite's unknown-outcome boundary. A
	// test can return an error without committing, or call tx.Commit and then
	// return an error, so reconciliation proves both possible outcomes.
	commitTx func(*sql.Tx) error
}

func NewStore(filePath string) *Store {
	return NewStoreWithIdentity(filePath, identity.Resolver{})
}

// NewStoreWithIdentity constructs a Store with the operator's explicit
// authentication-alias resolver. Existing callers use NewStore and retain
// the zero-resolver behavior; production wires the configured resolver once
// at service startup so every internal ownership key uses one identity.
func NewStoreWithIdentity(filePath string, resolver identity.Resolver) *Store {
	s := &Store{
		filePath: strings.TrimSpace(filePath),
		shadow:   shadowIndex{},
		state: PersistedState{
			SchemaVersion:  currentSchemaVersion,
			Ready:          map[string]bool{},
			Picks:          []DraftPick{},
			Members:        map[string]Member{},
			Invites:        []string{},
			Boards:         map[string][]string{},
			TeamNames:      map[string]string{},
			DraftOrder:     []string{},
			Scoring:        map[string]float64{},
			Pickems:        map[string]map[string]string{},
			BlitzEntries:   map[string]map[string]BlitzEntry{},
			Autopick:       map[string]bool{},
			SentLog:        map[string]time.Time{},
			NotifyPrefs:    map[string]map[string]bool{},
			BadgeClaims:    map[string]string{},
			AvatarRefs:     map[string]string{},
			Announcements:  []Announcement{},
			Lineups:        map[string]map[int]map[string]string{},
			Transactions:   []Transaction{},
			WaiverClaims:   []WaiverClaim{},
			TradeOffers:    []TradeOffer{},
			RosterZones:    map[string]map[string]ZoneAssignment{},
			CoInvites:      map[string]string{},
			TrimmedTeamIDs: []string{},
		},
	}
	s.identityResolver = resolver
	if err := s.openLocked(); err != nil {
		s.loadErr = err
	}
	if s.db != nil {
		// Close the database once the Store becomes unreachable. A
		// long-lived process holds one Store for its whole life; a test
		// that drops one gets its file handles back at the next
		// collection. Call Close to release them at a known point.
		runtime.AddCleanup(s, func(db *sql.DB) { _ = db.Close() }, s.db)
	}
	return s
}

func (s *Store) canonicalEmail(email string) string {
	return s.identityResolver.Resolve(email)
}

// admissionEmail deliberately does not resolve aliases. Invite entries are
// authorization policy and must remain the exact provider identities the
// commissioner admitted; internal ownership keys use canonicalEmail.
func admissionEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// Close releases the database. A closed Store must not be used again.
// Production never calls it (the process holds one Store for its whole
// life); it exists so a caller that builds many stores can release their
// handles at a known point.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil
	}
	db := s.db
	s.db = nil
	return db.Close()
}

// PersistenceError reports the current persistence health, including a boot
// failure, fatal identity poison, or the latest retryable ordinary write
// failure. A retryable failure is cleared only after a later write has
// completed its durable database transaction (or an equivalent successful
// reconciliation), never merely because a mutator returned nil without a
// write.
func (s *Store) PersistenceError() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.loadErr != nil {
		return s.loadErr
	}
	if s.persistencePoison != nil {
		return s.persistencePoison
	}
	return s.persistenceWriteError
}

// StartupError is retained for callers that need the constructor/runtime
// gate. It now delegates to PersistenceError so existing callers and the
// health surface cannot accidentally hide a write failure that happened
// after process start.
func (s *Store) StartupError() error {
	return s.PersistenceError()
}

func (s *Store) Snapshot() PersistedState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := cloneState(s.state)
	if !s.identityHealthyLocked() {
		if s.persistencePoison != nil {
			out = cloneState(s.poisonedState)
		}
		// Snapshot feeds several render/data paths. Hide both identity maps
		// while poisoned so callers cannot derive an avatar or badge URL from
		// an uncertain pair.
		out.BadgeClaims = map[string]string{}
		out.AvatarRefs = map[string]string{}
	} else {
		// Keep snapshots safe even when a test/import seam supplies a state
		// that bypassed the normal database loader.
		normalizeIdentityCollections(&out)
	}
	// The authority marker is boot metadata, not league state. Keep it in the
	// Store's private working copy so future scalar writes preserve the row,
	// but never expose it through the public snapshot or legacy JSON-shaped
	// view model.
	out.persistenceAuthority = ""
	return out
}

func (s *Store) ToggleReady(teamID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeErrorLocked(); err != nil {
		return false, err
	}
	if !knownTeam(teamID) {
		return false, fmt.Errorf("unknown team %q", teamID)
	}
	s.state.Ready[teamID] = !s.state.Ready[teamID]
	return s.state.Ready[teamID], s.persistLocked(colReady)
}

// MakePick records a manual pick and arms the next deadline in the same
// transaction: one lock acquisition, one persist covers both. madeBy is the
// pick's provenance ("manager", or "commissioner" for a forced pick routed
// through this path — see the callers). nextDeadline is the caller's
// choice: pass now+pickClock to arm the next pick's clock, or the zero
// value to leave the clock unarmed (the final pick, or a paused draft).
func (s *Store) MakePick(teamID, playerID, madeBy string, now time.Time, nextDeadline time.Time) (DraftPick, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeErrorLocked(); err != nil {
		return DraftPick{}, err
	}
	if !s.state.DraftStarted && !s.draftLifecycleBypass {
		return DraftPick{}, fmt.Errorf("the commissioner has not started the draft")
	}
	if !knownTeam(teamID) {
		return DraftPick{}, fmt.Errorf("unknown team %q", teamID)
	}
	for _, pick := range s.state.Picks {
		if pick.PlayerID == playerID {
			return DraftPick{}, fmt.Errorf("that player has already been drafted")
		}
	}
	// The draft ends after every team fills its roster; the round count
	// (CurrentDraftRounds, lineup.go) is the cap, not display copy alone.
	if len(s.state.Picks) >= len(defaultTeams())*CurrentDraftRounds() {
		return DraftPick{}, fmt.Errorf("the draft is complete")
	}
	number := len(s.state.Picks) + 1
	expected := teamOnClock(s.state.DraftOrder, number)
	if expected != teamID {
		return DraftPick{}, fmt.Errorf("%s is on the clock", expected)
	}
	// Best-effort backup: a failure here must not block the pick itself.
	_ = s.backupSnapshotLocked()
	pick := DraftPick{
		Number:   number,
		Round:    pickRound(activeTeamCount(s.state.DraftOrder), number),
		TeamID:   teamID,
		PlayerID: playerID,
		MadeAt:   now.UTC(),
		MadeBy:   madeBy,
	}
	prevDeadline := s.state.ClockDeadline
	s.state.Picks = append(s.state.Picks, pick)
	s.state.ClockDeadline = nextDeadline
	// A failed persist must not leave the pick "real" in memory while the
	// caller was told it failed (the FirstSend/FirstSendBatch rollback
	// precedent above): roll the append and the deadline back so the next
	// attempt sees the same on-clock team and number it saw before this
	// call, instead of silently landing a pick the manager believes never
	// happened.
	if err := s.persistLocked(colPicks, colScalars); err != nil {
		s.state.Picks = s.state.Picks[:len(s.state.Picks)-1]
		s.state.ClockDeadline = prevDeadline
		return DraftPick{}, err
	}
	return pick, nil
}

// UndoLastPick removes the most recent pick and, unless the clock is
// paused, re-arms ClockDeadline with the caller-supplied nextDeadline —
// mirroring MakePick's "caller decides the next deadline" contract: pass
// now.Add(pick clock duration) to arm the reopened slot's clock, or the
// zero value to leave it unarmed. A paused clock keeps ClockDeadline and
// ClockRemainingSec untouched: pause freezes the timer, not the draft, so
// an undo during a pause must not silently resume it.
func (s *Store) UndoLastPick(nextDeadline time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeErrorLocked(); err != nil {
		return err
	}
	if len(s.state.Picks) == 0 {
		return errors.New("no picks to undo")
	}
	// Best-effort backup: a failure here must not block the undo itself.
	_ = s.backupSnapshotLocked()
	removed := s.state.Picks[len(s.state.Picks)-1]
	prevDeadline := s.state.ClockDeadline
	prevRemaining := s.state.ClockRemainingSec
	s.state.Picks = s.state.Picks[:len(s.state.Picks)-1]
	if !s.state.ClockPaused {
		s.state.ClockDeadline = nextDeadline
		s.state.ClockRemainingSec = 0
	}
	// Same rollback rule as MakePick/AutoPick: a failed persist must not
	// leave the pick "gone" in memory while the commissioner was told the
	// undo failed — a retry on that stale in-memory count would drop a
	// second pick instead of retrying the same one.
	if err := s.persistLocked(colPicks, colScalars); err != nil {
		s.state.Picks = append(s.state.Picks, removed)
		s.state.ClockDeadline = prevDeadline
		s.state.ClockRemainingSec = prevRemaining
		return err
	}
	return nil
}

// AutoPick records a clock-driven pick. Under the lock it re-validates:
//   - expectedNumber is still the next pick number (no human pick raced in),
//   - the clock is not paused, unless madeBy is "commissioner" — a forced
//     pick is defined to work while paused (see AdminForceAutopick); the
//     ticker's own "auto" picks never race past a pause because clockTick
//     already checks ClockPaused before it gets here, so this guard exists
//     to catch a pause that commits between the ticker's snapshot and this
//     call,
//   - ClockDeadline still equals deadlineSeen (no extend/resume/reset raced
//     in),
//   - the player is still undrafted, and the resolved team is still on the
//     clock.
//
// Any mismatch returns errStaleAutoPick; the caller drops it and the next
// tick (or admin retry) re-evaluates from a fresh snapshot.
func (s *Store) AutoPick(teamID, playerID, madeBy string, expectedNumber int, deadlineSeen time.Time, now time.Time, nextDeadline time.Time) (DraftPick, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeErrorLocked(); err != nil {
		return DraftPick{}, err
	}
	if !s.state.DraftStarted && !s.draftLifecycleBypass {
		return DraftPick{}, errStaleAutoPick
	}
	if !knownTeam(teamID) {
		return DraftPick{}, fmt.Errorf("unknown team %q", teamID)
	}
	number := len(s.state.Picks) + 1
	if number != expectedNumber {
		return DraftPick{}, errStaleAutoPick
	}
	if madeBy != "commissioner" && s.state.ClockPaused {
		return DraftPick{}, errStaleAutoPick
	}
	if !s.state.ClockDeadline.Equal(deadlineSeen) {
		return DraftPick{}, errStaleAutoPick
	}
	if len(s.state.Picks) >= len(defaultTeams())*CurrentDraftRounds() {
		return DraftPick{}, errStaleAutoPick
	}
	expected := teamOnClock(s.state.DraftOrder, number)
	if expected != teamID {
		return DraftPick{}, errStaleAutoPick
	}
	for _, pick := range s.state.Picks {
		if pick.PlayerID == playerID {
			return DraftPick{}, errStaleAutoPick
		}
	}
	// Best-effort backup: a failure here must not block the auto-pick
	// itself. AutoPick mutates Picks the same way MakePick does, so it
	// gets the same rolling .bak snapshot.
	_ = s.backupSnapshotLocked()
	pick := DraftPick{
		Number:   number,
		Round:    pickRound(activeTeamCount(s.state.DraftOrder), number),
		TeamID:   teamID,
		PlayerID: playerID,
		MadeAt:   now.UTC(),
		MadeBy:   madeBy,
	}
	prevDeadline := s.state.ClockDeadline
	s.state.Picks = append(s.state.Picks, pick)
	s.state.ClockDeadline = nextDeadline
	// Same rollback-on-persist-failure rule as MakePick: an unwound append
	// keeps the in-memory number and on-clock team matching whatever the
	// caller (the ticker or AdminForceAutopick) is told actually happened.
	if err := s.persistLocked(colPicks, colScalars); err != nil {
		s.state.Picks = s.state.Picks[:len(s.state.Picks)-1]
		s.state.ClockDeadline = prevDeadline
		return DraftPick{}, err
	}
	return pick, nil
}

// ArmClock sets the deadline directly. It backs both the ticker's "arm an
// unarmed clock" step and the restart-recovery boot logic; it does not
// touch ClockPaused or ClockRemainingSec.
func (s *Store) ArmClock(deadline time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeErrorLocked(); err != nil {
		return err
	}
	if !s.state.DraftStarted && !s.draftLifecycleBypass {
		return fmt.Errorf("the commissioner has not started the draft")
	}
	s.state.ClockDeadline = deadline
	return s.persistLocked(colScalars)
}

// StartDraft is the only transition that opens the room. It records the
// actual start and arms pick one's clock in the same durable transaction.
// Repeated or racing calls preserve the first timestamp and deadline.
func (s *Store) StartDraft(now time.Time, duration time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeErrorLocked(); err != nil {
		return false, err
	}
	if s.state.DraftStarted {
		return false, nil
	}
	previous := cloneState(s.state)
	s.state.DraftStarted = true
	s.state.DraftStartedAt = now.UTC()
	s.state.ClockPaused = false
	s.state.ClockRemainingSec = 0
	s.state.ClockDeadline = now.Add(duration)
	if err := s.persistLocked(colScalars); err != nil {
		s.state = previous
		return false, err
	}
	return true, nil
}

// PauseClock stops the running deadline, storing the remaining seconds
// (floored at zero) so a resume can restore it. Persisted, so a restart
// stays paused. Pauses an unarmed clock harmlessly (remaining stays 0).
func (s *Store) PauseClock(now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeErrorLocked(); err != nil {
		return err
	}
	remaining := 0
	if !s.state.ClockDeadline.IsZero() {
		remaining = int(s.state.ClockDeadline.Sub(now).Seconds())
		if remaining < 0 {
			remaining = 0
		}
	}
	s.state.ClockRemainingSec = remaining
	s.state.ClockDeadline = time.Time{}
	s.state.ClockPaused = true
	return s.persistLocked(colScalars)
}

// ResumeClock restores the deadline from the stored remaining seconds, or
// grants the full duration when nothing was captured (an unarmed clock, or
// a fresh on-clock team after a manual pick during a pause). In demo mode
// this doubles as "start the clock."
func (s *Store) ResumeClock(now time.Time, duration time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeErrorLocked(); err != nil {
		return err
	}
	if !s.state.DraftStarted && !s.draftLifecycleBypass {
		return fmt.Errorf("start the draft before resuming its clock")
	}
	remaining := time.Duration(s.state.ClockRemainingSec) * time.Second
	if remaining <= 0 {
		remaining = duration
	}
	s.state.ClockDeadline = now.Add(remaining)
	s.state.ClockPaused = false
	s.state.ClockRemainingSec = 0
	return s.persistLocked(colScalars)
}

// ExtendClock adds delta to the running deadline, clamped to
// [now+MinPickClock, now+MaxPickClock]. A paused or unarmed clock rejects
// the extend; there is no running deadline to extend.
func (s *Store) ExtendClock(now time.Time, delta time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeErrorLocked(); err != nil {
		return err
	}
	if s.state.ClockPaused || s.state.ClockDeadline.IsZero() {
		return fmt.Errorf("the clock is not running")
	}
	next := s.state.ClockDeadline.Add(delta)
	floor := now.Add(MinPickClock)
	ceiling := now.Add(MaxPickClock)
	if next.Before(floor) {
		next = floor
	}
	if next.After(ceiling) {
		next = ceiling
	}
	s.state.ClockDeadline = next
	return s.persistLocked(colScalars)
}

// SetClockDuration overrides the persisted pick-clock duration, clamped to
// [MinPickClock, MaxPickClock]. It applies starting with the next arm; the
// running deadline is untouched.
func (s *Store) SetClockDuration(seconds int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeErrorLocked(); err != nil {
		return err
	}
	duration := clampPickClock(time.Duration(seconds) * time.Second)
	s.state.ClockDurationSec = int(duration.Seconds())
	return s.persistLocked(colScalars)
}

// ClearClock zeroes every clock field without touching picks or Autopick.
// The draft-completion path calls this once, when a leftover deadline from
// the final pick needs clearing.
func (s *Store) ClearClock() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeErrorLocked(); err != nil {
		return err
	}
	if s.state.ClockDeadline.IsZero() && !s.state.ClockPaused && s.state.ClockRemainingSec == 0 {
		return nil
	}
	s.state.ClockDeadline = time.Time{}
	s.state.ClockPaused = false
	s.state.ClockRemainingSec = 0
	return s.persistLocked(colScalars)
}

// SetAutopick toggles a team's away-mode auto-pick flag.
func (s *Store) SetAutopick(teamID string, on bool) error {
	if !knownTeam(teamID) {
		return fmt.Errorf("unknown team %q", teamID)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeErrorLocked(); err != nil {
		return err
	}
	if on {
		s.state.Autopick[teamID] = true
	} else {
		delete(s.state.Autopick, teamID)
	}
	return s.persistLocked(colAutopick)
}

// AssignMember binds email to the first open seat, or returns the existing
// member when the email already holds one. created reports whether this
// call bound a brand-new seat claim — the one-lock check-and-write keeps
// the report atomic, so two racing callers for the same email cannot both
// see created == true (the N1 seat-claimed notification's hook relies on
// this; see service.go's assignMember).
//
// A member who already exists but carries no seat (TeamID == "" — the
// EnsureMember-only shape every signed-in member now starts in, since the
// registration wave's sign-in path never claims a seat on its own) is not
// short-circuited: this call upgrades that record in place with a claimed
// seat, exactly as if it were brand new, and reports created == true. A
// member who already holds a seat (TeamID != "") is the only case that
// still short-circuits — that decision is already made and does not
// change here, only the display name may refresh.
func (s *Store) AssignMember(email, name string) (member Member, created bool, err error) {
	email = s.canonicalEmail(email)
	name = strings.TrimSpace(name)
	if email == "" {
		return Member{}, false, fmt.Errorf("email is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeErrorLocked(); err != nil {
		return Member{}, false, err
	}
	if existing, ok := s.state.Members[email]; ok && existing.TeamID != "" {
		if name != "" && existing.Name != name {
			previous := existing
			existing.Name = name
			s.state.Members[email] = existing
			if err := s.persistLocked(colMembers); err != nil {
				s.state.Members[email] = previous
				return previous, false, err
			}
		}
		return existing, false, nil
	}
	used := map[string]bool{}
	for _, member := range s.state.Members {
		used[member.TeamID] = true
	}
	for _, team := range defaultTeams() {
		if used[team.ID] {
			continue
		}
		if name == "" {
			name = team.Manager
		}
		newMember := Member{TeamID: team.ID, Name: name, Email: email}
		s.state.Members[email] = newMember
		if err := s.persistLocked(colMembers); err != nil {
			delete(s.state.Members, email)
			return Member{}, false, err
		}
		return newMember, true, nil
	}
	return Member{}, false, ErrLeagueFull
}

// EnsureMember records email as a league member without claiming a team
// seat, or returns the existing member unchanged. It is the
// membership-only counterpart to AssignMember (the deliberate seat-claim
// path, called only from Google sign-in): a TeamID "" member is a full
// pick'em participant with no fantasy seat (see pickem.go's
// pickemLeaderboard, which already renders that shape). created reports
// whether this call recorded a brand-new member.
func (s *Store) EnsureMember(email, name string) (member Member, created bool, err error) {
	email = s.canonicalEmail(email)
	name = strings.TrimSpace(name)
	if email == "" {
		return Member{}, false, fmt.Errorf("email is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeErrorLocked(); err != nil {
		return Member{}, false, err
	}
	if existing, ok := s.state.Members[email]; ok {
		if name != "" && existing.Name != name {
			previous := existing
			existing.Name = name
			s.state.Members[email] = existing
			if err := s.persistLocked(colMembers); err != nil {
				s.state.Members[email] = previous
				return previous, false, err
			}
		}
		return existing, false, nil
	}
	newMember := Member{TeamID: "", Name: name, Email: email}
	s.state.Members[email] = newMember
	if err := s.persistLocked(colMembers); err != nil {
		delete(s.state.Members, email)
		return Member{}, false, err
	}
	return newMember, true, nil
}

func (s *Store) MemberByEmail(email string) (Member, bool) {
	email = s.canonicalEmail(email)
	s.mu.RLock()
	defer s.mu.RUnlock()
	member, ok := s.state.Members[email]
	return member, ok
}

// AddInvite records a manager email that may claim a seat.
func (s *Store) AddInvite(email string) error {
	email = admissionEmail(email)
	if email == "" || !strings.Contains(email, "@") {
		return fmt.Errorf("enter a valid email address")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeErrorLocked(); err != nil {
		return err
	}
	for _, existing := range s.state.Invites {
		if existing == email {
			return nil
		}
	}
	s.state.Invites = append(s.state.Invites, email)
	return s.persistLocked(colInvites)
}

// RemoveInvite drops an email from the invite list. Existing seat claims stay.
func (s *Store) RemoveInvite(email string) error {
	email = admissionEmail(email)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeErrorLocked(); err != nil {
		return err
	}
	kept := s.state.Invites[:0]
	for _, existing := range s.state.Invites {
		if existing != email {
			kept = append(kept, existing)
		}
	}
	s.state.Invites = kept
	return s.persistLocked(colInvites)
}

// Invited reports whether the email is on the stored invite list.
func (s *Store) Invited(email string) bool {
	email = admissionEmail(email)
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, existing := range s.state.Invites {
		if existing == email {
			return true
		}
	}
	return false
}

// ReleaseSeat unbinds every member holding the team — the primary and, if
// bound, a co-manager (registration wave, build item 4: "seat release
// clears both") — and clears its ready flag. Any pending, not-yet-bound
// co-invite for teamID is dropped too, so a release never leaves an
// orphaned invite that would silently re-bind a future sign-in to a seat
// that has since moved on.
func (s *Store) ReleaseSeat(teamID string) error {
	if !knownTeam(teamID) {
		return fmt.Errorf("unknown team %q", teamID)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeErrorLocked(); err != nil {
		return err
	}
	for email, member := range s.state.Members {
		if member.TeamID == teamID {
			delete(s.state.Members, email)
		}
	}
	for email, pendingTeamID := range s.state.CoInvites {
		if pendingTeamID == teamID {
			delete(s.state.CoInvites, email)
		}
	}
	delete(s.state.Ready, teamID)
	return s.persistLocked(colMembers, colCoInvites, colReady)
}

// errCoManagerLimit is the co-manager registration wave's exact-message
// sentinel: "Limit one co per seat with an exact message" (build item 4).
// It fires whether the existing co is already bound (a Role "co" Member)
// or only pending (a CoInvites entry awaiting that person's first
// sign-in) — from the primary's perspective, both mean "this seat already
// has a co-manager."
var errCoManagerLimit = errors.New("this seat already has a co-manager; detach them first")

// InviteCoManager records a pending co-manager binding for email against
// teamID's seat (registration wave, build item 4): email is added to the
// invite list exactly like AddInvite (a co-manager needs EmailAllowed to
// pass on their first sign-in too), and the pending teamID binding is
// recorded in CoInvites so that sign-in lands them as a co-manager
// (BindCoManager), not a free agent. One co per seat: an already-bound
// co or an already-pending invite for this seat is rejected with
// errCoManagerLimit; re-inviting the same email to the same seat it is
// already pending for is a harmless no-op. An email that already holds a
// team seat of its own (primary or co, this seat or another) is
// rejected — a person operates at most one seat.
func (s *Store) InviteCoManager(teamID, email string) error {
	if !knownTeam(teamID) {
		return fmt.Errorf("unknown team %q", teamID)
	}
	email = admissionEmail(email)
	if email == "" || !strings.Contains(email, "@") {
		return fmt.Errorf("enter a valid email address")
	}
	canonical := s.canonicalEmail(email)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeErrorLocked(); err != nil {
		return err
	}
	if existing, ok := s.state.Members[canonical]; ok && existing.TeamID != "" {
		return fmt.Errorf("%s already holds a team seat", canonical)
	}
	for _, member := range s.state.Members {
		if member.TeamID == teamID && member.Role == "co" {
			return errCoManagerLimit
		}
	}
	if pendingTeamID, ok := s.state.CoInvites[canonical]; ok && pendingTeamID == teamID {
		return nil
	}
	for candidate, pendingTeamID := range s.state.CoInvites {
		if pendingTeamID == teamID && candidate != canonical {
			return errCoManagerLimit
		}
	}
	inviteExists := false
	for _, existing := range s.state.Invites {
		if existing == email {
			inviteExists = true
			break
		}
	}
	if !inviteExists {
		s.state.Invites = append(s.state.Invites, email)
	}
	if s.state.CoInvites == nil {
		s.state.CoInvites = map[string]string{}
	}
	s.state.CoInvites[canonical] = teamID
	return s.persistLocked(colInvites, colCoInvites)
}

// BindCoManager consumes email's pending co-invite (if any) into a bound
// Role "co" Member for that invite's teamID, one lock, one persist. bound
// reports whether a pending invite existed; false with a nil error means
// "nothing to bind" — the sign-in path's ordinary EnsureMember fallback
// applies instead (see Service's sign-in-time binding). Binding
// overwrites any prior Members[email] entry outright: a co-invitee who
// already signed up seatless (EnsureMember) upgrades in place to a bound
// co-manager, the same "overwrite is the whole operation" shape
// AssignMember/EnsureMember use for a repeat call.
func (s *Store) BindCoManager(email, name string) (member Member, bound bool, err error) {
	email = s.canonicalEmail(email)
	name = strings.TrimSpace(name)
	if email == "" {
		return Member{}, false, fmt.Errorf("email is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeErrorLocked(); err != nil {
		return Member{}, false, err
	}
	teamID, pending := s.state.CoInvites[email]
	if !pending {
		return Member{}, false, nil
	}
	if existing, ok := s.state.Members[email]; ok && existing.TeamID != "" {
		if existing.TeamID != teamID {
			return Member{}, false, fmt.Errorf("%q already holds team %q; refusing co-manager bind to team %q", email, existing.TeamID, teamID)
		}
		if existing.Role != "co" {
			return Member{}, false, fmt.Errorf("%q is already the primary manager for team %q; refusing co-manager bind", email, teamID)
		}
		// A stale duplicate invite for an already-bound co-manager is safe to
		// consume. Returning the existing record keeps retries idempotent and
		// never rewrites its seat or role.
		delete(s.state.CoInvites, email)
		if err := s.persistLocked(colMembers, colCoInvites); err != nil {
			s.state.CoInvites[email] = teamID
			return Member{}, false, err
		}
		return existing, true, nil
	}
	newMember := Member{TeamID: teamID, Name: name, Email: email, Role: "co"}
	s.state.Members[email] = newMember
	delete(s.state.CoInvites, email)
	if err := s.persistLocked(colMembers, colCoInvites); err != nil {
		return Member{}, false, err
	}
	return newMember, true, nil
}

// DetachCoManager removes teamID's co-manager, bound or still pending
// (registration wave, build item 4): a bound Role "co" Member is deleted,
// and/or a not-yet-claimed CoInvites entry is dropped, whichever exists —
// either way the seat returns to single-primary. Idempotent: a seat with
// neither is a harmless no-op, matching ReleaseBadge's precedent.
func (s *Store) DetachCoManager(teamID string) error {
	if !knownTeam(teamID) {
		return fmt.Errorf("unknown team %q", teamID)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeErrorLocked(); err != nil {
		return err
	}
	changed := false
	for email, member := range s.state.Members {
		if member.TeamID == teamID && member.Role == "co" {
			delete(s.state.Members, email)
			changed = true
		}
	}
	for email, pendingTeamID := range s.state.CoInvites {
		if pendingTeamID == teamID {
			delete(s.state.CoInvites, email)
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return s.persistLocked(colMembers, colCoInvites)
}

// resetDraftSentLogPrefixes are the SentLog key prefixes ResetDraft drops:
// a re-run draft is a new subject entity. draftrem: (reminder keys) is
// deliberately absent: same draftAt, same reminder, no resend (spec
// section 6.2). "waiver:" (N14) and "lineupwarn:" (N18, a WP-R2 omission
// this closes) join the list under the roster-ops spec section 7.3: every
// waiver claim and lineup reference a rostered player, and a redrawn draft
// orphans them all. "tradeoffer:", "tradedone:", and "tradeveto:" (N15-N17,
// roster-ops spec section 9) join the list here (WP-R5): every trade offer
// names rostered players too, and a redrawn draft orphans them the same
// way.
var resetDraftSentLogPrefixes = []string{
	"onclock:", "autopick:", "draftdone:", "waiver:", "lineupwarn:",
	"tradeoffer:", "tradedone:", "tradeveto:",
}

// resetLeagueSentLogPrefixes extends resetDraftSentLogPrefixes: a rebuilt
// league starts a clean ledger for these key spaces too. order: and
// draftrem: are absent; both re-key naturally (spec section 6.2).
var resetLeagueSentLogPrefixes = append(append([]string{}, resetDraftSentLogPrefixes...),
	"seat:", "pickem-remind:", "pickem-results:", "recap:", "scoring:", "kickoff:")

// ResetDraft clears every pick and ready flag. Seats and boards survive. The
// clock fields and the Autopick map are also cleared: a redrawn draft
// starts with a clean, unarmed clock and no stale away-mode toggles.
// Transactions is cleared too (roster-ops spec section 7.3): every record
// references a rostered player, and a redrawn draft orphans them all. The
// draft-scoped SentLog entries (resetDraftSentLogPrefixes) are pruned in
// the same persist.
func (s *Store) ResetDraft() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeErrorLocked(); err != nil {
		return err
	}
	previous := cloneState(s.state)
	s.state.Picks = []DraftPick{}
	s.state.Ready = map[string]bool{}
	s.state.Transactions = []Transaction{}
	// Lineups derive from the roster the draft produced; a reset that
	// clears Picks must clear them too (roster-ops spec section 7.3 —
	// a WP-R1 omission this closes).
	s.state.Lineups = map[string]map[int]map[string]string{}
	// WaiverClaims name add/drop players that only exist against the
	// current roster; a redrawn draft orphans them, same rationale as
	// Transactions and Lineups above (roster-ops spec section 7.3).
	s.state.WaiverClaims = []WaiverClaim{}
	s.state.WaiversProcessedThrough = time.Time{}
	// TradeOffers name rostered players (Give/Get) against the pre-reset
	// draft; same rationale as WaiverClaims above (roster-ops spec section
	// 7.3).
	s.state.TradeOffers = []TradeOffer{}
	// RosterZones name a zoned player against the pre-reset roster; same
	// rationale (SK IR spec) — a redrawn draft must not leave a stale
	// reserve/IR tag behind to trip up the new one.
	s.state.RosterZones = map[string]map[string]ZoneAssignment{}
	s.state.DraftStarted = false
	s.state.DraftStartedAt = time.Time{}
	s.clearClockFieldsLocked()
	s.pruneSentLogPrefixesLocked(resetDraftSentLogPrefixes...)
	if err := s.persistLocked(colPicks, colReady, colTransactions, colLineups, colWaiverClaims, colTradeOffers,
		colRosterZones, colAutopick, colSentLog, colScalars); err != nil {
		s.state = previous
		return err
	}
	return nil
}

// ResetLeague clears picks, seats, ready flags, boards, pick'em picks,
// Preseason Blitz entries, and Transactions (section 7.3, same rationale
// as ResetDraft). Invites and team name overrides survive; both are
// commissioner configuration, not game state. Clock fields and Autopick
// are cleared, same as ResetDraft. Blitz entries are game state, not
// draft state (F19), so ResetDraft does not touch them. The league-scoped
// SentLog entries (resetLeagueSentLogPrefixes) are pruned in the same
// persist.
func (s *Store) ResetLeague() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeErrorLocked(); err != nil {
		return err
	}
	previous := cloneState(s.state)
	s.state.Picks = []DraftPick{}
	s.state.Ready = map[string]bool{}
	s.state.Members = map[string]Member{}
	s.state.Boards = map[string][]string{}
	s.state.Pickems = map[string]map[string]string{}
	s.state.BlitzEntries = map[string]map[string]BlitzEntry{}
	s.state.Transactions = []Transaction{}
	// Lineups derive from the roster the draft produced; a reset that
	// clears Picks must clear them too (roster-ops spec section 7.3 —
	// a WP-R1 omission this closes).
	s.state.Lineups = map[string]map[int]map[string]string{}
	// See ResetDraft's WaiverClaims comment; same rationale.
	s.state.WaiverClaims = []WaiverClaim{}
	s.state.WaiversProcessedThrough = time.Time{}
	// See ResetDraft's TradeOffers comment; same rationale.
	s.state.TradeOffers = []TradeOffer{}
	// See ResetDraft's RosterZones comment; same rationale.
	s.state.RosterZones = map[string]map[string]ZoneAssignment{}
	s.state.DraftStarted = false
	s.state.DraftStartedAt = time.Time{}
	s.clearClockFieldsLocked()
	s.pruneSentLogPrefixesLocked(resetLeagueSentLogPrefixes...)
	if err := s.persistLocked(colPicks, colReady, colMembers, colBoards, colPickems, colBlitzEntries, colTransactions,
		colLineups, colWaiverClaims, colTradeOffers, colRosterZones, colAutopick, colSentLog, colScalars); err != nil {
		s.state = previous
		return err
	}
	return nil
}

// clearClockFieldsLocked zeroes every clock field and the Autopick map. The
// caller must hold s.mu.
func (s *Store) clearClockFieldsLocked() {
	s.state.ClockDeadline = time.Time{}
	s.state.ClockPaused = false
	s.state.ClockRemainingSec = 0
	s.state.ClockDurationSec = 0
	s.state.Autopick = map[string]bool{}
}

// pruneSentLogPrefixesLocked drops every SentLog entry whose key carries
// any of the given prefixes. The caller must hold s.mu and persist
// afterward.
func (s *Store) pruneSentLogPrefixesLocked(prefixes ...string) {
	for key := range s.state.SentLog {
		for _, prefix := range prefixes {
			if strings.HasPrefix(key, prefix) {
				delete(s.state.SentLog, key)
				break
			}
		}
	}
}

// validateTeamName trims and enforces the shared 40-character ceiling
// every team-name write obeys (Store.SetTeamName, the fantasy-signup
// atomic claim in service.go's claimFantasySeat). It does not reject an
// empty name: SetTeamName's caller may legitimately clear an override,
// while the signup flow additionally requires a non-empty result — that
// check lives at the signup call site, not here.
func validateTeamName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if len(name) > 40 {
		return "", fmt.Errorf("team names must be 40 characters or fewer")
	}
	return name, nil
}

// SetTeamName overrides a team's display name. An empty name clears the
// override and restores the default.
func (s *Store) SetTeamName(teamID, name string) error {
	if !knownTeam(teamID) {
		return fmt.Errorf("unknown team %q", teamID)
	}
	name, err := validateTeamName(name)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeErrorLocked(); err != nil {
		return err
	}
	if name == "" {
		delete(s.state.TeamNames, teamID)
	} else {
		s.state.TeamNames[teamID] = name
	}
	return s.persistLocked(colTeamNames)
}

// SetDraftOrder stores a commissioner-drawn draft order. The order must name
// every default team exactly once, and no pick may exist yet; reset the
// draft first to redraw the order after picks start.
func (s *Store) SetDraftOrder(order []string) error {
	if err := validateDraftOrder(order); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeErrorLocked(); err != nil {
		return err
	}
	if s.state.DraftStarted {
		return fmt.Errorf("reset the draft before changing the order")
	}
	s.state.DraftOrder = append([]string(nil), order...)
	return s.persistLocked(colDraftOrder)
}

// validateDraftOrder rejects anything that is not an exact permutation of
// the eight default team IDs.
func validateDraftOrder(order []string) error {
	defaults := defaultTeamIDs()
	if len(order) != len(defaults) {
		return fmt.Errorf("the draft order must name all %d teams exactly once", len(defaults))
	}
	seen := make(map[string]bool, len(order))
	for _, teamID := range order {
		if !knownTeam(teamID) {
			return fmt.Errorf("unknown team %q", teamID)
		}
		if seen[teamID] {
			return fmt.Errorf("team %q appears more than once in the order", teamID)
		}
		seen[teamID] = true
	}
	return nil
}

// TrimUnclaimedSeats drops every currently-unclaimed seat (SK unclaimed-
// seat spec: "basically whatever seats are filled by an hour before draft
// time"), locking the league to its claimed team count. A seat counts as
// claimed when memberForTeam finds a primary or co-manager Member for it;
// a kept team's own record (ID, name, division, tone) is untouched — this
// only shrinks which team IDs defaultTeams() returns going forward, never
// mutates a surviving team. Rejected once the draft has picks on the tape,
// mirroring SetDraftOrder/SetRosterOverride's post-first-pick lock.
// Rejected below minTeams kept, so a trim can never leave the league under
// the engine's own floor (config.go, also GenerateSchedule's own 4-team
// floor). Always evaluated against activeTeams (the full config-seeded
// list), never against an already-trimmed defaultTeams() result, so a
// repeat call recomputes the same answer instead of compounding —
// idempotent. Also clears any already-drawn DraftOrder: an order drawn
// before the trim named the full, now-stale team set (SetDraftOrder's own
// permutation check would fail against the new, smaller defaultTeamIDs()
// on the next redraw regardless), so the trim itself resets it rather than
// leaving a stale order that activeTeamCount would keep reading as the old
// count. A pre-draft schedule is stale for the same reason, so it is cleared
// in the same persistence transaction and must be regenerated for the kept
// teams.
func (s *Store) TrimUnclaimedSeats() (kept []Team, removedIDs []string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeErrorLocked(); err != nil {
		return nil, nil, err
	}
	if s.state.DraftStarted {
		return nil, nil, fmt.Errorf("seats lock once the draft starts")
	}
	for _, team := range activeTeams {
		if memberForTeam(s.state.Members, team.ID).Email != "" {
			kept = append(kept, team)
		} else {
			removedIDs = append(removedIDs, team.ID)
		}
	}
	if len(kept) < minTeams {
		return nil, nil, fmt.Errorf("at least %d seats must be claimed before trimming", minTeams)
	}
	previous := cloneState(s.state)
	previousDirty := s.dirty
	s.state.TrimmedTeamIDs = append([]string(nil), removedIDs...)
	s.state.DraftOrder = nil
	// A pre-draft schedule names the full seat topology. Trimming seats must
	// discard it in this same transaction, otherwise a restart can resurrect
	// matchups containing teams that no longer exist in the active league.
	s.state.Schedule = nil
	if err := s.persistLocked(colScalars, colDraftOrder, colSchedule); err != nil {
		s.state = previous
		s.dirty = previousDirty
		return nil, nil, err
	}
	return kept, removedIDs, nil
}

// SetScoringValue overrides one scoring rule's point value. Setting the
// default value clears the override so future default changes apply.
func (s *Store) SetScoringValue(key string, points float64) error {
	rule, ok := scoringRuleByKey(key)
	if !ok {
		return fmt.Errorf("unknown scoring key %q", key)
	}
	if points < -25 || points > 25 {
		return fmt.Errorf("points must be between -25 and 25")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeErrorLocked(); err != nil {
		return err
	}
	if points == rule.Points {
		delete(s.state.Scoring, key)
	} else {
		s.state.Scoring[key] = points
	}
	return s.persistLocked(colScoring)
}

// ResetScoring clears every scoring override, restoring the default rules.
func (s *Store) ResetScoring() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeErrorLocked(); err != nil {
		return err
	}
	s.state.Scoring = map[string]float64{}
	return s.persistLocked(colScoring)
}

const boardLimit = 100

// BoardAdd appends a player to the owner's ranked board.
func (s *Store) BoardAdd(owner, playerID string) error {
	owner = s.canonicalEmail(owner)
	if owner == "" || playerID == "" {
		return fmt.Errorf("board owner and player are required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeErrorLocked(); err != nil {
		return err
	}
	board := s.state.Boards[owner]
	for _, existing := range board {
		if existing == playerID {
			return nil
		}
	}
	if len(board) >= boardLimit {
		return fmt.Errorf("the board holds at most %d players", boardLimit)
	}
	s.state.Boards[owner] = append(board, playerID)
	return s.persistLocked(colBoards)
}

// BoardMove shifts a player up (delta -1) or down (delta +1) on the board.
func (s *Store) BoardMove(owner, playerID string, delta int) error {
	owner = s.canonicalEmail(owner)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeErrorLocked(); err != nil {
		return err
	}
	board := s.state.Boards[owner]
	for index, existing := range board {
		if existing != playerID {
			continue
		}
		target := index + delta
		if target < 0 || target >= len(board) {
			return nil
		}
		board[index], board[target] = board[target], board[index]
		return s.persistLocked(colBoards)
	}
	return fmt.Errorf("that player is not on the board")
}

// BoardMoveTo moves a player to an absolute zero-based index on the
// owner's board, mirroring BoardMove's known-player check. An index
// outside the board's bounds clamps to the nearest valid position instead
// of failing — the declarative reorder primitive (gosx#212) already
// clamps its own drop target client-side, so this is a defense-in-depth
// clamp, not the primary bound.
func (s *Store) BoardMoveTo(owner, playerID string, index int) error {
	owner = s.canonicalEmail(owner)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeErrorLocked(); err != nil {
		return err
	}
	board := s.state.Boards[owner]
	pos := -1
	for i, existing := range board {
		if existing == playerID {
			pos = i
			break
		}
	}
	if pos == -1 {
		return fmt.Errorf("that player is not on the board")
	}
	if index < 0 {
		index = 0
	}
	if index > len(board)-1 {
		index = len(board) - 1
	}
	if index == pos {
		return nil
	}
	rest := make([]string, 0, len(board)-1)
	rest = append(rest, board[:pos]...)
	rest = append(rest, board[pos+1:]...)
	updated := make([]string, 0, len(board))
	updated = append(updated, rest[:index]...)
	updated = append(updated, playerID)
	updated = append(updated, rest[index:]...)
	s.state.Boards[owner] = updated
	return s.persistLocked(colBoards)
}

// BoardRemove drops a player from the owner's board.
func (s *Store) BoardRemove(owner, playerID string) error {
	owner = s.canonicalEmail(owner)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeErrorLocked(); err != nil {
		return err
	}
	board := s.state.Boards[owner]
	kept := board[:0]
	for _, existing := range board {
		if existing != playerID {
			kept = append(kept, existing)
		}
	}
	s.state.Boards[owner] = kept
	return s.persistLocked(colBoards)
}

// BoardClear removes every player from the owner's board.
func (s *Store) BoardClear(owner string) error {
	owner = s.canonicalEmail(owner)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeErrorLocked(); err != nil {
		return err
	}
	delete(s.state.Boards, owner)
	return s.persistLocked(colBoards)
}

// SetPickem records the owner's pick for one game. It does not validate the
// game or the team against the schedule; the service layer owns that.
func (s *Store) SetPickem(owner, gameID, team string) error {
	owner = s.canonicalEmail(owner)
	if owner == "" || gameID == "" {
		return fmt.Errorf("pick owner and game are required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeErrorLocked(); err != nil {
		return err
	}
	if s.state.Pickems[owner] == nil {
		s.state.Pickems[owner] = map[string]string{}
	}
	s.state.Pickems[owner][gameID] = team
	return s.persistLocked(colPickems)
}

// BlitzSetEntry replaces owner's slate entry wholesale. It performs no
// validation — the service layer owns that (SetPickem precedent, F9).
// Two-tab last-write-wins is accepted for eight users (R8). An empty
// players slice still records the entry, with UpdatedAt moved to now; the
// service layer's BlitzRemove relies on that to persist a player removal.
func (s *Store) BlitzSetEntry(owner, slate string, players []string, now time.Time) error {
	owner = s.canonicalEmail(owner)
	if owner == "" || slate == "" {
		return fmt.Errorf("blitz entry owner and slate are required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeErrorLocked(); err != nil {
		return err
	}
	if s.state.BlitzEntries[owner] == nil {
		s.state.BlitzEntries[owner] = map[string]BlitzEntry{}
	}
	s.state.BlitzEntries[owner][slate] = BlitzEntry{
		Players:   append([]string(nil), players...),
		UpdatedAt: now.UTC(),
	}
	return s.persistLocked(colBlitzEntries)
}

// FirstSend records key with now (converted to UTC) and returns true when
// the key is new. One lock acquisition, one persist: the check and the
// write are atomic, so two evaluation paths (an event hook and a
// derivation tick) racing the same key cannot both report true (spec
// section 6.2, 6.3). true always means "on disk": when the persist fails,
// the in-memory entry is rolled back and FirstSend returns (false, err),
// so a caller that ignores the error never believes a send it must not
// repeat, and a caller that honors it can retry the same key next time
// (finding M1).
func (s *Store) FirstSend(key string, now time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeErrorLocked(); err != nil {
		return false, err
	}
	if _, exists := s.state.SentLog[key]; exists {
		return false, nil
	}
	s.state.SentLog[key] = now.UTC()
	if err := s.persistLocked(colSentLog); err != nil {
		delete(s.state.SentLog, key)
		return false, err
	}
	return true, nil
}

// FirstSendBatch does the same as FirstSend for a league-wide fan-out, in
// one persist: result[i] reports whether keys[i] was new. A repeated key
// within the same call is new only for its first occurrence. When the
// persist fails, every key newly added by this call is rolled back and
// FirstSendBatch returns an all-false result plus the error; a key that
// was already recorded before this call is left untouched, since this
// call never claimed to be the one that made it true (finding M1).
func (s *Store) FirstSendBatch(keys []string, now time.Time) ([]bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeErrorLocked(); err != nil {
		return nil, err
	}
	result := make([]bool, len(keys))
	sentAt := now.UTC()
	added := make([]string, 0, len(keys))
	for i, key := range keys {
		if _, exists := s.state.SentLog[key]; exists {
			continue
		}
		s.state.SentLog[key] = sentAt
		result[i] = true
		added = append(added, key)
	}
	if err := s.persistLocked(colSentLog); err != nil {
		for _, key := range added {
			delete(s.state.SentLog, key)
		}
		return make([]bool, len(keys)), err
	}
	return result, nil
}

// PruneSentLog drops SentLog entries older than cutoff and entries whose
// key carries any of the given prefixes. A zero cutoff prunes for prefix
// only, which is how ResetDraft and ResetLeague reuse this rule outside
// their own atomic persist (see pruneSentLogPrefixesLocked); the
// notifier's daily prune calls it with prefixes empty and cutoff 180 days
// back (spec section 6.2). It persists only when at least one entry was
// actually dropped, so a no-op prune does not rewrite the state file and
// move the fingerprint for nothing (finding nit 6).
func (s *Store) PruneSentLog(cutoff time.Time, prefixes ...string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeErrorLocked(); err != nil {
		return err
	}
	deleted := false
	for key, sentAt := range s.state.SentLog {
		if !cutoff.IsZero() && sentAt.Before(cutoff) {
			delete(s.state.SentLog, key)
			deleted = true
			continue
		}
		for _, prefix := range prefixes {
			if strings.HasPrefix(key, prefix) {
				delete(s.state.SentLog, key)
				deleted = true
				break
			}
		}
	}
	if !deleted {
		return nil
	}
	return s.persistLocked(colSentLog)
}

// SetNotifyPref records one member's category preference. Only overrides
// are stored (spec section 7.1); the league.json default and the catalog
// default apply when nothing is stored here.
func (s *Store) SetNotifyPref(email, category string, enabled bool) error {
	email = s.canonicalEmail(email)
	category = strings.TrimSpace(category)
	if email == "" {
		return fmt.Errorf("email is required")
	}
	if category == "" {
		return fmt.Errorf("category is required")
	}
	if !notificationPreferenceCategoryAllowed(category) {
		return fmt.Errorf("unsupported notification category %q", category)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeErrorLocked(); err != nil {
		return err
	}
	if s.state.NotifyPrefs[email] == nil {
		s.state.NotifyPrefs[email] = map[string]bool{}
	}
	s.state.NotifyPrefs[email][category] = enabled
	return s.persistLocked(colNotifyPrefs)
}

// SetSchedule replaces the persisted regular-season schedule wholesale. It
// does not validate the schedule's shape (GenerateSchedule already did);
// callers that need the section 2.3 regeneration guard (season not
// started, no matchup final yet) enforce it before calling this — see
// AdminGenerateSchedule / AdminRegenerateSchedule.
func (s *Store) SetSchedule(sch SeasonSchedule) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeErrorLocked(); err != nil {
		return err
	}
	s.state.Schedule = cloneSchedule(&sch)
	return s.persistLocked(colSchedule, colScalars)
}

// SetScheduleWeek replaces one week's data (matchups, scores, bye) in the
// persisted schedule, matched by week.Week. It fails when no schedule
// exists or the week number is not part of it; see season.go's closeWeek.
func (s *Store) SetScheduleWeek(week ScheduleWeek) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeErrorLocked(); err != nil {
		return err
	}
	if s.state.Schedule == nil {
		return fmt.Errorf("no schedule has been generated")
	}
	for i, wk := range s.state.Schedule.Weeks {
		if wk.Week == week.Week {
			stored := week
			stored.Matchups = append([]LeagueMatchup(nil), week.Matchups...)
			s.state.Schedule.Weeks[i] = stored
			return s.persistLocked(colSchedule)
		}
	}
	return fmt.Errorf("week %d is not part of the schedule", week.Week)
}

// SetScheduleWeekWithLineups persists one week's close in a single lock and
// a single persist (roster-ops spec section 4.2's materialize-at-close):
// the effective-lineup pin for every team named in pins, plus the scored,
// final week itself — so a crash between the two can never leave one
// written without the other. pins maps teamID -> slot ID -> player ID.
//
// Idempotency lives here, not on "does Lineups[teamID][week] already have
// an entry" — a team can carry a *partial* stored week from an ordinary,
// still-open lineup-set (SetLineupSlot) before its week ever closes, and
// that must not block materialize-at-close from writing the *complete*
// pin over it. The correct signal is the schedule's own Final flag: when
// this week already reads final under the lock (a racing close won first
// — season.go's own scheduleWeekIsFinal check makes this rare, not
// impossible), this call is a pure no-op, leaving both the schedule and
// every pin exactly as the winning call left them. Otherwise it always
// overwrites week.Week's entry in every named team's Lineups map, in full,
// with pins' contents — that overwrite is what turns a partial live edit
// into the frozen, complete snapshot the spec calls for.
func (s *Store) SetScheduleWeekWithLineups(week ScheduleWeek, pins map[string]map[string]string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeErrorLocked(); err != nil {
		return err
	}
	if s.state.Schedule == nil {
		return fmt.Errorf("no schedule has been generated")
	}
	index := -1
	for i, wk := range s.state.Schedule.Weeks {
		if wk.Week == week.Week {
			index = i
			break
		}
	}
	if index == -1 {
		return fmt.Errorf("week %d is not part of the schedule", week.Week)
	}
	if matchupsAllFinal(s.state.Schedule.Weeks[index].Matchups) {
		return nil // idempotent: this week already closed under the lock.
	}
	stored := week
	stored.Matchups = append([]LeagueMatchup(nil), week.Matchups...)
	s.state.Schedule.Weeks[index] = stored

	if s.state.Lineups == nil {
		s.state.Lineups = map[string]map[int]map[string]string{}
	}
	for teamID, slots := range pins {
		byWeek, ok := s.state.Lineups[teamID]
		if !ok {
			byWeek = map[int]map[string]string{}
			s.state.Lineups[teamID] = byWeek
		}
		copied := make(map[string]string, len(slots))
		for slot, playerID := range slots {
			copied[slot] = playerID
		}
		byWeek[week.Week] = copied
	}
	return s.persistLocked(colSchedule, colLineups)
}

// SetPhase overrides the persisted season phase (section 5.2). season.go
// and playoffs.go call this at the transitions this work package drives;
// it performs no validation of the transition itself, matching the
// existing store idiom of pushing invariant checks to the service layer.
func (s *Store) SetPhase(phase string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeErrorLocked(); err != nil {
		return err
	}
	s.state.Phase = phase
	return s.persistLocked(colScalars)
}

// SetPlayoffs replaces the persisted playoff bracket state wholesale.
func (s *Store) SetPlayoffs(state PlayoffState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeErrorLocked(); err != nil {
		return err
	}
	s.state.Playoffs = clonePlayoffState(&state)
	return s.persistLocked(colPlayoffs)
}

// Avatar identity transitions share one Store transaction and one authority check.
// avatarIdentity is the durable pair whose transitions must be atomic.
type avatarIdentity struct {
	ref   string
	motif string
}

func avatarIdentityOf(state PersistedState, teamID string) avatarIdentity {
	return avatarIdentity{ref: state.AvatarRefs[teamID], motif: state.BadgeClaims[teamID]}
}

func avatarActorAllowed(state PersistedState, teamID string, actor seatActor, commissionerOnly bool) bool {
	if actor.commissioner || actor.demo {
		return true
	}
	if commissionerOnly {
		return false
	}
	member, ok := state.Members[strings.ToLower(strings.TrimSpace(actor.email))]
	return ok && member.TeamID == teamID
}

func (s *Store) identityHealthyLocked() bool {
	return s.loadErr == nil && !s.identityUnhealthy && s.persistencePoison == nil
}

// writeErrorLocked is the single write-side health gate. Callers must invoke
// it immediately after acquiring s.mu and before reading or mutating state,
// taking a backup, running a hook, or doing any other write-side work. A
// failed boot or an unreconciled identity commit closes every later writer;
// persistLocked repeats this check as a final defense for legacy mutators that
// update their working state before reaching the common persistence boundary.
func (s *Store) writeErrorLocked() error {
	if s.loadErr != nil {
		return s.loadErr
	}
	if s.persistencePoison != nil {
		return s.persistencePoison
	}
	return nil
}

// IdentityHealthy reports whether identity reads can be served. An
// unreconciled commit outcome fails closed until the Store is restarted.
func (s *Store) IdentityHealthy() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.identityHealthyLocked()
}

// mutateAvatarIdentity applies a candidate state only after rechecking the
// current actor under the same Store lock that writes the pair. forbidden is
// operation-specific so a badge seat-churn cannot be mislabeled as an avatar
// failure. A failed pre-commit write discards the candidate; a committed or
// unknown outcome is reconciled against a fresh database read before it can
// be published.
func (s *Store) mutateAvatarIdentity(teamID string, actor seatActor, commissionerOnly bool, forbidden error, mutate func(*PersistedState) (bool, error)) (bool, error) {
	s.mu.RLock()
	if err := s.writeErrorLocked(); err != nil {
		s.mu.RUnlock()
		return false, err
	}
	// The request boundary canonicalizes authenticated users, but this Store
	// transaction is the final authority gate and must remain correct for
	// direct/internal callers too. Resolve aliases before both the optimistic
	// and locked authority checks so one logical manager has one seat identity.
	actor.email = s.canonicalEmail(actor.email)
	earlyAllowed := avatarActorAllowed(s.state, teamID, actor, commissionerOnly)
	preflightHook := s.identityPreflightHook
	s.mu.RUnlock()
	if !earlyAllowed {
		return false, forbidden
	}
	if preflightHook != nil {
		preflightHook()
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeErrorLocked(); err != nil {
		return false, err
	}
	if !avatarActorAllowed(s.state, teamID, actor, commissionerOnly) {
		return false, forbidden
	}
	before := cloneState(s.state)
	beforeDirty := s.dirty
	candidate := cloneState(before)
	changed, err := mutate(&candidate)
	if err != nil || !changed {
		return false, err
	}
	// Re-apply the v1 catalog boundary immediately before any identity
	// collection is emitted. This keeps a hand-edited/old persisted row from
	// surviving a later claim, release, or avatar transition.
	normalizeIdentityCollections(&candidate)
	desired := avatarIdentityOf(candidate, teamID)
	previous := avatarIdentityOf(before, teamID)
	s.state = candidate
	if err := s.persistLocked(colBadgeClaims, colAvatarRefs); err == nil {
		return true, nil
	} else {
		disposition := persistDispositionOf(err)
		if disposition == persistNotCommitted {
			s.state = before
			s.dirty = beforeDirty
			return false, err
		}
		readState := loadStateFromDB
		if s.identityReconcileReadHook != nil {
			readState = s.identityReconcileReadHook
		}
		stored, readErr := readState(s.db)
		if readErr == nil {
			got := avatarIdentityOf(stored, teamID)
			if got == desired {
				s.state = stored
				if rebuildErr := s.rebuildShadowLocked(); rebuildErr != nil {
					s.poisonIdentityLocked(before, beforeDirty)
					return false, ErrPersistenceIndeterminate
				}
				s.dirty = 0
				// The authoritative read proved that the candidate did land;
				// this is a known-successful reconciliation, so it clears a
				// prior retryable write-health failure.
				s.persistenceWriteError = nil
				return true, nil
			}
			if got == previous {
				// The transaction did not land. Rebuild the shadow from the
				// authoritative database, then restore the exact prior in-memory
				// candidate and dirty mask so unrelated pending work is not lost.
				s.state = stored
				if rebuildErr := s.rebuildShadowLocked(); rebuildErr != nil {
					s.poisonIdentityLocked(before, beforeDirty)
					return false, ErrPersistenceIndeterminate
				}
				s.state = before
				s.dirty = beforeDirty
				return false, err
			}
			// The database read succeeded but returned neither the
			// candidate nor the prior pair. Do not publish this ambiguous
			// state; poisonIdentityLocked restores the pre-candidate copy.
		}
		// A read failure, or a readable but irreconcilable identity, is
		// fatal for this Store. Restore before closing all later writes.
		s.poisonIdentityLocked(before, beforeDirty)
		return false, ErrPersistenceIndeterminate
	}
}

// poisonIdentityLocked records the exact state and dirty mask that existed
// before an identity candidate was published, restores them immediately, and
// permanently closes persistence for this Store. The poison is intentionally
// store-wide: allowing an unrelated write after an identity ambiguity would
// publish more in-memory state while the database's authoritative pair is
// unknown.
func (s *Store) poisonIdentityLocked(before PersistedState, beforeDirty uint32) {
	s.identityUnhealthy = true
	s.persistencePoison = ErrPersistenceIndeterminate
	s.poisonedState = cloneState(before)
	s.poisonedDirty = beforeDirty
	s.state = cloneState(before)
	s.dirty = beforeDirty
}

func (s *Store) claimBadgeForActor(teamID, motif string, actor seatActor) error {
	_, err := s.claimBadgeForActorTransition(teamID, motif, actor)
	return err
}

func (s *Store) claimBadgeForActorTransition(teamID, motif string, actor seatActor) (bool, error) {
	teamID = strings.TrimSpace(teamID)
	motif = strings.TrimSpace(motif)
	if !knownTeam(teamID) {
		return false, fmt.Errorf("unknown team %q", teamID)
	}
	if !knownMotif(motif) {
		return false, ErrBadgeUnknownMotif
	}
	avatarCleared := false
	_, err := s.mutateAvatarIdentity(teamID, actor, false, ErrBadgeForbidden, func(candidate *PersistedState) (bool, error) {
		normalizeIdentityCollections(candidate)
		for holder, claimed := range candidate.BadgeClaims {
			if claimed == motif && holder != teamID {
				return false, &badgeClaimedError{teamID: holder}
			}
		}
		old := avatarIdentityOf(*candidate, teamID)
		if old.motif == motif && old.ref == "" {
			return false, nil
		}
		if candidate.BadgeClaims == nil {
			candidate.BadgeClaims = map[string]string{}
		}
		if candidate.AvatarRefs == nil {
			candidate.AvatarRefs = map[string]string{}
		}
		candidate.BadgeClaims[teamID] = motif
		if _, ok := candidate.AvatarRefs[teamID]; ok {
			avatarCleared = true
		}
		delete(candidate.AvatarRefs, teamID)
		return true, nil
	})
	if err != nil {
		return false, err
	}
	return avatarCleared, nil
}

func (s *Store) ClaimBadge(teamID, motif string) error {
	return s.claimBadgeForActor(teamID, motif, seatActor{commissioner: true})
}

func (s *Store) activateAvatar(teamID, ref string, actor seatActor) (bool, error) {
	teamID = strings.TrimSpace(teamID)
	if !knownTeam(teamID) {
		return false, fmt.Errorf("unknown team %q", teamID)
	}
	if !validAvatarRef(ref) {
		return false, fmt.Errorf("invalid avatar reference")
	}
	released := false
	_, err := s.mutateAvatarIdentity(teamID, actor, false, ErrAvatarForbidden, func(candidate *PersistedState) (bool, error) {
		old := avatarIdentityOf(*candidate, teamID)
		if old.ref == ref && old.motif == "" {
			return false, nil
		}
		if candidate.AvatarRefs == nil {
			candidate.AvatarRefs = map[string]string{}
		}
		candidate.AvatarRefs[teamID] = ref
		if _, ok := candidate.BadgeClaims[teamID]; ok {
			released = true
			delete(candidate.BadgeClaims, teamID)
		}
		return true, nil
	})
	if err != nil {
		return false, err
	}
	return released, nil
}

func (s *Store) clearAvatar(teamID string, actor seatActor) error {
	teamID = strings.TrimSpace(teamID)
	if !knownTeam(teamID) {
		return fmt.Errorf("unknown team %q", teamID)
	}
	_, err := s.mutateAvatarIdentity(teamID, actor, true, ErrAvatarForbidden, func(candidate *PersistedState) (bool, error) {
		if _, ok := candidate.AvatarRefs[teamID]; !ok {
			return false, nil
		}
		delete(candidate.AvatarRefs, teamID)
		return true, nil
	})
	return err
}

func (s *Store) releaseBadgeForActor(teamID string, actor seatActor) (bool, error) {
	teamID = strings.TrimSpace(teamID)
	if !knownTeam(teamID) {
		return false, fmt.Errorf("unknown team %q", teamID)
	}
	released := false
	_, err := s.mutateAvatarIdentity(teamID, actor, false, ErrBadgeForbidden, func(candidate *PersistedState) (bool, error) {
		if _, ok := candidate.BadgeClaims[teamID]; !ok {
			return false, nil
		}
		delete(candidate.BadgeClaims, teamID)
		released = true
		return true, nil
	})
	if err != nil {
		return false, err
	}
	return released, nil
}

func (s *Store) ReleaseBadge(teamID string) (bool, error) {
	return s.releaseBadgeForActor(teamID, seatActor{commissioner: true})
}

func (s *Store) AvatarRef(teamID string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.identityHealthyLocked() || !knownTeam(teamID) {
		return "", false
	}
	ref, ok := s.state.AvatarRefs[teamID]
	return ref, ok && validAvatarRef(ref)
}

func (s *Store) AvatarRefs() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]string, len(s.state.AvatarRefs))
	if !s.identityHealthyLocked() {
		return out
	}
	for teamID, ref := range s.state.AvatarRefs {
		if knownTeam(teamID) && validAvatarRef(ref) {
			out[teamID] = ref
		}
	}
	return out
}

// BadgeClaim reports teamID's currently claimed motif slug, if any.
func (s *Store) BadgeClaim(teamID string) (motif string, ok bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.identityHealthyLocked() || !knownTeam(teamID) {
		return "", false
	}
	motif, ok = s.state.BadgeClaims[teamID]
	return motif, ok && knownMotif(motif)
}

// BadgeClaims returns a snapshot of every team ID's current badge claim,
// for the picker UI's grid (which team, if any, holds each motif).
func (s *Store) BadgeClaims() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.identityHealthyLocked() {
		return map[string]string{}
	}
	out := make(map[string]string, len(s.state.BadgeClaims))
	for teamID, motif := range s.state.BadgeClaims {
		if knownTeam(teamID) && knownMotif(motif) {
			out[teamID] = motif
		}
	}
	return out
}

// SetRosterOverride persists a commissioner-chosen roster shape override,
// replacing the config-resolved default (roster-shape-editor spec). It is
// rejected once the draft has picks on the tape: the roster shape and its
// derived draft-round count only stay free to change while nobody has been
// drafted against a live slot count yet (mirrors SetDraftOrder's
// post-first-pick lock, same message shape).
func (s *Store) SetRosterOverride(o RosterOverride) error {
	if err := validateRosterOverride(o); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeErrorLocked(); err != nil {
		return err
	}
	if s.state.DraftStarted {
		return fmt.Errorf("the roster shape locks once the draft starts")
	}
	s.state.RosterOverride = cloneRosterOverride(&o)
	return s.persistLocked(colScalars)
}

// ClearRosterOverride drops any commissioner roster-shape override,
// restoring the config-resolved default. Same post-first-pick lock as
// SetRosterOverride.
func (s *Store) ClearRosterOverride() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeErrorLocked(); err != nil {
		return err
	}
	if s.state.DraftStarted {
		return fmt.Errorf("the roster shape locks once the draft starts")
	}
	s.state.RosterOverride = nil
	return s.persistLocked(colScalars)
}

// SetLineupSlot persists one lineup-slot assignment for teamID/week, one
// lock, one persist (roster-ops spec section 4.4). An empty playerID
// clears the slot; a non-empty playerID first clears any other slot within
// the same stored week that already names it — a player occupies at most
// one slot, so the displacement happens in this same write. now is
// currently unused (reserved for a future per-write audit trail) but kept
// so the signature mirrors the spec's SetLineupSlot(teamID, week, slot,
// playerID, effective, now) shape; business validation needing pool or
// schedule data (L4-L8) happens one layer up, in Service.SetLineup —
// mirroring the split MakePick/BoardAdd already establish between
// Service-level business validation and Store-level atomic persistence.
func (s *Store) SetLineupSlot(teamID string, week int, slot, playerID string, now time.Time) error {
	_ = now
	if !knownTeam(teamID) {
		return fmt.Errorf("unknown team %q", teamID)
	}
	if week < 1 {
		return fmt.Errorf("unknown lineup week %d", week)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeErrorLocked(); err != nil {
		return err
	}
	if s.state.Lineups == nil {
		s.state.Lineups = map[string]map[int]map[string]string{}
	}
	byWeek, ok := s.state.Lineups[teamID]
	if !ok {
		byWeek = map[int]map[string]string{}
		s.state.Lineups[teamID] = byWeek
	}
	slots, ok := byWeek[week]
	if !ok {
		slots = map[string]string{}
		byWeek[week] = slots
	}
	playerID = strings.TrimSpace(playerID)
	if playerID == "" {
		delete(slots, slot)
		return s.persistLocked(colLineups)
	}
	for otherSlot, occupant := range slots {
		if otherSlot != slot && occupant == playerID {
			delete(slots, otherSlot)
		}
	}
	slots[slot] = playerID
	return s.persistLocked(colLineups)
}

// SetLineupWeek replaces teamID's entire explicit lineup for week with
// slots (slot ID -> player ID), one lock, one persist. The lineup-auto
// action (roster-ops spec section 4.7) is the only caller: it recomputes
// every unlocked slot at once, so the whole week's explicit map is
// replaced rather than edited key by key the way SetLineupSlot does for a
// single lineup-set action.
func (s *Store) SetLineupWeek(teamID string, week int, slots map[string]string) error {
	if !knownTeam(teamID) {
		return fmt.Errorf("unknown team %q", teamID)
	}
	if week < 1 {
		return fmt.Errorf("unknown lineup week %d", week)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeErrorLocked(); err != nil {
		return err
	}
	if s.state.Lineups == nil {
		s.state.Lineups = map[string]map[int]map[string]string{}
	}
	byWeek, ok := s.state.Lineups[teamID]
	if !ok {
		byWeek = map[int]map[string]string{}
		s.state.Lineups[teamID] = byWeek
	}
	copied := make(map[string]string, len(slots))
	for slot, playerID := range slots {
		copied[slot] = playerID
	}
	byWeek[week] = copied
	return s.persistLocked(colLineups)
}

// RecordTransaction appends one add/drop transaction (roster-ops spec
// section 7.1), one lock, one persist — the appended record is the only
// roster effect written (section 3's derive-never-store rule:
// currentRosters replays Transactions, so there is no separate roster
// field that could fall out of sync with the log). Before appending it
// re-validates against the freshest locked state, the same
// re-validate-under-lock discipline AutoPick uses for the draft: every
// Adds player must still be unrostered, every Drops player must still sit
// on txn.TeamID's roster, and txn.TeamID's post-move roster size must not
// exceed rosterCap. This closes the race two concurrent add/drop requests
// could otherwise open between the service layer's snapshot and this
// write; the service layer performs the same checks first (for the exact
// W-table messages), so this is a defense-in-depth repeat, not the
// primary validation path.
func (s *Store) RecordTransaction(txn Transaction, rosterCap int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeErrorLocked(); err != nil {
		return err
	}
	if !knownTeam(txn.TeamID) {
		return fmt.Errorf("unknown team %q", txn.TeamID)
	}
	owner := rosterOwner(currentRosters(s.state))
	for _, gain := range txn.Adds {
		if owner[gain.PlayerID] != "" {
			return fmt.Errorf("%s is already on a roster", gain.Name)
		}
	}
	for _, drop := range txn.Drops {
		if owner[drop.PlayerID] != txn.TeamID {
			return fmt.Errorf("%s", lineupNotOnRosterMessage)
		}
	}
	// effectiveRosterSize excludes IR occupants from the cap math (SK
	// spec: "placing a player in IR frees a general roster spot") — with
	// no IR occupants this is exactly the raw ownership count, unchanged.
	current := effectiveRosterSize(s.state, txn.TeamID)
	if next := current + len(txn.Adds) - len(txn.Drops); next > rosterCap {
		return fmt.Errorf("your roster is full; choose a player to drop")
	}
	txn.At = txn.At.UTC()
	s.state.Transactions = append(s.state.Transactions, txn)
	dropIDs := make([]string, 0, len(txn.Drops))
	for _, drop := range txn.Drops {
		dropIDs = append(dropIDs, drop.PlayerID)
	}
	s.clearZoneAssignmentsLocked(txn.TeamID, dropIDs)
	return s.persistLocked(colTransactions, colRosterZones)
}

// BaselineWaiversProcessedThrough sets WaiversProcessedThrough to now
// without processing any claim (section 5.4 step 1): the fresh- or
// migrated-state guard against a retroactive run, the notifyLastPruneAt
// boot precedent (notifications.go) inverted. A no-op once already set —
// only the tick's own zero-check calls this, but the guard here keeps the
// method itself safe to call idempotently.
func (s *Store) BaselineWaiversProcessedThrough(now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeErrorLocked(); err != nil {
		return err
	}
	if !s.state.WaiversProcessedThrough.IsZero() {
		return nil
	}
	s.state.WaiversProcessedThrough = now.UTC()
	return s.persistLocked(colScalars)
}

// FileClaim appends one open waiver claim (roster-ops spec section 5.3),
// one lock, one persist. It re-validates against the freshest locked
// state — RecordTransaction's re-validate-under-lock discipline: the add
// player must still be unrostered and not already claimed by this team,
// and a named drop player must still sit on the claiming team's roster.
// The service layer performs the same checks first (for the exact
// W-table messages); this is defense-in-depth, not the primary
// validation path, so its messages are generic rather than name-exact —
// Store carries no player-identity lookup. Priority is assigned here,
// under the lock, as one past the filing team's current open-claim count,
// so two near-simultaneous claims by the same team can never collide.
func (s *Store) FileClaim(claim WaiverClaim) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeErrorLocked(); err != nil {
		return err
	}
	if !knownTeam(claim.TeamID) {
		return fmt.Errorf("unknown team %q", claim.TeamID)
	}
	owner := rosterOwner(currentRosters(s.state))
	if owner[claim.AddID] != "" {
		return fmt.Errorf("that player is already on a roster")
	}
	if _, exists := waiverClaimByTeamAndPlayer(s.state.WaiverClaims, claim.TeamID, claim.AddID); exists {
		return fmt.Errorf("you already hold a claim for that player")
	}
	if claim.DropID != "" && owner[claim.DropID] != claim.TeamID {
		return fmt.Errorf("%s", lineupNotOnRosterMessage)
	}
	claim.FiledAt = claim.FiledAt.UTC()
	claim.Priority = teamOpenClaimCount(s.state.WaiverClaims, claim.TeamID) + 1
	s.state.WaiverClaims = append(s.state.WaiverClaims, claim)
	return s.persistLocked(colWaiverClaims)
}

// CancelClaim removes teamID's open claim named by claimID. Removing a
// claim that does not exist, or belongs to another team, is a harmless
// no-op — ReleaseBadge's idempotent precedent (badge.go) — so a stale
// double-submit never surfaces a confusing error.
func (s *Store) CancelClaim(teamID, claimID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeErrorLocked(); err != nil {
		return err
	}
	kept := make([]WaiverClaim, 0, len(s.state.WaiverClaims))
	for _, c := range s.state.WaiverClaims {
		if c.ID == claimID && c.TeamID == teamID {
			continue
		}
		kept = append(kept, c)
	}
	s.state.WaiverClaims = kept
	return s.persistLocked(colWaiverClaims)
}

// ProcessWaivers runs one daily waiver cycle at instant now (roster-ops
// spec section 5.4): due claims resolve in deterministic priority order,
// each re-validated against the state left by every earlier win in the
// same run, in one lock and one persist (or zero writes to Transactions
// when nothing is due — WaiversProcessedThrough still advances). cfg
// supplies the mode (perf-priority | faab), season_weight_pct, and
// clear_days; games backs both the due-instant and drop-lock
// re-validation; poolByID resolves player identity for the Transaction
// snapshot and NFL-team lock/kickoff checks; rosterCap bounds a
// no-drop-named win.
//
// Determinism (test 15): waiverOrder is re-derived from the mutating,
// in-memory state after every win, so a later contest for the same add
// player, or a later claim by the winning team, always sees the fresh
// back-of-order penalty before its own turn resolves — "re-derive the
// order after each win before comparing the next contested player"
// (section 5.4 step 3).
func (s *Store) ProcessWaivers(now time.Time, cfg Config, games []GameInfo, poolByID map[string]Player, rosterCap int) ([]WaiverResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeErrorLocked(); err != nil {
		return nil, err
	}

	if len(s.state.WaiverClaims) == 0 {
		s.state.WaiversProcessedThrough = now.UTC()
		if err := s.persistLocked(colScalars); err != nil {
			return nil, err
		}
		return nil, nil
	}

	pending := append([]WaiverClaim(nil), s.state.WaiverClaims...)
	var due, notYetDue []WaiverClaim
	for _, c := range pending {
		player := poolByID[c.AddID]
		status := playerWaiverStatus(s.state, cfg, games, c.AddID, player.NFLTeam, now)
		if status.State == AvailabilityOnWaivers {
			notYetDue = append(notYetDue, c)
			continue
		}
		due = append(due, c)
	}

	var results []WaiverResult
	remaining := due
	for len(remaining) > 0 {
		order := waiverOrder(s.state, cfg)
		var budget map[string]int
		if cfg.Waivers.Mode == "faab" {
			budget = faabRemaining(s.state, cfg.Waivers.FAABBudget)
		}

		var next WaiverClaim
		next, remaining = pickNextClaim(remaining, order, cfg.Waivers.Mode)

		result := WaiverResult{Claim: next}
		owner := rosterOwner(currentRosters(s.state))
		addPlayer, addKnown := poolByID[next.AddID]
		var outgoing []string
		if next.DropID != "" {
			outgoing = []string{next.DropID}
		}
		limitPosition, limitCap, limitBreach := "", 0, false
		if addKnown {
			limitPosition, limitCap, limitBreach = teamWouldBreachLimit(s.state, poolByID, next.TeamID, []string{next.AddID}, outgoing)
		}

		switch {
		case owner[next.AddID] != "":
			result.Outcome = "beaten"
			result.WinningTeamID = owner[next.AddID]
		case !addKnown:
			result.Outcome = "failed"
			result.Reason = "that player is no longer available"
		case next.DropID != "" && owner[next.DropID] != next.TeamID:
			result.Outcome = "failed"
			result.Reason = lineupNotOnRosterMessage
		case next.DropID != "" && playerLocked(games, pickemWeekAt(games, now), poolByID[next.DropID].NFLTeam, now):
			result.Outcome = "failed"
			result.Reason = fmt.Sprintf("%s is locked and cannot be dropped until the week closes", poolByID[next.DropID].Name)
		case next.DropID == "" && effectiveRosterSize(s.state, next.TeamID)+1 > rosterCap:
			result.Outcome = "failed"
			result.Reason = "your roster is full; choose a player to drop"
		case cfg.Waivers.Mode == "faab" && next.Bid > budget[next.TeamID]:
			result.Outcome = "failed"
			result.Reason = fmt.Sprintf("your bid exceeds your remaining budget ($%d left)", budget[next.TeamID])
		case limitBreach:
			result.Outcome = "failed"
			result.Reason = limitMessage(limitPosition, limitCap)
		default:
			week := pickemWeekAt(games, now)
			txn := Transaction{
				Season: cfg.Season,
				Week:   week,
				Type:   "claim",
				TeamID: next.TeamID,
				Adds:   []TransactionPlayer{transactionPlayerFromPlayer(addPlayer)},
				By:     "manager",
				At:     now.UTC(),
			}
			if next.DropID != "" {
				txn.Drops = []TransactionPlayer{transactionPlayerFromPlayer(poolByID[next.DropID])}
			}
			if cfg.Waivers.Mode == "faab" {
				txn.Bid = next.Bid
				result.WinningBid = next.Bid
			} else {
				txn.Position = waiverOrderPosition(order, next.TeamID)
				result.Position = txn.Position
			}
			id, err := randomTransactionID()
			if err != nil {
				return results, err
			}
			txn.ID = id
			s.state.Transactions = append(s.state.Transactions, txn)
			s.clearZoneAssignmentsLocked(next.TeamID, outgoing)
			result.Outcome = "won"
			result.Week = week
		}
		results = append(results, result)
	}

	s.state.WaiverClaims = notYetDue
	s.state.WaiversProcessedThrough = now.UTC()
	if err := s.persistLocked(colWaiverClaims, colTransactions, colRosterZones, colScalars); err != nil {
		return nil, err
	}
	return results, nil
}

// ---------------------------------------------------------------------
// Trade offers (roster-ops spec section 6). Every content check here
// (T4-T8, T12) runs through validateTradeAssets — the one function the
// Service layer's exact-message validation, the propose write, the
// accept write, and the execution write (commissioner approve or the
// rosterOpsTick T-exec step, trades.go) all call, so "applied at propose,
// re-applied at accept, re-applied at execution" (section 6.3) is one
// function called three times, never three hand-duplicated rule sets.
// Actor-identity checks (T2, T3, T10, T11, T13) and the note-length check
// (T14) live at the Service layer only: they are derived fresh from the
// authenticated request on every call, never from stale client-submitted
// state, so there is no TOCTOU window for Store to re-close.
// ---------------------------------------------------------------------

// findTradeOfferIndexLocked returns offerID's index into s.state.TradeOffers,
// or -1. The caller must hold s.mu.
func (s *Store) findTradeOfferIndexLocked(offerID string) int {
	for index, offer := range s.state.TradeOffers {
		if offer.ID == offerID {
			return index
		}
	}
	return -1
}

// ProposeTradeOffer appends a new "open" offer (section 6.1's propose
// step), one lock, one persist, after re-validating its content under the
// lock (T4-T8, T12) — the propose-time half of "applied at propose."
func (s *Store) ProposeTradeOffer(offer TradeOffer, cfg Config, games []GameInfo, poolByID map[string]Player, starterCount, rosterCap int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeErrorLocked(); err != nil {
		return err
	}
	if !knownTeam(offer.FromTeamID) || !knownTeam(offer.ToTeamID) {
		return fmt.Errorf("unknown team")
	}
	if err := validateTradeAssets(s.state, cfg, games, poolByID, offer.CreatedAt, offer, starterCount, rosterCap); err != nil {
		return err
	}
	s.state.TradeOffers = append(s.state.TradeOffers, offer)
	return s.persistLocked(colTradeOffers)
}

// CounterTradeOffer implements section 6.1's counter step: the original
// offer must still be "open" (T9), the new (swapped-sides) offer's content
// re-validates (T4-T8, T12), and both status changes — original "open" →
// "countered", new offer appended "open" with ParentID set — commit in the
// same lock and persist ("one open offer chain").
func (s *Store) CounterTradeOffer(originalID string, newOffer TradeOffer, cfg Config, games []GameInfo, poolByID map[string]Player, now time.Time, starterCount, rosterCap int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeErrorLocked(); err != nil {
		return err
	}
	index := s.findTradeOfferIndexLocked(originalID)
	if index < 0 || s.state.TradeOffers[index].Status != TradeStatusOpen {
		return fmt.Errorf("this offer is no longer open")
	}
	if err := validateTradeAssets(s.state, cfg, games, poolByID, now, newOffer, starterCount, rosterCap); err != nil {
		return err
	}
	s.state.TradeOffers[index].Status = TradeStatusCountered
	s.state.TradeOffers[index].ResolvedAt = now.UTC()
	s.state.TradeOffers = append(s.state.TradeOffers, newOffer)
	return s.persistLocked(colTradeOffers)
}

// DeclineTradeOffer implements section 6.1's decline step: open → declined.
// Re-checks T9 (still open) under the lock; the actor check (T10-shaped,
// "only the receiving manager") already ran at the Service layer.
func (s *Store) DeclineTradeOffer(offerID string, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeErrorLocked(); err != nil {
		return err
	}
	index := s.findTradeOfferIndexLocked(offerID)
	if index < 0 || s.state.TradeOffers[index].Status != TradeStatusOpen {
		return fmt.Errorf("this offer is no longer open")
	}
	s.state.TradeOffers[index].Status = TradeStatusDeclined
	s.state.TradeOffers[index].ResolvedAt = now.UTC()
	return s.persistLocked(colTradeOffers)
}

// WithdrawTradeOffer implements section 6.1's withdraw step: open →
// withdrawn. Re-checks T9 under the lock; T11's actor check already ran at
// the Service layer.
func (s *Store) WithdrawTradeOffer(offerID string, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeErrorLocked(); err != nil {
		return err
	}
	index := s.findTradeOfferIndexLocked(offerID)
	if index < 0 || s.state.TradeOffers[index].Status != TradeStatusOpen {
		return fmt.Errorf("this offer is no longer open")
	}
	s.state.TradeOffers[index].Status = TradeStatusWithdrawn
	s.state.TradeOffers[index].ResolvedAt = now.UTC()
	return s.persistLocked(colTradeOffers)
}

// AcceptTradeOffer implements section 6.1's accept step: open → accepted,
// AcceptedAt set, starting the review window. Re-checks T9 and re-applies
// T4-T8/T12 under the lock (validateTradeAssets) — the accept-time half of
// "re-applied at accept." T10's actor check already ran at the Service
// layer.
func (s *Store) AcceptTradeOffer(offerID string, cfg Config, games []GameInfo, poolByID map[string]Player, now time.Time, starterCount, rosterCap int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeErrorLocked(); err != nil {
		return err
	}
	index := s.findTradeOfferIndexLocked(offerID)
	if index < 0 || s.state.TradeOffers[index].Status != TradeStatusOpen {
		return fmt.Errorf("this offer is no longer open")
	}
	offer := s.state.TradeOffers[index]
	if err := validateTradeAssets(s.state, cfg, games, poolByID, now, offer, starterCount, rosterCap); err != nil {
		return err
	}
	s.state.TradeOffers[index].Status = TradeStatusAccepted
	s.state.TradeOffers[index].AcceptedAt = now.UTC()
	return s.persistLocked(colTradeOffers)
}

// ExecuteTradeOffer implements section 6.1's execution step — shared by
// the commissioner's early "approve" action and rosterOpsTick's T-exec
// step, so both trigger paths run exactly one execution routine. Requires
// the offer to currently be "accepted"; a community veto that has already
// crossed threshold (FileTradeVetoOffer) has already moved it to "vetoed"
// by the time this runs, so an approve racing a completed veto sees a
// non-"accepted" status here and fails closed — the "both" mode's
// "approve does not override a completed community veto" rule, and the
// section 6.1 amendment's "whichever mutation persists first wins,
// deterministically" race resolution, both fall out of this one status
// check under one lock.
//
// On success: re-validates content (T5-T8; T4/T12 cannot newly fail post-
// propose) and, only then, appends one "trade" Transaction covering both
// sides and marks the offer executed, in the same persist ("executed
// trades append ONE Type trade transaction ... in the same persist that
// resolves the offer"). On a re-validation failure — a player moved or
// locked since accept — fails closed: the offer moves to "failed" with
// FailReason set, in the same persist, and no Transaction is appended
// ("never half-applies").
func (s *Store) ExecuteTradeOffer(offerID string, cfg Config, games []GameInfo, poolByID map[string]Player, now time.Time, starterCount, rosterCap int) (Transaction, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeErrorLocked(); err != nil {
		return Transaction{}, err
	}
	index := s.findTradeOfferIndexLocked(offerID)
	if index < 0 || s.state.TradeOffers[index].Status != TradeStatusAccepted {
		return Transaction{}, fmt.Errorf("this trade is no longer under review")
	}
	offer := s.state.TradeOffers[index]
	if err := validateTradeAssets(s.state, cfg, games, poolByID, now, offer, starterCount, rosterCap); err != nil {
		s.state.TradeOffers[index].Status = TradeStatusFailed
		s.state.TradeOffers[index].FailReason = err.Error()
		s.state.TradeOffers[index].ResolvedAt = now.UTC()
		if perr := s.persistLocked(colTradeOffers); perr != nil {
			return Transaction{}, perr
		}
		return Transaction{}, err
	}
	week := pickemWeekAt(games, now)
	id, err := randomTransactionID()
	if err != nil {
		return Transaction{}, err
	}
	txn := Transaction{
		ID:          id,
		Season:      cfg.Season,
		Week:        week,
		Type:        "trade",
		TeamID:      offer.FromTeamID,
		OtherTeamID: offer.ToTeamID,
		Adds:        transactionPlayersFromIDs(poolByID, offer.Get),
		Drops:       transactionPlayersFromIDs(poolByID, offer.Give),
		OfferID:     offer.ID,
		By:          "manager",
		At:          now.UTC(),
	}
	s.state.Transactions = append(s.state.Transactions, txn)
	s.state.TradeOffers[index].Status = TradeStatusExecuted
	s.state.TradeOffers[index].ResolvedAt = now.UTC()
	// A traded player never carries their zone tag to the receiving team
	// (they land in the general pool, like any other acquisition) — clear
	// both sides' outgoing players' zone assignments in this same persist.
	s.clearZoneAssignmentsLocked(offer.FromTeamID, offer.Give)
	s.clearZoneAssignmentsLocked(offer.ToTeamID, offer.Get)
	if err := s.persistLocked(colTradeOffers, colTransactions, colRosterZones); err != nil {
		return Transaction{}, err
	}
	return txn, nil
}

// CommissionerVetoTradeOffer implements section 6.1's veto step under
// commissioner or both mode: accepted → vetoed. Requires the offer to
// currently be "accepted" — an offer already executed by a racing approve
// fails closed here, the same store-lock-order race resolution
// ExecuteTradeOffer's doc comment describes.
func (s *Store) CommissionerVetoTradeOffer(offerID string, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeErrorLocked(); err != nil {
		return err
	}
	index := s.findTradeOfferIndexLocked(offerID)
	if index < 0 || s.state.TradeOffers[index].Status != TradeStatusAccepted {
		return fmt.Errorf("this trade is no longer under review")
	}
	s.state.TradeOffers[index].Status = TradeStatusVetoed
	s.state.TradeOffers[index].ResolvedAt = now.UTC()
	return s.persistLocked(colTradeOffers)
}

// FileTradeVetoOffer implements section 6.1's veto step under vote or both
// mode: one veto per non-party seat, accumulated in Vetoes. Voting twice
// from the same team is a harmless no-op (the CancelClaim idempotent
// precedent). Once len(Vetoes) reaches threshold the offer flips straight
// to "vetoed" in this same lock and persist — the vote that crosses the
// line performs the accepted → vetoed transition itself, synchronously, so
// there is no separate tick step for vote-mode vetoes and no window for a
// racing approve to see a stale "accepted" status once threshold is met.
func (s *Store) FileTradeVetoOffer(offerID, teamID string, now time.Time, threshold int) (crossed bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeErrorLocked(); err != nil {
		return false, err
	}
	index := s.findTradeOfferIndexLocked(offerID)
	if index < 0 || s.state.TradeOffers[index].Status != TradeStatusAccepted {
		return false, fmt.Errorf("this trade is no longer under review")
	}
	for _, voter := range s.state.TradeOffers[index].Vetoes {
		if voter == teamID {
			return false, nil
		}
	}
	s.state.TradeOffers[index].Vetoes = append(s.state.TradeOffers[index].Vetoes, teamID)
	if len(s.state.TradeOffers[index].Vetoes) >= threshold {
		s.state.TradeOffers[index].Status = TradeStatusVetoed
		s.state.TradeOffers[index].ResolvedAt = now.UTC()
		crossed = true
	}
	if err := s.persistLocked(colTradeOffers); err != nil {
		return false, err
	}
	return crossed, nil
}

// ExpireTradeOffer implements section 6.1's T-expire step for one open
// offer: 7 days since CreatedAt, or the configured trade deadline,
// whichever comes first. Re-derives the due predicate under the lock
// (now, cfg) rather than trusting the caller's snapshot-time judgment —
// the re-validate-under-lock discipline every mutation in this file uses.
// Returns (false, nil), not an error, when the offer no longer qualifies
// (already resolved by a concurrent action, or not actually due yet):
// rosterOpsTick's caller only invokes this for offers its own snapshot
// judged due, so a false return here just means another mutation raced it.
func (s *Store) ExpireTradeOffer(offerID string, cfg Config, now time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeErrorLocked(); err != nil {
		return false, err
	}
	index := s.findTradeOfferIndexLocked(offerID)
	if index < 0 || s.state.TradeOffers[index].Status != TradeStatusOpen {
		return false, nil
	}
	offer := s.state.TradeOffers[index]
	due := now.Sub(offer.CreatedAt) >= tradeOfferMaxAge
	if deadline, ok := parseTradeDeadline(cfg); ok && !now.Before(deadline) {
		due = true
	}
	if !due {
		return false, nil
	}
	s.state.TradeOffers[index].Status = TradeStatusExpired
	s.state.TradeOffers[index].ResolvedAt = now.UTC()
	if err := s.persistLocked(colTradeOffers); err != nil {
		return false, err
	}
	return true, nil
}

// announcementBodyMaxRunes and announcementCap bound one announcement's
// text and the feed's stored length (league-announcements spec).
const (
	announcementBodyMaxRunes = 500
	announcementCap          = 20
)

// announcementID derives a short, stable content-hash ID for a new
// announcement: the same "first 8 hex bytes of SHA-256" shape orderHash8
// already uses for the draft order (notifications.go), applied to the
// post's body, author, and instant so two different posts never collide.
func announcementID(body, postedBy string, postedAt time.Time) string {
	sum := sha256.Sum256([]byte(body + "|" + postedBy + "|" + postedAt.UTC().Format(time.RFC3339Nano)))
	return "ann-" + hex.EncodeToString(sum[:8])
}

// PostAnnouncement records a new league announcement at the front of the
// feed (newest first) and trims the list to announcementCap entries,
// dropping the oldest. body is trimmed and must be non-empty and no more
// than announcementBodyMaxRunes runes; postedBy is the acting
// commissioner's identity (the service layer resolves it — see
// Service.commissionerProvenance).
func (s *Store) PostAnnouncement(body, postedBy string, now time.Time) (Announcement, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return Announcement{}, fmt.Errorf("announcement text is required")
	}
	if runes := []rune(body); len(runes) > announcementBodyMaxRunes {
		return Announcement{}, fmt.Errorf("announcements must be %d characters or fewer", announcementBodyMaxRunes)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeErrorLocked(); err != nil {
		return Announcement{}, err
	}
	announcement := Announcement{
		ID:       announcementID(body, postedBy, now),
		Body:     body,
		PostedAt: now.UTC(),
		PostedBy: postedBy,
	}
	s.state.Announcements = append([]Announcement{announcement}, s.state.Announcements...)
	if len(s.state.Announcements) > announcementCap {
		s.state.Announcements = s.state.Announcements[:announcementCap]
	}
	if err := s.persistLocked(colAnnouncements); err != nil {
		return Announcement{}, err
	}
	return announcement, nil
}

// DeleteAnnouncement removes one announcement by ID. Deleting an ID that
// does not exist is a harmless no-op, matching ReleaseBadge's idempotent
// precedent.
func (s *Store) DeleteAnnouncement(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeErrorLocked(); err != nil {
		return err
	}
	kept := make([]Announcement, 0, len(s.state.Announcements))
	for _, a := range s.state.Announcements {
		if a.ID != id {
			kept = append(kept, a)
		}
	}
	s.state.Announcements = kept
	return s.persistLocked(colAnnouncements)
}

// persistLocked writes every collection named in cols, plus any
// collection an earlier failed attempt left marked, in one transaction.
//
// cols is the mutator's declaration of what it touched. It is the whole
// dirty-tracking scheme: a mutator that changes picks and the clock calls
// persistLocked(colPicks, colScalars), and only those two collections'
// rows are emitted and diffed. Declaring a collection that did not change
// costs nothing (the row diff finds no change and writes nothing);
// failing to declare one that did change would skip the write, so the
// package's own tests run with sqlitePersistVerify on, which re-reads the
// database after every persist and fails loudly on any mismatch.
//
// The marks survive a failed write and clear only after a commit reports
// success, so a retry always covers everything still unwritten.
func (s *Store) persistLocked(cols ...collectionID) error {
	if err := s.writeErrorLocked(); err != nil {
		if s.persistencePoison == nil {
			return err
		}
		// Mutators use copy-on-write only for avatar identity; the older
		// Store mutators update their working state before calling this
		// common persistence boundary. Restore the poisoned baseline here so
		// none of those attempted writes can publish an additional in-memory
		// candidate after the fatal identity ambiguity.
		s.state = cloneState(s.poisonedState)
		s.dirty = s.poisonedDirty
		return err
	}
	for _, id := range cols {
		s.dirty |= 1 << uint(id)
	}
	if s.db == nil {
		return nil
	}
	if s.dirty == 0 {
		return nil
	}
	if err := s.writeDirtyLocked(); err != nil {
		// Keep the latest ordinary failure visible to readiness. A later
		// successful write is the only place that clears it; a no-op or
		// validation-only call must not make health look recovered.
		s.persistenceWriteError = err
		return err
	}
	// A successful SQLite transaction with no changed rows proves only that
	// an empty diff was accepted; it does not prove that the earlier failed
	// write path is healthy again. Clear the health error only after this
	// invocation actually changed persisted rows. Identity mutations have a
	// separate authoritative reconciliation path above that clears the error
	// when it proves the desired pair landed.
	if s.lastPersistRows > 0 {
		s.persistenceWriteError = nil
	}
	return nil
}

func cloneState(in PersistedState) PersistedState {
	out := PersistedState{
		persistenceAuthority:    in.persistenceAuthority,
		SchemaVersion:           in.SchemaVersion,
		Ready:                   make(map[string]bool, len(in.Ready)),
		Picks:                   append([]DraftPick(nil), in.Picks...),
		Members:                 make(map[string]Member, len(in.Members)),
		Invites:                 append([]string(nil), in.Invites...),
		Boards:                  make(map[string][]string, len(in.Boards)),
		TeamNames:               make(map[string]string, len(in.TeamNames)),
		DraftOrder:              append([]string(nil), in.DraftOrder...),
		Scoring:                 make(map[string]float64, len(in.Scoring)),
		DraftStarted:            in.DraftStarted,
		DraftStartedAt:          in.DraftStartedAt,
		Pickems:                 make(map[string]map[string]string, len(in.Pickems)),
		BlitzEntries:            make(map[string]map[string]BlitzEntry, len(in.BlitzEntries)),
		ClockDeadline:           in.ClockDeadline,
		ClockPaused:             in.ClockPaused,
		ClockRemainingSec:       in.ClockRemainingSec,
		ClockDurationSec:        in.ClockDurationSec,
		Autopick:                make(map[string]bool, len(in.Autopick)),
		SentLog:                 make(map[string]time.Time, len(in.SentLog)),
		NotifyPrefs:             make(map[string]map[string]bool, len(in.NotifyPrefs)),
		ScoringChangedAt:        in.ScoringChangedAt,
		Schedule:                cloneSchedule(in.Schedule),
		Playoffs:                clonePlayoffState(in.Playoffs),
		Phase:                   in.Phase,
		BadgeClaims:             make(map[string]string, len(in.BadgeClaims)),
		AvatarRefs:              make(map[string]string, len(in.AvatarRefs)),
		RosterOverride:          cloneRosterOverride(in.RosterOverride),
		Announcements:           append([]Announcement(nil), in.Announcements...),
		Lineups:                 make(map[string]map[int]map[string]string, len(in.Lineups)),
		Transactions:            make([]Transaction, len(in.Transactions)),
		WaiverClaims:            make([]WaiverClaim, len(in.WaiverClaims)),
		WaiversProcessedThrough: in.WaiversProcessedThrough,
		TradeOffers:             make([]TradeOffer, len(in.TradeOffers)),
		RosterZones:             make(map[string]map[string]ZoneAssignment, len(in.RosterZones)),
		CoInvites:               make(map[string]string, len(in.CoInvites)),
		TrimmedTeamIDs:          append([]string(nil), in.TrimmedTeamIDs...),
	}
	for key, value := range in.Ready {
		out.Ready[key] = value
	}
	for key, value := range in.Members {
		out.Members[key] = value
	}
	for key, value := range in.Boards {
		out.Boards[key] = append([]string(nil), value...)
	}
	for key, value := range in.TeamNames {
		out.TeamNames[key] = value
	}
	for key, value := range in.Scoring {
		out.Scoring[key] = value
	}
	for owner, picks := range in.Pickems {
		inner := make(map[string]string, len(picks))
		for gameID, team := range picks {
			inner[gameID] = team
		}
		out.Pickems[owner] = inner
	}
	for owner, bySlate := range in.BlitzEntries {
		inner := make(map[string]BlitzEntry, len(bySlate))
		for slate, entry := range bySlate {
			inner[slate] = BlitzEntry{
				Players:   append([]string(nil), entry.Players...),
				UpdatedAt: entry.UpdatedAt,
			}
		}
		out.BlitzEntries[owner] = inner
	}
	for key, value := range in.Autopick {
		out.Autopick[key] = value
	}
	for key, value := range in.SentLog {
		out.SentLog[key] = value
	}
	for email, prefs := range in.NotifyPrefs {
		inner := make(map[string]bool, len(prefs))
		for category, enabled := range prefs {
			inner[category] = enabled
		}
		out.NotifyPrefs[email] = inner
	}
	for teamID, motif := range in.BadgeClaims {
		out.BadgeClaims[teamID] = motif
	}
	for teamID, ref := range in.AvatarRefs {
		out.AvatarRefs[teamID] = ref
	}
	for teamID, byWeek := range in.Lineups {
		innerByWeek := make(map[int]map[string]string, len(byWeek))
		for week, slots := range byWeek {
			innerSlots := make(map[string]string, len(slots))
			for slot, playerID := range slots {
				innerSlots[slot] = playerID
			}
			innerByWeek[week] = innerSlots
		}
		out.Lineups[teamID] = innerByWeek
	}
	for index, txn := range in.Transactions {
		out.Transactions[index] = Transaction{
			ID:          txn.ID,
			Season:      txn.Season,
			Week:        txn.Week,
			Type:        txn.Type,
			TeamID:      txn.TeamID,
			OtherTeamID: txn.OtherTeamID,
			Adds:        append([]TransactionPlayer(nil), txn.Adds...),
			Drops:       append([]TransactionPlayer(nil), txn.Drops...),
			Bid:         txn.Bid,
			Position:    txn.Position,
			OfferID:     txn.OfferID,
			By:          txn.By,
			Note:        txn.Note,
			At:          txn.At,
		}
	}
	// WaiverClaim carries only value fields, so a plain element copy is
	// already a full deep copy — the same "make, then copy" shape as
	// Transactions above, chosen over append([]WaiverClaim(nil), ...) so a
	// non-nil-but-empty in.WaiverClaims clones to a non-nil, empty slice
	// too (append with zero elements to append returns the nil it started
	// from, which would silently break the old-state-file-load contract).
	copy(out.WaiverClaims, in.WaiverClaims)
	// TradeOffer carries slice fields (Give/Get/Picks/Vetoes), so a plain
	// element copy would still alias them; copy each field explicitly, the
	// same shape Transactions above uses for its Adds/Drops slices.
	for index, offer := range in.TradeOffers {
		out.TradeOffers[index] = TradeOffer{
			ID:         offer.ID,
			FromTeamID: offer.FromTeamID,
			ToTeamID:   offer.ToTeamID,
			Give:       append([]string(nil), offer.Give...),
			Get:        append([]string(nil), offer.Get...),
			Picks:      append([]string(nil), offer.Picks...),
			Note:       offer.Note,
			Status:     offer.Status,
			ParentID:   offer.ParentID,
			Vetoes:     append([]string(nil), offer.Vetoes...),
			FailReason: offer.FailReason,
			CreatedAt:  offer.CreatedAt,
			AcceptedAt: offer.AcceptedAt,
			ResolvedAt: offer.ResolvedAt,
		}
	}
	for teamID, zones := range in.RosterZones {
		out.RosterZones[teamID] = cloneZoneAssignments(zones)
	}
	for email, teamID := range in.CoInvites {
		out.CoInvites[email] = teamID
	}
	sort.Slice(out.Picks, func(i, j int) bool { return out.Picks[i].Number < out.Picks[j].Number })
	return out
}

func knownTeam(teamID string) bool {
	for _, team := range defaultTeams() {
		if team.ID == teamID {
			return true
		}
	}
	return false
}

// activeTeamCount resolves the team count backing draft-order math: the
// commissioner-drawn order's length when set, otherwise the default
// team-ID count. This is the one place round math and team-count math
// resolve (competition-formats spec section 1.3); MakePick, AutoPick, and
// DraftData's round display all call pickRound(activeTeamCount(...), ...)
// instead of separately assuming len(defaultTeams()) or len(s.teams)
// (items 3 and 11 of the spec's hardcoded-8 inventory).
func activeTeamCount(order []string) int {
	if len(order) == 0 {
		return len(defaultTeamIDs())
	}
	return len(order)
}

// pickRound resolves a pick number's round under a snake draft with
// teamCount active teams.
func pickRound(teamCount, number int) int {
	if teamCount <= 0 {
		teamCount = 1
	}
	return ((number - 1) / teamCount) + 1
}

// teamOnClock resolves the team due to pick at pickNumber under a snake
// draft. order is a slice of team IDs; a nil or empty order falls back to
// the default team-ID order (team-1..team-8).
func teamOnClock(order []string, pickNumber int) string {
	ids := order
	if len(ids) == 0 {
		ids = defaultTeamIDs()
	}
	if pickNumber < 1 {
		pickNumber = 1
	}
	index := (pickNumber - 1) % len(ids)
	round := (pickNumber-1)/len(ids) + 1
	if round%2 == 0 {
		index = len(ids) - 1 - index
	}
	return ids[index]
}
