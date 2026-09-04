package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	_ "time/tzdata"

	"gridiron-2000/internal/fantasy"
	"gridiron-2000/internal/league"
	"gridiron-2000/internal/livescore"
	"gridiron-2000/internal/mailer"
	"gridiron-2000/internal/navigation"
	"gridiron-2000/internal/notify"
	"gridiron-2000/internal/openstats"
	_ "gridiron-2000/modules"
	"m31labs.dev/gosx/auth"
	"m31labs.dev/gosx/env"
	"m31labs.dev/gosx/server"
	"m31labs.dev/gosx/session"
)

// isLocalAppEnv reports whether APP_ENV names a local, non-deployed
// environment. It is an allow-list: every other label, "prod" and "staging"
// included, is a deployment. The cookie policy, AppConfig.validate, and
// internal/league's demo-mode gate share this one answer (league.IsLocalAppEnv)
// so none of them can disagree about where the process runs.
func isLocalAppEnv(appEnv string) bool {
	return league.IsLocalAppEnv(appEnv)
}

// gridironSessionOptions keeps the cookie policy explicit at the one place
// where the application decides whether it is serving local plain HTTP or a
// deployed HTTPS environment. gosx defaults to Secure when AllowInsecure is
// omitted, so only known local/default environments opt in to plain HTTP.
//
// MaxAge is 180 days (owner decision, setup-wizard design section 6.2: the
// design proposed raising it only for a Tier-0-only instance, "because
// re-auth costs a commissioner round-trip"; the owner's parameter list
// applies the longer window unconditionally, for every sign-in tier, to
// avoid computing "is Tier 0 the only method" per deployment). The cookie
// stays Secure/HTTPOnly/Encrypt/SameSite=Lax regardless of its length; a
// per-member session-epoch revocation (design section 6.4) is a later
// slice's addition for a commissioner who needs to invalidate one seat's
// session early.
func gridironSessionOptions(appEnv string) session.Options {
	localHTTP := isLocalAppEnv(appEnv)
	return session.Options{
		CookieName:    "gridiron_session",
		Secure:        !localHTTP,
		AllowInsecure: localHTTP,
		HTTPOnly:      true,
		Encrypt:       true,
		MaxAge:        180 * 24 * time.Hour,
		SameSite:      http.SameSiteLaxMode,
	}
}

func main() {
	_, thisFile, _, _ := runtime.Caller(0)
	if err := env.LoadDir(server.ResolveAppRoot(thisFile), ""); err != nil {
		log.Fatal(err)
	}
	// Load runtime.env (design section 4.5 / owner decision 6) before
	// anything reads process environment: COMMISSIONER_EMAILS,
	// IDENTITY_ALIASES, and SESSION_SECRET, written by a completed setup
	// wizard commit beside league.db. Absence is not an error — a
	// checkout that has never run setup, or a Kubernetes bundle-mode
	// instance, simply has no such file. Real env always wins.
	if err := loadRuntimeEnvFile(dataDirFromEnv()); err != nil {
		log.Fatal(err)
	}
	// The boot state decision runs before AppConfigFromEnv/BuildApp and
	// before any league code: SETUP and FAIL_CLOSED never construct the
	// league.Default() singleton at all (setup-wizard design section 3.1).
	// A CONFIGURED decision falls through unchanged to the normal boot
	// below, which resolves league.json again itself through the same
	// lookup rule — DetermineBootState only decided which app to build.
	bootDecision, err := league.DetermineBootState()
	if err != nil {
		log.Fatal(err)
	}
	cfg, err := AppConfigFromEnv()
	if err != nil {
		log.Fatal(err)
	}
	switch bootDecision.State {
	case league.BootFailClosed:
		app, err := BuildFailClosedApp(cfg)
		if err != nil {
			log.Fatal(err)
		}
		log.Printf("league boot state: fail_closed (marker or league data present, no league.json resolves) — %s", failClosedOperatorMessage)
		serveSimpleApp(app, cfg.Port, "fail-closed operator page")
		return
	case league.BootSetup:
		app, setupRuntime, err := BuildSetupApp(cfg, bootDecision.Store)
		if err != nil {
			log.Fatal(err)
		}
		defer func() { _ = setupRuntime.Store.Close() }()
		log.Printf("league boot state: setup")
		serveSimpleApp(app, cfg.Port, "setup wizard")
		return
	}
	runtimeContext, stopRuntime := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopRuntime()
	app, rt, err := BuildApp(cfg)
	if err != nil {
		log.Fatal(err)
	}
	// The notify worker keeps its own cancellation, separate from
	// runtimeContext: on shutdown the HTTP server stops accepting requests
	// immediately, but the worker keeps draining whatever is already queued
	// (see rt.Drain below the server's shutdown branch) until it finishes or
	// the drain deadline expires. A message the ledger already marked "sent"
	// must not be silently abandoned (design spec section 6.3; finding m1).
	if rt.StopNotify != nil {
		defer rt.StopNotify()
	}
	rt.Start(runtimeContext)
	log.Printf("%s listening on http://localhost:%s", rt.AppName, rt.Port)
	if rt.HQV1 != nil {
		log.Printf("Commissioner HQ v1 provider listening on its private listener")
	}
	type runtimeServerError struct {
		name string
		err  error
	}
	serverErrors := make(chan runtimeServerError, 2)
	go func() {
		serverErrors <- runtimeServerError{name: "application", err: app.ListenAndServe(":" + rt.Port)}
	}()
	if rt.HQV1 != nil {
		go func() {
			serverErrors <- runtimeServerError{name: "Commissioner HQ provider", err: rt.HQV1.Serve()}
		}()
	}
	var runtimeFailure *runtimeServerError
	select {
	case result := <-serverErrors:
		if result.err != nil && !errors.Is(result.err, http.ErrServerClosed) {
			runtimeFailure = &result
		}
	case <-runtimeContext.Done():
	}
	shutdownContext, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	var shutdownGroup sync.WaitGroup
	shutdownGroup.Add(1)
	go func() {
		defer shutdownGroup.Done()
		if err := app.Shutdown(shutdownContext); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("server shutdown: %v", err)
		}
	}()
	if rt.HQV1 != nil {
		shutdownGroup.Add(1)
		go func() {
			defer shutdownGroup.Done()
			if err := rt.HQV1.Shutdown(shutdownContext); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Printf("Commissioner HQ provider shutdown: %v", err)
			}
		}()
	}
	shutdownGroup.Wait()
	cancelShutdown()
	// Give the notify worker a bounded window to finish delivering whatever
	// was already queued before the process exits. This applies equally to a
	// signal and either listener failing unexpectedly.
	if rt.Drain != nil {
		rt.Drain(10 * time.Second)
	}
	if rt.StopNotify != nil {
		rt.StopNotify()
	}
	if runtimeFailure != nil {
		log.Fatalf("%s listener failed: %v", runtimeFailure.name, runtimeFailure.err)
	}
}

