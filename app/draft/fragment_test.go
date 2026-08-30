package draft

import (
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"gridiron-2000/internal/league"
)

// fixtureTeams matches pickFixture's eight-team draft order.
var fixtureTeams = []string{"Kernel Panic", "Segfault City", "Null Pointers", "Garbage Collectors", "Race Condition", "Big Endians", "Stack Overflow", "Hot Path"}

// pickFixture renders picks[i] the way pickMaps does (service.go): the
// team and player nested maps carry the same keys teamMap/playerMap build,
// so a template reading pick.team.abbreviation or pick.player.name sees a
// realistic row. madeBy is "manager", "auto", or "commissioner".
func pickFixture(number int, madeBy string) map[string]any {
	teams := len(fixtureTeams)
	round := (number-1)/teams + 1
	column := (number - 1) % teams
	if round%2 == 0 {
		column = teams - 1 - column
	}
	positions := []string{"WR", "RB", "QB", "TE"}
	name := fixtureTeams[column]
	return map[string]any{
		"number": number, "round": round,
		"team": map[string]any{
			"id": fmt.Sprintf("team-%d", column+1), "name": name, "abbreviation": strings.ToUpper(name[:2]), "division": "EAST",
			"manager": "Manager " + name, "claimed": true, "tone": "cyan", "has_avatar": false, "has_avatar_image": false, "avatar_image_url": "",
		},
		"player": map[string]any{
			"id": fmt.Sprintf("player-%03d", number), "name": fmt.Sprintf("Fixture Player %03d", number), "position": positions[number%len(positions)], "nfl_team": "CIN",
			"projection": "12.0", "points": "0.0", "status": "Drafted", "news": "", "rank": fmt.Sprintf("%03d", number), "detail": "CIN · BYE 10",
			"headshot": "", "has_headshot": false, "jersey": "", "has_breakdown": false, "breakdown": []map[string]any{}, "breakdown_total": "",
			"has_hist": false, "hist": "", "search": "fixture", "is_rookie": false, "draft_capital": "", "has_draft_capital": false,
		},
		"made_by": madeBy, "is_auto": madeBy == "auto", "is_commissioner": madeBy == "commissioner",
	}
}

// tapePickFixture builds one league.TapePick by hand, mirroring pickFixture's
// eight-team draft order and snake math (internal/league's own pickColumn/
// pickSlot/pickLabel are unexported, so this test package cannot call
// them directly).
func tapePickFixture(number int, madeBy string) league.TapePick {
	teams := len(fixtureTeams)
	round := (number-1)/teams + 1
	slot := (number-1)%teams + 1
	column := slot
	if round%2 == 0 {
		column = teams - slot + 1
	}
	positions := []string{"WR", "RB", "QB", "TE"}
	name := fixtureTeams[column-1]
	return league.TapePick{
		Number: number, Round: round, Slot: slot, Column: column,
		Label:  fmt.Sprintf("%d.%02d", round, slot),
		TeamID: fmt.Sprintf("team-%d", column), TeamName: name, TeamAbbr: strings.ToUpper(name[:2]), TeamTone: "cyan", Manager: "Manager " + name,
		PlayerID: fmt.Sprintf("player-%03d", number), PlayerName: fmt.Sprintf("Fixture Player %03d", number), Position: positions[number%len(positions)], NFLTeam: "CIN",
		MadeBy: madeBy, IsAuto: madeBy == "auto", IsCommissioner: madeBy == "commissioner",
		TimeToPickSec: 30, TimeToPick: "0:30", MadeAt: "2026-01-01T00:00:00Z",
	}
}

// tapeHistoryFixture groups picks into league.TapeRound entries the way
// Service.DraftHistory does (draft_history.go): newest round first, each
// round's picks newest-number first.
func tapeHistoryFixture(picks []league.TapePick) league.DraftHistoryView {
	teams := len(fixtureTeams)
	byRound := map[int][]league.TapePick{}
	for _, pick := range picks {
		byRound[pick.Round] = append(byRound[pick.Round], pick)
	}
	roundNumbers := make([]int, 0, len(byRound))
	for round := range byRound {
		roundNumbers = append(roundNumbers, round)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(roundNumbers)))
	rounds := make([]league.TapeRound, 0, len(roundNumbers))
	for _, round := range roundNumbers {
		roundPicks := append([]league.TapePick(nil), byRound[round]...)
		sort.Slice(roundPicks, func(i, j int) bool { return roundPicks[i].Number > roundPicks[j].Number })
		direction := "→"
		if round%2 == 0 {
			direction = "←"
		}
		rounds = append(rounds, league.TapeRound{
			Round: round, First: (round-1)*teams + 1, Last: round * teams, Direction: direction,
			Made: len(roundPicks), Total: teams, Picks: roundPicks,
		})
	}
	return league.DraftHistoryView{Rounds: rounds, Picks: picks}
}

