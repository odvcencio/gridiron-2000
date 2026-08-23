package league

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ScheduleSource supplies the real NFL schedule and scores. main.go adapts
// the nflverse mirror; a nil source leaves pick'em empty.
type ScheduleSource func() []GameInfo

// SetScheduleSource attaches the schedule feed. Call it once during startup,
// before the server accepts requests.
func (s *Service) SetScheduleSource(source ScheduleSource) {
	s.poolMu.Lock()
	s.scheduleFn = source
	s.poolMu.Unlock()
}

// schedule returns the current game list, or nil when no source is attached.
func (s *Service) schedule() []GameInfo {
	s.poolMu.Lock()
	source := s.scheduleFn
	s.poolMu.Unlock()
	if source == nil {
		return nil
	}
	return source()
}

// pickemWeek picks the week to show: the smallest week that still has a game
// kicking off within the last four hours or later, or the largest week when
// every game has already passed that window. An empty schedule returns 1.
func (s *Service) pickemWeek(games []GameInfo, now time.Time) int {
	return pickemWeekAt(games, now)
}

// pickemWeekAt is pickemWeek's pure core, extracted so Store methods that
// need "the current league week" (roster-ops spec's claim-commit Week,
// section 7.1) can resolve it without a Service reference — the same
// week resolver every caller shares, never a second one (spec section
// 4.4: "reuse it; do not write a second week resolver").
func pickemWeekAt(games []GameInfo, now time.Time) int {
	if len(games) == 0 {
		return 1
	}
	cutoff := now.Add(-4 * time.Hour)
	largestWeek := games[0].Week
	upcomingWeek := 0
	haveUpcoming := false
	for _, game := range games {
		if game.Week > largestWeek {
			largestWeek = game.Week
		}
		if game.Kickoff.After(cutoff) && (!haveUpcoming || game.Week < upcomingWeek) {
			upcomingWeek = game.Week
			haveUpcoming = true
		}
	}
	if haveUpcoming {
		return upcomingWeek
	}
	return largestWeek
}

type PickemOutcome string

const (
	pickemPending    PickemOutcome = "pending"
	pickemWin        PickemOutcome = "win"
	pickemLoss       PickemOutcome = "loss"
	pickemPush       PickemOutcome = "push"
	pickemMissedLoss PickemOutcome = "missed_loss"
	pickemVoid       PickemOutcome = "void"
)

type pickemGrade struct {
	Outcome PickemOutcome
	Cover   string
}

type PickemATSRecord struct {
	Wins         int
	Losses       int
	Pushes       int
	Participated bool
}

func validPick(game GameInfo, pick string) bool {
	return pick == game.Away || pick == game.Home
}

func pickemParticipation(games []GameInfo, picks map[string]string) map[int]bool {
	participated := make(map[int]bool)
	for _, game := range games {
		if validPick(game, picks[game.ID]) {
			participated[game.Week] = true
		}
	}
	return participated
}

// gradePickem is the one ATS authority used by cards, records, streaks, and
// leaderboards. nflverse's positive line means the home team is favored, so
// subtracting it from the home margin yields the covering side. A game never
// grades from a moving candidate or placeholder scores.
func gradePickem(game GameInfo, market PickemMarket, pick string, participated bool, now time.Time) pickemGrade {
	if game.Kickoff.IsZero() || now.Before(game.Kickoff) {
		return pickemGrade{Outcome: pickemPending}
	}
	if market.Void || !market.Frozen || !market.LinePresent {
		return pickemGrade{Outcome: pickemVoid}
	}
	if !validPick(game, pick) {
		if participated {
			return pickemGrade{Outcome: pickemMissedLoss}
		}
		return pickemGrade{Outcome: pickemPending}
	}
	if !game.Final || !game.ScoresPresent {
		return pickemGrade{Outcome: pickemPending}
	}
	adjustedHomeMarginTenths := (game.HomeScore-game.AwayScore)*10 - market.LineTenths
	if adjustedHomeMarginTenths == 0 {
		return pickemGrade{Outcome: pickemPush}
	}
	cover := game.Away
	if adjustedHomeMarginTenths > 0 {
		cover = game.Home
	}
	if pick == cover {
		return pickemGrade{Outcome: pickemWin, Cover: cover}
	}
	return pickemGrade{Outcome: pickemLoss, Cover: cover}
}

