package openstats

import (
	"bufio"
	"compress/gzip"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

func parseSchedules(path string, season int) ([]ScheduleGame, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	reader := csv.NewReader(file)
	reader.FieldsPerRecord = -1
	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("read schedule header: %w", err)
	}
	index := headerIndex(header)
	required := []string{"game_id", "season", "game_type", "week", "gameday", "away_team", "home_team"}
	if err := requireColumns(index, required); err != nil {
		return nil, fmt.Errorf("schedule CSV: %w", err)
	}
	games := make([]ScheduleGame, 0, 320)
	for {
		row, readErr := reader.Read()
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, fmt.Errorf("read schedule row: %w", readErr)
		}
		rowSeason := intValue(cell(row, index, "season"))
		if rowSeason != season {
			continue
		}
		awayScore, awayScorePresent := optionalFloatValue(cell(row, index, "away_score"))
		homeScore, homeScorePresent := optionalFloatValue(cell(row, index, "home_score"))
		spreadLine, spreadLinePresent := optionalFloatValue(cell(row, index, "spread_line"))
		result := cell(row, index, "result")
		game := ScheduleGame{
			GameID:    cell(row, index, "game_id"),
			Season:    rowSeason,
			GameType:  cell(row, index, "game_type"),
			Week:      intValue(cell(row, index, "week")),
			GameDay:   cell(row, index, "gameday"),
			GameTime:  cell(row, index, "gametime"),
			AwayTeam:  cell(row, index, "away_team"),
			AwayScore:        awayScore,
			AwayScorePresent: awayScorePresent,
			HomeTeam:  cell(row, index, "home_team"),
			HomeScore:        homeScore,
			HomeScorePresent: homeScorePresent,
			Result:           result,
			ResultPresent:    result != "",
		}
		if spreadLinePresent {
			game.SpreadLine = &spreadLine
		}
		games = append(games, game)
	}
	return games, nil
}

func parsePlayerStats(path string, season int) ([]PlayerWeekStat, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	reader := csv.NewReader(file)
	reader.FieldsPerRecord = -1
	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("read player-stat header: %w", err)
	}
	index := headerIndex(header)
	required := []string{"player_id", "player_display_name", "season", "week", "game_id", "team", "fantasy_points", "fantasy_points_ppr"}
	if err := requireColumns(index, required); err != nil {
		return nil, fmt.Errorf("player-stat CSV: %w", err)
	}
	stats := make([]PlayerWeekStat, 0, 24000)
	for {
		row, readErr := reader.Read()
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, fmt.Errorf("read player-stat row: %w", readErr)
		}
		rowSeason := intValue(cell(row, index, "season"))
		if rowSeason != season {
			continue
		}
		stats = append(stats, PlayerWeekStat{
			PlayerID:             cell(row, index, "player_id"),
			PlayerName:           cell(row, index, "player_display_name"),
			Position:             cell(row, index, "position"),
			Season:               rowSeason,
			Week:                 intValue(cell(row, index, "week")),
			SeasonType:           cell(row, index, "season_type"),
			GameID:               cell(row, index, "game_id"),
			Team:                 cell(row, index, "team"),
			OpponentTeam:         cell(row, index, "opponent_team"),
			PassingYards:         floatValue(cell(row, index, "passing_yards")),
			PassingTDs:           floatValue(cell(row, index, "passing_tds")),
			PassingInterceptions: floatValue(cell(row, index, "passing_interceptions")),
			RushingYards:         floatValue(cell(row, index, "rushing_yards")),
			RushingTDs:           floatValue(cell(row, index, "rushing_tds")),
			Receptions:           floatValue(cell(row, index, "receptions")),
			ReceivingYards:       floatValue(cell(row, index, "receiving_yards")),
			ReceivingTDs:         floatValue(cell(row, index, "receiving_tds")),
			FumblesLost: floatValue(cell(row, index, "rushing_fumbles_lost")) +
				floatValue(cell(row, index, "receiving_fumbles_lost")) +
				floatValue(cell(row, index, "sack_fumbles_lost")),
			FantasyPoints:    floatValue(cell(row, index, "fantasy_points")),
			FantasyPointsPPR: floatValue(cell(row, index, "fantasy_points_ppr")),
			// Kicking and punting columns (WP-R2) are optional: absent on
			// every row before this release added them, and absent on
			// every non-K/P row within a release that has them. cell()
			// returns "" for a missing column and floatValue("") is 0, so
			// an older or non-kicking/punting row decodes to honest zeros,
			// never an error.
			FGMade:         floatValue(cell(row, index, "fg_made")),
			FGMissed:       floatValue(cell(row, index, "fg_missed")),
			XPMade:         floatValue(cell(row, index, "pat_made")),
			Punts:          floatValue(cell(row, index, "pt_att")),
			PuntYardsGross: floatValue(cell(row, index, "pt_yards")),
			PuntLong:       floatValue(cell(row, index, "pt_long")),
			PuntInside20:   floatValue(cell(row, index, "pt_inside_20")),
			PuntDowned:     floatValue(cell(row, index, "pt_downed")),
			PuntTouchback:  floatValue(cell(row, index, "pt_touchback")),
			PuntBlocked:    floatValue(cell(row, index, "pt_blocked")),
		})
	}
	return stats, nil
}