// serveSimpleApp runs the SETUP and BootFailClosed apps: a single listener,
// no background loops, no Commissioner HQ provider — everything BuildApp's
// AppRuntime otherwise coordinates. Shutdown follows the same signal +
// bounded-timeout shape as the CONFIGURED path below, just without a
// runtime to drain.
func serveSimpleApp(app *server.App, port, name string) {
	runtimeContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	log.Printf("%s listening on http://localhost:%s", name, port)
	serverErrors := make(chan error, 1)
	go func() {
		serverErrors <- app.ListenAndServe(":" + port)
	}()
	select {
	case err := <-serverErrors:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("%s listener failed: %v", name, err)
		}
	case <-runtimeContext.Done():
	}
	shutdownContext, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelShutdown()
	if err := app.Shutdown(shutdownContext); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Printf("%s shutdown: %v", name, err)
	}
}

// blitzHealthPayload keeps the public /api/health shape flat enough for
// operators while reusing the same typed source facts commissioner summary
// consumes. The nested pre1 object is intentionally separate because its
// partial evidence must not make live slate readiness look offline.
func blitzHealthPayload(value league.BlitzDependencyHealth) map[string]any {
	return map[string]any{
		"enabled":              value.Source.Enabled,
		"state":                value.Source.State,
		"lastAttempt":          value.Source.LastAttempt,
		"lastSuccess":          value.Source.LastSuccess,
		"error":                value.Source.SafeError,
		"expectedGames":        value.Source.ExpectedGames,
		"fetchedGames":         value.Source.FetchedGames,
		"finalGames":           value.Source.FinalGames,
		"expectedScoringGames": value.Source.ExpectedScoringGames,
		"fetchedScoringGames":  value.Source.FetchedScoringGames,
		"scoringComplete":      value.Source.ScoringComplete,
		"complete":             value.Source.Complete,
		"final":                value.Source.Final,
		"verifiedZero":         value.Source.VerifiedZero,
		"slates":               value.Source.Slates,
		"pre1":                 value.Pre1,
	}
}

// gridironSecurityPolicy keeps the browser-facing surface on GoSX's native
// nonce contract. The app has a few deliberate inline style attributes (badge
// colors and consensus bars), so style-src allows inline styles while script
// execution remains nonce-gated. Google OAuth is a top-level redirect, not a
// frame or XHR, and the policy explicitly permits its form destination.
func gridironSecurityPolicy() server.SecurityPolicy {
	return server.SecurityPolicy{
		ContentSecurityPolicy: strings.Join([]string{
			"default-src 'self'",
			"base-uri 'self'",
			"object-src 'none'",
			"frame-ancestors 'none'",
			"form-action 'self' https://accounts.google.com",
			"script-src 'self' 'nonce-{nonce}' 'strict-dynamic' 'wasm-unsafe-eval'",
			"style-src 'self' 'unsafe-inline'",
			"font-src 'self'",
			"img-src 'self' data: https://a.espncdn.com",
			"connect-src 'self'",
			"manifest-src 'self'",
			"worker-src 'self' blob:",
		}, "; "),
		FrameOptions:      "DENY",
		ReferrerPolicy:    "strict-origin-when-cross-origin",
		PermissionsPolicy: "camera=(), microphone=(), geolocation=()",
	}
}

// requireLeagueSession gates the league pages behind sign-in, like a hosted
// service: anonymous visitors see the landing page, the login page, and the
// legal pages only. Demo mode leaves everything open for local rehearsal.
func requireLeagueSession(next http.Handler) http.Handler {
	return requireLeagueSessionWithDemoMode(next, func() bool {
		return league.Default().DemoMode()
	})
}

