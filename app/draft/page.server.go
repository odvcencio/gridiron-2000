package draft

import (
	"fmt"
	"gridiron-2000/internal/actionui"
	"log"
	"net/http"
	"net/url"
	"os"
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
	Rounds   []draftTapeRoundView
	Board    draftBoardView
	Teams    []draftTeamColumnView
	Complete bool
	Latest   int
	Since    int
	// View/ShowTape/ShowBoard/ShowTeams: item 1a (2026-08-30 review). View
	// is one of "tape"/"board"/"teams" (attachDraftFragmentView,
	// fragment.go, normalizes any "?view=" to one of these, defaulting to
	// tape); the three ShowX bools are what DraftHistory's own template
	// actually branches on (page.gsx renders only the selected sub-view's
	// markup, never all three at once — the tape sub-view alone eagerly
	// carried far more bytes than the D3 refresh-budget's 4 KB ceiling
	// allows once all three views rendered together on every poll).
	View        string
	ShowTape    bool
	ShowBoard   bool
	ShowTeams   bool
	HasOnClock  bool
	NextLabel   string
	OnClockName string
	OnClockAbbr string
	OnClockTone string
	// OnClockHasAvatarImage/OnClockAvatarImageURL: see page.gsx's
	// DraftHistoryProps doc comment (P10, 2026-08-30 review).
	OnClockHasAvatarImage bool
	OnClockAvatarImageURL string
	// RoundsEmpty: see page.gsx's DraftHistoryProps doc comment. len()
	// never resolves correctly in a .gsx expression through
	// route.RenderProgramComponent (any shape — direct or nested, slice
	// or string, 2026-08-30 review), so every length this page needs
	// inside a template is computed here in Go and passed down as a
	// plain field instead.
	RoundsEmpty bool
	// HasOlderRounds/OlderHref: item 3 (2026-08-30 review), the "Older
	// rounds ↓" link at the tape's foot. HasOlderRounds is true only on a
	// full render (Since < 0) whose UNCAPPED round count exceeds
	// draftTapeMaxRenderedRounds and whose request did not already carry
	// "?rounds=all" — attachDraftFragmentView (fragment.go) computes both
	// alongside the cap itself. OlderHref always targets "?rounds=all",
	// carrying the viewer's own pool q/pos/page (item 6).
	HasOlderRounds bool
	OlderHref      string
	// TapeURL/TargetMode (findings 1/2/3/6, 2026-08-30 review): TapeURL is
	// target mode's OWN static URL for its single nested tape-rows region
	// (attachDraftFragmentView, fragment.go) — a distinct string from
	// history_tape_url (which still names the full "tape" region:
	// Tape/Board/Teams plus the pane shell, fallback mode's own outer
	// region). TargetMode selects target-mode's nested live-root/
	// single-replace-region structure versus fallback's plain
	// full-replace region (DRAFT_LIVE_MODE=fallback).
	TapeURL    string
	TargetMode bool
	// detail resolves one pick's full accordion content on demand (item 1,
	// 2026-08-30 review): attachDraftFragmentPick (fragment.go) calls this
	// exactly once, for the single pick number a "?pick=" query names, and
	// only when that pick's row is actually present in Rounds — never
	// eagerly for every row (internal/league's DraftHistoryView.Detail is
	// already a cheap on-demand closure, not a precomputed map; see its own
	// doc comment). Unexported: fragment.go reads it directly since both
	// files share this package.
	detail func(number int) league.PickDetail
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
	// Open/Href: item 1 (2026-08-30 review). Href is this row's own
	// soft-navigation target — "/draft?view=tape&pick=N&..." to open it,
	// or (once Open) the same URL with "pick" dropped, to close it — built
	// server-side (tapeRoundsProps/attachDraftFragmentPick, carrying the
	// viewer's own pool q/pos/page, item 6) rather than a client-side
	// write: gosx@v0.53.9's capture-phase click handler cancels a
	// <summary>'s native toggle under any data-gosx-set ancestor, so the
	// prior <details><summary data-gosx-set> row never actually opened.
	// Open is true for exactly the one pick "?pick=" named, when that
	// pick's row survived the tape's own newest-3-round cap.
	Open bool
	Href string
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
	// ShowHeader/MadeBindKey/CurrentBindAttr (review item 2/3, 2026-08-30):
	// a "?since=" response renders a round's header only the FIRST time
	// that round appears at all (since < round.First, filterTapeRoundsSince,
	// fragment.go) — every later poll within the SAME round omits it, since
	// gosx's region-key dedupe (data-tape-key) would drop a re-sent header
	// anyway, and a dropped node's own fresh "N of M made" text never
	// reaches the DOM. MadeBindKey ("round.<r>.made") and CurrentBindAttr
	// ("data-current:round.<r>.current") keep an EMIT-ONCE header's own
	// made-count and current-round flag live instead — internal/league's
	// draft_events.go emits both under "round.<r>.*" on every draft:pick/
	// draft:undo/draft:state. ShowHeader is always true on a full render
	// (Since < 0, tapeRoundsProps' own default); filterTapeRoundsSince
	// overrides it per round for a "?since=" response.
	ShowHeader      bool
	MadeBindKey     string
	CurrentBindAttr string
}

