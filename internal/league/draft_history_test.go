package league

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func makePicks(t *testing.T, service *Service, count int) PersistedState {
	t.Helper()
	state := service.store.Snapshot()
	pool := service.pool()
	for n := 1; n <= count; n++ {
		if _, err := service.store.MakePick(teamOnClock(state.DraftOrder, n), pool.players[n-1].ID, "manager", service.clock(), service.clock()); err != nil {
			t.Fatal(err)
		}
		state = service.store.Snapshot()
	}
	return state
}

func TestDraftHistoryGroupsRoundsNewestFirstWithSnakeDirection(t *testing.T) {
	service, _, _ := newEventTestService(t)
	teams := len(service.Teams())
	state := makePicks(t, service, teams+1) // one pick into round 2, direction ←
	history := service.DraftHistory(state, "")
	if len(history.Rounds) != 2 || history.Rounds[0].Round != 2 || history.Rounds[0].Direction != "←" || history.Rounds[0].First != teams+1 || history.Rounds[0].Last != 2*teams {
		t.Fatalf("rounds = %+v", history.Rounds)
	}
	if lead := history.Rounds[0].Picks[0]; lead.Number != teams+1 || lead.Column != teams || lead.Label != "2.01" {
		t.Fatalf("round 2 leads with pick %d in column %d: %+v", teams+1, teams, lead)
	}
	// Ordering rule: history.Rounds is newest-first (the tape), while
	// history.Board.Rows is round-ascending (Rows[1] is round 2).
	if cells := history.Board.Rows[1].Cells; cells[teams-1].Number != teams+1 || cells[0].Label != fmt.Sprintf("2.%02d", teams) || cells[0].Filled {
		t.Fatalf("board round 2 = %+v", cells)
	}
	if len(history.Board.Rows) != CurrentDraftRounds() || len(history.Board.Rows[0].Cells) != teams {
		t.Fatalf("board is %d x %d, want %d x %d", len(history.Board.Rows), len(history.Board.Rows[0].Cells), CurrentDraftRounds(), teams)
	}
}

// TestDraftLedgerIsAscendingAndUntinted covers the CSV export's data
// source (app/draft's LedgerCSVHandler calls DraftLedger directly): every
// made pick, ascending by number (the opposite of the tape's newest-first
// Rounds), and Mine always false — the CSV carries no viewer-specific tint,
// so every downloader gets the same file.
func TestDraftLedgerIsAscendingAndUntinted(t *testing.T) {
	service, _, _ := newEventTestService(t)
	teams := len(service.Teams())
	makePicks(t, service, teams+2)
	ledger := service.DraftLedger()
	if len(ledger) != teams+2 {
		t.Fatalf("ledger length = %d, want %d", len(ledger), teams+2)
	}
	for index, pick := range ledger {
		if pick.Number != index+1 {
			t.Fatalf("ledger[%d].Number = %d, want %d (ascending)", index, pick.Number, index+1)
		}
		if pick.Mine {
			t.Fatalf("ledger[%d].Mine = true, want false (no viewer tint)", index)
		}
		if pick.PlayerName == "" || pick.TeamName == "" {
			t.Fatalf("ledger[%d] missing identity: %+v", index, pick)
		}
	}
}

// TestDraftedByPlayerIDKeysEveryMadePickByPlayer is wave 7's item 2: the
// shared helper playerMap's drafted parameter and /players' owner chip
// both read (draftedByPlayerID) must return every made pick keyed by its
// own PlayerID, and nothing for a player no pick has touched.
func TestDraftedByPlayerIDKeysEveryMadePickByPlayer(t *testing.T) {
	service, _, _ := newEventTestService(t)
	teams := len(service.Teams())
	state := makePicks(t, service, teams+1)
	drafted := draftedByPlayerID(state)
	if len(drafted) != teams+1 {
		t.Fatalf("len(drafted) = %d, want %d", len(drafted), teams+1)
	}
	for _, pick := range state.Picks {
		got, ok := drafted[pick.PlayerID]
		if !ok {
			t.Fatalf("drafted[%s] missing", pick.PlayerID)
		}
		if got.Number != pick.Number || got.Round != pick.Round || got.TeamID != pick.TeamID {
			t.Fatalf("drafted[%s] = %+v, want %+v", pick.PlayerID, got, pick)
		}
	}
	if _, ok := drafted["never-picked"]; ok {
		t.Fatal("drafted must carry no entry for a player no pick has touched")
	}
}

