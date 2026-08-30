package replay

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
)

// Frame is one served box-score snapshot: Body is a complete, already
// marshalled Tank01 envelope ({"statusCode":200,"body":...}), ready to
// write straight onto an HTTP response (server.go).
type Frame struct {
	Index  int
	Period string
	Clock  string
	Body   []byte
}

// dstDeltaKeys are the D/ST fields Frames accumulates from each play's
// Defense group, mirroring fantasy's own dstStatKeys (that list is
// unexported, so this package keeps its own copy).
var dstDeltaKeys = []string{"sacks", "defensiveInterceptions", "fumblesRecovered", "defTD", "safeties"}

// Frames builds every frame of this game, in order: frame 0 is a
// scoreless pre-game snapshot, frames 1..PlayCount() are cumulative
// in-progress box scores (one per play, deltas summed play by play), and
// the final frame is the fixture's own untouched final body.
func (g *Game) Frames() []Frame {
	frames := make([]Frame, 0, len(g.plays)+2)

	working := deepCopyMap(g.final)
	resetToPregame(working)
	frames = append(frames, Frame{Index: 0, Body: marshalEnvelope(working)})

	finalPlayerStats, _ := g.final["playerStats"].(map[string]any)

	cumulative := map[string]map[string]map[string]float64{} // playerID -> group -> field -> value
	teamDefense := map[string]map[string]float64{}           // teamAbv -> field -> value
	var cursor int
	var awayScore, homeScore float64

	for i, p := range g.plays {
		for playerID, groups := range p.Players {
			for group, fields := range groups {
				for field, raw := range fields {
					value, ok := parseFloat(raw)
					if !ok {
						continue
					}
					addCumulative(cumulative, playerID, group, field, value)
					if group == "Defense" {
						if team := g.teamOf[playerID]; team != "" {
							addTeamStat(teamDefense, team, field, value)
						}
					}
				}
			}
		}
		for cursor < len(g.scoring) && notAfter(g.scoring[cursor], p) {
			awayScore = parseScore(g.scoring[cursor].Away)
			homeScore = parseScore(g.scoring[cursor].Home)
			cursor++
		}

		working["playerStats"] = buildPlayerStats(cumulative, finalPlayerStats)
		working["DST"] = buildDST(g.final["DST"], teamDefense, g.Away, g.Home, awayScore, homeScore)
		working["currentPeriod"] = p.Period
		working["gameClock"] = p.Clock
		working["gameStatus"] = "Live - In Progress"
		working["gameStatusCode"] = "1"
		working["awayPts"] = formatStat(awayScore)
		working["homePts"] = formatStat(homeScore)
		setLineScoreTotals(working, awayScore, homeScore)
		working["scoringPlays"] = g.scoring[:cursor]

		frames = append(frames, Frame{Index: i + 1, Period: p.Period, Clock: p.Clock, Body: marshalEnvelope(working)})
	}

	finalPeriod, _ := g.final["currentPeriod"].(string)
	finalClock, _ := g.final["gameClock"].(string)
	frames = append(frames, Frame{Index: len(g.plays) + 1, Period: finalPeriod, Clock: finalClock, Body: marshalEnvelope(g.final)})
	return frames
}

// deepCopyMap clones a map[string]any through a JSON round trip so
// Frames can mutate the working copy on every play without disturbing
// Game.final, which the last frame serves verbatim.
func deepCopyMap(in map[string]any) map[string]any {
	raw, err := json.Marshal(in)
	if err != nil {
		return map[string]any{}
	}
	out := map[string]any{}
	_ = json.Unmarshal(raw, &out)
	return out
}

// marshalEnvelope wraps body in the Tank01 {"statusCode":200,"body":...}
// shape every frame is served in.
func marshalEnvelope(body any) []byte {
	raw, err := json.Marshal(struct {
		StatusCode int `json:"statusCode"`
		Body       any `json:"body"`
	}{StatusCode: 200, Body: body})
	if err != nil {
		return []byte(`{"statusCode":200,"body":{}}`)
	}
	return raw
}

