package main

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gridiron-2000/internal/league"
	"m31labs.dev/gosx"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
	"m31labs.dev/gosx/session"
)

// setupSessionCookieName is deliberately distinct from the CONFIGURED app's
// "gridiron_session" cookie: SETUP and CONFIGURED never run at the same
// time in one process (main.go picks exactly one boot state), but keeping
// the cookie namespace separate means a stale setup-phase cookie can never
// be mistaken for league sign-in state after a restart into CONFIGURED.
const setupSessionCookieName = "gridiron_setup_session"

// setupSessionEpochKey is the session value a successful token claim writes
// (setup_token.go's setupTokenGuard.Claim epoch). Every later /setup request
// must present the same value to stay authorized.
const setupSessionEpochKey = "setup_epoch"

// setupSessionOptions mirrors gridironSessionOptions' local/deployed cookie
// policy split, under the setup-only cookie name. MaxAge is generous (the
// token guard's own 60-minute idle rule is the real, tight expiry); this
// only bounds how long a claimed-but-abandoned browser tab's cookie survives
// at the transport layer.
func setupSessionOptions(appEnv string) session.Options {
	localHTTP := isLocalAppEnv(appEnv)
	return session.Options{
		CookieName:    setupSessionCookieName,
		Secure:        !localHTTP,
		AllowInsecure: localHTTP,
		HTTPOnly:      true,
		Encrypt:       true,
		MaxAge:        24 * time.Hour,
		SameSite:      http.SameSiteLaxMode,
	}
}

// SetupRuntime holds what main() needs to keep after BuildSetupApp
// returns: the token guard, the Store the wizard persists progress into,
// the wizard's in-memory state manager, and the commit completion state
// (design section 4.5's hybrid restart behavior — see completion.go).
type SetupRuntime struct {
	Guard  *setupTokenGuard
	Store  *league.Store
	Wizard *wizardStateManager
	// Restart is the process-restart side effect a successful commit
	// triggers (owner decision, design open decision 7): os.Exit(0) only
	// when GRIDIRON_SUPERVISED=1; otherwise a no-op, and the process keeps
	// serving the completion page. A field, not a bare function call, so
	// a test can observe/replace it without ever invoking the real exit.
	Restart func()

	mu         sync.Mutex
	completion *wizardCommitResult
}

// Completion returns the commit result once a commit has succeeded, or nil
// before that.
func (rt *SetupRuntime) Completion() *wizardCommitResult {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return rt.completion
}

// SetCompletion records a successful commit. Every later /setup request —
// including one that presents a still-valid session — renders the
// completion state instead of any wizard page from this point on: the
// process's SETUP lifetime is over even if it has not restarted yet.
func (rt *SetupRuntime) SetCompletion(result wizardCommitResult) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.completion = &result
}

// publicBaseURLForSetup resolves the best-effort base URL for the boot
// banner's full tokenized link. LEAGUE_URL may already be set even before a
// league.json exists (an operator who knows their public host ahead of
// time); otherwise fall back to localhost at the bound port, matching the
// "open http://<host>:<port>/setup" example in the design.
func publicBaseURLForSetup(cfg AppConfig) string {
	if url := strings.TrimSpace(os.Getenv("LEAGUE_URL")); url != "" {
		return strings.TrimSuffix(url, "/")
	}
	return "http://localhost:" + cfg.Port
}

// defaultRestartHook implements the design's hybrid restart behavior
// (owner decision): GRIDIRON_SUPERVISED=1 exits the process so the
// supervisor (the compose lane's restart: policy) restarts it into
// CONFIGURED; otherwise this is a no-op and the process keeps serving the
// completion page until a human restarts it.
func defaultRestartHook() func() {
	return func() {
		if strings.TrimSpace(os.Getenv("GRIDIRON_SUPERVISED")) == "1" {
			osExit(0)
		}
	}
}

// BuildSetupApp assembles the SETUP-state HTTP application (design section
// 3.4): only /setup and its step pages/actions, /styles.css and static
// assets, /api/live, and /api/health (reporting state "setup", not ready).
// It never touches league.Default() or runs any league code — see
// BuildApp's own doc comment for the CONFIGURED-state twin. store is the
// Store DetermineBootState already opened at the same path Default() will
// find after the wizard's atomic commit and the process restarts.
func BuildSetupApp(cfg AppConfig, store *league.Store) (*server.App, *SetupRuntime, error) {
	baseURL := publicBaseURLForSetup(cfg)
	return buildSetupAppWithTokenSink(cfg, store, func(token string) {
		printSetupTokenBanner(baseURL, token)
	})
}

