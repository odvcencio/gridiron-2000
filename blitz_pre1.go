package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"gridiron-2000/internal/fantasy"
	"gridiron-2000/internal/league"
)

// blitzPre1Label is the Tank01 gameWeek label for the finished
// preseason-week-1 slate. Every regular-season week's fantasy pool sync
// already leans on label matching over the request's "week" param (P1,
// blitz_source.go); pre1 follows the same rule rather than trusting one
// hardcoded param value.
const blitzPre1Label = "Preseason Week 1"

// blitzPre1WeekProbeParams are the Tank01 "week" values tried to find
// blitzPre1Label's games, mirroring blitzWeekProbeParams' probe-a-spread
// approach. Verified live 2026-08-16: week=2 resolved "Preseason Week 1"
// (the same P1 offset blitzWeekProbeParams documents for pre2/pre3); the
// probe still tries a spread rather than hardcoding that one value, in
// case Tank01's offset shifts across a season boundary.
var blitzPre1WeekProbeParams = []string{"1", "2", "3"}

// blitzPre1Path is where the computed preseason-week-1 production map
// persists. Week 1 is complete and its box scores are immutable, so one
// file — not blitz_source.go's per-game cache dir — is enough; a missing
// or stale (see blitzPre1StaleAfter) file triggers a recompute (see
// loadOrComputeBlitzPre1). It is a var, not a const, so
// blitz_pre1_test.go can point it at a temp file without touching the
// repo's own data/ directory.
var blitzPre1Path = "data/blitz/pre1.json"

// blitzPre1StaleAfter caps how long the on-disk cache is trusted without
// a recompute. Week 1's box scores never change once final, so this is a
// generous safety margin against a partial first compute (some games'
// box-score fetches failed) rather than a freshness requirement.
const blitzPre1StaleAfter = 24 * time.Hour

// blitzPre1Cache is pre1.json's on-disk shape (F22's atomic-write
// pattern: temp file, chmod 0600, rename — matching blitzBoxScoreCache
// in blitz_source.go).
type blitzPre1Cache struct {
	Stats      map[string]map[string]float64 `json:"stats"`
	ComputedAt time.Time                     `json:"computedAt"`
}

// startBlitzPre1 resolves preseason-week-1 production once at boot: the
// on-disk cache when it is fresh, else a fetch-and-merge pass over every
// "Preseason Week 1" box score. It runs synchronously within its own
// goroutine (the caller backgrounds it — see main.go) and finishes
// quickly, a handful of REST calls against already-final games; unlike
// startBlitzPoller's live ticker, week 1 never changes again so there is
// nothing to keep polling. A total failure degrades honestly: one log
// line, league.SetBlitzPre1Source is never called, and the board falls
// back to its non-pre1 ordering (see league.BlitzPre1Source's doc
// comment) — no crash either way.
func startBlitzPre1(ctx context.Context, pool *fantasy.Service, lg *league.Service) {
	if !pool.Enabled() {
		log.Printf("blitz: pre1 skipped — TANK01_API_KEY is not set")
		return
	}
	stats, ok := loadOrComputeBlitzPre1(ctx, pool, time.Now())
	if !ok {
		return
	}
	lg.SetBlitzPre1Source(func() map[string]map[string]float64 { return stats })
}

// loadOrComputeBlitzPre1 returns the pre1 stat map to serve: the disk
// cache when it exists and is fresher than blitzPre1StaleAfter, else a
// fresh compute. A fresh-compute failure falls back to a stale cache when
// one exists (partial or old data beats none); with no cache at all it
// returns ok=false, having already logged the one required line.
func loadOrComputeBlitzPre1(ctx context.Context, pool *fantasy.Service, now time.Time) (map[string]map[string]float64, bool) {
	cache, cacheErr := readBlitzPre1Cache()
	if cacheErr == nil && now.Sub(cache.ComputedAt) < blitzPre1StaleAfter {
		return cache.Stats, true
	}
	stats, err := computeBlitzPre1(ctx, pool)
	if err != nil {
		if cacheErr == nil {
			log.Printf("blitz: pre1 refresh failed (%v); serving the cached pre1 data from %s", err, cache.ComputedAt.Format(time.RFC3339))
			return cache.Stats, true
		}
		log.Printf("blitz: pre1 data unavailable (%v); the board falls back to its non-pre1 ordering", err)
		return nil, false
	}
	if err := writeBlitzPre1Cache(stats, now); err != nil {
		log.Printf("blitz: pre1 cache write failed: %v", err)
	}
	return stats, true
}

// computeBlitzPre1 fetches every "Preseason Week 1" game and merges each
// final game's box score into one playerID -> stat map — the same raw
// shape blitz_source.go's live poller produces, so league.
// blitzEligiblePlayers scores both through the identical
// scoreStatsWithValues call. A probe that finds no games at all is an
// error (nothing to serve); one game's box-score fetch failing is logged
// and skipped so the rest of the week still counts — partial pre1 data
// beats none.
func computeBlitzPre1(ctx context.Context, pool *fantasy.Service) (map[string]map[string]float64, error) {
	var games []fantasy.PreseasonGame
	for _, weekParam := range blitzPre1WeekProbeParams {
		fetched, err := pool.FetchPreseasonWeek(ctx, weekParam)
		if err != nil {
			log.Printf("blitz: pre1 probe week=%s failed: %v", weekParam, err)
			continue
		}
		if matched := fantasy.SelectPreseasonGames(fetched, blitzPre1Label); len(matched) > 0 {
			games = matched
			break
		}
	}
	if len(games) == 0 {
		return nil, fmt.Errorf("no week param returned games labeled %q", blitzPre1Label)
	}

	out := map[string]map[string]float64{}
	fetchedAny := false
	for _, game := range games {
		if !game.Final {
			continue
		}
		stats, _, err := pool.FetchPreseasonBoxScore(ctx, game.ID)
		if err != nil {
			log.Printf("blitz: pre1 box score fetch for %s failed: %v", game.ID, err)
			continue
		}
		fetchedAny = true
		for playerID, line := range stats {
			out[playerID] = line
		}
	}
	if !fetchedAny {
		return nil, fmt.Errorf("every %s box-score fetch failed", blitzPre1Label)
	}
	return out, nil
}

// readBlitzPre1Cache reads and decodes blitzPre1Path. Any error (missing
// file, malformed JSON) is returned as-is; the caller treats every error
// alike — "no usable cache" — rather than distinguishing causes.
func readBlitzPre1Cache() (blitzPre1Cache, error) {
	raw, err := os.ReadFile(blitzPre1Path)
	if err != nil {
		return blitzPre1Cache{}, err
	}
	var cache blitzPre1Cache
	if err := json.Unmarshal(raw, &cache); err != nil {
		return blitzPre1Cache{}, err
	}
	return cache, nil
}

// writeBlitzPre1Cache writes stats to blitzPre1Path (F22's atomic-write
// pattern: temp file in the same directory, chmod 0600, rename).
func writeBlitzPre1Cache(stats map[string]map[string]float64, computedAt time.Time) error {
	dir := filepath.Dir(blitzPre1Path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(blitzPre1Cache{Stats: stats, ComputedAt: computedAt}, "", "  ")
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(dir, ".pre1-*.json")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return err
	}
	if _, err := temp.Write(encoded); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, blitzPre1Path)
}