type draftBoardCellView struct {
	Round, Column, Number  int
	Label                  string
	Filled, Mine, OnClock  bool
	PlayerName, Position   string
	NFLTeam                string
	IsAuto, IsCommissioner bool
	// CellBindKey/PosBindAttr (Task 8, target mode): pre-formatted
	// data-gosx-live-bind/-bind-attr values ("cell.<round>.<column>",
	// "data-pos:cellpos.<round>.<column>") — computed here, in Go, rather
	// than concatenated from Round/Column ints inside the .gsx template,
	// matching this package's established rule against relying on
	// unproven GSX template-side expression shapes (ColumnCount's own
	// doc comment, DraftHistoryProps.RoundsEmpty's own doc comment).
	CellBindKey string
	PosBindAttr string
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

// tapeRowManager drops manager when it only restates team — "Big Endians
// Manager" managing "Big Endians" read as one run-on, duplicated name
// (P3-22, UI pass 2026-08-30) rather than as two distinct facts. A real
// manager's own display name only coincides with their team name by
// exact match or literal "<team> ..." prefix in a demo/rehearsal
// league; either way the team name alone already carries that fact.
func tapeRowManager(team, manager string) string {
	manager = strings.TrimSpace(manager)
	team = strings.TrimSpace(team)
	if team == "" || manager == "" {
		return manager
	}
	if strings.EqualFold(manager, team) {
		return ""
	}
	// strings.HasPrefix compares whole, always-valid UTF-8 strings, never a
	// byte-index slice of manager — team's own byte length (len(team)) does
	// not line up with manager's rune boundaries when either name carries a
	// multi-byte character, so a byte slice there could split mid-rune.
	if strings.HasPrefix(strings.ToLower(manager), strings.ToLower(team)+" ") {
		return ""
	}
	return manager
}

// tapePickProps converts one league.TapePick into its page-level mirror:
// field-for-field, the same conversion boundary draftTeamProps/
// draftPlayerProps already use for teams/players.
func tapePickProps(pick league.TapePick) draftTapePickView {
	return draftTapePickView{
		Number: pick.Number, Round: pick.Round, Slot: pick.Slot, Column: pick.Column,
		Label:    pick.Label,
		TeamID:   pick.TeamID,
		TeamName: pick.TeamName, TeamAbbr: pick.TeamAbbr, TeamTone: pick.TeamTone, Manager: tapeRowManager(pick.TeamName, pick.Manager),
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

// draftHistoryHref builds one "/draft?view=..." navigation target,
// carrying the viewer's own pool q/pos/page (item 6, 2026-08-30 review)
// plus any extra key/value pairs one specific link needs ("pick", the
// open tape row; "rounds", the "Older rounds ↓" link). Every Tape/Board/
// Teams segment link (DraftHistoryHead), the phone Picks/Teams tabs
// (DraftMobileTabs), a tape row's own open/close link, and the older-
// rounds link all resolve through this one function, so the pool state
// — and, for a row's own link, the open pick — travels with every one of
// them, never silently dropped by a link built ad hoc.
func draftHistoryHref(view, pos, query string, page int, extra map[string]string) string {
	values := url.Values{}
	values.Set(draftHistoryViewQueryKey, view)
	if pos != "" {
		values.Set("pos", pos)
	}
	if query != "" {
		values.Set("q", query)
	}
	if page > 1 {
		values.Set("page", strconv.Itoa(page))
	}
	for key, value := range extra {
		if value != "" {
			values.Set(key, value)
		}
	}
	return "/draft?" + values.Encode()
}

// tapeRoundsProps converts league's newest-first rounds into their
// page-level mirror, with each pick's own Href set to its open target
// (item 1/6, 2026-08-30 review). Detail hydration (Projection, Source,
// BestAvailable, TeamPicks) is NOT eager here — attachDraftFragmentPick
// (fragment.go) hydrates the single "?pick="-named row after the fact —
// so every row's own struct build stays O(1), not one Detail lookup per
// row on every render regardless of whether a viewer ever opens it.
func tapeRoundsProps(rounds []league.TapeRound, pos, query string, page int) []draftTapeRoundView {
	out := make([]draftTapeRoundView, 0, len(rounds))
	for _, round := range rounds {
		picks := make([]draftTapePickView, 0, len(round.Picks))
		for _, pick := range round.Picks {
			card := tapePickProps(pick)
			card.Href = draftHistoryHref(draftHistoryViewTape, pos, query, page, map[string]string{draftHistoryPickKey: strconv.Itoa(pick.Number)})
			picks = append(picks, card)
		}
		roundKey := strconv.Itoa(round.Round)
		out = append(out, draftTapeRoundView{
			Round: round.Round, First: round.First, Last: round.Last, Direction: round.Direction,
			Current: round.Current, Made: round.Made, Total: round.Total,
			Picks: picks,
			// ShowHeader defaults true here (every full render shows every
			// round's own header); filterTapeRoundsSince (fragment.go)
			// overrides it per round for a "?since=" response.
			ShowHeader:      true,
			MadeBindKey:     "round." + roundKey + ".made",
			CurrentBindAttr: "data-current:round." + roundKey + ".current",
		})
	}
	return out
}

// draftTapeMaxRenderedRounds is item 1's own final lever (2026-08-30
// review), the fallback the plan pre-approves once item 1a
// (single-view-per-response) and 1b (lazy per-pick detail) alone still
// left the tape sub-view over the D3 refresh budget's 4 KB gzip ceiling
// at a full 120-pick draft (measured ~5 KB, item 1a+1b alone — the pick
// number's own repetition across data-tape-key/data-pick-number/the slot
// label/#N/the two lazy-region attributes is genuine per-row entropy
// gzip cannot compress away, unlike the surrounding boilerplate it
// already collapses to a handful of bytes per repeat). Capping the FULL
// pane render (Since < 0) to the newest 3 rounds keeps the live tail's
// own "?since=" catch-up unaffected (it only ever asks for picks newer
// than an already-seen cursor, always inside this window in practice),
// while a viewer wanting the complete historical record still has the
// Board/Teams tabs (unaffected: neither reads Rounds) and, once the
// draft finishes, the CSV export.
const draftTapeMaxRenderedRounds = 3

// capTapeRounds keeps at most the newest draftTapeMaxRenderedRounds
// entries of rounds (already newest-first, league.DraftHistory's own
// ordering) — a no-op once a draft holds three rounds or fewer.
//
// Item 2 (2026-08-30 review): the caller (attachDraftFragmentView,
// fragment.go) applies this ONLY on a full render (history.Since < 0),
// and only after attachDraftFragmentSince (which runs first in
// draftFragmentHandler) has already filtered Rounds down to picks above
// its own "?since=" cursor. Capping first and filtering second — the
// pre-fix order, when this ran unconditionally inside
// buildDraftHistoryView before Since was even known — silently dropped
// any pick that fell outside the newest-3-round window: "?since=39" at
// 60 picks lost pick 40, because pick 40's round had already been
// sliced away by the cap before the since-filter ever saw it.
func capTapeRounds(rounds []draftTapeRoundView) []draftTapeRoundView {
	if len(rounds) <= draftTapeMaxRenderedRounds {
		return rounds
	}
	return rounds[:draftTapeMaxRenderedRounds]
}

func boardCellProps(cell league.BoardCell) draftBoardCellView {
	round, column := strconv.Itoa(cell.Round), strconv.Itoa(cell.Column)
	return draftBoardCellView{
		Round: cell.Round, Column: cell.Column, Number: cell.Number, Label: cell.Label,
		Filled: cell.Filled, Mine: cell.Mine, OnClock: cell.OnClock,
		PlayerName: cell.PlayerName, Position: cell.Position, NFLTeam: cell.NFLTeam,
		IsAuto: cell.IsAuto, IsCommissioner: cell.IsCommissioner,
		CellBindKey: "cell." + round + "." + column,
		PosBindAttr: "data-pos:cellpos." + round + "." + column,
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
// draftLiveMode reads DRAFT_LIVE_MODE (review item 8, 2026-08-30): "target"
// (the default, and anything else) keeps gosx@v0.53.10's fetchless
// data-gosx-live-* binds; "fallback" (case-insensitive) restores the pre-
// Task-8 data-gosx-region*-driven refetch-and-swap wiring in the exact
// same page.gsx, gated by <If cond={data.live_mode == "target"}> pairs. A
// plain process env var, the same simplicity PICK_CLOCK/GOSX_APP_ROOT
// already use (sim_child_test.go) — no additional local-env gate, since
// this selects a rendering strategy, not a privileged or destructive
// action.
func draftLiveMode() string {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("DRAFT_LIVE_MODE")), "fallback") {
		return "fallback"
	}
	return "target"
}

func buildDraftHistoryView(data map[string]any, liveMode string) draftHistoryView {
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
	pos := stringField(data, "pool_position")
	query := stringField(data, "pool_query")
	page := intField(data, "pool_page")
	return draftHistoryView{
		// Rounds is UNCAPPED here (item 2, 2026-08-30 review):
		// attachDraftFragmentView (fragment.go) applies capTapeRounds
		// itself, once it knows whether this is a full render (Since < 0)
		// or a "?since=" poll — see capTapeRounds' own doc comment for why
		// the old unconditional cap here lost picks on a since-poll.
		Rounds: tapeRoundsProps(history.Rounds, pos, query, page), Board: boardViewProps(history.Board), Teams: teamColumnsProps(history.Teams),
		// Complete comes from data["draft"]["complete"] (the same signal
		// draftRoomStatus/DraftCommandBar already read), not history.Complete:
		// a fixture that never sets data["history"] (every non-league test
		// fixture in this package) would otherwise always see complete=false.
		Complete: complete, Latest: history.Latest, Since: -1,
		// View defaults to tape here (the initial full-page render,
		// Page()'s own inline <DraftHistory>, and every non-fragment
		// caller): fragment.go's attachDraftFragmentView overwrites this
		// per-request from "?view=" for the tape pane's own region
		// fetches. Defaulting the SSR render to tape too, rather than
		// rendering all three sub-views inline, keeps the very first
		// paint consistent with every refresh afterward — a viewer who
		// has never touched the segment sees the same single-view shape
		// before and after the first draft:pick.
		View: draftHistoryViewTape, ShowTape: true, ShowBoard: false, ShowTeams: false,
		HasOnClock: hasOnClock, NextLabel: nextLabel,
		OnClockName: stringField(onClock, "name"), OnClockAbbr: stringField(onClock, "abbreviation"), OnClockTone: stringField(onClock, "tone"),
		OnClockHasAvatarImage: boolField(onClock, "has_avatar_image"), OnClockAvatarImageURL: stringField(onClock, "avatar_image_url"),
		RoundsEmpty: len(history.Rounds) == 0,
		TargetMode:  liveMode == "target",
		detail:      history.Detail,
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
	// yourpick_bind_key (Task 8, target mode): the command bar's "your pick
	// in N" text binds this whole key, a pre-formatted "yourpick.<team>.
	// label" string (never a Go int concatenated inside the .gsx template
	// itself — this file's established rule, ColumnCount's own doc
	// comment). draftLiveTail (draft_events.go) already emits a full
	// "your pick in N" / "no more picks" label under this exact per-team
	// key on every draft:pick/undo/state event, so the bound span's whole
	// text tracks it with no further field needed. Meaningless (and
	// unread: the command bar only renders the bound span while
	// viewer.has_seat) for an unseated viewer, so an empty team id is
	// safe here.
	viewData["yourpick_bind_key"] = "yourpick." + stringField(mapField(data, "viewer"), "team_id") + ".label"

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
	// (data-draft-live-mode, the --final class variant). Task 8 pins
	// gosx@v0.53.10 and switches the room to "target" by default: the
	// command, available, and my-team panes apply hub payloads through
	// data-gosx-live-* binds with no fetch; the tape pane's own single
	// nested region (findings 1/2/3/6, 2026-08-30 review) does a plain
	// replace fetch on every draft:pick/draft:undo/draft:state instead of
	// a whole-pane refetch or a growing prepend. DRAFT_LIVE_MODE=fallback
	// (review item 8,
	// draftLiveMode below) restores "fallback" (data-gosx-region*
	// refetch-and-swap on every draft:pick/undo/state) — every fragment
	// endpoint and region attribute Task 6/7 built stays fully wired in
	// this same page.gsx for that env, gated by <If cond={data.live_mode
	// == "target"}> pairs rather than a second template.
	liveMode := draftLiveMode()
	output["live_mode"] = liveMode
	output["shell_modifier"] = ""
	if complete, _ := mapField(data, "draft")["complete"].(bool); complete {
		output["shell_modifier"] = " draft-shell--final"
	}
	output["command"] = draftCommandView{Data: viewData, Actions: actions, StatusSummary: room.StatusSummary}
	// Since defaults to -1 ("unset"): draftRegionView (fragment.go) renders
	// the full DraftHistory pane until attachDraftFragmentSince overwrites
	// it with a "?since=" request's cursor and precomputed Rows.
	output["history"] = buildDraftHistoryView(data, liveMode)
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
			data := attachDraftRequestState(attachDraftFragmentPick(attachDraftFragmentView(prepareDraftData(league.Default().DraftData(ctx.Request)), ctx.Request), ctx.Request), ctx.Request)
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
