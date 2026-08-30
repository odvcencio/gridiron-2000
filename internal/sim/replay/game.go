// Package replay turns a recorded Tank01 play-by-play box score into an
// ordered sequence of in-progress box scores, and serves them behind a
// fake relay (server.go) so the real poller, overlay, fingerprint, hub,
// and browser can run unchanged against a replayed game (S7).
package replay

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// play is one entry in the fixture's allPlayByPlay list: the per-play
// cumulative stat deltas (Tank01 groups keyed by field name, values
// arriving as strings), the clock at the moment it happened, and which
// team had the ball.
type play struct {
	Text    string                               `json:"play"`
	Period  string                               `json:"playPeriod"`
	Clock   string                               `json:"playClock"`
	Players map[string]map[string]map[string]any `json:"playerStats"`
	TeamID  string                               `json:"teamID"`
}

// scoringPlay is one entry in the fixture's top-level scoringPlays list,
// reduced to the fields Frames needs to reconstruct the running score at
// every play (game.go's notAfter, frames.go).
type scoringPlay struct {
	Period string `json:"scorePeriod"`
	Time   string `json:"scoreTime"`
	Home   string `json:"homeScore"`
	Away   string `json:"awayScore"`
}

// Game is one loaded fixture: the untouched final box score body (the
// authoritative Frame N+1), the ordered plays, the ordered scoring plays,
// and each player's team abbreviation (read once from the final
// playerStats, since a per-play delta carries no team of its own).
type Game struct {
	ID         string
	Away, Home string // Tank01 abbreviations, upper case
	final      map[string]any
	plays      []play
	scoring    []scoringPlay
	teamOf     map[string]string // playerID -> teamAbv, from the final playerStats
}

// PlayCount is the number of plays this game's fixture recorded.
func (g *Game) PlayCount() int { return len(g.plays) }

// envelope is the Tank01 response shape {"statusCode":.., "body":..}.
// fantasy exports no envelope decoder, so Load keeps its own copy, local
// to this package (Task 8 plan note).
type envelope struct {
	Body json.RawMessage `json:"body"`
}

// Load reads one Tank01 box-score fixture (an envelope or a bare body)
// from path and decodes it into a Game.
func Load(path string) (*Game, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	body := json.RawMessage(raw)
	var env envelope
	if err := json.Unmarshal(raw, &env); err == nil && len(env.Body) > 0 {
		body = env.Body
	}
	var final map[string]any
	if err := json.Unmarshal(body, &final); err != nil {
		return nil, fmt.Errorf("replay: decode %s: %w", path, err)
	}
	var wrapper struct {
		Plays   []play        `json:"allPlayByPlay"`
		Scoring []scoringPlay `json:"scoringPlays"`
	}
	if err := json.Unmarshal(body, &wrapper); err != nil {
		return nil, fmt.Errorf("replay: decode plays in %s: %w", path, err)
	}
	// allPlayByPlay is decoded separately (above) and dropped from the
	// final body: Frame N+1 is the served box score, which never carried
	// play-by-play in the first place, and every mid-game frame builds
	// its own reduced playerStats/DST from the cumulative deltas.
	delete(final, "allPlayByPlay")

	game := &Game{
		ID:      strings.TrimSpace(stringField(final["gameID"])),
		Away:    strings.ToUpper(strings.TrimSpace(stringField(final["away"]))),
		Home:    strings.ToUpper(strings.TrimSpace(stringField(final["home"]))),
		final:   final,
		plays:   wrapper.Plays,
		scoring: wrapper.Scoring,
		teamOf:  map[string]string{},
	}
	if playerStats, ok := final["playerStats"].(map[string]any); ok {
		for playerID, raw := range playerStats {
			entry, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			team := strings.ToUpper(strings.TrimSpace(stringField(entry["teamAbv"])))
			if team != "" {
				game.teamOf[playerID] = team
			}
		}
	}
	return game, nil
}

// LoadDir loads the first box-*.json in dir that carries allPlayByPlay.
func LoadDir(dir string) (*Game, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("no box-*.json with allPlayByPlay in %s", dir)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, "box-") || !strings.HasSuffix(name, ".json") {
			continue
		}
		path := filepath.Join(dir, name)
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if !strings.Contains(string(raw), "allPlayByPlay") {
			continue
		}
		game, err := Load(path)
		if err != nil {
			continue
		}
		return game, nil
	}
	return nil, fmt.Errorf("no box-*.json with allPlayByPlay in %s", dir)
}

// stringField reads a raw JSON value (decoded into a map[string]any) as a
// string. Tank01 fixtures are string-typed throughout, but this stays
// lenient about a JSON number the same way fantasy's own flexString does,
// since this package has no access to that unexported helper.
func stringField(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case fmt.Stringer:
		return typed.String()
	default:
		return ""
	}
}
