package fantasy

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// stubPunterHook returns a hook that resolves exactly the entries table
// gives it, keyed on the exact name string, so a test can pin both a hit
// and a deliberate miss without touching the real embedded league asset.
// It ignores requireTeam: a name-only fixture table has no notion of a
// live-pool surname collision on its own — TestEnrichPuntersRequiresTeamOn...
// below pins that behavior with a hook that DOES read requireTeam.
func stubPunterHook(entries map[string]float64) func(name, team string, requireTeam bool) (float64, bool) {
	return func(name, team string, requireTeam bool) (float64, bool) {
		perGame, ok := entries[name]
		return perGame, ok
	}
}

// TestNormalizePoolEnrichesZeroProjectionPunter pins the core enrichment
// contract: a Position "P" player carrying zero Projection gets the
// hook's value verbatim (same per-game scale, no rescaling) — the exact
// arithmetic (148.8 / 17 = 8.752941...) mirrors a realistic embedded value.
func TestNormalizePoolEnrichesZeroProjectionPunter(t *testing.T) {
	hookValue := 148.8 / 17.0
	hook := stubPunterHook(map[string]float64{"Tommy Townsend": hookValue})
	pool := []Player{
		{ID: "p1", Name: "Tommy Townsend", Position: "P", NFLTeam: "HOU"},
	}
	out := normalizePool(pool, hook)
	if out[0].Projection != hookValue {
		t.Fatalf("Projection = %v, want exactly %v", out[0].Projection, hookValue)
	}
	if out[0].PunterRank != 1 {
		t.Fatalf("PunterRank = %d, want 1", out[0].PunterRank)
	}
}

// TestNormalizePoolLeavesHookMissedPunterZero pins the miss path: a
// punter the hook does not resolve keeps Projection at zero and PunterRank
// at zero — playerMap (internal/league) renders that as "—", never a
// falsely precise rank.
func TestNormalizePoolLeavesHookMissedPunterZero(t *testing.T) {
	hook := stubPunterHook(map[string]float64{"Someone Else": 9.0})
	pool := []Player{
		{ID: "p1", Name: "Unknown Punter", Position: "P", NFLTeam: "HOU"},
	}
	out := normalizePool(pool, hook)
	if out[0].Projection != 0 {
		t.Fatalf("Projection = %v, want 0 (hook miss)", out[0].Projection)
	}
	if out[0].PunterRank != 0 {
		t.Fatalf("PunterRank = %d, want 0 (unranked on a hook miss)", out[0].PunterRank)
	}
}

// TestNormalizePoolNeverConsultsHookForNonPunterOrAlreadyProjected checks
// the enrichment's two guards: a non-P player never reaches the hook (a
// hook that would panic or return a nonsense value proves this), and a P
// player that already carries a nonzero Projection (a hypothetical future
// real Tank01 punter projection) is left exactly as it was, not
// overwritten by the hook.
func TestNormalizePoolNeverConsultsHookForNonPunterOrAlreadyProjected(t *testing.T) {
	calls := map[string]int{}
	hook := func(name, team string, requireTeam bool) (float64, bool) {
		calls[name]++
		return 99.0, true
	}
	pool := []Player{
		{ID: "wr1", Name: "Some Receiver", Position: "WR", NFLTeam: "CIN", Projection: 0},
		{ID: "p1", Name: "Already Projected Punter", Position: "P", NFLTeam: "HOU", Projection: 7.5},
	}
	out := normalizePool(pool, hook)
	if calls["Some Receiver"] != 0 {
		t.Errorf("hook was consulted for a non-P player: %d calls", calls["Some Receiver"])
	}
	for _, player := range out {
		if player.ID == "wr1" && player.Projection != 0 {
			t.Errorf("non-P player's Projection changed: %+v", player)
		}
		if player.ID == "p1" && player.Projection != 7.5 {
			t.Errorf("already-projected punter's Projection was overwritten: %+v", player)
		}
	}
}

