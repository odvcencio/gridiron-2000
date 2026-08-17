package app

import (
	"log"

	"gridiron-2000/internal/league"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
)

// MatchupTeamCard is the typed spread source for TeamMark's strict props
// (Tone, Abbreviation, Name, HasAvatarImage, AvatarImageURL), plus the
// extra fields MiniMatchup's own legacy body reads (Manager, ID, Score).
// The strict call-site boundary (gosx#184) proves struct values field by
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

// MatchupCard is the typed data.featured entry MiniMatchup's legacy body
// reads (ID, Status, Clock) and spreads (Away, Home) into TeamMark.
type MatchupCard struct {
	ID     string
	Status string
	Clock  string
	Away   MatchupTeamCard
	Home   MatchupTeamCard
}

// StandingTeamCard is the typed data.divisions[].teams entry StandingRow
// reads (Rank, Name, Manager, Record, PointsFor, Streak) and bare-spreads
// into TeamMark (Tone, Abbreviation, Name, HasAvatarImage, AvatarImageURL).
type StandingTeamCard struct {
	Rank           string
	Name           string
	Manager        string
	Record         string
	PointsFor      string
	Streak         string
	Tone           string
	Abbreviation   string
	HasAvatarImage bool
	AvatarImageURL string
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

// dashboardMatchupCards converts DashboardData's map[string]any "featured"
// slice into typed MatchupCard values so MiniMatchup's {...props.Away} /
// {...props.Home} spread into strict TeamMark proves clean: the tier-2
// spread boundary rejects a map[string]any source outright (it "cannot
// prove field coverage").
func dashboardMatchupCards(raw []map[string]any) []MatchupCard {
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

// dashboardDivisions rebuilds DashboardData's "divisions" slice, converting
// each division's "teams" entries into typed StandingTeamCard values for
// StandingRow's {...props} spread into strict TeamMark.
func dashboardDivisions(raw []map[string]any) []map[string]any {
	out := make([]map[string]any, 0, len(raw))
	for _, division := range raw {
		rawTeams, _ := division["teams"].([]map[string]any)
		teams := make([]StandingTeamCard, 0, len(rawTeams))
		for _, team := range rawTeams {
			teams = append(teams, StandingTeamCard{
				Rank:           stringField(team, "rank"),
				Name:           stringField(team, "name"),
				Manager:        stringField(team, "manager"),
				Record:         stringField(team, "record"),
				PointsFor:      stringField(team, "points_for"),
				Streak:         stringField(team, "streak"),
				Tone:           stringField(team, "tone"),
				Abbreviation:   stringField(team, "abbreviation"),
				HasAvatarImage: boolField(team, "has_avatar_image"),
				AvatarImageURL: stringField(team, "avatar_image_url"),
			})
		}
		out = append(out, map[string]any{
			"name":  stringField(division, "name"),
			"teams": teams,
		})
	}
	return out
}

func init() {
	if err := route.RegisterFileModuleHere(route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			ctx.NoStore()
			data := league.Default().DashboardData(ctx.Request.Context(), ctx.Request)
			if featured, ok := data["featured"].([]map[string]any); ok {
				data["featured"] = dashboardMatchupCards(featured)
			}
			if divisions, ok := data["divisions"].([]map[string]any); ok {
				data["divisions"] = dashboardDivisions(divisions)
			}
			return data, nil
		},
		Metadata: func(ctx *route.RouteContext, page route.FilePage, data any) (server.Metadata, error) {
			return server.Metadata{
				Title:       server.Title{Default: league.PageTitle("")},
				Description: "The live league headquarters for " + league.SeatCountArticle() + " " + league.SeatCountWord() + "-manager fantasy football season.",
			}, nil
		},
	}); err != nil {
		log.Fatal(err)
	}
}
