package main

import (
	"context"
	"errors"
	"math"
	"net"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"gridiron-2000/internal/commissionerhq/v1provider"
	"gridiron-2000/internal/commissionerhq/v1transport"
	"gridiron-2000/internal/fantasy"
	"gridiron-2000/internal/league"
	"gridiron-2000/internal/openstats"
)

var (
	commissionerHQV1GitSHA = regexp.MustCompile(`^[0-9a-f]{40}$`)
	commissionerHQV1Digest = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

type commissionerHQV1Runtime struct {
	server    *http.Server
	listener  net.Listener
	listening atomic.Bool
}

func newCommissionerHQV1Runtime(config v1provider.Config, source v1transport.SnapshotSource) (*commissionerHQV1Runtime, error) {
	if !config.Enabled {
		return nil, nil
	}
	server, err := v1provider.NewServer(config, source)
	if err != nil {
		return nil, errors.New("Commissioner HQ provider server is invalid")
	}
	listener, err := net.Listen("tcp", config.Address)
	if err != nil {
		return nil, errors.New("Commissioner HQ provider listener could not bind")
	}
	runtime := &commissionerHQV1Runtime{server: server, listener: listener}
	runtime.listening.Store(true)
	return runtime, nil
}

func (runtime *commissionerHQV1Runtime) Serve() error {
	if runtime == nil || runtime.server == nil || runtime.listener == nil {
		return errors.New("Commissioner HQ provider runtime is unavailable")
	}
	err := runtime.server.Serve(runtime.listener)
	runtime.listening.Store(false)
	return err
}

func (runtime *commissionerHQV1Runtime) Shutdown(ctx context.Context) error {
	if runtime == nil || runtime.server == nil {
		return nil
	}
	err := runtime.server.Shutdown(ctx)
	runtime.listening.Store(false)
	return err
}

func (runtime *commissionerHQV1Runtime) Listening() bool {
	return runtime != nil && runtime.listening.Load()
}

func buildCommissionerHQV1Runtime(config v1provider.Config, service *league.Service, pool *fantasy.Service, stats *openstats.Service) (*commissionerHQV1Runtime, error) {
	if !config.Enabled {
		return nil, nil
	}
	if service == nil || pool == nil || stats == nil {
		return nil, errors.New("Commissioner HQ provider dependencies are unavailable")
	}
	release, err := commissionerHQV1ReleaseSnapshot()
	if err != nil {
		return nil, err
	}
	source, err := league.NewCommissionerSummaryV1Source(league.CommissionerSummaryV1Captures{
		Config: func(context.Context) (league.CommissionerSummaryV1ConfigSnapshot, error) {
			// Blitz is a built-in route/capability in this binary. Its upstream
			// health may degrade, but that cannot erase immutable configuration.
			return service.CommissionerSummaryV1Config(config.InstanceID, config.LeagueID, true)
		},
		Store: func(context.Context) (league.PersistedState, error) {
			return service.CommissionerSummaryV1State()
		},
		Data: func(context.Context) (league.CommissionerSummaryV1DataSnapshot, error) {
			poolStatus := pool.Status()
			schedule := stats.ScheduleSnapshot()
			return commissionerHQV1DataSnapshot(poolStatus, schedule), nil
		},
		Release: func(context.Context) (league.CommissionerSummaryV1ReleaseSnapshot, error) {
			return release, nil
		},
		Clock: time.Now,
	})
	if err != nil {
		return nil, errors.New("Commissioner HQ provider source is invalid")
	}
	return newCommissionerHQV1Runtime(config, source)
}

func commissionerHQV1ReleaseSnapshot() (league.CommissionerSummaryV1ReleaseSnapshot, error) {
	gitSHA := strings.TrimSpace(appGitSHA)
	if !commissionerHQV1GitSHA.MatchString(gitSHA) || gitSHA == strings.Repeat("0", 40) || gitSHA == strings.Repeat("f", 40) {
		return league.CommissionerSummaryV1ReleaseSnapshot{}, errors.New("Commissioner HQ provider release identity is invalid")
	}
	builtAt, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(appBuildDate))
	if err != nil {
		return league.CommissionerSummaryV1ReleaseSnapshot{}, errors.New("Commissioner HQ provider build time is invalid")
	}
	builtAt = builtAt.UTC()
	digest := strings.TrimSpace(os.Getenv("APP_IMAGE_DIGEST"))
	if digest != "" && (!commissionerHQV1Digest.MatchString(digest) || digest == "sha256:"+strings.Repeat("0", 64) || digest == "sha256:"+strings.Repeat("f", 64)) {
		return league.CommissionerSummaryV1ReleaseSnapshot{}, errors.New("Commissioner HQ provider image digest is invalid")
	}
	return league.CommissionerSummaryV1ReleaseSnapshot{GitSHA: gitSHA, BuiltAt: builtAt, ImageDigest: digest}, nil
}

func commissionerHQV1DataSnapshot(status fantasy.Status, schedule openstats.ScheduleSnapshot) league.CommissionerSummaryV1DataSnapshot {
	data := league.CommissionerSummaryV1DataSnapshot{Games: leagueGamesFromScheduleSnapshot(schedule)}
	if status.LastSync.IsZero() {
		data.Quality = "not_reported"
		return data
	}
	state := strings.ToLower(strings.TrimSpace(status.State))
	switch state {
	case "live":
		data.Quality = "healthy"
	case "cached", "stale":
		data.Quality = "degraded"
		data.DegradationCode = "stale"
	case "degraded":
		data.Quality = "degraded"
		data.DegradationCode = "partial"
	case "offline", "unavailable":
		data.Quality = "degraded"
		data.DegradationCode = "unreachable"
	default:
		data.Quality = "not_reported"
		return data
	}
	players := status.Players
	data.SourceMode = strings.ToLower(strings.TrimSpace(status.Mode))
	data.SourceState = state
	data.PlayerCount = &players
	data.LastSuccessAt = status.LastSync.UTC()
	data.AsOf = status.LastSync.UTC()
	return data
}

func leagueGamesFromScheduleSnapshot(snapshot openstats.ScheduleSnapshot) []league.GameInfo {
	eastern := openStatsEastern()
	out := make([]league.GameInfo, 0, len(snapshot.Games))
	for _, game := range snapshot.Games {
		if game.GameType != "REG" {
			continue
		}
		kickoff, ok := openStatsKickoff(game, eastern)
		if !ok {
			continue
		}
		info := league.GameInfo{
			ID:               game.GameID,
			Week:             game.Week,
			Kickoff:          kickoff,
			Away:             strings.ToUpper(game.AwayTeam),
			Home:             strings.ToUpper(game.HomeTeam),
			AwayScore:        int(game.AwayScore),
			HomeScore:        int(game.HomeScore),
			Final:            game.HasResult(),
			ScoresPresent:    game.HasFinalScore(),
			SourceObservedAt: snapshot.ObservedAt,
			SourceUpdatedAt:  snapshot.UpdatedAt,
			SourceURL:        snapshot.SourceURL,
			SourceProvenance: snapshot.Provenance,
		}
		if game.SpreadLine != nil {
			info.SpreadLinePresent = true
			info.SpreadLineTenths = int(math.Round(*game.SpreadLine * 10))
		}
		out = append(out, info)
	}
	return out
}
