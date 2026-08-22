package league

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"gridiron-2000/internal/identity"
	"gridiron-2000/internal/navigation"
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
	store            *Store
	identityResolver identity.Resolver
	feed             *liveFeed
	draftAt          time.Time
	draftTZ          *time.Location
	demoMode         bool
	// teamsMu guards teams: a plain field set once at construction for
	// every test Service literal (including a test that reassigns it
	// directly afterward — see admin_test.go's
	// TestInviteEmailTemplateReproducesDeployedLeagueFacts), but mutated
	// once more, post-construction, on the production singleton by
	// TrimUnclaimedSeats (SK unclaimed-seat spec). Every read goes through
	// Teams() below, never the bare field, so that later mutation is race-
	// free against concurrent request handling.
	teamsMu sync.RWMutex
	teams   []Team
	players []Player

	// cfg is the resolved league.json (or DefaultConfig() when none loads)
	// every identity string and copy fragment reads from — see config.go.
	// Default() sets it via LoadConfig(); tests that construct a Service
	// literal directly leave it at the zero Config, so any test exercising
	// cfg-derived copy must set it explicitly (see newTestService).
	cfg Config

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
	// statsUpdatedAtFn supplies the open-stats player-ledger freshness instant
	// used by the commissioner week-close readiness view. It is optional so
	// fixtures and deployments without the mirror fail closed rather than
	// inventing a ready state.
	statsUpdatedAtFn func() time.Time
	historicalFn     HistoricalSource
	weekStatsFn      WeekStatsSource
	// injuryFn supplies the openstats mirror's weekly injury-report
	// designation (roster-ops SK spec): IR placement and the healed-IR
	// ticker both read it via injuryDesignationSource() (zones.go). nil
	// means no source is wired (every test Service literal by default) —
	// IR placement fails closed and the healed-IR ticker is a no-op.
	injuryFn InjuryDesignationSource
	// blitzFn supplies the Preseason Blitz feed (games plus live stats,
	// WP-B1); see blitz.go's SetBlitzSource. nil means the feature is
	// disabled — no TANK01_API_KEY, or the contest has sunset — and
	// BlitzData renders its honest feed-offline state.
	blitzFn BlitzSource
	// blitzPre1Fn supplies preseason-week-1 production (owner directive,
	// 2026-08-16); see blitz.go's SetBlitzPre1Source. nil means no pre1
	// data is available yet — the board falls back to its non-pre1
	// rookie/ADP tiering, never a crash.
	blitzPre1Fn BlitzPre1Source
	// matchupFn supplies the matchup-rank cache (main.go's matchup-rank
	// pipeline); see matchup.go's SetMatchupSource. nil means the cache
	// has not computed yet — every row still shows its opponent, just no
	// rank chip, never a crash.
	matchupFn MatchupSource
	// matchupLabel is matchupFn's honest season-source label ("2025
	// season" or "2026 thru wk N", design point 4), set alongside
	// matchupFn by SetMatchupSource. Empty means no cache has computed
	// yet; see MatchupSourceLabel.
	matchupLabel string

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

	// avatarRoot is the upload target (AVATAR_ROOT). avatarDurableRoot is a
	// pre-existing, real directory that bounds every avatar write
	// (AVATAR_DURABLE_ROOT; data by default, /app/data in the image). Avatar
	// writes never create or sync outside that anchor; a custom target must
	// remain strictly below its configured anchor. defaultBadgeRoot is the
	// separate commissioner-supplied fallback-art root.
	avatarRoot        string
	avatarDurableRoot string
	defaultBadgeRoot  string
	badgeCache        badgeToneCache

	// motifRoot is the team-badge picker feature's filesystem root
	// (AVATAR_MOTIFS_ROOT env, see badge.go's motifDir) — the same
	// override pattern as avatarRoot/defaultBadgeRoot above. badgeArt
	// caches tinted, PNG-encoded badge renders keyed by motif+tone; see
	// tintedBadgePNG.
	motifRoot string
	badgeArt  badgeArtCache
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

// Default builds (once) the process-wide Service, loading league.json (or
// the neutral built-in default when none is found) through LoadConfig —
// productization spec section 3.4. A found-but-invalid config, or an
// explicit $LEAGUE_FILE that does not exist, is fatal at startup: the spec
// section 3.3 rule is "silent fallback on a bad file is worse than a
// crash for an operator." applyActiveConfig then publishes the resolved
// config into every package var the rest of the codebase reads
// (DraftRounds, defaultTeams, ActiveRosterPreset, ...) exactly once, before
// this function returns — see its doc comment in config.go.
func Default() *Service {
	defaultOnce.Do(func() {
		statePath := strings.TrimSpace(os.Getenv("DATA_FILE"))
		if statePath == "" {
			statePath = filepath.Join("data", "league-state.json")
		}
		cfg, err := LoadConfig()
		if err != nil {
			log.Fatal(err)
		}
		resolver, err := identity.FromEnv()
		if err != nil {
			log.Fatalf("identity alias configuration: %v", err)
		}
		applyActiveConfig(cfg)
		if cfg.Source == "defaults" {
			log.Printf("league config: no league.json found; running the built-in neutral reference league (%s). Run the init flow or drop a league.json into config/ to run your own.", cfg.Name)
		} else {
			log.Printf("league config: loaded %s", cfg.Source)
		}
		demo := parseBool(os.Getenv("DEMO_MODE"), os.Getenv("GOOGLE_CLIENT_ID") == "")
		// Production never runs demo mode, no matter what the environment
		// says. Demo mode bypasses the sign-in gate and grants commissioner
		// powers to every visitor; one misconfigured or auto-loaded env file
		// must not be able to open a live league to the internet. The
		// owner's rule: the deployed site is for signed-up members only.
		if strings.EqualFold(strings.TrimSpace(os.Getenv("APP_ENV")), "production") {
			if demo {
				log.Printf("league: DEMO_MODE requested but APP_ENV=production; demo mode is disabled unconditionally in production")
			}
			demo = false
		}
		draftTZ, err := time.LoadLocation(cfg.Timezone)
		if err != nil || draftTZ == nil {
			draftTZ = time.UTC
		}
		store := NewStoreWithIdentity(statePath, resolver)
		if err := store.StartupError(); err != nil {
			// Do this before assigning defaultSvc or starting any background
			// source/poller. A failed persistence boot must be an explicit
			// process-start failure, never a published service that renders
			// zero state and later overwrites a good database.
			log.Fatalf("league persistence startup failed: %v", err)
		}
		defaultSvc = &Service{
			store:             store,
			identityResolver:  resolver,
			draftAt:           cfg.DraftAt,
			draftTZ:           draftTZ,
			demoMode:          demo,
			teams:             defaultTeams(),
			players:           defaultPlayers(),
			cfg:               cfg,
			presence:          newPresenceTracker(time.Now()),
			pickClockDefault:  parsePickClock(os.Getenv("PICK_CLOCK")),
			avatarRoot:        avatarEnvString("AVATAR_ROOT", filepath.Join("data", "avatars")),
			avatarDurableRoot: avatarEnvString("AVATAR_DURABLE_ROOT", "data"),
			defaultBadgeRoot:  avatarEnvString("AVATAR_DEFAULTS_ROOT", filepath.Join("public", "avatars", "defaults")),
			motifRoot:         avatarEnvString("AVATAR_MOTIFS_ROOT", filepath.Join("public", "avatars", "motifs")),
		}
		// scheduleProvider reads the persisted league schedule once one has
		// been generated; until then it defers to the honest preseason
		// snapshot (feed.go). This replaces the always-empty demoProvider
		// default (competition-formats spec section 2.5).
		defaultSvc.feed = newLiveFeed(scheduleProvider{svc: defaultSvc})
		// A commissioner-chosen roster-shape override (roster-shape-editor
		// spec) survives a restart in the state file, not in league.json;
		// apply it on top of applyActiveConfig's just-published baseline now
		// that the store is available, so CurrentRoster/CurrentDraftRounds
		// reflect it before this function returns and the server starts
		// accepting requests. The override was already validated when it was
		// written (Store.SetRosterOverride), so re-applying it here is a
		// plain conversion, not a second validation pass.
		if override := defaultSvc.store.Snapshot().RosterOverride; override != nil {
			setRosterShape(rosterOverridePreset(*override))
		}
		// A commissioner's seat trim (SK unclaimed-seat spec) similarly
		// survives a restart in the state file's TrimmedTeamIDs, not in
		// league.json: the commissioner runs it once, against real
		// membership, and a redeploy must never un-trim the league back to
		// the full configured seat count. Recomputed against the
		// just-published activeTeams baseline (never stored as a frozen
		// team snapshot), so a *kept* team's own league.json edit (name,
		// tone) still applies after a restart — only which team IDs
		// survive is durable.
		if trimmed := defaultSvc.store.Snapshot().TrimmedTeamIDs; len(trimmed) > 0 {
			removed := make(map[string]bool, len(trimmed))
			for _, id := range trimmed {
				removed[id] = true
			}
			kept := make([]Team, 0, len(activeTeams))
			for _, team := range activeTeams {
				if !removed[team.ID] {
					kept = append(kept, team)
				}
			}
			applySeatTrim(kept)
			defaultSvc.setTeams(kept)
		}
	})
	return defaultSvc
}

// Config returns the service's resolved league.json (or the neutral
// default). main.go and every page.server.go's Metadata function read
// identity strings through here.
func (s *Service) Config() Config { return s.cfg }

// PersistenceError reports the store's current boot/runtime persistence
// state to the process health surface. It includes the latest ordinary write
// failure until a later durable write or reconciliation succeeds. Optional
// upstream feeds do not participate in this result: a transient
// Tank01/OpenStats outage must not trigger a restart loop, while a poisoned
// persistence authority must make readiness fail closed.
func (s *Service) PersistenceError() error {
	if s == nil || s.store == nil {
		return errors.New("league persistence is unavailable")
	}
	return s.store.PersistenceError()
}

// StartupError is the constructor/runtime gate retained for callers that
// need the historical name. Health callers should use PersistenceError so
// the method's post-start write-failure semantics remain explicit.
func (s *Service) StartupError() error {
	return s.PersistenceError()
}

const identityUnavailableCopy = "Badge and avatar identity is temporarily unavailable. Badge choices are disabled until persistence recovers."

func (s *Service) identityView() (available bool, message string) {
	if s != nil && s.store != nil && s.store.IdentityHealthy() {
		return true, ""
	}
	return false, identityUnavailableCopy
}

// PageTitle renders one page's browser-tab title from the live config
// (spec section 5.1): "{section} · {league.name}", or "{league.name} ·
// League HQ" for the landing page (section == ""). Every page.server.go's
// Metadata function calls this instead of hardcoding "· GRIDIRON 2000".
func PageTitle(section string) string {
	name := Default().Config().Name
	if section == "" {
		return name + " · League HQ"
	}
	return section + " · " + name
}

// SeatCountWord renders the active league's team count as an English word
// ("eight", "ten", ...) for meta descriptions and page copy that used to
// hardcode "eight-manager" / "eight-team" (spec section 3.6 fix 5).
func SeatCountWord() string {
	return countWord(Default().TeamCount())
}

// SeatCountArticle returns "a" or "an" to match SeatCountWord's leading
// sound ("an eight-manager league", "a ten-manager league").
func SeatCountArticle() string {
	return article(SeatCountWord())
}

// article returns "an" when word starts with a vowel sound, else "a".
func article(word string) string {
	if word != "" && strings.ContainsRune("aeiou", rune(word[0])) {
		return "an"
	}
	return "a"
}

// Teams returns the league's current team list: whatever s.teams currently
// holds — the config-seeded default a construction literal sets it to
// (newTestService, Default()), a test's own direct override (see
// admin_test.go), or, on the production singleton, a commissioner's
// trimmed list once TrimUnclaimedSeats has run. Every Service-method read
// site in the package calls this instead of the bare field, so a runtime
// trim is visible to every in-flight request immediately and race-free.
// Package-level code with no Service receiver (store.go, roster.go's
// draftComplete) reads defaultTeams() instead, which the same trim keeps
// in sync via applySeatTrim.
func (s *Service) Teams() []Team {
	s.teamsMu.RLock()
	defer s.teamsMu.RUnlock()
	return s.teams
}

// setTeams installs teams as s.teams under the write lock. Called only by
// TrimUnclaimedSeats today.
func (s *Service) setTeams(teams []Team) {
	s.teamsMu.Lock()
	s.teams = teams
	s.teamsMu.Unlock()
}

