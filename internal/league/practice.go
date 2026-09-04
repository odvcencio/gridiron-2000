package league

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Practice draft (owner ask, 2026-09-04: "a draft room feature around
// simulation so people can get a feel for a few picks in a round").
//
// A PracticeDraft is a solo, ephemeral copy of the draft room: the
// signed-in manager sits in their real seat, the other seats are played by
// bots that draft from each seat's real Big Board (then house order — the
// exact autopickChoice the real clock uses), the pool is the real pool,
// and the pick clock is the league's real pick clock. It runs on a second
// Service whose Store was built with NewStore(""): the explicit in-memory
// mode where persistLocked is a no-op, so nothing a practice does can
// reach data/league.db. The sandbox never installs the real draft event
// sink, never records presence on the real tracker, and never reads the
// real store after construction, so the real room cannot see it either.
//
// One session per member email lives in a PracticeRegistry, evicted after
// practiceIdleTTL without a page load or action, and capped at
// practiceMaxSessions. The registry's single one-second ticker drives every
// session's bots and clocks; there is no goroutine per session.

const (
	// PracticeRoomPath is the practice room's own route; every pool href,
	// fragment endpoint, and action the sandbox renders resolves under it,
	// never under /draft (roomPath, below).
	PracticeRoomPath = "/draft/practice"
	// PracticeLiveHubName names the practice room's own hub, so the page's
	// live binds and regions never listen on the real room's "draft-live".
	PracticeLiveHubName = "practice-live"
	// PracticeRoundSpan is how many rounds one practice runs from its start
	// round: "a few picks in a round" — enough to feel the clock and the
	// snake turn without drafting a whole roster.
	PracticeRoundSpan = 3

	practiceIdleTTL     = 30 * time.Minute
	practiceMaxSessions = 32
	practiceTickPeriod  = time.Second
)

// PracticeStartOption is one start-round choice the practice page offers.
type PracticeStartOption struct {
	Round  int
	Label  string
	Detail string
}

// PracticeStartOptions lists the start rounds a practice may begin at,
// with the plain label each carries in the UI. Rounds past the draft's
// last round are dropped, so a 14-round league never offers "round 15".
func PracticeStartOptions() []PracticeStartOption {
	all := []PracticeStartOption{
		{Round: 1, Label: "Early rounds", Detail: "Start at pick 1."},
		{Round: 5, Label: "Middle rounds", Detail: "Rounds 1 to 4 are already made."},
		{Round: 10, Label: "Late rounds", Detail: "Rounds 1 to 9 are already made."},
		{Round: 15, Label: "Specialists", Detail: "Rounds 1 to 14 are already made; kickers, defenses, and punters come off the board."},
	}
	last := CurrentDraftRounds()
	out := make([]PracticeStartOption, 0, len(all))
	for _, option := range all {
		if option.Round <= last {
			out = append(out, option)
		}
	}
	return out
}

// clampPracticeStartRound maps a submitted round to one of the offered
// options; anything else starts at round 1.
func clampPracticeStartRound(round int) int {
	for _, option := range PracticeStartOptions() {
		if option.Round == round {
			return round
		}
	}
	return 1
}

// practiceThinkTime is a bot's delay before it picks: two to five seconds,
// deterministic from the pick number so a test can predict it and a
// manager sees an unhurried, human-feeling cadence rather than an instant
// wall of picks.
func practiceThinkTime(pickNumber int) time.Duration {
	if pickNumber < 0 {
		pickNumber = -pickNumber
	}
	return time.Duration(2+pickNumber%4) * time.Second
}

// PracticeAvailability says whether the request's viewer may start a
// practice right now, with the plain-language reason when not: the
// disabled-with-reason pattern the product contract requires.
type PracticeAvailability struct {
	Allowed  bool
	Reason   string
	SignedIn bool
	HasSeat  bool
	TeamID   string
}

// Map renders the availability for a page's data map.
func (a PracticeAvailability) Map() map[string]any {
	return map[string]any{
		"allowed":   a.Allowed,
		"reason":    a.Reason,
		"signed_in": a.SignedIn,
		"has_seat":  a.HasSeat,
		"href":      PracticeRoomPath,
	}
}

// PracticeAvailability resolves the gate for r's viewer against the REAL
// league: a seat is required, and the real draft must not have started or
// completed (a live draft is the real thing; practice would only confuse).
func (s *Service) PracticeAvailability(r *http.Request) PracticeAvailability {
	return s.practiceAvailabilityForState(r, s.store.Snapshot())
}

// practiceAvailabilityForState is PracticeAvailability over a snapshot the
// caller already holds — draftData and DashboardData read it on every
// render, and a second whole-state clone per request is draft-night cost
// for nothing.
func (s *Service) practiceAvailabilityForState(r *http.Request, state PersistedState) PracticeAvailability {
	user, signedIn := s.CurrentUser(r)
	out := PracticeAvailability{SignedIn: signedIn}
	if !signedIn {
		out.Reason = "Sign in to practice."
		return out
	}
	member, ok := memberByEmail(state.Members, user.Email)
	teamID := strings.TrimSpace(member.TeamID)
	if !ok || teamID == "" || !knownTeam(teamID) {
		out.Reason = "You need a seat to practice."
		return out
	}
	out.HasSeat = true
	out.TeamID = teamID
	if draftComplete(state) {
		out.Reason = "The draft is complete."
		return out
	}
	if state.DraftStarted {
		out.Reason = "The real draft is live."
		return out
	}
	out.Allowed = true
	return out
}

// PracticeKey is the registry key for a signed-in viewer: the canonical
// email CurrentUser already resolved, lowercased. The practice hub stamps
// it on every connection so events route to one viewer only.
func PracticeKey(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func practiceKey(email string) string { return PracticeKey(email) }

// PracticeRegistry holds every open practice session, one per member.
type PracticeRegistry struct {
	base *Service

	mu       sync.Mutex
	sessions map[string]*PracticeDraft
	max      int
	ttl      time.Duration
	// sink receives every sandbox's draft events, tagged with the owning
	// viewer key, so the practice hub can route each event to that one
	// viewer's own connections and no one else's. nil until the app wires
	// the hub; a session started before that emits nothing.
	sink func(viewerKey string, event DraftEvent)
}

// NewPracticeRegistry builds an empty registry over the real Service.
func NewPracticeRegistry(base *Service) *PracticeRegistry {
	return &PracticeRegistry{
		base:     base,
		sessions: map[string]*PracticeDraft{},
		max:      practiceMaxSessions,
		ttl:      practiceIdleTTL,
	}
}

// SetEventSink installs the per-viewer event router (see sink above).
func (p *PracticeRegistry) SetEventSink(fn func(viewerKey string, event DraftEvent)) {
	p.mu.Lock()
	p.sink = fn
	p.mu.Unlock()
}

// Len reports how many sessions are open.
func (p *PracticeRegistry) Len() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.sessions)
}

