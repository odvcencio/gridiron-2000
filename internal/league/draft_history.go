package league

import (
	"fmt"
	"math"
	"net/http"
	"sort"
	"time"
)

// TapePick is one recorded pick's full tape row: identity (who picked,
// what/when), provenance, and (for a made pick) its value against ADP. It
// backs the tape (TapeRound.Picks), the ledger/CSV (DraftHistoryView.Picks),
// the by-team columns (TeamColumn.Picks), and — embedded — the expanded
// pick-detail accordion (PickDetail).
type TapePick struct {
	Number, Round, Slot, Column                   int
	Label                                         string // "3.04" = round.slot
	TeamID, TeamName, TeamAbbr, TeamTone, Manager string
	HasAvatarImage                                bool
	AvatarImageURL                                string
	PlayerID, PlayerName, Position, NFLTeam       string
	MadeBy                                        string
	IsAuto, IsCommissioner, Mine                  bool
	TimeToPickSec                                 int
	TimeToPick                                    string // "0:49"
	HasValue                                      bool
	Value                                         int
	ValueLabel                                    string // "+8", "−4"
	MadeAt                                        string // RFC 3339 UTC
}

// TapeRound groups every pick made in one round, newest pick first. First
// and Last are the round's fixed pick-number bounds regardless of how many
// picks have actually landed.
type TapeRound struct {
	Round, First, Last int
	Direction          string // "→" or "←"
	Current            bool
	Made, Total        int
	Picks              []TapePick // newest first
}

// BoardCell is one round x column slot of the pick board. Column is the
// team's fixed position in draft order, never the pick's slot within its
// round (those two coincide only in odd rounds).
type BoardCell struct {
	Round, Column, Number int
	Label                 string
	Filled, Mine, OnClock bool
	PlayerName, Position  string
	// NFLTeam backs the mockup's board-cell copy ("1.01 · WR · CIN",
	// PickBoard.dc.html — a mockup divergence closed 2026-08-30 review):
	// empty on an unfilled cell.
	NFLTeam                string
	IsAuto, IsCommissioner bool
}

// BoardRow is one round's full column strip, one BoardCell per team.
type BoardRow struct {
	Round     int
	Direction string
	Cells     []BoardCell // one per team column, draft order
}

// BoardView is the whole pick board: one column header per team (in draft
// order, each carrying "mine") and one BoardRow per round.
type BoardView struct {
	Columns []map[string]any // teamMap in draft order plus "mine"
	Rows    []BoardRow
}

// TeamColumn is one team's full pick history plus its roster-needs tally,
// backing the "By team" tab.
type TeamColumn struct {
	Team  map[string]any
	Picks []TapePick
	Needs []map[string]any // label, filled, total, open
}

// BestAvailablePick is the compact best-available-at-this-pick entry:
// identity only, the fields PickDetail's accordion actually renders.
type BestAvailablePick struct {
	ID, Name, Position, NFLTeam string
}

// PickDetail is one pick's expanded accordion content: the tape row plus
// its projection, provenance (queue slot or best-available), the board's
// best-available snapshot at that pick, and the drafting team's picks so
// far.
type PickDetail struct {
	TapePick
	Projection    string
	Source        string // "queue #1" or "best available"
	BestAvailable []BestAvailablePick
	TeamPicks     []TapePick
}

// DraftHistoryView is the whole pick-history view: the tape (newest round
// first), the board (round-ascending), the by-team columns, the ascending
// ledger (CSV export), and per-pick detail lookup.
type DraftHistoryView struct {
	Rounds   []TapeRound
	Board    BoardView
	Teams    []TeamColumn
	Picks    []TapePick // ascending: the ledger and the CSV
	Complete bool
	Latest   int
	// detail computes one pick's expanded accordion on demand (P1 perf fix,
	// 2026-08-30): a closure over this render's own retained state and pool,
	// not a map precomputed for every pick up front. hydratedTapePicksProps
	// (page.server.go) is the one caller that invokes it, once per row the
	// tape actually renders — the command/available/queue fragments never
	// call Detail at all, and draftData already skips building history for
	// them (DraftDataOptions.IncludeHistory).
	detail func(number int) PickDetail
}

