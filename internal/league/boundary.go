package league

import (
	"fmt"
	"strings"
	"time"
)

// boundaryDigest renders which side of every clock-driven boundary the UI
// renders `now` currently falls on. StateFingerprint appends it, so a page
// open across a boundary learns about the crossing on the same 4s
// /api/league/version poll everything else uses.
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
// That is the whole design constraint: nine pages poll this endpoint every
// four seconds, so a term that moved on its own would re-render all of
// them forever. A term must change exactly when a boundary is crossed and
// hold still otherwise — see TestBoundaryDigestStableAcrossQuietSeconds.
//
// Adding a deadline to this product means adding a term here. The list is
// currently: the shared NFL schedule (kickoff and final, which gate
// Pick'em, the lineup, and waiver availability), the draft start, the
// trade deadline, and the Preseason Blitz slate locks.
//
// Known gap: a waiver clear instant (clearsAt / deferredClearsAt,
// waivers.go) is not a term here. Its common case — a player locked by
// their own kickoff — already rides the schedule term; only the drop-clear
// half is still poll-blind.
func (s *Service) boundaryDigest(now time.Time, blitzGames []BlitzGame) string {
	parts := make([]string, 0, 4)

	// Kickoff gates Pick'em picks (pickem.go), lineup edits (lineup.go),
	// and waiver availability (waivers.go); Final gates Pick'em's
	// consensus reveal and a waiver's resolves-at. Counts, not IDs: the
	// count changes on exactly the crossings that matter and stays a few
	// bytes across a 272-game season.
	started, final := 0, 0
	for _, game := range s.schedule() {
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
	draftAt := s.EffectiveDraftAt(s.store.Snapshot())
	parts = append(parts, "draft:"+boundaryFlag(!now.Before(draftAt)))

	// The trade deadline (T8, trades.go) lives in configuration, so no
	// state write marks its passing.
	if deadline, ok := parseTradeDeadline(s.cfg); ok {
		parts = append(parts, "trade:"+boundaryFlag(!now.Before(deadline)))
	}

	// Preseason Blitz slate locks. The caller passes the games it already
	// pulled from the source, so one fingerprint never copies the Blitz
	// snapshot twice.
	if len(blitzGames) > 0 {
		parts = append(parts, "blitz:"+blitzLockDigest(blitzGames, now))
	}

	return strings.Join(parts, "|")
}

// boundaryFlag renders a crossed/not-crossed bit compactly.
func boundaryFlag(crossed bool) string {
	if crossed {
		return "1"
	}
	return "0"
}
