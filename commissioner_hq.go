package main

import (
	"gridiron-2000/internal/commissionerhq"
	"gridiron-2000/internal/openstats"
)

func commissionerOpenData(status openstats.Status) commissionerhq.OpenData {
	return commissionerhq.OpenData{
		Season:          status.Season,
		Running:         status.Running,
		Schedules:       commissionerDataset(status.Schedules),
		PlayerStats:     commissionerDataset(status.PlayerStats),
		PlayerStatsPrev: commissionerDataset(status.PlayerStatsPrev),
		Injuries:        commissionerDataset(status.Injuries),
		TeamStats:       commissionerDataset(status.TeamStats),
		PlayByPlay:      commissionerDataset(status.PlayByPlay),
	}
}

func commissionerDataset(status openstats.DatasetStatus) commissionerhq.DatasetStatus {
	return commissionerhq.DatasetStatus{
		State:       status.State,
		LastChecked: status.LastChecked,
		LastUpdated: status.LastUpdated,
	}
}
