package league

import (
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"
)

// rosterCorrectionReasonMaxLength caps the commissioner's required audit
// reason (build brief: "reason non-empty (<= 240 chars)").
const rosterCorrectionReasonMaxLength = 240

// RosterCorrectionResult names every entity AdminRosterCorrection actually
// touched, for the console's own confirmation notice: the affected team,
// the added and dropped players (either side may be empty), the reason,
// and the one shared plain-language Summary every surface (the console
// redirect notice, the /activity commissioner-event row, and the /team
// one-time flash) renders unchanged.
type RosterCorrectionResult struct {
	TeamID   string
	TeamName string
	AddID    string
	AddName  string
	AddPos   string
	DropID   string
	DropName string
	DropPos  string
	Reason   string
	Summary  string
}

// rosterCorrectionClause renders the shared "adds A (POS), drops B (POS) —
// reason: ..." clause. adds/drops carry at most one TransactionPlayer each
// (a correction moves at most one player per side); an empty side is
// simply omitted, so a drop-only or add-only correction still reads
// naturally. Every roster-correction surface (the commissioner event
// summary, the /team one-time flash, and this package's own tests) calls
// this one function, so the three can never drift on wording.
func rosterCorrectionClause(adds, drops []TransactionPlayer, reason string) string {
	parts := make([]string, 0, 2)
	if add := transactionPlayerNames(adds); add != "" {
		parts = append(parts, "adds "+add)
	}
	if drop := transactionPlayerNames(drops); drop != "" {
		parts = append(parts, "drops "+drop)
	}
	clause := strings.Join(parts, ", ")
	if reason != "" {
		clause += " — reason: " + reason
	}
	return clause
}

// rosterCorrection is resolveRosterCorrection's whole validated outcome:
// the atomic Transaction ready to record, and the RosterCorrectionResult
// every surface (the console notice, the review-confirm summary, the
// commissioner-event audit row, and the /team flash) renders from.
type rosterCorrection struct {
	txn    Transaction
	result RosterCorrectionResult
}