// TeamCount returns the active league's team count.
func (s *Service) TeamCount() int { return len(s.Teams()) }

// RosterSpots returns the active roster shape's total size (starters plus
// bench) — the reference count main.go uses to scale FANTASY_POOL_LIMIT's
// default (owner decision, productization wave: teams × roster spots ×
// headroom, not a flat constant).
func (s *Service) RosterSpots() int { return s.cfg.Roster.Total() }

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

func (s *Service) DraftLifecycle() (bool, time.Time) {
	state := PersistedState{}
	if s.store != nil {
		state = s.store.Snapshot()
	}
	return state.DraftStarted, state.DraftStartedAt
}

func (s *Service) DemoMode() bool { return s.demoMode }

// CanonicalUser applies only the operator's explicit identity mapping. The
// provider's stable subject ID remains untouched; application ownership,
// audit, and commissioner checks use the canonical email.
func (s *Service) CanonicalUser(user auth.User) auth.User {
	user.Email = s.identityResolver.Resolve(user.Email)
	return user
}

// CurrentUser resolves the provider session once at the request boundary so
// every service path observes the same canonical person.
func (s *Service) CurrentUser(r *http.Request) (auth.User, bool) {
	user, ok := auth.Current(r)
	if !ok {
		return auth.User{}, false
	}
	return s.CanonicalUser(user), true
}

