package league

import (
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"
)

// FinalGameStats is the first final box accepted for one NFL game. Stat
// values are stored rather than points so the ordinary scoring engine is
// shared by the live page, starter ledger, and week-close transaction.
type FinalGameStats struct {
	Season     int            `json:"season"`
	Week       int            `json:"week"`
	GameID     string         `json:"gameId"`
	CapturedAt time.Time      `json:"capturedAt"`
	Complete   bool           `json:"complete,omitempty"`
	Lines      []WeekStatLine `json:"lines"`
}

func finalGameStatsKey(season, week int, gameID string) string {
	return fmt.Sprintf("%d/%d/%s", season, week, gameID)
}

func cloneFinalGameStats(in FinalGameStats) FinalGameStats {
	out := in
	out.Lines = make([]WeekStatLine, len(in.Lines))
	for i, line := range in.Lines {
		out.Lines[i] = WeekStatLine{Key: line.Key, Source: line.Source, Stats: make(map[string]float64, len(line.Stats))}
		for key, value := range line.Stats {
			out.Lines[i].Stats[key] = value
		}
	}
	return out
}

// RecordFinalGameStats is first-write-wins under the Store lock. It commits
// before the poller publishes final, so a crash or an upstream correction
// cannot change a score the app already called final. A posted week remains
// untouched even if a delayed final box arrives after a force close.
func (s *Store) RecordFinalGameStats(record FinalGameStats) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeErrorLocked(); err != nil {
		return err
	}
	if record.Week < 1 || strings.TrimSpace(record.GameID) == "" {
		return fmt.Errorf("final game requires a week and game ID")
	}
	if s.state.Schedule == nil {
		return nil // there is no fantasy week whose score could change
	}
	season := s.state.Schedule.Season
	key := finalGameStatsKey(season, record.Week, record.GameID)
	if _, exists := s.state.FinalGameStats[key]; exists {
		return nil
	}
	weekExists := false
	for _, week := range s.state.Schedule.Weeks {
		if week.Week != record.Week {
			continue
		}
		weekExists = true
		if scheduleWeekIsFinal(week) {
			return nil
		}
	}
	if !weekExists {
		return nil
	}
	previous := s.state.FinalGameStats
	previousDirty := s.dirty
	updated := make(map[string]FinalGameStats, len(previous)+1)
	for id, saved := range previous {
		updated[id] = saved
	}
	record.Season = season
	record = cloneFinalGameStats(record)
	updated[key] = record
	s.state.FinalGameStats = updated
	if err := s.persistLocked(colScalars); err != nil {
		if persistDispositionOf(err) == persistNotCommitted {
			s.state.FinalGameStats = previous
			s.dirty = previousDirty
		}
		return err
	}
	return nil
}

// ReopenFinalGameStats removes one game's frozen final box, so exactly one
// later RecordFinalGameStats call — the next automatic poll, once an
// upstream feed or NFL stat correction lands, or a commissioner's own
// manual re-entry — wins for that key again. RecordFinalGameStats's
// write-once guard exists to win the race against the poller re-posting a
// still-settling number mid-game, not to survive a real correction for
// the rest of the season; this is the one commissioner-only escape hatch
// for that, so a wrong box does not need the ResetLeague sledgehammer
// (which also wipes every seat, pick, and roster).
//
// Every other game's frozen box, and this game's own write-once guard the
// moment a new box lands, are untouched: reopening one key never disarms
// the general first-write-wins protection.
func (s *Store) ReopenFinalGameStats(week int, gameID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeErrorLocked(); err != nil {
		return err
	}
	gameID = strings.TrimSpace(gameID)
	if week < 1 || gameID == "" {
		return fmt.Errorf("reopening a final box requires a week and game ID")
	}
	if s.state.Schedule == nil {
		return fmt.Errorf("no active schedule; there is no final box to reopen")
	}
	// A posted (closed) fantasy week already refuses every
	// RecordFinalGameStats write, by design (see that method's own
	// posted-week guard) — a closed week's score is immutable. Reopening
	// a game in that state would clear its box with no way for any later
	// write to ever refill it, a one-way data loss instead of the
	// targeted correction this action exists for.
	for _, wk := range s.state.Schedule.Weeks {
		if wk.Week == week && scheduleWeekIsFinal(wk) {
			return fmt.Errorf("week %d is already closed and posted; its final boxes cannot be reopened", week)
		}
	}
	season := s.state.Schedule.Season
	key := finalGameStatsKey(season, week, gameID)
	if _, exists := s.state.FinalGameStats[key]; !exists {
		return fmt.Errorf("no final box recorded for game %q in week %d", gameID, week)
	}
	previous := s.state.FinalGameStats
	previousDirty := s.dirty
	updated := make(map[string]FinalGameStats, len(previous))
	for id, saved := range previous {
		if id == key {
			continue
		}
		updated[id] = saved
	}
	s.state.FinalGameStats = updated
	if err := s.persistLocked(colScalars); err != nil {
		if persistDispositionOf(err) == persistNotCommitted {
			s.state.FinalGameStats = previous
			s.dirty = previousDirty
		}
		return err
	}
	return nil
}

