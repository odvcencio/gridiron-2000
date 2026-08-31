package matchups

import (
	"log"
	"strings"
	"unicode/utf8"

	"gridiron-2000/internal/league"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
)

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

// starterStateClass derives one starter cell's state-chip modifier class
// from its rendered GameState text (starterGameState, matchup_ledger.go):
// "FINAL" is always the literal string that function returns for a final
// game; an in-progress period is always Tank01's uppercase Q1..Q4/OT
// (the Sep 10 drill's decision, plan doc); everything else — a formatted
// kickoff instant ("SUN 4:25 PM") or the empty string when the game state
// is not yet known — reads as not-yet-started. This is a render-time-only
// classification: like the top status line's own data-live-state
// attribute, it is set once at render and does not itself live-update
// (only the bound text inside it does) — see FeaturedMatchup's/StarterCell's
// own state-chip doc comments in page.gsx.
func starterStateClass(gameState string) string {
	switch {
	case gameState == "FINAL":
		return "state--final"
	case strings.HasPrefix(gameState, "Q") || strings.HasPrefix(gameState, "OT"):
		return "state--live"
	default:
		return "state--pre"
	}
}

// matchupStateClass derives a matchup-level state-chip modifier class
// from the A5 LiveState (LIVE/PAUSED/FINAL/LEDGER): PAUSED reads as the
// same "signal in flight, just degraded" live treatment as LIVE, since
// both mean a real NFL game the poller is tracking is underway; LEDGER
// (nothing kicked off, or the mirrored weekly ledger already stands in)
// reads as not-yet-started.
func matchupStateClass(liveState string) string {
	switch liveState {
	case "LIVE", "PAUSED":
		return "state--live"
	case "FINAL":
		return "state--final"
	default:
		return "state--pre"
	}
}

// StarterCellData is one lineup-slot pair's single side ("mine" or
// "theirs" in FeaturedMatchupPairData) — the strict-component twin of a
// starterLedgerMaps row. A nil raw value (a slot featuredStarterPairs
// could not resolve a row for on this side) converts to the zero value,
// which reads as an unconfigured slot (HasPlayer false), the same as an
// explicit empty-slot row.
type StarterCellData struct {
	HasPlayer       bool
	Right           bool
	LiveKey         string
	PlayerID        string
	PlayerName      string
	PlayerNameShort string
	Position        string
	NFLTeam         string
	HasNFLTeam      bool
	Points          string
	Provenance      string
	JoinState       string
	Detail          string
	Source          string
	GameState       string
	StateClass      string
	// Possession is GC-2b's possession chip text ("ON OFFENSE", "DEFENSE
	// ON FIELD", or "" — league.StarterLedgerRow.Possession's own doc
	// comment). Rendered only when non-empty (public/styles.css's
	// .possession-chip:empty rule hides an empty bound span the same way
	// .state-chip:empty already does).
	Possession string
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
	playerName := stringField(row, "player_name")
	return StarterCellData{
		HasPlayer:       stringField(row, "player_id") != "",
		Right:           right,
		LiveKey:         stringField(row, "live_key"),
		PlayerID:        stringField(row, "player_id"),
		PlayerName:      playerName,
		PlayerNameShort: mobileShortName(playerName),
		Position:        stringField(row, "position"),
		NFLTeam:         nflTeam,
		HasNFLTeam:      nflTeam != "",
		Points:          stringField(row, "points"),
		Provenance:      stringField(row, "provenance"),
		JoinState:       stringField(row, "join_state"),
		Detail:          stringField(row, "detail"),
		Source:          stringField(row, "source"),
		GameState:       stringField(row, "game_state"),
		StateClass:      starterStateClass(stringField(row, "game_state")),
		Possession:      stringField(row, "possession"),
	}
}

// mobileNameOverflowChars is the full "First Last" length past which a
// name reliably overflows the phone four-column slot-row's name cell (the
// minmax(0,1fr) track beside the two fixed 54px point columns —
// public/styles.css's phone-width .matchups-page .slot-row rule): a
// browser measurement against the replay fixture's own starter names put
// the fitting budget at roughly 12 characters at the cell's ~12.5px bold
// body font (sim_matchups_browser_test.go's mobile name-overflow probe,
// round-1 finding). Kept a plain character count rather than a real glyph
// measurement: this runs at SSR time, with no canvas or layout available,
// and every name in the pool has comparable average glyph width, so
// length is a good enough proxy.
const mobileNameOverflowChars = 12

