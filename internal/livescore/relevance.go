package livescore

import (
	"strings"
	"time"
)

// TeamRelevance is GC-2b's adaptive-cadence summary for one NFL team,
// derived from current effective starters league-wide: OffensiveStarter
// is true when some league team's starting lineup fields a non-DST
// player on this team; DSTStarter is true when some league team starts
// this team's own D/ST unit. Both false means no league team's lineup
// has any stake in this team's box score at all this week.
type TeamRelevance struct {
	OffensiveStarter bool
	DSTStarter       bool
}

// RelevanceSource is the poller's own callback seam for GC-2b's adaptive
// cadence — the same decoupled-callback pattern Fetcher and
// ScheduleSource already use to keep this package free of an
// internal/league import. live_scoring.go wires the real implementation
// from league.Service; a nil RelevanceSource (every existing test, a
// caller that builds Config directly, or replay mode before it is wired)
// is handled by gameRelevance below, never by a nil-check at every call
// site.
type RelevanceSource func(team string) TeamRelevance

// boxFetchTier is GC-2b's per-game box-fetch cadence classification.
type boxFetchTier int

const (
	// boxFetchIdle: neither team in the game fields a single league
	// starter (offensive or DST) this week. Its box score is fetched at
	// most once — on the game's first sighting, whenever boxFetchDue
	// finds no prior gameRecord at all, regardless of tier (owner
	// direction, GC-2b refinement: "fetch at most once ... if that helps
	// the snapshot's completeness — your call, document it"). That one
	// fetch matters for reasons beyond this one game's own fantasy
	// relevance: it is what first populates GameState (score, period,
	// clock) for every downstream reader of the snapshot, and it is what
	// lets a later relevance re-evaluation (a lineup change mid-week)
	// ever have a possession baseline to build on. After that first
	// fetch, an idle game is never fetched again: the scoreboard tick's
	// shared listing call keeps its score/period/final state current
	// enough for free, and repeated box polling would add nothing any
	// league team could ever see. On a bye-heavy week, every such game
	// still costs only that one bounded fetch plus its own share of the
	// shared scoreboard call — never the repeated baseline/fast cadence a
	// relevant game gets.
	boxFetchIdle boxFetchTier = iota
	// boxFetchBaseline: the flat LIVE_BOX_BASELINE cadence — GC-2's
	// original layer 2 fallback, now also GC-2b's own fallback whenever a
	// relevant game's possession is not itself known-relevant (including
	// simply unknown), or a break-state/unchanged-payload backoff has
	// temporarily dropped a fast-tier game back down.
	boxFetchBaseline
	// boxFetchFast: LIVE_BOX_FAST — a relevant game whose last-known
	// possession is itself relevant right now (the possessing team fields
	// a league offensive starter, or the defending team's DST is
	// started), the clock is actually running (not a break state), and
	// the last two fast-tier fetches have not returned identical content.
	boxFetchFast
)

// fastBackoffThreshold is GC-2b's unchanged-payload backoff: once this
// many consecutive fast-tier fetches for a game have returned
// content-identical stat payloads, the game drops to the baseline
// cadence until a fetch (at any tier, including the baseline fetches the
// backoff itself now causes) shows a real content change.
const fastBackoffThreshold = 2

// gameRelevance reports, for game, whether either side fields a single
// league starter at all (hasStarter) and, given the last-known
// possessing team, whether that specific possession is itself relevant
// (possessionRelevant): the possessing team fields a league offensive
// starter, or the defending team (the other side of game) starts its own
// DST. A nil Relevance callback (see RelevanceSource's own doc comment)
// answers hasStarter=true, possessionRelevant=false unconditionally —
// the flat, always-baseline, never-idle, never-fast behavior GC-2 always
// had, so a caller that never wires this seam (every pre-GC-2b test, a
// bare Config, replay mode) is unaffected.
func (p *Poller) gameRelevance(game Game, possessingTeam string, possessionKnown bool) (hasStarter, possessionRelevant bool) {
	if p.cfg.Relevance == nil {
		return true, false
	}
	awayRel := p.cfg.Relevance(NormalizeTeam(game.Away))
	homeRel := p.cfg.Relevance(NormalizeTeam(game.Home))
	hasStarter = awayRel.OffensiveStarter || awayRel.DSTStarter || homeRel.OffensiveStarter || homeRel.DSTStarter
	if !possessionKnown || possessingTeam == "" {
		return hasStarter, false
	}
	offenseRel, defenseRel := awayRel, homeRel
	if NormalizeTeam(possessingTeam) == NormalizeTeam(game.Home) {
		offenseRel, defenseRel = homeRel, awayRel
	}
	possessionRelevant = offenseRel.OffensiveStarter || defenseRel.DSTStarter
	return hasStarter, possessionRelevant
}

