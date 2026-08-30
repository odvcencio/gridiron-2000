package league

import "time"

// LiveGameState is one NFL game's clock as the live poller last saw it.
type LiveGameState struct {
	GameID     string
	Away, Home string
	Period     string
	Clock      string
	Final      bool
	InProgress bool
	Kickoff    time.Time
}

// LiveStatus is the live poller's provenance for the render path. Degraded
// and Reason feed the PAUSED matchup state; Games feed the per-starter
// game-state labels. The version lives behind SetLiveVersionSource.
type LiveStatus struct {
	Enabled   bool
	Degraded  bool
	Reason    string
	CheckedAt time.Time
	Games     map[string]LiveGameState // keyed by nflverse team abbreviation
}

// LiveStatusSource returns the current LiveStatus from memory; it must not
// perform network work (the poller owns that).
type LiveStatusSource func() LiveStatus

// SetLiveVersionSource attaches the poller's cheap version accessor. The
// fingerprint appends it and the live feed cache is keyed by it.
func (s *Service) SetLiveVersionSource(fn func() int64) {
	s.poolMu.Lock()
	s.liveVersionFn = fn
	s.poolMu.Unlock()
	if s.feed != nil {
		s.feed.setVersionSource(fn)
	}
}

// SetLiveStatusSource attaches the poller's state copy beside SetBlitzSource.
func (s *Service) SetLiveStatusSource(fn LiveStatusSource) {
	s.poolMu.Lock()
	s.liveStatusFn = fn
	s.poolMu.Unlock()
}

func (s *Service) liveVersion() (int64, bool) {
	s.poolMu.Lock()
	fn := s.liveVersionFn
	s.poolMu.Unlock()
	if fn == nil {
		return 0, false
	}
	return fn(), true
}

// LiveVersionForTest exposes liveVersion to the root package's tests.
func (s *Service) LiveVersionForTest() (int64, bool) { return s.liveVersion() }

func (s *Service) liveStatus() (LiveStatus, bool) {
	s.poolMu.Lock()
	fn := s.liveStatusFn
	s.poolMu.Unlock()
	if fn == nil {
		return LiveStatus{}, false
	}
	return fn(), true
}
