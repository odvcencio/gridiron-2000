package fantasy

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
)

// ScoreboardGame is one game's row from getNFLScoresOnly — Tank01's
// whole-slate live scoreboard, keyed by gameDate: score, clock, period,
// status, and (through Raw) the same lineScore possession shape the box
// score carries. It exists so the live poller's layer-1 tick can read
// score/period/possession for every game in one call, which the
// games-for-week listing verifiably cannot provide (see PreseasonGame's
// own doc comment). Verified against testdata/scoresonly-20250907.json, a
// real capture fetched live on 2026-08-31; the in-progress field values
// come from testdata/scoresonly-inprogress-sample.json, synthetic until
// the 2026-09-10 TNF capture pins them.
type ScoreboardGame struct {
	GameID     string
	Away, Home string // Tank01 abbreviations, upper case
	AwayPoints float64
	HomePoints float64
	Status     string // gameStatus text
	StatusCode string // gameStatusCode: "2" final, "1" in progress, "0"/"" pre-game
	Period     string // lineScore.period: "", "Q1".."Q4", "OT", "Final"
	Clock      string // gameClock: "8:12" or ""
	Final      bool
	InProgress bool
	// Raw is the decoded per-game entry, kept for the same tolerant
	// downstream seam BoxScore.Raw feeds: internal/livescore's possession
	// extraction reads its "lineScore.{away,home}.currentlyInPossession"
	// shape. No scoring rule may ever read this field.
	Raw map[string]any
}

// ParseScoresOnly unwraps the Tank01 envelope and parses a
// getNFLScoresOnly body: an object keyed by gameID, one entry per game on
// the requested gameDate. Entries come back sorted by GameID so callers
// see a deterministic order (the wire object's key order is not one). A
// malformed or non-object body parses to no games, never an error — the
// poller treats an empty scoreboard exactly like a failed fetch and falls
// back to its tiered cadence.
func ParseScoresOnly(raw []byte) []ScoreboardGame {
	var body map[string]json.RawMessage
	if err := json.Unmarshal(unwrapEnvelope(raw), &body); err != nil {
		return nil
	}
	games := make([]ScoreboardGame, 0, len(body))
	for key, rawEntry := range body {
		var entry map[string]any
		if err := json.Unmarshal(rawEntry, &entry); err != nil {
			continue
		}
		game := ScoreboardGame{
			GameID:     flexString(entry["gameID"]),
			Away:       strings.ToUpper(strings.TrimSpace(flexString(entry["away"]))),
			Home:       strings.ToUpper(strings.TrimSpace(flexString(entry["home"]))),
			AwayPoints: flexFloat(entry["awayPts"]),
			HomePoints: flexFloat(entry["homePts"]),
			Status:     flexString(entry["gameStatus"]),
			StatusCode: strings.TrimSpace(flexString(entry["gameStatusCode"])),
			Clock:      strings.TrimSpace(flexString(entry["gameClock"])),
			Raw:        entry,
		}
		if game.GameID == "" {
			game.GameID = key
		}
		if lineScore, ok := entry["lineScore"].(map[string]any); ok {
			game.Period = strings.TrimSpace(flexString(lineScore["period"]))
			if game.Clock == "" {
				game.Clock = strings.TrimSpace(flexString(lineScore["gameClock"]))
			}
		}
		game.Final = preseasonFinal(game.Status, game.StatusCode) || strings.EqualFold(game.Period, "final")
		game.InProgress = !game.Final && (game.StatusCode == "1" || (game.StatusCode != "0" && game.StatusCode != "" && game.Period != ""))
		games = append(games, game)
	}
	sort.Slice(games, func(i, j int) bool { return games[i].GameID < games[j].GameID })
	return games
}

// FetchScoresOnly lists every game on one gameDate (YYYYMMDD, Eastern)
// with live score, clock, period, and possession-bearing lineScore — the
// live poller's layer-1 scoreboard call.
func (c *BoxScoreClient) FetchScoresOnly(ctx context.Context, gameDate string) ([]ScoreboardGame, error) {
	raw, err := c.client.get(ctx, "getNFLScoresOnly", map[string]string{"gameDate": gameDate})
	if err != nil {
		return nil, err
	}
	return parseScoresOnlyBody(raw), nil
}

// parseScoresOnlyBody is ParseScoresOnly for a body tank01Client.get has
// already unwrapped (unwrapEnvelope tolerates both, so this is the same
// code path; the split only mirrors ParseBoxScore/parseBoxScore's shape).
func parseScoresOnlyBody(raw json.RawMessage) []ScoreboardGame { return ParseScoresOnly(raw) }
