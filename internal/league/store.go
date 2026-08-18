package league

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

var ErrLeagueFull = errors.New("all league seats are already assigned")

// errStaleAutoPick means an optimistic re-validation inside AutoPick failed:
// a human pick, a pause, or a clock event raced in between the ticker's
// snapshot and the write. The caller drops the pick; the next tick
// re-evaluates from a fresh snapshot. See AutoPick.
var errStaleAutoPick = errors.New("auto-pick is stale")

// currentSchemaVersion is the state file schema version this binary writes
// and the highest version it accepts on load. See PersistedState's
// SchemaVersion doc comment and Store.load.
const currentSchemaVersion = 2

// errSchemaTooNew is returned by NewStore/load when the state file's
// SchemaVersion exceeds currentSchemaVersion: an older binary must not
// silently drop fields a newer one wrote (section 6.3).
var errSchemaTooNew = errors.New("state file schema version is newer than this binary supports")

// Store keeps the starter deployable without a database while preserving
// state across restarts. Its methods are safe for concurrent requests.
type Store struct {
	mu       sync.RWMutex
	filePath string
	state    PersistedState
	// loadErr holds a load() failure the constructor could not recover
	// from: today, only errSchemaTooNew (section 6.3). persistLocked
	// refuses to write while it is set, so a downgraded binary can never
	// clobber a newer-schema file with blank in-memory state; see
	// StartupError.
	loadErr error
}

func NewStore(filePath string) *Store {
	s := &Store{
		filePath: strings.TrimSpace(filePath),
		state: PersistedState{
			SchemaVersion: currentSchemaVersion,
			Ready:         map[string]bool{},
			Picks:         []DraftPick{},
			Members:       map[string]Member{},
			Invites:       []string{},
			Boards:        map[string][]string{},
			TeamNames:     map[string]string{},
			DraftOrder:    []string{},
			Scoring:       map[string]float64{},
			Pickems:       map[string]map[string]string{},
			BlitzEntries:  map[string]map[string]BlitzEntry{},
			Autopick:      map[string]bool{},
			SentLog:       map[string]time.Time{},
			NotifyPrefs:   map[string]map[string]bool{},
			BadgeClaims:   map[string]string{},
			Announcements: []Announcement{},
			Lineups:       map[string]map[int]map[string]string{},
			Transactions:  []Transaction{},
			WaiverClaims:  []WaiverClaim{},
		},
	}
	if err := s.load(); err != nil {
		s.loadErr = err
	}
	return s
}

// StartupError reports a load failure the constructor could not recover
// from safely: today, only a state file whose SchemaVersion exceeds
// currentSchemaVersion (section 6.3 — "refuse to start with a clear
// error"). Every write method fails with the same error while this is set,
// protecting the on-disk file from a downgraded binary silently
// overwriting it with blank in-memory state. A malformed (non-JSON) file
// still decodes to the pre-existing "start fresh" behavior, unchanged by
// this work package.
func (s *Store) StartupError() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.loadErr
}

func (s *Store) Snapshot() PersistedState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneState(s.state)
}

func (s *Store) ToggleReady(teamID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !knownTeam(teamID) {
		return false, fmt.Errorf("unknown team %q", teamID)
	}
	s.state.Ready[teamID] = !s.state.Ready[teamID]
	return s.state.Ready[teamID], s.persistLocked()
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
	s.state.Picks = append(s.state.Picks, pick)
	s.state.ClockDeadline = nextDeadline
	return pick, s.persistLocked()
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
	if len(s.state.Picks) == 0 {
		return errors.New("no picks to undo")
	}
	// Best-effort backup: a failure here must not block the undo itself.
	_ = s.backupSnapshotLocked()
	s.state.Picks = s.state.Picks[:len(s.state.Picks)-1]
	if !s.state.ClockPaused {
		s.state.ClockDeadline = nextDeadline
		s.state.ClockRemainingSec = 0
	}
	return s.persistLocked()
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
	s.state.Picks = append(s.state.Picks, pick)
	s.state.ClockDeadline = nextDeadline
	return pick, s.persistLocked()
}