// buildSetupAppWithTokenSink is BuildSetupApp's testable core: tokenSink
// receives the raw token at every mint (initial and every idle re-mint),
// so a test can observe it without scraping stdout/log output.
func buildSetupAppWithTokenSink(cfg AppConfig, store *league.Store, tokenSink func(string)) (*server.App, *SetupRuntime, error) {
	guard, err := newSetupTokenGuard(setupTokenIdleTimeout, time.Now, tokenSink)
	if err != nil {
		return nil, nil, err
	}
	limiter := newSetupRateLimiter(5, time.Minute, 30*time.Second, time.Now)
	wizard, err := newWizardStateManager(store, time.Now)
	if err != nil {
		return nil, nil, err
	}

	sessions, err := session.New(cfg.SessionKey, setupSessionOptions(cfg.AppEnv))
	if err != nil {
		return nil, nil, err
	}

	rt := &SetupRuntime{Guard: guard, Store: store, Wizard: wizard, Restart: defaultRestartHook()}

	router := route.NewRouter()
	router.SetLayout(func(ctx *route.RouteContext, body gosx.Node) gosx.Node {
		ctx.SetLanguage("en")
		ctx.SetMetadata(server.Metadata{
			Links: []server.LinkTag{
				{Rel: "stylesheet", Href: "/styles.css"},
				{Rel: "icon", Href: "/favicon.svg", Type: "image/svg+xml"},
			},
			ThemeColor: []server.ThemeColor{{Color: "#070A16"}},
		})
		return server.HTMLDocument(ctx.Document("Gridiron Setup", body))
	})
	router.Add(route.Route{Pattern: "/setup", Handler: setupRootPageHandler(rt)})
	router.Handle("POST /setup", setupClaimActionHandler(rt, limiter))
	registerWizardRoutes(router, rt)

	app := server.New()
	app.EnableSecurityPolicy(gridironSecurityPolicy())
	app.Use(sessions.Middleware)
	app.Use(sessions.Protect)
	app.SetPublicDir(filepath.Join(cfg.Root, "public"))

	app.API("GET /api/live", func(ctx *server.Context) (any, error) {
		ctx.NoStore()
		return livenessPayload(), nil
	})
	app.API("GET /api/health", func(ctx *server.Context) (any, error) {
		ctx.NoStore()
		return setupHealthPayload(rt), nil
	})

	rootHandler, err := router.BuildChecked()
	if err != nil {
		return nil, nil, err
	}
	app.Mount("/", rootHandler)

	return app, rt, nil
}

// setupHealthPayload is the design's truthful "setup" health report: ready
// is always false until a commit has actually happened, and state names
// exactly which boot state is live so an operator or orchestrator never
// has to infer it from the absence of other fields. Once a commit
// succeeds the state read "setup_complete_pending_restart" — the process
// is still technically SETUP, but truthfully done and waiting to restart.
func setupHealthPayload(rt *SetupRuntime) map[string]any {
	state := "setup"
	if rt.Completion() != nil {
		state = "setup_complete_pending_restart"
	}
	return map[string]any{
		"ok":         true,
		"liveness":   true,
		"readiness":  false,
		"state":      state,
		"version":    appVersion,
		"appVersion": appVersion,
		"gitSHA":     appGitSHA,
		"buildDate":  appBuildDate,
		"time":       time.Now().UTC().Format(time.RFC3339),
	}
}

// wizardAuthorized reports whether r's session still carries the setup
// token guard's current claimed epoch.
func wizardAuthorized(rt *SetupRuntime, r *http.Request) bool {
	epoch := ""
	if store := session.Current(r); store != nil {
		epoch = store.String(setupSessionEpochKey)
	}
	return rt.Guard.Authorized(epoch)
}

// wizardPageGuard wraps a wizard page's real render function with the
// completion check and the token-session authorization check, so every
// GET route shares the exact same gate instead of repeating it. Touch
// resets the 60-minute idle clock on every authorized wizard page view.
func wizardPageGuard(rt *SetupRuntime, next func(ctx *route.RouteContext) gosx.Node) route.PageHandler {
	return func(ctx *route.RouteContext) gosx.Node {
		ctx.NoStore()
		if result := rt.Completion(); result != nil {
			return setupCompletionNode(ctx, *result)
		}
		if !wizardAuthorized(rt, ctx.Request) {
			return setupTokenEntryNode(ctx)
		}
		rt.Guard.Touch()
		return next(ctx)
	}
}

// wizardActionGuard is wizardPageGuard's POST twin: it redirects to /setup
// (which then renders whichever of completion/token-entry/wizard applies)
// instead of rendering inline, since an action handler's job is to
// mutate-then-redirect either way.
func wizardActionGuard(rt *SetupRuntime, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if rt.Completion() != nil {
			http.Redirect(w, r, "/setup", http.StatusSeeOther)
			return
		}
		if !wizardAuthorized(rt, r) {
			http.Redirect(w, r, "/setup", http.StatusSeeOther)
			return
		}
		rt.Guard.Touch()
		next.ServeHTTP(w, r)
	})
}

func setupRootPageHandler(rt *SetupRuntime) route.PageHandler {
	return wizardPageGuard(rt, func(ctx *route.RouteContext) gosx.Node {
		target := "/setup/" + rt.Wizard.View().Status.FirstIncompleteStep()
		return metaRefreshNode(target, "Continuing to the setup wizard.")
	})
}

func setupClaimActionHandler(rt *SetupRuntime, limiter *setupRateLimiter) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if rt.Completion() != nil {
			http.Redirect(w, r, "/setup", http.StatusSeeOther)
			return
		}
		if !limiter.Allow(setupRequestIP(r)) {
			session.AddFlash(r, "notice", "Too many attempts. Wait 30 seconds and try again.")
			http.Redirect(w, r, "/setup", http.StatusSeeOther)
			return
		}
		if err := r.ParseForm(); err != nil {
			session.AddFlash(r, "notice", "That submission could not be read. Try again.")
			http.Redirect(w, r, "/setup", http.StatusSeeOther)
			return
		}
		candidate := strings.TrimSpace(r.PostFormValue("token"))
		epoch, ok, already := rt.Guard.Claim(candidate)
		switch {
		case already:
			session.AddFlash(r, "notice", "This setup link has already been claimed. Restart the container to mint a fresh token.")
		case !ok:
			session.AddFlash(r, "notice", "That token is not valid. Check the token printed at boot.")
		default:
			if store := session.Current(r); store != nil {
				store.Set(setupSessionEpochKey, epoch)
			}
			session.AddFlash(r, "notice", "Setup token accepted.")
		}
		http.Redirect(w, r, "/setup", http.StatusSeeOther)
	})
}