// AdminReopenFinalGameStats is ReopenFinalGameStats's commissioner-facing
// entry point: it authorizes the request, reopens the box, and leaves a
// durable, person-attributed audit record (RecordCommissionerEvent) so
// "who reopened this and when" survives the correction that follows.
func (s *Service) AdminReopenFinalGameStats(r *http.Request, week int, gameID string) error {
	if err := s.requireCommissioner(r); err != nil {
		return err
	}
	gameID = strings.TrimSpace(gameID)
	if err := s.store.ReopenFinalGameStats(week, gameID); err != nil {
		return err
	}
	summary := fmt.Sprintf("reopened the final box for %s (week %d)", gameID, week)
	if _, err := s.RecordCommissionerEvent(r, "finalstats.reopen", summary, CommissionerEventRefs{Week: week}); err != nil {
		log.Printf("commissioner event: finalstats.reopen: %v", err)
	}
	return nil
}

// finalGameRecords returns detached, deterministic copies of this season's
// immutable final boxes for one week.
func (s *Store) finalGameRecords(week int) []FinalGameStats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.state.Schedule == nil {
		return nil
	}
	season := s.state.Schedule.Season
	var keys []string
	for key, record := range s.state.FinalGameStats {
		if record.Season == season && record.Week == week {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	var records []FinalGameStats
	for _, key := range keys {
		records = append(records, cloneFinalGameStats(s.state.FinalGameStats[key]))
	}
	return records
}

func (s *Store) FinalGameLines(week int) []WeekStatLine {
	var lines []WeekStatLine
	for _, record := range s.finalGameRecords(week) {
		lines = append(lines, record.Lines...)
	}
	return lines
}

// RecordFinalGameStats adds confirmed zero rows for every pool player on
// the two NFL teams. A player absent from a final box must not acquire
// box-supported points days later merely because a weekly ledger row arrived.
// D/ST lines are recorded only when the final box actually supplies their
// unit block; an omitted upstream block does not establish a zero defense.
func (s *Service) RecordFinalGameStats(week int, gameID, away, home string, lines []WeekStatLine, complete bool) error {
	known := make(map[string]bool, len(lines))
	finalLines := make([]WeekStatLine, 0, len(lines))
	for _, line := range lines {
		if line.Key == "" || known[line.Key] {
			continue
		}
		line.Source = StatSourceLiveFinal
		known[line.Key] = true
		finalLines = append(finalLines, line)
	}
	away, home = normalizeNFLAbbreviation(away), normalizeNFLAbbreviation(home)
	for _, player := range s.pool().byID {
		if team := normalizeNFLAbbreviation(player.NFLTeam); team != away && team != home {
			continue
		}
		key := playerStatKey(player)
		if !known[key] {
			if strings.EqualFold(player.Position, "DST") {
				continue // an omitted D/ST block cannot establish zero defense
			}
			known[key] = true
			finalLines = append(finalLines, WeekStatLine{Key: key, Source: StatSourceLiveFinal, Stats: map[string]float64{}})
		}
	}
	sort.Slice(finalLines, func(i, j int) bool { return finalLines[i].Key < finalLines[j].Key })
	return s.store.RecordFinalGameStats(FinalGameStats{Week: week, GameID: gameID, CapturedAt: s.clock(), Complete: complete, Lines: finalLines})
}

// ApplyFinalGameStats lets a complete saved final box win for every scoring
// category. Older or incomplete saved boxes still freeze their supported
// categories and take the rest from the weekly mirror. Call after the live
// merge so a later mirror import cannot change a complete final score.
func (s *Service) ApplyFinalGameStats(week int, lines []WeekStatLine) []WeekStatLine {
	records := s.store.finalGameRecords(week)
	if len(records) == 0 {
		return lines
	}
	out := append([]WeekStatLine(nil), lines...)
	index := make(map[string]int, len(out))
	for i, line := range out {
		index[line.Key] = i
	}
	for _, record := range records {
		for _, line := range record.Lines {
			if at, ok := index[line.Key]; ok {
				if record.Complete {
					out[at] = line
					continue
				}
				// A missing final-box player confirms zero for box-supported
				// scoring, but says nothing about mirror-only categories. Those
				// must remain eligible to score under the league's rules.
				merged := WeekStatLine{Key: line.Key, Source: StatSourceLiveFinal, Stats: map[string]float64{}}
				for key, value := range out[at].Stats {
					if !finalBoxRuleKey(key) {
						merged.Stats[key] = value
					}
				}
				for key, value := range line.Stats {
					if finalBoxRuleKey(key) {
						merged.Stats[key] = value
					}
				}
				out[at] = merged
			} else {
				index[line.Key] = len(out)
				out = append(out, line)
			}
		}
	}
	return out
}

// finalBoxRuleKey is the compatibility rule for older/incomplete final
// records. Complete boxes freeze every scoring key through the branch above.
func finalBoxRuleKey(key string) bool {
	switch key {
	case "passYards", "passTD", "passInt", "rushYards", "rushTD",
		"reception", "recYards", "recTD", "fumbleLost", "returnTD",
		"fgMade", "fgMissed", "xpMade", "puntIn20", "puntTouchback",
		"dstSack", "dstInt", "dstFumbleRec", "dstTD", "dstSafety",
		"dstPointsAllowed0", "dstPointsAllowed1", "dstPointsAllowed7",
		"dstPointsAllowed14", "dstPointsAllowed21", "dstPointsAllowed28", "dstPointsAllowed35",
		"dstYardsAllowed0", "dstYardsAllowed100", "dstYardsAllowed200",
		"dstYardsAllowed300", "dstYardsAllowed400", "dstYardsAllowed450", "dstYardsAllowed500":
		return true
	}
	return false
}
