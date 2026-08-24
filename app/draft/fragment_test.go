package draft

import (
	"html"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

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
	prepared := draftFragmentFixture()
	prepareDraftData(prepared)

	fresh := draftFragmentFixture()
	available, ok := fresh["available"].([]map[string]any)
	if !ok || len(available) != 1 {
		t.Fatalf("fresh fixture available = %#v, want one raw player map", fresh["available"])
	}
	if got := stringField(available[0], "name"); got != "Test Player" {
		t.Fatalf("fresh fixture player name = %q, want Test Player", got)
	}
	if _, ok := prepared["available"].([]draftPlayerCardView); !ok {
		t.Fatalf("prepared fixture available = %T, want prepared player cards", prepared["available"])
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

func TestDraftRegionContractIsScopedAndMounted(t *testing.T) {
	page, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	source := string(page)
	for _, want := range []string{
		`data-gosx-region-url="/draft/fragment/room"`,
		`data-gosx-region-interval="4s"`,
		`<DraftRoom {...data.room}></DraftRoom>`,
		`<DraftWorkspace {...data.workspace}></DraftWorkspace>`,
		`aria-live="polite"`,
		`Reload the room`,
	} {
		if !strings.Contains(source, want) {
			t.Errorf("draft region contract missing %q", want)
		}
	}
	if strings.Contains(source, "data-gosx-revalidate") {
		t.Fatal("draft page still performs whole-page revalidation")
	}

	mainSource, err := os.ReadFile("../../main.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`app.Mount("GET /draft/fragment/room", draftpage.RoomFragmentHandler(league.Default()))`,
		`app.Mount("GET /draft/fragment/workspace", draftpage.WorkspaceFragmentHandler(league.Default()))`,
	} {
		if !strings.Contains(string(mainSource), want) {
			t.Errorf("draft fragment route missing mount %q", want)
		}
	}
}
