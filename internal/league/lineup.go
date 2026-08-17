package league

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// This file carries the roster-ops spec's slot and preset DATA only
// (section 4.1, 4.1.1) as WP-R0's draft-critical pre-draft package: the
// house roster shape must be authoritative before the 2026-08-22 draft
// records its first pick. No lineup engine reads this table yet — slot
// derivation, effective-lineup resolution, locks, and auto-fill land in
// WP-R1 (internal/league/lineup.go gains that behavior in the same file).

// SlotDef is one named lineup slot: its eligible positions, in the fixed
// engine order every future lineup feature (auto-fill, N18, the team page)
// walks. FLEX and SUPERFLEX intentionally exclude P — punters keep their
// own slot (owner direction, roster-ops spec section 4.1).
type SlotDef struct {
	// Key is the slot's config and display key ("QB", "FLEX", "SUPERFLEX",
	// "P", ...).
	Key string
	// Eligible lists the positions that may fill this slot.
	Eligible []string
}

// Eligible reports whether position may fill this slot.
func (s SlotDef) Fits(position string) bool {
	for _, eligible := range s.Eligible {
		if eligible == position {
			return true
		}
	}
	return false
}

// slotTable is the section 4.1 slot model, in engine order. A preset's
// slots map (below) selects a subset of these keys with a count; a count
// above one numbers the slot IDs (RB1, RB2, ...) — that numbering and every
// other engine behavior is WP-R1 scope.
var slotTable = []SlotDef{
	{Key: "QB", Eligible: []string{"QB"}},
	{Key: "RB", Eligible: []string{"RB"}},
	{Key: "WR", Eligible: []string{"WR"}},
	{Key: "TE", Eligible: []string{"TE"}},
	{Key: "FLEX", Eligible: []string{"RB", "WR", "TE"}},
	{Key: "SUPERFLEX", Eligible: []string{"QB", "RB", "WR", "TE"}},
	{Key: "DST", Eligible: []string{"DST"}},
	{Key: "K", Eligible: []string{"K"}},
	{Key: "P", Eligible: []string{"P"}},
}

// slotByKey looks up one slot definition by its key.
func slotByKey(key string) (SlotDef, bool) {
	for _, slot := range slotTable {
		if slot.Key == key {
			return slot, true
		}
	}
	return SlotDef{}, false
}

// RosterPreset names a commissioner-selectable roster shape (roster-ops
// spec section 4.1.1): a starters slot count plus a bench size. Slots keys
// come from slotTable; a key absent from Slots carries a zero count.
type RosterPreset struct {
	Name  string
	Slots map[string]int
	Bench int
}

// Starters returns the preset's total starter count (the sum of every
// slot's count).
func (p RosterPreset) Starters() int {
	total := 0
	for _, count := range p.Slots {
		total += count
	}
	return total
}

// Total returns the preset's full roster size: starters plus bench. The
// slot-count/draft-round equality rule (roster-ops spec section 10) requires
// this to equal DraftRounds for whichever preset is active.
func (p RosterPreset) Total() int {
	return p.Starters() + p.Bench
}

// rosterPresets is every named preset the roster-ops spec section 4.1.1
// defines. config.go's LoadConfig selects among these by name
// (roster.preset) and validates draft.rounds against the chosen preset's
// Total(); resolveRosterBlock and validateRoster are the call sites.
var rosterPresets = map[string]RosterPreset{
	// gridiron-house: the reference league (owner decision). Superflex QB
	// value plus a startable punter for an 8-team league. 11 starters + 6
	// bench = 17; requires draft.rounds 17.
	"gridiron-house": {
		Name: "gridiron-house",
		Slots: map[string]int{
			"QB": 1, "RB": 2, "WR": 2, "TE": 1, "FLEX": 1,
			"SUPERFLEX": 1, "K": 1, "P": 1, "DST": 1,
		},
		Bench: 6,
	},
	// standard: the conventional 8-12 team shape. 9 starters + 6 bench =
	// 15; matches draft.rounds 15.
	"standard": {
		Name: "standard",
		Slots: map[string]int{
			"QB": 1, "RB": 2, "WR": 2, "TE": 1, "FLEX": 1, "DST": 1, "K": 1,
		},
		Bench: 6,
	},
	// superflex: QB value elevated, no punter. 10 starters + 5 bench = 15;
	// matches draft.rounds 15.
	"superflex": {
		Name: "superflex",
		Slots: map[string]int{
			"QB": 1, "RB": 2, "WR": 2, "TE": 1, "FLEX": 1,
			"SUPERFLEX": 1, "DST": 1, "K": 1,
		},
		Bench: 5,
	},
	// deep-league: recommended at 13-14 teams. 10 starters + 4 bench = 14;
	// requires draft.rounds 14.
	"deep-league": {
		Name: "deep-league",
		Slots: map[string]int{
			"QB": 1, "RB": 2, "WR": 2, "TE": 1, "FLEX": 1,
			"SUPERFLEX": 1, "DST": 1, "K": 1,
		},
		Bench: 4,
	},
}

