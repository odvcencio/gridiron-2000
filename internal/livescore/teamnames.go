package livescore

import "strings"

// teamAliases is a static NFL team alias table (nflverse abbreviation ->
// city, nickname): the wire-trigger seam's own team-resolution data (GC-2
// layer 3, live_scoring.go). Signal (internal/wire/model.go) carries no
// structured team field, so the seam falls back to matching a signal's
// free text against these aliases. A false match is acceptable — it costs
// at most one triggered box fetch per game per triggerCooldown (10s),
// never a stat or a score (see Poller.TriggerBoxFetch's own doc
// comment) — so this table favors coverage over precision; it is not a
// player-pool identity resolution and never feeds one.
var teamAliases = map[string][]string{
	"ARI": {"Arizona", "Cardinals"},
	"ATL": {"Atlanta", "Falcons"},
	"BAL": {"Baltimore", "Ravens"},
	"BUF": {"Buffalo", "Bills"},
	"CAR": {"Carolina", "Panthers"},
	"CHI": {"Chicago", "Bears"},
	"CIN": {"Cincinnati", "Bengals"},
	"CLE": {"Cleveland", "Browns"},
	"DAL": {"Dallas", "Cowboys"},
	"DEN": {"Denver", "Broncos"},
	"DET": {"Detroit", "Lions"},
	"GB":  {"Green Bay", "Packers"},
	"HOU": {"Houston", "Texans"},
	"IND": {"Indianapolis", "Colts"},
	"JAX": {"Jacksonville", "Jaguars"},
	"KC":  {"Kansas City", "Chiefs"},
	"LA":  {"Los Angeles", "Rams"},
	"LAC": {"Los Angeles", "Chargers"},
	"LV":  {"Las Vegas", "Raiders"},
	"MIA": {"Miami", "Dolphins"},
	"MIN": {"Minnesota", "Vikings"},
	"NE":  {"New England", "Patriots"},
	"NO":  {"New Orleans", "Saints"},
	"NYG": {"New York", "Giants"},
	"NYJ": {"New York", "Jets"},
	"PHI": {"Philadelphia", "Eagles"},
	"PIT": {"Pittsburgh", "Steelers"},
	"SEA": {"Seattle", "Seahawks"},
	"SF":  {"San Francisco", "49ers"},
	"TB":  {"Tampa Bay", "Buccaneers"},
	"TEN": {"Tennessee", "Titans"},
	"WAS": {"Washington", "Commanders"},
}

// TeamMentioned reports whether text names team (nflverse abbreviation):
// a case-insensitive substring match against the team's own nickname or
// city (teamAliases), or the bare abbreviation itself. An unrecognized
// team always reports false.
func TeamMentioned(team, text string) bool {
	aliases, ok := teamAliases[NormalizeTeam(team)]
	if !ok {
		return false
	}
	text = strings.ToLower(text)
	for _, alias := range aliases {
		if strings.Contains(text, strings.ToLower(alias)) {
			return true
		}
	}
	return false
}
