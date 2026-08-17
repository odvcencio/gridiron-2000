package league

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// BlitzGame is one real preseason game feeding Preseason Blitz (WP-B1).
type BlitzGame struct {
	ID      string // Tank01 gameID: 20260820_LV@HOU
	Slate   string // "pre2" | "pre3"
	Away    string
	Home    string
	Kickoff time.Time // UTC
	Final   bool
}

// BlitzSnapshot is the Blitz feed's current state: the known slate games
// and every live-tracked player's normalized stat line. Live scores are
// never persisted in league state (design spec section 4.4); the adapter
// (main package) caches the box-score JSON on disk and swaps an immutable
// snapshot pointer on each poll (the playerPool idiom, service.go).
type BlitzSnapshot struct {
	Version int64
	Games   []BlitzGame
	// Stats: slate ID -> player ID -> stat key -> value.
	Stats map[string]map[string]map[string]float64
}

// BlitzSource returns the current snapshot from memory. It must not
// perform network work; the adapter's poller (main package) owns that.
type BlitzSource func() BlitzSnapshot

// SetBlitzSource attaches the Preseason Blitz feed. Call it once during
// startup, before the server accepts requests, beside SetScheduleSource.
// Never calling it leaves /blitz in its honest feed-offline state.
func (s *Service) SetBlitzSource(source BlitzSource) {
	s.poolMu.Lock()
	s.blitzFn = source
	s.poolMu.Unlock()
}

// blitzSnapshot returns the current snapshot, or the zero value when no
// source is attached.
func (s *Service) blitzSnapshot() (BlitzSnapshot, bool) {
	s.poolMu.Lock()
	source := s.blitzFn
	s.poolMu.Unlock()
	if source == nil {
		return BlitzSnapshot{}, false
	}
	return source(), true
}

// BlitzPre1Source returns the current preseason-week-1 per-player raw stat
// lines: playerID -> stat key -> value, the SAME shape BlitzSnapshot.Stats
// uses for live pre2/pre3 scoring — so blitzEligiblePlayers scores pre1
// production through the identical scoreStatsWithValues call live scoring
// uses, never a forked formula. It must perform no network work; the
// main-package pipeline that fetches, scores once, and persists this data
// to disk owns that, exactly like BlitzSource owns the live-stats fetch.
// A nil or empty map means no pre1 data is available (the source was
// never attached, or the pipeline's own fetch failed and logged its one
// honest line) — blitzEligiblePlayers then treats every player as
// zero-pre1 and falls back to ranking by the rookie/ADP tiering alone.
type BlitzPre1Source func() map[string]map[string]float64

// SetBlitzPre1Source attaches the preseason-week-1 production feed (owner
// directive, 2026-08-16: "based on week 1 preseason mostly and
// rookies/likely to play players so it's less confusing"). Call it once
// during startup, beside SetBlitzSource. Never calling it is an honest
// degrade, not a bug — see BlitzPre1Source's doc comment.
func (s *Service) SetBlitzPre1Source(source BlitzPre1Source) {
	s.poolMu.Lock()
	s.blitzPre1Fn = source
	s.poolMu.Unlock()
}

// blitzPre1Stats returns the current pre1 stat map, or nil when no source
// is attached.
func (s *Service) blitzPre1Stats() map[string]map[string]float64 {
	s.poolMu.Lock()
	source := s.blitzPre1Fn
	s.poolMu.Unlock()
	if source == nil {
		return nil
	}
	return source()
}

// validBlitzSlate reports whether id names one of the two Preseason Blitz
// slates. The ID-to-label table itself (pre2 -> "Preseason Week 2") lives
// in the main package's blitz_source.go, next to the fetch calls that use
// it (F15's seam boundary: internal/league imports nothing from main or
// internal/fantasy); this package only needs the ID space to validate.
func validBlitzSlate(id string) bool {
	return id == "pre2" || id == "pre3"
}

