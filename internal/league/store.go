package league

import (
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

// Store keeps the starter deployable without a database while preserving
// state across restarts. Its methods are safe for concurrent requests.
type Store struct {
	mu       sync.RWMutex
	filePath string
	state    PersistedState
}

func NewStore(filePath string) *Store {
	s := &Store{
		filePath: strings.TrimSpace(filePath),
		state: PersistedState{
			Ready:      map[string]bool{},
			Picks:      []DraftPick{},
			Members:    map[string]Member{},
			Invites:    []string{},
			Boards:     map[string][]string{},
			TeamNames:  map[string]string{},
			DraftOrder: []string{},
			Scoring:    map[string]float64{},
			Pickems:    map[string]map[string]string{},
			Autopick:   map[string]bool{},
		},
	}
	_ = s.load()
	return s
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
	// The draft ends after every team fills its roster; "15 rounds" was
	// display copy only until this cap.
	if len(s.state.Picks) >= len(defaultTeams())*DraftRounds {
		return DraftPick{}, fmt.Errorf("the draft is complete")
	}
	number := len(s.state.Picks) + 1
	expected := teamOnClock(s.state.DraftOrder, number)
	if expected != teamID {
		return DraftPick{}, fmt.Errorf("%s is on the clock", expected)
	}
	pick := DraftPick{
		Number:   number,
		Round:    ((number - 1) / len(defaultTeams())) + 1,
		TeamID:   teamID,
		PlayerID: playerID,
		MadeAt:   now.UTC(),
		MadeBy:   madeBy,
	}
	s.state.Picks = append(s.state.Picks, pick)
	s.state.ClockDeadline = nextDeadline
	return pick, s.persistLocked()
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
	if len(s.state.Picks) >= len(defaultTeams())*DraftRounds {
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
	pick := DraftPick{
		Number:   number,
		Round:    ((number - 1) / len(defaultTeams())) + 1,
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

func (s *Store) AssignMember(email, name string) (Member, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	name = strings.TrimSpace(name)
	if email == "" {
		return Member{}, fmt.Errorf("email is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.state.Members[email]; ok {
		if name != "" && existing.Name != name {
			existing.Name = name
			s.state.Members[email] = existing
			_ = s.persistLocked()
		}
		return existing, nil
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
		member := Member{TeamID: team.ID, Name: name, Email: email}
		s.state.Members[email] = member
		return member, s.persistLocked()
	}
	return Member{}, ErrLeagueFull
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

// ResetDraft clears every pick and ready flag. Seats and boards survive. The
// clock fields and the Autopick map are also cleared: a redrawn draft
// starts with a clean, unarmed clock and no stale away-mode toggles.
func (s *Store) ResetDraft() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.Picks = []DraftPick{}
	s.state.Ready = map[string]bool{}
	s.clearClockFieldsLocked()
	return s.persistLocked()
}

// ResetLeague clears picks, seats, ready flags, boards, and pick'em picks.
// Invites and team name overrides survive; both are commissioner
// configuration, not game state. Clock fields and Autopick are cleared,
// same as ResetDraft.
func (s *Store) ResetLeague() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.Picks = []DraftPick{}
	s.state.Ready = map[string]bool{}
	s.state.Members = map[string]Member{}
	s.state.Boards = map[string][]string{}
	s.state.Pickems = map[string]map[string]string{}
	s.clearClockFieldsLocked()
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
	if state.Autopick == nil {
		state.Autopick = map[string]bool{}
	}
	s.state = state
	return nil
}

func (s *Store) persistLocked() error {
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
		Ready:             make(map[string]bool, len(in.Ready)),
		Picks:             append([]DraftPick(nil), in.Picks...),
		Members:           make(map[string]Member, len(in.Members)),
		Invites:           append([]string(nil), in.Invites...),
		Boards:            make(map[string][]string, len(in.Boards)),
		TeamNames:         make(map[string]string, len(in.TeamNames)),
		DraftOrder:        append([]string(nil), in.DraftOrder...),
		Scoring:           make(map[string]float64, len(in.Scoring)),
		Pickems:           make(map[string]map[string]string, len(in.Pickems)),
		ClockDeadline:     in.ClockDeadline,
		ClockPaused:       in.ClockPaused,
		ClockRemainingSec: in.ClockRemainingSec,
		ClockDurationSec:  in.ClockDurationSec,
		Autopick:          make(map[string]bool, len(in.Autopick)),
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
	for key, value := range in.Autopick {
		out.Autopick[key] = value
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
