package guide

import (
	"fmt"
	"log"
	"strings"
	"time"

	"gridiron-2000/internal/fantasy"
	"gridiron-2000/internal/league"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
)

var loadPublicGuideData = func() map[string]any {
	svc := league.Default()
	return publicGuideDataAt(svc.Config(), svc.DraftAt(), svc.MembershipPosture())
}

// publicGuideData deliberately projects only operator-authored, public league
// configuration plus the PII-free posture contract. It never exposes members,
// invitation addresses, seats, picks, boards, or any other private runtime
// state.
func publicGuideData(cfg league.Config) map[string]any {
	return publicGuideDataAt(cfg, cfg.DraftAt)
}

func publicGuideDataAt(cfg league.Config, effectiveDraftAt time.Time, postures ...league.MembershipPosture) map[string]any {
	teamCount := len(cfg.Teams)
	rosterSpots := cfg.Roster.Total()
	capacity := teamCount * rosterSpots
	target := fantasy.ScaledPoolLimit(teamCount, rosterSpots)
	mode := strings.ToUpper(strings.TrimSpace(cfg.ModeLabel))
	if mode == "" {
		mode = "LEAGUE"
	}

	posture := league.ResolveMembershipPosture(cfg.Membership.AllowedDomain, false)
	if len(postures) > 0 {
		posture = postures[0]
	}
	draftAt := effectiveDraftAt
	if location, err := time.LoadLocation(cfg.Timezone); err == nil {
		draftAt = draftAt.In(location)
	}

	coverage := "0.00x"
	if capacity > 0 {
		coverage = fmt.Sprintf("%.2fx", float64(target)/float64(capacity))
	}
	return map[string]any{
		"league_name":             cfg.Name,
		"mode_label":              mode,
		"team_count":              teamCount,
		"roster_spots":            rosterSpots,
		"roster_capacity":         capacity,
		"draft_at":                draftAt.Format("Mon, Jan 2, 2006 · 3:04 PM MST"),
		"draft_timezone":          cfg.Timezone,
		"membership_label":        posture.Label(),
		"membership_detail":       posture.Detail(),
		"pool_target":             target,
		"pool_target_cushion":     max(0, target-capacity),
		"pool_target_coverage":    coverage,
		"league_format_summary":   fmt.Sprintf("%d teams · %s", teamCount, mode),
		"league_capacity_summary": fmt.Sprintf("%d × %d = %d draft slots", teamCount, rosterSpots, capacity),
	}
}

func guideString(data any, key, fallback string) string {
	values, ok := data.(map[string]any)
	if !ok {
		return fallback
	}
	value, ok := values[key].(string)
	if !ok || strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func init() {
	if err := route.RegisterFileModuleHere(route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			ctx.NoStore()
			return loadPublicGuideData(), nil
		},
		Metadata: func(ctx *route.RouteContext, page route.FilePage, data any) (server.Metadata, error) {
			leagueName := guideString(data, "league_name", "Gridiron")
			format := guideString(data, "league_format_summary", "private fantasy football")
			return server.Metadata{
				Title:       server.Title{Default: "Manager Guide · " + leagueName},
				Description: fmt.Sprintf("A plain-language manager and commissioner guide to %s (%s).", leagueName, format),
			}, nil
		},
	}); err != nil {
		log.Fatal(err)
	}
}