// EmailAllowed reports whether the email may join the league (registration
// wave, build item 5 — the domain-gate membership rule): it is admitted
// unconditionally when its domain matches the config's
// membership.allowed_domain (when one is set), otherwise it must appear
// in the LEAGUE_ALLOWED_EMAILS environment list or the stored invite
// list. When both of those lists are empty the league is open (initial
// setup) regardless of the domain gate. The domain gate and the invite
// list are additive, not exclusive — "the invite list still works
// alongside" a configured domain, so a non-domain guest can still be
// invited by email.
func (s *Service) EmailAllowed(email string) bool {
	email = strings.ToLower(strings.TrimSpace(email))
	if domain := strings.ToLower(strings.TrimSpace(s.cfg.Membership.AllowedDomain)); domain != "" {
		if strings.HasSuffix(email, "@"+domain) {
			return true
		}
	}
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
	user, ok := s.CurrentUser(r)
	if !ok {
		return false
	}
	email := user.Email
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
	if user, ok := s.CurrentUser(r); ok {
		return user.Email
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
	// Avatar files live on disk, outside PersistedState, so a plain
	// json.Marshal(state) above never changes when one is uploaded or
	// reset; the digest is what lets an open page's poll notice (design
	// decision 5). See avatarDigest.
	suffix += fmt.Sprintf("|avatars:%s", s.avatarDigest())
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
			"target": 0, "roster_capacity": 0, "cushion": 0, "coverage": "0.0×",
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
// historical source, unless the player already carries one. Callers hold
// poolMu already (see buildPool).
//
// Punter fallback (roster-ops spec section 4.1.2 / WP-R0): nflverse's
// season-summary mirror — the attached historicalFn source — carries no
// punting columns, so a Position "P" player never matches there. When the
// primary source is absent or misses and the player is a punter, this
// falls back to the embedded 2025 punter index (punters_hist.go), matching
// by team and last name. A mismatch attaches nothing (fail quiet).
func (s *Service) withHistorical(player Player) Player {
	if player.Hist != "" {
		return player
	}
	if s.historicalFn != nil {
		if line, ok := s.historicalFn(player.Name, player.Position); ok && line != "" {
			player.Hist = line
			return player
		}
	}
	if player.Position == "P" {
		if line, ok := punterHistLine(player.Name, player.NFLTeam); ok && line != "" {
			player.Hist = line
		}
	}
	return player
}

func (s *Service) AssignManager(email, name string) (Member, error) {
	return s.assignMember(email, name)
}

// assignMember is the single deliberate seat-claim path in the service:
// Google sign-in calls it through AssignManager, and nothing else does.
// created == true fires the N1 seat-claimed hook exactly once per real
// seat claim (spec section 3, N1). Any code that merely needs "a member
// record exists for this signed-in email" — a page view, an FF action's
// acting-team resolver — must call ensureMember instead: it never grabs a
// seat as a side effect (seatless-membership audit, gridiron-2000 pick'em
// HQ task).
func (s *Service) assignMember(email, name string) (Member, error) {
	email = s.identityResolver.Resolve(email)
	member, created, err := s.store.AssignMember(email, name)
	if err != nil {
		return Member{}, err
	}
	if created {
		s.notifySeatClaimed(member)
	}
	return member, nil
}

// EnsureMember records email as a league member without claiming a team
// seat: the seatless-membership counterpart to AssignManager. Every
// signed-in, allowed email has somewhere to land, seat or no seat, and
// pick'em treats a TeamID "" member as a full participant. main.go's
// Google sign-in callback falls back to this when every seat is already
// claimed, rather than turning the sign-in away.
func (s *Service) EnsureMember(email, name string) (Member, error) {
	return s.ensureMember(email, name)
}

// ensureMember is the membership-only counterpart to assignMember: it
// records email as a league member but never assigns a team seat, and
// never fires N1 (no seat was claimed). Viewer's just-in-time record and
// actingTeam's both call this, not assignMember, so loading a page or
// attempting a non-team action never claims a seat as a side effect.
func (s *Service) ensureMember(email, name string) (Member, error) {
	email = s.identityResolver.Resolve(email)
	member, _, err := s.store.EnsureMember(email, name)
	if err != nil {
		return Member{}, err
	}
	return member, nil
}

// BindCoManagerOnSignIn consumes email's pending co-invite, if any
// (registration wave, build item 4): main.go's Google sign-in callback
// calls this before its ordinary EnsureMember fallback, so a co-invitee's
// first sign-in lands them bound to their seat instead of as a free
// agent. bound reports whether a pending invite existed; when it did, N1
// (notifySeatClaimed) fires for the co-manager's own email, exactly as it
// does for a primary's seat claim — this is that person's own welcome,
// not a fan-out.
func (s *Service) BindCoManagerOnSignIn(email, name string) (member Member, bound bool, err error) {
	email = s.identityResolver.Resolve(email)
	member, bound, err = s.store.BindCoManager(email, name)
	if err != nil || !bound {
		return member, bound, err
	}
	s.notifySeatClaimed(member)
	return member, bound, nil
}

// isPrimaryOfTeam reports whether the request's signed-in identity is
// teamID's own primary manager (Role == ""). Demo mode has no real
// per-seat identity, so it treats every named seat as actable — the same
// "demo mode may act as any seat" precedent actingTeam sets.
func (s *Service) isPrimaryOfTeam(r *http.Request, teamID string) bool {
	if s.demoMode {
		return knownTeam(teamID)
	}
	user, ok := s.CurrentUser(r)
	if !ok {
		return false
	}
	member, exists := s.store.MemberByEmail(user.Email)
	return exists && member.TeamID == teamID && member.Role == ""
}

// canManageCoManager reports whether the request may detach teamID's
// co-manager: the commissioner (any seat), or that seat's own primary
// manager (registration wave, build item 4: "Commissioner and primary
// can detach the co").
func (s *Service) canManageCoManager(r *http.Request, teamID string) bool {
	return s.IsCommissioner(r) || s.isPrimaryOfTeam(r, teamID)
}

// InviteCoManager lets teamID's primary manager invite email as the
// seat's co-manager (registration wave, build item 4): only the primary
// may invite (not the commissioner, not a co — see the build note),
// enforced here before the call ever reaches Store.InviteCoManager's own
// one-co-per-seat and already-seated checks.
func (s *Service) InviteCoManager(r *http.Request, teamID, email string) error {
	teamID = strings.TrimSpace(teamID)
	if !knownTeam(teamID) {
		return fmt.Errorf("unknown team %q", teamID)
	}
	if !s.isPrimaryOfTeam(r, teamID) {
		return errors.New("only the seat's primary manager may invite a co-manager")
	}
	return s.store.InviteCoManager(teamID, email)
}

// DetachCoManager lets teamID's primary manager or the commissioner
// remove the seat's co-manager, bound or still pending (registration
// wave, build item 4).
func (s *Service) DetachCoManager(r *http.Request, teamID string) error {
	teamID = strings.TrimSpace(teamID)
	if !knownTeam(teamID) {
		return fmt.Errorf("unknown team %q", teamID)
	}
	if !s.canManageCoManager(r, teamID) {
		return errors.New("only the seat's primary manager or the commissioner can detach a co-manager")
	}
	return s.store.DetachCoManager(teamID)
}

// NextOpenSeatTone resolves the tone the next AssignMember call would
// claim: the first default team with no member bound to it, in team
// order — the same "first team not already used" scan AssignMember
// itself runs (store.go), reused here so the fantasy-signup page's badge
// previews (build item 2) tint in the exact tone the claimed seat will
// carry. ok is false once every seat is claimed.
func (s *Service) NextOpenSeatTone() (tone string, ok bool) {
	used := claimedSeatIDs(s.store.Snapshot().Members)
	for _, team := range s.Teams() {
		if !used[team.ID] {
			return team.Tone, true
		}
	}
	return "", false
}

// UnclaimedBadgeOption is one motif the fantasy-signup page's badge picker
// offers: its slug (the claimed value and the avatar image filename stem)
// and its display name.
type UnclaimedBadgeOption struct {
	Slug string
	Name string
}

// unclaimedBadgeGrid renders the fantasy-signup page's badge picker
// (build item 2): every motif no team currently holds. Unlike the team
// page's full 16-motif grid (badgeGrid), a pre-signup visitor has no
// "mine" state of their own yet, and a taken motif is not worth showing
// at all — there is nothing to compare it against before the seat
// exists.
func (s *Service) unclaimedBadgeGrid(state PersistedState) []UnclaimedBadgeOption {
	taken := make(map[string]bool, len(state.BadgeClaims))
	for _, motif := range state.BadgeClaims {
		taken[motif] = true
	}
	out := make([]UnclaimedBadgeOption, 0, len(BadgeMotifs))
	for _, motif := range BadgeMotifs {
		if taken[motif.Slug] {
			continue
		}
		out = append(out, UnclaimedBadgeOption{Slug: motif.Slug, Name: motif.Name})
	}
	return out
}

// SignupData assembles the /join fantasy-signup page (build item 2): a
// signed-in member with no seat sees a one-form signup (team name + an
// unclaimed badge motif, previewed in the next open seat's tone). The
// page itself owns the already-seated redirect (main.go's
// redirectSeatedFromJoin) and renders the honest closed state when
// league_full is true.
func (s *Service) SignupData(r *http.Request) map[string]any {
	state := s.store.Snapshot()
	identityAvailable, identityError := s.identityView()
	viewer := s.Viewer(r)
	hasSeat, _ := viewer["has_seat"].(bool)
	open := len(s.Teams()) - claimedSeatCount(state.Members)
	if open < 0 {
		open = 0
	}
	tone, ok := s.NextOpenSeatTone()
	toneHex := ""
	if ok {
		toneHex, _ = BadgeToneHex(tone)
	}
	badgeGrid := []UnclaimedBadgeOption{}
	if identityAvailable {
		badgeGrid = s.unclaimedBadgeGrid(state)
	}
	return map[string]any{
		"viewer":             viewer,
		"has_seat":           hasSeat,
		"league_full":        open == 0,
		"open_seats":         open,
		"badge_tone_hex":     toneHex,
		"badge_grid":         badgeGrid,
		"identity_available": identityAvailable,
		"identity_error":     identityError,
		"league":             s.leagueMap(),
		"league_mode":        s.cfg.ModeLabel,
	}
}

// ClaimFantasySeat resolves the signed-in member's email/name from r and
// completes the fantasy-signup atomic claim (build item 2: team name +
// badge motif in one seat claim). An anonymous request or an
// already-seated member is rejected before any store write; see
// claimFantasySeat for the seat/name/badge write sequence and its
// rollback contract.
func (s *Service) ClaimFantasySeat(r *http.Request, teamName, motif string) (Team, error) {
	user, ok := s.CurrentUser(r)
	if !ok {
		return Team{}, fmt.Errorf("Google sign-in is required for league actions")
	}
	if member, exists := s.store.MemberByEmail(user.Email); exists && member.TeamID != "" {
		return Team{}, fmt.Errorf("you already hold a team seat")
	}
	return s.claimFantasySeat(user.Email, user.Name, teamName, motif)
}

// claimFantasySeat is the fantasy-signup atomic-claim primitive (build
// item 2), driven by email/name directly rather than an *http.Request so
// it is unit-testable without forging Google auth (see ClaimFantasySeat,
// the thin public wrapper that resolves those two from the request).
//
// Atomicity design: the three writes — AssignMember (claim the next open
// seat), Store.SetTeamName, Store.ClaimBadge — are not one store
// transaction; each of those three primitives already owns its own
// lock/persist, and merging them would mean either duplicating all three
// under Store or growing a bespoke fourth Store method whose only caller
// is this one path, for a rollback need that is otherwise rare (a badge
// motif race between two concurrent signups is the only realistic
// failure once the seat itself is claimed). Instead this method rolls
// back on a later failure: a name-set failure releases the just-claimed
// seat, and a badge-claim failure releases both the name and the seat —
// so a half-finished signup (a claimed seat with a placeholder name, or a
// name with no badge) never sits there for the next visitor to trip
// over. A best-effort motif-availability pre-check runs before the seat
// is even claimed, to keep the common "stale page, someone else already
// took that motif" case from claiming (and then rolling back) a seat at
// all; the store-level ClaimBadge call afterward remains the one
// authoritative check for the true concurrent-signup race, and its exact
// "that badge is already claimed by <team>" message is what both paths
// surface.
func (s *Service) claimFantasySeat(email, name, teamName, motif string) (Team, error) {
	if !s.store.IdentityHealthy() {
		return Team{}, errors.New(identityUnavailableCopy)
	}
	displayName, err := validateTeamName(teamName)
	if err != nil {
		return Team{}, err
	}
	if displayName == "" {
		return Team{}, fmt.Errorf("enter a team name")
	}
	motif = strings.TrimSpace(motif)
	// An empty motif means the signup form was submitted with no badge
	// picked. That is a different mistake from naming a motif the catalog
	// does not have, and it is the one a person actually makes, so it gets
	// its own message instead of "unknown badge motif". The form cannot
	// enforce this in the browser: the radios are visually hidden for
	// styling, and a required control that cannot be shown makes Chrome
	// block submission with no visible message at all.
	if motif == "" {
		return Team{}, errors.New("choose a badge for your team")
	}
	if !knownMotif(motif) {
		return Team{}, ErrBadgeUnknownMotif
	}
	for holderTeamID, claimedMotif := range s.store.BadgeClaims() {
		if claimedMotif == motif {
			return Team{}, &badgeTakenError{teamName: s.teamByID(holderTeamID).Name}
		}
	}
	member, err := s.assignMember(email, name)
	if err != nil {
		return Team{}, err
	}
	teamID := member.TeamID
	if err := s.store.SetTeamName(teamID, displayName); err != nil {
		_ = s.store.ReleaseSeat(teamID)
		return Team{}, err
	}
	if err := s.store.ClaimBadge(teamID, motif); err != nil {
		_ = s.store.SetTeamName(teamID, "")
		_ = s.store.ReleaseSeat(teamID)
		var claimed *badgeClaimedError
		if errors.As(err, &claimed) {
			return Team{}, &badgeTakenError{teamName: s.teamByID(claimed.teamID).Name}
		}
		return Team{}, err
	}
	return s.teamByID(teamID), nil
}

func (s *Service) Viewer(r *http.Request) map[string]any {
	user, signedIn := s.CurrentUser(r)
	if !signedIn {
		team := s.Teams()[0]
		return map[string]any{
			"signed_in":       false,
			"demo":            s.demoMode,
			"name":            "Guest Coach",
			"email":           "",
			"initials":        "GC",
			"team_id":         team.ID,
			"team_name":       team.Name,
			"has_seat":        s.demoMode,
			"is_commissioner": s.demoMode,
		}
	}
	member, ok := s.store.MemberByEmail(user.Email)
	if !ok {
		member, _ = s.ensureMember(user.Email, user.Name)
	}
	hasSeat := member.TeamID != ""
	teamID, teamName := "", ""
	if hasSeat {
		team := s.teamByID(member.TeamID)
		teamID, teamName = team.ID, team.Name
	}
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
		"team_id":         teamID,
		"team_name":       teamName,
		"has_seat":        hasSeat,
		"is_commissioner": s.IsCommissioner(r),
	}
}

// DashboardData assembles the home page. A seated member gets the full FF
// command center (live matchups, standings, activity); a signed-in member
// with no team seat gets a pick'em-forward dashboard instead — their
// record, this week's outstanding picks, and a link into the HQ — rather
// than an empty fantasy dashboard with nothing to show (build item 3).
// viewer["has_seat"] is precomputed server-side so app/page.gsx only ever
// branches on a plain bool, never re-derives seat state itself.
func (s *Service) DashboardData(ctx context.Context, r *http.Request) map[string]any {
	now := s.clock()
	live := s.feed.Snapshot(ctx, now)
	state := s.store.Snapshot()
	viewer := s.Viewer(r)
	hasSeat, _ := viewer["has_seat"].(bool)
	featured := s.matchupMaps(state, live.Matchups[:min(2, len(live.Matchups))])
	announcements := s.announcementListMaps(5)
	transactions := s.activityMaps(state, 5)
	standings := s.dashboardStandingState(state)
	standingsTitle, standingsNote, standingsEmptyTitle := s.dashboardStandingsCopy(state, standings)
	return map[string]any{
		"viewer":                viewer,
		"has_seat":              hasSeat,
		"draft":                 s.draftSummary(now),
		"live":                  s.liveMap(live),
		"featured":              featured,
		"standings":             s.standingsMaps(state),
		"divisions":             s.divisionMaps(state),
		"standings_available":   standings.HasResults,
		"standings_title":       standingsTitle,
		"standings_note":        standingsNote,
		"standings_empty_title": standingsEmptyTitle,
		"transactions":          transactions,
		"pickem_home":           s.pickemHomeSummary(r, state, now),
		// fantasy_card is the dashboard's FANTASY status card (registration
		// wave, build item 3): both status cards are always present for a
		// signed-in member, seated or not — see fantasyCardData.
		"fantasy_card": s.fantasyCardData(state, viewer),
		// GSX conditions cannot call .length on server data; ship the bools.
		"transactions_empty":  len(transactions) == 0,
		"featured_empty":      len(featured) == 0,
		"league_size":         len(s.Teams()),
		"season":              strconv.Itoa(s.cfg.Season),
		"league_mode":         s.cfg.ModeLabel,
		"league":              s.leagueMap(),
		"announcements":       announcements,
		"announcements_empty": len(announcements) == 0,
	}
}

// fantasyCardData assembles the dashboard's FANTASY status card
// (registration wave, build item 3): a seated member sees their team
// mark/name/record with a link to /team; a seatless member sees the
// signup CTA with the open-seat count while seats remain, or the honest
// closed line once the league is full. viewer is the caller's own
// already-resolved Viewer() map (DashboardData and TeamData both already
// hold one), so this never re-derives has_seat/team_id itself.
func (s *Service) fantasyCardData(state PersistedState, viewer map[string]any) map[string]any {
	hasSeat, _ := viewer["has_seat"].(bool)
	open := len(s.Teams()) - claimedSeatCount(state.Members)
	if open < 0 {
		open = 0
	}
	// "team" is always present (a zero-valued map when seatless) rather
	// than an absent key: GSX templates render map[string]any data
	// dynamically, so every branch app/page.gsx's <If> guards needs the
	// same key set to stay safe to evaluate regardless of which branch
	// actually renders — the same discipline pickem_home's
	// always-present map follows for the seatless-vs-seated split above
	// it.
	team := map[string]any{}
	if hasSeat {
		teamID, _ := viewer["team_id"].(string)
		team = s.teamMap(s.teamView(state, teamID))
	}
	return map[string]any{
		"has_seat":    hasSeat,
		"league_full": open == 0,
		"open_seats":  open,
		"team":        team,
	}
}

func seasonScheduleWeeks(schedule SeasonSchedule) []int {
	weeks := make([]int, 0, len(schedule.Weeks))
	seen := make(map[int]bool, len(schedule.Weeks))
	for _, week := range schedule.Weeks {
		if week.Week <= 0 || seen[week.Week] {
			continue
		}
		seen[week.Week] = true
		weeks = append(weeks, week.Week)
	}
	sort.Ints(weeks)
	return weeks
}

func matchupWeekHref(week int) string {
	return fmt.Sprintf("/matchups?week=%d", week)
}

func (s *Service) MatchupsData(ctx context.Context, r *http.Request) map[string]any {
	state := s.store.Snapshot()
	live := s.feed.Snapshot(ctx, s.clock())
	viewer := s.Viewer(r)
	currentWeek := live.Week
	selectedWeek := live.Week
	weekNotice := ""
	hasSchedule := state.Schedule != nil && len(state.Schedule.Weeks) > 0
	weekOptions := []map[string]any{}
	previousWeekHref := ""
	nextWeekHref := ""
	currentWeekHref := ""
	hasPreviousWeek := false
	hasNextWeek := false
	if hasSchedule {
		schedule := *state.Schedule
		currentWeek = currentScheduleWeek(schedule)
		selectedWeek = currentWeek
		weeks := seasonScheduleWeeks(schedule)
		rawWeek := strings.TrimSpace(r.URL.Query().Get("week"))
		if rawWeek != "" {
			parsed, err := strconv.Atoi(rawWeek)
			switch {
			case err != nil || parsed <= 0:
				weekNotice = fmt.Sprintf("Week %q is not on the published schedule. Showing Week %d.", rawWeek, currentWeek)
			case !containsInt(weeks, parsed):
				weekNotice = fmt.Sprintf("Week %d is not on the published schedule. Showing Week %d.", parsed, currentWeek)
			default:
				selectedWeek = parsed
			}
		}
		for _, week := range weeks {
			weekOptions = append(weekOptions, map[string]any{
				"value":    strconv.Itoa(week),
				"label":    fmt.Sprintf("WEEK %d", week),
				"selected": week == selectedWeek,
			})
		}
		for i, week := range weeks {
			if week != selectedWeek {
				continue
			}
			if i > 0 {
				hasPreviousWeek = true
				previousWeekHref = matchupWeekHref(weeks[i-1])
			}
			if i+1 < len(weeks) {
				hasNextWeek = true
				nextWeekHref = matchupWeekHref(weeks[i+1])
			}
			if selectedWeek != currentWeek {
				currentWeekHref = "/matchups"
			}
			break
		}
		// Keep the current request on the live feed. A selected historical or
		// future week gets an explicit persisted-schedule snapshot instead,
		// so the current-week poll can never replace the requested view.
		if selectedWeek != currentWeek || live.Week != currentWeek {
			if selected, err := (scheduleProvider{svc: s}).SnapshotWeek(ctx, s.clock(), selectedWeek); err == nil {
				live = selected
			}
		}
	} else if strings.TrimSpace(r.URL.Query().Get("week")) != "" {
		weekNotice = "The season schedule is not published yet; showing the preseason view."
	}
	if currentWeek <= 0 {
		currentWeek = live.Week
	}
	if selectedWeek <= 0 {
		selectedWeek = live.Week
	}
	isCurrentWeek := selectedWeek == currentWeek
	matchups := s.matchupMaps(state, live.Matchups)
	return map[string]any{
		"viewer":             viewer,
		"live":               s.liveMapForWeek(live, isCurrentWeek),
		"matchups":           matchups,
		"matchups_empty":     len(matchups) == 0,
		"leaders":            s.leaderMaps(),
		"league":             s.leagueMap(),
		"week":               selectedWeek,
		"current_week":       currentWeek,
		"has_weeks":          hasSchedule,
		"week_options":       weekOptions,
		"has_previous_week":  hasPreviousWeek,
		"previous_week_href": previousWeekHref,
		"has_next_week":      hasNextWeek,
		"next_week_href":     nextWeekHref,
		"is_current_week":    isCurrentWeek,
		"current_week_href":  currentWeekHref,
		"week_notice":        weekNotice,
		"has_week_notice":    weekNotice != "",
		"live_interval":      map[bool]string{true: "1m", false: ""}[isCurrentWeek],
		"next_matchup":       s.nextManagerMatchup(state, viewer, state.Schedule, currentWeek),
	}
}

func containsInt(values []int, want int) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func (s *Service) nextManagerMatchup(state PersistedState, viewer map[string]any, schedule *SeasonSchedule, currentWeek int) map[string]any {
	out := map[string]any{
		"has_seat":         false,
		"has_matchup":      false,
		"is_bye":           false,
		"week":             "",
		"week_label":       "",
		"team_name":        "",
		"opponent_name":    "",
		"opponent_manager": "",
		"location":         "",
		"location_label":   "",
		"href":             "",
		"message":          "Claim a franchise to see your next matchup.",
	}
	hasSeat, _ := viewer["has_seat"].(bool)
	if !hasSeat {
		return out
	}
	out["has_seat"] = true
	out["message"] = "Your next matchup will appear when the schedule is published."
	if schedule == nil || len(schedule.Weeks) == 0 {
		return out
	}
	teamID, _ := viewer["team_id"].(string)
	if strings.TrimSpace(teamID) == "" {
		out["has_seat"] = false
		out["message"] = "Claim a franchise to see your next matchup."
		return out
	}
	weeks := seasonScheduleWeeks(*schedule)
	for _, weekNumber := range weeks {
		if weekNumber <= currentWeek {
			continue
		}
		week, ok := scheduleWeekByNumber(*schedule, weekNumber)
		if !ok {
			continue
		}
		if week.ByeTeamID == teamID {
			out["is_bye"] = true
			out["week"] = strconv.Itoa(weekNumber)
			out["week_label"] = fmt.Sprintf("WEEK %d", weekNumber)
			out["href"] = matchupWeekHref(weekNumber)
			out["message"] = "BYE WEEK"
			return out
		}
		for _, matchup := range week.Matchups {
			var opponentID, location string
			switch {
			case matchup.HomeTeamID == teamID:
				opponentID, location = matchup.AwayTeamID, "HOME"
			case matchup.AwayTeamID == teamID:
				opponentID, location = matchup.HomeTeamID, "AWAY"
			default:
				continue
			}
			team := s.teamView(state, teamID)
			opponent := s.teamView(state, opponentID)
			manager := strings.TrimSpace(opponent.Manager)
			if manager == "" {
				manager = "UNCLAIMED"
			}
			out["has_matchup"] = true
			out["week"] = strconv.Itoa(weekNumber)
			out["week_label"] = fmt.Sprintf("WEEK %d", weekNumber)
			out["team_name"] = team.Name
			out["opponent_name"] = opponent.Name
			out["opponent_manager"] = manager
			out["location"] = location
			out["location_label"] = map[string]string{"HOME": "HOME", "AWAY": "AWAY"}[location]
			out["href"] = matchupWeekHref(weekNumber)
			out["message"] = ""
			return out
		}
	}
	out["message"] = "No later week remains on the published schedule."
	return out
}

// TeamData assembles the team terminal, now the lineup surface (WP-R1):
// every starting slot (from the live roster shape, CurrentRoster()) shows
// its assigned player or EMPTY with a managed assignment form, locked
// slots render read-only with the kickoff reason, and the bench lists the
// roster remainder. week defaults to the current NFL week
// (pickemWeek); ?week=N (current or future) previews a later week's
// carry-forward/auto-fill resolution, matching section 8.1's week
// selector.
func (s *Service) TeamData(r *http.Request) map[string]any {
	viewer := s.Viewer(r)
	teamID, _ := viewer["team_id"].(string)
	state := s.store.Snapshot()
	identityAvailable, identityError := s.identityView()
	// A seatless member (no team_id) gets the honest "no franchise" state
	// with the signup CTA, never team-1's roster (registration wave, build
	// item 6 — the flagged paper cut: teamView's empty-TeamID fallback to
	// defaultTeams()[0] used to leak team-1's own lineup to every seatless
	// visitor of /team).
	if hasSeat, _ := viewer["has_seat"].(bool); !hasSeat {
		return map[string]any{
			"viewer":               viewer,
			"has_seat":             false,
			"predraft_visible":     false,
			"predraft_has_board":   false,
			"predraft_board_count": 0,
			"predraft_ready":       false,
			"league":               s.leagueMap(),
			"league_mode":          s.cfg.ModeLabel,
			"fantasy_card":         s.fantasyCardData(state, viewer),
			"identity_available":   identityAvailable,
			"identity_error":       identityError,
			"badge_grid":           []map[string]any{},
		}
	}
	team := s.teamView(state, teamID)
	roster, drafted := s.rosterForTeam(state, teamID)
	projected := 0.0
	for _, player := range roster {
		projected += player.Projection
	}
	teamMap := s.teamMap(team)
	boardCount := len(state.Boards[boardKeyForViewer(state, s.viewerKey(r))])
	managerReady := state.Ready[teamID]
	badgeToneHex, _ := BadgeToneHex(team.Tone)
	hasBadgeClaim := false
	badgeGrid := []map[string]any{}
	if identityAvailable {
		_, hasBadgeClaim = s.store.BadgeClaim(teamID)
		badgeGrid = s.badgeGrid(state, teamID)
	}

	now := s.clock()
	games := s.schedule()
	currentWeek := s.pickemWeek(games, now)
	week := currentWeek
	if raw := strings.TrimSpace(r.URL.Query().Get("week")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed >= currentWeek {
			week = parsed
		}
	}
	preset := CurrentRoster()
	// Zone occupants (RESERVE, IR) never reach the lineup engine: general
	// is the starters/bench pool effectiveLineup/autoFillWeek may draw
	// from (roster-ops SK spec: "effectiveLineup/lineupStarters NEVER
	// include zone occupants").
	general, reserveOccupants, irOccupants := splitRosterZones(state, teamID, roster)
	lineup := effectiveLineup(preset, general, state.Lineups[teamID], week, games, now)
	scoringValues := s.currentScoringValues()
	matchupLabel, hasMatchupLabel := s.MatchupSourceLabel()
	filled := 0
	for _, a := range lineup.Slots {
		if a.HasPlayer {
			filled++
		}
	}
	weekOptions := make([]map[string]any, 0, 6)
	for w := currentWeek; w < currentWeek+6; w++ {
		weekOptions = append(weekOptions, map[string]any{
			"value":    strconv.Itoa(w),
			"label":    fmt.Sprintf("WEEK %d", w),
			"selected": w == week,
		})
	}

	placeOptions := make([]map[string]any, 0, len(general))
	for _, p := range general {
		placeOptions = append(placeOptions, map[string]any{
			"id": p.ID, "label": fmt.Sprintf("%s (%s)", p.Name, p.Position),
		})
	}

	return map[string]any{
		"viewer":               viewer,
		"has_seat":             true,
		"team":                 teamMap,
		"drafted":              drafted,
		"predraft_visible":     !state.DraftStarted && strings.TrimSpace(team.Manager) != "",
		"predraft_has_board":   boardCount > 0,
		"predraft_board_count": boardCount,
		"predraft_ready":       managerReady,
		"projected":            fmt.Sprintf("%.1f", projected),
		"division":             teamMap["division"],
		"scouting":             s.topAvailable(state, 3),
		"is_commissioner":      s.IsCommissioner(r),
		"league_mode":          s.cfg.ModeLabel,
		"league":               s.leagueMap(),
		"badge_tone_hex":       badgeToneHex,
		"has_badge_claim":      hasBadgeClaim,
		"badge_grid":           badgeGrid,
		"identity_available":   identityAvailable,
		"identity_error":       identityError,
		"roster_shape":         rosterShapeRows(),
		"shape_summary":        rosterShapeSummary(len(general) + len(reserveOccupants)),
		"week":                 strconv.Itoa(week),
		"week_options":         weekOptions,
		"starters":             s.starterRowMaps(lineup, general, games, now, scoringValues),
		"starters_filled":      strconv.Itoa(filled),
		"starters_total":       strconv.Itoa(len(lineup.Slots)),
		"bench":                playerMapsWithScoring(lineup.Bench, scoringValues, s.matchupIndexFor(games, week)),
		"bench_empty":          len(lineup.Bench) == 0,
		// RESERVE and IR sections (roster-ops SK spec): render-tolerant —
		// has_reserve/has_ir are false, and the section stays hidden,
		// whenever the active roster shape carries no such zone.
		"has_reserve":             len(preset.Reserve) > 0,
		"reserve_capacity":        fmt.Sprintf("%d / %d", len(reserveOccupants), preset.ReserveTotal()),
		"reserve_occupants":       s.zoneOccupantRows(reserveOccupants, scoringValues, games, week, now, false),
		"reserve_occupants_empty": len(reserveOccupants) == 0,
		"reserve_place_options":   placeOptions,
		"reserve_place_empty":     len(placeOptions) == 0,
		"has_ir":                  preset.IR > 0,
		"ir_capacity":             fmt.Sprintf("%d / %d", len(irOccupants), preset.IR),
		"ir_occupants":            s.zoneOccupantRows(irOccupants, scoringValues, games, week, now, true),
		"ir_occupants_empty":      len(irOccupants) == 0,
		"ir_place_options":        placeOptions,
		"ir_place_empty":          len(placeOptions) == 0,
		"ir_drop_options":         placeOptions,
		"ir_drop_empty":           len(placeOptions) == 0,
		// co_manager (registration wave, build item 4): "Operated by X ·
		// with Y" plus the invite/detach forms' authority gates. can_invite
		// is primary-only (only the primary may invite); can_detach also
		// admits the commissioner (canManageCoManager), matching
		// InviteCoManager/DetachCoManager's own authority rules.
		"co_manager": s.coManagerMap(r, state, teamID),
		// fantasy_card is unused on the seated branch's own template path
		// (its <If> only reaches the seatless section above), but the key
		// stays present anyway — the same "every branch carries the same
		// key set" discipline fantasyCardData's own "team" field follows —
		// so a template read never depends on which branch built the map.
		"fantasy_card":         s.fantasyCardData(state, viewer),
		"matchup_source_label": matchupLabel,
		"has_matchup_source":   hasMatchupLabel,
	}
}

// coManagerMap renders TeamData's "Operated by X · with Y" block: the
// primary's display name, the co-manager's (if bound or still pending),
// and whether the viewing request may invite or detach one.
func (s *Service) coManagerMap(r *http.Request, state PersistedState, teamID string) map[string]any {
	members := teamMembers(state.Members, teamID)
	primaryName, coName := "", ""
	hasCo := false
	for _, member := range members {
		if member.Role == "co" {
			coName = member.Name
			if coName == "" {
				coName = member.Email
			}
			hasCo = true
		} else {
			primaryName = member.Name
		}
	}
	pendingEmail := ""
	for email, pendingTeamID := range state.CoInvites {
		if pendingTeamID == teamID {
			pendingEmail = email
			break
		}
	}
	canInvite := s.isPrimaryOfTeam(r, teamID)
	return map[string]any{
		"primary_name":  primaryName,
		"has_co":        hasCo,
		"co_name":       coName,
		"has_pending":   pendingEmail != "" && !hasCo,
		"pending_email": pendingEmail,
		"can_invite":    canInvite && !hasCo && pendingEmail == "",
		"can_detach":    s.canManageCoManager(r, teamID) && (hasCo || pendingEmail != ""),
	}
}

// rosterShapeRows renders the league's live roster shape (CurrentRoster —
// the commissioner's runtime override included) as one display row per
// active slot, in slotTable engine order, with the bench appended last.
// Eligibility text is only carried for multi-position slots; a QB slot
// explaining "QB" would be noise.
func rosterShapeRows() []map[string]any {
	preset := CurrentRoster()
	out := make([]map[string]any, 0, len(slotTable)+3)
	for _, slot := range slotTable {
		count := preset.Slots[slot.Key]
		if count == 0 {
			continue
		}
		eligible := ""
		if len(slot.Eligible) > 1 {
			eligible = strings.Join(slot.Eligible, "/")
		}
		out = append(out, map[string]any{
			"label":        fmt.Sprintf("%s ×%d", slot.Key, count),
			"eligible":     eligible,
			"has_eligible": eligible != "",
		})
	}
	out = append(out, map[string]any{
		"label":        fmt.Sprintf("BENCH ×%d", preset.Bench),
		"eligible":     "",
		"has_eligible": false,
	})
	// Reserve/IR rows only appear when the active shape carries that zone
	// (roster-ops SK spec) — a flagship config with neither renders byte-
	// identical to before this rule existed.
	if reserveTotal := preset.ReserveTotal(); reserveTotal > 0 {
		keys := make([]string, 0, len(preset.Reserve))
		for key := range preset.Reserve {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		labels := make([]string, 0, len(keys))
		for _, key := range keys {
			labels = append(labels, fmt.Sprintf("%s ×%d", key, preset.Reserve[key]))
		}
		out = append(out, map[string]any{
			"label":        fmt.Sprintf("RESERVE ×%d", reserveTotal),
			"eligible":     strings.Join(labels, ", "),
			"has_eligible": true,
		})
	}
	if preset.IR > 0 {
		out = append(out, map[string]any{
			"label":        fmt.Sprintf("IR ×%d", preset.IR),
			"eligible":     "",
			"has_eligible": false,
		})
	}
	return out
}

// rosterShapeSummary is the one-line count summary under the shape strip:
// starters + bench + reserve = total, and how many of those spots the
// team has filled so far (reserve occupants counted; IR excluded — it
// sits outside Total(), the SK spec's cap-math rule). Slot-level
// assignment (who is the FLEX) is WP-R1's lineup engine; until it lands
// this stays an honest count, not a claim about which player sits in
// which slot.
func rosterShapeSummary(filled int) string {
	preset := CurrentRoster()
	total := preset.Total()
	open := total - filled
	if open < 0 {
		open = 0
	}
	base := fmt.Sprintf("%d starters + %d bench", preset.Starters(), preset.Bench)
	if reserveTotal := preset.ReserveTotal(); reserveTotal > 0 {
		base += fmt.Sprintf(" + %d reserve", reserveTotal)
	}
	summary := fmt.Sprintf("%s = %d spots · %d filled · %d open", base, total, filled, open)
	if preset.IR > 0 {
		summary += fmt.Sprintf(" · IR %d (outside cap)", preset.IR)
	}
	return summary
}

// rosterForTeam returns the team's current roster in currentRosters order
// (roster-ops spec section 3): draft picks first, then any free-agent
// adds, minus any drops — the replay over Picks plus Transactions, not
// Picks alone (fact 8's gap, closed by WP-R3). Every lineup, scoring, and
// team-page caller that already reads through here (lineup.go, scorer.go,
// TeamData) picks up add/drop effects with no further wiring. An empty
// slice means the seat holds no players yet; the page renders the empty
// state.
func (s *Service) rosterForTeam(state PersistedState, teamID string) ([]Player, bool) {
	pool := s.pool()
	pickByPlayer := make(map[string]DraftPick, len(state.Picks))
	for _, pick := range state.Picks {
		if pick.TeamID == teamID {
			pickByPlayer[pick.PlayerID] = pick
		}
	}
	ids := currentRosters(state)[teamID]
	roster := make([]Player, 0, len(ids))
	for _, id := range ids {
		player, ok := pool.byID[id]
		if !ok {
			continue
		}
		if player.Status == "" {
			if pick, ok := pickByPlayer[id]; ok {
				player.Status = fmt.Sprintf("Rd %d · Pick %d", pick.Round, pick.Number)
			} else {
				player.Status = "Free agency add"
			}
		}
		roster = append(roster, player)
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
	// Resolved once for the same reason: matchupIndexFor scans the whole
	// schedule, and a draft-room page can render hundreds of pool rows.
	games := s.schedule()
	matchup := s.matchupIndexFor(games, s.pickemWeek(games, now))
	matchupLabel, hasMatchupLabel := s.MatchupSourceLabel()
	picked := make(map[string]bool, len(state.Picks))
	for _, pick := range state.Picks {
		picked[pick.PlayerID] = true
	}
	pos := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("pos")))
	if pos == "ALL" {
		pos = ""
	}
	rawQuery := strings.TrimSpace(r.URL.Query().Get("q"))
	query := strings.ToLower(rawQuery)
	available := make([]Player, 0, len(pool.players))
	for _, player := range pool.players {
		if !picked[player.ID] {
			if pos != "" && player.Position != pos {
				continue
			}
			if !playerMatchesQuery(player, query) {
				continue
			}
			available = append(available, player)
		}
	}
	pagination := newPoolPagination(len(available), r.URL.Query().Get("page"))
	pagedAvailable := available[pagination.Start:pagination.End]
	nextNumber := len(state.Picks) + 1
	onClockID := teamOnClock(state.DraftOrder, nextNumber)
	onClock := s.teamView(state, onClockID)
	viewerTeam, _ := viewer["team_id"].(string)
	draftOpen := state.DraftStarted || s.store.draftLifecycleBypass
	canPick := draftOpen && viewerTeam == onClockID
	if s.demoMode && draftOpen {
		canPick = true
	}
	boardPanel := make([]map[string]any, 0, 5)
	for _, id := range state.Boards[boardKeyForViewer(state, s.viewerKey(r))] {
		if picked[id] {
			continue
		}
		if player, ok := pool.byID[id]; ok {
			boardPanel = append(boardPanel, playerMap(player, scoringValues, matchup))
			if len(boardPanel) == 5 {
				break
			}
		}
	}
	return map[string]any{
		"viewer":               viewer,
		"draft":                s.draftSummary(now),
		"teams":                s.draftTeamMaps(state, onClockID),
		"picks":                s.pickMaps(state, pool.byID, scoringValues),
		"available":            playerMapsWithScoring(pagedAvailable, scoringValues, matchup),
		"board":                boardPanel,
		"board_count":          len(boardPanel),
		"pool_label":           pool.label,
		"pool_live":            pool.label == "live" || pool.label == "cache",
		"pool_count":           len(pool.players),
		"available_count":      len(available),
		"pool_query":           rawQuery,
		"pool_position":        pos,
		"pool_total":           pagination.Total,
		"pool_page":            pagination.Page,
		"pool_pages":           pagination.Pages,
		"pool_page_size":       pagination.PageSize,
		"pool_page_start":      pagination.Start + 1,
		"pool_page_end":        pagination.End,
		"pool_has_previous":    pagination.HasPrevious,
		"pool_has_next":        pagination.HasNext,
		"pool_previous_href":   poolPageHref("/draft", pos, rawQuery, pagination.Page-1),
		"pool_next_href":       poolPageHref("/draft", pos, rawQuery, pagination.Page+1),
		"pool_all_href":        poolPageHref("/draft", "", rawQuery, 1),
		"pool_rb_href":         poolPageHref("/draft", "RB", rawQuery, 1),
		"pool_wr_href":         poolPageHref("/draft", "WR", rawQuery, 1),
		"pool_qb_href":         poolPageHref("/draft", "QB", rawQuery, 1),
		"pool_te_href":         poolPageHref("/draft", "TE", rawQuery, 1),
		"pool_k_href":          poolPageHref("/draft", "K", rawQuery, 1),
		"pool_dst_href":        poolPageHref("/draft", "DST", rawQuery, 1),
		"pool_p_href":          poolPageHref("/draft", "P", rawQuery, 1),
		"on_clock":             s.teamMap(onClock),
		"on_clock_id":          onClockID,
		"pick_number":          nextNumber,
		"picks_empty":          len(state.Picks) == 0,
		"round":                pickRound(activeTeamCount(state.DraftOrder), nextNumber),
		"can_pick":             canPick,
		"demo_mode":            s.demoMode,
		"ready_count":          readyCount(state.Ready),
		"manager_count":        len(s.Teams()),
		"viewer_ready":         viewerTeam != "" && state.Ready[viewerTeam],
		"viewer_autopick":      viewerTeam != "" && state.Autopick[viewerTeam],
		"order_randomized":     len(state.DraftOrder) > 0,
		"league_mode":          s.cfg.ModeLabel,
		"clock":                s.clockView(state, now),
		"league":               s.leagueMap(),
		"matchup_source_label": matchupLabel,
		"has_matchup_source":   hasMatchupLabel,
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
		"remaining_label":    countdownMMSSLabel(remaining),
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

// StaticPageData is the minimal data map for pages with no per-request
// state of their own (privacy, terms): just the viewer badge and the
// league identity block the shared layout needs on every route.
func (s *Service) StaticPageData(r *http.Request) map[string]any {
	return map[string]any{
		"viewer": s.Viewer(r),
		"league": s.leagueMap(),
	}
}

func (s *Service) LoginData(r *http.Request, configured bool) map[string]any {
	next := navigation.DefaultReturnPath
	if r != nil && r.URL != nil {
		next = navigation.SafeReturnPath(r.URL.Query().Get("next"))
	}
	return map[string]any{
		"viewer":          s.Viewer(r),
		"configured":      configured,
		"demo_mode":       s.demoMode,
		"seats":           len(s.Teams()),
		"seat_numbers":    seatNumbers(len(s.Teams())),
		"league":          s.leagueMap(),
		"draft":           s.draftSummary(s.clock()),
		"oauth_start":     navigation.OAuthStartPath(next),
		"return_path":     next,
		"has_return_path": next != navigation.DefaultReturnPath,
	}
}

func (s *Service) LiveScores(ctx context.Context) LiveSnapshot {
	return s.feed.Snapshot(ctx, s.clock())
}

// LiveScoresView flattens LiveScores into the shape /api/live/week actually
// serves: matchups/page.gsx's data-gosx-live-bind text region (gosx#217)
// can only walk a top-level key, or one level of nested-object keys, into
// the polled JSON — never through an array — so the per-team score and
// per-matchup status/clock this used to read out of the "matchups" array
// live in their own flat, ID-keyed objects instead ("scores", per team ID;
// "matchupStatus" and "matchupClock", per matchup ID). liveStatus and
// liveUpdated are the same "source · timestamp[, FALLBACK]" label and bare
// timestamp the old score-sync JS composed client-side in applySnapshot.
func (s *Service) LiveScoresView(ctx context.Context) map[string]any {
	live := s.LiveScores(ctx)
	presentation := matchupPresentation(live.State)
	scores := make(map[string]string, len(live.Matchups)*2)
	matchupStatus := make(map[string]string, len(live.Matchups))
	matchupClock := make(map[string]string, len(live.Matchups))
	matchupIndicator := make(map[string]string, len(live.Matchups))
	for _, matchup := range live.Matchups {
		scores[matchup.Away.ID] = fmt.Sprintf("%.1f", matchup.Away.Score)
		scores[matchup.Home.ID] = fmt.Sprintf("%.1f", matchup.Home.Score)
		status := matchup.Status
		if status == "" {
			status = "SYNCED"
		}
		matchupStatus[matchup.ID] = status
		matchupClock[matchup.ID] = matchupClockLabel(matchup.Clock)
		matchupIndicator[matchup.ID] = liveIndicatorToken(matchup.State)
	}
	timestamp := s.formatMatchupUpdate(live.LastUpdated)
	liveStatus := presentation["sync_label"] + " · " + timestamp
	if live.Warning != "" {
		liveStatus += " · FALLBACK"
	}
	return map[string]any{
		"ok":               live.OK,
		"source":           live.Source,
		"sourceLabel":      live.SourceLabel,
		"week":             live.Week,
		"weekLabel":        live.WeekLabel,
		"state":            live.State,
		"status":           live.Status,
		"warning":          live.Warning,
		"scores":           scores,
		"matchupStatus":    matchupStatus,
		"matchupClock":     matchupClock,
		"matchupIndicator": matchupIndicator,
		"liveStatus":       liveStatus,
		"liveUpdated":      timestamp,
		"liveIndicator":    liveIndicatorToken(live.State),
		"headlineTop":      presentation["headline_top"],
		"headlineBottom":   presentation["headline_bottom"],
		"refreshLabel":     presentation["refresh_label"],
		"noteTitle":        presentation["note_title"],
		"noteBody":         presentation["note_body"],
	}
}

// matchupClockLabel is the shared clock-cell fallback matchupMaps (initial
// render) and LiveScoresView (live-bind poll) both apply, so the two never
// disagree — see DefaultMatchupClockLabel's own doc comment.
func matchupClockLabel(clock string) string {
	if clock == "" {
		return DefaultMatchupClockLabel
	}
	return clock
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
	// Limits (optional knob, default off, SK spec): a draft pick is an
	// enforcement point too, just like an add/claim/trade.
	if position, limit, breach := teamWouldBreachLimit(state, s.pool().byID, teamID, []string{playerID}, nil); breach {
		return DraftPick{}, Player{}, Team{}, fmt.Errorf("%s", limitMessage(position, limit))
	}
	// The pick and its clock reset land in one store transaction (section
	// 4.6 of the pick-clock spec). A paused clock stays unarmed after the
	// pick — pause freezes the timer, not the draft — and the final pick
	// leaves the clock unarmed for good.
	totalPicks := len(s.Teams()) * CurrentDraftRounds()
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
	if user, ok := s.CurrentUser(r); ok {
		member, exists := s.store.MemberByEmail(user.Email)
		if !exists {
			var err error
			member, err = s.ensureMember(user.Email, user.Name)
			if err != nil {
				return "", err
			}
		}
		if member.TeamID == "" {
			return "", fmt.Errorf("claim a team seat before taking this action")
		}
		return member.TeamID, nil
	}
	if s.demoMode && knownTeam(requested) {
		return requested, nil
	}
	return "", fmt.Errorf("Google sign-in is required for league actions")
}

// draftIsLive reports the persisted commissioner-controlled lifecycle.
// The scheduled timestamp is deliberately not an authorization source.
func (s *Service) draftIsLive(_ time.Time) bool {
	return s.store.Snapshot().DraftStarted || s.store.draftLifecycleBypass
}

func (s *Service) draftSummary(now time.Time) map[string]any {
	location := s.draftTZ
	if location == nil {
		location, _ = time.LoadLocation(DefaultDraftTZ)
	}
	local := s.draftAt.In(location)
	timezone := strings.TrimSpace(s.cfg.Timezone)
	if timezone == "" {
		timezone = location.String()
	}
	statusLabel := "SCHEDULED WINDOW"
	statusNote := "The commissioner controls when the room opens. This is the scheduled draft window."
	if !now.Before(s.draftAt) {
		statusLabel = "AWAITING COMMISSIONER"
		statusNote = "The scheduled window has arrived. The room stays closed until the commissioner starts it."
	}
	state := PersistedState{}
	if s.store != nil {
		state = s.store.Snapshot()
	}
	if state.DraftStarted {
		statusLabel = "LIVE"
		statusNote = "The commissioner opened the room. Pick one is on the clock."
	}
	startedAt := ""
	if !state.DraftStartedAt.IsZero() {
		startedAt = state.DraftStartedAt.Format(time.RFC3339)
	}
	return map[string]any{
		"at":              s.draftAt.Format(time.RFC3339),
		"event_label":     "LEAGUE DRAFT",
		"date":            strings.ToUpper(local.Format("Mon · Jan")) + " " + strconv.Itoa(local.Day()),
		"time":            local.Format("3:04 PM MST"),
		"timezone":        timezone,
		"long_date":       local.Format("Monday, January 2, 2006"),
		"format":          s.draftFormatLabel(),
		"started":         state.DraftStarted,
		"started_at":      startedAt,
		"window_reached":  !now.Before(s.draftAt),
		"status_label":    statusLabel,
		"status_note":     statusNote,
		"days_until":      max(0, int(s.draftAt.Sub(now).Hours()/24)),
		"countdown_label": countdownDHMSLabel(s.draftAt.Sub(now)),
	}
}

// countdownDHMSLabel formats a duration the same way the data-gosx-countdown
// runtime's own compact "dhms" format does ("{days}d {HH}:{MM}:{SS}"), so
// the page's server-rendered initial text (see the v0.43.0 runtime guide's
// declarative countdown recipe) matches the client's first tick exactly —
// no visible jump once the runtime takes over one second after page load.
// A negative or zero remainder clamps to zero, matching the runtime's own
// clamp.
func countdownDHMSLabel(remaining time.Duration) string {
	if remaining < 0 {
		remaining = 0
	}
	total := int64(remaining.Seconds())
	days := total / 86400
	hours := (total % 86400) / 3600
	minutes := (total % 3600) / 60
	seconds := total % 60
	return fmt.Sprintf("%dd %02d:%02d:%02d", days, hours, minutes, seconds)
}

// countdownMMSSLabel formats a remaining-seconds count the same way the
// data-gosx-countdown runtime's own compact "mm:ss" format does (minutes
// unpadded, seconds zero-padded), so the pick clock's server-rendered
// initial text matches the client's first tick exactly.
func countdownMMSSLabel(remainingSeconds int) string {
	if remainingSeconds < 0 {
		remainingSeconds = 0
	}
	return fmt.Sprintf("%d:%02d", remainingSeconds/60, remainingSeconds%60)
}

// draftFormatLabel renders the draft-room masthead / invite-email format
// line: the config's own draft.format_label when the operator set one,
// otherwise "{ModeLabel titlecase} · Snake · {rounds} rounds" — which
// reproduces "Dynasty · Snake · 15 rounds" exactly for the neutral default
// (productization spec section 3.2 field rules).
func (s *Service) draftFormatLabel() string {
	if label := strings.TrimSpace(s.cfg.FormatLabel); label != "" {
		return label
	}
	return fmt.Sprintf("%s · Snake · %d rounds", titleCase(s.cfg.ModeLabel), s.cfg.Rounds)
}

// titleCase renders an all-caps or free-form mode label ("DYNASTY",
// "REDRAFT") as "Dynasty", "Redraft": first rune upper, the rest lower.
// Multi-word labels keep each word's own capitalization rule.
func titleCase(value string) string {
	words := strings.Fields(strings.ToLower(value))
	for i, word := range words {
		if word == "" {
			continue
		}
		words[i] = strings.ToUpper(word[:1]) + word[1:]
	}
	return strings.Join(words, " ")
}

// countWord maps a team/seat count to its English word (spec section 3.2's
// CountWord rule, 4-16 covers the config's validated team-count range).
// Counts outside the table fall back to the digit form so a future range
// change never panics or silently prints nothing.
func countWord(n int) string {
	words := map[int]string{
		4: "four", 5: "five", 6: "six", 7: "seven", 8: "eight",
		9: "nine", 10: "ten", 11: "eleven", 12: "twelve",
		13: "thirteen", 14: "fourteen", 15: "fifteen", 16: "sixteen",
	}
	if word, ok := words[n]; ok {
		return word
	}
	return strconv.Itoa(n)
}

// seatNumbers returns [1..n], the server-supplied replacement for the
// GSX login page's old literal []int{1,...,8} (spec section 3.6 fix 1).
func seatNumbers(n int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = i + 1
	}
	return out
}

// leagueURL resolves the league's public URL: the LEAGUE_URL environment
// override when set (checked live, so an operator or a test can flip it
// without reconstructing the service), otherwise league.url from config
// (spec section 3.3's env-wins precedence for this key).
func (s *Service) leagueURL() string {
	if url := strings.TrimSpace(os.Getenv("LEAGUE_URL")); url != "" {
		return url
	}
	return s.cfg.URL
}

// leaguePathURL resolves an application route against the league's public
// URL. Keeping route construction here prevents doubled slashes when an
// operator supplies a trailing slash and preserves any base path used by a
// hosted installation. If the configured URL is malformed, retain the old
// best-effort behavior; the caller still escapes it for its output context.
func (s *Service) leaguePathURL(route string) string {
	base := strings.TrimSpace(s.leagueURL())
	joined, err := url.JoinPath(base, route)
	if err == nil {
		return joined
	}
	return strings.TrimRight(base, "/") + "/" + strings.TrimLeft(route, "/")
}

// divisionList returns the distinct division names in first-seen team
// order, or nil when every team carries an empty division (a flat,
// undivided league — spec section 3.2: "division may be empty on all
// teams").
func (s *Service) divisionList() []string {
	seen := map[string]bool{}
	var out []string
	for _, team := range s.Teams() {
		if team.Division == "" || seen[team.Division] {
			continue
		}
		seen[team.Division] = true
		out = append(out, team.Division)
	}
	return out
}

// heroKicker renders the landing page's hero kicker: the config's own
// copy.hero_kicker when set, otherwise a generated line reproducing
// "Eight-manager dynasty league · Aqua and Orange divisions" in shape for
// any team count / division set (spec section 3.2, 5.2).
func (s *Service) heroKicker() string {
	if kicker := strings.TrimSpace(s.cfg.Copy.HeroKicker); kicker != "" {
		return kicker
	}
	label := fmt.Sprintf("%s-manager %s league", countWord(len(s.Teams())), strings.ToLower(s.cfg.ModeLabel))
	if divisions := s.divisionList(); len(divisions) > 0 {
		label += " · " + strings.Join(divisions, " and ") + " divisions"
	}
	return label
}

// seasonOpenLine renders "League play begins Week 1 · {Month Day}." from
// season_start_at (spec section 3.2's derived preseason string), replacing
// the old hardcoded "September 13" in app/matchups/page.gsx.
func (s *Service) seasonOpenLine() string {
	return "League play begins Week 1 · " + s.cfg.SeasonStartAt.Format("January 2") + "."
}

// scoringNote renders the scoring page's format footnote from
// scoring_format (spec section 3.2: "half-PPR" / "full-PPR" / "standard"
// consensus feed).
func (s *Service) scoringNote() string {
	label := "half-PPR"
	switch s.cfg.ScoringFormat {
	case "ppr":
		label = "full-PPR"
	case "standard":
		label = "standard"
	}
	return fmt.Sprintf("Draft ADP and projections use the %s consensus feed.", label)
}

// formatBlurb is the short, human-readable league-format phrase shared by
// public product copy. Dynasty and redraft describe league formats, not
// scoring systems; unknown labels use an honest neutral fallback.
func (s *Service) formatBlurb() string {
	switch strings.ToUpper(strings.TrimSpace(s.cfg.ModeLabel)) {
	case "DYNASTY":
		return "dynasty format"
	case "REDRAFT":
		return "redraft format"
	default:
		return "custom fantasy format"
	}
}

// leagueMap is the identity block every page's data map carries as
// data["league"] (spec section 5.1): the layout's header/footer, the
// landing page's wordmark and kicker, and every derived copy fragment
// read from config instead of a hardcoded literal.
func (s *Service) leagueMap() map[string]any {
	footerLabel := "PRESEASON MATCHUPS"
	footerLive := false
	if s.feed != nil {
		state := s.feed.Snapshot(context.Background(), s.clock()).State
		footerLive = state == MatchupStateInProgress
		switch state {
		case MatchupStateScheduled:
			footerLabel = "MATCHUPS SCHEDULED"
		case MatchupStateInProgress:
			footerLabel = "MATCHUPS IN PROGRESS"
		case MatchupStateFinal:
			footerLabel = "MATCHUP RESULTS FINAL"
		case MatchupStateDegraded:
			footerLabel = "MATCHUP STATUS LIMITED"
		}
	}
	return map[string]any{
		"name":                 s.cfg.Name,
		"short_code":           s.cfg.ShortCode,
		"tagline":              s.cfg.Tagline,
		"mode_label":           s.cfg.ModeLabel,
		"format_blurb":         s.formatBlurb(),
		"season":               strconv.Itoa(s.cfg.Season),
		"prior_season_short":   fmt.Sprintf("%02d", (s.cfg.Season-1)%100),
		"hero_kicker":          s.heroKicker(),
		"footer_line":          s.cfg.Copy.FooterLine,
		"has_footer_line":      s.cfg.Copy.FooterLine != "",
		"seat_count":           len(s.Teams()),
		"seat_count_word":      countWord(len(s.Teams())),
		"seat_numbers":         seatNumbers(len(s.Teams())),
		"season_open_line":     s.seasonOpenLine(),
		"matchup_footer_label": footerLabel,
		"matchup_footer_live":  footerLive,
		// fantasy_seats_open (registration wave, build item 3): whether any
		// fantasy seat remains unclaimed — the shared layout's nav uses
		// this, alongside the viewer's own has_seat, to show the /join link
		// only "while seats remain" (the build directive's own phrase),
		// without every page's own data function computing it separately.
		"fantasy_seats_open": claimedSeatCount(s.store.Snapshot().Members) < len(s.Teams()),
		// latest_announcement carries the shared layout's dismiss-free
		// banner data (league-announcements spec). It lives here, not in a
		// separate data key, because leagueMap is the one map every page's
		// data function already includes — see the doc comment above — so
		// the layout's banner needs no per-page wiring to reach it.
		"latest_announcement": s.latestAnnouncementBanner(),
	}
}

// announcementBannerWindow is how long a freshly posted announcement stays
// on the shared layout's banner (league-announcements spec: "under 72
// hours old").
const announcementBannerWindow = 72 * time.Hour

// latestAnnouncementBanner renders the shared layout's banner data: the
// single newest announcement when one exists and is under
// announcementBannerWindow old, otherwise has=false. Dismiss-free by
// design (no per-user dismissed state this wave) — see layout.gsx.
func (s *Service) latestAnnouncementBanner() map[string]any {
	state := s.store.Snapshot()
	if len(state.Announcements) == 0 {
		return map[string]any{"has": false, "body": "", "posted_at": ""}
	}
	latest := state.Announcements[0]
	if s.clock().Sub(latest.PostedAt) > announcementBannerWindow {
		return map[string]any{"has": false, "body": "", "posted_at": ""}
	}
	return map[string]any{
		"has":       true,
		"body":      latest.Body,
		"posted_at": latest.PostedAt.Format("Jan 2, 3:04 PM MST"),
	}
}

// announcementListMaps renders up to limit announcements (newest first, as
// stored) for the home page's announcements section: each entry's body,
// absolute posted time, and a relative "N hours ago" label.
func (s *Service) announcementListMaps(limit int) []map[string]any {
	state := s.store.Snapshot()
	now := s.clock()
	n := len(state.Announcements)
	if n > limit {
		n = limit
	}
	out := make([]map[string]any, 0, n)
	for _, a := range state.Announcements[:n] {
		out = append(out, map[string]any{
			"id":         a.ID,
			"body":       a.Body,
			"posted_by":  a.PostedBy,
			"posted_at":  a.PostedAt.Format("Jan 2, 3:04 PM MST"),
			"posted_ago": relativeTime(now, a.PostedAt),
		})
	}
	return out
}

// relativeTime renders a compact "N unit(s) ago" label for a past instant,
// floored at "just now" for anything under a minute. Only the coarsest
// unit that fits is shown (spec: home page's announcements section, "body
// + relative/absolute time").
func relativeTime(now, then time.Time) string {
	d := now.Sub(then)
	if d < time.Minute {
		return "just now"
	}
	switch {
	case d < time.Hour:
		return pluralUnit(int(d/time.Minute), "minute")
	case d < 24*time.Hour:
		return pluralUnit(int(d/time.Hour), "hour")
	default:
		return pluralUnit(int(d/(24*time.Hour)), "day")
	}
}

func pluralUnit(n int, unit string) string {
	if n == 1 {
		return "1 " + unit + " ago"
	}
	return fmt.Sprintf("%d %ss ago", n, unit)
}

// dashboardStandingsState is the one snapshot-derived standings view shared
// by DashboardData, the flat standings list, and the division-grouped home
// table. A nil schedule or a schedule with no finalized matchup deliberately
// leaves ByTeam empty: configured rank is not presented as a season result.
type dashboardStandingsState struct {
	ByTeam         map[string]Standing
	HasResults     bool
	LastScoredWeek int
	AllWeeksFinal  bool
}

func (s *Service) dashboardStandingState(state PersistedState) dashboardStandingsState {
	result := dashboardStandingsState{ByTeam: map[string]Standing{}}
	if state.Schedule == nil {
		return result
	}

	result.AllWeeksFinal = len(state.Schedule.Weeks) > 0
	teamIDs := make([]string, 0, len(s.Teams()))
	for _, team := range s.Teams() {
		teamIDs = append(teamIDs, team.ID)
	}
	for _, week := range state.Schedule.Weeks {
		if !matchupsAllFinal(week.Matchups) {
			result.AllWeeksFinal = false
		}
		for _, matchup := range week.Matchups {
			if matchup.Final {
				result.HasResults = true
				if week.Week > result.LastScoredWeek {
					result.LastScoredWeek = week.Week
				}
			}
		}
	}
	if !result.HasResults {
		return result
	}

	standings := ComputeStandings(*state.Schedule, teamIDs, TiebreakInputs{
		SeasonSeed: state.Schedule.Seed,
	})
	for _, standing := range standings {
		result.ByTeam[standing.TeamID] = standing
	}
	return result
}

func (s *Service) dashboardStandingsCopy(state PersistedState, standings dashboardStandingsState) (title, note, emptyTitle string) {
	season := s.cfg.Season
	if season == 0 && state.Schedule != nil {
		season = state.Schedule.Season
	}
	seasonLabel := strconv.Itoa(season)
	if state.Schedule == nil {
		return "Standings pending", "The commissioner has not published a regular-season schedule yet.", "NO SEASON TABLE"
	}
	if !standings.HasResults {
		return seasonLabel + " standings", "No matchup has been finalized yet. The table will populate after the first scored week.", "NO SCORED WEEKS"
	}
	title = seasonLabel + " standings"
	if standings.AllWeeksFinal || state.Phase == PhaseSeasonComplete {
		title = "Final " + seasonLabel + " standings"
	}
	note = fmt.Sprintf("Through Week %d · Records and points reflect finalized league matchups.", standings.LastScoredWeek)
	return title, note, ""
}

func standingRecord(standing Standing) string {
	if standing.Ties == 0 {
		return fmt.Sprintf("%d–%d", standing.Wins, standing.Losses)
	}
	return fmt.Sprintf("%d–%d–%d", standing.Wins, standing.Losses, standing.Ties)
}

func (s *Service) dashboardTeam(state PersistedState, team Team, standings map[string]Standing) Team {
	view := s.teamView(state, team.ID)
	if standing, ok := standings[team.ID]; ok {
		view.Record = standingRecord(standing)
		view.PointsFor = standing.PointsFor
		view.Rank = standing.Rank
		view.Streak = standing.Streak
	}
	return view
}

func (s *Service) standingsMaps(state PersistedState) []map[string]any {
	computed := s.dashboardStandingState(state)
	teams := append([]Team(nil), s.Teams()...)
	rank := func(team Team) int {
		if standing, ok := computed.ByTeam[team.ID]; ok {
			return standing.Rank
		}
		return team.Rank
	}
	sort.SliceStable(teams, func(i, j int) bool { return rank(teams[i]) < rank(teams[j]) })
	out := make([]map[string]any, 0, len(teams))
	for _, team := range teams {
		out = append(out, s.teamMap(s.dashboardTeam(state, team, computed.ByTeam)))
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
			"number": pick.Number,
			"round":  pick.Round,
			"team":   s.teamMap(team),
			// The pick tape never renders a projection (app/draft/page.gsx
			// shows only name/position/nfl_team for a pick-tape row), so no
			// schedule context is threaded here — zero-value matchupIndex{}
			// renders no opponent/matchup, matching the tape's own template.
			"player":          playerMap(player, scoringValues, matchupIndex{}),
			"made_by":         madeBy,
			"is_auto":         madeBy == "auto",
			"is_commissioner": madeBy == "commissioner",
		})
	}
	return out
}

