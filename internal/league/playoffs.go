package league

import (
	"fmt"
	"sort"
	"time"
)

// PlayoffConfig configures one league's playoff bracket (section 3.1).
type PlayoffConfig struct {
	TeamCount            int      `json:"teamCount"`
	StartWeek            int      `json:"startWeek"`
	RoundLengthWeeks     int      `json:"roundLengthWeeks"`
	Qualification        string   `json:"qualification,omitempty"`
	TiebreakOrder        []string `json:"tiebreakOrder,omitempty"`
	Byes                 int      `json:"byes,omitempty"`
	DivisionWinnersFirst bool     `json:"divisionWinnersFirst"`
	Reseed               bool     `json:"reseed"`
	Consolation          bool     `json:"consolation"`
	ToiletBowl           bool     `json:"toiletBowl"`
}

// PlayoffSeed is one team's playoff position (section 3.4).
type PlayoffSeed struct {
	Seed                int    `json:"seed"`
	TeamID              string `json:"teamId"`
	Source              string `json:"source"` // "division-winner" | "wildcard" | "field"
	TieBreakExplanation string `json:"tieBreakExplanation,omitempty"`
}

// PlayoffMatchup is one bracket slot, in any round, in any of the three
// brackets (section 3.4).
type PlayoffMatchup struct {
	ID                  string                   `json:"id"`
	Bracket             string                   `json:"bracket"` // "championship" | "consolation" | "toilet"
	Round               int                      `json:"round"`   // 1-based, within its own bracket
	Week                int                      `json:"week"`    // first NFL week of the round
	HomeSeed            int                      `json:"homeSeed"`
	AwaySeed            int                      `json:"awaySeed"` // 0 = bye
	HomeTeamID          string                   `json:"homeTeamId"`
	AwayTeamID          string                   `json:"awayTeamId"` // "" = bye
	HomeScore           float64                  `json:"homeScore"`  // summed across round weeks
	AwayScore           float64                  `json:"awayScore"`
	WinnerTeamID        string                   `json:"winnerTeamId"`
	Final               bool                     `json:"final"`
	TieBreakExplanation string                   `json:"tieBreakExplanation,omitempty"`
	ResultProvenance    *PlayoffResultProvenance `json:"resultProvenance,omitempty"`
}

// PlayoffState is the persisted bracket: config, seeds, every matchup
// across every bracket and round, and the season's sealed outcomes
// (section 3.4, 3.5).
type PlayoffState struct {
	Config         PlayoffConfig       `json:"config"`
	Seeds          []PlayoffSeed       `json:"seeds"`
	Matchups       []PlayoffMatchup    `json:"matchups"`
	ChampionTeamID string              `json:"championTeamId"`
	RunnerUpTeamID string              `json:"runnerUpTeamId"`
	ToiletTeamID   string              `json:"toiletTeamId,omitempty"`
	Status         string              `json:"status,omitempty"`
	PreviewID      string              `json:"previewId,omitempty"`
	Revision       int                 `json:"revision,omitempty"`
	PreviewedAt    time.Time           `json:"previewedAt,omitempty"`
	PublishedAt    time.Time           `json:"publishedAt,omitempty"`
	Provenance     PlayoffProvenance   `json:"provenance,omitempty"`
	Audit          []PlayoffAuditEntry `json:"audit,omitempty"`
}

// clonePlayoffState deep-copies state (nil-safe), matching the store's
// snapshot/clone discipline.
func clonePlayoffState(state *PlayoffState) *PlayoffState {
	if state == nil {
		return nil
	}
	out := *state
	out.Seeds = append([]PlayoffSeed(nil), state.Seeds...)
	out.Matchups = append([]PlayoffMatchup(nil), state.Matchups...)
	out.Config.TiebreakOrder = append([]string(nil), state.Config.TiebreakOrder...)
	out.Provenance = clonePlayoffProvenance(state.Provenance)
	out.Audit = append([]PlayoffAuditEntry(nil), state.Audit...)
	for i := range out.Matchups {
		if state.Matchups[i].ResultProvenance != nil {
			result := *state.Matchups[i].ResultProvenance
			out.Matchups[i].ResultProvenance = &result
		}
	}
	return &out
}

