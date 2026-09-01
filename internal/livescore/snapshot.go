package livescore

import (
	"sort"
	"time"

	"gridiron-2000/internal/fantasy"
)

// Line is one live player row in Tank01 stat keys. GameID is the schedule
// game ID (Game.ID, the same key snapshot.Games uses): Tank01 sometimes
// omits teamAbv, leaving Team empty, so the overlay's in-progress decision
// needs a fallback that does not depend on Team being set (round-2 note
// 3).
type Line struct {
	PlayerID string
	Name     string
	Team     string // nflverse abbreviation; may be empty (Tank01 sometimes omits teamAbv)
	GameID   string // Game.ID / GameState key, for the empty-Team fallback
	Stats    map[string]float64
	Final    bool
}

// DSTLine is one live D/ST unit.
type DSTLine struct {
	Stats map[string]float64 // dst keys plus ptsAllowed
	Final bool
}

// GameState is one polled game's clock, keyed by the schedule game ID.
type GameState struct {
	ID         string
	Tank01ID   string
	Week       int
	Away, Home string // nflverse abbreviations
	Period     string
	Clock      string
	AwayPoints float64
	HomePoints float64
	Final      bool
	InProgress bool
	Kickoff    time.Time
	FetchedAt  time.Time
	// Possession and PossessionKnown are GC-2b's display seam: the
	// nflverse abbreviation of the team ExtractPossession last resolved as
	// currently on offense, and whether that resolution is actually known.
	// Both are always their zero value unless InProgress was true at fetch
	// time — possession has no honest meaning otherwise (addBoxToSnapshot
	// gates the extraction call on box.InProgress).
	Possession      string
	PossessionKnown bool
}

// WeekLines groups one week's live rows.
type WeekLines struct {
	Lines []Line
	DST   map[string]DSTLine // nflverse abbreviation -> unit
}

// Snapshot is an immutable copy of the poller's state.
type Snapshot struct {
	Version   int64
	CheckedAt time.Time
	Weeks     map[int]WeekLines
	Games     map[string]GameState
}

// Health is the poller's provenance for the PAUSED decision.
type Health struct {
	Enabled         bool
	Degraded        bool
	Reason          string
	Failures        int
	ListingFailures int
	// ScoreboardFailures and LastScoreboardError track the layer-1
	// getNFLScoresOnly endpoint apart from box and listing failures: a
	// relay that serves boxes but fails every scoreboard call silently
	// loses the change gate and the possession freshness it feeds, so it
	// must degrade visibly.
	ScoreboardFailures  int
	LastScoreboardError string
	BudgetUsed          int
	BudgetLimit         int
	CircuitOpenUntil    time.Time
	LastSuccess         time.Time
	LastError           string
	InWindow            int
	// Unmatched and UnmatchedGames are the in-window games the last tick
	// could not map to a Tank01 listing (round-2 note 1) — a schedule row
	// with no counterpart in the fetched listing, so it was never
	// fetched at all.
	Unmatched      int
	UnmatchedGames []string
}

// SnapshotFromBoxScores builds the shape Poller.Snapshot returns from parsed
// box scores; the Matchups render fixture (Task 11a) uses it. Games are
// keyed by the Tank01 game ID.
func SnapshotFromBoxScores(week int, kickoff time.Time, boxes ...fantasy.BoxScore) Snapshot {
	out := Snapshot{Version: 1, CheckedAt: kickoff, Weeks: map[int]WeekLines{}, Games: map[string]GameState{}}
	for _, box := range boxes {
		addBoxToSnapshot(&out, Game{ID: box.GameID, Week: week, Kickoff: kickoff, Away: NormalizeTeam(box.Away), Home: NormalizeTeam(box.Home)}, box, kickoff)
	}
	sortSnapshotLines(&out)
	return out
}

func addBoxToSnapshot(out *Snapshot, game Game, box fantasy.BoxScore, at time.Time) {
	week := out.Weeks[game.Week]
	if week.DST == nil {
		week.DST = map[string]DSTLine{}
	}
	for playerID, line := range box.Players {
		stats := make(map[string]float64, len(line.Stats))
		for k, v := range line.Stats {
			stats[k] = v
		}
		week.Lines = append(week.Lines, Line{PlayerID: playerID, Name: line.Name, Team: NormalizeTeam(line.Team), GameID: game.ID, Stats: stats, Final: box.Final})
	}
	for team, unit := range box.DST {
		stats := make(map[string]float64, len(unit))
		for k, v := range unit {
			stats[k] = v
		}
		week.DST[NormalizeTeam(team)] = DSTLine{Stats: stats, Final: box.Final}
	}
	out.Weeks[game.Week] = week
	// Possession only appears while the game is live (GC-2b): a pre-game
	// or final box's raw payload is never even consulted, so a stale or
	// coincidentally-shaped field there can never leak a fabricated
	// possession onto a game that has not started or has already ended.
	possession, possessionKnown := "", false
	if box.InProgress {
		possession, possessionKnown = ExtractPossession(box.Raw)
	}
	out.Games[game.ID] = GameState{ID: game.ID, Tank01ID: box.GameID, Week: game.Week, Away: NormalizeTeam(box.Away), Home: NormalizeTeam(box.Home),
		Period: box.Period, Clock: box.Clock, AwayPoints: box.AwayPoints, HomePoints: box.HomePoints, Final: box.Final, InProgress: box.InProgress, Kickoff: game.Kickoff, FetchedAt: at,
		Possession: possession, PossessionKnown: possessionKnown}
}

// sortSnapshotLines orders every week's Lines by PlayerID (round-2 note
// 36). box.Players is a map, so addBoxToSnapshot's append order is
// nondeterministic; a caller comparing two snapshots, or rendering one,
// needs a stable order instead of map iteration order.
func sortSnapshotLines(out *Snapshot) {
	for _, week := range out.Weeks {
		sort.Slice(week.Lines, func(i, j int) bool { return week.Lines[i].PlayerID < week.Lines[j].PlayerID })
	}
}
