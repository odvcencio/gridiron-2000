package league

import (
	"fmt"
	"image/color"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"m31labs.dev/gosx/auth"
)

// TestSeasonJourneyAcceptance is the durable counterpart to the disposable
// lifecycle rehearsal used during the season-readiness pass. It intentionally
// crosses the real service/store boundaries instead of manufacturing state
// maps: a future change that makes one surface lie about the state left by a
// previous surface should fail here with the invariant that matters to a
// manager or commissioner.
func TestSeasonJourneyAcceptance(t *testing.T) {
	// Bench 0 (house-rank change, 2026-08-30): this fixture's pool carries
	// exactly 8 QBs and 8 RBs for 8 teams — supply exactly equal to
	// Starters demand. A bench slot would let AdminForceAutopick's house
	// order (houserank.go), which ranks same-position players together
	// rather than interleaving positions the way market ADP naturally
	// does, legally spend it on a SECOND QB or RB for one team and starve
	// a later team of the one it needs. Zero bench makes every pick's
	// legality check (draftCandidateKeepsRosterViable) require the exact
	// still-missing position, independent of ranking order, so the
	// terminal 1-QB/1-RB-per-team shape this test's later assertions rely
	// on is guaranteed rather than order-dependent.
	setRosterShape(RosterPreset{
		Name:  "season-journey",
		Slots: map[string]int{"QB": 1, "RB": 1},
		Bench: 0,
	})
	t.Cleanup(clearRosterShape)

	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	service, clock := newNotifyTestService(t, now.Add(time.Hour), now)
	service.demoMode = false
	service.store.draftLifecycleBypass = false
	pool := seasonJourneyPool()
	service.SetPlayerSource(func() ([]Player, int64, string) {
		return pool, 1, "live"
	})
	avatarAnchor := t.TempDir()
	service.avatarDurableRoot = avatarAnchor
	service.avatarRoot = filepath.Join(avatarAnchor, "avatars")

	// Admission is membership first and seat claim second. A co-manager is a
	// second operator on the same seat, never a second franchise.
	member, err := service.EnsureMember("primary@example.com", "Primary")
	if err != nil || member.TeamID != "" {
		t.Fatalf("membership admission = %+v, %v; want a seatless member", member, err)
	}
	primary, err := service.AssignManager("primary@example.com", "Primary")
	if err != nil || primary.TeamID != "team-1" {
		t.Fatalf("primary seat claim = %+v, %v; want team-1", primary, err)
	}
	if err := service.store.InviteCoManager(primary.TeamID, "co@example.com"); err != nil {
		t.Fatalf("invite co-manager: %v", err)
	}
	co, bound, err := service.BindCoManagerOnSignIn("co@example.com", "Co")
	if err != nil || !bound || co.TeamID != primary.TeamID || co.Role != "co" {
		t.Fatalf("co-manager bind = %+v, bound=%v err=%v", co, bound, err)
	}
	seatState := service.store.Snapshot()
	if got := len(teamMembers(seatState.Members, primary.TeamID)); got != 2 {
		t.Fatalf("team operator count = %d, want primary + co-manager", got)
	}
	seatless, err := service.EnsureMember("claimant@example.com", "Claimant")
	if err != nil || seatless.TeamID != "" {
		t.Fatalf("second admission = %+v, %v; want seatless before claim", seatless, err)
	}
	claimedTeam, err := service.claimFantasySeat("claimant@example.com", "Claimant", "Claimant Franchise", "helmet")
	if err != nil || claimedTeam.ID != "team-2" {
		t.Fatalf("second seat claim = %+v, %v; want team-2", claimedTeam, err)
	}

	t.Setenv("COMMISSIONER_EMAILS", "commissioner@example.com")
	primaryRequest := authenticatedJourneyRequest(t, "primary@example.com", "Primary", "/journey")
	coRequest := authenticatedJourneyRequest(t, "co@example.com", "Co", "/journey")
	teamTwoRequest := authenticatedJourneyRequest(t, "claimant@example.com", "Claimant", "/journey")
	commissionerRequest := authenticatedJourneyRequest(t, "commissioner@example.com", "Commissioner", "/admin")

	// The team terminal is a useful pre-draft preview, but it must not imply
	// that a roster exists before the draft has actually produced picks.
	predraft := service.TeamData(primaryRequest)
	if predraft["predraft_visible"] != true || predraft["drafted"] != false {
		t.Fatalf("pre-draft team state = %+v, want visible preview and empty roster", predraft)
	}
	if starters, ok := predraft["starters"].([]map[string]any); !ok || len(starters) != 2 {
		t.Fatalf("pre-draft starters = %#v, want the QB/RB empty preview", predraft["starters"])
	}
	for _, row := range predraft["starters"].([]map[string]any) {
		if row["has_player"] == true {
			t.Fatalf("pre-draft starter unexpectedly occupied: %+v", row)
		}
	}

	// A custom image is an identity replacement: uploading it must release a
	// claimed badge, and selecting a badge afterward must clear the image.
	if err := service.store.ClaimBadge(primary.TeamID, "wolf"); err != nil {
		t.Fatalf("seed badge claim: %v", err)
	}
	avatar, err := service.UploadAvatar(primaryRequest, primary.TeamID, solidPNG(t, 96, 96, color.RGBA{R: 21, G: 224, B: 255, A: 255}))
	if err != nil || !avatar.BadgeReleased || avatar.Ref == "" {
		t.Fatalf("custom avatar transition = %+v, %v", avatar, err)
	}
	if _, claimed := service.store.BadgeClaim(primary.TeamID); claimed {
		t.Fatal("custom avatar upload retained the old badge claim")
	}
	transition, err := service.ClaimBadgeWithTransition(primaryRequest, primary.TeamID, "star")
	if err != nil || !transition.AvatarCleared {
		t.Fatalf("badge replacement transition = %+v, %v", transition, err)
	}
	if _, exists := service.store.AvatarRef(primary.TeamID); exists {
		t.Fatal("badge selection retained the superseded custom avatar")
	}

	// Big Board edits are real persisted actions. The board is also the input
	// to commissioner-forced AUTO, so its order is a product invariant rather
	// than presentation-only state.
	if _, err := service.BoardAdd(primaryRequest, "pool-004"); err != nil {
		t.Fatalf("primary board add: %v", err)
	}
	if _, err := service.BoardAdd(coRequest, "pool-005"); err != nil {
		t.Fatalf("co-manager shared board add: %v", err)
	}
	if got := service.store.Snapshot().Boards["primary@example.com"]; len(got) != 2 || got[0] != "pool-004" || got[1] != "pool-005" {
		t.Fatalf("shared primary/co-manager board = %v, want [pool-004 pool-005]", got)
	}
	if _, exists := service.store.Snapshot().Boards["co@example.com"]; exists {
		t.Fatal("co-manager received a separate board instead of sharing the seat board")
	}
	boardData := service.BoardData(coRequest)
	if boardData["board_count"] != 2 {
		t.Fatalf("co-manager board data count = %v, want 2", boardData["board_count"])
	}
	// Team two's top preference is intentionally not the pool's next-best
	// player. The forced AUTO assertions below therefore distinguish a real
	// persisted board decision from a coincidental ADP fallback.
	if _, err := service.BoardAdd(teamTwoRequest, "pool-014"); err != nil {
		t.Fatalf("team-2 board add: %v", err)
	}
	if got := service.store.Snapshot().Boards["claimant@example.com"]; len(got) != 1 || got[0] != "pool-014" {
		t.Fatalf("team-2 persisted board = %v, want [pool-014]", got)
	}

	// Presence, readiness, and manager-selected AUTO are independent durable
	// controls. The commissioner can also set another seat's controls.
	service.RecordPresence(primaryRequest, now)
	service.RecordPresence(coRequest, now)
	for _, email := range []string{"primary@example.com", "co@example.com"} {
		if seen, ok := service.presence.seen(email); !ok || !seen.Equal(now) {
			t.Fatalf("presence for %s = %v/%v, want %v", email, seen, ok, now)
		}
	}
	beforeUnauthorizedReady := service.store.Snapshot()
	if err := service.AdminSetReady(primaryRequest, "team-2", true); err == nil {
		t.Fatal("non-commissioner changed another seat's ready state")
	}
	afterUnauthorizedReady := service.store.Snapshot()
	if !reflect.DeepEqual(beforeUnauthorizedReady.Ready, afterUnauthorizedReady.Ready) {
		t.Fatalf("rejected cross-team ready action mutated state: before=%v after=%v", beforeUnauthorizedReady.Ready, afterUnauthorizedReady.Ready)
	}
	ready, _, err := service.ToggleReady(primaryRequest, primary.TeamID)
	if err != nil || !ready {
		t.Fatalf("manager ready = %v, %v; want true", ready, err)
	}
	autopick, _, err := service.ToggleAutopick(coRequest, primary.TeamID)
	if err != nil || !autopick {
		t.Fatalf("co-manager shared-seat autopick = %v, %v; want true", autopick, err)
	}
	if err := service.AdminSetReady(commissionerRequest, "team-2", true); err != nil {
		t.Fatalf("commissioner ready: %v", err)
	}
	if err := service.AdminSetAutopick(commissionerRequest, "team-2", true); err != nil {
		t.Fatalf("commissioner autopick: %v", err)
	}
	controls := service.store.Snapshot()
	if !controls.Ready[primary.TeamID] || !controls.Ready["team-2"] || !controls.Autopick[primary.TeamID] || !controls.Autopick["team-2"] {
		t.Fatalf("draft controls = ready:%v autopick:%v", controls.Ready, controls.Autopick)
	}

	// The scheduled timestamp is not an implicit authorization. A manager's
	// pick is rejected until the commissioner performs the intentional start.
	if _, _, _, err := service.MakePick(primaryRequest, primary.TeamID, "pool-001"); err == nil || !strings.Contains(err.Error(), "not open yet") {
		t.Fatalf("pre-start pick error = %v, want explicit-start rejection", err)
	}
	started, err := service.AdminStartDraft(commissionerRequest)
	if err != nil || !started {
		t.Fatalf("commissioner start = %v, %v", started, err)
	}
	startedState := service.store.Snapshot()
	if !startedState.DraftStarted || startedState.DraftStartedAt.IsZero() || startedState.ClockDeadline.IsZero() {
		t.Fatalf("draft start state = %+v", startedState)
	}

	beforeUnauthorizedPick := service.store.Snapshot()
	if _, _, _, err := service.MakePick(teamTwoRequest, primary.TeamID, "pool-002"); err == nil {
		t.Fatal("team-2 manager picked for team-1 while team-1 was on the clock")
	}
	if after := service.store.Snapshot(); !reflect.DeepEqual(beforeUnauthorizedPick.Picks, after.Picks) {
		t.Fatalf("rejected cross-team pick mutated draft: before=%v after=%v", beforeUnauthorizedPick.Picks, after.Picks)
	}
	managerPick, _, _, err := service.MakePick(primaryRequest, primary.TeamID, "pool-001")
	if err != nil || managerPick.MadeBy != "manager" || managerPick.Number != 1 {
		t.Fatalf("manager pick = %+v, %v", managerPick, err)
	}
	// pool-002 is the next-best ADP fallback. Team two's deliberately lower
	// pool-014 board entry must win instead, proving seat-scoped Board input.
	forcedBoardPick, forcedBoardPlayer, forcedBoardTeam, err := service.AdminForceAutopick(commissionerRequest, ForceCurrentPickConfirmation, draftCurrentPickToken(service.store.Snapshot()))
	if err != nil || forcedBoardPlayer.ID != "pool-014" || forcedBoardTeam.ID != "team-2" || forcedBoardPick.MadeBy != "commissioner" {
		t.Fatalf("board AUTO = pick:%+v player:%+v team:%+v err:%v", forcedBoardPick, forcedBoardPlayer, forcedBoardTeam, err)
	}
	if service.store.Snapshot().Picks[1].PlayerID != "pool-014" {
		t.Fatal("board AUTO did not persist the selected Big Board player")
	}
	if err := service.BoardClear(teamTwoRequest); err != nil {
		t.Fatalf("clear team-2 board before fallback AUTO: %v", err)
	}
	if got := service.store.Snapshot().Boards["claimant@example.com"]; len(got) != 0 {
		t.Fatalf("team-2 board clear targeted wrong owner: %v", got)
	}
	// The next unclaimed seat has no board, so forced AUTO separately proves
	// the pool's best-available branch chooses pool-002 rather than pool-014.
	fallbackPick, fallbackPlayer, fallbackTeam, err := service.AdminForceAutopick(commissionerRequest, ForceCurrentPickConfirmation, draftCurrentPickToken(service.store.Snapshot()))
	if err != nil || fallbackPlayer.ID != "pool-002" || fallbackTeam.ID != "team-3" || fallbackPick.MadeBy != "commissioner" {
		t.Fatalf("best-available AUTO = pick:%+v player:%+v team:%+v err:%v", fallbackPick, fallbackPlayer, fallbackTeam, err)
	}

	// Complete the draft through the same commissioner AUTO operation used
	// for an absent manager. Every pick keeps provenance, clock state, and
	// roster capacity coherent at the terminal transition.
	totalPicks := len(service.Teams()) * CurrentDraftRounds()
	for len(service.store.Snapshot().Picks) < totalPicks {
		if _, _, _, err := service.AdminForceAutopick(commissionerRequest, ForceCurrentPickConfirmation, draftCurrentPickToken(service.store.Snapshot())); err != nil {
			t.Fatalf("commissioner AUTO at pick %d: %v", len(service.store.Snapshot().Picks)+1, err)
		}
	}
	state := service.store.Snapshot()
	if !draftComplete(state) || len(state.Picks) != totalPicks || !state.ClockDeadline.IsZero() {
		t.Fatalf("terminal draft state = complete:%v picks:%d/%d deadline:%v", draftComplete(state), len(state.Picks), totalPicks, state.ClockDeadline)
	}
	for _, team := range service.Teams() {
		roster, drafted := service.rosterForTeam(state, team.ID)
		if !drafted || len(roster) != CurrentDraftRounds() {
			t.Fatalf("%s roster = %d drafted:%v, want %d drafted", team.ID, len(roster), drafted, CurrentDraftRounds())
		}
	}
	terminalTeam := service.TeamData(primaryRequest)
	if terminalTeam["drafted"] != true {
		t.Fatalf("team terminal after draft = %+v, want drafted=true", terminalTeam["drafted"])
	}

	// Waiver add/drop is filed first and materialized by the scheduled run;
	// the replayed roster and append-only transaction are the assertions.
	teamOneRoster, _ := service.rosterForTeam(state, "team-1")
	if len(teamOneRoster) != CurrentDraftRounds() {
		t.Fatalf("team-1 pre-waiver roster = %d, want %d", len(teamOneRoster), CurrentDraftRounds())
	}
	waiverAdd := pool[39].ID
	waiverDrop := ""
	for _, player := range teamOneRoster {
		if player.Position != "QB" {
			waiverDrop = player.ID
			break
		}
	}
	if waiverDrop == "" {
		t.Fatal("waiver fixture needs a non-quarterback drop so the lineup path stays covered")
	}
	claimMessage, err := service.FileClaim(primaryRequest, "team-1", waiverAdd, waiverDrop, 0)
	if err != nil || !strings.Contains(claimMessage, "Claim filed") {
		t.Fatalf("waiver filing = %q, %v", claimMessage, err)
	}
	waiverPool := make(map[string]Player, len(pool))
	for _, player := range pool {
		waiverPool[player.ID] = player
	}
	waiverResults, err := service.store.ProcessWaivers(now.Add(24*time.Hour), service.cfg, nil, waiverPool, CurrentRoster().Total())
	if err != nil || len(waiverResults) != 1 || waiverResults[0].Outcome != "won" {
		t.Fatalf("waiver resolution = %+v, %v", waiverResults, err)
	}
	state = service.store.Snapshot()
	if len(state.Transactions) == 0 || state.Transactions[len(state.Transactions)-1].Type != "claim" {
		t.Fatalf("waiver transaction ledger = %+v", state.Transactions)
	}
	teamOneRoster, _ = service.rosterForTeam(state, "team-1")
	if containsPlayer(teamOneRoster, waiverDrop) || !containsPlayer(teamOneRoster, waiverAdd) {
		t.Fatalf("waiver roster = %v, want drop %s and add %s", playerIDs(teamOneRoster), waiverDrop, waiverAdd)
	}

	// A managed trade runs through propose -> accept -> execute under the
	// configured no-review policy. The two sides' derived rosters must swap
	// the exact assets named by the trade transaction.
	service.cfg.Trades.Veto = "none"
	teamTwoRoster, _ := service.rosterForTeam(state, "team-2")
	if len(teamOneRoster) == 0 || len(teamTwoRoster) == 0 {
		t.Fatalf("trade fixture rosters = team-1:%v team-2:%v", playerIDs(teamOneRoster), playerIDs(teamTwoRoster))
	}
	tradeGive := ""
	for _, player := range teamOneRoster {
		if player.Position != "QB" {
			tradeGive = player.ID
			break
		}
	}
	if tradeGive == "" {
		t.Fatal("trade fixture needs a non-quarterback asset so the lineup path stays covered")
	}
	tradeGet := teamTwoRoster[0].ID
	if _, err := service.ProposeTrade(primaryRequest, "team-1", "team-2", []string{tradeGive}, []string{tradeGet}, nil, "journey acceptance"); err != nil {
		t.Fatalf("propose trade: %v", err)
	}
	state = service.store.Snapshot()
	if len(state.TradeOffers) != 1 || state.TradeOffers[0].Status != TradeStatusOpen {
		t.Fatalf("open trade offer = %+v", state.TradeOffers)
	}
	offerID := state.TradeOffers[0].ID
	transactionsBeforeRejectedAccept := len(state.Transactions)
	if _, err := service.AcceptTrade(primaryRequest, "team-2", offerID, tradeAcceptConfirmation); err == nil {
		t.Fatal("proposing manager accepted the receiving team's offer")
	}
	state = service.store.Snapshot()
	if state.TradeOffers[0].Status != TradeStatusOpen || len(state.Transactions) != transactionsBeforeRejectedAccept {
		t.Fatalf("rejected cross-team trade accept mutated state: offer=%+v transactions=%d/%d", state.TradeOffers[0], len(state.Transactions), transactionsBeforeRejectedAccept)
	}
	if _, err := service.AcceptTrade(teamTwoRequest, "team-2", offerID, tradeAcceptConfirmation); err != nil {
		t.Fatalf("accept trade: %v", err)
	}
	state = service.store.Snapshot()
	if len(state.TradeOffers) != 1 || state.TradeOffers[0].Status != TradeStatusExecuted {
		t.Fatalf("executed trade offer = %+v", state.TradeOffers)
	}
	tradeTxn := state.Transactions[len(state.Transactions)-1]
	if tradeTxn.Type != "trade" || tradeTxn.TeamID != "team-1" || tradeTxn.OtherTeamID != "team-2" {
		t.Fatalf("trade transaction provenance = %+v", tradeTxn)
	}
	teamOneRoster, _ = service.rosterForTeam(state, "team-1")
	teamTwoRoster, _ = service.rosterForTeam(state, "team-2")
	if !containsPlayer(teamOneRoster, tradeGet) || containsPlayer(teamOneRoster, tradeGive) || !containsPlayer(teamTwoRoster, tradeGive) || containsPlayer(teamTwoRoster, tradeGet) {
		t.Fatalf("trade replay = team-1:%v team-2:%v, want swapped %s/%s", playerIDs(teamOneRoster), playerIDs(teamTwoRoster), tradeGive, tradeGet)
	}

	// Lineup changes are open before kickoff and fail closed afterward. The
	// lock is tied to the player's NFL game, not to a client-side timestamp.
	var quarterback Player
	for _, player := range teamOneRoster {
		if player.Position == "QB" {
			quarterback = player
			break
		}
	}
	if quarterback.ID == "" {
		t.Fatalf("team-1 lost its required QB after trade: %v", playerIDs(teamOneRoster))
	}
	beforeUnauthorizedLineup := service.store.Snapshot()
	if _, err := service.SetLineup(teamTwoRequest, "team-1", 1, "QB", quarterback.ID); err == nil {
		t.Fatal("team-2 manager set team-1's quarterback")
	}
	if after := service.store.Snapshot(); !reflect.DeepEqual(beforeUnauthorizedLineup.Lineups, after.Lineups) {
		t.Fatalf("rejected cross-team lineup action mutated state: before=%v after=%v", beforeUnauthorizedLineup.Lineups, after.Lineups)
	}
	if _, err := service.SetLineup(primaryRequest, "team-1", 1, "QB", quarterback.ID); err != nil {
		t.Fatalf("open-week lineup change: %v", err)
	}
	service.SetScheduleSource(func() []GameInfo {
		return []GameInfo{{ID: "journey-kickoff", Week: 1, Kickoff: now.Add(-time.Minute), Away: quarterback.NFLTeam, Home: "MIA"}}
	})
	if _, err := service.SetLineup(primaryRequest, "team-1", 1, "QB", ""); err == nil || !strings.Contains(err.Error(), "locked") {
		t.Fatalf("post-kickoff lineup clear = %v, want lock rejection", err)
	}

	// Make sure the injected clock remains the same deterministic instant;
	// this catches accidental wall-clock reads in the journey's decisions.
	if !clock.Equal(now) {
		t.Fatalf("journey clock moved unexpectedly: %v, want %v", *clock, now)
	}
}

