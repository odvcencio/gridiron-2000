package league

import (
	"fmt"
	"strings"
)

// TeamMatchup is one team's fantasy-matchup difficulty for a given
// position: how tough that team is to face there this season, under
// this league's own scoring rules. Rank 1 is the toughest matchup on the
// board (for an offensive position: the stingiest defense; for DST: the
// most potent opposing offense); Rank Total is the softest/most
// generous. Tier is one of "difficult" | "neutral" | "favorable",
// precomputed by the ranker (internal/matchup) so a template never has
// to bucket a raw number itself. SourceLabel honestly names the season
// the rank was computed from (design point 4: "2025 season" before the
// current season has enough weeks, "2026 thru wk N" once it does) —
// every chip and tooltip that shows a rank also shows this label.
type TeamMatchup struct {
	Rank        int
	Total       int
	Tier        string
	SourceLabel string
}

// MatchupSource resolves opponent's matchup difficulty against position:
// for QB/RB/WR/TE, how many fantasy points opponent's defense allows at
// that position; for DST, how many fantasy points opponent's own
// offense scores — both computed under this league's own scoring rules
// from a full season of stats (see internal/matchup and main.go's
// matchup-rank cache), never a generic yards-based formula. ok is false
// when no rank is available for opponent/position (the cache has not
// computed yet, or the team carries no ranked sample) — the caller must
// render the opponent alone, never a fabricated rank. Wired once at
// boot via SetMatchupSource, mirroring SetBlitzPre1Source's seam.
type MatchupSource func(opponent, position string) (TeamMatchup, bool)

// SetMatchupSource attaches the matchup-rank cache (main.go) plus the
// honest season-source label the whole snapshot was computed from
// (design point 4: "2025 season" or "2026 thru wk N") — every page that
// shows a rank chip also shows this label once, at the page level (see
// MatchupSourceLabel), so nobody mistakes a stale or off-season rank for
// a current one. Call it once during startup and again on every refresh.
// Never calling it is an honest degrade: every player row still shows
// its opponent, just no rank chip — the same "opponent alone, never
// fabricated" rule a lookup miss follows.
func (s *Service) SetMatchupSource(source MatchupSource, sourceLabel string) {
	s.poolMu.Lock()
	s.matchupFn = source
	s.matchupLabel = sourceLabel
	s.poolMu.Unlock()
}

// matchupSource returns the currently attached MatchupSource, or nil.
func (s *Service) matchupSource() MatchupSource {
	s.poolMu.Lock()
	defer s.poolMu.Unlock()
	return s.matchupFn
}

// MatchupSourceLabel returns the matchup-rank cache's currently active
// season-source label and whether one is set at all. Every surface that
// renders a matchup chip calls this once per render (page-level banner),
// alongside matchupIndexFor's per-row lookups.
func (s *Service) MatchupSourceLabel() (string, bool) {
	s.poolMu.Lock()
	defer s.poolMu.Unlock()
	return s.matchupLabel, s.matchupLabel != ""
}

// matchupIndex resolves each pool player's live opponent for one render
// (from the league's own schedule mirror, or a Preseason Blitz slate —
// see matchupIndexFor/matchupIndexForSlate) and, when a ranking source
// is wired, that opponent's matchup difficulty. Built once per request,
// mirroring currentScoringValues' one-snapshot-per-render rule, and
// threaded through playerMap so a page rendering hundreds of rows never
// repeats the schedule scan or the ranking lookup per row. The zero
// value is safe to use (resolve == nil): every player renders with no
// opponent and no matchup, the same honest empty state a lookup miss
// produces.
type matchupIndex struct {
	resolve func(nflTeam string) (opponent string, home bool, ok bool)
	source  MatchupSource
}

// matchupIndexFor builds a regular-season matchupIndex: each player's
// opponent in week, resolved from games (s.schedule()). A nil ranking
// source (the cache has not computed yet) still resolves opponents;
// only the rank half degrades honestly.
func (s *Service) matchupIndexFor(games []GameInfo, week int) matchupIndex {
	return matchupIndex{
		resolve: func(nflTeam string) (string, bool, bool) { return opponentInWeek(games, nflTeam, week) },
		source:  s.matchupSource(),
	}
}

// matchupIndexForSlate builds a Preseason Blitz matchupIndex: each
// player's opponent in the current slate's games (already filtered to
// one slate — a Preseason Blitz slate carries no separate week number).
// The same season-long defense/offense ranks apply; TeamMatchup.
// SourceLabel keeps that honestly labelled as regular-season data even
// though the opponent itself is a preseason game.
func (s *Service) matchupIndexForSlate(games []BlitzGame) matchupIndex {
	return matchupIndex{
		resolve: func(nflTeam string) (string, bool, bool) { return opponentInSlate(games, nflTeam) },
		source:  s.matchupSource(),
	}
}

// opponentInWeek finds nflTeam's game in week and reports the other
// team plus whether nflTeam is at home. false means no scheduled game
// for that team/week — a bye, an unscheduled future week, or a missing
// schedule mirror — and the caller must render no opponent at all
// rather than guess.
func opponentInWeek(games []GameInfo, nflTeam string, week int) (opponent string, home bool, ok bool) {
	team := strings.ToUpper(strings.TrimSpace(nflTeam))
	if team == "" {
		return "", false, false
	}
	for _, g := range games {
		if g.Week != week {
			continue
		}
		gHome, gAway := strings.ToUpper(g.Home), strings.ToUpper(g.Away)
		if team == gHome {
			return gAway, true, true
		}
		if team == gAway {
			return gHome, false, true
		}
	}
	return "", false, false
}

