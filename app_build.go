package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"html"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	activitypage "gridiron-2000/app/activity"
	adminpage "gridiron-2000/app/admin"
	commissionerpage "gridiron-2000/app/commissioner"
	draftpage "gridiron-2000/app/draft"
	lockerpage "gridiron-2000/app/locker"
	matchupspage "gridiron-2000/app/matchups"
	pickempage "gridiron-2000/app/pickem"
	playerspage "gridiron-2000/app/players"
	teampage "gridiron-2000/app/team"
	tradespage "gridiron-2000/app/trades"
	wirepage "gridiron-2000/app/wire"
	"gridiron-2000/internal/commissionerhq"
	"gridiron-2000/internal/commissionerhq/v1fleet"
	"gridiron-2000/internal/commissionerhq/v1provider"
	"gridiron-2000/internal/density"
	"gridiron-2000/internal/fantasy"
	"gridiron-2000/internal/league"
	"gridiron-2000/internal/mailer"
	"gridiron-2000/internal/navigation"
	"gridiron-2000/internal/notify"
	"gridiron-2000/internal/openstats"
	"gridiron-2000/internal/wire"
	"m31labs.dev/gosx"
	"m31labs.dev/gosx/auth"
	runtimehost "m31labs.dev/gosx/client/runtime/host"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
	"m31labs.dev/gosx/session"
)

// AppConfig is everything BuildApp needs that main() used to read inline.
type AppConfig struct {
	Root       string
	AppEnv     string
	Port       string
	SessionKey string
	TestAuth   bool   // GRIDIRON_TEST_AUTH=1 outside production
	TestPool   string // GRIDIRON_TEST_POOL: "" or "offline-live"
}

// harnessTestPools lists every accepted GRIDIRON_TEST_POOL value. An empty
// value keeps the real fantasy pool.
var harnessTestPools = map[string]bool{"": true, "offline-live": true}

// validate refuses a configuration that arms a harness switch outside a
// local environment. The rule is an allow-list, not a "production" match:
// APP_ENV=prod, APP_ENV=staging, and every unknown label are deployments,
// so a leaked flag cannot open a live league. BuildApp calls this too, so a
// hand-built AppConfig obeys the same rule as an environment-read one.
func (cfg AppConfig) validate() error {
	if !harnessTestPools[cfg.TestPool] {
		return errors.New("GRIDIRON_TEST_POOL must be empty or offline-live")
	}
	if (cfg.TestAuth || cfg.TestPool != "") && !isLocalAppEnv(cfg.AppEnv) {
		return errors.New("GRIDIRON_TEST_AUTH and GRIDIRON_TEST_POOL are refused outside a local APP_ENV")
	}
	return nil
}

// AppConfigFromEnv reads the process environment. It refuses the harness
// switches outside a local environment so a leaked flag cannot open a live
// league.
func AppConfigFromEnv() (AppConfig, error) {
	_, thisFile, _, _ := runtime.Caller(0)
	cfg := AppConfig{
		Root:       server.ResolveAppRoot(thisFile),
		AppEnv:     strings.TrimSpace(os.Getenv("APP_ENV")),
		Port:       getenv("PORT", "8080"),
		SessionKey: getenv("SESSION_SECRET", "gridiron-2000-local-session-secret-change-me"),
		TestPool:   strings.TrimSpace(os.Getenv("GRIDIRON_TEST_POOL")),
		TestAuth:   os.Getenv("GRIDIRON_TEST_AUTH") == "1",
	}
	if err := cfg.validate(); err != nil {
		return cfg, err
	}
	return cfg, nil
}

// AppRuntime owns the background loops BuildApp wired but did not start,
// plus the few values main()'s startup logs and shutdown path still need.
// One process gets one AppRuntime: the loops it owns drive league.Default(),
// a process singleton, so a second draft clock or roster-ops loop would be a
// bug, not extra capacity.
type AppRuntime struct {
	starters  []func(ctx context.Context)
	startOnce sync.Once
	closeOnce sync.Once
	// wg is joined by any starter whose background goroutine Close should
	// actually wait for, rather than merely fire and forget — today, only
	// the live-scoring poller (live_scoring.go's buildLiveScoring). Every
	// other rt.starters entry keeps its pre-existing semantics: it stops
	// when the ctx passed to Start is canceled, but Close does not wait
	// for it (round-2 review of commit cdeb7f2, finding 4 — a deliberate,
	// narrower change, not a general join-everything policy).
	wg         sync.WaitGroup
	StopNotify context.CancelFunc
	Drain      func(timeout time.Duration) int
	AppName    string
	Port       string
	HQV1       *commissionerHQV1Runtime
	Live       *liveScoringRuntime
	// closers runs inside Close, after restoreClock: today only the
	// replay server's httptest.Server (live_scoring.go's
	// liveScoringInputs), so LIVE_REPLAY_FIXTURE demo mode stops its
	// listener on shutdown instead of leaking it for the process
	// lifetime.
	closers      []func()
	restoreClock func() // nil unless cfg.TestAuth mounted the harness clock override
}

// Start runs every background loop BuildApp registered, in the order main()
// used to start them. A second call does nothing.
func (r *AppRuntime) Start(ctx context.Context) {
	r.startOnce.Do(func() {
		for _, start := range r.starters {
			start(ctx)
		}
	})
}

// closeWaitTimeout bounds Close's wait on r.wg (round-2 review of commit
// cdeb7f2, finding 4): Close must never hang a shutdown path indefinitely
// because one background goroutine is slow to notice its context was
// canceled.
const closeWaitTimeout = 5 * time.Second

// Close releases the resources BuildApp acquired outside the normal
// request/response path, and waits — up to closeWaitTimeout — for every
// goroutine registered on r.wg to actually return. Close does not cancel
// any context itself; that is still the caller's job before calling Close
// (main.go cancels runtimeContext via signal.NotifyContext; a test calls
// its own cancel()), exactly as it always has been for every rt.starters
// entry. This only keeps Close from returning while a goroutine it knows
// about is still unwinding, so a caller that already canceled the context
// and immediately calls Close does not race that goroutine's own cleanup.
// A goroutine still running past closeWaitTimeout is logged, not killed —
// Go has no way to force one to stop, so hitting that log line means a
// real shutdown-ordering bug to fix, not something Close can paper over.
// Close also restores the harness clock override mountTestRoutes may have
// installed: lazy (only the first /test/clock request installs it), so a
// harness build that never exercises /test/clock never touches the
// process-wide league clock at all, and that part is then a no-op. It
// then runs every closer in r.closers (today: a LIVE_REPLAY_FIXTURE
// replay server's httptest.Server.Close), in registration order, after
// the clock restore and before waiting on r.wg. Safe to call more than
// once, and safe to call on every AppRuntime, harness or not — a caller
// does not need to know whether cfg.TestAuth was set, or whether Start
// was ever called, to clean up.
func (r *AppRuntime) Close() {
	if r == nil {
		return
	}
	r.closeOnce.Do(func() {
		if r.restoreClock != nil {
			r.restoreClock()
		}
		league.Default().StopDraftEvents()
		for _, closer := range r.closers {
			closer()
		}
		done := make(chan struct{})
		go func() {
			r.wg.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(closeWaitTimeout):
			log.Printf("AppRuntime.Close: a background goroutine did not stop within %s", closeWaitTimeout)
		}
	})
}

