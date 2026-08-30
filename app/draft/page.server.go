package draft

import (
	"fmt"
	"gridiron-2000/internal/actionui"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"gridiron-2000/internal/league"
	"m31labs.dev/gosx/action"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
	"m31labs.dev/gosx/session"
)

// DraftTeamCard is the typed data.teams entry spread into strict
// DraftTeam. It deliberately does not share DraftTeamProps' name (page.gsx
// declares that name itself, for gosx's own strict-component check): the
// tier-2 spread boundary (strictSpreadProps) proves struct values field by
// field, so DraftTeamCard only needs to structurally cover DraftTeamProps,
// and a distinct name here avoids colliding with page.gsx's own
// declaration when gosx build's strict-component check merges the two
// files' types.
type DraftTeamCard struct {
	TeamID         string
	OnClock        bool
	Tone           string
	HasAvatarImage bool
	AvatarImageURL string
	Name           string
	Abbreviation   string
	Presence       string
	PresenceLabel  string
	PresenceDetail string
	OperatorCount  int
	Manager        string
	Division       string
	Claimed        bool
	Ready          bool
	Autopick       bool
	BoardCount     int
	BoardGap       bool
}

type DraftSeatControlCard struct {
	TeamID         string
	Name           string
	Manager        string
	PresenceLabel  string
	PresenceDetail string
	OnClock        bool
	Ready          bool
	Autopick       bool
	BoardCount     int
	BoardGap       bool
	Action         string
	ReadyAction    string
	CSRF           string
}

func stringField(m map[string]any, key string) string {
	value, _ := m[key].(string)
	return value
}

func parseSeatAutopick(raw string) (bool, error) {
	switch raw {
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, fmt.Errorf("AUTO mode must be exactly true or false")
	}
}

func parseSeatReady(raw string) (bool, error) {
	switch raw {
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, fmt.Errorf("Ready must be exactly true or false")
	}
}

func boolField(m map[string]any, key string) bool {
	value, _ := m[key].(bool)
	return value
}

func intField(m map[string]any, key string) int {
	value, _ := m[key].(int)
	return value
}

func mapField(m map[string]any, key string) map[string]any {
	value, _ := m[key].(map[string]any)
	if value == nil {
		return map[string]any{}
	}
	return value
}

func draftActionPath(name string) string { return "/draft/__actions/" + name }

func draftRedirectTarget(pos, query, page string) string {
	values := url.Values{}
	if pos != "" {
		values.Set("pos", pos)
	}
	if query != "" {
		values.Set("q", query)
	}
	if parsed, err := strconv.Atoi(page); err == nil && parsed > 1 {
		values.Set("page", strconv.Itoa(parsed))
	}
	if encoded := values.Encode(); encoded != "" {
		return "/draft?" + encoded
	}
	return "/draft"
}

func draftActionSuccess(ctx *action.Context, target, message string) error {
	if action.WantsJSON(ctx.Request) {
		return ctx.Success(message, map[string]any{"value": "refresh"})
	}
	actionui.RedirectWithNotice(ctx, target, message)
	return nil
}

type draftBreakdownRowView struct {
	Scored bool
	Label  string
	Calc   string
	Points string
}

type draftPlayerCardView struct {
	ID              string
	Name            string
	Position        string
	NFLTeam         string
	Projection      string
	Rank            string
	Detail          string
	Headshot        string
	HasHeadshot     bool
	Jersey          string
	HasBreakdown    bool
	Breakdown       []draftBreakdownRowView
	BreakdownTotal  string
	HasHist         bool
	Hist            string
	Search          string
	HasDraftCapital bool
	DraftCapital    string
	HasOpponent     bool
	Opponent        string
	HasMatchup      bool
	MatchupTier     string
	MatchupChip     string
	MatchupDetail   string
	CanDraft        bool
	// Taken marks a personal-queue entry someone else already drafted (the
	// row still shows, struck through client-side, rather than silently
	// disappearing). Always false for an available-pool entry, which
	// excludes drafted players by construction.
	Taken bool
	// HasValue/ValueLabel back the available pane's VS ADP cell (R4).
	HasValue   bool
	ValueLabel string
}

type draftRoomView struct {
	Data          map[string]any
	CSRF          string
	Actions       map[string]string
	StatusSummary string
}

type draftWorkspaceView struct {
	Data           map[string]any
	Players        []draftPlayerCardView
	CSRF           string
	MakePickAction string
}

// draftCommandView backs the always-visible command bar region: the
// on-clock team, the pick clock, the room summary, the sound and
// commissioner controls, and (while seated) the ready/autopick controls.
type draftCommandView struct {
	Data          map[string]any
	CSRF          string
	Actions       map[string]string
	StatusSummary string
}

