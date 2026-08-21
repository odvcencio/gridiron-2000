package league

import (
	"fmt"
	"net/http"
)

// BoardData assembles the seat's shared draft board page: the ranked list
// plus the remaining pool to add from, both in draft-relevant order. Primary
// and co-managers resolve to the same key so the board shown to either person
// is the board the draft clock will actually use for that team.
func (s *Service) BoardData(r *http.Request) map[string]any {
	viewer := s.Viewer(r)
	state := s.store.Snapshot()
	key := boardKeyForViewer(state, s.viewerKey(r))
	pool := s.pool()
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
		entry["picked"] = picked[id]
		entries = append(entries, entry)
	}
	available := make([]map[string]any, 0, len(pool.players))
	for _, player := range pool.players {
		if onBoard[player.ID] || picked[player.ID] {
			continue
		}
		available = append(available, playerMap(player, scoringValues, matchup))
	}
	return map[string]any{
		"viewer":               viewer,
		"can_edit":             key != "",
		"board":                entries,
		"board_count":          len(entries),
		"available":            available,
		"pool_live":            pool.label == "live" || pool.label == "cache",
		"is_commissioner":      s.IsCommissioner(r),
		"league":               s.leagueMap(),
		"matchup_source_label": matchupLabel,
		"has_matchup_source":   hasMatchupLabel,
	}
}

func (s *Service) boardOwner(r *http.Request) (string, error) {
	key := boardKeyForViewer(s.store.Snapshot(), s.viewerKey(r))
	if key == "" {
		return "", fmt.Errorf("sign in to build a draft board")
	}
	return key, nil
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
	owner, err := s.boardOwner(r)
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
	owner, err := s.boardOwner(r)
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
	owner, err := s.boardOwner(r)
	if err != nil {
		return err
	}
	return s.store.BoardMoveTo(owner, playerID, index)
}

// BoardRemove drops a board entry.
func (s *Service) BoardRemove(r *http.Request, playerID string) error {
	owner, err := s.boardOwner(r)
	if err != nil {
		return err
	}
	return s.store.BoardRemove(owner, playerID)
}

// BoardClear empties the viewer's board.
func (s *Service) BoardClear(r *http.Request) error {
	owner, err := s.boardOwner(r)
	if err != nil {
		return err
	}
	return s.store.BoardClear(owner)
}