// gamesInWeek filters games to one week, in schedule order (no sort
// guarantee — callers that need kickoff order call sortGamesByKickoff).
func gamesInWeek(games []GameInfo, week int) []GameInfo {
	out := make([]GameInfo, 0, len(games))
	for _, game := range games {
		if game.Week == week {
			out = append(out, game)
		}
	}
	return out
}

// sortGamesByKickoff orders games by kickoff, earliest first, breaking a
// tie by ID for a stable render order.
func sortGamesByKickoff(games []GameInfo) {
	sort.Slice(games, func(i, j int) bool {
		if games[i].Kickoff.Equal(games[j].Kickoff) {
			return games[i].ID < games[j].ID
		}
		return games[i].Kickoff.Before(games[j].Kickoff)
	})
}

// pickemWeeks lists every distinct week present in games, ascending — the
// real span the HQ page's week navigation may move across (build item 2:
// "week navigation across the real schedule").
func pickemWeeks(games []GameInfo) []int {
	seen := make(map[int]bool, 8)
	weeks := make([]int, 0, 8)
	for _, game := range games {
		if !seen[game.Week] {
			seen[game.Week] = true
			weeks = append(weeks, game.Week)
		}
	}
	sort.Ints(weeks)
	return weeks
}

func tallyPicks(games []GameInfo, markets map[string]PickemMarket, picks map[string]string, now time.Time) PickemATSRecord {
	participated := pickemParticipation(games, picks)
	record := PickemATSRecord{Participated: len(participated) > 0}
	for _, game := range games {
		switch gradePickem(game, markets[game.ID], picks[game.ID], participated[game.Week], now).Outcome {
		case pickemWin:
			record.Wins++
		case pickemLoss, pickemMissedLoss:
			record.Losses++
		case pickemPush:
			record.Pushes++
		}
	}
	return record
}

// pickemStreak computes the viewer's current streak of consecutive ATS wins,
// walking from the most recent kickoff backward. Losses and missed losses
// break the streak; pushes, voids, and pending games are neutral. The streak
// resets to 0 when there are no graded wins yet. Ties
// on kickoff (an early or late slate's several simultaneous games) break
// by ID, descending, the same stable tie-break sortGamesByKickoff uses —
// without it, entries at an identical kickoff have no defined relative
// order and the streak becomes nondeterministic between runs.
func pickemStreak(games []GameInfo, markets map[string]PickemMarket, picks map[string]string, now time.Time) int {
	type graded struct {
		kickoff time.Time
		id      string
		outcome PickemOutcome
	}
	participated := pickemParticipation(games, picks)
	entries := make([]graded, 0, len(games))
	for _, game := range games {
		outcome := gradePickem(game, markets[game.ID], picks[game.ID], participated[game.Week], now).Outcome
		if outcome == pickemPending {
			continue
		}
		entries = append(entries, graded{kickoff: game.Kickoff, id: game.ID, outcome: outcome})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].kickoff.Equal(entries[j].kickoff) {
			return entries[i].id > entries[j].id
		}
		return entries[i].kickoff.After(entries[j].kickoff)
	})
	streak := 0
	for _, entry := range entries {
		switch entry.outcome {
		case pickemPush, pickemVoid:
			continue
		case pickemLoss, pickemMissedLoss:
			break
		case pickemWin:
			streak++
		}
		if entry.outcome == pickemLoss || entry.outcome == pickemMissedLoss {
			break
		}
	}
	return streak
}

// PickemConsensusView is one game's rendered pick split: the raw counts
// and each side's share, plus precomputed inline-style strings — the
// strict-surface .gsx template only ever does string concatenation on
// server-typed strings (team/page.gsx's --badge-tone precedent), never
// computes a bar width from a raw percentage itself.
type PickemConsensusView struct {
	HasPicks     bool
	Total        int
	AwayPct      int
	HomePct      int
	AwayBarStyle string
	HomeBarStyle string
}

