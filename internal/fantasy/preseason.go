package fantasy

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gridiron-2000/internal/gameclock"
)

// PreseasonGame is one Tank01 preseason game from getNFLGamesForWeek,
// normalized for the Preseason Blitz feature (design spec section 5.1).
// Label carries the raw gameWeek text; slate identity is a label match, not
// the request's week param (P1) — see SelectPreseasonGames.
//
// GC-2 investigated adding a live score/period/clock to this type so the
// live poller's scoreboard tick (internal/livescore) could change-gate a
// game's box fetch off this one games-list call alone. A live probe on
// 2026-08-31 settled it: the raw getNFLGamesForWeek entry carries only
// schedule fields, status, and espn/cbs links — no score, clock, or
// possession — so these fields do not belong here. The whole-slate live
// scoreboard is a different endpoint, getNFLScoresOnly, verified the same
// day (see ScoreboardGame and testdata/scoresonly-20250907.json); the
// poller change-gates box fetches off that call, and this listing's
// tick-time job is Tank01 ID resolution alone.
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
	Period     string // normalized currentPeriod: "", "Q1".."Q4", "HALF", "OT", "Final"
	Clock      string // gameClock: "8:12" or ""
	Final      bool
	InProgress bool                          // code "1", or any non-final code with a non-empty period
	Players    map[string]PlayerLine         // Tank01 playerID -> line
	DST        map[string]map[string]float64 // Tank01 team abbreviation -> dstStatKeys plus ptsAllowed
	// A complete play list reconciles every punter's event count and gross
	// yardage with the final player box. The poller requires this before
	// accepting a real provider box as final.
	PuntPlayByPlayComplete bool
	ScoringComplete        bool // final box has every live rule source, including team stats and punt plays
	// Raw is the decoded top-level getNFLBoxScore body, kept only for a
	// tolerant downstream seam this package does not itself model:
	// internal/livescore's GC-2b possession extraction (ExtractPossession)
	// reads it for the "lineScore.{away,home}.currentlyInPossession" shape
	// verified against testdata/box-20250904_DAL-PHI.json and
	// testdata/preseason-boxscore-sample.json (both completed games, so
	// both sides read "False" there — the shape is real, a live "True" is
	// not yet observed), plus a short list of speculative fallback keys.
	// No scoring rule may ever read this field; every scored value comes
	// from the typed fields above. It rides along in poller.go's
	// change-detection hash on purpose: a possession flip with no other
	// stat change still counts as new content worth a version bump.
	Raw map[string]any
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
//
// ydsAllowed joined the list on 2026-09-09: the live box score has always
// carried it (verified against testdata/box-20250904_DAL-PHI.json, where
// both units report it), and the yards-allowed ladder the DEFENSE group
// gained that day scores from it. Nothing else in the live DST block is
// left unread. Additional D/ST rules draw on the same box's teamStats and
// playerStats blocks below.
var dstStatKeys = []string{"sacks", "defensiveInterceptions", "fumblesRecovered", "defTD", "safeties", "ydsAllowed"}

// ParseBoxScore unwraps the Tank01 envelope and parses the body. The
// replay tests and the render fixture use it from other packages.
func ParseBoxScore(raw []byte) BoxScore { return parseBoxScore(unwrapEnvelope(raw)) }

// parseBoxScore decodes a getNFLBoxScore response (already unwrapped) into
// game identity, clock, per-player stat lines, and the two D/ST units.
// It reuses passingStatKeys/rushingStatKeys/receivingStatKeys (F5)
// verbatim, adds kickingStatKeys for the P5 fields, synthesizes returnTD
// from the overloaded Kicking/Punting groups (P6, keyed by field name,
// never by group identity), player-level two-point conversions, and fumblesLost from the top level,
// Fumbles, or Defense. Tank01 reported Kyren Williams's lost fumble in
// Defense during the 2026 week 2 Monday game. Defense-only rows without a
// lost fumble carry no scored offense/kicking stats and are dropped from
// Players. Explicit zero-valued
// offense/kicking fields retain a scoreless player's row, so a final box
// score can account for the player without an invented missing-stat gap.
// Punter rows instead retain validated per-player punting aggregates,
// including confirmed zero-punt rows, through addLivePuntingStats.
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
	box.Period = gameclock.NormalizePeriod(flexString(body["currentPeriod"]))
	box.Clock = strings.TrimSpace(flexString(body["gameClock"]))
	box.Final = preseasonFinal(box.Status, box.StatusCode) || strings.EqualFold(box.Period, "final")
	box.InProgress = !box.Final && (box.StatusCode == "1" || (box.StatusCode != "0" && box.StatusCode != "" && box.Period != ""))
	box.Raw = body
	playerStats, _ := body["playerStats"].(map[string]any)
	for playerID, rawEntry := range playerStats {
		entry, ok := rawEntry.(map[string]any)
		if !ok {
			continue
		}
		stats := livePlayerStats(entry)
		puntingKnown := addLivePuntingStats(stats, entry)
		if len(stats) == 0 && !puntingKnown {
			continue
		}
		box.Players[playerID] = PlayerLine{
			Name:  strings.TrimSpace(flexString(entry["longName"])),
			Team:  strings.ToUpper(strings.TrimSpace(flexString(entry["teamAbv"]))),
			Stats: stats,
		}
	}
	addPuntPlayByPlay(&box, body, playerStats)
	forcedFumbles := boxForcedFumbles(body, playerStats, box.Away, box.Home)
	teamStats, _ := body["teamStats"].(map[string]any)
	dst, _ := body["DST"].(map[string]any)
	if dst != nil {
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
			line["forcedFumbles"] = forcedFumbles[team]
			if rawTeam, ok := teamStats[pair.side].(map[string]any); ok {
				line["blockedKicks"] = flexFloat(rawTeam["blockedPunt"]) + flexFloat(rawTeam["blockedFG"]) + flexFloat(rawTeam["blockedXP"])
				line["twoPointReturns"] = flexFloat(rawTeam["defensiveTwoPointConversionReturns"])
				// This team field combines defense and return touchdowns;
				// the separate D/ST defTD total accounts for the former.
				line["specialTeamsTD"] = math.Max(0, flexFloat(rawTeam["defensiveOrSpecialTeamsTds"])-line["defTD"])
			}
			box.DST[team] = line
		}
	}
	box.ScoringComplete = playerStats != nil && box.PuntPlayByPlayComplete && len(box.DST) == 2 && completeDSTBox(dst, box.Away, box.Home) && completeDSTTeamStats(teamStats)
	return box
}