// draftHistoryView backs the pick-tape pane's Tape/Board/Teams tabs (D4):
// the same field set page.gsx's DraftHistoryProps declares, so this one
// value renders through either "DraftHistory" (fragment.go's
// draftRegionView, Since < 0) or "DraftTapeRows" (Since >= 0) — the
// established pattern every other shell view (room/workspace/command/
// available/queue) already uses, a page-level Go type distinct from but
// structurally covering its GSX prop counterpart. Since is the "?since="
// tape cursor (draftTapeSinceKey, fragment.go): -1 means "unset", the
// pane's full render; a non-negative value switches draftRegionView to
// DraftTapeRows and attachDraftFragmentSince below has already trimmed
// Rounds to picks numbered above it.
type draftHistoryView struct {
	Rounds      []draftTapeRoundView
	Board       draftBoardView
	Teams       []draftTeamColumnView
	Complete    bool
	Latest      int
	Since       int
	HasOnClock  bool
	NextLabel   string
	OnClockName string
	OnClockAbbr string
	OnClockTone string
}

// draftTapePickView is the typed pick/board/team view's row-level entry,
// structurally covering page.gsx's TapePick (the tier-2 spread boundary
// DraftTeamCard's doc comment above describes) under a distinct name so
// gosx build's strict-component check never sees two TapePick
// declarations when it merges this file with page.gsx.
type draftTapePickView struct {
	Number, Round, Slot, Column                   int
	Label                                         string
	TeamID, TeamName, TeamAbbr, TeamTone, Manager string
	HasAvatarImage                                bool
	AvatarImageURL                                string
	PlayerID, PlayerName, Position, NFLTeam       string
	MadeBy                                        string
	IsAuto, IsCommissioner, Mine                  bool
	TimeToPickSec                                 int
	TimeToPick                                    string
	HasValue                                      bool
	Value                                         int
	ValueLabel                                    string
	MadeAt                                        string
	Projection                                    string
	Source                                        string
	BestAvailable                                 []draftBestAvailableView
	TeamPicks                                     []draftTapePickView
}

type draftBestAvailableView struct {
	Name, Position, NFLTeam string
}

type draftTapeRoundView struct {
	Round, First, Last int
	Direction          string
	Current            bool
	Made, Total        int
	Picks              []draftTapePickView
}

type draftBoardCellView struct {
	Round, Column, Number  int
	Label                  string
	Filled, Mine, OnClock  bool
	PlayerName, Position   string
	IsAuto, IsCommissioner bool
}

type draftBoardRowView struct {
	Round     int
	Direction string
	Cells     []draftBoardCellView
}

type draftBoardView struct {
	Columns     []map[string]any
	Rows        []draftBoardRowView
	ColumnCount string
}

// draftBoardTeamView mirrors page.gsx's DraftBoardTeam.
type draftBoardTeamView struct {
	ID             string
	Name           string
	Abbreviation   string
	Tone           string
	Manager        string
	HasAvatarImage bool
	AvatarImageURL string
	Mine           bool
}

type draftTeamColumnView struct {
	Team  draftBoardTeamView
	Picks []draftTapePickView
	Needs []map[string]any
}

func boardTeamProps(team map[string]any) draftBoardTeamView {
	return draftBoardTeamView{
		ID: stringField(team, "id"), Name: stringField(team, "name"), Abbreviation: stringField(team, "abbreviation"),
		Tone: stringField(team, "tone"), Manager: stringField(team, "manager"),
		HasAvatarImage: boolField(team, "has_avatar_image"), AvatarImageURL: stringField(team, "avatar_image_url"),
		Mine: boolField(team, "mine"),
	}
}

// tapePickProps converts one league.TapePick into its page-level mirror:
// field-for-field, the same conversion boundary draftTeamProps/
// draftPlayerProps already use for teams/players.
func tapePickProps(pick league.TapePick) draftTapePickView {
	return draftTapePickView{
		Number: pick.Number, Round: pick.Round, Slot: pick.Slot, Column: pick.Column,
		Label:    pick.Label,
		TeamID:   pick.TeamID,
		TeamName: pick.TeamName, TeamAbbr: pick.TeamAbbr, TeamTone: pick.TeamTone, Manager: pick.Manager,
		HasAvatarImage: pick.HasAvatarImage, AvatarImageURL: pick.AvatarImageURL,
		PlayerID: pick.PlayerID, PlayerName: pick.PlayerName, Position: pick.Position, NFLTeam: pick.NFLTeam,
		MadeBy: pick.MadeBy, IsAuto: pick.IsAuto, IsCommissioner: pick.IsCommissioner, Mine: pick.Mine,
		TimeToPickSec: pick.TimeToPickSec, TimeToPick: pick.TimeToPick,
		HasValue: pick.HasValue, Value: pick.Value, ValueLabel: pick.ValueLabel, MadeAt: pick.MadeAt,
	}
}