// resolveRosterCorrection is AdminRosterCorrection and
// AdminRosterCorrectionPreview's one shared validation path: commissioner
// authorization, a known team, at least one of dropPlayerID/addPlayerID
// named, a non-empty reason within rosterCorrectionReasonMaxLength, live
// player data, the named drop (rostered by this team, not locked by a
// kicked-off game this week — the same playerLockedForRosterMutation
// predicate AddPlayer/DropPlayer already use), the named add (an actual
// free agent — not on any roster, not on waivers pending), and the
// post-move roster shape/limits. It never writes anything; the caller
// decides whether to commit.
func (s *Service) resolveRosterCorrection(r *http.Request, teamID, dropPlayerID, addPlayerID, reason string) (rosterCorrection, error) {
	if err := s.requireCommissioner(r); err != nil {
		return rosterCorrection{}, err
	}
	teamID = strings.TrimSpace(teamID)
	dropPlayerID = strings.TrimSpace(dropPlayerID)
	addPlayerID = strings.TrimSpace(addPlayerID)
	reason = strings.TrimSpace(reason)

	if !knownTeam(teamID) {
		return rosterCorrection{}, fmt.Errorf("choose a team")
	}
	if dropPlayerID == "" && addPlayerID == "" {
		return rosterCorrection{}, fmt.Errorf("choose a player to drop, add, or both")
	}
	if reason == "" {
		return rosterCorrection{}, fmt.Errorf("a reason is required")
	}
	if len([]rune(reason)) > rosterCorrectionReasonMaxLength {
		return rosterCorrection{}, fmt.Errorf("reason must be %d characters or fewer", rosterCorrectionReasonMaxLength)
	}

	pool := s.pool()
	if err := requirePlayerData(pool); err != nil {
		return rosterCorrection{}, err
	}
	state := s.store.Snapshot()
	// A roster correction only ever makes sense once real rosters exist —
	// the build brief's own motivating cases are both post-draft mistakes.
	// This mirrors AddPlayer/DropPlayer's identical pre-draft gate
	// (players.go): before the draft completes, currentRosters is empty
	// and "free agent" is not yet a meaningful state for any player.
	if !draftComplete(state) {
		return rosterCorrection{}, fmt.Errorf("free agency opens once the draft is complete")
	}
	owner := rosterOwner(currentRosters(state))
	now := s.clock()
	games := s.schedule()
	week := lineupCurrentWeekAt(games, now)

	txn := Transaction{
		Season: s.cfg.Season,
		Week:   week,
		Type:   "commissioner_correction",
		TeamID: teamID,
		By:     "commissioner",
		Note:   reason,
		At:     now,
	}

	var dropPlayer Player
	if dropPlayerID != "" {
		var ok bool
		dropPlayer, ok = pool.byID[dropPlayerID]
		if !ok || owner[dropPlayerID] != teamID {
			return rosterCorrection{}, fmt.Errorf("%s", lineupNotOnRosterMessage)
		}
		if playerLockedForRosterMutation(state, games, week, dropPlayer, now) {
			return rosterCorrection{}, fmt.Errorf("%s is locked and cannot be dropped until the week closes", dropPlayer.Name)
		}
		txn.Drops = []TransactionPlayer{transactionPlayerFromPlayer(dropPlayer)}
	}

	var addPlayer Player
	if addPlayerID != "" {
		var ok bool
		addPlayer, ok = pool.byID[addPlayerID]
		if !ok {
			return rosterCorrection{}, fmt.Errorf("choose an available player")
		}
		if owner[addPlayerID] != "" {
			return rosterCorrection{}, fmt.Errorf("%s is already on a roster", addPlayer.Name)
		}
		status := playerWaiverStatus(state, s.cfg, games, addPlayerID, addPlayer.NFLTeam, now)
		if status.State == AvailabilityOnWaivers {
			resolves := formatResolvesAt(s.cfg, status.ResolvesAt)
			if status.Reason == "kickoff" {
				return rosterCorrection{}, fmt.Errorf("%s locked at kickoff; the claim resolves %s", addPlayer.Name, resolves)
			}
			return rosterCorrection{}, fmt.Errorf("%s is on waivers; claims resolve %s", addPlayer.Name, resolves)
		}
		txn.Adds = []TransactionPlayer{transactionPlayerFromPlayer(addPlayer)}
	}

	rosterCap := CurrentRoster().Total()
	var dropIDs []string
	if dropPlayerID != "" {
		dropIDs = []string{dropPlayerID}
	}
	adds := 0
	if addPlayerID != "" {
		adds = 1
	}
	if effectiveRosterSize(state, teamID)+adds-creditedDropCount(state, teamID, dropIDs) > rosterCap {
		return rosterCorrection{}, fmt.Errorf("the roster is full; choose a player to drop")
	}
	if addPlayerID != "" {
		if position, limit, breach := teamWouldBreachLimit(state, pool.byID, teamID, []string{addPlayerID}, dropIDs); breach {
			return rosterCorrection{}, fmt.Errorf("%s", limitMessage(position, limit))
		}
	}

	team := s.teamByID(teamID)
	clause := rosterCorrectionClause(txn.Adds, txn.Drops, reason)
	result := RosterCorrectionResult{
		TeamID: teamID, TeamName: team.Name, Reason: reason,
		Summary: fmt.Sprintf("corrects %s: %s", team.Name, clause),
	}
	if addPlayerID != "" {
		result.AddID, result.AddName, result.AddPos = addPlayer.ID, addPlayer.Name, addPlayer.Position
	}
	if dropPlayerID != "" {
		result.DropID, result.DropName, result.DropPos = dropPlayer.ID, dropPlayer.Name, dropPlayer.Position
	}
	return rosterCorrection{txn: txn, result: result}, nil
}

// AdminRosterCorrectionPreview runs AdminRosterCorrection's whole
// validation path and returns the resolved RosterCorrectionResult WITHOUT
// writing anything — the console's review step calls this to resolve real
// team and player names for its confirmation copy before the commissioner
// ever commits. Calling it twice, or calling it and never confirming, has
// no effect on league state.
func (s *Service) AdminRosterCorrectionPreview(r *http.Request, teamID, dropPlayerID, addPlayerID, reason string) (RosterCorrectionResult, error) {
	resolved, err := s.resolveRosterCorrection(r, teamID, dropPlayerID, addPlayerID, reason)
	if err != nil {
		return RosterCorrectionResult{}, err
	}
	return resolved.result, nil
}

