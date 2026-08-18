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

// DivisionCard is the typed data.divisions entry (fix, registration
// wave): Teams must be a concrete typed slice at the top level, not
// nested inside a map[string]any wrapper. A []StandingTeamCard nested
// inside a plain map value loses its concrete Go type by the time
// <Each of={division.teams}> reaches StandingRow's {...props} spread
// into strict TeamMark — the render pipeline only preserves a value's
// real struct type through a directly, statically typed field, the same
// reason dashboardMatchupCards returns []MatchupCard at the top level
// rather than wrapping it in another map. Before this fix, any signed-in
// seated visit to the dashboard hit a hard render error at this spread
// ("spread source has type map[string]interface {}") — unreachable in
// this repo's test suite (which only asserts DashboardData's map shape,
// never renders the .gsx template) and in demo mode (Viewer never
// reports signed_in for an anonymous demo visitor), so nothing had
// exercised this render path until the registration wave's manual
// browser verification.
type DivisionCard struct {
	Name  string
	Teams []StandingTeamCard
}

// dashboardDivisions rebuilds DashboardData's "divisions" slice, converting
// each division's "teams" entries into typed StandingTeamCard values for
// StandingRow's {...props} spread into strict TeamMark.
func dashboardDivisions(raw []map[string]any) []DivisionCard {
	out := make([]DivisionCard, 0, len(raw))
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
		out = append(out, DivisionCard{
			Name:  stringField(division, "name"),
			Teams: teams,
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
