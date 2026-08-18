package matchup

import "time"

// Snapshot is the full computed matchup-rank table for one chosen
// season: every team's defense-allowed rank per offensive position
// (Defense[team][position]), and every team's own offense-output rank
// (Offense[team], for DST matchups), plus the honest season-source label
// every display of a rank must carry (design point 4). It is what
// main.go's matchup-rank cache computes once and persists to disk —
// never recomputed per row or per request.
type Snapshot struct {
	Season      int                            `json:"season"`
	SourceLabel string                         `json:"source_label"`
	ComputedAt  time.Time                      `json:"computed_at"`
	Defense     map[string]map[string]TeamRank `json:"defense"`
	Offense     map[string]TeamRank            `json:"offense"`
}

// Compute builds a Snapshot from rows (already limited to one season and
// already scored via the league's own scoring engine — see WeekRow's doc
// comment). rows should carry only offensive skill-position players
// (QB/RB/WR/TE, see OffensePositions); Compute ranks each of those
// positions' defense-allowed table plus one combined offense-output
// table from the same rows.
func Compute(rows []WeekRow, season int, sourceLabel string, now time.Time) Snapshot {
	defense := make(map[string]map[string]TeamRank, 32)
	for _, position := range OffensePositions() {
		for team, rank := range RankDefenseAllowed(rows, position) {
			if defense[team] == nil {
				defense[team] = make(map[string]TeamRank, len(offensePositions))
			}
			defense[team][position] = rank
		}
	}
	return Snapshot{
		Season:      season,
		SourceLabel: sourceLabel,
		ComputedAt:  now,
		Defense:     defense,
		Offense:     RankOffenseOutput(rows),
	}
}

// DefenseRank looks up team's defense-allowed rank at position. ok is
// false when the snapshot carries no ranked sample for that team/
// position — the honest "no rank" signal every caller must respect
// rather than fabricate a value.
func (snap Snapshot) DefenseRank(team, position string) (TeamRank, bool) {
	byPosition, ok := snap.Defense[team]
	if !ok {
		return TeamRank{}, false
	}
	rank, ok := byPosition[position]
	return rank, ok
}

// OffenseRank looks up team's own offense-output rank (for a DST
// matchup). ok is false when the snapshot carries no ranked sample for
// that team.
func (snap Snapshot) OffenseRank(team string) (TeamRank, bool) {
	rank, ok := snap.Offense[team]
	return rank, ok
}
