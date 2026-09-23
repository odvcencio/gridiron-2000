package fantasy

import "strings"

func completeDSTTeamStats(stats map[string]any) bool {
	for _, side := range []string{"away", "home"} {
		team, ok := stats[side].(map[string]any)
		if !ok {
			return false
		}
		if strings.TrimSpace(flexString(team["teamAbv"])) == "" || strings.TrimSpace(flexString(team["teamID"])) == "" {
			return false
		}
		for _, key := range []string{"blockedPunt", "blockedFG", "blockedXP", "defensiveTwoPointConversionReturns", "defensiveOrSpecialTeamsTds"} {
			if _, valid := flexFloatOK(team[key]); !valid {
				return false
			}
		}
	}
	return true
}

func completeDSTBox(dst map[string]any, away, home string) bool {
	for _, side := range []struct{ name, team string }{{"away", away}, {"home", home}} {
		unit, ok := dst[side.name].(map[string]any)
		if !ok || !strings.EqualFold(flexString(unit["teamAbv"]), side.team) {
			return false
		}
		for _, key := range append(append([]string(nil), dstStatKeys...), "ptsAllowed") {
			if _, valid := flexFloatOK(unit[key]); !valid {
				return false
			}
		}
	}
	return true
}

// boxForcedFumbles sums credited player defenses for each D/ST. Tank01
// sometimes leaves a replay-overturned forced fumble in a player's final
// aggregate; the full play list records the reversal, so remove that
// credit before publishing the final score.
func boxForcedFumbles(body map[string]any, rawPlayers map[string]any, away, home string) map[string]float64 {
	forced := map[string]float64{}
	for _, rawPlayer := range rawPlayers {
		player, _ := rawPlayer.(map[string]any)
		team := strings.ToUpper(strings.TrimSpace(flexString(player["teamAbv"])))
		defense, _ := player["Defense"].(map[string]any)
		forced[team] += flexFloat(defense["forcedFumbles"])
	}
	teamIDs := map[string]string{}
	teamStats, _ := body["teamStats"].(map[string]any)
	for _, rawTeam := range teamStats {
		team, _ := rawTeam.(map[string]any)
		teamIDs[flexString(team["teamID"])] = strings.ToUpper(flexString(team["teamAbv"]))
	}
	plays, _ := body["allPlayByPlay"].([]any)
	for _, rawPlay := range plays {
		play, _ := rawPlay.(map[string]any)
		text := strings.ToUpper(flexString(play["play"]))
		before, after, reversed := strings.Cut(text, "REVERSED")
		if !reversed || !strings.Contains(before, "FUMBLES (") || strings.Contains(after, "FUMBLES (") {
			continue
		}
		possession := teamIDs[flexString(play["teamID"])]
		defense := ""
		switch possession {
		case away:
			defense = home
		case home:
			defense = away
		}
		if defense != "" && forced[defense] > 0 {
			forced[defense]--
		}
	}
	return forced
}
