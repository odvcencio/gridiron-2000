// Roster zones (roster-ops SK spec, 2026-08-18 owner decision): the
// RESERVE zone (position-gated, draftable, counts toward Total()) and the
// IR zone (injury-gated, not draftable, sits outside Total()). Both are one
// primitive — a per-team, per-player overlay atop the derived roster
// (currentRosters/Picks+Transactions, roster.go) recording which zone, if
// any, a player currently occupies. Placing or activating a zone occupant
// never touches ownership; it only moves where the player sits. This file
// also carries the optional per-position roster Limits knob, which shares
// the zones' "extend the one validation source" discipline
// (validateZonesAndLimits) even though it is not itself a zone.
package league

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

const (
	// zoneReserve is the position-gated reserve zone's key (ZoneAssignment.Zone).
	zoneReserve = "reserve"
	// zoneIR is the injury-gated IR zone's key (ZoneAssignment.Zone).
	zoneIR = "ir"
)

// ZoneAssignment is one player's placement into a roster zone (RESERVE or
// IR). Position is the player's position snapshotted at placement time —
// display-only and the reserve capacity gate's own record; the live pool
// stays the source of truth for every other gate.
type ZoneAssignment struct {
	Zone     string    `json:"zone"`
	Position string    `json:"position"`
	PlacedAt time.Time `json:"placedAt"`
}

// cloneZoneAssignments deep-copies a team's zone map (nil-safe), matching
// the store's snapshot/clone discipline (see cloneRosterOverride).
func cloneZoneAssignments(in map[string]ZoneAssignment) map[string]ZoneAssignment {
	out := make(map[string]ZoneAssignment, len(in))
	for playerID, za := range in {
		out[playerID] = za
	}
	return out
}

// validPoolPosition reports whether key is one of the seven real player
// positions (players.go's playerPoolPositions) — the key set both the
// reserve zone and the Limits knob gate on. Slot names (FLEX, SUPERFLEX)
// are deliberately excluded: a reserve or limit entry names a player's
// actual position, never a lineup slot.
func validPoolPosition(key string) bool {
	for _, p := range playerPoolPositions {
		if p == key {
			return true
		}
	}
	return false
}

// validateZonesAndLimits is the one validation source Reserve/IR/Limits
// share (extending validateRoster and validateRosterOverride, which both
// call this after their own slot/bench checks — SK spec: "validated by the
// same rules ... extend those rules only where the new concepts require").
// scope names the caller for its exact-message prefix ("league config" or
// "roster shape").
func validateZonesAndLimits(reserve map[string]int, ir int, limits map[string]int, scope string) error {
	for key, count := range reserve {
		if !validPoolPosition(key) {
			return fmt.Errorf("%s: reserve: unknown position %q; valid positions: %s", scope, key, strings.Join(playerPoolPositions, ", "))
		}
		if count < 0 || count > 4 {
			return fmt.Errorf("%s: reserve.%s must be 0 to 4", scope, key)
		}
	}
	if ir < 0 || ir > 10 {
		return fmt.Errorf("%s: ir must be 0 to 10", scope)
	}
	for key, count := range limits {
		if !validPoolPosition(key) {
			return fmt.Errorf("%s: limits: unknown position %q; valid positions: %s", scope, key, strings.Join(playerPoolPositions, ", "))
		}
		if count < 1 || count > 20 {
			return fmt.Errorf("%s: limits.%s must be 1 to 20", scope, key)
		}
	}
	return nil
}

// limitMessage is the one exact message pattern every Limits enforcement
// point returns (SK spec: "one exact message pattern"), at every point —
// draft picks, adds, claims, trade execution, and IR activation.
func limitMessage(position string, limit int) string {
	return fmt.Sprintf("your roster already holds the maximum %d %s", limit, position)
}

