package league

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// TradeBlockEntry is one team's public "available for trade" listing: the
// league-wide trade block every hosted provider shows beside its trade
// desk. A listing is a signal, not an offer; proposing still goes through
// the ordinary trade flow.
type TradeBlockEntry struct {
	TeamID   string    `json:"teamId"`
	PlayerID string    `json:"playerId"`
	Note     string    `json:"note,omitempty"`
	ListedAt time.Time `json:"listedAt"`
}

// tradeBlockNoteMaxRunes bounds the optional "looking for" note, the same
// budget a trade offer's own note carries.
const tradeBlockNoteMaxRunes = 240

// SetTradeBlock lists (listed) or unlists one of teamID's own rostered
// players on the trade block. Only the roster that holds the player may
// list them, and unlisting a player who is not listed is a no-op rather
// than an error, so a stale button never fails a manager.
func (s *Store) SetTradeBlock(teamID, playerID string, listed bool, note string, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeErrorLocked(); err != nil {
		return err
	}
	teamID = strings.TrimSpace(teamID)
	playerID = strings.TrimSpace(playerID)
	if teamID == "" || playerID == "" {
		return errors.New("choose one of your rostered players")
	}
	if owner := rosterOwner(currentRosters(s.state)); owner[playerID] != teamID {
		return fmt.Errorf("%s", lineupNotOnRosterMessage)
	}
	if s.state.TradeBlock == nil {
		s.state.TradeBlock = map[string]TradeBlockEntry{}
	}
	if !listed {
		if _, ok := s.state.TradeBlock[playerID]; !ok {
			return nil
		}
		delete(s.state.TradeBlock, playerID)
		return s.persistLocked(colScalars)
	}
	note = strings.TrimSpace(note)
	if utf8.RuneCountInString(note) > tradeBlockNoteMaxRunes {
		return fmt.Errorf("keep the trade block note to %d characters", tradeBlockNoteMaxRunes)
	}
	s.state.TradeBlock[playerID] = TradeBlockEntry{TeamID: teamID, PlayerID: playerID, Note: note, ListedAt: now.UTC()}
	return s.persistLocked(colScalars)
}

// tradeBlockEntries reads the block against the current rosters: a listing
// whose player has since been dropped or traded away is not shown, so a
// roster move never leaves a ghost entry. Sorted by team, newest listing
// first, then player ID for a stable render.
func tradeBlockEntries(state PersistedState) []TradeBlockEntry {
	owner := rosterOwner(currentRosters(state))
	out := make([]TradeBlockEntry, 0, len(state.TradeBlock))
	for playerID, entry := range state.TradeBlock {
		if entry.PlayerID == "" {
			entry.PlayerID = playerID
		}
		if owner[entry.PlayerID] != entry.TeamID {
			continue
		}
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].TeamID != out[j].TeamID {
			return out[i].TeamID < out[j].TeamID
		}
		if !out[i].ListedAt.Equal(out[j].ListedAt) {
			return out[i].ListedAt.After(out[j].ListedAt)
		}
		return out[i].PlayerID < out[j].PlayerID
	})
	return out
}

// SetTradeBlockSelection replaces teamID's listings with exactly playerIDs,
// each of which must be on teamID's roster, keeping ListedAt for a player
// who was already listed and applying one note to every listing. This is
// the team page's one-form contract: the checked players are the block.
func (s *Store) SetTradeBlockSelection(teamID string, playerIDs []string, note string, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeErrorLocked(); err != nil {
		return err
	}
	teamID = strings.TrimSpace(teamID)
	if teamID == "" {
		return errors.New("choose a team")
	}
	note = strings.TrimSpace(note)
	if utf8.RuneCountInString(note) > tradeBlockNoteMaxRunes {
		return fmt.Errorf("keep the trade block note to %d characters", tradeBlockNoteMaxRunes)
	}
	owner := rosterOwner(currentRosters(s.state))
	wanted := make(map[string]bool, len(playerIDs))
	for _, raw := range playerIDs {
		playerID := strings.TrimSpace(raw)
		if playerID == "" {
			continue
		}
		if owner[playerID] != teamID {
			return fmt.Errorf("%s", lineupNotOnRosterMessage)
		}
		wanted[playerID] = true
	}
	if s.state.TradeBlock == nil {
		s.state.TradeBlock = map[string]TradeBlockEntry{}
	}
	for playerID, entry := range s.state.TradeBlock {
		if entry.TeamID == teamID && !wanted[playerID] {
			delete(s.state.TradeBlock, playerID)
		}
	}
	for playerID := range wanted {
		entry, listed := s.state.TradeBlock[playerID]
		if !listed || entry.TeamID != teamID {
			entry = TradeBlockEntry{TeamID: teamID, PlayerID: playerID, ListedAt: now.UTC()}
		}
		entry.Note = note
		s.state.TradeBlock[playerID] = entry
	}
	return s.persistLocked(colScalars)
}

