package league

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
)

type trophyEvaluation struct {
	Context *trophyContext
	Results map[string]trophyResult
	Awards  []TrophyAward
}

// TrophyCatalogItem combines league holders with the viewer's own state.
// A trophy can have league holders while still being locked for this viewer.
type TrophyCatalogItem struct {
	ID          string
	Title       string
	Description string
	Rule        string
	Scope       string
	Tier        string
	Special     bool
	Earned      bool
	Status      string
	Progress    string
	HasProgress bool
	Counter     string
	HasCounter  bool
	Holders     []TrophyRowView
	HasHolders  bool
}
type TrophyCatalogGroup struct {
	ID    string
	Title string
	Items []TrophyCatalogItem
}

func (s *Service) trophyCatalog(eval trophyEvaluation, email string, signedIn bool) []TrophyCatalogGroup {
	groups := []TrophyCatalogGroup{{ID: "pickem", Title: "Pick'em trophies"}, {ID: "streaks", Title: "Streaks"}, {ID: "week", Title: "Fantasy week achievements"}, {ID: "season", Title: "Season records"}, {ID: "manager", Title: "Manager achievements"}}
	member, _ := memberByEmail(eval.Context.state.Members, email)
	managers := s.trophyManagersFor(eval.Context.state)
	for _, def := range trophyRegistry {
		if !def.visible(eval.Context.roster) {
			continue
		}
		item := TrophyCatalogItem{ID: def.Kind, Title: def.Title, Description: def.Description, Rule: def.Rule, Scope: def.Scope, Tier: def.Tier.Name, Special: def.Special, Status: "Locked"}
		owner := email
		key := trophyManagerKey(email)
		if def.Team {
			owner = member.TeamID
			key = managers.teamKey[member.TeamID]
		}
		count := 0
		for _, award := range eval.Awards {
			if award.Kind != def.Kind {
				continue
			}
			item.Holders = append(item.Holders, trophyRowView(award, true))
			if signedIn && award.ManagerKey == key {
				count++
			}
		}
		item.HasHolders = len(item.Holders) > 0
		item.Earned = item.HasHolders
		if signedIn {
			item.Earned = count > 0
			if !item.Earned {
				item.Status = "Locked for you"
			}
			item.Progress = eval.Results[def.Kind].Progress[owner]
			if def.Team && owner == "" {
				item.Progress = "Claim a fantasy seat to earn this achievement"
			}
			if item.Progress == "" && def.Category == "streaks" && def.Tier.Threshold > 0 {
				label := "Winning streak"
				if strings.HasPrefix(def.Kind, "pickem_") {
					label = "Pick'em streak"
				}
				if def.Kind == "top_scorer_streak" {
					label = "Top-scorer streak"
				}
				item.Progress = streakProgress(label, 0, 0, def.Tier.Threshold)
			}
			item.HasProgress = item.Progress != ""
			if def.Special {
				item.Counter = fmt.Sprintf("Career ×%d", count)
				item.HasCounter = true
			}
		}
		if item.Earned {
			item.Status = "Earned"
			if def.Season && !eval.Context.stateIsComplete() {
				item.Status = "Leader so far"
			}
		}
		for i := range groups {
			if groups[i].ID == def.Category {
				groups[i].Items = append(groups[i].Items, item)
				break
			}
		}
	}
	return groups
}
func (c *trophyContext) stateIsComplete() bool { return c.state.Phase == PhaseSeasonComplete }

// ManagerTrophyStrip is the signed-in manager's compact case. Co-managers
// share fantasy achievements and retain their own independent Pick'em wins.
func (s *Service) ManagerTrophyStrip(r *http.Request) map[string]any {
	viewer := s.Viewer(r)
	signedIn, _ := viewer["signed_in"].(bool)
	email, _ := viewer["email"].(string)
	state := s.store.Snapshot()
	var rows []TrophyRowView
	var owned []TrophyAward
	if signedIn {
		member, _ := memberByEmail(state.Members, email)
		teamKey := s.trophyManagersFor(state).teamKey[member.TeamID]
		for _, award := range s.allTrophies(state, s.clock()) {
			def := trophyDefFor(award.Kind)
			if (def.Team && award.ManagerKey == teamKey) || (!def.Team && award.ManagerKey == trophyManagerKey(email)) {
				owned = append(owned, award)
			}
		}
	}
	total := len(owned)
	sort.SliceStable(owned, func(i, j int) bool {
		if owned[i].Season != owned[j].Season {
			return !owned[i].Season
		}
		return owned[i].Week > owned[j].Week
	})
	for _, award := range owned[:min(5, len(owned))] {
		rows = append(rows, trophyRowView(award, false))
	}
	return map[string]any{"shown": signedIn, "rows": rows, "has_rows": len(rows) > 0, "total": total, "href": TrophyManagerHref(email)}
}

// LeagueTrophyHighlights shows the latest weekly achievements and every
// special achievement of that week separately, so the callout is never lost
// behind the ordinary highlights' size limit.
func (s *Service) LeagueTrophyHighlights(r *http.Request) map[string]any {
	return s.leagueTrophyHighlights(s.store.Snapshot())
}
func (s *Service) leagueTrophyHighlights(state PersistedState) map[string]any {
	awards := s.allTrophies(state, s.clock())
	latest := 0
	for _, award := range awards {
		if !award.Season {
			latest = max(latest, award.Week)
		}
	}
	rows := []TrophyRowView{}
	// The special callout shows each manager's best special award of the
	// week (a Trifecta over a Gold over a plain award), at most
	// specialHighlightLimit of them, so one strong week cannot turn Home
	// into a wall of near-duplicate cards. The rest stay one tap away.
	bestSpecial := map[string]TrophyAward{}
	var specialOrder []string
	specialTotal := 0
	for i := len(awards) - 1; i >= 0; i-- {
		award := awards[i]
		if award.Season || award.Week != latest {
			continue
		}
		if award.Special {
			specialTotal++
			key := award.ManagerKey
			if key == "" {
				key = award.Manager
			}
			current, seen := bestSpecial[key]
			if !seen {
				specialOrder = append(specialOrder, key)
			}
			if !seen || specialHighlightRank(award.Kind) < specialHighlightRank(current.Kind) {
				bestSpecial[key] = award
			}
		} else if len(rows) < 4 {
			rows = append(rows, trophyRowView(award, true))
		}
	}
	special := []TrophyRowView{}
	for _, key := range specialOrder {
		if len(special) == specialHighlightLimit {
			break
		}
		special = append(special, trophyRowView(bestSpecial[key], true))
	}
	more := specialTotal - len(special)
	return map[string]any{"rows": rows, "special_rows": special, "has_rows": len(rows) > 0 || len(special) > 0, "has_special": len(special) > 0,
		"special_more": more, "has_special_more": more > 0, "week": latest}
}

// specialHighlightLimit caps the Home special-teams callout.
const specialHighlightLimit = 4

// specialHighlightRank orders one manager's special awards for the Home
// callout: lower is shown first.
func specialHighlightRank(kind string) int {
	switch {
	case kind == "special_teams_trifecta":
		return 0
	case strings.HasSuffix(kind, "_gold"):
		return 1
	default:
		return 2
	}
}
