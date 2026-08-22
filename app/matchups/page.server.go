package matchups

import (
	"log"

	"gridiron-2000/internal/league"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
)

// MatchupCardData is the typed data.matchups entry: structurally
// identical to page.gsx's strict MatchupCardProps, field for field. It
// is flat (AwayID, AwayName, ... HomeID, HomeName, ...) rather than
// nesting Away/Home sub-structs, because Page() (page.gsx) reaches the
// strict MatchupCard component through exactly one {...matchup} spread;
// that spread proves this type structurally covers MatchupCardProps at
// the top level, but a nested struct-typed field would additionally
// need to match MatchupCardProps' corresponding field by exact type
// identity — impossible to arrange cleanly across a .go/.gsx file pair,
// since gosx's strict-component check separately requires that any
// struct type MatchupCard's own body reaches through field access be
// declared beside the component, in the .gsx file, not here. Flattening
// removes every nested struct field, so only plain scalars (string,
// bool) cross the boundary, and both requirements are satisfied without
// contradiction. It is named MatchupCardData, not MatchupCard, because
// page.gsx's strict `component MatchupCard` compiles to a package-level
// Go declaration named MatchupCard too; a converter type sharing that
// exact name here would collide with it.
type MatchupCardData struct {
	ID                 string
	State              string
	ShowLiveIndicator  bool
	Status             string
	Clock              string
	AwayID             string
	AwayName           string
	AwayManager        string
	AwayScore          string
	AwayTone           string
	AwayAbbreviation   string
	AwayHasAvatarImage bool
	AwayAvatarImageURL string
	HomeID             string
	HomeName           string
	HomeManager        string
	HomeScore          string
	HomeTone           string
	HomeAbbreviation   string
	HomeHasAvatarImage bool
	HomeAvatarImageURL string
}

func stringField(m map[string]any, key string) string {
	value, _ := m[key].(string)
	return value
}

func boolField(m map[string]any, key string) bool {
	value, _ := m[key].(bool)
	return value
}

// matchupsPageCards converts MatchupsData's map[string]any "matchups" slice
// (each entry carrying nested "away"/"home" maps) into flat typed
// MatchupCardData values, so page.gsx's strict MatchupCard/ScoreTeam/
// TeamMark spread and attribute boundaries all prove clean: the tier-2
// spread boundary rejects a map[string]any source outright (it "cannot
// prove field coverage").
func matchupsPageCards(raw []map[string]any) []MatchupCardData {
	out := make([]MatchupCardData, 0, len(raw))
	for _, entry := range raw {
		away, _ := entry["away"].(map[string]any)
		home, _ := entry["home"].(map[string]any)
		out = append(out, MatchupCardData{
			ID:                 stringField(entry, "id"),
			State:              stringField(entry, "state"),
			ShowLiveIndicator:  boolField(entry, "show_live_indicator"),
			Status:             stringField(entry, "status"),
			Clock:              stringField(entry, "clock"),
			AwayID:             stringField(away, "id"),
			AwayName:           stringField(away, "name"),
			AwayManager:        stringField(away, "manager"),
			AwayScore:          stringField(away, "score"),
			AwayTone:           stringField(away, "tone"),
			AwayAbbreviation:   stringField(away, "abbreviation"),
			AwayHasAvatarImage: boolField(away, "has_avatar_image"),
			AwayAvatarImageURL: stringField(away, "avatar_image_url"),
			HomeID:             stringField(home, "id"),
			HomeName:           stringField(home, "name"),
			HomeManager:        stringField(home, "manager"),
			HomeScore:          stringField(home, "score"),
			HomeTone:           stringField(home, "tone"),
			HomeAbbreviation:   stringField(home, "abbreviation"),
			HomeHasAvatarImage: boolField(home, "has_avatar_image"),
			HomeAvatarImageURL: stringField(home, "avatar_image_url"),
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
				Title:       server.Title{Default: league.PageTitle("Matchups")},
				Description: "Fantasy matchup schedules, scoring status, and final results.",
			}, nil
		},
	}); err != nil {
		log.Fatal(err)
	}
}