// Detail returns number's expanded pick detail, or the zero value when no
// such pick has been made (or this view was built with no detail closure —
// the zero-value DraftHistoryView every non-league test fixture starts
// from).
func (v DraftHistoryView) Detail(number int) PickDetail {
	if v.detail == nil {
		return PickDetail{}
	}
	return v.detail(number)
}

// boardCellNumber resolves the pick number that lands in round/column under
// a snake draft with teamCount active teams: column is the team's fixed
// draft-order position (1-based), never the pick's slot within the round.
func boardCellNumber(round, column, teamCount int) int {
	if round%2 != 0 {
		return (round-1)*teamCount + column
	}
	return round*teamCount - column + 1
}

// draftedByPlayerID returns every made pick's DraftPick, keyed by player
// ID — the same pickByPlayer shape rosterForTeam (service.go) already
// builds ad hoc for its own "Rd %d · Pick %d" Status string, exposed once
// here (wave 7, item 2) so any other caller in this package — playerMap's
// drafted parameter (the /players owner chip, service.go), a /team
// roster chip, or a future one — can resolve "which pick landed this
// player" without repeating the O(picks) build. state is a PersistedState
// snapshot; a state with no picks yet returns an empty, non-nil map.
func draftedByPlayerID(state PersistedState) map[string]DraftPick {
	out := make(map[string]DraftPick, len(state.Picks))
	for _, pick := range state.Picks {
		out[pick.PlayerID] = pick
	}
	return out
}

