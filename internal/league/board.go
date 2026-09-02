package league

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// BoardPositionFilter canonicalizes the position query used by the Big Board.
// The allowlist is the same pool-position set used by /players; FLEX and
// SUPERFLEX are slot names, never player positions. Invalid values are
// ignored so a copied or hand-edited URL cannot create a phantom filter.
func BoardPositionFilter(raw string) string {
	position := strings.ToUpper(strings.TrimSpace(raw))
	if position == "" || position == "ALL" {
		return ""
	}
	for _, allowed := range playerPoolPositions {
		if position == allowed {
			return position
		}
	}
	return ""
}

const boardPoolFragment = "#board-pool"

func boardPageHref(position, query string, page int) string {
	return poolPageHref("/board", position, query, page) + boardPoolFragment
}

func boardPositionFilters(active, query string) []map[string]any {
	tabs := make([]map[string]any, 0, len(playerPoolPositions)+1)
	tabs = append(tabs, map[string]any{
		"label":  "ALL",
		"href":   boardPageHref("", query, 1),
		"active": active == "",
	})
	for _, position := range playerPoolPositions {
		tabs = append(tabs, map[string]any{
			"label":  position,
			"href":   boardPageHref(position, query, 1),
			"active": active == position,
		})
	}
	return tabs
}

func boardPageLinks(position, query string, pages, current int) []map[string]any {
	links := make([]map[string]any, 0, pages)
	for page := 1; page <= pages; page++ {
		links = append(links, map[string]any{
			"label":   fmt.Sprintf("%d", page),
			"page":    page,
			"href":    boardPageHref(position, query, page),
			"current": page == current,
		})
	}
	return links
}

// BoardData assembles the seat's shared draft board page: the ranked list
// plus the remaining pool to add from, both in draft-relevant order. Primary
// and co-managers resolve to the same key so the board shown to either person
// is the board the draft clock will actually use for that team.
func (s *Service) BoardData(r *http.Request) map[string]any {
	viewer := s.Viewer(r)
	state := s.store.Snapshot()
	// Read and mutation projections share the same persisted seat authority,
	// so an authenticated but unseated identity cannot acquire or view a
	// private scratch board that the draft clock would never use.
	authority, authorityErr := s.requestSeatAuthorityForState(r, state, "")
	key := ""
	if authorityErr == nil {
		key = authority.OwnerKey
	}
	pool := s.pool()
	position := BoardPositionFilter(r.URL.Query().Get("pos"))
	rawQuery := strings.TrimSpace(r.URL.Query().Get("q"))
	query := strings.ToLower(rawQuery)
	// Resolve commissioner scoring overrides once so board tooltips show the
	// same breakdown math as the draft room.
	scoringValues := s.currentScoringValues()
	// Resolved once for the same reason: matchupIndexFor scans the whole
	// schedule, and the board can render hundreds of pool rows.
	now := s.clock()
	games := s.schedule()
	matchup := s.matchupIndexFor(games, s.pickemWeek(games, now))
	matchupLabel, hasMatchupLabel := s.MatchupSourceLabel()
	picked := make(map[string]bool, len(state.Picks))
	for _, pick := range state.Picks {
		picked[pick.PlayerID] = true
	}
	boardIDs := state.Boards[key]
	onBoard := make(map[string]bool, len(boardIDs))
	entries := make([]map[string]any, 0, len(boardIDs))
	for index, id := range boardIDs {
		player, ok := pool.byID[id]
		if !ok {
			continue
		}
		onBoard[id] = true
		entry := playerMap(player, scoringValues, matchup)
		entry["board_rank"] = fmt.Sprintf("%02d", index+1)
		entry["board_can_move_up"] = index > 0
		entry["board_can_move_down"] = index+1 < len(boardIDs)
		entry["picked"] = picked[id]
		entries = append(entries, entry)
	}
	availablePlayers := make([]Player, 0, len(pool.players))
	for _, player := range pool.players {
		if onBoard[player.ID] || picked[player.ID] {
			continue
		}
		availablePlayers = append(availablePlayers, player)
	}
	availableCount := len(availablePlayers)
	matchingPlayers := make([]Player, 0, availableCount)
	for _, player := range availablePlayers {
		if position != "" && player.Position != position {
			continue
		}
		if query != "" && !strings.Contains(playerSearchText(player), query) {
			continue
		}
		matchingPlayers = append(matchingPlayers, player)
	}
	pagination := newPoolPagination(len(matchingPlayers), r.URL.Query().Get("page"))
	pagedPlayers := matchingPlayers[pagination.Start:pagination.End]
	paged := playerMapsWithScoring(pagedPlayers, scoringValues, matchup)
	return map[string]any{
		"viewer":       viewer,
		"public_entry": publicEntryData(s.publicEntryViewForViewerState(r, viewer, state)),
		"can_edit":     key != "",
		"board":        entries,
		"board_count":  len(entries),
		"available":    paged,
		// available_count/available_total are deliberately the unfiltered
		// available pool. matching_count/pool_total describe the current
		// server-side filter, so the UI can tell "all available" from
		// "matches on this query" without shipping the full pool.
		"available_count":      availableCount,
		"available_total":      availableCount,
		"matching_count":       pagination.Total,
		"matching_empty":       pagination.Total == 0,
		"available_empty":      availableCount == 0,
		"has_filters":          position != "" || rawQuery != "",
		"pos":                  position,
		"query":                rawQuery,
		"pool_position":        position,
		"pool_query":           rawQuery,
		"pool_total":           pagination.Total,
		"pool_page":            pagination.Page,
		"pool_pages":           pagination.Pages,
		"pool_page_size":       pagination.PageSize,
		"pool_page_start":      pageRangeStart(pagination),
		"pool_page_end":        pagination.End,
		"pool_has_previous":    pagination.HasPrevious,
		"pool_has_next":        pagination.HasNext,
		"pool_previous_href":   boardPageHref(position, rawQuery, pagination.Page-1),
		"pool_next_href":       boardPageHref(position, rawQuery, pagination.Page+1),
		"pool_page_links":      boardPageLinks(position, rawQuery, pagination.Pages, pagination.Page),
		"clear_filters_href":   "/board" + boardPoolFragment,
		"position_filters":     boardPositionFilters(position, rawQuery),
		"pool_status":          s.poolFreshnessMap(pool),
		"is_commissioner":      s.IsCommissioner(r),
		"league":               s.leagueMapForViewer(r),
		"matchup_source_label": matchupLabel,
		"has_matchup_source":   hasMatchupLabel,
	}
}

