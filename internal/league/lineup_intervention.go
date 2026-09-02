package league

import (
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// lineupViewTarget is the narrow cross-franchise projection used by the Team
// terminal. A commissioner may inspect and edit a claimed franchise's
// lineup, but the page must remain in lineup-only mode: no target identity,
// badge, ownership, or other franchise-management controls is implied.
type lineupViewTarget struct {
	TeamID       string
	Intervention bool
}

// claimedLineupTeam resolves a target against the service's active topology
// and a single state snapshot. Unknown, stale, and unclaimed IDs are never
// valid commissioner lineup targets. Demo rehearsal treats every active
// configured team as a synthetic claimed seat so the local commissioner
// rehearsal can exercise the same lineup-only surface.
func (s *Service) claimedLineupTeam(state PersistedState, requested string) (Team, bool) {
	requested = strings.TrimSpace(requested)
	if requested == "" {
		return Team{}, false
	}
	for _, candidate := range s.Teams() {
		if candidate.ID != requested {
			continue
		}
		team := s.teamView(state, requested)
		if s.demoMode || strings.TrimSpace(team.Manager) != "" {
			return team, true
		}
		return Team{}, false
	}
	return Team{}, false
}

// lineupViewTargetForRequest selects the Team terminal's target. Ordinary
// managers are always scoped to their own seat, regardless of ?team=. A
// commissioner may select only a claimed active franchise, and a rejected
// target falls back to the commissioner's own seat without leaking the
// invalid value into the view model.
func (s *Service) lineupViewTargetForRequest(r *http.Request, state PersistedState, ownTeamID string) lineupViewTarget {
	requested := ""
	if r != nil && r.URL != nil {
		requested = strings.TrimSpace(r.URL.Query().Get("team"))
	}
	if !s.IsCommissioner(r) || requested == "" {
		return lineupViewTarget{TeamID: ownTeamID}
	}
	if _, ok := s.claimedLineupTeam(state, requested); !ok {
		return lineupViewTarget{TeamID: ownTeamID}
	}
	if requested == ownTeamID {
		return lineupViewTarget{TeamID: ownTeamID}
	}
	return lineupViewTarget{TeamID: requested, Intervention: true}
}

// lineupInterventionAudit reports whether a SetLineup/LineupAuto call
// against teamID is a genuine commissioner intervention (dead-manager
// insurance, lineupActingTeam's own doc comment) rather than an ordinary
// manager — who may also hold the commissioner role — setting their own
// lineup through the same shared call. Only a true intervention (the
// commissioner acting on a claimed seat that is not their own) is a
// commissioner-gated mutation worth a commissioner-console audit row; an
// ordinary self-lineup edit must not become one merely because the actor
// also happens to be the commissioner. own's error (no claimed seat of
// the commissioner's own) counts as "not their own team" too.
func (s *Service) lineupInterventionAudit(r *http.Request, teamID string) bool {
	if teamID == "" || !s.IsCommissioner(r) {
		return false
	}
	own, err := s.actingTeam(r, "")
	return err != nil || own != teamID
}

// recordLineupInterventionEvent leaves a commissioner-console audit row
// for a true SetLineup/LineupAuto intervention (lineupInterventionAudit
// above), after the underlying store write has already succeeded. A
// RecordCommissionerEvent failure is deliberately log-only here, matching
// every other commissioner action's call site (RecordCommissionerEvent's
// own doc comment, commissioner_event.go): the lineup write this audits
// has already committed by the time this runs.
func (s *Service) recordLineupInterventionEvent(r *http.Request, teamID string, week int, playerID, kind, summary string) {
	if !s.lineupInterventionAudit(r, teamID) {
		return
	}
	if _, err := s.RecordCommissionerEvent(r, kind, summary, CommissionerEventRefs{TeamID: teamID, PlayerID: playerID, Week: week}); err != nil {
		log.Printf("commissioner event: %s: %v", kind, err)
	}
}

// LineupTargetAllowed is the page/action return seam for one validated
// commissioner target. It intentionally exposes only a boolean: callers do
// not need a target's manager, membership, or ownership details to build a
// safe same-page return URL.
func (s *Service) LineupTargetAllowed(r *http.Request, requested string) bool {
	if !s.IsCommissioner(r) {
		return false
	}
	_, ok := s.claimedLineupTeam(s.store.Snapshot(), requested)
	return ok
}

// lineupTargetOptions exposes only claimed franchise labels and safe same-page
// links. It never includes manager emails, co-manager data, or any identity
// control; the Team terminal consumes it as a commissioner-only selector.
func (s *Service) lineupTargetOptions(state PersistedState, selected string, week int) []map[string]any {
	out := make([]map[string]any, 0)
	for _, candidate := range s.Teams() {
		team, ok := s.claimedLineupTeam(state, candidate.ID)
		if !ok {
			continue
		}
		out = append(out, map[string]any{
			"id":       team.ID,
			"label":    team.Name,
			"selected": team.ID == selected,
			"href":     "/team?team=" + url.QueryEscape(team.ID) + "&week=" + strconv.Itoa(week) + "#lineup",
		})
	}
	return out
}