// DraftHistory builds every history view from one persisted state. viewer
// is the viewer's team ID, "" when seatless.
func (s *Service) DraftHistory(state PersistedState, viewer string) DraftHistoryView {
	pool := s.pool()
	order := state.DraftOrder
	if len(order) == 0 {
		order = defaultTeamIDs()
	}
	teamCount := len(order)
	rounds := CurrentDraftRounds()
	complete := draftComplete(state)
	next := len(state.Picks) + 1
	currentRound := pickRound(teamCount, next)
	viewerColumn := 0
	if viewer != "" {
		viewerColumn = pickColumn(order, viewer)
	}

	valueEligible := pool.label != "offline"
	pickedUpTo := map[string]int{} // playerID -> pick number that took it

	// teamPicksAsc/teamPickPos back Detail's TeamPicks lookup in O(1): built
	// once in this same O(P) pass rather than an O(P) rescan of tapePicks
	// per pick (the P1 perf fix, 2026-08-30 review — the old per-pick scan
	// made the whole view O(P^2), ~8ms at 120 picks). A team's own picks
	// land in teamPicksAsc[teamID] in ascending Number order (picks are
	// numbered 1..P in draft order already), so teamPickPos[number] — that
	// pick's 1-based position within its own team's slice — turns "this
	// team's picks up to and including this one" into a slice, no copy.
	teamPicksAsc := map[string][]TapePick{}
	teamPickPos := map[int]int{}
	pickByNumber := make(map[int]TapePick, len(state.Picks))

	tapePicks := make([]TapePick, len(state.Picks))
	for index, pick := range state.Picks {
		player := pool.byID[pick.PlayerID]
		team := s.teamView(state, pick.TeamID)
		teamMapView := s.teamMap(team)
		madeBy := pick.MadeBy
		if madeBy == "" {
			madeBy = "manager"
		}
		label := pickLabel(pick.Number, teamCount)
		timeToPickSec := timeToPickSeconds(state, index)
		hasValue := false
		value := 0
		valueLabel := ""
		if valueEligible && player.ADP > 0 {
			hasValue = true
			value = pick.Number - int(math.Round(player.ADP))
			valueLabel = valueVsADPLabel(pick.Number, player.ADP)
		}
		tapePicks[index] = TapePick{
			Number: pick.Number, Round: pick.Round, Slot: pickSlot(teamCount, pick.Number), Column: pickColumn(order, pick.TeamID),
			Label:    label,
			TeamID:   pick.TeamID,
			TeamName: stringAny(teamMapView, "name"), TeamAbbr: stringAny(teamMapView, "abbreviation"), TeamTone: stringAny(teamMapView, "tone"),
			Manager:        stringAny(teamMapView, "manager"),
			HasAvatarImage: boolAny(teamMapView, "has_avatar_image"), AvatarImageURL: stringAny(teamMapView, "avatar_image_url"),
			PlayerID: pick.PlayerID, PlayerName: player.Name, Position: player.Position, NFLTeam: player.NFLTeam,
			MadeBy: madeBy, IsAuto: madeBy == "auto", IsCommissioner: madeBy == "commissioner",
			Mine:          viewer != "" && pick.TeamID == viewer,
			TimeToPickSec: timeToPickSec,
			TimeToPick:    formatMMSS(time.Duration(timeToPickSec) * time.Second),
			HasValue:      hasValue, Value: value, ValueLabel: valueLabel,
			MadeAt: pick.MadeAt.UTC().Format(time.RFC3339),
		}
		pickedUpTo[pick.PlayerID] = pick.Number
		pickByNumber[pick.Number] = tapePicks[index]
		teamPicksAsc[pick.TeamID] = append(teamPicksAsc[pick.TeamID], tapePicks[index])
		teamPickPos[pick.Number] = len(teamPicksAsc[pick.TeamID])
	}

	// Rounds: newest round first, each round's picks newest pick first.
	byRound := map[int][]TapePick{}
	for _, pick := range tapePicks {
		byRound[pick.Round] = append(byRound[pick.Round], pick)
	}
	roundNumbers := make([]int, 0, len(byRound))
	for round := range byRound {
		roundNumbers = append(roundNumbers, round)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(roundNumbers)))
	tapeRounds := make([]TapeRound, 0, len(roundNumbers))
	for _, round := range roundNumbers {
		picks := byRound[round]
		sort.Slice(picks, func(i, j int) bool { return picks[i].Number > picks[j].Number })
		first := (round-1)*teamCount + 1
		last := round * teamCount
		tapeRounds = append(tapeRounds, TapeRound{
			Round: round, First: first, Last: last, Direction: snakeDirection(teamCount, first),
			Current: round == currentRound, Made: len(picks), Total: teamCount,
			Picks: picks,
		})
	}

	// Board: round-ascending, one cell per team column.
	columns := make([]map[string]any, 0, teamCount)
	for _, teamID := range order {
		column := s.teamMap(s.teamView(state, teamID))
		column["mine"] = viewer != "" && teamID == viewer
		columns = append(columns, column)
	}
	boardRows := make([]BoardRow, 0, rounds)
	for round := 1; round <= rounds; round++ {
		first := (round-1)*teamCount + 1
		row := BoardRow{Round: round, Direction: snakeDirection(teamCount, first), Cells: make([]BoardCell, teamCount)}
		for column := 1; column <= teamCount; column++ {
			number := boardCellNumber(round, column, teamCount)
			cell := BoardCell{
				Round: round, Column: column, Number: number, Label: pickLabel(number, teamCount),
				Mine: viewer != "" && column == viewerColumn, OnClock: !complete && number == next,
			}
			if number <= len(state.Picks) {
				pick := state.Picks[number-1]
				player := pool.byID[pick.PlayerID]
				madeBy := pick.MadeBy
				cell.Filled = true
				cell.PlayerName, cell.Position, cell.NFLTeam = player.Name, player.Position, player.NFLTeam
				cell.IsAuto, cell.IsCommissioner = madeBy == "auto", madeBy == "commissioner"
			}
			row.Cells[column-1] = cell
		}
		boardRows = append(boardRows, row)
	}

	// Teams: draft order, each with its own picks ascending and needs tally.
	// slotNames (D12, spruce audit) walks slotTable's own fixed lineup
	// order (QB, RB, WR, TE, FLEX, SUPERFLEX, DST, K, P — lineup.go, the
	// same order the roster page and auto-fill already use), filtered to
	// the keys this preset actually carries — never sort.Strings, which
	// scattered the chips alphabetically (DST, FLEX, K, P, QB, RB,
	// SUPERFLEX, TE, WR) with no relation to how a manager reads a
	// lineup.
	teamColumns := make([]TeamColumn, 0, teamCount)
	preset := CurrentRoster()
	slotNames := make([]string, 0, len(preset.Slots))
	for _, slot := range slotTable {
		if _, ok := preset.Slots[slot.Key]; ok {
			slotNames = append(slotNames, slot.Key)
		}
	}
	for _, teamID := range order {
		teamMapView := s.teamMap(s.teamView(state, teamID))
		teamMapView["mine"] = viewer != "" && teamID == viewer
		var picks []TapePick
		filled := map[string]int{}
		for _, pick := range tapePicks {
			if pick.TeamID != teamID {
				continue
			}
			picks = append(picks, pick)
			filled[pick.Position]++
		}
		needs := make([]map[string]any, 0, len(slotNames))
		for _, name := range slotNames {
			required := preset.Slots[name]
			have := filled[name]
			if have > required {
				have = required
			}
			needs = append(needs, map[string]any{"label": name, "filled": have, "total": required, "open": have < required})
		}
		teamColumns = append(teamColumns, TeamColumn{Team: teamMapView, Picks: picks, Needs: needs})
	}

	// detailFn computes one pick's PickDetail on request (P1 perf fix): the
	// source/queue-index scan is bounded by the team's own board length and
	// the best-available scan by pool.byADP (both independent of P, the
	// pick count), and TeamPicks is an O(1) slice off teamPicksAsc/
	// teamPickPos above — so a single Detail call costs O(board + pool),
	// never O(P), and this closure itself does no work until called.
	detailFn := func(number int) PickDetail {
		pick, ok := pickByNumber[number]
		if !ok {
			return PickDetail{}
		}
		player := pool.byID[pick.PlayerID]
		board := state.Boards[s.boardKeyForTeam(state, pick.TeamID)]
		source := "best available"
		queueIndex := 0
		for _, id := range board {
			if takenAt, taken := pickedUpTo[id]; taken && takenAt < pick.Number {
				continue
			}
			queueIndex++
			if id == pick.PlayerID {
				source = fmt.Sprintf("queue #%d", queueIndex)
				break
			}
		}
		best := make([]BestAvailablePick, 0, 3)
		for _, candidate := range pool.byADP {
			if takenAt, taken := pickedUpTo[candidate.ID]; taken && takenAt <= pick.Number {
				continue
			}
			best = append(best, BestAvailablePick{ID: candidate.ID, Name: candidate.Name, Position: candidate.Position, NFLTeam: candidate.NFLTeam})
			if len(best) == 3 {
				break
			}
		}
		var teamPicks []TapePick
		if pos, ok := teamPickPos[number]; ok {
			teamPicks = teamPicksAsc[pick.TeamID][:pos]
		}
		return PickDetail{
			TapePick: pick, Projection: fmt.Sprintf("%.1f", player.Projection),
			Source: source, BestAvailable: best, TeamPicks: teamPicks,
		}
	}

	latest := 0
	if len(tapePicks) > 0 {
		latest = tapePicks[len(tapePicks)-1].Number
	}

	return DraftHistoryView{
		Rounds: tapeRounds, Board: BoardView{Columns: columns, Rows: boardRows}, Teams: teamColumns,
		Picks: tapePicks, Complete: complete, Latest: latest, detail: detailFn,
	}
}