// teamWouldBreachLimit reports whether moving incoming positions onto
// teamID's roster (while outgoing positions leave in the same move) would
// push any position past the league's optional Limits cap (SK spec:
// "config limits map position->max across starters+bench+reserve (IR
// exempt) ... absent = unlimited"). state/pool resolve the team's current
// holdings; IR-zoned holdings are excluded from the baseline, matching the
// exemption. Every enforcement point (AddPlayer, ProcessWaivers,
// validateTradeAssets, MakePick, ActivateFromIR) calls this one function,
// so the cap math never drifts into five hand-duplicated rule sets. A nil
// or empty Limits map (the default) always returns false — zero behavior
// change when the knob is absent.
func teamWouldBreachLimit(state PersistedState, pool map[string]Player, teamID string, incoming, outgoing []string) (position string, limit int, breach bool) {
	preset := CurrentRoster()
	if len(preset.Limits) == 0 {
		return "", 0, false
	}
	counts := map[string]int{}
	for _, id := range currentRosters(state)[teamID] {
		if zoneOfPlayer(state, teamID, id) == zoneIR {
			continue
		}
		if p, ok := pool[id]; ok {
			counts[p.Position]++
		}
	}
	for _, id := range outgoing {
		if zoneOfPlayer(state, teamID, id) == zoneIR {
			continue // never counted in the baseline above; do not double-subtract
		}
		if p, ok := pool[id]; ok {
			counts[p.Position]--
		}
	}
	for _, id := range incoming {
		if p, ok := pool[id]; ok {
			counts[p.Position]++
		}
	}
	for pos, count := range counts {
		if lim, capped := preset.Limits[pos]; capped && lim > 0 && count > lim {
			return pos, lim, true
		}
	}
	return "", 0, false
}

// ---------------------------------------------------------------------
// Injury-designation source (IR eligibility gate)
// ---------------------------------------------------------------------

// InjuryDesignationSource supplies one player's current NFL weekly
// injury-report designation, keyed the same way WeekStatsSource and
// HistoricalSource key their rows — normalizePlayerKey(name, position),
// scoped to nflTeam to keep the openstats query cheap (scorer.go's
// documented reason internal/league cannot import internal/openstats
// applies here too: main.go wires the real adapter over the mirror). A
// GSIS-ID join is not viable: internal/fantasy's live Tank01-backed pool
// carries no GSIS ID at all (only the demo fixture, defaultPlayers, sets
// Player.GSISID) — name+position/team is the one join key both providers
// can produce. ok is false when the mirror carries no report for that
// player this season (never listed, or an ID the injuries feed does not
// track — DST and most kickers).
type InjuryDesignationSource func(name, position, nflTeam string) (designation string, ok bool)

// SetInjuryDesignationSource attaches the injury-report lookup used by IR
// placement (PlaceInIR) and the healed-IR ticker (rosterops.go). Call it
// once during startup, before the server accepts requests — the
// SetWeekStatsSource precedent (scorer.go).
func (s *Service) SetInjuryDesignationSource(fn InjuryDesignationSource) {
	s.poolMu.Lock()
	s.injuryFn = fn
	s.poolMu.Unlock()
}

// injuryDesignationSource returns the current injury lookup, or nil when
// none is attached (every test Service literal, and any deployment before
// main.go wires openstats).
func (s *Service) injuryDesignationSource() InjuryDesignationSource {
	s.poolMu.Lock()
	fn := s.injuryFn
	s.poolMu.Unlock()
	return fn
}

// irQualifyingDesignations is the IR eligibility gate's qualifying set
// (SK spec: "define the qualifying set from what the mirror actually
// carries, document it"). The openstats mirror's weekly injury report
// (nflverse injuries dataset, report_status column — internal/openstats'
// InjuryReport.ReportStatus) carries exactly the NFL's official
// three-tier weekly practice-report scale: "Out", "Doubtful",
// "Questionable" (openstats/service_test.go fixes this shape). This rule
// treats "Out" and "Doubtful" as qualifying — both signal the player is
// not expected to play. "Questionable" does not qualify: a Questionable
// player is still live to play that week, so IR (a season-length stash)
// is not the correct zone for him.
var irQualifyingDesignations = map[string]bool{
	"out":      true,
	"doubtful": true,
}

// irQualifies reports whether designation (case-insensitive, trimmed) is
// in irQualifyingDesignations.
func irQualifies(designation string) bool {
	return irQualifyingDesignations[strings.ToLower(strings.TrimSpace(designation))]
}

// irEligible reports whether player currently carries a qualifying injury
// designation, per source. false when source is nil (not wired) or the
// mirror carries no report for this player.
func irEligible(source InjuryDesignationSource, player Player) bool {
	if source == nil {
		return false
	}
	designation, ok := source(player.Name, player.Position, player.NFLTeam)
	return ok && irQualifies(designation)
}

