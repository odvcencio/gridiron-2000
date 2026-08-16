package openstats

import (
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
		games = append(games, ScheduleGame{
			GameID:    cell(row, index, "game_id"),
			Season:    rowSeason,
			GameType:  cell(row, index, "game_type"),
			Week:      intValue(cell(row, index, "week")),
			GameDay:   cell(row, index, "gameday"),
			GameTime:  cell(row, index, "gametime"),
			AwayTeam:  cell(row, index, "away_team"),
			AwayScore: floatValue(cell(row, index, "away_score")),
			HomeTeam:  cell(row, index, "home_team"),
			HomeScore: floatValue(cell(row, index, "home_score")),
		})
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