// blitzSlateLabel derives the slate's display label from its ID. The ID
// convention (design spec section 4.1) already carries the week number, so
// deriving here avoids a second copy of the {pre2,pre3} table.
func blitzSlateLabel(slate string) string {
	switch slate {
	case "pre2":
		return "Preseason Week 2"
	case "pre3":
		return "Preseason Week 3"
	default:
		return ""
	}
}

// blitzGamesForSlate filters games to one slate, sorted by kickoff then ID
// for deterministic display (matching PickemData's weekGames sort).
func blitzGamesForSlate(games []BlitzGame, slate string) []BlitzGame {
	out := make([]BlitzGame, 0, len(games))
	for _, game := range games {
		if game.Slate == slate {
			out = append(out, game)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kickoff.Equal(out[j].Kickoff) {
			return out[i].ID < out[j].ID
		}
		return out[i].Kickoff.Before(out[j].Kickoff)
	})
	return out
}

// blitzSlateClosed reports whether every game in the slate is final (V10).
// An empty slate — no games loaded yet, the feed-offline state — is never
// closed; a slate with no known games has nothing to close.
func blitzSlateClosed(games []BlitzGame) bool {
	if len(games) == 0 {
		return false
	}
	for _, game := range games {
		if !game.Final {
			return false
		}
	}
	return true
}

// blitzGameForTeam finds the slate game team plays in, home or away.
func blitzGameForTeam(games []BlitzGame, team string) (BlitzGame, bool) {
	team = strings.ToUpper(strings.TrimSpace(team))
	for _, game := range games {
		if game.Away == team || game.Home == team {
			return game, true
		}
	}
	return BlitzGame{}, false
}

// blitzPlayerLocked reports whether a player's NFL team has already kicked
// off in this slate (the pick'em lock idiom, F8: !now.Before(kickoff)). A
// team with no slate game locks by default — a defensive fallback for a
// stale entry left over after a pool change; see BlitzRemove.
func blitzPlayerLocked(games []BlitzGame, team string, now time.Time) bool {
	game, ok := blitzGameForTeam(games, team)
	if !ok {
		return true
	}
	return !now.Before(game.Kickoff)
}

// blitzSunsetAt resolves the sunset instant: 48 hours after the last pre3
// kickoff (design spec section 4.5). The zero time means "unknown" — no
// pre3 games are loaded yet — so the contest never sunsets on missing
// data.
func blitzSunsetAt(games []BlitzGame) time.Time {
	var last time.Time
	for _, game := range games {
		if game.Slate == "pre3" && game.Kickoff.After(last) {
			last = game.Kickoff
		}
	}
	if last.IsZero() {
		return time.Time{}
	}
	return last.Add(48 * time.Hour)
}

// blitzArchived reports whether now has reached the sunset instant.
func blitzArchived(games []BlitzGame, now time.Time) bool {
	sunset := blitzSunsetAt(games)
	return !sunset.IsZero() && !now.Before(sunset)
}

// blitzActiveSlate resolves the default slate to show: the smallest slate
// (pre2 before pre3) with a game kicking off within the last four hours or
// later, else the latest slate — the pick'em week rule (pickemWeek,
// pickem.go:37-58), applied to the two-slate space instead of NFL weeks.
// An empty game list defaults to pre2.
func blitzActiveSlate(games []BlitzGame, now time.Time) string {
	if len(games) == 0 {
		return "pre2"
	}
	rank := map[string]int{"pre2": 1, "pre3": 2}
	cutoff := now.Add(-4 * time.Hour)
	largest := games[0].Slate
	upcoming := ""
	haveUpcoming := false
	for _, game := range games {
		if rank[game.Slate] > rank[largest] {
			largest = game.Slate
		}
		if game.Kickoff.After(cutoff) && (!haveUpcoming || rank[game.Slate] < rank[upcoming]) {
			upcoming = game.Slate
			haveUpcoming = true
		}
	}
	if haveUpcoming {
		return upcoming
	}
	return largest
}

