package openstats

import (
	"math"
	"sort"
	"strings"
	"unicode"
)

// PlayerSeasonSummaries aggregates the previous-season "player_stats_prev"
// dataset into one row per player, summed across every week on record. The
// result is sorted by descending fantasy points, ties broken by name, for a
// stable and readable ranking.
func (service *Service) PlayerSeasonSummaries() []PlayerSeasonSummary {
	service.mu.RLock()
	stats := service.statsPrev
	service.mu.RUnlock()
	return aggregatePlayerSeasonSummaries(stats)
}

// playerSeasonAccumulator collects running per-player totals while a season's
// weekly rows are scanned in file order.
type playerSeasonAccumulator struct {
	name          string
	position      string
	team          string
	season        int
	weeks         map[int]bool
	passYds       float64
	passTD        float64
	passInt       float64
	rushYds       float64
	rushTD        float64
	receptions    float64
	recYds        float64
	recTD         float64
	fantasyPoints float64
}

// aggregatePlayerSeasonSummaries sums PlayerWeekStat rows per player_id.
// Games counts distinct weeks seen; Team keeps the last team seen for that
// player in row order (handles in-season trades). FantasyPoints sums the
// half-PPR value, computed as the average of the parser's FantasyPoints
// (standard) and FantasyPointsPPR fields -- exact because PPR scoring adds
// exactly 1.0 point per reception over standard and half-PPR adds exactly
// 0.5, so the midpoint of the two fields IS the half-PPR total, not an
// approximation.
func aggregatePlayerSeasonSummaries(stats []PlayerWeekStat) []PlayerSeasonSummary {
	order := make([]string, 0, len(stats))
	byID := make(map[string]*playerSeasonAccumulator, len(stats))
	for _, stat := range stats {
		id := stat.PlayerID
		if id == "" {
			continue
		}
		acc, ok := byID[id]
		if !ok {
			acc = &playerSeasonAccumulator{weeks: map[int]bool{}}
			byID[id] = acc
			order = append(order, id)
		}
		if stat.PlayerName != "" {
			acc.name = stat.PlayerName
		}
		if stat.Position != "" {
			acc.position = stat.Position
		}
		if stat.Team != "" {
			acc.team = stat.Team
		}
		acc.season = stat.Season
		acc.weeks[stat.Week] = true
		acc.passYds += stat.PassingYards
		acc.passTD += stat.PassingTDs
		acc.passInt += stat.PassingInterceptions
		acc.rushYds += stat.RushingYards
		acc.rushTD += stat.RushingTDs
		acc.receptions += stat.Receptions
		acc.recYds += stat.ReceivingYards
		acc.recTD += stat.ReceivingTDs
		acc.fantasyPoints += (stat.FantasyPoints + stat.FantasyPointsPPR) / 2
	}

	summaries := make([]PlayerSeasonSummary, 0, len(order))
	for _, id := range order {
		acc := byID[id]
		summaries = append(summaries, PlayerSeasonSummary{
			PlayerName:    acc.name,
			Position:      acc.position,
			Team:          acc.team,
			Season:        acc.season,
			Games:         len(acc.weeks),
			PassYds:       roundToInt(acc.passYds),
			PassTD:        roundToInt(acc.passTD),
			PassInt:       roundToInt(acc.passInt),
			RushYds:       roundToInt(acc.rushYds),
			RushTD:        roundToInt(acc.rushTD),
			Receptions:    roundToInt(acc.receptions),
			RecYds:        roundToInt(acc.recYds),
			RecTD:         roundToInt(acc.recTD),
			FantasyPoints: roundTo(acc.fantasyPoints, 2),
		})
	}
	sort.SliceStable(summaries, func(i, j int) bool {
		if summaries[i].FantasyPoints != summaries[j].FantasyPoints {
			return summaries[i].FantasyPoints > summaries[j].FantasyPoints
		}
		return summaries[i].PlayerName < summaries[j].PlayerName
	})
	return summaries
}

func roundToInt(value float64) int {
	return int(math.Round(value))
}

func roundTo(value float64, places int) float64 {
	scale := math.Pow(10, float64(places))
	return math.Round(value*scale) / scale
}

// NormalizePlayerKey builds a lookup key for matching a player across
// datasets that spell names differently (punctuation, casing, suffixes). It
// lowercases the name, strips every non-alphanumeric character, and appends
// the upper-cased position so same-named players at different positions do
// not collide.
func NormalizePlayerKey(name, position string) string {
	var builder strings.Builder
	for _, r := range name {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			builder.WriteRune(unicode.ToLower(r))
		}
	}
	builder.WriteByte('|')
	builder.WriteString(strings.ToUpper(strings.TrimSpace(position)))
	return builder.String()
}
