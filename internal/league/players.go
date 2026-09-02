package league

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

const playerDataUnavailableMessage = "player data is unavailable; roster and waiver actions are temporarily blocked"

func requirePlayerData(pool playerPool) error {
	if playerPoolIsUnavailable(pool) {
		return fmt.Errorf("%s", playerDataUnavailableMessage)
	}
	return nil
}

// waiverClaimResolutionView projects the processor-authoritative resolution
// state for one open private claim. A drop-clear instant may delay the next
// processor run, but a stale clear instant never substitutes for that run.
// Kickoff waivers depend on the live Final flag, so their approximate
// kickoff-plus-five-hours display estimate is never promoted to an exact
// claim-resolution timestamp.
func waiverClaimResolutionView(state PersistedState, cfg Config, games []GameInfo, player Player, now time.Time) map[string]any {
	view := map[string]any{
		"resolution_state":    "unknown",
		"resolution_label":    "Resolution time unavailable.",
		"resolution_at":       "",
		"resolution_relative": "",
		"has_resolution_at":   false,
	}
	owner := rosterOwner(currentRosters(state))
	if owner[player.ID] != "" {
		view["resolution_label"] = "Player status changed; refresh before waiver processing."
		return view
	}

	resolveAt := nextWaiverProcessingRun(cfg, state.WaiversProcessedThrough, now)
	status := playerWaiverStatus(state, cfg, games, player.ID, player.NFLTeam, now)
	if status.State == AvailabilityOnWaivers {
		if status.Reason == "kickoff" {
			// F7: share the exact same label the pool row renders
			// (waiverKickoffPendingLabel), so this player's kickoff-locked
			// resolve time can never disagree between the two surfaces.
			view["resolution_state"] = "degraded"
			view["resolution_label"] = waiverKickoffPendingLabel
			return view
		}
		if status.ResolvesAt.IsZero() {
			view["resolution_label"] = "The waiver processor has no authoritative resolution time for this claim."
			return view
		}
		if status.ResolvesAt.After(resolveAt) {
			resolveAt = status.ResolvesAt
		}
	}

	view["resolution_at"] = formatResolvesAt(cfg, resolveAt)
	view["resolution_relative"] = deadlineRelativeTime(now, resolveAt)
	view["has_resolution_at"] = true
	if resolveAt.After(now) {
		view["resolution_state"] = "scheduled"
		view["resolution_label"] = "Resolves"
	} else {
		view["resolution_state"] = "overdue"
		view["resolution_label"] = "Resolution overdue; waiver processing has not recorded an outcome yet."
	}
	return view
}

// playerPoolPositions is the /players position-filter tab set (roster-ops
// spec section 8.2): the slotTable's real positions, in engine order. FLEX
// and SUPERFLEX are slot names, not positions, and are excluded — a pool
// player's Position is always one of these seven.
var playerPoolPositions = []string{"QB", "RB", "WR", "TE", "DST", "K", "P"}

// positionFilterTabs renders the /players position filter as plain,
// server-side GET links (?pos=RB) — no client-side filtering (the hard
// constraint: search/filter here is a managed GET form; the declarative
// client filter primitive is a future gosx addition).
func positionFilterTabs(active, query string) []map[string]any {
	tabs := make([]map[string]any, 0, len(playerPoolPositions)+1)
	tabs = append(tabs, map[string]any{"label": "ALL", "href": poolPageHref("/players", "", query, 1), "active": active == ""})
	for _, pos := range playerPoolPositions {
		tabs = append(tabs, map[string]any{"label": pos, "href": poolPageHref("/players", pos, query, 1), "active": active == pos})
	}
	return tabs
}

// playerSearchText is the normalized search text shared by pool maps and
// server-side filtering on the draft, players, and Big Board surfaces.
func playerSearchText(player Player) string {
	return strings.ToLower(player.Name + " " + player.NFLTeam + " " + player.Position)
}

// playerMatchesQuery reports whether the normalized player search text
// contains query (already lower-cased). An empty query always matches.
func playerMatchesQuery(player Player, query string) bool {
	if query == "" {
		return true
	}
	return strings.Contains(playerSearchText(player), query)
}

