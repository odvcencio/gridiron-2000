package commissioner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"time"

	"gridiron-2000/internal/commissionerhq"
	"gridiron-2000/internal/commissionerhq/v1fleet"
	"gridiron-2000/internal/league"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
)

func init() {
	if err := route.RegisterFileModuleHere(route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			ctx.NoStore()
			ctx.Runtime().EnableBootstrap()
			return commissionerPageData(ctx.Request), nil
		},
		Metadata: func(ctx *route.RouteContext, page route.FilePage, data any) (server.Metadata, error) {
			return server.Metadata{
				Title:       server.Title{Default: league.PageTitle("Commissioner HQ")},
				Description: "Read-only fleet status for every league this commissioner operates.",
			}, nil
		},
	}); err != nil {
		panic(err)
	}
}

type fleetReadoutProps struct {
	IsCommissioner    bool
	FederationEnabled bool
	Cards             []map[string]any
	AttentionQueue    []map[string]any
	LeagueCount       int
	// LeagueWord is LeagueCount's pluralized noun ("LEAGUE" for exactly
	// one, "LEAGUES" otherwise — league.Plural), precomputed here because
	// a GoSX template cannot call an arbitrary Go function inline
	// (wave-2 audit: the masthead read "1 LEAGUES" for a single-league
	// fleet).
	LeagueWord          string
	ClaimedSeats        int
	TotalSeats          int
	DraftsLive          int
	AttentionCount      int
	CriticalCount       int
	WarningCount        int
	GeneratedAt         string
	GeneratedAtISO      string
	GeneratedAtRelative string
	HQV1Enabled         bool
	HQV1                hqV1PortfolioProps
}

var hqV1Service *v1fleet.Service

// SetHQV1Fleet installs the process-local, read-only HQ v1 collector. The
// main package calls this once during startup; keeping the setter here keeps
// route rendering independent from process wiring and makes disabled hosting
// an explicit, harmless state.
func SetHQV1Fleet(service *v1fleet.Service) { hqV1Service = service }

func emptyFleetReadout(isCommissioner, federationEnabled bool) fleetReadoutProps {
	return fleetReadoutProps{
		IsCommissioner: isCommissioner, FederationEnabled: federationEnabled,
		Cards: []map[string]any{}, AttentionQueue: []map[string]any{},
		LeagueWord:  league.Plural(0, "LEAGUE"),
		GeneratedAt: "—",
		HQV1:        emptyHQV1Portfolio(),
	}
}

// readoutFromView projects view (already built with its own location, see
// buildFleetView) into the page's render props. Every timestamp on data
// was already converted through view.Location by toData(); this only
// reads it back out, so there is exactly one place that resolves the
// league zone for a given fleet snapshot.
func readoutFromView(view fleetPageView, isCommissioner, federationEnabled bool) fleetReadoutProps {
	data := view.toData()
	return fleetReadoutProps{
		IsCommissioner: isCommissioner, FederationEnabled: federationEnabled,
		Cards:          data["cards"].([]map[string]any),
		AttentionQueue: data["attention_queue"].([]map[string]any),
		LeagueCount:    view.LeagueCount, LeagueWord: league.Plural(view.LeagueCount, "LEAGUE"),
		ClaimedSeats: view.ClaimedSeats,
		TotalSeats:   view.TotalSeats, DraftsLive: view.DraftsLive,
		AttentionCount: view.AttentionCount, CriticalCount: view.CriticalCount,
		WarningCount: view.WarningCount, GeneratedAt: data["generated_at"].(string),
		GeneratedAtISO:      data["generated_at_iso"].(string),
		GeneratedAtRelative: data["generated_at_relative"].(string),
		HQV1:                emptyHQV1Portfolio(),
	}
}

func withHQV1Portfolio(request *http.Request, readout fleetReadoutProps, isCommissioner bool) fleetReadoutProps {
	readout.HQV1 = emptyHQV1Portfolio()
	if !isCommissioner || hqV1Service == nil || !hqV1Service.Enabled() {
		return readout
	}
	readout.HQV1Enabled = true
	readout.HQV1 = hqV1PortfolioView(hqV1Service.Collect(request.Context()), timeNow())
	return readout
}