// ValidatePlayoffConfig enforces section 3.1's validation table plus the
// section 3.1 cross-checks: teamCount 2-8 and <= the league's team count;
// startWeek strictly after the regular season's start week; roundLengthWeeks
// is 1 or 2; divisionWinnersFirst requires divisions to exist and
// teamCount >= divisionCount; and the bracket (startWeek plus every round)
// fits inside seasonFinalWeek.
func ValidatePlayoffConfig(cfg PlayoffConfig, leagueTeamCount, divisionCount, seasonStartWeek, seasonFinalWeek int) error {
	if cfg.TeamCount < 2 || cfg.TeamCount > 8 {
		return fmt.Errorf("playoffs.teamCount must be between 2 and 8")
	}
	if cfg.TeamCount > leagueTeamCount {
		return fmt.Errorf("playoffs.teamCount (%d) exceeds the league size (%d)", cfg.TeamCount, leagueTeamCount)
	}
	if cfg.RoundLengthWeeks != 1 && cfg.RoundLengthWeeks != 2 {
		return fmt.Errorf("playoffs.roundLengthWeeks must be 1 or 2")
	}
	if cfg.StartWeek <= seasonStartWeek {
		return fmt.Errorf("playoffs.startWeek must be after season.startWeek")
	}
	if cfg.DivisionWinnersFirst && divisionCount == 0 {
		return fmt.Errorf("playoffs.divisionWinnersFirst requires divisions to exist")
	}
	if cfg.DivisionWinnersFirst && cfg.TeamCount < divisionCount {
		return fmt.Errorf("playoffs.divisionWinnersFirst requires teamCount (%d) >= division count (%d)", cfg.TeamCount, divisionCount)
	}
	rounds := log2(nextPow2(cfg.TeamCount))
	end := cfg.StartWeek + rounds*cfg.RoundLengthWeeks - 1
	if end > seasonFinalWeek {
		return fmt.Errorf("the playoff bracket (rounds ending week %d) runs past season.finalWeek (%d)", end, seasonFinalWeek)
	}
	return nil
}

// nextPow2 returns the smallest power of two >= n (n >= 1).
func nextPow2(n int) int {
	if n < 1 {
		return 1
	}
	p := 1
	for p < n {
		p *= 2
	}
	return p
}

// log2 returns the smallest r such that 2^r >= n (n >= 1).
func log2(n int) int {
	r := 0
	for (1 << r) < n {
		r++
	}
	return r
}

// SeedPlayoffs computes playoff seeds from final regular-season standings
// (section 3.2). standings must already carry Rank (ComputeStandings'
// output); ties are pre-resolved by the tiebreaker chain that produced it.
func SeedPlayoffs(standings []Standing, divisions map[string]string, cfg PlayoffConfig) ([]PlayoffSeed, error) {
	if cfg.TeamCount < 2 || cfg.TeamCount > 8 {
		return nil, fmt.Errorf("playoffs.teamCount must be between 2 and 8")
	}
	if cfg.TeamCount > len(standings) {
		return nil, fmt.Errorf("playoffs.teamCount (%d) exceeds the league size (%d)", cfg.TeamCount, len(standings))
	}
	ordered := append([]Standing(nil), standings...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].Rank != ordered[j].Rank {
			return ordered[i].Rank < ordered[j].Rank
		}
		return ordered[i].TeamID < ordered[j].TeamID
	})

	seeds := make([]PlayoffSeed, 0, cfg.TeamCount)
	if cfg.DivisionWinnersFirst && len(divisions) > 0 {
		seenDivision := map[string]bool{}
		isWinner := map[string]bool{}
		for _, st := range ordered {
			div := divisions[st.TeamID]
			if div == "" || seenDivision[div] {
				continue
			}
			seenDivision[div] = true
			isWinner[st.TeamID] = true
			seeds = append(seeds, PlayoffSeed{TeamID: st.TeamID, Source: "division-winner", TieBreakExplanation: standingSeedExplanation(st)})
			if len(seeds) == cfg.TeamCount {
				break
			}
		}
		if len(seeds) < cfg.TeamCount {
			for _, st := range ordered {
				if isWinner[st.TeamID] {
					continue
				}
				seeds = append(seeds, PlayoffSeed{TeamID: st.TeamID, Source: "wildcard", TieBreakExplanation: standingSeedExplanation(st)})
				if len(seeds) == cfg.TeamCount {
					break
				}
			}
		}
	} else {
		for _, st := range ordered {
			seeds = append(seeds, PlayoffSeed{TeamID: st.TeamID, Source: "field", TieBreakExplanation: standingSeedExplanation(st)})
			if len(seeds) == cfg.TeamCount {
				break
			}
		}
	}
	for i := range seeds {
		seeds[i].Seed = i + 1
	}
	return seeds, nil
}