// AdminRosterCorrection lets the commissioner correct a named team's
// roster on that team's own behalf — an immediate, review-confirm action
// (the build brief's product-contract classification: no waiver period,
// no typed confirmation, but the console's own two-step form flow
// restates the change, naming the team and both players, before this
// call ever runs — see AdminRosterCorrectionPreview and app/admin's
// "roster-correction" action). Unlike AddPlayer/DropPlayer (players.go),
// which bind to the signed-in manager's own seat through actingTeam, this
// targets an explicitly named team — the commissioner is correcting a bad
// autopick or a roster mistake FOR another franchise, most often right
// after the draft (the build brief's own motivating cases: a
// six-quarterback seat and a reached-for rookie).
//
// Re-validates through the exact same path AdminRosterCorrectionPreview
// used to build the confirmation copy (resolveRosterCorrection), so a
// stale confirm — league state moved between the review and the confirm
// request — fails closed with a fresh, honest error instead of committing
// a correction the commissioner never actually reviewed. The roster write
// itself goes through Store.RecordTransactionWithAuthority — the same
// store path AddPlayer/DropPlayer use — as one atomic Transaction of the
// new "commissioner_correction" kind, so the affected team's roster, the
// transaction ledger, and every replay-derived view (currentRosters,
// activity, receipts) update in the same single write. A
// RecordCommissionerEvent audit row and the affected team's one-time
// /team flash are best-effort follow-ups after that write commits — see
// RecordCommissionerEvent's own doc comment for why a failure there must
// never roll back or block the already-applied roster change.
func (s *Service) AdminRosterCorrection(r *http.Request, teamID, dropPlayerID, addPlayerID, reason string) (RosterCorrectionResult, error) {
	resolved, err := s.resolveRosterCorrection(r, teamID, dropPlayerID, addPlayerID, reason)
	if err != nil {
		return RosterCorrectionResult{}, err
	}
	txn := resolved.txn
	result := resolved.result

	id, err := randomTransactionID()
	if err != nil {
		return RosterCorrectionResult{}, err
	}
	txn.ID = id
	// now is txn.At itself (resolveRosterCorrection's own single s.clock()
	// read) rather than a fresh call: RecordTransactionWithAuthority's
	// kickoff re-validation and the notice's own timestamp both describe
	// this one correction instant, not two microseconds apart.
	now := txn.At
	rosterCap := CurrentRoster().Total()
	games := s.schedule()
	if err := s.store.RecordTransactionWithAuthority(txn, rosterCap, games, now); err != nil {
		return RosterCorrectionResult{}, err
	}

	if _, err := s.RecordCommissionerEvent(r, "roster.correction", result.Summary, CommissionerEventRefs{TeamID: result.TeamID, Week: txn.Week}); err != nil {
		log.Printf("commissioner event: roster.correction: %v", err)
	}
	clause := rosterCorrectionClause(txn.Adds, txn.Drops, result.Reason)
	teamNotice := "The commissioner corrected your roster: " + clause
	if err := s.store.SetRosterCorrectionNotice(result.TeamID, teamNotice, now); err != nil {
		log.Printf("roster correction notice: %v", err)
	}
	return result, nil
}

// rosterCorrectionAddOptionCap bounds the free-agent add list the console
// renders (the pool can hold hundreds of free agents once the roster
// isn't full-league drafted): the same order of magnitude /players' own
// pool page renders per page.
const rosterCorrectionAddOptionCap = 50

