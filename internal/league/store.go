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
			Ready:   map[string]bool{},
			Picks:   []DraftPick{},
			Members: map[string]Member{},
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

func (s *Store) MakePick(teamID, playerID string, now time.Time) (DraftPick, error) {
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
	number := len(s.state.Picks) + 1
	expected := teamOnClock(number)
	if expected != teamID {
		return DraftPick{}, fmt.Errorf("%s is on the clock", expected)
	}
	pick := DraftPick{
		Number:   number,
		Round:    ((number - 1) / len(defaultTeams())) + 1,
		TeamID:   teamID,
		PlayerID: playerID,
		MadeAt:   now.UTC(),
	}
	s.state.Picks = append(s.state.Picks, pick)
	return pick, s.persistLocked()
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
		Ready:   make(map[string]bool, len(in.Ready)),
		Picks:   append([]DraftPick(nil), in.Picks...),
		Members: make(map[string]Member, len(in.Members)),
	}
	for key, value := range in.Ready {
		out.Ready[key] = value
	}
	for key, value := range in.Members {
		out.Members[key] = value
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

func teamOnClock(pickNumber int) string {
	teams := defaultTeams()
	if pickNumber < 1 {
		pickNumber = 1
	}
	index := (pickNumber - 1) % len(teams)
	round := (pickNumber-1)/len(teams) + 1
	if round%2 == 0 {
		index = len(teams) - 1 - index
	}
	return teams[index].ID
}