// valueVsADPLabel signs a pick-vs-ADP delta the tape/board/available pane
// convention: "+8" ahead of ADP (a value), "−4" behind it (a reach), with
// U+2212 MINUS SIGN rather than an ASCII hyphen for the negative case.
// pickNumber is the reference pick (a made pick's own Number, or the
// available pane's upcoming pick); adp is the player's ADP (already
// checked > 0 by the caller).
func valueVsADPLabel(pickNumber int, adp float64) string {
	value := pickNumber - int(math.Round(adp))
	if value < 0 {
		return "−" + fmt.Sprintf("%d", -value)
	}
	return fmt.Sprintf("+%d", value)
}

// ViewerFirstPickTeaser answers the home page's post-draft "Draft
// results" card (wave 7, item 3): the viewer's own earliest pick,
// rendered as "Ja'Marr Chase at 1.01" — playerName plus label
// (pickLabel's own "round.slot" form). hasPick is false when the viewer
// holds no seat, or their team has made no pick at all yet (the draft
// has not reached their turn). state.Picks is already Number-ascending
// (MakePick only ever appends), so the first match for teamID is that
// team's earliest pick — no sort needed.
//
// has_seat, not a bare non-empty team_id, gates this: viewerReadOnly
// (draftlive.go) fills team_id with Teams()[0]'s own id for EVERY
// signed-out or seatless request (its own read-only-fragment-polling
// placeholder identity), so team_id alone would tint that team's picks
// as "the viewer's own" for a visitor who never claimed a seat at all.
func (s *Service) ViewerFirstPickTeaser(r *http.Request) (playerName, label string, hasPick bool) {
	state := s.store.Snapshot()
	viewer := s.viewerReadOnly(r, state)
	if hasSeat, _ := viewer["has_seat"].(bool); !hasSeat {
		return "", "", false
	}
	teamID, _ := viewer["team_id"].(string)
	if teamID == "" {
		return "", "", false
	}
	order := state.DraftOrder
	if len(order) == 0 {
		order = defaultTeamIDs()
	}
	teamCount := len(order)
	pool := s.pool()
	for _, pick := range state.Picks {
		if pick.TeamID != teamID {
			continue
		}
		return pool.byID[pick.PlayerID].Name, pickLabel(pick.Number, teamCount), true
	}
	return "", "", false
}

