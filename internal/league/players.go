package league

import (
	"fmt"
	"net/http"
	"strings"
)

// playerPoolPositions is the /players position-filter tab set (roster-ops
// spec section 8.2): the slotTable's real positions, in engine order. FLEX
// and SUPERFLEX are slot names, not positions, and are excluded — a pool
// player's Position is always one of these seven.
var playerPoolPositions = []string{"QB", "RB", "WR", "TE", "DST", "K", "P"}

// positionFilterTabs renders the /players position filter as plain,
// server-side GET links (?pos=RB) — no client-side filtering (the hard
// constraint: search/filter here is a managed GET form; the declarative
// client filter primitive is a future gosx addition).
func positionFilterTabs(active string) []map[string]any {
	tabs := make([]map[string]any, 0, len(playerPoolPositions)+1)
	tabs = append(tabs, map[string]any{"label": "ALL", "href": "/players", "active": active == ""})
	for _, pos := range playerPoolPositions {
		tabs = append(tabs, map[string]any{"label": pos, "href": "/players?pos=" + pos, "active": active == pos})
	}
	return tabs
}

// playerMatchesQuery reports whether player's name, NFL team, or position
// contains query (already lower-cased). An empty query always matches.
func playerMatchesQuery(player Player, query string) bool {
	if query == "" {
		return true
	}
	return strings.Contains(strings.ToLower(player.Name), query) ||
		strings.Contains(strings.ToLower(player.NFLTeam), query) ||
		strings.Contains(strings.ToLower(player.Position), query)
}

// PlayersData assembles the /players pool browser (roster-ops spec
// section 8.2): every pool player's availability — ROSTERED with the
// owning team's abbreviation, or FREE AGENT with an add form — plus a
// server-side position filter (?pos=) and search (?q=), both plain GET
// params. Free agency opens the moment the draft completes (section
// 5.1's post-draft initial-state decision, draftComplete); before that,
// every row renders honestly with adds disabled rather than pretend an
// undrafted pool is already free-agent territory.
func (s *Service) PlayersData(r *http.Request) map[string]any {
	viewer := s.Viewer(r)
	teamID, _ := viewer["team_id"].(string)
	canEdit := s.viewerKey(r) != ""
	state := s.store.Snapshot()
	pool := s.pool()
	scoringValues := s.currentScoringValues()
	owner := rosterOwner(currentRosters(state))
	open := draftComplete(state)

	pos := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("pos")))
	rawQuery := strings.TrimSpace(r.URL.Query().Get("q"))
	query := strings.ToLower(rawQuery)

	myRoster := currentRosters(state)[teamID]
	rosterCap := CurrentRoster().Total()
	atCap := len(myRoster) >= rosterCap

	rows := make([]map[string]any, 0, len(pool.players))
	for _, player := range pool.players {
		if pos != "" && pos != "ALL" && player.Position != pos {
			continue
		}
		if !playerMatchesQuery(player, query) {
			continue
		}
		ownerID := owner[player.ID]
		rostered := ownerID != ""
		row := playerMap(player, scoringValues)
		row["rostered"] = rostered
		row["free_agent"] = !rostered
		ownerAbbr := ""
		if rostered {
			ownerAbbr = s.teamByID(ownerID).Abbreviation
		}
		row["owner_abbr"] = ownerAbbr
		row["can_add"] = canEdit && open && !rostered
		row["needs_drop"] = atCap
		row["mine"] = canEdit && rostered && ownerID == teamID
		rows = append(rows, row)
	}

	dropOptions := make([]map[string]any, 0, len(myRoster))
	for _, id := range myRoster {
		if player, ok := pool.byID[id]; ok {
			dropOptions = append(dropOptions, map[string]any{
				"id":    player.ID,
				"label": fmt.Sprintf("%s (%s)", player.Name, player.Position),
			})
		}
	}

	return map[string]any{
		"viewer":             viewer,
		"league":             s.leagueMap(),
		"can_edit":           canEdit,
		"free_agency_open":   open,
		"pos":                pos,
		"positions":          positionFilterTabs(pos),
		"query":              rawQuery,
		"players":            rows,
		"players_empty":      len(rows) == 0,
		"pool_live":          pool.label == "live" || pool.label == "cache",
		"pool_label":         pool.label,
		"at_cap":             atCap,
		"roster_size":        len(myRoster),
		"roster_cap":         rosterCap,
		"drop_options":       dropOptions,
		"drop_options_empty": len(dropOptions) == 0,
	}
}

