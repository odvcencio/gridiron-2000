package fantasy

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// buildDeepADPPunterPoolFixture builds the synthetic scenario the
// draft-blocking punter bug actually reproduces on the flagship: an ADP
// head deep enough (rankedCount) to fill a pool limit on its own, plus a
// rest tier holding punterCount Position "P" players that each carry a
// real (house) projection but, being punters, no ADP at all — mirroring
// Tank01's live feed, where a punter never appears in getNFLADP. A
// handful of zero-projection, non-punter filler players are mixed into
// the rest tier too, so a floors test also proves punters outrank them
// for the backfilled slots on projection alone, not merely on being the
// only rest-tier occupants.
func buildDeepADPPunterPoolFixture(rankedCount, punterCount, fillerCount int) (base map[string]Player, adp []adpEntry, projections map[string]projEntry) {
	base = make(map[string]Player, rankedCount+punterCount+fillerCount)
	adp = make([]adpEntry, 0, rankedCount)
	projections = make(map[string]projEntry, punterCount)

	rankedPositions := []string{"WR", "RB", "QB", "TE"}
	for i := 0; i < rankedCount; i++ {
		id := fmt.Sprintf("ranked-%03d", i)
		base[id] = Player{
			ID:       id,
			Name:     fmt.Sprintf("Ranked Player %03d", i),
			Position: rankedPositions[i%len(rankedPositions)],
			NFLTeam:  "FA",
		}
		adp = append(adp, adpEntry{PlayerID: id, Name: base[id].Name, Position: base[id].Position, Team: "FA", ADP: float64(i + 1)})
	}

	// Named after the three real house punters the draft-blocking bug
	// report cited (Tommy Townsend, Ryan Rehkow, Austin McNamara), then a
	// synthetic tail — every punter's projection is unique and strictly
	// descending, so backfill order is unambiguous and independently
	// checkable against the input.
	names := []struct {
		name string
		team string
	}{
		{"Tommy Townsend", "TEN"},
		{"Ryan Rehkow", "CIN"},
		{"Austin McNamara", "NYJ"},
	}
	for i := 0; i < punterCount; i++ {
		id := fmt.Sprintf("punter-%03d", i)
		name := fmt.Sprintf("Punter %03d", i)
		team := "FA"
		if i < len(names) {
			name = names[i].name
			team = names[i].team
		}
		base[id] = Player{ID: id, Name: name, Position: "P", NFLTeam: team}
		projections[id] = projEntry{Points: 8.75 - float64(i)*0.05}
	}

	for i := 0; i < fillerCount; i++ {
		id := fmt.Sprintf("filler-%03d", i)
		base[id] = Player{ID: id, Name: fmt.Sprintf("Filler Player %03d", i), Position: "WR", NFLTeam: "FA"}
	}
	return base, adp, projections
}

// TestMergePoolAppliesPositionFloors is the draft-blocking fix's own
// regression test: an ADP head (767 entries) deep enough to fill the pool
// limit (340) on its own must no longer cut every punter, once floors
// carries a "P" minimum. See mergePool's "floors" doc comment.
func TestMergePoolAppliesPositionFloors(t *testing.T) {
	const (
		rankedCount = 767
		punterCount = 55
		fillerCount = 20
		limit       = 340
		punterFloor = 12
	)
	base, adp, projections := buildDeepADPPunterPoolFixture(rankedCount, punterCount, fillerCount)

	pool := mergePool(base, adp, projections, nil, nil, limit, nil, map[string]int{"P": punterFloor})

	if len(pool) != limit+punterFloor {
		t.Fatalf("pool length = %d, want %d (limit %d + punter floor shortfall %d)", len(pool), limit+punterFloor, limit, punterFloor)
	}

	var punters []Player
	seen := make(map[string]bool, len(pool))
	for _, player := range pool {
		if seen[player.ID] {
			t.Fatalf("duplicate ID %s in pool", player.ID)
		}
		seen[player.ID] = true
		if player.Position == "P" {
			punters = append(punters, player)
		}
	}
	if len(punters) != punterFloor {
		t.Fatalf("punters kept = %d, want the floor %d", len(punters), punterFloor)
	}

	// The floor's own doc comment promises "next-best... in their existing
	// order": the rest tier is projection-descending, so the 12 kept
	// punters must be exactly the 12 highest-projection punters, in that
	// order, each with the house rank (PunterRank) matching its position.
	for i, punter := range punters {
		wantID := fmt.Sprintf("punter-%03d", i)
		if punter.ID != wantID {
			t.Errorf("kept punter[%d] = %s, want %s (highest projections first)", i, punter.ID, wantID)
		}
		if punter.PunterRank != i+1 {
			t.Errorf("kept punter[%d] (%s) PunterRank = %d, want %d", i, punter.ID, punter.PunterRank, i+1)
		}
	}

	// Every non-punter position's kept slice (the first `limit` entries)
	// must be byte-for-byte what mergePool would have produced with no
	// floors at all: the floor backfill is strictly additive, appended
	// beyond the cut, never reordering or displacing the original cut.
	baseline := mergePool(base, adp, projections, nil, nil, limit, nil, nil)
	if !reflect.DeepEqual(pool[:limit], baseline) {
		t.Fatalf("pool[:limit] diverged from the no-floors baseline; floors must be strictly additive beyond the cut")
	}
}