// BlitzEnteredTeams returns the set of NFL team abbreviations with at
// least one player entered in either slate, across every owner. The
// poller's relevance filter (design spec section 4.8) reads this instead
// of reaching into BlitzEntries and the pool directly, keeping the adapter
// (main package) decoupled from the store's shape (F15).
func (s *Service) BlitzEnteredTeams() map[string]bool {
	state := s.store.Snapshot()
	pool := s.pool()
	teams := map[string]bool{}
	for _, bySlate := range state.BlitzEntries {
		for _, entry := range bySlate {
			for _, playerID := range entry.Players {
				if player, ok := pool.byID[playerID]; ok && player.NFLTeam != "" {
					teams[strings.ToUpper(player.NFLTeam)] = true
				}
			}
		}
	}
	return teams
}

// BlitzAdd adds playerID to the viewer's slate entry after validating
// section 7's rules in order: V1 sign-in, V2 known slate, V10 slate
// closed, V3 pool membership, V4 not a defense, V5 team plays in the
// slate, V9 not yet kicked off, V7 not already entered, V6 room for a
// sixth player, V8 team cap.
func (s *Service) BlitzAdd(r *http.Request, slate, playerID string) error {
	now := s.clock()
	owner := s.viewerKey(r)
	if owner == "" {
		return fmt.Errorf("sign in to enter the blitz")
	}
	slate = strings.TrimSpace(slate)
	if !validBlitzSlate(slate) {
		return fmt.Errorf("unknown slate")
	}
	snapshot, _ := s.blitzSnapshot()
	slateGames := blitzGamesForSlate(snapshot.Games, slate)
	if blitzSlateClosed(slateGames) {
		return fmt.Errorf("this slate is closed")
	}
	playerID = strings.TrimSpace(playerID)
	pool := s.pool()
	player, ok := pool.byID[playerID]
	if !ok {
		return fmt.Errorf("choose a player from the pool")
	}
	if player.Position == "DST" {
		return fmt.Errorf("defenses are not eligible in the blitz")
	}
	game, hasGame := blitzGameForTeam(slateGames, player.NFLTeam)
	if !hasGame {
		return fmt.Errorf("that player's team does not play in this slate")
	}
	if !now.Before(game.Kickoff) {
		return fmt.Errorf("that player's game has already kicked off")
	}
	entry := s.store.Snapshot().BlitzEntries[owner][slate]
	for _, existing := range entry.Players {
		if existing == playerID {
			return fmt.Errorf("that player is already in your entry")
		}
	}
	if len(entry.Players) >= 5 {
		return fmt.Errorf("an entry holds at most 5 players")
	}
	teamCount := 0
	for _, existing := range entry.Players {
		if existingPlayer, ok := pool.byID[existing]; ok && strings.EqualFold(existingPlayer.NFLTeam, player.NFLTeam) {
			teamCount++
		}
	}
	if teamCount >= 2 {
		return fmt.Errorf("at most 2 players from one NFL team")
	}
	updated := append(append([]string(nil), entry.Players...), playerID)
	return s.store.BlitzSetEntry(owner, slate, updated, now)
}