// isBreakState reports whether a box score's last-fetched clock/period
// shows a stopped-clock intermission — GC-2b's break-state backoff:
// there is nothing for the fast tier to catch while the clock is not
// running, and halftime alone runs about 13 minutes. period is checked
// for any value containing "half" (case-insensitive): no verified
// capture in this repo carries the exact string "Halftime" (the
// play-by-play fixture's own playPeriod values run only Q1..Q4 — see
// internal/sim/replay/testdata/box-20250907_BAL-BUF-pbp.json), so this
// branch is forward-compatible, not asserted from evidence. clock's own
// documented empty-string state (BoxScore.Clock's own doc comment)
// covers every other clock-stopped intermission the same way, including
// one Tank01 never names with a distinct period string at all —
// boxFetchTier only ever calls this for a game already known to be
// in-progress with a relevant possession, so an empty clock there is not
// pre-game or final, it is a stoppage.
func isBreakState(period, clock string) bool {
	if strings.Contains(strings.ToLower(period), "half") {
		return true
	}
	return strings.TrimSpace(clock) == ""
}

// boxFetchTier classifies game's box-fetch cadence for this tick, from
// the poller's own last-recorded state for it (an unseen game is never
// idle merely for lacking history — gameRelevance's hasStarter check
// depends only on the schedule's two team names, never on rec).
func (p *Poller) boxFetchTier(game Game) boxFetchTier {
	p.mu.Lock()
	rec, seen := p.games[game.ID]
	p.mu.Unlock()
	var possessingTeam, period, clock string
	var possessionKnown bool
	var unchangedFast int
	if seen {
		possessingTeam, possessionKnown = rec.possession, rec.possessionKnown
		period, clock = rec.box.Period, rec.box.Clock
		unchangedFast = rec.unchangedFastFetches
	}
	hasStarter, possessionRelevant := p.gameRelevance(game, possessingTeam, possessionKnown)
	switch {
	case !hasStarter:
		return boxFetchIdle
	case !possessionRelevant:
		return boxFetchBaseline
	case isBreakState(period, clock):
		return boxFetchBaseline
	case unchangedFast >= fastBackoffThreshold:
		return boxFetchBaseline
	default:
		return boxFetchFast
	}
}

// intervalFor returns tier's own fetch interval, for boxFetchDue.
// boxFetchIdle has no meaningful interval (callers must check for it
// separately; a game never due at all needs no interval at all).
func (p *Poller) intervalFor(tier boxFetchTier) time.Duration {
	if tier == boxFetchFast {
		return p.cfg.BoxFast
	}
	return p.cfg.BoxBaseline
}

// updateFastStreak applies GC-2b's unchanged-payload backoff bookkeeping
// for one just-recorded fetch: changed always resets the streak to 0
// (at any tier — a baseline-tier fetch made while backed off that shows
// real new content is exactly the "snap back to fast" signal), and an
// unchanged fast-tier fetch increments it. An unchanged baseline-tier
// fetch (the game is already backed off) leaves the streak untouched: it
// is already at or above fastBackoffThreshold, and only a real content
// change should move it.
func (p *Poller) updateFastStreak(id string, tier boxFetchTier, changed bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	rec, ok := p.games[id]
	if !ok {
		return
	}
	switch {
	case changed:
		rec.unchangedFastFetches = 0
	case tier == boxFetchFast:
		rec.unchangedFastFetches++
	default:
		return
	}
	p.games[id] = rec
}
