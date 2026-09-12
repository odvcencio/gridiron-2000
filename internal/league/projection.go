package league

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// remainingFractionByPeriod holds the fixed remaining-time-of-game
// fraction A6's projection uses for each quarter (decision, Task 11a): the
// browser never learns the game clock's exact seconds remaining, so each
// quarter's fraction is that quarter's own midpoint estimate — "half of
// this quarter, plus every quarter still to come" — rather than a false
// precision the render path cannot actually back up. OT reads the same as
// Q4: once a game reaches overtime its own remaining fraction is already
// exhausted to the Q4 estimate, and no further schedule signal exists to
// refine it.
var remainingFractionByPeriod = map[string]float64{
	"Q1": 0.875,
	"Q2": 0.625,
	"Q3": 0.375,
	"Q4": 0.125,
	"OT": 0.125,
}

// remainingFraction is projectedTotal's per-game multiplier on a starter's
// remaining projection. known is false when the live poller has no
// affirmative signal for this exact game (no live status source wired at
// all, or the poller's status simply has no entry for this team yet); in
// that case the whole projection still applies (fraction 1): the
// manager-facing total should read as "the player's full week is still
// ahead of them", not silently drop the projection because the poller has
// not caught up. A game the schedule or the poller marks Final always
// contributes zero remaining projection, regardless of known. Once known
// and InProgress, an unrecognized period label (for example Tank01's
// "HALF") is a real live game the table just has no entry for, not the
// same "nothing has happened yet" claim a pre-kickoff or unwired read is
// — it reads as 0.5 (round-2 review of commit 133d1d7, finding 6), the
// same neutral halfway estimate remainingFractionByPeriod already picks
// for the middle of a normal quarter. A known, not-in-progress,
// unrecognized period (the pre-kickoff case, Period == "") keeps the
// full-projection fraction 1.
func remainingFraction(state LiveGameState, known bool) float64 {
	if state.Final {
		return 0
	}
	if !known {
		return 1
	}
	if fraction, ok := remainingFractionByPeriod[state.Period]; ok {
		return fraction
	}
	if state.InProgress {
		return 0.5
	}
	return 1
}

// winProbability is the A6 heuristic (decision, settled in the Task 10
// review): a logistic on the two sides' projected-total gap, scale 10 —
// chosen so a 10-point projected edge alone reads as roughly a 73% win
// probability, a comfortable-but-not-certain lead, without needing any
// larger statistical model this render path has no data to support.
func winProbability(mine, theirs float64) float64 {
	return 1 / (1 + math.Exp(-(mine-theirs)/10))
}

// winProbabilityDashText is the honest not-yet-known render for a win
// probability or projected total no starter data can back up
// (winProbabilityText, projectedText, and the "win_prob"/"projected"
// JSON/bind fields below).
const winProbabilityDashText = "—"

// hasProjectableStarters reports whether rows carries at least one filled
// starting slot — the minimum projectedTotal needs to mean anything.
// Before kickoff every filled slot still contributes its full weekly
// projection (remainingFraction's known=false case), so this is the right
// gate for "does a projection exist", independent of whether the CURRENT
// score is known yet (TeamWeekLedger.Known/ScoreKnown, which stays false
// for the entire pre-kickoff window and used to also blank the projection
// with it — wave-8 audit item 2). A side with every slot empty (no draft
// yet, or a lineup nobody has ever set) has nothing to project.
func hasProjectableStarters(rows []StarterLedgerRow) bool {
	for _, row := range rows {
		if row.PlayerID != "" {
			return true
		}
	}
	return false
}

// playerHasProjection is the projection-presence rule shared by the
// matchup render and the starter-only team total. Tank01's parser only
// admits a positive projection into the fantasy pool, and the fantasy
// service's WithProj status uses the same rule; a zero here therefore means
// the source did not provide a forecast, not that a filled starter is
// forecast to score zero. Empty slots are handled by the row/lineup callers
// and remain honest zeroes.
func playerHasProjection(player Player) bool {
	return player.ID != "" && projectionValueKnown(player.Projection)
}