// nonPlayoffToiletSeeds builds the toilet bracket's inverse-seeded field
// (section 3.4): the worst-ranked non-playoff team gets toilet seed 1 (and
// any bye), down to the best non-playoff team at the last seed.
func nonPlayoffToiletSeeds(standings []Standing, teamCount int) []PlayoffSeed {
	ordered := append([]Standing(nil), standings...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Rank < ordered[j].Rank })
	if teamCount >= len(ordered) {
		return nil
	}
	nonPlayoff := ordered[teamCount:] // best-to-worst among non-playoff teams
	n := len(nonPlayoff)
	seeds := make([]PlayoffSeed, n)
	for i := 0; i < n; i++ {
		team := nonPlayoff[n-1-i] // i=0 -> the worst non-playoff team
		seeds[i] = PlayoffSeed{Seed: i + 1, TeamID: team.TeamID, Source: "field"}
	}
	return seeds
}

// pairBracketRound1 builds round 1 of one bracket from its seed list
// (section 3.2 step 4, 3.4): seed k pairs against bracketSize+1-k; a
// pairing against a missing seed (bracketSize > len(seeds)) is a bye and
// advances immediately.
func pairBracketRound1(seeds []PlayoffSeed, bracket string, week int) ([]PlayoffMatchup, error) {
	bracketSize := nextPow2(len(seeds))
	bySeed := make(map[int]PlayoffSeed, len(seeds))
	for _, s := range seeds {
		bySeed[s.Seed] = s
	}
	out := make([]PlayoffMatchup, 0, bracketSize/2)
	for k := 1; k <= bracketSize/2; k++ {
		homeNum, awayNum := k, bracketSize+1-k
		home, ok := bySeed[homeNum]
		if !ok {
			return nil, fmt.Errorf("internal error: seed %d missing for bracket %q", homeNum, bracket)
		}
		m := PlayoffMatchup{
			ID:         fmt.Sprintf("%s-r1-s%d-s%d", bracket, homeNum, awayNum),
			Bracket:    bracket,
			Round:      1,
			Week:       week,
			HomeSeed:   homeNum,
			HomeTeamID: home.TeamID,
		}
		if away, ok := bySeed[awayNum]; ok {
			m.AwaySeed = awayNum
			m.AwayTeamID = away.TeamID
		} else {
			m.Final = true
			m.WinnerTeamID = home.TeamID
			m.TieBreakExplanation = fmt.Sprintf("bye: seed %d advances without an opponent", homeNum)
		}
		out = append(out, m)
	}
	return out, nil
}

// roundWeek resolves round's first NFL week from the championship
// bracket's config. Every bracket (championship, consolation, toilet)
// shares the same weekly calendar.
func roundWeek(cfg PlayoffConfig, round int) int {
	return cfg.StartWeek + (round-1)*cfg.RoundLengthWeeks
}