// DraftResultsData answers /draft/results (wave 7, item 4): the full
// DraftHistoryView (Teams in draft order, the viewer's own team, if any,
// reordered to lead — app/draft/results' own page layer does that
// reshaping, the same "convert the canonical view into a page-specific
// shape" pattern app/draft's tapePickProps/boardViewProps already use),
// plus the header facts DraftHistoryView alone does not carry: the
// draft's own league-local date/time (draftSummaryForState, the same
// map /draft's command bar and the home page's own teaser both already
// read), and its round/team counts. ViewerTeamID is "" for a seatless
// viewer — has_seat gates it the same way ViewerFirstPickTeaser does
// (see that function's own doc comment for why a bare team_id is not
// enough: viewerReadOnly fills it with a placeholder team for every
// signed-out or seatless request).
func (s *Service) DraftResultsData(r *http.Request) map[string]any {
	state := s.store.Snapshot()
	viewer := s.viewerReadOnly(r, state)
	viewerTeamID := ""
	if hasSeat, _ := viewer["has_seat"].(bool); hasSeat {
		viewerTeamID, _ = viewer["team_id"].(string)
	}
	history := s.DraftHistory(state, viewerTeamID)
	order := state.DraftOrder
	if len(order) == 0 {
		order = defaultTeamIDs()
	}
	summary := s.draftSummaryForState(s.clock(), state)
	return map[string]any{
		"history":        history,
		"viewer_team_id": viewerTeamID,
		"complete":       history.Complete,
		"rounds":         CurrentDraftRounds(),
		"team_count":     len(order),
		"long_date":      summary["long_date"],
		"time":           summary["time"],
		"timezone":       summary["timezone"],
		"published":      summary["published"],
	}
}

// DraftLedger returns every pick in ascending order (DraftHistoryView.Picks,
// the tape's own ledger/CSV shape), read-only. It carries no viewer-specific
// tint (Mine is always false): the CSV export every viewer downloads from
// /draft/ledger.csv (app/draft's LedgerCSVHandler) is the same file for
// everyone.
func (s *Service) DraftLedger() []TapePick {
	return s.DraftHistory(s.store.Snapshot(), "").Picks
}

// DraftPickDetail answers one pick's lazily-loaded detail body (item 1b,
// 2026-08-30 review): app/draft's GET /draft/fragment/pick/{n} calls this
// directly, rather than routing through the eager DraftHistory build every
// other fragment shares, so opening one tape row costs one Detail lookup
// plus the DraftHistory build it depends on, not a whole-pane re-render.
// The bool is false for a pick number that has not been made (0, negative,
// or past the current pick count) — the caller answers 404 for that case,
// matching every other fragment handler's not-found shape.
func (s *Service) DraftPickDetail(r *http.Request, number int) (PickDetail, bool) {
	state := s.store.Snapshot()
	if number < 1 || number > len(state.Picks) {
		return PickDetail{}, false
	}
	viewer := s.viewerReadOnly(r, state)
	viewerTeam, _ := viewer["team_id"].(string)
	return s.DraftHistory(state, viewerTeam).Detail(number), true
}

func stringAny(m map[string]any, key string) string {
	value, _ := m[key].(string)
	return value
}

func boolAny(m map[string]any, key string) bool {
	value, _ := m[key].(bool)
	return value
}
