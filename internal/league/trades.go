// Trade offers: the propose/counter/decline/withdraw/accept/approve/veto/
// execute/expire lifecycle (roster-ops spec section 6). Player-for-player
// only in v1; Picks is reserved and rejected when non-empty (section 6.2).
// Content validation (T4-T8, T12) is one function, validateTradeAssets,
// called at propose, at accept, and at execution (store.go's
// ProposeTradeOffer/AcceptTradeOffer/ExecuteTradeOffer) — "applied at
// propose, re-applied at accept, re-applied at execution" is structural,
// not three hand-copied rule sets.
package league

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"gridiron-2000/internal/emailkit"
)

// Trade offer statuses (roster-ops spec section 6.1's transitions table).
const (
	TradeStatusOpen      = "open"
	TradeStatusAccepted  = "accepted"
	TradeStatusExecuted  = "executed"
	TradeStatusDeclined  = "declined"
	TradeStatusWithdrawn = "withdrawn"
	TradeStatusCountered = "countered"
	TradeStatusVetoed    = "vetoed"
	TradeStatusExpired   = "expired"
	TradeStatusFailed    = "failed"
)

// tradeOfferMaxAge is the fixed 7-day expiry (section 6.1's T-expire step:
// "fixed 7 days; not a knob").
const tradeOfferMaxAge = 7 * 24 * time.Hour

// tradeNoteMaxRunes is T14's note-length bound (section 6.3).
const tradeNoteMaxRunes = 280