func draftFragmentFixture() map[string]any {
	return map[string]any{
		"draft": map[string]any{
			"date":            "TEST DRAFT",
			"long_date":       "Test draft date",
			"format":          "Snake draft",
			"status_note":     "Test draft is live.",
			"at":              "",
			"countdown_label": "",
			"started":         true,
			"complete":        false,
		},
		"clock": map[string]any{
			"paused":             false,
			"armed":              false,
			"effective_deadline": "",
			"remaining_label":    "",
			"reason":             "waiting for the next pick",
		},
		"viewer": map[string]any{
			"team_id":         "team-test",
			"has_seat":        true,
			"is_commissioner": false,
		},
		"public_entry": map[string]any{
			"can_claim":    false,
			"detail":       "The test viewer already has a seat.",
			"state_label":  "SEAT ASSIGNED",
			"action_href":  "/draft",
			"action_label": "Return to draft",
			"admitted":     true,
			"league_full":  false,
		},
		"pool_status": map[string]any{
			"has_notice":            false,
			"label":                 "",
			"detail":                "",
			"has_last_success":      false,
			"last_success":          "",
			"last_success_relative": "",
		},
		"on_clock": map[string]any{
			"abbreviation": "TST",
		},
		"on_clock_id":          "team-test",
		"pick_number":          1,
		"ready_count":          0,
		"manager_count":        0,
		"round":                1,
		"order_randomized":     false,
		"demo_mode":            false,
		"viewer_ready":         false,
		"viewer_autopick":      false,
		"has_matchup_source":   false,
		"matchup_source_label": "",
		"teams":                []map[string]any{},
		"picks":                []map[string]any{},
		"board":                []map[string]any{},
		"available": []map[string]any{
			{
				"id":                "player-test",
				"name":              "Test Player",
				"position":          "QB",
				"nfl_team":          "TST",
				"projection":        "0.0",
				"rank":              "001",
				"detail":            "QB · TST",
				"headshot":          "",
				"has_headshot":      false,
				"jersey":            "1",
				"has_breakdown":     false,
				"breakdown":         []map[string]any{},
				"breakdown_total":   "",
				"has_hist":          false,
				"hist":              "",
				"search":            "test player qb tst",
				"has_draft_capital": false,
				"draft_capital":     "",
				"has_opponent":      false,
				"opponent":          "",
				"has_matchup":       false,
				"matchup_tier":      "",
				"matchup_chip":      "",
				"matchup_detail":    "",
				"draft_eligible":    true,
			},
		},
		"draft_complete":     false,
		"can_pick":           true,
		"pool_count":         1,
		"pool_position":      "",
		"pool_query":         "",
		"pool_page":          1,
		"pool_total":         1,
		"pool_page_start":    1,
		"pool_page_end":      1,
		"pool_has_previous":  false,
		"pool_has_next":      false,
		"pool_previous_href": "",
		"pool_next_href":     "",
		"pool_all_href":      "/draft/fragment/workspace",
		"pool_rb_href":       "/draft/fragment/workspace?pos=RB",
		"pool_wr_href":       "/draft/fragment/workspace?pos=WR",
		"pool_qb_href":       "/draft/fragment/workspace?pos=QB",
		"pool_te_href":       "/draft/fragment/workspace?pos=TE",
		"pool_k_href":        "/draft/fragment/workspace?pos=K",
		"pool_dst_href":      "/draft/fragment/workspace?pos=DST",
		"pool_p_href":        "/draft/fragment/workspace?pos=P",
		"board_count":        0,
		"picks_empty":        true,
	}
}

func TestDraftFragmentRejectsMethodAndUnauthorizedBeforeLoading(t *testing.T) {
	tests := []struct {
		name    string
		method  string
		allowed bool
		want    int
	}{
		{name: "method", method: http.MethodPost, allowed: true, want: http.StatusMethodNotAllowed},
		{name: "anonymous", method: http.MethodGet, allowed: false, want: http.StatusUnauthorized},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			loads := 0
			handler := draftFragmentHandler(draftRoomRegion, func(*http.Request) bool {
				return test.allowed
			}, func(*http.Request) map[string]any {
				loads++
				return nil
			})
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(test.method, "/draft/fragment/room", nil))
			if response.Code != test.want {
				t.Fatalf("status = %d, want %d", response.Code, test.want)
			}
			if loads != 0 {
				t.Fatalf("rejected request performed %d draft reads", loads)
			}
			if test.method != http.MethodGet && response.Header().Get("Allow") != http.MethodGet {
				t.Fatalf("Allow = %q, want GET", response.Header().Get("Allow"))
			}
		})
	}
}

func TestDraftFragmentsRenderScopedHTMLAndReturnBodyless304(t *testing.T) {
	load := func(*http.Request) map[string]any {
		return draftFragmentFixture()
	}
	tests := []struct {
		region string
		path   string
		class  string
	}{
		{region: draftRoomRegion, path: "/draft/fragment/room", class: "draft-live-room"},
		{region: draftWorkspaceRegion, path: "/draft/fragment/workspace", class: "draft-live-workspace"},
		{region: draftCommandRegion, path: "/draft/fragment/command", class: "draft-command__inner"},
		{region: draftTapeRegion, path: "/draft/fragment/tape", class: "draft-history"},
		{region: draftAvailableRegion, path: "/draft/fragment/available", class: "draft-available"},
		{region: draftQueueRegion, path: "/draft/fragment/queue", class: "draft-mine"},
	}
	for _, test := range tests {
		t.Run(test.region, func(t *testing.T) {
			handler := draftFragmentHandler(test.region, func(*http.Request) bool { return true }, load)
			first := httptest.NewRecorder()
			handler.ServeHTTP(first, httptest.NewRequest(http.MethodGet, test.path, nil))
			if first.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", first.Code, first.Body.String())
			}
			if !strings.Contains(first.Body.String(), `class="`+test.class+`"`) || strings.Contains(first.Body.String(), "<html") {
				t.Fatalf("fragment is not scoped %s markup: %s", test.region, first.Body.String())
			}
			if test.region == draftWorkspaceRegion {
				fixture := load(httptest.NewRequest(http.MethodGet, test.path, nil))
				players, _ := fixture["available"].([]map[string]any)
				if len(players) == 0 {
					t.Fatal("workspace fixture has no available players")
				}
				name := stringField(players[0], "name")
				id := stringField(players[0], "id")
				if !strings.Contains(first.Body.String(), html.EscapeString(name)) {
					t.Fatalf("workspace omitted player name %q: %s", name, first.Body.String())
				}
				if !strings.Contains(first.Body.String(), `name="player_id" value="`+html.EscapeString(id)+`"`) {
					t.Fatalf("workspace omitted player id %q", id)
				}
			}
			for name, want := range map[string]string{
				"Cache-Control": "private, no-store",
				"Content-Type":  "text/html; charset=utf-8",
				"Vary":          "Cookie",
			} {
				if got := first.Header().Get(name); got != want {
					t.Errorf("%s = %q, want %q", name, got, want)
				}
			}
			etag := first.Header().Get("ETag")
			if etag == "" {
				t.Fatal("missing ETag")
			}

			secondRequest := httptest.NewRequest(http.MethodGet, test.path, nil)
			secondRequest.Header.Set("If-None-Match", etag)
			second := httptest.NewRecorder()
			handler.ServeHTTP(second, secondRequest)
			if second.Code != http.StatusNotModified || second.Body.Len() != 0 {
				t.Fatalf("conditional response = %d with %d bytes, want bodyless 304", second.Code, second.Body.Len())
			}
		})
	}
}

