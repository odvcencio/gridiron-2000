package matchups

import (
	"log"

	"gridiron-2000/internal/league"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
)

// MatchupTeamData is one side of a matchup: the fields page.gsx's
// ScoreTeamProps needs, structurally — not by shared name. gosx v0.48.0
// proves a nested struct-typed field inside a spread structurally
// (gosx#230), so this type does not have to match ScoreTeamProps' name,
// only its fields, one for one, by name and declared type.
type MatchupTeamData struct {
	ID             string
	Name           string
	Manager        string
	Score          string
	Tone           string
	Abbreviation   string
	HasAvatarImage bool
	AvatarImageURL string
}

// MatchupCardData is the typed data.matchups entry: it nests Away/Home
// as MatchupTeamData-typed fields, matching page.gsx's strict
// MatchupCardProps structurally at both the top level and inside each
// nested field. Before gosx v0.48.0, a nested struct-typed field inside
// a spread was checked by exact type identity, which conflicted with
// the rule that a strict component's prop types live in its own .gsx
// file — MatchupCardData used to flatten Away/Home into scalar fields
// (AwayID, AwayName, ...) purely to dodge that conflict. It is named
// MatchupCardData, not MatchupCard, because page.gsx's strict
// `component MatchupCard` compiles to a package-level Go declaration
// named MatchupCard too; a converter type sharing that exact name here
// would collide with it.
type MatchupCardData struct {
	ID     string
	Status string
	Clock  string
	Away   MatchupTeamData
	Home   MatchupTeamData
}

func stringField(m map[string]any, key string) string {
	value, _ := m[key].(string)
	return value
}

func boolField(m map[string]any, key string) bool {
	value, _ := m[key].(bool)
	return value
}

// matchupTeamData converts one "away"/"home" map entry into a typed
// MatchupTeamData value.
func matchupTeamData(m map[string]any) MatchupTeamData {
	return MatchupTeamData{
		ID:             stringField(m, "id"),
		Name:           stringField(m, "name"),
		Manager:        stringField(m, "manager"),
		Score:          stringField(m, "score"),
		Tone:           stringField(m, "tone"),
		Abbreviation:   stringField(m, "abbreviation"),
		HasAvatarImage: boolField(m, "has_avatar_image"),
		AvatarImageURL: stringField(m, "avatar_image_url"),
	}
}

// matchupsPageCards converts MatchupsData's map[string]any "matchups" slice
// (each entry carrying nested "away"/"home" maps) into typed
// MatchupCardData values, each nesting Away/Home as MatchupTeamData, so
// page.gsx's strict MatchupCard/ScoreTeam/TeamMark spread and attribute
// boundaries all prove clean: the tier-2 spread boundary rejects a
// map[string]any source outright (it "cannot prove field coverage").
func matchupsPageCards(raw []map[string]any) []MatchupCardData {
	out := make([]MatchupCardData, 0, len(raw))
	for _, entry := range raw {
		away, _ := entry["away"].(map[string]any)
		home, _ := entry["home"].(map[string]any)
		out = append(out, MatchupCardData{
			ID:     stringField(entry, "id"),
			Status: stringField(entry, "status"),
			Clock:  stringField(entry, "clock"),
			Away:   matchupTeamData(away),
			Home:   matchupTeamData(home),
		})
	}
	return out
}

func init() {
	if err := route.RegisterFileModuleHere(route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			ctx.NoStore()
			// The "Sync now" control's data-gosx-set click write and the
			// data-gosx-live-signal subscribe it triggers both need the
			// shared-signal store client/js/bootstrap-src/00-textlayout.js
			// installs, plus actions.ts' click delegation for data-gosx-set
			// itself — neither ships in the lean NavigationScript()/
			// EnableNavigation() payload every page already loads, only in
			// a bootstrap bundle. This page registers no island, engine, or
			// hub, so EnableBootstrap resolves to the small "lite" bundle
			// (~29 KB gzipped), the same tier app/wire's page.server.go
			// already opts into for its own data-gosx-region need.
			ctx.Runtime().EnableBootstrap()
			data := league.Default().MatchupsData(ctx.Request.Context(), ctx.Request)
			if matchups, ok := data["matchups"].([]map[string]any); ok {
				data["matchups"] = matchupsPageCards(matchups)
			}
			return data, nil
		},
		Metadata: func(ctx *route.RouteContext, page route.FilePage, data any) (server.Metadata, error) {
			return server.Metadata{
				Title:       server.Title{Default: league.PageTitle("Live Matchups")},
				Description: "Fantasy scores refreshed every sixty seconds from the active league provider.",
			}, nil
		},
	}); err != nil {
		log.Fatal(err)
	}
}
