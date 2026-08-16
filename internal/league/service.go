package league

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"gridiron-2000/internal/notify"

	"m31labs.dev/gosx/auth"
)

// PlayerSource supplies the live draft pool: players in draft order, a
// version that changes when the pool changes, and a mode label
// (live | cache | offline | demo).
type PlayerSource func() ([]Player, int64, string)

// playerPool is the indexed, version-cached view of the draft pool.
type playerPool struct {
	version int64
	label   string
	players []Player
	byID    map[string]Player
}

// Service owns the starter's application state and view-model assembly.
type Service struct {
	store    *Store
	feed     *liveFeed
	draftAt  time.Time
	draftTZ  *time.Location
	demoMode bool
	teams    []Team
	players  []Player

	// now overrides time.Now() for every time-dependent decision that
	// tests must drive deterministically: DraftData, MakePick, clockTick,
	// and the fingerprint's presence digest. nil means time.Now(); see
	// clock().
	now func() time.Time
	// presence tracks per-viewer last-seen instants, in memory only. The
	// zero value is ready to use (see presenceTracker's doc comment).
	presence presenceTracker
	// pickClockDefault is the PICK_CLOCK environment default, parsed once
	// at construction. Zero (the test-construction default) falls back to
	// DefaultPickClock; see pickClock.
	pickClockDefault time.Duration

	poolMu       sync.Mutex
	poolSource   PlayerSource
	poolCache    playerPool
	poolStatusFn PoolStatusSource
	scheduleFn   ScheduleSource
	historicalFn HistoricalSource
	weekStatsFn  WeekStatsSource
	// blitzFn supplies the Preseason Blitz feed (games plus live stats,
	// WP-B1); see blitz.go's SetBlitzSource. nil means the feature is
	// disabled — no TANK01_API_KEY, or the contest has sunset — and
	// BlitzData renders its honest feed-offline state.
	blitzFn BlitzSource

	// notifyQueue is the delivery queue every notification hook enqueues to
	// (internal/notify). nil means notifications were never wired (most
	// tests); notifyReady also requires notifyTransportEnabled, main.go's
	// mailer.Config.Enabled() reading passed through SetNotifier — see the
	// design spec section 6.6.
	notifyQueue            *notify.Queue
	notifyTransportEnabled bool
	// notifyLastPruneAt gates the notifier ticker's daily SentLog prune
	// (spec section 6.2, 6.4): zero at construction, so the first tick
	// always prunes once at boot, matching the draft clock's
	// bootRecoverClock precedent of "act once, immediately, at startup."
	notifyLastPruneAt time.Time
}

// clock returns the service's current instant: the test-injected now hook
// when set, otherwise time.Now(). Every time-dependent decision outside the
// enforcement loop's own ticker wiring reads the instant through here, so
// tests can drive the whole system with a fake clock.
func (s *Service) clock() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

var (
	defaultOnce sync.Once
	defaultSvc  *Service
)

func Default() *Service {
	defaultOnce.Do(func() {
		statePath := strings.TrimSpace(os.Getenv("DATA_FILE"))
		if statePath == "" {
			statePath = filepath.Join("data", "league-state.json")
		}
		draftAt := parseDraftAt(os.Getenv("DRAFT_AT"))
		demo := parseBool(os.Getenv("DEMO_MODE"), os.Getenv("GOOGLE_CLIENT_ID") == "")
		defaultSvc = &Service{
			store:            NewStore(statePath),
			draftAt:          draftAt,
			draftTZ:          parseDraftTZ(os.Getenv("DRAFT_TZ")),
			demoMode:         demo,
			teams:            defaultTeams(),
			players:          defaultPlayers(),
			presence:         newPresenceTracker(time.Now()),
			pickClockDefault: parsePickClock(os.Getenv("PICK_CLOCK")),
		}
		// scheduleProvider reads the persisted league schedule once one has
		// been generated; until then it defers to the honest preseason
		// snapshot (feed.go). This replaces the always-empty demoProvider
		// default (competition-formats spec section 2.5).
		defaultSvc.feed = newLiveFeed(scheduleProvider{svc: defaultSvc})
	})
	return defaultSvc
}

func parseDraftAt(value string) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		value = DefaultDraftAt
	}
	draftAt, err := time.Parse(time.RFC3339, value)
	if err != nil {
		draftAt, _ = time.Parse(time.RFC3339, DefaultDraftAt)
	}
	return draftAt
}

// parseDraftTZ resolves the league's display timezone. The countdown uses
// the absolute DRAFT_AT instant; this only shapes the printed clock times.
func parseDraftTZ(value string) *time.Location {
	value = strings.TrimSpace(value)
	if value == "" {
		value = DefaultDraftTZ
	}
	location, err := time.LoadLocation(value)
	if err != nil {
		location, _ = time.LoadLocation(DefaultDraftTZ)
	}
	if location == nil {
		location = time.UTC
	}
	return location
}

