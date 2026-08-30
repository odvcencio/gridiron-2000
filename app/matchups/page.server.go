package matchups

import (
	"log"

	"gridiron-2000/internal/league"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
)

// MatchupCardData is the typed data.matchups entry: structurally
// identical to page.gsx's strict MatchupCardProps, field for field. It
// stays flat for the two team marks and carries the disclosure rows as one
// package-local StarterLedgerRow slice: Page() reaches MatchupCard through
// exactly one {...matchup} spread, while GoSX still receives a typed row
// value for its strict <Each>. It is named MatchupCardData, not MatchupCard,
// because page.gsx's strict component owns that package-level name.
type MatchupCardData struct {
	ID                 string
	State              string
	ShowLiveIndicator  bool
	LiveIndicator      string
	Status             string
	Clock              string
	AwayID             string
	AwayName           string
	AwayManager        string
	AwayScore          string
	AwayScoreKnown     bool
	AwayScoreNote      string
	AwayTone           string
	AwayAbbreviation   string
	AwayHasAvatarImage bool
	AwayAvatarImageURL string
	LedgerRows         []map[string]any
	HomeID             string
	HomeName           string
	HomeManager        string
	HomeScore          string
	HomeScoreKnown     bool
	HomeScoreNote      string
	HomeTone           string
	HomeAbbreviation   string
	HomeHasAvatarImage bool
	HomeAvatarImageURL string
}

func stringField(m map[string]any, key string) string {
	value, _ := m[key].(string)
	return value
}

func boolField(m map[string]any, key string) bool {
	value, _ := m[key].(bool)
	return value
}

func starterLedgerRows(raw any, teamName string) []map[string]any {
	entries, _ := raw.([]map[string]any)
	rows := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		rows = append(rows, map[string]any{
			"live_key": stringField(entry, "live_key"), "team_name": teamName,
			"slot": stringField(entry, "slot"), "player_id": stringField(entry, "player_id"),
			"player_name": stringField(entry, "player_name"), "position": stringField(entry, "position"),
			"nfl_team": stringField(entry, "nfl_team"), "has_nfl_team": stringField(entry, "nfl_team") != "",
			"points": stringField(entry, "points"), "provenance": stringField(entry, "provenance"),
			"join_state": stringField(entry, "join_state"), "detail": stringField(entry, "detail"),
		})
	}
	return rows
}

// matchupsPageCards converts MatchupsData's map[string]any "matchups" slice
// (each entry carrying nested "away"/"home" maps) into flat typed
// MatchupCardData values, so page.gsx's strict MatchupCard/ScoreTeam/
// TeamMark spread and attribute boundaries all prove clean: the tier-2
// spread boundary rejects a map[string]any source outright (it "cannot
// prove field coverage").
func matchupsPageCards(raw []map[string]any) []MatchupCardData {
	out := make([]MatchupCardData, 0, len(raw))
	for _, entry := range raw {
		away, _ := entry["away"].(map[string]any)
		home, _ := entry["home"].(map[string]any)
		out = append(out, MatchupCardData{
			ID:                 stringField(entry, "id"),
			State:              stringField(entry, "state"),
			ShowLiveIndicator:  boolField(entry, "show_live_indicator"),
			LiveIndicator:      stringField(entry, "live_indicator"),
			Status:             stringField(entry, "status"),
			Clock:              stringField(entry, "clock"),
			AwayID:             stringField(away, "id"),
			AwayName:           stringField(away, "name"),
			AwayManager:        stringField(away, "manager"),
			AwayScore:          stringField(away, "score"),
			AwayScoreKnown:     boolField(away, "score_known"),
			AwayScoreNote:      stringField(away, "score_note"),
			AwayTone:           stringField(away, "tone"),
			AwayAbbreviation:   stringField(away, "abbreviation"),
			AwayHasAvatarImage: boolField(away, "has_avatar_image"),
			AwayAvatarImageURL: stringField(away, "avatar_image_url"),
			HomeID:             stringField(home, "id"),
			HomeName:           stringField(home, "name"),
			HomeManager:        stringField(home, "manager"),
			HomeScore:          stringField(home, "score"),
			HomeScoreKnown:     boolField(home, "score_known"),
			HomeScoreNote:      stringField(home, "score_note"),
			HomeTone:           stringField(home, "tone"),
			HomeAbbreviation:   stringField(home, "abbreviation"),
			HomeHasAvatarImage: boolField(home, "has_avatar_image"),
			HomeAvatarImageURL: stringField(home, "avatar_image_url"),
			LedgerRows:         append(starterLedgerRows(away["starters"], stringField(away, "name")), starterLedgerRows(home["starters"], stringField(home, "name"))...),
		})
	}
	return out
}

func init() {
	if err := route.RegisterFileModuleHere(route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			ctx.NoStore()
			ctx.Runtime().EnableBootstrap()
			ctx.Runtime().BindHub(ScoresLiveHubName, ScoresLiveBindingPath(), nil)
			data := league.Default().MatchupsData(ctx.Request.Context(), ctx.Request)
			if matchups, ok := data["matchups"].([]map[string]any); ok {
				data["matchups"] = matchupsPageCards(matchups)
			}
			return data, nil
		},
		Metadata: func(ctx *route.RouteContext, page route.FilePage, data any) (server.Metadata, error) {
			return server.Metadata{
				Title:       server.Title{Default: league.PageTitle("Matchups")},
				Description: "Fantasy matchup schedules, scoring status, and final results.",
			}, nil
		},
	}); err != nil {
		log.Fatal(err)
	}
}
