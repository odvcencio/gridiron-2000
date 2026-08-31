// Package main implements seasonprobe, a read-only, operator-run upstream
// probe (GC-2b item 1). It never mutates league state and never writes to
// Tank01/statrelay beyond the handful of GET calls its own checks make; an
// agent must never run it against a real relay or production endpoint —
// only against fixtures, through this package's own tests.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gridiron-2000/internal/fantasy"
	"gridiron-2000/internal/livescore"
	"gridiron-2000/internal/openstats"
)

// ProbeConfig drives one run. Every field is operator-supplied (flags or
// env, see main.go); nothing here has a network-reaching default.
type ProbeConfig struct {
	// Tank01BaseURL is TANK01_BASE_URL: the statrelay this probe reads
	// through, exactly the way the live poller and the fantasy pool do.
	// Required.
	Tank01BaseURL string
	// OpenStatsRoot is OPEN_STATS_ROOT: the local nflverse mirror
	// directory this probe reads games.csv from — read-only, no network
	// fetch of its own (see openstats.NewService with Enabled=false).
	OpenStatsRoot string
	// Season is the NFL season both checks target (spec: the 2026 season;
	// overridable for a fixture test against a different season number).
	Season int
	// Week is the regular-season week whose games list check 1 diffs
	// against the mirror (spec: week 1).
	Week int
	// PreseasonWeek is the Tank01 "week" query param check 2 searches for
	// a completed game (spec: one completed 2026 preseason box score).
	// This is a request hint only (P1, see fantasy.SelectPreseasonGames'
	// own doc comment); the probe accepts the first game the response
	// itself reports Final, whichever week label it actually carries.
	PreseasonWeek string
	// CaptureDir, when non-empty, receives both raw payloads this probe
	// fetches (the games-list body and the box-score body), each written
	// as its own timestamped .json file — the evidence an operator
	// attaches to the release receipt.
	CaptureDir string
	// HTTPClient is the client every HTTP call in this package uses; nil
	// defaults to http.DefaultClient. Tests inject one pointed at an
	// httptest.Server.
	HTTPClient *http.Client
	// Now is the clock capture filenames use; nil defaults to time.Now.
	Now func() time.Time
}

// Result is one named check's outcome.
type Result struct {
	Name   string
	Pass   bool
	Detail string
}

