package league

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// The league trophy case: achievements derived from data the league
// already keeps (closed fantasy weeks and settled Pick'em weeks). Nothing is
// stored, so a trophy can never disagree with the scores or picks that earned
// it. Two views read the same list: by week and by manager.
const (
	trophyHighScore   = "high_score"
	trophyNailbiter   = "nailbiter"
	trophyBlowout     = "blowout"
	trophyToiletBowl  = "toilet_bowl"
	trophyPickemWin   = "pickem"
	trophyPerfectWeek = "perfect_week"
	trophyUpsetHunter = "upset_hunter"
	trophyContrarian  = "contrarian"
	trophyPointsLead  = "points_leader"
	trophyHotStreak   = "hot_streak"
	trophyBestPicker  = "best_picker"
	trophyBestWeek    = "best_week"
)

// hotStreakMin is the shortest weekly winning run that earns Hot Streak.
const hotStreakMin = 2

func trophyDefFor(kind string) TrophyDef {
	for _, def := range trophyRegistry {
		if def.Kind == kind {
			return def
		}
	}
	return TrophyDef{Kind: kind, Title: kind}
}

func trophyOrder(kind string) int {
	for i, def := range trophyRegistry {
		if def.Kind == kind {
			return i
		}
	}
	return len(trophyRegistry)
}

// TrophyAward is one manager's win of one trophy. Week is 0 for a season
// trophy. Season trophies show the current leader until the season ends.
type TrophyAward struct {
	Kind       string
	Title      string
	Week       int
	Season     bool
	ManagerKey string
	Manager    string
	Value      string
	Rule       string
	Special    bool
	Tier       string
	EarnedWeek int
	Complete   bool
}

// trophyManagerKey is the URL-safe manager identity. It is a short hash of
// the persisted key, so a deep link never carries an email address. A team
// with no seated manager falls back to "t-<team id>".
func trophyManagerKey(email string) string {
	sum := sha256.Sum256([]byte(normalizeEmail(email)))
	return "m-" + hex.EncodeToString(sum[:5])
}

// TrophyManagerHref is the deep link to one manager's trophy case.
func TrophyManagerHref(email string) string {
	return "/trophies?manager=" + url.QueryEscape(trophyManagerKey(email))
}

type trophyManagers struct {
	names   map[string]string // key -> display name
	teamKey map[string]string // team id -> key
}

func (s *Service) trophyManagersFor(state PersistedState) trophyManagers {
	m := trophyManagers{names: map[string]string{}, teamKey: map[string]string{}}
	emails := make([]string, 0, len(state.Members))
	for email := range state.Members {
		emails = append(emails, email)
	}
	sort.Strings(emails)
	for _, email := range emails {
		member := state.Members[email]
		key := trophyManagerKey(email)
		m.names[key] = s.pickemDisplayName(state, email)
		if member.TeamID != "" && member.Role == "" {
			m.teamKey[member.TeamID] = key
		}
	}
	for owner := range state.Pickems {
		key := trophyManagerKey(owner)
		if _, ok := m.names[key]; !ok {
			m.names[key] = s.pickemDisplayName(state, owner)
		}
	}
	for _, team := range s.Teams() {
		if _, ok := m.teamKey[team.ID]; ok {
			continue
		}
		key := "t-" + team.ID
		name := strings.TrimSpace(team.Manager)
		if name == "" {
			name = team.Name
		}
		m.names[key] = name
		m.teamKey[team.ID] = key
	}
	return m
}

func (m trophyManagers) award(w trophyWin, team bool) TrophyAward {
	def := trophyDefFor(w.Kind)
	key := w.Owner
	if team {
		key = m.teamKey[w.Owner]
		if key == "" {
			key = "t-" + w.Owner
		}
	} else {
		key = trophyManagerKey(w.Owner)
	}
	name := m.names[key]
	if name == "" {
		name = "Unknown manager"
	}
	return TrophyAward{Kind: def.Kind, Title: def.Title, Week: w.Week, Season: def.Season, ManagerKey: key, Manager: name, Value: w.Value, Rule: def.Rule, Special: def.Special, Tier: def.Tier.Name, EarnedWeek: w.EarnedWeek}
}