// GeneratePlayoffState builds the initial bracket from final regular-season
// standings: seeds, championship round 1 (with byes resolved immediately),
// and — when enabled — the toilet bracket's round 1. The consolation
// bracket has no participants yet (it seeds from championship round-1
// losers, section 3.4); AdvancePlayoffRound creates it once round 1
// completes.
func GeneratePlayoffState(standings []Standing, divisions map[string]string, cfg PlayoffConfig) (PlayoffState, error) {
	if cfg.TeamCount < 2 || cfg.TeamCount > 8 {
		return PlayoffState{}, fmt.Errorf("playoffs.teamCount must be between 2 and 8")
	}
	if cfg.TeamCount > len(standings) {
		return PlayoffState{}, fmt.Errorf("playoffs.teamCount (%d) exceeds the league size (%d)", cfg.TeamCount, len(standings))
	}
	seeds, err := SeedPlayoffs(standings, divisions, cfg)
	if err != nil {
		return PlayoffState{}, err
	}
	champRound1, err := pairBracketRound1(seeds, "championship", roundWeek(cfg, 1))
	if err != nil {
		return PlayoffState{}, err
	}
	state := PlayoffState{Config: cfg, Seeds: seeds, Matchups: champRound1}

	if cfg.ToiletBowl {
		toiletSeeds := nonPlayoffToiletSeeds(standings, cfg.TeamCount)
		if len(toiletSeeds) >= 2 {
			toiletRound1, err := pairBracketRound1(toiletSeeds, "toilet", roundWeek(cfg, 1))
			if err != nil {
				return PlayoffState{}, err
			}
			state.Matchups = append(state.Matchups, toiletRound1...)
		}
	}
	return state, nil
}

// PlayoffRoundResult carries one matchup's final round score plus the
// tiebreak input (best single week within the round) AdvancePlayoffRound
// needs when the summed round score ties (section 3.4: "a tied playoff
// score resolves by the higher single best week within the round, then by
// seed").
type PlayoffRoundResult struct {
	MatchupID     string
	HomeScore     float64
	AwayScore     float64
	HomeBestWeek  float64
	AwayBestWeek  float64
	Final         bool
	Authoritative bool
	SourceState   string
	Source        string
	ObservedAt    time.Time
}

// AdvancePlayoffRound applies a round's results to state: marks matchups
// final, resolves winners, and — once every matchup in a round is final —
// generates the next round (reseeded or fixed, per state.Config.Reseed),
// spawns the consolation bracket from championship round-1 losers when
// state.Config.Consolation, and, at each bracket's own final round,
// records ChampionTeamID / RunnerUpTeamID / ToiletTeamID. It is a pure
// function; the caller persists the result. Re-applying results for
// matchups that are already final is a no-op (idempotent replay).
func AdvancePlayoffRound(state PlayoffState, results []PlayoffRoundResult) (PlayoffState, error) {
	out := *clonePlayoffState(&state)
	byID := make(map[string]int, len(out.Matchups))
	for i, m := range out.Matchups {
		byID[m.ID] = i
	}
	for _, res := range results {
		idx, ok := byID[res.MatchupID]
		if !ok {
			return PlayoffState{}, fmt.Errorf("unknown playoff matchup %q", res.MatchupID)
		}
		m := &out.Matchups[idx]
		if m.Final {
			continue
		}
		if m.HomeTeamID == "" || m.AwayTeamID == "" {
			return PlayoffState{}, fmt.Errorf("matchup %q has no opponent to score", res.MatchupID)
		}
		m.HomeScore = res.HomeScore
		m.AwayScore = res.AwayScore
		m.Final = true
		m.WinnerTeamID = resolvePlayoffWinner(*m, res)
		m.TieBreakExplanation = playoffWinnerExplanation(*m, res)
		m.ResultProvenance = playoffResultProvenance(res)
	}
	for _, bracket := range presentBrackets(out.Matchups) {
		advanceBracket(&out, bracket)
	}
	return out, nil
}

