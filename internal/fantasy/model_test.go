package fantasy

import "testing"

// TestTank01ScoringParams is rules-audit item 4's mapping-table test: every
// tank01ProjectionScoringParams entry must carry values' matching rule
// value, formatted with strconv.FormatFloat's shortest form (no forced
// trailing zeros, matching Tank01's own numeric-string style observed
// live), and targets/xpMissed — no matching defaultScoringRules key exists
// — must carry an explicit "0" rather than being left out of the map.
func TestTank01ScoringParams(t *testing.T) {
	values := map[string]float64{
		"passYards": 0.04, "passTD": 4, "passInt": -2,
		"rushYards": 0.1, "rushTD": 6, "fumbleLost": -2,
		"recYards": 0.1, "recTD": 6,
		"twoPt": 2, "fgMade": 3, "fgMissed": -1, "xpMade": 1,
		"reception": 0.5, // reception has no tank01ProjectionScoringParams entry (pointsPerReception owns it)
	}
	got := tank01ScoringParams(values)
	want := map[string]string{
		"passYards": "0.04", "passTD": "4", "passInterceptions": "-2",
		"rushYards": "0.1", "rushTD": "6", "fumbles": "-2",
		"receivingYards": "0.1", "receivingTD": "6",
		"twoPointConversions": "2", "fgMade": "3", "fgMissed": "-1", "xpMade": "1",
		"targets": "0", "xpMissed": "0",
	}
	if len(got) != len(want) {
		t.Fatalf("tank01ScoringParams = %+v, want %+v", got, want)
	}
	for param, value := range want {
		if got[param] != value {
			t.Errorf("tank01ScoringParams[%q] = %q, want %q", param, got[param], value)
		}
	}
	if _, ok := got["pointsPerReception"]; ok {
		t.Error(`tank01ScoringParams must not emit "pointsPerReception": that knob is owned by pointsPerReception(scoringFormat), not the scoring-values table`)
	}

	// A nil values map (no SetScoringValues wiring) must still emit every
	// key, all "0" — an honest "not configured," never an omitted
	// parameter Tank01 could apply its own default weight to instead.
	unwired := tank01ScoringParams(nil)
	for param := range want {
		if unwired[param] != "0" {
			t.Errorf("tank01ScoringParams(nil)[%q] = %q, want 0", param, unwired[param])
		}
	}
}

// TestScaledPoolLimit pins the productization wave's pool-limit scaling
// rule (owner decision): teams × roster spots × 2.5 headroom, clamped to
// [200, 800], instead of a flat 400.
func TestScaledPoolLimit(t *testing.T) {
	cases := []struct {
		name        string
		teams       int
		rosterSpots int
		want        int
	}{
		{"reference league (8 x 17)", 8, 17, 340},
		{"small league clamps to floor", 4, 15, 200},
		{"large league clamps to ceiling", 14, 17, 595},
		{"zero teams falls back", 0, 15, DefaultPoolLimitFallback},
		{"zero roster spots falls back", 8, 0, DefaultPoolLimitFallback},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ScaledPoolLimit(tc.teams, tc.rosterSpots); got != tc.want {
				t.Errorf("ScaledPoolLimit(%d, %d) = %d, want %d", tc.teams, tc.rosterSpots, got, tc.want)
			}
		})
	}
}

// TestConfigFromEnvUsesCallerDefaultPoolLimit checks the FANTASY_POOL_LIMIT
// precedence: env wins when set; the caller-supplied scaled default
// applies otherwise; <= 0 falls back to the flat constant.
func TestConfigFromEnvUsesCallerDefaultPoolLimit(t *testing.T) {
	t.Setenv("FANTASY_POOL_LIMIT", "")
	if got := ConfigFromEnv(340).PoolLimit; got != 340 {
		t.Errorf("PoolLimit = %d, want the caller-supplied default 340", got)
	}
	if got := ConfigFromEnv(0).PoolLimit; got != DefaultPoolLimitFallback {
		t.Errorf("PoolLimit = %d, want the flat fallback %d", got, DefaultPoolLimitFallback)
	}
	t.Setenv("FANTASY_POOL_LIMIT", "123")
	if got := ConfigFromEnv(340).PoolLimit; got != 123 {
		t.Errorf("PoolLimit = %d, want the env override 123", got)
	}
}