// opponentInSlate mirrors opponentInWeek for a Preseason Blitz slate's
// games, which carry no Week field (one slate is one week by
// definition, so every game in the list is already "this week").
func opponentInSlate(games []BlitzGame, nflTeam string) (opponent string, home bool, ok bool) {
	team := strings.ToUpper(strings.TrimSpace(nflTeam))
	if team == "" {
		return "", false, false
	}
	for _, g := range games {
		gHome, gAway := strings.ToUpper(g.Home), strings.ToUpper(g.Away)
		if team == gHome {
			return gAway, true, true
		}
		if team == gAway {
			return gHome, false, true
		}
	}
	return "", false, false
}

// matchupTierWord renders TeamMatchup.Tier as the plain-English word the
// tooltip's parenthetical uses. The always-visible row chip carries the
// tier only as a data-matchup-tier class (see playerMap/styles.css), not
// this word, to keep the row compact (owner directive: "favor a short
// chip over a sentence").
func matchupTierWord(tier string) string {
	switch tier {
	case "favorable":
		return "soft"
	case "difficult":
		return "tough"
	default:
		return "even"
	}
}

// ordinal renders n as "1st", "2nd", "3rd", "4th", ... with the English
// 11th/12th/13th exception.
func ordinal(n int) string {
	if n <= 0 {
		return fmt.Sprintf("%d", n)
	}
	if n%100 >= 11 && n%100 <= 13 {
		return fmt.Sprintf("%dth", n)
	}
	switch n % 10 {
	case 1:
		return fmt.Sprintf("%dst", n)
	case 2:
		return fmt.Sprintf("%dnd", n)
	case 3:
		return fmt.Sprintf("%drd", n)
	default:
		return fmt.Sprintf("%dth", n)
	}
}

// matchupChipText renders the always-visible row chip. "31st-toughest"
// is unambiguous even with no color and regardless of which side of the
// ball the rank describes: a low number always reads as tough, a high
// number always reads as soft (design point 3's own suggested phrasing).
func matchupChipText(m TeamMatchup) string {
	return ordinal(m.Rank) + "-toughest"
}

// matchupDetailText renders the stat-tip tooltip's fuller line: the same
// rank, its plain-English tier word, what it is ranked against, and the
// honest season-source label (design point 4) — every rank the chip
// shows also carries this provenance somewhere on the page.
func matchupDetailText(position string, m TeamMatchup) string {
	subject := "vs " + position
	if position == "DST" {
		subject = "offense on the schedule"
	}
	return fmt.Sprintf("%s-toughest of %d %s (%s) — ranked from the %s", ordinal(m.Rank), m.Total, subject, matchupTierWord(m.Tier), m.SourceLabel)
}

// matchupSkipsRank reports whether position never carries a matchup
// rank at all: K and P (design directive — "I don't think it matters
// much... inventing a weak signal would be worse than none"). The
// opponent itself still renders for these positions; only the rank half
// is skipped.
func matchupSkipsRank(position string) bool {
	return position == "K" || position == "P"
}

// fields renders one player's matchup view-model fields for playerMap:
// the opponent (from resolve, falling back to the player's own static
// Opponent field for the demo/offline pool, which carries no live
// schedule mirror to resolve against), and, when a ranking source is
// wired and the position is eligible (matchupSkipsRank excludes K/P),
// the matchup chip/tier/detail. Every branch that cannot resolve
// something renders its honest empty state rather than a zero or a
// guess (missing schedule, bye week, K/P, no ranking source, no ranked
// sample for this team) — see design point 5.
func (m matchupIndex) fields(player Player) map[string]any {
	rawOpponent := ""
	home := true
	hasOpponent := false
	if m.resolve != nil {
		if resolved, isHome, ok := m.resolve(player.NFLTeam); ok {
			rawOpponent, home, hasOpponent = resolved, isHome, true
		}
	}
	opponentDisplay := player.Opponent
	if hasOpponent {
		prefix := "vs "
		if !home {
			prefix = "@ "
		}
		opponentDisplay = prefix + rawOpponent
	} else if opponentDisplay != "" {
		hasOpponent = true
	}
	out := map[string]any{
		"opponent":       opponentDisplay,
		"has_opponent":   hasOpponent,
		"has_matchup":    false,
		"matchup_tier":   "",
		"matchup_chip":   "",
		"matchup_detail": "",
	}
	if rawOpponent == "" || m.source == nil || matchupSkipsRank(player.Position) {
		return out
	}
	rank, ok := m.source(rawOpponent, player.Position)
	if !ok || rank.Rank <= 0 || rank.Total <= 0 {
		return out
	}
	out["has_matchup"] = true
	out["matchup_tier"] = rank.Tier
	out["matchup_chip"] = matchupChipText(rank)
	out["matchup_detail"] = matchupDetailText(player.Position, rank)
	return out
}
