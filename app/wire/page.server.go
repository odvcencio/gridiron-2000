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

const (
	wireCommunityAnchor   = "#community-input"
	wireReturnTargetField = action.ReturnTargetField
)

// wireCategory is the single allowlist boundary for the category query used
// by the page, feed fragment, and submit form. Unknown or hostile values are
// dropped rather than copied into a redirect or filter link.
func wireCategory(category string) string {
	category = strings.ToLower(strings.TrimSpace(category))
	for _, option := range WireFilterOptions {
		if option.Slug == category {
			return category
		}
	}
	return ""
}

// wireRedirectTarget builds the canonical same-origin return target for the
// community form. url.Values performs query escaping; the path and anchor
// are constants, so form data cannot steer a redirect outside the Wire page.
func wireRedirectTarget(category string) string {
	values := neturl.Values{}
	if category := wireCategory(category); category != "" {
		values.Set("category", category)
	}
	target := "/wire"
	if encoded := values.Encode(); encoded != "" {
		target += "?" + encoded
	}
	return target + wireCommunityAnchor
}

// wireRequestWithActionCategory restores the category from a flashed action
// view before loading a native PRG response. Only the Wire allowlist can
// affect the query; the action's other submitted values remain local form
// state.
func wireRequestWithActionCategory(request *http.Request, view action.View) *http.Request {
	if request == nil {
		return request
	}
	clone := request.Clone(request.Context())
	query := clone.URL.Query()
	if category := wireCategory(view.Value("category")); category != "" {
		query.Set("category", category)
	} else {
		query.Del("category")
	}
	clone.URL.RawQuery = query.Encode()
	return clone
}

// wireReturnTargetForData mirrors the category-normalized page state in the
// hidden GoSX return-target control. It is generated server-side so a form
// rendered after a hostile query still posts a bounded target.
func wireReturnTargetForData(data map[string]any) string {
	return wireRedirectTarget(fmt.Sprint(data["category"]))
}

// wireValidationWithRedirect keeps native POST-redirect-GET validation on the
// submitted category while leaving managed forms in place for GoSX to project
// the values and field errors into the current page.
func wireValidationWithRedirect(ctx *action.Context, redirect string, err error) error {
	validation := actionui.ValidationFields(ctx, "wire", err, sightingFieldErrors)
	if action.WantsJSON(ctx.Request) {
		return validation
	}
	if result, ok := validation.(*action.ResultError); ok {
		result.Result.Redirect = redirect
	}
	return validation
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
	category = wireCategory(category)
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
			request := ctx.Request
			if view, ok := ctx.ActionState("submit-sighting"); ok {
				request = wireRequestWithActionCategory(request, view)
			}
			data := wirePageData(request, signals, stats)
			data["wire_return_target_field"] = wireReturnTargetField
			data["wire_return_target"] = wireReturnTargetForData(data)
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
					return wireValidationWithRedirect(ctx, wireRedirectTarget(ctx.FormData["category"]), err)
				}
				actionui.RedirectBackWithNotice(ctx, wireRedirectTarget(ctx.FormData["category"]), fmt.Sprintf("%s added to the provisional wire.", signal.Label))
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
	signalwire.ModeDisabled:         "OFF",
	signalwire.ModeAwaitingSources:  "OFF",
	signalwire.ModeReady:            "READY",
	signalwire.ModeSyndicationReady: "READY",
	signalwire.ModeSyndicating:      "LIVE",
	signalwire.ModeResolvingSources: "STARTING",
	signalwire.ModeConnecting:       "STARTING",
	signalwire.ModeStreaming:        "LIVE",
	signalwire.ModeReconnecting:     "CATCHING UP",
	signalwire.ModeSourceError:      "QUIET",
	signalwire.ModeStopped:          "OFF",
}

func wireModeLabel(mode string) string {
	if label, ok := wireModeLabels[mode]; ok {
		return label
	}
	return "UNAVAILABLE"
}

func wireFeedStaleThreshold(status signalwire.Status) time.Duration {
	if status.FeedStaleAfter > 0 {
		return status.FeedStaleAfter
	}
	return signalwire.DeriveFeedStaleAfter(0)
}

func wireFeedHealthLabelAt(feed signalwire.FeedStatus, staleAfter time.Duration, now time.Time) string {
	if feed.LastError != "" || feed.State == "error" {
		return "ERROR"
	}
	if feed.LastChecked.IsZero() {
		return "NEVER CHECKED"
	}
	if feed.LastChecked.Before(now.Add(-staleAfter)) {
		return "STALE"
	}
	if feed.State != "ready" {
		return "UNAVAILABLE"
	}
	return "READY"
}

// wireFeedHealthLabel is deliberately derived from the timestamps and error
// retained by the internal service. A feed that has never completed a check
// must not look ready merely because its configured state says "waiting".
func wireFeedHealthLabel(feed signalwire.FeedStatus, now time.Time) string {
	return wireFeedHealthLabelAt(feed, signalwire.DeriveFeedStaleAfter(0), now)
}

