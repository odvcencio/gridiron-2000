package league

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestPunterHistLineMatchesTownsendHOU pins the embedded 2025 punter index's
// canonical match: team + last name, case-insensitive, against the
// Townsend/HOU entry the roster-ops spec (section 4.1.2 / WP-R0) names
// explicitly.
func TestPunterHistLineMatchesTownsendHOU(t *testing.T) {
	line, ok := punterHistLine("Tommy Townsend", "HOU")
	if !ok || line == "" {
		t.Fatalf("Townsend/HOU must match the embedded index; got ok=%v line=%q", ok, line)
	}
	// Case-insensitive on both the team and the last name.
	if line2, ok2 := punterHistLine("tommy townsend", "hou"); !ok2 || line2 != line {
		t.Fatalf("lower-cased name/team must still match: ok=%v line=%q, want %q", ok2, line2, line)
	}
}

// TestPunterHistLineNoMatch pins the fail-quiet contract: an unknown last
// name, a known last name on the wrong team, and an empty name all return
// ("", false) rather than a wrong attribution or a panic.
func TestPunterHistLineNoMatch(t *testing.T) {
	cases := []struct {
		name string
		team string
	}{
		{"Nobody Punter", "HOU"},  // unknown last name
		{"Tommy Townsend", "DAL"}, // right last name, wrong team
		{"", "HOU"},               // empty name
		{"   ", "HOU"},            // whitespace-only name
	}
	for _, tc := range cases {
		if line, ok := punterHistLine(tc.name, tc.team); ok {
			t.Errorf("punterHistLine(%q, %q) = (%q, true), want a miss", tc.name, tc.team, line)
		}
	}
}

// TestPunterHistLineUsesLastWord checks that matching keys on the last
// space-separated token of the full name, so a multi-word first/middle
// name (for example a hyphenated or suffixed name) still resolves.
func TestPunterHistLineUsesLastWord(t *testing.T) {
	if _, ok := punterHistLine("A.J. Cole", "LV"); !ok {
		t.Error("A.J. Cole (LV) must match the embedded Cole/LV entry")
	}
	if got := lastWord("A.J. Cole"); got != "Cole" {
		t.Errorf("lastWord(%q) = %q, want Cole", "A.J. Cole", got)
	}
	if got := lastWord(""); got != "" {
		t.Errorf("lastWord empty name = %q, want empty", got)
	}
}

// TestWithHistoricalAttachesPunterFallback checks the full merge path
// (service.go's withHistorical): a Position "P" player whose primary
// historicalFn source misses (nflverse carries no punting columns) still
// gets its Hist filled from the embedded punter index; a non-punter
// position never triggers the fallback; an already-set Hist is left
// untouched.
func TestWithHistoricalAttachesPunterFallback(t *testing.T) {
	service := newTestService(t, true)
	service.SetHistoricalSource(func(name, position string, values map[string]float64) (string, bool) {
		return "", false // nflverse never matches a punter (no punting columns)
	})

	punter := service.withHistorical(Player{Name: "Tommy Townsend", Position: "P", NFLTeam: "HOU"}, nil)
	if punter.Hist == "" {
		t.Fatalf("punter fallback did not attach a hist line: %+v", punter)
	}

	unknownPunter := service.withHistorical(Player{Name: "Nobody Punter", Position: "P", NFLTeam: "HOU"}, nil)
	if unknownPunter.Hist != "" {
		t.Fatalf("an unmatched punter must attach nothing: %+v", unknownPunter)
	}

	kicker := service.withHistorical(Player{Name: "Tommy Townsend", Position: "K", NFLTeam: "HOU"}, nil)
	if kicker.Hist != "" {
		t.Fatalf("the punter fallback must never apply to a non-P position: %+v", kicker)
	}

	already := service.withHistorical(Player{Name: "Tommy Townsend", Position: "P", NFLTeam: "HOU", Hist: "already set"}, nil)
	if already.Hist != "already set" {
		t.Fatalf("existing Hist must not be overwritten: %+v", already)
	}
}

