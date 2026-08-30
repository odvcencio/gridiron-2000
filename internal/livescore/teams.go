package livescore

import "strings"

// tank01ToNFLverse maps the Tank01 abbreviations that differ from nflverse.
var tank01ToNFLverse = map[string]string{"LAR": "LA", "WSH": "WAS", "JAC": "JAX"}

// NormalizeTeam returns the nflverse abbreviation for a Tank01 one.
func NormalizeTeam(abbr string) string {
	upper := strings.ToUpper(strings.TrimSpace(abbr))
	if mapped, ok := tank01ToNFLverse[upper]; ok {
		return mapped
	}
	return upper
}

// dstNames maps an nflverse abbreviation to the pool's D/ST display name
// (moved here from main.go's dstNicknames so the overlay and the render
// fixtures share one table; no import cycle: league never imports this).
var dstNames = map[string]string{
	"ARI": "Cardinals D/ST", "ATL": "Falcons D/ST", "BAL": "Ravens D/ST", "BUF": "Bills D/ST",
	"CAR": "Panthers D/ST", "CHI": "Bears D/ST", "CIN": "Bengals D/ST", "CLE": "Browns D/ST",
	"DAL": "Cowboys D/ST", "DEN": "Broncos D/ST", "DET": "Lions D/ST", "GB": "Packers D/ST",
	"HOU": "Texans D/ST", "IND": "Colts D/ST", "JAX": "Jaguars D/ST", "KC": "Chiefs D/ST",
	"LA": "Rams D/ST", "LAC": "Chargers D/ST", "LV": "Raiders D/ST", "MIA": "Dolphins D/ST",
	"MIN": "Vikings D/ST", "NE": "Patriots D/ST", "NO": "Saints D/ST", "NYG": "Giants D/ST",
	"NYJ": "Jets D/ST", "PHI": "Eagles D/ST", "PIT": "Steelers D/ST", "SEA": "Seahawks D/ST",
	"SF": "49ers D/ST", "TB": "Buccaneers D/ST", "TEN": "Titans D/ST", "WAS": "Commanders D/ST",
}

// DSTName returns the pool display name of a team's D/ST unit.
func DSTName(team string) (string, bool) {
	name, ok := dstNames[NormalizeTeam(team)]
	return name, ok
}