// Start opens (or replaces) the viewer's practice at round. It refuses
// with PracticeAvailability's own reason when the gate is closed, and
// with a plain sentence when the registry is full.
func (p *PracticeRegistry) Start(r *http.Request, round int) (*PracticeDraft, error) {
	availability := p.base.PracticeAvailability(r)
	if !availability.Allowed {
		return nil, errors.New(availability.Reason)
	}
	user, _ := p.base.CurrentUser(r)
	key := practiceKey(user.Email)
	round = clampPracticeStartRound(round)
	now := p.base.clock()

	// Cap check first, then build OUTSIDE the lock: newPracticeDraft's
	// fast-forward can run (start round - 1) x teams autopick walks, and
	// every other session's tick and page load would otherwise wait on it.
	// The viewer's own open session does not count against the cap: a
	// restart replaces it.
	p.mu.Lock()
	p.sweepLocked(now)
	_, replacing := p.sessions[key]
	sink := p.sink
	full := !replacing && len(p.sessions) >= p.max
	p.mu.Unlock()
	if full {
		return nil, errPracticeRegistryFull
	}
	session, err := newPracticeDraft(p.base, key, availability.TeamID, round, now, sink)
	if err != nil {
		// The viewer's in-progress practice, if any, is untouched: a
		// failed build never costs them the session they had.
		return nil, err
	}
	p.mu.Lock()
	previous := p.sessions[key]
	if previous == nil && len(p.sessions) >= p.max {
		p.mu.Unlock()
		session.close()
		return nil, errPracticeRegistryFull
	}
	p.sessions[key] = session
	p.mu.Unlock()
	if previous != nil {
		previous.close()
	}
	return session, nil
}