// fantasyTrophyWins derives the fantasy weekly and season trophies from
// closed weeks. Owners are team IDs. The three honors that already exist as
// weekly awards (High Score, Pick'em Winner, Nailbiter) come from
// awardsForWeek so the two surfaces cannot disagree.
func (s *Service) fantasyTrophyWins(state PersistedState) (team []trophyWin, pickem []trophyWin) {
	if state.Schedule == nil {
		return nil, nil
	}
	points := map[string]float64{}
	results := map[string][]int{} // team -> +1 win, -1 loss/tie in week order
	weeks := append([]ScheduleWeek(nil), state.Schedule.Weeks...)
	sort.Slice(weeks, func(i, j int) bool { return weeks[i].Week < weeks[j].Week })
	closed := false
	for _, week := range weeks {
		if !scheduleWeekIsFinal(week) || len(week.Matchups) == 0 {
			continue
		}
		closed = true
		prefix := fmt.Sprintf("Week %d · ", week.Week)
		for _, award := range s.awardsForWeek(state, week) {
			value := strings.TrimPrefix(award.Detail, prefix)
			if award.Kind == weeklyAwardPickem {
				pickem = append(pickem, trophyWin{Kind: trophyPickemWin, Week: week.Week, Owner: award.Email, Value: value})
				continue
			}
			if !trophyTeamEligible(state, award.TeamID, week.Week) {
				continue
			}
			team = append(team, trophyWin{Kind: award.Kind, Week: week.Week, Owner: award.TeamID, Value: value})
		}
		blowout := 0.0
		low := math.Inf(1)
		for _, m := range week.Matchups {
			blowout = math.Max(blowout, math.Abs(m.HomeScore-m.AwayScore))
			low = math.Min(low, math.Min(m.HomeScore, m.AwayScore))
			if trophyTeamEligible(state, m.HomeTeamID, week.Week) {
				points[m.HomeTeamID] += m.HomeScore
			}
			if trophyTeamEligible(state, m.AwayTeamID, week.Week) {
				points[m.AwayTeamID] += m.AwayScore
			}
			switch {
			case m.HomeScore > m.AwayScore:
				results[m.HomeTeamID] = append(results[m.HomeTeamID], 1)
				results[m.AwayTeamID] = append(results[m.AwayTeamID], -1)
			case m.AwayScore > m.HomeScore:
				results[m.AwayTeamID] = append(results[m.AwayTeamID], 1)
				results[m.HomeTeamID] = append(results[m.HomeTeamID], -1)
			default:
				results[m.HomeTeamID] = append(results[m.HomeTeamID], -1)
				results[m.AwayTeamID] = append(results[m.AwayTeamID], -1)
			}
		}
		for id := range results {
			if !trophyTeamEligible(state, id, week.Week) {
				results[id] = nil
			}
		}
		seenLow := map[string]bool{}
		for _, m := range week.Matchups {
			if blowout > 0 && sameAwardScore(math.Abs(m.HomeScore-m.AwayScore), blowout) {
				winner := m.HomeTeamID
				if m.AwayScore > m.HomeScore {
					winner = m.AwayTeamID
				}
				if trophyTeamEligible(state, winner, week.Week) {
					team = append(team, trophyWin{Kind: trophyBlowout, Week: week.Week, Owner: winner, Value: fmt.Sprintf("won by %.1f", blowout)})
				}
			}
			for _, side := range []struct {
				id    string
				score float64
			}{{m.HomeTeamID, m.HomeScore}, {m.AwayTeamID, m.AwayScore}} {
				if trophyTeamEligible(state, side.id, week.Week) && sameAwardScore(side.score, low) && !seenLow[side.id] {
					seenLow[side.id] = true
					team = append(team, trophyWin{Kind: trophyToiletBowl, Week: week.Week, Owner: side.id, Value: fmt.Sprintf("%.1f points", low)})
				}
			}
		}
	}
	if !closed {
		return team, pickem
	}
	best := math.Inf(-1)
	for _, p := range points {
		best = math.Max(best, p)
	}
	ids := make([]string, 0, len(points))
	for id := range points {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if sameAwardScore(points[id], best) {
			team = append(team, trophyWin{Kind: trophyPointsLead, Owner: id, Value: fmt.Sprintf("%.1f points", best)})
		}
	}
	longest := 0
	runs := map[string]int{}
	for _, id := range ids {
		run, top := 0, 0
		for _, r := range results[id] {
			if r > 0 {
				run++
				if run > top {
					top = run
				}
			} else {
				run = 0
			}
		}
		runs[id] = top
		if top > longest {
			longest = top
		}
	}
	if longest >= hotStreakMin {
		for _, id := range ids {
			if runs[id] == longest {
				team = append(team, trophyWin{Kind: trophyHotStreak, Owner: id, Value: fmt.Sprintf("%d wins in a row", longest)})
			}
		}
	}
	return team, pickem
}

// allTrophies is the whole case, oldest week first, season trophies last.
func (s *Service) allTrophies(state PersistedState, now time.Time) []TrophyAward {
	return s.evaluateTrophies(state, now).Awards
}