// ArmClock sets the deadline directly. It backs both the ticker's "arm an
// unarmed clock" step and the restart-recovery boot logic; it does not
// touch ClockPaused or ClockRemainingSec.
func (s *Store) ArmClock(deadline time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.ClockDeadline = deadline
	return s.persistLocked()
}

// PauseClock stops the running deadline, storing the remaining seconds
// (floored at zero) so a resume can restore it. Persisted, so a restart
// stays paused. Pauses an unarmed clock harmlessly (remaining stays 0).
func (s *Store) PauseClock(now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
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
	return s.persistLocked()
}

// ResumeClock restores the deadline from the stored remaining seconds, or
// grants the full duration when nothing was captured (an unarmed clock, or
// a fresh on-clock team after a manual pick during a pause). In demo mode
// this doubles as "start the clock."
func (s *Store) ResumeClock(now time.Time, duration time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	remaining := time.Duration(s.state.ClockRemainingSec) * time.Second
	if remaining <= 0 {
		remaining = duration
	}
	s.state.ClockDeadline = now.Add(remaining)
	s.state.ClockPaused = false
	s.state.ClockRemainingSec = 0
	return s.persistLocked()
}

// ExtendClock adds delta to the running deadline, clamped to
// [now+MinPickClock, now+MaxPickClock]. A paused or unarmed clock rejects
// the extend; there is no running deadline to extend.
func (s *Store) ExtendClock(now time.Time, delta time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
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
	return s.persistLocked()
}

// SetClockDuration overrides the persisted pick-clock duration, clamped to
// [MinPickClock, MaxPickClock]. It applies starting with the next arm; the
// running deadline is untouched.
func (s *Store) SetClockDuration(seconds int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	duration := clampPickClock(time.Duration(seconds) * time.Second)
	s.state.ClockDurationSec = int(duration.Seconds())
	return s.persistLocked()
}

// ClearClock zeroes every clock field without touching picks or Autopick.
// The draft-completion path calls this once, when a leftover deadline from
// the final pick needs clearing.
func (s *Store) ClearClock() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.ClockDeadline.IsZero() && !s.state.ClockPaused && s.state.ClockRemainingSec == 0 {
		return nil
	}
	s.state.ClockDeadline = time.Time{}
	s.state.ClockPaused = false
	s.state.ClockRemainingSec = 0
	return s.persistLocked()
}

