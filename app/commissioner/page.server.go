package commissioner

import (
	"fmt"
	"strings"

	"gridiron-2000/internal/commissionerhq"
	"gridiron-2000/internal/league"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
)

func init() {
	if err := route.RegisterFileModuleHere(route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			ctx.NoStore()
			isCommissioner := league.Default().IsCommissioner(ctx.Request)
			data := map[string]any{
				"viewer": league.Default().Viewer(ctx.Request), "is_commissioner": isCommissioner,
				"cards": []map[string]any{}, "league_count": 0, "attention_count": 0,
				"claimed_seats": 0, "total_seats": 0, "drafts_live": 0,
			}
			if !isCommissioner {
				return data, nil
			}
			service := commissionerhq.Default()
			entries := service.Fleet(ctx.Request.Context())
			cards := make([]map[string]any, 0, len(entries))
			for _, entry := range entries {
				card := fleetCard(entry)
				cards = append(cards, card)
				if entry.Available() {
					data["claimed_seats"] = data["claimed_seats"].(int) + entry.Summary.Membership.ClaimedSeats
					data["total_seats"] = data["total_seats"].(int) + entry.Summary.Membership.Seats
					data["attention_count"] = data["attention_count"].(int) + len(entry.Summary.Attention)
					if entry.Summary.Draft.Status == "live" {
						data["drafts_live"] = data["drafts_live"].(int) + 1
					}
				}
			}
			data["cards"] = cards
			data["league_count"] = len(cards)
			data["federation_enabled"] = service.Enabled()
			return data, nil
		},
		Metadata: func(ctx *route.RouteContext, page route.FilePage, data any) (server.Metadata, error) {
			return server.Metadata{
				Title:       server.Title{Default: league.PageTitle("Commissioner HQ")},
				Description: "Read-only fleet status for every league this commissioner operates.",
			}, nil
		},
	}); err != nil {
		panic(err)
	}
}

func fleetCard(entry commissionerhq.FleetEntry) map[string]any {
	if !entry.Available() {
		return map[string]any{
			"available": false, "peer_id": entry.PeerID, "public_url": entry.PublicURL, "error": entry.Error,
		}
	}
	summary := entry.Summary
	base := strings.TrimRight(summary.Instance.PublicURL, "/")
	attention := make([]map[string]any, 0, len(summary.Attention))
	for _, item := range summary.Attention {
		attention = append(attention, map[string]any{
			"severity": item.Severity, "message": item.Message,
		})
	}
	return map[string]any{
		"available": true, "peer_id": entry.PeerID,
		"name": summary.Instance.Name, "short_code": summary.Instance.ShortCode,
		"mode": summary.Instance.Mode, "season": summary.Instance.Season,
		"ready": summary.Runtime.Ready, "version": summary.Runtime.AppVersion,
		"git_sha": shortSHA(summary.Runtime.GitSHA),
		"seats":   summary.Membership.Seats, "claimed_seats": summary.Membership.ClaimedSeats,
		"ready_seats":  summary.Membership.ReadySeats,
		"draft_status": strings.ToUpper(summary.Draft.Status),
		"draft_at":     summary.Draft.ScheduledAt.Format("Mon, Jan 2 · 3:04 PM MST"),
		"picks":        summary.Draft.Picks, "draft_slots": summary.Pool.RosterCapacity,
		"pool_mode": strings.ToUpper(summary.Pool.Mode), "pool_players": summary.Pool.Actual,
		"pool_target": summary.Pool.Target, "pool_cushion": summary.Pool.Cushion,
		"pool_coverage": fmt.Sprintf("%.1f×", summary.Pool.ActualCoverage),
		"attention":     attention, "has_attention": len(attention) > 0,
		"attention_count": len(attention),
		"home_url":        base + "/", "admin_url": base + "/admin", "draft_url": base + "/draft",
	}
}

func shortSHA(value string) string {
	if len(value) > 7 {
		return value[:7]
	}
	return value
}