// mobileShortName abbreviates a starter's first name to its initial
// ("Jayden Daniels" -> "J. Daniels") once the full name is long enough to
// overflow the phone slot-row's name cell (mobileNameOverflowChars). A
// name at or under the budget, or one gosx-render can't split into a
// first and last part (no space, or either side blank — a single-word
// D/ST name, for instance), returns unchanged: StarterCell renders this
// value only inside the phone breakpoint's own name span, so the full
// PlayerName still carries desktop's copy and any name this function
// declines to shorten. The budget counts runes, not bytes (rider on the
// review of ae1a525, item 6): a multi-byte accented name (for example
// "José Ramírez") must compare against the same ~12-glyph budget an
// equally long ASCII name gets, not appear artificially long because
// len() counts its UTF-8 encoding's extra bytes.
func mobileShortName(name string) string {
	if utf8.RuneCountInString(name) <= mobileNameOverflowChars {
		return name
	}
	first, last, ok := strings.Cut(name, " ")
	if !ok || first == "" || last == "" {
		return name
	}
	initial := []rune(first)[0]
	return string(initial) + ". " + last
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
	StateClass       string
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
		StateClass:       matchupStateClass(stringField(raw, "live_state")),
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
	Manager        string
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
		Manager:        stringField(team, "manager"),
		Abbreviation:   stringField(team, "abbreviation"),
		Score:          stringField(team, "score"),
		Tone:           stringField(team, "tone"),
		HasAvatarImage: boolField(team, "has_avatar_image"),
		AvatarImageURL: stringField(team, "avatar_image_url"),
	}
}

// ScorebugData is one data.other_matchups entry: a matchupMaps entry
// reduced to the compact scorebug card's own fields, plus the A6 summary
// fields MatchupsData adds to every matchup it does not feature
// (LiveState, ProjectedAway, ProjectedHome, StillToPlay,
// StillToPlayTotal, Pairs). ScorebugSummary (Task 11b) is a copy of
// app/page.gsx's MiniMatchup, not a shared component — see that
// component's own doc comment for why.
type ScorebugData struct {
	ID               string
	LiveState        string
	StateClass       string
	LiveIndicator    string
	Status           string
	Clock            string
	Away             ScorebugTeamData
	Home             ScorebugTeamData
	ProjectedAway    string
	ProjectedHome    string
	StillToPlay      int
	StillToPlayTotal int
	Pairs            []FeaturedMatchupPairData
}

// matchupsPageScorebugs converts MatchupsData's "other_matchups" slice
// (each matchupMaps entry, plus A6's per-matchup projection/still-to-play
// summary fields) into typed values a strict spread boundary can prove
// field coverage for — the same conversion shape featuredMatchupData
// uses for my_matchup.
func matchupsPageScorebugs(raw []map[string]any) []ScorebugData {
	out := make([]ScorebugData, 0, len(raw))
	for _, entry := range raw {
		away, _ := entry["away"].(map[string]any)
		home, _ := entry["home"].(map[string]any)
		pairsRaw, _ := entry["pairs"].([]map[string]any)
		pairs := make([]FeaturedMatchupPairData, 0, len(pairsRaw))
		for _, pair := range pairsRaw {
			pairs = append(pairs, FeaturedMatchupPairData{
				Slot:   stringField(pair, "slot"),
				Mine:   starterCellData(pair["mine"], false),
				Theirs: starterCellData(pair["theirs"], true),
			})
		}
		out = append(out, ScorebugData{
			ID:               stringField(entry, "id"),
			LiveState:        stringField(entry, "live_state"),
			StateClass:       matchupStateClass(stringField(entry, "live_state")),
			LiveIndicator:    stringField(entry, "live_indicator"),
			Status:           stringField(entry, "status"),
			Clock:            stringField(entry, "clock"),
			Away:             scorebugTeamData(away),
			Home:             scorebugTeamData(home),
			ProjectedAway:    stringField(entry, "projected_away"),
			ProjectedHome:    stringField(entry, "projected_home"),
			StillToPlay:      intField(entry, "still_to_play"),
			StillToPlayTotal: intField(entry, "still_to_play_total"),
			Pairs:            pairs,
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