// resetToPregame mutates working (a clone of the final body) into frame
// 0's scoreless, unplayed shape: no player stats, zeroed D/ST units,
// zeroed score and line score, and the pre-game status fields.
func resetToPregame(working map[string]any) {
	working["playerStats"] = map[string]any{}
	if dst, ok := working["DST"].(map[string]any); ok {
		for _, side := range []string{"away", "home"} {
			unit, ok := dst[side].(map[string]any)
			if !ok {
				continue
			}
			for key := range unit {
				switch key {
				case "teamAbv", "teamID":
					// keep identity fields.
				default:
					unit[key] = "0"
				}
			}
		}
	}
	working["currentPeriod"] = ""
	working["gameClock"] = ""
	working["gameStatus"] = "Scheduled"
	working["gameStatusCode"] = "0"
	working["awayPts"] = "0"
	working["homePts"] = "0"
	if lineScore, ok := working["lineScore"].(map[string]any); ok {
		lineScore["period"] = ""
		lineScore["gameClock"] = ""
		for _, side := range []string{"away", "home"} {
			unit, ok := lineScore[side].(map[string]any)
			if !ok {
				continue
			}
			for key := range unit {
				switch key {
				case "teamID", "teamAbv":
					// keep identity fields.
				case "currentlyInPossession":
					unit[key] = "False"
				default:
					unit[key] = "0"
				}
			}
		}
	}
	working["scoringPlays"] = []scoringPlay{}
}

// addCumulative folds one play's delta into the running per-player total.
func addCumulative(cumulative map[string]map[string]map[string]float64, playerID, group, field string, value float64) {
	if cumulative[playerID] == nil {
		cumulative[playerID] = map[string]map[string]float64{}
	}
	if cumulative[playerID][group] == nil {
		cumulative[playerID][group] = map[string]float64{}
	}
	cumulative[playerID][group][field] += value
}

// addTeamStat folds one play's Defense delta into the running per-team
// total (attributed by the defender's own team, never the play's
// possessing team — see the game.go teamOf comment).
func addTeamStat(teamStats map[string]map[string]float64, team, field string, value float64) {
	if teamStats[team] == nil {
		teamStats[team] = map[string]float64{}
	}
	teamStats[team][field] += value
}

// buildPlayerStats renders the cumulative per-player totals into the
// playerStats shape parseBoxScore reads: each group's fields as strings,
// plus longName/teamAbv/team copied from the final row's own identity.
func buildPlayerStats(cumulative map[string]map[string]map[string]float64, finalPlayerStats map[string]any) map[string]any {
	out := make(map[string]any, len(cumulative))
	for playerID, groups := range cumulative {
		row := make(map[string]any, len(groups)+3)
		for group, fields := range groups {
			inner := make(map[string]any, len(fields))
			for field, value := range fields {
				inner[field] = formatStat(value)
			}
			row[group] = inner
		}
		if finalRow, ok := finalPlayerStats[playerID].(map[string]any); ok {
			for _, key := range []string{"longName", "teamAbv", "team"} {
				if value, present := finalRow[key]; present {
					row[key] = value
				}
			}
		}
		out[playerID] = row
	}
	return out
}

// buildDST renders both D/ST units for one frame: sacks/int/fumbleRec/
// defTD/safeties from the per-team cumulative Defense totals, ptsAllowed
// from the opponent's running score (away allows home's points and vice
// versa), and teamAbv/teamID copied from the final DST block.
func buildDST(rawFinal any, teamDefense map[string]map[string]float64, away, home string, awayScore, homeScore float64) map[string]any {
	finalDST, _ := rawFinal.(map[string]any)
	return map[string]any{
		"away": buildDSTUnit(finalDST["away"], teamDefense[away], homeScore),
		"home": buildDSTUnit(finalDST["home"], teamDefense[home], awayScore),
	}
}

