package wire

import (
	"fmt"
	"log"
	"net/http"
	neturl "net/url"
	"strings"
	"time"

	"gridiron-2000/internal/league"
	"gridiron-2000/internal/openstats"
	signalwire "gridiron-2000/internal/wire"
	"m31labs.dev/gosx"
	"m31labs.dev/gosx/action"
	"m31labs.dev/gosx/auth"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
	"m31labs.dev/gosx/session"
)

// wireFilterOption is one entry in the wire page's category filter strip
// (WireFilterOptions below). Slug is the query-string value the fragment
// endpoint and the page's own Load both read; Label is the button text.
type wireFilterOption struct {
	Slug  string
	Label string
}

// WireFilterOptions is the fixed category vocabulary the wire feed accepts,
// in display order. The "All" entry's empty Slug means "no category
// filter" everywhere a category is read (Recent, the fragment endpoint).
var WireFilterOptions = []wireFilterOption{
	{"", "All"},
	{"touchdown", "Scores"},
	{"injury", "Injuries"},
	{"practice", "Practice"},
	{"inactive", "Inactive"},
	{"transaction", "Moves"},
	{"weather", "Weather"},
	{"news", "News"},
	{"market", "Market"},
	{"community", "Tips"},
}

// wireFilterMaps renders WireFilterOptions as plain, soft-navigable links
// (gosx#215 replaced the click-to-refetch JS buttons): each link's href
// carries the category in the query string, so choosing a filter is a
// normal same-origin navigation the page's own EnableNavigation() runtime
// already soft-swaps — no bespoke fetch/classList JS is needed for this
// either. active marks the filter matching the request's own category, so
// the freshly-rendered page always shows the correct pill highlighted.
func wireFilterMaps(category string) []map[string]any {
	out := make([]map[string]any, 0, len(WireFilterOptions))
	for _, opt := range WireFilterOptions {
		href := "/wire"
		if opt.Slug != "" {
			href = "/wire?category=" + neturl.QueryEscape(opt.Slug)
		}
		out = append(out, map[string]any{
			"slug":   opt.Slug,
			"label":  opt.Label,
			"href":   href,
			"active": opt.Slug == category,
		})
	}
	return out
}

// wireFragmentURL is the data-gosx-region-url the wire feed polls
// (gosx#217): /wire/fragment, mirroring the current category filter so a
// periodic poll never drops back to the unfiltered list.
func wireFragmentURL(category string) string {
	if category == "" {
		return "/wire/fragment"
	}
	return "/wire/fragment?category=" + neturl.QueryEscape(category)
}

func init() {
	if err := route.RegisterFileModuleHere(route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			ctx.NoStore()
			// The wire feed's data-gosx-region/-interval poll (gosx#217)
			// needs the declarative-regions client module, which ships in
			// the shared bootstrap runtime, not the lean navigation script
			// every page already loads. EnableBootstrap with zero islands,
			// engines, hubs, or controllers registered on this page
			// resolves to the small "lite" bootstrap bundle (~29 KB
			// gzipped), not the full island runtime.
			ctx.Runtime().EnableBootstrap()
			signals, err := signalwire.Default()
			if err != nil {
				return nil, err
			}
			stats, err := openstats.Default()
			if err != nil {
				return nil, err
			}
			data := wirePageData(ctx.Request, signals, stats)
			applySubmissionState(ctx, data)
			return data, nil
		},
		Metadata: func(ctx *route.RouteContext, page route.FilePage, data any) (server.Metadata, error) {
			return server.Metadata{
				Title:       server.Title{Default: league.PageTitle("Signal Wire")},
				Description: "The private league's no-key publisher, community, social, and open NFL intelligence mesh.",
			}, nil
		},
		Actions: route.FileActions{
			"submit-sighting": func(ctx *action.Context) error {
				reporterID, reporterName, ok := sightingReporter(ctx.Request)
				if !ok {
					return action.Error(http.StatusUnauthorized, "Google sign-in is required to add a sighting")
				}
				signals, err := signalwire.Default()
				if err != nil {
					return action.Error(http.StatusServiceUnavailable, "The signal wire is unavailable")
				}
				signal, err := signals.SubmitSighting(signalwire.CommunitySubmission{
					ReporterID:   reporterID,
					ReporterName: reporterName,
					EvidenceType: ctx.FormData["evidence_type"],
					SourceName:   ctx.FormData["source_name"],
					SourceURL:    ctx.FormData["source_url"],
					Summary:      ctx.FormData["summary"],
				})
				if err != nil {
					return action.Validation(err.Error(), sightingFieldErrors(err), ctx.FormData)
				}
				session.AddFlash(ctx.Request, "notice", fmt.Sprintf("%s added to the provisional wire.", signal.Label))
				ctx.Redirect("/wire#community-input")
				return nil
			},
		},
	}); err != nil {
		log.Fatal(err)
	}
}

