package openstats

import (
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

// pbpFixtureCSV is a small play-by-play sample shaped exactly like
// nflverse's real "pbp" release (column names verified against a live
// season file): one game's worth of punts covering every PUNTING-group
// event WP-R2 derives, plus a wrong-season row and a non-punt row that
// must both be filtered out.
const pbpFixtureCSV = `season,week,game_id,posteam,punt_attempt,punt_blocked,punt_inside_twenty,punt_out_of_bounds,punt_downed,punt_fair_catch,touchback,kick_distance,yardline_100,punter_player_id,punter_player_name
2026,1,2026_01_BUF_MIA,BUF,1,0,0,0,0,0,0,50,70,00-101,Normal Punter
2026,1,2026_01_BUF_MIA,BUF,1,0,1,0,0,1,0,45,60,00-101,Normal Punter
2026,1,2026_01_BUF_MIA,BUF,1,0,1,0,1,0,0,50,54,00-101,Normal Punter
2026,1,2026_01_BUF_MIA,BUF,1,0,1,1,0,0,0,50,58,00-101,Normal Punter
2026,1,2026_01_BUF_MIA,BUF,1,0,0,0,0,0,1,60,65,00-101,Normal Punter
2026,1,2026_01_BUF_MIA,BUF,1,1,0,0,1,0,0,3,8,00-101,Normal Punter
2025,1,2025_01_BUF_MIA,BUF,1,0,1,0,1,0,0,50,54,00-101,Normal Punter
2026,1,2026_01_BUF_MIA,BUF,0,0,0,0,0,0,0,0,0,,
`

func writeFixture(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeGzipFixture(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestParsePlayByPlayFiltersToSeasonAndPunts checks the season filter and
// the punt_attempt filter: a wrong-season punt and a non-punt row within
// the right season both drop out, leaving exactly the six punt rows.
func TestParsePlayByPlayFiltersToSeasonAndPunts(t *testing.T) {
	path := writeFixture(t, "pbp.csv", pbpFixtureCSV)
	events, err := parsePlayByPlay(path, 2026)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 6 {
		t.Fatalf("events = %d, want 6 (season filter + punt_attempt filter): %+v", len(events), events)
	}
}

// TestParsePlayByPlayDerivesEachPuntingEvent pins the exact per-punt
// derivation WP-R2 needs: a normal return carries no location flags, a
// fair catch inside the 20 sets InsideTwenty only, a downed punt inside the
// 5 sets Inside5 (and InsideTwenty), an out-of-bounds punt inside the 10
// sets CoffinCorner, a touchback sets Touchback alone, and a blocked punt
// suppresses CoffinCorner/Inside5 even when the source also marks it
// downed (the !blocked guard).
func TestParsePlayByPlayDerivesEachPuntingEvent(t *testing.T) {
	path := writeFixture(t, "pbp.csv", pbpFixtureCSV)
	events, err := parsePlayByPlay(path, 2026)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 6 {
		t.Fatalf("events = %d, want 6", len(events))
	}

	normalReturn := events[0]
	if normalReturn.InsideTwenty || normalReturn.CoffinCorner || normalReturn.Inside5 || normalReturn.Touchback || normalReturn.Blocked {
		t.Fatalf("normal return punt carries an unexpected event flag: %+v", normalReturn)
	}
	if normalReturn.Distance != 50 || normalReturn.Punter != "Normal Punter" || normalReturn.PunterID != "00-101" {
		t.Fatalf("normal return punt identity/yardage wrong: %+v", normalReturn)
	}

	fairCatchInside20 := events[1]
	if !fairCatchInside20.InsideTwenty || !fairCatchInside20.FairCatch {
		t.Fatalf("fair catch inside 20 must set InsideTwenty and FairCatch: %+v", fairCatchInside20)
	}
	if fairCatchInside20.CoffinCorner || fairCatchInside20.Inside5 {
		t.Fatalf("fair catch inside 20 (landing at the 15) must not set CoffinCorner or Inside5: %+v", fairCatchInside20)
	}

	downedInside5 := events[2]
	if !downedInside5.Downed || !downedInside5.Inside5 {
		t.Fatalf("downed punt at the 4 must set Downed and Inside5: %+v", downedInside5)
	}
	if downedInside5.CoffinCorner {
		t.Fatalf("a downed (not out-of-bounds) punt must never set CoffinCorner: %+v", downedInside5)
	}

	coffinCorner := events[3]
	if !coffinCorner.OutOfBounds || !coffinCorner.CoffinCorner {
		t.Fatalf("out-of-bounds punt at the 8 must set OutOfBounds and CoffinCorner: %+v", coffinCorner)
	}
	if coffinCorner.Inside5 {
		t.Fatalf("an out-of-bounds (not downed) punt must never set Inside5: %+v", coffinCorner)
	}

	touchback := events[4]
	if !touchback.Touchback {
		t.Fatalf("touchback punt must set Touchback: %+v", touchback)
	}
	if touchback.CoffinCorner || touchback.Inside5 {
		t.Fatalf("a touchback must not also set CoffinCorner or Inside5: %+v", touchback)
	}

	blocked := events[5]
	if !blocked.Blocked {
		t.Fatalf("blocked punt must set Blocked: %+v", blocked)
	}
	if blocked.CoffinCorner || blocked.Inside5 {
		t.Fatalf("a blocked punt must suppress CoffinCorner/Inside5 even when the source also marks it downed: %+v", blocked)
	}
}

// TestParsePlayByPlayAutoDetectsGzip checks that a gzip-compressed source
// (the real nflverse pbp release ships as .csv.gz) parses identically to a
// plain CSV of the same content.
func TestParsePlayByPlayAutoDetectsGzip(t *testing.T) {
	path := writeGzipFixture(t, "pbp.csv.gz", pbpFixtureCSV)
	events, err := parsePlayByPlay(path, 2026)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 6 {
		t.Fatalf("gzip-decoded events = %d, want 6", len(events))
	}
	if events[0].Distance != 50 {
		t.Fatalf("gzip-decoded first event distance = %v, want 50", events[0].Distance)
	}
}

// TestParseTeamStatsFeedsDefenseKeysExcludesOwnFumbles checks the DST
// source mapping and the fumble-recovery honesty rule: only
// fumble_recovery_opp (a takeaway) feeds FumbleRecoveryOpp;
// fumble_recovery_own never counts as a defensive scoring event.
func TestParseTeamStatsFeedsDefenseKeysExcludesOwnFumbles(t *testing.T) {
	const csvBody = "season,week,team,season_type,game_id,opponent_team,def_sacks,def_interceptions,def_tds,def_safeties,fumble_recovery_opp,fumble_recovery_own\n" +
		"2026,1,BAL,REG,2026_01_BAL_PIT,PIT,4,2,1,1,2,3\n" +
		"2025,1,BAL,REG,2025_01_BAL_PIT,PIT,9,9,9,9,9,9\n"
	path := writeFixture(t, "team_stats.csv", csvBody)
	stats, err := parseTeamStats(path, 2026)
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) != 1 {
		t.Fatalf("team stats = %d rows, want 1 (season filter)", len(stats))
	}
	row := stats[0]
	if row.DefSacks != 4 || row.DefInterceptions != 2 || row.DefTDs != 1 || row.DefSafeties != 1 {
		t.Fatalf("defense keys wrong: %+v", row)
	}
	if row.FumbleRecoveryOpp != 2 {
		t.Fatalf("FumbleRecoveryOpp = %v, want 2 (must exclude fumble_recovery_own)", row.FumbleRecoveryOpp)
	}
}

// TestParsePlayerStatsFeedsKickingAndPuntingBoxScoreColumns checks the
// KICKING-group source mapping (fg_made, fg_missed, pat_made) and the
// punting box-score aggregate columns (the honest fallback path), plus the
// backward-compatibility guarantee: a row from a CSV release that predates
// these columns decodes to zero, never an error.
func TestParsePlayerStatsFeedsKickingAndPuntingBoxScoreColumns(t *testing.T) {
	const header = "player_id,player_display_name,position,season,week,season_type,game_id,team,opponent_team,fantasy_points,fantasy_points_ppr,fg_made,fg_missed,pat_made,pt_att,pt_yards,pt_long,pt_inside_20,pt_downed,pt_touchback,pt_blocked\n"
	const kickerRow = "00-201,Example Kicker,K,2026,1,REG,2026_01_BUF_MIA,BUF,MIA,10.0,10.0,2,1,3,0,0,0,0,0,0,0\n"
	const punterRow = "00-202,Example Punter,P,2026,1,REG,2026_01_BUF_MIA,BUF,MIA,0,0,0,0,0,5,220,52,2,1,1,0\n"
	path := writeFixture(t, "stats.csv", header+kickerRow+punterRow)
	stats, err := parsePlayerStats(path, 2026)
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) != 2 {
		t.Fatalf("stats = %d rows, want 2", len(stats))
	}
	kicker, punter := stats[0], stats[1]
	if kicker.FGMade != 2 || kicker.FGMissed != 1 || kicker.XPMade != 3 {
		t.Fatalf("kicker columns wrong: %+v", kicker)
	}
	if punter.Punts != 5 || punter.PuntYardsGross != 220 || punter.PuntLong != 52 ||
		punter.PuntInside20 != 2 || punter.PuntDowned != 1 || punter.PuntTouchback != 1 || punter.PuntBlocked != 0 {
		t.Fatalf("punter box-score columns wrong: %+v", punter)
	}

	// Backward compatibility: an older-release row with none of these
	// columns must decode to honest zeros, not an error.
	const oldHeader = "player_id,player_display_name,position,season,week,season_type,game_id,team,opponent_team,fantasy_points,fantasy_points_ppr\n"
	const oldRow = "00-203,Old Release Kicker,K,2026,1,REG,2026_01_BUF_MIA,BUF,MIA,10.0,10.0\n"
	oldPath := writeFixture(t, "old_stats.csv", oldHeader+oldRow)
	oldStats, err := parsePlayerStats(oldPath, 2026)
	if err != nil {
		t.Fatal(err)
	}
	if len(oldStats) != 1 {
		t.Fatalf("old-release stats = %d rows, want 1", len(oldStats))
	}
	if oldStats[0].FGMade != 0 || oldStats[0].FGMissed != 0 || oldStats[0].XPMade != 0 {
		t.Fatalf("old-release row must decode kicking columns to zero: %+v", oldStats[0])
	}
}