func requireLeagueSessionWithDemoMode(next http.Handler, demoMode func() bool) http.Handler {
	open := map[string]bool{
		// /help is public on every instance (wave-6 item 8): its own gating
		// used to differ from the demo build (public there, blocked here)
		// purely as a side effect of this allowlist, not a deliberate
		// membership decision — the page carries no seat-scoped or
		// league-private content, the same reason /guide is already open.
		"/": true, "/guide": true, "/help": true, "/login": true, "/privacy": true, "/terms": true,
		"/open-source": true,
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if demoMode != nil && demoMode() {
			next.ServeHTTP(w, r)
			return
		}
		if _, ok := league.Default().CurrentUser(r); ok {
			next.ServeHTTP(w, r)
			return
		}
		path := strings.TrimSuffix(r.URL.Path, "/")
		if path == "" {
			path = "/"
		}
		// /help/{topic-id} shares its parent's public status: every link
		// into a topic (from /help or /guide) must resolve for the same
		// anonymous visitor who can already open /help itself.
		if open[path] || strings.HasPrefix(path, "/help/") {
			next.ServeHTTP(w, r)
			return
		}
		session.AddFlash(r, "notice", "Sign in to enter the league.")
		http.Redirect(w, r, navigation.LoginPathForRequest(r), http.StatusSeeOther)
	})
}

// redirectSeatedFromJoin sends an already-seated visitor straight to
// their team terminal instead of the fantasy-signup form (registration
// wave, build item 2: "already-seated visitors get redirected to
// /team"). GoSX applies this middleware only to a matched file page or
// action; the signup action itself posts to /join/__actions/signup-claim,
// a distinct path this leaves untouched. Every other matched route passes
// through unchanged.
func redirectSeatedFromJoin(next http.Handler) http.Handler {
	return redirectSeatedFromJoinWithViewer(next, func(r *http.Request) bool {
		hasSeat, _ := league.Default().Viewer(r)["has_seat"].(bool)
		return hasSeat
	})
}

func redirectSeatedFromJoinWithViewer(next http.Handler, hasSeat func(*http.Request) bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.TrimSuffix(r.URL.Path, "/") == "/join" {
			if hasSeat != nil && hasSeat(r) {
				http.Redirect(w, r, "/team", http.StatusSeeOther)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// notificationSender adapts mailer.Config to notify.Sender: it converts one
// rendered notify.Message into a mailer.Message, tagging it with its
// notification category and carrying its ledger key as the Resend
// Idempotency-Key (design spec section 6.5). The league-shortcode tag
// named in the spec is not wired yet — league.json's config surface (the
// productization spec) has not landed, so there is no shortcode to read
// here; it is a documented follow-up, not a silent gap. An SMTP-transport
// failure is wrapped notify.Permanent so Queue.deliver does not retry it:
// smtp.SendMail can error after the server already accepted the message,
// and at-most-once is preferred there. A Resend failure is returned
// unwrapped and stays retryable — its Idempotency-Key header protects a
// retry from becoming a duplicate (finding M3).
func notificationSender(mailCfg mailer.Config) notify.Sender {
	return func(m notify.Message) error {
		err := mailCfg.SendMessage(mailer.Message{
			To:             m.To,
			Subject:        m.Subject,
			Text:           m.Text,
			HTML:           m.HTML,
			Tags:           map[string]string{"category": m.Category},
			IdempotencyKey: m.Key,
		})
		if errors.Is(err, mailer.ErrSMTPTransport) {
			return notify.Permanent(err)
		}
		return err
	}
}

// fantasyPlayerSource adapts the fantasy pool to the league's PlayerSource.
// Conversion runs only when the pool version changes, so page renders reuse
// one converted slice.
func fantasyPlayerSource(pool *fantasy.Service) league.PlayerSource {
	var mu sync.Mutex
	var lastVersion int64
	var lastState string
	var converted []league.Player
	return func() ([]league.Player, int64, string) {
		players, version := pool.Players()
		state := pool.Status().State
		mu.Lock()
		defer mu.Unlock()
		if converted == nil || version != lastVersion || state != lastState {
			converted = make([]league.Player, 0, len(players))
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
					// PunterRank is fantasy's house rank for Position "P"
					// (internal/fantasy's normalizePool, off the league's
					// own embedded 2025 punter rescoring) — Tank01 carries
					// no punter ADP at all, so this is the only rank a
					// punter ever gets. playerMap (internal/league) renders
					// it as "P##" whenever ADPRank is zero.
					PunterRank: player.PunterRank,
				})
			}
			lastVersion = version
			lastState = state
		}
		return converted, version, state
	}
}

// leagueScheduleSource adapts the mirrored nflverse schedule to the Pick'em
// engine without discarding market or source truth. nflverse kickoff times
// are Eastern. Final follows actual result/score presence, never elapsed time,
// and a missing spread remains distinct from a real pick'em line of zero.
func leagueScheduleSource(stats *openstats.Service) league.ScheduleSource {
	return func() []league.GameInfo {
		return leagueGamesFromScheduleSnapshot(stats.ScheduleSnapshot())
	}
}

// currentNFLWeekFunc adapts the mirrored nflverse schedule into the
// fantasy pool sync's "which NFL week is current" signal (GC-1 fix 1):
// internal/fantasy must not import internal/openstats (an inverted
// dependency — see fantasy.Service.SetCurrentWeek's doc comment), so
// app_build.go wires this closure in instead, the same injection pattern
// SetPunterProjections already uses for league.PunterProjection.
func currentNFLWeekFunc(stats *openstats.Service) func() int {
	return func() int {
		return currentNFLWeekAt(stats.ScheduleSnapshot().Games, time.Now())
	}
}