func projectionValueKnown(value float64) bool {
	return value > 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

// hasKnownStarterProjections reports whether every filled starter row has a
// matching positive source projection. It deliberately returns false for an
// empty lineup, and also for a partially known lineup: a team total based on
// only the known subset would masquerade as a complete forecast.
func hasKnownStarterProjections(rows []StarterLedgerRow, byID map[string]Player) bool {
	hasStarter := false
	for _, row := range rows {
		if row.PlayerID == "" {
			continue
		}
		hasStarter = true
		player, ok := byID[row.PlayerID]
		if !ok || !playerHasProjection(player) {
			return false
		}
	}
	return hasStarter
}

// projectionWeekMatches reports whether an explicit pool source week can be
// used for the requested matchup week. A missing source week is not a match;
// callers without a status seam handle that test-only absence explicitly.
func projectionWeekMatches(requestedWeek, sourceWeek int) bool {
	return requestedWeek <= 0 || requestedWeek == sourceWeek
}

// projectionWeekNote explains why a requested week has no projection. The
// source week is an observed PoolStatus fact; this function deliberately does
// not speculate about an upstream future-week feed.
func projectionWeekNote(requestedWeek, sourceWeek int) string {
	if requestedWeek <= 0 || sourceWeek == requestedWeek {
		return ""
	}
	if sourceWeek <= 0 {
		return fmt.Sprintf(" · Projections unavailable for Week %d; source projection week is unavailable.", requestedWeek)
	}
	return fmt.Sprintf(" · Projections unavailable for Week %d; latest source snapshot is Week %d.", requestedWeek, sourceWeek)
}

// winProbabilityText renders A6's win-probability percentage, but only
// once both sides have at least one projectable starter (mineHasProjection,
// theirsHasProjection — see hasProjectableStarters): a side with no
// lineup at all has nothing to compare, so it renders the honest "—"
// instead of a false 50/50. This gate is deliberately independent of the
// CURRENT score's known-ness (TeamWeekLedger.Known) — pre-kickoff, every
// filled slot still carries its full weekly projection, so the win-
// probability bar has real numbers to compare well before either side's
// actual score is known (wave-8 audit item 2; previously tied to
// ScoreKnown, per the review of ae1a525, item 1, which this supersedes).
// WinProbabilityAriaLabel adapts winProbabilityText's own already-rendered
// text ("59%", or the honest "—" placeholder, winProbabilityDashText)
// into the win-probability meter's own aria-label (sumac comb re-audit
// item 5): the same plain-language sentence a sighted manager already
// reads beside the bar (app/matchups/page.gsx's own small.mono line —
// "59% to win"), restated for a screen reader on the meter itself, since
// the bar (<i style="width:...">) carries no text content of its own.
// Exported for app/matchups/page.server.go, which only ever holds the
// already-rendered text (the JSON "win_prob" field), never the raw
// floats and projection flags winProbabilityText computed it from.
func WinProbabilityAriaLabel(winProbText string) string {
	if winProbText == "" || winProbText == winProbabilityDashText {
		return "Win probability not yet known"
	}
	return winProbText + " to win"
}

// WinProbabilityAriaValue adapts win_prob_width's own literal CSS width
// (service.go's featuredMatchupMap — "59%", or the "0%" fallback for
// winProbabilityDashText) into the win-probability meter's own
// aria-valuenow (sumac comb re-audit item 5): an ARIA meter's valuenow
// must be a bare number, never a percent sign. An unparseable or
// missing width honestly reports 0, matching win_prob_width's own "0%"
// fallback for the identical not-yet-known case.
func WinProbabilityAriaValue(winProbWidth string) float64 {
	trimmed := strings.TrimSuffix(strings.TrimSpace(winProbWidth), "%")
	value, err := strconv.ParseFloat(trimmed, 64)
	if err != nil {
		return 0
	}
	return value
}

func winProbabilityText(mine, theirs float64, mineHasProjection, theirsHasProjection bool) string {
	if !mineHasProjection || !theirsHasProjection {
		return winProbabilityDashText
	}
	return fmt.Sprintf("%.0f%%", winProbability(mine, theirs)*100)
}

// projectedText renders one side's projected total, gated on hasProjection
// (see hasProjectableStarters) rather than the current score's known-ness:
// a "proj N" figure is honest as soon as the side has a lineup to project
// from, even before kickoff when the score cell itself still reads "—"
// (wave-8 audit item 2; previously tied to ScoreKnown, per the review of
// ff2a9b3, item 5, which this supersedes — that rule made every
// pre-kickoff projection blank along with the score, contradicting /team's
// own PROJECTED figure for the same lineup).
func projectedText(projected float64, hasProjection bool) string {
	if !hasProjection {
		return winProbabilityDashText
	}
	return fmt.Sprintf("%.1f", projected)
}

// projectedTotal is one side's rest-of-game projected total: every
// starter's points already on the board, plus their remaining Tank01
// weekly projection scaled by their own game's remainingFraction. hasLive
// gates whether status.Games is trusted at all — with no live status
// source wired, every row falls back to remainingFraction's known=false
// case (fraction 1) as if no game had been checked yet, exactly like a
// per-row miss.
func projectedTotal(rows []StarterLedgerRow, projections map[string]float64, status LiveStatus, hasLive bool) float64 {
	total := 0.0
	for _, row := range rows {
		total += row.Points
		projection := projections[row.PlayerID]
		if !projectionValueKnown(projection) {
			continue
		}
		// Same two corrections as starterProjectedTotal above: a finished
		// game projects nothing further, and the live lookup normalizes
		// the team abbreviation. A team total is the sum of these rows, so
		// an un-corrected row here inflates the whole side's projection
		// and, through it, its win probability.
		if row.GameFinal {
			continue
		}
		game, ok := status.Games[normalizeNFLAbbreviation(row.NFLTeam)]
		total += projection * remainingFraction(game, hasLive && ok)
	}
	return total
}

// stillToPlay counts configured starters (a row with a player assigned)
// whose NFL game has not started yet. An empty slot contributes nothing to
// either side of the "X of Y" count this backs (MatchupsData's
// still_to_play label).
//
// 2026-09-10: this used to read the live poller alone, and with a raw team
// key, which gave it both halves of the bug that showed a finished
// defense a 23.8 projection.
//
//   - A poller entry disappears once its game's window closes, and a team
//     with no entry counted as still to play. So the morning after a
//     Wednesday opener, every starter who had already finished was counted
//     as yet to take the field, and the sentence beside the score read
//     "N of M starters still to play" while naming players who were done.
//     row.GameFinal settles it instead: it is resolved from the poller AND
//     the schedule (starterGameFinal, matchup_ledger.go), so a game the
//     poller has forgotten is still known to be over.
//   - LiveStatus.Games is keyed by nflverse abbreviation while a pool
//     player carries Tank01's, so every Rams, Commanders, and Jaguars
//     starter missed the lookup and counted as still to play all game.
func stillToPlay(rows []StarterLedgerRow, status LiveStatus) int {
	count := 0
	for _, row := range rows {
		if row.PlayerID == "" {
			continue
		}
		if row.GameFinal {
			continue
		}
		game, ok := status.Games[normalizeNFLAbbreviation(row.NFLTeam)]
		if !ok || (!game.Final && !game.InProgress) {
			count++
		}
	}
	return count
}

// TeamStartersProjectedTotal (2026-09-07 truth pass, item 8) is the one
// canonical "projected total, starters only" figure a team's stat strip
// (service.go's teamData), its own matchup-preview card, and /matchups'
// featured card (featuredMatchupMap, via projectedTotal below) must all
// agree on for the same team and week — before this helper existed, the
// stat strip summed the WHOLE roster (bench included) and so read a
// bigger, wrong number than the matchup card for the same lineup.
//
// It sums each filled starting slot's own weekly projection; an empty
// slot contributes nothing. Before kickoff this is mathematically
// identical to projectedTotal's own starters-only sum: remainingFraction
// always returns 1 when no live status is known, and no starter has
// scored yet, so projectedTotal reduces to the same plain sum this
// function computes directly. TestTeamProjectedTotalHelpersAgreePreKickoff
// pins that equivalence so the two call sites can never drift back apart.
func TeamStartersProjectedTotal(lineup EffectiveLineup) float64 {
	return TeamStartersProjectedTotalWithWeek(lineup, nil)
}

// TeamStartersProjectedTotalWithWeek is TeamStartersProjectedTotal with live
// game facts: a starter whose game is final contributes the score they
// actually posted rather than a forecast of a week already played. See
// PlayerWeekFacts (lineup_projection.go) for why this exists.
func TeamStartersProjectedTotalWithWeek(lineup EffectiveLineup, facts WeekFactsFunc) float64 {
	total := 0.0
	for _, slot := range lineup.Slots {
		if !slot.HasPlayer {
			continue
		}
		if value, known := projectionContribution(slot.Player, facts); known {
			total += value
		}
	}
	return total
}

// TeamStartersProjectionKnown is the presence gate paired with
// TeamStartersProjectedTotal. A filled starter whose source projection is
// absent cannot make a complete team forecast, so callers should render the
// total as unavailable until every filled starter is known. Empty slots do
// not count as missing forecasts.
func TeamStartersProjectionKnown(lineup EffectiveLineup) bool {
	hasStarter := false
	for _, slot := range lineup.Slots {
		if !slot.HasPlayer {
			continue
		}
		hasStarter = true
		if !playerHasProjection(slot.Player) {
			return false
		}
	}
	return hasStarter
}

// starterProjections reads each row's player's weekly Tank01 projection
// from byID, keyed by PlayerID exactly as projectedTotal expects. byID is
// the caller's own single s.pool().byID read for the whole render — a
// render can build a projections map for every side of every matchup
// (round-2 review of commit 133d1d7, finding 3: about 28 calls for a
// seven-matchup week), so this takes the pool by reference instead of
// calling s.pool() (which takes poolMu) once per side. Rows with no
// matching pool entry (an empty slot, or a player the pool no longer
// carries) are simply absent from the map, which projectedTotal already
// treats as a zero projection.
func starterProjections(rows []StarterLedgerRow, byID map[string]Player) map[string]float64 {
	out := make(map[string]float64, len(rows))
	for _, row := range rows {
		if row.PlayerID == "" {
			continue
		}
		if player, ok := byID[row.PlayerID]; ok && playerHasProjection(player) {
			out[row.PlayerID] = player.Projection
		}
	}
	return out
}

// starterProjectedTotal is one starter row's own rest-of-game projected
// points — the identical per-row term projectedTotal (above) sums across
// a whole side, isolated here so the slot table's own PROJ column (A2,
// matchup redesign 2026-09-07) sums to exactly the same total the header
// card already shows for that side. byID is the caller's single
// s.pool().byID read for the whole render (the same map starterProjections
// reads), so this never needs its own intermediate projections map. An
// empty slot (no PlayerID) always projects 0.0 — the same honest zero
// projectedTotal already gives it.
func starterProjectedTotal(row StarterLedgerRow, byID map[string]Player, status LiveStatus, hasLive bool) float64 {
	if row.PlayerID == "" {
		return 0
	}
	player, ok := byID[row.PlayerID]
	if !ok || !playerHasProjection(player) {
		return row.Points
	}
	// A finished game has nothing left to project, whichever source knows
	// it is finished (starterGameFinal, matchup_ledger.go). Without this,
	// a starter whose game ended and whose live entry has since aged out
	// of the poller's window reads as points-plus-whole-projection.
	if row.GameFinal {
		return row.Points
	}
	// normalizeNFLAbbreviation: LiveStatus.Games is keyed by nflverse
	// abbreviation while a pool player carries Tank01's, so a raw lookup
	// misses every Rams, Commanders, and Jaguars starter and hands them
	// the same doubled projection.
	game, ok := status.Games[normalizeNFLAbbreviation(row.NFLTeam)]
	return row.Points + player.Projection*remainingFraction(game, hasLive && ok)
}

// starterProjectedText renders starterProjectedTotal for the slot table's
// PROJ cell. An empty slot is an honest 0.0, while a filled slot without a
// source forecast is unavailable; a numeric partial projection would make
// the row look like a zero forecast and would not reconcile with a gated
// team total.
func starterProjectedText(row StarterLedgerRow, byID map[string]Player, status LiveStatus, hasLive bool) string {
	if row.PlayerID == "" {
		return "0.0"
	}
	player, ok := byID[row.PlayerID]
	if !ok || !playerHasProjection(player) {
		return winProbabilityDashText
	}
	return fmt.Sprintf("%.1f", starterProjectedTotal(row, byID, status, hasLive))
}

// originalStarterProjectedTotal is the source forecast for one starting
// slot, without the rest-of-game adjustment projectedTotal uses while games
// are underway. Matchups presents this number as the original weekly
// projection so a manager can compare it with the final score after the
// week closes instead of watching the reference number move underneath
// them.
func originalStarterProjectedTotal(row StarterLedgerRow, byID map[string]Player) (float64, bool) {
	if row.PlayerID == "" {
		return 0, false
	}
	player, ok := byID[row.PlayerID]
	if !ok || !playerHasProjection(player) {
		return 0, false
	}
	return player.Projection, true
}

// originalStarterProjectedText is the stable counterpart to
// starterProjectedText. It deliberately ignores points and game status:
// PROJ is a pregame source forecast, not a second live score.
func originalStarterProjectedText(row StarterLedgerRow, byID map[string]Player) string {
	if row.PlayerID == "" {
		return "0.0"
	}
	value, ok := originalStarterProjectedTotal(row, byID)
	if !ok {
		return winProbabilityDashText
	}
	return fmt.Sprintf("%.1f", value)
}

// originalProjectedTotal sums the source forecast for filled starters and
// reports whether every filled starter has one. This is intentionally
// separate from projectedTotal: the latter is a useful live, rest-of-game
// estimate for win-probability heuristics, while the Matchups PROJ value is
// the stable number managers saw before kickoff and should still see after
// FINAL.
func originalProjectedTotal(rows []StarterLedgerRow, byID map[string]Player) (float64, bool) {
	total := 0.0
	hasStarter := false
	for _, row := range rows {
		if row.PlayerID == "" {
			continue
		}
		hasStarter = true
		value, ok := originalStarterProjectedTotal(row, byID)
		if !ok {
			return total, false
		}
		total += value
	}
	return total, hasStarter
}

// starterProgressSegment is one of the ten pieces shown beside a matchup.
// The first nine pieces follow the ordinary configured slots; any remaining
// slots (K/P in the reference eleven-slot roster) share the tenth piece.
// That keeps the ring legible while still requiring every configured starter
// on both sides of a matchup to finish before a piece reads complete.
type starterProgressSegment struct {
	Index         int
	Label         string
	PlayerNames   string
	Progress      int
	ProgressLabel string
	Active        bool
}

const (
	starterProgressPieceCount       = 10
	starterProgressIndividualPieces = starterProgressPieceCount - 1
)

func starterProgressQuarter(row StarterLedgerRow, status LiveStatus) int {
	if row.PlayerID == "" {
		return 0
	}
	state := strings.ToUpper(strings.TrimSpace(row.GameState))
	if state == "BYE" || state == "FINAL" || strings.HasPrefix(state, "W ") || strings.HasPrefix(state, "L ") || strings.HasPrefix(state, "T ") {
		return 4
	}
	if game, ok := status.Games[normalizeNFLAbbreviation(row.NFLTeam)]; ok {
		if game.Final {
			return 4
		}
		if game.InProgress {
			return progressQuarterFromPeriod(game.Period)
		}
	}
	// A render can carry a useful row-level state even when the live map has
	// aged the game out. Use it as a conservative fallback for the ring.
	return progressQuarterFromPeriod(state)
}

func progressQuarterFromPeriod(period string) int {
	period = strings.ToUpper(strings.TrimSpace(period))
	switch {
	case strings.HasPrefix(period, "Q1"):
		return 1
	case strings.HasPrefix(period, "Q2") || strings.HasPrefix(period, "HALF"):
		return 2
	case strings.HasPrefix(period, "Q3"):
		return 3
	case strings.HasPrefix(period, "Q4") || strings.HasPrefix(period, "OT") || strings.HasPrefix(period, "OVERTIME"):
		return 4
	default:
		return 0
	}
}

func progressLabel(progress int) string {
	switch progress {
	case 1:
		return "Through Q1"
	case 2:
		return "Through halftime"
	case 3:
		return "Through Q3"
	case 4:
		return "Complete"
	default:
		return "Not started"
	}
}

func starterProgressSegments(mineRows, theirsRows []StarterLedgerRow, status LiveStatus) []starterProgressSegment {
	out := make([]starterProgressSegment, 0, starterProgressPieceCount)
	for index := 0; index < starterProgressPieceCount; index++ {
		label := "OPEN SLOT"
		group := make([]StarterLedgerRow, 0, 4)
		if index < starterProgressIndividualPieces {
			if index < len(mineRows) {
				group = append(group, mineRows[index])
				if slot := strings.TrimSpace(mineRows[index].Slot); slot != "" {
					label = slot
				}
			}
			if index < len(theirsRows) {
				group = append(group, theirsRows[index])
				if label == "OPEN SLOT" {
					if slot := strings.TrimSpace(theirsRows[index].Slot); slot != "" {
						label = slot
					}
				}
			}
		} else {
			label = "K/P"
			if len(mineRows) > starterProgressIndividualPieces {
				group = append(group, mineRows[starterProgressIndividualPieces:]...)
			}
			if len(theirsRows) > starterProgressIndividualPieces {
				group = append(group, theirsRows[starterProgressIndividualPieces:]...)
			}
		}

		progress := 4
		active := false
		nameSet := make(map[string]bool)
		names := make([]string, 0, len(group))
		if len(group) == 0 {
			progress = 4
		} else {
			for _, row := range group {
				if strings.TrimSpace(row.Slot) != "" {
					active = true
				}
				if row.PlayerID == "" {
					progress = 0
					continue
				}
				if name := strings.TrimSpace(row.PlayerName); name != "" && !nameSet[name] {
					nameSet[name] = true
					names = append(names, name)
				}
				if quarter := starterProgressQuarter(row, status); quarter < progress {
					progress = quarter
				}
			}
		}
		if len(names) == 0 {
			names = append(names, "No player assigned")
		}
		out = append(out, starterProgressSegment{
			Index:         index,
			Label:         label,
			PlayerNames:   strings.Join(names, " + "),
			Progress:      progress,
			ProgressLabel: progressLabel(progress),
			Active:        active,
		})
	}
	return out
}

func starterProgressMark(progress, quarter int) string {
	if progress >= quarter {
		return "■"
	}
	return ""
}

func starterProgressSummary(segments []starterProgressSegment) string {
	return fmt.Sprintf("%d of %d starter pieces complete", starterProgressCompleted(segments), len(segments))
}

func starterProgressCompleted(segments []starterProgressSegment) int {
	complete := 0
	for _, segment := range segments {
		if segment.Progress >= 4 && segment.Active {
			complete++
		}
	}
	return complete
}

func starterProgressMaps(mineRows, theirsRows []StarterLedgerRow, status LiveStatus) []map[string]any {
	return starterProgressMapsForMatchup("", mineRows, theirsRows, status)
}

func starterProgressMapsForMatchup(matchupID string, mineRows, theirsRows []StarterLedgerRow, status LiveStatus) []map[string]any {
	segments := starterProgressSegments(mineRows, theirsRows, status)
	out := make([]map[string]any, 0, len(segments))
	for _, segment := range segments {
		out = append(out, map[string]any{
			"index": segment.Index, "label": segment.Label, "player_names": segment.PlayerNames,
			"progress": segment.Progress, "progress_label": segment.ProgressLabel, "active": segment.Active,
			"q1": starterProgressMark(segment.Progress, 1), "q2": starterProgressMark(segment.Progress, 2),
			"q3": starterProgressMark(segment.Progress, 3), "q4": starterProgressMark(segment.Progress, 4),
		})
		if matchupID != "" {
			out[len(out)-1]["bind_key"] = starterProgressBindKey(matchupID, segment.Index)
		}
	}
	return out
}

// starterProgressSegmentsForTeam builds one team's ten-piece progress ring.
// A matchup renders two of these rings — one for each team — so a stalled or
// completed opponent can never mask the other team's own starter progress.
// The first nine pieces follow the ordinary configured slots; the final K/P
// piece groups the special-teams slots just as the original matchup ring did.
func starterProgressSegmentsForTeam(rows []StarterLedgerRow, status LiveStatus) []starterProgressSegment {
	out := make([]starterProgressSegment, 0, starterProgressPieceCount)
	for index := 0; index < starterProgressPieceCount; index++ {
		label := "OPEN SLOT"
		group := make([]StarterLedgerRow, 0, 4)
		if index < starterProgressIndividualPieces {
			if index < len(rows) {
				group = append(group, rows[index])
				if slot := strings.TrimSpace(rows[index].Slot); slot != "" {
					label = slot
				}
			}
		} else {
			label = "K/P"
			if len(rows) > starterProgressIndividualPieces {
				group = append(group, rows[starterProgressIndividualPieces:]...)
			}
		}

		progress := 4
		active := false
		hasPlayer := false
		nameSet := make(map[string]bool)
		names := make([]string, 0, len(group))
		for _, row := range group {
			if strings.TrimSpace(row.Slot) != "" {
				active = true
			}
			if row.PlayerID == "" {
				continue
			}
			hasPlayer = true
			if name := strings.TrimSpace(row.PlayerName); name != "" && !nameSet[name] {
				nameSet[name] = true
				names = append(names, name)
			}
			quarter := starterProgressQuarter(row, status)
			if quarter < progress {
				progress = quarter
			}
		}
		if !hasPlayer {
			progress = 0
		}
		if len(names) == 0 {
			names = append(names, "No player assigned")
		}
		out = append(out, starterProgressSegment{
			Index:         index,
			Label:         label,
			PlayerNames:   strings.Join(names, " + "),
			Progress:      progress,
			ProgressLabel: progressLabel(progress),
			Active:        active,
		})
	}
	return out
}

func starterProgressMapsForTeam(matchupID, side string, rows []StarterLedgerRow, status LiveStatus) []map[string]any {
	segments := starterProgressSegmentsForTeam(rows, status)
	out := make([]map[string]any, 0, len(segments))
	for _, segment := range segments {
		bindKey := starterProgressTeamBindKey(matchupID, side, segment.Index)
		out = append(out, map[string]any{
			"index": segment.Index, "label": segment.Label, "player_names": segment.PlayerNames,
			"progress": segment.Progress, "progress_label": segment.ProgressLabel, "active": segment.Active,
			"bind_key": bindKey,
			"q1":       starterProgressMark(segment.Progress, 1), "q2": starterProgressMark(segment.Progress, 2),
			"q3": starterProgressMark(segment.Progress, 3), "q4": starterProgressMark(segment.Progress, 4),
		})
	}
	return out
}

func starterProgressMapsForTeams(matchupID string, firstRows []StarterLedgerRow, firstSide string, secondRows []StarterLedgerRow, secondSide string, status LiveStatus) map[string]any {
	return map[string]any{
		firstSide:  starterProgressMapsForTeam(matchupID, firstSide, firstRows, status),
		secondSide: starterProgressMapsForTeam(matchupID, secondSide, secondRows, status),
	}
}

func starterProgressSummariesForTeams(firstRows []StarterLedgerRow, firstSide string, secondRows []StarterLedgerRow, secondSide string, status LiveStatus) map[string]string {
	return map[string]string{
		firstSide:  starterProgressSummary(starterProgressSegmentsForTeam(firstRows, status)),
		secondSide: starterProgressSummary(starterProgressSegmentsForTeam(secondRows, status)),
	}
}

func starterProgressTeamBindKey(matchupID, side string, index int) string {
	return strings.TrimSpace(matchupID) + "-" + strings.TrimSpace(side) + "-" + strconv.Itoa(index)
}

func starterProgressBindKey(matchupID string, index int) string {
	return strings.TrimSpace(matchupID) + "-" + strconv.Itoa(index)
}

// stillToPlaySentence renders A6's still-to-play line in plain words (A1,
// matchup redesign 2026-09-07), retiring the fixed "N of M starters still
// to play" jargon for a sentence that reads naturally at both ends of the
// range: every starter yet to play, every starter already played, or the
// honest count in between. total 0 (no configured starters at all) reads
// the same as "every starter has played" — there is nothing left to wait
// for either way.
func stillToPlaySentence(stillToPlay, total int) string {
	switch {
	case stillToPlay <= 0:
		return "Every starter has played"
	case stillToPlay >= total:
		return fmt.Sprintf("All %d starters yet to play", total)
	default:
		return fmt.Sprintf("%d of %d starters still to play", stillToPlay, total)
	}
}

// playerProjectionText renders a bench player's source weekly forecast.
// Bench rows do not participate in the matchup total, but an omitted source
// forecast still must not look like a real 0.0 projection.
func playerProjectionText(player Player) string {
	if !playerHasProjection(player) {
		return winProbabilityDashText
	}
	return fmt.Sprintf("%.1f", player.Projection)
}
