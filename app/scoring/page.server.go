package scoring

import (
	"fmt"
	"gridiron-2000/internal/actionui"
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
// carries the render-time view above instead of the bare data, plus ID
// (P2-13, UI pass 2026-08-30) so the section and its jump-list anchor
// (scoringJumpSection, below) always agree.
type scoringRuleGroupView struct {
	ID    string
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
		out = append(out, scoringRuleGroupView{ID: "scoring-group-" + scoringSlug(group.Name), Name: group.Name, Note: group.Note, Rules: rows})
	}
	return out
}

// scoringSlug lowercases name and keeps only [a-z0-9-], collapsing every
// other run of characters to one hyphen, for a stable #id a scoring-group
// name (configured, not user free text) can safely become.
func scoringSlug(name string) string {
	out := make([]rune, 0, len(name))
	lastHyphen := false
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			out = append(out, r)
			lastHyphen = false
		case r >= 'A' && r <= 'Z':
			out = append(out, r+('a'-'A'))
			lastHyphen = false
		default:
			if !lastHyphen && len(out) > 0 {
				out = append(out, '-')
				lastHyphen = true
			}
		}
	}
	for len(out) > 0 && out[len(out)-1] == '-' {
		out = out[:len(out)-1]
	}
	return string(out)
}

// scoringJumpSection is one entry in the sticky anchor jump-list at the
// top of /scoring (P2-13, UI pass 2026-08-30): a phone-length page
// (~7,400px, pre-UI-pass) is otherwise a scroll-and-hope wall. Built from
// the same fixed section order and the same runtime group data the page
// body itself renders, so the two can never disagree.
type scoringJumpSection struct {
	ID    string
	Label string
}

// scoringJumpSections lists every anchor the jump-list nav renders, in
// page order. The admin-only "RESET // SCORING" danger zone is
// deliberately not a jump target: it is a destructive action, not
// explanatory content, and it renders only for a commissioner.
func scoringJumpSections(groups []scoringRuleGroupView) []scoringJumpSection {
	out := []scoringJumpSection{
		{ID: "scoring-league", Label: "League"},
		{ID: "scoring-membership", Label: "Membership"},
		{ID: "scoring-roster", Label: "Roster"},
		{ID: "scoring-draft", Label: "Draft"},
		{ID: "scoring-lineups", Label: "Lineups"},
	}
	for _, group := range groups {
		out = append(out, scoringJumpSection{ID: group.ID, Label: group.Name})
	}
	out = append(out,
		scoringJumpSection{ID: "scoring-week-close", Label: "Week close"},
		scoringJumpSection{ID: "scoring-free-agency", Label: "Free agency"},
		scoringJumpSection{ID: "scoring-waivers", Label: "Waivers"},
		scoringJumpSection{ID: "scoring-trades", Label: "Trades"},
		scoringJumpSection{ID: "scoring-pickem", Label: "Pick'em"},
		scoringJumpSection{ID: "scoring-blitz", Label: "Preseason Blitz"},
	)
	return out
}

func init() {
	if err := route.RegisterFileModuleHere(route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			ctx.NoStore()
			data := league.Default().ScoringData(ctx.Request)
			if groups, ok := data["groups"].([]league.ScoringRuleGroup); ok {
				editable, _ := data["editable"].(bool)
				views := scoringRuleGroupViews(groups, editable, ctx.ActionPath("scoring-set"), session.Token(ctx.Request))
				data["groups"] = views
				data["jump_sections"] = scoringJumpSections(views)
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
					return actionui.Validation(ctx, "scoring", "scoring", err)
				}
				actionui.RedirectWithNotice(ctx, "/scoring", rule.Label+" updated.")
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
				actionui.RedirectWithNotice(ctx, "/scoring", "Scoring restored to defaults.")
				return nil
			},
		},
	}); err != nil {
		log.Fatal(err)
	}
}