// TestSeasonJourneyCapacitySupportsFourteenConfiguredTeams complements the
// eight-team end-to-end path with the recommended 14-team deep-league shape.
// It starts and completes through authenticated commissioner service APIs so
// player-source readiness, roster viability, pick provenance, and capacity
// are all exercised at the same boundary production uses.
func TestSeasonJourneyCapacitySupportsFourteenConfiguredTeams(t *testing.T) {
	baseline := DefaultConfig()
	clearSeatTrim()
	clearRosterShape()
	config := baseline
	config.Teams = make([]TeamSeed, 14)
	for index := range config.Teams {
		division := "East"
		if index >= 7 {
			division = "West"
		}
		config.Teams[index] = TeamSeed{
			ID:           fmt.Sprintf("team-%d", index+1),
			Name:         fmt.Sprintf("Configured %02d", index+1),
			Abbreviation: fmt.Sprintf("C%02d", index+1),
			Division:     division,
			Tone:         []string{"cyan", "blue", "violet", "lime", "orange", "gold", "magenta", "pink"}[index%8],
		}
	}
	config.RosterPresetName = "deep-league"
	config.RosterConflict = false
	config.Roster = rosterPresets["deep-league"]
	config.Rounds = config.Roster.Total()
	warnings, err := validateConfig(&config)
	if err != nil {
		t.Fatalf("fourteen-team config rejected: %v", err)
	}
	if len(warnings) == 0 {
		t.Fatal("fourteen-team config lost its deep-league warning")
	}
	t.Cleanup(func() {
		clearSeatTrim()
		clearRosterShape()
		applyActiveConfig(baseline)
	})
	applyActiveConfig(config)

	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	service, _ := newNotifyTestService(t, now.Add(time.Hour), now)
	service.demoMode = false
	service.cfg = config
	service.store.draftLifecycleBypass = false
	pool := seasonCapacityPool()
	service.SetPlayerSource(func() ([]Player, int64, string) { return pool, 1, "live" })
	t.Setenv("COMMISSIONER_EMAILS", "commissioner@example.com")
	commissionerRequest := authenticatedJourneyRequest(t, "commissioner@example.com", "Commissioner", "/admin")

	capacity := len(service.Teams()) * CurrentDraftRounds()
	if capacity != 196 || len(pool) != capacity || activeTeamCount(nil) != 14 {
		t.Fatalf("fourteen-team capacity = %d, pool = %d, active teams = %d; want 196/196/14", capacity, len(pool), activeTeamCount(nil))
	}
	started, err := service.AdminStartDraft(commissionerRequest)
	if err != nil || !started {
		t.Fatalf("authenticated fourteen-team service start = %v, %v", started, err)
	}
	for number := 1; number <= capacity; number++ {
		pick, _, team, err := service.AdminForceAutopick(commissionerRequest, ForceCurrentPickConfirmation, draftCurrentPickToken(service.store.Snapshot()))
		if err != nil {
			t.Fatalf("fourteen-team commissioner AUTO %d (%s): %v", number, team.ID, err)
		}
		if pick.Number != number || pick.MadeBy != "commissioner" {
			t.Fatalf("fourteen-team pick %d provenance = %+v", number, pick)
		}
	}
	state := service.store.Snapshot()
	if !draftComplete(state) || len(state.Picks) != capacity || !state.ClockDeadline.IsZero() {
		t.Fatalf("fourteen-team terminal state = complete:%v picks:%d/%d deadline:%v", draftComplete(state), len(state.Picks), capacity, state.ClockDeadline)
	}
	for _, team := range service.Teams() {
		roster, drafted := service.rosterForTeam(state, team.ID)
		if !drafted || len(roster) != config.Roster.Total() {
			t.Fatalf("%s deep-league roster = %d drafted:%v, want %d drafted", team.ID, len(roster), drafted, config.Roster.Total())
		}
	}
}

