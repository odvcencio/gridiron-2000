package wire

import (
	"fmt"
	"gridiron-2000/internal/actionui"
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
				Description: "League news, community chatter, and NFL intel in one feed.",
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
				actionui.RedirectWithNotice(ctx, "/wire#community-input", fmt.Sprintf("%s added to the provisional wire.", signal.Label))
				return nil
			},
		},
	}); err != nil {
		log.Fatal(err)
	}
}

// wireModeLabels turns signalwire.Service's runtime mode constants into
// plain football labels for the wire masthead. The default case is a safe
// neutral word, never the raw mode token, so an unmapped future mode still
// reads as English instead of leaking a machine state name.
var wireModeLabels = map[string]string{
	"syndicating":       "LIVE",
	"resolving_sources": "STARTING",
	"source_error":      "QUIET",
	"reconnecting":      "CATCHING UP",
	"awaiting_sources":  "OFF",
}

func wireModeLabel(mode string) string {
	if label, ok := wireModeLabels[mode]; ok {
		return label
	}
	return "UNAVAILABLE"
}

// dataStateLabels turns the open-data dataset/feed state constants (shared
// by openstats.DatasetStatus.State and signalwire's per-feed State) into
// plain football labels. The default case is a safe neutral word, never
// the raw state token.
var dataStateLabels = map[string]string{
	"ready":            "READY",
	"waiting":          "WAITING",
	"disabled":         "OFF",
	"awaiting_release": "NOT OUT YET",
	"error":            "UNAVAILABLE",
}

func dataStateLabel(state string) string {
	if label, ok := dataStateLabels[state]; ok {
		return label
	}
	return "UNAVAILABLE"
}

// evidenceTypeLabels turns a feed's or signal's evidence_type token into a
// plain football label. The default case is a safe neutral word, never the
// raw token.
var evidenceTypeLabels = map[string]string{
	"news":           "NEWS",
	"community_feed": "COMMUNITY",
	"social":         "SOCIAL MEDIA",
}

func evidenceTypeLabel(evidenceType string) string {
	if label, ok := evidenceTypeLabels[evidenceType]; ok {
		return label
	}
	return "SOURCE"
}

