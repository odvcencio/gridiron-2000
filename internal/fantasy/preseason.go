package fantasy

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

// PreseasonGame is one Tank01 preseason game from getNFLGamesForWeek,
// normalized for the Preseason Blitz feature (design spec section 5.1).
// Label carries the raw gameWeek text; slate identity is a label match, not
// the request's week param (P1) — see SelectPreseasonGames.
type PreseasonGame struct {
	ID      string
	Label   string
	Away    string
	Home    string
	Kickoff time.Time
	Final   bool
}

// parsePreseasonWeek decodes a getNFLGamesForWeek response (already
// unwrapped by unwrapEnvelope) into a game list. It reads no "week"
// field: the response never echoes the request param, and the index is
// offset from label to label anyway (P1) — a game's identity is its
// gameWeek label, resolved by the caller (see SelectPreseasonGames).
func parsePreseasonWeek(raw json.RawMessage) []PreseasonGame {
	var entries []map[string]any
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil
	}
	out := make([]PreseasonGame, 0, len(entries))
	for _, entry := range entries {
		id := flexString(entry["gameID"])
		label := flexString(entry["gameWeek"])
		if id == "" || label == "" {
			continue
		}
		out = append(out, PreseasonGame{
			ID:      id,
			Label:   label,
			Away:    strings.ToUpper(flexString(entry["away"])),
			Home:    strings.ToUpper(flexString(entry["home"])),
			Kickoff: preseasonKickoff(entry),
			Final:   preseasonFinal(flexString(entry["gameStatus"]), flexString(entry["gameStatusCode"])),
		})
	}
	return out
}

// SelectPreseasonGames returns every game in games whose gameWeek label
// matches target, case-insensitively. Selection never reads the "week"
// query param that produced the response: P1 confirms the index is
// offset (week=N can return games labeled for week N-1), so identity must
// come from the label alone (section 4.1). A target with zero matches
// returns nil; the caller (the poller's boot probe, blitz_source.go) owns
// logging that as a probe miss.
func SelectPreseasonGames(games []PreseasonGame, target string) []PreseasonGame {
	target = strings.ToLower(strings.TrimSpace(target))
	if target == "" {
		return nil
	}
	var out []PreseasonGame
	for _, game := range games {
		if strings.ToLower(strings.TrimSpace(game.Label)) == target {
			out = append(out, game)
		}
	}
	return out
}

// preseasonKickoff resolves one game's kickoff instant: gameTime_epoch
// (via flexFloat) first, falling back to gameDate+gameTime parsed in
// America/New_York (section 5.1). A game with neither field usable
// returns the zero time.
func preseasonKickoff(entry map[string]any) time.Time {
	if epoch := flexFloat(entry["gameTime_epoch"]); epoch > 0 {
		return time.Unix(int64(epoch), 0).UTC()
	}
	date := strings.TrimSpace(flexString(entry["gameDate"]))
	if date == "" {
		return time.Time{}
	}
	clock := strings.TrimSpace(flexString(entry["gameTime"]))
	if clock == "" {
		clock = "1:00p"
	}
	eastern, err := time.LoadLocation("America/New_York")
	if err != nil {
		eastern = time.UTC
	}
	// Tank01 has shipped a few gameTime spellings across endpoints; try the
	// ones observed in this codebase's other feeds before giving up.
	layouts := []string{"20060102 3:04p", "20060102 3:04 PM", "20060102 15:04"}
	for _, layout := range layouts {
		if parsed, err := time.ParseInLocation(layout, date+" "+clock, eastern); err == nil {
			return parsed.UTC()
		}
	}
	return time.Time{}
}

// preseasonFinal reports whether a game or box score is final: gameStatus
// of Final or Completed, or gameStatusCode "2" (P7). Either signal alone is
// sufficient; the week-list endpoint carries only the first, the box-score
// endpoint carries both.
func preseasonFinal(status, statusCode string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "final", "completed":
		return true
	}
	return strings.TrimSpace(statusCode) == "2"
}

// kickingStatKeys maps a raw Kicking group field (P5) to its normalized
// stat key. kickReturns/kickReturnYds are deliberately absent — section 4.3
// keeps only the touchdown synthesis from the overloaded Kicking/Punting
// groups (P6); a returner's raw return yardage does not score in this
// league.
var kickingStatKeys = map[string]string{
	"fgMade":   "fgMade",
	"fgMissed": "fgMissed",
	"xpMade":   "xpMade",
}

