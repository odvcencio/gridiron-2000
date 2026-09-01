package livescore

import (
	"context"
	"fmt"
	"sort"
	"time"

	"gridiron-2000/internal/fantasy"
)

// scoreboardRecord is the poller's last-seen getNFLScoresOnly row for one
// schedule game — GC-2's layer 1 made real: score, period, status, and
// possession for every in-window game, refreshed every ScoreboardInterval
// through one shared call per game date. It exists for two readers: the
// change gate (deltaKey/changedAt, boxFetchDue) and the fresher-than-box
// display overlay (Snapshot).
type scoreboardRecord struct {
	row fantasy.ScoreboardGame
	at  time.Time
	// possession and possessionKnown are ExtractPossession's result
	// against row.Raw, gated on row.InProgress exactly as the box path
	// gates it (record's own doc comment) — the scoreboard carries the
	// identical lineScore.currentlyInPossession shape, verified against
	// the real capture in internal/fantasy/testdata/scoresonly-20250907.json.
	possession      string
	possessionKnown bool
	// deltaKey is the change-gate signature: score, possession, period,
	// status. The running game clock is deliberately NOT part of it — the
	// clock moves on essentially every read while play runs, so hashing it
	// would collapse the gate into "always changed" and fetch every box on
	// every tick, the exact blanket-polling cost the three-layer design
	// exists to remove.
	deltaKey string
	// changedAt is when deltaKey last changed. boxFetchDue reads it: a
	// delta newer than the game's last recorded box fetch marks the box
	// due immediately, inside its tier interval.
	changedAt time.Time
}

// scoreboardDeltaKey builds deltaKey for one row. possession enters as the
// extracted (team, known) pair, not the raw lineScore, so an unparseable
// possession shape can never thrash the gate.
func scoreboardDeltaKey(row fantasy.ScoreboardGame, possession string, possessionKnown bool) string {
	return fmt.Sprintf("%v|%v|%v|%s|%s|%s|%v|%v",
		row.AwayPoints, row.HomePoints, possessionKnown, possession,
		row.Period, row.StatusCode, row.Final, row.InProgress)
}

// scoreboardDates collects the distinct game dates (YYYYMMDD, the leading
// segment of a Tank01 game ID) across the matched games, sorted so the
// fetch order is deterministic. An ID too short to carry a date is
// skipped — matchGames only ever emits real Tank01 IDs, so this is a
// guard, not a code path.
func scoreboardDates(matched map[string]string) []string {
	set := map[string]bool{}
	for _, tank01ID := range matched {
		if len(tank01ID) < 9 || tank01ID[8] != '_' {
			continue
		}
		set[tank01ID[:8]] = true
	}
	dates := make([]string, 0, len(set))
	for date := range set {
		dates = append(dates, date)
	}
	sort.Strings(dates)
	return dates
}

// refreshScoreboard runs layer 1's scoreboard pass for one tick: one
// FetchScoresOnly call per distinct matched game date, then one
// scoreboardRecord update per matched game a returned row covers. A
// failed date is recorded (recordScoreboardFailure) and skipped — the box
// tiers and the wire trigger still govern on their own, exactly the
// pre-scoreboard behavior. An empty reply is a real answer (an off date),
// never a failure. A game the reply does not cover keeps its previous
// record untouched.
func (p *Poller) refreshScoreboard(ctx context.Context, matched map[string]string, now time.Time) {
	rowsByTank01 := map[string]fantasy.ScoreboardGame{}
	for _, date := range scoreboardDates(matched) {
		rows, err := p.fetcher.FetchScoresOnly(ctx, date)
		if err != nil {
			p.recordScoreboardFailure(err, now)
			continue
		}
		p.mu.Lock()
		p.scoreboardFailures, p.lastScoreboardError = 0, ""
		p.mu.Unlock()
		for _, row := range rows {
			rowsByTank01[row.GameID] = row
		}
	}
	if len(rowsByTank01) == 0 {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.scoreboard == nil {
		p.scoreboard = map[string]scoreboardRecord{}
	}
	for gameID, tank01ID := range matched {
		row, ok := rowsByTank01[tank01ID]
		if !ok {
			continue
		}
		possession, possessionKnown := "", false
		if row.InProgress {
			possession, possessionKnown = ExtractPossession(row.Raw)
		}
		deltaKey := scoreboardDeltaKey(row, possession, possessionKnown)
		record := scoreboardRecord{row: row, at: now,
			possession: possession, possessionKnown: possessionKnown, deltaKey: deltaKey}
		if previous, seen := p.scoreboard[gameID]; seen && previous.deltaKey == deltaKey {
			record.changedAt = previous.changedAt
		} else {
			record.changedAt = now
		}
		p.scoreboard[gameID] = record
	}
}

// recordScoreboardFailure tracks one failed FetchScoresOnly call, apart
// from box and listing failures for the same reason those two are kept
// apart (the Poller field comment): a relay that serves boxes but fails
// every scoreboard call must stay visible in Health.
func (p *Poller) recordScoreboardFailure(err error, now time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.scoreboardFailures++
	p.lastScoreboardError = err.Error()
	p.openCircuitOnRateLimit(err, now)
	p.cfg.Logf("livescore: scoreboard fetch failed: %v", err)
}