// BlitzRemove drops playerID from the viewer's slate entry after
// validating section 7's rules in order: V1 sign-in, V2 known slate, V10
// slate closed, V11 player is in the entry, V9 not yet kicked off.
func (s *Service) BlitzRemove(r *http.Request, slate, playerID string) error {
	now := s.clock()
	owner := s.viewerKey(r)
	if owner == "" {
		return fmt.Errorf("sign in to enter the blitz")
	}
	slate = strings.TrimSpace(slate)
	if !validBlitzSlate(slate) {
		return fmt.Errorf("unknown slate")
	}
	snapshot, _ := s.blitzSnapshot()
	slateGames := blitzGamesForSlate(snapshot.Games, slate)
	if blitzSlateClosed(slateGames) {
		return fmt.Errorf("this slate is closed")
	}
	playerID = strings.TrimSpace(playerID)
	entry := s.store.Snapshot().BlitzEntries[owner][slate]
	found := false
	for _, existing := range entry.Players {
		if existing == playerID {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("that player is not in your entry")
	}
	if player, ok := s.pool().byID[playerID]; ok {
		if game, hasGame := blitzGameForTeam(slateGames, player.NFLTeam); hasGame && !now.Before(game.Kickoff) {
			return fmt.Errorf("that player's game has already kicked off")
		}
	}
	updated := make([]string, 0, len(entry.Players))
	for _, existing := range entry.Players {
		if existing != playerID {
			updated = append(updated, existing)
		}
	}
	return s.store.BlitzSetEntry(owner, slate, updated, now)
}

// blitzSlotMaps renders the viewer's own slate entry as up to five filled
// slots. Unlike the leaderboard, the viewer's own picks are never hidden —
// only another member's unlocked pick is (section 4.5's reveal rule).
func (s *Service) blitzSlotMaps(entry BlitzEntry, slateGames []BlitzGame, liveStats map[string]map[string]float64, scoringValues map[string]float64, pool playerPool, now time.Time) []map[string]any {
	out := make([]map[string]any, 0, len(entry.Players))
	for _, playerID := range entry.Players {
		player, ok := pool.byID[playerID]
		if !ok {
			continue
		}
		locked := blitzPlayerLocked(slateGames, player.NFLTeam, now)
		points := 0.0
		var rows []map[string]any
		var total string
		if locked {
			statLine := liveStats[playerID]
			points = scoreStatsWithValues(statLine, scoringValues)
			rows, total = scoreBreakdownWithValues(statLine, scoringValues)
		}
		detail := player.NFLTeam
		if player.Injury != "" {
			detail += " · " + player.Injury
		}
		jersey := ""
		if player.Jersey != "" {
			jersey = "#" + player.Jersey
		}
		out = append(out, map[string]any{
			"id":              player.ID,
			"name":            player.Name,
			"position":        player.Position,
			"nfl_team":        player.NFLTeam,
			"detail":          detail,
			"jersey":          jersey,
			"headshot":        player.Headshot,
			"has_headshot":    player.Headshot != "",
			"locked":          locked,
			"points":          fmt.Sprintf("%.1f", points),
			"has_breakdown":   len(rows) > 0,
			"breakdown":       rows,
			"breakdown_total": total,
		})
	}
	return out
}

// blitzRestingADPRankMax caps "low" (strong) ADPRank for the zero-pre1
// group's rest tier: the top blitzRestingADPRankMax picks are locked-in
// NFL starters — the players most likely to sit out most of the
// preseason once their coach is satisfied with a healthy roster (owner
// directive, 2026-08-16: "less confusing" board). It is a tuning knob,
// not a measured constant. mergePool only assigns ADPRank to a player
// with ADP > 0 (internal/fantasy/service.go), so this only ever gates a
// player the live ADP list actually ranked.
const blitzRestingADPRankMax = 100

// blitzRests reports whether player belongs in the zero-pre1 group's
// "likely to rest" tier (see blitzEligiblePlayers): an established
// veteran starter — strong (low) ADPRank — with no pre-week-1
// production. A rookie never rests here, even at a strong ADPRank: a
// debut season carries no prior-year renown to be "resting" on, and the
// owner directive calls rookies out by name as players the board should
// surface, not bury.
func blitzRests(player Player) bool {
	return !player.Rookie && player.ADPRank > 0 && player.ADPRank <= blitzRestingADPRankMax
}

// pre1StatItem is one preseason-week-1 stat-line entry: the raw stat key
// (the same key parsePreseasonBoxScore emits, internal/fantasy/
// preseason.go), its short display suffix, and whether a value of
// exactly 1 prints bare ("TD") instead of "1 TD".
type pre1StatItem struct {
	statKey string
	suffix  string
	bareOne bool
}

// pre1StatGroups is the preseason-week-1 stat-line table, grouped
// Passing, Rushing, Receiving, Kicking, then Misc — the same grouping
// breakdownRows uses (breakdown.go) — so the board's compact line and
// the score-breakdown tooltip never disagree about which stat belongs to
// which group. A key this table does not list simply never shows.
var pre1StatGroups = [][]pre1StatItem{
	{{"passAttempts", "pass att", false}, {"passYds", "pass yds", false}, {"passTD", "pass TD", true}, {"passInt", "INT", true}},
	{{"carries", "att", false}, {"rushYds", "rush yds", false}, {"rushTD", "rush TD", true}},
	{{"receptions", "rec", false}, {"recYds", "rec yds", false}, {"recTD", "rec TD", true}},
	{{"fgMade", "FG", false}, {"fgMissed", "FG miss", false}, {"xpMade", "XP", false}},
	{{"returnTD", "ret TD", true}, {"fumblesLost", "FUM", true}},
}

// preWeek1StatLine renders a compact, honest summary of a player's
// preseason-week-1 box score: only the nonzero stats it carries, grouped
// and ordered by pre1StatGroups, items within a group joined by ", ",
// groups joined by " · ". An empty or all-zero stats map returns "" —
// blitzEligiblePlayers falls back to the "no pre1 snaps" copy in that
// case, never a blank line.
func preWeek1StatLine(stats map[string]float64) string {
	if len(stats) == 0 {
		return ""
	}
	var groups []string
	for _, group := range pre1StatGroups {
		var parts []string
		for _, item := range group {
			value, ok := stats[item.statKey]
			if !ok || value == 0 {
				continue
			}
			if item.bareOne && value == 1 {
				parts = append(parts, item.suffix)
				continue
			}
			parts = append(parts, strconv.FormatFloat(value, 'f', -1, 64)+" "+item.suffix)
		}
		if len(parts) > 0 {
			groups = append(groups, strings.Join(parts, ", "))
		}
	}
	return strings.Join(groups, " · ")
}

// blitzEligibleRow pairs one pool player with its preseason-week-1
// evidence and rest-tier tag, ahead of sorting into blitzEligiblePlayers'
// three board tiers.
type blitzEligibleRow struct {
	player   Player
	points   float64
	hasData  bool
	statLine string
	resting  bool
}

// blitzOrderEligible sorts rows into the board's three tiers (owner
// directive, 2026-08-16): pre-week-1 producers first — points > 0,
// descending, name tiebreak — then the zero-pre1 group's front tier
// (rookies and no-ADP/deep-ADP depth players, alphabetical), then its
// back tier (established, strong-ADP starters tagged "likely to rest,"
// alphabetical). With no pre1 data at all, every row's points is 0, so
// every row lands in the zero-pre1 group — the board still ranks by the
// rookie/ADP tiering alone rather than crashing or reverting to raw
// pool order.
func blitzOrderEligible(rows []blitzEligibleRow) {
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		aScored, bScored := a.points > 0, b.points > 0
		if aScored != bScored {
			return aScored
		}
		if aScored {
			if a.points != b.points {
				return a.points > b.points
			}
			return a.player.Name < b.player.Name
		}
		if a.resting != b.resting {
			return !a.resting
		}
		return a.player.Name < b.player.Name
	})
}