var errPracticeRegistryFull = errors.New("Too many practice drafts are open right now. Try again in a few minutes.")

// Current returns the viewer's open session, touching its idle timer.
func (p *PracticeRegistry) Current(r *http.Request) (*PracticeDraft, bool) {
	user, signedIn := p.base.CurrentUser(r)
	if !signedIn {
		return nil, false
	}
	p.mu.Lock()
	session := p.sessions[practiceKey(user.Email)]
	p.mu.Unlock()
	if session == nil {
		return nil, false
	}
	session.touch(p.base.clock())
	return session, true
}

// Session returns the open session for a registry key (PracticeKey),
// touching its idle timer. The practice hub uses it on join.
func (p *PracticeRegistry) Session(key string) (*PracticeDraft, bool) {
	p.mu.Lock()
	session := p.sessions[key]
	p.mu.Unlock()
	if session == nil {
		return nil, false
	}
	session.touch(p.base.clock())
	return session, true
}

// Leave closes and forgets the viewer's session. It reports whether one
// existed.
func (p *PracticeRegistry) Leave(r *http.Request) bool {
	user, signedIn := p.base.CurrentUser(r)
	if !signedIn {
		return false
	}
	key := practiceKey(user.Email)
	p.mu.Lock()
	session := p.sessions[key]
	delete(p.sessions, key)
	p.mu.Unlock()
	if session == nil {
		return false
	}
	session.close()
	return true
}

// Sweep closes every session idle for longer than the TTL and reports how
// many it evicted.
func (p *PracticeRegistry) Sweep(now time.Time) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.sweepLocked(now)
}

func (p *PracticeRegistry) sweepLocked(now time.Time) int {
	evicted := 0
	for key, session := range p.sessions {
		if now.Sub(session.lastTouchedAt()) > p.ttl {
			session.close()
			delete(p.sessions, key)
			evicted++
		}
	}
	return evicted
}

// Tick advances every open session by one instant, then sweeps.
func (p *PracticeRegistry) Tick(now time.Time) {
	p.mu.Lock()
	sessions := make([]*PracticeDraft, 0, len(p.sessions))
	for _, session := range p.sessions {
		sessions = append(sessions, session)
	}
	p.mu.Unlock()
	for _, session := range sessions {
		session.Tick(now)
	}
	p.Sweep(now)
}

// Run is the registry's one ticker loop; app_build.go registers it as a
// starter beside the real draft clock. It stops with ctx.
func (p *PracticeRegistry) Run(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(practiceTickPeriod)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				p.mu.Lock()
				for key, session := range p.sessions {
					session.close()
					delete(p.sessions, key)
				}
				p.mu.Unlock()
				return
			case <-ticker.C:
				p.Tick(p.base.clock())
			}
		}
	}()
}

// PracticeDraft is one member's open practice session.
type PracticeDraft struct {
	svc    *Service
	base   *Service
	key    string
	teamID string

	startRound int
	endRound   int

	mu          sync.Mutex
	lastTouched time.Time
	botDuePick  int
	botDue      time.Time
	done        bool
	closed      bool
}