// ActiveRosterPreset is the league's active roster shape: config-derived
// (productization spec section 3.4, roster-ops spec section 10). It starts
// at the neutral shipped default (standard) and is mutated exactly once,
// at boot, by applyActiveConfig — see that function's doc comment in
// config.go for why no test may call it. The reference deployment's own
// league.json selects gridiron-house (owner decision, roster-ops spec
// section 4.1.1) via roster.preset; an absent roster block in a config
// *file* still resolves to gridiron-house (resolveRosterBlock's fallback,
// config.go) — only the *compiled*, no-file-at-all default is neutral.
var ActiveRosterPreset = DefaultConfig().Roster

// ---------------------------------------------------------------------
// Runtime roster-shape override (commissioner roster-shape editor)
// ---------------------------------------------------------------------

// RosterOverride is a commissioner-chosen roster shape, persisted in
// PersistedState and applied on top of the config-resolved default
// (ActiveRosterPreset/DraftRounds). It carries a complete shape: Slots and
// Bench replace the config's, they do not merge with it, the same
// "explicit shape" contract RosterBlock's slots/bench pair already uses in
// config.go.
type RosterOverride struct {
	Slots map[string]int `json:"slots"`
	Bench int            `json:"bench"`
}

// cloneRosterOverride deep-copies o (nil-safe), matching the store's
// snapshot/clone discipline (see cloneSchedule, clonePlayoffState).
func cloneRosterOverride(o *RosterOverride) *RosterOverride {
	if o == nil {
		return nil
	}
	out := &RosterOverride{Bench: o.Bench, Slots: make(map[string]int, len(o.Slots))}
	for key, count := range o.Slots {
		out.Slots[key] = count
	}
	return out
}

// rosterOverridePreset renders o as a RosterPreset, for CurrentRoster and
// the admin console's computed summary line.
func rosterOverridePreset(o RosterOverride) RosterPreset {
	return RosterPreset{Name: "custom", Slots: o.Slots, Bench: o.Bench}
}

// validateRosterOverride is the roster-shape editor's single validation
// source (roster-ops spec section 10 numbers, reused verbatim): every slot
// key must be one slotTable names (validRosterSlotKeys, config.go); each
// slot count is 0-4; at least 1 QB; RB plus WR at least 2; starters total
// at least 6; bench 0-10; the full roster (starters plus bench) 10-25.
// Store.SetRosterOverride is the only caller today, keeping validation in
// one place the way validateDraftOrder backs SetDraftOrder.
func validateRosterOverride(o RosterOverride) error {
	for key, count := range o.Slots {
		if !validSlotKey(key) {
			return fmt.Errorf("roster shape: unknown slot %q; valid slots: %s", key, strings.Join(validRosterSlotKeys, ", "))
		}
		if count < 0 || count > 4 {
			return fmt.Errorf("roster shape: %s must be 0 to 4", key)
		}
	}
	preset := RosterPreset{Slots: o.Slots}
	if o.Slots["QB"] < 1 {
		return fmt.Errorf("roster shape: QB must be at least 1")
	}
	if o.Slots["RB"]+o.Slots["WR"] < 2 {
		return fmt.Errorf("roster shape: RB plus WR must total at least 2")
	}
	if preset.Starters() < 6 {
		return fmt.Errorf("roster shape: starters must total at least 6")
	}
	if o.Bench < 0 || o.Bench > 10 {
		return fmt.Errorf("roster shape: bench must be 0 to 10")
	}
	total := preset.Starters() + o.Bench
	if total < 10 || total > 25 {
		return fmt.Errorf("roster shape: total roster size (starters plus bench) must be 10 to 25; got %d", total)
	}
	return nil
}