func TestDraftFragmentFixtureIsFreshForEachRender(t *testing.T) {
	// prepareDraftData never writes back into its argument (page.server.go):
	// its raw input stays a plain fixture, so the test must read the
	// prepared view off prepareDraftData's return value, not the map it
	// was handed.
	prepared := prepareDraftData(draftFragmentFixture())

	fresh := draftFragmentFixture()
	available, ok := fresh["available"].([]map[string]any)
	if !ok || len(available) != 1 {
		t.Fatalf("fresh fixture available = %#v, want one raw player map", fresh["available"])
	}
	if got := stringField(available[0], "name"); got != "Test Player" {
		t.Fatalf("fresh fixture player name = %q, want Test Player", got)
	}
	preparedView, ok := prepared["available"].(draftAvailableView)
	if !ok || len(preparedView.Players) != 1 {
		t.Fatalf("prepared fixture available = %#v, want one prepared player card", prepared["available"])
	}
}

func TestDraftRegionETagIgnoresWallClockTextButTracksLeagueState(t *testing.T) {
	view := draftRoomView{Data: map[string]any{
		"pick_number": 4,
		"clock":       map[string]any{"armed": true, "effective_deadline": "2026-08-23T00:00:00Z", "server_now": "first", "remaining_seconds": 40, "remaining_label": "00:40"},
		"draft":       map[string]any{"started": true, "countdown_label": "TODAY", "days_until": 0},
	}}
	first, err := draftRegionETag(draftRoomRegion, view)
	if err != nil {
		t.Fatal(err)
	}
	view.Data["clock"].(map[string]any)["server_now"] = "second"
	view.Data["clock"].(map[string]any)["remaining_seconds"] = 39
	view.Data["clock"].(map[string]any)["remaining_label"] = "00:39"
	second, err := draftRegionETag(draftRoomRegion, view)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("wall-clock-only update changed ETag: %s != %s", first, second)
	}
	view.Data["pick_number"] = 5
	third, err := draftRegionETag(draftRoomRegion, view)
	if err != nil {
		t.Fatal(err)
	}
	if third == second {
		t.Fatal("authoritative pick change did not change ETag")
	}
}

// TestCommandFragmentETagIgnoresTicksButChangesWithTheDeadline is execution
// note 1 (Task 5a review): semanticDraftRegionView must cover the four new
// shell view types, not just draftRoomView/draftWorkspaceView, so a
// wall-clock-only tick on the command fragment still answers a bodyless
// 304 and a real deadline change still busts the ETag.
func TestCommandFragmentETagIgnoresTicksButChangesWithTheDeadline(t *testing.T) {
	fixture := draftFragmentFixture()
	clock := fixture["clock"].(map[string]any)
	clock["armed"], clock["state"], clock["effective_deadline"], clock["remaining_label"] = true, "RUNNING", "2026-09-06T17:01:30Z", "1:30"
	handler := draftFragmentHandler(draftCommandRegion, func(*http.Request) bool { return true }, func(*http.Request) map[string]any { return fixture })
	etagFor := func() string {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/draft/fragment/command", nil))
		return response.Header().Get("ETag")
	}
	first := etagFor()
	clock["remaining_label"], clock["remaining_seconds"] = "1:29", 89
	if etagFor() != first {
		t.Fatal("a tick must not change the ETag")
	}
	clock["effective_deadline"] = "2026-09-06T17:03:00Z"
	if etagFor() == first {
		t.Fatal("a new deadline must change the ETag")
	}
}