// resolvePlayoffWinner applies section 3.4's tie chain: higher round
// score, then higher single best week within the round, then the better
// (lower-numbered) seed.
func resolvePlayoffWinner(m PlayoffMatchup, res PlayoffRoundResult) string {
	if m.HomeScore != m.AwayScore {
		if m.HomeScore > m.AwayScore {
			return m.HomeTeamID
		}
		return m.AwayTeamID
	}
	if res.HomeBestWeek != res.AwayBestWeek {
		if res.HomeBestWeek > res.AwayBestWeek {
			return m.HomeTeamID
		}
		return m.AwayTeamID
	}
	if m.HomeSeed <= m.AwaySeed {
		return m.HomeTeamID
	}
	return m.AwayTeamID
}

func presentBrackets(matchups []PlayoffMatchup) []string {
	seen := map[string]bool{}
	out := make([]string, 0, 3)
	for _, m := range matchups {
		if !seen[m.Bracket] {
			seen[m.Bracket] = true
			out = append(out, m.Bracket)
		}
	}
	return out
}

func matchupsForRound(matchups []PlayoffMatchup, bracket string, round int) []PlayoffMatchup {
	out := make([]PlayoffMatchup, 0)
	for _, m := range matchups {
		if m.Bracket == bracket && m.Round == round {
			out = append(out, m)
		}
	}
	return out
}

func allFinal(matchups []PlayoffMatchup) bool {
	if len(matchups) == 0 {
		return false
	}
	for _, m := range matchups {
		if !m.Final {
			return false
		}
	}
	return true
}

// advanceBracket checks bracket's current (highest-numbered) round; when
// every matchup in it is final, it either finalizes the bracket (its last
// round just completed) or generates the next round. It also spawns the
// consolation bracket right after championship round 1 completes.
func advanceBracket(state *PlayoffState, bracket string) {
	maxRound := 0
	for _, m := range state.Matchups {
		if m.Bracket == bracket && m.Round > maxRound {
			maxRound = m.Round
		}
	}
	if maxRound == 0 {
		return
	}
	current := matchupsForRound(state.Matchups, bracket, maxRound)
	if !allFinal(current) {
		return
	}
	totalRounds := bracketTotalRounds(state, bracket)
	if maxRound >= totalRounds {
		finalizeBracket(state, bracket, current)
	} else if len(matchupsForRound(state.Matchups, bracket, maxRound+1)) == 0 {
		next := nextRoundMatchups(current, bracket, maxRound+1, roundWeek(state.Config, maxRound+1), state.Config.Reseed, bracket == "toilet")
		state.Matchups = append(state.Matchups, next...)
	}
	if bracket == "championship" && maxRound == 1 && state.Config.Consolation {
		if len(matchupsForRound(state.Matchups, "consolation", 1)) == 0 {
			losers := round1Losers(current)
			if len(losers) >= 2 {
				consSeeds := reseedBySeedAscending(losers)
				if consRound1, err := pairBracketRound1(consSeeds, "consolation", roundWeek(state.Config, 2)); err == nil {
					state.Matchups = append(state.Matchups, consRound1...)
				}
			}
		}
	}
}

// bracketTotalRounds resolves how many rounds bracket needs to reach a
// single champion. The championship bracket size comes from
// state.Config.TeamCount; the toilet and consolation brackets size
// themselves off their own round-1 matchup count, since neither has a
// dedicated config field.
func bracketTotalRounds(state *PlayoffState, bracket string) int {
	if bracket == "championship" {
		return log2(nextPow2(state.Config.TeamCount))
	}
	round1 := matchupsForRound(state.Matchups, bracket, 1)
	if len(round1) == 0 {
		return 0
	}
	return log2(len(round1) * 2)
}