func wirePageData(request *http.Request, signals *signalwire.Service, stats *openstats.Service) map[string]any {
	wireStatus := signals.Status()
	openStatus := stats.Status()
	viewer := league.Default().Viewer(request)
	category := strings.TrimSpace(request.URL.Query().Get("category"))
	recent := signals.Recent(50, category)
	items := make([]map[string]any, 0, len(recent))
	for _, signal := range recent {
		items = append(items, signalMap(signal))
	}
	sources := make([]map[string]any, 0, len(wireStatus.Sources))
	for _, source := range wireStatus.Sources {
		name := source.Handle
		if name == "" {
			name = shortDID(source.DID)
		}
		sources = append(sources, map[string]any{
			"name": name,
			"did":  source.DID,
		})
	}
	feeds := make([]map[string]any, 0, len(wireStatus.Feeds))
	readyFeeds := 0
	feedIgnored := int64(0)
	for _, feed := range wireStatus.Feeds {
		if feed.State == "ready" {
			readyFeeds++
		}
		feedIgnored += feed.Ignored
		feeds = append(feeds, map[string]any{
			"name":       feed.Name,
			"url":        feed.URL,
			"evidence":   strings.ToUpper(strings.ReplaceAll(feed.EvidenceType, "_", " ")),
			"state":      strings.ToUpper(strings.ReplaceAll(feed.State, "_", " ")),
			"accepted":   feed.Accepted,
			"ignored":    feed.Ignored,
			"checked":    displayTime(feed.LastChecked),
			"has_error":  feed.LastError != "",
			"last_error": feed.LastError,
		})
	}
	lastID := ""
	if len(recent) > 0 {
		lastID = recent[0].ID
	}
	return map[string]any{
		"viewer":           viewer,
		"signals":          items,
		"empty":            len(items) == 0,
		"last_event_id":    lastID,
		"category":         category,
		"filters":          wireFilterMaps(category),
		"fragment_url":     wireFragmentURL(category),
		"wire_mode":        strings.ToUpper(strings.ReplaceAll(wireStatus.Mode, "_", " ")),
		"wire_configured":  wireStatus.Configured,
		"wire_issue":       wireStatus.ConfigurationIssue,
		"wire_error":       wireStatus.LastError,
		"source_count":     len(wireStatus.Sources) + len(wireStatus.Feeds),
		"bluesky_count":    len(wireStatus.Sources),
		"feed_count":       len(wireStatus.Feeds),
		"feed_ready":       readyFeeds,
		"feeds":            feeds,
		"sources":          sources,
		"can_submit":       league.Default().DemoMode() || viewer["signed_in"] == true,
		"signal_count":     wireStatus.RelevantSignals,
		"ignored_count":    wireStatus.IgnoredPosts + feedIgnored,
		"deleted_count":    wireStatus.DeletedSignals,
		"schedule_state":   strings.ToUpper(strings.ReplaceAll(openStatus.Schedules.State, "_", " ")),
		"schedule_rows":    openStatus.Schedules.Rows,
		"schedule_updated": displayTime(openStatus.Schedules.LastUpdated),
		"player_state":     strings.ToUpper(strings.ReplaceAll(openStatus.PlayerStats.State, "_", " ")),
		"player_rows":      openStatus.PlayerStats.Rows,
		"player_updated":   displayTime(openStatus.PlayerStats.LastUpdated),
		"injury_state":     strings.ToUpper(strings.ReplaceAll(openStatus.Injuries.State, "_", " ")),
		"injury_rows":      openStatus.Injuries.Rows,
		"injury_updated":   displayTime(openStatus.Injuries.LastUpdated),
		"season":           openStatus.Season,
		"license":          openStatus.License,
		"attribution_url":  openStatus.AttributionURL,
		"refresh_seconds":  20,
	}
}

