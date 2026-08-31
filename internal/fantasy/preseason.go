package fantasy

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// PreseasonGame is one Tank01 preseason game from getNFLGamesForWeek,
// normalized for the Preseason Blitz feature (design spec section 5.1).
// Label carries the raw gameWeek text; slate identity is a label match, not
// the request's week param (P1) — see SelectPreseasonGames.
type PreseasonGame struct {
	ID         string
	Label      string
	Away       string
	Home       string
	Kickoff    time.Time
	Final      bool
	Date       string // gameDate, e.g. "20250907"
	StatusCode string // gameStatusCode: "2" final, "1" in progress, "0"/"" pre-game
}

// GameListing is the live-scoring name for a week's game entry. It is an
// alias so the poller (internal/livescore) and the parser share one type.
type GameListing = PreseasonGame

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
			ID:         id,
			Label:      label,
			Away:       strings.ToUpper(flexString(entry["away"])),
			Home:       strings.ToUpper(flexString(entry["home"])),
			Kickoff:    preseasonKickoff(entry),
			Final:      preseasonFinal(flexString(entry["gameStatus"]), flexString(entry["gameStatusCode"])),
			Date:       strings.TrimSpace(flexString(entry["gameDate"])),
			StatusCode: strings.TrimSpace(flexString(entry["gameStatusCode"])),
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

// BoxScore is one parsed getNFLBoxScore body: game identity and clock,
// per-player Tank01-keyed stat lines, and the two D/ST units.
type BoxScore struct {
	GameID     string
	Away, Home string // Tank01 abbreviations, upper case (LAR, WSH)
	AwayPoints float64
	HomePoints float64
	Status     string // gameStatus text
	StatusCode string // gameStatusCode: "2" final, "1" in progress, "0"/"" pre-game
	Period     string // currentPeriod: "", "Q1".."Q4", "OT", "Final"
	Clock      string // gameClock: "8:12" or ""
	Final      bool
	InProgress bool                          // code "1", or any non-final code with a non-empty period
	Players    map[string]PlayerLine         // Tank01 playerID -> line
	DST        map[string]map[string]float64 // Tank01 team abbreviation -> dstStatKeys plus ptsAllowed
}

// PlayerLine is one player's box-score row with the identity fields the
// overlay needs when the pool has no matching Tank01 ID.
type PlayerLine struct {
	Name  string // longName
	Team  string // teamAbv
	Stats map[string]float64
}

// dstStatKeys are the DST group fields the DEFENSE scoring group can
// consume. ptsAllowed is read separately because its zero is meaningful.
var dstStatKeys = []string{"sacks", "defensiveInterceptions", "fumblesRecovered", "defTD", "safeties"}

// ParseBoxScore unwraps the Tank01 envelope and parses the body. The
// replay tests and the render fixture use it from other packages.
func ParseBoxScore(raw []byte) BoxScore { return parseBoxScore(unwrapEnvelope(raw)) }

// parseBoxScore decodes a getNFLBoxScore response (already unwrapped) into
// game identity, clock, per-player stat lines, and the two D/ST units.
// It reuses passingStatKeys/rushingStatKeys/receivingStatKeys (F5)
// verbatim, adds kickingStatKeys for the P5 fields, synthesizes returnTD
// from the overloaded Kicking/Punting groups (P6, keyed by field name,
// never by group identity), and parses fumblesLost from both candidate
// locations (P9, unverified — see R2). Defense-only rows and punter-only
// rows carry no scored offense/kicking stats and are dropped from Players
// entirely, matching the projectionStats idiom (F5) of never emitting an
// all-zero entry.
func parseBoxScore(raw json.RawMessage) BoxScore {
	box := BoxScore{Players: map[string]PlayerLine{}, DST: map[string]map[string]float64{}}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		return box
	}
	box.GameID = flexString(body["gameID"])
	box.Away = strings.ToUpper(strings.TrimSpace(flexString(body["away"])))
	box.Home = strings.ToUpper(strings.TrimSpace(flexString(body["home"])))
	box.AwayPoints = flexFloat(body["awayPts"])
	box.HomePoints = flexFloat(body["homePts"])
	box.Status = flexString(body["gameStatus"])
	box.StatusCode = strings.TrimSpace(flexString(body["gameStatusCode"]))
	box.Period = strings.TrimSpace(flexString(body["currentPeriod"]))
	box.Clock = strings.TrimSpace(flexString(body["gameClock"]))
	box.Final = preseasonFinal(box.Status, box.StatusCode) || strings.EqualFold(box.Period, "final")
	box.InProgress = !box.Final && (box.StatusCode == "1" || (box.StatusCode != "0" && box.StatusCode != "" && box.Period != ""))
	playerStats, _ := body["playerStats"].(map[string]any)
	for playerID, rawEntry := range playerStats {
		entry, ok := rawEntry.(map[string]any)
		if !ok {
			continue
		}
		stats := preseasonPlayerStats(entry)
		if len(stats) == 0 {
			continue
		}
		box.Players[playerID] = PlayerLine{
			Name:  strings.TrimSpace(flexString(entry["longName"])),
			Team:  strings.ToUpper(strings.TrimSpace(flexString(entry["teamAbv"]))),
			Stats: stats,
		}
	}
	if dst, ok := body["DST"].(map[string]any); ok {
		// A slice, not a map literal, so "away" is always visited before
		// "home" — deterministic order for anyone stepping through this
		// in a debugger or diffing test output.
		sides := [2]struct {
			side           string
			opponentPoints float64
		}{
			{"away", box.HomePoints},
			{"home", box.AwayPoints},
		}
		for _, pair := range sides {
			unit, ok := dst[pair.side].(map[string]any)
			if !ok {
				continue
			}
			team := strings.ToUpper(strings.TrimSpace(flexString(unit["teamAbv"])))
			if team == "" {
				continue
			}
			line := make(map[string]float64, len(dstStatKeys)+1)
			for _, key := range dstStatKeys {
				line[key] = flexFloat(unit[key])
			}
			// ptsAllowed overrides the opponent-score fallback only when
			// the field parses to a real number. A blank string or a
			// JSON null on an early live frame must fall through to the
			// fallback, not be read as an explicit, shutout-faking 0; an
			// explicit "0" (the game has genuinely allowed no points so
			// far) still overrides, matching TestParseBoxScoreInProgressKeepsZeroPointsAllowed.
			line["ptsAllowed"] = pair.opponentPoints
			if value, ok := flexFloatOK(unit["ptsAllowed"]); ok {
				line["ptsAllowed"] = value
			}
			box.DST[team] = line
		}
	}
	return box
}