// TestNormalizePoolOrdersPuntersByProjectionAboveZeroProjectionRest pins
// the ordering contract (TestMergePool...-style): once enriched, punters
// sort by projection among themselves, and an enriched punter (nonzero
// projection) sits ahead of the true zero/zero rest tier (unenriched
// camp bodies) — exactly the same rest-tier rule mergePool already applies
// to every other position. PunterRank is assigned 1..N in that same order.
func TestNormalizePoolOrdersPuntersByProjectionAboveZeroProjectionRest(t *testing.T) {
	hook := stubPunterHook(map[string]float64{
		"Low Punter":  6.0,
		"High Punter": 9.0,
	})
	pool := []Player{
		{ID: "camp1", Name: "Aaa Camp Body", Position: "WR", NFLTeam: "CIN"},
		{ID: "low", Name: "Low Punter", Position: "P", NFLTeam: "DAL"},
		{ID: "missed", Name: "Unmatched Punter", Position: "P", NFLTeam: "NYJ"},
		{ID: "high", Name: "High Punter", Position: "P", NFLTeam: "HOU"},
	}
	out := normalizePool(pool, hook)
	ids := make([]string, len(out))
	for i, p := range out {
		ids[i] = p.ID
	}
	// high (9.0) ahead of low (6.0), both ahead of the zero-projection
	// tail (camp1 and missed, alphabetical: "Aaa Camp Body" < "Unmatched
	// Punter").
	want := []string{"high", "low", "camp1", "missed"}
	for i, id := range want {
		if ids[i] != id {
			t.Fatalf("pool order = %v, want %v", ids, want)
		}
	}
	byID := make(map[string]Player, len(out))
	for _, p := range out {
		byID[p.ID] = p
	}
	if byID["high"].PunterRank != 1 {
		t.Errorf("high punter PunterRank = %d, want 1", byID["high"].PunterRank)
	}
	if byID["low"].PunterRank != 2 {
		t.Errorf("low punter PunterRank = %d, want 2", byID["low"].PunterRank)
	}
	if byID["missed"].PunterRank != 0 {
		t.Errorf("unmatched punter PunterRank = %d, want 0", byID["missed"].PunterRank)
	}
	if byID["high"].ADPRank != 0 || byID["low"].ADPRank != 0 {
		t.Errorf("enriched punters must never carry ADPRank (no market ADP): high=%d low=%d", byID["high"].ADPRank, byID["low"].ADPRank)
	}
}

// TestNormalizePoolNeverTouchesADPRankedPlayers checks that an ADP-ranked
// (market-ordered) segment of the pool is left in place — position and
// ADPRank both untouched — by normalizePool's rest-tier re-sort, matching
// mergePool's own "ADP order, never overridden" contract.
func TestNormalizePoolNeverTouchesADPRankedPlayers(t *testing.T) {
	pool := []Player{
		{ID: "adp1", Name: "Market Starter", Position: "WR", NFLTeam: "CIN", ADP: 1.2, ADPRank: 1},
		{ID: "adp2", Name: "Market Second", Position: "RB", NFLTeam: "DET", ADP: 2.4, ADPRank: 2},
		{ID: "p1", Name: "Tail Punter", Position: "P", NFLTeam: "HOU"},
	}
	out := normalizePool(pool, nil)
	if out[0].ID != "adp1" || out[0].ADPRank != 1 {
		t.Fatalf("ADP-ranked head disturbed: %+v", out[0])
	}
	if out[1].ID != "adp2" || out[1].ADPRank != 2 {
		t.Fatalf("ADP-ranked second disturbed: %+v", out[1])
	}
}