// FeedFragment renders the markup /wire/fragment answers with (main.go's
// GET /wire/fragment handler): the data-gosx-region primitive (gosx#217)
// replaces the wire feed's children with this response verbatim.
//
// It cannot call Page()'s own SignalCard/WireEmptyState: a .gsx file's
// component functions are resolved through gosx's own file-routing render
// pipeline, not linked as ordinary Go symbols, so no hand-written .go file
// outside that pipeline — this one included — can call them directly (a
// plain `go build` reports them "undefined", confirmed against this
// module's own build server-binary step). signalCardNode and
// wireEmptyStateNode below re-express the same two markup shapes with
// gosx's low-level Node API (El/Attrs/Text) instead, over the exact same
// signalMap data each row already uses, so only the HTML structure is
// duplicated, not the field derivation. Keep both pairs in sync by hand;
// see the doc comment on signalCardNode for the upstream gap this reflects.
func FeedFragment(request *http.Request, signals *signalwire.Service) gosx.Node {
	category := strings.TrimSpace(request.URL.Query().Get("category"))
	wireStatus := signals.Status()
	recent := signals.Recent(50, category)
	if len(recent) == 0 {
		return wireEmptyStateNode(wireStatus.Configured, wireStatus.ConfigurationIssue)
	}
	cards := make([]gosx.Node, 0, len(recent))
	for _, signal := range recent {
		cards = append(cards, signalCardNode(signalMap(signal)))
	}
	return gosx.Fragment(cards...)
}

// signalCardNode is a hand-written mirror of page.gsx's SignalCard
// component — see FeedFragment's doc comment for why a fragment handler
// outside the file-routing pipeline cannot call SignalCard itself. Keep
// this in exact structural sync with SignalCard by hand; a mismatch here
// is invisible to `gosx check` (it only validates the .gsx side).
func signalCardNode(props map[string]any) gosx.Node {
	category := stringProp(props, "category")
	footer := []gosx.Node{
		gosx.El("span", gosx.Attrs(gosx.Attr("class", "mono")), gosx.Text(stringProp(props, "source"))),
	}
	if boolProp(props, "has_reporter") {
		footer = append(footer, gosx.El("span", gosx.Attrs(gosx.Attr("class", "mono")), gosx.Text("VIA "+stringProp(props, "reported_by"))))
	}
	if boolProp(props, "has_corroboration") {
		footer = append(footer, gosx.El("span", gosx.Attrs(gosx.Attr("class", "wire-event__corroboration mono")), gosx.Text(stringProp(props, "corroboration_label"))))
	}
	footer = append(footer, gosx.El("time", gosx.Attrs(gosx.Attr("class", "mono")), gosx.Text(stringProp(props, "time"))))
	if boolProp(props, "has_url") {
		footer = append(footer, gosx.El("a", gosx.Attrs(
			gosx.Attr("href", stringProp(props, "url")),
			gosx.Attr("target", "_blank"),
			gosx.Attr("rel", "noreferrer"),
		), gosx.Text("Inspect source ↗")))
	}
	return gosx.El("article", gosx.Attrs(
		gosx.Attr("class", "wire-event wire-event--"+category),
		gosx.Attr("data-wire-event", stringProp(props, "id")),
		gosx.Attr("data-wire-category", category),
	),
		gosx.El("header", nil,
			gosx.El("div", gosx.Attrs(gosx.Attr("class", "wire-event__heading")),
				gosx.El("span", gosx.Attrs(gosx.Attr("class", "wire-event__label")), gosx.Text(stringProp(props, "label"))),
				gosx.El("span", gosx.Attrs(gosx.Attr("class", "wire-event__evidence")), gosx.Text(stringProp(props, "evidence"))),
			),
			gosx.El("span", gosx.Attrs(gosx.Attr("class", "wire-event__trust mono")), gosx.Text(stringProp(props, "trust")+" · "+stringProp(props, "confidence")+"%")),
		),
		gosx.El("p", nil, gosx.Text(stringProp(props, "text"))),
		gosx.El("footer", nil, gosx.Fragment(footer...)),
	)
}

