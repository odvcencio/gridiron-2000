package league

import (
	"fmt"
	"net/url"
	"time"
)

// TeamTerminalPhase is the finite lifecycle presented by the Team terminal.
// Draft scheduling is intentionally not a phase: only the persisted
// commissioner start transition moves a league out of PRE_DRAFT.
type TeamTerminalPhase string

const (
	TeamTerminalPreDraft          TeamTerminalPhase = "PRE_DRAFT"
	TeamTerminalDraftLiveEmpty    TeamTerminalPhase = "DRAFT_LIVE_EMPTY"
	TeamTerminalDraftLiveBuilding TeamTerminalPhase = "DRAFT_LIVE_BUILDING"
	TeamTerminalRosterComplete    TeamTerminalPhase = "ROSTER_COMPLETE"
)

// TeamTerminalLifecycle keeps draft lifecycle and roster occupancy separate.
// A team can be empty while the draft is live, and a completed draft can still
// have an empty starting lineup until its manager assigns starters.
type TeamTerminalLifecycle struct {
	Phase          TeamTerminalPhase
	RosterCount    int
	RosterCapacity int
	DraftStarted   bool
	DraftComplete  bool
}

// resolveTeamTerminalLifecycle is the single phase resolver used by the Team
// page. rosterCount is derived from currentRosters (not Picks alone), while
// draftComplete remains the league-wide pick-capacity truth.
func resolveTeamTerminalLifecycle(state PersistedState, rosterCount, rosterCapacity int) TeamTerminalLifecycle {
	lifecycle := TeamTerminalLifecycle{
		RosterCount:    rosterCount,
		RosterCapacity: rosterCapacity,
		DraftStarted:   state.DraftStarted,
		DraftComplete:  draftComplete(state),
	}
	switch {
	case lifecycle.DraftComplete:
		lifecycle.Phase = TeamTerminalRosterComplete
	case !lifecycle.DraftStarted:
		lifecycle.Phase = TeamTerminalPreDraft
	case rosterCount == 0:
		lifecycle.Phase = TeamTerminalDraftLiveEmpty
	default:
		lifecycle.Phase = TeamTerminalDraftLiveBuilding
	}
	return lifecycle
}

func (l TeamTerminalLifecycle) data() map[string]any {
	return map[string]any{
		"team_terminal_phase":               string(l.Phase),
		"team_terminal_pre_draft":           l.Phase == TeamTerminalPreDraft,
		"team_terminal_draft_live":          l.Phase == TeamTerminalDraftLiveEmpty || l.Phase == TeamTerminalDraftLiveBuilding,
		"team_terminal_draft_live_empty":    l.Phase == TeamTerminalDraftLiveEmpty,
		"team_terminal_draft_live_building": l.Phase == TeamTerminalDraftLiveBuilding,
		"team_terminal_roster_complete":     l.Phase == TeamTerminalRosterComplete,
		"team_terminal_roster_count":        l.RosterCount,
		"team_terminal_roster_capacity":     l.RosterCapacity,
		"team_terminal_roster_empty":        l.RosterCount == 0,
	}
}

func (l TeamTerminalLifecycle) copy() map[string]any {
	data := l.data()
	switch l.Phase {
	case TeamTerminalPreDraft:
		data["team_terminal_label"] = "ROSTER EMPTY · DRAFT NOT STARTED"
		data["team_terminal_detail"] = fmt.Sprintf("Your configured roster shape is ready: %d spots stay empty until the commissioner starts the room and picks are made.", l.RosterCapacity)
		data["team_terminal_primary_href"] = "/board"
		data["team_terminal_primary_label"] = "Open Big Board"
		data["team_terminal_secondary_href"] = "/draft"
		data["team_terminal_secondary_label"] = "Open Draft Room"
	case TeamTerminalDraftLiveEmpty:
		data["team_terminal_label"] = "DRAFT LIVE · FIRST PICK PENDING"
		data["team_terminal_detail"] = "The commissioner opened the room. Your roster is still empty; stay in the Draft Room for your first pick or AUTO selection."
		data["team_terminal_primary_href"] = "/draft"
		data["team_terminal_primary_label"] = "Open Draft Room"
		data["team_terminal_secondary_href"] = "/board"
		data["team_terminal_secondary_label"] = "Review Big Board"
	case TeamTerminalDraftLiveBuilding:
		data["team_terminal_label"] = "DRAFT LIVE · ROSTER BUILDING"
		data["team_terminal_detail"] = fmt.Sprintf("%d of %d roster spots are filled. Keep the Draft Room open for your next pick and watch AUTO selections as they happen.", l.RosterCount, l.RosterCapacity)
		data["team_terminal_primary_href"] = "/draft"
		data["team_terminal_primary_label"] = "Open Draft Room"
		data["team_terminal_secondary_href"] = "/board"
		data["team_terminal_secondary_label"] = "Review Big Board"
	case TeamTerminalRosterComplete:
		data["team_terminal_label"] = "ROSTER COMPLETE · SET YOUR LINEUP"
		data["team_terminal_detail"] = fmt.Sprintf("The draft is complete and %d roster spots are available for lineup, waiver, and matchup decisions.", l.RosterCount)
		data["team_terminal_primary_href"] = "/players"
		data["team_terminal_primary_label"] = "Open Player Pool"
		data["team_terminal_secondary_href"] = "/matchups"
		data["team_terminal_secondary_label"] = "View Matchup"
	}
	return data
}