// Run performs every check in order and returns their results. It never
// returns early on a failed check — every check that CAN run, does, so
// an operator sees the full picture in one pass — but does return an
// error for a setup failure so fundamental no check could possibly run
// (an empty Tank01BaseURL or OpenStatsRoot).
func Run(ctx context.Context, cfg ProbeConfig) ([]Result, error) {
	cfg.Tank01BaseURL = strings.TrimSpace(cfg.Tank01BaseURL)
	cfg.OpenStatsRoot = strings.TrimSpace(cfg.OpenStatsRoot)
	if cfg.Tank01BaseURL == "" {
		return nil, fmt.Errorf("seasonprobe: TANK01_BASE_URL (or --tank01-base-url) is required")
	}
	if cfg.OpenStatsRoot == "" {
		return nil, fmt.Errorf("seasonprobe: OPEN_STATS_ROOT (or --open-stats-root) is required")
	}
	if cfg.Season <= 0 {
		cfg.Season = time.Now().Year()
	}
	if cfg.Week <= 0 {
		cfg.Week = 1
	}
	if strings.TrimSpace(cfg.PreseasonWeek) == "" {
		cfg.PreseasonWeek = "1"
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = http.DefaultClient
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.CaptureDir != "" {
		if err := os.MkdirAll(cfg.CaptureDir, 0o755); err != nil {
			return nil, fmt.Errorf("seasonprobe: create capture dir: %w", err)
		}
	}

	client, err := fantasy.NewBoxScoreClient(cfg.Tank01BaseURL, cfg.Season, cfg.HTTPClient, 0)
	if err != nil {
		return nil, fmt.Errorf("seasonprobe: %w", err)
	}

	var results []Result
	results = append(results, checkScheduleMatch(ctx, cfg, client)...)
	results = append(results, checkPreseasonBox(ctx, cfg, client)...)
	return results, nil
}

// checkScheduleMatch is GC-2b item 1's first half: fetch the season's
// regular-season week-1 games list, diff team pairs and Eastern dates
// against the mirrored nflverse games.csv, and capture the raw payload.
func checkScheduleMatch(ctx context.Context, cfg ProbeConfig, client *fantasy.BoxScoreClient) []Result {
	var out []Result

	raw, err := fetchRaw(ctx, cfg.HTTPClient, cfg.Tank01BaseURL, "getNFLGamesForWeek", map[string]string{
		"week": weekParam(cfg.Week), "seasonType": "reg", "season": fmt.Sprint(cfg.Season),
	})
	if err != nil {
		out = append(out, Result{Name: "fetch regular-season week games list", Pass: false, Detail: err.Error()})
		return out
	}
	out = append(out, Result{Name: "fetch regular-season week games list", Pass: true, Detail: fmt.Sprintf("%d bytes", len(raw))})
	writeCapture(cfg, fmt.Sprintf("games-week%d-season%d", cfg.Week, cfg.Season), raw)

	listings, err := client.FetchGamesForWeek(ctx, "reg", weekParam(cfg.Week))
	if err != nil {
		out = append(out, Result{Name: "parse regular-season week games list", Pass: false, Detail: err.Error()})
		return out
	}
	out = append(out, Result{Name: "parse regular-season week games list", Pass: len(listings) > 0,
		Detail: fmt.Sprintf("%d games", len(listings))})

	mirror, err := openstats.NewService(openstats.Config{Root: cfg.OpenStatsRoot, Season: cfg.Season, Enabled: false})
	if err != nil {
		out = append(out, Result{Name: "read mirrored games.csv", Pass: false, Detail: err.Error()})
		return out
	}
	schedule := mirror.Games(cfg.Week)
	var mirrorKeys []scheduleKey
	for _, game := range schedule {
		if !strings.EqualFold(strings.TrimSpace(game.GameType), "REG") {
			continue
		}
		key, ok := keyForScheduleGame(game)
		if !ok {
			continue
		}
		mirrorKeys = append(mirrorKeys, key)
	}
	out = append(out, Result{Name: "read mirrored games.csv", Pass: len(mirrorKeys) > 0,
		Detail: fmt.Sprintf("%d week-%d REG rows under %s", len(mirrorKeys), cfg.Week, cfg.OpenStatsRoot)})

	tank01Keys := make([]scheduleKey, 0, len(listings))
	for _, listing := range listings {
		tank01Keys = append(tank01Keys, keyForTank01Listing(listing))
	}
	missingFromMirror, missingFromTank01 := diffScheduleKeys(tank01Keys, mirrorKeys)
	pass := len(missingFromMirror) == 0 && len(missingFromTank01) == 0
	detail := fmt.Sprintf("%d Tank01 games, %d mirror games matched", len(tank01Keys)-len(missingFromMirror), len(mirrorKeys))
	if !pass {
		detail = fmt.Sprintf("in Tank01 but not the mirror: %s; in the mirror but not Tank01: %s",
			joinKeys(missingFromMirror), joinKeys(missingFromTank01))
	}
	out = append(out, Result{Name: "diff team pairs + Eastern dates (Tank01 vs mirrored games.csv)", Pass: pass, Detail: detail})
	return out
}

// checkPreseasonBox is GC-2b item 1's second half: fetch one completed
// 2026 preseason box score (the game is picked from a listing call, per
// spec) and run it through the existing box parser.
func checkPreseasonBox(ctx context.Context, cfg ProbeConfig, client *fantasy.BoxScoreClient) []Result {
	var out []Result

	listings, err := client.FetchGamesForWeek(ctx, "pre", cfg.PreseasonWeek)
	if err != nil {
		out = append(out, Result{Name: "fetch preseason games listing", Pass: false, Detail: err.Error()})
		return out
	}
	out = append(out, Result{Name: "fetch preseason games listing", Pass: len(listings) > 0,
		Detail: fmt.Sprintf("%d games in preseason week %s", len(listings), cfg.PreseasonWeek)})

	var gameID string
	for _, listing := range listings {
		if listing.Final {
			gameID = listing.ID
			break
		}
	}
	if gameID == "" {
		out = append(out, Result{Name: "pick a completed preseason game", Pass: false,
			Detail: fmt.Sprintf("no completed (Final) game in preseason week %s; try a different --preseason-week", cfg.PreseasonWeek)})
		return out
	}
	out = append(out, Result{Name: "pick a completed preseason game", Pass: true, Detail: gameID})

	raw, err := fetchRaw(ctx, cfg.HTTPClient, cfg.Tank01BaseURL, "getNFLBoxScore", map[string]string{"gameID": gameID})
	if err != nil {
		out = append(out, Result{Name: "fetch preseason box score", Pass: false, Detail: err.Error()})
		return out
	}
	out = append(out, Result{Name: "fetch preseason box score", Pass: true, Detail: fmt.Sprintf("%d bytes", len(raw))})
	writeCapture(cfg, "preseason-box-"+sanitizeFilename(gameID), raw)

	box := fantasy.ParseBoxScore(raw)
	pass := box.Final && (len(box.Players) > 0 || len(box.DST) > 0)
	out = append(out, Result{Name: "parse preseason box score", Pass: pass,
		Detail: fmt.Sprintf("gameID=%s final=%v players=%d dst=%d", box.GameID, box.Final, len(box.Players), len(box.DST))})
	return out
}

// scheduleKey is the date-plus-team-pair identity match.go's own
// matchGames uses (internal/livescore/match.go), reproduced here so this
// probe validates the exact same normalization the live poller relies
// on for the season.
type scheduleKey struct {
	date, away, home string
}

func (k scheduleKey) String() string { return k.date + " " + k.away + "@" + k.home }

func keyForTank01Listing(listing fantasy.GameListing) scheduleKey {
	return scheduleKey{
		date: strings.TrimSpace(listing.Date),
		away: livescore.NormalizeTeam(listing.Away),
		home: livescore.NormalizeTeam(listing.Home),
	}
}

// keyForScheduleGame reads one mirrored games.csv row's own gameday
// column (already an Eastern local calendar date, nflverse's own
// convention) into the same "20060102" shape Tank01's gameDate uses.
func keyForScheduleGame(game openstats.ScheduleGame) (scheduleKey, bool) {
	day := strings.TrimSpace(game.GameDay)
	parsed, err := time.Parse("2006-01-02", day)
	if err != nil {
		return scheduleKey{}, false
	}
	return scheduleKey{
		date: parsed.Format("20060102"),
		away: livescore.NormalizeTeam(game.AwayTeam),
		home: livescore.NormalizeTeam(game.HomeTeam),
	}, true
}

// diffScheduleKeys reports which tank01 keys have no counterpart in
// mirror and vice versa.
func diffScheduleKeys(tank01, mirror []scheduleKey) (missingFromMirror, missingFromTank01 []scheduleKey) {
	mirrorSet := make(map[scheduleKey]bool, len(mirror))
	for _, key := range mirror {
		mirrorSet[key] = true
	}
	tank01Set := make(map[scheduleKey]bool, len(tank01))
	for _, key := range tank01 {
		tank01Set[key] = true
		if !mirrorSet[key] {
			missingFromMirror = append(missingFromMirror, key)
		}
	}
	for _, key := range mirror {
		if !tank01Set[key] {
			missingFromTank01 = append(missingFromTank01, key)
		}
	}
	sort.Slice(missingFromMirror, func(i, j int) bool { return missingFromMirror[i].String() < missingFromMirror[j].String() })
	sort.Slice(missingFromTank01, func(i, j int) bool { return missingFromTank01[i].String() < missingFromTank01[j].String() })
	return missingFromMirror, missingFromTank01
}

func joinKeys(keys []scheduleKey) string {
	if len(keys) == 0 {
		return "(none)"
	}
	texts := make([]string, len(keys))
	for i, key := range keys {
		texts[i] = key.String()
	}
	return strings.Join(texts, ", ")
}

func weekParam(week int) string { return fmt.Sprint(week) }

// fetchRaw performs one unauthenticated GET against baseURL/endpoint —
// statrelay holds the real Tank01 credential itself (see
// internal/fantasy/tank01Client's own doc comment: a caller pointed at a
// relay needs no key of its own) — and returns the full response body
// verbatim, envelope included, exactly as this probe's own capture files
// preserve it.
func fetchRaw(ctx context.Context, httpClient *http.Client, baseURL, endpoint string, params map[string]string) ([]byte, error) {
	query := url.Values{}
	for key, value := range params {
		if value != "" {
			query.Set(key, value)
		}
	}
	requestURL := strings.TrimRight(baseURL, "/") + "/" + endpoint
	if encoded := query.Encode(); encoded != "" {
		requestURL += "?" + encoded
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	response, err := httpClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: HTTP %d", endpoint, response.StatusCode)
	}
	return body, nil
}

// writeCapture saves raw under cfg.CaptureDir, prefixed with cfg.Now()'s
// timestamp; a no-op when CaptureDir is empty. Write failures are
// reported to stderr by the caller (main.go), never fatal to the probe
// itself — a capture is diagnostic evidence, not a check.
func writeCapture(cfg ProbeConfig, name string, raw []byte) error {
	if cfg.CaptureDir == "" {
		return nil
	}
	stamp := cfg.Now().UTC().Format("20060102T150405Z")
	path := filepath.Join(cfg.CaptureDir, fmt.Sprintf("%s-%s.json", stamp, sanitizeFilename(name)))
	pretty, err := reindentJSON(raw)
	if err != nil {
		pretty = raw
	}
	return os.WriteFile(path, pretty, 0o644)
}

// reindentJSON pretty-prints raw for a human operator reviewing a
// capture; a payload that fails to decode as JSON is saved verbatim by
// the caller instead (never dropped).
func reindentJSON(raw []byte) ([]byte, error) {
	var buf bytes.Buffer
	if err := json.Indent(&buf, raw, "", "  "); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func sanitizeFilename(name string) string {
	replacer := strings.NewReplacer("/", "_", "\\", "_", " ", "_", ":", "_", "@", "-")
	return replacer.Replace(strings.TrimSpace(name))
}