func parseInjuries(path string, season int) ([]InjuryReport, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	reader := csv.NewReader(file)
	reader.FieldsPerRecord = -1
	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("read injury header: %w", err)
	}
	index := headerIndex(header)
	required := []string{"season", "team", "week", "gsis_id", "position", "full_name"}
	if err := requireColumns(index, required); err != nil {
		return nil, fmt.Errorf("injury CSV: %w", err)
	}
	reports := make([]InjuryReport, 0, 5000)
	for {
		row, readErr := reader.Read()
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, fmt.Errorf("read injury row: %w", readErr)
		}
		rowSeason := intValue(cell(row, index, "season"))
		if rowSeason != season {
			continue
		}
		seasonType := cell(row, index, "season_type")
		if seasonType == "" {
			seasonType = cell(row, index, "game_type")
		}
		reports = append(reports, InjuryReport{
			Season:                  rowSeason,
			SeasonType:              seasonType,
			Team:                    cell(row, index, "team"),
			Week:                    intValue(cell(row, index, "week")),
			PlayerID:                cell(row, index, "gsis_id"),
			Position:                cell(row, index, "position"),
			PlayerName:              cell(row, index, "full_name"),
			ReportPrimaryInjury:     cell(row, index, "report_primary_injury"),
			ReportSecondaryInjury:   cell(row, index, "report_secondary_injury"),
			ReportStatus:            cell(row, index, "report_status"),
			PracticePrimaryInjury:   cell(row, index, "practice_primary_injury"),
			PracticeSecondaryInjury: cell(row, index, "practice_secondary_injury"),
			PracticeStatus:          cell(row, index, "practice_status"),
			DateModified:            cell(row, index, "date_modified"),
		})
	}
	return reports, nil
}

func parseTeamStats(path string, season int) ([]TeamWeekStat, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	reader := csv.NewReader(file)
	reader.FieldsPerRecord = -1
	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("read team-stat header: %w", err)
	}
	index := headerIndex(header)
	required := []string{"season", "week", "team", "opponent_team"}
	if err := requireColumns(index, required); err != nil {
		return nil, fmt.Errorf("team-stat CSV: %w", err)
	}
	stats := make([]TeamWeekStat, 0, 600)
	for {
		row, readErr := reader.Read()
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, fmt.Errorf("read team-stat row: %w", readErr)
		}
		rowSeason := intValue(cell(row, index, "season"))
		if rowSeason != season {
			continue
		}
		stats = append(stats, TeamWeekStat{
			Season:            rowSeason,
			Week:              intValue(cell(row, index, "week")),
			SeasonType:        cell(row, index, "season_type"),
			GameID:            cell(row, index, "game_id"),
			Team:              cell(row, index, "team"),
			OpponentTeam:      cell(row, index, "opponent_team"),
			DefSacks:          floatValue(cell(row, index, "def_sacks")),
			DefInterceptions:  floatValue(cell(row, index, "def_interceptions")),
			DefTDs:            floatValue(cell(row, index, "def_tds")),
			DefSafeties:       floatValue(cell(row, index, "def_safeties")),
			FumbleRecoveryOpp: floatValue(cell(row, index, "fumble_recovery_opp")),
		})
	}
	return stats, nil
}