// waiverReceiptRow projects one WaiverReceipt for a viewer (F5, F8). The
// durable WaiverReceipt itself always carries the true winning FAAB bid
// (needed for the audited record and the commissioner's oversight view),
// but discloseWinningBid gates whether this particular projection may ever
// surface it: false for a team's own private "MY CLAIMS" receipts (F8 — a
// beaten team's own view says only that it was outbid, never the amount),
// true only for the commissioner's all-teams overlay (F5). includeTeam
// adds the owning team's name/abbreviation, which only the commissioner's
// cross-team view needs — a manager's own receipts are already known to
// be their own team's.
func (s *Service) waiverReceiptRow(receipt WaiverReceipt, includeTeam, discloseWinningBid bool) map[string]any {
	dropLabel := ""
	if len(receipt.Drops) > 0 {
		dropLabel = fmt.Sprintf("%s (%s)", receipt.Drops[0].Name, receipt.Drops[0].Position)
	}
	winnerName, winnerAbbr := "", ""
	if receipt.WinningTeamID != "" {
		winner := s.teamByID(receipt.WinningTeamID)
		winnerName, winnerAbbr = winner.Name, winner.Abbreviation
	}
	winningBid, hasWinningBid := 0, false
	if discloseWinningBid && receipt.Outcome == "beaten" && receipt.Mode == "faab" && receipt.WinningBidKnown {
		winningBid, hasWinningBid = receipt.WinningBid, true
	}
	row := map[string]any{
		"claim_id":          receipt.ClaimID,
		"season":            receipt.Season,
		"week":              receipt.Week,
		"add_name":          receipt.Add.Name,
		"add_position":      receipt.Add.Position,
		"drop_label":        dropLabel,
		"has_drop":          dropLabel != "",
		"bid":               receipt.Bid,
		"faab":              receipt.Mode == "faab",
		"submitted_order":   receipt.SubmittedPriority,
		"waiver_position":   receipt.WaiverPosition,
		"waiver_team_count": receipt.WaiverTeamCount,
		"outcome":           strings.ToUpper(receipt.Outcome),
		"won":               receipt.Outcome == "won",
		"beaten":            receipt.Outcome == "beaten",
		"failed":            receipt.Outcome == "failed",
		"reason":            receipt.Reason,
		"has_winner":        receipt.WinningTeamID != "",
		"winner_name":       winnerName,
		"winner_abbr":       winnerAbbr,
		"winning_bid":       winningBid,
		"has_winning_bid":   hasWinningBid,
		"resolved_at":       s.leagueTimeStamp(receipt.ResolvedAt),
	}
	if includeTeam {
		team := s.teamByID(receipt.TeamID)
		row["team_name"] = team.Name
		row["team_abbr"] = team.Abbreviation
	}
	return row
}

// playerRosterZoneCounts reports the owned roster's presentation buckets for
// the Player Pool capacity summary. General and reserve occupants consume the
// draftable roster capacity; IR occupants are owned, but intentionally sit
// outside that cap. Count only currently owned IDs so a stale zone overlay
// cannot manufacture occupancy for a player who has already left the roster.
func playerRosterZoneCounts(state PersistedState, teamID string) (general, reserve, ir int) {
	for _, playerID := range currentRosters(state)[teamID] {
		switch zoneOfPlayer(state, teamID, playerID) {
		case zoneReserve:
			reserve++
		case zoneIR:
			ir++
		default:
			general++
		}
	}
	return general, reserve, ir
}

