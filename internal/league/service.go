package league

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gridiron-2000/internal/identity"
	"gridiron-2000/internal/navigation"
	"gridiron-2000/internal/notify"

	"m31labs.dev/gosx/auth"
)

// PlayerSource supplies the draft pool: players in draft order, a version
// that changes when the pool changes, and its canonical freshness state
// (live | cached | stale | degraded | offline | unavailable).
type PlayerSource func() ([]Player, int64, string)

// playerPool is the indexed, version-cached view of the draft pool.
type playerPool struct {
	version int64
	label   string
	players []Player
	byID    map[string]Player
	// byADP is players sorted ADP-ascending, a player with no meaningful ADP
	// (<=0) sorted last: built once per pool version (buildPool), not
	// per-request. draft_history.go's "best available at this pick" snapshot
	// reads this order directly (P1 perf fix, 2026-08-30) rather than
	// re-sorting the whole pool on every DraftHistory call.
	byADP []Player
	// byHouse is the house-ordered index: every player carrying a
	// HouseRank, in HouseRank order (best VORP first), followed by every
	// HouseRank-0 player in the pool's own order (players, above) — see
	// houseOrderedIndex (houserank.go). Built once per pool version
	// (buildPool), alongside byADP. autopickChoice (draftclock.go) walks
	// this order instead of players; the board display and every other
	// "best available" consumer keep reading players/byADP (market ADP).
	byHouse []Player
	// unavailable is set when an explicitly wired production source has no
	// authoritative rows. It keeps the embedded resolution players out of
	// the lookup map as well as the ordered pool, so a stale player ID cannot
	// turn a source outage into an actionable roster mutation.
	unavailable bool
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
	// testNow is the simulation harness's clock seam. It wins over now
	// when set; see SetClockForTest and clock().
	testNow atomic.Pointer[func() time.Time]
	// topologyMutationHook is a test-only checkpoint within the shared
	// topology-serialization boundary, immediately before a candidate store
	// write or between a trim/reset commit and runtime publication. It makes
	// interleavings deterministic; nil in every live Service.
	topologyMutationHook func(string)
	// presence tracks per-viewer last-seen instants, in memory only. The
	// zero value is ready to use (see presenceTracker's doc comment).
	presence presenceTracker
	// pickClockDefault is the PICK_CLOCK environment default, parsed once
	// at construction. Zero (the test-construction default) falls back to
	// DefaultPickClock; see pickClock.
	pickClockDefault time.Duration

	poolMu     sync.Mutex
	poolSource PlayerSource
	poolCache  playerPool
	// poolFrozenLoggedVersion/poolFrozenLoggedSet de-duplicate the pool()
	// freeze log line (rules-audit item 1): a started, incomplete draft
	// can see hundreds of pool() calls before the next resync, and every
	// one of them rejects the same pending source version, so without
	// this pair the freeze would log once per request instead of once
	// per rejected version. poolFrozenLoggedSet distinguishes "never
	// logged yet" from "logged version 0", since 0 is itself a valid
	// PlayerSource version (see buildPool's demo-mode callers). Guarded
	// by poolMu, like poolCache.
	poolFrozenLoggedVersion int64
	poolFrozenLoggedSet     bool
	// liveNameIndex is ResolveLivePlayer's normalized-name lookup, built
	// lazily and rebuilt only when liveNameVersion no longer matches
	// poolCache.version — see ResolveLivePlayer.
	liveNameVersion int64
	liveNameIndex   map[string][]Player
	poolStatusFn    PoolStatusSource
	scheduleFn      ScheduleSource
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
	// liveVersionFn and liveStatusFn are the live-scoring poller's two
	// seams (live_status.go): liveVersionFn is the cheap int64 accessor
	// StateFingerprint and the live feed cache call on every request;
	// liveStatusFn copies the poller's state and is read only by the
	// overlay and the render path (Task 10). Both nil means live scoring
	// is not wired — every reader falls back to its existing behavior.
	liveVersionFn func() int64
	liveStatusFn  LiveStatusSource
	// blitzPre1Fn supplies preseason-week-1 production (owner directive,
	// 2026-08-16); see blitz.go's SetBlitzPre1Source. nil means no pre1
	// data is available yet — the board falls back to its non-pre1
	// rookie/ADP tiering, never a crash.
	blitzPre1Fn BlitzPre1Source
	// blitzPre1SnapshotFn is the provenance-aware pre1 seam. The legacy
	// blitzPre1Fn remains for tests/integrations that only provide a map.
	blitzPre1SnapshotFn BlitzPre1SnapshotSource
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

	// draftQueue is the single-consumer dispatch channel SetDraftEventSink
	// installs and StopDraftEvents tears down; nil until a sink is wired,
	// which makes emitDraft a silent no-op in every test that does not opt
	// in. draftQueueCancel stops draftEventDrain's goroutine. Both are
	// guarded by poolMu, alongside lastPresence below.
	draftQueue chan DraftEvent
	// draftRepairSignal is emitDraft's drop notifier: buffered to exactly
	// one slot, so a burst of drops coalesces into at most one pending
	// repair; see draftRepairLoop.
	draftRepairSignal chan struct{}
	// draftRepairQueue is the repair's own single-slot delivery channel
	// (P2 perf fix, 2026-08-30 review): draftEventDrain selects on it
	// alongside draftQueue, so draftRepairLoop's blocking send
	// (emitDraftEventBlocking) never shares draftQueue with — and can never
	// stall behind the drain rate of — a real producer's own non-blocking
	// emitDraftEvent.
	draftRepairQueue chan DraftEvent
	draftQueueCancel context.CancelFunc
	// draftEmitMu serializes emitDraft's "assign the next generation" with
	// "push onto draftQueue" as one step, so two concurrent producers (an
	// HTTP pick and the clock ticker's autopick, say) can never enqueue out
	// of generation order; see draft_events.go.
	draftEmitMu sync.Mutex
	// draftGeneration is draft:*'s monotonic ordering key, one process-wide
	// counter shared by every event name so a client can total-order a mix
	// of draft:pick, draft:clock, and draft:seat.
	draftGeneration atomic.Uint64
	// draftDropped counts events emitDraft discarded because draftQueue was
	// full; draftFullBinds surfaces it as dropped_events on every
	// draft:state so a client can tell a repair is authoritative rather
	// than a delta.
	draftDropped atomic.Uint64
	// draftCompleteEmitted is maybeEmitDraftComplete's single-emit latch:
	// CompareAndSwap(false, true) makes "was the completion draft:state
	// already sent" atomic across every call site (MakePick, clockTick's
	// autopick, AdminForceAutopick), which can race each other independently
	// of the store's own per-pick serialization. AdminResetDraft and an
	// AdminUndoPick that reopens the final slot reset it to false.
	draftCompleteEmitted atomic.Bool
	// lastPresence is clockTick's and RecordPresence's shared memory of each
	// seat's last-announced presence label, guarded by poolMu. It exists so
	// emitPresenceTransitions (and RecordPresence's own transition check)
	// emit one draft:seat per real change, never once per tick or poll.
	lastPresence map[string]string

	// lockerEventSink is the locker-live hub's broadcast hook (GC-4;
	// internal/league cannot import app/locker, the same constraint
	// SetDraftEventSink already documents). nil in every test Service
	// literal and until app_build.go calls SetLockerEventSink; every
	// PostLockerPost/RemoveLockerPost commit calls it, nil-safe, right
	// after its own store write succeeds — locker mutations are
	// synchronous HTTP requests, not an external async source, so no
	// queue/generation machinery is needed the way draft's own sink uses.
	lockerEventSink func()
}

// SetLockerEventSink installs the hook PostLockerPost/RemoveLockerPost call
// after every successful commit (GC-4). fn is nil-safe to call through;
// passing nil restores the no-op default.
func (s *Service) SetLockerEventSink(fn func()) {
	s.poolMu.Lock()
	s.lockerEventSink = fn
	s.poolMu.Unlock()
}

func (s *Service) emitLockerChanged() {
	s.poolMu.Lock()
	fn := s.lockerEventSink
	s.poolMu.Unlock()
	if fn != nil {
		fn()
	}
}