// currentNFLWeekAt is currentNFLWeekFunc's pure core: the smallest week
// with a game kicking off within the last four hours or later, or the
// largest week when every game has already passed that window — the same
// upcoming-or-latest rule internal/league's own pickemWeekAt uses, evaluated
// here over openstats.ScheduleGame rows instead of league.GameInfo (fantasy
// cannot import league either; this package already adapts the same raw
// rows to league.GameInfo elsewhere — see leagueGamesFromScheduleSnapshot).
// A schedule with no REG rows for the active season (before that season's
// games.csv is published) reports week 1, matching the spec's own rule:
// "before NFL week 1, keep week 1."
func currentNFLWeekAt(games []openstats.ScheduleGame, now time.Time) int {
	eastern := openStatsEastern()
	cutoff := now.Add(-4 * time.Hour)
	largestWeek := 0
	upcomingWeek := 0
	haveUpcoming := false
	for _, game := range games {
		if game.GameType != "REG" || game.Week <= 0 {
			continue
		}
		kickoff, ok := openStatsKickoff(game, eastern)
		if !ok {
			continue
		}
		if game.Week > largestWeek {
			largestWeek = game.Week
		}
		if kickoff.After(cutoff) && (!haveUpcoming || game.Week < upcomingWeek) {
			upcomingWeek = game.Week
			haveUpcoming = true
		}
	}
	if haveUpcoming {
		return upcomingWeek
	}
	if largestWeek == 0 {
		return 1
	}
	return largestWeek
}

// openStatsEastern resolves the league's game-time zone, falling back to
// UTC when the tzdata lookup fails (the same fallback leagueScheduleSource
// used inline before this helper existed).
func openStatsEastern() *time.Location {
	eastern, err := time.LoadLocation("America/New_York")
	if err != nil {
		return time.UTC
	}
	return eastern
}

// openStatsKickoff parses one schedule row's authoritative kickoff instant.
// A blank/TBA time is deliberately not fabricated as 1:00 PM: it is neither
// pickable nor eligible to create a missed-pick loss until the source supplies
// a real time.
func openStatsKickoff(game openstats.ScheduleGame, eastern *time.Location) (time.Time, bool) {
	gameTime := strings.TrimSpace(game.GameTime)
	if gameTime == "" {
		return time.Time{}, false
	}
	kickoff, err := time.ParseInLocation("2006-01-02 15:04", game.GameDay+" "+gameTime, eastern)
	if err != nil {
		return time.Time{}, false
	}
	return kickoff, true
}

// openStatsGameFinal is the single source-truth finality rule shared by
// Pick'em and DST points allowed. The location and clock remain in the
// signature for existing callers, but elapsed wall time cannot make a game
// final without an actual result or both source scores.
func openStatsGameFinal(game openstats.ScheduleGame, _ *time.Location, _ time.Time) bool {
	return game.HasResult()
}

// pointsAllowedByTeam maps a team abbreviation to the opponent's score in
// its week game, but only for a game openStatsGameFinal reports final — an
// in-progress or unplayed game's 0-0 placeholder score must never look like
// a shutout. A team absent from the returned map has no final score yet;
// dstWeekStatLines must not compute dstShutout for it.
func pointsAllowedByTeam(games []openstats.ScheduleGame, eastern *time.Location, now time.Time) map[string]float64 {
	allowed := make(map[string]float64, len(games)*2)
	for _, game := range games {
		if game.GameType != "REG" || !openStatsGameFinal(game, eastern, now) {
			continue
		}
		allowed[strings.ToUpper(game.HomeTeam)] = game.AwayScore
		allowed[strings.ToUpper(game.AwayTeam)] = game.HomeScore
	}
	return allowed
}

// dstWeekStatLines builds one WeekStatLine per NFL team's defense/special
// teams unit for week from the team-stats mirror (WP-R2, DEFENSE group):
// dstSack, dstInt, dstFumbleRec (opponent-fumble recoveries only —
// recovering the team's own fumble is not a defensive scoring event), and
// dstTD, dstSafety feed directly from stats_team_week's def_* columns.
// dstShutout derives from the schedule's points-allowed, gated on the same
// finality rule as leagueScheduleSource (openStatsGameFinal) so an
// unplayed or in-progress game never reads as a shutout.
func dstWeekStatLines(stats *openstats.Service, eastern *time.Location, week int) []league.WeekStatLine {
	rows := stats.TeamStats(openstats.TeamStatsQuery{Week: week, Limit: 64})
	allowed := pointsAllowedByTeam(stats.Games(week), eastern, time.Now())
	out := make([]league.WeekStatLine, 0, len(rows))
	for _, row := range rows {
		team := strings.ToUpper(row.Team)
		name, ok := livescore.DSTName(team)
		if !ok {
			// An unrecognized team abbreviation (a source drift, not a
			// user error) scores nothing rather than guessing a name —
			// the same fail-quiet discipline punterHistLine follows.
			continue
		}
		statLine := map[string]float64{
			"dstSack":      row.DefSacks,
			"dstInt":       row.DefInterceptions,
			"dstFumbleRec": row.FumbleRecoveryOpp,
			"dstTD":        row.DefTDs,
			"dstSafety":    row.DefSafeties,
			"dstShutout":   0,
		}
		if pointsAllowed, ok := allowed[team]; ok && pointsAllowed == 0 {
			statLine["dstShutout"] = 1
		}
		out = append(out, league.WeekStatLine{
			Key:    openstats.NormalizePlayerKey(name, "DST"),
			Stats:  statLine,
			Source: league.StatSourceLedger,
		})
	}
	return out
}