func wireFeedHealthLabelForStatus(feed signalwire.FeedStatus, status signalwire.Status, now time.Time) string {
	return wireFeedHealthLabelAt(feed, wireFeedStaleThreshold(status), now)
}

func wireFeedCheckedLabel(feed signalwire.FeedStatus) string {
	if feed.LastChecked.IsZero() {
		return "NEVER CHECKED"
	}
	return displayTime(feed.LastChecked)
}

func wireFeedPublishedLabel(feed signalwire.FeedStatus) string {
	if feed.LastPublished.IsZero() {
		return "NEVER PUBLISHED"
	}
	return displayTime(feed.LastPublished)
}

func wireHasDegradedFeed(status signalwire.Status, now time.Time) bool {
	if len(status.Feeds) == 0 || status.Mode == signalwire.ModeDisabled || status.Mode == signalwire.ModeAwaitingSources {
		return false
	}
	for _, feed := range status.Feeds {
		if wireFeedHealthLabelForStatus(feed, status, now) != "READY" {
			return true
		}
	}
	return false
}

func wireHasPartialOutage(status signalwire.Status, now time.Time) bool {
	if wireHasDegradedFeed(status, now) {
		return true
	}
	if status.SourcesPartial || (status.SourceIssue != "" && status.Mode != signalwire.ModeSourceError) {
		return true
	}
	// A failed Bluesky source can coexist with healthy syndication feeds. The
	// service keeps source_error instead of overwriting it with syndicating, so
	// this branch makes that partial outage visible in the aggregate label.
	return status.BlueskyConfigured && len(status.Feeds) > 0 && status.Mode == signalwire.ModeSourceError
}

// wirePresentationLabel is the aggregate product label. The runtime mode is
// still the source of truth for a healthy wire, but a single failed, stale, or
// never-checked feed is visible even when a Bluesky stream remains healthy.
func wirePresentationLabel(status signalwire.Status, now time.Time) string {
	base := wireModeLabel(status.Mode)
	if base == "UNAVAILABLE" {
		return base
	}
	if wireHasPartialOutage(status, now) {
		return "DEGRADED"
	}
	return base
}

func wireHealthLabel(status signalwire.Status, now time.Time) string {
	base := wireModeLabel(status.Mode)
	if base == "UNAVAILABLE" {
		return base
	}
	if wireHasPartialOutage(status, now) {
		return "PARTIAL"
	}
	switch base {
	case "LIVE", "READY":
		return "HEALTHY"
	default:
		return base
	}
}

