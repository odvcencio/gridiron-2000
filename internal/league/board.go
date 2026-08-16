package league

import (
	"fmt"
	"net/http"
)

// BoardData assembles the personal draft board page: the viewer's ranked
// list plus the remaining pool to add from, both in draft-relevant order.
func (s *Service) BoardData(r *http.Request) map[string]any {
	viewer := s.Viewer(r)
	key := s.viewerKey(r)
	state := s.store.Snapshot()
	pool := s.pool()
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
		entry := playerMap(player)
		entry["board_rank"] = fmt.Sprintf("%02d", index+1)
		entry["picked"] = picked[id]
		entries = append(entries, entry)
	}
	available := make([]map[string]any, 0, len(pool.players))
	for _, player := range pool.players {
		if onBoard[player.ID] || picked[player.ID] {
			continue
		}
		available = append(available, playerMap(player))
	}
	return map[string]any{
		"viewer":          viewer,
		"can_edit":        key != "",
		"board":           entries,
		"board_count":     len(entries),
		"available":       available,
		"pool_live":       pool.label == "live" || pool.label == "cache",
		"is_commissioner": s.IsCommissioner(r),
	}
}

func (s *Service) boardOwner(r *http.Request) (string, error) {
	key := s.viewerKey(r)
	if key == "" {
		return "", fmt.Errorf("sign in to build a draft board")
	}
	return key, nil
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