// rosterRuntimeMu guards the runtime-mutable roster shape below. Every read
// site outside config.go's boot path (applyActiveConfig) and this file's
// own accessors should read CurrentRoster/CurrentDraftRounds instead of the
// raw ActiveRosterPreset/DraftRounds package vars: those two stay in place,
// mutated exactly once at boot, for compatibility and as the "config
// baseline" clearRosterShape restores; they simply stop being the whole
// story once a commissioner applies an override.
var (
	rosterRuntimeMu    sync.RWMutex
	runtimeRoster      RosterPreset
	runtimeDraftRounds int
	runtimeRosterSet   bool
)

// CurrentRoster returns the league's roster shape as of right now: the
// commissioner's runtime override when one is applied, otherwise the
// config-resolved default (ActiveRosterPreset).
func CurrentRoster() RosterPreset {
	rosterRuntimeMu.RLock()
	defer rosterRuntimeMu.RUnlock()
	if runtimeRosterSet {
		return runtimeRoster
	}
	return ActiveRosterPreset
}

// CurrentDraftRounds returns the league's draft-round count as of right
// now, mirroring CurrentRoster's override precedence. Draft rounds are
// always derived from the active roster's Total(), never set
// independently, so this and CurrentRoster().Total() always agree.
func CurrentDraftRounds() int {
	rosterRuntimeMu.RLock()
	defer rosterRuntimeMu.RUnlock()
	if runtimeRosterSet {
		return runtimeDraftRounds
	}
	return DraftRounds
}

// setRosterShape applies preset as the runtime roster override: every
// CurrentRoster/CurrentDraftRounds call reflects it immediately. Draft
// rounds derive from preset.Total(), never set independently. Called at
// boot (Default(), once a persisted override is found) and by
// Service.AdminSetRosterShape after Store.SetRosterOverride succeeds.
func setRosterShape(preset RosterPreset) {
	rosterRuntimeMu.Lock()
	runtimeRoster = preset
	runtimeDraftRounds = preset.Total()
	runtimeRosterSet = true
	rosterRuntimeMu.Unlock()
}

// clearRosterShape reverts CurrentRoster/CurrentDraftRounds to the config
// baseline (ActiveRosterPreset/DraftRounds). Called by
// Service.AdminResetRosterShape after Store.ClearRosterOverride succeeds.
func clearRosterShape() {
	rosterRuntimeMu.Lock()
	runtimeRosterSet = false
	rosterRuntimeMu.Unlock()
}

// ---------------------------------------------------------------------
// WP-R1: lineup engine (roster-ops spec section 4) — slot IDs, effective
// lineup, locks, auto-fill, and the lineup-set/lineup-auto actions.
//
// Reconciliation note (report this at hand-off): every slot derivation
// below reads CurrentRoster(), never ActiveRosterPreset directly, so a
// commissioner's runtime roster-shape override (roster-shape-editor spec,
// landed on main ahead of this work package) is authoritative for lineup
// slots exactly the way it already is for the roster-shape strip
// (service.go's rosterShapeRows/rosterShapeSummary).
// ---------------------------------------------------------------------

// SlotInstance is one starting slot in a resolved lineup shape (roster-ops
// spec section 4.1): its ID and eligibility definition. A slot key whose
// configured count is 1 keeps the bare key as its ID ("QB", "TE", "FLEX",
// ...); a count above 1 numbers each instance ("RB1", "RB2", ...), 1-based,
// in slotTable engine order.
type SlotInstance struct {
	ID  string
	Def SlotDef
}

