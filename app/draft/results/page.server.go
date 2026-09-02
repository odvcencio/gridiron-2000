package results

import (
	"fmt"
	"log"
	"net/url"
	"strings"

	"gridiron-2000/internal/league"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
)

// resultsView* name the three "?view=" values this page answers (wave 7,
// item 4) — by team (the default, the "?team=" or viewer's own team
// leading), by round, and the grid. The query VALUE is "board", not
// "grid": the agreed URL contract with /team's own post-draft callout
// (item 3, "/draft/results?team=<code>") is "?view=teams|board|rounds",
// matching /draft's own "?view=board" for its identical grid (fragment.go
// draftHistoryViewBoard) — one query vocabulary across both pages, even
// though the visible segment label on both is "Draft grid".
const (
	resultsViewQueryKey = "view"
	resultsTeamQueryKey = "team"
	resultsViewTeams    = "teams"
	resultsViewRounds   = "rounds"
	resultsViewGrid     = "board"
)

// This page's own Load result carries plain map[string]any/[]map[string]any
// values throughout, never a typed struct spread into a strict component
// (Page(), page.gsx, reads every field directly off "data" — the same
// escape hatch app/board's own page.gsx already uses for its own lists):
// a static, read-only results page has no polling region and no
// per-component reuse that would otherwise justify the tier-2 typed-
// spread boundary app/draft's own page.server.go needs for its live
// fragments.
func stringField(m map[string]any, key string) string {
	value, _ := m[key].(string)
	return value
}

func boolField(m map[string]any, key string) bool {
	value, _ := m[key].(bool)
	return value
}

// resultsPickLabel formats pick's round·pick the SAME way service.go's
// playerMap/activityMaps and ledger.go's CSV export already do (wave 7,
// items 2/5): "R3 · P28", never pick.Label's own terser "3.04" tape form
// — a viewer who has already learned that label on /players or
// /activity reads the identical fact here.
func resultsPickLabel(pick league.TapePick) string {
	return fmt.Sprintf("R%d · P%d", pick.Round, pick.Number)
}

func resultsPickMap(pick league.TapePick) map[string]any {
	return map[string]any{
		"label": resultsPickLabel(pick), "player_name": pick.PlayerName, "position": pick.Position, "nfl_team": pick.NFLTeam,
		"has_value": pick.HasValue, "value_label": pick.ValueLabel,
	}
}

// resultsTeamsView converts league's draft-order TeamColumn slice into
// the by-team view's own card order: the named team (leadID, "?team="
// or, when that is empty, the viewer's own team) leads; every other
// team follows in the SAME draft order DraftHistoryView.Teams already
// carries — never re-sorted, so two managers comparing the same league
// still see every OTHER team in one shared, predictable order.
func resultsTeamsView(teams []league.TeamColumn, leadID string) []map[string]any {
	out := make([]map[string]any, 0, len(teams))
	leadIndex := -1
	for index, column := range teams {
		if leadID != "" && stringField(column.Team, "id") == leadID {
			leadIndex = index
			break
		}
	}
	appendTeam := func(column league.TeamColumn) {
		picks := make([]map[string]any, 0, len(column.Picks))
		for _, pick := range column.Picks {
			picks = append(picks, resultsPickMap(pick))
		}
		out = append(out, map[string]any{
			"id": stringField(column.Team, "id"), "name": stringField(column.Team, "name"), "abbreviation": stringField(column.Team, "abbreviation"),
			"tone": stringField(column.Team, "tone"), "manager": stringField(column.Team, "manager"),
			"has_avatar_image": boolField(column.Team, "has_avatar_image"), "avatar_image_url": stringField(column.Team, "avatar_image_url"),
			"mine": boolField(column.Team, "mine"), "picks": picks, "pick_count": len(picks),
		})
	}
	if leadIndex >= 0 {
		appendTeam(teams[leadIndex])
	}
	for index, column := range teams {
		if index == leadIndex {
			continue
		}
		appendTeam(column)
	}
	return out
}

// resultsRoundsView groups history's ascending ledger (Picks) into
// round-ascending lists — Round 1 first, unlike DraftHistoryView.Rounds
// (the live tape's own newest-round-first order, built for a room still
// in progress, not a finished record read start to finish).
func resultsRoundsView(picks []league.TapePick) []map[string]any {
	out := make([]map[string]any, 0)
	var currentRound int
	var currentPicks []map[string]any
	flush := func() {
		if currentPicks != nil {
			out = append(out, map[string]any{"round": currentRound, "picks": currentPicks})
		}
	}
	for _, pick := range picks {
		if currentPicks == nil || currentRound != pick.Round {
			flush()
			currentRound = pick.Round
			currentPicks = nil
		}
		currentPicks = append(currentPicks, map[string]any{
			"label": resultsPickLabel(pick), "team_name": pick.TeamName, "team_abbr": pick.TeamAbbr, "team_tone": pick.TeamTone, "manager": pick.Manager,
			"has_avatar_image": pick.HasAvatarImage, "avatar_image_url": pick.AvatarImageURL,
			"player_name": pick.PlayerName, "position": pick.Position, "nfl_team": pick.NFLTeam, "mine": pick.Mine,
		})
	}
	flush()
	return out
}

