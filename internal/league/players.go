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
// owning team's abbreviation, ON WAIVERS with a claim form, or FREE AGENT
// with an add form — plus a server-side position filter (?pos=) and
// search (?q=), both plain GET params, the MY CLAIMS panel, and the
// WAIVER ORDER strip. Free agency opens the moment the draft completes
// (section 5.1's post-draft initial-state decision, draftComplete);
// before that, every row renders honestly with adds and claims disabled
// rather than pretend an undrafted pool is already free-agent territory.
func (s *Service) PlayersData(r *http.Request) map[string]any {
	viewer := s.Viewer(r)
	teamID, _ := viewer["team_id"].(string)
	canEdit := s.viewerKey(r) != ""
	state := s.store.Snapshot()
	pool := s.pool()
	scoringValues := s.currentScoringValues()
	owner := rosterOwner(currentRosters(state))
	open := draftComplete(state)
	now := s.clock()
	games := s.schedule()
	faab := s.cfg.Waivers.Mode == "faab"

	pos := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("pos")))
	rawQuery := strings.TrimSpace(r.URL.Query().Get("q"))
	query := strings.ToLower(rawQuery)

	myRoster := currentRosters(state)[teamID]
	rosterCap := CurrentRoster().Total()
	// atCap reads the effective (IR-excluding) size, so a team stashing an
	// injured player on IR correctly sees the spot it frees (SK spec).
	atCap := effectiveRosterSize(state, teamID) >= rosterCap

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

		status := waiverStatus{State: AvailabilityRostered}
		if !rostered {
			status = playerWaiverStatus(state, s.cfg, games, player.ID, player.NFLTeam, now)
		}
		onWaivers := status.State == AvailabilityOnWaivers
		freeAgent := status.State == AvailabilityFreeAgent
		_, claimedByMe := waiverClaimByTeamAndPlayer(state.WaiverClaims, teamID, player.ID)

		row["free_agent"] = freeAgent
		row["on_waivers"] = onWaivers
		row["waiver_resolves"] = ""
		if onWaivers {
			row["waiver_resolves"] = formatResolvesAt(s.cfg, status.ResolvesAt)
		}
		ownerAbbr := ""
		if rostered {
			ownerAbbr = s.teamByID(ownerID).Abbreviation
		}
		row["owner_abbr"] = ownerAbbr
		row["can_add"] = canEdit && open && freeAgent
		row["can_claim"] = canEdit && open && onWaivers && !claimedByMe
		row["claimed_by_me"] = claimedByMe
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

	order := waiverOrder(state, s.cfg)
	orderRows := make([]map[string]any, 0, len(order))
	for index, id := range order {
		orderRows = append(orderRows, map[string]any{
			"position": index + 1,
			"name":     s.teamByID(id).Name,
			"abbr":     s.teamByID(id).Abbreviation,
			"mine":     id == teamID,
		})
	}
	myPosition := waiverOrderPosition(order, teamID)
	faabRemainingByTeam := map[string]int{}
	if faab {
		faabRemainingByTeam = faabRemaining(state, s.cfg.Waivers.FAABBudget)
	}

	myClaims := make([]map[string]any, 0, len(state.WaiverClaims))
	for _, claim := range state.WaiverClaims {
		if claim.TeamID != teamID {
			continue
		}
		addPlayer := pool.byID[claim.AddID]
		dropLabel := ""
		if claim.DropID != "" {
			if dropPlayer, ok := pool.byID[claim.DropID]; ok {
				dropLabel = fmt.Sprintf("%s (%s)", dropPlayer.Name, dropPlayer.Position)
			}
		}
		myClaims = append(myClaims, map[string]any{
			"id":           claim.ID,
			"add_name":     addPlayer.Name,
			"add_position": addPlayer.Position,
			"drop_label":   dropLabel,
			"has_drop":     dropLabel != "",
			"filed_at":     claim.FiledAt.Format("Jan 2, 3:04 PM MST"),
			"bid":          claim.Bid,
			// priority is the team's own claim position (perf-priority
			// mode's public order, section 8.2: "the team's current claim
			// position of N ... from waiverOrder"), not the claim's own
			// filing-order Priority field (which only breaks ties among
			// this team's own claims at processing time).
			"priority": myPosition,
			"faab":     faab,
		})
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
		"waivers_faab":       faab,
		"waiver_order":       orderRows,
		"waiver_team_count":  len(order),
		"my_waiver_position": myPosition,
		"my_faab_remaining":  faabRemainingByTeam[teamID],
		"my_claims":          myClaims,
		"my_claims_empty":    len(myClaims) == 0,
	}
}