func buildDSTUnit(rawFinalUnit any, sums map[string]float64, ptsAllowed float64) map[string]any {
	finalUnit, _ := rawFinalUnit.(map[string]any)
	unit := make(map[string]any, len(dstDeltaKeys)+3)
	if finalUnit != nil {
		for _, key := range []string{"teamAbv", "teamID"} {
			if value, ok := finalUnit[key]; ok {
				unit[key] = value
			}
		}
	}
	for _, key := range dstDeltaKeys {
		unit[key] = formatStat(sums[key])
	}
	unit["ptsAllowed"] = formatStat(ptsAllowed)
	return unit
}

// setLineScoreTotals updates only the running totalPts on each side of
// working's lineScore; the per-quarter breakdown is never read by
// parseBoxScore, so it is left at the final body's own values.
func setLineScoreTotals(working map[string]any, awayScore, homeScore float64) {
	lineScore, ok := working["lineScore"].(map[string]any)
	if !ok {
		return
	}
	if unit, ok := lineScore["away"].(map[string]any); ok {
		unit["totalPts"] = formatStat(awayScore)
	}
	if unit, ok := lineScore["home"].(map[string]any); ok {
		unit["totalPts"] = formatStat(homeScore)
	}
}

// parseFloat reads one Tank01 stat delta value, which arrives as a JSON
// string in every fixture seen so far but is read leniently as a bare
// number too. ok is false for an empty or non-numeric value, so callers
// skip it rather than fold in a bogus zero.
func parseFloat(raw any) (float64, bool) {
	switch v := raw.(type) {
	case string:
		v = strings.TrimSpace(v)
		if v == "" {
			return 0, false
		}
		parsed, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return 0, false
		}
		return parsed, true
	case float64:
		return v, true
	default:
		return 0, false
	}
}

// parseScore reads one scoringPlay's homeScore/awayScore field, which
// Tank01 always sends as a numeric string. A malformed value reads as 0
// rather than aborting the whole replay build.
func parseScore(raw string) float64 {
	value, _ := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	return value
}

// formatStat renders a cumulative stat as the string Tank01 itself would
// send: an integer without a decimal point when the value is whole (every
// stat this package accumulates always is), a plain decimal otherwise.
func formatStat(value float64) string {
	if value == math.Trunc(value) {
		return strconv.FormatFloat(value, 'f', 0, 64)
	}
	return strconv.FormatFloat(value, 'f', -1, 64)
}

// notAfter reports whether scoring play s happened at or before play p,
// by Tank01's countdown clock: an earlier period is always "not after";
// within the same period, a scoring play's own clock reading must be
// greater than or equal to p's (the clock counts down, so a higher
// reading is earlier in the quarter).
func notAfter(s scoringPlay, p play) bool {
	scoreRank, playRank := periodRank(s.Period), periodRank(p.Period)
	if scoreRank != playRank {
		return scoreRank < playRank
	}
	return clockSeconds(s.Time) >= clockSeconds(p.Clock)
}

// periodRank orders Tank01's period labels for notAfter's comparison.
// Regulation quarters rank in order; any overtime label (OT, OT2, ...)
// ranks after them — this fixture never reaches overtime, so ordering
// two distinct overtime periods against each other never matters here.
func periodRank(period string) int {
	switch strings.ToUpper(strings.TrimSpace(period)) {
	case "Q1":
		return 1
	case "Q2":
		return 2
	case "Q3":
		return 3
	case "Q4":
		return 4
	case "":
		return 0
	default:
		return 5
	}
}

// clockSeconds parses a Tank01 "M:SS" countdown clock into total seconds
// remaining in the period. An empty or malformed clock reads as 0 (the
// end of the period), never as an error — every notAfter/periodRank
// caller treats a lower reading as later in the period regardless of why
// it is low.
func clockSeconds(clock string) int {
	minutes, seconds, found := strings.Cut(strings.TrimSpace(clock), ":")
	if !found {
		return 0
	}
	m, err1 := strconv.Atoi(minutes)
	s, err2 := strconv.Atoi(seconds)
	if err1 != nil || err2 != nil {
		return 0
	}
	return m*60 + s
}
