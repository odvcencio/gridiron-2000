package players

import (
	"strings"
	"testing"

	"m31labs.dev/gosx/route"
)

// playersRegionParityFixtureData builds a hand-written data map covering
// every data.*/player.*/claim.*/receipt.*/tab.*/opt.*/slot.* field page.gsx
// reads, with one droppable rostered player and one needs-a-drop free
// agent — the two rows that carry the two-step confirmation gate this
// item restores. Building the map directly (rather than through
// league.Service) keeps the fixture independent of draft/roster
// simulation while still exercising the real Page()/PlayerPoolRegion()/
// WaiverDeskRegion() render paths byte-for-byte.
func playersRegionParityFixtureData() map[string]any {
	return map[string]any{
		"viewer":               map[string]any{"demo": false, "team_id": "team-1"},
		"pool_unavailable":     false,
		"free_agency_open":     true,
		"can_edit":             true,
		"notice_count":         0,
		"notice_first_kind":    "",
		"has_notice":           false,
		"notice":               "",
		"has_players_error":    false,
		"players_error":        "",
		"has_matchup_source":   false,
		"matchup_source_label": "",
		"pool_status": map[string]any{
			"has_notice":            false,
			"label":                 "",
			"detail":                "",
			"has_last_success":      false,
			"last_success":          "",
			"last_success_relative": "",
		},
		"public_entry": map[string]any{
			"state_label":        "",
			"detail":             "",
			"can_claim":          false,
			"action_href":        "/join",
			"action_label":       "",
			"is_commissioner":    false,
			"commissioner_href":  "",
			"commissioner_label": "",
		},
		"positions":                    []map[string]any{{"href": "/players", "label": "ALL", "active": true}},
		"query":                        "",
		"pos":                          "",
		"pool_total":                   2,
		"pool_page":                    1,
		"pool_pages":                   1,
		"pool_page_start":              1,
		"pool_page_end":                2,
		"players_empty":                false,
		"pool_has_previous":            false,
		"pool_has_next":                false,
		"pool_previous_href":           "",
		"pool_next_href":               "",
		"drop_options":                 []map[string]any{{"id": "p-drop", "label": "Bench Runner"}},
		"add_locked_reason":            "",
		"roster_size":                  10,
		"roster_cap":                   16,
		"roster_general_size":          9,
		"roster_general_cap":           13,
		"roster_reserve_size":          1,
		"roster_reserve_cap":           3,
		"roster_ir_size":               0,
		"roster_ir_cap":                2,
		"is_commissioner":              false,
		"my_claims":                    []map[string]any{},
		"my_claims_empty":              true,
		"waiver_order":                 []map[string]any{},
		"my_waiver_receipts":           []map[string]any{},
		"my_receipts_empty":            true,
		"commissioner_waiver_receipts": []map[string]any{},
		"commissioner_receipts_empty":  true,
		"waivers_faab":                 false,
		"my_faab_remaining":            0,
		"my_waiver_position":           1,
		"waiver_team_count":            10,
		"pool_fragment_url":            "/players/fragment/pool",
		"pool_fragment_interval":       playersRegionInterval,
		"waiver_fragment_url":          "/players/fragment/waivers",
		"waiver_fragment_interval":     playersRegionInterval,
		"players": []map[string]any{
			{
				"id": "p-drop", "name": "Bench Runner", "position": "RB", "nfl_team": "SF",
				"rank": 1, "has_house_rank": false, "house_rank": "",
				"has_headshot": false, "headshot": "",
				"has_draft_capital": false, "draft_capital": "",
				"detail":       "SF · BYE 9",
				"has_opponent": false, "opponent": "", "has_matchup": false, "matchup_chip": "", "matchup_tier": "", "matchup_detail": "",
				"has_hist": false, "hist": "", "hist_label": "",
				"has_news": false, "news": "", "has_injury": false, "injury": "",
				"projection": "12.3",
				"rostered":   true, "owner_abbr": "ME", "owner_name": "My Team", "is_drafted": false, "drafted_label": "",
				"on_waivers": false, "waiver_resolves": "",
				"free_agent":    false,
				"claimed_by_me": false,
				"can_add":       false, "can_claim": false, "needs_drop": false,
				"can_drop": true, "drop_locked": false, "drop_lock_reason": "",
			},
			{
				"id": "p-add", "name": "Waiver Wire Guy", "position": "WR", "nfl_team": "KC",
				"rank": 2, "has_house_rank": true, "house_rank": "H002",
				"has_headshot": false, "headshot": "",
				"has_draft_capital": false, "draft_capital": "",
				"detail":       "KC · BYE 6",
				"has_opponent": false, "opponent": "", "has_matchup": false, "matchup_chip": "", "matchup_tier": "", "matchup_detail": "",
				"has_hist": false, "hist": "", "hist_label": "",
				"has_news": false, "news": "", "has_injury": false, "injury": "",
				"projection": "9.1",
				"rostered":   false, "owner_abbr": "", "owner_name": "", "is_drafted": false, "drafted_label": "",
				"on_waivers": false, "waiver_resolves": "",
				"free_agent":    true,
				"claimed_by_me": false,
				"can_add":       true, "can_claim": false, "needs_drop": true,
				"can_drop": false, "drop_locked": false, "drop_lock_reason": "",
			},
		},
	}
}