// TestSyncNowWiresPunterProjectionsHook is the sync-path integration test:
// SetPunterProjections, called before SyncNow (matching app_build.go's
// wiring order), enriches and ranks a punter the live Tank01 feed itself
// never projects at all (parsePlayerList never sets Projection, and the
// fixture's getNFLProjections payload carries no punter entry).
func TestSyncNowWiresPunterProjectionsHook(t *testing.T) {
	root := t.TempDir()
	hits := map[string]int{}
	server := tank01PunterStub(t, hits)
	defer server.Close()

	service := newTestService(t, root, server, "test-key")
	service.SetPunterProjections(stubPunterHook(map[string]float64{"Zed Punter": 8.75}))

	if err := service.SyncNow(context.Background()); err != nil {
		t.Fatalf("SyncNow: %v", err)
	}
	players, _ := service.Players()
	var punter Player
	found := false
	for _, p := range players {
		if p.ID == "9" {
			punter = p
			found = true
		}
	}
	if !found {
		t.Fatalf("punter missing from synced pool: %+v", players)
	}
	if punter.Projection != 8.75 {
		t.Errorf("synced punter Projection = %v, want 8.75", punter.Projection)
	}
	if punter.PunterRank != 1 {
		t.Errorf("synced punter PunterRank = %d, want 1", punter.PunterRank)
	}
	if punter.ADPRank != 0 {
		t.Errorf("synced punter ADPRank = %d, want 0 (no market ADP)", punter.ADPRank)
	}
}

// tank01PunterStub extends tank01Stub's fixture with one Position "P"
// player Tank01's own player list and ADP/projection feeds never cover —
// the roster-ops spec section 4.1.2 gap this whole feature exists to fill.
func tank01PunterStub(t *testing.T, hits map[string]int) *httptest.Server {
	t.Helper()
	payloads := map[string]string{
		"/getNFLPlayerList": `{"statusCode":200,"body":[
			{"playerID":"1","longName":"Alpha Receiver","pos":"WR","team":"CIN"},
			{"playerID":"9","longName":"Zed Punter","pos":"P","team":"HOU"}
		]}`,
		"/getNFLADP":         `{"statusCode":200,"body":{"adpList":[{"playerID":"1","adp":"1.8"}]}}`,
		"/getNFLProjections": `{"statusCode":200,"body":{"playerProjections":{"1":{"fantasyPointsDefault":{"halfPPR":"17.5"}}}}}`,
		"/getNFLNews":        `{"statusCode":200,"body":[]}`,
		"/getNFLTeams":       `{"statusCode":200,"body":[]}`,
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits[r.URL.Path]++
		if r.Header.Get("x-rapidapi-key") != "test-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		payload, ok := payloads[r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(payload))
	}))
}

// TestSetPunterProjectionsEnrichesACacheLoadedPool is the cache-path test:
// a pool cached BEFORE this feature existed (its punter carries zero
// Projection, exactly what a real pre-feature cache looks like) still
// comes out enriched and ranked once SetPunterProjections runs — matching
// app_build.go's real ordering, where NewService's loadCache (inside
// fantasy.Default) always runs before SetPunterProjections can.
func TestSetPunterProjectionsEnrichesACacheLoadedPool(t *testing.T) {
	root := t.TempDir()
	unenriched := poolCache{
		SchemaVersion: SchemaVersion,
		Provider:      "tank01",
		Scoring:       "half_ppr",
		Players: []Player{
			{ID: "wr1", Name: "Cached Receiver", Position: "WR", NFLTeam: "CIN", ADP: 1.1, ADPRank: 1, Projection: 15.0},
			{ID: "camp1", Name: "Aaa Camp Body", Position: "WR", NFLTeam: "DET"},
			{ID: "p1", Name: "Cached Punter", Position: "P", NFLTeam: "HOU"},
		},
	}
	encoded, err := json.MarshalIndent(unenriched, "", "  ")
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
	if status := service.Status(); status.Mode != "cache" {
		t.Fatalf("mode = %q, want cache before the hook runs", status.Mode)
	}
	before, beforeVersion := service.Players()
	for _, p := range before {
		if p.ID == "p1" && (p.Projection != 0 || p.PunterRank != 0) {
			t.Fatalf("pre-hook cached punter already enriched (fixture wrong): %+v", p)
		}
	}

	service.SetPunterProjections(stubPunterHook(map[string]float64{"Cached Punter": 8.0}))

	after, version := service.Players()
	// Pinning version == 0 as "the setter bumped it" is vacuous: NewService
	// starts version at 1 and loadCache bumps it again, so it is already
	// nonzero before SetPunterProjections ever runs. The setter's own
	// contract is that it must further, strictly increase whatever version
	// was already in place.
	if version <= beforeVersion {
		t.Fatalf("SetPunterProjections must strictly bump the pool version: before=%d, after=%d", beforeVersion, version)
	}
	var punter, camp Player
	for _, p := range after {
		switch p.ID {
		case "p1":
			punter = p
		case "camp1":
			camp = p
		}
	}
	if punter.Projection != 8.0 {
		t.Errorf("cache-loaded punter Projection = %v, want 8.0", punter.Projection)
	}
	if punter.PunterRank != 1 {
		t.Errorf("cache-loaded punter PunterRank = %d, want 1", punter.PunterRank)
	}
	// The enriched punter must now sit ahead of the zero-projection camp
	// body in the rest tier.
	punterIndex, campIndex := -1, -1
	for i, p := range after {
		if p.ID == "p1" {
			punterIndex = i
		}
		if p.ID == "camp1" {
			campIndex = i
		}
	}
	if punterIndex < 0 || campIndex < 0 || punterIndex >= campIndex {
		t.Fatalf("enriched punter (index %d) must sort ahead of the zero-projection camp body (index %d)", punterIndex, campIndex)
	}
	if camp.PunterRank != 0 {
		t.Errorf("non-punter PunterRank = %d, want 0", camp.PunterRank)
	}
}