// parsePreseasonBoxScore decodes a getNFLBoxScore response (already
// unwrapped) into playerID -> normalized stat line, plus the game's final
// flag. It reuses passingStatKeys/rushingStatKeys/receivingStatKeys (F5)
// verbatim, adds kickingStatKeys for the P5 fields, synthesizes returnTD
// from the overloaded Kicking/Punting groups (P6, keyed by field name,
// never by group identity), and parses fumblesLost from both candidate
// locations (P9, unverified — see R2). Defense-only rows and punter-only
// rows carry no scored stats and are dropped entirely, matching the
// projectionStats idiom (F5) of never emitting an all-zero entry.
func parsePreseasonBoxScore(raw json.RawMessage) (map[string]map[string]float64, bool) {
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		return map[string]map[string]float64{}, false
	}
	final := preseasonFinal(flexString(body["gameStatus"]), flexString(body["gameStatusCode"])) ||
		strings.EqualFold(strings.TrimSpace(flexString(body["currentPeriod"])), "final")

	playerStats, _ := body["playerStats"].(map[string]any)
	out := make(map[string]map[string]float64, len(playerStats))
	for playerID, rawEntry := range playerStats {
		entry, ok := rawEntry.(map[string]any)
		if !ok {
			continue
		}
		stats := preseasonPlayerStats(entry)
		if len(stats) == 0 {
			continue
		}
		out[playerID] = stats
	}
	return out, final
}

// preseasonPlayerStats flattens one playerStats row into the section 4.3
// stat-key space. Zero values are dropped (the projectionStats idiom, F5),
// so a row with no scored group at all (Defense only, or a punter's Punting
// group) returns an empty map and the caller drops it.
func preseasonPlayerStats(entry map[string]any) map[string]float64 {
	stats := map[string]float64{}
	addGroup := func(groupKey string, keyMap map[string]string) {
		group, ok := entry[groupKey].(map[string]any)
		if !ok {
			return
		}
		for rawKey, normKey := range keyMap {
			if value := flexFloat(group[rawKey]); value != 0 {
				stats[normKey] = value
			}
		}
	}
	addGroup("Passing", passingStatKeys)
	addGroup("Rushing", rushingStatKeys)
	addGroup("Receiving", receivingStatKeys)
	addGroup("Kicking", kickingStatKeys)

	returnTD := 0.0
	if group, ok := entry["Kicking"].(map[string]any); ok {
		returnTD += flexFloat(group["kickReturnTD"])
	}
	if group, ok := entry["Punting"].(map[string]any); ok {
		returnTD += flexFloat(group["puntReturnTD"])
	}
	if returnTD != 0 {
		stats["returnTD"] = returnTD
	}

	if value := flexFloat(entry["fumblesLost"]); value != 0 {
		stats["fumblesLost"] = value
	} else if group, ok := entry["Fumbles"].(map[string]any); ok {
		if value := flexFloat(group["fumblesLost"]); value != 0 {
			stats["fumblesLost"] = value
		}
	}
	return stats
}

// FetchPreseasonWeek fetches and parses one getNFLGamesForWeek response.
// weekParam is the Tank01 "week" query value — a hint only (P1); callers
// resolve slate identity by label (SelectPreseasonGames). The request goes
// through the shared client, so its cost stays visible on the admin card's
// request counter (F2).
func (s *Service) FetchPreseasonWeek(ctx context.Context, weekParam string) ([]PreseasonGame, error) {
	raw, err := s.client.get(ctx, "getNFLGamesForWeek", map[string]string{
		"week":       weekParam,
		"seasonType": "pre",
		"season":     strconv.Itoa(s.config.Season),
	})
	if err != nil {
		return nil, err
	}
	return parsePreseasonWeek(raw), nil
}

// FetchPreseasonBoxScore fetches and parses one getNFLBoxScore response:
// the normalized per-player stat lines (section 4.3) plus the game's final
// flag (P7).
func (s *Service) FetchPreseasonBoxScore(ctx context.Context, gameID string) (map[string]map[string]float64, bool, error) {
	raw, err := s.client.get(ctx, "getNFLBoxScore", map[string]string{"gameID": gameID})
	if err != nil {
		return nil, false, err
	}
	stats, final := parsePreseasonBoxScore(raw)
	return stats, final, nil
}
