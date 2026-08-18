package scoring

import (
	"fmt"
	"log"
	"net/http"

	"gridiron-2000/internal/league"
	"m31labs.dev/gosx/action"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
	"m31labs.dev/gosx/session"
)

// scoringRuleRowView is one scoring-rule line as ScoringRow (page.gsx, a
// strict component) reads it: the rule itself plus the per-request fields
// (edit permission, the set-scoring action path, the CSRF token) the
// template used to read from data.editable/actionPath/csrf.token
// directly. Editable/SetAction/CSRF are request-scoped, so they are
// resolved once here and carried on every row rather than recomputed by
// the template per row, and Rule nests league.ScoringRuleRow structurally
// (gosx#230): a spread's nested struct-typed field is proved by the
// fields the callee reads, not by its declared type's name, so this needs
// only to share shape with page.gsx's own ScoringRuleRow declaration.
type scoringRuleRowView struct {
	Rule      league.ScoringRuleRow
	Editable  bool
	SetAction string
	CSRF      string
}

// scoringRuleGroupView is one scoring section as page.gsx's Page() reads
// it: unchanged from league.ScoringRuleGroup except each Rule Rules entry
// carries the render-time view above instead of the bare data.
type scoringRuleGroupView struct {
	Name  string
	Note  string
	Rules []scoringRuleRowView
}

// scoringRuleGroupViews converts ScoringData's typed groups into the
// page's render view, baking in the one request-scoped state
// (editability, the scoring-set action path, the CSRF token) every row
// needs to render its own inline edit form.
func scoringRuleGroupViews(groups []league.ScoringRuleGroup, editable bool, setAction, csrfToken string) []scoringRuleGroupView {
	out := make([]scoringRuleGroupView, 0, len(groups))
	for _, group := range groups {
		rows := make([]scoringRuleRowView, 0, len(group.Rules))
		for _, rule := range group.Rules {
			rows = append(rows, scoringRuleRowView{
				Rule:      rule,
				Editable:  editable,
				SetAction: setAction,
				CSRF:      csrfToken,
			})
		}
		out = append(out, scoringRuleGroupView{Name: group.Name, Note: group.Note, Rules: rows})
	}
	return out
}

func init() {
	if err := route.RegisterFileModuleHere(route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			ctx.NoStore()
			data := league.Default().ScoringData(ctx.Request)
			if groups, ok := data["groups"].([]league.ScoringRuleGroup); ok {
				editable, _ := data["editable"].(bool)
				data["groups"] = scoringRuleGroupViews(groups, editable, ctx.ActionPath("scoring-set"), session.Token(ctx.Request))
			}
			data["has_notice"] = false
			data["notice"] = ""
			if store := session.Current(ctx.Request); store != nil {
				if flashes := store.Flashes("notice"); len(flashes) > 0 {
					data["has_notice"] = true
					data["notice"] = fmt.Sprint(flashes[0])
				}
			}
			data["has_scoring_error"] = false
			data["scoring_error"] = ""
			for _, name := range []string{"scoring-set", "scoring-reset"} {
				if view, ok := ctx.ActionState(name); ok {
					if message := view.Error("scoring"); message != "" {
						data["has_scoring_error"] = true
						data["scoring_error"] = message
					}
				}
			}
			return data, nil
		},
		Metadata: func(ctx *route.RouteContext, page route.FilePage, data any) (server.Metadata, error) {
			return server.Metadata{
				Title:       server.Title{Default: league.PageTitle("Rules & Scoring")},
				Description: "Roster shape, scoring values, the draft, and the season, visible to every manager.",
			}, nil
		},
		Actions: route.FileActions{
			"scoring-set": func(ctx *action.Context) error {
				rule, err := league.Default().AdminSetScoring(ctx.Request, ctx.FormData["key"], ctx.FormData["points"])
				if err != nil {
					return action.Validation(err.Error(), map[string]string{"scoring": err.Error()}, ctx.FormData)
				}
				session.AddFlash(ctx.Request, "notice", rule.Label+" updated.")
				ctx.Redirect("/scoring")
				return nil
			},
			"scoring-reset": func(ctx *action.Context) error {
				if ctx.FormData["confirm"] != "RESET" {
					message := "type RESET to confirm"
					return action.Validation(message, map[string]string{"scoring": message}, ctx.FormData)
				}
				if err := league.Default().AdminResetScoring(ctx.Request); err != nil {
					return action.Error(http.StatusUnauthorized, err.Error())
				}
				session.AddFlash(ctx.Request, "notice", "Scoring restored to defaults.")
				ctx.Redirect("/scoring")
				return nil
			},
		},
	}); err != nil {
		log.Fatal(err)
	}
}