// newPracticeDraft builds the sandbox: a second Service over an in-memory
// Store seeded from a clone of the real snapshot, sharing the real pool
// (frozen for the session — one snapshot, never re-read, so a resync
// mid-practice cannot reorder the board under the manager), then
// fast-forwarded to the start round with the bot strategy.
func newPracticeDraft(base *Service, key, teamID string, startRound int, now time.Time, sink func(string, DraftEvent)) (*PracticeDraft, error) {
	pool := base.pool()
	if pool.unavailable || len(pool.byID) == 0 {
		return nil, errors.New("No player pool is available to practice with. Try again after the next player-data refresh.")
	}
	real := base.store.Snapshot()
	state := cloneState(real)
	state.Picks = []DraftPick{}
	state.Autopick = map[string]bool{}
	state.DraftStarted = true
	state.DraftStartedAt = now.UTC()
	state.ClockPaused = false
	state.ClockRemainingSec = 0
	state.ClockDeadline = time.Time{}
	state.ClockDurationSec = int(base.pickClock(real) / time.Second)

	store := NewStoreWithIdentity("", base.identityResolver)
	store.mu.Lock()
	store.state = state
	store.mu.Unlock()

	base.poolMu.Lock()
	poolStatusFn := base.poolStatusFn
	scheduleFn := base.scheduleFn
	historicalFn := base.historicalFn
	weekStatsFn := base.weekStatsFn
	injuryFn := base.injuryFn
	matchupFn := base.matchupFn
	matchupLabel := base.matchupLabel
	statsUpdatedAtFn := base.statsUpdatedAtFn
	base.poolMu.Unlock()

	svc := &Service{
		store:             store,
		identityResolver:  base.identityResolver,
		draftAt:           base.draftAt,
		draftTZ:           base.draftTZ,
		demoMode:          false,
		teams:             base.Teams(),
		players:           base.players,
		cfg:               base.cfg,
		now:               base.clock,
		presence:          newPresenceTracker(now),
		pickClockDefault:  base.pickClockDefault,
		poolStatusFn:      poolStatusFn,
		scheduleFn:        scheduleFn,
		historicalFn:      historicalFn,
		weekStatsFn:       weekStatsFn,
		injuryFn:          injuryFn,
		matchupFn:         matchupFn,
		matchupLabel:      matchupLabel,
		statsUpdatedAtFn:  statsUpdatedAtFn,
		avatarRoot:        base.avatarRoot,
		avatarDurableRoot: base.avatarDurableRoot,
		defaultBadgeRoot:  base.defaultBadgeRoot,
		motifRoot:         base.motifRoot,
		roomPath:          PracticeRoomPath,
		liveHub:           PracticeLiveHubName,
		poolOverride:      func() playerPool { return pool },
	}
	svc.feed = newLiveFeed(nil, svc)

	last := CurrentDraftRounds()
	endRound := startRound + PracticeRoundSpan - 1
	if endRound > last {
		endRound = last
	}
	session := &PracticeDraft{
		svc:         svc,
		base:        base,
		key:         key,
		teamID:      teamID,
		startRound:  startRound,
		endRound:    endRound,
		lastTouched: now,
	}
	if sink != nil {
		svc.SetDraftEventSink(func(event DraftEvent) { sink(key, event) })
	}
	session.recordPresence(now)
	if err := session.fastForward(now); err != nil {
		session.close()
		return nil, err
	}
	// Arm the first live pick's clock. The viewer's seat runs on it; a bot
	// seat ignores it and picks on think time, then re-arms for the next
	// seat the same way the real room does.
	if err := svc.store.ArmClock(now.Add(svc.pickClock(svc.store.Snapshot()))); err != nil {
		session.close()
		return nil, err
	}
	return session, nil
}

// fastForward makes every pick before the start round with the bot
// strategy, the viewer's own seat included (its real board first, then
// house order), so a "late rounds" practice opens on a realistic board.
func (d *PracticeDraft) fastForward(now time.Time) error {
	state := d.svc.store.Snapshot()
	teams := activeTeamCount(state.DraftOrder)
	target := (d.startRound - 1) * teams
	for number := 1; number <= target; number++ {
		state = d.svc.store.Snapshot()
		teamID := teamOnClock(state.DraftOrder, number)
		playerID, ok := d.svc.autopickChoice(state, teamID)
		if !ok {
			return fmt.Errorf("practice: no legal candidate for %s at pick %d", teamID, number)
		}
		if _, err := d.svc.store.MakePick(teamID, playerID, "auto", now, time.Time{}); err != nil {
			return fmt.Errorf("practice: pick %d: %w", number, err)
		}
	}
	return nil
}

// recordPresence marks every seat's operators HERE on the sandbox's own
// tracker. Practice is about the feel of the clock, not attendance: no
// seat is ever NOT SEEN, so the real room's short-clock rule never fires.
func (d *PracticeDraft) recordPresence(now time.Time) {
	state := d.svc.store.Snapshot()
	for _, team := range d.svc.Teams() {
		for _, key := range d.svc.presenceKeysForTeam(state, team.ID) {
			d.svc.presence.record(key, now)
		}
	}
}

func (d *PracticeDraft) touch(now time.Time) {
	d.mu.Lock()
	if now.After(d.lastTouched) {
		d.lastTouched = now
	}
	d.mu.Unlock()
}