// TradeBlockOption is one rostered player as the team page's trade block
// panel shows it: a checkbox row that is checked while the player is listed.
type TradeBlockOption struct {
	ID       string
	Name     string
	Position string
	NFLTeam  string
	Listed   bool
}

// TradeBlockPlayerView is one listed player on the league-wide block.
type TradeBlockPlayerView struct {
	ID       string
	Name     string
	Position string
	NFLTeam  string
	Note     string
	HasNote  bool
}

// TradeBlockTeamView is one team's listings on the league-wide block, with
// the link a reader follows next: the trade desk composer for another
// team, the team terminal for their own.
type TradeBlockTeamView struct {
	TeamID      string
	TeamName    string
	IsViewer    bool
	ProposeHref string
	// Note is the team's shared "looking for" line, shown once per team;
	// every listing carries the same note (SetTradeBlockSelection).
	Note    string
	HasNote bool
	Players []TradeBlockPlayerView
}

// UpdateTradeBlock applies the team page's trade block form for the acting
// team: the checked players become the listing. The same admission gate
// every roster move uses (actingTeam) decides who may act for the seat;
// the store decides whether each player is theirs to list.
func (s *Service) UpdateTradeBlock(r *http.Request, requestedTeam string, playerIDs []string, note string) (string, error) {
	teamID, err := s.actingTeam(r, requestedTeam)
	if err != nil {
		return "", err
	}
	if !draftComplete(s.store.Snapshot()) {
		return "", fmt.Errorf("the trade block opens once the draft is complete")
	}
	if err := s.store.SetTradeBlockSelection(teamID, playerIDs, note, s.clock()); err != nil {
		return "", err
	}
	count := 0
	for _, raw := range playerIDs {
		if strings.TrimSpace(raw) != "" {
			count++
		}
	}
	switch count {
	case 0:
		return "Trade block cleared.", nil
	case 1:
		return "Trade block updated: 1 player listed.", nil
	default:
		return fmt.Sprintf("Trade block updated: %d players listed.", count), nil
	}
}

// tradeBlockPanel is the team page's own view of the block: every player
// on teamID's roster as a checkbox option, and the note the listings
// share. Options keep roster order (starters, then bench, then reserve)
// as rosterForTeam returns it.
func (s *Service) tradeBlockPanel(state PersistedState, teamID string) (options []TradeBlockOption, note string) {
	listed := make(map[string]TradeBlockEntry)
	for _, entry := range tradeBlockEntries(state) {
		if entry.TeamID == teamID {
			listed[entry.PlayerID] = entry
			if note == "" {
				note = entry.Note
			}
		}
	}
	roster, _ := s.rosterForTeam(state, teamID)
	options = make([]TradeBlockOption, 0, len(roster))
	for _, player := range roster {
		_, on := listed[player.ID]
		options = append(options, TradeBlockOption{ID: player.ID, Name: player.Name, Position: player.Position, NFLTeam: player.NFLTeam, Listed: on})
	}
	return options, note
}

// tradeBlockViews is the league-wide block as /trades renders it: one
// entry per team with at least one listing, the viewer's own team first,
// then by team label.
func (s *Service) tradeBlockViews(state PersistedState, viewerTeamID string) []TradeBlockTeamView {
	pool := s.pool()
	byTeam := make(map[string][]TradeBlockPlayerView)
	for _, entry := range tradeBlockEntries(state) {
		view := TradeBlockPlayerView{ID: entry.PlayerID, Note: entry.Note, HasNote: entry.Note != ""}
		if player, ok := pool.byID[entry.PlayerID]; ok {
			view.Name, view.Position, view.NFLTeam = player.Name, player.Position, player.NFLTeam
		}
		if view.Name == "" {
			view.Name = entry.PlayerID
		}
		byTeam[entry.TeamID] = append(byTeam[entry.TeamID], view)
	}
	out := make([]TradeBlockTeamView, 0, len(byTeam))
	for teamID, players := range byTeam {
		team := TradeBlockTeamView{TeamID: teamID, TeamName: s.TeamLabel(teamID), IsViewer: teamID == viewerTeamID, Players: players}
		for _, player := range players {
			if player.Note != "" {
				team.Note, team.HasNote = player.Note, true
				break
			}
		}
		if !team.IsViewer {
			team.ProposeHref = "/trades?counterparty=" + url.QueryEscape(teamID)
		}
		out = append(out, team)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].IsViewer != out[j].IsViewer {
			return out[i].IsViewer
		}
		return out[i].TeamName < out[j].TeamName
	})
	return out
}