// lineupSlots expands preset's slot counts into numbered slot instances, in
// slotTable engine order — the section 4.1 numbering rule, generic over any
// preset (the house shape, a 2QB league, a commissioner's runtime
// override, ...).
func lineupSlots(preset RosterPreset) []SlotInstance {
	out := make([]SlotInstance, 0, preset.Starters())
	for _, def := range slotTable {
		count := preset.Slots[def.Key]
		if count <= 0 {
			continue
		}
		if count == 1 {
			out = append(out, SlotInstance{ID: def.Key, Def: def})
			continue
		}
		for i := 1; i <= count; i++ {
			out = append(out, SlotInstance{ID: fmt.Sprintf("%s%d", def.Key, i), Def: def})
		}
	}
	return out
}

// lineupSlotByID looks up one slot instance by its ID within preset's
// resolved shape.
func lineupSlotByID(preset RosterPreset, id string) (SlotInstance, bool) {
	for _, slot := range lineupSlots(preset) {
		if slot.ID == id {
			return slot, true
		}
	}
	return SlotInstance{}, false
}

// playerLockAt resolves the kickoff instant of nflTeam's week-W game
// (roster-ops spec section 4.3): the pick'em lock rule (pickem.go's
// GameInfo.Kickoff check) applied per player instead of per game. ok is
// false when nflTeam has no week-W game (a bye) — a bye player never
// kickoff-locks.
func playerLockAt(games []GameInfo, week int, nflTeam string) (time.Time, bool) {
	for _, g := range games {
		if g.Week != week {
			continue
		}
		if g.Away == nflTeam || g.Home == nflTeam {
			return g.Kickoff, true
		}
	}
	return time.Time{}, false
}

// playerLocked reports whether a player on nflTeam is locked for week as of
// now: their week-W kickoff has passed. A bye player is never locked.
func playerLocked(games []GameInfo, week int, nflTeam string, now time.Time) bool {
	kickoff, ok := playerLockAt(games, week, nflTeam)
	return ok && !now.Before(kickoff)
}

// slotWarnsBye reports whether player's bye week matches week (roster-ops
// spec section 4.5).
func slotWarnsBye(player Player, week int) bool {
	return player.ByeWeek == week
}

// injuryWarnPrefixes are the section 4.5 case-insensitive prefixes that
// mark a player's free-text Injury field as a lineup warning. An unknown
// string never warns — the field is free text (fact 23).
var injuryWarnPrefixes = []string{"Out", "IR", "Doubtful"}

// slotWarnsInjury reports whether player's Injury text starts with one of
// injuryWarnPrefixes, case-insensitive (roster-ops spec section 4.5).
func slotWarnsInjury(player Player) bool {
	injury := strings.TrimSpace(player.Injury)
	if injury == "" {
		return false
	}
	for _, prefix := range injuryWarnPrefixes {
		if len(injury) >= len(prefix) && strings.EqualFold(injury[:len(prefix)], prefix) {
			return true
		}
	}
	return false
}

// bestAutoFillCandidate implements the section 4.7 pick rule over an
// already-filtered candidate pool (eligible for the slot, unassigned, and
// unlocked for week): the highest-projection player whose week is not a
// bye and whose injury does not warn; when none qualifies, the
// highest-projection eligible player anyway. Ties break toward the lower
// player ID, for determinism. ok is false when candidates is empty.
func bestAutoFillCandidate(candidates []Player, week int) (player Player, ok bool) {
	var clean, all []Player
	for _, p := range candidates {
		all = append(all, p)
		if !slotWarnsBye(p, week) && !slotWarnsInjury(p) {
			clean = append(clean, p)
		}
	}
	pick := clean
	if len(pick) == 0 {
		pick = all
	}
	if len(pick) == 0 {
		return Player{}, false
	}
	best := pick[0]
	for _, p := range pick[1:] {
		if p.Projection > best.Projection || (p.Projection == best.Projection && p.ID < best.ID) {
			best = p
		}
	}
	return best, true
}

