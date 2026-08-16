package league

import (
	"fmt"
	"net/http"
	"sort"
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

// blitzEligiblePlayers lists pool players eligible for slate: non-DST
// positions whose NFL team plays a slate game (section 4.2). Already
// entered players still appear — V7 catches a repeat add — so the list
// need not track the viewer's own entry.
func (s *Service) blitzEligiblePlayers(pool playerPool, slateGames []BlitzGame, scoringValues map[string]float64) []map[string]any {
	teams := map[string]bool{}
	for _, game := range slateGames {
		teams[game.Away] = true
		teams[game.Home] = true
	}
	out := make([]map[string]any, 0, len(pool.players))
	for _, player := range pool.players {
		if player.Position == "" || player.Position == "DST" {
			continue
		}
		if !teams[strings.ToUpper(player.NFLTeam)] {
			continue
		}
		out = append(out, playerMap(player, scoringValues))
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
		eligible = s.blitzEligiblePlayers(pool, slateGames, scoringValues)
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