// TestConfigFromEnvReadsBaseURL checks TANK01_BASE_URL's env wiring: unset
// leaves BaseURL empty (direct mode, unchanged); a trailing slash is
// trimmed so tank01.go's "baseURL + \"/\" + endpoint" join never doubles
// up.
func TestConfigFromEnvReadsBaseURL(t *testing.T) {
	t.Setenv("TANK01_BASE_URL", "")
	if got := ConfigFromEnv(0).BaseURL; got != "" {
		t.Errorf("BaseURL = %q, want empty when unset", got)
	}
	t.Setenv("TANK01_BASE_URL", "http://statrelay.gridiron.svc.cluster.local")
	if got := ConfigFromEnv(0).BaseURL; got != "http://statrelay.gridiron.svc.cluster.local" {
		t.Errorf("BaseURL = %q, want the env value verbatim", got)
	}
	t.Setenv("TANK01_BASE_URL", "http://statrelay.gridiron.svc.cluster.local/")
	if got := ConfigFromEnv(0).BaseURL; got != "http://statrelay.gridiron.svc.cluster.local" {
		t.Errorf("BaseURL = %q, want the trailing slash trimmed", got)
	}
}

// TestPlayerIsRookie pins Exp == "R" (case-insensitive, whitespace
// tolerant) as the only rookie signal Tank01's raw player list carries
// (verified live via getNFLPlayerList, 2026-08-16); a veteran's season
// count and a blank (unreported) Exp both report false, not a guess.
func TestPlayerIsRookie(t *testing.T) {
	cases := []struct {
		name string
		exp  string
		want bool
	}{
		{"rookie", "R", true},
		{"lowercase rookie tolerated", "r", true},
		{"padded rookie tolerated", " R ", true},
		{"veteran season count", "9", false},
		{"single-digit veteran", "1", false},
		{"unreported", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			player := Player{Exp: tc.exp}
			if got := player.IsRookie(); got != tc.want {
				t.Errorf("Player{Exp: %q}.IsRookie() = %v, want %v", tc.exp, got, tc.want)
			}
		})
	}
}

// TestPlayerDraftCapital pins the exact gate a "presumed usage" signal must
// clear: this year's rookie class (IsRookie) AND a real Tank01 pick
// (DraftPick > 0). A veteran's own draftInfo (Tank01 keeps a player's
// original draft slot on his record for his whole career — verified live
// 2026-08-18) is a stale artifact of a past class, not usage evidence for
// the current season, so it must never leak into DraftCapital. An
// undrafted rookie (no draftInfo at all — a UDFA) must report false
// honestly rather than guessing a slot that does not exist.
func TestPlayerDraftCapital(t *testing.T) {
	cases := []struct {
		name     string
		player   Player
		wantPick int
		wantOK   bool
	}{
		{"rookie with draft capital", Player{Exp: "R", DraftRound: 1, DraftPick: 3}, 3, true},
		{"rookie without draft capital (UDFA)", Player{Exp: "R"}, 0, false},
		{"veteran with a past draft slot", Player{Exp: "3", DraftRound: 1, DraftPick: 3}, 0, false},
		{"unreported exp with a draft slot", Player{DraftRound: 1, DraftPick: 3}, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotPick, gotOK := tc.player.DraftCapital()
			if gotOK != tc.wantOK || gotPick != tc.wantPick {
				t.Errorf("DraftCapital() = (%d, %v), want (%d, %v)", gotPick, gotOK, tc.wantPick, tc.wantOK)
			}
		})
	}
}

// TestPlayerDraftCapitalLabel pins the compact chip text a pool row shows
// (owner directive 2026-08-18 — "show the reasoning," matching the example
// format "R1 · P8"). It renders "" whenever DraftCapital reports no usable
// slot — no placeholder dash, no partial label — so a row template's
// has_draft_capital gate and this label can never disagree.
func TestPlayerDraftCapitalLabel(t *testing.T) {
	cases := []struct {
		name   string
		player Player
		want   string
	}{
		{"round and pick both known", Player{Exp: "R", DraftRound: 1, DraftPick: 8}, "R1 · P8"},
		{"late round", Player{Exp: "R", DraftRound: 6, DraftPick: 204}, "R6 · P204"},
		{"pick known, round missing", Player{Exp: "R", DraftPick: 40}, "P40"},
		{"undrafted rookie", Player{Exp: "R"}, ""},
		{"veteran carrying a past draft slot", Player{Exp: "3", DraftRound: 1, DraftPick: 8}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.player.DraftCapitalLabel(); got != tc.want {
				t.Errorf("DraftCapitalLabel() = %q, want %q", got, tc.want)
			}
		})
	}
}
