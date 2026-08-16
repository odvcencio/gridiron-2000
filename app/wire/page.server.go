package wire

import (
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"gridiron-2000/internal/league"
	"gridiron-2000/internal/openstats"
	signalwire "gridiron-2000/internal/wire"
	"m31labs.dev/gosx/action"
	"m31labs.dev/gosx/auth"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
	"m31labs.dev/gosx/session"
)

func init() {
	if err := route.RegisterFileModuleHere(route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			ctx.NoStore()
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
	recent := signals.Recent(50, "")
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