func tapePicksProps(picks []league.TapePick) []draftTapePickView {
	out := make([]draftTapePickView, 0, len(picks))
	for _, pick := range picks {
		out = append(out, tapePickProps(pick))
	}
	return out
}

// bestAvailableProps converts a pick detail's best-available snapshot into
// its page-level mirror.
func bestAvailableProps(items []league.BestAvailablePick) []draftBestAvailableView {
	out := make([]draftBestAvailableView, 0, len(items))
	for _, item := range items {
		out = append(out, draftBestAvailableView{Name: item.Name, Position: item.Position, NFLTeam: item.NFLTeam})
	}
	return out
}

// hydratedTapePicksProps enriches each bare TapePick with its own expanded
// PickDetail fields (Projection, Source, BestAvailable, TeamPicks) — the
// tape's own inline accordion — by looking each one up in history.Detail.
// TeamColumn.Picks and the ledger/CSV never call this: neither renders a
// <DraftPickDetail>, so their picks stay bare (the zero value for those
// fields is harmless — see TapePick's doc comment, page.gsx).
func hydratedTapePicksProps(picks []league.TapePick, history league.DraftHistoryView) []draftTapePickView {
	out := make([]draftTapePickView, 0, len(picks))
	for _, pick := range picks {
		card := tapePickProps(pick)
		detail := history.Detail(pick.Number)
		card.Projection = detail.Projection
		card.Source = detail.Source
		card.BestAvailable = bestAvailableProps(detail.BestAvailable)
		card.TeamPicks = tapePicksProps(detail.TeamPicks)
		out = append(out, card)
	}
	return out
}

func tapeRoundsProps(rounds []league.TapeRound, history league.DraftHistoryView) []draftTapeRoundView {
	out := make([]draftTapeRoundView, 0, len(rounds))
	for _, round := range rounds {
		out = append(out, draftTapeRoundView{
			Round: round.Round, First: round.First, Last: round.Last, Direction: round.Direction,
			Current: round.Current, Made: round.Made, Total: round.Total,
			Picks: hydratedTapePicksProps(round.Picks, history),
		})
	}
	return out
}

func boardCellProps(cell league.BoardCell) draftBoardCellView {
	return draftBoardCellView{
		Round: cell.Round, Column: cell.Column, Number: cell.Number, Label: cell.Label,
		Filled: cell.Filled, Mine: cell.Mine, OnClock: cell.OnClock,
		PlayerName: cell.PlayerName, Position: cell.Position, IsAuto: cell.IsAuto, IsCommissioner: cell.IsCommissioner,
	}
}

func boardRowsProps(rows []league.BoardRow) []draftBoardRowView {
	out := make([]draftBoardRowView, 0, len(rows))
	for _, row := range rows {
		cells := make([]draftBoardCellView, 0, len(row.Cells))
		for _, cell := range row.Cells {
			cells = append(cells, boardCellProps(cell))
		}
		out = append(out, draftBoardRowView{Round: row.Round, Direction: row.Direction, Cells: cells})
	}
	return out
}

func boardViewProps(board league.BoardView) draftBoardView {
	return draftBoardView{Columns: board.Columns, Rows: boardRowsProps(board.Rows), ColumnCount: strconv.Itoa(len(board.Columns))}
}

func teamColumnsProps(teams []league.TeamColumn) []draftTeamColumnView {
	out := make([]draftTeamColumnView, 0, len(teams))
	for _, team := range teams {
		out = append(out, draftTeamColumnView{Team: boardTeamProps(team.Team), Picks: tapePicksProps(team.Picks), Needs: team.Needs})
	}
	return out
}