// puntEventsByPunter groups week's play-by-play punt events by nflverse
// player ID (the same ID scheme stats_player_week uses, so PlayerID joins
// directly — no name-normalization needed for this particular join). A nil
// return (as opposed to a non-nil empty map) is the honest-fallback signal:
// the play-by-play mirror has no punts at all for this week, so every
// punter degrades to the box-score aggregate path — never per-punter
// silence dressed up as "this punter had zero punts."
func puntEventsByPunter(stats *openstats.Service, week int) map[string][]openstats.PuntEvent {
	events := stats.PuntEvents(openstats.PuntQuery{Week: week, Limit: 5000})
	if len(events) == 0 {
		return nil
	}
	byPunter := make(map[string][]openstats.PuntEvent, 32)
	for _, event := range events {
		byPunter[event.PunterID] = append(byPunter[event.PunterID], event)
	}
	return byPunter
}

// addPuntingStatsFromPBP fills every PUNTING scoring key from this week's
// play-by-play punt events (WP-R2): the exact-events source. The puntYards
// 40+-yard gate is applied here, at scoring time, not in openstats'
// ingestion layer (openstats.PuntEvent carries gross per-punt distance
// only). puntLong50 counts every 50+-yard punt ("(each)", scoring.go), not
// a single per-game flag. A blocked punt cannot also count toward
// puntYards/puntLong50 (its recorded distance is 0 in the source anyway)
// and is excluded from those two by the !Blocked guard for clarity.
func addPuntingStatsFromPBP(statLine map[string]float64, events []openstats.PuntEvent) {
	var yards40Plus, in20, coffin, inside5, long50, touchback, blocked float64
	for _, event := range events {
		if !event.Blocked && event.Distance >= 40 {
			yards40Plus += event.Distance
		}
		if event.InsideTwenty {
			in20++
		}
		if event.CoffinCorner {
			coffin++
		}
		if event.Inside5 {
			inside5++
		}
		if !event.Blocked && event.Distance >= 50 {
			long50++
		}
		if event.Touchback {
			touchback++
		}
		if event.Blocked {
			blocked++
		}
	}
	statLine["puntYards"] = yards40Plus
	statLine["puntIn20"] = in20
	statLine["coffinCorner"] = coffin
	statLine["puntDownedInside5"] = inside5
	statLine["puntLong50"] = long50
	statLine["puntTouchback"] = touchback
	statLine["puntBlocked"] = blocked
}

// addPuntingStatsFromBoxScore is the honest fallback (WP-R2, section 3):
// stats_player_week's per-game punting aggregates, used only when the
// play-by-play mirror has no data for the requested week. puntYards cannot
// honor the 40+-yard gate from an aggregate (pt_yards sums every punt in
// the game, with no per-punt breakdown) and stays at the same zero the
// pre-WP-R2 dormant rule used, rather than an unqualified approximation.
// coffinCorner and puntDownedInside5 need a per-punt landing spot no
// aggregate carries and also stay at zero. puntIn20, puntTouchback, and
// puntBlocked have honest box-score equivalents and feed for real.
func addPuntingStatsFromBoxScore(statLine map[string]float64, row openstats.PlayerWeekStat) {
	statLine["puntYards"] = 0
	statLine["puntIn20"] = row.PuntInside20
	statLine["coffinCorner"] = 0
	statLine["puntDownedInside5"] = 0
	statLine["puntLong50"] = 0
	if row.PuntLong >= 50 {
		statLine["puntLong50"] = 1
	}
	statLine["puntTouchback"] = row.PuntTouchback
	statLine["puntBlocked"] = row.PuntBlocked
}

// leagueWeekStatsSource adapts the mirrored nflverse weekly player ledger,
// team-week box scores, and play-by-play mirror to the league's
// WeekStatsSource seam (competition-formats spec section 2.4): one
// WeekStatLine per player (plus one per NFL team's DST unit), keyed by the
// same openstats.NormalizePlayerKey the pick'em and historical-line seams
// already use, with stats mapped onto the league's scoring rule keys.
// PASSING/RUSHING/RECEIVING/MISC feed as before; WP-R2 adds KICKING
// (fgMade, fgMissed, xpMade — defaultScoringRules' KICKING group carries no
// FG distance bands or an xpMissed key, so nothing else is available to
// feed), DEFENSE (dstWeekStatLines), and PUNTING (per-punt from
// play-by-play when it has data for the week, else the honest box-score
// fallback with one log line — never a crash, never a fabricated event).
// openstats.Service.PlayerStats caps results at 1000 rows per call
// regardless of Limit, an existing openstats constraint this adapter does
// not change.
// offenseStatLine maps one openstats weekly player row onto the league's
// scoring-rule keys for PASSING/RUSHING/RECEIVING/MISC plus KICKING —
// every group a single player-week row can feed (DEFENSE and PUNTING's
// per-punt keys need a different source; see dstWeekStatLines and
// addPuntingStatsFromPBP/addPuntingStatsFromBoxScore). Both live weekly
// scoring (leagueWeekStatsSource) and the matchup-rank cache
// (matchup_cache.go) score through this one mapping, so a fantasy-
// points-allowed rank is computed from the exact same fields a live
// score is.
func offenseStatLine(row openstats.PlayerWeekStat) map[string]float64 {
	return map[string]float64{
		"passYards":  row.PassingYards,
		"passTD":     row.PassingTDs,
		"passInt":    row.PassingInterceptions,
		"rushYards":  row.RushingYards,
		"rushTD":     row.RushingTDs,
		"reception":  row.Receptions,
		"recYards":   row.ReceivingYards,
		"recTD":      row.ReceivingTDs,
		"fumbleLost": row.FumblesLost,
		// twoPt (GC-1 fix 3) sums every two-point conversion type from the
		// mirrored nflverse ledger. This is the only source that ever
		// feeds it: Tank01's live box score carries no per-player
		// two-point field at all (see scoring.go's twoPt rule doc comment
		// and internal/fantasy's preseasonPlayerStats), so twoPt scores at
		// week close only, the same closed-week-only pattern several
		// PUNTING keys already follow.
		"twoPt":    row.PassingTwoPt + row.RushingTwoPt + row.ReceivingTwoPt,
		"fgMade":   row.FGMade,
		"fgMissed": row.FGMissed,
		"xpMade":   row.XPMade,
	}
}