// blitzEligiblePlayers lists pool players eligible for slate: non-DST
// positions whose NFL team plays a slate game (section 4.2). Already
// entered players still appear — V7 catches a repeat add — so the list
// need not track the viewer's own entry.
//
// Board order (owner directive, 2026-08-16 — "based on week 1 preseason
// mostly and rookies/likely to play players so it's less confusing"):
// see blitzOrderEligible. pre1Stats is playerID -> raw stat key -> value
// (see BlitzPre1Source); scoring goes through scoreStatsWithValues, the
// SAME function live Blitz scoring already uses (breakdown.go) — no
// forked math. Every row carries its own pre1 evidence (points plus a
// compact stat line, or an honest "no pre1 snaps" when there is none),
// a rookie flag, and the resting tag, so BlitzPoolRow (app/blitz/
// page.gsx) never has to compute any of this itself.
func (s *Service) blitzEligiblePlayers(pool playerPool, slateGames []BlitzGame, scoringValues map[string]float64, pre1Stats map[string]map[string]float64) []map[string]any {
	teams := map[string]bool{}
	for _, game := range slateGames {
		teams[game.Away] = true
		teams[game.Home] = true
	}
	rows := make([]blitzEligibleRow, 0, len(pool.players))
	for _, player := range pool.players {
		if player.Position == "" || player.Position == "DST" {
			continue
		}
		if !teams[strings.ToUpper(player.NFLTeam)] {
			continue
		}
		stats := pre1Stats[player.ID]
		rows = append(rows, blitzEligibleRow{
			player:   player,
			points:   scoreStatsWithValues(stats, scoringValues),
			hasData:  len(stats) > 0,
			statLine: preWeek1StatLine(stats),
			resting:  blitzRests(player),
		})
	}
	blitzOrderEligible(rows)

	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		card := playerMap(row.player, scoringValues)
		summary := "no pre1 snaps"
		switch {
		case row.hasData && row.statLine != "":
			summary = "PRE1 " + fmt.Sprintf("%.1f", row.points) + " — " + row.statLine
		case row.hasData:
			summary = "PRE1 " + fmt.Sprintf("%.1f", row.points)
		}
		card["has_pre1"] = row.hasData
		card["pre1_summary"] = summary
		card["is_rookie"] = row.player.Rookie
		card["resting"] = row.resting
		out = append(out, card)
	}
	return out
}