// buildDraftHistoryView reads data["history"] (a league.DraftHistoryView,
// set by internal/league's draftData — see service.go) and converts it
// into the page-level draftHistoryView above. A fixture that never sets
// "history" (every non-league test fixture in this package) type-asserts
// to the zero value, so the pane still renders — empty, never a panic.
func buildDraftHistoryView(data map[string]any) draftHistoryView {
	history, _ := data["history"].(league.DraftHistoryView)
	complete := boolField(mapField(data, "draft"), "complete")
	onClock := mapField(data, "on_clock")
	hasOnClock := !complete
	nextLabel := ""
	if hasOnClock {
		if teams := len(history.Board.Columns); teams > 0 {
			pickNumber := intField(data, "pick_number")
			round := (pickNumber-1)/teams + 1
			slot := (pickNumber-1)%teams + 1
			nextLabel = fmt.Sprintf("%d.%02d", round, slot)
		}
	}
	return draftHistoryView{
		Rounds: tapeRoundsProps(history.Rounds, history), Board: boardViewProps(history.Board), Teams: teamColumnsProps(history.Teams),
		// Complete comes from data["draft"]["complete"] (the same signal
		// draftRoomStatus/DraftCommandBar already read), not history.Complete:
		// a fixture that never sets data["history"] (every non-league test
		// fixture in this package) would otherwise always see complete=false.
		Complete: complete, Latest: history.Latest, Since: -1,
		HasOnClock: hasOnClock, NextLabel: nextLabel,
		OnClockName: stringField(onClock, "name"), OnClockAbbr: stringField(onClock, "abbreviation"), OnClockTone: stringField(onClock, "tone"),
	}
}

// draftAvailableView backs the available-players pane: the pool list plus
// the make-pick and queue-add actions every eligible row's buttons post to.
// Actions also backs DraftPickBar's mobile ready/autopick prompt (V1): the
// pick bar and the available pane already share this one view.
type draftAvailableView struct {
	Data           map[string]any
	Players        []draftPlayerCardView
	CSRF           string
	MakePickAction string
	QueueAddAction string
	Actions        map[string]string
}

// draftQueueView backs the "my team" pane: the viewer's full personal
// queue (including already-taken entries, Task 5a's DraftMyTeam), the
// roster-needs tally, the queue-remove action a taken row's Clear button
// posts to, and (V1) the Room segment's own ready/autopick controls, which
// moved here off the command bar.
type draftQueueView struct {
	Data              map[string]any
	Queue             []draftPlayerCardView
	CSRF              string
	QueueRemoveAction string
	Actions           map[string]string
}

func draftBreakdownProps(raw []map[string]any) []draftBreakdownRowView {
	out := make([]draftBreakdownRowView, 0, len(raw))
	for _, row := range raw {
		out = append(out, draftBreakdownRowView{
			Scored: boolField(row, "scored"),
			Label:  stringField(row, "label"),
			Calc:   stringField(row, "calc"),
			Points: stringField(row, "points"),
		})
	}
	return out
}

func draftPlayerProps(raw []map[string]any) []draftPlayerCardView {
	out := make([]draftPlayerCardView, 0, len(raw))
	for _, player := range raw {
		breakdown, _ := player["breakdown"].([]map[string]any)
		out = append(out, draftPlayerCardView{
			ID:              stringField(player, "id"),
			Name:            stringField(player, "name"),
			Position:        stringField(player, "position"),
			NFLTeam:         stringField(player, "nfl_team"),
			Projection:      stringField(player, "projection"),
			Rank:            stringField(player, "rank"),
			Detail:          stringField(player, "detail"),
			Headshot:        stringField(player, "headshot"),
			HasHeadshot:     boolField(player, "has_headshot"),
			Jersey:          stringField(player, "jersey"),
			HasBreakdown:    boolField(player, "has_breakdown"),
			Breakdown:       draftBreakdownProps(breakdown),
			BreakdownTotal:  stringField(player, "breakdown_total"),
			HasHist:         boolField(player, "has_hist"),
			Hist:            stringField(player, "hist"),
			Search:          stringField(player, "search"),
			HasDraftCapital: boolField(player, "has_draft_capital"),
			DraftCapital:    stringField(player, "draft_capital"),
			HasOpponent:     boolField(player, "has_opponent"),
			Opponent:        stringField(player, "opponent"),
			HasMatchup:      boolField(player, "has_matchup"),
			MatchupTier:     stringField(player, "matchup_tier"),
			MatchupChip:     stringField(player, "matchup_chip"),
			MatchupDetail:   stringField(player, "matchup_detail"),
			CanDraft:        boolField(player, "draft_eligible"),
			Taken:           boolField(player, "taken"),
			HasValue:        boolField(player, "has_value"),
			ValueLabel:      stringField(player, "value_label"),
		})
	}
	return out
}