func commissionerPageData(request *http.Request) map[string]any {
	isCommissioner := league.Default().IsCommissioner(request)
	if !isCommissioner {
		return commissionerPageDataWithReader(request, false, nil, false)
	}
	service := commissionerhq.Default()
	return commissionerPageDataWithReader(request, true, service.Fleet, service.Enabled())
}

func commissionerPageDataWithReader(request *http.Request, isCommissioner bool, reader func(context.Context) []commissionerhq.FleetEntry, federationEnabled bool) map[string]any {
	readout := emptyFleetReadout(isCommissioner, federationEnabled)
	if isCommissioner && reader != nil {
		readout = readoutFromView(buildFleetView(reader(request.Context()), timeNow(), league.Default().LeagueLocation()), true, federationEnabled)
	}
	readout = withHQV1Portfolio(request, readout, isCommissioner)
	return map[string]any{
		"viewer": league.Default().Viewer(request), "is_commissioner": isCommissioner,
		"fleet": readout,
	}
}

// FragmentHandler serves the same fleet readout used by the full SSR page.
// Authentication, commissioner authorization, and peer reads all happen on the
// server; the browser receives no peer service address or trust material.
func FragmentHandler(service *commissionerhq.Service) http.Handler {
	var reader func(context.Context) []commissionerhq.FleetEntry
	federationEnabled := false
	if service != nil {
		reader = service.Fleet
		federationEnabled = service.Enabled()
	}
	return fragmentHandler(commissionerFragmentAccess, reader, federationEnabled)
}

func commissionerFragmentAccess(request *http.Request) (int, bool) {
	service := league.Default()
	if _, signedIn := service.CurrentUser(request); !signedIn && !service.DemoMode() {
		return http.StatusUnauthorized, false
	}
	if !service.IsCommissioner(request) {
		return http.StatusForbidden, false
	}
	return 0, true
}

func fragmentHandler(
	access func(*http.Request) (int, bool),
	reader func(context.Context) []commissionerhq.FleetEntry,
	federationEnabled bool,
) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		setCommissionerFragmentPrivacyHeaders(writer)
		if request.Method != http.MethodGet {
			writer.Header().Set("Allow", http.MethodGet)
			http.Error(writer, "The league readout is unavailable. Reload the page.", http.StatusMethodNotAllowed)
			return
		}
		if status, allowed := access(request); !allowed {
			http.Error(writer, "The league readout is unavailable. Reload the page.", status)
			return
		}

		readout := emptyFleetReadout(true, federationEnabled)
		if reader != nil {
			readout = readoutFromView(buildFleetView(reader(request.Context()), timeNow(), league.Default().LeagueLocation()), true, federationEnabled)
		}
		readout = withHQV1Portfolio(request, readout, true)
		program, err := route.LoadFileProgramHere("page.gsx")
		if err != nil {
			http.Error(writer, "The league readout is unavailable. Reload the page.", http.StatusInternalServerError)
			return
		}
		html, err := route.RenderProgramComponent(program, "FleetReadout", route.ProgramRenderEnv{
			Values: map[string]any{"props": readout},
		})
		if err != nil {
			http.Error(writer, "The league readout is unavailable. Reload the page.", http.StatusInternalServerError)
			return
		}
		writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		etag := commissionerFragmentETag(html)
		writer.Header().Set("ETag", etag)
		if commissionerETagMatches(request.Header.Get("If-None-Match"), etag) {
			writer.WriteHeader(http.StatusNotModified)
			return
		}
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte(html))
	})
}

func setCommissionerFragmentPrivacyHeaders(writer http.ResponseWriter) {
	writer.Header().Set("Cache-Control", "private, no-store")
	writer.Header().Set("Vary", "Cookie")
}

func commissionerFragmentETag(html string) string {
	digest := sha256.Sum256([]byte(html))
	return `"` + hex.EncodeToString(digest[:]) + `"`
}

func commissionerETagMatches(header, current string) bool {
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(candidate), "W/"))
		if candidate == "*" || candidate == current {
			return true
		}
	}
	return false
}

// timeNow is a seam for render tests and keeps the view model request-local.
var timeNow = func() time.Time { return time.Now().UTC() }

// fleetCard remains the narrow contract used by existing renderer tests. The
// full page and live fragment both use the richer model behind it.
func fleetCard(entry commissionerhq.FleetEntry) map[string]any {
	return cardView(entry, timeNow(), time.UTC, false).toMap()
}