// parsePickClock resolves the PICK_CLOCK environment default: a Go
// duration string ("90s", "2m") or bare seconds ("90"). An empty value or a
// parse failure falls back to DefaultPickClock; every result clamps to
// [MinPickClock, MaxPickClock].
func parsePickClock(value string) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return DefaultPickClock
	}
	if seconds, err := strconv.Atoi(value); err == nil {
		return clampPickClock(time.Duration(seconds) * time.Second)
	}
	duration, err := time.ParseDuration(value)
	if err != nil {
		return DefaultPickClock
	}
	return clampPickClock(duration)
}

// pickClock resolves the pick-clock duration for state: the commissioner's
// persisted override when set, otherwise the PICK_CLOCK environment
// default (or DefaultPickClock when the service was built without one, as
// in most tests).
func (s *Service) pickClock(state PersistedState) time.Duration {
	if state.ClockDurationSec > 0 {
		return clampPickClock(time.Duration(state.ClockDurationSec) * time.Second)
	}
	if s.pickClockDefault > 0 {
		return s.pickClockDefault
	}
	return DefaultPickClock
}

func parseBool(value string, fallback bool) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func (s *Service) DraftAt() time.Time { return s.draftAt }

func (s *Service) DemoMode() bool { return s.demoMode }

// EmailAllowed reports whether the email may claim a seat: it must appear in
// the LEAGUE_ALLOWED_EMAILS environment list or the stored invite list. When
// both lists are empty the league is open (initial setup).
func (s *Service) EmailAllowed(email string) bool {
	email = strings.ToLower(strings.TrimSpace(email))
	envList := splitEmails(os.Getenv("LEAGUE_ALLOWED_EMAILS"))
	invites := s.store.Snapshot().Invites
	if len(envList) == 0 && len(invites) == 0 {
		return true
	}
	for _, candidate := range envList {
		if candidate == email {
			return true
		}
	}
	return s.store.Invited(email)
}

// IsCommissioner reports whether the request belongs to a commissioner.
// COMMISSIONER_EMAILS names them; demo mode grants it for local rehearsal.
func (s *Service) IsCommissioner(r *http.Request) bool {
	if s.demoMode {
		return true
	}
	user, ok := auth.Current(r)
	if !ok {
		return false
	}
	email := strings.ToLower(strings.TrimSpace(user.Email))
	for _, candidate := range splitEmails(os.Getenv("COMMISSIONER_EMAILS")) {
		if candidate == email {
			return true
		}
	}
	return false
}

// viewerKey identifies the acting person for board storage: the signed-in
// email, or a shared guest key in demo mode.
func (s *Service) viewerKey(r *http.Request) string {
	if user, ok := auth.Current(r); ok {
		return strings.ToLower(strings.TrimSpace(user.Email))
	}
	if s.demoMode {
		return "demo-guest"
	}
	return ""
}

// RecordPresence stores now as the requester's last-seen instant. An
// anonymous, non-demo request carries no viewer key and is skipped. Call it
// before computing a fingerprint the same requester will read, so the
// poller's own transition to CONNECTED is visible in that response.
func (s *Service) RecordPresence(r *http.Request, now time.Time) {
	s.presence.record(s.viewerKey(r), now)
}

// presenceKeyForTeam resolves the viewer key that stands for one team's
// seat: the claiming member's email, or "demo-guest" for every seat in demo
// mode (a single guest key drives every rehearsal seat). An unclaimed seat
// outside demo mode resolves to "".
func (s *Service) presenceKeyForTeam(state PersistedState, teamID string) string {
	if s.demoMode {
		return "demo-guest"
	}
	return strings.ToLower(strings.TrimSpace(memberForTeam(state.Members, teamID).Email))
}

// presenceFloor returns key's last-seen instant, floored at the presence
// tracker's process-start instant. The floor keeps a just-booted server
// from reading every key as instantly, spuriously AWAY: the away cap
// cannot fire before startedAt + PresenceIdleWithin + AwayClockCap.
func (s *Service) presenceFloor(key string) time.Time {
	seenAt, ok := s.presence.seen(key)
	if !ok || seenAt.Before(s.presence.startedAt) {
		return s.presence.startedAt
	}
	return seenAt
}

// presenceDigest renders "team-1=connected,team-2=away,..." across every
// default team ID, in order (already sorted, since team-1..team-8 compare
// lexically). Buckets change only on a presenceState transition, so this
// string — and the fingerprint suffix it feeds — stays stable between
// transitions and changes exactly when a room's presence dot must change.
func (s *Service) presenceDigest(state PersistedState, now time.Time) string {
	order := defaultTeamIDs()
	parts := make([]string, 0, len(order))
	for _, teamID := range order {
		label := "none"
		if key := s.presenceKeyForTeam(state, teamID); key != "" {
			label = presenceState(s.presenceFloor(key), now)
		}
		parts = append(parts, teamID+"="+label)
	}
	return strings.Join(parts, ",")
}