// wireEmptyStateNode is a hand-written mirror of page.gsx's WireEmptyState
// component — see FeedFragment's doc comment for why. Keep this in exact
// structural sync with WireEmptyState by hand.
func wireEmptyStateNode(wireConfigured bool, wireIssue string) gosx.Node {
	body := []gosx.Node{
		gosx.El("span", gosx.Attrs(gosx.Attr("class", "mono")), gosx.Text("NO RELEVANT PACKETS YET")),
		gosx.El("h3", nil, gosx.Text("Your wire is quiet—not broken.")),
	}
	if !wireConfigured {
		body = append(body,
			gosx.El("p", nil, gosx.Text(wireIssue)),
			gosx.El("p", nil,
				gosx.Text("Enable the built-in public feeds, add a feed file, or put trusted reporter and team handles in "),
				gosx.El("span", gosx.Attrs(gosx.Attr("class", "inline-code")), gosx.Text("BLUESKY_HANDLES")),
				gosx.Text("."),
			),
		)
	} else {
		body = append(body, gosx.El("p", nil, gosx.Text("The server is listening. Relevant feed items and league sightings appear here and remain provisional until reconciliation.")))
	}
	return gosx.El("div", gosx.Attrs(gosx.Attr("class", "wire-empty"), gosx.BoolAttr("data-wire-empty")), gosx.Fragment(body...))
}

func stringProp(props map[string]any, key string) string {
	value, _ := props[key].(string)
	return value
}

func boolProp(props map[string]any, key string) bool {
	value, _ := props[key].(bool)
	return value
}

// PulseData is the small polled JSON object /api/wire/pulse answers with
// (main.go's wirePulseHandler): the data-gosx-live-bind text region on the
// wire page's masthead and status line (gosx#217) reads these three flat
// keys — "mode", "count", "status" — replacing the old bespoke
// syncWire()'s setText calls for the same three elements.
func PulseData(signals *signalwire.Service) map[string]any {
	wireStatus := signals.Status()
	count := wireStatus.RelevantSignals
	status := fmt.Sprintf("%d relevant packet%s · %s", count, pluralSuffix(count), displayTime(time.Now()))
	return map[string]any{
		"mode":   strings.ToUpper(strings.ReplaceAll(wireStatus.Mode, "_", " ")),
		"count":  count,
		"status": status,
	}
}

func pluralSuffix(count int64) string {
	if count == 1 {
		return ""
	}
	return "s"
}