// SlotAssignment is one resolved starting slot (roster-ops spec section
// 4.2): its instance, the player in it (if any), whether that player was
// placed by the auto-fill algorithm rather than an explicit lineup-set /
// lineup-auto write, and its lock/warning state for week.
type SlotAssignment struct {
	Slot       SlotInstance
	Player     Player
	HasPlayer  bool
	AutoFilled bool
	Locked     bool
	WarnBye    bool
	WarnInjury bool
}

// EffectiveLineup is one team's resolved starting lineup and bench for one
// week (roster-ops spec section 4.2).
type EffectiveLineup struct {
	Week  int
	Slots []SlotAssignment
	Bench []Player
}

// effectiveLineup resolves teamID's starting lineup for week (roster-ops
// spec section 4.2).
//
// Reconciliation (report this at hand-off): the spec's literal algorithm
// treats the first stored week found walking back from week to 1 as a
// complete, final base — auto-fill only runs when *no* week has ever been
// stored at all. Taken literally, one lineup-set write to a single slot
// (worked example 11.1: "the store writes Lineups[team-3][1][RB2] =
// white, one persist") would make every *other* slot of that week read
// back empty forever after, which contradicts the same example's own next
// beat (Warren still resolves at week close) and this work package's own
// brief ("auto-fill for unassigned slots"). This implementation resolves
// per slot instead: the first stored week's map supplies whichever slots
// it explicitly names; every slot that map does not name auto-fills
// independently, at read time, on every render. An empty (or absent)
// stored map is the literal spec's "no stored week exists" case restated
// as this rule's zero value, so no separate branch is needed for it.
//
// roster is teamID's current roster (Service.rosterForTeam); stored is
// PersistedState.Lineups[teamID] (nil is valid — no week ever stored);
// games backs the per-player lock check (nil is valid — nothing locks).
func effectiveLineup(preset RosterPreset, roster []Player, stored map[int]map[string]string, week int, games []GameInfo, now time.Time) EffectiveLineup {
	slots := lineupSlots(preset)
	byID := make(map[string]Player, len(roster))
	for _, p := range roster {
		byID[p.ID] = p
	}

	// Walk weeks back to 1 for the first explicit (stored) week's map.
	var explicit map[string]string
	for w := week; w >= 1; w-- {
		if m, ok := stored[w]; ok {
			explicit = m
			break
		}
	}

	taken := make(map[string]bool, len(slots))
	assignments := make([]SlotAssignment, len(slots))
	for i, slot := range slots {
		assignments[i] = SlotAssignment{Slot: slot}
		playerID := explicit[slot.ID]
		if playerID == "" {
			continue
		}
		player, ok := byID[playerID]
		if !ok {
			// Step 3: a trade or drop emptied the slot — the stored
			// pointer survives, but the roster no longer backs it.
			continue
		}
		assignments[i].Player = player
		assignments[i].HasPlayer = true
		taken[player.ID] = true
	}

	// Auto-fill every remaining gap, in engine order, so a slot with no
	// explicit entry always resolves the same way the spec's "no stored
	// week" case does — see the reconciliation note above.
	for i := range assignments {
		if assignments[i].HasPlayer {
			continue
		}
		slot := assignments[i].Slot
		var candidates []Player
		for _, p := range roster {
			if taken[p.ID] || !slot.Def.Fits(p.Position) || playerLocked(games, week, p.NFLTeam, now) {
				continue
			}
			candidates = append(candidates, p)
		}
		best, ok := bestAutoFillCandidate(candidates, week)
		if !ok {
			continue
		}
		assignments[i].Player = best
		assignments[i].HasPlayer = true
		assignments[i].AutoFilled = true
		taken[best.ID] = true
	}

	for i := range assignments {
		if !assignments[i].HasPlayer {
			continue
		}
		p := assignments[i].Player
		assignments[i].Locked = playerLocked(games, week, p.NFLTeam, now)
		assignments[i].WarnBye = slotWarnsBye(p, week)
		assignments[i].WarnInjury = slotWarnsInjury(p)
	}

	bench := make([]Player, 0, len(roster))
	for _, p := range roster {
		if !taken[p.ID] {
			bench = append(bench, p)
		}
	}
	sort.Slice(bench, func(i, j int) bool { return bench[i].ID < bench[j].ID })

	return EffectiveLineup{Week: week, Slots: assignments, Bench: bench}
}