func (s *Service) evaluateTrophies(state PersistedState, now time.Time) trophyEvaluation {
	managers := s.trophyManagersFor(state)
	ctx := s.trophyContext(state, now)
	eval := trophyEvaluation{Context: ctx, Results: map[string]trophyResult{}}
	var out []TrophyAward
	for _, def := range trophyRegistry {
		if !def.visible(ctx.roster) {
			continue
		}
		result := def.Evaluate(ctx)
		sortTrophyWins(result.Wins)
		eval.Results[def.Kind] = result
		for _, w := range result.Wins {
			award := managers.award(w, def.Team)
			if def.Season && award.EarnedWeek == 0 {
				if def.Team && len(ctx.closed) > 0 {
					award.EarnedWeek = ctx.closed[len(ctx.closed)-1].Week
				}
				if !def.Team && len(ctx.settled) > 0 {
					award.EarnedWeek = ctx.settled[len(ctx.settled)-1]
				}
			}
			award.Complete = state.Phase == PhaseSeasonComplete
			out = append(out, award)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Season != b.Season {
			return !a.Season
		}
		if a.Week != b.Week {
			return a.Week < b.Week
		}
		if a.Kind != b.Kind {
			return trophyOrder(a.Kind) < trophyOrder(b.Kind)
		}
		if a.Manager != b.Manager {
			return a.Manager < b.Manager
		}
		return a.ManagerKey < b.ManagerKey
	})
	eval.Awards = out
	return eval
}

// TrophyRowView is one row as the trophies page renders it.
type TrophyRowView struct {
	Title       string
	Meta        string
	Manager     string
	ManagerHref string
	HasManager  bool
	Value       string
	Rule        string
	Special     bool
	Tier        string
}

// TrophyCountView is one "Title x N" line of a manager's tally.
type TrophyCountView struct {
	Title string
	Count int
}

func trophyRowView(a TrophyAward, showManager bool) TrophyRowView {
	meta := "Season"
	if !a.Season {
		meta = "Week " + strconv.Itoa(a.Week)
	} else {
		meta = "Season · leader so far"
	}
	if a.Season && a.Complete {
		meta = "Season · earned"
	}
	if a.Season && a.EarnedWeek > 0 {
		meta += " · Week " + strconv.Itoa(a.EarnedWeek)
	}
	row := TrophyRowView{Title: a.Title, Meta: meta, Value: a.Value, Rule: a.Rule, Special: a.Special, Tier: a.Tier}
	if showManager {
		row.Manager = a.Manager
		row.HasManager = true
		row.ManagerHref = "/trophies?manager=" + url.QueryEscape(a.ManagerKey)
	}
	return row
}

// TrophyManagerOption is one manager link in the by-manager picker.
type TrophyManagerOption struct {
	Name     string
	Href     string
	Count    int
	Selected bool
}

// TrophyCaseData builds the /trophies page from one snapshot. view is
// "manager" when a manager query is present, else "week". A week query names
// the week; without one the view defaults to the latest week holding an
// award.
func (s *Service) TrophyCaseData(r *http.Request) map[string]any {
	state := s.store.Snapshot()
	now := s.clock()
	eval := s.evaluateTrophies(state, now)
	data := s.trophyCaseData(state, r.URL.Query().Get("week"), r.URL.Query().Get("manager"), now, eval)
	viewer := s.Viewer(r)
	email, _ := viewer["email"].(string)
	signedIn, _ := viewer["signed_in"].(bool)
	// viewer and league feed the app shell (app/layout.gsx): without them
	// the layout read every visitor as signed out, so /trophies dropped the
	// navigation rail and showed a "Sign in" header to signed-in managers.
	data["viewer"] = viewer
	data["league"] = s.leagueMapForViewer(r)
	data["catalog_groups"] = s.trophyCatalog(eval, email, signedIn)
	data["catalog_has_viewer"] = signedIn
	data["catalog_href"] = "/trophies?view=catalog"
	data["is_catalog"] = r.URL.Query().Get("view") == "catalog" && r.URL.Query().Get("manager") == ""
	if data["is_catalog"] == true {
		data["is_week"] = false
		data["view"] = "catalog"
	}
	return data
}

