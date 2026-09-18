package league

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// SetWatch stars (watched) or unstars one pool player on a member's
// private watchlist. Unstarring a player who is not on the list is a
// no-op, so a stale button never fails a manager. The pool decides what
// a valid player is (ToggleWatch); the store only keeps the set.
func (s *Store) SetWatch(email, playerID string, watched bool, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeErrorLocked(); err != nil {
		return err
	}
	email = strings.ToLower(strings.TrimSpace(email))
	playerID = strings.TrimSpace(playerID)
	if email == "" || playerID == "" {
		return errors.New("sign in and choose a player to watch")
	}
	if s.state.Watchlists == nil {
		s.state.Watchlists = map[string]map[string]time.Time{}
	}
	list := s.state.Watchlists[email]
	if !watched {
		if _, ok := list[playerID]; !ok {
			return nil
		}
		delete(list, playerID)
		if len(list) == 0 {
			delete(s.state.Watchlists, email)
		}
		return s.persistLocked(colScalars)
	}
	if list == nil {
		list = map[string]time.Time{}
		s.state.Watchlists[email] = list
	}
	if _, ok := list[playerID]; ok {
		return nil
	}
	list[playerID] = now.UTC()
	return s.persistLocked(colScalars)
}

// watchlistFor is the signed-in member's own set, keyed by player ID, or
// nil for a visitor the league does not know.
func watchlistFor(state PersistedState, email string) map[string]time.Time {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return nil
	}
	return state.Watchlists[email]
}

// watchViewerEmail is the canonical email of a signed-in, admitted member,
// or "" for anyone else: the same admission every other member-scoped
// bookmark uses, no seat required.
func (s *Service) watchViewerEmail(r *http.Request, state PersistedState) string {
	user, ok := s.CurrentUser(r)
	if !ok {
		return ""
	}
	email := s.identityResolver.Resolve(user.Email)
	if email == "" {
		return ""
	}
	if _, exists := memberByEmail(state.Members, email); !exists {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(email))
}

// ToggleWatch stars or unstars one pool player for the signed-in member.
func (s *Service) ToggleWatch(r *http.Request, playerID string, watched bool) (string, error) {
	state := s.store.Snapshot()
	email := s.watchViewerEmail(r, state)
	if email == "" {
		if s.demoMode {
			email = "demo-guest"
		} else {
			return "", errors.New("sign in as a league member to keep a watchlist")
		}
	}
	playerID = strings.TrimSpace(playerID)
	pool := s.pool()
	if err := requirePlayerData(pool); err != nil {
		return "", err
	}
	player, ok := pool.byID[playerID]
	if !ok {
		return "", fmt.Errorf("that player is not in the pool")
	}
	if err := s.store.SetWatch(email, playerID, watched, s.clock()); err != nil {
		return "", err
	}
	if watched {
		return player.Name + " added to your watchlist.", nil
	}
	return player.Name + " removed from your watchlist.", nil
}