// liveMapForWeek keeps the current week on the existing live/degraded
// presentation contract, while a historical or future selection is an
// explicitly static view. A non-current snapshot must not claim it will
// refresh itself or show a live indicator the page cannot update in place.
// Final results remain unchanged because their existing presentation is
// already static and authoritative.
func (s *Service) liveMapForWeek(live LiveSnapshot, isCurrentWeek bool) map[string]any {
	view := s.liveMap(live)
	if isCurrentWeek || live.State == MatchupStateFinal || live.State == MatchupStatePreseason {
		return view
	}
	for key, value := range matchupStaticPresentation(live.State) {
		view[key] = value
	}
	view["show_live_indicator"] = false
	view["live_indicator"] = ""
	return view
}

func matchupStaticPresentation(state string) map[string]string {
	switch state {
	case MatchupStateScheduled:
		return map[string]string{
			"headline_top": "WEEK", "headline_bottom": "SCHEDULED.",
			"sync_label": "Published schedule", "refresh_label": "Static week view",
			"note_title": "Scheduled scoring", "note_body": "This is a static schedule view; current-week scoring updates are shown on the current week.",
		}
	case MatchupStateInProgress:
		return map[string]string{
			"headline_top": "WEEK", "headline_bottom": "IN PROGRESS.",
			"sync_label": "Published snapshot", "refresh_label": "Static week view",
			"note_title": "Scoring snapshot", "note_body": "This week is a static snapshot; current-week scoring updates are shown on the current week.",
		}
	case MatchupStateDegraded:
		return map[string]string{
			"headline_top": "SCHEDULE", "headline_bottom": "STATUS.",
			"sync_label": "Status snapshot", "refresh_label": "Static week view",
			"note_title": "Limited matchup data", "note_body": "This week is a static snapshot; kickoff or scoring status is not currently authoritative.",
		}
	default:
		return map[string]string{
			"headline_top": "WEEK", "headline_bottom": "IN VIEW.",
			"sync_label": "Published snapshot", "refresh_label": "Static week view",
			"note_title": "Schedule snapshot", "note_body": "This week is a static snapshot; current-week scoring updates are shown on the current week.",
		}
	}
}