// ---------------------------------------------------------------------
// Pure zone-state helpers (roster.go's currentRosters/rosterOwner
// precedent: pure functions over PersistedState, no Service needed).
// ---------------------------------------------------------------------

// zoneOfPlayer returns the zone playerID occupies for teamID ("reserve",
// "ir"), or "" when the player sits in the general pool (starters/bench).
func zoneOfPlayer(state PersistedState, teamID, playerID string) string {
	if za, ok := state.RosterZones[teamID][playerID]; ok {
		return za.Zone
	}
	return ""
}

// splitRosterZones partitions roster into the general pool (starters/bench
// eligible) and each zone's occupants, preserving roster's own order.
// Every lineup/scoring read that must never see a zone occupant
// (effectiveLineup, autoFillWeek, lineupStarters) calls this first and
// only ever uses general.
func splitRosterZones(state PersistedState, teamID string, roster []Player) (general, reserve, ir []Player) {
	for _, p := range roster {
		switch zoneOfPlayer(state, teamID, p.ID) {
		case zoneReserve:
			reserve = append(reserve, p)
		case zoneIR:
			ir = append(ir, p)
		default:
			general = append(general, p)
		}
	}
	return general, reserve, ir
}

// reserveOccupantCount counts teamID's reserve occupants at position.
func reserveOccupantCount(state PersistedState, teamID, position string) int {
	n := 0
	for _, za := range state.RosterZones[teamID] {
		if za.Zone == zoneReserve && za.Position == position {
			n++
		}
	}
	return n
}

// irOccupantCount counts teamID's IR occupants (any position).
func irOccupantCount(state PersistedState, teamID string) int {
	n := 0
	for _, za := range state.RosterZones[teamID] {
		if za.Zone == zoneIR {
			n++
		}
	}
	return n
}

// effectiveRosterSize is teamID's roster-cap count: ownership
// (currentRosters) minus IR occupants (SK spec: "placing a player in IR
// frees a general roster spot"). Reserve occupants stay counted — reserve
// is in-cap space, only zoned apart from the general bench. Every cap
// check against CurrentRoster().Total() (AddPlayer, FileClaim,
// RecordTransaction, ProcessWaivers) reads this instead of a raw
// len(currentRosters(state)[teamID]), so an IR stash always frees the
// spot it promises. With no IR occupants this equals the raw count
// exactly — zero behavior change for a config that never places one.
func effectiveRosterSize(state PersistedState, teamID string) int {
	return len(currentRosters(state)[teamID]) - irOccupantCount(state, teamID)
}

// nextKickoffGrace is how far into the past a kickoff still counts as
// "the current one" for nextKickoffForTeam — the same 4-hour convention
// pickemWeekAt (pickem.go) already uses for "which NFL week is this."
const nextKickoffGrace = 4 * time.Hour

// nextKickoffForTeam returns nflTeam's relevant kickoff for the SK IR
// rule's deadline anchor ("before his NFL team's next kickoff"): the
// earliest kickoff at or after (now - nextKickoffGrace), across every
// week the schedule mirror carries. The grace window matters for
// evalHealedIR's auto-cut branch specifically: a strictly-future-only
// filter would make that branch unreachable the instant now first
// crosses the deadline kickoff — precisely the instant it needs to fire.
// Past the grace window (a missed tick, real ticker downtime, ...) this
// naturally falls forward to the *next* scheduled kickoff instead of a
// long-stale one, which correctly restarts the due-window/deadline cycle
// against that team's next real game rather than silently auto-cutting
// (or silently never resolving) against ancient history.
func nextKickoffForTeam(games []GameInfo, nflTeam string, now time.Time) (time.Time, bool) {
	cutoff := now.Add(-nextKickoffGrace)
	var best time.Time
	found := false
	for _, g := range games {
		if g.Away != nflTeam && g.Home != nflTeam {
			continue
		}
		if g.Kickoff.Before(cutoff) {
			continue
		}
		if !found || g.Kickoff.Before(best) {
			best = g.Kickoff
			found = true
		}
	}
	return best, found
}

// ---------------------------------------------------------------------
// Store: zone persistence
// ---------------------------------------------------------------------

