package league

import (
	"fmt"
	"math"
	"strings"
	"time"
)

type pickemLiveView struct {
	State        string
	ScoreDisplay string
	HasScores    bool
	AwayScore    int
	HomeScore    int
	InProgress   bool
}

// pickemGameLiveView reads the existing week-scoped poller snapshot. Live
// scores are presentation only; pickemSchedule remains the final authority.
func pickemGameLiveView(game GameInfo, status LiveStatus, now time.Time) pickemLiveView {
	view := pickemLiveView{State: "SCHEDULED"}
	if game.Final {
		view.State = "FINAL"
		view.HasScores = game.ScoresPresent
		view.AwayScore, view.HomeScore = game.AwayScore, game.HomeScore
	} else {
		if !game.Kickoff.IsZero() && !now.Before(game.Kickoff) {
			view.State = "AWAITING LIVE SCORE"
		}
		live, ok := status.Games[normalizeNFLAbbreviation(game.Away)]
		// An exact schedule identity prevents a neighboring week or a
		// different game involving the same team from supplying this score.
		if status.Enabled && ok && live.GameID == game.ID && live.Week == game.Week &&
			normalizeNFLAbbreviation(live.Away) == normalizeNFLAbbreviation(game.Away) &&
			normalizeNFLAbbreviation(live.Home) == normalizeNFLAbbreviation(game.Home) &&
			live.InProgress && !live.Final {
			view.InProgress = true
			view.State = strings.TrimSpace(live.Period + " " + live.Clock)
			if view.State == "" {
				view.State = "IN PROGRESS"
			}
			view.HasScores = live.ScoresPresent
			for _, score := range []float64{live.AwayPoints, live.HomePoints} {
				if math.IsNaN(score) || math.IsInf(score, 0) || score < 0 || score != math.Trunc(score) {
					view.HasScores = false
				}
			}
			if view.HasScores {
				view.AwayScore, view.HomeScore = int(live.AwayPoints), int(live.HomePoints)
			}
		}
	}
	if view.HasScores {
		view.ScoreDisplay = fmt.Sprintf("%d-%d", view.AwayScore, view.HomeScore)
	}
	return view
}

// pickemLiveStanding never grades a pick or uses a moving market candidate.
// A final push still follows gradePickemAt's existing loss rule.
func pickemLiveStanding(game GameInfo, market PickemMarket, pick string, view pickemLiveView) (string, string) {
	if game.Final || !validPick(game, pick) || market.Void || !view.InProgress {
		return "", ""
	}
	if !market.Frozen {
		return "pending", "WAITING FOR LINE"
	}
	if !market.LinePresent || !view.HasScores {
		return "", ""
	}
	margin := (view.HomeScore-view.AwayScore)*10 - market.LineTenths
	if margin == 0 {
		return "tied", "TIED ATS"
	}
	if (margin > 0 && pick == game.Home) || (margin < 0 && pick == game.Away) {
		return "winning", "WINNING ATS"
	}
	return "losing", "LOSING ATS"
}