func resultsBoardCellMap(cell league.BoardCell) map[string]any {
	return map[string]any{
		"label": cell.Label, "filled": cell.Filled, "mine": cell.Mine, "on_clock": cell.OnClock,
		"player_name": cell.PlayerName, "position": cell.Position, "nfl_team": cell.NFLTeam,
		"is_auto": cell.IsAuto, "is_commissioner": cell.IsCommissioner,
	}
}

func resultsBoardView(board league.BoardView) map[string]any {
	columns := make([]map[string]any, 0, len(board.Columns))
	mineID := ""
	for _, column := range board.Columns {
		columns = append(columns, map[string]any{
			"id": stringField(column, "id"), "name": stringField(column, "name"), "abbreviation": stringField(column, "abbreviation"),
			"tone": stringField(column, "tone"), "has_avatar_image": boolField(column, "has_avatar_image"), "avatar_image_url": stringField(column, "avatar_image_url"),
			"mine": boolField(column, "mine"),
		})
		if boolField(column, "mine") {
			mineID = stringField(column, "id")
		}
	}
	rows := make([]map[string]any, 0, len(board.Rows))
	for _, row := range board.Rows {
		cells := make([]map[string]any, 0, len(row.Cells))
		for _, cell := range row.Cells {
			cells = append(cells, resultsBoardCellMap(cell))
		}
		rows = append(rows, map[string]any{"round": row.Round, "direction": row.Direction, "cells": cells})
	}
	return map[string]any{
		"columns": columns, "rows": rows, "column_count": fmt.Sprintf("%d", len(columns)),
		"has_mine": mineID != "", "mine_id": mineID,
	}
}

// resultsHref builds one "/draft/results?..." navigation target, always
// carrying the current "?team=" (so switching Teams/Rounds/Grid never
// drops which team a "?team=" link asked to lead with) plus the given
// view.
func resultsHref(view, team string) string {
	values := url.Values{}
	values.Set(resultsViewQueryKey, view)
	if team != "" {
		values.Set(resultsTeamQueryKey, team)
	}
	return "/draft/results?" + values.Encode()
}

func resultsNormalizeView(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case resultsViewRounds:
		return resultsViewRounds
	case resultsViewGrid:
		return resultsViewGrid
	default:
		return resultsViewTeams
	}
}

func init() {
	if err := route.RegisterFileModuleHere(route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			ctx.NoStore()
			raw := league.Default().DraftResultsData(ctx.Request)
			history, _ := raw["history"].(league.DraftHistoryView)
			viewerTeamID := stringField(raw, "viewer_team_id")

			query := ctx.Request.URL.Query()
			leadTeam := strings.ToUpper(strings.TrimSpace(query.Get(resultsTeamQueryKey)))
			leadID := ""
			if leadTeam != "" {
				for _, column := range history.Teams {
					if strings.EqualFold(stringField(column.Team, "abbreviation"), leadTeam) || strings.EqualFold(stringField(column.Team, "id"), leadTeam) {
						leadID = stringField(column.Team, "id")
						break
					}
				}
			}
			if leadID == "" {
				leadID = viewerTeamID
			}

			view := resultsNormalizeView(query.Get(resultsViewQueryKey))
			data := map[string]any{
				"complete":    boolField(raw, "complete"),
				"long_date":   stringField(raw, "long_date"),
				"time":        stringField(raw, "time"),
				"timezone":    stringField(raw, "timezone"),
				"published":   boolField(raw, "published"),
				"rounds":      raw["rounds"],
				"team_count":  raw["team_count"],
				"view":        view,
				"show_teams":  view == resultsViewTeams,
				"show_rounds": view == resultsViewRounds,
				"show_grid":   view == resultsViewGrid,
				"teams_href":  resultsHref(resultsViewTeams, leadTeam),
				"rounds_href": resultsHref(resultsViewRounds, leadTeam),
				"grid_href":   resultsHref(resultsViewGrid, leadTeam),
				"teams":       resultsTeamsView(history.Teams, leadID),
				"team_rounds": resultsRoundsView(history.Picks),
				"board":       resultsBoardView(history.Board),
				"ledger_href": "/draft/ledger.csv",
			}
			return data, nil
		},
		Metadata: func(ctx *route.RouteContext, page route.FilePage, data any) (server.Metadata, error) {
			return server.Metadata{
				Title:       server.Title{Default: league.PageTitle("Draft results")},
				Description: "Every pick from the league draft — by team, by round, or on the grid.",
			}, nil
		},
	}); err != nil {
		log.Fatal(err)
	}
}