// PlaceInZone assigns playerID into zone (zoneReserve or zoneIR) for
// teamID, one lock, one persist. It re-validates under the lock —
// RecordTransaction's re-validate-under-lock discipline — that playerID
// is still owned by teamID and not already zoned; capacity and
// position/injury gating (the exact-message business rules) are the
// Service layer's job (PlaceInReserve/PlaceInIR), matching the
// FileClaim/Service.FileClaim split: Store stays generic, defense in
// depth only.
func (s *Store) PlaceInZone(teamID, playerID, zone, position string, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	owner := rosterOwner(currentRosters(s.state))
	if owner[playerID] != teamID {
		return fmt.Errorf("%s", lineupNotOnRosterMessage)
	}
	if _, already := s.state.RosterZones[teamID][playerID]; already {
		return fmt.Errorf("that player already sits in a roster zone")
	}
	if s.state.RosterZones == nil {
		s.state.RosterZones = map[string]map[string]ZoneAssignment{}
	}
	if s.state.RosterZones[teamID] == nil {
		s.state.RosterZones[teamID] = map[string]ZoneAssignment{}
	}
	s.state.RosterZones[teamID][playerID] = ZoneAssignment{Zone: zone, Position: position, PlacedAt: now.UTC()}
	return s.persistLocked()
}

// ClearZone removes playerID's zone assignment for teamID, one lock, one
// persist. It re-validates that the player currently occupies expectZone
// — a stale double-submit (activating an already-activated player) is
// rejected rather than silently no-op, matching the SetLineupSlot/
// SetRosterOverride discipline of failing closed on a state mismatch.
func (s *Store) ClearZone(teamID, playerID, expectZone string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	za, ok := s.state.RosterZones[teamID][playerID]
	if !ok || za.Zone != expectZone {
		return fmt.Errorf("%s", lineupNotOnRosterMessage)
	}
	delete(s.state.RosterZones[teamID], playerID)
	return s.persistLocked()
}

// ActivateFromIRWithDrop clears playerID's IR zone assignment and appends
// dropTxn (a normal "drop" Transaction for a different, named player) in
// one lock, one persist — the SK spec's "activated with a corresponding
// drop" move. It re-validates under the lock that playerID is still on
// teamID's IR and that every dropTxn.Drops player is still owned by
// teamID.
func (s *Store) ActivateFromIRWithDrop(teamID, playerID string, dropTxn Transaction) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if za, ok := s.state.RosterZones[teamID][playerID]; !ok || za.Zone != zoneIR {
		return fmt.Errorf("%s", lineupNotOnRosterMessage)
	}
	owner := rosterOwner(currentRosters(s.state))
	for _, drop := range dropTxn.Drops {
		if owner[drop.PlayerID] != teamID {
			return fmt.Errorf("%s", lineupNotOnRosterMessage)
		}
	}
	delete(s.state.RosterZones[teamID], playerID)
	dropTxn.At = dropTxn.At.UTC()
	s.state.Transactions = append(s.state.Transactions, dropTxn)
	return s.persistLocked()
}

// AutoCutHealedIR appends one "auto-drop" Transaction and clears
// player's IR zone assignment for teamID, one lock, one persist (SK IR
// rule: "the platform auto-cuts him — typed auto-drop transaction — one
// atomic persist"). It re-validates under the lock that player is still
// on teamID's IR, so a manager's own concurrent activate-from-IR always
// wins the race, never a double cut; this is also the mechanism's
// idempotency guard — once cut, the player is no longer a zone occupant,
// so a repeat tick that still holds a stale snapshot naturally no-ops on
// its next fresh read (evalHealedIR, rosterops.go).
func (s *Store) AutoCutHealedIR(teamID string, player TransactionPlayer, season, week int, now time.Time) (Transaction, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if za, ok := s.state.RosterZones[teamID][player.PlayerID]; !ok || za.Zone != zoneIR {
		return Transaction{}, fmt.Errorf("%s is no longer on IR", player.Name)
	}
	delete(s.state.RosterZones[teamID], player.PlayerID)
	id, err := randomTransactionID()
	if err != nil {
		return Transaction{}, err
	}
	txn := Transaction{
		ID:     id,
		Season: season,
		Week:   week,
		Type:   "auto-drop",
		TeamID: teamID,
		Drops:  []TransactionPlayer{player},
		By:     "system",
		At:     now.UTC(),
	}
	s.state.Transactions = append(s.state.Transactions, txn)
	if err := s.persistLocked(); err != nil {
		return Transaction{}, err
	}
	return txn, nil
}