// blitzMemberEntry is one leaderboard row before ranking.
type blitzMemberEntry struct {
	owner     string
	name      string
	team      string
	total     float64
	updatedAt time.Time
	players   []map[string]any
}

// blitzLeaderboard ranks every owner holding a nonempty entry in slate by
// summed live score: sum floats first, format once (section 4.3). Shared
// rank on an exact tie; display order among equals is earlier UpdatedAt
// then email (section 4.5, T4). Another owner's unlocked player is hidden
// — blank name/position/team, revealed false — but still contributes its
// by-definition 0.0 to the total (the reveal rule, section 4.5).
func (s *Service) blitzLeaderboard(state PersistedState, slate string, slateGames []BlitzGame, liveStats map[string]map[string]float64, scoringValues map[string]float64, pool playerPool, now time.Time) []map[string]any {
	entries := make([]blitzMemberEntry, 0, len(state.BlitzEntries))
	for owner, bySlate := range state.BlitzEntries {
		entry, ok := bySlate[slate]
		if !ok || len(entry.Players) == 0 {
			continue
		}
		member := state.Members[owner]
		name := strings.TrimSpace(member.Name)
		if name == "" {
			name = strings.Split(owner, "@")[0]
		}
		team := ""
		if member.TeamID != "" {
			team = s.teamAbbreviation(member.TeamID)
		}
		total := 0.0
		players := make([]map[string]any, 0, len(entry.Players))
		for _, playerID := range entry.Players {
			player, known := pool.byID[playerID]
			locked := known && blitzPlayerLocked(slateGames, player.NFLTeam, now)
			points := 0.0
			var rows []map[string]any
			var breakdownTotal string
			pName, pPosition, pTeam := "", "", ""
			if locked {
				statLine := liveStats[playerID]
				points = scoreStatsWithValues(statLine, scoringValues)
				rows, breakdownTotal = scoreBreakdownWithValues(statLine, scoringValues)
				pName, pPosition, pTeam = player.Name, player.Position, player.NFLTeam
			}
			total += points
			players = append(players, map[string]any{
				"id":              playerID,
				"name":            pName,
				"position":        pPosition,
				"team":            pTeam,
				"revealed":        locked,
				"points":          fmt.Sprintf("%.1f", points),
				"has_breakdown":   len(rows) > 0,
				"breakdown":       rows,
				"breakdown_total": breakdownTotal,
			})
		}
		entries = append(entries, blitzMemberEntry{
			owner: owner, name: name, team: team,
			total: total, updatedAt: entry.UpdatedAt, players: players,
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].total != entries[j].total {
			return entries[i].total > entries[j].total
		}
		if !entries[i].updatedAt.Equal(entries[j].updatedAt) {
			return entries[i].updatedAt.Before(entries[j].updatedAt)
		}
		return entries[i].owner < entries[j].owner
	})
	out := make([]map[string]any, 0, len(entries))
	rank := 0
	previousTotal := 0.0
	for index, entry := range entries {
		if index == 0 || entry.total != previousTotal {
			rank = index + 1
		}
		previousTotal = entry.total
		out = append(out, map[string]any{
			"rank":    fmt.Sprintf("%02d", rank),
			"name":    entry.name,
			"team":    entry.team,
			"total":   fmt.Sprintf("%.1f", entry.total),
			"players": entry.players,
		})
	}
	return out
}