func (s *Service) trophyCaseData(state PersistedState, rawWeek, rawManager string, now time.Time, evaluations ...trophyEvaluation) map[string]any {
	var all []TrophyAward
	if len(evaluations) > 0 {
		all = evaluations[0].Awards
	} else {
		all = s.allTrophies(state, now)
	}
	managers := s.trophyManagersFor(state)

	var weekly, season []TrophyAward
	weekSet := map[int]bool{}
	for _, a := range all {
		if a.Season {
			season = append(season, a)
			continue
		}
		weekly = append(weekly, a)
		weekSet[a.Week] = true
	}
	weeks := make([]int, 0, len(weekSet))
	for w := range weekSet {
		weeks = append(weeks, w)
	}
	sort.Ints(weeks)

	view := "week"
	managerKey := strings.TrimSpace(rawManager)
	if managerKey != "" {
		view = "manager"
	}

	selectedWeek := 0
	if n, err := strconv.Atoi(strings.TrimSpace(rawWeek)); err == nil && n >= 1 {
		selectedWeek = n
	} else if len(weeks) > 0 {
		selectedWeek = weeks[len(weeks)-1]
	}

	data := map[string]any{
		"view":       view,
		"is_week":    view == "week",
		"is_manager": view == "manager",
		"has_awards": len(all) > 0,
	}

	// Season trophies show on both views: the current leaders.
	seasonRows := make([]TrophyRowView, 0, len(season))
	for _, a := range season {
		seasonRows = append(seasonRows, trophyRowView(a, true))
	}
	data["season_rows"] = seasonRows
	data["has_season_rows"] = len(seasonRows) > 0

	// By week.
	weekRows := make([]TrophyRowView, 0)
	for _, a := range weekly {
		if a.Week == selectedWeek {
			weekRows = append(weekRows, trophyRowView(a, true))
		}
	}
	data["week"] = selectedWeek
	data["week_rows"] = weekRows
	data["has_week_rows"] = len(weekRows) > 0
	prev, next := 0, 0
	for _, w := range weeks {
		if w < selectedWeek {
			prev = w
		}
		if w > selectedWeek && next == 0 {
			next = w
		}
	}
	data["has_prev_week"] = prev > 0
	data["prev_week_href"] = "/trophies?week=" + strconv.Itoa(prev)
	data["has_next_week"] = next > 0
	data["next_week_href"] = "/trophies?week=" + strconv.Itoa(next)
	options := make([]map[string]any, 0, len(weeks))
	for _, w := range weeks {
		options = append(options, map[string]any{"value": strconv.Itoa(w), "label": "Week " + strconv.Itoa(w), "selected": w == selectedWeek})
	}
	if len(weeks) == 0 || !weekSet[selectedWeek] {
		// A requested week with no award still needs a selectable entry.
		if selectedWeek > 0 {
			options = append(options, map[string]any{"value": strconv.Itoa(selectedWeek), "label": "Week " + strconv.Itoa(selectedWeek), "selected": true})
		}
	}
	data["week_options"] = options
	data["has_weeks"] = len(options) > 0
	data["by_week_href"] = "/trophies"
	data["by_manager_href"] = "/trophies?manager="

	// By manager.
	counts := map[string]int{}
	for _, a := range all {
		counts[a.ManagerKey]++
	}
	keys := make([]string, 0, len(managers.names))
	for key := range managers.names {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		ni, nj := managers.names[keys[i]], managers.names[keys[j]]
		if ni != nj {
			return ni < nj
		}
		return keys[i] < keys[j]
	})
	opts := make([]TrophyManagerOption, 0, len(keys))
	for _, key := range keys {
		opts = append(opts, TrophyManagerOption{Name: managers.names[key], Href: "/trophies?manager=" + url.QueryEscape(key), Count: counts[key], Selected: key == managerKey})
	}
	data["manager_options"] = opts
	data["has_manager_options"] = len(opts) > 0

	_, known := managers.names[managerKey]
	managerRows := make([]TrophyRowView, 0)
	tally := map[string]int{}
	for _, a := range all {
		if a.ManagerKey == managerKey {
			managerRows = append(managerRows, trophyRowView(a, false))
			tally[a.Kind]++
		}
	}
	countRows := make([]TrophyCountView, 0, len(tally))
	for _, def := range trophyRegistry {
		if tally[def.Kind] > 0 {
			countRows = append(countRows, TrophyCountView{Title: def.Title, Count: tally[def.Kind]})
		}
	}
	data["manager_known"] = known
	data["manager_unknown"] = managerKey != "" && !known
	data["manager_name"] = managers.names[managerKey]
	data["manager_rows"] = managerRows
	data["has_manager_rows"] = len(managerRows) > 0
	data["manager_counts"] = countRows
	data["manager_total"] = len(managerRows)

	defs := make([]TrophyRowView, 0, len(trophyRegistry))
	for _, def := range trophyRegistry {
		if !def.visible(CurrentRoster()) {
			continue
		}
		scope := "Weekly"
		if def.Season {
			scope = "Season"
		}
		defs = append(defs, TrophyRowView{Title: def.Title, Meta: scope, Rule: def.Rule})
	}
	data["trophy_defs"] = defs
	return data
}