func (d *PracticeDraft) lastTouchedAt() time.Time {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.lastTouched
}

// close stops the sandbox's event drain. Safe to call more than once.
func (d *PracticeDraft) close() {
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return
	}
	d.closed = true
	d.done = true
	d.mu.Unlock()
	d.svc.StopDraftEvents()
}

// TeamID is the viewer's seat.
func (d *PracticeDraft) TeamID() string { return d.teamID }

// StartRound and EndRound bound the practice's live rounds.
func (d *PracticeDraft) StartRound() int { return d.startRound }
func (d *PracticeDraft) EndRound() int   { return d.endRound }

// Complete reports whether the practice has run past its end round.
func (d *PracticeDraft) Complete() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.done
}

// Snapshot is the sandbox's own persisted-shape state.
func (d *PracticeDraft) Snapshot() PersistedState { return d.svc.store.Snapshot() }

// ViewerOnClock reports whether the viewer's seat is due to pick.
func (d *PracticeDraft) ViewerOnClock() bool {
	if d.Complete() {
		return false
	}
	state := d.svc.store.Snapshot()
	if draftComplete(state) {
		return false
	}
	return teamOnClock(state.DraftOrder, len(state.Picks)+1) == d.teamID
}

// Tick advances the practice by one instant: presence, then either the
// viewer's clock (the real clockTick — arm, expire, autopick) or the
// on-clock bot's think time and pick.
func (d *PracticeDraft) Tick(now time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.done {
		return
	}
	// The real draft going live ends every practice: the manager's real
	// clock is what matters now, and the strip says so (real_started,
	// practiceMap) with a link to the real room.
	if d.base.store.draftStartedQuick() {
		d.finishLocked(d.svc.store.Snapshot(), now)
		return
	}
	d.recordPresence(now)
	state := d.svc.store.Snapshot()
	if d.overLocked(state) {
		d.finishLocked(state, now)
		return
	}
	number := len(state.Picks) + 1
	teamID := teamOnClock(state.DraftOrder, number)
	if teamID == d.teamID {
		d.botDuePick = 0
		d.svc.clockTick(now)
		if after := d.svc.store.Snapshot(); d.overLocked(after) {
			d.finishLocked(after, now)
		}
		return
	}
	if d.botDuePick != number {
		d.botDuePick = number
		d.botDue = now.Add(practiceThinkTime(number))
		return
	}
	if now.Before(d.botDue) {
		return
	}
	playerID, ok := d.svc.autopickChoice(state, teamID)
	if !ok {
		log.Printf("practice draft: no legal candidate for %s at pick %d; ending the practice", teamID, number)
		d.finishLocked(state, now)
		return
	}
	total := activeTeamCount(state.DraftOrder) * CurrentDraftRounds()
	nextDeadline := time.Time{}
	if number < total {
		nextDeadline = now.Add(d.svc.pickClock(state))
	}
	pick, err := d.svc.store.MakePick(teamID, playerID, "auto", now, nextDeadline)
	if err != nil {
		log.Printf("practice draft: bot pick %d failed: %v", number, err)
		return
	}
	snapshot := d.svc.store.Snapshot()
	d.svc.emitDraft("draft:pick", d.svc.draftPickPayload(snapshot, pick, now))
	d.svc.maybeEmitDraftComplete(state, snapshot, now)
	if d.overLocked(snapshot) {
		d.finishLocked(snapshot, now)
	}
}

// overLocked reports whether the next pick falls past the end round (or
// the draft itself is complete).
func (d *PracticeDraft) overLocked(state PersistedState) bool {
	if draftComplete(state) {
		return true
	}
	next := len(state.Picks) + 1
	return pickRound(activeTeamCount(state.DraftOrder), next) > d.endRound
}

// finishLocked closes the live part of the practice: the clock stops and
// a draft:state goes out so the room re-renders its "practice complete"
// strip and drops its pick controls.
func (d *PracticeDraft) finishLocked(state PersistedState, now time.Time) {
	d.done = true
	if err := d.svc.store.ClearClock(); err != nil {
		log.Printf("practice draft: clear clock: %v", err)
	}
	after := d.svc.store.Snapshot()
	d.svc.emitDraftState(after, now, true, draftComplete(after))
}