// parsePlayByPlay reads the nflverse play-by-play mirror (plain or
// gzip-compressed CSV, auto-detected — the source ships as .csv.gz) and
// keeps only punt plays, discarding every other column immediately: a full
// season carries ~370 columns and ~48,000 rows, and PUNTING scoring needs
// only the punt-relevant dozen.
func parsePlayByPlay(path string, season int) ([]PuntEvent, error) {
	reader, closeFn, err := openCSVAutoGzip(path)
	if err != nil {
		return nil, err
	}
	defer closeFn()
	reader.FieldsPerRecord = -1
	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("read play-by-play header: %w", err)
	}
	index := headerIndex(header)
	required := []string{"season", "week", "game_id", "posteam", "punt_attempt", "kick_distance", "yardline_100"}
	if err := requireColumns(index, required); err != nil {
		return nil, fmt.Errorf("play-by-play CSV: %w", err)
	}
	events := make([]PuntEvent, 0, 2200)
	for {
		row, readErr := reader.Read()
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, fmt.Errorf("read play-by-play row: %w", readErr)
		}
		rowSeason := intValue(cell(row, index, "season"))
		if rowSeason != season {
			continue
		}
		if cell(row, index, "punt_attempt") != "1" {
			continue
		}
		blocked := cell(row, index, "punt_blocked") == "1"
		outOfBounds := cell(row, index, "punt_out_of_bounds") == "1"
		downed := cell(row, index, "punt_downed") == "1"
		distance := floatValue(cell(row, index, "kick_distance"))
		yardline100 := floatValue(cell(row, index, "yardline_100"))
		// landingSpot estimates where the punt hit the ground, in yards
		// from the receiving team's goal line, before any return:
		// yardline_100 (distance-to-go from the punting team's snap spot)
		// minus the gross kick distance. Only meaningful for a
		// downed/out-of-bounds punt (no return yardage to subtract), which
		// is exactly when coffinCorner/puntDownedInside5 apply.
		landingSpot := yardline100 - distance
		events = append(events, PuntEvent{
			Season:       rowSeason,
			Week:         intValue(cell(row, index, "week")),
			GameID:       cell(row, index, "game_id"),
			Team:         cell(row, index, "posteam"),
			PunterID:     cell(row, index, "punter_player_id"),
			Punter:       cell(row, index, "punter_player_name"),
			Distance:     distance,
			Blocked:      blocked,
			InsideTwenty: cell(row, index, "punt_inside_twenty") == "1",
			Touchback:    cell(row, index, "touchback") == "1",
			Downed:       downed,
			OutOfBounds:  outOfBounds,
			FairCatch:    cell(row, index, "punt_fair_catch") == "1",
			CoffinCorner: !blocked && outOfBounds && landingSpot >= 0 && landingSpot <= 10,
			Inside5:      !blocked && downed && landingSpot >= 0 && landingSpot <= 5,
		})
	}
	return events, nil
}

// openCSVAutoGzip opens path and returns a csv.Reader over it, transparently
// decompressing when the file starts with the gzip magic bytes. The nflverse
// play-by-play release ships as .csv.gz; fixture files in tests may be
// plain CSV, so both must work through the same parser.
func openCSVAutoGzip(path string) (*csv.Reader, func() error, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	buffered := bufio.NewReader(file)
	magic, peekErr := buffered.Peek(2)
	if peekErr == nil && len(magic) == 2 && magic[0] == 0x1f && magic[1] == 0x8b {
		gz, gzErr := gzip.NewReader(buffered)
		if gzErr != nil {
			_ = file.Close()
			return nil, nil, fmt.Errorf("open gzip play-by-play: %w", gzErr)
		}
		return csv.NewReader(gz), func() error {
			_ = gz.Close()
			return file.Close()
		}, nil
	}
	return csv.NewReader(buffered), file.Close, nil
}

func headerIndex(header []string) map[string]int {
	index := make(map[string]int, len(header))
	for position, name := range header {
		index[strings.ToLower(strings.TrimSpace(name))] = position
	}
	return index
}

func requireColumns(index map[string]int, columns []string) error {
	for _, column := range columns {
		if _, exists := index[column]; !exists {
			return fmt.Errorf("missing %q column", column)
		}
	}
	return nil
}

func cell(row []string, index map[string]int, name string) string {
	position, exists := index[name]
	if !exists || position < 0 || position >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[position])
}

func intValue(value string) int {
	parsed, _ := strconv.Atoi(strings.TrimSpace(value))
	return parsed
}

func floatValue(value string) float64 {
	parsed, _ := strconv.ParseFloat(strings.TrimSpace(value), 64)
	return parsed
}

func optionalFloatValue(value string) (float64, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, false
	}
	return parsed, true
}
