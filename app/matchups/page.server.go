package matchups

import (
	"log"

	"gridiron-2000/internal/league"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
)

// MatchupTeamCard is the typed spread source for TeamMark's strict props
// (Tone, Abbreviation, Name, HasAvatarImage, AvatarImageURL), plus the
// extra fields ScoreTeam's own legacy body reads (Manager, ID, Score). The
// strict call-site boundary (gosx#184) proves struct values field by
// field, so this only needs to structurally cover TeamMarkProps, not share
// its name or identity.
type MatchupTeamCard struct {
	ID             string
	Name           string
	Manager        string
	Score          string
	Tone           string
	Abbreviation   string
	HasAvatarImage bool
	AvatarImageURL string
}

// MatchupCard is the typed data.matchups entry MatchupCard's legacy body
// reads (ID, Status, Clock) and spreads (Away, Home) into ScoreTeam, which
// bare-spreads its own props straight into strict TeamMark.
type MatchupCard struct {
	ID     string
	Status string
	Clock  string
	Away   MatchupTeamCard
	Home   MatchupTeamCard
}

func stringField(m map[string]any, key string) string {
	value, _ := m[key].(string)
	return value
}

func boolField(m map[string]any, key string) bool {
	value, _ := m[key].(bool)
	return value
}

func matchupTeamCardFromMap(raw map[string]any) MatchupTeamCard {
	return MatchupTeamCard{
		ID:             stringField(raw, "id"),
		Name:           stringField(raw, "name"),
		Manager:        stringField(raw, "manager"),
		Score:          stringField(raw, "score"),
		Tone:           stringField(raw, "tone"),
		Abbreviation:   stringField(raw, "abbreviation"),
		HasAvatarImage: boolField(raw, "has_avatar_image"),
		AvatarImageURL: stringField(raw, "avatar_image_url"),
	}
}

// matchupsPageCards converts MatchupsData's map[string]any "matchups" slice
// into typed MatchupCard values so ScoreTeam's bare {...props} spread into
// strict TeamMark proves clean: the tier-2 spread boundary rejects a
// map[string]any source outright (it "cannot prove field coverage").
func matchupsPageCards(raw []map[string]any) []MatchupCard {
	out := make([]MatchupCard, 0, len(raw))
	for _, entry := range raw {
		away, _ := entry["away"].(map[string]any)
		home, _ := entry["home"].(map[string]any)
		out = append(out, MatchupCard{
			ID:     stringField(entry, "id"),
			Status: stringField(entry, "status"),
			Clock:  stringField(entry, "clock"),
			Away:   matchupTeamCardFromMap(away),
			Home:   matchupTeamCardFromMap(home),
		})
	}
	return out
}

func init() {
	if err := route.RegisterFileModuleHere(route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			ctx.NoStore()
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