// TestMergePoolZeroOrAbsentFloorsMatchesPreFix is finding (b): a floors
// map with no entry for a position, a floor of exactly zero, and a nil
// floors argument must all three produce identical output to mergePool's
// pre-fix behavior (a plain pool[:limit] truncation, no backfill).
func TestMergePoolZeroOrAbsentFloorsMatchesPreFix(t *testing.T) {
	const limit = 340
	base, adp, projections := buildDeepADPPunterPoolFixture(767, 55, 20)

	nilFloors := mergePool(base, adp, projections, nil, nil, limit, nil, nil)
	absentFloors := mergePool(base, adp, projections, nil, nil, limit, nil, map[string]int{})
	zeroFloor := mergePool(base, adp, projections, nil, nil, limit, nil, map[string]int{"P": 0})
	otherPositionFloor := mergePool(base, adp, projections, nil, nil, limit, nil, map[string]int{"K": 0, "DST": 0})

	if len(nilFloors) != limit {
		t.Fatalf("nil floors: pool length = %d, want the plain limit %d", len(nilFloors), limit)
	}
	for name, got := range map[string][]Player{
		"empty map":        absentFloors,
		"P floor of zero":  zeroFloor,
		"unrelated floors": otherPositionFloor,
	} {
		if !reflect.DeepEqual(got, nilFloors) {
			t.Errorf("%s produced a pool different from the nil-floors baseline", name)
		}
	}
}

// TestBackfillPositionFloorsSkipsMetAndUnflooredPositions is
// backfillPositionFloors' own unit test for a multi-position floor: it
// must walk every "beyond" candidate in order, taking only the ones that
// still owe their position a floor slot — skipping both an unfloored
// position (no entry in floors) and a floored position whose minimum an
// earlier candidate already satisfied — and it must never mutate the
// original pool argument's backing array while doing it (kept is an
// explicit copy; see backfillPositionFloors' own doc comment).
func TestBackfillPositionFloorsSkipsMetAndUnflooredPositions(t *testing.T) {
	pool := []Player{
		{ID: "k1", Position: "A"},
		{ID: "k2", Position: "B"},
		{ID: "n1", Position: "N"}, // unfloored position: always skipped
		{ID: "x1", Position: "X", Projection: 9},
		{ID: "x2", Position: "X", Projection: 8}, // X's floor of 1 is already met by x1
		{ID: "n2", Position: "N"},                // unfloored position: always skipped
		{ID: "y1", Position: "Y", Projection: 7},
	}
	original := append([]Player(nil), pool...)
	floors := map[string]int{"X": 1, "Y": 1}

	got := backfillPositionFloors(pool, 2, floors)

	want := []Player{
		{ID: "k1", Position: "A"},
		{ID: "k2", Position: "B"},
		{ID: "x1", Position: "X", Projection: 9},
		{ID: "y1", Position: "Y", Projection: 7},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("backfillPositionFloors(pool, 2, floors) =\n%+v\nwant\n%+v", got, want)
	}
	if !reflect.DeepEqual(pool, original) {
		t.Fatalf("backfillPositionFloors mutated its pool argument: got %+v, want unchanged %+v", pool, original)
	}
}