// clearZoneAssignmentsLocked removes every named player's zone assignment
// for teamID. The caller must already hold s.mu. Every ownership-changing
// write that can move a zoned player off a roster (RecordTransaction's
// drops, ProcessWaivers' claim-drop, ExecuteTradeOffer's both sides) calls
// this so a dropped or traded player never leaves an orphaned zone tag
// behind for a future re-add to trip over.
func (s *Store) clearZoneAssignmentsLocked(teamID string, playerIDs []string) {
	zones := s.state.RosterZones[teamID]
	if len(zones) == 0 {
		return
	}
	for _, id := range playerIDs {
		delete(zones, id)
	}
}

// ---------------------------------------------------------------------
// Service: zone actions (managed forms on the team page)
// ---------------------------------------------------------------------

// PlaceInReserve applies the reserve-zone placement action: the acting
// team's own general-pool player (starters/bench) moves into the
// position-gated reserve zone. Validates, in order: signed-in seat,
// ownership, not already zoned, the player's position is one of the
// league's configured reserve keys, and that position's reserve count
// still has room. Reserve occupants keep counting toward the roster cap
// — this is a re-zone, not an add, so it never touches Transactions.
func (s *Service) PlaceInReserve(r *http.Request, requestedTeam, playerID string) (string, error) {
	teamID, err := s.actingTeam(r, requestedTeam)
	if err != nil {
		return "", err
	}
	playerID = strings.TrimSpace(playerID)
	pool := s.pool()
	player, ok := pool.byID[playerID]
	if !ok {
		return "", fmt.Errorf("%s", lineupNotOnRosterMessage)
	}
	state := s.store.Snapshot()
	owner := rosterOwner(currentRosters(state))
	if owner[playerID] != teamID {
		return "", fmt.Errorf("%s", lineupNotOnRosterMessage)
	}
	if zoneOfPlayer(state, teamID, playerID) != "" {
		return "", fmt.Errorf("%s already sits in a roster zone", player.Name)
	}
	preset := CurrentRoster()
	capacity, gated := preset.Reserve[player.Position]
	if !gated || capacity <= 0 {
		return "", fmt.Errorf("%s does not qualify for the reserve zone", player.Position)
	}
	if reserveOccupantCount(state, teamID, player.Position) >= capacity {
		return "", fmt.Errorf("the reserve zone is full for %s", player.Position)
	}
	if err := s.store.PlaceInZone(teamID, playerID, zoneReserve, player.Position, s.clock()); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s moved to reserve.", player.Name), nil
}

// PlaceInIR applies the IR-zone placement action: the acting team's own
// general-pool player moves into the injury-gated IR zone, freeing a
// general roster spot for the season. Validates, in order: signed-in
// seat, ownership, not already zoned, the league carries an IR zone at
// all, the zone still has room, and the player currently carries a
// qualifying injury designation (irEligible) from the wired openstats
// mirror at this instant — "at placement time" (SK spec).
func (s *Service) PlaceInIR(r *http.Request, requestedTeam, playerID string) (string, error) {
	teamID, err := s.actingTeam(r, requestedTeam)
	if err != nil {
		return "", err
	}
	playerID = strings.TrimSpace(playerID)
	pool := s.pool()
	player, ok := pool.byID[playerID]
	if !ok {
		return "", fmt.Errorf("%s", lineupNotOnRosterMessage)
	}
	state := s.store.Snapshot()
	owner := rosterOwner(currentRosters(state))
	if owner[playerID] != teamID {
		return "", fmt.Errorf("%s", lineupNotOnRosterMessage)
	}
	if zoneOfPlayer(state, teamID, playerID) != "" {
		return "", fmt.Errorf("%s already sits in a roster zone", player.Name)
	}
	preset := CurrentRoster()
	if preset.IR <= 0 {
		return "", fmt.Errorf("this league does not carry an IR zone")
	}
	if irOccupantCount(state, teamID) >= preset.IR {
		return "", fmt.Errorf("the IR zone is full")
	}
	if !irEligible(s.injuryDesignationSource(), player) {
		return "", fmt.Errorf("%s does not carry a qualifying injury designation", player.Name)
	}
	if err := s.store.PlaceInZone(teamID, playerID, zoneIR, player.Position, s.clock()); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s moved to IR.", player.Name), nil
}