// draftTeamProps converts DraftData's map[string]any "teams" slice into
// typed DraftTeamCard values so the draft-room grid's {...team} spread
// into strict DraftTeam proves clean: the tier-2 spread boundary rejects a
// map[string]any source outright (it "cannot prove field coverage").
func draftTeamProps(raw []map[string]any) []DraftTeamCard {
	out := make([]DraftTeamCard, 0, len(raw))
	for _, team := range raw {
		out = append(out, DraftTeamCard{
			TeamID:         stringField(team, "id"),
			OnClock:        boolField(team, "on_clock"),
			Tone:           stringField(team, "tone"),
			HasAvatarImage: boolField(team, "has_avatar_image"),
			AvatarImageURL: stringField(team, "avatar_image_url"),
			Name:           stringField(team, "name"),
			Abbreviation:   stringField(team, "abbreviation"),
			Presence:       stringField(team, "presence"),
			PresenceLabel:  stringField(team, "presence_label"),
			PresenceDetail: stringField(team, "presence_detail"),
			OperatorCount:  intField(team, "operator_count"),
			Manager:        stringField(team, "manager"),
			Division:       stringField(team, "division"),
			Claimed:        boolField(team, "claimed"),
			Ready:          boolField(team, "ready"),
			Autopick:       boolField(team, "autopick"),
			BoardCount:     intField(team, "board_count"),
			BoardGap:       boolField(team, "board_gap"),
		})
	}
	return out
}

func draftSeatControlProps(raw []map[string]any) []DraftSeatControlCard {
	out := make([]DraftSeatControlCard, 0, len(raw))
	for _, team := range raw {
		// AUTO delegates a real manager's future turns. Open franchises have
		// neither an operator nor a Big Board owner, so presenting a control
		// for them would invent authority and a selection strategy.
		if !boolField(team, "claimed") {
			continue
		}
		out = append(out, DraftSeatControlCard{
			TeamID:         stringField(team, "id"),
			Name:           stringField(team, "name"),
			Manager:        stringField(team, "manager"),
			PresenceLabel:  stringField(team, "presence_label"),
			PresenceDetail: stringField(team, "presence_detail"),
			OnClock:        boolField(team, "on_clock"),
			Ready:          boolField(team, "ready"),
			Autopick:       boolField(team, "autopick"),
			BoardCount:     intField(team, "board_count"),
			BoardGap:       boolField(team, "board_gap"),
			Action:         draftActionPath("seat-autopick"),
			ReadyAction:    draftActionPath("seat-ready"),
		})
	}
	return out
}

// prepareDraftData never writes back into its data parameter: every
// derived field lands on viewData or output, two maps built fresh on each
// call. A caller that hands the same map to two requests in a row (the
// service always builds a fresh one per request, but a repeated-fixture
// test — TestCommandFragmentETagIgnoresTicksButChangesWithTheDeadline —
// deliberately does not) must keep reading the same raw teams/available/
// queue lists on the second call, not the first call's typed leftovers.
func prepareDraftData(data map[string]any) map[string]any {
	teams, _ := data["teams"].([]map[string]any)
	players, _ := data["available"].([]map[string]any)
	queueRaw, _ := data["queue"].([]map[string]any)
	typedTeams := draftTeamProps(teams)
	typedPlayers := draftPlayerProps(players)
	typedQueue := draftPlayerProps(queueRaw)

	// viewData is the one map every view's Data field shares (room,
	// workspace, command, history, available, queue below). It must never
	// itself gain one of those keys: draftRegionETag's json.Marshal of
	// semanticDraftRegionView's copy would otherwise walk a cycle back
	// through that key's own nested .Data. output, built after it, is the
	// top-level map prepareDraftData actually returns; nothing ETags
	// output itself, so it is free to carry them.
	viewData := make(map[string]any, len(data)+4)
	for key, value := range data {
		viewData[key] = value
	}
	viewData["teams"] = typedTeams
	viewData["seat_controls"] = draftSeatControlProps(teams)
	// has_adp gates the available pane's whole VS ADP column, header and
	// cell alike (R4): service.go's draftData sets it once the pool source
	// carries real ADP figures. The pane root's data-has-adp="false"
	// attribute (page.gsx) is the matching CSS hook (R4's five-track
	// .draft-available[data-has-adp="false"] .avail-row rule) so the
	// column's grid track drops with it, never an empty cell.
	viewData["has_adp"] = boolField(data, "has_adp")
	viewData["available_search_placeholder"] = fmt.Sprintf("Search %d available", intField(data, "available_count"))
	viewData["workspace_fragment_url"] = draftWorkspaceFragmentURL(data)

	actions := map[string]string{
		"draft_start": draftActionPath("draft-start"), "toggle_ready": draftActionPath("toggle-ready"),
		"toggle_autopick": draftActionPath("toggle-autopick"), "clock_pause": draftActionPath("clock-pause"),
		"clock_resume": draftActionPath("clock-resume"), "clock_extend": draftActionPath("clock-extend"),
		"clock_duration": draftActionPath("clock-set-duration"), "clock_autopick": draftActionPath("clock-force-autopick"),
		"seat_autopick": draftActionPath("seat-autopick"), "seat_ready": draftActionPath("seat-ready"),
	}
	room := draftRoomView{Data: viewData, Actions: actions}
	room.StatusSummary = draftRoomStatus(viewData)

	output := make(map[string]any, len(viewData)+8)
	for key, value := range viewData {
		output[key] = value
	}
	output["room"] = room
	output["workspace"] = draftWorkspaceView{
		Data: viewData, Players: typedPlayers, MakePickAction: draftActionPath("make-pick"),
	}
	// live_mode/shell_modifier drive the shell root's own attributes
	// (data-draft-live-mode, the --final class variant); "fallback" is the
	// only mode until Task 8 pins v0.53.10 and switches to target mode.
	output["live_mode"] = "fallback"
	output["shell_modifier"] = ""
	if complete, _ := mapField(data, "draft")["complete"].(bool); complete {
		output["shell_modifier"] = " draft-shell--final"
	}
	output["command"] = draftCommandView{Data: viewData, Actions: actions, StatusSummary: room.StatusSummary}
	// Since defaults to -1 ("unset"): draftRegionView (fragment.go) renders
	// the full DraftHistory pane until attachDraftFragmentSince overwrites
	// it with a "?since=" request's cursor and precomputed Rows.
	output["history"] = buildDraftHistoryView(data)
	output["available"] = draftAvailableView{
		Data: viewData, Players: typedPlayers,
		MakePickAction: draftActionPath("make-pick"), QueueAddAction: draftActionPath("queue-add"), Actions: actions,
	}
	output["queue"] = draftQueueView{
		Data: viewData, Queue: typedQueue, QueueRemoveAction: draftActionPath("queue-remove"), Actions: actions,
	}
	return output
}