// slotAssignment finds slotID's resolved assignment within lineup, if any.
func (l EffectiveLineup) slotAssignment(slotID string) (SlotAssignment, bool) {
	for _, a := range l.Slots {
		if a.Slot.ID == slotID {
			return a, true
		}
	}
	return SlotAssignment{}, false
}

// autoFillWeek implements the section 4.7 SET BEST LINEUP algorithm:
// unlike effectiveLineup's per-slot gap fill (which leaves an explicit,
// unlocked-but-suboptimal pick — a bye or an injury — untouched), this
// recomputes every *unlocked* slot from scratch in engine order, the
// one-tap fix the spec's rationale names. A currently locked slot keeps
// its resolved occupant untouched; the action never errors on a lock. The
// result is a full slot-ID -> player-ID map, meant to replace the team's
// entire stored week (Store.SetLineupWeek) so a locked slot's occupant —
// excluded from every future auto-fill candidate pool by definition —
// stays pinned in storage rather than silently dropping out.
func autoFillWeek(preset RosterPreset, roster []Player, current EffectiveLineup, week int, games []GameInfo, now time.Time) map[string]string {
	slots := lineupSlots(preset)
	taken := make(map[string]bool, len(slots))
	resolved := make(map[string]string, len(slots))
	for _, a := range current.Slots {
		if a.HasPlayer && a.Locked {
			resolved[a.Slot.ID] = a.Player.ID
			taken[a.Player.ID] = true
		}
	}
	for _, slot := range slots {
		if _, pinned := resolved[slot.ID]; pinned {
			continue
		}
		var candidates []Player
		for _, p := range roster {
			if taken[p.ID] || !slot.Def.Fits(p.Position) || playerLocked(games, week, p.NFLTeam, now) {
				continue
			}
			candidates = append(candidates, p)
		}
		best, ok := bestAutoFillCandidate(candidates, week)
		if !ok {
			continue
		}
		resolved[slot.ID] = best.ID
		taken[best.ID] = true
	}
	return resolved
}

// ---------------------------------------------------------------------
// Exact validation messages (roster-ops spec section 4.4, table L1-L8).
// ---------------------------------------------------------------------

func lineupWeekClosedMessage(week int) string {
	return fmt.Sprintf("week %d is closed; lineups can no longer change", week)
}

func lineupUnknownSlotMessage(slot string) string {
	return fmt.Sprintf("unknown lineup slot %q", slot)
}

const lineupNotOnRosterMessage = "that player is not on your roster"

func lineupPositionMessage(position, slotID string) string {
	return fmt.Sprintf("%s does not fit the %s slot", position, slotID)
}

func lineupLockedMessage(name string, week int, nflTeam string) string {
	return fmt.Sprintf("%s is locked for week %d; the %s game has kicked off", name, week, nflTeam)
}

// lineupActingTeam resolves which team a lineup action targets and whether
// the requester may act for it: the signed-in seat's own team
// (Service.actingTeam — roster-ops spec section 4.4, L1), or, per this
// work package's explicit test-plan direction, a commissioner acting on a
// named seat's behalf (dead-manager insurance). The roster-ops spec's
// section 14 open question 2 ("should the commissioner set another team's
// lineup?") default is "v1 says no"; this work package's brief overrides
// that default to "yes" — report the reconciliation at hand-off.
func (s *Service) lineupActingTeam(r *http.Request, requestedTeam string) (string, error) {
	if s.IsCommissioner(r) && knownTeam(strings.TrimSpace(requestedTeam)) {
		return strings.TrimSpace(requestedTeam), nil
	}
	return s.actingTeam(r, requestedTeam)
}