func (s *Service) liveMap(live LiveSnapshot) map[string]any {
	presentation := matchupPresentation(live.State)
	return map[string]any{
		"source":              live.Source,
		"source_label":        live.SourceLabel,
		"week":                live.Week,
		"week_label":          live.WeekLabel,
		"state":               live.State,
		"status":              live.Status,
		"last_updated":        s.formatMatchupUpdate(live.LastUpdated),
		"warning":             live.Warning,
		"headline_top":        presentation["headline_top"],
		"headline_bottom":     presentation["headline_bottom"],
		"sync_label":          presentation["sync_label"],
		"refresh_label":       presentation["refresh_label"],
		"note_title":          presentation["note_title"],
		"note_body":           presentation["note_body"],
		"show_live_indicator": live.State == MatchupStateInProgress,
		"live_indicator":      liveIndicatorToken(live.State),
	}
}

// liveIndicatorToken gives a text-only live binding a stable way to toggle
// the CSS-drawn dot. Empty means hidden; any non-empty token means visible.
// The token itself is visually suppressed by .live-dot--bound.
func liveIndicatorToken(state string) string {
	if state == MatchupStateInProgress {
		return "live"
	}
	return ""
}

func matchupPresentation(state string) map[string]string {
	switch state {
	case MatchupStateScheduled:
		return map[string]string{
			"headline_top": "WEEK", "headline_bottom": "SCHEDULED.",
			"sync_label": "Waiting for kickoff", "refresh_label": "Checks every 60 sec",
			"note_title": "Scheduled scoring", "note_body": "Scores begin updating after the first NFL kickoff for this fantasy week.",
		}
	case MatchupStateInProgress:
		return map[string]string{
			"headline_top": "LIVE", "headline_bottom": "SIGNAL.",
			"sync_label": "Feed connected", "refresh_label": "60 sec",
			"note_title": "Live scoring", "note_body": "Scores update on their own. No need to refresh the page.",
		}
	case MatchupStateFinal:
		return map[string]string{
			"headline_top": "FINAL", "headline_bottom": "SCORES.",
			"sync_label": "Results posted", "refresh_label": "Final",
			"note_title": "Final results", "note_body": "This fantasy week is closed and its posted scores are final.",
		}
	case MatchupStateDegraded:
		return map[string]string{
			"headline_top": "SCHEDULE", "headline_bottom": "STATUS.",
			"sync_label": "Timing unavailable", "refresh_label": "Retrying every 60 sec",
			"note_title": "Limited matchup data", "note_body": "Pairings remain visible, but kickoff or scoring status is not currently authoritative.",
		}
	default:
		return map[string]string{
			"headline_top": "MATCHUPS", "headline_bottom": "COMING SOON.",
			"sync_label": "Preseason schedule", "refresh_label": "Before Week 1",
			"note_title": "Preseason", "note_body": "Fantasy matchup scoring begins when the regular season opens.",
		}
	}
}