// pickemConsensus reports the league's pick split for one game: how many
// recorded picks went to each side and each side's share of the total.
// Callers must only invoke this once a game is locked (build item 2's
// consensus rule: "never before lock, no information leaks") — the
// function itself does not gate on lock state, so PickemData is the single
// place that decides when consensus may be computed at all.
func pickemConsensus(state PersistedState, game GameInfo) PickemConsensusView {
	awayCount, homeCount := 0, 0
	for _, picks := range state.Pickems {
		switch picks[game.ID] {
		case game.Away:
			awayCount++
		case game.Home:
			homeCount++
		}
	}
	total := awayCount + homeCount
	awayPct, homePct := 0, 0
	if total > 0 {
		awayPct = int((float64(awayCount)/float64(total))*100 + 0.5)
		homePct = 100 - awayPct
	}
	return PickemConsensusView{
		HasPicks:     total > 0,
		Total:        total,
		AwayPct:      awayPct,
		HomePct:      homePct,
		AwayBarStyle: fmt.Sprintf("width:%d%%;", awayPct),
		HomeBarStyle: fmt.Sprintf("width:%d%%;", homePct),
	}
}

// hiddenConsensus is the pre-lock placeholder every game card carries: no
// pick split is computed or shipped before a game locks, so there is
// nothing to leak even if a template were to render it by mistake.
func hiddenConsensus() PickemConsensusView {
	return PickemConsensusView{AwayBarStyle: "width:0%;", HomeBarStyle: "width:0%;"}
}

// PickemData assembles the pick'em HQ page: the viewed week's games with
// the viewer's picks and (once locked) the league's consensus, the
// viewer's weekly and season record and streak, and both the season and
// weekly leaderboards. week defaults to the current pick'em week
// (pickemWeek); ?week=N reuses that same resolver's output only as the
// default, then moves freely across every week the real schedule carries
// (build item 2's week navigation).

// PickemGameRow is one game on the pick'em slate as the page renders it:
// display fields, the viewer's own pick state, and (once locked) the
// league's consensus. A real struct, not a map, because PickemRow
// (page.gsx) is a strict component and reads it as a named prop.
// PickedAway/PickedHome are precomputed rather than compared in the
// template: the strict server renderer's attribute-value expressions
// support string concatenation only, not a "==" comparison (gosx check),
// so aria-pressed's true/false must already be a bool field.
type PickemGameRow struct {
	ID             string
	Week           int
	Label          string
	KickoffDisplay string
	Away           string
	Home           string
	Pick           string
	PickedAway     bool
	PickedHome     bool
	Picked         bool
	Locked         bool
	Final          bool
	Winner         string
	Correct        bool
	Wrong          bool
	Push           bool
	MissedLoss     bool
	Void           bool
	Outcome        string
	ResultLabel    string
	AwayLine       string
	HomeLine       string
	SpreadState    string
	SpreadAsOf     string
	SpreadLock     string
	SpreadSource   string
	ScoreDisplay   string
	Consensus      PickemConsensusView
}

func spreadTeamDisplay(team string, lineTenths int) string {
	if lineTenths == 0 {
		return team + " PK"
	}
	sign := "+"
	if lineTenths < 0 {
		sign = "-"
		lineTenths = -lineTenths
	}
	return fmt.Sprintf("%s %s%d.%d", team, sign, lineTenths/10, lineTenths%10)
}

func pickemSpreadView(game GameInfo, market PickemMarket, location *time.Location) (away, home, state, asOf, lock, source string) {
	if location == nil {
		location = time.UTC
	}
	state = "WAITING FOR LINE"
	if market.Void {
		state = "NO LINE · VOID"
	} else if market.Frozen {
		state = "FROZEN LINE"
	} else if market.LinePresent {
		state = "CURRENT LINE"
	}
	awayTeam, homeTeam := market.Away, market.Home
	if awayTeam == "" {
		awayTeam = game.Away
	}
	if homeTeam == "" {
		homeTeam = game.Home
	}
	if market.LinePresent && !market.Void {
		away = spreadTeamDisplay(awayTeam, market.LineTenths)
		home = spreadTeamDisplay(homeTeam, -market.LineTenths)
	} else {
		away, home = awayTeam+" —", homeTeam+" —"
	}
	if !market.ObservedAt.IsZero() {
		asOf = "AS OF " + market.ObservedAt.In(location).Format("Mon Jan 2 · 3:04 PM MST")
	}
	if !market.LockAt.IsZero() {
		prefix := "FREEZES "
		if market.Frozen || market.Void {
			prefix = "FROZEN "
		}
		lock = prefix + market.LockAt.In(location).Format("Mon Jan 2 · 3:04 PM MST")
	}
	if market.SourceURL != "" || market.SourceProvenance != "" {
		source = "VEGAS MARKET VIA NFLVERSE"
	}
	return
}