// MakePick is the viewer's own pick, through the sandbox Service's
// MakePick — the same validation the real room applies (on the clock,
// available, roster viability, limits, scarcity).
func (d *PracticeDraft) MakePick(r *http.Request, playerID string) (DraftPick, Player, Team, error) {
	if d.Complete() {
		return DraftPick{}, Player{}, Team{}, errors.New("This practice is complete. Start another to keep drafting.")
	}
	pick, player, team, err := d.svc.MakePick(r, d.teamID, playerID)
	if err != nil {
		return pick, player, team, err
	}
	d.touch(d.svc.clock())
	d.mu.Lock()
	d.botDuePick = 0
	state := d.svc.store.Snapshot()
	if d.overLocked(state) {
		d.finishLocked(state, d.svc.clock())
	}
	d.mu.Unlock()
	return pick, player, team, nil
}

// ToggleAutopick flips the viewer's practice autopick flag, sandbox only.
func (d *PracticeDraft) ToggleAutopick(r *http.Request) (bool, string, error) {
	return d.svc.ToggleAutopick(r, d.teamID)
}

// LiveView is the practice room's full bind object for its own live.json.
func (d *PracticeDraft) LiveView(r *http.Request) map[string]any {
	return d.svc.DraftLiveView(r)
}

// Fingerprint is the sandbox's own state fingerprint.
func (d *PracticeDraft) Fingerprint() string {
	return d.svc.StateFingerprint(0)
}

// Data renders the practice room's draft data: the sandbox's own draftData
// (read-only viewer resolution — practice never provisions anything) plus
// the practice keys the strip, the checklist entry, and the room's
// practice-only branches read. Commissioner surfaces are hidden: a
// practice has no commissioner.
func (d *PracticeDraft) Data(r *http.Request) map[string]any {
	now := d.svc.clock()
	data := d.svc.draftData(r, true, true)
	complete := d.Complete()
	if viewer, ok := data["viewer"].(map[string]any); ok {
		viewer["is_commissioner"] = false
	}
	realStarted := d.base.store.draftStartedQuick()
	if complete || realStarted {
		data["can_pick"] = false
		data["viewer_on_clock"] = false
	}
	// Readiness is not part of a practice: the pick bar's "check in for
	// draft night" branch keys on viewer_ready, so a practice reads as
	// checked in and the bar shows the autopick state instead.
	data["viewer_ready"] = true
	state := d.svc.store.Snapshot()
	round := pickRound(activeTeamCount(state.DraftOrder), len(state.Picks)+1)
	if complete {
		round = d.endRound
	}
	data["practice"] = d.practiceMap(now, round, complete, realStarted)
	// region_interval (practice_handlers.go / page.gsx): the practice room's
	// fallback regions also poll, so a refused hub connection (the hub's
	// MaxClients, a proxy that drops the upgrade) degrades to a slower
	// room, never a silent one. The real room stamps "" and never polls.
	data["region_interval"] = practicePollInterval
	return data
}

// practicePollInterval is the practice room's region poll (see Data).
const practicePollInterval = "8s"