// SetLineup applies one lineup-set(week, slot, player_id) action
// (roster-ops spec section 4.4): validates L1-L8 in order and, on success,
// persists the assignment. An empty playerID clears the slot (L8); a
// non-empty playerID that already holds another explicit slot this week
// displaces from it in the same store write (Store.SetLineupSlot) — a
// player occupies at most one slot.
func (s *Service) SetLineup(r *http.Request, requestedTeam string, week int, slotID, playerID string) (string, error) {
	teamID, err := s.lineupActingTeam(r, requestedTeam) // L1
	if err != nil {
		return "", err
	}
	now := s.clock()
	state := s.store.Snapshot()
	games := s.schedule()
	currentWeek := s.pickemWeek(games, now)
	if week < currentWeek { // L2
		return "", fmt.Errorf("%s", lineupWeekClosedMessage(week))
	}
	preset := CurrentRoster()
	slot, ok := lineupSlotByID(preset, slotID) // L3
	if !ok {
		return "", fmt.Errorf("%s", lineupUnknownSlotMessage(slotID))
	}
	roster, _ := s.rosterForTeam(state, teamID)
	byID := make(map[string]Player, len(roster))
	for _, p := range roster {
		byID[p.ID] = p
	}
	current := effectiveLineup(preset, roster, state.Lineups[teamID], week, games, now)
	occupant, _ := current.slotAssignment(slot.ID)

	playerID = strings.TrimSpace(playerID)
	if playerID == "" {
		if occupant.HasPlayer && occupant.Locked { // L8
			return "", fmt.Errorf("%s", lineupLockedMessage(occupant.Player.Name, week, occupant.Player.NFLTeam))
		}
		if err := s.store.SetLineupSlot(teamID, week, slot.ID, "", now); err != nil {
			return "", err
		}
		return fmt.Sprintf("%s cleared.", slot.ID), nil
	}

	player, ok := byID[playerID]
	if !ok { // L4
		return "", fmt.Errorf("%s", lineupNotOnRosterMessage)
	}
	if !slot.Def.Fits(player.Position) { // L5
		return "", fmt.Errorf("%s", lineupPositionMessage(player.Position, slot.ID))
	}
	if playerLocked(games, week, player.NFLTeam, now) { // L6
		return "", fmt.Errorf("%s", lineupLockedMessage(player.Name, week, player.NFLTeam))
	}
	if occupant.HasPlayer && occupant.Player.ID != player.ID && occupant.Locked { // L7
		return "", fmt.Errorf("%s", lineupLockedMessage(occupant.Player.Name, week, occupant.Player.NFLTeam))
	}
	if err := s.store.SetLineupSlot(teamID, week, slot.ID, player.ID, now); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s starts at %s.", player.Name, slot.ID), nil
}

// LineupAuto applies the section 4.7 SET BEST LINEUP action for teamID's
// week: recomputes every unlocked slot via autoFillWeek and replaces the
// team's stored week in one write (Store.SetLineupWeek). It never errors
// on a lock — locked slots simply keep their current occupant.
func (s *Service) LineupAuto(r *http.Request, requestedTeam string, week int) (string, error) {
	teamID, err := s.lineupActingTeam(r, requestedTeam) // L1
	if err != nil {
		return "", err
	}
	now := s.clock()
	state := s.store.Snapshot()
	games := s.schedule()
	currentWeek := s.pickemWeek(games, now)
	if week < currentWeek { // L2
		return "", fmt.Errorf("%s", lineupWeekClosedMessage(week))
	}
	preset := CurrentRoster()
	roster, _ := s.rosterForTeam(state, teamID)
	current := effectiveLineup(preset, roster, state.Lineups[teamID], week, games, now)
	resolved := autoFillWeek(preset, roster, current, week, games, now)
	if err := s.store.SetLineupWeek(teamID, week, resolved); err != nil {
		return "", err
	}
	return "Your best lineup is set.", nil
}

// effectiveLineupForTeam snapshots state and resolves teamID's effective
// lineup for week — the one call site TeamData and the lineup actions'
// view-model builders share.
func (s *Service) effectiveLineupForTeam(state PersistedState, teamID string, week int) EffectiveLineup {
	preset := CurrentRoster()
	roster, _ := s.rosterForTeam(state, teamID)
	games := s.schedule()
	now := s.clock()
	return effectiveLineup(preset, roster, state.Lineups[teamID], week, games, now)
}

