package league

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

// PlayoffTruthData is the shared, read-only postseason projection consumed by
// member pages, commissioner surfaces, and the fleet summary. It deliberately
// reads the persisted snapshot only; a preview is never exposed to members
// until the commissioner publishes it.
func (s *Service) PlayoffTruthData(r *http.Request) map[string]any {
	state := s.store.Snapshot()
	commissioner := r != nil && s.IsCommissioner(r)
	return s.playoffTruthMap(state, s.clock(), commissioner)
}

func (s *Service) playoffTruthMap(state PersistedState, now time.Time, commissioner bool) map[string]any {
	phase := state.Phase
	if phase == "" {
		phase = s.SeasonPhase(now)
	}
	base := map[string]any{
		"status":                    "waiting",
		"canonical_status":          "",
		"status_label":              "WAITING",
		"season_phase":              phase,
		"season_phase_label":        playoffPhaseLabel(phase),
		"has_bracket":               false,
		"published":                 false,
		"preview":                   false,
		"is_published":              false,
		"is_preview":                false,
		"waiting":                   true,
		"headline":                  "PLAYOFF BRACKET WAITING",
		"detail":                    "The persisted playoff bracket is not available yet.",
		"recovery":                  "",
		"source":                    "",
		"source_state":              "",
		"authoritative":             false,
		"snapshot_id":               "",
		"captured_at":               "",
		"published_at":              "",
		"preview_id":                "",
		"revision":                  0,
		"final_week":                0,
		"regular_season_start_week": 0,
		"tiebreak_order":            []string{},
		"config":                    playoffConfigMap(PlayoffConfig{}),
		"seeds":                     []map[string]any{},
		"matchups":                  []map[string]any{},
		"audit":                     []map[string]any{},
		"has_matchups":              false,
		"current_round":             0,
		"next_matchup_count":        0,
		"champion_team_id":          "",
		"champion_name":             "",
		"runner_up_team_id":         "",
		"runner_up_name":            "",
		"toilet_team_id":            "",
		"toilet_name":               "",
	}
	truth := state.Playoffs
	if truth == nil {
		if phase == PhasePlayoffs {
			base["detail"] = "The regular season is final. A commissioner preview and explicit publication are required before bracket truth is available."
			base["recovery"] = "Open the commissioner console, preview from final standings, review provenance, then publish the preview."
		} else if phase == PhaseSeasonComplete {
			base["detail"] = "The season is marked complete, but no persisted playoff bracket is available."
			base["recovery"] = "Commissioner review is required before postseason results can be displayed."
		} else {
			base["headline"] = "PLAYOFFS NOT ACTIVE"
			base["detail"] = "The published season phase is " + playoffPhaseLabel(phase) + "; playoff truth will appear after the regular season is final."
		}
		return base
	}

	copy := clonePlayoffState(truth)
	status := effectivePlayoffStatus(*copy)
	base["canonical_status"] = status
	base["revision"] = copy.Revision
	base["preview_id"] = copy.PreviewID
	base["source"] = copy.Provenance.Source
	base["source_state"] = copy.Provenance.SourceState
	base["authoritative"] = copy.Provenance.Authoritative
	base["snapshot_id"] = copy.Provenance.SnapshotID
	base["captured_at"] = playoffDisplayTime(copy.Provenance.CapturedAt)
	base["published_at"] = playoffDisplayTime(copy.PublishedAt)
	base["final_week"] = copy.Provenance.FinalWeek
	base["regular_season_start_week"] = copy.Provenance.RegularSeasonStartWeek
	base["tiebreak_order"] = append([]string(nil), copy.Provenance.TiebreakOrder...)
	base["config"] = playoffConfigMap(copy.Config)
	provenanceExplicit := strings.TrimSpace(copy.Provenance.Source) != "" || strings.TrimSpace(copy.Provenance.SourceState) != "" || copy.Provenance.Authoritative || !copy.Provenance.CapturedAt.IsZero()
	if provenanceExplicit && (strings.ToLower(strings.TrimSpace(copy.Provenance.SourceState)) != "final" || !copy.Provenance.Authoritative) {
		base["detail"] = "The persisted playoff source is partial, stale, degraded, or non-final; it is not shown as authoritative bracket truth."
		base["recovery"] = "Wait for a complete final source, then have the commissioner build and publish a fresh preview."
		return base
	}

	if status == PlayoffStatusPreview && !commissioner {
		base["status"] = "waiting"
		base["status_label"] = "WAITING"
		base["detail"] = "A commissioner preview exists, but it is not published. Preview is not bracket truth."
		base["recovery"] = "The commissioner must review the final-standings provenance and publish the preview before members can see seeds or matchups."
		return base
	}

	base["status"] = status
	base["has_bracket"] = status == PlayoffStatusPublished || (status == PlayoffStatusPreview && commissioner)
	base["published"] = status == PlayoffStatusPublished
	base["preview"] = status == PlayoffStatusPreview && commissioner
	base["is_published"] = base["published"]
	base["is_preview"] = base["preview"]
	if status == PlayoffStatusPreview {
		base["status_label"] = "PREVIEW"
		base["headline"] = "PLAYOFF BRACKET PREVIEW"
		base["detail"] = "Preview only · not published to managers. Review seeds, tie-break explanations, and provenance before publication."
		base["recovery"] = "Publish this exact preview from the commissioner console with the explicit confirmation phrase."
	} else if status == PlayoffStatusPublished {
		base["status_label"] = "PUBLISHED"
		base["headline"] = "PLAYOFF BRACKET PUBLISHED"
		base["detail"] = "One persisted bracket truth · advancement accepts only complete, final, authoritative scoring ledgers."
		base["recovery"] = "If a round is still open, wait for every source week to become final and retry ledger advancement. Earlier-round corrections require a fresh preview."
	} else {
		base["status_label"] = "WAITING"
		base["detail"] = "The persisted bracket is not in a publishable lifecycle state."
		base["recovery"] = "Commissioner review is required; no degraded or partial bracket is shown as authoritative."
	}

	seeds := make([]map[string]any, 0, len(copy.Seeds))
	for _, seed := range copy.Seeds {
		team := s.playoffTeamMap(state, seed.TeamID)
		seeds = append(seeds, map[string]any{
			"seed": seed.Seed, "team_id": seed.TeamID, "team_name": team["name"],
			"abbreviation": team["abbreviation"], "source": seed.Source,
			"tie_break_explanation": seed.TieBreakExplanation, "team": team,
		})
	}
	base["seeds"] = seeds
	matchups := make([]map[string]any, 0, len(copy.Matchups))
	currentRound := 0
	nextCount := 0
	for _, matchup := range copy.Matchups {
		home := s.playoffTeamMap(state, matchup.HomeTeamID)
		away := s.playoffTeamMap(state, matchup.AwayTeamID)
		winner := s.playoffTeamMap(state, matchup.WinnerTeamID)
		resultSource, resultState, resultAt, resultAuthoritative := "", "", "", false
		if matchup.ResultProvenance != nil {
			resultSource = matchup.ResultProvenance.Source
			resultState = matchup.ResultProvenance.SourceState
			resultAt = playoffDisplayTime(matchup.ResultProvenance.ObservedAt)
			resultAuthoritative = matchup.ResultProvenance.Authoritative
		}
		if !matchup.Final && matchup.HomeTeamID != "" && matchup.AwayTeamID != "" {
			nextCount++
			if currentRound == 0 || matchup.Round < currentRound {
				currentRound = matchup.Round
			}
		}
		matchups = append(matchups, map[string]any{
			"id": matchup.ID, "bracket": matchup.Bracket, "round": matchup.Round, "week": matchup.Week,
			"home_seed": matchup.HomeSeed, "away_seed": matchup.AwaySeed,
			"home_team_id": matchup.HomeTeamID, "away_team_id": matchup.AwayTeamID,
			"home_team_name": home["name"], "away_team_name": away["name"],
			"home": home, "away": away, "has_away": matchup.AwayTeamID != "", "bye": matchup.AwayTeamID == "",
			"home_score": matchup.HomeScore, "away_score": matchup.AwayScore,
			"home_score_text": fmt.Sprintf("%.1f", matchup.HomeScore), "away_score_text": fmt.Sprintf("%.1f", matchup.AwayScore),
			"final": matchup.Final, "winner_team_id": matchup.WinnerTeamID, "winner_name": winner["name"],
			"winner": winner, "tie_break_explanation": matchup.TieBreakExplanation,
			"source": resultSource, "source_state": resultState, "observed_at": resultAt, "authoritative": resultAuthoritative,
		})
	}
	if currentRound == 0 {
		for _, matchup := range copy.Matchups {
			if matchup.Round > currentRound {
				currentRound = matchup.Round
			}
		}
	}
	base["matchups"] = matchups
	base["has_matchups"] = len(matchups) > 0
	base["current_round"] = currentRound
	base["next_matchup_count"] = nextCount

	audit := make([]map[string]any, 0, len(copy.Audit))
	for _, entry := range copy.Audit {
		audit = append(audit, map[string]any{
			"action": entry.Action, "actor": entry.Actor, "at": playoffDisplayTime(entry.At),
			"reason": entry.Reason, "preview_id": entry.PreviewID, "matchup_id": entry.MatchupID,
			"winner_team_id": entry.WinnerTeamID, "previous_winner_team_id": entry.PreviousWinnerTeamID,
			"revision": entry.Revision,
		})
	}
	base["audit"] = audit
	base["champion_team_id"] = copy.ChampionTeamID
	base["champion_name"] = s.playoffTeamMap(state, copy.ChampionTeamID)["name"]
	base["runner_up_team_id"] = copy.RunnerUpTeamID
	base["runner_up_name"] = s.playoffTeamMap(state, copy.RunnerUpTeamID)["name"]
	base["toilet_team_id"] = copy.ToiletTeamID
	base["toilet_name"] = s.playoffTeamMap(state, copy.ToiletTeamID)["name"]
	if status == PlayoffStatusPublished && nextCount == 0 && copy.ChampionTeamID == "" {
		base["detail"] = "Published bracket is waiting for an active round result."
	}
	if copy.ChampionTeamID != "" {
		base["detail"] = "Championship result is final and persisted as the season truth."
		base["recovery"] = "A terminal correction requires commissioner confirmation and an audit reason; earlier-round correction requires a fresh preview."
	}
	return base
}

