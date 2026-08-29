package league

import (
	"fmt"
	"strings"
	"time"
)

// boundaryDigest renders which side of every clock-driven boundary the UI
// renders `now` currently falls on. StateFingerprint appends it, so general
// pages learn about a crossing through /api/league/version and the Draft Room
// learns through its live hub.
//
// It exists because a fingerprint built from persisted state alone cannot
// see these transitions: a kickoff, a draft start, and a trade deadline
// are crossed by the clock, not by a state write, so nothing rewrites the
// state file at the instant the meaning changes. Before this digest, a
// member holding a page open watched a control stay live past its
// deadline; the server refused the mutation correctly, but only after the
// click.
//
// Every term is a count or a flag, never a timestamp or a clock bucket.
// That is the whole design constraint: fingerprint consumers run continuously,
// so a term that moved on its own would re-render pages forever. A term must
// change exactly when a boundary is crossed and hold still otherwise — see
// TestBoundaryDigestStableAcrossQuietSeconds.
//
// Adding a deadline to this product means adding a term here. The list is
// currently: the shared NFL schedule (kickoff and final, which gate
// Pick'em, the lineup, and waiver availability), the draft start, the
// trade deadline, the waiver clear window, and the Preseason Blitz slate
// locks.
func (s *Service) boundaryDigest(now time.Time, blitzGames []BlitzGame) string {
	parts := make([]string, 0, 5)
	state := s.store.Snapshot()
	games := s.schedule()

	// Kickoff gates Pick'em picks (pickem.go), lineup edits (lineup.go),
	// and waiver availability (waivers.go); Final gates Pick'em's
	// consensus reveal and a waiver's resolves-at. Counts, not IDs: the
	// count changes on exactly the crossings that matter and stays a few
	// bytes across a 272-game season.
	started, final := 0, 0
	for _, game := range games {
		if !now.Before(game.Kickoff) {
			started++
		}
		if game.Final {
			final++
		}
	}
	parts = append(parts, fmt.Sprintf("sched:%d/%d", started, final))

	// The draft-start crossing flips canPick and the draft page's
	// "started" flag (service.go). The announced meeting can be moved before
	// the lifecycle starts, so resolve it from the same persisted snapshot.
	draftAt := s.EffectiveDraftAt(state)
	parts = append(parts, "draft:"+boundaryFlag(!now.Before(draftAt)))

	// The trade deadline (T8, trades.go) lives in configuration, so no
	// state write marks its passing.
	if deadline, ok := parseTradeDeadline(s.cfg); ok {
		parts = append(parts, "trade:"+boundaryFlag(!now.Before(deadline)))
	}

	// A manager drop and an IR auto-drop become free agents at a daily
	// processor boundary, even though no state write occurs at that instant.
	// The schedule term above already covers a player's own kickoff lock; this
	// term covers only the drop-clear half so an open Players/Team page learns
	// that an add is available without waiting for another mutation.
	parts = append(parts, waiverClearBoundaryDigest(state, s.cfg, games, now))

	// Preseason Blitz slate locks. The caller passes the games it already
	// pulled from the source, so one fingerprint never copies the Blitz
	// snapshot twice.
	if len(blitzGames) > 0 {
		parts = append(parts, "blitz:"+blitzLockDigest(blitzGames, now))
	}

	return strings.Join(parts, "|")
}

// waiverClearBoundaryDigest counts the latest clear boundary crossed by each
// currently unrostered dropped player. Counting crossings (rather than
// embedding IDs or timestamps) keeps the shared fingerprint quiet between
// real availability changes. A player re-rostered after a drop is excluded;
// the roster mutation itself already changes the persisted-state digest.
func waiverClearBoundaryDigest(state PersistedState, cfg Config, games []GameInfo, now time.Time) string {
	owner := rosterOwner(currentRosters(state))
	droppedPlayers := make(map[string]struct{})
	for _, txn := range state.Transactions {
		if txn.Type != "drop" && txn.Type != "auto-drop" {
			continue
		}
		for _, drop := range txn.Drops {
			if drop.PlayerID == "" || owner[drop.PlayerID] != "" {
				continue
			}
			droppedPlayers[drop.PlayerID] = struct{}{}
		}
	}

	crossed := 0
	for playerID := range droppedPlayers {
		droppedAt, origin, ok := lastDropInstant(state, playerID)
		if !ok {
			continue
		}
		clears := clearsAt(cfg, droppedAt)
		if origin == "auto-drop" {
			clears = deferredClearsAt(cfg, games, droppedAt)
		}
		if !now.Before(clears) {
			crossed++
		}
	}
	return fmt.Sprintf("waiver:%d", crossed)
}

// boundaryFlag renders a crossed/not-crossed bit compactly.
func boundaryFlag(crossed bool) string {
	if crossed {
		return "1"
	}
	return "0"
}