// SetAutopick toggles a team's away-mode auto-pick flag.
func (s *Store) SetAutopick(teamID string, on bool) error {
	if !knownTeam(teamID) {
		return fmt.Errorf("unknown team %q", teamID)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if on {
		s.state.Autopick[teamID] = true
	} else {
		delete(s.state.Autopick, teamID)
	}
	return s.persistLocked()
}

// AssignMember binds email to the first open seat, or returns the existing
// member when the email already holds one. created reports whether this
// call bound a brand-new member — the one-lock check-and-write keeps the
// report atomic, so two racing callers for the same new email cannot both
// see created == true (the N1 seat-claimed notification's hook relies on
// this; see service.go's assignMember).
func (s *Store) AssignMember(email, name string) (member Member, created bool, err error) {
	email = strings.ToLower(strings.TrimSpace(email))
	name = strings.TrimSpace(name)
	if email == "" {
		return Member{}, false, fmt.Errorf("email is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.state.Members[email]; ok {
		if name != "" && existing.Name != name {
			existing.Name = name
			s.state.Members[email] = existing
			_ = s.persistLocked()
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
		return newMember, true, s.persistLocked()
	}
	return Member{}, false, ErrLeagueFull
}

func (s *Store) MemberByEmail(email string) (Member, bool) {
	email = strings.ToLower(strings.TrimSpace(email))
	s.mu.RLock()
	defer s.mu.RUnlock()
	member, ok := s.state.Members[email]
	return member, ok
}

// AddInvite records a manager email that may claim a seat.
func (s *Store) AddInvite(email string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" || !strings.Contains(email, "@") {
		return fmt.Errorf("enter a valid email address")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.state.Invites {
		if existing == email {
			return nil
		}
	}
	s.state.Invites = append(s.state.Invites, email)
	return s.persistLocked()
}

// RemoveInvite drops an email from the invite list. Existing seat claims stay.
func (s *Store) RemoveInvite(email string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := s.state.Invites[:0]
	for _, existing := range s.state.Invites {
		if existing != email {
			kept = append(kept, existing)
		}
	}
	s.state.Invites = kept
	return s.persistLocked()
}

// Invited reports whether the email is on the stored invite list.
func (s *Store) Invited(email string) bool {
	email = strings.ToLower(strings.TrimSpace(email))
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, existing := range s.state.Invites {
		if existing == email {
			return true
		}
	}
	return false
}

// ReleaseSeat unbinds the member holding the team and clears its ready flag.
func (s *Store) ReleaseSeat(teamID string) error {
	if !knownTeam(teamID) {
		return fmt.Errorf("unknown team %q", teamID)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for email, member := range s.state.Members {
		if member.TeamID == teamID {
			delete(s.state.Members, email)
		}
	}
	delete(s.state.Ready, teamID)
	return s.persistLocked()
}

// resetDraftSentLogPrefixes are the SentLog key prefixes ResetDraft drops:
// a re-run draft is a new subject entity. draftrem: (reminder keys) is
// deliberately absent: same draftAt, same reminder, no resend (spec
// section 6.2). "waiver:" (N14) and "lineupwarn:" (N18, a WP-R2 omission
// this closes) join the list under the roster-ops spec section 7.3: every
// waiver claim and lineup reference a rostered player, and a redrawn draft
// orphans them all. WP-R5 appends "tradeoffer:", "tradedone:", and
// "tradeveto:" when trades land — out of this work package's scope.
var resetDraftSentLogPrefixes = []string{"onclock:", "autopick:", "draftdone:", "waiver:", "lineupwarn:"}

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
	s.clearClockFieldsLocked()
	s.pruneSentLogPrefixesLocked(resetDraftSentLogPrefixes...)
	return s.persistLocked()
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
	s.clearClockFieldsLocked()
	s.pruneSentLogPrefixesLocked(resetLeagueSentLogPrefixes...)
	return s.persistLocked()
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

// SetTeamName overrides a team's display name. An empty name clears the
// override and restores the default.
func (s *Store) SetTeamName(teamID, name string) error {
	if !knownTeam(teamID) {
		return fmt.Errorf("unknown team %q", teamID)
	}
	name = strings.TrimSpace(name)
	if len(name) > 40 {
		return fmt.Errorf("team names must be 40 characters or fewer")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if name == "" {
		delete(s.state.TeamNames, teamID)
	} else {
		s.state.TeamNames[teamID] = name
	}
	return s.persistLocked()
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
	if len(s.state.Picks) > 0 {
		return fmt.Errorf("reset the draft before changing the order")
	}
	s.state.DraftOrder = append([]string(nil), order...)
	return s.persistLocked()
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
	if points == rule.Points {
		delete(s.state.Scoring, key)
	} else {
		s.state.Scoring[key] = points
	}
	return s.persistLocked()
}

// ResetScoring clears every scoring override, restoring the default rules.
func (s *Store) ResetScoring() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.Scoring = map[string]float64{}
	return s.persistLocked()
}

const boardLimit = 100

// BoardAdd appends a player to the owner's ranked board.
func (s *Store) BoardAdd(owner, playerID string) error {
	if owner == "" || playerID == "" {
		return fmt.Errorf("board owner and player are required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
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
	return s.persistLocked()
}

// BoardMove shifts a player up (delta -1) or down (delta +1) on the board.
func (s *Store) BoardMove(owner, playerID string, delta int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
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
		return s.persistLocked()
	}
	return fmt.Errorf("that player is not on the board")
}

// BoardRemove drops a player from the owner's board.
func (s *Store) BoardRemove(owner, playerID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	board := s.state.Boards[owner]
	kept := board[:0]
	for _, existing := range board {
		if existing != playerID {
			kept = append(kept, existing)
		}
	}
	s.state.Boards[owner] = kept
	return s.persistLocked()
}

// BoardClear removes every player from the owner's board.
func (s *Store) BoardClear(owner string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.state.Boards, owner)
	return s.persistLocked()
}

// SetPickem records the owner's pick for one game. It does not validate the
// game or the team against the schedule; the service layer owns that.
func (s *Store) SetPickem(owner, gameID, team string) error {
	if owner == "" || gameID == "" {
		return fmt.Errorf("pick owner and game are required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.Pickems[owner] == nil {
		s.state.Pickems[owner] = map[string]string{}
	}
	s.state.Pickems[owner][gameID] = team
	return s.persistLocked()
}

// BlitzSetEntry replaces owner's slate entry wholesale. It performs no
// validation — the service layer owns that (SetPickem precedent, F9).
// Two-tab last-write-wins is accepted for eight users (R8). An empty
// players slice still records the entry, with UpdatedAt moved to now; the
// service layer's BlitzRemove relies on that to persist a player removal.
func (s *Store) BlitzSetEntry(owner, slate string, players []string, now time.Time) error {
	if owner == "" || slate == "" {
		return fmt.Errorf("blitz entry owner and slate are required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.BlitzEntries[owner] == nil {
		s.state.BlitzEntries[owner] = map[string]BlitzEntry{}
	}
	s.state.BlitzEntries[owner][slate] = BlitzEntry{
		Players:   append([]string(nil), players...),
		UpdatedAt: now.UTC(),
	}
	return s.persistLocked()
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
	if _, exists := s.state.SentLog[key]; exists {
		return false, nil
	}
	s.state.SentLog[key] = now.UTC()
	if err := s.persistLocked(); err != nil {
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
	if err := s.persistLocked(); err != nil {
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
	return s.persistLocked()
}

// SetNotifyPref records one member's category preference. Only overrides
// are stored (spec section 7.1); the league.json default and the catalog
// default apply when nothing is stored here.
func (s *Store) SetNotifyPref(email, category string, enabled bool) error {
	email = strings.ToLower(strings.TrimSpace(email))
	category = strings.TrimSpace(category)
	if email == "" {
		return fmt.Errorf("email is required")
	}
	if category == "" {
		return fmt.Errorf("category is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.NotifyPrefs[email] == nil {
		s.state.NotifyPrefs[email] = map[string]bool{}
	}
	s.state.NotifyPrefs[email][category] = enabled
	return s.persistLocked()
}

// SetSchedule replaces the persisted regular-season schedule wholesale. It
// does not validate the schedule's shape (GenerateSchedule already did);
// callers that need the section 2.3 regeneration guard (season not
// started, no matchup final yet) enforce it before calling this — see
// AdminGenerateSchedule / AdminRegenerateSchedule.
func (s *Store) SetSchedule(sch SeasonSchedule) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.Schedule = cloneSchedule(&sch)
	return s.persistLocked()
}

// SetScheduleWeek replaces one week's data (matchups, scores, bye) in the
// persisted schedule, matched by week.Week. It fails when no schedule
// exists or the week number is not part of it; see season.go's closeWeek.
func (s *Store) SetScheduleWeek(week ScheduleWeek) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.Schedule == nil {
		return fmt.Errorf("no schedule has been generated")
	}
	for i, wk := range s.state.Schedule.Weeks {
		if wk.Week == week.Week {
			stored := week
			stored.Matchups = append([]LeagueMatchup(nil), week.Matchups...)
			s.state.Schedule.Weeks[i] = stored
			return s.persistLocked()
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
	return s.persistLocked()
}

// SetPhase overrides the persisted season phase (section 5.2). season.go
// and playoffs.go call this at the transitions this work package drives;
// it performs no validation of the transition itself, matching the
// existing store idiom of pushing invariant checks to the service layer.
func (s *Store) SetPhase(phase string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.Phase = phase
	return s.persistLocked()
}

// SetPlayoffs replaces the persisted playoff bracket state wholesale.
func (s *Store) SetPlayoffs(state PlayoffState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.Playoffs = clonePlayoffState(&state)
	return s.persistLocked()
}

// ClaimBadge sets teamID's badge claim to motif, first-come-first-served:
// a motif already held by a different team is rejected with a
// badgeClaimedError naming that team's ID (Service.ClaimBadge resolves
// the display name and builds the exact-message error — Store has no
// business rendering a display name, matching every other store method's
// "push the view-layer concern up" split). Setting a motif this team
// already holds, or a different one, always overwrites teamID's own
// previous claim — an implicit swap that frees the old motif in the same
// transaction, matching ToggleReady's lock-then-persist shape.
func (s *Store) ClaimBadge(teamID, motif string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !knownTeam(teamID) {
		return fmt.Errorf("unknown team %q", teamID)
	}
	if !knownMotif(motif) {
		return ErrBadgeUnknownMotif
	}
	for holder, claimed := range s.state.BadgeClaims {
		if claimed == motif && holder != teamID {
			return &badgeClaimedError{teamID: holder}
		}
	}
	if s.state.BadgeClaims == nil {
		s.state.BadgeClaims = map[string]string{}
	}
	s.state.BadgeClaims[teamID] = motif
	return s.persistLocked()
}

// ReleaseBadge clears teamID's badge claim. Releasing a seat with no
// claim is a harmless no-op, not an error — matching AdminResetAvatar's
// idempotent-reset precedent.
func (s *Store) ReleaseBadge(teamID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !knownTeam(teamID) {
		return fmt.Errorf("unknown team %q", teamID)
	}
	delete(s.state.BadgeClaims, teamID)
	return s.persistLocked()
}

// BadgeClaim reports teamID's currently claimed motif slug, if any.
func (s *Store) BadgeClaim(teamID string) (motif string, ok bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	motif, ok = s.state.BadgeClaims[teamID]
	return motif, ok
}

// BadgeClaims returns a snapshot of every team ID's current badge claim,
// for the picker UI's grid (which team, if any, holds each motif).
func (s *Store) BadgeClaims() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]string, len(s.state.BadgeClaims))
	for teamID, motif := range s.state.BadgeClaims {
		out[teamID] = motif
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
	if len(s.state.Picks) > 0 {
		return fmt.Errorf("the roster shape locks once the draft starts")
	}
	s.state.RosterOverride = cloneRosterOverride(&o)
	return s.persistLocked()
}

// ClearRosterOverride drops any commissioner roster-shape override,
// restoring the config-resolved default. Same post-first-pick lock as
// SetRosterOverride.
func (s *Store) ClearRosterOverride() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.state.Picks) > 0 {
		return fmt.Errorf("the roster shape locks once the draft starts")
	}
	s.state.RosterOverride = nil
	return s.persistLocked()
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
		return s.persistLocked()
	}
	for otherSlot, occupant := range slots {
		if otherSlot != slot && occupant == playerID {
			delete(slots, otherSlot)
		}
	}
	slots[slot] = playerID
	return s.persistLocked()
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
	return s.persistLocked()
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
	current := len(currentRosters(s.state)[txn.TeamID])
	if next := current + len(txn.Adds) - len(txn.Drops); next > rosterCap {
		return fmt.Errorf("your roster is full; choose a player to drop")
	}
	txn.At = txn.At.UTC()
	s.state.Transactions = append(s.state.Transactions, txn)
	return s.persistLocked()
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
	if !s.state.WaiversProcessedThrough.IsZero() {
		return nil
	}
	s.state.WaiversProcessedThrough = now.UTC()
	return s.persistLocked()
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
	return s.persistLocked()
}

// CancelClaim removes teamID's open claim named by claimID. Removing a
// claim that does not exist, or belongs to another team, is a harmless
// no-op — ReleaseBadge's idempotent precedent (badge.go) — so a stale
// double-submit never surfaces a confusing error.
func (s *Store) CancelClaim(teamID, claimID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := make([]WaiverClaim, 0, len(s.state.WaiverClaims))
	for _, c := range s.state.WaiverClaims {
		if c.ID == claimID && c.TeamID == teamID {
			continue
		}
		kept = append(kept, c)
	}
	s.state.WaiverClaims = kept
	return s.persistLocked()
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

	if len(s.state.WaiverClaims) == 0 {
		s.state.WaiversProcessedThrough = now.UTC()
		if err := s.persistLocked(); err != nil {
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
		case next.DropID == "" && len(currentRosters(s.state)[next.TeamID])+1 > rosterCap:
			result.Outcome = "failed"
			result.Reason = "your roster is full; choose a player to drop"
		case cfg.Waivers.Mode == "faab" && next.Bid > budget[next.TeamID]:
			result.Outcome = "failed"
			result.Reason = fmt.Sprintf("your bid exceeds your remaining budget ($%d left)", budget[next.TeamID])
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
			result.Outcome = "won"
			result.Week = week
		}
		results = append(results, result)
	}

	s.state.WaiverClaims = notYetDue
	s.state.WaiversProcessedThrough = now.UTC()
	if err := s.persistLocked(); err != nil {
		return nil, err
	}
	return results, nil
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
	if err := s.persistLocked(); err != nil {
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
	kept := make([]Announcement, 0, len(s.state.Announcements))
	for _, a := range s.state.Announcements {
		if a.ID != id {
			kept = append(kept, a)
		}
	}
	s.state.Announcements = kept
	return s.persistLocked()
}

func (s *Store) load() error {
	if s.filePath == "" {
		return nil
	}
	raw, err := os.ReadFile(s.filePath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var state PersistedState
	if err := json.Unmarshal(raw, &state); err != nil {
		return err
	}
	if state.SchemaVersion > currentSchemaVersion {
		return fmt.Errorf("%w: file is version %d, this binary supports up to %d",
			errSchemaTooNew, state.SchemaVersion, currentSchemaVersion)
	}
	// Migrate forward: a missing SchemaVersion decodes as 0 ("version 1"),
	// and every field this spec adds is additive with a nil-safe zero
	// value (Schedule and Playoffs are pointers; nil is already correct).
	// No per-field migration is needed beyond the nil-map guards below;
	// stamp the file current so the next persist records it.
	state.SchemaVersion = currentSchemaVersion
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
	if state.Pickems == nil {
		state.Pickems = map[string]map[string]string{}
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
	s.state = state
	return nil
}

// backupSnapshotLocked copies the current on-disk state file to filePath + ".bak"
// using the temp-file-plus-rename atomic pattern matching persistLocked. If no state
// file exists yet, it silently returns nil. Every other read failure is returned to
// the caller; every call site today treats this as a best-effort backup and ignores
// the error deliberately (see the "Best-effort backup" comments at each call site).
func (s *Store) backupSnapshotLocked() error {
	if s.filePath == "" {
		return nil
	}
	raw, err := os.ReadFile(s.filePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	bakPath := s.filePath + ".bak"
	if err := os.MkdirAll(filepath.Dir(bakPath), 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(bakPath), ".league-state-bak-*.json")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, bakPath)
}

func (s *Store) persistLocked() error {
	if s.loadErr != nil {
		return s.loadErr
	}
	if s.filePath == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.filePath), 0o750); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.filePath), ".league-state-*.json")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, s.filePath)
}

func cloneState(in PersistedState) PersistedState {
	out := PersistedState{
		SchemaVersion:           in.SchemaVersion,
		Ready:                   make(map[string]bool, len(in.Ready)),
		Picks:                   append([]DraftPick(nil), in.Picks...),
		Members:                 make(map[string]Member, len(in.Members)),
		Invites:                 append([]string(nil), in.Invites...),
		Boards:                  make(map[string][]string, len(in.Boards)),
		TeamNames:               make(map[string]string, len(in.TeamNames)),
		DraftOrder:              append([]string(nil), in.DraftOrder...),
		Scoring:                 make(map[string]float64, len(in.Scoring)),
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
		RosterOverride:          cloneRosterOverride(in.RosterOverride),
		Announcements:           append([]Announcement(nil), in.Announcements...),
		Lineups:                 make(map[string]map[int]map[string]string, len(in.Lineups)),
		Transactions:            make([]Transaction, len(in.Transactions)),
		WaiverClaims:            make([]WaiverClaim, len(in.WaiverClaims)),
		WaiversProcessedThrough: in.WaiversProcessedThrough,
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