// wireLiveIndicator is the only wire status allowed to render the glowing live
// lamp. Every non-live state remains visible in the adjacent text label without
// suggesting that the feed is currently streaming.
func wireLiveIndicator(status signalwire.Status, now time.Time) string {
	if wirePresentationLabel(status, now) == "LIVE" {
		return "LIVE"
	}
	return ""
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
	now := time.Now().UTC()
	viewer := league.Default().Viewer(request)
	category := wireCategory(request.URL.Query().Get("category"))
	recent := signals.Recent(50, category)
	items := make([]WireSignalCard, 0, len(recent))
	for _, signal := range recent {
		items = append(items, wireSignalCard(signal, wireStatus, now))
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
		feedState := wireFeedHealthLabelForStatus(feed, wireStatus, now)
		if feedState == "READY" {
			readyFeeds++
		}
		feedIgnored += feed.Ignored
		checked := wireFeedCheckedLabel(feed)
		published := wireFeedPublishedLabel(feed)
		feeds = append(feeds, map[string]any{
			"name":           feed.Name,
			"url":            feed.URL,
			"evidence":       evidenceTypeLabel(feed.EvidenceType),
			"state":          feedState,
			"accepted":       feed.Accepted,
			"ignored":        feed.Ignored,
			"checked":        checked,
			"last_checked":   checked,
			"published":      published,
			"last_published": published,
			"has_checked":    !feed.LastChecked.IsZero(),
			"has_published":  !feed.LastPublished.IsZero(),
			"has_error":      feed.LastError != "",
			"last_error":     feed.LastError,
		})
	}
	lastID := ""
	if len(recent) > 0 {
		lastID = recent[0].ID
	}
	return map[string]any{
		"viewer":            viewer,
		"league":            league.Default().LeagueIdentityForViewer(request),
		"signals":           items,
		"empty":             len(items) == 0,
		"last_event_id":     lastID,
		"category":          category,
		"filters":           wireFilterMaps(category),
		"fragment_url":      wireFragmentURL(category),
		"wire_mode":         wirePresentationLabel(wireStatus, now),
		"wire_health":       wireHealthLabel(wireStatus, now),
		"wire_indicator":    wireLiveIndicator(wireStatus, now),
		"wire_configured":   wireStatus.Configured,
		"wire_issue":        wireStatus.ConfigurationIssue,
		"wire_source_issue": wireStatus.SourceIssue,
		"wire_error":        wireStatus.LastError,
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
		"feed_stale_after": wireStatus.FeedStaleAfter,
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
// replaces the wire feed's children with this response verbatim. It keeps
// this historical Node-returning API for callers that already use it; the
// server route uses FeedFragmentWithError so a failed sibling-program load
// becomes an HTTP error instead of an empty successful response.
func FeedFragment(request *http.Request, signals *signalwire.Service) gosx.Node {
	node, err := FeedFragmentWithError(request, signals)
	if err != nil {
		return gosx.Node{}
	}
	return node
}

// FeedFragmentWithError is the error-reporting form used by the production
// /wire/fragment handler. GoSX v0.53.6's relocation-safe route API resolves
// page.gsx next to this source file even in a -trimpath binary moved away from
// its build checkout. The program is loaded exactly once per request, then
// every row (or the empty state) is rendered through the typed components the
// initial page uses; no second HTML/node implementation can drift from them.
func FeedFragmentWithError(request *http.Request, signals *signalwire.Service) (gosx.Node, error) {
	program, err := route.LoadFileProgramHere("page.gsx")
	if err != nil {
		return gosx.Node{}, fmt.Errorf("load wire page.gsx: %w", err)
	}
	category := strings.TrimSpace(request.URL.Query().Get("category"))
	wireStatus := signals.Status()
	now := time.Now().UTC()
	recent := signals.Recent(50, category)
	if len(recent) == 0 {
		return route.RenderProgramComponentNode(program, "WireEmptyState", route.ProgramRenderEnv{
			Props: WireEmptyView{
				WireConfigured: wireStatus.Configured,
				WireIssue:      wireStatus.ConfigurationIssue,
			},
		})
	}
	cards := make([]gosx.Node, 0, len(recent))
	for _, signal := range recent {
		card := wireSignalCard(signal, wireStatus, now)
		node, err := route.RenderProgramComponentNode(program, "SignalCard", route.ProgramRenderEnv{
			Props: card,
		})
		if err != nil {
			return gosx.Node{}, fmt.Errorf("render wire SignalCard %q: %w", card.ID, err)
		}
		cards = append(cards, node)
	}
	return gosx.Fragment(cards...), nil
}

// PulseData is the small polled JSON object /api/wire/pulse answers with
// (main.go's wirePulseHandler): the data-gosx-live-bind text region on the
// wire page's masthead and status line (gosx#217) reads these three flat
// keys — "mode", "count", "status" — replacing the old bespoke
// syncWire()'s setText calls for the same three elements.
func PulseData(signals *signalwire.Service) map[string]any {
	wireStatus := signals.Status()
	now := time.Now().UTC()
	count := wireStatus.RelevantSignals
	health := wireHealthLabel(wireStatus, now)
	status := fmt.Sprintf("%d relevant signal%s · %s · %s", count, pluralSuffix(count), health, displayTime(now))
	if issue := strings.TrimSpace(wireStatus.SourceIssue); issue != "" {
		status += " · " + issue
	}
	return map[string]any{
		"mode":         wirePresentationLabel(wireStatus, now),
		"count":        count,
		"status":       status,
		"health":       health,
		"indicator":    wireLiveIndicator(wireStatus, now),
		"source_issue": wireStatus.SourceIssue,
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
	Retained           bool
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

func wireSignalCard(signal signalwire.Signal, status signalwire.Status, now time.Time) WireSignalCard {
	card := signalMap(signal)
	card.Retained = signalRetained(signal, status, now)
	return card
}

func signalRetained(signal signalwire.Signal, status signalwire.Status, now time.Time) bool {
	switch signal.Source {
	case signalwire.SourceFeed:
		for _, feed := range status.Feeds {
			if feed.Name == signal.SourceName {
				return wireFeedHealthLabelForStatus(feed, status, now) != "READY"
			}
		}
	case signalwire.SourceBluesky:
		if !status.BlueskyConfigured {
			return false
		}
		return status.Mode != signalwire.ModeStreaming && status.Mode != signalwire.ModeSyndicating
	}
	return false
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

func sightingFieldErrors(message string) map[string]string {
	lower := strings.ToLower(message)
	field := ""
	switch {
	case strings.Contains(lower, "summary"):
		field = "summary"
	case strings.Contains(lower, "source name"), strings.Contains(lower, "where you saw"):
		field = "source_name"
	case strings.Contains(lower, "source link"):
		field = "source_url"
	case strings.Contains(lower, "sighting type"):
		field = "evidence_type"
	}
	if field == "" {
		return nil
	}
	return map[string]string{field: message}
}

// displayTime renders one wire timestamp in the league's canonical zone
// with a relative label, per the contract's time rule (exact league-local
// time, timezone, and a useful relative value). It previously hard-coded
// America/Los_Angeles — three hours behind the league — and carried no
// relative text.
func displayTime(value time.Time) string {
	return formatWireTime(value, time.Now(), league.Default().LeagueLocation())
}

// formatWireTime is displayTime's pure core, split out so the format is
// testable without the league singleton or the wall clock.
func formatWireTime(value, now time.Time, location *time.Location) string {
	if value.IsZero() {
		return "WAITING"
	}
	stamp := strings.ToUpper(value.In(location).Format("Jan 02 · 3:04 PM MST"))
	return stamp + " · " + league.RelativeTime(now, value)
}

func shortDID(did string) string {
	if len(did) <= 22 {
		return did
	}
	return did[:18] + "…"
}
