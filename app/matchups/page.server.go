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

func intField(m map[string]any, key string) int {
	switch value := m[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	default:
		return 0
	}
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

// StarterCellData is one lineup-slot pair's single side ("mine" or
// "theirs" in FeaturedMatchupPairData) — the strict-component twin of a
// starterLedgerMaps row. A nil raw value (a slot featuredStarterPairs
// could not resolve a row for on this side) converts to the zero value,
// which reads as an unconfigured slot (HasPlayer false), the same as an
// explicit empty-slot row.
type StarterCellData struct {
	HasPlayer  bool
	Right      bool
	LiveKey    string
	PlayerID   string
	PlayerName string
	Position   string
	NFLTeam    string
	HasNFLTeam bool
	Points     string
	Provenance string
	JoinState  string
	Detail     string
	Source     string
	GameState  string
}

// starterCellData converts one side of a FeaturedMatchupPairData row.
// right marks the "theirs" cell of a pair (round-2 review of commit
// 133d1d7, finding 5: the plan's skeleton read a precomputed RightClass
// string, but the six-column slot-row layout only needs to know which
// side it is — page.gsx composes the CSS from Right through a plain
// data-right attribute, styled in public/styles.css).
func starterCellData(raw any, right bool) StarterCellData {
	row, _ := raw.(map[string]any)
	nflTeam := stringField(row, "nfl_team")
	return StarterCellData{
		HasPlayer:  stringField(row, "player_id") != "",
		Right:      right,
		LiveKey:    stringField(row, "live_key"),
		PlayerID:   stringField(row, "player_id"),
		PlayerName: stringField(row, "player_name"),
		Position:   stringField(row, "position"),
		NFLTeam:    nflTeam,
		HasNFLTeam: nflTeam != "",
		Points:     stringField(row, "points"),
		Provenance: stringField(row, "provenance"),
		JoinState:  stringField(row, "join_state"),
		Detail:     stringField(row, "detail"),
		Source:     stringField(row, "source"),
		GameState:  stringField(row, "game_state"),
	}
}

// FeaturedMatchupPairData is one FeaturedMatchupData.Pairs entry: one
// lineupSlots(CurrentRoster()) slot, both sides' starter cell side by
// side, the shape the summary-first "my matchup" card's slot rows render.
type FeaturedMatchupPairData struct {
	Slot   string
	Mine   StarterCellData
	Theirs StarterCellData
}

// FeaturedTeamData is FeaturedMatchupData's Mine/Theirs team summary —
// deliberately not MatchupTeamCard/MatchupCardData's team shape: the
// featured card additionally needs Record and Projected, and never needs
// ScoreNote or ScoreKnown (the featured card always has a live score to
// show, even a 0.0 one).
type FeaturedTeamData struct {
	ID             string
	Name           string
	Manager        string
	Record         string
	Score          string
	Projected      string
	Tone           string
	Abbreviation   string
	HasAvatarImage bool
	AvatarImageURL string
}

func featuredTeamData(raw any) FeaturedTeamData {
	team, _ := raw.(map[string]any)
	return FeaturedTeamData{
		ID:             stringField(team, "id"),
		Name:           stringField(team, "name"),
		Manager:        stringField(team, "manager"),
		Record:         stringField(team, "record"),
		Score:          stringField(team, "score"),
		Projected:      stringField(team, "projected"),
		Tone:           stringField(team, "tone"),
		Abbreviation:   stringField(team, "abbreviation"),
		HasAvatarImage: boolField(team, "has_avatar_image"),
		AvatarImageURL: stringField(team, "avatar_image_url"),
	}
}

// FeaturedMatchupData is the typed data.my_matchup entry: MatchupsData's
// summary-first featured card (A6) — the viewer's own matchup this week,
// or the week's first matchup labeled FEATURED when they have none.
// HasMatchup false (no matchups published this week at all) leaves every
// other field at its zero value; a page rendering this must gate on
// HasMatchup first, the same way data.matchups_empty gates MatchupCard.
type FeaturedMatchupData struct {
	HasMatchup       bool
	IsViewer         bool
	ID               string
	Label            string
	LiveIndicator    string
	LiveState        string
	WinProb          string
	WinProbWidth     string
	StillToPlay      int
	StillToPlayTotal int
	NextLineupHref   string
	NextWeek         int
	HasNextWeek      bool
	Mine             FeaturedTeamData
	Theirs           FeaturedTeamData
	Pairs            []FeaturedMatchupPairData
}

func featuredMatchupData(raw map[string]any) FeaturedMatchupData {
	pairsRaw, _ := raw["pairs"].([]map[string]any)
	pairs := make([]FeaturedMatchupPairData, 0, len(pairsRaw))
	for _, pair := range pairsRaw {
		pairs = append(pairs, FeaturedMatchupPairData{
			Slot:   stringField(pair, "slot"),
			Mine:   starterCellData(pair["mine"], false),
			Theirs: starterCellData(pair["theirs"], true),
		})
	}
	return FeaturedMatchupData{
		HasMatchup:       boolField(raw, "has_matchup"),
		IsViewer:         boolField(raw, "is_viewer"),
		ID:               stringField(raw, "id"),
		Label:            stringField(raw, "label"),
		LiveIndicator:    stringField(raw, "live_indicator"),
		LiveState:        stringField(raw, "live_state"),
		WinProb:          stringField(raw, "win_prob"),
		WinProbWidth:     stringField(raw, "win_prob_width"),
		StillToPlay:      intField(raw, "still_to_play"),
		StillToPlayTotal: intField(raw, "still_to_play_total"),
		NextLineupHref:   stringField(raw, "next_lineup_href"),
		NextWeek:         intField(raw, "next_week"),
		HasNextWeek:      boolField(raw, "has_next_week"),
		Mine:             featuredTeamData(raw["mine"]),
		Theirs:           featuredTeamData(raw["theirs"]),
		Pairs:            pairs,
	}
}

// ScorebugTeamData is ScorebugData's Away/Home team summary: the compact
// scorebug card's own trimmed team shape (no ScoreNote/ScoreKnown/starter
// ledger — see MatchupCardData for the full-card equivalent).
type ScorebugTeamData struct {
	ID             string
	Name           string
	Abbreviation   string
	Score          string
	Tone           string
	HasAvatarImage bool
	AvatarImageURL string
}

func scorebugTeamData(raw any) ScorebugTeamData {
	team, _ := raw.(map[string]any)
	return ScorebugTeamData{
		ID:             stringField(team, "id"),
		Name:           stringField(team, "name"),
		Abbreviation:   stringField(team, "abbreviation"),
		Score:          stringField(team, "score"),
		Tone:           stringField(team, "tone"),
		HasAvatarImage: boolField(team, "has_avatar_image"),
		AvatarImageURL: stringField(team, "avatar_image_url"),
	}
}

// ScorebugData is one data.other_matchups entry: a matchupMaps entry (see
// MatchupCardData) reduced to the compact scorebug card's own fields,
// plus the three A6 summary fields MatchupsData adds to every matchup it
// does not feature (LiveState, ProjectedAway, ProjectedHome,
// StillToPlay). ScorebugSummary (Task 11b) is a copy of app/page.gsx's
// MiniMatchup, not a shared component — see that component's own doc
// comment for why.
type ScorebugData struct {
	ID               string
	LiveState        string
	LiveIndicator    string
	Status           string
	Clock            string
	Away             ScorebugTeamData
	Home             ScorebugTeamData
	ProjectedAway    string
	ProjectedHome    string
	StillToPlay      int
	StillToPlayTotal int
}

// matchupsPageScorebugs converts MatchupsData's "other_matchups" slice
// the way matchupsPageCards converts "matchups" — into typed values a
// strict spread boundary can prove field coverage for.
func matchupsPageScorebugs(raw []map[string]any) []ScorebugData {
	out := make([]ScorebugData, 0, len(raw))
	for _, entry := range raw {
		away, _ := entry["away"].(map[string]any)
		home, _ := entry["home"].(map[string]any)
		out = append(out, ScorebugData{
			ID:               stringField(entry, "id"),
			LiveState:        stringField(entry, "live_state"),
			LiveIndicator:    stringField(entry, "live_indicator"),
			Status:           stringField(entry, "status"),
			Clock:            stringField(entry, "clock"),
			Away:             scorebugTeamData(away),
			Home:             scorebugTeamData(home),
			ProjectedAway:    stringField(entry, "projected_away"),
			ProjectedHome:    stringField(entry, "projected_home"),
			StillToPlay:      intField(entry, "still_to_play"),
			StillToPlayTotal: intField(entry, "still_to_play_total"),
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
			if myMatchup, ok := data["my_matchup"].(map[string]any); ok {
				data["my_matchup"] = featuredMatchupData(myMatchup)
			}
			if others, ok := data["other_matchups"].([]map[string]any); ok {
				data["other_matchups"] = matchupsPageScorebugs(others)
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