func signalMap(signal signalwire.Signal) map[string]any {
	source := signal.SourceName
	if source == "" {
		source = signal.SourceHandle
	}
	if source == "" {
		source = shortDID(signal.SourceDID)
	}
	if source == "" {
		source = "League source"
	}
	corroborationLabel := "1 SOURCE"
	if signal.Corroborations > 1 {
		corroborationLabel = fmt.Sprintf("%d SOURCES", signal.Corroborations)
	}
	return map[string]any{
		"id":                  signal.ID,
		"category":            signal.Category,
		"label":               signal.Label,
		"text":                signal.Text,
		"source":              source,
		"reported_by":         signal.ReportedBy,
		"has_reporter":        signal.ReportedBy != "",
		"evidence":            strings.ToUpper(strings.ReplaceAll(signal.EvidenceType, "_", " ")),
		"trust":               signal.TrustTier,
		"time":                displayTime(signal.OccurredAt),
		"url":                 signal.SourceURL,
		"has_url":             signal.SourceURL != "",
		"rule":                signal.Rule,
		"confidence":          fmt.Sprintf("%.0f", signal.Confidence*100),
		"corroborations":      signal.Corroborations,
		"has_corroboration":   signal.Corroborations > 1,
		"corroboration_label": corroborationLabel,
	}
}

func applySubmissionState(ctx *route.RouteContext, data map[string]any) {
	data["has_notice"] = false
	data["notice"] = ""
	if store := session.Current(ctx.Request); store != nil {
		if flashes := store.Flashes("notice"); len(flashes) > 0 {
			data["has_notice"] = true
			data["notice"] = fmt.Sprint(flashes[0])
		}
	}
	data["has_submit_error"] = false
	data["submit_error"] = ""
	data["submit_evidence_type"] = "community"
	data["submit_source_name"] = ""
	data["submit_source_url"] = ""
	data["submit_summary"] = ""
	for _, name := range []string{"evidence_type", "source_name", "source_url", "summary"} {
		data["has_"+name+"_error"] = false
		data[name+"_error"] = ""
	}
	if view, ok := ctx.ActionState("submit-sighting"); ok {
		data["has_submit_error"] = !view.OK()
		data["submit_error"] = view.Message()
		for _, name := range []string{"evidence_type", "source_name", "source_url", "summary"} {
			if value := view.Value(name); value != "" {
				data["submit_"+name] = value
			}
			if message := view.Error(name); message != "" {
				data["has_"+name+"_error"] = true
				data[name+"_error"] = message
			}
		}
	}
	evidence, _ := data["submit_evidence_type"].(string)
	data["evidence_community"] = evidence == "community"
	data["evidence_news"] = evidence == "submitted_news"
	data["evidence_market"] = evidence == "market"
}

func sightingReporter(request *http.Request) (string, string, bool) {
	if user, ok := auth.Current(request); ok {
		id := strings.TrimSpace(user.ID)
		if id == "" {
			id = strings.TrimSpace(user.Email)
		}
		name := strings.TrimSpace(user.Name)
		if name == "" && user.Email != "" {
			name = strings.Split(user.Email, "@")[0]
		}
		return id, name, id != ""
	}
	if league.Default().DemoMode() {
		return "demo-commissioner", "Demo commissioner", true
	}
	return "", "", false
}

func sightingFieldErrors(err error) map[string]string {
	message := strings.ToLower(err.Error())
	field := ""
	switch {
	case strings.Contains(message, "summary"):
		field = "summary"
	case strings.Contains(message, "source name"), strings.Contains(message, "where you saw"):
		field = "source_name"
	case strings.Contains(message, "source link"):
		field = "source_url"
	case strings.Contains(message, "sighting type"):
		field = "evidence_type"
	}
	if field == "" {
		return nil
	}
	return map[string]string{field: err.Error()}
}

func displayTime(value time.Time) string {
	if value.IsZero() {
		return "WAITING"
	}
	location, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		location = time.Local
	}
	return strings.ToUpper(value.In(location).Format("Jan 02 · 3:04 PM MST"))
}

func shortDID(did string) string {
	if len(did) <= 22 {
		return did
	}
	return did[:18] + "…"
}