// BlitzData assembles the /blitz page: the active slate's schedule, the
// viewer's entry builder, the eligible-player list, the live leaderboard,
// and — once the contest has sunset — the archive panel.
func (s *Service) BlitzData(r *http.Request) map[string]any {
	now := s.clock()
	state := s.store.Snapshot()
	viewerKey := s.viewerKey(r)
	pool := s.pool()
	scoringValues := s.currentScoringValues()
	snapshot, attached := s.blitzSnapshot()

	slate := strings.TrimSpace(r.URL.Query().Get("slate"))
	if !validBlitzSlate(slate) {
		slate = blitzActiveSlate(snapshot.Games, now)
	}
	other := "pre3"
	if slate == "pre3" {
		other = "pre2"
	}

	slateGames := blitzGamesForSlate(snapshot.Games, slate)
	archived := blitzArchived(snapshot.Games, now)
	closed := blitzSlateClosed(slateGames)
	liveStats := snapshot.Stats[slate]

	entry := state.BlitzEntries[viewerKey][slate]
	slots := s.blitzSlotMaps(entry, slateGames, liveStats, scoringValues, pool, now)
	eligible := []map[string]any{}
	if !archived && !closed {
		eligible = s.blitzEligiblePlayers(pool, slateGames, scoringValues, s.blitzPre1Stats())
	}

	entryCount := 0
	for _, bySlate := range state.BlitzEntries {
		if e, ok := bySlate[slate]; ok && len(e.Players) > 0 {
			entryCount++
		}
	}

	location := s.draftTZ
	if location == nil {
		location, _ = time.LoadLocation(DefaultDraftTZ)
	}
	games := make([]map[string]any, 0, len(slateGames))
	for _, game := range slateGames {
		locked := !now.Before(game.Kickoff)
		status := "UPCOMING"
		switch {
		case game.Final:
			status = "FINAL"
		case locked && attached:
			status = "LIVE"
		case locked:
			status = "LOCKED"
		}
		games = append(games, map[string]any{
			"id":              game.ID,
			"label":           game.Away + " @ " + game.Home,
			"kickoff_display": game.Kickoff.In(location).Format("Mon Jan 2 · 3:04 PM MST"),
			"locked":          locked,
			"final":           game.Final,
			"status":          status,
		})
	}

	leaderboard := s.blitzLeaderboard(state, slate, slateGames, liveStats, scoringValues, pool, now)

	data := map[string]any{
		"viewer":            s.Viewer(r),
		"feed_offline":      !attached,
		"archived":          archived,
		"slate":             slate,
		"slate_label":       blitzSlateLabel(slate),
		"other_slate":       other,
		"other_slate_label": blitzSlateLabel(other),
		"can_enter":         viewerKey != "" && !archived,
		// entry_open additionally requires the slate itself to still be
		// open; the template uses this single flag to gate the Add/Remove
		// controls instead of combining can_enter and slate_closed inline.
		"entry_open":        viewerKey != "" && !archived && !closed,
		"entry_count":       entryCount,
		"slots":             slots,
		"slots_count":       len(slots),
		"slots_empty":       len(slots) == 0,
		"slots_full":        len(slots) >= 5,
		"eligible":          eligible,
		"eligible_empty":    len(eligible) == 0,
		"slate_closed":      closed,
		"games":             games,
		"games_empty":       len(games) == 0,
		"leaderboard":       leaderboard,
		"leaderboard_empty": len(leaderboard) == 0,
		"has_archive":       false,
		"archive":           map[string]any{},
		"league":            s.leagueMap(),
	}
	if archived {
		data["has_archive"] = true
		data["archive"] = s.blitzArchiveMap(state, snapshot, scoringValues, pool, now)
	}
	return data
}