// parsePreseasonBoxScore keeps the offense/kicking Blitz shape over the
// general live parser. Punter scoring belongs to the weekly league ledger,
// not the preseason entry-builder's section 4.3 stat space.
func parsePreseasonBoxScore(raw json.RawMessage) (map[string]map[string]float64, bool) {
	box := parseBoxScore(raw)
	out := make(map[string]map[string]float64, len(box.Players))
	for playerID, line := range box.Players {
		stats := make(map[string]float64, len(line.Stats))
		for key, value := range line.Stats {
			if !strings.HasPrefix(key, "punt") && value != 0 {
				stats[key] = value
			}
		}
		if len(stats) > 0 {
			out[playerID] = stats
		}
	}
	return out, box.Final
}

// livePlayerStats keeps explicit offense/kicking zeroes as scoring evidence.
// An absent, null, malformed, or non-finite value cannot establish a row.
// Return-only groups and a standalone zero fumble count do not establish
// offense/kicking participation; punting has its own validated-count path.
func livePlayerStats(entry map[string]any) map[string]float64 {
	stats := preseasonPlayerStats(entry)
	for key, value := range stats {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			delete(stats, key)
		}
	}
	for groupKey, keyMap := range map[string]map[string]string{
		"Passing": passingStatKeys, "Rushing": rushingStatKeys,
		"Receiving": receivingStatKeys, "Kicking": kickingStatKeys,
	} {
		group, _ := entry[groupKey].(map[string]any)
		for rawKey, normalized := range keyMap {
			if value, valid := flexFloatOK(group[rawKey]); valid && value == 0 {
				stats[normalized] = value
			}
		}
	}
	return stats
}

// preseasonPlayerStats flattens one playerStats row into the section 4.3
// stat-key space. Zero values are dropped for the Blitz shape. The general
// live parser retains explicit offense/kicking zeroes through livePlayerStats
// and separately adds validated punting aggregates, including zero-punt rows.
//
// Tank01's 2026 week 2 box puts Travis Etienne's two-point conversion in
// Rushing.rushingTwoPointConversion. Read player-level conversions from
// all three offense groups; teamStats.twoPointConversions is a team total
// and cannot be attributed to a player.
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
	for _, group := range []struct {
		name string
		keys []string
	}{
		{"Passing", []string{"passingTwoPointConversion", "passingTwoPointConversions"}},
		{"Rushing", []string{"rushingTwoPointConversion", "rushingTwoPointConversions"}},
		{"Receiving", []string{"receivingTwoPointConversion", "receivingTwoPointConversions"}},
	} {
		values := groupStats(entry, group.name)
		for _, key := range group.keys {
			if count, ok := flexFloatOK(values[key]); ok {
				stats["twoPt"] += count
				break // singular/plural aliases describe the same conversions
			}
		}
	}
	if stats["twoPt"] == 0 {
		delete(stats, "twoPt")
	}

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

	for _, source := range []map[string]any{entry, groupStats(entry, "Fumbles"), groupStats(entry, "Defense")} {
		if value, ok := flexFloatOK(source["fumblesLost"]); ok && value != 0 {
			stats["fumblesLost"] = value
			break
		}
	}
	return stats
}

func groupStats(entry map[string]any, group string) map[string]any {
	stats, _ := entry[group].(map[string]any)
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
	client          *tank01Client
	season          int
	requireFinalPBP bool
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
	return &BoxScoreClient{client: s.client, season: s.config.Season, requireFinalPBP: true}
}

func (c *BoxScoreClient) FetchBoxScore(ctx context.Context, gameID string) (BoxScore, error) {
	raw, err := c.client.getFresh(ctx, "getNFLBoxScore", map[string]string{"gameID": gameID, "playByPlay": "true"})
	if err != nil {
		return BoxScore{}, err
	}
	box := parseBoxScore(raw)
	if c.requireFinalPBP && box.Final && !box.ScoringComplete {
		return BoxScore{}, fmt.Errorf("final box for %s has missing or inconsistent scoring details", gameID)
	}
	return box, nil
}

// FetchGamesForWeek lists one week. seasonType is "reg" or "pre".
func (c *BoxScoreClient) FetchGamesForWeek(ctx context.Context, seasonType, week string) ([]GameListing, error) {
	raw, err := c.client.getFresh(ctx, "getNFLGamesForWeek", map[string]string{"week": week, "seasonType": seasonType, "season": strconv.Itoa(c.season)})
	if err != nil {
		return nil, err
	}
	return parsePreseasonWeek(raw), nil
}