// TestWithHistoricalPunterFallbackWithNoPrimarySource checks that the
// punter fallback still applies when no HistoricalSource is attached at
// all (SetHistoricalSource never called) — the fallback does not depend on
// the primary source existing.
func TestWithHistoricalPunterFallbackWithNoPrimarySource(t *testing.T) {
	service := newTestService(t, true)
	punter := service.withHistorical(Player{Name: "Tommy Townsend", Position: "P", NFLTeam: "HOU"}, nil)
	if punter.Hist == "" {
		t.Fatalf("punter fallback must work with no primary source attached: %+v", punter)
	}
}

// TestEmbeddedPunterLastNamesAreUnique verifies the design assumption
// PunterProjection's last-name-primary match rests on: every one of the
// embedded asset's 35 entries carries a unique last name. This is the
// factual check the design calls for stating in the implementation report.
func TestEmbeddedPunterLastNamesAreUnique(t *testing.T) {
	var entries []punterHistEntry
	if err := json.Unmarshal(puntersHistRaw, &entries); err != nil {
		t.Fatalf("decode embedded asset: %v", err)
	}
	if len(entries) != 35 {
		t.Fatalf("embedded asset entry count = %d, want 35", len(entries))
	}
	seen := make(map[string]string, len(entries))
	for _, entry := range entries {
		key := strings.ToUpper(strings.TrimSpace(entry.LastName))
		if prior, dup := seen[key]; dup {
			t.Fatalf("last name %q is not unique: %q and %q both carry it", key, prior, entry.Team)
		}
		seen[key] = entry.Team
	}
	if len(seen) != 35 {
		t.Fatalf("unique last names = %d, want 35", len(seen))
	}
}

// TestPunterProjectionMatchesTownsendHOUWithExactPerGameArithmetic pins the
// scale contract: PunterProjection returns TotalPts/Games exactly, on the
// SAME per-game scale internal/fantasy's other Projection values already
// carry — not a season total, not a rounded display figure.
func TestPunterProjectionMatchesTownsendHOUWithExactPerGameArithmetic(t *testing.T) {
	perGame, ok := PunterProjection("Tommy Townsend", "HOU", false)
	if !ok {
		t.Fatal("Townsend/HOU must match the embedded index")
	}
	want := 148.8 / 17.0
	if perGame != want {
		t.Fatalf("PunterProjection(Townsend, HOU) = %v, want exactly %v (148.8 TotalPts / 17 Games)", perGame, want)
	}
	// A realistic per-game figure: nowhere near a season total (148.8) and
	// nowhere near zero.
	if perGame < 5 || perGame > 15 {
		t.Fatalf("per-game projection = %v, want a realistic weekly punter value", perGame)
	}
}

// TestPunterProjectionMatchesByLastNameAcrossTeamChange checks the primary
// matching rule directly: a team mismatch (a punter who has since changed
// teams) does not block the match when the last name alone is unambiguous
// in the embedded asset — the opposite of punterHistLine's own team+name
// exact-match rule, deliberately, because punters change teams between
// seasons far more than skill players (design decision, this feature).
func TestPunterProjectionMatchesByLastNameAcrossTeamChange(t *testing.T) {
	wantHOU, ok := PunterProjection("Tommy Townsend", "HOU", false)
	if !ok {
		t.Fatal("Townsend/HOU must match")
	}
	gotWrongTeam, ok := PunterProjection("Tommy Townsend", "DAL", false)
	if !ok {
		t.Fatal("Townsend must still match on a team the asset does not carry for him")
	}
	if gotWrongTeam != wantHOU {
		t.Fatalf("team-mismatched match = %v, want the same value as the team match %v", gotWrongTeam, wantHOU)
	}
	// requireTeam=true is the live-pool collision guard: it must block the
	// exact same team mismatch that requireTeam=false just allowed.
	if _, ok := PunterProjection("Tommy Townsend", "DAL", true); ok {
		t.Fatal("requireTeam=true must block a team the asset does not carry for this punter")
	}
	if gotRequireTeam, ok := PunterProjection("Tommy Townsend", "HOU", true); !ok || gotRequireTeam != wantHOU {
		t.Fatalf("requireTeam=true must still match the punter's real team: got %v, %v, want %v, true", gotRequireTeam, ok, wantHOU)
	}
}