func wirePageData(request *http.Request, signals *signalwire.Service, stats *openstats.Service) map[string]any {
	wireStatus := signals.Status()
	openStatus := stats.Status()
	viewer := league.Default().Viewer(request)
	category := strings.TrimSpace(request.URL.Query().Get("category"))
	recent := signals.Recent(50, category)
	items := make([]WireSignalCard, 0, len(recent))
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
			"evidence":   evidenceTypeLabel(feed.EvidenceType),
			"state":      dataStateLabel(feed.State),
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
		"viewer":          viewer,
		"signals":         items,
		"empty":           len(items) == 0,
		"last_event_id":   lastID,
		"category":        category,
		"filters":         wireFilterMaps(category),
		"fragment_url":    wireFragmentURL(category),
		"wire_mode":       wireModeLabel(wireStatus.Mode),
		"wire_configured": wireStatus.Configured,
		"wire_issue":      wireStatus.ConfigurationIssue,
		"wire_error":      wireStatus.LastError,
		// wire_empty is WireEmptyState's spread source: a strict component
		// called from this legacy Page() body must receive one {...} spread,
		// never named attributes built from separate map keys (gosx's
		// legacy-caller rule), so wire_configured/wire_issue are bundled
		// here as the one struct the template spreads.
		"wire_empty":       WireEmptyView{WireConfigured: wireStatus.Configured, WireIssue: wireStatus.ConfigurationIssue},
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
		"schedule_state":   dataStateLabel(openStatus.Schedules.State),
		"schedule_rows":    openStatus.Schedules.Rows,
		"schedule_updated": displayTime(openStatus.Schedules.LastUpdated),
		"player_state":     dataStateLabel(openStatus.PlayerStats.State),
		"player_rows":      openStatus.PlayerStats.Rows,
		"player_updated":   displayTime(openStatus.PlayerStats.LastUpdated),
		"injury_state":     dataStateLabel(openStatus.Injuries.State),
		"injury_rows":      openStatus.Injuries.Rows,
		"injury_updated":   displayTime(openStatus.Injuries.LastUpdated),
		"season":           openStatus.Season,
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
func signalCardNode(card WireSignalCard) gosx.Node {
	footer := []gosx.Node{
		gosx.El("span", gosx.Attrs(gosx.Attr("class", "mono")), gosx.Text(card.Source)),
	}
	if card.HasReporter {
		footer = append(footer, gosx.El("span", gosx.Attrs(gosx.Attr("class", "mono")), gosx.Text("VIA "+card.ReportedBy)))
	}
	if card.HasCorroboration {
		footer = append(footer, gosx.El("span", gosx.Attrs(gosx.Attr("class", "wire-event__corroboration mono")), gosx.Text(card.CorroborationLabel)))
	}
	footer = append(footer, gosx.El("time", gosx.Attrs(gosx.Attr("class", "mono")), gosx.Text(card.Time)))
	if card.HasURL {
		footer = append(footer, gosx.El("a", gosx.Attrs(
			gosx.Attr("href", card.URL),
			gosx.Attr("target", "_blank"),
			gosx.Attr("rel", "noreferrer"),
		), gosx.Text("Inspect source ↗")))
	}
	return gosx.El("article", gosx.Attrs(
		gosx.Attr("class", "wire-event wire-event--"+card.Category),
		gosx.Attr("data-wire-event", card.ID),
		gosx.Attr("data-wire-category", card.Category),
	),
		gosx.El("header", nil,
			gosx.El("div", gosx.Attrs(gosx.Attr("class", "wire-event__heading")),
				gosx.El("span", gosx.Attrs(gosx.Attr("class", "wire-event__label")), gosx.Text(card.Label)),
				gosx.El("span", gosx.Attrs(gosx.Attr("class", "wire-event__evidence")), gosx.Text(card.Evidence)),
			),
			gosx.El("span", gosx.Attrs(gosx.Attr("class", "wire-event__trust mono")), gosx.Text(card.Trust+" · "+card.Confidence+"%")),
		),
		gosx.El("p", nil, gosx.Text(card.Text)),
		gosx.El("footer", nil, gosx.Fragment(footer...)),
	)
}

// wireEmptyStateNode is a hand-written mirror of page.gsx's WireEmptyState
// component — see FeedFragment's doc comment for why. Keep this in exact
// structural sync with WireEmptyState by hand.
func wireEmptyStateNode(wireConfigured bool, wireIssue string) gosx.Node {
	body := []gosx.Node{
		gosx.El("span", gosx.Attrs(gosx.Attr("class", "mono")), gosx.Text("NO SIGNALS YET")),
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
		body = append(body, gosx.El("p", nil, gosx.Text("Relevant feed items and league sightings appear here, and stay provisional until the official stats catch up.")))
	}
	return gosx.El("div", gosx.Attrs(gosx.Attr("class", "wire-empty"), gosx.BoolAttr("data-wire-empty")), gosx.Fragment(body...))
}

// PulseData is the small polled JSON object /api/wire/pulse answers with
// (main.go's wirePulseHandler): the data-gosx-live-bind text region on the
// wire page's masthead and status line (gosx#217) reads these three flat
// keys — "mode", "count", "status" — replacing the old bespoke
// syncWire()'s setText calls for the same three elements.
func PulseData(signals *signalwire.Service) map[string]any {
	wireStatus := signals.Status()
	count := wireStatus.RelevantSignals
	status := fmt.Sprintf("%d relevant signal%s · %s", count, pluralSuffix(count), displayTime(time.Now()))
	return map[string]any{
		"mode":   wireModeLabel(wireStatus.Mode),
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

// WireSignalCard is one wire feed item as SignalCard (page.gsx, a strict
// component) reads it: a real struct, not a map, because a legacy Page()
// body spreading a slice entry into a strict component needs each entry
// to carry its own proven struct type (gosx's field-coverage boundary).
type WireSignalCard struct {
	ID                 string
	Category           string
	Label              string
	Text               string
	Source             string
	ReportedBy         string
	HasReporter        bool
	Evidence           string
	Trust              string
	Time               string
	URL                string
	HasURL             bool
	Rule               string
	Confidence         string
	Corroborations     int
	HasCorroboration   bool
	CorroborationLabel string
}

// WireEmptyView is WireEmptyState's (page.gsx, a strict component) spread
// source: whether the wire is configured and, when not, why.
type WireEmptyView struct {
	WireConfigured bool
	WireIssue      string
}

func signalMap(signal signalwire.Signal) WireSignalCard {
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
	return WireSignalCard{
		ID:                 signal.ID,
		Category:           signal.Category,
		Label:              signal.Label,
		Text:               signal.Text,
		Source:             source,
		ReportedBy:         signal.ReportedBy,
		HasReporter:        signal.ReportedBy != "",
		Evidence:           evidenceTypeLabel(signal.EvidenceType),
		Trust:              signal.TrustTier,
		Time:               displayTime(signal.OccurredAt),
		URL:                signal.SourceURL,
		HasURL:             signal.SourceURL != "",
		Rule:               signal.Rule,
		Confidence:         fmt.Sprintf("%.0f", signal.Confidence*100),
		Corroborations:     signal.Corroborations,
		HasCorroboration:   signal.Corroborations > 1,
		CorroborationLabel: corroborationLabel,
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
		user = league.Default().CanonicalUser(user)
		id := strings.TrimSpace(user.Email)
		if id == "" {
			id = strings.TrimSpace(user.ID)
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
