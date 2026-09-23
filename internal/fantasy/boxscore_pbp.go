package fantasy

import (
	"math"
	"regexp"
	"strconv"
	"strings"
)

var puntLanding = regexp.MustCompile(`(?i)\bto\s+([A-Z]{2,3})\s+(\d+)\b`)
var puntLaterSpot = regexp.MustCompile(`(?i)\b(?:impetus ends at|recovers at|recovered by .*? at)\s+([A-Z]{2,3})\s+(\d+)\b`)

// addPuntPlayByPlay uses individual Tank01 punt plays for the rule's
// distance and landing bonuses. It accepts a punter's events only when
// their count and corrected gross yards match the final box aggregate.
// A missing or partial play list leaves the conservative aggregate path.
func addPuntPlayByPlay(box *BoxScore, body map[string]any, rawPlayers map[string]any) {
	plays, present := body["allPlayByPlay"].([]any)
	if !present {
		return
	}
	type total struct {
		count, gross float64
		stats        map[string]float64
	}
	byPlayer := map[string]*total{}
	for _, rawPlay := range plays {
		play, ok := rawPlay.(map[string]any)
		if !ok {
			continue
		}
		participants, _ := play["playerStats"].(map[string]any)
		for playerID, rawStats := range participants {
			groups, _ := rawStats.(map[string]any)
			kicking, _ := groups["Kicking"].(map[string]any)
			punts, validCount := nonnegativePuntingStat(kicking["punts"])
			distance, validDistance := nonnegativePuntingStat(kicking["puntYds"])
			if !validCount || !validDistance || punts != 1 {
				continue
			}
			item := byPlayer[playerID]
			if item == nil {
				item = &total{stats: map[string]float64{}}
				byPlayer[playerID] = item
			}
			text := flexString(play["play"])
			distance, landing := puntDistanceAndLanding(text, distance)
			blocked := strings.Contains(strings.ToLower(text), "punt is blocked") ||
				strings.Contains(strings.ToLower(text), "punt blocked")
			item.count++
			item.gross += distance
			if !blocked && distance >= 40 {
				item.stats["puntYards"] += distance
			}
			if !blocked && distance >= 50 {
				item.stats["puntLong50"]++
			}
			lower := strings.ToLower(text)
			if !blocked && landing >= 0 && landing <= 10 && strings.Contains(lower, "out of bounds") {
				item.stats["coffinCorner"]++
			}
			if !blocked && landing >= 0 && landing <= 5 && strings.Contains(lower, "downed by") {
				item.stats["puntDownedInside5"]++
			}
			if blocked {
				item.stats["puntBlocked"]++
			}
		}
	}
	complete := true
	for playerID, rawPlayer := range rawPlayers {
		player, _ := rawPlayer.(map[string]any)
		punting, _ := player["Punting"].(map[string]any)
		punts, validCount := nonnegativePuntingStat(punting["punts"])
		gross, validGross := nonnegativePuntingStat(punting["puntYds"])
		if !validCount || !validGross {
			continue
		}
		item := byPlayer[playerID]
		if punts == 0 && item == nil {
			item = &total{stats: map[string]float64{}}
		}
		if item == nil || item.count != punts || math.Abs(item.gross-gross) > 0.001 {
			complete = false
			continue
		}
		line, found := box.Players[playerID]
		if !found {
			complete = false
			continue
		}
		for _, key := range []string{"puntYards", "puntLong50", "coffinCorner", "puntDownedInside5", "puntBlocked"} {
			line.Stats[key] = item.stats[key]
		}
		box.Players[playerID] = line
	}
	for playerID := range byPlayer {
		if _, exists := rawPlayers[playerID]; !exists {
			complete = false
		}
	}
	box.PuntPlayByPlayComplete = complete
}

// An NFL gamebook can correct a punt after a muff or an impetus ruling.
// Tank01's play stat retains the initial distance, while its player
// aggregate uses the corrected distance. Both spots appear in the text.
func puntDistanceAndLanding(play string, distance float64) (float64, float64) {
	match := puntLanding.FindStringSubmatchIndex(play)
	if match == nil {
		return distance, -1
	}
	team := play[match[2]:match[3]]
	landing, _ := strconv.ParseFloat(play[match[4]:match[5]], 64)
	lower := strings.ToLower(play)
	if strings.Contains(lower, "muffs") || strings.Contains(lower, "impetus ends") {
		for _, later := range puntLaterSpot.FindAllStringSubmatch(play[match[1]:], -1) {
			if !strings.EqualFold(later[1], team) {
				continue
			}
			spot, err := strconv.ParseFloat(later[2], 64)
			if err == nil {
				return distance + landing - spot, landing
			}
		}
	}
	return distance, landing
}