// TestPunterProjectionMisses pins the fail-quiet contract: an unknown last
// name and an empty name both return (0, false), never a wrong
// attribution or a panic.
func TestPunterProjectionMisses(t *testing.T) {
	if _, ok := PunterProjection("Nobody Punter", "HOU", false); ok {
		t.Error("an unknown last name must miss")
	}
	if _, ok := PunterProjection("", "HOU", false); ok {
		t.Error("an empty name must miss")
	}
}

// TestPunterProjectionRequiresMinimumGames is finding 4's own regression
// test: a punter below MinPunterGamesForRank games misses entirely, even
// though its per-game average would otherwise be real and positive — a
// 5-game small sample must never outrank a full-season punter on a
// handful of good punts. A punter at or above the floor still matches.
func TestPunterProjectionRequiresMinimumGames(t *testing.T) {
	store := punterHistStore{
		byLastName: map[string][]punterHistEntry{
			"SMALLSAMPLE": {{LastName: "SmallSample", Team: "ARI", Games: 5, TotalPts: 47.3}},
			"FULLSEASON":  {{LastName: "FullSeason", Team: "HOU", Games: 17, TotalPts: 148.8}},
		},
	}
	if _, ok := punterProjectionFrom(store, "SmallSample", "ARI", false); ok {
		t.Error("a 5-game entry (below MinPunterGamesForRank) must miss")
	}
	perGame, ok := punterProjectionFrom(store, "FullSeason", "HOU", false)
	if !ok || perGame != 148.8/17.0 {
		t.Errorf("a 17-game entry (at/above MinPunterGamesForRank) = %v, %v, want %v, true", perGame, ok, 148.8/17.0)
	}
}

// TestLastWordStripsGenerationalSuffix is finding 8's own regression test:
// a trailing JR/SR/II/III/IV token, with or without a period, must never
// itself be extracted as the last name.
func TestLastWordStripsGenerationalSuffix(t *testing.T) {
	cases := map[string]string{
		"Michael Dickson Jr.": "Dickson",
		"Michael Dickson Jr":  "Dickson",
		"Michael Dickson JR.": "Dickson",
		"John Smith III":      "Smith",
		"John Smith II":       "Smith",
		"Bob Jones Sr.":       "Jones",
		"Jr.":                 "", // suffix-only name has no last name at all
	}
	for name, want := range cases {
		if got := lastWord(name); got != want {
			t.Errorf("lastWord(%q) = %q, want %q", name, got, want)
		}
	}
}

// TestPunterHistLineStillMatchesWithSuffixedName checks that
// punterHistLine's display match — sharing lastWord with PunterProjection
// — still resolves a suffixed name correctly; the shared helper's suffix
// fix must not regress punterHistLine's own matching semantics.
func TestPunterHistLineStillMatchesWithSuffixedName(t *testing.T) {
	want, ok := punterHistLine("Tommy Townsend", "HOU")
	if !ok {
		t.Fatal("Townsend/HOU must match")
	}
	if got, ok := punterHistLine("Tommy Townsend Jr.", "HOU"); !ok || got != want {
		t.Errorf("suffixed name must still match: got %q, %v, want %q, true", got, ok, want)
	}
}