// PlayersData assembles the /players pool browser (roster-ops spec
// section 8.2): every pool player's availability — ROSTERED with the
// owning team's abbreviation, ON WAIVERS with a claim form, or FREE AGENT
// with an add form — plus a server-side position filter (?pos=) and
// search (?q=), both plain GET params, the MY CLAIMS panel, and the
// WAIVER ORDER strip. Free agency opens the moment the draft completes
// (section 5.1's post-draft initial-state decision, draftComplete);
// before that, every row renders honestly with adds, claims, and drops
// disabled rather than permit roster churn while the draft is still live.
func (s *Service) PlayersData(r *http.Request) map[string]any {
	viewer := s.Viewer(r)
	teamID, _ := viewer["team_id"].(string)
	hasSeat, _ := viewer["has_seat"].(bool)
	state := s.store.Snapshot()
	publicEntry := s.publicEntryDataForViewerState(r, viewer, state)
	pool := s.pool()
	poolUnavailable := playerPoolIsUnavailable(pool)
	canEdit := hasSeat && !poolUnavailable
	scoringValues := s.currentScoringValues()
	owner := rosterOwner(currentRosters(state))
	open := draftComplete(state)
	now := s.clock()
	games := s.schedule()
	lineupWeek := lineupCurrentWeekAt(games, now)
	// Resolved once for the same reason scoringValues is: matchupIndexFor
	// scans the whole schedule, and this pool renders every player in the
	// league.
	matchup := s.matchupIndexFor(games, s.pickemWeek(games, now))
	matchupLabel, hasMatchupLabel := s.MatchupSourceLabel()
	faab := s.cfg.Waivers.Mode == "faab"

	pos := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("pos")))
	rawQuery := strings.TrimSpace(r.URL.Query().Get("q"))
	query := strings.ToLower(rawQuery)

	myRoster := currentRosters(state)[teamID]
	preset := CurrentRoster()
	rosterCap := preset.Total()
	generalRosterCap := preset.Starters() + preset.Bench
	generalRosterSize, reserveRosterSize, irRosterSize := playerRosterZoneCounts(state, teamID)
	// atCap reads the effective (IR-excluding) size, so a team stashing an
	// injured player on IR correctly sees the spot it frees (SK spec).
	effectiveSize := effectiveRosterSize(state, teamID)
	atCap := effectiveSize >= rosterCap

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
		row := playerMap(player, scoringValues, matchup)
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
			// F7: one shared answer for a kickoff-locked player's resolve
			// time, so this pool row can never disagree with the same
			// player's MY CLAIMS row (waiverClaimResolutionView) — a
			// kickoff-lock's ResolvesAt is only ever an estimate the live
			// processor does not itself use to decide "due".
			if status.Reason == "kickoff" {
				row["waiver_resolves"] = waiverKickoffPendingLabel
			} else {
				// Append the same relative-time idiom the MY CLAIMS row
				// already carries (waiverClaimResolutionView's
				// resolution_relative, deadlineRelativeTime) — the pool row
				// used to show only the absolute time, so "ON WAIVERS · Sep
				// 4, 9:00 AM EDT" disagreed with the desk's own "(in 2
				// days)" for the exact same deadline (2026-08-31 gap audit).
				row["waiver_resolves"] = formatResolvesAt(s.cfg, status.ResolvesAt)
				if relative := deadlineRelativeTime(now, status.ResolvesAt); relative != "" {
					row["waiver_resolves"] = row["waiver_resolves"].(string) + " · " + relative
				}
			}
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
		dropLocked := rostered && ownerID == teamID && playerLockedForRosterMutation(state, games, lineupWeek, player, now)
		dropLockReason := ""
		if dropLocked {
			// lineupWeek is an action/display selector and may already be W2
			// while this player's W1 game is kicked but the persisted W1
			// schedule is not final. Prefer the durable historical lock week
			// so the explanation names the scoring week that is actually
			// protected, not the later selector week.
			lockWeek := lineupWeek
			if historicalWeek, historicalLocked := playerLockedByUnfinalizedWeek(state, games, player.NFLTeam, now); historicalLocked {
				lockWeek = historicalWeek
			}
			dropLockReason = fmt.Sprintf("DROP LOCKED: %s", lineupLockedMessage(player.Name, lockWeek, player.NFLTeam))
		}
		row["drop_locked"] = dropLocked
		row["drop_lock_reason"] = dropLockReason
		row["can_drop"] = canEdit && open && rostered && ownerID == teamID && !dropLocked
		rows = append(rows, row)
	}
	pagination := newPoolPagination(len(rows), r.URL.Query().Get("page"))
	rows = rows[pagination.Start:pagination.End]

	dropOptions := make([]map[string]any, 0, len(myRoster))
	for _, id := range myRoster {
		if player, ok := pool.byID[id]; ok {
			if playerLockedForRosterMutation(state, games, lineupWeek, player, now) {
				continue
			}
			label := fmt.Sprintf("%s (%s)", player.Name, player.Position)
			// F3: an IR occupant stays offered as a drop choice, but its
			// label says so plainly — effectiveRosterSize already excludes
			// it from the cap, so dropping it frees no additional roster
			// spot on top of the one IR already gave (it is priced at zero
			// credit; see creditedDropCount, zones.go).
			if zoneOfPlayer(state, teamID, id) == zoneIR {
				label += " — IR, will not free a roster spot"
			}
			dropOptions = append(dropOptions, map[string]any{
				"id":    player.ID,
				"label": label,
			})
		}
	}

	order := waiverOrder(state, s.cfg, games, now)
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

	myClaimIndices := teamClaimIndices(state.WaiverClaims, teamID)
	myClaims := make([]map[string]any, 0, len(myClaimIndices))
	for claimIndex, stateIndex := range myClaimIndices {
		claim := state.WaiverClaims[stateIndex]
		addPlayer := pool.byID[claim.AddID]
		dropLabel := ""
		if claim.DropID != "" {
			if dropPlayer, ok := pool.byID[claim.DropID]; ok {
				dropLabel = fmt.Sprintf("%s (%s)", dropPlayer.Name, dropPlayer.Position)
			}
		}
		// F6: an AddID absent from the current pool is not "unknown" data —
		// it is a claim the processor deferred rather than failed (a roster
		// shuffle or source refresh left the player out of this run's
		// bounded pool). This label matches the exact deferred state the
		// claim stays open under (store.go's ProcessWaivers), so MY CLAIMS
		// never mislabels an open, still-live claim as a data outage.
		resolution := map[string]any{
			"resolution_state": "deferred",
			"resolution_label": "Deferred: player not in the pool this run; the claim stays open and resolves once they return, or you cancel it.",
			"resolution_at":    "", "resolution_relative": "", "has_resolution_at": false,
		}
		if addPlayer.ID != "" {
			resolution = waiverClaimResolutionView(state, s.cfg, games, addPlayer, now)
		}
		myClaims = append(myClaims, map[string]any{
			"id":                  claim.ID,
			"add_name":            addPlayer.Name,
			"add_position":        addPlayer.Position,
			"drop_label":          dropLabel,
			"has_drop":            dropLabel != "",
			"resolution_state":    resolution["resolution_state"],
			"resolution_label":    resolution["resolution_label"],
			"resolution_at":       resolution["resolution_at"],
			"resolution_relative": resolution["resolution_relative"],
			"has_resolution_at":   resolution["has_resolution_at"],
			"filed_at":            s.leagueTimeStamp(claim.FiledAt),
			"bid":                 claim.Bid,
			"priority":            claim.Priority,
			"claim_count":         len(myClaimIndices),
			"waiver_position":     myPosition,
			"waiver_team_count":   len(order),
			"can_move_up":         claimIndex > 0,
			"can_move_down":       claimIndex+1 < len(myClaimIndices),
			"faab":                faab,
		})
	}

	myReceipts := make([]map[string]any, 0, 20)
	if canEdit {
		for index := len(state.WaiverReceipts) - 1; index >= 0 && len(myReceipts) < 20; index-- {
			receipt := state.WaiverReceipts[index]
			if receipt.TeamID != teamID {
				continue
			}
			myReceipts = append(myReceipts, s.waiverReceiptRow(receipt, false, false))
		}
	}

	// F5: the commissioner sees every team's receipts, not just their own
	// team's — the same commissioner gate (IsCommissioner) every other
	// commissioner-only overlay in this package already reuses. F8: this
	// is also the one place the true winning FAAB bid may ever surface
	// for a beaten claim — a trusted, audited overlay, never the beaten
	// team's own private view.
	isCommissioner := s.IsCommissioner(r)
	commissionerReceipts := make([]map[string]any, 0, 50)
	if isCommissioner {
		for index := len(state.WaiverReceipts) - 1; index >= 0 && len(commissionerReceipts) < 50; index-- {
			commissionerReceipts = append(commissionerReceipts, s.waiverReceiptRow(state.WaiverReceipts[index], true, true))
		}
	}

	// addLockedReason is the contract's adjacent plain-language reason for
	// the disabled Add control every non-addable free-agent row renders
	// (the 2026-09-01 UX audit found fifty disabled Adds with no reason in
	// reach). One page-level value: every row locks for the same cause. A
	// pool outage stays empty here — the OFFLINE PLAYER LIST notice
	// already owns that explanation.
	addLockedReason := ""
	switch {
	case poolUnavailable:
	case s.DemoMode() && !hasSeat:
		addLockedReason = "Demo mode is read-only. Sign in with a team seat to make moves."
	case !hasSeat:
		addLockedReason = "Adding players needs a team seat."
	case !open:
		addLockedReason = "Roster moves open after the draft."
	}

	return map[string]any{
		"viewer":             viewer,
		"public_entry":       publicEntry,
		"league":             s.leagueMapForViewer(r),
		"can_edit":           canEdit,
		"pool_unavailable":   poolUnavailable,
		"free_agency_open":   open,
		"add_locked_reason":  addLockedReason,
		"pos":                pos,
		"positions":          positionFilterTabs(pos, rawQuery),
		"query":              rawQuery,
		"players":            rows,
		"players_empty":      pagination.Total == 0,
		"pool_total":         pagination.Total,
		"pool_page":          pagination.Page,
		"pool_pages":         pagination.Pages,
		"pool_page_size":     pagination.PageSize,
		"pool_page_start":    pagination.Start + 1,
		"pool_page_end":      pagination.End,
		"pool_has_previous":  pagination.HasPrevious,
		"pool_has_next":      pagination.HasNext,
		"pool_previous_href": poolPageHref("/players", pos, rawQuery, pagination.Page-1),
		"pool_next_href":     poolPageHref("/players", pos, rawQuery, pagination.Page+1),
		"pool_status":        s.poolFreshnessMap(pool),
		"at_cap":             atCap,
		// roster_size is the effective draftable count (general + reserve),
		// not raw owned IDs. An IR occupant is owned but sits outside the
		// cap, so this remains honest at e.g. 17 / 17 plus IR 1 / 2.
		"roster_size":         effectiveSize,
		"roster_cap":          rosterCap,
		"roster_general_size": generalRosterSize,
		"roster_general_cap":  generalRosterCap,
		"roster_reserve_size": reserveRosterSize,
		"roster_reserve_cap":  preset.ReserveTotal(),
		"roster_ir_size":      irRosterSize,
		"roster_ir_cap":       preset.IR,
		"roster_capacity_summary": fmt.Sprintf(
			"GENERAL %d / %d · RESERVE %d / %d · IR %d / %d",
			generalRosterSize, generalRosterCap,
			reserveRosterSize, preset.ReserveTotal(),
			irRosterSize, preset.IR,
		),
		"drop_options":       dropOptions,
		"drop_options_empty": len(dropOptions) == 0,
		"waivers_faab":       faab,
		"waiver_order":       orderRows,
		"waiver_team_count":  len(order),
		"my_waiver_position": myPosition,
		"my_faab_remaining":  faabRemainingByTeam[teamID],
		"my_claims":          myClaims,
		"my_claims_empty":    len(myClaims) == 0,
		"my_waiver_receipts": myReceipts,
		"my_receipts_empty":  len(myReceipts) == 0,
		// is_commissioner/commissioner_waiver_receipts (F5): commissioner
		// oversight of the waiver wire — every team's receipts, not just
		// the viewer's own, reusing the same IsCommissioner gate every
		// other commissioner-only overlay in this package already uses.
		"is_commissioner":              isCommissioner,
		"commissioner_waiver_receipts": commissionerReceipts,
		"commissioner_receipts_empty":  len(commissionerReceipts) == 0,
		"matchup_source_label":         matchupLabel,
		"has_matchup_source":           hasMatchupLabel,
	}
}