// TestPlayersPageEmbedsTheSameRegionComponentsAsTheFragmentHandlers is the
// root-cause parity proof for item 1 (rowan's route-crawl finding): the
// player-pool and waiver-desk fragments used to be hand-copied markup that
// silently dropped the two-step drop-confirmation gate. Page() now embeds
// <PlayerPoolRegion></PlayerPoolRegion> and <WaiverDeskRegion></WaiverDeskRegion>
// directly, so this test renders "Page", "PlayerPoolRegion", and
// "WaiverDeskRegion" from the SAME program with the SAME data fixture and
// asserts the region output is byte-identical to (a substring of) the
// full-page output — drift between the initial render and a later 4s poll
// is now a structural impossibility, not just a passing test.
func TestPlayersPageEmbedsTheSameRegionComponentsAsTheFragmentHandlers(t *testing.T) {
	program, err := route.LoadFileProgramHere("page.gsx")
	if err != nil {
		t.Fatalf("LoadFileProgramHere: %v", err)
	}
	env := route.ProgramRenderEnv{
		Values: map[string]any{
			"data": playersRegionParityFixtureData(),
			"csrf": map[string]any{"token": "test-csrf-token", "field": "csrf_token"},
		},
		Funcs: map[string]any{
			"actionPath": func(name string) string { return "/players/__actions/" + name },
		},
	}

	pageHTML, err := route.RenderProgramComponent(program, "Page", env)
	if err != nil {
		t.Fatalf("render Page: %v", err)
	}
	poolHTML, err := route.RenderProgramComponent(program, "PlayerPoolRegion", env)
	if err != nil {
		t.Fatalf("render PlayerPoolRegion: %v", err)
	}
	waiverHTML, err := route.RenderProgramComponent(program, "WaiverDeskRegion", env)
	if err != nil {
		t.Fatalf("render WaiverDeskRegion: %v", err)
	}

	if !strings.Contains(pageHTML, poolHTML) {
		t.Errorf("Page() render does not contain PlayerPoolRegion() render verbatim — the fragment and the initial page have drifted apart")
	}
	if !strings.Contains(pageHTML, waiverHTML) {
		t.Errorf("Page() render does not contain WaiverDeskRegion() render verbatim — the fragment and the initial page have drifted apart")
	}

	// The two-step drop-confirmation gate (item 1's own regression): a
	// <details> disclosure names the player in its <summary>, warns the
	// action cannot be undone, requires a checkbox, and the submit button
	// carries a player-named aria-label — present identically in both the
	// full page and the standalone fragment render.
	dropGateMarkers := []string{
		`<details class="action-confirmation">`,
		`<summary>Drop Bench Runner</summary>`,
		`I understand this player will leave my roster.`,
		`aria-label="Confirm drop Bench Runner"`,
	}
	for _, render := range []struct {
		name string
		html string
	}{{"Page", pageHTML}, {"PlayerPoolRegion", poolHTML}} {
		for _, marker := range dropGateMarkers {
			if !strings.Contains(render.html, marker) {
				t.Errorf("%s render missing drop-confirmation marker %q", render.name, marker)
			}
		}
	}

	// The add-and-drop gate carries the equivalent two-step disclosure.
	addDropGateMarkers := []string{
		`<summary>Add and drop a player</summary>`,
		"The drop is recorded and cannot be undone from this screen.",
		`value="add-drop-player"`,
	}
	for _, render := range []struct {
		name string
		html string
	}{{"Page", pageHTML}, {"PlayerPoolRegion", poolHTML}} {
		for _, marker := range addDropGateMarkers {
			if !strings.Contains(render.html, marker) {
				t.Errorf("%s render missing add-and-drop confirmation marker %q", render.name, marker)
			}
		}
	}

	// One id per control (rowan's finding): the search input and its
	// label must appear exactly once in the full page, never twice under
	// a "-sync-" variant.
	for _, id := range []string{`id="players-search"`, `for="players-search"`, `id="waivers-content"`} {
		if got := strings.Count(pageHTML, id); got != 1 {
			t.Errorf("Page() render has %d occurrences of %q, want exactly 1", got, id)
		}
	}
}