// parsePreseasonBoxScore keeps the Blitz shape over the general parser.
func parsePreseasonBoxScore(raw json.RawMessage) (map[string]map[string]float64, bool) {
	box := parseBoxScore(raw)
	out := make(map[string]map[string]float64, len(box.Players))
	for playerID, line := range box.Players {
		out[playerID] = line.Stats
	}
	return out, box.Final
}

// preseasonPlayerStats flattens one playerStats row into the section 4.3
// stat-key space. Zero values are dropped (the projectionStats idiom, F5),
// so a row with no scored group at all (Defense only, or a punter's Punting
// group) returns an empty map and the caller drops it.
//
// Decision: an all-zero row and an absent row are equivalent for the live
// overlay's precedence rule. A live cumulative stat line only grows over
// the course of a game, so it is never below a ledger (mirror) row's
// value; dropping the zero row costs the overlay nothing, since a caller
// that finds no live row simply keeps its ledger row instead, the same
// outcome as if this returned a present-but-zero map.
//
// Two-point conversions (GC-1 fix 3) score at week close only: Tank01's
// box score carries no per-player two-point field at all (verified
// against testdata/preseason-boxscore-sample.json and
// testdata/box-20250904_DAL-PHI.json — the only twoPointConversions field
// either fixture carries is a team-level total under teamStats, not
// attributable to a player), so this function has no source to read it
// from. The league's "twoPt" rule scores from the closed-week nflverse
// ledger instead (main.go's offenseStatLine), the same closed-week-only
// pattern several PUNTING keys already follow.
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

// BoxScoreClient fetches box scores and week listings. The live poller
// holds one; it shares the pool's client so the request counter stays
// whole, or (replay mode) points at a fake relay of its own.
type BoxScoreClient struct {
	client *tank01Client
	season int
}

// defaultBoxScoreMaxBody is NewBoxScoreClient's maxBodyBytes fallback: it
// matches Config's own default (service.go) so a caller that passes 0
// behaves the same as an unconfigured Service.
const defaultBoxScoreMaxBody = 32 << 20

// NewBoxScoreClient builds a standalone client for a caller outside
// Service (the live poller's own client, or a replay-mode fake relay). A
// nil httpClient defaults to http.DefaultClient. baseURL is required —
// this client always talks to a relay (statrelay or a replay server),
// never straight to RapidAPI — and an empty one is almost certainly a
// missing TANK01_BASE_URL, so it returns an error rather than silently
// building a client that can never succeed. maxBodyBytes <= 0 falls back
// to defaultBoxScoreMaxBody so a replay-mode client without an explicit
// Config.MaxBodyBytes still gets a sane cap.
func NewBoxScoreClient(baseURL string, season int, httpClient *http.Client, maxBodyBytes int64) (*BoxScoreClient, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil, fmt.Errorf("NewBoxScoreClient: baseURL is required")
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	if maxBodyBytes <= 0 {
		maxBodyBytes = defaultBoxScoreMaxBody
	}
	return &BoxScoreClient{client: &tank01Client{baseURL: baseURL, client: httpClient, maxBody: maxBodyBytes}, season: season}, nil
}

func (s *Service) BoxScoreClient() *BoxScoreClient {
	return &BoxScoreClient{client: s.client, season: s.config.Season}
}

func (c *BoxScoreClient) FetchBoxScore(ctx context.Context, gameID string) (BoxScore, error) {
	raw, err := c.client.get(ctx, "getNFLBoxScore", map[string]string{"gameID": gameID})
	if err != nil {
		return BoxScore{}, err
	}
	return parseBoxScore(raw), nil
}

// FetchGamesForWeek lists one week. seasonType is "reg" or "pre".
func (c *BoxScoreClient) FetchGamesForWeek(ctx context.Context, seasonType, week string) ([]GameListing, error) {
	raw, err := c.client.get(ctx, "getNFLGamesForWeek", map[string]string{"week": week, "seasonType": seasonType, "season": strconv.Itoa(c.season)})
	if err != nil {
		return nil, err
	}
	return parsePreseasonWeek(raw), nil
}