func pageRangeStart(pagination poolPagination) int {
	if pagination.Total == 0 {
		return 0
	}
	return pagination.Start + 1
}

func (s *Service) boardOwner(r *http.Request) (string, error) {
	key := boardKeyForViewer(s.store.Snapshot(), s.viewerKey(r))
	if key == "" {
		return "", fmt.Errorf("sign in to build a draft board")
	}
	return key, nil
}

func (s *Service) boardActionOwner(r *http.Request) (string, error) {
	authority, err := s.requestSeatAuthority(r, "")
	switch {
	case errors.Is(err, errSeatActionSignIn):
		return "", fmt.Errorf("sign in to build a draft board")
	case errors.Is(err, errSeatActionRequired):
		return "", fmt.Errorf("claim a team seat before building a draft board")
	case err != nil:
		return "", err
	default:
		return authority.OwnerKey, nil
	}
}

// boardKeyForViewer returns the durable owner key for a draft board. A seated
// viewer, including a co-manager, uses the primary member's normalized email;
// this preserves existing primary-manager boards while making the board a
// truthful seat-level draft control. Unseated managers keep a personal board
// until they claim a seat, and demo mode's shared guest key passes through.
func boardKeyForViewer(state PersistedState, viewerKey string) string {
	viewerKey = normalizeEmail(viewerKey)
	if viewerKey == "" || viewerKey == "demo-guest" {
		return viewerKey
	}
	member, ok := state.Members[viewerKey]
	if !ok || member.TeamID == "" {
		return viewerKey
	}
	primary := memberForTeam(state.Members, member.TeamID)
	if key := normalizeEmail(primary.Email); key != "" {
		return key
	}
	return viewerKey
}

// BoardAdd puts a pool player on the viewer's board.
func (s *Service) BoardAdd(r *http.Request, playerID string) (Player, error) {
	owner, err := s.boardActionOwner(r)
	if err != nil {
		return Player{}, err
	}
	player, ok := s.pool().byID[playerID]
	if !ok {
		return Player{}, fmt.Errorf("choose a player from the pool")
	}
	return player, s.store.BoardAdd(owner, playerID)
}

// BoardMove shifts a board entry one slot up or down.
func (s *Service) BoardMove(r *http.Request, playerID, direction string) error {
	owner, err := s.boardActionOwner(r)
	if err != nil {
		return err
	}
	delta := 1
	if direction == "up" {
		delta = -1
	}
	return s.store.BoardMove(owner, playerID, delta)
}

// BoardMoveTo moves a board entry to an absolute zero-based position, the
// action the declarative reorder primitive (data-gosx-reorder, see
// app/board/page.gsx) posts on drop. It mirrors BoardMove's ownership
// check; the store clamps an out-of-range index rather than rejecting it.
func (s *Service) BoardMoveTo(r *http.Request, playerID string, index int) error {
	owner, err := s.boardActionOwner(r)
	if err != nil {
		return err
	}
	return s.store.BoardMoveTo(owner, playerID, index)
}

// BoardRemove drops a board entry.
func (s *Service) BoardRemove(r *http.Request, playerID string) error {
	owner, err := s.boardActionOwner(r)
	if err != nil {
		return err
	}
	return s.store.BoardRemove(owner, playerID)
}

// BoardClear empties the viewer's board.
func (s *Service) BoardClear(r *http.Request) error {
	owner, err := s.boardActionOwner(r)
	if err != nil {
		return err
	}
	return s.store.BoardClear(owner)
}