// TestSetPunterProjectionsDoesNotRaceWithHeldPlayers is finding 1's own
// regression test: a Players() result handed to a reader before
// SetPunterProjections runs must never be mutated by that setter — only
// -race, against a genuinely concurrent read of the exact slice the
// setter used to mutate in place, can catch this (a single-threaded
// assertion cannot). held is captured once, up front, and the reader
// goroutine keeps ranging over that SAME slice throughout; the writer
// goroutine calls SetPunterProjections concurrently. Before the fix,
// SetPunterProjections passed s.players itself into normalizePool, which
// writes Player fields in place — the same backing array held still
// pointed at.
func TestSetPunterProjectionsDoesNotRaceWithHeldPlayers(t *testing.T) {
	root := t.TempDir()
	service, err := NewService(Config{Root: root, Season: 2026, ScoringFormat: "half_ppr"})
	if err != nil {
		t.Fatal(err)
	}
	// OfflinePool (NewService's starting pool) carries no punters; add one
	// so SetPunterProjections' normalizePool actually mutates a Player
	// field on every call, not a no-op walk.
	service.mu.Lock()
	service.players = append(service.players, Player{ID: "race-punter", Name: "Race Punter", Position: "P", NFLTeam: "HOU"})
	service.mu.Unlock()

	held, _ := service.Players()

	var wg sync.WaitGroup
	done := make(chan struct{})
	wg.Add(2)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-done:
				return
			default:
				for _, p := range held {
					_ = p.Projection
				}
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			service.SetPunterProjections(stubPunterHook(map[string]float64{"Race Punter": 8.75}))
		}
		close(done)
	}()
	wg.Wait()
}