// PlayersDataReadOnly is the fragment projection contract. It intentionally
// delegates to the existing snapshot-only PlayersData assembly: the caller
// may poll it from another client, but this boundary does not provision a
// member, claim a seat, process waivers, or write persisted league state.
func (s *Service) PlayersDataReadOnly(r *http.Request) map[string]any {
	return s.PlayersData(r)
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
func (s *Service) AddPlayer(r *http.Request, requestedTeam, addID, dropID, confirmation string) (string, error) {
	teamID, err := s.actingTeam(r, requestedTeam) // W1
	if err != nil {
		return "", err
	}
	addID = strings.TrimSpace(addID)
	dropID = strings.TrimSpace(dropID)
	pool := s.pool()
	if err := requirePlayerData(pool); err != nil {
		return "", err
	}
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
	week := lineupCurrentWeekAt(games, now)
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
		if playerLockedForRosterMutation(state, games, week, dropPlayer, now) { // W8
			return "", fmt.Errorf("%s is locked and cannot be dropped until the week closes", dropPlayer.Name)
		}
		txn.Drops = []TransactionPlayer{transactionPlayerFromPlayer(dropPlayer)}
		dropIDs = []string{dropID}
	}
	// W6: F3's fix — credit only a non-IR named drop. The old `else if`
	// here skipped this check entirely whenever any drop was named, on
	// the assumption a drop always frees exactly the one spot the add
	// needs; that assumption is false for an IR occupant, whom
	// effectiveRosterSize already excludes from the count, so dropping
	// one frees no additional spot.
	if effectiveRosterSize(state, teamID)+1-creditedDropCount(state, teamID, dropIDs) > rosterCap {
		return "", fmt.Errorf("your roster is full; choose a player to drop")
	}
	// Limits (optional knob, default off, SK spec).
	if position, limit, breach := teamWouldBreachLimit(state, pool.byID, teamID, []string{addID}, dropIDs); breach {
		return "", fmt.Errorf("%s", limitMessage(position, limit))
	}
	if dropID != "" {
		if err := requireMutationConfirmation(playerAddDropConfirmation, confirmation); err != nil {
			return "", err
		}
	}

	id, err := randomTransactionID()
	if err != nil {
		return "", err
	}
	txn.ID = id
	if err := s.store.RecordTransactionWithAuthority(txn, rosterCap, games, now); err != nil {
		return "", err
	}
	if len(txn.Drops) > 0 {
		return fmt.Sprintf("%s signed; %s dropped.", addPlayer.Name, txn.Drops[0].Name), nil
	}
	return fmt.Sprintf("%s signed.", addPlayer.Name), nil
}