// deepADPPositionFloorStub serves a getNFLADP list deep enough (5 ranked
// WRs) to fill a small PoolLimit (3) entirely from the ranked head alone —
// the exact shape of the draft-blocking bug — plus one Position "P"
// player getNFLADP never lists at all, mirroring the live Tank01 feed.
func deepADPPositionFloorStub(t *testing.T) *httptest.Server {
	t.Helper()
	players := `{"playerID":"1","longName":"Ranked One","pos":"WR","team":"CIN"},
		{"playerID":"2","longName":"Ranked Two","pos":"WR","team":"CIN"},
		{"playerID":"3","longName":"Ranked Three","pos":"WR","team":"CIN"},
		{"playerID":"4","longName":"Ranked Four","pos":"WR","team":"CIN"},
		{"playerID":"5","longName":"Ranked Five","pos":"WR","team":"CIN"},
		{"playerID":"9","longName":"Zed Punter","pos":"P","team":"HOU"}`
	adp := `{"playerID":"1","adp":"1.0"},{"playerID":"2","adp":"2.0"},
		{"playerID":"3","adp":"3.0"},{"playerID":"4","adp":"4.0"},{"playerID":"5","adp":"5.0"}`
	payloads := map[string]string{
		"/getNFLPlayerList":  `{"statusCode":200,"body":[` + players + `]}`,
		"/getNFLADP":         `{"statusCode":200,"body":{"adpList":[` + adp + `]}}`,
		"/getNFLProjections": `{"statusCode":200,"body":{"playerProjections":{}}}`,
		"/getNFLNews":        `{"statusCode":200,"body":[]}`,
		"/getNFLTeams":       `{"statusCode":200,"body":[]}`,
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload, ok := payloads[r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(payload))
	}))
}

// TestSyncNowAppliesPositionFloors is the wiring test end to end through a
// real sync: SetPositionFloors, followed by SyncNow, must produce a synced
// pool that keeps the floored punter even though PoolLimit (3) is smaller
// than the ranked ADP head (5) alone — matching app_build.go's real
// SetPunterProjections/SetPositionFloors/SyncNow ordering.
func TestSyncNowAppliesPositionFloors(t *testing.T) {
	root := t.TempDir()
	server := deepADPPositionFloorStub(t)
	defer server.Close()
	target, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(Config{
		APIKey:        "test-key",
		Root:          root,
		Season:        2026,
		ScoringFormat: "half_ppr",
		PoolLimit:     3,
		HTTPClient:    &http.Client{Transport: stubTransport{target: target}},
	})
	if err != nil {
		t.Fatal(err)
	}
	service.SetPunterProjections(stubPunterHook(map[string]float64{"Zed Punter": 8.75}))
	service.SetPositionFloors(map[string]int{"P": 1})

	if err := service.SyncNow(context.Background()); err != nil {
		t.Fatalf("SyncNow: %v", err)
	}
	players, _ := service.Players()
	if len(players) != 4 {
		t.Fatalf("players = %d, want 4 (PoolLimit 3 + punter floor shortfall 1)", len(players))
	}
	found := false
	for _, p := range players {
		if p.ID != "9" {
			continue
		}
		found = true
		if p.Projection != 8.75 {
			t.Errorf("synced punter Projection = %v, want 8.75", p.Projection)
		}
		if p.PunterRank != 1 {
			t.Errorf("synced punter PunterRank = %d, want 1", p.PunterRank)
		}
	}
	if !found {
		t.Fatalf("floored punter missing from a real SyncNow's synced pool: %+v", players)
	}
}

// TestSetPositionFloorsDoesNotReNormalizeCacheLoadedPool is the cache-path
// half of SetPositionFloors' own contract (see its doc comment): unlike
// SetPunterProjections, it must NOT retroactively touch whatever pool
// NewService's loadCache already installed — that pool was already
// truncated to its final, persisted shape before floors ever existed, so
// there is no beyond-the-cut candidate left to recover it from. version
// must stay exactly where loadCache left it; only a real SyncNow may
// change the installed pool from here.
func TestSetPositionFloorsDoesNotReNormalizeCacheLoadedPool(t *testing.T) {
	root := t.TempDir()
	cache := poolCache{
		SchemaVersion: SchemaVersion,
		Provider:      "tank01",
		Scoring:       "half_ppr",
		Players: []Player{
			{ID: "wr1", Name: "Cached Receiver", Position: "WR", NFLTeam: "CIN", ADP: 1.1, ADPRank: 1, Projection: 15.0},
		},
	}
	encoded, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "players.json"), encoded, 0o600); err != nil {
		t.Fatal(err)
	}

	service, err := NewService(Config{Root: root, Season: 2026, ScoringFormat: "half_ppr"})
	if err != nil {
		t.Fatal(err)
	}
	before, beforeVersion := service.Players()

	service.SetPositionFloors(map[string]int{"P": 12})

	after, afterVersion := service.Players()
	if afterVersion != beforeVersion {
		t.Fatalf("SetPositionFloors bumped the pool version from %d to %d; it must not re-normalize a cache-loaded pool", beforeVersion, afterVersion)
	}
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("SetPositionFloors changed the cache-loaded pool: before %+v, after %+v", before, after)
	}
}