// ActivateFromReserve returns a reserve occupant to the general roster
// (starters/bench). Reserve is already in-cap space, so this never needs
// a drop and never changes the roster's total occupied count.
func (s *Service) ActivateFromReserve(r *http.Request, requestedTeam, playerID string) (string, error) {
	teamID, err := s.actingTeam(r, requestedTeam)
	if err != nil {
		return "", err
	}
	playerID = strings.TrimSpace(playerID)
	pool := s.pool()
	player, ok := pool.byID[playerID]
	if !ok {
		return "", fmt.Errorf("%s", lineupNotOnRosterMessage)
	}
	state := s.store.Snapshot()
	if zoneOfPlayer(state, teamID, playerID) != zoneReserve {
		return "", fmt.Errorf("%s", lineupNotOnRosterMessage)
	}
	if err := s.store.ClearZone(teamID, playerID, zoneReserve); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s activated from reserve.", player.Name), nil
}

// ActivateFromIR applies the IR-activation action (SK IR rule: "must be
// activated (with a corresponding drop) before his NFL team's next
// kickoff"). IR sits outside Total(), so bringing the player back into
// the cap needs a same-move drop whenever the team is already at cap;
// dropID is optional otherwise. Also re-checks the optional Limits knob
// against the activated player's position (IR is exempt from Limits
// while stashed, but re-entering the counted roster is not).
func (s *Service) ActivateFromIR(r *http.Request, requestedTeam, playerID, dropID string) (string, error) {
	teamID, err := s.actingTeam(r, requestedTeam)
	if err != nil {
		return "", err
	}
	playerID = strings.TrimSpace(playerID)
	dropID = strings.TrimSpace(dropID)
	pool := s.pool()
	player, ok := pool.byID[playerID]
	if !ok {
		return "", fmt.Errorf("%s", lineupNotOnRosterMessage)
	}
	state := s.store.Snapshot()
	if zoneOfPlayer(state, teamID, playerID) != zoneIR {
		return "", fmt.Errorf("%s", lineupNotOnRosterMessage)
	}
	rosters := currentRosters(state)
	owner := rosterOwner(rosters)
	rosterCap := CurrentRoster().Total()
	effective := effectiveRosterSize(state, teamID)
	now := s.clock()
	games := s.schedule()
	week := s.pickemWeek(games, now)

	if dropID != "" {
		dropPlayer, ok := pool.byID[dropID]
		if !ok || owner[dropID] != teamID || zoneOfPlayer(state, teamID, dropID) == zoneIR {
			return "", fmt.Errorf("%s", lineupNotOnRosterMessage)
		}
		if playerLocked(games, week, dropPlayer.NFLTeam, now) {
			return "", fmt.Errorf("%s is locked and cannot be dropped until the week closes", dropPlayer.Name)
		}
		if position, limit, breach := teamWouldBreachLimit(state, pool.byID, teamID, []string{playerID}, []string{dropID}); breach {
			return "", fmt.Errorf("%s", limitMessage(position, limit))
		}
		txn := Transaction{
			Season: s.cfg.Season, Week: week, Type: "drop", TeamID: teamID,
			Drops: []TransactionPlayer{transactionPlayerFromPlayer(dropPlayer)}, By: "manager", At: now,
		}
		id, err := randomTransactionID()
		if err != nil {
			return "", err
		}
		txn.ID = id
		if err := s.store.ActivateFromIRWithDrop(teamID, playerID, txn); err != nil {
			return "", err
		}
		return fmt.Sprintf("%s activated from IR; %s dropped.", player.Name, dropPlayer.Name), nil
	}

	if effective+1 > rosterCap {
		return "", fmt.Errorf("your roster is full; choose a player to drop")
	}
	if position, limit, breach := teamWouldBreachLimit(state, pool.byID, teamID, []string{playerID}, nil); breach {
		return "", fmt.Errorf("%s", limitMessage(position, limit))
	}
	if err := s.store.ClearZone(teamID, playerID, zoneIR); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s activated from IR.", player.Name), nil
}
