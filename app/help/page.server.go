package help

import (
	"log"
	"strings"
	"time"

	"gridiron-2000/internal/league"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
)

// TopicView exposes the template-safe projection shared by the index and topic routes.
// Keeping the projection here prevents GoSX from relying on Go struct field names.
func TopicView(topic Topic) map[string]any {
	return map[string]any{
		"id": topic.ID, "title": topic.Title, "summary": topic.Summary,
		"category": topic.Category, "action_route": topic.ActionRoute,
		"keywords": strings.Join(topic.Keywords, ", "), "synonyms": strings.Join(topic.Synonyms, ", "),
		"introduced_version": topic.IntroducedVersion, "last_verified_sha": topic.LastVerifiedSHA,
		"actor": topic.Actor, "prerequisites": topic.Prerequisites, "supported": topic.Supported,
		"states": topic.States, "deadline": topic.Deadline, "steps": topic.Steps,
		"privacy": topic.Privacy, "consequence": topic.Consequence, "reversibility": topic.Reversibility,
		"result": topic.Result, "failure": topic.Failure, "recovery": topic.Recovery,
		"runtime_source": topic.RuntimeSource, "example": topic.Example,
	}
}

func runtimeProjection() map[string]any {
	svc := league.Default()
	cfg := svc.Config()
	phase := strings.ToUpper(strings.ReplaceAll(svc.SeasonPhase(time.Now()), "-", " "))
	mode := strings.ToUpper(strings.TrimSpace(cfg.ModeLabel))
	if mode == "" {
		mode = "CONFIGURED"
	}
	zone := cfg.Timezone
	location, err := time.LoadLocation(zone)
	if err != nil {
		location = time.UTC
	}
	// DefaultConfig ships a neutral 400+-day-out placeholder draft instant
	// (2099-01-01, config.go's placeholderDraftAt), which the 2026-09-01
	// audit found rendered here as a live "Next draft meeting" fact. Apply
	// the same DraftDatePublished guard draftSummaryForState already uses
	// for / and /guide.
	now := time.Now()
	draftAt := svc.DraftAt()
	draftAtLabel := "Not published yet — the commissioner sets it"
	if league.DraftDatePublished(now, draftAt) {
		draftAtLabel = draftAt.In(location).Format("Mon, Jan 2, 2006 · 3:04 PM MST")
	}
	return map[string]any{
		"league_name": cfg.Name, "mode": mode, "phase": phase, "timezone": league.FriendlyTimezoneLabel(zone),
		"draft_at":     draftAtLabel,
		"runtime_note": "Rules, dates, deadlines, capabilities, and freshness remain owned by the current league runtime.",
	}
}

func checklistView(items []ChecklistItem) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, map[string]any{
			"id": item.ID, "role": item.Role, "title": item.Title, "detail": item.Detail,
			"predicate": item.Predicate, "action_route": item.ActionRoute, "applicable": item.Applicable,
		})
	}
	return out
}

// glossaryView projects GlossaryEntry structs into the lowercase map keys
// app/help/page.gsx reads (term, aliases, definition, topic_id). Passing the
// struct slice straight through left every field and every /help/<id> link
// blank (F1: 75 empty glossary cards, 75 dead "/help/" links).
func glossaryView(entries []GlossaryEntry) []map[string]any {
	out := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		out = append(out, map[string]any{
			"term": entry.Term, "aliases": strings.Join(entry.Aliases, ", "),
			"definition": entry.Definition, "topic_id": entry.TopicID,
		})
	}
	return out
}

// migrationView projects MigrationMapping structs into the lowercase map
// keys app/help/page.gsx reads (canonical, incoming_aliases, difference,
// next_action). Passing the struct slice straight through left the nine
// concept-transition rows blank (F2).
func migrationView(mappings []MigrationMapping) []map[string]any {
	out := make([]map[string]any, 0, len(mappings))
	for _, mapping := range mappings {
		out = append(out, map[string]any{
			"canonical": mapping.Canonical, "incoming_aliases": strings.Join(mapping.IncomingAliases, ", "),
			"difference": mapping.Difference, "next_action": mapping.NextAction,
		})
	}
	return out
}

func helpIndexData(ctx *route.RouteContext) map[string]any {
	data := league.Default().StaticPageData(ctx.Request)
	query := strings.TrimSpace(ctx.Query("q"))
	phase := strings.ToLower(runtimeProjection()["phase"].(string))
	mode := strings.ToLower(runtimeProjection()["mode"].(string))
	if query == "" {
		query = ""
	}
	topics := make([]map[string]any, 0, len(TopicCorpus()))
	for _, item := range TopicCorpus() {
		topics = append(topics, TopicView(item))
	}
	results := Search(query)
	searchResults := make([]map[string]any, 0, len(results))
	for _, result := range results {
		view := TopicView(result.Topic)
		view["score"] = result.Score
		searchResults = append(searchResults, view)
	}
	categoryViews := make([]map[string]any, 0, len(categories))
	for _, category := range Categories() {
		cards := make([]map[string]any, 0)
		for _, item := range TopicCorpus() {
			if item.Category == category.ID {
				cards = append(cards, TopicView(item))
			}
		}
		categoryViews = append(categoryViews, map[string]any{"id": category.ID, "title": category.Title, "order": category.Order, "topics": cards})
	}
	data["query"] = query
	data["has_query"] = query != ""
	data["has_results"] = len(searchResults) > 0
	data["categories"] = categoryViews
	data["topics"] = topics
	data["search_results"] = searchResults
	data["runtime"] = runtimeProjection()
	data["corpus_version"] = CorpusVersion
	data["source_sha"] = VerifiedSourceSHA
	data["source_sha_short"] = ShortSHA(VerifiedSourceSHA)
	data["checklist"] = checklistView(ChecklistFor("primary", mode, phase, false))
	data["commissioner_checklist"] = checklistView(ChecklistFor("seatless", mode, phase, true))
	data["migration"] = migrationView(MigrationMappings())
	data["glossary"] = glossaryView(Glossary())
	return data
}

func init() {
	if err := route.RegisterFileModuleHere(route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			ctx.NoStore()
			return helpIndexData(ctx), nil
		},
		Metadata: func(ctx *route.RouteContext, page route.FilePage, data any) (server.Metadata, error) {
			return server.Metadata{
				Title:       server.Title{Default: "Help center · " + runtimeProjection()["league_name"].(string)},
				Description: "Task-first Gridiron help, migration concepts, glossary, and state recovery.",
			}, nil
		},
	}); err != nil {
		log.Fatal(err)
	}
}