// harnessProvider signs in whoever the X-Test-User header names
// ("email|display name"), registers that identity as a league member on
// first sight, and otherwise falls back to the normal session sign-in so
// the Google callback keeps working. A header without an email falls back
// too: an empty identity must never sign in. Harness only.
//
// Each email carries one sync.Once, so concurrent first requests for the
// same manager make exactly one EnsureMember call and every one of them
// waits for it: no request signs in while its own registration is still
// running. A repeat request takes the map lock only. A failed registration
// logs one line and drops the email from the map, so the next request tries
// again.
func harnessProvider(sessionAuth *auth.Manager, membership interface {
	EnsureMember(email, name string) (league.Member, error)
}) auth.Provider {
	var mu sync.Mutex
	registrations := make(map[string]*sync.Once)
	register := func(email, name string) {
		mu.Lock()
		once, known := registrations[email]
		if !known {
			once = new(sync.Once)
			registrations[email] = once
		}
		mu.Unlock()
		var failure error
		once.Do(func() {
			if _, err := membership.EnsureMember(email, name); err != nil {
				failure = err
			}
		})
		if failure == nil {
			return
		}
		log.Printf("harness auth: register %s failed: %v", email, failure)
		mu.Lock()
		if registrations[email] == once {
			delete(registrations, email)
		}
		mu.Unlock()
	}
	return auth.ProviderFunc(func(r *http.Request) (auth.User, bool) {
		raw := strings.TrimSpace(r.Header.Get("X-Test-User"))
		// isLoopbackRemote (test_routes.go) is the same check every /test/*
		// route applies. Without it here too, a non-loopback caller's
		// X-Test-User would already have registered a member by the time a
		// /test/* route it was headed to got a chance to answer 403 — see
		// isLoopbackRemote's doc comment.
		if raw == "" || !isLoopbackRemote(r) {
			return sessionAuth.Current(r)
		}
		email, name, _ := strings.Cut(raw, "|")
		email = strings.TrimSpace(email)
		if email == "" {
			return sessionAuth.Current(r)
		}
		if name = strings.TrimSpace(name); name == "" {
			name = email
		}
		register(email, name)
		return auth.User{ID: email, Email: email, Name: name}, true
	})
}

// offlinePoolAsLive presents the built-in offline pool as a live one. The
// draft-start readiness check refuses an "offline" pool outside demo mode,
// so a simulated draft needs the same rows under the live label. The field
// mapping mirrors fantasyPlayerSource so an offline row renders like a
// live one.
func offlinePoolAsLive() league.PlayerSource {
	players := fantasy.OfflinePool()
	converted := make([]league.Player, 0, len(players))
	for _, player := range players {
		converted = append(converted, league.Player{
			ID:           player.ID,
			Name:         player.Name,
			Position:     player.Position,
			NFLTeam:      player.NFLTeam,
			ADP:          player.ADP,
			ADPRank:      player.ADPRank,
			ByeWeek:      player.ByeWeek,
			Injury:       player.Injury,
			Headshot:     player.Headshot,
			Jersey:       player.Jersey,
			ProjStats:    player.ProjStats,
			Projection:   player.Projection,
			News:         player.News,
			Status:       "Available",
			Rookie:       player.IsRookie(),
			DraftCapital: player.DraftCapitalLabel(),
			// OfflinePool carries zero Position "P" entries today (see its
			// doc comment), so PunterRank is always zero here — mapped for
			// correctness, matching fantasyPlayerSource (main.go).
			PunterRank: player.PunterRank,
		})
	}
	return func() ([]league.Player, int64, string) {
		return converted, 1, "live"
	}
}

// hashedPublicAssetHref returns name's public URL (see server.AssetURL) with
// a content-hash query appended, computed from name's current bytes under
// root/public. This is deliberately GoSX's own "?v=" content-addressing
// convention, not a literal hashed filename: App.servePublic (see
// m31labs.dev/gosx/server's server.go) already serves any request carrying
// a non-empty "v" query as "Cache-Control: public, max-age=31536000,
// immutable" and leaves every unversioned request under the previous
// "public, max-age=0, must-revalidate" policy, so this href change is the
// entire fix — no routing or header code needs to move out of the vendored
// static handler. A missing or unreadable file falls back to the
// unversioned href so a packaging error degrades to a revalidated
// stylesheet rather than a broken page.
func hashedPublicAssetHref(root, name string) string {
	href := server.AssetURL(name)
	data, err := os.ReadFile(filepath.Join(root, "public", filepath.FromSlash(name)))
	if err != nil {
		return href
	}
	sum := sha256.Sum256(data)
	// 8 hex bytes (32 bits) is ample collision resistance for a single
	// deploy's worth of asset versions and keeps the query string short.
	return href + "?v=" + hex.EncodeToString(sum[:8])
}

// navigationScriptNonceAttr renders nonce as a ` nonce="..."` attribute
// fragment, or an empty string when nonce is empty — the same shape
// GoSX's own unexported server.nonceAttr gives the framework's default
// navigation script (server/navigation.go), reproduced here because that
// helper is not exported and this app's replacement script (see
// router.SetNavigationHead in BuildApp) needs the identical CSP-nonce
// attribute.
func navigationScriptNonceAttr(nonce string) string {
	if nonce == "" {
		return ""
	}
	return ` nonce="` + html.EscapeString(nonce) + `"`
}