func playoffConfigMap(cfg PlayoffConfig) map[string]any {
	return map[string]any{
		"team_count": cfg.TeamCount, "start_week": cfg.StartWeek, "round_length_weeks": cfg.RoundLengthWeeks,
		"qualification": cfg.Qualification, "tiebreak_order": append([]string(nil), cfg.TiebreakOrder...),
		"byes": cfg.Byes, "division_winners_first": cfg.DivisionWinnersFirst, "reseed": cfg.Reseed,
		"consolation": cfg.Consolation, "toilet_bowl": cfg.ToiletBowl,
	}
}

func (s *Service) playoffTeamMap(state PersistedState, teamID string) map[string]any {
	team := Team{ID: teamID, Name: teamID}
	for _, candidate := range s.Teams() {
		if candidate.ID == teamID {
			team = candidate
			break
		}
	}
	if override := strings.TrimSpace(state.TeamNames[teamID]); override != "" {
		team.Name = override
	}
	return map[string]any{
		"id": teamID, "name": team.Name, "abbreviation": team.Abbreviation,
	}
}

func playoffDisplayTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func playoffPhaseLabel(phase string) string {
	switch strings.ToLower(strings.TrimSpace(phase)) {
	case PhasePlayoffs:
		return "PLAYOFFS"
	case PhaseSeasonComplete:
		return "SEASON COMPLETE"
	case PhaseRegularSeason:
		return "REGULAR SEASON"
	case "preseason":
		return "PRESEASON"
	default:
		return "WAITING"
	}
}