// ---------------------------------------------------------------------
// Team-page view model (app/team's lineup surface — managed forms only,
// no client JS: every choice is a <select> plus a submit button, per this
// work package's owner directive).
// ---------------------------------------------------------------------

// lineupSlotOptions builds one starting slot's <select> option list: a
// leading "clear the slot" option, then every roster player who fits the
// slot and is not locked for week, highest projection first (ties by
// name). currentID is the slot's currently resolved player ID, if any —
// its option is marked selected; an empty currentID selects the clear
// option instead. A locked player never appears: L6 forbids moving a
// locked player into any slot, so offering one in the list would only
// invite a rejected submit.
func lineupSlotOptions(roster []Player, slot SlotInstance, week int, games []GameInfo, now time.Time, currentID string) []map[string]any {
	eligible := make([]Player, 0, len(roster))
	for _, p := range roster {
		if !slot.Def.Fits(p.Position) || playerLocked(games, week, p.NFLTeam, now) {
			continue
		}
		eligible = append(eligible, p)
	}
	sort.Slice(eligible, func(i, j int) bool {
		if eligible[i].Projection != eligible[j].Projection {
			return eligible[i].Projection > eligible[j].Projection
		}
		return eligible[i].Name < eligible[j].Name
	})
	out := make([]map[string]any, 0, len(eligible)+1)
	out = append(out, map[string]any{"id": "", "label": "— EMPTY —", "selected": currentID == ""})
	for _, p := range eligible {
		out = append(out, map[string]any{
			"id":       p.ID,
			"label":    fmt.Sprintf("%s · %s · %s", p.Name, p.Position, p.NFLTeam),
			"selected": p.ID == currentID,
		})
	}
	return out
}

// slotWarningLabel renders one occupied-or-empty slot's warning chip text
// (roster-ops spec section 4.5), reusing the N18 catalog's own issue
// vocabulary (BYE / OUT / EMPTY, spec section 9) even though N18 itself is
// WP-R2 scope. An empty slot always warns; an occupied slot warns on bye
// before injury when — rare — both apply (an arbitrary but deterministic
// tie-break; nothing in the spec orders the two).
func slotWarningLabel(a SlotAssignment) (label string, warns bool) {
	if !a.HasPlayer {
		return "EMPTY", true
	}
	if a.WarnBye {
		return "BYE", true
	}
	if a.WarnInjury {
		return "OUT", true
	}
	return "", false
}

// starterRowMaps renders lineup's starting slots as view-model maps for
// app/team's per-slot assignment forms: each row carries the assigned
// player (playerMap's shape, merged in) or an empty state, plus the
// lock/warning/auto-fill chips and, when unlocked, the eligible-player
// <select> options (lineupSlotOptions).
func (s *Service) starterRowMaps(lineup EffectiveLineup, roster []Player, games []GameInfo, now time.Time, scoringValues map[string]float64) []map[string]any {
	location := s.draftTZ
	if location == nil {
		location, _ = time.LoadLocation(DefaultDraftTZ)
	}
	out := make([]map[string]any, 0, len(lineup.Slots))
	for _, a := range lineup.Slots {
		row := map[string]any{
			"slot_id":     a.Slot.ID,
			"has_player":  a.HasPlayer,
			"auto_filled": a.AutoFilled,
			"locked":      a.Locked,
			"lock_label":  "",
		}
		currentID := ""
		if a.HasPlayer {
			currentID = a.Player.ID
			for k, v := range playerMap(a.Player, scoringValues) {
				row[k] = v
			}
			if a.Locked {
				if kickoff, ok := playerLockAt(games, lineup.Week, a.Player.NFLTeam); ok {
					row["lock_label"] = "LOCKED · " + kickoff.In(location).Format("3:04 PM MST")
				}
			}
		}
		label, warns := slotWarningLabel(a)
		row["has_warning"] = warns
		row["warning_label"] = label
		row["options"] = lineupSlotOptions(roster, a.Slot, lineup.Week, games, now, currentID)
		out = append(out, row)
	}
	return out
}