// TestTapeFragmentSinceReturnsOnlyNewerRows proves attachDraftFragmentSince
// and filterTapeRoundsSince (fragment.go): "?since=N" switches the tape
// fragment to DraftTapeRows, every pick above N alone.
//
// Review item 2 (2026-08-30) flipped the header half of this test: since
// round 1's own header (data-tape-key="round-1") already reached the page
// on an earlier response (since < round.First), since=2 — still inside
// round 1 (First=1) — must NOT re-carry it. A re-sent header would only
// be dropped by the prepend region's own data-tape-key dedupe
// (client/runtime/host/regions.ts), discarding the whole node — including
// any fresher "N of M made" text — before it ever reaches the DOM; its
// own live MadeBindKey/CurrentBindAttr bind (item 3) is what actually
// keeps an already-rendered header current from here on.
func TestTapeFragmentSinceReturnsOnlyNewerRows(t *testing.T) {
	fixture := draftFragmentFixture()
	fixture["history"] = tapeHistoryFixture([]league.TapePick{tapePickFixture(1, "manager"), tapePickFixture(2, "manager"), tapePickFixture(3, "auto")})
	fixture["picks_empty"] = false
	handler := draftFragmentHandler(draftTapeRegion, func(*http.Request) bool { return true }, func(*http.Request) map[string]any { return fixture })
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/draft/fragment/tape?since=2", nil))
	body := response.Body.String()
	if !strings.Contains(body, `data-tape-key="pick-3"`) || strings.Contains(body, `data-tape-key="pick-2"`) || strings.Contains(body, `class="draft-history"`) {
		t.Fatalf("since=2 must render rows newer than 2 only: %s", body)
	}
	if strings.Contains(body, `data-tape-key="round-1"`) {
		t.Fatalf("since=2 must NOT re-carry round 1's own header, already on the page: %s", body)
	}
}

// TestTapeRowsFragmentSinceReturnsOnlyNewerRows pins finding 1 (2026-08-30
// review): target mode's own markup never sends "?since=" to the
// "tape-rows" region, but attachDraftFragmentSince runs for every region,
// so a caller that does send it still gets only the rows numbered above
// the cursor — the same filtered DraftTapeRows body the "tape" region's
// own "?since=" poll returns (TestTapeFragmentSinceReturnsOnlyNewerRows,
// above). This is API compatibility only (TapeRowsFragmentHandler's own
// doc comment, fragment.go).
func TestTapeRowsFragmentSinceReturnsOnlyNewerRows(t *testing.T) {
	fixture := draftFragmentFixture()
	fixture["history"] = tapeHistoryFixture([]league.TapePick{tapePickFixture(1, "manager"), tapePickFixture(2, "manager"), tapePickFixture(3, "auto")})
	fixture["picks_empty"] = false
	handler := draftFragmentHandler(draftTapeRowsRegion, func(*http.Request) bool { return true }, func(*http.Request) map[string]any { return fixture })
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/draft/fragment/tape-rows?since=2", nil))
	body := response.Body.String()
	if !strings.Contains(body, `data-tape-key="pick-3"`) || strings.Contains(body, `data-tape-key="pick-2"`) || strings.Contains(body, `class="draft-history"`) {
		t.Fatalf("tape-rows since=2 must render rows newer than 2 only: %s", body)
	}
}

// TestTapeRowsFragmentNeverLeaksTheLiveRootOrShell is finding 4 (2026-08-30
// review): DraftTapeRows carries no outer element of its own (its own doc
// comment, page.gsx) — the "tape-rows" region's response must be the bare
// round/row markup alone, none of the wrapping DraftHistory pane's own
// live root, region element, #tape-latest anchor, or role="status"
// stale-fallback paragraph. Checked at 0, 1, and 11 picks (0 crosses into
// RoundsEmpty; 11 crosses an 8-team round boundary).
func TestTapeRowsFragmentNeverLeaksTheLiveRootOrShell(t *testing.T) {
	forbidden := []string{
		"data-gosx-live-mode",
		"data-gosx-region",
		"tape-latest",
		"draft-history",
		`role="status"`,
	}
	for _, made := range []int{0, 1, 11} {
		picks := make([]league.TapePick, 0, made)
		for n := 1; n <= made; n++ {
			picks = append(picks, tapePickFixture(n, "manager"))
		}
		fixture := draftFragmentFixture()
		fixture["history"] = tapeHistoryFixture(picks)
		fixture["picks_empty"] = made == 0
		handler := draftFragmentHandler(draftTapeRowsRegion, func(*http.Request) bool { return true }, func(*http.Request) map[string]any { return fixture })
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/draft/fragment/tape-rows", nil))
		body := response.Body.String()
		for _, marker := range forbidden {
			if strings.Contains(body, marker) {
				t.Errorf("%d picks: tape-rows body leaked %q: %s", made, marker, body)
			}
		}
	}
}

// TestTapeFragmentSinceRoundHeaderCrossesRoundBoundary is review item 2's
// own T1/T2/T4 sequence (2026-08-30): three sequential same-round picks
// (T1, T2, T4 — an 8-team league's round 1, picks 1/2/4) show round 1's
// header exactly once, on the FIRST of the three "?since=" fetches, never
// re-sent on the later two; a fourth pick that crosses the round boundary
// (pick 9, round 2) DOES carry round 2's own fresh header, and still
// never re-carries round 1's.
func TestTapeFragmentSinceRoundHeaderCrossesRoundBoundary(t *testing.T) {
	fixture := draftFragmentFixture()
	fixture["history"] = tapeHistoryFixture([]league.TapePick{
		tapePickFixture(1, "manager"), tapePickFixture(2, "manager"), tapePickFixture(4, "manager"),
		tapePickFixture(9, "manager"),
	})
	fixture["picks_empty"] = false
	handler := draftFragmentHandler(draftTapeRegion, func(*http.Request) bool { return true }, func(*http.Request) map[string]any { return fixture })
	fetch := func(since int) string {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/draft/fragment/tape?since="+strconv.Itoa(since), nil))
		return response.Body.String()
	}
	// T1: nothing seen yet — round 1's header is genuinely new.
	if body := fetch(0); !strings.Contains(body, `data-tape-key="round-1"`) {
		t.Fatalf("since=0 must carry round 1's own header: %s", body)
	}
	// T2 (a second same-round pick, since=1 >= round.First=1): no header.
	if body := fetch(1); strings.Contains(body, `data-tape-key="round-1"`) {
		t.Fatalf("since=1 must NOT re-carry round 1's own header: %s", body)
	}
	// T4 (a third same-round pick, since=2): still no header.
	if body := fetch(2); strings.Contains(body, `data-tape-key="round-1"`) {
		t.Fatalf("since=2 must NOT re-carry round 1's own header: %s", body)
	}
	// Crossing into round 2 at pick 9 (since=4): round 2's header is new.
	body := fetch(4)
	if !strings.Contains(body, `data-tape-key="round-2"`) {
		t.Fatalf("since=4 (crossing into round 2) must carry round 2's own header: %s", body)
	}
	if strings.Contains(body, `data-tape-key="round-1"`) {
		t.Fatalf("since=4 must not re-carry round 1's own header even at a round boundary: %s", body)
	}
}