// TestEnrichPuntersRequiresTeamOnLivePoolSurnameCollision is finding 3's
// own regression test: two live "P" players sharing a last name (a
// live-pool collision — Taylor, say, is unique in the embedded asset but
// common among real live punters) must each resolve independently by
// team, not both inherit whichever candidate the hook happens to return
// first. A third punter with a unique-in-pool surname still resolves by
// last name alone (no team match required), so a punter who has since
// changed teams keeps working.
func TestEnrichPuntersRequiresTeamOnLivePoolSurnameCollision(t *testing.T) {
	calls := map[string]bool{}
	hook := func(name, team string, requireTeam bool) (float64, bool) {
		calls[name] = requireTeam
		switch {
		case name == "Home Taylor" && team == "DET":
			return 9.5, true
		case name == "Away Taylor" && team == "SEA":
			// The hook itself would happily resolve "Taylor" by last name
			// alone (matching DET's asset entry) if requireTeam allowed it;
			// with requireTeam it must miss, since SEA is not DET.
			if requireTeam {
				return 0, false
			}
			return 9.5, true
		case name == "Moved Punter":
			return 6.25, true
		}
		return 0, false
	}
	pool := []Player{
		{ID: "home", Name: "Home Taylor", Position: "P", NFLTeam: "DET"},
		{ID: "away", Name: "Away Taylor", Position: "P", NFLTeam: "SEA"},
		{ID: "moved", Name: "Moved Punter", Position: "P", NFLTeam: "ARI"},
	}
	enrichPunters(pool, hook)

	byID := make(map[string]Player, len(pool))
	for _, p := range pool {
		byID[p.ID] = p
	}
	if !calls["Home Taylor"] || !calls["Away Taylor"] {
		t.Fatalf("both live-pool-colliding punters must be looked up with requireTeam=true: calls=%v", calls)
	}
	if calls["Moved Punter"] {
		t.Fatalf("a unique-in-pool surname must be looked up with requireTeam=false: calls=%v", calls)
	}
	if byID["home"].Projection != 9.5 {
		t.Errorf("team-matching live Taylor Projection = %v, want 9.5 (enriched)", byID["home"].Projection)
	}
	if byID["away"].Projection != 0 {
		t.Errorf("non-team-matching live Taylor Projection = %v, want 0 (must miss, not inherit home's projection)", byID["away"].Projection)
	}
	if byID["moved"].Projection != 6.25 {
		t.Errorf("unique-surname moved punter Projection = %v, want 6.25 (still matches by last name alone)", byID["moved"].Projection)
	}
}

// TestMergePoolEnrichesPunterBeforePoolLimitTruncation is finding 2's own
// regression test: mergePool must enrich a Position "P" player's
// projection BEFORE the rest-tier sort and the pool-limit truncation, not
// after. A pool limit small enough that an unenriched (zero-projection,
// alphabetically-tailed) punter would be cut must still keep an ENRICHED
// punter, because its real projection outranks the other zero-projection
// rest-tier filler competing for the same limited slots.
func TestMergePoolEnrichesPunterBeforePoolLimitTruncation(t *testing.T) {
	base := map[string]Player{
		"punter": {ID: "punter", Name: "Zzz Punter", Position: "P", NFLTeam: "HOU"},
	}
	// 10 zero-projection filler players whose names alphabetize ahead of
	// "Zzz Punter" — exactly the tail an enriched punter must outrank once
	// its real projection is applied before the sort.
	for i := 0; i < 10; i++ {
		id := fmt.Sprintf("filler%02d", i)
		base[id] = Player{ID: id, Name: fmt.Sprintf("Aaa Filler %02d", i), Position: "WR", NFLTeam: "FA"}
	}
	hook := stubPunterHook(map[string]float64{"Zzz Punter": 9.0})

	const limit = 3
	pool := mergePool(base, nil, nil, nil, nil, limit, hook)
	if len(pool) != limit {
		t.Fatalf("pool size = %d, want %d", len(pool), limit)
	}
	found := false
	for _, p := range pool {
		if p.ID == "punter" {
			found = true
			if p.Projection != 9.0 {
				t.Errorf("enriched punter Projection = %v, want 9.0", p.Projection)
			}
			if p.PunterRank != 1 {
				t.Errorf("enriched punter PunterRank = %d, want 1", p.PunterRank)
			}
		}
	}
	if !found {
		t.Fatalf("enriched punter was truncated out of a %d-entry pool limit; enrichment must run before truncation: %+v", limit, pool)
	}

	// The hookless (nil) call site — TestMergePoolPuntersSortToTailWithNoCrash
	// — must still pass unchanged with a nil hook: no enrichment, no
	// panic, punter sorts to the alphabetical tail with the other
	// zero-projection players.
	unenriched := mergePool(base, nil, nil, nil, nil, limit, nil)
	for _, p := range unenriched {
		if p.ID == "punter" {
			t.Fatalf("with a nil hook, the punter must not be enriched into the truncation-surviving set: %+v", unenriched)
		}
	}
}