func pickemResultLabel(grade pickemGrade, locked bool) string {
	switch grade.Outcome {
	case pickemWin:
		return "WIN · " + grade.Cover + " COVERED"
	case pickemLoss:
		return "LOSS · " + grade.Cover + " COVERED"
	case pickemPush:
		return "PUSH"
	case pickemMissedLoss:
		return "MISSED LOSS"
	case pickemVoid:
		return "VOID · NO FROZEN LINE"
	case pickemPending:
		if locked {
			return "LOCKED · IN PROGRESS"
		}
	}
	return ""
}

func (s *Service) PickemData(r *http.Request) map[string]any {
	now := s.clock()
	viewerKey := s.viewerKey(r)
	allGames := s.schedule()
	// A page load is also an immediate reconciliation opportunity. The
	// lifecycle ticker remains authoritative when nobody has the page open.
	_ = s.store.ReconcilePickemMarkets(now, allGames, nil)
	state := s.store.Snapshot()
	currentWeek := s.pickemWeek(allGames, now)

	week := currentWeek
	if raw := strings.TrimSpace(r.URL.Query().Get("week")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			week = parsed
		}
	}

	weeks := pickemWeeks(allGames)
	minWeek, maxWeek := 1, 1
	if len(weeks) > 0 {
		minWeek, maxWeek = weeks[0], weeks[len(weeks)-1]
	}
	hasWeeks := len(weeks) > 0
	weekOptions := make([]map[string]any, 0, len(weeks))
	for _, w := range weeks {
		weekOptions = append(weekOptions, map[string]any{
			"value":    strconv.Itoa(w),
			"label":    fmt.Sprintf("WEEK %d", w),
			"selected": w == week,
		})
	}

	weekGames := gamesInWeek(allGames, week)
	sortGamesByKickoff(weekGames)

	location := s.draftTZ
	if location == nil {
		location, _ = time.LoadLocation(DefaultDraftTZ)
	}
	marketLocation, err := time.LoadLocation("America/New_York")
	if err != nil {
		marketLocation = location
	}

	viewerPicks := state.Pickems[viewerKey]
	viewerParticipation := pickemParticipation(allGames, viewerPicks)
	pickedCount, unpickedCount := 0, 0
	games := make([]PickemGameRow, 0, len(weekGames))
	for _, game := range weekGames {
		pick := viewerPicks[game.ID]
		locked := !now.Before(game.Kickoff)
		if pick != "" {
			pickedCount++
		} else if !locked {
			unpickedCount++
		}
		market := state.PickemMarkets[game.ID]
		grade := gradePickem(game, market, pick, viewerParticipation[game.Week], now)
		awayLine, homeLine, spreadState, spreadAsOf, spreadLock, spreadSource := pickemSpreadView(game, market, marketLocation)
		scoreDisplay := ""
		if game.Final {
			scoreDisplay = fmt.Sprintf("%d-%d", game.AwayScore, game.HomeScore)
		}
		consensus := hiddenConsensus()
		if locked {
			consensus = pickemConsensus(state, game)
		}
		games = append(games, PickemGameRow{
			ID:             game.ID,
			Week:           game.Week,
			Label:          game.Away + " @ " + game.Home,
			KickoffDisplay: game.Kickoff.In(location).Format("Mon Jan 2 · 3:04 PM MST"),
			Away:           game.Away,
			Home:           game.Home,
			Pick:           pick,
			PickedAway:     pick != "" && pick == game.Away,
			PickedHome:     pick != "" && pick == game.Home,
			Picked:         pick != "",
			Locked:         locked,
			Final:          game.Final,
			Winner:         grade.Cover,
			Correct:        grade.Outcome == pickemWin,
			Wrong:          grade.Outcome == pickemLoss || grade.Outcome == pickemMissedLoss,
			Push:           grade.Outcome == pickemPush,
			MissedLoss:     grade.Outcome == pickemMissedLoss,
			Void:           grade.Outcome == pickemVoid,
			Outcome:        string(grade.Outcome),
			ResultLabel:    pickemResultLabel(grade, locked),
			AwayLine:       awayLine,
			HomeLine:       homeLine,
			SpreadState:    spreadState,
			SpreadAsOf:     spreadAsOf,
			SpreadLock:     spreadLock,
			SpreadSource:   spreadSource,
			ScoreDisplay:   scoreDisplay,
			Consensus:      consensus,
		})
	}

	seasonLeaderboard := s.pickemLeaderboard(state, allGames, now)
	weekLeaderboard := s.pickemLeaderboard(state, weekGames, now)

	seasonRecord := tallyPicks(allGames, state.PickemMarkets, viewerPicks, now)
	weekRecord := tallyPicks(weekGames, state.PickemMarkets, viewerPicks, now)
	streak := pickemStreak(allGames, state.PickemMarkets, viewerPicks, now)

	return map[string]any{
		"viewer":            s.Viewer(r),
		"week":              week,
		"current_week":      currentWeek,
		"is_current_week":   week == currentWeek,
		"has_prev_week":     week > minWeek,
		"prev_week_href":    "/pickem?week=" + strconv.Itoa(week-1),
		"has_next_week":     week < maxWeek,
		"next_week_href":    "/pickem?week=" + strconv.Itoa(week+1),
		"current_week_href": "/pickem?week=" + strconv.Itoa(currentWeek),
		"week_options":      weekOptions,
		"has_weeks":         hasWeeks,
		"can_pick":          viewerKey != "",
		"games":             games,
		"games_empty":       len(games) == 0,
		"picked_count":      pickedCount,
		"unpicked_count":    unpickedCount,
		"record": map[string]any{
			"week_correct":   weekRecord.Wins,
			"week_total":     weekRecord.Wins + weekRecord.Losses + weekRecord.Pushes,
			"week_wins":      weekRecord.Wins,
			"week_losses":    weekRecord.Losses,
			"week_pushes":    weekRecord.Pushes,
			"season_correct": seasonRecord.Wins,
			"season_total":   seasonRecord.Wins + seasonRecord.Losses + seasonRecord.Pushes,
			"season_wins":    seasonRecord.Wins,
			"season_losses":  seasonRecord.Losses,
			"season_pushes":  seasonRecord.Pushes,
			"has_record":     seasonRecord.Participated,
			"streak":         streak,
			"has_streak":     streak > 0,
		},
		"leaderboard":            seasonLeaderboard,
		"leaderboard_empty":      len(seasonLeaderboard) == 0,
		"week_leaderboard":       weekLeaderboard,
		"week_leaderboard_empty": len(weekLeaderboard) == 0,
		"league":                 s.leagueMap(),
	}
}