// TestTapeFragmentSinceSurvivesTheRoundCap is item 2's own regression test
// (2026-08-30 review): the pre-fix code applied capTapeRounds INSIDE
// buildDraftHistoryView, before Since was even known, so a "?since="
// cursor whose own picks fell outside the newest-3-round cap window lost
// them — "since=39" at 60 picks (8 teams: round 5 covers picks 33-40,
// outside the newest-3-round window of rounds 6-8/picks 41-60) silently
// dropped pick 40, the ONE pick in round 5 above the cursor. The fix
// (attachDraftFragmentView) applies the cap only when Since < 0, so a
// since-poll's Rounds reach the template exactly as
// attachDraftFragmentSince's own filter left them.
func TestTapeFragmentSinceSurvivesTheRoundCap(t *testing.T) {
	const made = 60
	picks := make([]league.TapePick, 0, made)
	for n := 1; n <= made; n++ {
		picks = append(picks, tapePickFixture(n, "manager"))
	}
	fixture := draftFragmentFixture()
	fixture["history"] = tapeHistoryFixture(picks)
	fixture["picks_empty"] = false
	handler := draftFragmentHandler(draftTapeRegion, func(*http.Request) bool { return true }, func(*http.Request) map[string]any { return fixture })
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/draft/fragment/tape?since=39", nil))
	body := response.Body.String()
	want := made - 39
	if n := strings.Count(body, `data-tape-key="pick-`); n != want {
		t.Fatalf("since=39 at %d picks: %d rows, want %d (picks 40..%d)", made, n, want, made)
	}
	if !strings.Contains(body, `data-tape-key="pick-40"`) {
		t.Error("since=39 at 60 picks must still include pick 40 — its own round (5) fell outside the newest-3-round cap window")
	}
	if strings.Contains(body, `data-tape-key="pick-39"`) {
		t.Error("since=39 must exclude pick 39 itself")
	}
	if !strings.Contains(body, `data-tape-key="pick-60"`) {
		t.Error("since=39 must include the latest pick, 60")
	}
}