// TestViewerFirstPickTeaserAnswersTheEarliestPick is wave 7's item 3: the
// home page's post-draft card teaser ("You opened with X at 1.01") reads
// the SEATED viewer's own earliest pick, false before they have made
// one, and false outright for a seatless viewer.
func TestViewerFirstPickTeaserAnswersTheEarliestPick(t *testing.T) {
	service, _, member := newEventTestService(t)
	request := pickRequest(t, member.Email)

	if _, _, has := service.ViewerFirstPickTeaser(request); has {
		t.Fatal("hasPick must be false before the viewer's team has made any pick")
	}

	if _, _, _, err := service.MakePick(request, member.TeamID, "pool-001"); err != nil {
		t.Fatal(err)
	}
	name, label, has := service.ViewerFirstPickTeaser(request)
	if !has {
		t.Fatal("hasPick must be true once the viewer's team has made a pick")
	}
	if label != "1.01" {
		t.Fatalf("label = %q, want %q (first pick of a snake draft)", label, "1.01")
	}
	pool := service.pool()
	if want := pool.byID["pool-001"].Name; name != want {
		t.Fatalf("playerName = %q, want %q", name, want)
	}

	// A later pick from someone else's seat never overwrites the
	// viewer's own FIRST pick.
	state := service.store.Snapshot()
	otherTeam := state.DraftOrder[1]
	if _, err := service.store.MakePick(otherTeam, "pool-002", "manager", service.clock(), time.Time{}); err != nil {
		t.Fatal(err)
	}
	name2, label2, has2 := service.ViewerFirstPickTeaser(request)
	if !has2 || name2 != name || label2 != label {
		t.Fatalf("teaser changed after another team's pick: got %q/%q/%v, want %q/%q/true", name2, label2, has2, name, label)
	}

	// A seatless (unauthenticated) request answers false, never a stale
	// or borrowed team's pick.
	seatless := httptest.NewRequest(http.MethodGet, "/", nil)
	if _, _, has := service.ViewerFirstPickTeaser(seatless); has {
		t.Fatal("hasPick must be false for a seatless viewer")
	}
}

// TestLeagueMapCarriesDraftComplete is wave 7's item 6: leagueMap's own
// "draft_complete" field is the one fact app/layout.gsx's PrimaryNavigation
// needs to gate the "Draft results" nav destination — false before the
// draft finishes, true once every pick is locked, read off the SAME
// map every page's own data function already includes (leagueMap's own
// doc comment).
func TestLeagueMapCarriesDraftComplete(t *testing.T) {
	service, _, _ := newEventTestService(t)
	teams := len(service.Teams())
	if got := service.leagueMap()["draft_complete"]; got != false {
		t.Fatalf("draft_complete = %v before any pick, want false", got)
	}
	makePicks(t, service, teams*CurrentDraftRounds())
	if got := service.leagueMap()["draft_complete"]; got != true {
		t.Fatalf("draft_complete = %v after every pick, want true", got)
	}
}

// TestDraftResultsDataCarriesHistoryAndHeaderFacts is wave 7's item 4:
// /draft/results' own backing data — the full DraftHistoryView, the
// viewer's own team id (only once seated), and the draft's own round/
// team counts.
func TestDraftResultsDataCarriesHistoryAndHeaderFacts(t *testing.T) {
	service, _, member := newEventTestService(t)
	teams := len(service.Teams())
	state := makePicks(t, service, teams+1)
	request := pickRequest(t, member.Email)

	data := service.DraftResultsData(request)
	history, ok := data["history"].(DraftHistoryView)
	if !ok {
		t.Fatal("data[\"history\"] is not a DraftHistoryView")
	}
	if len(history.Picks) != teams+1 {
		t.Fatalf("history.Picks length = %d, want %d", len(history.Picks), teams+1)
	}
	if data["viewer_team_id"] != member.TeamID {
		t.Fatalf("viewer_team_id = %v, want %s (the seated member's own team)", data["viewer_team_id"], member.TeamID)
	}
	if data["team_count"] != teams {
		t.Fatalf("team_count = %v, want %d", data["team_count"], teams)
	}
	if data["rounds"] != CurrentDraftRounds() {
		t.Fatalf("rounds = %v, want %d", data["rounds"], CurrentDraftRounds())
	}
	wantComplete := draftComplete(state)
	if data["complete"] != wantComplete {
		t.Fatalf("complete = %v, want %v", data["complete"], wantComplete)
	}
	for _, key := range []string{"long_date", "time", "timezone"} {
		if s, ok := data[key].(string); !ok || s == "" {
			t.Errorf("data[%q] = %v, want a non-empty string", key, data[key])
		}
	}

	// A seatless request answers "" for viewer_team_id, never a borrowed
	// placeholder team (ViewerFirstPickTeaser's own doc comment explains
	// why viewerReadOnly's raw team_id is not enough on its own).
	seatless := httptest.NewRequest(http.MethodGet, "/draft/results", nil)
	if seatlessData := service.DraftResultsData(seatless); seatlessData["viewer_team_id"] != "" {
		t.Fatalf("seatless viewer_team_id = %v, want \"\"", seatlessData["viewer_team_id"])
	}
}