func (s *Service) matchupLocation() *time.Location {
	if s.draftTZ != nil {
		return s.draftTZ
	}
	if location, err := time.LoadLocation(strings.TrimSpace(s.cfg.Timezone)); err == nil && location != nil {
		return location
	}
	location, err := time.LoadLocation(DefaultDraftTZ)
	if err != nil || location == nil {
		return time.UTC
	}
	return location
}

func (s *Service) formatMatchupUpdate(value time.Time) string {
	return value.In(s.matchupLocation()).Format("Mon Jan 2 · 3:04:05 PM MST")
}

func (s *Service) matchupMaps(state PersistedState, matchups []ScoreMatchup) []map[string]any {
	out := make([]map[string]any, 0, len(matchups))
	for _, matchup := range matchups {
		away := s.teamView(state, matchup.Away.ID)
		home := s.teamView(state, matchup.Home.ID)
		// matchupMaps builds its own away/home maps rather than reusing
		// teamMap (it needs the live score, not the standings fields), so
		// the avatar fallback chain (design decision 4) is resolved here
		// too — this is the one TeamMark render path that does not already
		// pick it up through teamMap.
		awayHasAvatar, awayHasImage, awayAvatarURL := s.avatarView(away.ID, away.Tone)
		homeHasAvatar, homeHasImage, homeAvatarURL := s.avatarView(home.ID, home.Tone)
		out = append(out, map[string]any{
			"id":                  matchup.ID,
			"state":               matchup.State,
			"show_live_indicator": matchup.State == MatchupStateInProgress,
			"live_indicator":      liveIndicatorToken(matchup.State),
			"away": map[string]any{
				"id": matchup.Away.ID, "name": matchup.Away.Name, "abbreviation": matchup.Away.Abbreviation,
				"score": fmt.Sprintf("%.1f", matchup.Away.Score), "tone": away.Tone, "manager": away.Manager,
				"has_avatar": awayHasAvatar, "has_avatar_image": awayHasImage, "avatar_image_url": awayAvatarURL,
			},
			"home": map[string]any{
				"id": matchup.Home.ID, "name": matchup.Home.Name, "abbreviation": matchup.Home.Abbreviation,
				"score": fmt.Sprintf("%.1f", matchup.Home.Score), "tone": home.Tone, "manager": home.Manager,
				"has_avatar": homeHasAvatar, "has_avatar_image": homeHasImage, "avatar_image_url": homeAvatarURL,
			},
			"status": matchup.Status,
			"clock":  matchupClockLabel(matchup.Clock),
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
	for _, team := range s.Teams() {
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
	return s.Teams()[0]
}

func (s *Service) teamMap(team Team) map[string]any {
	manager := strings.TrimSpace(team.Manager)
	claimed := manager != ""
	if !claimed {
		manager = "UNCLAIMED"
	}
	// Team-avatar fallback chain (design decision 4): hasAvatar gates the
	// admin "reset avatar" control (only tier a — an uploaded photo — has
	// anything to reset); hasImage/avatarURL are what every TeamMark-class
	// render site needs to choose between an <img> and the plain text mark.
	hasAvatar, hasImage, avatarURL := s.avatarView(team.ID, team.Tone)
	return map[string]any{
		"id": team.ID, "name": team.Name, "abbreviation": team.Abbreviation, "division": strings.ToUpper(team.Division),
		"manager": manager, "claimed": claimed, "record": team.Record, "points_for": fmt.Sprintf("%.1f", team.PointsFor),
		"rank": fmt.Sprintf("%02d", team.Rank), "rank_number": team.Rank, "streak": team.Streak, "tone": team.Tone,
		"has_avatar": hasAvatar, "has_avatar_image": hasImage, "avatar_image_url": avatarURL,
	}
}

// divisionMaps groups the league into the divisions found in defaultTeams(), in
// first-occurrence (config) order, each with its teams sorted by rank.
// Zero divisions (every team's Division is "") renders one table; divisions
// of unequal size are legal (competition-formats spec section 1.3). Names
// and manager claims are resolved through teamView so overrides and claims
// reach the standings view.
func (s *Service) divisionMaps(state PersistedState) []map[string]any {
	computed := s.dashboardStandingState(state)
	byDivision := map[string][]Team{}
	seen := map[string]bool{}
	order := make([]string, 0, 2)
	for _, team := range s.Teams() {
		if !seen[team.Division] {
			seen[team.Division] = true
			order = append(order, team.Division)
		}
		byDivision[team.Division] = append(byDivision[team.Division], team)
	}
	rank := func(team Team) int {
		if standing, ok := computed.ByTeam[team.ID]; ok {
			return standing.Rank
		}
		return team.Rank
	}
	out := make([]map[string]any, 0, len(order))
	for _, division := range order {
		teams := append([]Team(nil), byDivision[division]...)
		sort.SliceStable(teams, func(i, j int) bool { return rank(teams[i]) < rank(teams[j]) })
		teamsOut := make([]map[string]any, 0, len(teams))
		for _, team := range teams {
			teamsOut = append(teamsOut, s.teamMap(s.dashboardTeam(state, team, computed.ByTeam)))
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
// pass nil to score the player's breakdown against the stock default
// rules instead, at no store-access cost. matchup resolves the player's
// live opponent and, when a ranking source is wired, that opponent's
// matchup-difficulty chip (see matchup.go's matchupIndex); pass the zero
// value matchupIndex{} where no schedule context applies (for example
// the draft pick tape, which never displays a projection). Callers that
// render many players per request should resolve scoringValues and
// matchup once and reuse them — see playerMapsWithScoring — rather than
// call playerMap in a bare loop, which forces every breakdown onto the
// default-only path and repeats the schedule scan per row.
func playerMap(player Player, scoringValues map[string]float64, matchup matchupIndex) map[string]any {
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
		breakdownRows, breakdownTotal = scoreBreakdownWithValues(player.ProjStats, scoringValues)
	}
	out := map[string]any{
		"id": player.ID, "name": player.Name, "position": player.Position, "nfl_team": player.NFLTeam,
		"projection": fmt.Sprintf("%.1f", player.Projection),
		"points":     fmt.Sprintf("%.1f", player.Points), "status": player.Status, "news": player.News,
		"rank": rank, "detail": detail,
		"headshot": player.Headshot, "has_headshot": player.Headshot != "",
		"jersey":          jersey,
		"has_breakdown":   hasBreakdown,
		"breakdown":       breakdownRows,
		"breakdown_total": breakdownTotal,
		"has_hist":        player.Hist != "",
		"hist":            player.Hist,
		"search":          strings.ToLower(player.Name + " " + player.NFLTeam + " " + player.Position),
		// is_rookie/draft_capital/has_draft_capital back the pool row's
		// rookie chip (owner directive 2026-08-18 — "show the reasoning"):
		// draft_capital is a compact label like "R1 · P8" pre-rendered by
		// fantasy.Player.DraftCapitalLabel and carried through untouched by
		// league.Player.DraftCapital, empty when Tank01 reports no usable
		// draft slot for this player. Display-only — see model.go's
		// Player.DraftCapital doc comment for why this can never change a
		// row's ORDER, only whether the chip renders.
		"is_rookie":         player.Rookie,
		"draft_capital":     player.DraftCapital,
		"has_draft_capital": player.DraftCapital != "",
	}
	for k, v := range matchup.fields(player) {
		out[k] = v
	}
	return out
}

// playerMaps renders many players against the stock default scoring rules
// and no matchup context. Pool-rendering callers should call
// playerMapsWithScoring instead, passing a scoringValues map and a
// matchupIndex resolved once per render; see currentScoringValues.
func playerMaps(players []Player) []map[string]any {
	return playerMapsWithScoring(players, nil, matchupIndex{})
}

// playerMapsWithScoring renders many players' view models against one
// already-resolved scoringValues map and matchupIndex, so a page with
// hundreds of players pays for one store snapshot and one schedule scan,
// not one per player. See currentScoringValues and matchupIndexFor.
func playerMapsWithScoring(players []Player, scoringValues map[string]float64, matchup matchupIndex) []map[string]any {
	out := make([]map[string]any, 0, len(players))
	for _, player := range players {
		out = append(out, playerMap(player, scoringValues, matchup))
	}
	return out
}

// zoneOccupantRows renders a RESERVE or IR zone's occupants for the team
// page: playerMap's usual fields plus, for IR only (checkHealed), whether
// the player no longer carries a qualifying injury designation and, when
// so, the activation deadline label (SK IR rule) — the "non-compliance/
// deadline state surfaced honestly" requirement. A nil injury source
// (never wired) or a schedule with no upcoming game for the player's NFL
// team both render "healed" false rather than guess.
func (s *Service) zoneOccupantRows(players []Player, scoringValues map[string]float64, games []GameInfo, week int, now time.Time, checkHealed bool) []map[string]any {
	rows := playerMapsWithScoring(players, scoringValues, s.matchupIndexFor(games, week))
	if !checkHealed {
		return rows
	}
	source := s.injuryDesignationSource()
	location := s.draftTZ
	if location == nil {
		location, _ = time.LoadLocation(DefaultDraftTZ)
	}
	for i, player := range players {
		healed := source != nil && !irEligible(source, player)
		rows[i]["healed"] = healed
		rows[i]["deadline_label"] = ""
		if !healed {
			continue
		}
		if kickoff, ok := nextKickoffForTeam(games, player.NFLTeam, now); ok {
			rows[i]["deadline_label"] = kickoff.In(location).Format("Mon 3:04 PM MST")
		}
	}
	return rows
}

// activityMaps merges Picks and Transactions into one time-sorted feed,
// newest first (roster-ops spec section 7.2: "the feed composes at read
// time" — this replaces the former transactionMaps() stub). limit caps
// the returned row count; zero means unlimited. Draft-pick lines resolve
// the player's live pool identity at read time; transaction lines render
// the TransactionPlayer identity snapshotted at commit time (section
// 7.1), so both survive pool churn. DashboardData calls this with limit 5
// for its panel; ActivityData (the /activity page) calls it with 0.
func (s *Service) activityMaps(state PersistedState, limit int) []map[string]any {
	pool := s.pool()
	type entry struct {
		at     time.Time
		teamID string
		action string
		player string
	}
	entries := make([]entry, 0, len(state.Picks)+len(state.Transactions))
	for _, pick := range state.Picks {
		label := pick.PlayerID
		if player, ok := pool.byID[pick.PlayerID]; ok {
			label = fmt.Sprintf("%s (%s)", player.Name, player.Position)
		}
		entries = append(entries, entry{at: pick.MadeAt, teamID: pick.TeamID, action: "drafts", player: label})
	}
	for _, txn := range state.Transactions {
		action, player := activityLine(txn)
		entries = append(entries, entry{at: txn.At, teamID: txn.TeamID, action: action, player: player})
	}
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].at.After(entries[j].at) })
	if limit > 0 && len(entries) > limit {
		entries = entries[:limit]
	}
	out := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		out = append(out, map[string]any{
			"time":   e.at.Format("Jan 2, 3:04 PM MST"),
			"team":   s.teamByID(e.teamID).Abbreviation,
			"action": e.action,
			"player": e.player,
		})
	}
	return out
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

// memberForTeam resolves "the" manager of a team seat: the primary
// (Role == "") when one is bound, otherwise a co-manager if that is all
// the seat has. A team seat holds at most one primary and one co Member
// (Store.InviteCoManager enforces the limit), so preferring the primary
// here — rather than returning whichever the map iteration visits first,
// which Go leaves undefined — is what keeps every single-manager display
// site (teamView's Manager name, the presence tracker, most notification
// recipient resolution) deterministic once a seat carries a co-manager.
// Callers that must reach every operator of a seat, not just one, use
// teamMembers instead.
func memberForTeam(members map[string]Member, teamID string) Member {
	var fallback Member
	haveFallback := false
	for _, member := range members {
		if member.TeamID != teamID {
			continue
		}
		if member.Role == "" {
			return member
		}
		fallback, haveFallback = member, true
	}
	if haveFallback {
		return fallback
	}
	return Member{}
}

// teamMembers returns every member bound to teamID, primary first (then
// the co-manager, if any) — the co-manager registration wave's "both
// operate the team identically" contract needs this wherever a
// team-scoped effect (a notification, an admin display) must reach every
// operator of a seat, not just the primary memberForTeam resolves.
func teamMembers(members map[string]Member, teamID string) []Member {
	var primary, co Member
	havePrimary, haveCo := false, false
	for _, member := range members {
		if member.TeamID != teamID {
			continue
		}
		if member.Role == "co" {
			co, haveCo = member, true
		} else {
			primary, havePrimary = member, true
		}
	}
	out := make([]Member, 0, 2)
	if havePrimary {
		out = append(out, primary)
	}
	if haveCo {
		out = append(out, co)
	}
	return out
}

// claimedSeatIDs is the set of team IDs currently bound to a member — a
// co-manager shares its primary's TeamID rather than opening a second
// seat, so this naturally de-duplicates: every "how many seats are
// open/claimed" computation (the fantasy dashboard card, the signup
// page, NextOpenSeatTone's AssignMember-mirroring scan) reads through
// this one function so they always agree, and a seat with both a primary
// and a co-manager still counts once.
func claimedSeatIDs(members map[string]Member) map[string]bool {
	out := make(map[string]bool, len(members))
	for _, member := range members {
		if member.TeamID != "" {
			out[member.TeamID] = true
		}
	}
	return out
}

// claimedSeatCount is claimedSeatIDs' count-only shorthand.
func claimedSeatCount(members map[string]Member) int {
	return len(claimedSeatIDs(members))
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