func leagueWeekStatsSource(stats *openstats.Service) league.WeekStatsSource {
	eastern := openStatsEastern()
	return func(week int) []league.WeekStatLine {
		rows := stats.PlayerStats(openstats.PlayerQuery{Week: week, Limit: 1000})
		puntsByPunter := puntEventsByPunter(stats, week)
		if puntsByPunter == nil {
			log.Printf("openstats: no play-by-play punt data for week %d; punting scores from box-score aggregates only (puntYards, coffinCorner, and puntDownedInside5 score zero this week)", week)
		}
		out := make([]league.WeekStatLine, 0, len(rows)+32)
		for _, row := range rows {
			statLine := offenseStatLine(row)
			if row.Position == "P" {
				if puntsByPunter != nil {
					// A non-nil map with no entry for this punter is a
					// genuine zero (no punts recorded for them this week),
					// not a fallback trigger — puntsByPunter[missing key]
					// safely returns a nil, empty slice.
					addPuntingStatsFromPBP(statLine, puntsByPunter[row.PlayerID])
				} else {
					addPuntingStatsFromBoxScore(statLine, row)
				}
			}
			out = append(out, league.WeekStatLine{
				Key:    openstats.NormalizePlayerKey(row.PlayerName, row.Position),
				Stats:  statLine,
				Source: league.StatSourceLedger,
			})
		}
		out = append(out, dstWeekStatLines(stats, eastern, week)...)
		return out
	}
}

// leagueInjuryDesignationSource adapts the mirrored nflverse weekly injury
// report to league.InjuryDesignationSource (roster-ops SK spec: the IR
// eligibility gate). It is keyed by normalizePlayerKey(name, position) —
// the same join key historicalSource and leagueWeekStatsSource already
// use — because internal/fantasy's Tank01-backed live pool carries no
// GSIS ID (see league.InjuryDesignationSource's doc comment); nflTeam
// scopes the openstats query so one lookup stays cheap (openstats caps
// InjuryReports at 1000 rows per call regardless of Limit). Among that
// team's reports for the matching name+position, the highest-week row's
// ReportStatus wins — "this player's most recently reported weekly
// designation." ok is false when no report matches at all.
func leagueInjuryDesignationSource(stats *openstats.Service) league.InjuryDesignationSource {
	return func(name, position, nflTeam string) (string, bool) {
		key := openstats.NormalizePlayerKey(name, position)
		reports := stats.InjuryReports(openstats.InjuryQuery{Team: nflTeam, Limit: 1000})
		bestWeek := -1
		designation := ""
		found := false
		for _, r := range reports {
			if openstats.NormalizePlayerKey(r.PlayerName, r.Position) != key {
				continue
			}
			if r.Week > bestWeek {
				bestWeek = r.Week
				designation = r.ReportStatus
				found = true
			}
		}
		return designation, found
	}
}

// thousands formats an int with a comma separator, US style.
func thousands(value int) string {
	raw := strconv.Itoa(value)
	if len(raw) <= 3 || value < 0 {
		return raw
	}
	var b strings.Builder
	lead := len(raw) % 3
	if lead > 0 {
		b.WriteString(raw[:lead])
	}
	for i := lead; i < len(raw); i += 3 {
		if b.Len() > 0 {
			b.WriteString(",")
		}
		b.WriteString(raw[i : i+3])
	}
	return b.String()
}

// fantasyPoolStatus projects raw fantasy-source facts into the typed league
// seam. Presentation and recovery copy stay in internal/league so Draft,
// Board, Players, and Admin cannot drift into different freshness meanings.
func fantasyPoolStatus(pool *fantasy.Service) league.PoolStatusSource {
	return func() league.PlayerPoolStatus {
		status := pool.Status()
		if status.LastError != "" {
			log.Printf("fantasy pool sync error: %s", status.LastError)
		}
		return league.PlayerPoolStatus{
			Provider:        status.Provider,
			Mode:            status.Mode,
			State:           status.State,
			Players:         status.Players,
			Target:          status.PoolLimit,
			Positions:       status.Positions,
			WithADP:         status.WithADP,
			WithProjection:  status.WithProj,
			WithBye:         status.WithBye,
			ProjectionWeek:  status.ProjectionWeek,
			Requests:        status.Requests,
			LastSuccess:     status.LastSync,
			FreshnessWindow: status.FreshFor,
			LastError:       status.LastError,
		}
	}
}

