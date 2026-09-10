package league

import "time"

// LiveGameState is one NFL game's clock as the live poller last saw it.
type LiveGameState struct {
	GameID     string
	Away, Home string
	Period     string
	Clock      string
	// AwayPoints and HomePoints are the polled game's real score, carried
	// through from livescore.GameState so a final game can render its
	// result and not only the word FINAL (starterGameState,
	// matchup_ledger.go). They are the poller's live running score while
	// InProgress and the final score once Final; before kickoff both are
	// zero, which is why every reader gates on Final or InProgress rather
	// than on the numbers themselves.
	AwayPoints float64
	HomePoints float64
	Final      bool
	InProgress bool
	Kickoff    time.Time
	// Possession and PossessionKnown are GC-2b's possession display seam:
	// the nflverse abbreviation of the team the live poller last resolved
	// as currently on offense, and whether that resolution is actually
	// known (internal/livescore.ExtractPossession's own tolerant seam,
	// carried through livescore.GameState). Both are always their zero
	// value unless InProgress is true — possession has no honest meaning
	// otherwise.
	Possession      string
	PossessionKnown bool
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

// SetLiveVersionSource attaches the poller's cheap version accessor.
// StateFingerprint appends its value and the live feed cache
// (liveFeed.Snapshot) keys its cache by it, both by reading
// s.liveVersionFn fresh on every call through liveVersion() — this is the
// single field that stores it. liveFeed itself never copies fn: it holds
// a back-pointer to this Service instead (liveFeed.owner), so a feed
// swapped in after this setter runs (svc.feed = newLiveFeed(...)) is
// still wired without calling this setter again.
//
// fn must be non-blocking and must never call back into this Service or
// its live feed: liveVersion() calls fn outside poolMu, but Snapshot
// calls liveVersion() while holding liveFeed.mu, so fn blocking or
// re-entering either lock would stall every other reader.
//
// Lock order: this setter only ever takes poolMu, never liveFeed.mu — see
// liveFeed's doc comment for the reverse direction (f.mu, then poolMu).
// Do not add a call into s.feed here; that would risk exactly the
// ordering this comment rules out.
func (s *Service) SetLiveVersionSource(fn func() int64) {
	s.poolMu.Lock()
	s.liveVersionFn = fn
	s.poolMu.Unlock()
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

// livePollerEnabled reports whether the live-scoring poller is actually
// wired and turned on (LIVE_SCORING_ENABLED != "false" in live_scoring.go,
// carried here as LiveStatus.Enabled). matchupPresentation (J3 F1) reads
// this once, here, rather than at each of its two callers, so both the
// /matchups status line and the home page's live region agree: neither
// may promise "Live scores on" while no poller is running to keep that
// promise. No wired source (tests, or a build that never called
// SetLiveStatusSource) means "unknown, not "off"; this returns true so
// behavior stays exactly as it was before this poller-aware check
// existed.
func (s *Service) livePollerEnabled() bool {
	status, hasLive := s.liveStatus()
	if !hasLive {
		return true
	}
	return status.Enabled
}