func TestPickValueIsPickNumberMinusADP(t *testing.T) {
	service, _, _ := newEventTestService(t)
	state := service.store.Snapshot()
	// testPool gives ADP = index+1; pick 1 takes pool-005 (ADP 5): value = 1 - 5 = -4, a reach.
	if _, err := service.store.MakePick(teamOnClock(state.DraftOrder, 1), "pool-005", "manager", service.clock(), service.clock()); err != nil {
		t.Fatal(err)
	}
	detail := service.DraftHistory(service.store.Snapshot(), "").Detail(1)
	if !detail.HasValue || detail.Value != -4 || detail.ValueLabel != "−4" {
		t.Fatalf("detail = %+v", detail)
	}
	if len(detail.BestAvailable) != 3 || detail.BestAvailable[0].ID != "pool-001" {
		t.Fatalf("best available = %+v", detail.BestAvailable)
	}
}

// TestDraftDataSkipsHistoryBuildWhenNotIncluded is the P1 perf fix's own
// contract (2026-08-30 review): DraftDataReadOnlyOptions(r, false) must
// hand back the zero DraftHistoryView, not run DraftHistory at all — the
// command/available/queue/room/workspace fragments never render it, and
// draftData used to build it unconditionally on every one of their polls.
//
// Item 11 (2026-08-30 review): asserts on the zero-value view alone, no
// wall-clock ceiling — item 9 moved this test's own former timing
// assertion to TestDraftHistoryStaysFastAtFullDraftPickCount's
// allocation-count check below, the one call site that actually still
// runs DraftHistory's build.
func TestDraftDataSkipsHistoryBuildWhenNotIncluded(t *testing.T) {
	service, _, member := newEventTestService(t)
	teams := len(service.Teams())
	rounds := CurrentDraftRounds()
	makePicks(t, service, teams*rounds)
	request := pickRequest(t, member.Email)

	data := service.DraftDataReadOnlyOptions(request, false)

	history, _ := data["history"].(DraftHistoryView)
	if len(history.Rounds) != 0 || len(history.Picks) != 0 || len(history.Teams) != 0 {
		t.Fatalf("history = %+v, want the zero value when includeHistory=false", history)
	}
}

// TestDraftHistoryStaysFastAtFullDraftPickCount is the P1 perf fix's other
// half: DraftHistory's build plus one Detail call per pick (the tape's own
// full render cost, hydratedTapePicksProps in page.server.go) must stay
// roughly linear in the pick count — the pre-fix O(P^2) TeamPicks rescan
// cost the review clocked at ~8ms at 120 picks.
//
// Item 9 (2026-08-30 review): a wall-clock ceiling is exactly the wrong
// tool for a shared CI runner — a noisy neighbor or a slower machine
// fails a fixed-millisecond assertion for reasons that have nothing to do
// with the algorithm under test (this file used to carry a build-tag pair,
// race_enabled_test.go/race_disabled_test.go, purely to widen the
// threshold under -race; both are gone, no longer needed). Allocation
// count is a deterministic, environment-independent proxy for
// algorithmic complexity instead: testing.AllocsPerRun reports the same
// number on a fast machine and a slow one, and an O(P) implementation
// allocates a roughly constant number of objects per pick regardless of
// how many picks came before it, while an O(P^2) regression (like the
// pre-fix TeamPicks rescan) blows that per-pick multiplier up by roughly
// the pick count itself.
func TestDraftHistoryStaysFastAtFullDraftPickCount(t *testing.T) {
	service, _, _ := newEventTestService(t)
	teams := len(service.Teams())
	rounds := CurrentDraftRounds()
	total := teams * rounds
	state := makePicks(t, service, total)

	allocs := testing.AllocsPerRun(5, func() {
		history := service.DraftHistory(state, "")
		for _, pick := range history.Picks {
			history.Detail(pick.Number)
		}
	})
	perPick := allocs / float64(total)
	// A generous per-pick ceiling: the un-instrumented build measures well
	// under this today (see the doc comment above), and an O(P^2)
	// regression at 120 picks would multiply the per-pick figure by
	// roughly 120x, not the 2-3x of ordinary allocator/GC noise between
	// runs.
	const limitPerPick = 60.0
	if perPick > limitPerPick {
		t.Fatalf("DraftHistory + one Detail per pick allocates %.0f objects at %d picks (%.1f/pick), want under %.1f/pick — looks like an O(P^2) regression", allocs, total, perPick, limitPerPick)
	}
}