// practiceMap is the "practice" key: active, the round window, the real
// draft's own scheduled start in league-local time with its relative
// phrase, and the viewer's seat.
func (d *PracticeDraft) practiceMap(now time.Time, round int, complete bool, realStarted bool) map[string]any {
	real := d.base.store.Snapshot()
	summary := d.base.draftSummaryForState(now, real)
	realAt := d.base.EffectiveDraftAt(real)
	label := ""
	relative := ""
	if published, _ := summary["published"].(bool); published {
		location := d.base.draftTZ
		if location == nil {
			location = time.UTC
		}
		label = realAt.In(location).Format(practiceTimeLayout)
		relative = relativeTime(now, realAt)
	}
	roundsLeft := d.endRound - round + 1
	if roundsLeft < 0 || complete {
		roundsLeft = 0
	}
	span := d.endRound - d.startRound + 1
	position := round - d.startRound + 1
	if position < 1 {
		position = 1
	}
	if position > span {
		position = span
	}
	// summary_short is the phone strip's one line; summary_full is the
	// desktop sentence and the phone Details body. Both are built here so
	// the room's own template carries no copy of its own for either.
	rounds := fmt.Sprintf("rounds %d to %d", d.startRound, d.endRound)
	if span == 1 {
		rounds = fmt.Sprintf("round %d", d.startRound)
	}
	realDraft := ""
	if label != "" {
		realDraft = " · the real draft starts " + label + " (" + relative + ")"
	}
	// The phone line carries no round: the h1 and the pill already say
	// it, and at 360-390 px the chip, the line, Details, and Leave must
	// share one row.
	short := "Picks don't count"
	full := "Practice draft · picks here do not count · " + rounds + realDraft + "."
	if complete {
		short = "Practice complete"
		full = "Practice complete · you drafted " + rounds + " · nothing was saved" + realDraft + "."
	}
	if realStarted {
		short = "The real draft has started"
		full = "The real draft has started · this practice is over · nothing was saved."
	}
	return map[string]any{
		"real_started":        realStarted,
		"real_room_href":      "/draft",
		"active":              true,
		"complete":            complete,
		"round":               round,
		"start_round":         d.startRound,
		"end_round":           d.endRound,
		"rounds_left":         roundsLeft,
		"span":                span,
		"position":            position,
		"team_id":             d.teamID,
		"team_name":           d.svc.teamByID(d.teamID).Name,
		"real_draft_label":    label,
		"real_draft_relative": relative,
		"real_draft_known":    label != "",
		"summary_short":       short,
		"summary_full":        full,
		"room_href":           "/draft",
		"href":                PracticeRoomPath,
	}
}

// practiceTimeLayout is the league's canonical league-local time label
// (the same "Mon Jan 2 · 3:04 PM MST" every kickoff, waiver run, and
// pick'em lock already renders with).
const practiceTimeLayout = "Mon Jan 2 · 3:04 PM MST"

// PracticeLobby is the lobby's own "real draft" card: the real draft's
// league-local date, time, zone, and relative phrase from the canonical
// summary, plus the viewer's own check-in state and seat, so the lobby
// answers "when is the real thing, and am I ready for it" beside the
// practice choice.
func (s *Service) PracticeLobby(r *http.Request) map[string]any {
	now := s.clock()
	state := s.store.Snapshot()
	summary := s.draftSummaryForState(now, state)
	published, _ := summary["published"].(bool)
	at := s.EffectiveDraftAt(state)
	location := s.draftTZ
	if location == nil {
		location = time.UTC
	}
	availability := s.PracticeAvailability(r)
	checkedIn := availability.HasSeat && state.Ready[availability.TeamID]
	out := map[string]any{
		"published":  published,
		"date":       summary["date"],
		"time":       summary["time"],
		"timezone":   summary["timezone"],
		"long_date":  summary["long_date"],
		"label":      "",
		"relative":   "",
		"started":    state.DraftStarted,
		"complete":   draftComplete(state),
		"has_seat":   availability.HasSeat,
		"checked_in": checkedIn,
		"team_name":  s.TeamLabel(availability.TeamID),
		"room_href":  "/draft",
	}
	if published {
		out["label"] = at.In(location).Format(practiceTimeLayout)
		out["relative"] = relativeTime(now, at)
	}
	return out
}

// PracticeInactiveMap is the "practice" key every REAL room render carries:
// the same shape, active false, so page.gsx can branch on
// data.practice.active without a missing-key lookup. availability adds the
// checklist entry's own gate and reason.
func PracticeInactiveMap(availability PracticeAvailability) map[string]any {
	out := availability.Map()
	out["active"] = false
	out["complete"] = false
	out["options"] = practiceOptionMaps()
	return out
}

func practiceOptionMaps() []map[string]any {
	options := PracticeStartOptions()
	out := make([]map[string]any, 0, len(options))
	for _, option := range options {
		out = append(out, map[string]any{"round": option.Round, "label": option.Label, "detail": option.Detail})
	}
	return out
}

// PickClockLabel is the league's effective pick clock as the room's own
// MM:SS label (the commissioner's persisted duration when set, else the
// PICK_CLOCK default) — the practice lobby names it beside the round count.
func (s *Service) PickClockLabel() string {
	return countdownMMSSLabel(int(s.pickClock(s.store.Snapshot()).Seconds()))
}

// draftStartedQuick reads DraftStarted under the read lock without cloning
// the whole state: the practice ticker asks every second for every open
// session, and practiceMap asks on every practice render.
func (s *Store) draftStartedQuick() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state.DraftStarted
}