// csrfExemptClientEvents wraps protect (sessions.Protect) so it never
// touches GoSX's own auto-mounted telemetry sink,
// server.ClientEventsRoute ("/_gosx/client-events" — see
// registerBuiltinRoutes in m31labs.dev/gosx/server's server.go, which
// mounts it unconditionally unless the app has already registered that
// exact route). ClientEventsHandler (server/client_events.go) only
// forwards each batched client-side event to a slog.Logger, bounded by a
// 64KB body cap and a per-remote-addr rate limit — no session read, no
// state write, nothing a forged cross-origin request could exploit — so
// CSRF protection has nothing to protect there. The bootstrap runtime's
// telemetry beacon (navigator.sendBeacon on visibilitychange, or a
// batched fetch every 2s) never attaches an X-CSRF-Token, so without this
// exemption every page load logged one spurious 403 here.
func csrfExemptClientEvents(protect func(http.Handler) http.Handler) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		protected := protect(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == server.ClientEventsRoute {
				next.ServeHTTP(w, r)
				return
			}
			protected.ServeHTTP(w, r)
		})
	}
}

// csrfFailureMessage is the truthful cause of a CSRF rejection: almost
// always an expired or rotated session, not a hostile forgery attempt.
// Both surfaces below (the shell error page and the managed-action JSON
// body) show the identical sentence.
const csrfFailureMessage = "Your session expired. Reload the page, then try again."

// csrfProtectedRequestMethod mirrors session.csrfProtectedMethod (an
// unexported predicate in m31labs.dev/gosx/session), so this wrapper only
// pays the response-buffering cost this file's own capture below needs on
// the exact same methods session.Manager.Protect itself inspects the CSRF
// token for. Every other method already bypasses Protect's own check and
// is forwarded untouched.
func csrfProtectedRequestMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

// csrfFailureCapture buffers exactly one wrapped handler invocation so
// csrfFailureRenderer can tell a genuine session.Manager.Protect CSRF
// rejection (never reaches its next handler) apart from a downstream
// route's own, unrelated response (which must be forwarded byte-for-byte,
// whatever its status). See csrfFailureRenderer's own doc comment for how
// the two are told apart.
type csrfFailureCapture struct {
	http.ResponseWriter
	header    http.Header
	status    int
	statusSet bool
	body      bytes.Buffer
}

func (c *csrfFailureCapture) Header() http.Header {
	if c.header == nil {
		c.header = make(http.Header)
	}
	return c.header
}

func (c *csrfFailureCapture) WriteHeader(status int) {
	if !c.statusSet {
		c.status = status
		c.statusSet = true
	}
}

func (c *csrfFailureCapture) Write(b []byte) (int, error) {
	if !c.statusSet {
		c.WriteHeader(http.StatusOK)
	}
	return c.body.Write(b)
}

// flush forwards the captured response to the real ResponseWriter
// untouched — the path csrfFailureRenderer takes for every response that
// is not itself the CSRF rejection this file replaces.
func (c *csrfFailureCapture) flush() {
	dst := c.ResponseWriter.Header()
	for key, values := range c.header {
		dst[key] = values
	}
	if c.statusSet {
		c.ResponseWriter.WriteHeader(c.status)
	}
	if c.body.Len() > 0 {
		_, _ = c.ResponseWriter.Write(c.body.Bytes())
	}
}

// csrfFailureRenderer replaces session.Manager.Protect's own CSRF
// rejection — a plain-text (or bare {"error":"invalid csrf token"}) 403
// with no app shell and no message the managed-action runtime's toast
// reads (client/runtime/host/navigation.ts reads a JSON result's
// "message" field, never "error"; without it the toast falls back to its
// generic "Action failed.", giving the visitor no cause and no recovery
// step) — with a truthful, actionable response on both surfaces: the app
// shell error page for a native form submission, and a {"message": ...}
// JSON body the managed runtime's toast can actually show for a managed
// one.
//
// It tells the two 403 sources (Protect's own rejection vs. a downstream
// route's unrelated 403) apart with a "reached" marker around next rather
// than by sniffing the captured status alone: Protect calls next only
// after the CSRF token matches, so a request that never reached next but
// still carries a captured 403 is unambiguously Protect's own rejection —
// every other outcome (next ran at all, or Protect failed some other way,
// e.g. missing session middleware) is forwarded untouched.
func csrfFailureRenderer(protect func(http.Handler) http.Handler, stylesheetHref string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		passthrough := protect(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !csrfProtectedRequestMethod(r.Method) {
				passthrough.ServeHTTP(w, r)
				return
			}
			reached := false
			marker := http.HandlerFunc(func(mw http.ResponseWriter, mr *http.Request) {
				reached = true
				next.ServeHTTP(mw, mr)
			})
			capture := &csrfFailureCapture{ResponseWriter: w}
			protect(marker).ServeHTTP(capture, r)
			if reached || capture.status != http.StatusForbidden {
				capture.flush()
				return
			}
			writeCSRFFailurePage(w, r, stylesheetHref)
		})
	}
}

// wantsManagedActionJSON reports whether r is a managed-action fetch (the
// GoSX runtime always sends Accept: application/json for these) rather
// than a native form submission, mirroring session.requestWantsJSON so
// both surfaces of a CSRF rejection classify a request the same way
// session.Manager.Protect itself already did.
func wantsManagedActionJSON(r *http.Request) bool {
	accept := r.Header.Get("Accept")
	contentType := r.Header.Get("Content-Type")
	return strings.Contains(accept, "application/json") || strings.HasPrefix(contentType, "application/json")
}

// csrfFailureBackTarget names where the shell error page's recovery link
// returns to: the same-origin page the rejected submission came from
// (Referer), sanitized through navigation.SafeReturnPath exactly like
// every other return-path destination in this app, or "/" when the
// Referer is absent, cross-origin, or malformed.
func csrfFailureBackTarget(r *http.Request) string {
	referer := r.Header.Get("Referer")
	if referer == "" {
		return navigation.SafeReturnPath("")
	}
	parsed, err := url.Parse(referer)
	if err != nil || parsed.Host != r.Host {
		return navigation.SafeReturnPath("")
	}
	target := parsed.Path
	if parsed.RawQuery != "" {
		target += "?" + parsed.RawQuery
	}
	return navigation.SafeReturnPath(target)
}

func writeCSRFFailurePage(w http.ResponseWriter, r *http.Request, stylesheetHref string) {
	if wantsManagedActionJSON(r) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":      false,
			"message": csrfFailureMessage,
		})
		return
	}
	back := csrfFailureBackTarget(r)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusForbidden)
	_, _ = io.WriteString(w, `<!DOCTYPE html><html lang="en"><head><meta charset="utf-8">`+
		`<meta name="viewport" content="width=device-width, initial-scale=1">`+
		`<title>Session expired</title>`+
		`<link rel="stylesheet" href="`+html.EscapeString(stylesheetHref)+`"></head>`+
		`<body class="app-shell"><main class="page" id="main-content">`+
		`<div class="error-message" role="alert"><p>`+html.EscapeString(csrfFailureMessage)+`</p></div>`+
		`<p><a class="button button--ghost" href="`+html.EscapeString(back)+`">Reload the page</a></p>`+
		`</main></body></html>`)
}