func googleStartHandler(flow *auth.OAuth, configured bool) http.Handler {
	if configured {
		begin := flow.BeginHandler("google")
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			request := r.Clone(r.Context())
			request.URL = new(url.URL)
			*request.URL = *r.URL
			query := request.URL.Query()
			query.Set("next", navigation.SafeReturnPath(query.Get("next")))
			request.URL.RawQuery = query.Encode()
			begin.ServeHTTP(w, request)
		})
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session.AddFlash(r, "notice", "Sign-in is not ready yet. Ask the commissioner to finish league setup.")
		// A visitor who followed a deep link (e.g. /login?next=%2Fdraft%3Fweek%3D1)
		// to the disabled-but-still-reachable Google start route must not lose
		// that destination on the bounce back to /login: preserve the
		// validated next through the same sanitizer LoginData already applies
		// to the sign-in button's own href (navigation.SafeReturnPath), so a
		// malformed or unsafe query value degrades to "/" instead of an open
		// redirect, and a legitimate one survives round-trip.
		next := navigation.SafeReturnPath(r.URL.Query().Get("next"))
		http.Redirect(w, r, "/login?setup=google&next="+url.QueryEscape(next), http.StatusSeeOther)
	})
}

type googleMembership interface {
	EmailAllowed(email string) bool
	CanonicalUser(user auth.User) auth.User
	BindCoManagerOnSignIn(email, name string) (league.Member, bool, error)
	EnsureMember(email, name string) (league.Member, error)
}

func googleCallbackHandler(flow *auth.OAuth, manager *auth.Manager, configured bool) http.Handler {
	return googleCallbackHandlerWithMembership(flow, manager, configured, league.Default())
}

func googleCallbackHandlerWithMembership(flow *auth.OAuth, manager *auth.Manager, configured bool, membership googleMembership) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !configured {
			// Mirrors googleStartHandler's own unconfigured branch: this route
			// is unreachable through the normal /login → /auth/google/start
			// path while unconfigured (that redirect already carries next),
			// but a stale bookmark or a manually edited URL can still land
			// here directly, and the deep-link destination it carried must
			// not be dropped on the bounce back to /login.
			next := navigation.SafeReturnPath(r.URL.Query().Get("next"))
			http.Redirect(w, r, "/login?setup=google&next="+url.QueryEscape(next), http.StatusSeeOther)
			return
		}
		// Peeked before flow.Callback runs: auth.OAuth.Callback only returns
		// its stored "next" (state.Next) on success — a failure discards it,
		// dropping the destination the visitor was trying to reach. The same
		// session key holds it non-destructively until Callback's own
		// consumeState deletes it, so a plain, sanitized peek here recovers
		// it for the failure redirect below without altering the library's
		// own consume-once contract.
		next := oauthNextFromSession(r)
		user, target, err := flow.Callback(r, "google")
		if err != nil {
			session.AddFlash(r, "notice", "Google sign-in did not finish. Try again, or tell the commissioner.")
			http.Redirect(w, r, "/login?error=oauth&next="+url.QueryEscape(next), http.StatusSeeOther)
			return
		}
		completeSignIn(w, r, manager, membership, user, target, completeSignInOptions{
			NotAdmittedRedirect: "/login?error=invite",
			ErrorRedirect:       "/login?error=oauth",
		})
	})
}

// oauthSessionNext mirrors the one field this package needs from the
// vendored m31labs.dev/gosx/auth package's own unexported oauthState: its
// "next" JSON field, persisted on the session key auth.NewOAuth defaults
// to ("auth.oauth", never overridden by this app's OAuthOptions) while a
// Google round trip is in flight. See oauthNextFromSession's own comment
// for why this app reads it directly instead of through auth.OAuth.
type oauthSessionNext struct {
	Next string `json:"next,omitempty"`
}

// oauthNextFromSession peeks the deep-link destination the OAuth start
// handler saved for the in-flight round trip, sanitized the same way the
// start handler already sanitizes its own next query value. It is a
// read-only Decode (session.Store.Decode never deletes), so it never
// interferes with auth.OAuth.Callback's own single-consume of the same
// key later in the same request. An absent or malformed session value
// degrades to "/" through navigation.SafeReturnPath, matching every other
// next-preservation path in this file.
func oauthNextFromSession(r *http.Request) string {
	store := session.Current(r)
	if store == nil {
		return navigation.SafeReturnPath("")
	}
	var decoded oauthSessionNext
	if !store.Decode("auth.oauth", &decoded) {
		return navigation.SafeReturnPath("")
	}
	return navigation.SafeReturnPath(decoded.Next)
}

// completeSignInOptions lets each provider name its own truthful redirect
// for the two failure classes completeSignIn can hit (denied admission vs.
// an internal sign-in failure), while the admission order, canonicalization,
// co-manager binding, and success copy stay identical for every provider
// (design section 9). ErrorMessage/NotAdmittedMessage default to the shared
// provider-neutral copy below when left blank — no caller needs to repeat
// it, but a caller with a genuinely different truthful thing to say may
// still override it.
type completeSignInOptions struct {
	NotAdmittedRedirect string
	NotAdmittedMessage  string
	ErrorRedirect       string
	ErrorMessage        string
}

const (
	completeSignInDefaultNotAdmittedMessage = "That account is not admitted by this league's membership policy."
	completeSignInDefaultErrorMessage       = "Sign-in could not be completed. Try again."
)

