package matchups

import (
	"log"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	helpcontent "gridiron-2000/app/help"
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
// from its rendered GameState text (starterGameState, matchup_ledger.go).
// A final game reads as that game's own result — "W 27-20", "L 20-27",
// "T 20-20" (starterFinalLabel) — or, when the source carries no score to
// read a result from, the bare "FINAL" this label used to always be; both
// forms are final here. An in-progress period is always Tank01's
// uppercase Q1..Q4/OT (the Sep 10 drill's decision, plan doc); everything
// else — a formatted kickoff instant ("SUN 4:25 PM") or the empty string
// when the game state is not yet known — reads as not-yet-started.
//
// Matching the result form on its leading W/L/T token is safe against
// every other string this chip can hold: a kickoff instant always starts
// with a weekday ("SUN", "MON", "TUE", "WED", "THU", "FRI", "SAT"), a
// period with Q or OT, and a bye with "BYE" — none of which begin with a
// bare W, L, or T followed by a space.
//
// This is a render-time-only classification: like the top status line's
// own data-live-state attribute, it is set once at render and does not
// itself live-update (only the bound text inside it does) — see
// FeaturedMatchup's/StarterCell's own state-chip doc comments in page.gsx.
func starterStateClass(gameState string) string {
	switch {
	case gameState == "FINAL" || starterStateIsResult(gameState):
		return "state--final"
	case strings.HasPrefix(gameState, "Q") || strings.HasPrefix(gameState, "OT"):
		return "state--live"
	default:
		return "state--pre"
	}
}

// starterStateIsResult reports whether a GameState string is
// starterFinalLabel's result form ("W 27-20"). See starterStateClass for
// why this leading token cannot collide with any other state text.
func starterStateIsResult(gameState string) bool {
	for _, prefix := range []string{"W ", "L ", "T "} {
		if strings.HasPrefix(gameState, prefix) {
			return true
		}
	}
	return false
}

// matchupStateClass derives a matchup-level state-chip modifier class
// from the A5 LiveState (LIVE/PAUSED/FINAL/LEDGER): PAUSED reads as the
// same "signal in flight, just degraded" live treatment as LIVE, since
// both mean a real NFL game the poller is tracking is underway; LEDGER
// (nothing kicked off, or the mirrored weekly ledger already stands in)
// reads as not-yet-started.
func matchupStateClass(liveState string) string {
	switch liveState {
	// UNDERWAY (2026-09-09) shares LIVE's treatment because it shares
	// LIVE's meaning for the reader: real points are on the board and the
	// week is not settled. Only the chip's own word separates them — one
	// says a game is running, the other that the next one has yet to
	// start.
	case "LIVE", "PAUSED", "UNDERWAY":
		return "state--live"
	case "FINAL":
		return "state--final"
	default:
		return "state--pre"
	}
}

// matchupPhaseLabel is the scorebug header's centre label (A1, matchup
// redesign 2026-09-07): PROJ before kickoff (the big number is a
// projection, not a score), nothing once the game is live (the score
// itself is the number that matters, with its own small proj-under-it
// line — see page.gsx's FeaturedMatchup/Scorebug), FINAL once the week
// closes. Deliberately mirrors matchupStateClass's own LIVE/PAUSED/FINAL
// switch so the label and the state-chip class this page already renders
// never disagree about which phase a matchup is in.
func matchupPhaseLabel(liveState string) string {
	switch liveState {
	// UNDERWAY renders no centre label for the same reason LIVE does not:
	// the big number is already the real score, with the projection on
	// its own line underneath. Labelling it PROJ would misread banked
	// points as a forecast, and FINAL — the bug this state was added to
	// fix — would claim a result that does not exist yet.
	case "LIVE", "PAUSED", "UNDERWAY":
		return ""
	case "FINAL":
		return "FINAL"
	default:
		return "PROJ"
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
	Proj            string
	Points          string
	Provenance      string
	JoinState       string
	Detail          string
	Source          string
	// Breakdown explains Points rule by rule
	// (league.ScoreBreakdownText) — the owner's 2026-09-09 request to see
	// where a player's points came from, not only the total.
	Breakdown string
	// Injury is the canonical designation code ("O", "D", "Q", "IR") and
	// InjuryLabel the plain word behind it; HasInjury gates the chip. A
	// late scratch has to be visible in the lineup you are watching, not
	// only on the page where you set it (owner report, 2026-09-10).
	Injury      string
	InjuryLabel string
	HasInjury   bool
	// ProvenanceText/JoinStateText/SourceText (wave-8 audit item 3) are
	// ledgerLineupText/ledgerStatsText/ledgerSourceText's already-labelled,
	// plain-word segments (service.go's starterLedgerMaps) — what the
	// page actually renders. Provenance/JoinState/Source above stay the
	// raw StarterLedgerRow tokens for a caller that wants those instead.
	ProvenanceText string
	JoinStateText  string
	SourceText     string
	GameState      string
	StateClass     string
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
	// proj defaults to the honest zero (matches an empty slot's own PROJ
	// 0.0, A2 of the matchup redesign) when the source map carries no
	// "proj" key at all — defensive against a raw source (for example
	// matchupMaps' own plain starterLedgerMaps, unused by this page today)
	// that has not been decorated with it.
	proj := stringField(row, "proj")
	if proj == "" {
		if stringField(row, "player_id") == "" {
			proj = "0.0"
		} else {
			proj = "—"
		}
	}
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
		Proj:            proj,
		Points:          stringField(row, "points"),
		Provenance:      stringField(row, "provenance"),
		JoinState:       stringField(row, "join_state"),
		Detail:          stringField(row, "detail"),
		Source:          stringField(row, "source"),
		ProvenanceText:  stringField(row, "provenance_text"),
		JoinStateText:   stringField(row, "join_state_text"),
		SourceText:      stringField(row, "source_text"),
		GameState:       stringField(row, "game_state"),
		Breakdown:       stringField(row, "breakdown"),
		Injury:          stringField(row, "injury"),
		InjuryLabel:     stringField(row, "injury_label"),
		HasInjury:       stringField(row, "injury") != "",
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
		ID:   stringField(team, "id"),
		Name: stringField(team, "name"),
		// Manager is the FIRST name only (A1, matchup redesign 2026-09-07):
		// the scorebug header reads "avatar, team name, manager first name,
		// record" — a full manager name duplicated the team name's own
		// identity work and was the more likely of the two to wrap or clip.
		Manager:        league.FirstName(stringField(team, "manager")),
		Record:         stringField(team, "record"),
		Score:          stringField(team, "score"),
		Projected:      stringField(team, "projected"),
		Tone:           stringField(team, "tone"),
		Abbreviation:   stringField(team, "abbreviation"),
		HasAvatarImage: boolField(team, "has_avatar_image"),
		AvatarImageURL: stringField(team, "avatar_image_url"),
	}
}

// BenchRowData is one Benches-disclosure row (A4, matchup redesign
// 2026-09-07): bench composition is public information in this league —
// it explains why a manager is favoured — so this carries only the plain
// facts a manager reads at a glance, no live-bind (the bench never plays
// in this matchup and its weekly projection never changes mid-game).
type BenchRowData struct {
	PlayerName string
	Position   string
	NFLTeam    string
	Proj       string
}

func benchRowsData(raw []map[string]any) []BenchRowData {
	out := make([]BenchRowData, 0, len(raw))
	for _, row := range raw {
		out = append(out, BenchRowData{
			PlayerName: stringField(row, "player_name"),
			Position:   stringField(row, "position"),
			NFLTeam:    stringField(row, "nfl_team"),
			Proj:       stringField(row, "proj"),
		})
	}
	return out
}

// FeaturedMatchupData is the typed data.my_matchup entry: MatchupsData's
// summary-first featured card (A6) — the viewer's own matchup this week,
// or the week's first matchup labeled FEATURED when they have none.
// HasMatchup false (no matchups published this week at all) leaves every
// other field at its zero value; a page rendering this must gate on
// HasMatchup first, the same way data.matchups_empty gates MatchupCard.
type FeaturedMatchupData struct {
	HasMatchup          bool
	IsViewer            bool
	ID                  string
	Label               string
	LiveIndicator       string
	LiveState           string
	StateClass          string
	WinProb             string
	WinProbWidth        string
	WinProbAriaLabel    string
	WinProbAriaValue    float64
	StillToPlay         int
	StillToPlayTotal    int
	StillToPlaySentence string
	PhaseLabel          string
	NextLineupHref      string
	NextWeek            int
	HasNextWeek         bool
	// MineIsHome says which of the two sides is hosting, and WinProbTeam
	// names whose percentage WinProb is. Both exist because neither was
	// derivable from the card itself: "mine" follows the viewer, so it is
	// Home for a spectator and either side for a manager.
	MineIsHome          bool
	WinProbTeam         string
	Mine                FeaturedTeamData
	Theirs              FeaturedTeamData
	Pairs               []FeaturedMatchupPairData
	MineBench           []BenchRowData
	TheirsBench         []BenchRowData
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
	mineBenchRaw, _ := raw["mine_bench"].([]map[string]any)
	theirsBenchRaw, _ := raw["theirs_bench"].([]map[string]any)
	liveState := stringField(raw, "live_state")
	return FeaturedMatchupData{
		HasMatchup:          boolField(raw, "has_matchup"),
		IsViewer:            boolField(raw, "is_viewer"),
		ID:                  stringField(raw, "id"),
		Label:               stringField(raw, "label"),
		LiveIndicator:       stringField(raw, "live_indicator"),
		LiveState:           liveState,
		StateClass:          matchupStateClass(liveState),
		WinProb:             stringField(raw, "win_prob"),
		WinProbWidth:        stringField(raw, "win_prob_width"),
		MineIsHome:          boolField(raw, "mine_is_home"),
		WinProbTeam:         stringField(raw, "win_prob_team"),
		WinProbAriaLabel:    league.WinProbabilityAriaLabel(stringField(raw, "win_prob")),
		WinProbAriaValue:    league.WinProbabilityAriaValue(stringField(raw, "win_prob_width")),
		StillToPlay:         intField(raw, "still_to_play"),
		StillToPlayTotal:    intField(raw, "still_to_play_total"),
		StillToPlaySentence: stringField(raw, "still_to_play_sentence"),
		PhaseLabel:          matchupPhaseLabel(liveState),
		NextLineupHref:      stringField(raw, "next_lineup_href"),
		NextWeek:            intField(raw, "next_week"),
		HasNextWeek:         boolField(raw, "has_next_week"),
		Mine:                featuredTeamData(raw["mine"]),
		Theirs:              featuredTeamData(raw["theirs"]),
		Pairs:               pairs,
		MineBench:           benchRowsData(mineBenchRaw),
		TheirsBench:         benchRowsData(theirsBenchRaw),
	}
}

// ScorebugTeamData is ScorebugData's Away/Home team summary: the compact
// scorebug card's own trimmed team shape (no ScoreNote/ScoreKnown/starter
// ledger — see MatchupCardData for the full-card equivalent).
type ScorebugTeamData struct {
	ID             string
	Name           string
	Manager        string
	Record         string
	Abbreviation   string
	Score          string
	Tone           string
	HasAvatarImage bool
	AvatarImageURL string
}

func scorebugTeamData(raw any) ScorebugTeamData {
	team, _ := raw.(map[string]any)
	return ScorebugTeamData{
		ID:   stringField(team, "id"),
		Name: stringField(team, "name"),
		// Manager is the first name only — see featuredTeamData's own doc
		// comment (A1, matchup redesign 2026-09-07): every around-the-league
		// card gets the same "avatar, team name, manager first name, record"
		// header shape as the featured card.
		Manager:        league.FirstName(stringField(team, "manager")),
		Record:         stringField(team, "record"),
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
	ID            string
	LiveState     string
	StateClass    string
	PhaseLabel    string
	LiveIndicator string
	Status        string
	Clock         string
	Away          ScorebugTeamData
	Home          ScorebugTeamData
	ProjectedAway string
	ProjectedHome string
	// WinProbHome/WinProbHomeWidth/WinProbHomeAriaLabel/WinProbHomeAriaValue
	// (A1, matchup redesign 2026-09-07) give every around-the-league card
	// its own accessible win-probability meter, expressed from the home
	// side's perspective — the same figure shape the featured card's own
	// WinProb/WinProbWidth/WinProbAriaLabel/WinProbAriaValue already carry.
	WinProbHome          string
	WinProbHomeWidth     string
	WinProbHomeAriaLabel string
	WinProbHomeAriaValue float64
	StillToPlay          int
	StillToPlayTotal     int
	StillToPlaySentence  string
	// WinProbTeam names whose probability WinProbHome is — the scorebug
	// reports the HOME side's. Without it the bare percentage said nothing
	// about which of the two teams it described (owner report, 2026-09-10).
	WinProbTeam string
	Pairs       []FeaturedMatchupPairData
	// FocusHref promotes this matchup to the page's own full-width
	// featured card (MatchupsData's "?m=" focus, 2026-09-09). The card
	// keeps its expandable body as well: the link is a second way to read
	// the same matchup at full size, not a replacement for opening it in
	// place.
	FocusHref string
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
		liveState := stringField(entry, "live_state")
		out = append(out, ScorebugData{
			ID:                   stringField(entry, "id"),
			LiveState:            liveState,
			StateClass:           matchupStateClass(liveState),
			PhaseLabel:           matchupPhaseLabel(liveState),
			LiveIndicator:        stringField(entry, "live_indicator"),
			Status:               stringField(entry, "status"),
			Clock:                stringField(entry, "clock"),
			Away:                 scorebugTeamData(away),
			Home:                 scorebugTeamData(home),
			ProjectedAway:        stringField(entry, "projected_away"),
			ProjectedHome:        stringField(entry, "projected_home"),
			WinProbHome:          stringField(entry, "win_prob_home"),
			WinProbHomeWidth:     stringField(entry, "win_prob_home_width"),
			WinProbHomeAriaLabel: league.WinProbabilityAriaLabel(stringField(entry, "win_prob_home")),
			WinProbHomeAriaValue: league.WinProbabilityAriaValue(stringField(entry, "win_prob_home_width")),
			StillToPlay:          intField(entry, "still_to_play"),
			StillToPlayTotal:     intField(entry, "still_to_play_total"),
			StillToPlaySentence:  stringField(entry, "still_to_play_sentence"),
			WinProbTeam:          stringField(entry, "win_prob_team"),
			Pairs:                pairs,
			FocusHref:            stringField(entry, "focus_href"),
		})
	}
	return out
}

// matchupsIsGameDay reports whether the viewed week has at least one
// matchup that is LIVE, PAUSED, UNDERWAY, or FINAL (wave 7b, item 1;
// UNDERWAY added 2026-09-09 — a week whose opener has been played has
// real scores worth leading with, even between games): LiveState
// (matchupStatusLine's own "live_state" field, internal/league/service.go)
// is documented as "the first of PAUSED, LIVE, FINAL, LEDGER present in
// any matchup", but in practice a week with no schedule published yet
// (the preseason fixture, page_render_test.go) leaves it at the Go zero
// value "" rather than the explicit LEDGER sentinel — an equally
// not-game-day state, so this checks the positive LIVE/PAUSED/FINAL set
// rather than "!= LEDGER", to read both the same way. Page() renders the
// score content (MatchupScoreBlock) ahead of the status-line prose
// (MatchupStatusBlock) only once this is true: a still-scheduled or
// not-yet-published week has no score worth leading with, so it keeps the
// status-line context first instead.
func matchupsIsGameDay(statusLineRaw any) bool {
	statusLine, _ := statusLineRaw.(map[string]any)
	switch stringField(statusLine, "live_state") {
	case league.LiveStateLive, league.LiveStatePaused, league.LiveStateUnderway, league.LiveStateFinal:
		return true
	default:
		return false
	}
}

// matchupsPrimaryAction resolves larch's PageActionBar contract for
// /matchups (wave 7b, item 1): the featured matchup's own "Set lineup for
// Week N" link is the one verb worth surfacing when that CTA exists (a
// viewer's own matchup, with a next editable week) — MatchupsData already
// resolves its href server-side (featuredMatchupMap, internal/league/
// service.go), so this only ever points at a week actually still open for
// edits. Every other matchups view (no seated viewer matchup, or nothing
// left to edit) returns nil: the page is read-only for that manager, and
// the bar should not invent a verb it does not have.
func matchupsPrimaryAction(myMatchup FeaturedMatchupData) map[string]any {
	if !myMatchup.IsViewer || !myMatchup.HasNextWeek {
		return nil
	}
	return map[string]any{
		"label": "Set lineup for Week " + strconv.Itoa(myMatchup.NextWeek),
		"href":  myMatchup.NextLineupHref,
		"kind":  "link",
		"tone":  "primary",
	}
}

func matchupsProjectionHelpHref(request *http.Request) string {
	return helpcontent.ContextualTopicURL(
		"lineups-locks-matchups-and-scoring",
		nil,
		helpcontent.ReturnPathForRequest(request, "main-content"),
	)
}

func init() {
	if err := route.RegisterFileModuleHere(route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			ctx.NoStore()
			ctx.Runtime().EnableBootstrap()
			ctx.Runtime().BindHub(ScoresLiveHubName, ScoresLiveBindingPath(), nil)
			data := league.Default().MatchupsData(ctx.Request.Context(), ctx.Request)
			data["projection_help_href"] = matchupsProjectionHelpHref(ctx.Request)
			data["is_game_day"] = matchupsIsGameDay(data["status_line"])
			if myMatchup, ok := data["my_matchup"].(map[string]any); ok {
				typedMyMatchup := featuredMatchupData(myMatchup)
				data["my_matchup"] = typedMyMatchup
				if action := matchupsPrimaryAction(typedMyMatchup); action != nil {
					data["primary_action"] = action
				}
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