// DropPlayer applies the roster-ops spec section 5.3 player-drop action:
// W1 (signed-in seat), the same post-draft free-agency gate as AddPlayer,
// W7 (ownership), and W8 (lock). Appends one drop
// transaction, one persist. The dropped player leaves the acting team's
// roster the moment this record replays (currentRosters simply stops
// naming them) and enters the ON WAIVERS state by derivation
// (playerWaiverStatus, waivers.go): the same Transaction this call
// appends is what a later playerWaiverStatus call finds via
// lastDropInstant to compute the clear window (section 5.1).
func (s *Service) DropPlayer(r *http.Request, requestedTeam, dropID, confirmation string) (string, error) {
	teamID, err := s.actingTeam(r, requestedTeam) // W1
	if err != nil {
		return "", err
	}
	dropID = strings.TrimSpace(dropID)
	pool := s.pool()
	if err := requirePlayerData(pool); err != nil {
		return "", err
	}
	dropPlayer, ok := pool.byID[dropID]
	if !ok {
		return "", fmt.Errorf("%s", lineupNotOnRosterMessage)
	}
	state := s.store.Snapshot()
	if !draftComplete(state) {
		return "", fmt.Errorf("free agency opens once the draft is complete")
	}
	owner := rosterOwner(currentRosters(state))
	if owner[dropID] != teamID { // W7
		return "", fmt.Errorf("%s", lineupNotOnRosterMessage)
	}
	now := s.clock()
	games := s.schedule()
	week := lineupCurrentWeekAt(games, now)
	if playerLockedForRosterMutation(state, games, week, dropPlayer, now) { // W8
		return "", fmt.Errorf("%s is locked and cannot be dropped until the week closes", dropPlayer.Name)
	}

	if err := requireMutationConfirmation(playerDropConfirmation, confirmation); err != nil {
		return "", err
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
	if err := s.store.RecordTransactionWithAuthority(txn, CurrentRoster().Total(), games, now); err != nil {
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
	if err := requirePlayerData(pool); err != nil {
		return "", err
	}
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
	if _, exists := waiverClaimByTeamAndPlayer(state.WaiverClaims, teamID, addID); exists { // W5
		return "", fmt.Errorf("you already hold a claim for %s", addPlayer.Name)
	}
	// F4: cap this team's open-claim queue at the roster size, for the
	// exact user-facing message — Store.fileClaimWithAuthority repeats
	// this as defense-in-depth for every filing path, including a direct
	// Store caller.
	if teamOpenClaimCount(state.WaiverClaims, teamID) >= maxOpenClaimsPerTeam() {
		return "", fmt.Errorf("you already have the maximum of %d open claims; cancel one before filing another", maxOpenClaimsPerTeam())
	}

	now := s.clock()
	games := s.schedule()
	week := lineupCurrentWeekAt(games, now)
	rosterCap := CurrentRoster().Total()
	claim := WaiverClaim{TeamID: teamID, AddID: addID, FiledAt: now}

	var claimOutgoing []string
	if dropID != "" {
		dropPlayer, ok := pool.byID[dropID]
		if !ok || owner[dropID] != teamID { // W7
			return "", fmt.Errorf("%s", lineupNotOnRosterMessage)
		}
		if playerLockedForRosterMutation(state, games, week, dropPlayer, now) { // W8
			return "", fmt.Errorf("%s is locked and cannot be dropped until the week closes", dropPlayer.Name)
		}
		claim.DropID = dropID
		claimOutgoing = []string{dropID}
	}
	// W6: F3's fix — credit only a non-IR named drop (see AddPlayer's
	// identical fix and creditedDropCount's doc comment, zones.go).
	// Filing-time fail fast; ProcessWaivers re-checks at resolution time,
	// since roster composition may shift between filing and the claim's
	// own run.
	if effectiveRosterSize(state, teamID)+1-creditedDropCount(state, teamID, claimOutgoing) > rosterCap { // W6
		return "", fmt.Errorf("your roster is full; choose a player to drop")
	}
	// Limits (optional knob, default off, SK spec) — filing-time fail
	// fast; ProcessWaivers re-checks at resolution time, since roster
	// composition may shift between filing and the claim's own run.
	if position, limit, breach := teamWouldBreachLimit(state, pool.byID, teamID, []string{addID}, claimOutgoing); breach {
		return "", fmt.Errorf("%s", limitMessage(position, limit))
	}

	if s.cfg.Waivers.Mode == "faab" {
		if bid < 0 { // W10
			return "", fmt.Errorf("bids must be between 0 and %d", s.cfg.Waivers.FAABBudget)
		}
		remaining := faabRemaining(state, s.cfg.Waivers.FAABBudget)[teamID]
		if bid > remaining { // W9
			return "", fmt.Errorf("your bid exceeds your remaining budget (%s left)", faabUnits(remaining))
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
	if err := s.store.FileClaimWithAuthority(claim, games, pool.byID, now); err != nil {
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

// MoveClaim changes one open claim's private, within-team filing order by
// one slot. actingTeam binds the mutation to the authenticated franchise;
// Store.MoveClaim rechecks ownership under the write lock.
func (s *Service) MoveClaim(r *http.Request, requestedTeam, claimID, direction string) (string, error) {
	teamID, err := s.actingTeam(r, requestedTeam)
	if err != nil {
		return "", err
	}
	claimID = strings.TrimSpace(claimID)
	direction = strings.ToLower(strings.TrimSpace(direction))
	moved, err := s.store.MoveClaim(teamID, claimID, direction)
	if err != nil {
		return "", err
	}
	if !moved {
		if direction == "up" {
			return "Claim is already first in your filing order.", nil
		}
		return "Claim is already last in your filing order.", nil
	}
	if direction == "up" {
		return "Claim moved up one position.", nil
	}
	return "Claim moved down one position.", nil
}