// BuildApp assembles the HTTP application from cfg. It starts no HTTP server
// and no background loop: every loop lands in the returned AppRuntime, so a
// caller can mount and serve the same wiring main() runs without also
// starting its pollers. It does bind one socket, and only one: when the
// Commissioner HQ v1 provider is configured, its private listener opens
// here, last, so no earlier failure can leave that port held. Build one app
// per process; the loops it registers drive the league.Default() singleton.
func BuildApp(cfg AppConfig) (*server.App, *AppRuntime, error) {
	if err := cfg.validate(); err != nil {
		return nil, nil, err
	}
	rt := &AppRuntime{Port: cfg.Port}
	root := cfg.Root
	signalFeed, err := wire.Default()
	if err != nil {
		return nil, nil, err
	}
	openStats, err := openstats.Default()
	if err != nil {
		return nil, nil, err
	}
	// league.Default() loads league.json (or the neutral built-in default)
	// first, so its team count and roster shape are on hand to scale
	// FANTASY_POOL_LIMIT's own default (owner decision, productization
	// wave: teams × roster spots × headroom, not a flat constant).
	poolLimit := fantasy.ScaledPoolLimit(league.Default().TeamCount(), league.Default().RosterSpots())
	fantasyPool, err := fantasy.Default(poolLimit)
	if err != nil {
		return nil, nil, err
	}
	// Wired before fantasyPool.Start below (and before the cache-loaded
	// pool from fantasy.Default's NewService could otherwise sit rankless
	// until the next sync): league.PunterProjection is the league's own
	// embedded 2025 punter rescoring — Tank01 carries no punter ADP or
	// projections at all. SetPunterProjections also re-normalizes whatever
	// pool NewService already loaded, so a cache boot is never rankless.
	fantasyPool.SetPunterProjections(league.PunterProjection)
	// Also wired before fantasyPool.Start (GC-1 fix 1): the current NFL
	// week, derived from the mirrored nflverse schedule (openStats is
	// already constructed above). SyncNow reads this on every sync, so a
	// week-N sync requests week-N Tank01 projections instead of the old
	// hard-coded week 1.
	fantasyPool.SetCurrentWeek(currentNFLWeekFunc(openStats))
	rt.starters = append(rt.starters, signalFeed.Start, openStats.Start, fantasyPool.Start)
	// The harness may ask for the offline pool relabelled "live" so a
	// simulated draft can start without a live upstream; every other run
	// keeps the real pool adapter.
	if cfg.TestPool == "offline-live" {
		league.Default().SetPlayerSource(offlinePoolAsLive())
	} else {
		league.Default().SetPlayerSource(fantasyPlayerSource(fantasyPool))
	}
	league.Default().SetPoolStatus(fantasyPoolStatus(fantasyPool))
	league.Default().SetScheduleSource(leagueScheduleSource(openStats))
	rt.starters = append(rt.starters, league.Default().StartPickemMarketSync)
	league.Default().SetStatsUpdatedSource(func() time.Time {
		return openStats.Status().PlayerStats.LastUpdated
	})
	league.Default().SetHistoricalSource(seasonHouseHistSource(openStats))
	liveCfg, liveFetcher, replayServer := liveScoringInputs(fantasyPool, league.Default(), rt, cfg.AppEnv)
	// A replay server is its own self-contained relay: it needs no Tank01
	// credentials of its own, so its presence stands in for
	// fantasyPool.Enabled() when deciding whether buildLiveScoring may
	// leave the poller on (see buildLiveScoring's fantasyEnabled doc).
	liveRuntime := buildLiveScoring(liveCfg, liveFetcher, fantasyPool.Enabled() || replayServer != nil, openStats, league.Default(), signalFeed, rt)
	liveRuntime.Replay = replayServer
	rt.Live = liveRuntime
	league.Default().SetInjuryDesignationSource(leagueInjuryDesignationSource(openStats))
	rt.starters = append(rt.starters, func(ctx context.Context) {
		startBlitzPoller(ctx, fantasyPool, league.Default())
	})
	// startBlitzPre1 attaches its loading snapshot before it backgrounds the
	// handful of REST calls against already-final games, so first render can
	// distinguish "checking" from a verified zero-player evidence map.
	rt.starters = append(rt.starters, func(ctx context.Context) {
		startBlitzPre1(ctx, fantasyPool, league.Default())
	})
	// startMatchupRanks computes the matchup-difficulty rank cache (owner
	// ask: "we should see the opponent at a glance" plus a difficulty
	// rank) and keeps it refreshed; it backgrounds itself for the same
	// reason startBlitzPre1 does — see matchup_cache.go.
	rt.starters = append(rt.starters, func(ctx context.Context) {
		go startMatchupRanks(ctx, openStats, league.Default())
	})
	rt.starters = append(rt.starters, league.Default().StartDraftClock)
	leagueFingerprint := func() string {
		_, poolVersion := fantasyPool.Players()
		return league.Default().StateFingerprint(poolVersion)
	}
	draftLiveUpdates := draftpage.NewLiveUpdates(leagueFingerprint)
	draftLiveUpdates.SetRepairView(func() map[string]any { return league.Default().DraftLiveView(nil) })
	draftLiveUpdates.SetDraftEventSink(league.Default())
	rt.starters = append(rt.starters, draftLiveUpdates.Start)
	scoresLive := matchupspage.NewScoresLive(liveRuntime.Poller.Version, leagueFingerprint)
	rt.starters = append(rt.starters, scoresLive.Start)
	// lockerLive (GC-4) runs no Start ticker of its own — see LockerLive's
	// doc comment: a Locker Room mutation is always a synchronous HTTP
	// request, so its own success (SetLockerEventSink's hook) is already
	// the one broadcast trigger, unlike draftLiveUpdates/scoresLive's own
	// external draft-event queue and live-score poller above.
	lockerLive := lockerpage.NewLockerLive(league.Default().LockerVersion)
	league.Default().SetLockerEventSink(lockerLive.Broadcast)
	// StartRosterOps always runs, mail wired or not: waiver processing
	// (and WP-R5's trade execution/expiry) are state mutations, not sends
	// — only the send step at the end of each tick is itself
	// notifyReady-gated (roster-ops spec section 5.4).
	rt.starters = append(rt.starters, league.Default().StartRosterOps)
	notifyMailer := mailer.FromEnv()
	notifyQueue := notify.New(notificationSender(notifyMailer), log.Printf)
	league.Default().SetNotifier(notifyQueue, notifyMailer.Enabled())
	// The notify worker gets its own cancellation, separate from the context
	// AppRuntime.Start receives: on shutdown the HTTP server stops accepting
	// requests immediately, but the worker keeps draining whatever is already
	// queued until it finishes or the drain deadline expires. main() owns
	// both ends of that window — it calls rt.Drain after the server shuts
	// down and rt.StopNotify once the drain returns. A message the ledger
	// already marked "sent" must not be silently abandoned (design spec
	// section 6.3; finding m1).
	notifyContext, stopNotify := context.WithCancel(context.Background())
	rt.StopNotify = stopNotify
	rt.Drain = notifyQueue.Drain
	// Spec section 6.6: without a transport, notifications are disabled
	// across both the delivery queue and every league-side trigger hook.
	// StartNotifier always runs and re-checks the same enabled flag
	// (via notifyReady), so it is the single source of the spec's exact
	// startup log line — no separate log call is needed here.
	if notifyMailer.Enabled() {
		rt.starters = append(rt.starters, func(context.Context) {
			notifyQueue.Start(notifyContext)
		})
	}
	rt.starters = append(rt.starters, league.Default().StartNotifier)

	// The nightly local snapshot loop (BACKUP_ENABLED, default true;
	// BACKUP_KEEP, default 7) — see backup_scheduler.go and
	// docs/backup-restore.md. It only ever writes under the same data
	// directory as league.db; off-host copying remains the operator's job.
	backupCfg := backupSchedulerConfigFromEnv(league.Default().DataDir())
	rt.starters = append(rt.starters, func(ctx context.Context) {
		startBackupScheduler(ctx, league.Default(), backupCfg, appVersion)
	})

	// league.Default() has already resolved APP_NAME (env) over
	// league.name (file) over the neutral built-in default (spec section
	// 3.3 precedence); read the wordmark through it instead of a second,
	// independent getenv call so the two never disagree.
	appName := league.Default().Config().Name
	rt.AppName = appName
	hqConfig, err := commissionerhq.ConfigFromEnv()
	if err != nil {
		return nil, nil, err
	}
	hqService, err := commissionerhq.New(hqConfig, func() commissionerhq.Summary {
		poolStatus := fantasyPool.Status()
		openData := commissionerOpenData(openStats.Status())
		stateSchema := league.Default().StateSchemaCompatibility()
		return league.Default().CommissionerSummary(hqConfig.InstanceID, commissionerhq.Runtime{
			Ready:      league.Default().PersistenceError() == nil,
			AppVersion: appVersion, FrameworkVersion: gosx.Version,
			GitSHA: appGitSHA, Build: appBuildDate,
			StateSchema: commissionerhq.StateSchema{
				PersistedVersion:         stateSchema.PersistedVersion,
				SupportedVersion:         stateSchema.SupportedVersion,
				PersistedDatabaseVersion: stateSchema.PersistedDatabaseVersion,
				SupportedDatabaseVersion: stateSchema.SupportedDatabaseVersion,
				Compatible:               stateSchema.Compatible,
			},
		}, commissionerhq.Pool{
			Mode: poolStatus.State, Actual: poolStatus.Players, Target: poolStatus.PoolLimit,
			LastSync: poolStatus.LastSync, Error: poolStatus.LastError,
		}, openData)
	})
	if err != nil {
		return nil, nil, err
	}
	commissionerhq.SetDefault(hqService)
	hqV1Config, err := v1provider.ConfigFromEnv()
	if err != nil {
		return nil, nil, err
	}
	// The provider runtime binds a TCP listener, so it is created at the end
	// of this function (see the tail below). The health payload reads it
	// through this variable, which is nil until then and safe to call nil.
	var hqV1Runtime *commissionerHQV1Runtime
	hqV1FleetConfig, err := v1fleet.ConfigFromEnv()
	if err != nil {
		return nil, nil, err
	}
	hqV1Fleet, err := v1fleet.New(hqV1FleetConfig, v1fleet.Options{})
	if err != nil {
		return nil, nil, err
	}
	commissionerpage.SetHQV1Fleet(hqV1Fleet)
	sessions, err := session.New(cfg.SessionKey, gridironSessionOptions(cfg.AppEnv))
	if err != nil {
		return nil, nil, err
	}

	// The harness provider replaces the default session provider, so it must
	// fall back to a session-backed manager. Without that fallback the Google
	// callback would sign a manager in and no later request would see it.
	authOptions := auth.Options{LoginPath: "/login"}
	if cfg.TestAuth {
		sessionAuth := auth.New(sessions, authOptions)
		authOptions.Provider = harnessProvider(sessionAuth, league.Default())
	}
	authManager := auth.New(sessions, authOptions)
	googleConfigured := googleAuthConfigured()
	googleProvider := auth.GoogleProvider(
		os.Getenv("GOOGLE_CLIENT_ID"),
		os.Getenv("GOOGLE_CLIENT_SECRET"),
		getenv("GOOGLE_REDIRECT_URL", "http://localhost:8080/auth/google/callback"),
	)
	googleProvider.AuthParams = map[string]string{
		"include_granted_scopes": "true",
		"prompt":                 "select_account",
	}
	googleOAuth := authManager.OAuth(auth.OAuthOptions{
		Providers:   []auth.OAuthProvider{googleProvider},
		SuccessPath: "/",
		FailurePath: "/login?error=oauth",
	})

	// Hashed once at boot, not per request: styles.css only changes at
	// deploy time, and BuildApp already runs once per process (see its own
	// doc comment). See hashedPublicAssetHref's doc comment for why this is
	// a query-string hash rather than a literal "/styles.<hash>.css" path.
	stylesheetHref := hashedPublicAssetHref(root, "styles.css")
	// The navigation runtime is a fixed, build-time string (compiled into
	// this binary via runtimehost's go:embed), so its hash never changes
	// mid-process either — hashed once here for the same reason
	// stylesheetHref is. See the router.SetNavigationHead call below for
	// why this replaces app.EnableNavigation's default inline script.
	navigationRuntimeSum := sha256.Sum256([]byte(runtimehost.NavigationRuntime))
	navigationRuntimeHash := hex.EncodeToString(navigationRuntimeSum[:8])
	navigationRuntimeHref := "/gosx-nav/" + navigationRuntimeHash + ".js"

	router := route.NewRouter()
	router.SetLayout(func(ctx *route.RouteContext, body gosx.Node) gosx.Node {
		ctx.SetLanguage("en")
		ctx.SetMetadata(server.Metadata{
			Links: []server.LinkTag{
				{Rel: "stylesheet", Href: stylesheetHref},
				{Rel: "icon", Href: "/favicon.svg", Type: "image/svg+xml"},
			},
			ThemeColor: []server.ThemeColor{{Color: "#070A16"}},
		})
		// data-gosx-heartbeat/-interval (gosx#216) is the Draft Room's
		// attendance claim only. Its ping is visibility-aware (it pauses
		// while the tab is hidden) and carries no focused-control interaction
		// guard, so it keeps presence current while a manager is typing in a
		// search box — the exact gap the old JS's focusedControlActive()
		// special case existed only to close on the pre-gosx#216 sendPresence
		// loop this replaced. It must stay off every route besides /draft:
		// leagueHeartbeatEndpoint's own doc comment spells out why pointing
		// it at leagueVersionEndpoint elsewhere either duplicated a page's
		// own data-gosx-revalidate-src poll of that same URL every 4s tick,
		// or (on a page with no revalidate poll) fired a GET whose response
		// the client heartbeat always discards — never a real version-sync
		// primitive. So an empty return here means "omit the body marker,"
		// not "poll an empty src": ctx.BodyAttrs is skipped outright rather
		// than emitting data-gosx-heartbeat="" for the runtime to reject.
		// PageState.BodyAttrs (v0.50.0) puts the two heartbeat attributes
		// directly on <body> when present, so no wrapper element is needed.
		// The native route Document contract carries them through the
		// framework shell and re-reads them after managed navigation.
		if heartbeatEndpoint := leagueHeartbeatEndpoint(ctx.Request.URL.Path); heartbeatEndpoint != "" {
			ctx.BodyAttrs(
				gosx.Attr("data-gosx-heartbeat", heartbeatEndpoint),
				gosx.Attr("data-gosx-heartbeat-interval", "4s"),
			)
		}
		// Data density (P1-6, UI pass 2026-08-30): a viewer's session-carried
		// preference (internal/density) becomes a body attribute every route
		// picks up automatically, the same way the heartbeat attributes above
		// do — public/styles.css's body[data-density="compact"] block is the
		// only reader.
		if density.IsCompact(ctx.Request) {
			ctx.BodyAttrs(gosx.Attr("data-density", density.Compact))
		}
		return server.HTMLDocument(ctx.Document(appName, body))
	})
	// gap-audit "externalize the 88KB inline navigation runtime" (wave 3):
	// App.EnableNavigation's default head builder inlines
	// runtimehost.NavigationRuntime (88,076 bytes) as a literal <script>
	// body on every single page render — 71-93% of a typical page's
	// transfer. route.Router.SetNavigationHead lets an app supply its own
	// <head> builder instead; called here rather than through
	// app.EnableNavigation() (removed below, see the comment beside it),
	// because App.Build's registerMountRoutes overwrites any
	// router.SetNavigationHead already set with its own default
	// (navigationScriptWithNonce) whenever App.navigation is true — see
	// m31labs.dev/gosx@v0.53.10/server/server.go (registerMountRoutes,
	// ~line 702). The per-request data-gosx-navigation-state/-current-path
	// attributes and every other navigation-runtime behavior come from
	// RouteContext.NavigationEnabled() (true whenever this router's
	// navigationHead is non-nil — route.go's newRouteContext, ~line 644),
	// not from App.EnableNavigation directly, so this router-level call is
	// sufficient on its own for a route.Router-based, app.Mount-registered
	// app like this one (server.go's other App.navigation reader, ~line
	// 1007, only fires for app.Page/app.API document routes, which this
	// app does not register — it uses app.Mount exclusively).
	//
	// The script loads synchronously (no "defer"): the runtime's own
	// bottom-of-file bootstrap (client/runtime/host/navigation.ts, ~line
	// 6658) only re-scans the initial document through a
	// "DOMContentLoaded" listener when document.readyState is still
	// "loading" at the moment the script executes. A deferred script runs
	// after the parser has already flipped readyState to "interactive"
	// (the HTML spec's own script-processing order), which would silently
	// skip that re-scan; a plain blocking external script keeps the exact
	// timing an inline script already has today.
	router.SetNavigationHead(func(nonce string) gosx.Node {
		return gosx.RawHTML(`<script data-gosx-navigation="true" src="` + navigationRuntimeHref + `"` + navigationScriptNonceAttr(nonce) + `></script>`)
	})
	// Authentication and onboarding redirects belong to the file routes
	// themselves. GoSX applies this middleware only after a page or action
	// route matches, so an unknown URL keeps the normal truthful 404 instead
	// of being turned into a misleading login redirect.
	if err := router.AddDir(filepath.Join(root, "app"), route.FileRoutesOptions{
		Middleware: []route.Middleware{
			requireLeagueSession,
			redirectSeatedFromJoin,
		},
	}); err != nil {
		return nil, nil, err
	}
	// Tier 0 invite-link consume (setup-wizard design section 6.2):
	// registered directly on the router, not under app/, and deliberately
	// outside requireLeagueSession — an anonymous visitor presenting the
	// raw token is exactly who /auth/invite/{token} is for.
	registerInviteConsumeRoutes(router, authManager, league.Default(), league.Default())

	app := server.New()
	// app.EnableNavigation() is deliberately NOT called: its default head
	// builder is exactly what router.SetNavigationHead (above) replaces.
	// Calling both would let App.Build's registerMountRoutes overwrite the
	// router's own navigationHead field with the framework default the
	// instant this app builds — see the comment beside that call.
	app.EnableSecurityPolicy(gridironSecurityPolicy())
	app.EnableGzip()
	app.Use(avatarMultipartEnvelopeLimit)
	app.Use(sessions.Middleware)
	app.Use(csrfExemptClientEvents(csrfFailureRenderer(sessions.Protect, stylesheetHref)))
	app.Use(authManager.Middleware)
	app.SetPublicDir(filepath.Join(root, "public"))
	// Serves the externalized navigation runtime router.SetNavigationHead
	// (above) now references. The hash is in the path (not a "?v=" query,
	// unlike hashedPublicAssetHref) because this is a synthesized route,
	// not a public/ file GoSX's own servePublic already conditions on a
	// query string — a literal immutable Cache-Control here needs no such
	// condition, since this exact path only ever serves these exact bytes.
	app.Mount("GET "+navigationRuntimeHref, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		io.WriteString(w, runtimehost.NavigationRuntime)
	}))

	// Liveness is deliberately independent of league persistence and optional
	// upstream feeds. A process that is still serving requests must not be
	// restarted merely because readiness has withdrawn for an operator repair.
	app.API("GET /api/live", func(ctx *server.Context) (any, error) {
		ctx.NoStore()
		return livenessPayload(), nil
	})
	app.API("GET /api/health", func(ctx *server.Context) (any, error) {
		// Health is a live readiness signal. A public 30-second cache could
		// keep advertising "ok" after a runtime persistence poison, which is
		// exactly when an orchestrator must stop routing writes here. Optional
		// feed degradation remains diagnostic only and never changes readiness.
		ctx.NoStore()
		ctx.CacheTag("health")
		wireStatus := signalFeed.Status()
		openStatus := openStats.Status()
		poolStatus := fantasyPool.Status()
		draftStarted, draftStartedAt := league.Default().DraftLifecycle()
		draftStartedAtText := ""
		if !draftStartedAt.IsZero() {
			draftStartedAtText = draftStartedAt.Format(time.RFC3339)
		}
		persistenceErr := league.Default().PersistenceError()
		persistenceReady, persistenceStatus, persistenceMessage := persistenceHealth(persistenceErr)
		stateSchema := league.Default().StateSchemaCompatibility()
		blitzHealth := league.Default().BlitzDependencyHealth()
		rosterCapacity := league.Default().TeamCount() * league.CurrentDraftRounds()
		poolCushion := max(0, poolStatus.Players-rosterCapacity)
		poolCoverage := 0.0
		if rosterCapacity > 0 {
			poolCoverage = float64(poolStatus.PoolLimit) / float64(rosterCapacity)
		}
		if persistenceStatus != http.StatusOK {
			ctx.SetStatus(persistenceStatus)
		}
		return map[string]any{
			"ok":               persistenceReady,
			"liveness":         true,
			"readiness":        persistenceReady,
			"persistenceReady": persistenceReady,
			"persistenceError": persistenceMessage,
			// State schema evidence is intentionally numeric and PII-free. It
			// reports the authoritative persisted marker, not the normalized
			// in-memory version, so release rollback decisions can be made
			// before an image-only change.
			"stateSchema": stateSchemaPayload(stateSchema),
			// Blitz is an optional upstream dependency: expose its bounded,
			// safe provenance without making a Tank01 outage look like a
			// process-readiness failure.
			"blitz": blitzHealthPayload(blitzHealth),
			"app":   appName,
			// "version" is the Gridiron release, not the GoSX framework
			// version. Keeping the framework version adjacent makes runtime
			// drift (and an accidentally old image) immediately visible.
			"version":                    appVersion,
			"appVersion":                 appVersion,
			"frameworkVersion":           gosx.Version,
			"gitSHA":                     appGitSHA,
			"buildDate":                  appBuildDate,
			"googleOAuthReady":           googleConfigured,
			"commissionerHQV1Configured": hqV1Config.Enabled,
			"commissionerHQV1Listening":  hqV1Runtime.Listening(),
			"signalWireReady":            wireStatus.Configured,
			"signalWireMode":             wireStatus.Mode,
			"ownedSignals":               wireStatus.RelevantSignals,
			"openStatsRunning":           openStatus.Running,
			"openScheduleState":          openStatus.Schedules.State,
			"openPlayerStatsState":       openStatus.PlayerStats.State,
			"openInjuryState":            openStatus.Injuries.State,
			"fantasyPoolEnabled":         poolStatus.Enabled,
			"fantasyPoolMode":            poolStatus.Mode,
			"fantasyPoolState":           poolStatus.State,
			"fantasyPoolPlayers":         poolStatus.Players,
			"fantasyPoolTarget":          poolStatus.PoolLimit,
			"fantasyRosterCapacity":      rosterCapacity,
			"fantasyPoolCushion":         poolCushion,
			"fantasyPoolCoverage":        poolCoverage,
			"fantasyPoolScoring":         poolStatus.Scoring,
			"fantasyPoolError":           poolStatus.LastError,
			"fantasyPoolLastSuccess": func() string {
				if poolStatus.LastSync.IsZero() {
					return ""
				}
				return poolStatus.LastSync.UTC().Format(time.RFC3339)
			}(),
			"fantasyPoolAgeSeconds":             int64(poolStatus.Age / time.Second),
			"fantasyPoolFreshnessWindowSeconds": int64(poolStatus.FreshFor / time.Second),
			"draftAt":                           league.Default().DraftAt().Format(time.RFC3339),
			"draftStarted":                      draftStarted,
			"draftStartedAt":                    draftStartedAtText,
			// leagueConfig: "defaults" on an unconfigured checkout, or
			// "file:<path>" once a league.json loads (productization spec
			// section 4.3).
			"leagueConfig": league.Default().Config().Source,
			// state names the boot state truthfully, matching the SETUP
			// and fail-closed apps' own health payloads (setup_app.go,
			// fail_closed_app.go) so a monitor reads one consistent field
			// across every boot state instead of inferring CONFIGURED from
			// the absence of a "state" key.
			"state": "configured",
			"time":  time.Now().UTC().Format(time.RFC3339),
		}, nil
	})
	app.Mount("GET /api/live/week", liveWeekAPIHandler(requireLeagueAccess))
	registerLeagueHeartbeatAPIs(app, league.Default(), leagueFingerprint, league.Default().ClockForTest)
	// /wire/fragment answers app/wire/page.gsx's data-gosx-region /
	// data-gosx-region-interval poll (gosx#217): wirepage.FeedFragmentWithError
	// loads that page program once and renders its typed SignalCard /
	// WireEmptyState components, the same components the initial page uses.
	// It is a plain HTML fragment, not a JSON API, so it lives next to the page
	// it serves rather than under mountOwnedDataAPI's external data contract.
	app.Mount("GET /wire/fragment", requireLeagueAccess(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Cache-Control", "no-store")
		writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		node, err := wirepage.FeedFragmentWithError(request, signalFeed)
		if err != nil {
			http.Error(writer, "wire fragment unavailable", http.StatusInternalServerError)
			return
		}
		_, _ = io.WriteString(writer, gosx.RenderHTML(node))
	})))
	app.API("GET /api/wire/pulse", func(ctx *server.Context) (any, error) {
		ctx.NoStore()
		return wirepage.PulseData(signalFeed), nil
	})
	app.Mount("GET /commissioner/fragment", commissionerpage.FragmentHandler(hqService))
	app.Mount("GET /commissioner/switch", adminpage.SwitchHandler(hqService))
	app.Mount("GET /admin/fragment", adminpage.AdminAttentionFragmentHandler(league.Default()))
	app.Mount("GET /draft/fragment/room", draftpage.RoomFragmentHandler(league.Default()))
	app.Mount("GET /draft/fragment/workspace", draftpage.WorkspaceFragmentHandler(league.Default()))
	app.Mount("GET /draft/fragment/command", draftpage.CommandFragmentHandler(league.Default()))
	app.Mount("GET /draft/fragment/tape", draftpage.TapeFragmentHandler(league.Default()))
	// tape-rows (2026-08-30 review, findings 1/2/3/6): target mode's own
	// single nested tape region — DraftTapeRows only, never the pane
	// shell — replacing the deleted prepend region's "?since=" fetch.
	app.Mount("GET /draft/fragment/tape-rows", draftpage.TapeRowsFragmentHandler(league.Default()))
	// Stays mounted for gosx v0.53.10's target mode (Task 8), which binds a per-pick click region straight to it.
	app.Mount("GET /draft/fragment/pick/{n}", draftpage.PickDetailFragmentHandler(league.Default()))
	app.Mount("GET /draft/fragment/available", draftpage.AvailableFragmentHandler(league.Default()))
	app.Mount("GET /draft/fragment/queue", draftpage.QueueFragmentHandler(league.Default()))
	app.Mount("POST /draft/queue", draftpage.QueueMoveHandler(league.Default()))
	app.Mount("GET /draft/live.json", draftpage.LiveViewHandler(league.Default()))
	app.Mount("GET /draft/ledger.csv", draftpage.LedgerCSVHandler(league.Default()))
	// League backup (data sovereignty): a commissioner-only, read-only
	// download of one consistent local snapshot archive — see
	// docs/backup-restore.md and adminpage.BackupDownloadHandler's doc
	// comment. appVersion is the same release marker /api/health reports.
	app.Mount("GET /admin/backup.tar.gz", adminpage.BackupDownloadHandler(league.Default(), appVersion))
	app.Mount(draftpage.DraftLiveHubPath, draftLiveUpdates.Handler(league.Default()))
	app.Mount(matchupspage.ScoresLiveHubPath, scoresLive.Handler(league.Default()))
	app.Mount(lockerpage.LockerLiveHubPath, lockerLive.Handler(league.Default()))
	// Player-pool/waiver and transaction regions are read-only projections.
	// Their shared 4-second interval is the declared cross-client convergence
	// bound; managed player mutations signal the same regions immediately while
	// native forms continue through their existing POST-redirect-GET paths.
	app.Mount("GET /players/fragment/pool", playerspage.PlayersPoolFragmentHandler(league.Default()))
	app.Mount("GET /players/fragment/waivers", playerspage.PlayersWaiverFragmentHandler(league.Default()))
	app.Mount("GET /activity/fragment", activitypage.ActivityFragmentHandler(league.Default()))
	app.Mount("GET /team/fragment", teampage.TeamLineupFragmentHandler(league.Default()))
	app.Mount("GET /trades/fragment", tradespage.TradeDeskFragmentHandler(league.Default()))
	// Locker Room (GC-4): the board region refetches only on the
	// locker-live hub's locker:changed event above — no interval, unlike
	// every fragment in this block.
	app.Mount("GET /locker/fragment", lockerpage.LockerFragmentHandler(league.Default()))
	// Pick'em's selected-week region polls the authoritative per-game lock,
	// market, result, and sheet-scoring projection. Managed picks signal an
	// immediate refresh; the native POST-redirect-GET fallback remains intact.
	app.Mount("GET /pickem/fragment", pickempage.PickemFragmentHandler(league.Default()))
	app.Mount("GET /api/commissioner/v2/summary", hqService.SummaryHandler())
	mountOwnedDataAPI(app, signalFeed, openStats, fantasyPool, os.Getenv("DATA_API_TOKEN"))

	// Team avatars (design decisions 1-3): GoSX v0.50.0 exposes File/Files
	// and MaxActionBodyBytes for managed actions, but this native upload keeps
	// its own complete-multipart envelope cap until a bounded-multipart
	// contract can run before the session/CSRF parser. The serving route emits
	// its own fixed Cache-Control lifetime rather than the public-dir default,
	// since an uploaded avatar lives in the data dir, not public/. Both still
	// pass through the session/CSRF/auth middleware registered above (app.Use
	// wraps every mount, not just page routes).
	app.Mount("POST /avatar/upload", avatarUploadHandler(league.Default()))
	app.Mount("GET /avatars/", avatarServeHandler(league.Default()))

	// Team badges (the badge-picker feature): POST /avatar/badge claims,
	// swaps, or releases a team's badge motif; GET /avatars/badge/ is a
	// more specific subtree than the "GET /avatars/" mount above, so it is
	// preferred for any request under that path — see badge_handlers.go's
	// doc comments for the routing and PathValue details.
	app.Mount("POST /avatar/badge", badgeUploadHandler(league.Default()))
	app.Mount("GET /avatars/badge/", badgeServeHandler(league.Default()))

	app.Mount("GET /auth/google/start", googleStartHandler(googleOAuth, googleConfigured))
	app.Mount("GET /auth/google/callback", googleCallbackHandler(googleOAuth, authManager, googleConfigured))
	app.Mount("POST /auth/logout", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authManager.SignOut(r)
		session.AddFlash(r, "notice", "You are signed out. Sign in again to return to your team.")
		http.Redirect(w, r, "/login", http.StatusSeeOther)
	}))

	if cfg.TestAuth {
		rt.restoreClock = mountTestRoutes(app, league.Default(), authManager, rt.Live)
	}

	rootHandler, err := router.BuildChecked()
	if err != nil {
		return nil, nil, err
	}
	app.Mount("/", rootHandler)

	if !googleConfigured {
		log.Printf("Google OAuth is in setup mode; add GOOGLE_CLIENT_ID and GOOGLE_CLIENT_SECRET to .env")
	}
	wireStatus := signalFeed.Status()
	if !wireStatus.Configured {
		log.Printf("Signal wire is awaiting sources; enable public feeds or add BLUESKY_HANDLES / BLUESKY_DIDS")
	} else if !wireStatus.BlueskyConfigured {
		log.Printf("Free public feed mesh is active; add optional BLUESKY_HANDLES / BLUESKY_DIDS for event-driven social alerts")
	}
	// Last: this is the one call in BuildApp that claims an operating-system
	// resource. Every error above it returns without a bound port.
	hqV1Runtime, err = buildCommissionerHQV1Runtime(hqV1Config, league.Default(), fantasyPool, openStats)
	if err != nil {
		return nil, nil, err
	}
	rt.HQV1 = hqV1Runtime
	return app, rt, nil
}