// TestPunterProjectionFromRequiresTeamOnLastNameCollision pins the
// disambiguation rule PunterProjection would need the moment two asset
// entries ever do share a last name (none do today — see
// TestEmbeddedPunterLastNamesAreUnique): only then is a team match
// required; an unmatched team among the collision candidates misses
// entirely rather than guessing.
func TestPunterProjectionFromRequiresTeamOnLastNameCollision(t *testing.T) {
	store := punterHistStore{
		byLastName: map[string][]punterHistEntry{
			"SMITH": {
				{LastName: "Smith", Team: "HOU", Games: 17, TotalPts: 85.0},
				{LastName: "Smith", Team: "DAL", Games: 16, TotalPts: 80.0},
			},
		},
	}
	houPerGame, ok := punterProjectionFrom(store, "Smith", "HOU", false)
	if !ok || houPerGame != 85.0/17.0 {
		t.Fatalf("HOU Smith = %v, %v, want %v, true", houPerGame, ok, 85.0/17.0)
	}
	dalPerGame, ok := punterProjectionFrom(store, "Smith", "DAL", false)
	if !ok || dalPerGame != 80.0/16.0 {
		t.Fatalf("DAL Smith = %v, %v, want %v, true", dalPerGame, ok, 80.0/16.0)
	}
	if _, ok := punterProjectionFrom(store, "Smith", "NYJ", false); ok {
		t.Fatal("a team absent from the colliding candidates must miss, not guess")
	}
}

// TestPunterProjectionFromRequireTeamOnUniqueLastNameCollision pins the
// LIVE-pool collision guard (finding 3): a last name unique in the
// embedded asset still needs an exact team match once the caller (the
// fantasy pool's enrichment walk) says requireTeam — a second live
// punter sharing the surname is on a different team and must miss rather
// than inherit the first one's projection. requireTeam=false keeps the
// original last-name-only match, so a punter who has since changed teams
// still resolves.
func TestPunterProjectionFromRequireTeamOnUniqueLastNameCollision(t *testing.T) {
	store := punterHistStore{
		byLastName: map[string][]punterHistEntry{
			"TAYLOR": {{LastName: "Taylor", Team: "DET", Games: 17, TotalPts: 119.0}},
		},
	}
	want := 119.0 / 17.0
	if got, ok := punterProjectionFrom(store, "Taylor", "DET", true); !ok || got != want {
		t.Fatalf("requireTeam=true, matching team = %v, %v, want %v, true", got, ok, want)
	}
	if _, ok := punterProjectionFrom(store, "Taylor", "SEA", true); ok {
		t.Fatal("requireTeam=true, mismatched team must miss (a second live Taylor on another team)")
	}
	if got, ok := punterProjectionFrom(store, "Taylor", "SEA", false); !ok || got != want {
		t.Fatalf("requireTeam=false, mismatched team = %v, %v, want %v, true (unique surname, team-changed punter still matches)", got, ok, want)
	}
}

// suffixStrippingSurnameTable pins lastWord's suffix-strip rule against
// internal/fantasy/punters_test.go's identical table for punterSurname
// (TestPunterSurnameStripsGenerationalSuffix, mirrored there).
// internal/fantasy cannot import this package (see punterSurname's doc
// comment there), so the table is duplicated here verbatim; keep both
// copies in lockstep by hand whenever either side's suffix list changes
// (finding 1 of the punter-rankings review). Every expected value is
// upper-cased, matching punterSurname's own return convention, so this
// table compares strings.ToUpper(lastWord(name)) against it.
var suffixStrippingSurnameTable = map[string]string{
	"Michael Dickson Jr.": "DICKSON",
	"Michael Dickson Jr":  "DICKSON",
	"Michael Dickson JR.": "DICKSON",
	"John Smith III":      "SMITH",
	"John Smith II":       "SMITH",
	"Bob Jones Sr.":       "JONES",
	"Bob Jones SR":        "JONES",
	"AJ Cole III":         "COLE",
	"Bo Taylor IV":        "TAYLOR",
}

// TestLastWordAgreesWithPunterSurnameSuffixTable is finding 1's own
// regression test: lastWord and internal/fantasy's punterSurname must
// tokenize a generational suffix identically, or the fantasy pool's
// live-pool collision guard and this package's own last-name match
// disagree about which live "P" players collide.
func TestLastWordAgreesWithPunterSurnameSuffixTable(t *testing.T) {
	for name, want := range suffixStrippingSurnameTable {
		if got := strings.ToUpper(lastWord(name)); got != want {
			t.Errorf("lastWord(%q) = %q (upper-cased), want %q", name, got, want)
		}
	}
}