func authenticatedJourneyRequest(t *testing.T, email, name, path string) *http.Request {
	t.Helper()
	authn := auth.New(nil, auth.Options{Provider: auth.ProviderFunc(func(*http.Request) (auth.User, bool) {
		return auth.User{ID: email, Email: email, Name: name}, true
	})})
	var authenticated *http.Request
	authn.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authenticated = r
		w.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, path, nil))
	if authenticated == nil {
		t.Fatalf("authenticate %s: middleware did not forward the request", email)
	}
	return authenticated
}

func seasonCapacityPool() []Player {
	// Fourteen blocks of fourteen ensure every snake-draft seat receives the
	// same viable deep-league composition: ten starters plus four bench spots.
	roundPositions := []string{"QB", "RB", "RB", "WR", "WR", "TE", "RB", "QB", "DST", "K", "RB", "WR", "TE", "QB"}
	players := make([]Player, 0, 14*len(roundPositions))
	for round, position := range roundPositions {
		for seat := 0; seat < 14; seat++ {
			index := round*14 + seat + 1
			players = append(players, Player{
				ID:         fmt.Sprintf("capacity-%03d", index),
				Name:       fmt.Sprintf("Capacity Player %03d", index),
				Position:   position,
				NFLTeam:    "CIN",
				ADP:        float64(index),
				ADPRank:    index,
				Projection: 25 - float64(index)*0.05,
				Status:     "Available",
			})
		}
	}
	return players
}

func seasonJourneyPool() []Player {
	positions := []string{"QB", "RB", "WR", "TE", "DST", "K"}
	players := make([]Player, 0, 48)
	for index := 0; index < 48; index++ {
		players = append(players, Player{
			ID:         fmt.Sprintf("pool-%03d", index+1),
			Name:       fmt.Sprintf("Journey Player %03d", index+1),
			Position:   positions[index%len(positions)],
			NFLTeam:    "CIN",
			ADP:        float64(index + 1),
			ADPRank:    index + 1,
			Projection: 20 - float64(index)*0.1,
			Status:     "Available",
		})
	}
	return players
}

func containsPlayer(players []Player, id string) bool {
	for _, player := range players {
		if player.ID == id {
			return true
		}
	}
	return false
}

func playerIDs(players []Player) []string {
	ids := make([]string, 0, len(players))
	for _, player := range players {
		ids = append(ids, player.ID)
	}
	return ids
}