// TradeOffer is one trade proposal and its section 6.1 lifecycle (verbatim
// shape). Picks is reserved for the first dynasty rollover (section 6.2)
// and always rejected non-empty in v1 (T12). Vetoes names the team IDs
// that have filed a veto vote, in vote and both modes (section 6.1's
// owner amendment — "Vetoes doubles for both mode").
type TradeOffer struct {
	ID         string    `json:"id"` // "trd-" + 8 random hex
	FromTeamID string    `json:"fromTeamId"`
	ToTeamID   string    `json:"toTeamId"`
	Give       []string  `json:"give"`            // player IDs From sends
	Get        []string  `json:"get"`             // player IDs To sends
	Picks      []string  `json:"picks,omitempty"` // DraftPickAsset IDs; rejected in v1 (T12)
	Note       string    `json:"note,omitempty"`  // 280 chars max, escaped at render
	Status     string    `json:"status"`
	ParentID   string    `json:"parentId,omitempty"` // set on a counter
	Vetoes     []string  `json:"vetoes,omitempty"`   // team IDs; vote and both modes
	FailReason string    `json:"failReason,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
	AcceptedAt time.Time `json:"acceptedAt,omitzero"`
	ResolvedAt time.Time `json:"resolvedAt,omitzero"`
}

// randomTradeOfferID draws "trd-" + 8 random hex characters from
// crypto/rand, the same shape randomTransactionID/randomClaimID use.
func randomTradeOfferID() (string, error) {
	var buf [4]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return "trd-" + hex.EncodeToString(buf[:]), nil
}

// tradeVetoThreshold resolves ceil((seats - 2) / 2) — section 6.1's veto
// threshold: one veto per non-party seat, "3 of 6" (6 non-party seats)
// under the reference 8-team league. Integer ceil((n-2)/2) == (n-1)/2 for
// every n >= 2 (n-2 is always non-negative, so the +1-then-halve identity
// applies directly); a league below 2 seats cannot exist, so 0 is a safe,
// unreachable floor.
func tradeVetoThreshold(seatCount int) int {
	if seatCount < 2 {
		return 0
	}
	return (seatCount - 1) / 2
}

// teamNameByID resolves a team ID to its display name for an exact-message
// interpolation (T5, T7); an unknown ID returns itself rather than panic,
// so a malformed offer still renders a message instead of crashing.
func teamNameByID(id string) string {
	for _, team := range defaultTeams() {
		if team.ID == id {
			return team.Name
		}
	}
	return id
}

// parseTradeDeadline parses cfg.Trades.Deadline (RFC3339), returning
// (zero, false) when empty or unparseable — "empty means no deadline"
// (section 6.4); validateConfig already rejects an unparseable non-empty
// value at boot, so the false branch here is defense only.
func parseTradeDeadline(cfg Config) (time.Time, bool) {
	deadline := strings.TrimSpace(cfg.Trades.Deadline)
	if deadline == "" {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339, deadline)
	if err != nil {
		return time.Time{}, false
	}
	return parsed, true
}

// cleanTradePlayerIDs trims, drops empties, and de-duplicates a submitted
// player-ID list (a give/get checkbox group, or a picks list — always
// empty in v1).
func cleanTradePlayerIDs(ids []string) []string {
	out := make([]string, 0, len(ids))
	seen := map[string]bool{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// validateTradeAsset applies T5 (ownership) and T6 (no locked players) to
// one player ID against expectedTeamID.
func validateTradeAsset(games []GameInfo, poolByID map[string]Player, week int, now time.Time, owner map[string]string, playerID, expectedTeamID, teamName string) error {
	name := playerID
	player, known := poolByID[playerID]
	if known {
		name = player.Name
	}
	if owner[playerID] != expectedTeamID { // T5
		return fmt.Errorf("%s is not on %s's roster", name, teamName)
	}
	if known && playerLocked(games, week, player.NFLTeam, now) { // T6
		return fmt.Errorf("%s is locked until the week closes", name)
	}
	return nil
}

// validateTradeAssets applies section 6.3's content rules — T12 (no pick
// assets), T4 (both sides non-empty), T5 (ownership, both sides), T6 (no
// locked players), T7 (roster bounds, both teams), T8 (before the
// deadline) — against offer, over one (state, cfg, games, pool) snapshot.
// This is the single function propose, accept, and execution all call
// (store.go), so "applied at propose, re-applied at accept, re-applied at
// execution" never drifts into three separately maintained rule sets.
func validateTradeAssets(state PersistedState, cfg Config, games []GameInfo, poolByID map[string]Player, now time.Time, offer TradeOffer, starterCount, rosterCap int) error {
	if len(offer.Picks) > 0 { // T12
		return fmt.Errorf("pick trading opens after the first season rollover")
	}
	if len(offer.Give) == 0 || len(offer.Get) == 0 { // T4
		return fmt.Errorf("offer at least one player from each side")
	}
	rosters := currentRosters(state)
	owner := rosterOwner(rosters)
	week := pickemWeekAt(games, now)
	fromName := teamNameByID(offer.FromTeamID)
	toName := teamNameByID(offer.ToTeamID)
	for _, id := range offer.Give {
		if err := validateTradeAsset(games, poolByID, week, now, owner, id, offer.FromTeamID, fromName); err != nil {
			return err
		}
	}
	for _, id := range offer.Get {
		if err := validateTradeAsset(games, poolByID, week, now, owner, id, offer.ToTeamID, toName); err != nil {
			return err
		}
	}
	// T7's bound reads the effective (IR-excluding) roster size (SK spec:
	// "placing a player in IR frees a general roster spot"). A Give/Get
	// player who is themself an IR occupant on the sending side does not
	// count against that side's effective size in the first place, so it
	// is excluded from the outgoing delta too; the receiving side always
	// lands the player un-zoned (general pool), fully counted. With no IR
	// occupants this is exactly the pre-zones formula.
	giveIR := countInZone(state, offer.FromTeamID, offer.Give, zoneIR)
	getIR := countInZone(state, offer.ToTeamID, offer.Get, zoneIR)
	fromEffective := effectiveRosterSize(state, offer.FromTeamID)
	fromNext := fromEffective - (len(offer.Give) - giveIR) + len(offer.Get)
	if fromNext < starterCount || fromNext > rosterCap { // T7
		return fmt.Errorf("this trade would leave %s with %d players; rosters must hold %d to %d", fromName, fromNext, starterCount, rosterCap)
	}
	toEffective := effectiveRosterSize(state, offer.ToTeamID)
	toNext := toEffective - (len(offer.Get) - getIR) + len(offer.Give)
	if toNext < starterCount || toNext > rosterCap { // T7
		return fmt.Errorf("this trade would leave %s with %d players; rosters must hold %d to %d", toName, toNext, starterCount, rosterCap)
	}
	// Limits (optional knob, default off, SK spec): each side's incoming
	// players must not push any position past its configured cap.
	if position, limit, breach := teamWouldBreachLimit(state, poolByID, offer.FromTeamID, offer.Get, offer.Give); breach {
		return fmt.Errorf("%s", limitMessage(position, limit))
	}
	if position, limit, breach := teamWouldBreachLimit(state, poolByID, offer.ToTeamID, offer.Give, offer.Get); breach {
		return fmt.Errorf("%s", limitMessage(position, limit))
	}
	if deadline, ok := parseTradeDeadline(cfg); ok && !now.Before(deadline) { // T8
		return fmt.Errorf("the trade deadline (%s) has passed", formatResolvesAt(cfg, deadline))
	}
	return nil
}

// countInZone counts how many of ids currently sit in zone for teamID.
func countInZone(state PersistedState, teamID string, ids []string, zone string) int {
	n := 0
	for _, id := range ids {
		if zoneOfPlayer(state, teamID, id) == zone {
			n++
		}
	}
	return n
}

// findTradeOffer looks up one offer by ID in an already-taken snapshot.
func findTradeOffer(offers []TradeOffer, offerID string) (TradeOffer, bool) {
	for _, offer := range offers {
		if offer.ID == offerID {
			return offer, true
		}
	}
	return TradeOffer{}, false
}

// transactionPlayersFromIDs snapshots each player ID's display identity
// (section 7.1's TransactionPlayer); an ID absent from the pool is skipped
// rather than emitting a blank row — validateTradeAssets's ownership check
// already guarantees every Give/Get ID resolves against the pool by the
// time execution reaches this call.
func transactionPlayersFromIDs(poolByID map[string]Player, ids []string) []TransactionPlayer {
	out := make([]TransactionPlayer, 0, len(ids))
	for _, id := range ids {
		if player, ok := poolByID[id]; ok {
			out = append(out, transactionPlayerFromPlayer(player))
		}
	}
	return out
}

// tradePlayerNames joins each ID's pool display name ("Name, Name"),
// falling back to the raw ID when the pool does not know it.
func tradePlayerNames(poolByID map[string]Player, ids []string) string {
	names := make([]string, 0, len(ids))
	for _, id := range ids {
		if player, ok := poolByID[id]; ok {
			names = append(names, player.Name)
			continue
		}
		names = append(names, id)
	}
	return strings.Join(names, ", ")
}

// tradePerspective returns (gains, losses) for teamID against offer: the
// From team gains Get and loses Give; the To team gains Give and loses
// Get (section 7.1's trade replay semantics, mirrored for the recipient's
// own read of the deal).
func tradePerspective(offer TradeOffer, teamID string) (gains, losses []string) {
	if teamID == offer.FromTeamID {
		return offer.Get, offer.Give
	}
	return offer.Give, offer.Get
}

// tradeRosterBounds resolves T7's [starterCount, rosterCap] bound from the
// currently active roster shape (roster-ops spec section 6.3).
func tradeRosterBounds() (starterCount, rosterCap int) {
	preset := CurrentRoster()
	return preset.Starters(), preset.Total()
}

// ---------------------------------------------------------------------
// Service-level lifecycle actions (roster-ops spec section 6.1)
// ---------------------------------------------------------------------

// ProposeTrade applies section 6.1's propose step: T1 (signed-in), T2
// (not your own team), T3 (counterparty seat claimed), T14 (note length),
// then validateTradeAssets (T4-T8, T12).
func (s *Service) ProposeTrade(r *http.Request, requestedTeam, toTeamID string, give, get, picks []string, note string) (string, error) {
	teamID, err := s.actingTeam(r, requestedTeam) // T1
	if err != nil {
		return "", err
	}
	toTeamID = strings.TrimSpace(toTeamID)
	if toTeamID == teamID { // T2
		return "", fmt.Errorf("you cannot trade with your own team")
	}
	state := s.store.Snapshot()
	if !teamHasManager(state, toTeamID) { // T3
		return "", fmt.Errorf("that seat has no manager yet")
	}
	note = strings.TrimSpace(note)
	if utf8.RuneCountInString(note) > tradeNoteMaxRunes { // T14
		return "", fmt.Errorf("trade notes must be %d characters or fewer", tradeNoteMaxRunes)
	}
	now := s.clock()
	offer := TradeOffer{
		FromTeamID: teamID, ToTeamID: toTeamID,
		Give: cleanTradePlayerIDs(give), Get: cleanTradePlayerIDs(get), Picks: cleanTradePlayerIDs(picks),
		Note: note, Status: TradeStatusOpen, CreatedAt: now,
	}
	games := s.schedule()
	pool := s.pool()
	starterCount, rosterCap := tradeRosterBounds()
	if err := validateTradeAssets(state, s.cfg, games, pool.byID, now, offer, starterCount, rosterCap); err != nil {
		return "", err
	}
	id, err := randomTradeOfferID()
	if err != nil {
		return "", err
	}
	offer.ID = id
	if err := s.store.ProposeTradeOffer(offer, s.cfg, games, pool.byID, starterCount, rosterCap); err != nil {
		return "", err
	}
	s.notifyTradeOffer(offer)
	return fmt.Sprintf("Offer sent to %s.", s.teamByID(toTeamID).Name), nil
}

// CounterTrade applies section 6.1's counter step: the receiving manager
// (T1) proposes a new, sides-swapped offer against the original, which
// must still be open (T9); the actor must be the original's receiving
// manager; T14 and validateTradeAssets (T4-T8, T12) apply to the new
// offer.
func (s *Service) CounterTrade(r *http.Request, requestedTeam, offerID string, give, get, picks []string, note string) (string, error) {
	teamID, err := s.actingTeam(r, requestedTeam) // T1
	if err != nil {
		return "", err
	}
	state := s.store.Snapshot()
	original, ok := findTradeOffer(state.TradeOffers, offerID)
	if !ok || original.Status != TradeStatusOpen { // T9
		return "", fmt.Errorf("this offer is no longer open")
	}
	if original.ToTeamID != teamID {
		return "", fmt.Errorf("only the receiving manager can counter this offer")
	}
	note = strings.TrimSpace(note)
	if utf8.RuneCountInString(note) > tradeNoteMaxRunes { // T14
		return "", fmt.Errorf("trade notes must be %d characters or fewer", tradeNoteMaxRunes)
	}
	now := s.clock()
	newOffer := TradeOffer{
		FromTeamID: teamID, ToTeamID: original.FromTeamID,
		Give: cleanTradePlayerIDs(give), Get: cleanTradePlayerIDs(get), Picks: cleanTradePlayerIDs(picks),
		Note: note, Status: TradeStatusOpen, ParentID: original.ID, CreatedAt: now,
	}
	games := s.schedule()
	pool := s.pool()
	starterCount, rosterCap := tradeRosterBounds()
	if err := validateTradeAssets(state, s.cfg, games, pool.byID, now, newOffer, starterCount, rosterCap); err != nil {
		return "", err
	}
	id, err := randomTradeOfferID()
	if err != nil {
		return "", err
	}
	newOffer.ID = id
	if err := s.store.CounterTradeOffer(offerID, newOffer, s.cfg, games, pool.byID, now, starterCount, rosterCap); err != nil {
		return "", err
	}
	s.notifyTradeOffer(newOffer)
	return fmt.Sprintf("Counter sent to %s.", s.teamByID(original.FromTeamID).Name), nil
}

// DeclineTrade applies section 6.1's decline step: open → declined. T1,
// T9 (still open), and the receiving-manager actor check.
func (s *Service) DeclineTrade(r *http.Request, requestedTeam, offerID string) (string, error) {
	teamID, err := s.actingTeam(r, requestedTeam) // T1
	if err != nil {
		return "", err
	}
	state := s.store.Snapshot()
	offer, ok := findTradeOffer(state.TradeOffers, offerID)
	if !ok || offer.Status != TradeStatusOpen { // T9
		return "", fmt.Errorf("this offer is no longer open")
	}
	if offer.ToTeamID != teamID {
		return "", fmt.Errorf("only the receiving manager can decline this offer")
	}
	if err := s.store.DeclineTradeOffer(offerID, s.clock()); err != nil {
		return "", err
	}
	return "Offer declined.", nil
}

// WithdrawTrade applies section 6.1's withdraw step: open → withdrawn. T1,
// T9, T11 (only the proposing manager).
func (s *Service) WithdrawTrade(r *http.Request, requestedTeam, offerID string) (string, error) {
	teamID, err := s.actingTeam(r, requestedTeam) // T1
	if err != nil {
		return "", err
	}
	state := s.store.Snapshot()
	offer, ok := findTradeOffer(state.TradeOffers, offerID)
	if !ok || offer.Status != TradeStatusOpen { // T9
		return "", fmt.Errorf("this offer is no longer open")
	}
	if offer.FromTeamID != teamID { // T11
		return "", fmt.Errorf("only the proposing manager can withdraw this offer")
	}
	if err := s.store.WithdrawTradeOffer(offerID, s.clock()); err != nil {
		return "", err
	}
	return "Offer withdrawn.", nil
}

// AcceptTrade applies section 6.1's accept step: open → accepted,
// re-validating (T4-T8, T12) and starting the review window. T1, T9, T10
// (only the receiving manager). Under trades.veto == "none" there is no
// window: execution runs immediately, in the same request, right after
// the accept persists (section 6.1: "trades.veto = 'none': executes at
// accept, no window").
func (s *Service) AcceptTrade(r *http.Request, requestedTeam, offerID string) (string, error) {
	teamID, err := s.actingTeam(r, requestedTeam) // T1
	if err != nil {
		return "", err
	}
	state := s.store.Snapshot()
	offer, ok := findTradeOffer(state.TradeOffers, offerID)
	if !ok || offer.Status != TradeStatusOpen { // T9
		return "", fmt.Errorf("this offer is no longer open")
	}
	if offer.ToTeamID != teamID { // T10
		return "", fmt.Errorf("only the receiving manager can accept this offer")
	}
	now := s.clock()
	games := s.schedule()
	pool := s.pool()
	starterCount, rosterCap := tradeRosterBounds()
	if err := validateTradeAssets(state, s.cfg, games, pool.byID, now, offer, starterCount, rosterCap); err != nil {
		return "", err
	}
	if err := s.store.AcceptTradeOffer(offerID, s.cfg, games, pool.byID, now, starterCount, rosterCap); err != nil {
		return "", err
	}
	if s.cfg.Trades.Veto != "none" {
		return "Offer accepted; it now enters review.", nil
	}
	txn, err := s.store.ExecuteTradeOffer(offerID, s.cfg, games, pool.byID, now, starterCount, rosterCap)
	if err != nil {
		// The offer already recorded "failed" with FailReason inside
		// ExecuteTradeOffer; surface that reason rather than claiming
		// success.
		return "", err
	}
	s.notifyTradeExecuted(offer, txn)
	return fmt.Sprintf("Trade executed: %s for %s.", tradePlayerNames(pool.byID, offer.Get), tradePlayerNames(pool.byID, offer.Give)), nil
}

// ApproveTrade applies section 6.1's approve step: accepted → executed,
// early execution under commissioner or both mode. T13's commissioner
// variant gates it. In both mode, a completed community veto already
// moved the offer past "accepted" (Store.CommissionerVetoTradeOffer's
// doc comment), so this call fails closed on its own without any special
// case here — "approve does not override a completed community veto."
func (s *Service) ApproveTrade(r *http.Request, offerID string) (string, error) {
	if err := s.requireCommissioner(r); err != nil { // T13
		return "", err
	}
	if s.cfg.Trades.Veto != "commissioner" && s.cfg.Trades.Veto != "both" {
		return "", fmt.Errorf("commissioner review is not part of this league's trade policy")
	}
	state := s.store.Snapshot()
	offer, ok := findTradeOffer(state.TradeOffers, offerID)
	if !ok {
		return "", fmt.Errorf("this trade is no longer under review")
	}
	now := s.clock()
	games := s.schedule()
	pool := s.pool()
	starterCount, rosterCap := tradeRosterBounds()
	txn, err := s.store.ExecuteTradeOffer(offerID, s.cfg, games, pool.byID, now, starterCount, rosterCap)
	if err != nil {
		return "", err
	}
	s.notifyTradeExecuted(offer, txn)
	return fmt.Sprintf("Trade approved and executed: %s for %s.", tradePlayerNames(pool.byID, offer.Get), tradePlayerNames(pool.byID, offer.Give)), nil
}

// CommissionerVetoTrade applies section 6.1's veto step under
// commissioner or both mode: accepted → vetoed. T13's commissioner
// variant gates it.
func (s *Service) CommissionerVetoTrade(r *http.Request, offerID string) (string, error) {
	if err := s.requireCommissioner(r); err != nil { // T13
		return "", err
	}
	if s.cfg.Trades.Veto != "commissioner" && s.cfg.Trades.Veto != "both" {
		return "", fmt.Errorf("commissioner review is not part of this league's trade policy")
	}
	state := s.store.Snapshot()
	offer, ok := findTradeOffer(state.TradeOffers, offerID)
	if !ok {
		return "", fmt.Errorf("this trade is no longer under review")
	}
	if err := s.store.CommissionerVetoTradeOffer(offerID, s.clock()); err != nil {
		return "", err
	}
	s.notifyTradeVetoed(offer, "commissioner review")
	return "Trade vetoed.", nil
}

// VoteVetoTrade applies section 6.1's veto step under vote or both mode:
// one non-party manager's veto vote, accumulated toward
// ceil((seats-2)/2). T1, T13's vote variant ("only managers outside the
// trade can vote").
func (s *Service) VoteVetoTrade(r *http.Request, requestedTeam, offerID string) (string, error) {
	teamID, err := s.actingTeam(r, requestedTeam) // T1
	if err != nil {
		return "", err
	}
	if s.cfg.Trades.Veto != "vote" && s.cfg.Trades.Veto != "both" {
		return "", fmt.Errorf("league vote is not part of this league's trade policy")
	}
	state := s.store.Snapshot()
	offer, ok := findTradeOffer(state.TradeOffers, offerID)
	if !ok {
		return "", fmt.Errorf("this trade is no longer under review")
	}
	if teamID == offer.FromTeamID || teamID == offer.ToTeamID { // T13
		return "", fmt.Errorf("only managers outside the trade can vote")
	}
	now := s.clock()
	threshold := tradeVetoThreshold(len(defaultTeamIDs()))
	crossed, err := s.store.FileTradeVetoOffer(offerID, teamID, now, threshold)
	if err != nil {
		return "", err
	}
	if crossed {
		s.notifyTradeVetoed(offer, "league vote")
		return "Your vote vetoed the trade.", nil
	}
	return "Your veto vote is recorded.", nil
}

// teamHasManager reports whether teamID both names a real seat and has a
// signed-in manager assigned to it (T3: "that seat has no manager yet").
func teamHasManager(state PersistedState, teamID string) bool {
	if !knownTeam(teamID) {
		return false
	}
	for _, member := range state.Members {
		if member.TeamID == teamID {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------
// N15-N17 notifications (roster-ops spec section 9)
// ---------------------------------------------------------------------

// keyTradeOffer, keyTradeExecuted, keyTradeVetoed back N15/N16/N17's
// catalog entries (notifications.go).
func keyTradeOffer(offerID, email string) string {
	return "tradeoffer:" + offerID + ":" + normalizeEmail(email)
}
func keyTradeExecuted(offerID, email string) string {
	return "tradedone:" + offerID + ":" + normalizeEmail(email)
}
func keyTradeVetoed(offerID, email string) string {
	return "tradeveto:" + offerID + ":" + normalizeEmail(email)
}

// tradeOfferShortID renders offer.ID's short form for a Headline signal
// ("trd-a1b2c3d4" already reads short; this trims any future longer ID
// shape defensively).
func tradeOfferShortID(offerID string) string {
	if len(offerID) > 16 {
		return offerID[:16]
	}
	return offerID
}

// notifyTradeOffer fires N15 to every operator of the receiving seat
// (primary and, if bound, co-manager — registration wave, build item 4)
// when an offer opens (propose or counter).
func (s *Service) notifyTradeOffer(offer TradeOffer) {
	if !s.notifyReady() {
		return
	}
	state := s.store.Snapshot()
	for _, member := range teamMembers(state.Members, offer.ToTeamID) {
		if member.Email == "" {
			continue
		}
		key := keyTradeOffer(offer.ID, member.Email)
		s.recordAndSend(state, member.Email, categoryTransactions, key, s.clock(), func() renderedNotification {
			return s.buildTradeOffer(offer, member)
		})
	}
}

func (s *Service) buildTradeOffer(offer TradeOffer, member Member) renderedNotification {
	pool := s.pool()
	fromTeam := s.teamByID(offer.FromTeamID)
	firstGetName := offer.Get[0]
	if player, ok := pool.byID[offer.Get[0]]; ok {
		firstGetName = player.Name
	}
	subject := fmt.Sprintf("TRADE OFFER: %s wants %s", fromTeam.Name, firstGetName)
	if extra := len(offer.Get) - 1; extra > 0 {
		subject += fmt.Sprintf(" +%d", extra)
	}
	lede := fmt.Sprintf("%s offers %s for %s.", fromTeam.Name, tradePlayerNames(pool.byID, offer.Give), tradePlayerNames(pool.byID, offer.Get))

	rows := make([][]string, 0, len(offer.Give)+len(offer.Get))
	for _, id := range offer.Give {
		p := pool.byID[id]
		rows = append(rows, []string{"THEY GIVE", p.Name, p.Position, p.NFLTeam})
	}
	for _, id := range offer.Get {
		p := pool.byID[id]
		rows = append(rows, []string{"THEY GET", p.Name, p.Position, p.NFLTeam})
	}

	shell := s.shellFor(categoryTransactions, "TRADE DESK // OFFER "+tradeOfferShortID(offer.ID))
	blocks := []emailkit.Block{
		emailkit.Headline{Title: "SOMEONE WANTS YOUR GUYS.", Lede: lede},
		emailkit.StatTable{Title: "THE OFFER", Header: []string{"DIRECTION", "PLAYER", "POS", "TEAM"}, Rows: rows},
	}
	if offer.Note != "" {
		blocks = append(blocks, emailkit.Note{Text: offer.Note})
	}
	blocks = append(blocks, emailkit.CTA{Label: "OPEN THE TRADE DESK →", URL: s.leagueURL() + "/trades"})
	text, html := emailkit.Render(shell, blocks)
	return renderedNotification{
		Key: keyTradeOffer(offer.ID, member.Email), Category: categoryTransactions,
		To: member.Email, Subject: subject, Text: text, HTML: html,
	}
}

// notifyTradeExecuted fires N16 to every operator of both party seats —
// each side's primary and, if bound, co-manager (registration wave,
// build item 4) — when an accepted offer executes (tick, early approval,
// or immediate execution under veto "none").
func (s *Service) notifyTradeExecuted(offer TradeOffer, txn Transaction) {
	if !s.notifyReady() {
		return
	}
	state := s.store.Snapshot()
	for _, teamID := range [2]string{offer.FromTeamID, offer.ToTeamID} {
		recipientTeamID := teamID
		for _, member := range teamMembers(state.Members, teamID) {
			if member.Email == "" {
				continue
			}
			key := keyTradeExecuted(offer.ID, member.Email)
			s.recordAndSend(state, member.Email, categoryTransactions, key, s.clock(), func() renderedNotification {
				return s.buildTradeExecuted(state, offer, member, recipientTeamID)
			})
		}
	}
}

// tradeEmptiedCurrentSlot reports the current-week lineup slot (if any)
// that one of losses currently occupies for teamID — section 9's N16 lede
// consequence line, and section 6.3's "an emptied current-week slot
// triggers the N18 warning path, not an error."
func tradeEmptiedCurrentSlot(state PersistedState, games []GameInfo, now time.Time, teamID string, losses []string) string {
	week := pickemWeekAt(games, now)
	lost := make(map[string]bool, len(losses))
	for _, id := range losses {
		lost[id] = true
	}
	for slot, playerID := range state.Lineups[teamID][week] {
		if lost[playerID] {
			return slot
		}
	}
	return ""
}

func (s *Service) buildTradeExecuted(state PersistedState, offer TradeOffer, member Member, teamID string) renderedNotification {
	pool := s.pool()
	gains, losses := tradePerspective(offer, teamID)
	gainsNames := tradePlayerNames(pool.byID, gains)
	lossesNames := tradePlayerNames(pool.byID, losses)
	subject := fmt.Sprintf("TRADE COMPLETE: %s for %s", gainsNames, lossesNames)

	otherTeamID := offer.ToTeamID
	if teamID == offer.ToTeamID {
		otherTeamID = offer.FromTeamID
	}
	lede := fmt.Sprintf("Your trade with %s is final: you gain %s, you send %s.", s.teamByID(otherTeamID).Name, gainsNames, lossesNames)
	if slot := tradeEmptiedCurrentSlot(state, s.schedule(), s.clock(), teamID, losses); slot != "" {
		lede += fmt.Sprintf(" It empties your %s slot — set your lineup before kickoff.", slot)
	}

	rows := make([][]string, 0, len(gains)+len(losses))
	markRow := 0
	for _, id := range gains {
		p := pool.byID[id]
		rows = append(rows, []string{"YOU GET", p.Name, p.Position, p.NFLTeam})
		if markRow == 0 {
			markRow = len(rows)
		}
	}
	for _, id := range losses {
		p := pool.byID[id]
		rows = append(rows, []string{"YOU SEND", p.Name, p.Position, p.NFLTeam})
	}

	shell := s.shellFor(categoryTransactions, "TRADE DESK // EXECUTED")
	blocks := []emailkit.Block{
		emailkit.Headline{Title: "THE DEAL IS DONE.", Lede: lede},
		emailkit.StatTable{Title: "FINAL TERMS", Header: []string{"", "PLAYER", "POS", "TEAM"}, Rows: rows, MarkRow: markRow},
		emailkit.CTA{Label: "SET YOUR LINEUP →", URL: s.leagueURL() + "/team"},
	}
	text, html := emailkit.Render(shell, blocks)
	return renderedNotification{
		Key: keyTradeExecuted(offer.ID, member.Email), Category: categoryTransactions,
		To: member.Email, Subject: subject, Text: text, HTML: html,
	}
}

// notifyTradeVetoed fires N17 to every operator of both party seats
// (registration wave, build item 4) when an accepted offer vetoes
// (commissioner action, or the vote threshold trips). mechanism is
// "commissioner review" or "league vote" — named without naming
// individual voters (section 9's N17 lede rule).
func (s *Service) notifyTradeVetoed(offer TradeOffer, mechanism string) {
	if !s.notifyReady() {
		return
	}
	state := s.store.Snapshot()
	for _, teamID := range [2]string{offer.FromTeamID, offer.ToTeamID} {
		for _, member := range teamMembers(state.Members, teamID) {
			if member.Email == "" {
				continue
			}
			key := keyTradeVetoed(offer.ID, member.Email)
			s.recordAndSend(state, member.Email, categoryTransactions, key, s.clock(), func() renderedNotification {
				return s.buildTradeVetoed(offer, member, mechanism)
			})
		}
	}
}

func (s *Service) buildTradeVetoed(offer TradeOffer, member Member, mechanism string) renderedNotification {
	pool := s.pool()
	giveNames := tradePlayerNames(pool.byID, offer.Give)
	getNames := tradePlayerNames(pool.byID, offer.Get)
	subject := fmt.Sprintf("TRADE VETOED: %s for %s is dead", giveNames, getNames)
	lede := fmt.Sprintf("The trade between %s and %s did not survive review.", s.teamByID(offer.FromTeamID).Name, s.teamByID(offer.ToTeamID).Name)

	shell := s.shellFor(categoryTransactions, "TRADE DESK // VETOED")
	blocks := []emailkit.Block{
		emailkit.Headline{Title: "THE LEAGUE SAID NO.", Lede: lede},
		emailkit.Panel{Rows: []emailkit.PanelRow{
			{Label: "OFFER", Value: fmt.Sprintf("%s gives %s, gets %s", s.teamByID(offer.FromTeamID).Name, giveNames, getNames)},
			{Label: "MECHANISM", Value: mechanism},
			{Label: "POLICY", Value: s.cfg.Trades.Veto},
		}},
		emailkit.CTA{Label: "BACK TO THE DESK →", URL: s.leagueURL() + "/trades"},
	}
	text, html := emailkit.Render(shell, blocks)
	return renderedNotification{
		Key: keyTradeVetoed(offer.ID, member.Email), Category: categoryTransactions,
		To: member.Email, Subject: subject, Text: text, HTML: html,
	}
}

// ---------------------------------------------------------------------
// /trades page data (roster-ops spec section 8.3)
// ---------------------------------------------------------------------

// tradeOfferRow renders one TradeOffer for the COMPOSE/INBOX/OUTBOX/REVIEW
// panels: display fields plus the per-viewer action flags that decide
// which managed form(s) the row shows.
func (s *Service) tradeOfferRow(pool playerPool, offer TradeOffer, teamID string, canEdit, isCommissioner bool, threshold int) map[string]any {
	give := make([]map[string]any, 0, len(offer.Give))
	for _, id := range offer.Give {
		p := pool.byID[id]
		give = append(give, map[string]any{"id": id, "name": p.Name, "position": p.Position, "team": p.NFLTeam})
	}
	get := make([]map[string]any, 0, len(offer.Get))
	for _, id := range offer.Get {
		p := pool.byID[id]
		get = append(get, map[string]any{"id": id, "name": p.Name, "position": p.Position, "team": p.NFLTeam})
	}
	outsideParty := teamID != "" && teamID != offer.FromTeamID && teamID != offer.ToTeamID
	alreadyVoted := false
	for _, voter := range offer.Vetoes {
		if voter == teamID {
			alreadyVoted = true
		}
	}
	row := map[string]any{
		"id":                    offer.ID,
		"status":                offer.Status,
		"from_team":             s.teamByID(offer.FromTeamID).Name,
		"from_team_id":          offer.FromTeamID,
		"to_team":               s.teamByID(offer.ToTeamID).Name,
		"to_team_id":            offer.ToTeamID,
		"give":                  give,
		"get":                   get,
		"note":                  offer.Note,
		"has_note":              offer.Note != "",
		"created_at":            offer.CreatedAt.Format("Jan 2, 3:04 PM MST"),
		"fail_reason":           offer.FailReason,
		"vetoes_count":          len(offer.Vetoes),
		"vetoes_threshold":      threshold,
		"can_decline":           canEdit && offer.Status == TradeStatusOpen && offer.ToTeamID == teamID,
		"can_counter":           canEdit && offer.Status == TradeStatusOpen && offer.ToTeamID == teamID,
		"can_accept":            canEdit && offer.Status == TradeStatusOpen && offer.ToTeamID == teamID,
		"can_withdraw":          canEdit && offer.Status == TradeStatusOpen && offer.FromTeamID == teamID,
		"can_approve":           isCommissioner && offer.Status == TradeStatusAccepted && (s.cfg.Trades.Veto == "commissioner" || s.cfg.Trades.Veto == "both"),
		"can_veto_commissioner": isCommissioner && offer.Status == TradeStatusAccepted && (s.cfg.Trades.Veto == "commissioner" || s.cfg.Trades.Veto == "both"),
		"can_vote": canEdit && outsideParty && offer.Status == TradeStatusAccepted &&
			(s.cfg.Trades.Veto == "vote" || s.cfg.Trades.Veto == "both") && !alreadyVoted,
		"already_voted": alreadyVoted,
	}
	if offer.Status == TradeStatusAccepted {
		row["review_deadline"] = formatResolvesAt(s.cfg, offer.AcceptedAt.Add(time.Duration(s.cfg.Trades.ReviewHours)*time.Hour))
	}
	return row
}

// TradesData assembles the /trades page (roster-ops spec section 8.3):
// COMPOSE (counterparty roster options), INBOX/OUTBOX (open offers plus
// the viewer's own accepted offers awaiting the review window), REVIEW
// (commissioner approve/veto, commissioner or both mode), and VOTE (non-
// party managers, vote or both mode).
func (s *Service) TradesData(r *http.Request) map[string]any {
	viewer := s.Viewer(r)
	teamID, _ := viewer["team_id"].(string)
	canEdit := s.viewerKey(r) != ""
	isCommissioner := s.IsCommissioner(r)
	state := s.store.Snapshot()
	pool := s.pool()
	rosters := currentRosters(state)
	threshold := tradeVetoThreshold(len(defaultTeamIDs()))

	rosterOptions := func(id string) []map[string]any {
		out := make([]map[string]any, 0, len(rosters[id]))
		for _, playerID := range rosters[id] {
			if p, ok := pool.byID[playerID]; ok {
				out = append(out, map[string]any{"id": p.ID, "label": fmt.Sprintf("%s (%s)", p.Name, p.Position)})
			}
		}
		return out
	}

	myOptions := rosterOptions(teamID)
	counterparties := make([]map[string]any, 0, len(s.Teams()))
	for _, team := range s.Teams() {
		if team.ID == teamID {
			continue
		}
		counterparties = append(counterparties, map[string]any{
			"id": team.ID, "name": team.Name,
		})
	}

	// The composer is a two-step managed form (roster-ops spec section
	// 8.1's team-page swap idiom, applied to trades): ?counterparty=team-N
	// re-renders the page with that team's roster as the GET checkbox
	// group; the composer POST carries to_team_id as a hidden field so the
	// server always knows which roster the submitted "get" IDs came from.
	composeCounterpartyID := strings.TrimSpace(r.URL.Query().Get("counterparty"))
	if composeCounterpartyID == teamID {
		composeCounterpartyID = ""
	}
	composeOptions := []map[string]any{}
	composeCounterpartyName := ""
	if composeCounterpartyID != "" && knownTeam(composeCounterpartyID) {
		composeOptions = rosterOptions(composeCounterpartyID)
		composeCounterpartyName = s.teamByID(composeCounterpartyID).Name
	}

	inbox := []map[string]any{}
	outbox := []map[string]any{}
	review := []map[string]any{}
	votePanel := []map[string]any{}
	for _, offer := range state.TradeOffers {
		switch {
		case offer.Status == TradeStatusOpen && offer.ToTeamID == teamID:
			inbox = append(inbox, s.tradeOfferRow(pool, offer, teamID, canEdit, isCommissioner, threshold))
		case offer.Status == TradeStatusOpen && offer.FromTeamID == teamID:
			outbox = append(outbox, s.tradeOfferRow(pool, offer, teamID, canEdit, isCommissioner, threshold))
		case offer.Status == TradeStatusAccepted && (offer.FromTeamID == teamID || offer.ToTeamID == teamID):
			outbox = append(outbox, s.tradeOfferRow(pool, offer, teamID, canEdit, isCommissioner, threshold))
		}
		if offer.Status == TradeStatusAccepted && isCommissioner && (s.cfg.Trades.Veto == "commissioner" || s.cfg.Trades.Veto == "both") {
			review = append(review, s.tradeOfferRow(pool, offer, teamID, canEdit, isCommissioner, threshold))
		}
		if offer.Status == TradeStatusAccepted && teamID != "" && teamID != offer.FromTeamID && teamID != offer.ToTeamID &&
			(s.cfg.Trades.Veto == "vote" || s.cfg.Trades.Veto == "both") {
			votePanel = append(votePanel, s.tradeOfferRow(pool, offer, teamID, canEdit, isCommissioner, threshold))
		}
	}

	return map[string]any{
		"viewer":                    viewer,
		"league":                    s.leagueMap(),
		"can_edit":                  canEdit,
		"is_commissioner":           isCommissioner,
		"veto_mode":                 s.cfg.Trades.Veto,
		"note_max":                  tradeNoteMaxRunes,
		"counterparties":            counterparties,
		"my_options":                myOptions,
		"my_options_empty":          len(myOptions) == 0,
		"compose_counterparty_id":   composeCounterpartyID,
		"compose_counterparty_name": composeCounterpartyName,
		"compose_active":            composeCounterpartyID != "",
		"compose_options":           composeOptions,
		"compose_options_empty":     len(composeOptions) == 0,
		"inbox":                     inbox,
		"inbox_empty":               len(inbox) == 0,
		"outbox":                    outbox,
		"outbox_empty":              len(outbox) == 0,
		"review":                    review,
		"review_empty":              len(review) == 0,
		"vote_panel":                votePanel,
		"vote_panel_empty":          len(votePanel) == 0,
	}
}
