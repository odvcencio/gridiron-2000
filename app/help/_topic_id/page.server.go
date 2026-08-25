package topic

import (
	"log"
	"strings"

	helpcontent "gridiron-2000/app/help"
	"gridiron-2000/internal/league"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
)

func init() {
	if err := route.RegisterFileModuleHere(route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			ctx.NoStore()
			topic, ok := helpcontent.FindTopic(ctx.Param("topic_id"))
			if !ok {
				return nil, route.NotFound("help topic not found")
			}
			data := league.Default().StaticPageData(ctx.Request)
			data["topic"] = helpcontent.TopicView(topic)
			data["corpus_version"] = helpcontent.CorpusVersion
			data["source_sha"] = helpcontent.VerifiedSourceSHA
			state := strings.TrimSpace(ctx.Query("state"))
			field := strings.TrimSpace(ctx.Query("field"))
			data["has_state"] = state != ""
			data["has_field"] = field != ""
			data["state_help"] = stateGuidanceView(helpcontent.Guidance(topic.ID, state))
			data["field_help"] = helpcontent.ContextualFieldHelp(topic.ID, field)
			return data, nil
		},
		Metadata: func(ctx *route.RouteContext, page route.FilePage, data any) (server.Metadata, error) {
			topic, _ := helpcontent.FindTopic(ctx.Param("topic_id"))
			return helpcontentTopicMetadata(topic), nil
		},
	}); err != nil {
		log.Fatal(err)
	}
}

func stateGuidanceView(g helpcontent.StateGuidance) map[string]any {
	return map[string]any{
		"state": g.State, "why": g.Why, "impact": g.Impact, "remaining": g.Remaining,
		"context": g.PreservedContext, "next_action": g.NextAction, "retry": g.Retry,
		"last_success": g.LastSuccess, "topic_id": g.TopicID,
	}
}

func helpcontentTopicMetadata(topic helpcontent.Topic) server.Metadata {
	return server.Metadata{Title: server.Title{Default: topic.Title + " · Help"}, Description: topic.Summary}
}