func radarSignal(player Player) string {
	if player.ADPRank > 0 {
		return fmt.Sprintf("ADP #%d", player.ADPRank)
	}
	return "Projection " + fmt.Sprintf("%.1f", player.Projection)
}

func radarHref(path string, player Player) string {
	return path + "?q=" + url.QueryEscape(player.Name)
}

// teamTerminalRadar is phase-aware. Before the draft completes it is a
// shortlist of unpicked targets; afterward it is a true acquisition radar
// derived from currentRosters plus playerWaiverStatus, exactly like /players.
// No rostered player can leak into the post-draft radar.
func (s *Service) teamTerminalRadar(state PersistedState, phase TeamTerminalPhase, now time.Time, limit int) []map[string]any {
	if limit <= 0 {
		return []map[string]any{}
	}
	p := s.pool()
	out := make([]map[string]any, 0, limit)
	if phase != TeamTerminalRosterComplete {
		picked := make(map[string]bool, len(state.Picks))
		for _, pick := range state.Picks {
			picked[pick.PlayerID] = true
		}
		for _, player := range p.players {
			if picked[player.ID] {
				continue
			}
			out = append(out, map[string]any{
				"position":       player.Position,
				"name":           player.Name,
				"team":           player.NFLTeam,
				"signal":         radarSignal(player),
				"status":         "DRAFT TARGET",
				"has_resolution": false,
				"resolution":     "",
				"has_link":       true,
				"href":           radarHref("/draft", player),
				"link_label":     "Open draft room",
			})
			if len(out) >= limit {
				break
			}
		}
		return out
	}

	owner := rosterOwner(currentRosters(state))
	games := s.schedule()
	for _, player := range p.players {
		if owner[player.ID] != "" {
			continue
		}
		status := playerWaiverStatus(state, s.cfg, games, player.ID, player.NFLTeam, now)
		if status.State != AvailabilityFreeAgent && status.State != AvailabilityOnWaivers {
			continue
		}
		statusLabel := "FREE AGENT"
		hasResolution := false
		resolution := ""
		if status.State == AvailabilityOnWaivers {
			statusLabel = "ON WAIVERS"
			hasResolution = true
			// J3 F17: the same sentence /players' pool row and MY CLAIMS
			// card render (waiverResolutionPhrase) — Signal Watch used to
			// print a bare exact clock time straight off the kickoff-lock
			// estimate with no relative phrase, disagreeing with /players.
			resolution = waiverResolutionPhrase(s.cfg, player.NFLTeam, status, now)
		}
		out = append(out, map[string]any{
			"position":       player.Position,
			"name":           player.Name,
			"team":           player.NFLTeam,
			"signal":         radarSignal(player),
			"status":         statusLabel,
			"has_resolution": hasResolution,
			"resolution":     resolution,
			"has_link":       true,
			"href":           radarHref("/players", player),
			"link_label":     "Open player pool",
		})
		if len(out) >= limit {
			break
		}
	}
	return out
}

func teamTerminalRadarCopy(phase TeamTerminalPhase) map[string]any {
	if phase == TeamTerminalRosterComplete {
		return map[string]any{
			"radar_kicker":          "02 // WAIVER RADAR",
			"radar_title":           "Signal watch",
			"radar_description":     "Rostered players stay off this list. FREE AGENT and ON WAIVERS states come from the same availability rules as the Player Pool.",
			"radar_link_href":       "/players",
			"radar_link_label":      "Open player pool",
			"scouting_empty_title":  "NO ACQUISITION SIGNALS",
			"scouting_empty_detail": "Every player in the current pool is rostered or there is no available acquisition state to show.",
		}
	}
	return map[string]any{
		"radar_kicker":          "02 // DRAFT RADAR",
		"radar_title":           "Draft targets",
		"radar_description":     "Picked players leave this shortlist. Use the links to open the Draft Room and refine your Big Board before the next pick.",
		"radar_link_href":       "/draft",
		"radar_link_label":      "Open draft room",
		"scouting_empty_title":  "NO OPEN DRAFT TARGETS",
		"scouting_empty_detail": "The current player-pool snapshot has no unpicked targets to show.",
	}
}