// clock returns the service's current instant, in three-way precedence
// order: the simulation harness's clock (testNow) when set, then the
// package-test now hook when set, then time.Now(). Every time-dependent
// decision outside the enforcement loop's own ticker wiring reads the
// instant through here, so tests (and the harness) can drive the whole
// system with a fake clock.
func (s *Service) clock() time.Time {
	if fn := s.testNow.Load(); fn != nil {
		return (*fn)()
	}
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

var (
	defaultOnce sync.Once
	// defaultMu guards defaultSvc's assignment below against any read that
	// does not go through Default() itself — sync.Once only orders callers
	// of Do, and applyActiveConfig (config.go) reads defaultSvc directly
	// (also from every test that calls applyActiveConfig without ever
	// calling Default()). currentDefaultService is the one sanctioned
	// direct read.
	defaultMu  sync.Mutex
	defaultSvc *Service
)

// currentDefaultService safely reads defaultSvc outside Default()'s own
// sync.Once-ordered goroutine — see defaultMu's doc comment above.
func currentDefaultService() *Service {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	return defaultSvc
}

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
		// Demo mode requires an explicit opt-in AND a local APP_ENV. There is
		// no "GOOGLE_CLIENT_ID is empty" default anymore: a bare, unconfigured
		// deployment must boot closed (SETUP/invite-only), never as an open
		// demo commissioner console. Demo mode bypasses the sign-in gate and
		// grants commissioner powers to every visitor; one misconfigured or
		// auto-loaded env file must not be able to open a live league to the
		// internet. isLocalAppEnv is an allow-list ("", local, development,
		// test), so APP_ENV=prod, APP_ENV=staging, APP_ENV=production, and
		// every unknown label all refuse demo unconditionally.
		appEnv := os.Getenv("APP_ENV")
		demoRequested := parseBool(os.Getenv("DEMO_MODE"), false)
		demo := demoRequested && IsLocalAppEnv(appEnv)
		if demoRequested && !demo {
			log.Printf("league: DEMO_MODE=true requested but APP_ENV=%q is not a local environment (\"\", local, development, test); demo mode is refused", appEnv)
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
		// GC-1 fix 2: seed the reception scoring rule from scoring_format
		// on a genuinely fresh league (Scoring still empty). A non-fatal
		// persistence hiccup here must not block boot — the shipped
		// half_ppr default still serves correctly, just possibly
		// mismatched with scoring_format until the next successful write.
		if err := store.InitReceptionFromScoringFormat(ReceptionPointsForScoringFormat(cfg.ScoringFormat)); err != nil {
			log.Printf("league config: reception scoring seed from scoring_format failed: %v", err)
		}
		defaultMu.Lock()
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
			pickClockDefault:  resolvePickClockDefault(os.Getenv("PICK_CLOCK"), cfg.PickClockSeconds),
			avatarRoot:        avatarEnvString("AVATAR_ROOT", filepath.Join("data", "avatars")),
			avatarDurableRoot: avatarEnvString("AVATAR_DURABLE_ROOT", "data"),
			defaultBadgeRoot:  avatarEnvString("AVATAR_DEFAULTS_ROOT", filepath.Join("public", "avatars", "defaults")),
			motifRoot:         avatarEnvString("AVATAR_MOTIFS_ROOT", filepath.Join("public", "avatars", "motifs")),
		}
		defaultMu.Unlock()
		// scheduleProvider reads the persisted league schedule once one has
		// been generated; until then it defers to the honest preseason
		// snapshot (feed.go). This replaces the always-empty demoProvider
		// default (competition-formats spec section 2.5).
		defaultSvc.feed = newLiveFeed(scheduleProvider{svc: defaultSvc}, defaultSvc)
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

// StateSchemaCompatibility exposes the store's actual persisted schema
// marker and this binary's supported upper bound without exposing league
// state or any operator-only storage details.
func (s *Service) StateSchemaCompatibility() StateSchemaCompatibility {
	if s == nil || s.store == nil {
		return StateSchemaCompatibility{SupportedVersion: currentSchemaVersion}
	}
	return s.store.StateSchemaCompatibility()
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

// topologyMutationCheckpoint is a test-only seam used to hold one topology
// mutation at a named point. The service-level mutex in admin.go remains the
// actual correctness mechanism; this callback only makes ordering tests able
// to exercise its read/write/publication boundary deterministically.
func (s *Service) topologyMutationCheckpoint(operation string) {
	if s.topologyMutationHook != nil {
		s.topologyMutationHook(operation)
	}
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

// resolvePickClockDefault applies the documented precedence: PICK_CLOCK
// (environment) over draft.pick_clock_seconds (league.json) over
// DefaultPickClock. Every result clamps to [MinPickClock, MaxPickClock].
func resolvePickClockDefault(env string, configSeconds int) time.Duration {
	if strings.TrimSpace(env) != "" {
		return parsePickClock(env)
	}
	if configSeconds > 0 {
		return clampPickClock(time.Duration(configSeconds) * time.Second)
	}
	return DefaultPickClock
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

func (s *Service) DraftAt() time.Time {
	state := PersistedState{}
	if s.store != nil {
		state = s.store.Snapshot()
	}
	return s.EffectiveDraftAt(state)
}

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

// membershipAdmissionResolver is the private enforcement projection behind
// MembershipPosture. Its invitation and commissioner maps never cross into
// public view data; callers receive only the typed, PII-free posture value.
type membershipAdmissionResolver struct {
	posture       MembershipPosture
	invitations   map[string]struct{}
	commissioners map[string]struct{}
}

func (s *Service) membershipAdmissionResolver() membershipAdmissionResolver {
	invitations := make(map[string]struct{})
	for _, email := range splitEmails(os.Getenv("LEAGUE_ALLOWED_EMAILS")) {
		invitations[email] = struct{}{}
	}
	if s.store != nil {
		for _, invited := range s.store.Snapshot().Invites {
			if invited = admissionEmail(invited); invited != "" {
				invitations[invited] = struct{}{}
			}
		}
	}
	commissioners := make(map[string]struct{})
	for _, candidate := range splitEmails(os.Getenv("COMMISSIONER_EMAILS")) {
		canonical := s.identityResolver.Resolve(candidate)
		if canonical != "" {
			commissioners[canonical] = struct{}{}
		}
	}
	return membershipAdmissionResolver{
		posture:       ResolveMembershipPosture(s.cfg.Membership.AllowedDomain, len(invitations) > 0),
		invitations:   invitations,
		commissioners: commissioners,
	}
}

// MembershipPosture returns the effective, PII-free initial-admission
// contract used by admission and every public copy surface.
func (s *Service) MembershipPosture() MembershipPosture {
	return s.membershipAdmissionResolver().posture
}

func (p membershipAdmissionResolver) allows(rawEmail, canonicalEmail string) bool {
	if canonicalEmail != "" {
		if _, ok := p.commissioners[canonicalEmail]; ok {
			return true
		}
	}
	domain := p.posture.Domain
	if domain != "" && membershipDomainMatches(rawEmail, domain) {
		return true
	}
	if _, ok := p.invitations[rawEmail]; ok {
		return true
	}
	return p.posture.IsOpenAfterSignIn()
}

func (s *Service) hasPersistedMembership(canonicalEmail string) bool {
	if s.store == nil || canonicalEmail == "" {
		return false
	}
	state := s.store.Snapshot()
	if _, ok := state.Members[canonicalEmail]; ok {
		return true
	}
	// Startup reconciliation normally canonicalizes these keys. The scan keeps
	// a legacy member record continuous if a test or an older state snapshot is
	// observed before that migration runs.
	for key, member := range state.Members {
		if s.identityResolver.Resolve(key) == canonicalEmail ||
			s.identityResolver.Resolve(member.Email) == canonicalEmail {
			return true
		}
	}
	return false
}

// EmailAllowed reports whether a provider identity may create its initial
// persisted membership. Admission order is: persisted membership continuity,
// configured commissioner identity/aliases, raw provider-domain match, raw
// explicit invitation, then the no-domain/no-invitation setup fallback.
// Raw domain and invitation checks intentionally use the provider identity;
// the only canonicalization bypass is an explicit configured commissioner.
func (s *Service) EmailAllowed(email string) bool {
	rawEmail := admissionEmail(email)
	if rawEmail == "" {
		return false
	}
	canonicalEmail := s.identityResolver.Resolve(rawEmail)
	if s.hasPersistedMembership(canonicalEmail) {
		return true
	}
	return s.membershipAdmissionResolver().allows(rawEmail, canonicalEmail)
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
		if s.identityResolver.Resolve(candidate) == email {
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
//
// It also emits draft:seat when this heartbeat moves the requester's own
// presence label (never a team-wide rescan — that is clockTick's
// emitPresenceTransitions, guarded by the same lastPresence map): read the
// key's presenceStateSince before and after record, and, when the key
// belongs to a claimed seat, emit only on a real change.
func (s *Service) RecordPresence(r *http.Request, now time.Time) {
	key := s.viewerKey(r)
	if key == "" {
		return
	}
	seenAt, seen := s.presence.seen(key)
	before := presenceStateSince(seenAt, seen, now, s.presence.startedAt)
	s.presence.record(key, now)
	after := presenceStateSince(now, true, now, s.presence.startedAt)
	if after == before {
		return
	}
	// Only a heartbeat's own key transitioned so far; the snapshot and the
	// team-aggregate lookup below cost a store read, so they wait until
	// that much is established. Every non-transitioning poll (the common
	// case, one every PollPeriod) returns above without either.
	state := s.store.Snapshot()
	teamID := s.teamForPresenceKey(state, key)
	if teamID == "" {
		return
	}
	label, _, _ := s.teamPresence(state, teamID, now)
	s.poolMu.Lock()
	if s.lastPresence == nil {
		s.lastPresence = map[string]string{}
	}
	previous, seenTeam := s.lastPresence[teamID]
	// A co-manager can already hold the seat's aggregate at label (teamPresence
	// takes the strongest of every operator assigned to the seat), even
	// though this key's own state just moved: the room's rendered presence
	// has not changed, so this poll must emit nothing, the same rule
	// emitPresenceTransitions enforces on the ticker side.
	if seenTeam && previous == label {
		s.poolMu.Unlock()
		return
	}
	s.lastPresence[teamID] = label
	s.poolMu.Unlock()
	s.emitDraft("draft:seat", s.seatBinds(state, teamID, now))
}

// teamForPresenceKey returns the seat key belongs to, or "" when it is not
// assigned to any seat (an unclaimed viewer, or a commissioner with no
// team).
func (s *Service) teamForPresenceKey(state PersistedState, key string) string {
	for _, team := range s.Teams() {
		for _, candidate := range s.presenceKeysForTeam(state, team.ID) {
			if candidate == key {
				return team.ID
			}
		}
	}
	return ""
}

// boardKeyForTeam resolves the deterministic shared Big Board owner for one
// seat. Primary and co-managers intentionally share this board, so autopick
// must continue to read the same order whichever operator last edited it.
// Presence is separate: it aggregates every operator independently below.
func (s *Service) boardKeyForTeam(state PersistedState, teamID string) string {
	if s.demoMode {
		return "demo-guest"
	}
	return strings.ToLower(strings.TrimSpace(memberForTeam(state.Members, teamID).Email))
}

// presenceKeysForTeam returns every operator identity assigned to a seat.
// Co-managers are included so one connected operator keeps the seat visibly
// HERE even when the primary is away. Demo mode gives its single synthetic
// viewer the first rehearsal seat; it must not make every unclaimed seat look
// attended.
func (s *Service) presenceKeysForTeam(state PersistedState, teamID string) []string {
	if s.demoMode {
		if teams := s.Teams(); len(teams) > 0 && teamID == teams[0].ID {
			return []string{"demo-guest"}
		}
		return nil
	}
	seen := map[string]bool{}
	keys := make([]string, 0, 2)
	for _, member := range teamMembers(state.Members, teamID) {
		key := strings.ToLower(strings.TrimSpace(member.Email))
		if key != "" && !seen[key] {
			seen[key] = true
			keys = append(keys, key)
		}
	}
	return keys
}

// teamPresence aggregates the operators assigned to one seat. The strongest
// current state wins in the order HERE > IDLE > AWAY > NOT SEEN. The returned
// copy is intentionally explicit about freshness; presence is observational
// UI/notification context and never an authority to shorten a pick clock.
func (s *Service) teamPresence(state PersistedState, teamID string, now time.Time) (string, string, time.Time) {
	keys := s.presenceKeysForTeam(state, teamID)
	if len(keys) == 0 {
		return "unclaimed", "No manager assigned.", time.Time{}
	}
	best := "not_seen"
	bestRank := 0
	var bestSeen time.Time
	here := 0
	for _, key := range keys {
		seenAt, seen := s.presence.seen(key)
		stateLabel := presenceStateSince(seenAt, seen, now, s.presence.startedAt)
		if stateLabel == "here" {
			here++
		}
		rank := map[string]int{"not_seen": 0, "away": 1, "idle": 2, "here": 3}[stateLabel]
		if rank > bestRank || (rank == bestRank && !seenAt.IsZero() && (bestSeen.IsZero() || seenAt.After(bestSeen))) {
			best, bestRank, bestSeen = stateLabel, rank, seenAt
		}
	}
	operatorWord := "operator"
	if len(keys) != 1 {
		operatorWord = "operators"
	}
	switch best {
	case "here":
		if len(keys) > 1 {
			return best, fmt.Sprintf("At the room now · %d of %d %s here.", here, len(keys), operatorWord), bestSeen
		}
		return best, "At the room now.", bestSeen
	case "idle":
		return best, fmt.Sprintf("Last seen %s ago.", presenceAgeLabel(now.Sub(bestSeen))), bestSeen
	case "away":
		return best, fmt.Sprintf("Last seen %s ago · full clock remains.", presenceAgeLabel(now.Sub(bestSeen))), bestSeen
	default:
		return best, "No room heartbeat since this server started.", bestSeen
	}
}

func presenceAgeLabel(age time.Duration) string {
	if age < 0 {
		age = 0
	}
	if age < time.Minute {
		return fmt.Sprintf("%ds", int(age/time.Second))
	}
	if age < time.Hour {
		return fmt.Sprintf("%dm", int(age/time.Minute))
	}
	return fmt.Sprintf("%dh", int(age/time.Hour))
}

// presenceDigest renders "team-1=here,team-2=not_seen,..." across every
// default team ID, in order (already sorted, since team-1..team-8 compare
// lexically). Buckets change only on a presenceState transition, so this
// string — and the fingerprint suffix it feeds — stays stable between
// transitions and changes exactly when a room's presence dot must change.
func (s *Service) presenceDigest(state PersistedState, now time.Time) string {
	order := defaultTeamIDs()
	parts := make([]string, 0, len(order))
	for _, teamID := range order {
		label, _, _ := s.teamPresence(state, teamID, now)
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

// invalidatePoolCache drops the cached pool so the next s.pool() call
// rebuilds it (buildPool), even though the underlying player source's own
// version has not moved. pool()'s cache key is (source version, label)
// only (adversarial review finding, 2026-08-30): it was never meant to
// also track the roster preset HouseRank is computed against, so a
// commissioner's roster-shape override left stale HouseRanks behind on a
// constant-version pool. This follows the same direct-clear discipline
// SetPlayerSource already uses above, rather than folding a roster
// fingerprint into the cache key: it is a rare, commissioner-only event,
// not a per-render cost, so paying one full rebuild on the next pool()
// call is simpler than widening the key everywhere it is compared.
// AdminSetRosterShape and AdminResetRosterShape were the first callers;
// AdminSetScoring and AdminResetScoring (admin.go) call it too, for the
// identical reason applied to Hist lines instead of HouseRank.
func (s *Service) invalidatePoolCache() {
	s.poolMu.Lock()
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

// StateFingerprint hashes the persisted league state plus the pool version,
// a bucketed presence digest, and a clock-boundary digest. General pages poll
// it, while the Draft Room's live hub observes the same digest and pushes a
// scoped refresh event when it changes. This keeps every open draft room
// current — including its presence dots and pick-clock deadline, both of which
// live in or feed this hash — without full reloads.
//
// The three non-state terms exist because each covers something
// json.Marshal(state) cannot see: presence lives in memory, avatars live on
// disk, and a deadline is crossed by the clock rather than by a write. See
// boundaryDigest for that last one.
func (s *Service) StateFingerprint(poolVersion int64) string {
	now := s.clock()
	state := s.store.Snapshot()
	encoded, err := json.Marshal(state)
	if err != nil {
		encoded = []byte(err.Error())
	}
	suffix := fmt.Sprintf("|pool:%d|presence:%s", poolVersion, s.presenceDigest(state, now))
	// Preseason Blitz live scores are never persisted in league state
	// (design spec section 4.4): a poll must not rewrite the state file and
	// churn every other fingerprint reader. Appending the source's own
	// version here is what lets a live-stat update reach browsers through
	// their shared fingerprint synchronization paths (F14).
	// blitzFn and liveVersionFn are read together under one poolMu hold
	// (round-2 review of commit a3bf24a, finding 4) rather than two
	// separate locks — s.liveVersion() would have taken poolMu a second
	// time right after this block released it.
	s.poolMu.Lock()
	blitzSource := s.blitzFn
	liveVersionSource := s.liveVersionFn
	s.poolMu.Unlock()
	var blitzGames []BlitzGame
	if blitzSource != nil {
		// Pulled once and handed to boundaryDigest below, so a single
		// fingerprint never copies the Blitz snapshot twice.
		snapshot := blitzSource()
		blitzGames = snapshot.Games
		suffix += fmt.Sprintf("|blitz:%d", snapshot.Version)
	}
	// Live box-score rows are never persisted either (A2); the poller
	// version is the only thing that tells a page a score moved.
	if liveVersionSource != nil {
		suffix += fmt.Sprintf("|live:%d", liveVersionSource())
	}
	// Every clock-driven boundary the UI renders: kickoffs, the draft
	// start, the trade deadline, and the Blitz slate locks. A version
	// counter cannot carry these — nothing fetches or writes at the
	// instant a deadline passes.
	suffix += fmt.Sprintf("|bounds:%s", s.boundaryDigest(now, blitzGames))
	// Avatar files live on disk, outside PersistedState, so a plain
	// json.Marshal(state) above never changes when one is uploaded or
	// reset; the digest is what lets an open page's poll notice (design
	// decision 5). See avatarDigest.
	suffix += fmt.Sprintf("|avatars:%s", s.avatarDigest())
	digest := sha256.Sum256(append(encoded, []byte(suffix)...))
	return hex.EncodeToString(digest[:8])
}

// PlayerPoolStatus is the typed source-of-truth seam shared by manager pool
// notices, the commissioner console, and health projections. Keeping source
// facts typed here prevents each route from independently deciding whether a
// saved snapshot is live, cached, stale, degraded, offline, or unavailable.
// main.go adapts internal/fantasy into this shape to avoid an import cycle.
type PlayerPoolStatus struct {
	Provider       string
	Mode           string
	State          string
	Players        int
	Target         int
	Positions      map[string]int
	WithADP        int
	WithProjection int
	WithBye        int
	// ProjectionWeek (GC-1 fix 1) is the NFL week the current pool's
	// projections were requested for; zero before the first sync.
	ProjectionWeek  int
	Requests        int
	LastSuccess     time.Time
	FreshnessWindow time.Duration
	LastError       string
}

type PoolStatusSource func() PlayerPoolStatus

// SetPoolStatus attaches the diagnostics source. Call it during startup.
func (s *Service) SetPoolStatus(source PoolStatusSource) {
	s.poolMu.Lock()
	s.poolStatusFn = source
	s.poolMu.Unlock()
}

func (s *Service) poolSourceStatus(pool playerPool) PlayerPoolStatus {
	s.poolMu.Lock()
	source := s.poolStatusFn
	s.poolMu.Unlock()
	if source == nil {
		return PlayerPoolStatus{
			Mode: pool.label, State: normalizePlayerPoolState("", pool.label, len(pool.players)),
			Players: len(pool.players),
		}
	}
	status := source()
	status.State = normalizePlayerPoolState(status.State, status.Mode, status.Players)
	return status
}

func normalizePlayerPoolState(state, mode string, players int) string {
	if players == 0 && state == "" {
		return "unavailable"
	}
	switch state {
	case "live", "cached", "stale", "degraded", "offline", "unavailable":
		return state
	}
	switch mode {
	case "live":
		return "live"
	case "cache", "cached":
		return "cached"
	case "stale":
		return "stale"
	case "degraded":
		return "degraded"
	case "offline", "demo":
		return "offline"
	default:
		if players > 0 {
			return "cached"
		}
		return "unavailable"
	}
}

func playerPoolStateLabel(state string) string {
	switch state {
	case "live":
		return "LIVE"
	case "cached":
		return "CACHED SNAPSHOT"
	case "stale":
		return "STALE SNAPSHOT"
	case "degraded":
		return "DEGRADED SNAPSHOT"
	case "offline":
		return "OFFLINE PLAYER LIST"
	default:
		return "PLAYER DATA UNAVAILABLE"
	}
}

// poolFreshnessMap is the single manager-facing state/recovery contract for
// Draft, Big Board, and Players. Cached and degraded snapshots retain their
// useful player rows and name what remains available; no non-live state is
// collapsed into the old, misleading "OFFLINE POOL" boolean.
func (s *Service) poolFreshnessMap(pool playerPool) map[string]any {
	status := s.poolSourceStatus(pool)
	state := status.State
	freshnessWindow := playerPoolDurationLabel(status.FreshnessWindow)
	detail := ""
	switch state {
	case "cached":
		detail = "A saved player-data snapshot is serving this page while the next refresh is pending. Rankings, Big Board work, and draft actions remain available."
	case "stale":
		detail = "The last successful player-data update is outside its declared freshness window. The saved snapshot remains available; ask the commissioner to check Data and integrations."
		if freshnessWindow != "" {
			detail = "The last successful player-data update is outside its " + freshnessWindow + " freshness window. The saved snapshot remains available; ask the commissioner to check Data and integrations."
		}
	case "degraded":
		detail = "The latest refresh reported a source problem. The last successful snapshot remains available, but some rankings, projections, news, or bye details may be older or unavailable."
	case "offline":
		detail = "This instance is using its built-in player list. Rankings are approximate; browsing and rehearsal remain available, but a real draft requires a synced pool."
	case "unavailable":
		detail = "No reliable player pool is available. Draft and acquisition actions are blocked; retry later or ask the commissioner to check Data and integrations."
	}
	hasLastSuccess := !status.LastSuccess.IsZero()
	lastSuccess := ""
	lastSuccessRelative := ""
	if hasLastSuccess {
		lastSuccess = status.LastSuccess.In(s.matchupLocation()).Format("Mon Jan 2 · 3:04 PM MST")
		lastSuccessRelative = relativeTime(s.clock(), status.LastSuccess)
	}
	return map[string]any{
		"state":                 state,
		"label":                 playerPoolStateLabel(state),
		"live":                  state == "live",
		"has_notice":            state != "live",
		"detail":                detail,
		"has_last_success":      hasLastSuccess,
		"last_success":          lastSuccess,
		"last_success_relative": lastSuccessRelative,
		"freshness_window":      freshnessWindow,
	}
}

func playerPoolDurationLabel(value time.Duration) string {
	if value <= 0 {
		return ""
	}
	if value%time.Hour == 0 {
		hours := int(value / time.Hour)
		if hours == 1 {
			return "1-hour"
		}
		return fmt.Sprintf("%d-hour", hours)
	}
	if value%time.Minute == 0 {
		minutes := int(value / time.Minute)
		if minutes == 1 {
			return "1-minute"
		}
		return fmt.Sprintf("%d-minute", minutes)
	}
	return value.String()
}

func (s *Service) poolStatusMap() map[string]any {
	pool := s.pool()
	status := s.poolSourceStatus(pool)
	lastSync := "Never successfully updated"
	if !status.LastSuccess.IsZero() {
		lastSync = status.LastSuccess.In(s.matchupLocation()).Format("Mon Jan 2 · 3:04 PM MST") + " · " + relativeTime(s.clock(), status.LastSuccess)
	}
	positions := make([]map[string]any, 0, len(status.Positions))
	for _, position := range []string{"QB", "RB", "WR", "TE", "K", "P", "DST"} {
		if count, ok := status.Positions[position]; ok {
			positions = append(positions, map[string]any{"pos": position, "count": count})
		}
	}
	rosterCapacity := s.TeamCount() * CurrentDraftRounds()
	cushion := max(0, status.Players-rosterCapacity)
	coverage := 0.0
	actualCoverage := 0.0
	if rosterCapacity > 0 {
		coverage = float64(status.Target) / float64(rosterCapacity)
		actualCoverage = float64(status.Players) / float64(rosterCapacity)
	}
	errorMessage := ""
	if status.LastError != "" {
		errorMessage = "The latest player-pool refresh reported a source problem. The saved snapshot remains available while the integration recovers."
	}
	// projectionWeek (GC-1 fix 1) labels which NFL week the current pool's
	// projections were requested for. A truthful "—" before the first sync
	// ever completes, never a fabricated "Week 1".
	projectionWeek := "—"
	if status.ProjectionWeek > 0 {
		projectionWeek = fmt.Sprintf("Week %d", status.ProjectionWeek)
	}
	return map[string]any{
		"state":           status.State,
		"mode":            playerPoolStateLabel(status.State),
		"players":         status.Players,
		"target":          status.Target,
		"roster_capacity": rosterCapacity,
		"cushion":         cushion,
		// coverage (wave-6 item 7k) is the TARGET ratio only (status.Target
		// / rosterCapacity) — /admin's "Pool coverage" stat printed this
		// bare, reading as a live measurement rather than the configured
		// target it actually is. actual_coverage is the same ratio against
		// the pool's real current player count, matching Commissioner HQ's
		// own "ACTUAL {x} · TARGET {y}" presentation (app/commissioner/
		// view.go's ratio helper, card.PoolActualCoverage/PoolTargetCoverage).
		// Item 3 (2026-09-02 audit) made this a fact rather than an
		// aspiration: commissioner_summary.go's own ActualCoverage used to
		// divide by the planning target instead of rosterCapacity, so
		// /commissioner and /admin showed two different "ACTUAL" numbers
		// for the same league. Both now divide by rosterCapacity here.
		"coverage":        fmt.Sprintf("%.1f×", coverage),
		"actual_coverage": fmt.Sprintf("%.1f×", actualCoverage),
		"with_adp":        status.WithADP,
		"with_proj":       status.WithProjection,
		"with_bye":        status.WithBye,
		"projection_week": projectionWeek,
		"requests":        status.Requests,
		"last_sync":       lastSync,
		"error":           errorMessage,
		"positions_list":  positions,
	}
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

	// Pool freeze (rules-audit item 1, HIGH). buildPool reruns whenever the
	// source reports a new version/label, and FANTASY_SYNC_INTERVAL fires
	// that resync on a plain wall-clock timer with no awareness of a live
	// draft: a 120s-clock, 136-pick draft runs roughly 4.5 hours, so
	// several ordinary resyncs land mid-draft. A rebuild mid-draft moves
	// the ADP cut a stale board's undrafted rows were read against,
	// reorders byHouse under autopick's live walk, and — the sharpest
	// edge — can drop a player the old source carried but the new one
	// does not, which used to orphan that player everywhere his ID was
	// already committed (a pick, a roster slot) with no warning. So: once
	// a draft has opened and has not yet finished, and this Service
	// already has a real pool to serve (byID != nil and not the
	// fail-closed unavailable state — an outage must still surface, not
	// get masked by a frozen stale cache), pool() keeps returning that
	// exact cache no matter what the source reports, and only looks at
	// the source's version/label again once the draft completes. The
	// rejected version/label are never queued; the next post-draft
	// pool() call reads the source fresh, same as any other rebuild.
	if s.poolCache.byID != nil && !s.poolCache.unavailable {
		state := s.store.Snapshot()
		if state.DraftStarted && !draftComplete(state) {
			if s.poolCache.version != version || s.poolCache.label != label {
				if !s.poolFrozenLoggedSet || s.poolFrozenLoggedVersion != version {
					log.Printf("player pool: frozen during the draft: ignoring source version %d (%s); keeping cached version %d (%s) until the draft completes", version, label, s.poolCache.version, s.poolCache.label)
					s.poolFrozenLoggedVersion = version
					s.poolFrozenLoggedSet = true
				}
			}
			return s.poolCache
		}
	}
	if len(players) == 0 {
		// DEMO_MODE is the only intentional embedded-pool path. Once a
		// production source has been attached, an empty result is an
		// unavailable source—not permission to silently resurrect the demo
		// player list and its IDs.
		if s.demoMode {
			players, version, label = s.players, 0, "demo"
		} else {
			s.poolCache = playerPool{
				version:     version,
				label:       "unavailable",
				byID:        make(map[string]Player),
				unavailable: true,
			}
			return s.poolCache
		}
	}
	// A source may report an explicit unavailable state even while a stale
	// conversion still has rows in memory. In production, do not let those
	// rows authorize roster actions; the commissioner-facing source state is
	// authoritative for whether the pool may be used.
	if !s.demoMode && normalizePlayerPoolState("", label, len(players)) == "unavailable" {
		s.poolCache = playerPool{
			version:     version,
			label:       "unavailable",
			byID:        make(map[string]Player),
			unavailable: true,
		}
		return s.poolCache
	}
	if s.poolCache.byID == nil || s.poolCache.version != version || s.poolCache.label != label {
		s.poolCache = s.buildPool(players, version, label)
	}
	return s.poolCache
}

// ResolveLivePlayer maps one Tank01 box-score row onto a pool player: the
// Tank01 ID first, then the unique normalized name. The ID path also
// reaches the embedded fixture players buildPool merges into pool.byID
// (see buildPool), so a demo or offline pool's synthetic ID still
// resolves; the name path indexes pool.players only — the sourced players,
// not the embedded fixtures — by design, since a live report never names a
// fixture player. An ambiguous or unknown name does not resolve, so a live
// row never lands on the wrong player. The name index is rebuilt only when
// the pool version moves, and the rebuild check and the rebuild itself
// happen under one poolMu hold against the freshest s.poolCache, not the
// pool snapshot pool() returned earlier: releasing and re-acquiring poolMu
// between them would let a concurrent pool refresh race the rebuild and
// leave the index built from a stale snapshot.
func (s *Service) ResolveLivePlayer(tank01ID, longName string) (Player, bool) {
	pool := s.pool()
	if player, ok := pool.byID[strings.TrimSpace(tank01ID)]; ok {
		return player, true
	}
	key := strings.TrimSuffix(normalizePlayerKey(longName, ""), "|")
	if key == "" {
		return Player{}, false
	}
	s.poolMu.Lock()
	current := s.poolCache
	if s.liveNameIndex == nil || s.liveNameVersion != current.version {
		index := make(map[string][]Player, len(current.players))
		for _, player := range current.players {
			name := strings.TrimSuffix(normalizePlayerKey(player.Name, ""), "|")
			index[name] = append(index[name], player)
		}
		s.liveNameIndex, s.liveNameVersion = index, current.version
	}
	matches := s.liveNameIndex[key]
	s.poolMu.Unlock()
	if len(matches) != 1 {
		return Player{}, false
	}
	return matches[0], true
}

// playerPoolIsUnavailable identifies the fail-closed source state shared by
// manager-facing pool pages and roster/waiver mutation authority. A source
// status of unavailable wins even if a stale adapter accidentally returns
// rows; only an explicit DEMO_MODE source may use the embedded fixtures.
func playerPoolIsUnavailable(pool playerPool) bool {
	return pool.unavailable || normalizePlayerPoolState("", pool.label, len(pool.players)) == "unavailable"
}

// HistoricalSource supplies one player's legible previous-season line by
// name and position, scored against values — the league's live scoring
// values buildPool already resolved once for this rebuild (see
// currentScoringValues), never re-resolved by the source itself. main.go
// adapts the nflverse season summary mirror to this shape, which keeps
// that dependency out of internal/league.
type HistoricalSource func(name, position string, values map[string]float64) (line string, ok bool)

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
//
// scoringValues is resolved here exactly once, then passed to every
// withHistorical/historicalFn call below — currentScoringValues snapshots
// the whole store and clones it (breakdown.go's own doc comment: "up to
// 400 players render per page, and 400 store snapshots per render is
// wasteful"), so calling it once per player here instead of once per
// rebuild would pay that cost hundreds of times over (adversarial review
// finding, 2026-08-31).
func (s *Service) buildPool(players []Player, version int64, label string) playerPool {
	scoringValues := s.currentScoringValues()
	byID := make(map[string]Player, len(players)+len(s.players))
	for _, player := range s.players {
		byID[player.ID] = s.withHistorical(player, scoringValues)
	}
	annotated := make([]Player, len(players))
	for index, player := range players {
		annotated[index] = s.withHistorical(player, scoringValues)
	}
	// HouseRank (houserank.go) is computed once per pool version, here
	// alongside byADP — never per render. It reads the ACTIVE roster
	// preset and team count, not a fixed shape, so a commissioner's
	// roster-shape override or a team-count change is reflected on the
	// next pool version, the same way everything else CurrentRoster()
	// backs already is.
	annotated = applyHouseRanks(annotated, CurrentRoster(), s.TeamCount())
	for _, player := range annotated {
		byID[player.ID] = player
	}
	// Carry forward every drafted or rostered player ID the new source
	// dropped (rules-audit item 1's second half — independent of the
	// pool() freeze above, and not limited to a draft in progress: a
	// live source is free to shrink or reorder its list on any resync,
	// but an ID this league has already committed to a pick or a roster
	// spot must stay resolvable forever). Without this, rosterForTeam's
	// `continue` (service.go) on a missing ID silently shrinks that
	// team's roster below its real pick count, and
	// teamDraftedPlayers/positionScarcityBlocksCandidate undercount the
	// same team's needs. byID only — annotated/byADP/byHouse (the board
	// and autopick walk order) intentionally do not get these players
	// back: a carried-forward player is already spoken for, never an
	// "available" candidate again.
	s.carryForwardReferencedPlayers(byID)
	byADP := make([]Player, len(annotated))
	copy(byADP, annotated)
	sort.SliceStable(byADP, func(i, j int) bool {
		left, right := byADP[i].ADP, byADP[j].ADP
		if left <= 0 {
			left = math.MaxFloat64
		}
		if right <= 0 {
			right = math.MaxFloat64
		}
		return left < right
	})
	byHouse := houseOrderedIndex(annotated)
	return playerPool{version: version, label: label, players: annotated, byID: byID, byADP: byADP, byHouse: byHouse}
}

// carryForwardReferencedPlayers fills byID with the last-known fields for
// every draft-pick and current-roster player ID that this rebuild's own
// source list and embedded fixtures did not already resolve. It reads
// s.poolCache — the pool this rebuild is about to replace — as the
// "last-known" source, which is always still the previous cache here:
// buildPool runs from inside pool() before pool() overwrites poolCache
// with this call's own result (see pool()), and every other buildPool
// caller (tests aside) goes through pool() the same way. A first-ever
// build (s.poolCache.byID nil) carries nothing forward, which is correct:
// nothing has been drafted onto a roster before a pool has ever existed.
func (s *Service) carryForwardReferencedPlayers(byID map[string]Player) {
	state := s.store.Snapshot()
	carry := func(playerID string) {
		if playerID == "" {
			return
		}
		if _, ok := byID[playerID]; ok {
			return
		}
		if player, ok := s.poolCache.byID[playerID]; ok {
			byID[playerID] = player
		}
	}
	for _, pick := range state.Picks {
		carry(pick.PlayerID)
	}
	for _, ids := range currentRosters(state) {
		for _, id := range ids {
			carry(id)
		}
	}
}

// withHistorical fills a player's previous-season line from the attached
// historical source, unless the player already carries one. Callers hold
// poolMu already (see buildPool). scoringValues is buildPool's once-per-
// rebuild resolved values map (see buildPool's own doc comment); it is
// passed straight through to historicalFn, never re-resolved here.
//
// Punter fallback (roster-ops spec section 4.1.2 / WP-R0): nflverse's
// season-summary mirror — the attached historicalFn source — carries no
// punting columns, so a Position "P" player never matches there. When the
// primary source is absent or misses and the player is a punter, this
// falls back to the embedded 2025 punter index (punters_hist.go), matching
// by team and last name. A mismatch attaches nothing (fail quiet).
func (s *Service) withHistorical(player Player, scoringValues map[string]float64) Player {
	if player.Hist != "" {
		return player
	}
	if s.historicalFn != nil {
		if line, ok := s.historicalFn(player.Name, player.Position, scoringValues); ok && line != "" {
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
// record exists for this signed-in email" must resolve the persisted member
// directly; a read or action authorization check must never admit an
// identity as a side effect.
func (s *Service) assignMember(email, name string) (Member, error) {
	email = s.identityResolver.Resolve(email)
	topologyMutationMu.Lock()
	defer topologyMutationMu.Unlock()
	s.topologyMutationCheckpoint("assign-member-before-store")
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

// ensureMember is the membership-only counterpart to assignMember. It records
// an email only at a deliberate admission boundary, never from a read model
// or an action authorization check, never assigns a team seat, and never
// fires N1.
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
	// intervention is true only when the commissioner is acting on a seat
	// that is not their own — canManageCoManager also admits the seat's
	// own primary manager, and that ordinary self-service path (which may
	// coincidentally be exercised by a person who also holds the
	// commissioner role) must never turn into a commissioner-console audit
	// row (mirrors the lineup-intervention carve-out, lineup_intervention.go).
	intervention := s.IsCommissioner(r) && !s.isPrimaryOfTeam(r, teamID)
	if err := s.store.DetachCoManager(teamID); err != nil {
		return err
	}
	if intervention {
		summary := fmt.Sprintf("detached the co-manager for %s", s.TeamLabel(teamID))
		if _, err := s.RecordCommissionerEvent(r, "seat.co_detach", summary, CommissionerEventRefs{TeamID: teamID}); err != nil {
			log.Printf("commissioner event: seat.co_detach: %v", err)
		}
	}
	return nil
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
		"public_entry":       publicEntryData(s.publicEntryViewForViewerState(r, viewer, state)),
		"league":             s.leagueMapForViewer(r),
		"league_mode":        s.cfg.ModeLabel,
	}
}

// ClaimFantasySeat resolves the signed-in member's email/name from r and
// completes the fantasy-signup atomic claim (build item 2: team name +
// badge motif in one seat claim). The primitive owns every admission and
// existing-seat check so direct/stale action calls cannot bypass them.
func (s *Service) ClaimFantasySeat(r *http.Request, teamName, motif string) (Team, error) {
	user, ok := s.CurrentUser(r)
	if !ok {
		return Team{}, fmt.Errorf("Google sign-in is required for league actions")
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
	email = s.identityResolver.Resolve(email)
	state := s.store.Snapshot()
	if pendingTeamID, pending := state.CoInvites[email]; pending {
		return Team{}, fmt.Errorf(
			"complete your pending co-manager invitation for %s before claiming another franchise",
			s.TeamLabel(pendingTeamID),
		)
	}
	member, admitted := state.Members[email]
	if !admitted {
		return Team{}, errors.New("league admission is not recorded for this Google account; ask the commissioner to verify this identity before claiming a franchise")
	}
	if member.TeamID != "" {
		return Team{}, errors.New("you already hold a team seat")
	}
	if !s.store.IdentityHealthy() {
		return Team{}, errors.New(identityUnavailableCopy)
	}
	displayName, err := validateTeamName(teamName)
	if err != nil {
		return Team{}, claimValidationError(ClaimFieldTeamName, err)
	}
	if displayName == "" {
		return Team{}, claimValidationError(ClaimFieldTeamName, errors.New("enter a team name"))
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
		return Team{}, claimValidationError(ClaimFieldMotif, errors.New("choose a badge for your team"))
	}
	if !knownMotif(motif) {
		return Team{}, claimValidationError(ClaimFieldMotif, ErrBadgeUnknownMotif)
	}
	for holderTeamID, claimedMotif := range s.store.BadgeClaims() {
		if claimedMotif == motif {
			return Team{}, claimValidationError(ClaimFieldMotif, &badgeTakenError{teamName: s.teamByID(holderTeamID).Name})
		}
	}
	member, err = s.assignMember(email, name)
	if err != nil {
		return Team{}, err
	}
	teamID := member.TeamID
	if err := s.store.SetTeamName(teamID, displayName); err != nil {
		_ = s.store.ReleaseSeat(teamID)
		return Team{}, claimValidationError(ClaimFieldTeamName, err)
	}
	if err := s.store.ClaimBadge(teamID, motif); err != nil {
		_ = s.store.SetTeamName(teamID, "")
		_ = s.store.ReleaseSeat(teamID)
		var claimed *badgeClaimedError
		if errors.As(err, &claimed) {
			return Team{}, claimValidationError(ClaimFieldMotif, &badgeTakenError{teamName: s.teamByID(claimed.teamID).Name})
		}
		return Team{}, claimValidationError(ClaimFieldMotif, err)
	}
	return s.teamByID(teamID), nil
}

func (s *Service) Viewer(r *http.Request) map[string]any {
	user, signedIn := s.CurrentUser(r)
	if !signedIn {
		team := s.Teams()[0]
		return map[string]any{
			"signed_in":           false,
			"demo":                s.demoMode,
			"name":                "Guest Coach",
			"email":               "",
			"initials":            "GC",
			"team_id":             team.ID,
			"team_name":           team.Name,
			"has_seat":            s.demoMode,
			"seat_claim_eligible": false,
			"is_commissioner":     s.demoMode,
		}
	}
	member, memberExists := s.store.MemberByEmail(user.Email)
	_, pendingCoInvite := s.store.Snapshot().CoInvites[user.Email]
	hasSeat := member.TeamID != ""
	seatClaimEligible := memberExists && !hasSeat && !pendingCoInvite
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
		"signed_in":           true,
		"demo":                false,
		"name":                name,
		"email":               user.Email,
		"initials":            initials(name),
		"team_id":             teamID,
		"team_name":           teamName,
		"has_seat":            hasSeat,
		"seat_claim_eligible": seatClaimEligible,
		"is_commissioner":     s.IsCommissioner(r),
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
	games := s.schedule()
	_ = s.store.ReconcilePickemMarkets(now, games, nil)
	_ = s.store.BackfillPickemEnteredAt(games)
	state := s.store.Snapshot()
	viewer := s.Viewer(r)
	hasSeat, _ := viewer["has_seat"].(bool)
	featured := s.matchupMaps(state, live.Matchups[:min(2, len(live.Matchups))])
	announcements := s.announcementListMaps(5)
	transactions := s.activityMaps(state, 5)
	standings := s.dashboardStandingState(state)
	standingsTitle, standingsNote, standingsEmptyTitle := s.dashboardStandingsCopy(state, standings)
	pickemHome := s.pickemHomeSummaryFromSnapshot(r, state, now)
	livePoll := live.State != MatchupStateFinal && live.State != MatchupStatePreseason
	return map[string]any{
		"viewer":                viewer,
		"public_entry":          s.publicEntryDataForViewerState(r, viewer, state),
		"playoff_truth":         s.playoffTruthMap(state, now, s.IsCommissioner(r)),
		"has_seat":              hasSeat,
		"draft":                 s.draftSummary(now),
		"live":                  s.liveMap(live),
		"live_interval":         map[bool]string{true: "1m", false: ""}[livePoll],
		"live_poll":             livePoll,
		"featured":              featured,
		"standings":             s.standingsMaps(state),
		"divisions":             s.divisionMaps(state),
		"standings_available":   standings.HasResults,
		"standings_title":       standingsTitle,
		"standings_note":        standingsNote,
		"standings_empty_title": standingsEmptyTitle,
		"transactions":          transactions,
		"pickem_home":           pickemHome,
		"action_center":         s.actionCenterDataForSnapshot(r, state, viewer, pickemHome, now),
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
		"season_start_week":   s.seasonStartWeek(),
		"league":              s.leagueMapForViewer(r),
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
	publicEntry := s.publicEntryViewForViewerState(r, viewer, state)
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
	livePoll := isCurrentWeek && live.State != MatchupStateFinal && live.State != MatchupStatePreseason
	matchups := s.matchupMaps(state, live.Matchups)
	teamID, _ := viewer["team_id"].(string)
	// lockWeek is the lineup-lock authority (item 7, 2026-08-31 post-wave
	// audit): the same lineupCurrentWeekAt concept /team's own week
	// selector (teamWeekOptions, lineup_deadline.go) already uses to
	// decide which weeks are still editable. featuredMatchupMap uses it,
	// together with the VIEWED week (selectedWeek), to target its own
	// "Set lineup for Week N" CTA at whichever week is actually
	// editable — see that function's own doc comment.
	lockWeek := lineupCurrentWeekAt(s.schedule(), s.clock())
	myMatchup, otherMatchups := s.featuredMatchupViews(state, live, matchups, teamID, selectedWeek, lockWeek)
	return map[string]any{
		"viewer":             viewer,
		"live":               s.liveMapForWeek(live, isCurrentWeek),
		"playoff_truth":      s.playoffTruthMap(state, s.clock(), s.IsCommissioner(r)),
		"matchups":           matchups,
		"matchups_empty":     len(matchups) == 0,
		"my_matchup":         myMatchup,
		"other_matchups":     otherMatchups,
		"other_count_label":  otherMatchupsCountLabel(len(otherMatchups)),
		"status_line":        s.matchupStatusLine(live),
		"leaders":            s.leaderMaps(),
		"league":             s.leagueMapForViewer(r),
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
		"live_interval":      map[bool]string{true: "1m", false: ""}[livePoll],
		"live_poll":          livePoll,
		"next_matchup":       s.nextManagerMatchup(state, viewer, state.Schedule, currentWeek, publicEntry),
		"public_entry":       publicEntryData(publicEntry),
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

func (s *Service) nextManagerMatchup(state PersistedState, viewer map[string]any, schedule *SeasonSchedule, currentWeek int, publicEntry PublicEntryView) map[string]any {
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
		if publicEntry.SignedIn && !publicEntry.CanClaim {
			out["message"] = publicEntry.Detail
		}
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
		if publicEntry.SignedIn && !publicEntry.CanClaim {
			out["message"] = publicEntry.Detail
		}
		return out
	}
	weeks := seasonScheduleWeeks(*schedule)
	for _, weekNumber := range weeks {
		if weekNumber < currentWeek {
			continue
		}
		week, ok := scheduleWeekByNumber(*schedule, weekNumber)
		if !ok {
			continue
		}
		if scheduleWeekIsFinal(week) {
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
// (pickemWeek); ?week=N previews a later published week's
// carry-forward/auto-fill resolution. Invalid, closed, and unpublished
// weeks normalize back to the same current week used for lock enforcement.
func (s *Service) TeamData(r *http.Request) map[string]any {
	return s.teamData(r, false)
}

// TeamDataReadOnly assembles the same Team view without the membership
// provisioning path in Viewer. Fragment polling is observation only: an
// authenticated but unseated request must not create a Member or otherwise
// mutate league state merely by asking for current lineup HTML.
func (s *Service) TeamDataReadOnly(r *http.Request) map[string]any {
	return s.teamData(r, true)
}

func (s *Service) teamData(r *http.Request, readOnly bool) map[string]any {
	var viewer map[string]any
	var state PersistedState
	if readOnly {
		state = s.store.Snapshot()
		viewer = s.viewerReadOnly(r, state)
	} else {
		viewer = s.Viewer(r)
		state = s.store.Snapshot()
	}
	teamID, _ := viewer["team_id"].(string)
	lineupTarget := s.lineupViewTargetForRequest(r, state, teamID)
	identityAvailable, identityError := s.identityView()
	// A seatless member (no team_id) gets the honest "no franchise" state
	// with the signup CTA, never team-1's roster (registration wave, build
	// item 6 — the flagged paper cut: teamView's empty-TeamID fallback to
	// defaultTeams()[0] used to leak team-1's own lineup to every seatless
	// visitor of /team).
	if hasSeat, _ := viewer["has_seat"].(bool); !hasSeat && !lineupTarget.Intervention {
		return map[string]any{
			"viewer":               viewer,
			"public_entry":         publicEntryData(s.publicEntryViewForViewerState(r, viewer, state)),
			"playoff_truth":        s.playoffTruthMap(state, s.clock(), s.IsCommissioner(r)),
			"has_seat":             false,
			"predraft_visible":     false,
			"predraft_has_board":   false,
			"predraft_board_count": 0,
			"predraft_ready":       false,
			"league":               s.leagueMapForViewer(r),
			"league_mode":          s.cfg.ModeLabel,
			"fantasy_card":         s.fantasyCardData(state, viewer),
			"identity_available":   identityAvailable,
			"identity_error":       identityError,
			"badge_grid":           []map[string]any{},
		}
	}
	teamID = lineupTarget.TeamID
	team := s.teamView(state, teamID)
	roster, _ := s.rosterForTeam(state, teamID)
	rosterCapacity := CurrentRoster().Total()
	lifecycle := resolveTeamTerminalLifecycle(state, len(roster), rosterCapacity)
	terminalData := lifecycle.copy()
	now := s.clock()
	radar := s.teamTerminalRadar(state, lifecycle.Phase, now, 3)
	radarCopy := teamTerminalRadarCopy(lifecycle.Phase)
	projected := 0.0
	for _, player := range roster {
		projected += player.Projection
	}
	teamMap := s.teamMap(team)
	// has_custom_name (wave-6 glue item 5) gates the /team page's own
	// "Reset to configured name" control (page.gsx): the control has
	// nothing useful to do, and nothing to reset, for a team still
	// carrying its configured default name. Computed only here (not
	// inside teamMap itself, which every many-teams-per-page caller —
	// matchups, draft, standings, admin — also shares, none of which
	// render the Reset control) from state.TeamNames, the same override
	// map ResetTeamName (avatar.go) clears.
	teamMap["has_custom_name"] = strings.TrimSpace(state.TeamNames[teamID]) != ""
	// This page's own .team-monogram hero (app/team/page.gsx) is the one
	// render site that needs BadgeOutputSizeLarge instead of teamMap's own
	// BadgeOutputSize avatar_image_url — see avatarViewLarge's doc comment.
	// Computed only here (not inside teamMap itself, which every
	// many-teams-per-page caller — matchups, draft, standings, admin —
	// also shares) so those pages never pay to render a size they do not
	// use.
	_, _, avatarImageLargeURL := s.avatarViewLarge(team.ID, team.Tone)
	teamMap["avatar_image_large_url"] = avatarImageLargeURL
	// Demo mode grants one synthetic, fully authorized seat without writing
	// a production Member. Present that authority truthfully on the Team
	// terminal instead of calling the same viewer both the manager and an
	// unclaimed guest on adjacent surfaces.
	if s.demoMode {
		teamMap["manager"] = "REHEARSAL COMMISSIONER"
		teamMap["claimed"] = true
	}
	boardCount := len(state.Boards[boardKeyForViewer(state, s.viewerKey(r))])
	managerReady := state.Ready[teamID]
	badgeToneHex, _ := BadgeToneHex(team.Tone)
	hasBadgeClaim := false
	badgeGrid := []map[string]any{}
	if identityAvailable {
		_, hasBadgeClaim = s.store.BadgeClaim(teamID)
		badgeGrid = s.badgeGrid(state, teamID)
	}
	coManager := s.coManagerMap(r, state, teamID)
	if lineupTarget.Intervention {
		// Intervention is a lineup-only projection. Keep the selected
		// franchise label for context, but do not carry its manager,
		// co-manager, badge, or identity-editor state into the view model.
		teamMap["manager"] = ""
		coManager = map[string]any{
			"primary_name": "", "has_co": false, "co_name": "",
			"has_pending": false, "pending_email": "",
			"can_invite": false, "can_detach": false,
		}
		identityAvailable = false
		identityError = ""
		hasBadgeClaim = false
		badgeGrid = []map[string]any{}
	}

	games := s.schedule()
	// teamWeekOptions (item 6, 2026-08-31 post-wave audit) replaces a bare
	// normalizeLineupWeek + sortedFutureLineupWeeks pair here: those two
	// draw their offered/valid week set from games alone (the raw NFL
	// schedule mirror, up to 18 weeks), never from this league's own
	// published season length (state.Schedule). See teamWeekOptions' own
	// doc comment (lineup_deadline.go) for the full "1-18 on a 14-week
	// league" bug this closes.
	weekSelection := teamWeekOptions(r.URL.Query().Get("week"), state.Schedule, games, now)
	week := weekSelection.Week
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
	weekOptions := make([]map[string]any, 0, len(weekSelection.Weeks))
	for _, w := range weekSelection.Weeks {
		weekOptions = append(weekOptions, map[string]any{
			"value":    strconv.Itoa(w),
			"label":    fmt.Sprintf("WEEK %d", w),
			"selected": w == week,
		})
	}
	if len(weekOptions) == 0 {
		weekOptions = append(weekOptions, map[string]any{
			"value": strconv.Itoa(week), "label": fmt.Sprintf("WEEK %d", week), "selected": true,
		})
	}
	lineupDeadline := lineupDeadlineFor(lineup, general, games, week, now, s.matchupLocation())
	lineupDeadlineMap := map[string]any{
		"state":          string(lineupDeadline.State),
		"week":           strconv.Itoa(lineupDeadline.Week),
		"has_deadline":   lineupDeadline.HasDeadline,
		"exact":          lineupDeadline.Exact,
		"relative":       lineupDeadline.Relative,
		"timezone":       FriendlyTimezoneLabel(lineupDeadline.Timezone),
		"headline":       lineupDeadline.Headline,
		"detail":         lineupDeadline.Detail,
		"editable_slots": strconv.Itoa(lineupDeadline.EditableSlots),
		"locked_slots":   strconv.Itoa(lineupDeadline.LockedSlots),
		"total_slots":    strconv.Itoa(lineupDeadline.TotalSlots),
		"has_editable":   lineupDeadline.EditableSlots > 0,
		"is_upcoming":    lineupDeadline.State == LineupDeadlineUpcoming,
		"is_no_schedule": lineupDeadline.State == LineupDeadlineNoSchedule,
		"is_no_upcoming": lineupDeadline.State == LineupDeadlineNoUpcoming,
		"is_all_locked":  lineupDeadline.State == LineupDeadlineAllLocked,
		"is_degraded":    lineupDeadline.State == LineupDeadlineDegraded,
	}

	placeOptions := make([]map[string]any, 0, len(general))
	for _, p := range general {
		if playerLockedForRosterMutation(state, games, weekSelection.CurrentWeek, p, now) {
			continue
		}
		placeOptions = append(placeOptions, map[string]any{
			"id": p.ID, "label": fmt.Sprintf("%s (%s)", p.Name, p.Position),
		})
	}

	// starterRows/benchRows are built once here (rather than inline in the
	// data literal below) so wave 7's row decorators — group headers
	// (item 1) and the unconditional kickoff/bye second line (item 4) —
	// have a live map to write onto before the page ever sees it. Each
	// decorator only ever ADDS keys; none removes or replaces one
	// starterRowMaps/playerMapsWithScoring already set. drafted (item 5)
	// is draft_history.go's own league-wide playerID->DraftPick lookup
	// (hazel's helper, shared with /players' owner chip) threaded straight
	// into playerMap's variadic drafted param — is_drafted/drafted_round/
	// drafted_pick/drafted_label are playerMap's own fields, not a
	// second, locally reinvented lookup.
	drafted := draftedByPlayerID(state)
	starterRows := s.starterRowMaps(lineup, general, games, now, scoringValues, drafted)
	benchRows := playerMapsWithScoring(lineup.Bench, scoringValues, s.matchupIndexFor(games, week), drafted)
	addBenchGroupHeaders(benchRows)
	addScheduleLabels(benchRows, lineup.Bench, games, week, s.matchupLocation())
	draftClass := s.draftClassTeaser(state, teamID, 3)

	data := map[string]any{
		"viewer":                        viewer,
		"playoff_truth":                 s.playoffTruthMap(state, now, s.IsCommissioner(r)),
		"lineup_target_options":         s.lineupTargetOptions(state, lineupTarget.TeamID, week),
		"lineup_intervention_exit_href": "/team?week=" + strconv.Itoa(week) + "#lineup",
		"public_entry":                  publicEntryData(s.publicEntryViewForViewerState(r, viewer, state)),
		"has_seat":                      true,
		"lineup_intervention":           lineupTarget.Intervention,
		"lineup_target_id":              lineupTarget.TeamID,
		"team":                          teamMap,
		// has_team_streak (wave-8 audit item 6) guards the hero record
		// line's own "· {streak}" segment: team.Streak reads the em-dash
		// placeholder "—" (defaultTeams', computeStreak's own "no results
		// yet" case) before any week has closed, and printing "· —" right
		// after the points-scored figure read as a dangling separator with
		// nothing behind it, not the honest "no streak yet" the plain dash
		// means everywhere else in this app. Computed here, not inside the
		// shared teamMap (every other teamMap caller — standings, matchup
		// cards — has no equivalent "· {streak}" segment to guard).
		"has_team_streak": strings.TrimSpace(team.Streak) != "" && team.Streak != "—",
		// drafted is retained as a compatibility alias for the old template contract; lifecycle truth lives in team_terminal_phase and its explicit booleans below.
		"drafted":              lifecycle.DraftComplete,
		"predraft_visible":     !lineupTarget.Intervention && !state.DraftStarted && (strings.TrimSpace(team.Manager) != "" || s.demoMode),
		"predraft_has_board":   boardCount > 0,
		"predraft_board_count": boardCount,
		"predraft_ready":       managerReady,
		"projected":            fmt.Sprintf("%.1f", projected),
		"division":             teamMap["division"],
		"scouting":             radar,
		"scouting_empty":       len(radar) == 0,
		"is_commissioner":      s.IsCommissioner(r),
		"league_mode":          s.cfg.ModeLabel,
		"league":               s.leagueMapForViewer(r),
		"badge_tone_hex":       badgeToneHex,
		"has_badge_claim":      hasBadgeClaim,
		"badge_grid":           badgeGrid,
		"identity_available":   identityAvailable,
		"identity_error":       identityError,
		"roster_shape":         rosterShapeRows(),
		"shape_summary":        rosterShapeSummary(len(general) + len(reserveOccupants)),
		// positional_depth (wave 7 item 2) is the general roster's own
		// position counts — "2 QB · 4 RB · 5 WR · 2 TE · 1 K · 1 DST" — in
		// the same QB/RB/WR/TE/K/DST order the bench grouping
		// (addBenchGroupHeaders/benchPositionOrder, lineup.go) uses, so a
		// manager can see at a glance whether an empty FLEX is a real
		// shortage or just an unset lineup. general (not roster) is
		// deliberate: reserve/IR occupants sit outside the countable
		// lineup pool (roster-ops SK spec), the same slice effectiveLineup
		// itself draws starters and bench from.
		"positional_depth":       positionalDepthSummary(general),
		"positional_depth_chips": positionalDepthChips(general),
		"week":                   strconv.Itoa(week),
		"week_options":           weekOptions,
		"week_notice":            weekSelection.Notice,
		"has_week_notice":        weekSelection.Notice != "",
		"lineup_deadline":        lineupDeadlineMap,
		"starters":               starterRows,
		"starters_filled":        strconv.Itoa(filled),
		"starters_total":         strconv.Itoa(len(lineup.Slots)),
		// starters_empty/starters_empty_label back /team's persistent,
		// beside-the-count warning (gap-audit finding: SET BEST LINEUP used
		// to report plain success while a starting slot, e.g. K with no
		// kicker rostered, stayed empty). This is state, read fresh on
		// every render, so it stays honest independent of whether the
		// empty slot came from SET BEST LINEUP, a drop, or a lock.
		"starters_empty":       len(lineupEmptyStarterSlots(lineup)) > 0,
		"starters_empty_label": lineupEmptySlotsWarning(lineupEmptyStarterSlots(lineup)),
		"bench_capacity":       strconv.Itoa(preset.Bench),
		"bench":                benchRows,
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
		"co_manager": coManager,
		// fantasy_card is unused on the seated branch's own template path
		// (its <If> only reaches the seatless section above), but the key
		// stays present anyway — the same "every branch carries the same
		// key set" discipline fantasyCardData's own "team" field follows —
		// so a template read never depends on which branch built the map.
		"fantasy_card":         s.fantasyCardData(state, viewer),
		"matchup_source_label": matchupLabel,
		"has_matchup_source":   hasMatchupLabel,
		// draft_class_* (wave 7 item 6) back the post-draft "Your draft
		// class" callout: draft_class_href is the URL this wave agreed
		// with app/draft's own results page (hazel), and
		// draft_class_teaser carries the team's first three picks (by
		// pick order) as the teaser list. The callout itself is gated in
		// page.gsx on team_terminal_roster_complete (merged in from
		// terminalData just below) — draft_class_teaser_empty exists so
		// the template never needs a bare len() check of its own.
		"draft_class_href":         "/draft/results?team=" + url.QueryEscape(team.Abbreviation),
		"draft_class_teaser":       draftClass,
		"draft_class_teaser_empty": len(draftClass) == 0,
		// primary_action (wave 7 item 11) feeds the phone-only
		// PageActionBar (larch, layout-level): a thumb-zone shortcut to
		// this page's own primary verb. On /team that is SET BEST LINEUP
		// — the same gate (team_terminal_roster_complete) and the same
		// form (#lineup-auto-form, page.gsx) the toolbar's own inline
		// button already uses; an empty map when the form is not on the
		// page at all, matching an absent/no-op action rather than a
		// dangling form id.
		"primary_action": teamPrimaryAction(lifecycle.Phase == TeamTerminalRosterComplete),
	}
	for key, value := range terminalData {
		data[key] = value
	}
	for key, value := range radarCopy {
		data[key] = value
	}
	return data
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
	// pickByPlayer backed the old "Rd %d · Pick %d" Status branch below,
	// which never actually fired: every pool player already carries
	// Status: "Available" (players.go), so the "player.Status == \"\""
	// guard's dead-code condition was never true, and the draft round/pick
	// never reached a rendered row this way. Wave 7 item 5 renders the
	// same information honestly instead — draftedByPlayerID
	// (draft_history.go) resolves the identical state.Picks ledger into
	// playerMap's own is_drafted/drafted_round/drafted_pick/drafted_label
	// fields, threaded through both starter and bench rows in teamData,
	// rather than smuggling it through Status.
	ids := currentRosters(state)[teamID]
	roster := make([]Player, 0, len(ids))
	for _, id := range ids {
		player, ok := pool.byID[id]
		if !ok {
			continue
		}
		roster = append(roster, player)
	}
	return roster, len(roster) > 0
}

// positionalDepthSummary renders general's position counts (wave 7 item
// 2) as one compact line — "2 QB · 4 RB · 5 WR · 2 TE · 1 K · 1 DST" —
// in the same QB/RB/WR/TE/K/DST reading order the bench grouping
// (lineup.go's benchPositionOrder) uses, so a manager can see at a
// glance whether an empty FLEX slot is a real positional shortage or
// just an unset lineup. general is starters+bench only — the same
// reserve/IR-excluded slice effectiveLineup itself draws from (roster-
// ops SK spec: zone occupants sit outside the countable lineup pool).
// Any position outside the six named groups (a rostered P, or a shape
// this league's active preset never carries) still counts, appended in
// playerPoolPositions order after the six named groups, so a punter or
// an unusual position never silently drops off the end of the line.
func positionalDepthSummary(general []Player) string {
	return strings.Join(positionalDepthParts(general), " · ")
}

// positionalDepthChips renders the same counts positionalDepthSummary
// joins into one string, but as individual "label" entries instead —
// /team's own mobile layout (wave 7 item 8) renders each entry as its
// own wrapping chip rather than relying on a joined string to wrap
// mid-word. Both functions share positionalDepthParts so the two
// renderings can never drift out of sync with each other.
func positionalDepthChips(general []Player) []map[string]any {
	parts := positionalDepthParts(general)
	out := make([]map[string]any, 0, len(parts))
	for _, part := range parts {
		out = append(out, map[string]any{"label": part})
	}
	return out
}

// positionalDepthParts is positionalDepthSummary/positionalDepthChips'
// shared count-and-order pass: see positionalDepthSummary's own doc
// comment for the ordering rule (QB/RB/WR/TE/K/DST, then anything else)
// and why "general", not the full roster, is the right input.
func positionalDepthParts(general []Player) []string {
	counts := make(map[string]int, len(playerPoolPositions))
	for _, p := range general {
		counts[p.Position]++
	}
	parts := make([]string, 0, len(playerPoolPositions))
	for _, position := range benchPositionOrder {
		if n := counts[position]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, position))
		}
	}
	for _, position := range playerPoolPositions {
		if benchPositionRank(position) < len(benchPositionOrder) {
			continue
		}
		if n := counts[position]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, position))
		}
	}
	return parts
}

// addBenchGroupHeaders decorates bench's per-row view-model maps (already
// grouped into QB/RB/WR/TE/K/DST order by effectiveLineup's own bench
// sort — see lineup.go's benchPositionOrder) with a "group_header" label
// on the first row of each new position and a blank one on every other
// row (wave 7 item 1), so /team's flat <Each> bench list still reads as
// grouped even though GoSX has no native list-grouping construct.
// Reserve and IR occupants never pass through here — they render
// through zoneOccupantRows' own separate list — so this never touches
// those zones.
func addBenchGroupHeaders(rows []map[string]any) {
	last := ""
	for _, row := range rows {
		position, _ := row["position"].(string)
		hasHeader := position != last
		row["has_group_header"] = hasHeader
		row["group_header"] = ""
		if hasHeader {
			row["group_header"] = position
		}
		last = position
	}
}

// addScheduleLabels decorates rows (already-rendered playerMap output, in
// the same order as players) with the same unconditional kickoff_label/
// bye_label pair starterRowMaps emits for starters (wave 7 item 4):
// "SUN 4:25 PM" and "BYE N", present whether or not the player is
// anywhere near locked — lock only ever gates the WRITE side, never
// selection or render (see effectiveLineup's own doc comment on the P0
// this invariant fixed). rows and players must be the same length, in
// the same order; playerMapsWithScoring iterates its input in order and
// appends exactly one row per player, so this zip is always safe for
// its own output.
func addScheduleLabels(rows []map[string]any, players []Player, games []GameInfo, week int, location *time.Location) {
	for i, row := range rows {
		if i >= len(players) {
			return
		}
		player := players[i]
		kickoffLabel := ""
		if kickoff, ok := playerLockAt(games, week, player.NFLTeam); ok && !kickoff.IsZero() {
			kickoffLabel = strings.ToUpper(kickoff.In(location).Format("Mon 3:04 PM"))
		}
		row["kickoff_label"] = kickoffLabel
		row["has_kickoff_label"] = kickoffLabel != ""
		byeLabel := ""
		if player.ByeWeek > 0 {
			// "bye wk N" (wave-8 audit item 6), matching starterRowMaps'
			// own bye_label wording — see that function's doc comment.
			byeLabel = fmt.Sprintf("bye wk %d", player.ByeWeek)
		}
		row["bye_label"] = byeLabel
		row["has_bye_label"] = byeLabel != ""
	}
}

// draftClassTeaser renders teamID's first limit draft picks (by pick
// order — round then overall pick number) as /team's post-draft "Your
// draft class" callout teaser (wave 7 item 6): each entry names the
// player and its slot ("R3 · P28"). Returns an empty slice for a team
// that holds no picks yet; the caller (teamData) gates the whole
// callout section on team_terminal_roster_complete, so this never needs
// to render its own empty state.
func (s *Service) draftClassTeaser(state PersistedState, teamID string, limit int) []map[string]any {
	pool := s.pool()
	picks := make([]DraftPick, 0, len(state.Picks))
	for _, pick := range state.Picks {
		if pick.TeamID == teamID {
			picks = append(picks, pick)
		}
	}
	sort.Slice(picks, func(i, j int) bool {
		if picks[i].Round != picks[j].Round {
			return picks[i].Round < picks[j].Round
		}
		return picks[i].Number < picks[j].Number
	})
	if len(picks) > limit {
		picks = picks[:limit]
	}
	out := make([]map[string]any, 0, len(picks))
	for _, pick := range picks {
		player, ok := pool.byID[pick.PlayerID]
		if !ok {
			continue
		}
		out = append(out, map[string]any{
			"name":     player.Name,
			"position": player.Position,
			"label":    fmt.Sprintf("R%d · P%d", pick.Round, pick.Number),
		})
	}
	return out
}

// teamPrimaryAction renders /team's own primary_action entry (wave 7
// item 11) for the phone-only PageActionBar: a thumb-zone shortcut that
// submits the SET BEST LINEUP form (#lineup-auto-form, page.gsx) — the
// same team_terminal_roster_complete gate that form's own inline toolbar
// button already uses. rosterComplete false renders an empty map (no
// primary verb this page has to offer yet), never a dangling form id.
func teamPrimaryAction(rosterComplete bool) map[string]any {
	if !rosterComplete {
		return map[string]any{}
	}
	return map[string]any{
		"label": "Set best lineup",
		"href":  "",
		"kind":  "submit",
		"form":  "lineup-auto-form",
		"tone":  "primary",
	}
}

// DraftPoolSortADP and DraftPoolSortHouse name the draft room's two
// supported pool sorts (D9, spruce audit): market ADP order (pool.byADP)
// and house-rank order (pool.byHouse). ResolveDraftPoolSort never returns
// any other string.
const (
	DraftPoolSortADP   = "adp"
	DraftPoolSortHouse = "house"
)

// ResolveDraftPoolSort is the ONE place the draft room's active pool sort
// gets resolved: a request's own "?sort=house|adp" when present and valid,
// otherwise HOUSE for a superflex roster preset
// (CurrentRoster().Slots["SUPERFLEX"] > 0, the same superflex signal
// houserank.go's applyHouseRanks reads to decide whether a QB can fill a
// SUPERFLEX slot at all) and ADP otherwise. draftData (below) reads this to
// choose which of pool.byADP/pool.byHouse backs the available pane's row
// order; app/draft/page.server.go's own resolveDraftPoolSort delegates
// here too, so a page's rendered order and its displayed RK/sort-chip
// state can never disagree. r is nil-safe: a fixture test that resolves a
// sort without a live *http.Request still gets the roster-only default
// rather than a panic.
func ResolveDraftPoolSort(r *http.Request) string {
	active := DraftPoolSortADP
	if CurrentRoster().Slots["SUPERFLEX"] > 0 {
		active = DraftPoolSortHouse
	}
	if r == nil {
		return active
	}
	switch strings.ToLower(strings.TrimSpace(r.URL.Query().Get("sort"))) {
	case DraftPoolSortHouse:
		active = DraftPoolSortHouse
	case DraftPoolSortADP:
		active = DraftPoolSortADP
	}
	return active
}

// draftPoolPageHref extends poolPageHref (pagination.go) with the pool's
// active sort (D9 follow-up): sort must survive every pagination and
// position-chip link the draft room's own pool_*_href fields (below) hand
// out, or clicking one silently drops back to ResolveDraftPoolSort's own
// roster-only default on the next request. poolPageHref's other two
// callers, /players and /board, carry no sort concept at all, so this
// stays local to the draft room rather than growing poolPageHref's own
// shared signature.
func draftPoolPageHref(pos, query, activeSort string, page int) string {
	href := poolPageHref("/draft", pos, query, page)
	separator := "?"
	if strings.Contains(href, "?") {
		separator = "&"
	}
	return href + separator + "sort=" + url.QueryEscape(activeSort)
}

func (s *Service) DraftData(r *http.Request) map[string]any {
	return s.draftData(r, false, true)
}

// DraftDataReadOnly returns the same authoritative draft view without
// provisioning a signed-in member. Fragment polling is a read path: opening a
// draft tab must never create membership or rewrite league persistence.
func (s *Service) DraftDataReadOnly(r *http.Request) map[string]any {
	return s.draftData(r, true, true)
}

// DraftDataReadOnlyOptions is DraftDataReadOnly with control over whether
// the response builds DraftHistory (P1 perf fix, 2026-08-30 review):
// draftData runs on every one of app/draft's six fragment polls, but only
// the tape region ever renders history, so app/draft's other five fragment
// loaders (command, available, queue, room, workspace) pass
// includeHistory=false and skip a cost that otherwise scaled with the
// draft's pick count on every poll regardless of what the client asked for.
func (s *Service) DraftDataReadOnlyOptions(r *http.Request, includeHistory bool) map[string]any {
	return s.draftData(r, true, includeHistory)
}

func (s *Service) draftData(r *http.Request, readOnly bool, includeHistory bool) map[string]any {
	now := s.clock()
	var viewer map[string]any
	var state PersistedState
	if readOnly {
		state = s.store.Snapshot()
		viewer = s.viewerReadOnly(r, state)
	} else {
		viewer = s.Viewer(r)
		state = s.store.Snapshot()
	}
	publicEntry := s.publicEntryViewForViewerState(r, viewer, state)
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
	// activeSort/poolOrder (D9 follow-up): the available pane's row order
	// itself, not just its RK-cell display, now honors the ADP|HOUSE
	// toggle — pool.byADP (market order) or pool.byHouse (this league's
	// own superflex-aware VORP order, houserank.go) backs every filter,
	// search, and page consistently, and every fragment poll below
	// (DraftDataReadOnly/DraftDataReadOnlyOptions share this one method)
	// resolves the SAME request's own "?sort=" the same way.
	activeSort := ResolveDraftPoolSort(r)
	poolOrder := pool.byADP
	if activeSort == DraftPoolSortHouse {
		poolOrder = pool.byHouse
	}
	available := make([]Player, 0, len(poolOrder))
	for _, player := range poolOrder {
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
	complete := draftComplete(state)
	nextNumber := len(state.Picks) + 1
	displayPickNumber := nextNumber
	round := pickRound(activeTeamCount(state.DraftOrder), nextNumber)
	onClockID := ""
	onClockMap := blankTeamMap()
	if !complete {
		onClockID = teamOnClock(state.DraftOrder, nextNumber)
		onClockMap = s.teamMap(s.teamView(state, onClockID))
	} else {
		displayPickNumber = len(state.Picks)
		round = CurrentDraftRounds()
	}
	viewerTeam, _ := viewer["team_id"].(string)
	draftOpen := (state.DraftStarted || s.store.draftLifecycleBypass) && !complete
	canPick := draftOpen && viewerTeam == onClockID
	if s.demoMode && draftOpen {
		canPick = true
	}
	boardKey := boardKeyForViewer(state, s.viewerKey(r))
	boardOrder := state.Boards[boardKey]
	boardPanel := make([]map[string]any, 0, 5)
	// queuePanel is the shell's full personal-queue view (D2): every board
	// entry, in order, each carrying taken so a drafted target still shows
	// (struck through, client-side) rather than silently vanishing.
	// board_can_move_up/down mirror BoardData's own index math (board.go)
	// exactly, over the same boardOrder: the queue is the same underlying
	// board, so the no-JS up/down forms (DraftMyTeam, page.gsx) disable at
	// the same ends BoardRow's do. boardPanel stays the pre-existing 5-item
	// undrafted "peek" the legacy DraftWorkspace sidebar reads; both share
	// one playerMap build per id.
	queuePanel := make([]map[string]any, 0, len(boardOrder))
	for index, id := range boardOrder {
		player, ok := pool.byID[id]
		if !ok {
			continue
		}
		item := playerMap(player, scoringValues, matchup)
		item["taken"] = picked[id]
		item["board_can_move_up"] = index > 0
		item["board_can_move_down"] = index+1 < len(boardOrder)
		queuePanel = append(queuePanel, item)
		if !picked[id] && len(boardPanel) < 5 {
			boardPanel = append(boardPanel, item)
		}
	}
	// rosterNeeds tallies the viewer's own drafted players by exact
	// position against the active roster preset's starter slots (a simple
	// per-position count, not the FLEX-aware bipartite match
	// maximumDraftStarterFill uses for legality): display only, so a
	// player who could also cover a flex slot still counts under their
	// primary position here.
	rosterNeeds := make([]map[string]any, 0, 8)
	if viewerTeam != "" {
		preset := CurrentRoster()
		filledByPosition := map[string]int{}
		for _, pick := range state.Picks {
			if pick.TeamID != viewerTeam {
				continue
			}
			if player, ok := pool.byID[pick.PlayerID]; ok {
				filledByPosition[player.Position]++
			}
		}
		slotNames := make([]string, 0, len(preset.Slots))
		for name := range preset.Slots {
			slotNames = append(slotNames, name)
		}
		sort.Strings(slotNames)
		for _, name := range slotNames {
			required := preset.Slots[name]
			have := filledByPosition[name]
			if have > required {
				have = required
			}
			rosterNeeds = append(rosterNeeds, map[string]any{
				"label": name, "filled": have, "total": required, "open": have < required,
			})
		}
	}
	// nextQueued is the viewer's own top still-draftable board target — the
	// pick bar's "Draft" shortcut reads it so a phone never needs to
	// scroll to the available pane to take the queue's #1 pick.
	nextQueued := map[string]any{"has": false}
	if len(boardPanel) > 0 {
		top := boardPanel[0]
		nextQueued = map[string]any{
			"has": true, "id": top["id"], "name": top["name"],
			"position": top["position"], "nfl_team": top["nfl_team"],
		}
	}
	// hasADP gates the available pane's whole VS ADP column (header and
	// cell, R4): the built-in offline pool carries no real ADP figures, so
	// showing a column of empty/meaningless deltas would be worse than no
	// column at all. valueVsADP is "if drafted right now" — nextNumber
	// (the upcoming pick) minus the player's rounded ADP — the same sign
	// convention TapePick.ValueLabel uses for a made pick (draft_history.go).
	hasADP := pool.label != "offline"
	availableMaps := playerMapsWithScoring(pagedAvailable, scoringValues, matchup)
	for index, player := range pagedAvailable {
		availableMaps[index]["draft_eligible"] = !complete && onClockID != "" && draftCandidateKeepsRosterViable(state, pool.byID, onClockID, player.ID)
		hasValue := hasADP && player.ADP > 0
		availableMaps[index]["has_value"] = hasValue
		availableMaps[index]["value_label"] = ""
		if hasValue {
			availableMaps[index]["value_label"] = valueVsADPLabel(nextNumber, player.ADP)
		}
	}
	readyManagerCount, managerCount := s.draftSeatCounts(state)
	// The command bar's room summary and the shell's per-viewer turn math
	// both read the same team maps draftTeamMaps already builds for
	// "teams" below; computed once here and reused, not rebuilt.
	teamMaps := s.draftTeamMaps(state, onClockID)
	hereCount, autoCount := 0, 0
	for _, team := range teamMaps {
		if presence, _ := team["presence"].(string); presence == "here" {
			hereCount++
		}
		if auto, _ := team["autopick"].(bool); auto {
			autoCount++
		}
	}
	picksTotal := draftTeamCount() * CurrentDraftRounds()
	nextTeamMap := blankTeamMap()
	if nextNumber+1 <= picksTotal {
		nextTeamMap = s.teamMap(s.teamView(state, teamOnClock(state.DraftOrder, nextNumber+1)))
	}
	afterNextTeamMap := blankTeamMap()
	if nextNumber+2 <= picksTotal {
		afterNextTeamMap = s.teamMap(s.teamView(state, teamOnClock(state.DraftOrder, nextNumber+2)))
	}
	// yourPickIn counts picks until the viewer's own next turn: 0 when on
	// the clock right now, -1 when seatless or when no future turn remains
	// (mirrors yourPickBinds' convention in draft_events.go).
	yourPickIn := -1
	if viewerTeam != "" {
		for number := nextNumber; number <= picksTotal; number++ {
			if teamOnClock(state.DraftOrder, number) == viewerTeam {
				yourPickIn = number - nextNumber
				break
			}
		}
	}
	poolStatusMap := s.poolFreshnessMap(pool)
	// banner is the command bar's one-line status strip: paused takes
	// priority over rehearsal, which takes priority over an honest pool
	// freshness notice, so a manager never sees more than one competing
	// claim about why the room looks the way it does.
	banner := ""
	switch {
	case state.ClockPaused:
		banner = "Clock paused — picks stay open"
	case s.demoMode:
		banner = "Rehearsal mode"
	default:
		if hasNotice, _ := poolStatusMap["has_notice"].(bool); hasNotice {
			banner, _ = poolStatusMap["detail"].(string)
		}
	}
	// history is the typed pick-tape/board/by-team view (draft_history.go):
	// page.server.go's prepareDraftData reads it directly (a Service method
	// result stashed in this otherwise-untyped map, the same pattern teams/
	// picks/available already use for their own typed-conversion boundary).
	// Built only when includeHistory (P1 perf fix): the zero DraftHistoryView
	// otherwise stashed here already renders an empty pane for free —
	// buildDraftHistoryView (page.server.go) has treated a missing "history"
	// key this way since Task 7, the same fallback every non-league test
	// fixture in app/draft relies on.
	var history DraftHistoryView
	if includeHistory {
		history = s.DraftHistory(state, viewerTeam)
	}
	return map[string]any{
		"viewer":               viewer,
		"public_entry":         publicEntryData(publicEntry),
		"draft":                s.draftSummary(now),
		"history":              history,
		"has_adp":              hasADP,
		"teams":                teamMaps,
		"picks":                s.pickMaps(state, pool.byID, scoringValues),
		"available":            availableMaps,
		"board":                boardPanel,
		"board_count":          len(boardPanel),
		"pool_status":          poolStatusMap,
		"pool_count":           len(pool.players),
		"available_count":      len(available),
		"pool_query":           rawQuery,
		"pool_position":        pos,
		"pool_sort":            activeSort,
		"pool_total":           pagination.Total,
		"pool_page":            pagination.Page,
		"pool_pages":           pagination.Pages,
		"pool_page_size":       pagination.PageSize,
		"pool_page_start":      pagination.Start + 1,
		"pool_page_end":        pagination.End,
		"pool_has_previous":    pagination.HasPrevious,
		"pool_has_next":        pagination.HasNext,
		"pool_previous_href":   draftPoolPageHref(pos, rawQuery, activeSort, pagination.Page-1),
		"pool_next_href":       draftPoolPageHref(pos, rawQuery, activeSort, pagination.Page+1),
		"pool_all_href":        draftPoolPageHref("", rawQuery, activeSort, 1),
		"pool_rb_href":         draftPoolPageHref("RB", rawQuery, activeSort, 1),
		"pool_wr_href":         draftPoolPageHref("WR", rawQuery, activeSort, 1),
		"pool_qb_href":         draftPoolPageHref("QB", rawQuery, activeSort, 1),
		"pool_te_href":         draftPoolPageHref("TE", rawQuery, activeSort, 1),
		"pool_k_href":          draftPoolPageHref("K", rawQuery, activeSort, 1),
		"pool_dst_href":        draftPoolPageHref("DST", rawQuery, activeSort, 1),
		"pool_p_href":          draftPoolPageHref("P", rawQuery, activeSort, 1),
		"on_clock":             onClockMap,
		"on_clock_id":          onClockID,
		"pick_number":          displayPickNumber,
		"picks_empty":          len(state.Picks) == 0,
		"round":                round,
		"can_pick":             canPick,
		"draft_complete":       complete,
		"demo_mode":            s.demoMode,
		"ready_count":          readyManagerCount,
		"manager_count":        managerCount,
		"viewer_ready":         viewerTeam != "" && state.Ready[viewerTeam],
		"viewer_autopick":      viewerTeam != "" && state.Autopick[viewerTeam],
		"order_randomized":     len(state.DraftOrder) > 0,
		"league_mode":          s.cfg.ModeLabel,
		"season_start_week":    s.seasonStartWeek(),
		"clock":                s.clockView(state, now),
		"current_pick_token":   draftCurrentPickToken(state),
		"previous_pick_token":  draftPreviousPickToken(state),
		"league":               s.leagueMapForViewer(r),
		"matchup_source_label": matchupLabel,
		"has_matchup_source":   hasMatchupLabel,
		"picks_total":          picksTotal,
		"snake_direction":      snakeDirection(activeTeamCount(state.DraftOrder), nextNumber),
		"next_team":            nextTeamMap,
		"after_next_team":      afterNextTeamMap,
		"viewer_on_clock":      viewerTeam != "" && viewerTeam == onClockID,
		"your_pick_in":         yourPickIn,
		"here_count":           hereCount,
		"auto_count":           autoCount,
		"banner":               banner,
		"queue":                queuePanel,
		"queue_empty":          len(queuePanel) == 0,
		"next_queued":          nextQueued,
		"roster_needs":         rosterNeeds,
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
	armed := !deadline.IsZero() || state.ClockPaused
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
	if !deadline.IsZero() && !state.ClockPaused {
		remaining = int(effective.Sub(now).Seconds())
		if remaining < 0 {
			remaining = 0
		}
	}
	clockState := "NOT RUNNING"
	if state.ClockPaused {
		clockState = "PAUSED"
	} else if !deadline.IsZero() {
		clockState = "RUNNING"
	}
	canPause := clockState == "RUNNING"
	canResume := state.DraftStarted && !draftComplete(state) && (clockState == "PAUSED" || clockState == "NOT RUNNING")
	canExtend := canPause
	return map[string]any{
		"armed":              armed,
		"paused":             state.ClockPaused,
		"state":              clockState,
		"can_pause":          canPause,
		"can_resume":         canResume,
		"can_extend":         canExtend,
		"deadline":           formatClockInstant(deadline),
		"effective_deadline": formatClockInstant(effective),
		"reason":             clockReasonLabel(reason),
		"remaining_seconds":  remaining,
		"remaining_label":    countdownMMSSLabel(remaining),
		"duration_seconds":   int(s.pickClock(state).Seconds()),
		"duration_label":     countdownMMSSLabel(int(s.pickClock(state).Seconds())),
		"server_now":         now.UTC().Format(time.RFC3339),
		// These opaque values are form contracts, not authorization
		// credentials. current_pick_token covers the on-clock seat and
		// deadline; previous_pick_token covers the exact last pick for undo.
		"current_pick_token":  draftCurrentPickToken(state),
		"action_token":        draftCurrentPickToken(state),
		"previous_pick_token": draftPreviousPickToken(state),
	}
}

// clockReasonLabels turns clockView's internal reason token (also
// effectiveDeadline's control-flow reason in draftclock.go, left
// unchanged there) into the plain football label the pick-clock chip
// shows every manager on draft night. The default case is a safe neutral
// word, never the raw reason token.
var clockReasonLabels = map[string]string{
	"unarmed":  "NOT RUNNING",
	"clock":    "ON THE CLOCK",
	"autopick": "AUTOPICK ARMED",
	"paused":   "PAUSED",
	"not_seen": "NOT SEEN — SHORT SAFETY CLOCK",
}

func clockReasonLabel(reason string) string {
	if label, ok := clockReasonLabels[reason]; ok {
		return label
	}
	return "ON THE CLOCK"
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
		"league": s.leagueMapForViewer(r),
	}
}

// LeagueIdentity is the "league" block StaticPageData bundles, for a route
// server that assembles its own data map but still owes the shared layout
// its league identity — every route's brand and announcement banner read
// it, and a page that omits it paints an empty brand (the Signal Wire did,
// caught by the 2026-09-01 UX audit).
//
// LeagueIdentity itself takes no request and so cannot apply
// leagueMapForViewer's demo-anonymous attention override (item 5(d),
// 2026-08-31 post-wave audit); LeagueIdentityForViewer, just below, is the
// request-aware replacement. app/wire (tamarack) is this method's one
// caller today and still calls this one — swapping to
// LeagueIdentityForViewer(r) is a same-shape, additive change whenever
// that file's owner picks it up.
func (s *Service) LeagueIdentity() map[string]any { return s.leagueMap() }

// LeagueIdentityForViewer is LeagueIdentity's per-request counterpart,
// mirroring leagueMapForViewer exactly (see its own doc comment for the
// demo-anonymous attention override this closes).
func (s *Service) LeagueIdentityForViewer(r *http.Request) map[string]any {
	return s.leagueMapForViewer(r)
}

func (s *Service) LoginData(r *http.Request, configured bool) map[string]any {
	viewer := s.Viewer(r)
	next := navigation.DefaultReturnPath
	if r != nil && r.URL != nil {
		next = navigation.SafeReturnPath(r.URL.Query().Get("next"))
	}
	return map[string]any{
		"viewer":          viewer,
		"public_entry":    s.PublicEntryDataForViewer(r, viewer),
		"configured":      configured,
		"demo_mode":       s.demoMode,
		"seats":           len(s.Teams()),
		"seat_numbers":    seatNumbers(len(s.Teams())),
		"league":          s.leagueMapForViewer(r),
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
// "matchupStatus" and "matchupClock", per matchup ID). The view exposes
// checked/polled and mirrored-ledger freshness separately so those clocks
// cannot be conflated by the browser.
func (s *Service) LiveScoresView(ctx context.Context) map[string]any {
	live := s.LiveScores(ctx)
	presentation := matchupPresentation(live.State)
	scores := make(map[string]string, len(live.Matchups)*2)
	starterPoints := make(map[string]string)
	starterPlayerName := make(map[string]string)
	starterPosition := make(map[string]string)
	starterNFLTeam := make(map[string]string)
	starterProvenance := make(map[string]string)
	starterJoinState := make(map[string]string)
	starterDetail := make(map[string]string)
	starterSource := make(map[string]string)
	// starterProvenanceText/starterJoinStateText/starterSourceText carry
	// ledgerLineupText/ledgerStatsText/ledgerSourceText's humanized words
	// (wave-8 audit item 3) for the page's own disclosure line, kept
	// separate from starterProvenance/starterJoinState/starterSource
	// themselves: those three stay the RAW StarterLedgerRow tokens (e.g.
	// StatSourceLive, "live"), the join-provenance contract external
	// callers (sim_live_test.go's replay scenarios, matching
	// league.StatSourceLive verbatim) and any future caller need — the
	// same separation of raw fact from rendered prose ledgerPlayerDetail's
	// own Detail field already keeps for the tip's long-form sentence.
	starterProvenanceText := make(map[string]string)
	starterJoinStateText := make(map[string]string)
	starterSourceText := make(map[string]string)
	starterGameStateBind := make(map[string]string)
	starterPossessionBind := make(map[string]string)
	matchupStatus := make(map[string]string, len(live.Matchups))
	matchupClock := make(map[string]string, len(live.Matchups))
	matchupIndicator := make(map[string]string, len(live.Matchups))
	matchupLiveStateBind := make(map[string]string, len(live.Matchups))
	projected := make(map[string]string, len(live.Matchups)*2)
	// winProb is keyed by TEAM ID, not matchup ID: each side's own win
	// probability is published under its own key (round-2 review of
	// commit 133d1d7, finding 1). A matchup-keyed value could only ever
	// hold one side's number, and the featured card binds its own team
	// (mine), not a fixed side, so a matchup-keyed bind would silently
	// flip the shown percentage to the wrong side's number the first
	// time the viewer's team is Away.
	winProb := make(map[string]string, len(live.Matchups)*2)
	// stillToPlay/stillToPlayTotal are the bare count and the total,
	// matching my_matchup/other_matchups' own two int fields (round-2
	// review of commit 133d1d7, finding 2); the page composes the "N of
	// M starters still to play" sentence from the two bound spans.
	stillToPlayBind := make(map[string]string, len(live.Matchups))
	stillToPlayTotalBind := make(map[string]string, len(live.Matchups))
	liveStatusValue, hasLive := s.liveStatus()
	pool := s.pool()
	addStarterRow := func(row StarterLedgerRow) {
		// Keep every visible starter field in a stable, one-level map keyed by
		// the slot. The page binds all of these fields to the same live key so
		// a poll cannot pair a new points value with the prior identity or join
		// explanation when a lineup/stat join changes.
		starterPoints[row.LiveKey] = row.PointsText
		starterPlayerName[row.LiveKey] = row.PlayerName
		starterPosition[row.LiveKey] = row.Position
		starterNFLTeam[row.LiveKey] = row.NFLTeam
		starterProvenance[row.LiveKey] = row.Provenance
		starterJoinState[row.LiveKey] = row.JoinState
		starterDetail[row.LiveKey] = row.Detail
		starterSource[row.LiveKey] = row.Source
		// ledgerLineupText/ledgerStatsText/ledgerSourceText (matchup_ledger.go)
		// turn the raw Provenance/JoinState/Source tokens into the labelled
		// words the ledger disclosure shows, each with its own leading
		// separator or empty string, so a live poll re-sends the exact same
		// already-formatted text a full render would (wave-8 audit item 3).
		starterProvenanceText[row.LiveKey] = ledgerLineupText(row.Provenance)
		starterJoinStateText[row.LiveKey] = ledgerStatsText(row.JoinState)
		starterSourceText[row.LiveKey] = ledgerSourceText(row.Source)
		starterGameStateBind[row.LiveKey] = row.GameState
		starterPossessionBind[row.LiveKey] = row.Possession
	}
	for _, matchup := range live.Matchups {
		scores[matchup.Away.ID] = matchupScoreText(matchup.Away)
		scores[matchup.Home.ID] = matchupScoreText(matchup.Home)
		for _, row := range matchup.Away.StarterLedger {
			addStarterRow(row)
		}
		for _, row := range matchup.Home.StarterLedger {
			addStarterRow(row)
		}
		status := matchup.Status
		if status == "" {
			status = "SCORES POSTED"
		}
		matchupStatus[matchup.ID] = status
		matchupClock[matchup.ID] = matchupClockLabel(matchup.Clock)
		matchupIndicator[matchup.ID] = liveIndicatorToken(matchup.State)
		matchupLiveStateBind[matchup.ID] = matchup.LiveState
		awayProjected := projectedTotal(matchup.Away.StarterLedger, starterProjections(matchup.Away.StarterLedger, pool.byID), liveStatusValue, hasLive)
		homeProjected := projectedTotal(matchup.Home.StarterLedger, starterProjections(matchup.Home.StarterLedger, pool.byID), liveStatusValue, hasLive)
		// hasProjectableStarters, not ScoreKnown: a pre-kickoff lineup still
		// has a real projection to show, even while the current score cell
		// itself reads "—" (wave-8 audit item 2).
		awayHasProjection := hasProjectableStarters(matchup.Away.StarterLedger)
		homeHasProjection := hasProjectableStarters(matchup.Home.StarterLedger)
		projected[matchup.Away.ID] = projectedText(awayProjected, awayHasProjection)
		projected[matchup.Home.ID] = projectedText(homeProjected, homeHasProjection)
		winProb[matchup.Home.ID] = winProbabilityText(homeProjected, awayProjected, homeHasProjection, awayHasProjection)
		winProb[matchup.Away.ID] = winProbabilityText(awayProjected, homeProjected, awayHasProjection, homeHasProjection)
		combined := append(append([]StarterLedgerRow{}, matchup.Away.StarterLedger...), matchup.Home.StarterLedger...)
		stillToPlayBind[matchup.ID] = strconv.Itoa(stillToPlay(combined, liveStatusValue))
		stillToPlayTotalBind[matchup.ID] = strconv.Itoa(len(combined))
	}
	checkedAt := live.CheckedAt
	if checkedAt.IsZero() {
		checkedAt = live.LastUpdated
	}
	checked := s.formatMatchupUpdateOrUnavailable(checkedAt)
	statsUpdated := s.formatMatchupUpdateOrUnavailable(live.StatsUpdatedAt)
	liveStatus := s.liveStatusText(live, presentation)
	return map[string]any{
		"ok":                    live.OK,
		"source":                live.Source,
		"sourceLabel":           live.SourceLabel,
		"week":                  live.Week,
		"weekLabel":             s.presentedWeekLabel(live),
		"state":                 live.State,
		"status":                live.Status,
		"warning":               live.Warning,
		"liveState":             live.LiveState,
		"sourceLine":            live.SourceLine,
		"slateLine":             live.SlateLine,
		"gamesFinal":            live.GamesFinal,
		"scores":                scores,
		"matchupStatus":         matchupStatus,
		"matchupClock":          matchupClock,
		"matchupIndicator":      matchupIndicator,
		"matchupLiveState":      matchupLiveStateBind,
		"projected":             projected,
		"winProb":               winProb,
		"stillToPlay":           stillToPlayBind,
		"stillToPlayTotal":      stillToPlayTotalBind,
		"starterPoints":         starterPoints,
		"starterPlayerName":     starterPlayerName,
		"starterPosition":       starterPosition,
		"starterNFLTeam":        starterNFLTeam,
		"starterProvenance":     starterProvenance,
		"starterJoinState":      starterJoinState,
		"starterDetail":         starterDetail,
		"starterSource":         starterSource,
		"starterProvenanceText": starterProvenanceText,
		"starterJoinStateText":  starterJoinStateText,
		"starterSourceText":     starterSourceText,
		"starterGameState":      starterGameStateBind,
		"starterPossession":     starterPossessionBind,
		"liveStatus":            liveStatus,
		"liveUpdated":           checked,
		"lastUpdated":           statsUpdated,
		"checkedAt":             checked,
		"statsUpdatedAt":        statsUpdated,
		"liveStatsUpdated":      statsUpdated,
		"liveIndicator":         liveIndicatorToken(live.State),
		"headlineTop":           presentation["headline_top"],
		"headlineBottom":        presentation["headline_bottom"],
		"refreshLabel":          presentation["refresh_label"],
		"noteTitle":             presentation["note_title"],
		"noteBody":              presentation["note_body"],
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
	if err != nil {
		return ready, s.teamByID(teamID).Name, err
	}
	s.emitDraft("draft:seat", s.seatBinds(s.store.Snapshot(), teamID, s.clock()))
	return ready, s.teamByID(teamID).Name, nil
}

func (s *Service) MakePick(r *http.Request, requestedTeam, playerID string) (DraftPick, Player, Team, error) {
	playerID = strings.TrimSpace(playerID)
	pool := s.pool()
	player, ok := pool.byID[playerID]
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
	if position, limit, breach := teamWouldBreachLimit(state, pool.byID, teamID, []string{playerID}, nil); breach {
		return DraftPick{}, Player{}, Team{}, fmt.Errorf("%s", limitMessage(position, limit))
	}
	if !draftCandidateKeepsRosterViable(state, pool.byID, teamID, playerID) {
		return DraftPick{}, Player{}, Team{}, fmt.Errorf("choose a player who keeps every required starter slot fillable")
	}
	// Scarcity guard (rules-audit item 3): autopickChoice already refuses
	// a scarcity-blocked candidate (draftclock.go's positionScarcityBlocksCandidate),
	// but MakePick — the manual-pick path — never consulted it, so a
	// manager who covered their own requirement for a scarce position
	// (say, punter) could freely keep drafting more of them while a peer
	// seat had not yet drafted even one, with no guard and no warning.
	// Applying the identical guard here, with the identical picked/preset/
	// otherTeamIDs construction autopickChoice uses, closes that gap for
	// every manual pick the same way autopick was already protected.
	{
		picked := make(map[string]bool, len(state.Picks))
		for _, existing := range state.Picks {
			picked[existing.PlayerID] = true
		}
		preset := CurrentRoster()
		otherTeamIDs := make([]string, 0, len(s.Teams()))
		for _, team := range s.Teams() {
			if team.ID != teamID {
				otherTeamIDs = append(otherTeamIDs, team.ID)
			}
		}
		if blocked, supply, stillMissing := positionScarcityDetail(state, pool, picked, preset, teamID, player.Position, otherTeamIDs); blocked {
			return DraftPick{}, Player{}, Team{}, fmt.Errorf("%s", positionScarcityMessage(player.Position, supply, stillMissing))
		}
	}
	// The pick and its clock reset land in one store transaction (section
	// 4.6 of the pick-clock spec). A paused clock stays unarmed after the
	// pick — pause freezes the timer, not the draft — and the final pick
	// leaves the clock unarmed for good.
	totalPicks := draftTeamCount() * CurrentDraftRounds()
	nextDeadline := time.Time{}
	if !state.ClockPaused && len(state.Picks)+1 < totalPicks {
		nextDeadline = now.Add(s.pickClock(state))
	}
	pick, err := s.store.MakePick(teamID, playerID, "manager", now, nextDeadline)
	if err != nil {
		return DraftPick{}, Player{}, Team{}, err
	}
	snapshot := s.store.Snapshot()
	s.emitDraft("draft:pick", s.draftPickPayload(snapshot, pick, now))
	s.maybeEmitDraftComplete(state, snapshot, now)
	return pick, player, s.teamByID(teamID), nil
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
	s.emitDraft("draft:seat", s.seatBinds(s.store.Snapshot(), teamID, s.clock()))
	return on, s.teamByID(teamID).Name, nil
}

// seatActionAuthority is the canonical request-to-seat projection for every
// manager-facing seat-tied action. OwnerKey is the primary manager identity
// shared by a primary and co-manager. A commissioner receives no implicit
// cross-seat authority here; explicit commissioner APIs own that policy.
type seatActionAuthority struct {
	TeamID   string
	OwnerKey string
}

var (
	errSeatActionSignIn   = errors.New("Google sign-in is required for seat actions")
	errSeatActionRequired = errors.New("claim a team seat before taking this action")
	errSeatActionWrong    = errors.New("you may act only for your own team seat")
)

// requestSeatAuthority resolves a real persisted seat without creating
// membership as a side effect. requestedTeam may be empty; when present it
// must match the signed-in manager seat. Demo rehearsal retains its explicit
// synthetic authority and may name a known seat.
func (s *Service) requestSeatAuthority(r *http.Request, requestedTeam string) (seatActionAuthority, error) {
	return s.requestSeatAuthorityForState(r, s.store.Snapshot(), requestedTeam)
}

func (s *Service) requestSeatAuthorityForState(r *http.Request, state PersistedState, requestedTeam string) (seatActionAuthority, error) {
	requestedTeam = strings.TrimSpace(requestedTeam)
	if user, ok := s.CurrentUser(r); ok {
		canonicalEmail := s.identityResolver.Resolve(user.Email)
		member, exists := memberByEmail(state.Members, canonicalEmail)
		teamID := strings.TrimSpace(member.TeamID)
		if !exists || teamID == "" || !knownTeam(teamID) {
			return seatActionAuthority{}, errSeatActionRequired
		}
		if requestedTeam != "" && requestedTeam != teamID {
			return seatActionAuthority{}, errSeatActionWrong
		}
		owner := normalizeEmail(memberForTeam(state.Members, teamID).Email)
		if owner == "" {
			owner = normalizeEmail(member.Email)
		}
		if owner == "" {
			return seatActionAuthority{}, errSeatActionRequired
		}
		return seatActionAuthority{TeamID: teamID, OwnerKey: owner}, nil
	}
	if s.demoMode {
		teamID := requestedTeam
		if teamID == "" {
			teams := s.Teams()
			if len(teams) == 0 {
				return seatActionAuthority{}, errSeatActionRequired
			}
			teamID = teams[0].ID
		}
		if !knownTeam(teamID) {
			return seatActionAuthority{}, errSeatActionWrong
		}
		return seatActionAuthority{TeamID: teamID, OwnerKey: "demo-guest"}, nil
	}
	return seatActionAuthority{}, errSeatActionSignIn
}

func (s *Service) actingTeam(r *http.Request, requested string) (string, error) {
	if user, ok := s.CurrentUser(r); ok {
		state := s.store.Snapshot()
		canonicalEmail := s.identityResolver.Resolve(user.Email)
		member, exists := memberByEmail(state.Members, canonicalEmail)
		if !exists || strings.TrimSpace(member.TeamID) == "" {
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
	state := PersistedState{}
	if s.store != nil {
		state = s.store.Snapshot()
	}
	return s.draftSummaryForState(now, state)
}

func (s *Service) draftSummaryForState(now time.Time, state PersistedState) map[string]any {
	draftAt := s.EffectiveDraftAt(state)
	location := s.draftTZ
	if location == nil {
		location, _ = time.LoadLocation(DefaultDraftTZ)
	}
	local := draftAt.In(location)
	timezone := strings.TrimSpace(s.cfg.Timezone)
	if timezone == "" {
		timezone = location.String()
	}
	statusLabel := "SCHEDULED WINDOW"
	statusNote := "The commissioner controls when the room opens. This is the scheduled draft window."
	if !now.Before(draftAt) {
		statusLabel = "AWAITING COMMISSIONER"
		statusNote = "The scheduled window has arrived. The room stays closed until the commissioner starts it."
	}
	if state.DraftStarted {
		statusLabel = "LIVE"
		statusNote = "The commissioner opened the room. The current team is on the clock."
	}
	complete := draftComplete(state)
	if complete {
		statusLabel = "COMPLETE"
		statusNote = fmt.Sprintf("All %d picks are locked. Set lineups in the Team terminal; undrafted players are now free agents.", len(state.Picks))
	}
	startedAt := ""
	if !state.DraftStartedAt.IsZero() {
		startedAt = state.DraftStartedAt.Format(time.RFC3339)
	}
	summary := map[string]any{
		"at":              draftAt.Format(time.RFC3339),
		"overridden":      !state.DraftAtOverride.IsZero(),
		"input_value":     draftMeetingInputValue(draftAt, location),
		"event_label":     "LEAGUE DRAFT",
		"date":            strings.ToUpper(local.Format("Mon · Jan")) + " " + strconv.Itoa(local.Day()),
		"time":            local.Format("3:04 PM MST"),
		"timezone":        FriendlyTimezoneLabel(timezone),
		"long_date":       local.Format("Monday, January 2, 2006"),
		"format":          s.draftFormatLabel(),
		"started":         state.DraftStarted,
		"complete":        complete,
		"started_at":      startedAt,
		"window_reached":  !now.Before(draftAt),
		"status_label":    statusLabel,
		"status_note":     statusNote,
		"days_until":      max(0, int(draftAt.Sub(now).Hours()/24)),
		"countdown_label": countdownDHMSLabel(draftAt.Sub(now)),
		"published":       DraftDatePublished(now, draftAt),
	}
	// An unset or absurdly far-out draft date is a placeholder, not a
	// schedule: the neutral reference league ships 2098-12-31, and the
	// 2026-09-01 UX audit found it rendered as a live "26419d 16:51:31"
	// countdown on three surfaces. Render the honest empty state instead —
	// no countdown target, no fabricated calendar line. A started or
	// completed draft keeps its own truthful status; input_value stays the
	// raw value so the commissioner's own form still shows what is stored.
	if summary["published"] == false {
		summary["at"] = ""
		summary["countdown_label"] = ""
		summary["days_until"] = 0
		summary["window_reached"] = false
		switch {
		case (state.DraftStarted || complete) && !state.DraftStartedAt.IsZero():
			// The scheduled meeting date is unpublished, but the room did
			// open — an audited fact, not a placeholder. Anchor the date
			// line on that instant instead of repeating "not published"
			// beside a LIVE/COMPLETE status the same summary already
			// states (the audit's exact contradiction: "Draft time not
			// published yet" next to "COMPLETE — All 120 picks are
			// locked.").
			startedLocal := state.DraftStartedAt.In(location)
			summary["date"] = strings.ToUpper(startedLocal.Format("Mon · Jan")) + " " + strconv.Itoa(startedLocal.Day())
			summary["time"] = startedLocal.Format("3:04 PM MST")
			summary["long_date"] = startedLocal.Format("Monday, January 2, 2006")
		case state.DraftStarted || complete:
			// Started/complete with no recorded start time (a fixture or
			// migrated league): still no "not published" claim to make
			// beside a truthful LIVE/COMPLETE status — omit the date line
			// rather than contradict it.
			summary["date"] = ""
			summary["time"] = ""
			summary["long_date"] = ""
		default:
			summary["date"] = "TBD"
			summary["time"] = ""
			summary["long_date"] = "Draft time not published yet"
			summary["status_label"] = "NOT SCHEDULED"
			summary["status_note"] = "Draft time is not published yet. The commissioner sets it in League settings."
		}
	}
	return summary
}

// DraftDatePublished reports whether at is a real, operator-published
// draft date: non-zero and no further out than one season (400 days) from
// now. Far-future placeholder dates (the neutral reference league's
// 2098-12-31) fail it; any past date passes — a draft that already
// happened is still a real date.
func DraftDatePublished(now, at time.Time) bool {
	return !at.IsZero() && at.Before(now.Add(400*24*time.Hour))
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

// seasonOpenLine renders the persisted schedule's opening NFL week, or the
// explicit platform fallback before a schedule exists, from season_start_at.
//
// Guarded by the same DraftDatePublished sentinel check as rulesIdentityMap's
// season_start and the scoring masthead's own season-start line (2026-08-31
// gap-audit finding): DefaultConfig ships a far-future placeholder
// SeasonStartAt (config.go's placeholderSeasonStartAt, 2099-01-08), and this
// line used to print it as a literal calendar fact — the demo /matchups page
// showed "League play begins NFL week 1 · January 8" for a season that was
// never actually scheduled.
func (s *Service) seasonOpenLine() string {
	if !DraftDatePublished(s.clock(), s.cfg.SeasonStartAt) {
		return "League play begins when the commissioner publishes the season start."
	}
	return fmt.Sprintf("League play begins NFL week %d · %s.", s.seasonStartWeek(), s.cfg.SeasonStartAt.Format("January 2"))
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
	return fmt.Sprintf("Draft ADP and projections use %s consensus ranks.", label)
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
	// snapshot/now are captured once and reused by both fantasy_seats_open
	// and attention below, rather than each calling s.store.Snapshot() (or
	// s.clock()) a second time — leagueMap runs on every route's layout
	// render (gap-audit item 6), so one extra read is worth avoiding.
	snapshot := s.store.Snapshot()
	now := s.clock()
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
		"fantasy_seats_open": claimedSeatCount(snapshot.Members) < len(s.Teams()),
		// latest_announcement carries the shared layout's dismiss-free
		// banner data (league-announcements spec). It lives here, not in a
		// separate data key, because leagueMap is the one map every page's
		// data function already includes — see the doc comment above — so
		// the layout's banner needs no per-page wiring to reach it.
		"latest_announcement": s.latestAnnouncementBanner(),
		// attention (gap-audit item 6): the same "leagueMap is the one map
		// every page's data function already includes" property lets the
		// shared layout show one small "urgent" chip everywhere, not just
		// on the home Action Center. It is deliberately league-wide, not
		// per-viewer — leagueMap takes no *http.Request and 19 call sites
		// across 10 other files build the "league" key from it, so adding a
		// request parameter here would ripple far outside this file. A
		// per-viewer projection (the exact list an individual manager's
		// home Action Center shows) belongs behind a request-aware method
		// such as ActionCenterData; attentionMap instead surfaces the
		// state-level facts most likely to matter to any seated viewer
		// (an accepted trade awaiting review, this week's still-open
		// pick'em games), so it never disagrees with what triggers those
		// SAME home Action Center tasks, just without per-viewer framing.
		"attention": s.attentionMap(snapshot, now),
		// draft_complete (wave 7, item 6): the same "leagueMap is the one
		// map every page's data function already includes" property attention
		// (above) already relies on lets the shared layout's nav show a
		// "Draft results" destination only once the draft has actually
		// finished, with no per-page wiring — app/layout.gsx's own
		// PrimaryNavigation reads it as props.DraftComplete.
		"draft_complete": draftComplete(snapshot),
	}
}

// leagueMapForViewer is leagueMap's per-request wrapper (item 5(d),
// 2026-08-31 post-wave audit): identical to leagueMap() in every field
// except attention, which reads the honest empty shape (emptyAttentionMap)
// for a genuinely anonymous demo visitor. leagueMap itself stays
// request-less — its own doc comment above explains the cost of threading
// a *http.Request through its 19 call sites for every field — but
// attention is the one field whose correct value actually depends on who
// is asking, so this thin wrapper carries that one exception instead.
//
// Viewer's own "has_seat": s.demoMode (below, Viewer) deliberately lets
// ANY unauthenticated visitor to a demo-mode league act as team-1 for
// interactive purposes, so a spectator can try the app without signing
// in. But the shared chrome's attention chip is gated on has_seat too
// (app/layout.gsx), so that same pretense made a pure spectator — who
// manages nothing — see a "N items need attention" chip naming real
// accepted/open trades and pick'em games that belong to the actual seated
// managers. Every *Data(r) method that builds the "league" key calls this
// now, not leagueMap() directly (admin.go is magnolia's file; its own
// leagueMap() call site needs the same one-line swap — noted separately).
func (s *Service) leagueMapForViewer(r *http.Request) map[string]any {
	league := s.leagueMap()
	if s.demoMode {
		if _, signedIn := s.CurrentUser(r); !signedIn {
			league["attention"] = emptyAttentionMap()
			return league
		}
	}
	// Item 2 (2026-09-02 audit): attentionMap's pick'em item is league-wide
	// by design (see its own doc comment below) — every game still open
	// this week, not this viewer's own unpicked ones. That blind spot let
	// a manager who had already called all 16 week-1 games keep seeing "1
	// URGENT" for the eleven days those games stayed open for OTHER
	// managers to pick, even though the home Action Center (which already
	// reads the viewer's own picked_count) correctly showed STABLE. This
	// is the one *http.Request-aware seam leagueMap's 19 call sites
	// deliberately avoid (TestLeagueMapEmbedsAttention's own doc comment);
	// leagueMapForViewer already carries r and already overrides
	// league["attention"] once above for the anonymous-demo case, so the
	// same seam re-scopes the pick'em item to the admitted viewer's own
	// unpicked count via attentionMap's optional viewerOpenPickem
	// argument, leaving the trades items (already correctly league-wide
	// by design) untouched. Not admitted (no team, no membership, not the
	// demo guest) leaves league["attention"] exactly as s.leagueMap()
	// produced it — the chrome's own has_seat gate already hides the chip
	// from that visitor regardless (TestLeagueMapForViewerSuppressesAttentionForAnonymousDemoViewer).
	state := s.store.Snapshot()
	if _, admitted := s.pickemViewerKeyForState(r, state); admitted {
		now := s.clock()
		league["attention"] = s.attentionMap(state, now, s.openPickemGameCountForViewer(r, state, now))
	}
	return league
}

// attentionMap is gap-audit item 6's small "urgent_count"/"items" projection
// for the shared chrome (app/layout.gsx's rail-head and mobile-bar chip).
// Each item names a route and a plain-language label; a trade item's route
// carries the "#trade-<id>" fragment so the chip can deep-link straight to
// the row awaiting review (item 6's explicit requirement) once app/trades
// grows that anchor (rowan owns app/trades/page.gsx — see the integrator
// note in this wave's report). Kept cheap and state-only, deliberately not
// walking every team's live lineup (that facts pass belongs in
// ActionCenterData, which already exists for the home page itself) — this
// runs on the hot path of every route's layout render.
//
// Item 5(a)/(b) (2026-08-31 post-wave audit): trades now count BOTH an
// open offer awaiting a response and an accepted offer awaiting review —
// previously only accepted counted, so a seated manager with a live
// incoming offer saw the Action Center's own "Review incoming trade" task
// (tradeActions, hq.go) but no matching chip, and the chip's own total
// silently disagreed with the Action Center's (a field report of 2 vs 3).
// TradeStatusOpen and TradeStatusAccepted are exactly the two statuses
// actionCenterDataForSnapshot's own Trades-facts switch (service.go)
// already treats as active/pending; this loop now classifies a trade
// offer the same way, so the two surfaces cannot drift apart on WHICH
// offers count again even though the chip stays league-wide and the
// panel stays per-viewer (attentionMap's own longstanding, deliberate
// design split — see the doc comment above).
//
// viewerOpenPickem (item 2, 2026-09-02 audit) is optional and deliberately
// variadic, not a required parameter: leagueMap's own call below (no
// viewer in scope) keeps the original league-wide "any game still open
// this week" count, exactly as before — TestAttentionMapDerivesFrom-
// OpenAndAcceptedTradesAndOpenPickemGames and TestLeagueMapEmbedsAttention
// both pin that 2-argument call unchanged. leagueMapForViewer (the one
// caller with a *http.Request) passes the admitted viewer's own unpicked
// count instead, so the chip never disagrees with a viewer who has
// already called every open game.
func (s *Service) attentionMap(state PersistedState, now time.Time, viewerOpenPickem ...int) map[string]any {
	items := make([]map[string]any, 0, 4)
	tradesCount := 0
	for _, offer := range state.TradeOffers {
		from := s.teamByID(offer.FromTeamID).Name
		to := s.teamByID(offer.ToTeamID).Name
		switch offer.Status {
		case TradeStatusAccepted:
			items = append(items, map[string]any{
				"route": "/trades#trade-" + offer.ID,
				"label": "Accepted trade between " + from + " and " + to + " is in review",
			})
			tradesCount++
		case TradeStatusOpen:
			items = append(items, map[string]any{
				"route": "/trades#trade-" + offer.ID,
				"label": "Trade offer between " + from + " and " + to + " is awaiting a response",
			})
			tradesCount++
		}
	}
	pickemCount := 0
	if len(viewerOpenPickem) > 0 {
		if open := viewerOpenPickem[0]; open > 0 {
			items = append(items, map[string]any{
				"route": "/pickem",
				"label": CountNoun(open, "pick'em game") + " to call",
			})
			pickemCount = 1
		}
	} else if open := s.openPickemGameCount(state, now); open > 0 {
		items = append(items, map[string]any{
			"route": "/pickem",
			"label": CountNoun(open, "open pick'em game") + " this week",
		})
		pickemCount = 1
	}
	return attentionShape(items, tradesCount, pickemCount)
}

// attentionShape is attentionMap's return-shape builder, factored out so
// emptyAttentionMap (leagueMapForViewer's demo-anonymous override, item
// 5(d)) always produces the exact same key set attentionMap does — no
// caller can see a map missing a key just because it took the empty path.
func attentionShape(items []map[string]any, tradesCount, pickemCount int) map[string]any {
	return map[string]any{
		"urgent_count": len(items),
		"items":        items,
		"has_items":    len(items) > 0,
		// chip_label (item 5(c), 2026-08-31 post-wave audit) is the rail-
		// head/mobile-bar chip's full aria-label, rendered here so
		// app/layout.gsx reads one already-pluralized string instead of
		// concatenating urgent_count + " items need attention in the
		// Action Center" itself — that concatenation always read "1 items
		// need attention...", wrong for the single-item case. hickory
		// (app/layout.gsx) reads attention.chip_label in place of that
		// concatenation.
		"chip_label": attentionChipLabel(len(items)),
		// pickem_hot/trades_hot and their *_attention_text counterparts
		// (build item 2, rail-dot leftover) are pre-shaped scalars, not a
		// route-prefix filter over items, because app/layout.gsx's
		// PrimaryNavigation is a legacy (non-island) GoSX component: the
		// Phase 4 filter()/startsWith() expression forms only lower for
		// //gosx:island bytecode (client/vm/vm.go), never for the
		// server-rendered legacy runtime (transpile package has no opcode
		// handling for them at all). Deriving the per-route hot flag and
		// its screen-reader count text here, once, keeps layout.gsx a
		// plain boolean/string prop read — the same shape every other
		// PrimaryNavigationProps field already uses.
		"pickem_hot":            pickemCount > 0,
		"pickem_attention_text": attentionDotText(pickemCount),
		"trades_hot":            tradesCount > 0,
		"trades_attention_text": attentionDotText(tradesCount),
	}
}

// emptyAttentionMap is the honest empty shape leagueMapForViewer installs
// for a genuinely anonymous demo visitor (item 5(d)): the exact key set
// attentionShape produces, every count zero.
func emptyAttentionMap() map[string]any {
	return attentionShape(make([]map[string]any, 0), 0, 0)
}

// attentionDotText renders the visually-hidden count text a hot rail dot
// carries ("1 item needs attention" / "2 items need attention") — subject
// and verb agreement together, which Plural alone (a noun-only helper)
// cannot express, so this composes both from the same count. Empty for a
// non-positive count: the caller only reads it when the matching *_hot
// flag is true.
func attentionDotText(count int) string {
	if count <= 0 {
		return ""
	}
	verb := "needs"
	if count != 1 {
		verb = "need"
	}
	return fmt.Sprintf("%d %s %s attention", count, Plural(count, "item"), verb)
}

// attentionChipLabel renders app/layout.gsx's rail-head/mobile-bar chip
// aria-label from one source (item 5(c), 2026-08-31 post-wave audit): see
// attentionShape's "chip_label" doc comment above for the "1 items need
// attention" bug this closes. Empty for a non-positive count, mirroring
// attentionDotText's own convention — the caller only reads it when
// has_items is true.
func attentionChipLabel(count int) string {
	if count <= 0 {
		return ""
	}
	verb := "needs"
	if count != 1 {
		verb = "need"
	}
	return fmt.Sprintf("%d %s %s attention in the Action Center", count, Plural(count, "item"), verb)
}

// openPickemGameCount counts this pick'em week's games that are still open
// for a pick (not yet kicked off, and not a void/no-line market) — a
// league-wide fact, unlike pickemHomeSummaryFromSnapshot's "unpicked by
// THIS viewer" count, so attentionMap can run without a *http.Request.
func (s *Service) openPickemGameCount(state PersistedState, now time.Time) int {
	games := s.schedule()
	week := s.pickemWeek(games, now)
	open := 0
	for _, game := range gamesInWeek(games, week) {
		if market, ok := state.PickemMarkets[game.ID]; ok && pickemMarketUnavailable(market) {
			continue
		}
		if !game.Kickoff.IsZero() && now.Before(game.Kickoff) {
			open++
		}
	}
	return open
}

// openPickemGameCountForViewer is openPickemGameCount's per-viewer
// counterpart (item 2, 2026-09-02 audit): the same still-open-this-week
// scan, but skipping any game the admitted viewer has already picked, so
// leagueMapForViewer's attention chip agrees with what that one manager
// actually still has to do. Mirrors pickemHomeSummaryFromSnapshot's own
// open_unpicked_count loop (pickem.go) without that function's heavier
// season record/streak tallying — leagueMapForViewer runs on every
// route's layout render, so this stays a single cheap pass. Not admitted
// (pickemViewerKeyForState's own false) reads as zero: an unadmitted
// visitor has no Pick'em picks of their own to call.
func (s *Service) openPickemGameCountForViewer(r *http.Request, state PersistedState, now time.Time) int {
	viewerKey, admitted := s.pickemViewerKeyForState(r, state)
	if !admitted {
		return 0
	}
	games := s.schedule()
	week := s.pickemWeek(games, now)
	picks := state.Pickems[viewerKey]
	open := 0
	for _, game := range gamesInWeek(games, week) {
		if market, ok := state.PickemMarkets[game.ID]; ok && pickemMarketUnavailable(market) {
			continue
		}
		if picks[game.ID] != "" {
			continue
		}
		if !game.Kickoff.IsZero() && now.Before(game.Kickoff) {
			open++
		}
	}
	return open
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
		"posted_at": s.leagueTimeStamp(latest.PostedAt),
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
			"posted_at":  a.PostedAt.In(s.LeagueLocation()).Format("Jan 2, 3:04 PM MST"),
			"posted_ago": relativeTime(now, a.PostedAt),
		})
	}
	return out
}

// RelativeTime is the canonical past-instant relative label ("N hours
// ago", "just now") for every page's time-sensitive values — the product
// experience contract requires exact league-local time plus a useful
// relative value, from one formatter, so route servers (the Signal Wire's
// displayTime chief among them) call this instead of growing their own.
func RelativeTime(now, then time.Time) string { return relativeTime(now, then) }

// LeagueLocation is the league's canonical *time.Location (config.go's
// Timezone field, defaulting to America/New_York) — the same resolution
// matchup formatting already uses. Route servers outside this package use
// it so no page can drift onto its own hard-coded zone again (the Signal
// Wire shipped on America/Los_Angeles for a while; the audit that caught
// it is spore.2026-09-01.alder in the hyphae space).
func (s *Service) LeagueLocation() *time.Location { return s.matchupLocation() }

// leagueTimeStamp is the shared recipe every stored-instant display in
// this package routes through (gap-audit finding: trades.go, admin.go,
// and players.go each used to format a stored, often-UTC instant directly
// — whatever zone it happened to carry, no relative text). It renders t in
// the league's canonical zone (LeagueLocation) using this package's
// existing "Jan 2, 3:04 PM MST" idiom, with RelativeTime's trailing label
// folded into the same string rather than a second field: several
// consuming templates — the shared layout's dismiss-free announcement
// banner chief among them (app/layout.gsx, not this package) — have no
// second binding to carry a relative value separately. now anchors the
// relative label; callers pass s.clock() so a fixed test clock stays
// deterministic. A zero instant renders "".
func (s *Service) leagueTimeStamp(t time.Time) string {
	stamp := s.leagueAbsoluteTimeStamp(t)
	if stamp == "" {
		return ""
	}
	return stamp + " · " + RelativeTime(s.clock(), t)
}

// leagueAbsoluteTimeStamp is leagueTimeStamp without the trailing relative
// label — for the rare surface where the relative half would go stale
// somewhere the accessibility tree caches it (an aria-label is read once
// at focus/activation, not live-updated the way the visible row's own DOM
// node is; wave-2-verification item 5, the announcement delete control's
// accessible name). A zero instant renders "".
func (s *Service) leagueAbsoluteTimeStamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.In(s.LeagueLocation()).Format("Jan 2, 3:04 PM MST")
}

// relativeTime renders a compact "N unit(s) ago" label for a past instant,
// floored at "just now" for anything under a minute. Only the coarsest
// unit that fits is shown (spec: home page's announcements section, "body
// + relative/absolute time").
// relativeTime renders then relative to now: "N minutes/hours/days ago"
// (or "just now" inside the first minute) once then has already happened,
// "in N minutes/hours/days" (mirroring commissionerV1Relative's own
// forward-looking phrasing, commissioner_summary_v1_derive.go) when then
// is still ahead of now.
//
// Item 3 (2026-08-31 post-wave audit): the past-only branch used to read
// `d < time.Minute` for the "just now" case. now.Sub(then) is NEGATIVE
// for a future then, and every negative number is less than one minute,
// so a future instant always fell into "just now" too — a scheduled
// action, a not-yet-elapsed lock, anything fed through this one shared
// helper with then ahead of now silently claimed to have already
// happened. d < 0 is now its own branch, checked first.
func relativeTime(now, then time.Time) string {
	d := now.Sub(then)
	if d < 0 {
		return futureRelativeUnit(-d)
	}
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

// futureRelativeUnit renders relativeTime's forward-looking branch.
// magnitude is already the positive then-minus-now gap. Same
// minute/hour/24-hour boundaries as the past branch above, so one
// function reads consistently in both directions; only the "N ago" vs
// "in N" phrasing differs, matching commissionerV1Relative's own
// "less than a minute" / "in " + value convention.
func futureRelativeUnit(magnitude time.Duration) string {
	switch {
	case magnitude < time.Minute:
		return "in less than a minute"
	case magnitude < time.Hour:
		n := int(magnitude / time.Minute)
		return fmt.Sprintf("in %d %s", n, Plural(n, "minute"))
	case magnitude < 24*time.Hour:
		n := int(magnitude / time.Hour)
		return fmt.Sprintf("in %d %s", n, Plural(n, "hour"))
	default:
		n := int(magnitude / (24 * time.Hour))
		return fmt.Sprintf("in %d %s", n, Plural(n, "day"))
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
// Each entry also carries the seat's aggregate presence bucket (here | idle |
// away | not_seen | unclaimed) and its persistent AUTO toggle.
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
		if s.demoMode && len(s.Teams()) > 0 && team.ID == s.Teams()[0].ID {
			item["manager"] = "REHEARSAL SEAT"
			item["claimed"] = true
		}
		claimed, _ := item["claimed"].(bool)
		item["ready"] = state.Ready[team.ID]
		// comb — oleander, item 3: onClockID (draftData, above) is set
		// from teamOnClock(..., nextNumber) whenever the draft is not yet
		// complete, with no check for DraftStarted — nextNumber is always
		// pick 1's own team before the first pick, so this field marked
		// that seat "on_clock" pre-draft. DraftSeatControl (page.gsx)
		// reads it straight into the commissioner drawer's "ON CLOCK"
		// badge, which then painted before the commissioner ever opened
		// the room. Requiring DraftStarted keeps every seat's badge
		// truthful pre-draft.
		item["on_clock"] = state.DraftStarted && team.ID == onClockID
		presenceLabel, presenceDetail, presenceSeenAt := s.teamPresence(state, team.ID, now)
		item["presence"] = presenceLabel
		item["presence_label"] = strings.ToUpper(strings.ReplaceAll(presenceLabel, "_", " "))
		item["presence_detail"] = presenceDetail
		item["presence_seen_at"] = formatClockInstant(presenceSeenAt)
		item["operator_count"] = len(s.presenceKeysForTeam(state, team.ID))
		item["autopick"] = claimed && state.Autopick[team.ID]
		boardCount := len(state.Boards[commissionerV1BoardOwnerKey(state, team.ID)])
		item["board_count"] = boardCount
		item["board_gap"] = claimed && boardCount == 0
		out = append(out, item)
	}
	return out
}

// TeamMemberEmailsForTest maps each seat's team ID to its primary member's
// email (memberForTeam's Role == "" pick), or "" for a seat with no bound
// member. It exists only for the harness's GET /test/draft route
// (test_routes.go), which stamps the result onto each team row as
// member_email: a rehearsal that seats already-real members needs to know
// which email holds which seat, and no production render may ever expose
// another manager's address, so this stays a distinct call site rather
// than a field draftTeamMaps adds to every render.
func (s *Service) TeamMemberEmailsForTest() map[string]string {
	state := s.store.Snapshot()
	order := state.DraftOrder
	if len(order) == 0 {
		order = defaultTeamIDs()
	}
	out := make(map[string]string, len(order))
	for _, teamID := range order {
		out[teamID] = memberForTeam(state.Members, teamID).Email
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
			"sync_label": "Published schedule", "refresh_label": "Past week",
			"note_title": "Scheduled scoring", "note_body": "This is a static schedule view; current-week scoring updates are shown on the current week.",
		}
	case MatchupStateInProgress:
		return map[string]string{
			"headline_top": "WEEK", "headline_bottom": "IN PROGRESS.",
			"sync_label": "Week results", "refresh_label": "Past week",
			"note_title": "Scoring results", "note_body": "This week is closed. Live scores show on the current week.",
		}
	case MatchupStateDegraded:
		return map[string]string{
			"headline_top": "SCHEDULE", "headline_bottom": "STATUS.",
			"sync_label": "Week results", "refresh_label": "Past week",
			"note_title": "Limited matchup data", "note_body": "This week is closed. Live scores show on the current week.",
		}
	default:
		return map[string]string{
			"headline_top": "WEEK", "headline_bottom": "IN VIEW.",
			"sync_label": "Week results", "refresh_label": "Past week",
			"note_title": "Schedule results", "note_body": "This week is closed. Live scores show on the current week.",
		}
	}
}

// presentedWeekLabel is the masthead's week/date phrase: MatchupsData's
// "live.week_label" (initial render) and LiveScoresView's "weekLabel" (the
// live poll bind) both read this key, so page.gsx's h1 shows the same text
// before and after the runtime's first poll. feed.go's demoProvider embeds
// cfg.SeasonStartAt straight into WeekLabel ("Week 1 · Sundays from January
// 8"); before a commissioner sets a real date, SeasonStartAt still holds
// the packaged example league's far-future placeholder (config.go's
// 2099-01-08 sentinel), so the raw label would claim a season date had
// already been published. DraftDatePublished already draws this exact
// "implausibly far out or zero" line for the draft date; reusing it here
// keeps "published" meaning one thing everywhere the app checks it.
func (s *Service) presentedWeekLabel(live LiveSnapshot) string {
	if live.State != MatchupStatePreseason || DraftDatePublished(s.clock(), s.cfg.SeasonStartAt) {
		return live.WeekLabel
	}
	return fmt.Sprintf("Week %d · season start not published yet", live.Week)
}

func (s *Service) liveMap(live LiveSnapshot) map[string]any {
	presentation := matchupPresentation(live.State)
	if live.State == MatchupStatePreseason {
		presentation["refresh_label"] = fmt.Sprintf("Before NFL week %d", s.seasonStartWeek())
	}
	checkedAt := live.CheckedAt
	if checkedAt.IsZero() {
		checkedAt = live.LastUpdated
	}
	statsUpdatedAt := s.formatMatchupUpdateOrUnavailable(live.StatsUpdatedAt)
	liveStatus := s.liveStatusText(live, presentation)
	return map[string]any{
		"source":              live.Source,
		"source_label":        live.SourceLabel,
		"week":                live.Week,
		"week_label":          s.presentedWeekLabel(live),
		"state":               live.State,
		"status":              live.Status,
		"live_state":          live.LiveState,
		"source_line":         live.SourceLine,
		"slate_line":          live.SlateLine,
		"games_final":         live.GamesFinal,
		"last_updated":        statsUpdatedAt,
		"checked_at":          s.formatMatchupUpdateOrUnavailable(checkedAt),
		"stats_updated_at":    statsUpdatedAt,
		"live_status":         liveStatus,
		"checked_label":       "Browser checked",
		"stats_updated_label": "Stats ledger updated",
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

// liveStatusText composes the freshness clause's bound "liveStatus"
// sentence: the sync label plus a plain word on the weekly ledger's own
// freshness. It deliberately does NOT name the Checked clock — the
// freshness clause (page.gsx's matchup-status-line__freshness) already
// carries that as its own separate "checkedAt" bind, right beside this
// one; before wave-8 audit item 4, this function's own "· Checked ..."
// clause repeated that same clock a second time in one rendered sentence.
func (s *Service) liveStatusText(live LiveSnapshot, presentation map[string]string) string {
	statsUpdated := s.formatMatchupUpdateOrUnavailable(live.StatsUpdatedAt)
	ledgerClause := "Ledger " + statsUpdated
	if statsUpdated == "Unavailable" && (live.State == MatchupStateScheduled || live.State == MatchupStatePreseason) {
		// Before this week's (or the season's) first kickoff, the ledger has
		// never had anything to post — "Ledger Unavailable" read as an
		// outage sitting right beside the status line's own "Weekly ledger
		// (nflverse)" source clause, not as the ordinary "nothing yet"
		// wave-8 audit item 4 means.
		ledgerClause = "Weekly ledger opens after the first game"
	}
	status := presentation["sync_label"] + " · " + ledgerClause
	if live.Warning != "" {
		status += " · BACKUP SCORES"
	}
	return status
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
			"sync_label": "Waiting for kickoff", "refresh_label": "Push at kickoff · 60 s fallback",
			"note_title": "Scheduled scoring", "note_body": "Scores begin updating after the first NFL kickoff for this fantasy week.",
		}
	case MatchupStateInProgress:
		return map[string]string{
			"headline_top": "LIVE", "headline_bottom": "SIGNAL.",
			"sync_label": "Live scores on", "refresh_label": "Push · 60 s fallback",
			"note_title": "Live scoring", "note_body": "Scores push to this page during games. No refresh is needed.",
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
			"sync_label": "Timing unavailable", "refresh_label": "Retrying · 60 s fallback",
			"note_title": "Limited matchup data", "note_body": "Pairings remain visible, but kickoff or scoring status is not currently authoritative.",
		}
	default:
		return map[string]string{
			"headline_top": "MATCHUPS", "headline_bottom": "COMING SOON.",
			"sync_label": "Preseason schedule", "refresh_label": "Before season start",
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

func (s *Service) formatMatchupUpdateOrUnavailable(value time.Time) string {
	if value.IsZero() {
		return "Unavailable"
	}
	return s.formatMatchupUpdate(value)
}

func matchupScoreText(team ScoreTeam) string {
	if team.ScoreText != "" {
		return team.ScoreText
	}
	return fmt.Sprintf("%.1f", team.Score)
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
			"live_state":          matchup.LiveState,
			"show_live_indicator": matchup.State == MatchupStateInProgress,
			"live_indicator":      liveIndicatorToken(matchup.State),
			"away": map[string]any{
				"id": matchup.Away.ID, "name": matchup.Away.Name, "abbreviation": matchup.Away.Abbreviation,
				"score": matchupScoreText(matchup.Away), "score_known": matchup.Away.ScoreKnown, "ledger_total": matchup.Away.LedgerTotalText, "ledger_known": matchup.Away.LedgerKnown, "score_basis": matchup.Away.ScoreBasis, "score_note": matchup.Away.ScoreNote, "starters": starterLedgerMaps(matchup.Away.StarterLedger), "tone": away.Tone, "manager": away.Manager,
				"has_avatar": awayHasAvatar, "has_avatar_image": awayHasImage, "avatar_image_url": awayAvatarURL,
			},
			"home": map[string]any{
				"id": matchup.Home.ID, "name": matchup.Home.Name, "abbreviation": matchup.Home.Abbreviation,
				"score": matchupScoreText(matchup.Home), "score_known": matchup.Home.ScoreKnown, "ledger_total": matchup.Home.LedgerTotalText, "ledger_known": matchup.Home.LedgerKnown, "score_basis": matchup.Home.ScoreBasis, "score_note": matchup.Home.ScoreNote, "starters": starterLedgerMaps(matchup.Home.StarterLedger), "tone": home.Tone, "manager": home.Manager,
				"has_avatar": homeHasAvatar, "has_avatar_image": homeHasImage, "avatar_image_url": homeAvatarURL,
			},
			"status": matchup.Status,
			"clock":  matchupClockLabel(matchup.Clock),
		})
	}
	return out
}

// starterLedgerMaps' provenance/join_state/source fields stay the RAW
// StarterLedgerRow tokens (a caller matching join provenance verbatim,
// e.g. league.StatSourceLive, needs the exact token, not prose that is
// free to reword); provenance_text/join_state_text/source_text carry the
// already-labelled, plain-word ledgerLineupText/ledgerStatsText/
// ledgerSourceText segments (wave-8 audit item 3) instead, each with its
// own leading separator or "" — the page (StarterCell,
// app/matchups/page.gsx) concatenates all three *_text fields with no
// separator of its own.
func starterLedgerMaps(rows []StarterLedgerRow) []map[string]any {
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		out = append(out, map[string]any{
			"live_key": row.LiveKey, "slot": row.Slot, "player_id": row.PlayerID,
			"player_name": row.PlayerName, "position": row.Position, "nfl_team": row.NFLTeam,
			"points": row.PointsText, "provenance": row.Provenance, "join_state": row.JoinState,
			"provenance_text": ledgerLineupText(row.Provenance), "join_state_text": ledgerStatsText(row.JoinState),
			"detail": row.Detail, "source": row.Source, "source_text": ledgerSourceText(row.Source), "game_state": row.GameState,
			"possession": row.Possession,
		})
	}
	return out
}

// featuredMatchupIndex resolves which of matchups (live.Matchups, in
// schedule order) is "my_matchup": the viewer's own matchup this week
// when teamID names one of its two sides, else the week's first matchup
// (isViewer false — the page labels this case FEATURED instead of
// claiming it belongs to the viewer). index is -1 when the week has no
// matchups at all.
func featuredMatchupIndex(matchups []ScoreMatchup, teamID string) (index int, isViewer bool) {
	if teamID != "" {
		for i, m := range matchups {
			if m.Home.ID == teamID || m.Away.ID == teamID {
				return i, true
			}
		}
	}
	if len(matchups) > 0 {
		return 0, false
	}
	return -1, false
}

// otherMatchupsCountLabel renders MatchupsData's "other_count_label"
// ("3 other matchups", "1 other matchup", "0 other matchups").
func otherMatchupsCountLabel(count int) string {
	if count == 1 {
		return "1 other matchup"
	}
	return fmt.Sprintf("%d other matchups", count)
}

// featuredMatchupViews builds MatchupsData's summary-first pair: my_matchup
// (A6's featured card, viewer's own matchup or the week's first one) and
// other_matchups (matchupMaps' remaining entries, each carrying the
// projected-total and still-to-play fields the scorebug cards render).
// matchups must be s.matchupMaps(state, live.Matchups) — the same slice,
// in the same order, so index alignment with live.Matchups holds. pool is
// read once (s.pool(), which takes poolMu) for the whole render and
// threaded down to every starterProjections call this function and
// featuredMatchupMap make, rather than each call taking the pool itself
// (round-2 review of commit 133d1d7, finding 3).
func (s *Service) featuredMatchupViews(state PersistedState, live LiveSnapshot, matchups []map[string]any, teamID string, viewedWeek, lockWeek int) (map[string]any, []map[string]any) {
	status, hasLive := s.liveStatus()
	pool := s.pool()
	index, isViewer := featuredMatchupIndex(live.Matchups, teamID)
	other := make([]map[string]any, 0, len(matchups))
	for i, entry := range matchups {
		if i == index {
			continue
		}
		if i < len(live.Matchups) {
			m := live.Matchups[i]
			awayProjected := projectedTotal(m.Away.StarterLedger, starterProjections(m.Away.StarterLedger, pool.byID), status, hasLive)
			homeProjected := projectedTotal(m.Home.StarterLedger, starterProjections(m.Home.StarterLedger, pool.byID), status, hasLive)
			// hasProjectableStarters, not ScoreKnown (wave-8 audit item 2):
			// see the LiveScoresView call site above.
			entry["projected_away"] = projectedText(awayProjected, hasProjectableStarters(m.Away.StarterLedger))
			entry["projected_home"] = projectedText(homeProjected, hasProjectableStarters(m.Home.StarterLedger))
			combined := append(append([]StarterLedgerRow{}, m.Away.StarterLedger...), m.Home.StarterLedger...)
			// still_to_play/still_to_play_total are the bare count and the
			// total, both plain ints: the page composes "N of M starters
			// still to play" itself, the same shape my_matchup's own two
			// fields take (round-2 review of commit 133d1d7, finding 2 —
			// three different shapes for one figure across the two view
			// models and the live-bind map was the bug).
			entry["still_to_play"] = stillToPlay(combined, status)
			entry["still_to_play_total"] = len(combined)
			// Every scorebug's own expandable body carries the same
			// per-slot starter pairs the featured card renders (Task 11b's
			// Scorebug body is a ul.matchup-pairs of StarterCell, same as
			// FeaturedMatchup's).
			entry["pairs"] = featuredStarterPairs(m.Away.StarterLedger, m.Home.StarterLedger)
		}
		other = append(other, entry)
	}
	if index < 0 {
		return emptyFeaturedMatchup(), other
	}
	return s.featuredMatchupMap(state, live.Matchups[index], isViewer, teamID, viewedWeek, lockWeek, status, hasLive, pool.byID), other
}

// emptyFeaturedMatchup is my_matchup's shape when the week has no
// matchups at all (no schedule yet, or a bye with nobody else scheduled).
func emptyFeaturedMatchup() map[string]any {
	return map[string]any{
		"has_matchup": false, "is_viewer": false, "id": "", "label": "",
		"live_indicator": "", "live_state": "", "win_prob": "", "win_prob_width": "0%",
		"still_to_play": 0, "still_to_play_total": 0,
		"next_lineup_href": "", "next_week": 0, "has_next_week": false,
		"mine": map[string]any{}, "theirs": map[string]any{}, "pairs": []map[string]any{},
	}
}

// featuredMatchupMap renders my_matchup for one resolved ScoreMatchup.
// mine/theirs follow the viewer's own side when isViewer holds; the
// FEATURED (non-viewer) case has no "own side" to follow, so it labels
// Home "mine" and Away "theirs" — an arbitrary but stable choice, since
// nothing distinguishes the two sides for a spectator. byID is the
// caller's single s.pool().byID read for the whole render (see
// featuredMatchupViews).
func (s *Service) featuredMatchupMap(state PersistedState, m ScoreMatchup, isViewer bool, teamID string, viewedWeek, lockWeek int, status LiveStatus, hasLive bool, byID map[string]Player) map[string]any {
	mine, theirs := m.Home, m.Away
	if isViewer && m.Away.ID == teamID {
		mine, theirs = m.Away, m.Home
	}
	mineProjected := projectedTotal(mine.StarterLedger, starterProjections(mine.StarterLedger, byID), status, hasLive)
	theirsProjected := projectedTotal(theirs.StarterLedger, starterProjections(theirs.StarterLedger, byID), status, hasLive)
	// hasProjectableStarters, not ScoreKnown (wave-8 audit item 2): see the
	// LiveScoresView call site above.
	winProbText := winProbabilityText(mineProjected, theirsProjected, hasProjectableStarters(mine.StarterLedger), hasProjectableStarters(theirs.StarterLedger))
	// win_prob_width is a literal CSS width set once at render (not
	// live-bound, page.gsx's comment beside the bar), so it can never hold
	// the "—" placeholder winProbText renders for an unknown side; "0%"
	// is the same safe fallback emptyFeaturedMatchup already uses.
	winProbWidth := winProbText
	if winProbWidth == winProbabilityDashText {
		winProbWidth = "0%"
	}
	combined := append(append([]StarterLedgerRow{}, mine.StarterLedger...), theirs.StarterLedger...)
	label := ""
	if !isViewer {
		label = "FEATURED"
	}
	// next_week/has_next_week target the VIEWED week (viewedWeek) when its
	// own lineup slots are still editable — not yet kicked off, the same
	// lockWeek authority /team's own week selector uses
	// (lineupCurrentWeekAt, teamWeekOptions) — or lockWeek itself (the
	// next actually-editable week) when the viewed week has already
	// closed. Both are then clamped to the schedule's own last week — a
	// week that was never generated once the season is on its final week
	// (round-2 review of commit 133d1d7, finding 4).
	//
	// Item 7 (2026-08-31 post-wave audit): this used to be a flat
	// currentWeek+1 — currentWeek here was MatchupsData's own
	// currentScheduleWeek (a SCORING-finality concept, the first
	// not-all-final week), not lineupCurrentWeekAt's LOCK concept, and it
	// never read viewedWeek at all. A manager browsing /matchups?week=1
	// while the site's scoring-current week sat at 2 always saw "Set
	// lineup for Week 2," even when Week 1's own slots were the ones
	// still open.
	nextWeek := viewedWeek
	if viewedWeek < lockWeek {
		nextWeek = lockWeek
	}
	hasNextWeek := state.Schedule != nil
	if state.Schedule != nil {
		if weeks := seasonScheduleWeeks(*state.Schedule); len(weeks) > 0 && nextWeek > weeks[len(weeks)-1] {
			nextWeek = weeks[len(weeks)-1]
			hasNextWeek = false
		}
	}
	return map[string]any{
		"has_matchup":         true,
		"is_viewer":           isViewer,
		"id":                  m.ID,
		"label":               label,
		"live_indicator":      liveIndicatorToken(m.State),
		"live_state":          m.LiveState,
		"win_prob":            winProbText,
		"win_prob_width":      winProbWidth,
		"still_to_play":       stillToPlay(combined, status),
		"still_to_play_total": len(combined),
		"next_lineup_href":    fmt.Sprintf("/team?week=%d#lineup", nextWeek),
		"next_week":           nextWeek,
		"has_next_week":       hasNextWeek,
		"mine":                s.featuredTeamMap(state, mine, mineProjected),
		"theirs":              s.featuredTeamMap(state, theirs, theirsProjected),
		"pairs":               featuredStarterPairs(mine.StarterLedger, theirs.StarterLedger),
	}
}

// featuredTeamMap is my_matchup's mine/theirs team shape.
func (s *Service) featuredTeamMap(state PersistedState, side ScoreTeam, projected float64) map[string]any {
	team := s.teamView(state, side.ID)
	_, hasImage, avatarURL := s.avatarView(team.ID, team.Tone)
	return map[string]any{
		"id": side.ID, "name": side.Name, "manager": team.Manager, "record": team.Record,
		// hasProjectableStarters, not ScoreKnown (wave-8 audit item 2): see
		// the LiveScoresView call site above.
		"score": matchupScoreText(side), "projected": projectedText(projected, hasProjectableStarters(side.StarterLedger)),
		"tone": team.Tone, "abbreviation": side.Abbreviation,
		"has_avatar_image": hasImage, "avatar_image_url": avatarURL,
	}
}

// featuredStarterPairs zips both sides' starter-ledger rows one per
// lineupSlots(CurrentRoster()) entry, in the fixed engine slot order both
// sides' rows already share (matchupLineup resolves every team through
// the same lineupSlots(CurrentRoster()) walk). A side missing a row for a
// slot (should not happen — every configured slot always gets a row, even
// an empty one) renders that side of the pair as nil rather than
// panicking on an index a differently-shaped roster produced.
func featuredStarterPairs(mineRows, theirsRows []StarterLedgerRow) []map[string]any {
	mine := starterLedgerMaps(mineRows)
	theirs := starterLedgerMaps(theirsRows)
	slots := lineupSlots(CurrentRoster())
	out := make([]map[string]any, 0, len(slots))
	for i := range slots {
		var mineRow, theirsRow map[string]any
		if i < len(mine) {
			mineRow = mine[i]
		}
		if i < len(theirs) {
			theirsRow = theirs[i]
		}
		slot := ""
		switch {
		case mineRow != nil:
			slot, _ = mineRow["slot"].(string)
		case theirsRow != nil:
			slot, _ = theirsRow["slot"].(string)
		}
		out = append(out, map[string]any{"slot": slot, "mine": mineRow, "theirs": theirsRow})
	}
	return out
}

// matchupStatusLine renders MatchupsData's status_line: the one-sentence
// A5/A6 status the page shows in place of the old provenance table, plus
// the three raw fields (live_state, checked_at, games_final) a strict
// GSX component needs individually rather than pre-joined into prose.
func (s *Service) matchupStatusLine(live LiveSnapshot) map[string]any {
	checkedAt := live.CheckedAt
	if checkedAt.IsZero() {
		checkedAt = live.LastUpdated
	}
	return map[string]any{
		"live_state":       live.LiveState,
		"source_line":      live.SourceLine,
		"checked_at":       s.formatMatchupUpdateOrUnavailable(checkedAt),
		"stats_updated_at": s.formatMatchupUpdateOrUnavailable(live.StatsUpdatedAt),
		"games_final":      live.GamesFinal,
	}
}

func (s *Service) teamByID(id string) Team {
	return s.teamView(s.store.Snapshot(), id)
}

// TeamLabel resolves a team ID to its display name for member-facing copy
// (sign-in flashes, admin notices), reusing teamByID's lookup. It falls
// back to the raw ID only when no team matches it, so a stale or unknown
// ID never renders as an empty string.
func (s *Service) TeamLabel(id string) string {
	if id == "" {
		return id
	}
	team := s.teamByID(id)
	if team.ID != id {
		return id
	}
	return team.Name
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

// blankTeamMap carries the same keys as teamMap, every one at its zero
// value: the command bar's on-clock badge and its next/after-next labels
// (next_team.name, after_next_team.name) read straight off whichever of
// the two a request gets, with no separate has-a-team branch in the
// template, so a missing key here (rather than an empty match) would read
// back as a template error instead of the intended blank/neutral display.
func blankTeamMap() map[string]any {
	return map[string]any{
		"id": "", "name": "", "abbreviation": "", "division": "",
		"manager": "", "claimed": false, "record": "", "points_for": "",
		"rank": "", "rank_number": 0, "streak": 0, "tone": "",
		"has_avatar": false, "has_avatar_image": false, "avatar_image_url": "",
	}
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

// histScoringLabel is the qualifier every rendered Hist line carries
// (generalized-punter-pattern work): the embedded punter rescoring
// (punters_hist.go) and the computed QB/RB/WR/TE/K season line (main.go's
// seasonHouseHistSource) are both this league's own house-scored total,
// never a generic market or nflverse-raw number. A player with no Hist
// line carries no qualifier either — see playerMap, which gates both on
// the same player.Hist != "" check.
const histScoringLabel = "Scored under this league's own rules"

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
//
// drafted is an optional (variadic, so every existing call site keeps
// compiling unchanged) pickByPlayer lookup — draftedByPlayerID's own
// return shape (draft_history.go) — keyed by player ID. Passing it adds
// is_drafted/drafted_round/drafted_pick/drafted_label ("R3 · P28") for a
// player this league's own draft already selected; a caller that never
// needs the round/pick chip (the lineup, board, and blitz pool renders)
// omits it and still gets those four keys, always present with their
// zero values, so a template can read player.drafted_label unconditionally
// with no missing-key branch (wave 7, item 2). Only drafted[0] is read —
// a second argument is never meaningful, but the variadic shape avoids
// forcing a nil at every one of playerMap's nine pre-existing call sites.
func playerMap(player Player, scoringValues map[string]float64, matchup matchupIndex, drafted ...map[string]DraftPick) map[string]any {
	// ADPRank (real market ADP) always wins when present. Punters carry no
	// market ADP at all (blitz.go's ADPRank>0 market-ADP signal), so their
	// positional rank — PunterRank, from the league's own embedded 2025
	// rescoring (internal/fantasy's pool build) — renders as "P##" in its
	// place. A punter PunterRank missed (the embedded projection lookup
	// had no match) falls through to the same "—" every other unranked
	// player gets.
	rank := "—"
	switch {
	case player.ADPRank > 0:
		rank = fmt.Sprintf("%03d", player.ADPRank)
	case player.PunterRank > 0:
		rank = fmt.Sprintf("P%02d", player.PunterRank)
	}
	// houseRank is the display value for the SEPARATE house-rank column
	// (houserank.go): the format-aware VORP rank under the league's active
	// roster preset, shown beside the market rank above, never instead of
	// it. "H%03d" for a ranked player (HouseRank > 0) — three digits, so
	// the column holds a stable width once a deep pool's rank reaches
	// three digits (matching the market rank's own "%03d" above); empty
	// for one with no house rank (a zero-Projection player, or one
	// CurrentRoster's demand model never reaches — see houseRanks' doc
	// comment).
	//
	// DST fallback (sumac comb re-audit item 6): applyHouseRanks only
	// ranks a positive-Projection player (houserank.go), and a real
	// week's Tank01 feed sometimes carries no weekly DST projection at
	// all (a data-source gap, not a roster/format one — the VORP model
	// itself is fine), leaving HouseRank at its zero default. Before
	// this fallback, the /team row's house-rank chip simply skipped
	// (HasHouseRank false, app/team/page.gsx's RosterRow), so the next
	// visible text in the row — the missing-headshot avatar's own team
	// code (.player-avatar, DST never carries a headshot) — sat where a
	// manager expected a rank, reading as if the chip itself had
	// fallen back to the team code. Market ADP still orders defenses
	// even when the weekly VORP model has nothing to say, so a DST with
	// no house rank falls back to its ADP rank in the identical
	// "H%03d" shape — same column width, same chip, sourced from ADP
	// only when VORP genuinely has no signal.
	houseRank := ""
	switch {
	case player.HouseRank > 0:
		houseRank = fmt.Sprintf("H%03d", player.HouseRank)
	case player.Position == "DST" && player.ADPRank > 0:
		houseRank = fmt.Sprintf("H%03d", player.ADPRank)
	}
	// detail is the pool/board row's own ONE-LINE summary — team, bye,
	// injury only (wave 8 hotfix, item 1: "the news snippet is enlarging
	// the cell and making it crazy"). player.News used to fold in here
	// too; a real Tank01 headline runs 150-300 characters (the harness's
	// offline pool's News strings are short, so this never showed up
	// there), several lines longer than the row was ever laid out for.
	// The headline is still carried below under "news"/"has_news" — every
	// caller renders it inside its own stat-tip panel instead (a tap
	// away), never back into this line. See public/styles.css's
	// ".pool-player__text small" clamp for the belt-and-braces guard: no
	// future caller stuffing a long string into an already-short "detail"
	// can balloon a row again either.
	detail := player.NFLTeam
	if player.ByeWeek > 0 {
		detail += fmt.Sprintf(" · BYE %d", player.ByeWeek)
	}
	// detailTeamBye (comb — oleander, item 8): team/bye only, no injury —
	// /players' own phone-width row (app/players/page.gsx) uses this one
	// instead of "detail" below for its single visible meta line. A real
	// injury designation ("Questionable - Ankle") pushed "detail" past
	// one line on real data (the harness's own short/empty Injury
	// strings never showed this), re-growing the row past the compact
	// card's own height budget even after the news headline's own
	// removal (the "wave 8 hotfix" comment below). Injury itself is not
	// dropped — has_injury/injury (below) already exist as an
	// independent exposure of the same value; page.gsx now also renders
	// it inside the primary stat-tip panel (unlike "news," reachable
	// with no headline required), not only inline in the row.
	detailTeamBye := detail
	if player.Injury != "" {
		detail += " · " + player.Injury
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
	histLabel := ""
	if player.Hist != "" {
		histLabel = histScoringLabel
	}
	out := map[string]any{
		"id": player.ID, "name": player.Name, "position": player.Position, "nfl_team": player.NFLTeam,
		"projection": fmt.Sprintf("%.1f", player.Projection),
		"points":     fmt.Sprintf("%.1f", player.Points), "status": player.Status, "news": player.News,
		"has_news": player.News != "",
		// injury/has_injury back the news stat-tip's own secondary line
		// (wave 8 hotfix, item 1 design revision — "the injury note if
		// any" alongside the headline): detail (above) already folds the
		// same Injury string into its own team/bye/injury summary, so
		// this is a second, independent exposure of the identical value
		// for the news panel to render beside the headline, not a new
		// source of truth.
		"injury": player.Injury, "has_injury": player.Injury != "",
		"rank": rank, "house_rank": houseRank, "has_house_rank": houseRank != "", "detail": detail,
		"detail_team_bye": detailTeamBye,
		"headshot":        player.Headshot, "has_headshot": player.Headshot != "",
		"jersey":          jersey,
		"has_breakdown":   hasBreakdown,
		"breakdown":       breakdownRows,
		"breakdown_total": breakdownTotal,
		"has_hist":        player.Hist != "",
		"hist":            player.Hist,
		"hist_label":      histLabel,
		"search":          playerSearchText(player),
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
		// is_drafted/drafted_round/drafted_pick/drafted_label back the /players
		// owner chip and any other "which pick landed this player" surface
		// (wave 7, item 2): this league's OWN fantasy draft, never the NFL
		// draft capital above. Zero values (false/0/0/"") unless a caller
		// passed drafted and this player's ID is a key in it.
		"is_drafted":    false,
		"drafted_round": 0,
		"drafted_pick":  0,
		"drafted_label": "",
	}
	if len(drafted) > 0 {
		if pick, ok := drafted[0][player.ID]; ok {
			out["is_drafted"] = true
			out["drafted_round"] = pick.Round
			out["drafted_pick"] = pick.Number
			out["drafted_label"] = fmt.Sprintf("R%d · P%d", pick.Round, pick.Number)
		}
	}
	for k, v := range matchup.fields(player) {
		out[k] = v
	}
	return out
}

// playerMapsWithScoring renders many players' view models against one
// already-resolved scoringValues map and matchupIndex, so a page with
// hundreds of players pays for one store snapshot and one schedule scan,
// not one per player. See currentScoringValues and matchupIndexFor.
// drafted is the same optional, single-value variadic playerMap itself
// accepts (draftedByPlayerID's league-wide playerID->DraftPick lookup) —
// forwarded through unchanged so a many-players caller (wave 7's /team
// bench, item 5) gets is_drafted/drafted_round/drafted_pick/
// drafted_label on every row without a second, locally reinvented
// lookup. Omitting it (every pre-existing caller) renders those four
// fields at their honest zero value, exactly as playerMap's own doc
// comment already promises.
func playerMapsWithScoring(players []Player, scoringValues map[string]float64, matchup matchupIndex, drafted ...map[string]DraftPick) []map[string]any {
	out := make([]map[string]any, 0, len(players))
	for _, player := range players {
		out = append(out, playerMap(player, scoringValues, matchup, drafted...))
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

// activityActorClassCommissioner marks a feed row as a commissioner-actor
// event ("kind") rather than an ordinary team roster move — /activity's
// template uses this to render the "COMMISSIONER · <name> <summary>"
// actor-class row instead of the usual team/action/player row (wave-2
// commissioner console: commissioner actions had no durable record and no
// distinct feed presence).
const activityActorClassCommissioner = "commissioner"

// activityMaps merges Picks, Transactions, and CommissionerEvents into one
// time-sorted feed, newest first (roster-ops spec section 7.2: "the feed
// composes at read time" — this replaces the former transactionMaps()
// stub; CommissionerEvents joined in for the wave-2 commissioner-console
// audit trail). limit caps the returned row count; zero means unlimited.
// Draft-pick lines resolve the player's live pool identity at read time;
// transaction lines render the TransactionPlayer identity snapshotted at
// commit time (section 7.1), so both survive pool churn. DashboardData
// calls this with limit 5 for its panel; ActivityData (the /activity
// page) calls it with 0.
func (s *Service) activityMaps(state PersistedState, limit int) []map[string]any {
	pool := s.pool()
	type entry struct {
		at         time.Time
		teamIDs    []string
		action     string
		player     string
		kind       string // "" for an ordinary team move, activityActorClassCommissioner for a commissioner event
		actorName  string
		actorEmail string
	}
	entries := make([]entry, 0, len(state.Picks)+len(state.Transactions)+len(state.CommissionerEvents))
	for _, pick := range state.Picks {
		label := pick.PlayerID
		if player, ok := pool.byID[pick.PlayerID]; ok {
			label = fmt.Sprintf("%s (%s)", player.Name, player.Position)
		}
		// " — R1 · P1" (wave 7, item 2): the round/pick this activity row's
		// own DraftPick already carries, appended to the same player label
		// every other row's "player" field already holds — activityLine
		// (adds/drops/trades) never sets this suffix, so it stays specific
		// to a draft pick's own row.
		label = fmt.Sprintf("%s — R%d · P%d", label, pick.Round, pick.Number)
		entries = append(entries, entry{at: pick.MadeAt, teamIDs: []string{pick.TeamID}, action: "drafts", player: label})
	}
	for _, txn := range state.Transactions {
		action, player := activityLine(txn)
		entries = append(entries, entry{at: txn.At, teamIDs: activityTeamIDs(txn), action: action, player: player})
	}
	for _, event := range state.CommissionerEvents {
		teamIDs := []string{}
		if event.Refs.TeamID != "" {
			teamIDs = []string{event.Refs.TeamID}
		}
		player := ""
		if event.Refs.PlayerID != "" {
			player = event.Refs.PlayerID
			if p, ok := pool.byID[event.Refs.PlayerID]; ok {
				player = fmt.Sprintf("%s (%s)", p.Name, p.Position)
			}
		}
		entries = append(entries, entry{
			at: event.At, teamIDs: teamIDs, action: event.Summary, player: player,
			kind: activityActorClassCommissioner, actorName: event.ActorName, actorEmail: event.ActorEmail,
		})
	}
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].at.After(entries[j].at) })
	if limit > 0 && len(entries) > limit {
		entries = entries[:limit]
	}
	location := s.matchupLocation()
	now := s.clock()
	out := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		teamDisplay, teamAbbreviations, teamNames := s.activityTeamDisplay(state, e.teamIDs)
		teamSearch := append(append([]string{}, teamAbbreviations...), teamNames...)
		teamSearch = append(teamSearch, e.teamIDs...)
		if e.kind == activityActorClassCommissioner {
			// A commissioner event is attributed to the PERSON, not a team
			// or seat code (wave-2 audit): the "team" column — the row's
			// leading label — becomes the actor's own display name, with
			// "actor_class" carrying the distinct "COMMISSIONER" marker the
			// template renders ahead of it.
			teamDisplay = e.actorName
			teamSearch = append(teamSearch, "commissioner", e.actorName, e.actorEmail)
		}
		out = append(out, map[string]any{
			"time":                  e.at.In(location).Format("Jan 2, 3:04 PM MST"),
			"time_iso":              formatClockInstant(e.at),
			"time_relative":         relativeTime(now, e.at),
			"timezone":              FriendlyTimezoneLabel(location.String()),
			"team":                  teamDisplay,
			"teams":                 teamAbbreviations,
			"team_names":            teamNames,
			"team_ids":              e.teamIDs,
			"team_search":           strings.Join(teamSearch, " "),
			"action":                e.action,
			"player":                e.player,
			"kind":                  e.kind,
			"actor_class":           activityActorClassLabel(e.kind),
			"actor_name":            e.actorName,
			"is_commissioner_event": e.kind == activityActorClassCommissioner,
		})
	}
	return out
}

// activityActorClassLabel renders one feed row's actor-class marker. Every
// ordinary team roster move renders "" (the template shows no marker, the
// existing "TEAM ↔ TEAM" row); a commissioner event renders "COMMISSIONER".
func activityActorClassLabel(kind string) string {
	if kind == activityActorClassCommissioner {
		return "COMMISSIONER"
	}
	return ""
}

// activityTeamDisplay returns the parties represented by one feed entry.
// Draft picks and ordinary roster moves pass one team ID; a trade passes both
// sides so the one activity row remains truthful without duplicating the
// transaction in the feed. The composed label carries the team NAME with its
// code as a parenthetical secondary label ("Eastside Elite (E1)"), never the
// code alone (2026-09-01 audit: the feed read "E1 signs Tre Harris" with no
// team name anywhere on the row, even though this function already returned
// the name in its own third value — the template just never rendered it).
func (s *Service) activityTeamDisplay(state PersistedState, teamIDs []string) (string, []string, []string) {
	labels := make([]string, 0, len(teamIDs))
	abbreviations := make([]string, 0, len(teamIDs))
	names := make([]string, 0, len(teamIDs))
	seen := make(map[string]struct{}, len(teamIDs))
	for _, teamID := range teamIDs {
		if teamID == "" {
			continue
		}
		if _, ok := seen[teamID]; ok {
			continue
		}
		seen[teamID] = struct{}{}
		team := s.teamView(state, teamID)
		abbreviation := team.Abbreviation
		if abbreviation == "" {
			abbreviation = teamID
		}
		label := team.Name
		switch {
		case team.Name != "" && team.Abbreviation != "":
			label = team.Name + " (" + team.Abbreviation + ")"
		case team.Name == "":
			label = abbreviation
		}
		labels = append(labels, label)
		if team.Abbreviation != "" {
			abbreviations = append(abbreviations, team.Abbreviation)
		}
		if team.Name != "" {
			names = append(names, team.Name)
		}
	}
	return strings.Join(labels, " ↔ "), abbreviations, names
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

// draftSeatCounts is the readiness summary shown in the Draft Room. The
// configured team list describes the league topology, not the set of people
// who can check in: an open franchise must not make the UI claim a manager is
// "not ready" or inflate the readiness denominator. Demo mode's synthetic
// rehearsal seat is included as a claimed seat so its summary matches the
// visible REHEARSAL SEAT card.
func (s *Service) draftSeatCounts(state PersistedState) (ready, managers int) {
	claimed := claimedSeatIDs(state.Members)
	if s.demoMode && len(s.Teams()) > 0 {
		claimed[s.Teams()[0].ID] = true
	}
	for _, team := range s.Teams() {
		if !claimed[team.ID] {
			continue
		}
		managers++
		if state.Ready[team.ID] {
			ready++
		}
	}
	return ready, managers
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

// ActionCenterData assembles the authenticated manager's prioritized,
// read-only home action center from the same authorities used by the
// destination pages. It intentionally has no route actions or POST handlers.
func (s *Service) ActionCenterData(r *http.Request) map[string]any {
	now := s.clock()
	viewer := s.Viewer(r)
	games := s.schedule()
	_ = s.store.ReconcilePickemMarkets(now, games, nil)
	_ = s.store.BackfillPickemEnteredAt(games)
	state := s.store.Snapshot()
	pickemHome := s.pickemHomeSummaryFromSnapshot(r, state, now)
	return s.actionCenterDataForSnapshot(r, state, viewer, pickemHome, now)
}

func (s *Service) actionCenterDataForSnapshot(r *http.Request, state PersistedState, viewer map[string]any, pickemHome map[string]any, now time.Time) map[string]any {
	entry := s.publicEntryViewForViewerState(r, viewer, state)
	hasSeat, _ := viewer["has_seat"].(bool)
	teamID, _ := viewer["team_id"].(string)
	complete := draftComplete(state)
	seatCapacity := len(s.Teams())
	claimedSeats := claimedSeatCount(state.Members)
	claimedTeams := make(map[string]bool, claimedSeats)
	for _, member := range state.Members {
		if strings.TrimSpace(member.TeamID) != "" {
			claimedTeams[member.TeamID] = true
		}
	}
	readySeats := 0
	for teamID := range claimedTeams {
		if state.Ready[teamID] {
			readySeats++
		}
	}
	poolCount := len(s.pool().players)
	poolTarget := seatCapacity * CurrentDraftRounds()
	facts := ActionCenterFacts{
		Now: now, Location: s.draftTZ, EntryState: entry.State,
		EntryStateLabel: entry.StateLabel, EntryHeadline: entry.Headline,
		EntryActionHref: entry.ActionHref, EntryActionLabel: entry.ActionLabel,
		EntryDetail: entry.Detail, Admitted: entry.Admitted, HasSeat: hasSeat,
		TeamID: teamID, TeamName: entry.TeamName, Commissioner: entry.IsCommissioner,
		DraftStarted: state.DraftStarted, DraftComplete: complete,
		DraftAt: s.EffectiveDraftAt(state),
		Ready:   state.Ready[teamID], SeasonPhase: s.SeasonPhase(now),
		SeatCapacity: seatCapacity, ClaimedSeats: claimedSeats, ReadySeats: readySeats,
		DraftOrderSet: len(state.DraftOrder) > 0, DraftPoolCount: poolCount,
		DraftPoolTarget: poolTarget,
		ScheduleExists:  state.Schedule != nil,
		Pickem:          actionCenterPickemFacts(pickemHome),
	}
	if state.Schedule != nil {
		week := currentScheduleWeek(*state.Schedule)
		info := s.AdminWeekCloseInfo(week, now)
		facts.WeekCloseWeek = info.Week
		facts.WeekCloseFinal = info.Final
		facts.WeekCloseReady = info.Ready
		facts.WeekCloseGamesFinal = info.GamesFinal
		facts.WeekCloseGamesTotal = info.GamesTotal
		facts.WeekCloseStatsFresh = info.StatsFresh
		facts.WeekCloseReason = info.Reason
	}
	if facts.Location == nil {
		facts.Location, _ = time.LoadLocation(DefaultDraftTZ)
	}
	if hasSeat && state.DraftStarted && !complete {
		next := len(state.Picks) + 1
		teamCount := len(state.DraftOrder)
		if teamCount == 0 {
			teamCount = seatCapacity
		}
		total := teamCount * CurrentDraftRounds()
		if next <= total {
			onClockID := teamOnClock(state.DraftOrder, next)
			facts.ViewerOnClock = onClockID == teamID
			facts.OnClockTeamName = s.teamByID(onClockID).Name
		}
		facts.ClockDeadline = state.ClockDeadline
		facts.ClockPaused = state.ClockPaused
	}
	if hasSeat {
		facts.BoardCount = len(state.Boards[s.boardKeyForTeam(state, teamID)])
		games := s.schedule()
		week := s.pickemWeek(games, now)
		if complete {
			roster, _ := s.rosterForTeam(state, teamID)
			general, _, _ := splitRosterZones(state, teamID, roster)
			lineup := effectiveLineup(CurrentRoster(), general, state.Lineups[teamID], week, games, now)
			problems := lineupProblems(lineup, games, now)
			first, ok := firstKickoff(games, week)
			facts.Lineup = ActionCenterLineupFacts{Week: week, Problems: len(problems), FirstKickoff: first, HasFirstKickoff: ok}
		}
		for _, offer := range state.TradeOffers {
			switch {
			case offer.Status == TradeStatusOpen && offer.ToTeamID == teamID:
				facts.Trades.IncomingOpen++
			case offer.Status == TradeStatusAccepted && (offer.FromTeamID == teamID || offer.ToTeamID == teamID):
				facts.Trades.AcceptedReview++
				deadline := offer.AcceptedAt.Add(time.Duration(s.cfg.Trades.ReviewHours) * time.Hour)
				if !facts.Trades.HasReviewDeadline || deadline.Before(facts.Trades.NextReviewDeadline) {
					facts.Trades.NextReviewDeadline, facts.Trades.HasReviewDeadline = deadline, true
				}
			case offer.Status == TradeStatusOpen && offer.FromTeamID == teamID:
				facts.Trades.OutgoingOpen++
			}
		}
		facts.Trades.TradeDeadline, facts.Trades.HasTradeDeadline = parseTradeDeadline(s.cfg)
		pool := s.pool()
		runAt := firstRunAtOrAfter(s.cfg, now)
		if !state.WaiversProcessedThrough.IsZero() {
			processedRun := firstRunStrictlyAfter(s.cfg, state.WaiversProcessedThrough)
			if processedRun.After(runAt) {
				runAt = processedRun
			}
		}
		for _, claim := range state.WaiverClaims {
			if claim.TeamID != teamID {
				continue
			}
			facts.Waivers.OpenClaims++
			player := pool.byID[claim.AddID]
			status := playerWaiverStatus(state, s.cfg, games, claim.AddID, player.NFLTeam, now)
			resolveAt := runAt
			if status.State == AvailabilityOnWaivers && !status.ResolvesAt.IsZero() {
				resolveAt = status.ResolvesAt
			}
			if !facts.Waivers.HasNextRun || resolveAt.Before(facts.Waivers.NextRun) {
				facts.Waivers.NextRun, facts.Waivers.HasNextRun = resolveAt, true
			}
		}
	}
	return BuildActionCenter(facts).Data(facts.Location)
}

func actionCenterPickemFacts(raw map[string]any) ActionCenterPickemFacts {
	facts := ActionCenterPickemFacts{
		Week: actionCenterInt(raw, "week"), GameCount: actionCenterInt(raw, "game_count"),
		PickedCount: actionCenterInt(raw, "picked_count"), OpenUnpicked: actionCenterInt(raw, "open_unpicked_count"),
		LockedUnpicked: actionCenterInt(raw, "locked_unpicked_count"),
	}
	if value, ok := raw["next_open_lock_at"].(string); ok && value != "" {
		if at, err := time.Parse(time.RFC3339, value); err == nil {
			facts.NextOpenLock, facts.HasNextOpenLock = at, true
		}
	}
	return facts
}

func actionCenterInt(raw map[string]any, key string) int {
	switch value := raw[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	default:
		return 0
	}
}
