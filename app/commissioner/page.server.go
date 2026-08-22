package commissioner

import (
	"context"
	"net/http"
	"time"

	"gridiron-2000/internal/commissionerhq"
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

func commissionerPageData(request *http.Request) map[string]any {
	isCommissioner := league.Default().IsCommissioner(request)
	if !isCommissioner {
		return commissionerPageDataWithReader(request, false, nil, false)
	}
	service := commissionerhq.Default()
	return commissionerPageDataWithReader(request, true, service.Fleet, service.Enabled())
}

func commissionerPageDataWithReader(request *http.Request, isCommissioner bool, reader func(context.Context) []commissionerhq.FleetEntry, federationEnabled bool) map[string]any {
	data := map[string]any{
		"viewer": league.Default().Viewer(request), "is_commissioner": isCommissioner,
		"cards": []map[string]any{}, "attention_queue": []map[string]any{},
		"league_count": 0, "attention_count": 0, "critical_count": 0, "warning_count": 0,
		"claimed_seats": 0, "total_seats": 0, "drafts_live": 0,
		"generated_at": "—", "generated_at_iso": "", "federation_enabled": federationEnabled,
	}
	if !isCommissioner || reader == nil {
		return data
	}
	view := buildFleetView(reader(request.Context()), timeNow())
	for key, value := range view.toData() {
		data[key] = value
	}
	return data
}

// timeNow is a seam for render tests and keeps the view model request-local.
var timeNow = func() time.Time { return time.Now().UTC() }

// fleetCard remains the narrow contract used by existing renderer tests. The
// full page and live fragment both use the richer model behind it.
func fleetCard(entry commissionerhq.FleetEntry) map[string]any {
	return cardView(entry).toMap()
}