// AddPlayer applies the roster-ops spec section 5.3 player-add action: an
// instant free-agent signing, with an optional simultaneous drop (W6:
// required once the roster is full). Validates, in order: W1 (signed-in
// seat), W2 (pool membership), a pre-draft gate (free agency opens once
// the draft completes — section 5.1; not itself a numbered W-rule, since
// the spec assumes free agency starts post-draft), W3 (not already
// rostered), then either W7/W8 (drop ownership/lock, when dropID is set)
// or W6 (roster full with no drop named). One transaction record covers
// both sides in a single persist (Store.RecordTransaction) — a drop
// accompanying an add is one atomic move, not two.
func (s *Service) AddPlayer(r *http.Request, requestedTeam, addID, dropID string) (string, error) {
	teamID, err := s.actingTeam(r, requestedTeam) // W1
	if err != nil {
		return "", err
	}
	addID = strings.TrimSpace(addID)
	dropID = strings.TrimSpace(dropID)
	pool := s.pool()
	addPlayer, ok := pool.byID[addID]
	if !ok { // W2
		return "", fmt.Errorf("choose an available player")
	}
	state := s.store.Snapshot()
	if !draftComplete(state) {
		return "", fmt.Errorf("free agency opens once the draft is complete")
	}
	rosters := currentRosters(state)
	owner := rosterOwner(rosters)
	if owner[addID] != "" { // W3
		return "", fmt.Errorf("%s is already on a roster", addPlayer.Name)
	}

	now := s.clock()
	games := s.schedule()
	week := s.pickemWeek(games, now)
	rosterCap := CurrentRoster().Total()

	txn := Transaction{
		Season: s.cfg.Season,
		Week:   week,
		Type:   "add",
		TeamID: teamID,
		Adds:   []TransactionPlayer{transactionPlayerFromPlayer(addPlayer)},
		By:     "manager",
		At:     now,
	}
	if dropID != "" {
		dropPlayer, ok := pool.byID[dropID]
		if !ok || owner[dropID] != teamID { // W7
			return "", fmt.Errorf("%s", lineupNotOnRosterMessage)
		}
		if playerLocked(games, week, dropPlayer.NFLTeam, now) { // W8
			return "", fmt.Errorf("%s is locked and cannot be dropped until the week closes", dropPlayer.Name)
		}
		txn.Drops = []TransactionPlayer{transactionPlayerFromPlayer(dropPlayer)}
	} else if len(rosters[teamID])+1 > rosterCap { // W6
		return "", fmt.Errorf("your roster is full; choose a player to drop")
	}

	id, err := randomTransactionID()
	if err != nil {
		return "", err
	}
	txn.ID = id
	if err := s.store.RecordTransaction(txn, rosterCap); err != nil {
		return "", err
	}
	if len(txn.Drops) > 0 {
		return fmt.Sprintf("%s signed; %s dropped.", addPlayer.Name, txn.Drops[0].Name), nil
	}
	return fmt.Sprintf("%s signed.", addPlayer.Name), nil
}

// DropPlayer applies the roster-ops spec section 5.3 player-drop action:
// W1 (signed-in seat), W7 (ownership), W8 (lock). Appends one drop
// transaction, one persist. The dropped player becomes a free agent by
// derivation — currentRosters simply stops naming them the moment this
// record replays; WP-R3 has no waiver system, so there is no clearing
// window yet (section 5.1's ON WAIVERS state is WP-R4 scope).
func (s *Service) DropPlayer(r *http.Request, requestedTeam, dropID string) (string, error) {
	teamID, err := s.actingTeam(r, requestedTeam) // W1
	if err != nil {
		return "", err
	}
	dropID = strings.TrimSpace(dropID)
	pool := s.pool()
	dropPlayer, ok := pool.byID[dropID]
	if !ok {
		return "", fmt.Errorf("%s", lineupNotOnRosterMessage)
	}
	state := s.store.Snapshot()
	owner := rosterOwner(currentRosters(state))
	if owner[dropID] != teamID { // W7
		return "", fmt.Errorf("%s", lineupNotOnRosterMessage)
	}
	now := s.clock()
	games := s.schedule()
	week := s.pickemWeek(games, now)
	if playerLocked(games, week, dropPlayer.NFLTeam, now) { // W8
		return "", fmt.Errorf("%s is locked and cannot be dropped until the week closes", dropPlayer.Name)
	}

	id, err := randomTransactionID()
	if err != nil {
		return "", err
	}
	txn := Transaction{
		ID:     id,
		Season: s.cfg.Season,
		Week:   week,
		Type:   "drop",
		TeamID: teamID,
		Drops:  []TransactionPlayer{transactionPlayerFromPlayer(dropPlayer)},
		By:     "manager",
		At:     now,
	}
	if err := s.store.RecordTransaction(txn, CurrentRoster().Total()); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s dropped.", dropPlayer.Name), nil
}
