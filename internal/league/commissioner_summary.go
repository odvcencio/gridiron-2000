package league

import (
	"fmt"

	"gridiron-2000/internal/commissionerhq"
)

// CommissionerSummary builds the PII-free, snapshot-only federation view.
// It intentionally does not reuse AdminData, whose seat and invite rows carry
// manager identities that do not belong in a cross-instance protocol.
func (s *Service) CommissionerSummary(instanceID string, runtime commissionerhq.Runtime, pool commissionerhq.Pool) commissionerhq.Summary {
	state := s.store.Snapshot()
	teams := s.Teams()
	active := make(map[string]bool, len(teams))
	for _, team := range teams {
		active[team.ID] = true
	}
	claimed := make(map[string]bool, len(teams))
	for _, member := range state.Members {
		if active[member.TeamID] {
			claimed[member.TeamID] = true
		}
	}
	ready := 0
	for teamID := range active {
		if state.Ready[teamID] {
			ready++
		}
	}

	rosterCapacity := len(teams) * CurrentDraftRounds()
	pool.RosterCapacity = rosterCapacity
	pool.Cushion = max(0, pool.Players-rosterCapacity)
	if rosterCapacity > 0 {
		pool.Coverage = float64(pool.Target) / float64(rosterCapacity)
	}

	draftStatus := "scheduled"
	if state.DraftStarted {
		draftStatus = "live"
		if len(state.Picks) >= rosterCapacity {
			draftStatus = "complete"
		}
	}
	attention := make([]commissionerhq.Attention, 0, 4)
	if runtime.Ready == false {
		attention = append(attention, commissionerhq.Attention{Code: "persistence_unavailable", Severity: "critical", Message: "League persistence needs operator attention.", Href: "/admin"})
	}
	if pool.Mode != "live" && pool.Mode != "cache" {
		attention = append(attention, commissionerhq.Attention{Code: "pool_unavailable", Severity: "critical", Message: "The live player source is unavailable.", Href: "/admin#data"})
	} else if pool.Error != "" {
		attention = append(attention, commissionerhq.Attention{Code: "pool_degraded", Severity: "warning", Message: "The player pool is usable, but some enrichment data did not refresh.", Href: "/admin#data"})
	}
	if pool.Players < rosterCapacity {
		attention = append(attention, commissionerhq.Attention{Code: "pool_shortfall", Severity: "critical", Count: rosterCapacity - pool.Players, Message: fmt.Sprintf("Player pool is %d short of the %d draft slots.", rosterCapacity-pool.Players, rosterCapacity), Href: "/admin#data"})
	}
	if unclaimed := len(teams) - len(claimed); unclaimed > 0 {
		attention = append(attention, commissionerhq.Attention{Code: "unclaimed_seats", Severity: "warning", Count: unclaimed, Message: fmt.Sprintf("%d seats remain unclaimed.", unclaimed), Href: "/admin#seats"})
	}
	if notReady := len(teams) - ready; notReady > 0 && !state.DraftStarted {
		attention = append(attention, commissionerhq.Attention{Code: "managers_not_ready", Severity: "warning", Count: notReady, Message: fmt.Sprintf("%d seats are not marked ready.", notReady), Href: "/draft"})
	}
	if len(state.DraftOrder) == 0 && !state.DraftStarted {
		attention = append(attention, commissionerhq.Attention{Code: "draft_order_unset", Severity: "warning", Message: "Draft order is still using the configured default.", Href: "/admin#draft-order"})
	}
	if state.DraftStarted && state.ClockPaused {
		attention = append(attention, commissionerhq.Attention{Code: "draft_clock_paused", Severity: "warning", Message: "The live draft clock is paused.", Href: "/draft"})
	}

	return commissionerhq.Summary{
		SchemaVersion: commissionerhq.SchemaVersion,
		GeneratedAt:   s.clock().UTC(),
		Instance: commissionerhq.Instance{
			ID: instanceID, Name: s.cfg.Name, ShortCode: s.cfg.ShortCode,
			PublicURL: s.cfg.URL, Mode: s.cfg.ModeLabel, Season: s.cfg.Season,
		},
		Runtime: runtime,
		Membership: commissionerhq.Membership{
			Seats: len(teams), ClaimedSeats: len(claimed), ReadySeats: ready, Members: len(state.Members),
		},
		Draft: commissionerhq.Draft{
			ScheduledAt: s.cfg.DraftAt, Status: draftStatus, Started: state.DraftStarted,
			StartedAt: state.DraftStartedAt, Rounds: CurrentDraftRounds(), Picks: len(state.Picks),
			OrderSet: len(state.DraftOrder) > 0, ClockArmed: !state.ClockDeadline.IsZero(),
			ClockPaused: state.ClockPaused, Deadline: state.ClockDeadline,
		},
		Pool: pool, Attention: attention,
	}
}