func draftWorkspaceFragmentURL(data map[string]any) string {
	values := url.Values{}
	if pos := stringField(data, "pool_position"); pos != "" {
		values.Set("pos", pos)
	}
	if query := stringField(data, "pool_query"); query != "" {
		values.Set("q", query)
	}
	if page := intField(data, "pool_page"); page > 1 {
		values.Set("page", strconv.Itoa(page))
	}
	if encoded := values.Encode(); encoded != "" {
		return "/draft/fragment/workspace?" + encoded
	}
	return "/draft/fragment/workspace"
}

func draftRoomStatus(data map[string]any) string {
	if boolField(mapField(data, "draft"), "complete") {
		return fmt.Sprintf("Draft complete; %d picks locked; the clock is stopped.", intField(data, "pick_number"))
	}
	onClock := stringField(mapField(data, "on_clock"), "abbreviation")
	clockView := mapField(data, "clock")
	clockPhrase := "the clock is not running"
	if boolField(clockView, "paused") {
		clockPhrase = "clock paused"
	} else if boolField(clockView, "armed") {
		clockPhrase = "clock running"
	}
	return fmt.Sprintf("Pick %d; %s on the clock; %d of %d ready; %s.", intField(data, "pick_number"), onClock, intField(data, "ready_count"), intField(data, "manager_count"), clockPhrase)
}

func attachDraftRequestState(data map[string]any, request *http.Request) map[string]any {
	room, _ := data["room"].(draftRoomView)
	workspace, _ := data["workspace"].(draftWorkspaceView)
	command, _ := data["command"].(draftCommandView)
	available, _ := data["available"].(draftAvailableView)
	queue, _ := data["queue"].(draftQueueView)
	token := session.Token(request)
	room.CSRF = token
	workspace.CSRF = token
	command.CSRF = token
	available.CSRF = token
	queue.CSRF = token
	seats, _ := room.Data["seat_controls"].([]DraftSeatControlCard)
	for i := range seats {
		seats[i].CSRF = token
	}
	room.Data["seat_controls"] = seats
	workspace.Data["seat_controls"] = seats
	data["room"] = room
	data["workspace"] = workspace
	data["command"] = command
	data["available"] = available
	data["queue"] = queue
	return data
}

