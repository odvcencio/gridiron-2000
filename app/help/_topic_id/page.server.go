package topic

import (
	"log"
	"net/url"
	"strings"

	helpcontent "gridiron-2000/app/help"
	"gridiron-2000/internal/league"
	"gridiron-2000/internal/navigation"
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
			data["source_sha_short"] = helpcontent.ShortSHA(helpcontent.VerifiedSourceSHA)
			rawReturnTarget := ctx.Query(helpcontent.ReturnToQuery)
			returnTarget := navigation.SafeActionReturnPath(rawReturnTarget)
			hasReturnTarget := rawReturnTarget != "" && (returnTarget != navigation.DefaultReturnPath || rawReturnTarget == navigation.DefaultReturnPath)
			data["has_return_target"] = hasReturnTarget
			data["return_target"] = ""
			data["return_target_label"] = ""
			if hasReturnTarget {
				data["return_target"] = returnTarget
				data["return_target_label"] = "Return to " + navigation.PageLabel(returnTarget) + " →"
			} else {
				returnTarget = ""
			}
			state := strings.TrimSpace(ctx.Query("state"))
			field := strings.TrimSpace(ctx.Query("field"))
			data["has_state"] = state != ""
			data["has_field"] = field != ""
			guidance := helpcontent.Guidance(topic.ID, state)
			data["state_help"] = stateGuidanceView(guidance)
			data["state_help_href"] = helpcontent.ContextualTopicURL(topic.ID, url.Values{"state": []string{guidance.State}}, returnTarget)
			fieldHelp := helpcontent.ContextualFieldHelp(topic.ID, field)
			data["field_help"] = fieldHelp
			data["field_help_href"] = helpcontent.ContextualTopicURL(fieldHelp["topic_id"], nil, returnTarget)
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