func splitEmails(raw string) []string {
	out := []string{}
	for _, part := range strings.Split(raw, ",") {
		part = strings.ToLower(strings.TrimSpace(part))
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

// SetPlayerSource attaches the fantasy pool. Call it once during startup,
// before the server accepts requests.
func (s *Service) SetPlayerSource(source PlayerSource) {
	s.poolMu.Lock()
	s.poolSource = source
	s.poolCache = playerPool{}
	s.poolMu.Unlock()
}

// SetNotifier attaches the notification delivery queue and the mail
// transport's enabled state. Call it once during startup, before the
// server accepts requests, beside SetPlayerSource — main.go resolves
// transportEnabled from mailer.Config.Enabled(), the same check that gates
// notify.Queue.Start. Every notification hook and StartNotifier read this
// pair through notifyReady before building, recording, or enqueuing
// anything (design spec section 6.6).
func (s *Service) SetNotifier(queue *notify.Queue, transportEnabled bool) {
	s.poolMu.Lock()
	s.notifyQueue = queue
	s.notifyTransportEnabled = transportEnabled
	s.poolMu.Unlock()
}

// notifyReady reports whether notification hooks may build, record, or
// send: the queue is wired and its mail transport is enabled. league.json's
// notifications.enabled adds a second gate in a later work package
// (WP-E5); config.go does not exist yet, so this check stands alone for
// now (design spec section 6.6, 7.1).
func (s *Service) notifyReady() bool {
	s.poolMu.Lock()
	ready := s.notifyQueue != nil && s.notifyTransportEnabled
	s.poolMu.Unlock()
	return ready
}

// StateFingerprint hashes the persisted league state plus the pool version
// and a bucketed presence digest. Clients poll it and soft-refresh the page
// when it changes, which keeps every open draft room current — including
// its presence dots and pick-clock deadline, both of which live in or feed
// this hash — without full reloads.
func (s *Service) StateFingerprint(poolVersion int64) string {
	state := s.store.Snapshot()
	encoded, err := json.Marshal(state)
	if err != nil {
		encoded = []byte(err.Error())
	}
	suffix := fmt.Sprintf("|pool:%d|presence:%s", poolVersion, s.presenceDigest(state, s.clock()))
	// Preseason Blitz live scores are never persisted in league state
	// (design spec section 4.4): a poll must not rewrite the state file and
	// churn every other fingerprint reader. Appending the source's own
	// version here is what lets a live-stat update reach browsers through
	// the same 4s revalidation loop everything else uses (F14).
	s.poolMu.Lock()
	blitzSource := s.blitzFn
	s.poolMu.Unlock()
	if blitzSource != nil {
		suffix += fmt.Sprintf("|blitz:%d", blitzSource().Version)
	}
	digest := sha256.Sum256(append(encoded, []byte(suffix)...))
	return hex.EncodeToString(digest[:8])
}

// PoolStatusSource supplies legible fantasy pool diagnostics for the admin
// console. main.go injects it to avoid an import cycle with internal/fantasy.
type PoolStatusSource func() map[string]any

// SetPoolStatus attaches the diagnostics source. Call it during startup.
func (s *Service) SetPoolStatus(source PoolStatusSource) {
	s.poolMu.Lock()
	s.poolStatusFn = source
	s.poolMu.Unlock()
}

func (s *Service) poolStatusMap() map[string]any {
	s.poolMu.Lock()
	source := s.poolStatusFn
	s.poolMu.Unlock()
	if source == nil {
		return map[string]any{
			"mode": "unknown", "players": 0, "with_adp": 0, "with_proj": 0,
			"with_bye": 0, "requests": 0, "last_sync": "never", "error": "",
			"positions_list": []map[string]any{},
		}
	}
	return source()
}

// pool returns the indexed draft pool, rebuilding the index only when the
// source version changes. The demo fixtures remain reachable by ID so picks
// recorded during rehearsals keep resolving after the live pool arrives.
func (s *Service) pool() playerPool {
	s.poolMu.Lock()
	defer s.poolMu.Unlock()
	if s.poolSource == nil {
		if s.poolCache.byID == nil {
			s.poolCache = s.buildPool(s.players, 0, "demo")
		}
		return s.poolCache
	}
	players, version, label := s.poolSource()
	if len(players) == 0 {
		players, version, label = s.players, 0, "demo"
	}
	if s.poolCache.byID == nil || s.poolCache.version != version || s.poolCache.label != label {
		s.poolCache = s.buildPool(players, version, label)
	}
	return s.poolCache
}

// HistoricalSource supplies one player's legible previous-season line by
// name and position. main.go adapts the nflverse season summary mirror to
// this shape, which keeps that dependency out of internal/league.
type HistoricalSource func(name, position string) (line string, ok bool)

// SetHistoricalSource attaches the previous-season lookup. Call it once
// during startup, before the server accepts requests.
func (s *Service) SetHistoricalSource(fn HistoricalSource) {
	s.poolMu.Lock()
	s.historicalFn = fn
	s.poolMu.Unlock()
}

// buildPool runs once per pool version, not once per render, so it is the
// right place to amortize the historical-line lookup: every player without
// an Hist line gets one chance at it here, and the result travels with the
// cached pool. buildPool is called from pool(), which already holds poolMu,
// so it reads s.historicalFn directly rather than locking again.
func (s *Service) buildPool(players []Player, version int64, label string) playerPool {
	byID := make(map[string]Player, len(players)+len(s.players))
	for _, player := range s.players {
		byID[player.ID] = s.withHistorical(player)
	}
	annotated := make([]Player, len(players))
	for index, player := range players {
		annotated[index] = s.withHistorical(player)
		byID[annotated[index].ID] = annotated[index]
	}
	return playerPool{version: version, label: label, players: annotated, byID: byID}
}

// withHistorical fills a player's previous-season line from the attached
// historical source, unless the player already carries one or no source is
// attached. Callers hold poolMu already (see buildPool).
func (s *Service) withHistorical(player Player) Player {
	if player.Hist != "" || s.historicalFn == nil {
		return player
	}
	if line, ok := s.historicalFn(player.Name, player.Position); ok && line != "" {
		player.Hist = line
	}
	return player
}

func (s *Service) AssignManager(email, name string) (Member, error) {
	return s.assignMember(email, name)
}

// assignMember is the single call path for every AssignMember use in the
// service (Google sign-in via AssignManager, a demo-mode first visit via
// Viewer, and the acting-team resolver), so the N1 seat-claimed hook fires
// exactly once per real seat claim, from whichever entry point reached it
// first (spec section 3, N1).
func (s *Service) assignMember(email, name string) (Member, error) {
	member, created, err := s.store.AssignMember(email, name)
	if err != nil {
		return Member{}, err
	}
	if created {
		s.notifySeatClaimed(member)
	}
	return member, nil
}

func (s *Service) Viewer(r *http.Request) map[string]any {
	user, signedIn := auth.Current(r)
	if !signedIn {
		team := s.teams[0]
		return map[string]any{
			"signed_in":       false,
			"demo":            s.demoMode,
			"name":            "Guest Coach",
			"email":           "",
			"initials":        "GC",
			"team_id":         team.ID,
			"team_name":       team.Name,
			"is_commissioner": s.demoMode,
		}
	}
	member, ok := s.store.MemberByEmail(user.Email)
	if !ok {
		member, _ = s.assignMember(user.Email, user.Name)
	}
	team := s.teamByID(member.TeamID)
	name := strings.TrimSpace(user.Name)
	if name == "" {
		name = strings.Split(user.Email, "@")[0]
	}
	return map[string]any{
		"signed_in":       true,
		"demo":            false,
		"name":            name,
		"email":           user.Email,
		"initials":        initials(name),
		"team_id":         team.ID,
		"team_name":       team.Name,
		"is_commissioner": s.IsCommissioner(r),
	}
}

func (s *Service) DashboardData(ctx context.Context, r *http.Request) map[string]any {
	now := time.Now()
	live := s.feed.Snapshot(ctx, now)
	state := s.store.Snapshot()
	featured := s.matchupMaps(state, live.Matchups[:min(2, len(live.Matchups))])
	return map[string]any{
		"viewer":       s.Viewer(r),
		"draft":        s.draftSummary(now),
		"live":         s.liveMap(live),
		"featured":     featured,
		"standings":    s.standingsMaps(),
		"divisions":    s.divisionMaps(state),
		"transactions": transactionMaps(),
		// GSX conditions cannot call .length on server data; ship the bools.
		"transactions_empty": len(transactionMaps()) == 0,
		"featured_empty":     len(featured) == 0,
		"league_size":        len(s.teams),
		"season":             "2026",
		"league_mode":        "DYNASTY",
	}
}

func (s *Service) MatchupsData(ctx context.Context, r *http.Request) map[string]any {
	live := s.feed.Snapshot(ctx, time.Now())
	state := s.store.Snapshot()
	matchups := s.matchupMaps(state, live.Matchups)
	return map[string]any{
		"viewer":         s.Viewer(r),
		"live":           s.liveMap(live),
		"matchups":       matchups,
		"matchups_empty": len(matchups) == 0,
		"leaders":        s.leaderMaps(),
	}
}

func (s *Service) TeamData(r *http.Request) map[string]any {
	viewer := s.Viewer(r)
	teamID, _ := viewer["team_id"].(string)
	state := s.store.Snapshot()
	team := s.teamView(state, teamID)
	roster, drafted := s.rosterForTeam(state, teamID)
	projected := 0.0
	for _, player := range roster {
		projected += player.Projection
	}
	teamMap := s.teamMap(team)
	return map[string]any{
		"viewer":          viewer,
		"team":            teamMap,
		"roster":          playerMapsWithScoring(roster, s.currentScoringValues()),
		"drafted":         drafted,
		"starters":        len(roster),
		"projected":       fmt.Sprintf("%.1f", projected),
		"division":        teamMap["division"],
		"scouting":        s.topAvailable(state, 3),
		"is_commissioner": s.IsCommissioner(r),
		"league_mode":     "DYNASTY",
	}
}

// rosterForTeam returns the team's drafted players in pick order. An empty
// slice means the seat has not drafted; the page renders the empty state.
func (s *Service) rosterForTeam(state PersistedState, teamID string) ([]Player, bool) {
	pool := s.pool()
	roster := make([]Player, 0, 15)
	for _, pick := range state.Picks {
		if pick.TeamID != teamID {
			continue
		}
		if player, ok := pool.byID[pick.PlayerID]; ok {
			if player.Status == "" {
				player.Status = fmt.Sprintf("Rd %d · Pick %d", pick.Round, pick.Number)
			}
			roster = append(roster, player)
		}
	}
	return roster, len(roster) > 0
}

// topAvailable lists the best unpicked pool players for the waiver radar.
func (s *Service) topAvailable(state PersistedState, limit int) []map[string]any {
	pool := s.pool()
	picked := make(map[string]bool, len(state.Picks))
	for _, pick := range state.Picks {
		picked[pick.PlayerID] = true
	}
	out := make([]map[string]any, 0, limit)
	for _, player := range pool.players {
		if picked[player.ID] {
			continue
		}
		signal := "Projection " + fmt.Sprintf("%.1f", player.Projection)
		if player.ADPRank > 0 {
			signal = fmt.Sprintf("ADP #%d", player.ADPRank)
		}
		out = append(out, map[string]any{
			"position": player.Position,
			"name":     player.Name,
			"team":     player.NFLTeam,
			"signal":   signal,
			"status":   "OPEN",
		})
		if len(out) >= limit {
			break
		}
	}
	return out
}

func (s *Service) DraftData(r *http.Request) map[string]any {
	now := s.clock()
	viewer := s.Viewer(r)
	state := s.store.Snapshot()
	pool := s.pool()
	// Resolved once for the whole render: up to hundreds of players render
	// per page, and scoreBreakdown would otherwise snapshot store state once
	// per player. See currentScoringValues.
	scoringValues := s.currentScoringValues()
	picked := make(map[string]bool, len(state.Picks))
	for _, pick := range state.Picks {
		picked[pick.PlayerID] = true
	}
	available := make([]Player, 0, len(pool.players))
	for _, player := range pool.players {
		if !picked[player.ID] {
			available = append(available, player)
		}
	}
	nextNumber := len(state.Picks) + 1
	onClockID := teamOnClock(state.DraftOrder, nextNumber)
	onClock := s.teamView(state, onClockID)
	viewerTeam, _ := viewer["team_id"].(string)
	canPick := now.After(s.draftAt) && viewerTeam == onClockID
	if s.demoMode {
		canPick = true
	}
	boardPanel := make([]map[string]any, 0, 5)
	for _, id := range state.Boards[s.viewerKey(r)] {
		if picked[id] {
			continue
		}
		if player, ok := pool.byID[id]; ok {
			boardPanel = append(boardPanel, playerMap(player, scoringValues))
			if len(boardPanel) == 5 {
				break
			}
		}
	}
	return map[string]any{
		"viewer":           viewer,
		"draft":            s.draftSummary(now),
		"teams":            s.draftTeamMaps(state, onClockID),
		"picks":            s.pickMaps(state, pool.byID, scoringValues),
		"available":        playerMapsWithScoring(available, scoringValues),
		"board":            boardPanel,
		"board_count":      len(boardPanel),
		"pool_label":       pool.label,
		"pool_live":        pool.label == "live" || pool.label == "cache",
		"pool_count":       len(pool.players),
		"on_clock":         s.teamMap(onClock),
		"on_clock_id":      onClockID,
		"pick_number":      nextNumber,
		"picks_empty":      len(state.Picks) == 0,
		"round":            pickRound(activeTeamCount(state.DraftOrder), nextNumber),
		"can_pick":         canPick,
		"demo_mode":        s.demoMode,
		"ready_count":      readyCount(state.Ready),
		"manager_count":    len(s.teams),
		"order_randomized": len(state.DraftOrder) > 0,
		"league_mode":      "DYNASTY",
		"clock":            s.clockView(state, now),
	}
}

// clockView renders the pick-clock payload the draft room's countdown
// enhancer consumes: armed/paused state, both the persisted and the
// effective (capped) deadline, the reason label, and a remaining-seconds
// figure the client can render immediately, before its own 1-second
// interval takes over. now must come from s.clock() so this always agrees
// with the enforcement loop's own decision.
func (s *Service) clockView(state PersistedState, now time.Time) map[string]any {
	deadline := state.ClockDeadline
	armed := !deadline.IsZero()
	effective := deadline
	reason := "clock"
	switch {
	case state.ClockPaused:
		reason = "paused"
	case !armed:
		reason = "unarmed"
	default:
		effective, reason = s.effectiveDeadline(state, now)
	}
	remaining := 0
	if armed && !state.ClockPaused {
		remaining = int(effective.Sub(now).Seconds())
		if remaining < 0 {
			remaining = 0
		}
	}
	return map[string]any{
		"armed":              armed,
		"paused":             state.ClockPaused,
		"deadline":           formatClockInstant(deadline),
		"effective_deadline": formatClockInstant(effective),
		"reason":             reason,
		"remaining_seconds":  remaining,
		"duration_seconds":   int(s.pickClock(state).Seconds()),
		"server_now":         now.UTC().Format(time.RFC3339),
	}
}

// formatClockInstant renders t as RFC3339 UTC, or "" for the zero value
// (unarmed).
func formatClockInstant(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func (s *Service) LoginData(r *http.Request, configured bool) map[string]any {
	return map[string]any{
		"viewer":     s.Viewer(r),
		"configured": configured,
		"demo_mode":  s.demoMode,
		"seats":      len(s.teams),
	}
}

func (s *Service) LiveScores(ctx context.Context) LiveSnapshot {
	return s.feed.Snapshot(ctx, time.Now())
}

func (s *Service) ToggleReady(r *http.Request, requestedTeam string) (bool, string, error) {
	teamID, err := s.actingTeam(r, requestedTeam)
	if err != nil {
		return false, "", err
	}
	ready, err := s.store.ToggleReady(teamID)
	return ready, s.teamByID(teamID).Name, err
}

func (s *Service) MakePick(r *http.Request, requestedTeam, playerID string) (DraftPick, Player, Team, error) {
	playerID = strings.TrimSpace(playerID)
	player, ok := s.pool().byID[playerID]
	if !ok {
		return DraftPick{}, Player{}, Team{}, fmt.Errorf("choose an available player")
	}
	teamID, err := s.actingTeam(r, requestedTeam)
	if err != nil {
		return DraftPick{}, Player{}, Team{}, err
	}
	state := s.store.Snapshot()
	expected := teamOnClock(state.DraftOrder, len(state.Picks)+1)
	if s.demoMode {
		teamID = expected
	}
	now := s.clock()
	if !s.draftIsLive(now) {
		return DraftPick{}, Player{}, Team{}, fmt.Errorf("the draft room is not open yet")
	}
	// The pick and its clock reset land in one store transaction (section
	// 4.6 of the pick-clock spec). A paused clock stays unarmed after the
	// pick — pause freezes the timer, not the draft — and the final pick
	// leaves the clock unarmed for good.
	totalPicks := len(defaultTeams()) * DraftRounds
	nextDeadline := time.Time{}
	if !state.ClockPaused && len(state.Picks)+1 < totalPicks {
		nextDeadline = now.Add(s.pickClock(state))
	}
	pick, err := s.store.MakePick(teamID, playerID, "manager", now, nextDeadline)
	return pick, player, s.teamByID(teamID), err
}

// ToggleAutopick flips the acting seat's away-mode auto-pick flag. A
// manager toggles only their own seat; demo mode may name any seat, the
// same rule MakePick and ToggleReady use via actingTeam.
func (s *Service) ToggleAutopick(r *http.Request, requestedTeam string) (bool, string, error) {
	teamID, err := s.actingTeam(r, requestedTeam)
	if err != nil {
		return false, "", err
	}
	on := !s.store.Snapshot().Autopick[teamID]
	if err := s.store.SetAutopick(teamID, on); err != nil {
		return false, "", err
	}
	return on, s.teamByID(teamID).Name, nil
}

func (s *Service) actingTeam(r *http.Request, requested string) (string, error) {
	if user, ok := auth.Current(r); ok {
		member, exists := s.store.MemberByEmail(user.Email)
		if !exists {
			var err error
			member, err = s.assignMember(user.Email, user.Name)
			if err != nil {
				return "", err
			}
		}
		return member.TeamID, nil
	}
	if s.demoMode && knownTeam(requested) {
		return requested, nil
	}
	return "", fmt.Errorf("Google sign-in is required for league actions")
}

// draftIsLive reports whether picks (manual or a commissioner's forced
// auto-pick) may currently be recorded: always true in demo mode
// (rehearsals bypass the gate; see service.go's can_pick logic in
// DraftData), otherwise true once now reaches draftAt.
func (s *Service) draftIsLive(now time.Time) bool {
	if s.demoMode {
		return true
	}
	return !now.Before(s.draftAt)
}

func (s *Service) draftSummary(now time.Time) map[string]any {
	location := s.draftTZ
	if location == nil {
		location, _ = time.LoadLocation(DefaultDraftTZ)
	}
	local := s.draftAt.In(location)
	return map[string]any{
		"at":         s.draftAt.Format(time.RFC3339),
		"date":       strings.ToUpper(local.Format("Mon · Jan")) + " " + strconv.Itoa(local.Day()),
		"time":       local.Format("3:04 PM MST"),
		"long_date":  local.Format("Saturday, January 2, 2006"),
		"format":     "Dynasty · Snake · 15 rounds",
		"started":    !now.Before(s.draftAt),
		"days_until": max(0, int(s.draftAt.Sub(now).Hours()/24)),
	}
}

func (s *Service) standingsMaps() []map[string]any {
	teams := append([]Team(nil), s.teams...)
	sort.Slice(teams, func(i, j int) bool { return teams[i].Rank < teams[j].Rank })
	out := make([]map[string]any, 0, len(teams))
	for _, team := range teams {
		out = append(out, s.teamMap(team))
	}
	return out
}

// draftTeamMaps renders the draft-room team grid in draft order: the
// commissioner-drawn order when set, otherwise the default team-ID order.
// Each entry also carries the seat's presence bucket (connected | idle |
// away | none) and its Autopick toggle, both read against s.clock().
func (s *Service) draftTeamMaps(state PersistedState, onClockID string) []map[string]any {
	order := state.DraftOrder
	if len(order) == 0 {
		order = defaultTeamIDs()
	}
	now := s.clock()
	out := make([]map[string]any, 0, len(order))
	for _, teamID := range order {
		team := s.teamView(state, teamID)
		item := s.teamMap(team)
		item["ready"] = state.Ready[team.ID]
		item["on_clock"] = team.ID == onClockID
		presenceLabel := "none"
		if key := s.presenceKeyForTeam(state, team.ID); key != "" {
			presenceLabel = presenceState(s.presenceFloor(key), now)
		}
		item["presence"] = presenceLabel
		item["autopick"] = state.Autopick[team.ID]
		out = append(out, item)
	}
	return out
}

// pickMaps renders the pick tape, one entry per recorded pick. Each entry
// carries provenance: made_by ("manager", "auto", or "commissioner") plus
// the is_auto/is_commissioner bools GSX conditions need (they cannot switch
// on a string comfortably). An empty stored MadeBy — a pick recorded before
// this field existed — normalizes to "manager" here, in the view only; the
// stored pick itself stays byte-stable.
func (s *Service) pickMaps(state PersistedState, players map[string]Player, scoringValues map[string]float64) []map[string]any {
	out := make([]map[string]any, 0, len(state.Picks))
	for _, pick := range state.Picks {
		player := players[pick.PlayerID]
		team := s.teamView(state, pick.TeamID)
		madeBy := pick.MadeBy
		if madeBy == "" {
			madeBy = "manager"
		}
		out = append(out, map[string]any{
			"number":          pick.Number,
			"round":           pick.Round,
			"team":            s.teamMap(team),
			"player":          playerMap(player, scoringValues),
			"made_by":         madeBy,
			"is_auto":         madeBy == "auto",
			"is_commissioner": madeBy == "commissioner",
		})
	}
	return out
}

func (s *Service) liveMap(live LiveSnapshot) map[string]any {
	return map[string]any{
		"source":       live.Source,
		"source_label": live.SourceLabel,
		"week":         live.Week,
		"week_label":   live.WeekLabel,
		"status":       live.Status,
		"last_updated": live.LastUpdated.Local().Format("3:04:05 PM"),
		"warning":      live.Warning,
	}
}

func (s *Service) matchupMaps(state PersistedState, matchups []ScoreMatchup) []map[string]any {
	out := make([]map[string]any, 0, len(matchups))
	for _, matchup := range matchups {
		away := s.teamView(state, matchup.Away.ID)
		home := s.teamView(state, matchup.Home.ID)
		out = append(out, map[string]any{
			"id": matchup.ID,
			"away": map[string]any{
				"id": matchup.Away.ID, "name": matchup.Away.Name, "abbreviation": matchup.Away.Abbreviation,
				"score": fmt.Sprintf("%.1f", matchup.Away.Score), "tone": away.Tone, "manager": away.Manager,
			},
			"home": map[string]any{
				"id": matchup.Home.ID, "name": matchup.Home.Name, "abbreviation": matchup.Home.Abbreviation,
				"score": fmt.Sprintf("%.1f", matchup.Home.Score), "tone": home.Tone, "manager": home.Manager,
			},
			"status": matchup.Status,
			"clock":  matchup.Clock,
		})
	}
	return out
}

func (s *Service) teamByID(id string) Team {
	return s.teamView(s.store.Snapshot(), id)
}

// teamView resolves a team against an already-taken snapshot so callers in a
// loop pay for one state copy, not one per team.
func (s *Service) teamView(state PersistedState, id string) Team {
	for _, team := range s.teams {
		if team.ID == id {
			if member := memberForTeam(state.Members, id); member.Name != "" {
				team.Manager = member.Name
			}
			if override := strings.TrimSpace(state.TeamNames[id]); override != "" {
				team.Name = override
			}
			return team
		}
	}
	return s.teams[0]
}

func (s *Service) teamMap(team Team) map[string]any {
	manager := strings.TrimSpace(team.Manager)
	claimed := manager != ""
	if !claimed {
		manager = "UNCLAIMED"
	}
	return map[string]any{
		"id": team.ID, "name": team.Name, "abbreviation": team.Abbreviation, "division": strings.ToUpper(team.Division),
		"manager": manager, "claimed": claimed, "record": team.Record, "points_for": fmt.Sprintf("%.1f", team.PointsFor),
		"rank": fmt.Sprintf("%02d", team.Rank), "rank_number": team.Rank, "streak": team.Streak, "tone": team.Tone,
	}
}

// divisionMaps groups the league into the divisions found in s.teams, in
// first-occurrence (config) order, each with its teams sorted by rank.
// Zero divisions (every team's Division is "") renders one table; divisions
// of unequal size are legal (competition-formats spec section 1.3). Names
// and manager claims are resolved through teamView so overrides and claims
// reach the standings view.
func (s *Service) divisionMaps(state PersistedState) []map[string]any {
	byDivision := map[string][]Team{}
	seen := map[string]bool{}
	order := make([]string, 0, 2)
	for _, team := range s.teams {
		if !seen[team.Division] {
			seen[team.Division] = true
			order = append(order, team.Division)
		}
		byDivision[team.Division] = append(byDivision[team.Division], team)
	}
	out := make([]map[string]any, 0, len(order))
	for _, division := range order {
		teams := append([]Team(nil), byDivision[division]...)
		sort.Slice(teams, func(i, j int) bool { return teams[i].Rank < teams[j].Rank })
		teamsOut := make([]map[string]any, 0, len(teams))
		for _, team := range teams {
			teamsOut = append(teamsOut, s.teamMap(s.teamView(state, team.ID)))
		}
		out = append(out, map[string]any{
			"name":  strings.ToUpper(division),
			"teams": teamsOut,
		})
	}
	return out
}

// playerMap renders one player's view-model map. scoringValues is the
// league's live, override-aware point values (see currentScoringValues);
// pass none to score the player's breakdown against the stock default
// rules instead, at no store-access cost. Callers that render many players
// per request should resolve scoringValues once and reuse it — see
// playerMapsWithScoring — rather than call playerMap in a bare loop, which
// forces every breakdown onto the default-only path.
func playerMap(player Player, scoringValues ...map[string]float64) map[string]any {
	rank := "—"
	if player.ADPRank > 0 {
		rank = fmt.Sprintf("%03d", player.ADPRank)
	}
	detail := player.NFLTeam
	if player.ByeWeek > 0 {
		detail += fmt.Sprintf(" · BYE %d", player.ByeWeek)
	}
	if player.Injury != "" {
		detail += " · " + player.Injury
	}
	if player.News != "" {
		detail += " · " + player.News
	}
	jersey := ""
	if player.Jersey != "" {
		jersey = "#" + player.Jersey
	}
	hasBreakdown := len(player.ProjStats) > 0
	var breakdownRows []map[string]any
	breakdownTotal := ""
	if hasBreakdown {
		var values map[string]float64
		if len(scoringValues) > 0 {
			values = scoringValues[0]
		}
		breakdownRows, breakdownTotal = scoreBreakdownWithValues(player.ProjStats, values)
	}
	return map[string]any{
		"id": player.ID, "name": player.Name, "position": player.Position, "nfl_team": player.NFLTeam,
		"opponent": player.Opponent, "projection": fmt.Sprintf("%.1f", player.Projection),
		"points": fmt.Sprintf("%.1f", player.Points), "status": player.Status, "news": player.News,
		"rank": rank, "detail": detail,
		"headshot": player.Headshot, "has_headshot": player.Headshot != "",
		"jersey":          jersey,
		"has_breakdown":   hasBreakdown,
		"breakdown":       breakdownRows,
		"breakdown_total": breakdownTotal,
		"has_hist":        player.Hist != "",
		"hist":            player.Hist,
		"search":          strings.ToLower(player.Name + " " + player.NFLTeam + " " + player.Position),
	}
}

// playerMaps renders many players against the stock default scoring rules.
// Pool-rendering callers should call playerMapsWithScoring instead, passing
// a scoringValues map resolved once per render; see currentScoringValues.
func playerMaps(players []Player) []map[string]any {
	return playerMapsWithScoring(players, nil)
}

// playerMapsWithScoring renders many players' view models against one
// already-resolved scoringValues map, so a page with hundreds of players
// pays for one store snapshot, not one per player. See currentScoringValues.
func playerMapsWithScoring(players []Player, scoringValues map[string]float64) []map[string]any {
	out := make([]map[string]any, 0, len(players))
	for _, player := range players {
		out = append(out, playerMap(player, scoringValues))
	}
	return out
}

// transactionMaps returns the dashboard's transaction feed. The feed has no
// live source yet, so the page renders an empty state.
func transactionMaps() []map[string]any {
	return []map[string]any{}
}

// leaderMaps ranks the top four pool players by projection for the
// matchups page leaderboard.
func (s *Service) leaderMaps() []map[string]any {
	players := append([]Player(nil), s.pool().players...)
	sort.Slice(players, func(i, j int) bool { return players[i].Projection > players[j].Projection })
	if len(players) > 4 {
		players = players[:4]
	}
	out := make([]map[string]any, 0, len(players))
	for index, player := range players {
		trend := "—"
		if player.ADPRank > 0 {
			trend = fmt.Sprintf("ADP #%d", player.ADPRank)
		}
		out = append(out, map[string]any{
			"rank":     fmt.Sprintf("%02d", index+1),
			"name":     player.Name,
			"position": player.Position,
			"points":   fmt.Sprintf("%.1f", player.Projection),
			"trend":    trend,
		})
	}
	return out
}

func memberForTeam(members map[string]Member, teamID string) Member {
	for _, member := range members {
		if member.TeamID == teamID {
			return member
		}
	}
	return Member{}
}

func readyCount(ready map[string]bool) int {
	count := 0
	for _, value := range ready {
		if value {
			count++
		}
	}
	return count
}

func initials(name string) string {
	parts := strings.Fields(name)
	if len(parts) == 0 {
		return "GC"
	}
	value := string([]rune(parts[0])[0])
	if len(parts) > 1 {
		value += string([]rune(parts[len(parts)-1])[0])
	}
	return strings.ToUpper(value)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