// AddPlayer applies the roster-ops spec section 5.3 player-add action: an
// instant free-agent signing, with an optional simultaneous drop (W6:
// required once the roster is full). Validates, in order: W1 (signed-in
// seat), W2 (pool membership), a pre-draft gate (free agency opens once
// the draft completes — section 5.1; not itself a numbered W-rule, since
// the spec assumes free agency starts post-draft), W3 (not already
// rostered), W12/W13 (on waivers or kickoff-locked — file a claim
// instead), then either W7/W8 (drop ownership/lock, when dropID is set)
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
	status := playerWaiverStatus(state, s.cfg, games, addID, addPlayer.NFLTeam, now)
	if status.State == AvailabilityOnWaivers {
		resolves := formatResolvesAt(s.cfg, status.ResolvesAt)
		if status.Reason == "kickoff" { // W13
			return "", fmt.Errorf("%s locked at kickoff; file a claim — it resolves %s", addPlayer.Name, resolves)
		}
		// W12
		return "", fmt.Errorf("%s is on waivers; claims resolve %s", addPlayer.Name, resolves)
	}
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
	var dropIDs []string
	if dropID != "" {
		dropPlayer, ok := pool.byID[dropID]
		if !ok || owner[dropID] != teamID { // W7
			return "", fmt.Errorf("%s", lineupNotOnRosterMessage)
		}
		if playerLocked(games, week, dropPlayer.NFLTeam, now) { // W8
			return "", fmt.Errorf("%s is locked and cannot be dropped until the week closes", dropPlayer.Name)
		}
		txn.Drops = []TransactionPlayer{transactionPlayerFromPlayer(dropPlayer)}
		dropIDs = []string{dropID}
	} else if effectiveRosterSize(state, teamID)+1 > rosterCap { // W6
		return "", fmt.Errorf("your roster is full; choose a player to drop")
	}
	// Limits (optional knob, default off, SK spec).
	if position, limit, breach := teamWouldBreachLimit(state, pool.byID, teamID, []string{addID}, dropIDs); breach {
		return "", fmt.Errorf("%s", limitMessage(position, limit))
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
// transaction, one persist. The dropped player leaves the acting team's
// roster the moment this record replays (currentRosters simply stops
// naming them) and enters the ON WAIVERS state by derivation
// (playerWaiverStatus, waivers.go): the same Transaction this call
// appends is what a later playerWaiverStatus call finds via
// lastDropInstant to compute the clear window (section 5.1).
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

// FileClaim applies the roster-ops spec section 5.3 claim-filing action.
// Validates, in order: W1 (signed-in seat), W2 (pool membership), W3 (add
// player not rostered), W5 (one claim per team per add player), W6/W7/W8
// (drop required/owned/unlocked, mirroring AddPlayer), then, only in faab
// mode, W10 (non-negative) and W9 (budget) — or, outside faab mode, W11
// (no bid at all). bid is 0 for every non-faab filing (the form carries no
// bid field then; a stray non-zero value still trips W11 rather than
// silently zeroing). W4 needs no check: both FREE AGENT and ON WAIVERS
// players may be claimed (a claim on a FREE AGENT player is legal but the
// /players page only ever offers CLAIM on ON WAIVERS rows — section 8.2).
func (s *Service) FileClaim(r *http.Request, requestedTeam, addID, dropID string, bid int) (string, error) {
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
	rosters := currentRosters(state)
	owner := rosterOwner(rosters)
	if owner[addID] != "" { // W3
		return "", fmt.Errorf("%s is already on a roster", addPlayer.Name)
	}
	if _, exists := waiverClaimByTeamAndPlayer(state.WaiverClaims, teamID, addID); exists { // W5
		return "", fmt.Errorf("you already hold a claim for %s", addPlayer.Name)
	}

	now := s.clock()
	games := s.schedule()
	week := s.pickemWeek(games, now)
	rosterCap := CurrentRoster().Total()
	claim := WaiverClaim{TeamID: teamID, AddID: addID, FiledAt: now}

	if dropID != "" {
		dropPlayer, ok := pool.byID[dropID]
		if !ok || owner[dropID] != teamID { // W7
			return "", fmt.Errorf("%s", lineupNotOnRosterMessage)
		}
		if playerLocked(games, week, dropPlayer.NFLTeam, now) { // W8
			return "", fmt.Errorf("%s is locked and cannot be dropped until the week closes", dropPlayer.Name)
		}
		claim.DropID = dropID
	} else if effectiveRosterSize(state, teamID)+1 > rosterCap { // W6
		return "", fmt.Errorf("your roster is full; choose a player to drop")
	}
	// Limits (optional knob, default off, SK spec) — filing-time fail
	// fast; ProcessWaivers re-checks at resolution time, since roster
	// composition may shift between filing and the claim's own run.
	var claimOutgoing []string
	if claim.DropID != "" {
		claimOutgoing = []string{claim.DropID}
	}
	if position, limit, breach := teamWouldBreachLimit(state, pool.byID, teamID, []string{addID}, claimOutgoing); breach {
		return "", fmt.Errorf("%s", limitMessage(position, limit))
	}

	if s.cfg.Waivers.Mode == "faab" {
		if bid < 0 { // W10
			return "", fmt.Errorf("bids must be between 0 and %d", s.cfg.Waivers.FAABBudget)
		}
		remaining := faabRemaining(state, s.cfg.Waivers.FAABBudget)[teamID]
		if bid > remaining { // W9
			return "", fmt.Errorf("your bid exceeds your remaining budget ($%d left)", remaining)
		}
		claim.Bid = bid
	} else if bid != 0 { // W11
		return "", fmt.Errorf("bids apply only in faab mode")
	}

	id, err := randomClaimID()
	if err != nil {
		return "", err
	}
	claim.ID = id
	if err := s.store.FileClaim(claim); err != nil {
		return "", err
	}
	if claim.DropID != "" {
		return fmt.Sprintf("Claim filed for %s; %s will drop if it wins.", addPlayer.Name, pool.byID[claim.DropID].Name), nil
	}
	return fmt.Sprintf("Claim filed for %s.", addPlayer.Name), nil
}

// CancelClaim withdraws one of the acting team's own open claims (roster-
// ops spec section 5.3: "managers may file, edit, and cancel claims until
// the run that resolves them"). W1 (signed-in seat) is the only gate;
// canceling an unknown or already-resolved claim ID is a harmless no-op
// (Store.CancelClaim's idempotent contract), matching the drop/badge
// idioms elsewhere in this package.
func (s *Service) CancelClaim(r *http.Request, requestedTeam, claimID string) (string, error) {
	teamID, err := s.actingTeam(r, requestedTeam) // W1
	if err != nil {
		return "", err
	}
	claimID = strings.TrimSpace(claimID)
	if err := s.store.CancelClaim(teamID, claimID); err != nil {
		return "", err
	}
	return "Claim canceled.", nil
}