// TestDraftHistoryLinksPreserveThePoolState is item 6's own test
// (2026-08-30 review): the desktop segment's three navigation targets
// and a tape row's own open link all carry the viewer's current pool
// q/pos/page, built server-side (draftHistoryHref), so switching Tape/
// Board/Teams or opening a pick's detail never resets a filtered/paged
// pool search.
func TestDraftHistoryLinksPreserveThePoolState(t *testing.T) {
	fixture := draftFragmentFixture()
	fixture["pool_position"] = "RB"
	fixture["pool_query"] = "mahomes"
	fixture["pool_page"] = 3
	fixture["history"] = tapeHistoryFixture([]league.TapePick{tapePickFixture(1, "manager")})
	fixture["picks_empty"] = false

	prepared := attachDraftFragmentView(prepareDraftData(fixture), httptest.NewRequest(http.MethodGet, "/draft/fragment/tape", nil))
	for key, want := range map[string]string{
		"history_tape_href":  "/draft?page=3&pos=RB&q=mahomes&view=tape",
		"history_board_href": "/draft?page=3&pos=RB&q=mahomes&view=board",
		"history_teams_href": "/draft?page=3&pos=RB&q=mahomes&view=teams",
	} {
		if got, _ := prepared[key].(string); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
	history, ok := prepared["history"].(draftHistoryView)
	if !ok || len(history.Rounds) == 0 || len(history.Rounds[0].Picks) == 0 {
		t.Fatal("prepared history has no picks to check")
	}
	href := history.Rounds[0].Picks[0].Href
	for _, want := range []string{"page=3", "pos=RB", "q=mahomes", "pick=1"} {
		if !strings.Contains(href, want) {
			t.Errorf("tape row href = %q, missing %q", href, want)
		}
	}
}

// TestTapeFragmentRoundsAllURLPersistsAcrossRegionRefresh is the
// 2026-08-30 follow-up's own test: a request that already carries
// "?rounds=all" must see that cursor echoed back into history_tape_url,
// exactly as "?pick=" already was, so the pane stays expanded across the
// region's own draft:pick/draft:undo/draft:state refresh instead of
// silently recollapsing to the newest three rounds while the address bar
// still reads "?rounds=all". The tape fragment at the same query must
// also actually render every round, not just the newest three.
func TestTapeFragmentRoundsAllURLPersistsAcrossRegionRefresh(t *testing.T) {
	const made = 40 // 5 rounds at 8 teams
	picks := make([]league.TapePick, 0, made)
	for n := 1; n <= made; n++ {
		picks = append(picks, tapePickFixture(n, "manager"))
	}
	fixture := draftFragmentFixture()
	fixture["picks_empty"] = false
	fixture["history"] = fullHistoryFixture(picks, 5, made+1, false)

	prepared := attachDraftFragmentView(prepareDraftData(fixture), httptest.NewRequest(http.MethodGet, "/draft?view=tape&rounds=all", nil))
	tapeURL, _ := prepared["history_tape_url"].(string)
	if !strings.Contains(tapeURL, "rounds=all") {
		t.Errorf("history_tape_url = %q, must carry rounds=all when the request did", tapeURL)
	}

	body := renderTapeRegionPath(t, fixture, "/draft/fragment/tape?view=tape&rounds=all")
	for _, round := range []string{"round-1", "round-2", "round-3", "round-4", "round-5"} {
		if !strings.Contains(body, `data-tape-key="`+round+`"`) {
			t.Errorf("?view=tape&rounds=all must render every round, missing %s: %s", round, body)
		}
	}
}

// TestDraftTapeRegionIsAPlainReplaceInTargetMode supersedes the deleted
// prepend-cursor contract (findings 1/2/3/6, 2026-08-30 review): target
// mode's tape pane now nests exactly ONE plain REPLACE region (no
// data-gosx-region-mode, no data-gosx-region-key, no data-gosx-region-cursor,
// no "{cursor}" token anywhere in page.gsx), fetching TapeRowsFragmentHandler's
// own dedicated endpoint on every draft:pick/draft:undo/draft:state.
func TestDraftTapeRegionIsAPlainReplaceInTargetMode(t *testing.T) {
	source, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	body := string(source)
	for _, want := range []string{
		`data-gosx-region-url={props.TapeURL} data-gosx-region-on="draft:pick draft:undo draft:state"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page.gsx must request the tape-rows region as a plain replace, missing %q", want)
		}
	}
	for _, forbidden := range []string{
		`data-gosx-region-mode="prepend"`,
		`data-gosx-region-key=`,
		`data-gosx-region-cursor=`,
		`{cursor}`,
	} {
		if strings.Contains(body, forbidden) {
			t.Errorf("page.gsx must carry no prepend machinery, found %q", forbidden)
		}
	}
}

// TestTapeFragmentWithoutSinceRendersTheFullPane proves a plain GET (no
// "?since=") and an invalid one both fall back to DraftHistory, the pane's
// ordinary full render — draftHistoryView.Since defaults to -1
// (prepareDraftData) precisely so a request that never asks for a cursor
// keeps getting the whole tape, not an empty DraftTapeRows partial.
func TestTapeFragmentWithoutSinceRendersTheFullPane(t *testing.T) {
	fixture := draftFragmentFixture()
	fixture["history"] = tapeHistoryFixture([]league.TapePick{tapePickFixture(1, "manager")})
	fixture["picks_empty"] = false
	handler := draftFragmentHandler(draftTapeRegion, func(*http.Request) bool { return true }, func(*http.Request) map[string]any { return fixture })
	for _, path := range []string{"/draft/fragment/tape", "/draft/fragment/tape?since=nope", "/draft/fragment/tape?since=-1"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if !strings.Contains(response.Body.String(), `class="draft-history"`) {
			t.Errorf("%s must render the full DraftHistory pane: %s", path, response.Body.String())
		}
	}
}

// TestTapeFragmentETagChangesWithTheSinceCursor is the Task 6 review's R1
// fix: "/draft/fragment/tape", "?since=0", and "?since=2" against the SAME
// underlying picks must hash to three different ETags — one per distinct
// DraftTapeRows/DraftHistory body — so a client that switches its cursor
// never gets served a bodyless 304 carrying the wrong rows.
func TestTapeFragmentETagChangesWithTheSinceCursor(t *testing.T) {
	fixture := draftFragmentFixture()
	fixture["history"] = tapeHistoryFixture([]league.TapePick{tapePickFixture(1, "manager"), tapePickFixture(2, "manager"), tapePickFixture(3, "auto")})
	fixture["picks_empty"] = false
	handler := draftFragmentHandler(draftTapeRegion, func(*http.Request) bool { return true }, func(*http.Request) map[string]any { return fixture })
	etagFor := func(path string) string {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		return response.Header().Get("ETag")
	}
	full := etagFor("/draft/fragment/tape")
	since0 := etagFor("/draft/fragment/tape?since=0")
	since2 := etagFor("/draft/fragment/tape?since=2")
	if full == since0 || full == since2 || since0 == since2 {
		t.Fatalf("tape ETags must differ per cursor: full=%s since=0:%s since=2:%s", full, since0, since2)
	}
}

// TestDraftPostFormsEitherSignalOrAreExplicitlyAllowlisted is execution
// note 2 (Task 5a review): count(data-gosx-action-signal) + count(forms in
// an explicit signal-free allowlist) == count(<form method="post"), so an
// added post form can never silently omit both the manual refresh signal
// and a considered reason not to carry one.
func TestDraftPostFormsEitherSignalOrAreExplicitlyAllowlisted(t *testing.T) {
	source, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	// Scoped to the app shell (D2, D5, Page()'s own component tree): the
	// legacy DraftRoom/DraftWorkspace components above this marker keep
	// every form signaled — Task 8 (target mode, 2026-08-30) kept both
	// mounted rather than retiring them (they stay unreachable from
	// Page(), a documented, deliberately deferred cleanup) — and happen
	// to reuse the same props.Actions.toggle_ready/toggle_autopick
	// action expressions the shell's own signal-free forms below the
	// marker use.
	if marker := strings.Index(string(source), "// --- The app shell (D2, D5)"); marker >= 0 {
		source = source[marker:]
	}
	// Pick-mutation forms (make-pick, queue-add, queue-remove) and the
	// seated manager's own ready/autopick forms (the command bar until
	// V1 moved them into the my-team pane's Room tab and the mobile pick
	// bar) all rely on the typed hub events their own region already
	// listens to (draft:pick/undo/state/seat, Page()), not a manual
	// refresh-signal poke.
	allowlist := []string{
		`action={props.MakePickAction}`,
		`action={props.QueueAddAction}`,
		`action={props.QueueRemoveAction}`,
		`action={props.Actions.toggle_ready}`,
		`action={props.Actions.toggle_autopick}`,
	}
	var signaled, signalFree, total int
	for _, line := range strings.Split(string(source), "\n") {
		if !strings.Contains(line, `<form method="post"`) {
			continue
		}
		total++
		hasSignal := strings.Contains(line, "data-gosx-action-signal")
		allowed := false
		for _, marker := range allowlist {
			if strings.Contains(line, marker) {
				allowed = true
				break
			}
		}
		switch {
		case hasSignal && allowed:
			t.Errorf("form carries both a refresh signal and an allowlisted signal-free action; keep only one: %s", strings.TrimSpace(line))
		case hasSignal:
			signaled++
		case allowed:
			signalFree++
		default:
			t.Errorf("form neither carries data-gosx-action-signal nor matches the signal-free allowlist: %s", strings.TrimSpace(line))
		}
	}
	if total == 0 {
		t.Fatal(`no <form method="post"> found in page.gsx`)
	}
	if signaled+signalFree != total {
		t.Fatalf("post forms = %d, signaled %d + allowlisted %d = %d", total, signaled, signalFree, signaled+signalFree)
	}
}

func TestDraftRegionContractIsPushDrivenAndMounted(t *testing.T) {
	page, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	source := string(page)
	// Task 8 (target mode, gosx@v0.53.10): the command bar's own region
	// (data-gosx-region-url="/draft/fragment/command", -on=...) retired in
	// favor of a fetchless data-gosx-live-mode="event" root — the whole
	// point of target mode's zero-fetch-per-pick budget (S6). DraftRoom
	// and DraftWorkspace still carry the OLD fallback-shaped markup
	// verbatim (their own /draft/fragment/room|workspace routes stay
	// mounted, unused by Page()), so this test still pins them by name.
	for _, want := range []string{
		`data-gosx-live-mode="event"`,
		`data-gosx-live-src="/draft/live.json"`,
		`data-gosx-live-hub="draft-live"`,
		`data-gosx-live-on="draft:pick draft:undo draft:clock draft:seat draft:state"`,
		`data-gosx-action-signal="$draft.state.refresh"`,
		`data-gosx-countdown={props.Data.clock.effective_deadline}`,
		`data-gosx-countdown-format="mm:ss"`,
		`func DraftRoom(props DraftRoomProps) Node {`,
		`func DraftWorkspace(props DraftWorkspaceProps) Node {`,
		`aria-live="polite"`,
	} {
		if !strings.Contains(source, want) {
			t.Errorf("draft region contract missing %q", want)
		}
	}
	for _, forbidden := range []string{"data-gosx-revalidate", "data-gosx-region-interval", "Reload the room", "Reload the player list"} {
		if strings.Contains(source, forbidden) {
			t.Errorf("draft page retains refresh-driven behavior %q", forbidden)
		}
	}
	// Pick-mutation forms (make-pick, queue-add, queue-remove) rely on the
	// typed hub events the command/available/queue regions already listen
	// to, not a manual refresh-signal poke; a commissioner form still
	// carries the signal as an explicit nudge (its own region does not
	// listen to every event a commissioner action can produce).
	for _, forbiddenAction := range []string{`action={props.QueueAddAction} data-gosx-managed="true" data-gosx-action-signal`, `action={props.QueueRemoveAction} data-gosx-managed="true" data-gosx-action-signal`, `action={props.MakePickAction} data-gosx-managed="true" data-gosx-action-signal`} {
		if strings.Contains(source, forbiddenAction) {
			t.Errorf("a pick-mutation form still carries a manual refresh-signal poke: %q", forbiddenAction)
		}
	}
	if !strings.Contains(source, `action={props.Actions.draft_start} data-gosx-managed="true" data-gosx-action-signal="$draft.state.refresh"`) {
		t.Error("the commissioner draft-start form dropped its refresh-signal nudge")
	}

	serverSource, err := os.ReadFile("page.server.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`BindHub(draftLiveHubName, draftLiveBindingPath(), nil)`,
		`action.WantsJSON(ctx.Request)`,
		`ctx.Success(message, map[string]any{"value": "refresh"})`,
	} {
		if !strings.Contains(string(serverSource), want) {
			t.Errorf("draft server synchronization contract missing %q", want)
		}
	}

	buildSource := rootPackageSource(t)
	for _, want := range []string{
		`app.Mount("GET /draft/fragment/room", draftpage.RoomFragmentHandler(league.Default()))`,
		`app.Mount("GET /draft/fragment/workspace", draftpage.WorkspaceFragmentHandler(league.Default()))`,
		`app.Mount("GET /draft/fragment/command", draftpage.CommandFragmentHandler(league.Default()))`,
		`app.Mount("GET /draft/fragment/tape", draftpage.TapeFragmentHandler(league.Default()))`,
		`app.Mount("GET /draft/fragment/available", draftpage.AvailableFragmentHandler(league.Default()))`,
		`app.Mount("GET /draft/fragment/queue", draftpage.QueueFragmentHandler(league.Default()))`,
		`app.Mount("POST /draft/queue", draftpage.QueueMoveHandler(league.Default()))`,
		`app.Mount("GET /draft/live.json", draftpage.LiveViewHandler(league.Default()))`,
		`app.Mount("GET /draft/ledger.csv", draftpage.LedgerCSVHandler(league.Default()))`,
		`app.Mount(draftpage.DraftLiveHubPath, draftLiveUpdates.Handler(league.Default()))`,
	} {
		if !strings.Contains(buildSource, want) {
			t.Errorf("draft fragment route missing mount %q", want)
		}
	}
}

// rootPackageSource concatenates every non-test Go file of the root package.
// The mount contract asks where a route is registered, not which file holds
// it, so a later move inside the root package cannot silently pass.
func rootPackageSource(t *testing.T) string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join("..", "..", "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	var sources strings.Builder
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		sources.Write(body)
		sources.WriteByte('\n')
	}
	if sources.Len() == 0 {
		t.Fatal("root package sources not found")
	}
	return sources.String()
}

// TestQueueAddAndQueueRemoveCarryThePoolStateInputs is execution note 7
// (Task 5a review): queue-add and queue-remove redirect through
// draftRedirectTarget(pos, q, page) on a native (no-JS) submit, so both
// forms must carry the same three hidden pool-state inputs make-pick
// already does — otherwise a manager who queues or clears a player from
// page 3 of a filtered search lands back on page 1, unfiltered.
func TestQueueAddAndQueueRemoveCarryThePoolStateInputs(t *testing.T) {
	fixture := draftFragmentFixture()
	viewer := fixture["viewer"].(map[string]any)
	viewer["has_seat"] = true
	fixture["queue"] = []map[string]any{
		{
			"id": "player-taken", "name": "Taken Player", "position": "RB", "nfl_team": "TST",
			"projection": "0.0", "rank": "001", "detail": "RB · TST", "taken": true,
			"headshot": "", "has_headshot": false, "jersey": "", "has_breakdown": false, "breakdown": []map[string]any{}, "breakdown_total": "",
			"has_hist": false, "hist": "", "search": "taken player", "has_draft_capital": false, "draft_capital": "",
		},
	}
	load := func(*http.Request) map[string]any { return fixture }
	for _, test := range []struct {
		region string
		path   string
	}{
		{region: draftAvailableRegion, path: "/draft/fragment/available"},
		{region: draftQueueRegion, path: "/draft/fragment/queue"},
	} {
		handler := draftFragmentHandler(test.region, func(*http.Request) bool { return true }, load)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, test.path, nil))
		body := response.Body.String()
		for _, want := range []string{`name="pos"`, `name="q"`, `name="page"`} {
			if !strings.Contains(body, want) {
				t.Errorf("%s must carry a %s hidden input on its queue form: %s", test.path, want, body)
			}
		}
	}
}

// TestAvailableFragmentNeverRendersALockedChip is V3 (owner review): a row
// shows + QUEUE always and DRAFT only when the viewer is on the clock; when
// the viewer cannot currently pick the DRAFT control is simply absent, not
// a disabled "Locked" button.
func TestAvailableFragmentNeverRendersALockedChip(t *testing.T) {
	fixture := draftFragmentFixture()
	fixture["can_pick"] = false
	viewer := fixture["viewer"].(map[string]any)
	viewer["has_seat"] = true
	handler := draftFragmentHandler(draftAvailableRegion, func(*http.Request) bool { return true }, func(*http.Request) map[string]any { return fixture })
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/draft/fragment/available", nil))
	body := response.Body.String()
	if strings.Contains(strings.ToUpper(body), "LOCKED") {
		t.Fatalf("the available fragment must never render a LOCKED chip: %s", body)
	}
	if !strings.Contains(body, "+ Queue") {
		t.Fatalf("a seated viewer must still see + Queue when not on the clock: %s", body)
	}
}

// TestQueueFragmentRendersDataTakenForATakenQueuedPlayer is S4 (Task 5a
// review): the CSS strike-through targets [data-taken="true"] because
// that is what the markup actually emits, never an .is-taken class.
func TestQueueFragmentRendersDataTakenForATakenQueuedPlayer(t *testing.T) {
	fixture := draftFragmentFixture()
	fixture["queue"] = []map[string]any{
		{
			"id": "player-taken", "name": "Taken Player", "position": "RB", "nfl_team": "TST",
			"projection": "0.0", "rank": "001", "detail": "RB · TST", "taken": true,
			"headshot": "", "has_headshot": false, "jersey": "", "has_breakdown": false, "breakdown": []map[string]any{}, "breakdown_total": "",
			"has_hist": false, "hist": "", "search": "taken player", "has_draft_capital": false, "draft_capital": "",
		},
	}
	handler := draftFragmentHandler(draftQueueRegion, func(*http.Request) bool { return true }, func(*http.Request) map[string]any { return fixture })
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/draft/fragment/queue", nil))
	if !strings.Contains(response.Body.String(), `data-taken="true"`) {
		t.Fatalf("a taken queued player must render data-taken=\"true\": %s", response.Body.String())
	}
}