// AdminRosterCorrectionData assembles the /admin "Roster correction" panel
// (06 // ROSTER SHAPE section): every team by its real, live name; once a
// team is chosen (?correction_team=), that team's own roster as drop
// candidates — a locked player stays selectable in the <select> markup
// (removing the option would silently hide it) but is rendered
// disabled="disabled" with its lock reason folded into the option's own
// text, the only way an individual <option> can carry an adjacent,
// accessible reason; and the free-agent pool as add candidates, sorted by
// projection (highest first) and optionally narrowed to one position
// through a plain GET form (?correction_pos=). This never mutates
// anything; AdminData (admin.go) calls it once per render, the same
// read-only-projection shape every other admin panel data method uses.
func (s *Service) AdminRosterCorrectionData(r *http.Request) map[string]any {
	state := s.store.Snapshot()
	teamID := strings.TrimSpace(r.URL.Query().Get("correction_team"))
	pos := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("correction_pos")))

	teamOptions := make([]map[string]any, 0, len(s.Teams()))
	for _, team := range s.Teams() {
		view := s.teamView(state, team.ID)
		teamOptions = append(teamOptions, map[string]any{
			"id": team.ID, "label": view.Name, "selected": team.ID == teamID,
		})
	}
	hasTeam := knownTeam(teamID)
	draftOpen := draftComplete(state)

	dropOptions := make([]map[string]any, 0)
	addOptions := make([]map[string]any, 0)
	teamName := ""
	if hasTeam {
		teamName = s.teamView(state, teamID).Name
	}
	posTabs := make([]map[string]any, 0, len(playerPoolPositions)+1)

	if hasTeam && draftOpen {
		pool := s.pool()
		if !playerPoolIsUnavailable(pool) {
			now := s.clock()
			games := s.schedule()
			week := lineupCurrentWeekAt(games, now)
			owner := rosterOwner(currentRosters(state))

			for _, playerID := range currentRosters(state)[teamID] {
				player, ok := pool.byID[playerID]
				if !ok {
					continue
				}
				locked := playerLockedForRosterMutation(state, games, week, player, now)
				label := fmt.Sprintf("%s (%s)", player.Name, player.Position)
				if locked {
					label += " — DROP LOCKED: " + lineupLockedMessage(player.Name, week, player.NFLTeam)
				}
				dropOptions = append(dropOptions, map[string]any{
					"id": player.ID, "label": label, "disabled": locked,
				})
			}

			candidates := make([]Player, 0, len(pool.players))
			for _, player := range pool.players {
				if owner[player.ID] != "" {
					continue
				}
				if pos != "" && pos != "ALL" && player.Position != pos {
					continue
				}
				status := playerWaiverStatus(state, s.cfg, games, player.ID, player.NFLTeam, now)
				if status.State != AvailabilityFreeAgent {
					continue
				}
				candidates = append(candidates, player)
			}
			sort.SliceStable(candidates, func(i, j int) bool {
				return candidates[i].Projection > candidates[j].Projection
			})
			if len(candidates) > rosterCorrectionAddOptionCap {
				candidates = candidates[:rosterCorrectionAddOptionCap]
			}
			for _, player := range candidates {
				addOptions = append(addOptions, map[string]any{
					"id": player.ID,
					"label": fmt.Sprintf("%s (%s · %s) — %.1f proj pts",
						player.Name, player.Position, player.NFLTeam, player.Projection),
				})
			}
		}

		posTabs = append(posTabs, map[string]any{"label": "ALL", "value": "", "selected": pos == ""})
		for _, candidate := range playerPoolPositions {
			posTabs = append(posTabs, map[string]any{"label": candidate, "value": candidate, "selected": pos == candidate})
		}
	}

	return map[string]any{
		"team_options":       teamOptions,
		"selected_team_id":   teamID,
		"selected_team_name": teamName,
		"has_team":           hasTeam,
		"draft_complete":     draftOpen,
		"drop_options":       dropOptions,
		"drop_options_empty": len(dropOptions) == 0,
		"add_options":        addOptions,
		"add_options_empty":  len(addOptions) == 0,
		"add_options_capped": len(addOptions) >= rosterCorrectionAddOptionCap,
		"pos_tabs":           posTabs,
		"pos":                pos,
		"reason_max_length":  rosterCorrectionReasonMaxLength,
	}
}

// SetRosterCorrectionNotice records teamID's one pending /team flash,
// overwriting any earlier unread notice (a second correction before the
// manager's next visit replaces, rather than queues behind, the first —
// the manager still gets the latest true state, and a queue of stale
// flashes would only invite confusion).
func (s *Store) SetRosterCorrectionNotice(teamID, summary string, now time.Time) error {
	if !knownTeam(teamID) {
		return fmt.Errorf("unknown team %q", teamID)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeErrorLocked(); err != nil {
		return err
	}
	if s.state.RosterCorrectionNotices == nil {
		s.state.RosterCorrectionNotices = map[string]RosterCorrectionNotice{}
	}
	s.state.RosterCorrectionNotices[teamID] = RosterCorrectionNotice{TeamID: teamID, Summary: summary, At: now}
	return s.persistLocked(colScalars)
}

// ConsumeRosterCorrectionNotice pops teamID's pending notice, if any: the
// /team page (Service.teamData) calls this once per full-page load for
// the viewer's own claimed team, so the flash renders exactly once, on
// the manager's next visit, and is gone after that — the same "read it
// and it clears" mailbox idiom as an ordinary flash message, just scoped
// to the team rather than one browser session, since the commissioner
// (not the affected manager) is the request that created it. err follows
// this package's own checked-write-gate convention (AssignMember, for
// one); a caller treats a non-nil err the same as ok == false — the
// notice, if any, simply was not popped this time.
func (s *Store) ConsumeRosterCorrectionNotice(teamID string) (notice RosterCorrectionNotice, ok bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeErrorLocked(); err != nil {
		return RosterCorrectionNotice{}, false, err
	}
	notice, ok = s.state.RosterCorrectionNotices[teamID]
	if !ok {
		return RosterCorrectionNotice{}, false, nil
	}
	delete(s.state.RosterCorrectionNotices, teamID)
	if err := s.persistLocked(colScalars); err != nil {
		return notice, true, err
	}
	return notice, true, nil
}