// PickemLeaderboardEntry is one leaderboard row: a member's rank, display
// name, team mark, and correct/total tally. A real struct, not a map,
// because LeaderboardRow (page.gsx) is a strict component and reads it
// through a spread.
type PickemLeaderboardEntry struct {
	Rank    string
	Name    string
	Team    string
	Correct int
	Total   int
	Wins    int
	Losses  int
	Pushes  int
}

// assignSharedRanks sets each entry's Rank from its position in an
// already correct-descending, name-tiebroken sort, using competition
// ranking: entries tied on Correct share one rank, and the next distinct
// score jumps ahead by the tied count (01, 01, 03 — never 01, 01, 02).
func assignSharedRanks(out []PickemLeaderboardEntry) {
	rank := 0
	prevCorrect := -1
	for index := range out {
		correct := out[index].Correct
		if index == 0 || correct != prevCorrect {
			rank = index + 1
		}
		out[index].Rank = fmt.Sprintf("%02d", rank)
		prevCorrect = correct
	}
}

// pickemLeaderboard ranks participating members by ATS wins within games.
// A member appears as soon as they make one valid pick in that set, even
// before any game is graded. Called with the full schedule for the season
// leaderboard and with one week's games for the weekly leaderboard; both
// share this implementation and its shared-rank tie convention.
func (s *Service) pickemLeaderboard(state PersistedState, games []GameInfo, now time.Time) []PickemLeaderboardEntry {
	tallies := make(map[string]PickemATSRecord)
	for owner, picks := range state.Pickems {
		record := tallyPicks(games, state.PickemMarkets, picks, now)
		if record.Participated {
			tallies[owner] = record
		}
	}

	out := make([]PickemLeaderboardEntry, 0, len(tallies))
	for email, entry := range tallies {
		member := state.Members[email]
		name := strings.TrimSpace(member.Name)
		if name == "" {
			name = strings.Split(email, "@")[0]
		}
		team := ""
		if member.TeamID != "" {
			team = s.teamAbbreviation(member.TeamID)
		}
		out = append(out, PickemLeaderboardEntry{
			Name:    name,
			Team:    team,
			Correct: entry.Wins,
			Total:   entry.Wins + entry.Losses + entry.Pushes,
			Wins:    entry.Wins,
			Losses:  entry.Losses,
			Pushes:  entry.Pushes,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Wins != out[j].Wins {
			return out[i].Wins > out[j].Wins
		}
		return out[i].Name < out[j].Name
	})
	assignSharedRanks(out)
	return out
}

// teamAbbreviation resolves a known team ID to its abbreviation, ignoring
// commissioner name overrides which never touch the abbreviation.
func (s *Service) teamAbbreviation(teamID string) string {
	for _, team := range s.Teams() {
		if team.ID == teamID {
			return team.Abbreviation
		}
	}
	return ""
}

// PickemSet stores the viewer's pick for one real game after validating the
// game exists, the team is one of the two sides, and the game has not
// kicked off yet. It records only a pick, never a team seat: boardOwner
// resolves the acting email alone, and Store.SetPickem writes under that
// email with no Members-map read or write at all (seatless-membership
// audit, gridiron-2000 pick'em HQ task — this is the path the task asked
// "does submitting a pick auto-assign a seat" about; it does not, and nor
// does anything it calls).
func (s *Service) PickemSet(r *http.Request, gameID, team string) (GameInfo, error) {
	owner, err := s.boardOwner(r)
	if err != nil {
		return GameInfo{}, err
	}
	gameID = strings.TrimSpace(gameID)
	team = strings.TrimSpace(team)
	var game GameInfo
	found := false
	for _, candidate := range s.schedule() {
		if candidate.ID == gameID {
			game = candidate
			found = true
			break
		}
	}
	if !found {
		return GameInfo{}, fmt.Errorf("unknown game")
	}
	if team != game.Away && team != game.Home {
		return GameInfo{}, fmt.Errorf("pick one of the two teams")
	}
	if game.Kickoff.IsZero() || !s.clock().Before(game.Kickoff) {
		return GameInfo{}, fmt.Errorf("this game is locked")
	}
	if err := s.store.SetPickem(owner, gameID, team); err != nil {
		return GameInfo{}, err
	}
	return game, nil
}

// pickemHomeSummary assembles the seatless-member home dashboard's pick'em
// panel: this week's outstanding pick count and the viewer's season record
// and streak, so signing in without a team seat lands on a complete,
// honest home screen instead of an empty fantasy dashboard (build item 3).
func (s *Service) pickemHomeSummary(r *http.Request, state PersistedState, now time.Time) map[string]any {
	viewerKey := s.viewerKey(r)
	allGames := s.schedule()
	_ = s.store.ReconcilePickemMarkets(now, allGames, nil)
	state = s.store.Snapshot()
	week := s.pickemWeek(allGames, now)
	weekGames := gamesInWeek(allGames, week)

	picks := state.Pickems[viewerKey]
	unpicked := 0
	for _, game := range weekGames {
		if now.Before(game.Kickoff) && picks[game.ID] == "" {
			unpicked++
		}
	}
	record := tallyPicks(allGames, state.PickemMarkets, picks, now)
	streak := pickemStreak(allGames, state.PickemMarkets, picks, now)

	return map[string]any{
		"week":                week,
		"unpicked_count":      unpicked,
		"season_correct":      record.Wins,
		"season_total":        record.Wins + record.Losses + record.Pushes,
		"season_wins":         record.Wins,
		"season_losses":       record.Losses,
		"season_pushes":       record.Pushes,
		"has_record":          record.Participated,
		"streak":              streak,
		"has_streak":          streak > 0,
		"has_games_this_week": len(weekGames) > 0,
	}
}