func init() {
	if err := route.RegisterFileModuleHere(route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			ctx.NoStore()
			ctx.Runtime().EnableBootstrap()
			ctx.Runtime().BindHub(draftLiveHubName, draftLiveBindingPath(), nil)
			// The initial page view is an attendance claim. The body heartbeat
			// keeps presence current while hub events own draft-state convergence.
			league.Default().RecordPresence(ctx.Request, time.Now())
			data := attachDraftRequestState(prepareDraftData(league.Default().DraftData(ctx.Request)), ctx.Request)
			data["has_notice"] = false
			data["notice"] = ""
			if store := session.Current(ctx.Request); store != nil {
				if flashes := store.Flashes("notice"); len(flashes) > 0 {
					data["has_notice"] = true
					data["notice"] = fmt.Sprint(flashes[0])
				}
			}
			data["has_pick_error"] = false
			data["pick_error"] = ""
			data["force_current_pick_confirm"] = ""
			// Keep the submitted optimistic token in the validation render.
			// Recomputing a fresh token here would turn a stale form into a
			// newly-authorized form if another browser changed the draft while
			// the first submission was being corrected.
			for _, name := range []string{"clock-force-autopick", "clock-extend"} {
				if view, ok := ctx.ActionState(name); ok {
					if view.Error("player_id") == "" {
						continue
					}
					if name == "clock-force-autopick" {
						data["force_current_pick_confirm"] = view.Value("confirm")
					}
					if submitted := strings.TrimSpace(view.Value("current_pick_token")); submitted != "" {
						data["current_pick_token"] = submitted
						if clock, ok := data["clock"].(map[string]any); ok {
							clock["current_pick_token"] = submitted
							clock["action_token"] = submitted
						}
					}
				}
			}
			for _, name := range []string{"make-pick", "draft-start", "clock-force-autopick", "clock-extend"} {
				if view, ok := ctx.ActionState(name); ok {
					if message := view.Error("player_id"); message != "" {
						data["has_pick_error"] = true
						data["pick_error"] = message
					}
				}
			}
			return data, nil
		},
		Metadata: func(ctx *route.RouteContext, page route.FilePage, data any) (server.Metadata, error) {
			return server.Metadata{
				Title:       server.Title{Default: league.PageTitle("Draft Room")},
				Description: "The live " + league.SeatCountWord() + "-team snake draft room.",
			}, nil
		},
		Actions: route.FileActions{
			"draft-start": func(ctx *action.Context) error {
				if strings.TrimSpace(ctx.FormData["confirm"]) != "START" {
					message := "type START to confirm"
					return action.Validation(message, map[string]string{"player_id": message}, ctx.FormData)
				}
				started, err := league.Default().AdminStartDraft(ctx.Request)
				if err != nil {
					return actionui.Validation(ctx, "draft", "player_id", err)
				}
				message := "Draft was already live; the original clock is unchanged."
				if started {
					message = "Draft started. Pick one is on the clock."
				}
				return draftActionSuccess(ctx, "/draft", message)
			},
			// The commissioner clock controls render on THIS page (the
			// clock panel in page.gsx) and actionPath resolves against the
			// draft module, so the five clock actions must be registered
			// here as well as on /admin — the service methods carry the
			// requireCommissioner gate, so this is routing, not authority.
			// Before this registration the draft-room clock forms posted
			// into a 404 (found during the gosx v0.46 adoption pass).
			"clock-pause": func(ctx *action.Context) error {
				if err := league.Default().AdminPauseClock(ctx.Request); err != nil {
					return actionui.Validation(ctx, "draft", "player_id", err)
				}
				return draftActionSuccess(ctx, "/draft", "Pick clock paused.")
			},
			"clock-resume": func(ctx *action.Context) error {
				if err := league.Default().AdminResumeClock(ctx.Request); err != nil {
					return actionui.Validation(ctx, "draft", "player_id", err)
				}
				return draftActionSuccess(ctx, "/draft", "Pick clock resumed.")
			},
			"clock-force-autopick": func(ctx *action.Context) error {
				pick, player, team, err := league.Default().AdminForceAutopick(ctx.Request, ctx.FormData["confirm"], ctx.FormData["current_pick_token"])
				if err != nil {
					return actionui.Validation(ctx, "draft", "player_id", err)
				}
				return draftActionSuccess(ctx, "/draft", fmt.Sprintf("Pick %d: %s auto-selects %s.", pick.Number, team.Name, player.Name))
			},
			"clock-extend": func(ctx *action.Context) error {
				secs, err := strconv.Atoi(strings.TrimSpace(ctx.FormData["seconds"]))
				if err != nil {
					message := "enter seconds as a whole number"
					return action.Validation(message, map[string]string{"player_id": message}, ctx.FormData)
				}
				if err := league.Default().AdminExtendClock(ctx.Request, secs, ctx.FormData["current_pick_token"]); err != nil {
					return actionui.Validation(ctx, "draft", "player_id", err)
				}
				return draftActionSuccess(ctx, "/draft", fmt.Sprintf("Clock extended by %d seconds.", secs))
			},
			"clock-set-duration": func(ctx *action.Context) error {
				secs, err := strconv.Atoi(strings.TrimSpace(ctx.FormData["seconds"]))
				if err != nil {
					message := "enter seconds as a whole number"
					return action.Validation(message, map[string]string{"player_id": message}, ctx.FormData)
				}
				if err := league.Default().AdminSetClockSeconds(ctx.Request, secs); err != nil {
					return actionui.Validation(ctx, "draft", "player_id", err)
				}
				return draftActionSuccess(ctx, "/draft", fmt.Sprintf("Pick clock set to %d seconds.", secs))
			},
			"toggle-ready": func(ctx *action.Context) error {
				ready, teamName, err := league.Default().ToggleReady(ctx.Request, ctx.FormData["team_id"])
				if err != nil {
					return action.Error(http.StatusUnauthorized, err.Error())
				}
				status := "checked out"
				if ready {
					status = "locked in"
				}
				return draftActionSuccess(ctx, "/draft", fmt.Sprintf("%s is %s for draft night.", teamName, status))
			},
			"make-pick": func(ctx *action.Context) error {
				pick, player, team, err := league.Default().MakePick(ctx.Request, ctx.FormData["team_id"], ctx.FormData["player_id"])
				if err != nil {
					return actionui.Validation(ctx, "draft", "player_id", err)
				}
				return draftActionSuccess(ctx, draftRedirectTarget(ctx.FormData["pos"], ctx.FormData["q"], ctx.FormData["page"]), fmt.Sprintf("Pick %d: %s selects %s.", pick.Number, team.Name, player.Name))
			},
			// queue-add/queue-remove back the shell's own Big Board controls
			// (the available pane's "+ Queue" button and the my-team pane's
			// "Clear" button on a taken row): BoardAdd/BoardRemove already
			// carry the ownership and validation rules, so these are routing
			// only, the same shape as make-pick above.
			"queue-add": func(ctx *action.Context) error {
				player, err := league.Default().BoardAdd(ctx.Request, ctx.FormData["player_id"])
				if err != nil {
					return actionui.Validation(ctx, "draft", "player_id", err)
				}
				target := draftRedirectTarget(ctx.FormData["pos"], ctx.FormData["q"], ctx.FormData["page"])
				return draftActionSuccess(ctx, target, fmt.Sprintf("%s added to your queue.", player.Name))
			},
			"queue-remove": func(ctx *action.Context) error {
				if err := league.Default().BoardRemove(ctx.Request, ctx.FormData["player_id"]); err != nil {
					return actionui.Validation(ctx, "draft", "player_id", err)
				}
				target := draftRedirectTarget(ctx.FormData["pos"], ctx.FormData["q"], ctx.FormData["page"])
				return draftActionSuccess(ctx, target, "Removed from your queue.")
			},
			"toggle-autopick": func(ctx *action.Context) error {
				on, teamName, err := league.Default().ToggleAutopick(ctx.Request, ctx.FormData["team_id"])
				if err != nil {
					return action.Error(http.StatusUnauthorized, err.Error())
				}
				status := "off"
				if on {
					status = "on"
				}
				return draftActionSuccess(ctx, "/draft", fmt.Sprintf("Autopick is %s for %s.", status, teamName))
			},
			"seat-autopick": func(ctx *action.Context) error {
				onRaw := ctx.FormData["on"]
				on, err := parseSeatAutopick(onRaw)
				if err != nil {
					return actionui.Validation(ctx, "draft", "on", err)
				}
				teamID := strings.TrimSpace(ctx.FormData["team_id"])
				if err := league.Default().AdminSetAutopick(ctx.Request, teamID, on); err != nil {
					return action.Error(http.StatusUnauthorized, err.Error())
				}
				status := "manual control restored"
				if on {
					status = "AUTO mode enabled"
				}
				return draftActionSuccess(ctx, "/draft", fmt.Sprintf("%s for %s.", status, league.Default().TeamLabel(teamID)))
			},
			// seat-ready sets a claimed seat's Ready flag on the commissioner's
			// own authority (compare toggle-ready, the manager's own path).
			// It is restricted to claimed seats, matching seat-autopick: an
			// unclaimed seat has no manager whose readiness this could assert.
			"seat-ready": func(ctx *action.Context) error {
				onRaw := ctx.FormData["on"]
				on, err := parseSeatReady(onRaw)
				if err != nil {
					return actionui.Validation(ctx, "draft", "on", err)
				}
				teamID := strings.TrimSpace(ctx.FormData["team_id"])
				if err := league.Default().AdminSetReady(ctx.Request, teamID, on); err != nil {
					return action.Error(http.StatusUnauthorized, err.Error())
				}
				status := "checked out"
				if on {
					status = "locked in"
				}
				return draftActionSuccess(ctx, "/draft", fmt.Sprintf("%s is %s for draft night.", league.Default().TeamLabel(teamID), status))
			},
		},
	}); err != nil {
		log.Fatal(err)
	}
}