// finalizeBracket records the bracket's sealed outcome once its last round
// (exactly one matchup) is final. Section 3.5: the champion (and toilet
// loser) are only ever recorded here, when the deciding matchup is final.
func finalizeBracket(state *PlayoffState, bracket string, finalRound []PlayoffMatchup) {
	if len(finalRound) != 1 {
		return
	}
	m := finalRound[0]
	loser := m.AwayTeamID
	if m.WinnerTeamID == m.AwayTeamID {
		loser = m.HomeTeamID
	}
	switch bracket {
	case "championship":
		state.ChampionTeamID = m.WinnerTeamID
		state.RunnerUpTeamID = loser
	case "toilet":
		// "Losers advance": the team that lost every round is the one
		// that lost this final matchup too, not the one that won it.
		state.ToiletTeamID = loser
	}
}

// round1Losers extracts the losing seed/team from every real (non-bye)
// round-1 matchup, for consolation-bracket seeding.
func round1Losers(round1 []PlayoffMatchup) []PlayoffSeed {
	out := make([]PlayoffSeed, 0, len(round1))
	for _, m := range round1 {
		if m.AwayTeamID == "" {
			continue // bye: no loser
		}
		seed, team := m.AwaySeed, m.AwayTeamID
		if m.WinnerTeamID == m.AwayTeamID {
			seed, team = m.HomeSeed, m.HomeTeamID
		}
		out = append(out, PlayoffSeed{Seed: seed, TeamID: team, Source: "field"})
	}
	return out
}

// reseedBySeedAscending renumbers seeds 1..N in ascending original-seed
// order, for handing a sub-population (round-1 losers) to
// pairBracketRound1 as its own self-contained bracket.
func reseedBySeedAscending(seeds []PlayoffSeed) []PlayoffSeed {
	ordered := append([]PlayoffSeed(nil), seeds...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Seed < ordered[j].Seed })
	for i := range ordered {
		ordered[i].Seed = i + 1
	}
	return ordered
}

// nextRoundMatchups builds bracket's round (round > 1) from the previous
// round's now-final matchups. When reseed is true, every advancing team is
// sorted by its ORIGINAL seed and paired best-vs-worst (mirroring round
// 1's k-vs-(n+1-k) pattern); when false, adjacent previous-round matchups'
// advancers pair together (fixed bracket progression). advanceLosers
// flips who advances from a decided (non-bye) matchup: the winner for the
// championship and consolation brackets, the loser for the toilet bracket
// ("losers advance," section 3.4).
func nextRoundMatchups(prevRound []PlayoffMatchup, bracket string, round, week int, reseed, advanceLosers bool) []PlayoffMatchup {
	type advancer struct {
		seed int
		team string
	}
	advancers := make([]advancer, 0, len(prevRound))
	for _, m := range prevRound {
		team, seed := m.WinnerTeamID, m.HomeSeed
		if m.WinnerTeamID != m.HomeTeamID {
			seed = m.AwaySeed
		}
		if advanceLosers && m.AwayTeamID != "" {
			if m.WinnerTeamID == m.HomeTeamID {
				team, seed = m.AwayTeamID, m.AwaySeed
			} else {
				team, seed = m.HomeTeamID, m.HomeSeed
			}
		}
		advancers = append(advancers, advancer{seed: seed, team: team})
	}
	if reseed {
		sort.Slice(advancers, func(i, j int) bool { return advancers[i].seed < advancers[j].seed })
	}
	n := len(advancers)
	out := make([]PlayoffMatchup, 0, n/2)
	for i := 0; i < n/2; i++ {
		home, away := advancers[2*i], advancers[2*i+1]
		if reseed {
			home, away = advancers[i], advancers[n-1-i]
		}
		out = append(out, PlayoffMatchup{
			ID:         fmt.Sprintf("%s-r%d-s%d-s%d", bracket, round, home.seed, away.seed),
			Bracket:    bracket,
			Round:      round,
			Week:       week,
			HomeSeed:   home.seed,
			AwaySeed:   away.seed,
			HomeTeamID: home.team,
			AwayTeamID: away.team,
		})
	}
	return out
}