// completeSignIn is the one sign-in completion chain every admission
// method shares (design section 9, extracted from the pre-slice-3
// googleCallbackHandlerWithMembership body): EmailAllowed → CanonicalUser →
// SignIn → BindCoManagerOnSignIn → EnsureMember → a truthful flash and
// redirect. Every caller (Google callback, invite-link consume today;
// transfer-code and magic-link consume in later slices) asserts an email
// through user.Email/user.Name and gets back the identical admission
// order, alias canonicalization, co-manager binding, and flash copy — so
// upgrading or downgrading between sign-in tiers can never diverge in
// which member row a signed-in identity lands on (service.go's
// CanonicalUser/hasPersistedMembership). Returns true on a completed sign-in.
func completeSignIn(w http.ResponseWriter, r *http.Request, manager *auth.Manager, membership googleMembership, user auth.User, target string, opts completeSignInOptions) bool {
	if opts.NotAdmittedMessage == "" {
		opts.NotAdmittedMessage = completeSignInDefaultNotAdmittedMessage
	}
	if opts.ErrorMessage == "" {
		opts.ErrorMessage = completeSignInDefaultErrorMessage
	}
	fail := func(redirect, message string) bool {
		manager.SignOut(r)
		session.AddFlash(r, "notice", message)
		http.Redirect(w, r, redirect, http.StatusSeeOther)
		return false
	}
	if !membership.EmailAllowed(user.Email) {
		return fail(opts.NotAdmittedRedirect, opts.NotAdmittedMessage)
	}
	// The provider signs the raw identity before this chain runs. Re-sign
	// the session with the canonical application identity after
	// raw-email admission succeeds, so every subsequent request shares the
	// same principal without allowing aliases to bypass admission.
	user = membership.CanonicalUser(user)
	if !manager.SignIn(r, user) {
		return fail(opts.ErrorRedirect, opts.ErrorMessage)
	}
	// Sign-in creates membership only (registration wave, build item 1 —
	// AssignManager's auto-seating at sign-in retires). Every signed-in,
	// allowed email is pick'em-enrolled by definition; claiming a fantasy
	// seat is now a deliberate act at /join (build item 2), not a side
	// effect of the first sign-in a member ever makes. AssignManager
	// itself stays live — /join's atomic claim calls it — this is the
	// only call site that retires.
	//
	// A co-manager invite is checked first: an email a primary invited
	// (Store.InviteCoManager) binds to that seat on this, its first
	// sign-in (BindCoManagerOnSignIn), rather than landing seatless. Every
	// provider — Google, an invite-link consume, a future magic link or
	// passkey — runs this exact same check, so a co-manager invite binds
	// identically no matter which sign-in tier the invitee first uses.
	member, bound, err := membership.BindCoManagerOnSignIn(user.Email, user.Name)
	if err != nil {
		return fail(opts.ErrorRedirect, opts.ErrorMessage)
	}
	if !bound {
		member, err = membership.EnsureMember(user.Email, user.Name)
		if err != nil {
			return fail(opts.ErrorRedirect, opts.ErrorMessage)
		}
	}
	if bound {
		primaryName := league.Default().PrimaryNameForTeam(member.TeamID, user.Email)
		teamLabel := league.Default().TeamLabel(member.TeamID)
		session.AddFlash(r, "notice", coManagerWelcomeFlash(teamLabel, primaryName))
		// F11a: a dedicated flash for the home page's first-session
		// arrival panel — the generic notice above never says what a
		// shared seat grants, and it may render on a deep-linked "next"
		// page rather than /.
		session.AddFlash(r, "co_manager_bound", map[string]any{
			"team_name":          teamLabel,
			"primary_first_name": league.FirstName(primaryName),
		})
	} else if member.TeamID != "" {
		session.AddFlash(r, "notice", "Welcome back to "+league.Default().TeamLabel(member.TeamID)+", "+user.Name+".")
	} else {
		session.AddFlash(r, "notice", "You're in, "+user.Name+". Claim a fantasy seat any time a spot opens, or head straight to Pick'em.")
	}
	http.Redirect(w, r, navigation.SafeReturnPath(target), http.StatusSeeOther)
	return true
}

// coManagerWelcomeFlash builds the sign-in flash for a co-manager invite
// just consumed (F6). It must credit the seat's primary manager, never the
// invitee who is reading it — the invitee already knows their own name.
func coManagerWelcomeFlash(teamLabel, primaryName string) string {
	primaryName = strings.TrimSpace(primaryName)
	if primaryName == "" {
		primaryName = "the primary manager"
	}
	return "You're co-managing " + teamLabel + " alongside its primary manager, " + primaryName + "."
}

func googleAuthConfigured() bool {
	return strings.TrimSpace(os.Getenv("GOOGLE_CLIENT_ID")) != "" && strings.TrimSpace(os.Getenv("GOOGLE_CLIENT_SECRET")) != ""
}

func mapPersistenceHealthError(err error) string {
	if err == nil {
		return ""
	}
	// Health callers need a truthful failure class, not filesystem paths,
	// SQL text, or other operator-only details that could disclose layout.
	return "persistence unavailable"
}

// persistenceHealth keeps the public readiness contract testable without
// booting the singleton service: persistence poison is a readiness failure,
// while optional feed degradation is reported separately and never reaches
// this decision.
func persistenceHealth(err error) (ready bool, status int, publicError string) {
	if err != nil {
		return false, http.StatusServiceUnavailable, mapPersistenceHealthError(err)
	}
	return true, http.StatusOK, ""
}

func stateSchemaPayload(value league.StateSchemaCompatibility) map[string]any {
	return map[string]any{
		"persistedVersion":         value.PersistedVersion,
		"supportedVersion":         value.SupportedVersion,
		"persistedDatabaseVersion": value.PersistedDatabaseVersion,
		"supportedDatabaseVersion": value.SupportedDatabaseVersion,
		"compatible":               value.Compatible,
	}
}

func livenessPayload() map[string]any {
	return map[string]any{"ok": true, "liveness": true}
}

func getenv(key string, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