// blitzArchiveMap assembles the post-sunset archive panel (section 4.5):
// both slates' final leaderboards, each slate's champion line (shared on a
// tie), and the combined champion by summed totals across both slates
// (members with one entry sum that one entry alone).
func (s *Service) blitzArchiveMap(state PersistedState, snapshot BlitzSnapshot, scoringValues map[string]float64, pool playerPool, now time.Time) map[string]any {
	boards := map[string][]map[string]any{}
	champions := map[string]string{}
	for _, slate := range []string{"pre2", "pre3"} {
		games := blitzGamesForSlate(snapshot.Games, slate)
		board := s.blitzLeaderboard(state, slate, games, snapshot.Stats[slate], scoringValues, pool, now)
		boards[slate] = board
		var names []string
		for _, row := range board {
			if row["rank"] != "01" {
				break
			}
			names = append(names, row["name"].(string))
		}
		champions[slate] = strings.Join(names, " & ")
	}

	overall := map[string]float64{}
	displayName := map[string]string{}
	for owner, bySlate := range state.BlitzEntries {
		total := 0.0
		hasEntry := false
		for _, slate := range []string{"pre2", "pre3"} {
			entry, ok := bySlate[slate]
			if !ok || len(entry.Players) == 0 {
				continue
			}
			hasEntry = true
			games := blitzGamesForSlate(snapshot.Games, slate)
			stats := snapshot.Stats[slate]
			for _, playerID := range entry.Players {
				player, known := pool.byID[playerID]
				if !known || !blitzPlayerLocked(games, player.NFLTeam, now) {
					continue
				}
				total += scoreStatsWithValues(stats[playerID], scoringValues)
			}
		}
		if !hasEntry {
			continue
		}
		overall[owner] = total
		member := state.Members[owner]
		name := strings.TrimSpace(member.Name)
		if name == "" {
			name = strings.Split(owner, "@")[0]
		}
		displayName[owner] = name
	}
	best := 0.0
	haveBest := false
	var overallChampions []string
	for owner, total := range overall {
		switch {
		case !haveBest || total > best:
			best = total
			overallChampions = []string{displayName[owner]}
			haveBest = true
		case total == best:
			overallChampions = append(overallChampions, displayName[owner])
		}
	}
	sort.Strings(overallChampions)

	return map[string]any{
		"pre2_leaderboard":       boards["pre2"],
		"pre2_leaderboard_empty": len(boards["pre2"]) == 0,
		"pre3_leaderboard":       boards["pre3"],
		"pre3_leaderboard_empty": len(boards["pre3"]) == 0,
		"pre2_champion":          champions["pre2"],
		"pre3_champion":          champions["pre3"],
		"overall_champion":       strings.Join(overallChampions, " & "),
	}
}
