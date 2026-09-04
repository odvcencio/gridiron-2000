package league

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestTransactionPlayerFromPlayer(t *testing.T) {
	player := Player{ID: "p-01", Name: "Ja'Marr Chase", Position: "WR", NFLTeam: "CIN"}
	got := transactionPlayerFromPlayer(player)
	want := TransactionPlayer{PlayerID: "p-01", Name: "Ja'Marr Chase", Position: "WR", NFLTeam: "CIN"}
	if got != want {
		t.Fatalf("transactionPlayerFromPlayer = %+v, want %+v", got, want)
	}
}

func TestRandomTransactionIDShapeAndUniqueness(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		id, err := randomTransactionID()
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(id, "txn-") || len(id) != len("txn-")+8 {
			t.Fatalf("id = %q, want \"txn-\" plus 8 hex characters", id)
		}
		if seen[id] {
			t.Fatalf("randomTransactionID repeated %q across 50 draws", id)
		}
		seen[id] = true
	}
}

func TestActivityLineAddOnly(t *testing.T) {
	txn := Transaction{Type: "add", Adds: []TransactionPlayer{{Name: "Free Agent Open", Position: "RB"}}}
	action, player := activityLine(txn)
	if action != "signs" || player != "Free Agent Open (RB)" {
		t.Fatalf("action=%q player=%q", action, player)
	}
}

func TestActivityLineDropOnly(t *testing.T) {
	txn := Transaction{Type: "drop", Drops: []TransactionPlayer{{Name: "Bench Wideout", Position: "WR"}}}
	action, player := activityLine(txn)
	if action != "drops" || player != "Bench Wideout (WR)" {
		t.Fatalf("action=%q player=%q", action, player)
	}
}

func TestActivityLineAddWithDrop(t *testing.T) {
	txn := Transaction{
		Type:  "add",
		Adds:  []TransactionPlayer{{Name: "Free Agent Open", Position: "RB"}},
		Drops: []TransactionPlayer{{Name: "Bench Wideout", Position: "WR"}},
	}
	action, player := activityLine(txn)
	if action != "signs" {
		t.Fatalf("action = %q, want %q", action, "signs")
	}
	if !strings.Contains(player, "Free Agent Open (RB)") || !strings.Contains(player, "Bench Wideout (WR)") {
		t.Fatalf("player = %q, want both names present", player)
	}
}

// TestActivityLineForwardCompatTypes checks the claim/trade/auto-drop/
// commissioner branches never render a blank line, even though WP-R3
// never writes these types itself — WP-R4 and WP-R5 land their entries
// into the same feed with no line-builder change.
func TestActivityLineForwardCompatTypes(t *testing.T) {
	cases := []Transaction{
		{Type: "claim", Adds: []TransactionPlayer{{Name: "A", Position: "RB"}}, Drops: []TransactionPlayer{{Name: "B", Position: "WR"}}},
		{Type: "trade", Adds: []TransactionPlayer{{Name: "A", Position: "RB"}}, Drops: []TransactionPlayer{{Name: "B", Position: "WR"}}},
		{Type: "auto-drop", Drops: []TransactionPlayer{{Name: "B", Position: "WR"}}},
		{Type: "commissioner", Adds: []TransactionPlayer{{Name: "A", Position: "RB"}}},
	}
	for _, txn := range cases {
		action, player := activityLine(txn)
		if action == "" || player == "" {
			t.Fatalf("type %q: action=%q player=%q, want both non-empty", txn.Type, action, player)
		}
	}
}

// TestActivityLineTradeReadsFromTheActingTeamsOwnPerspective is item 8's
// own regression test (2026-08-31 post-wave audit): ExecuteTradeOffer
// (store.go) sets a trade Transaction's TeamID to the acting
// (initiating) side, with Adds = what that team RECEIVED (offer.Get) and
// Drops = what that team GAVE UP (offer.Give). The feed row leads with
// this same team (activityTeamIDs, activity.go), so the line must read
// "gives <what they gave> for <what they got>" — before this fix,
// activityLine returned "trades " + adds + " for " + drops, naming what
// the acting team RECEIVED first: "Kernel Panic trades Jerry Jeudy for
// Chris Olave" read as if Kernel Panic gave up Jeudy, when they actually
// gave up Olave and received Jeudy.
func TestActivityLineTradeReadsFromTheActingTeamsOwnPerspective(t *testing.T) {
	txn := Transaction{
		Type:        "trade",
		TeamID:      "team-kernel-panic",
		OtherTeamID: "team-other",
		Adds:        []TransactionPlayer{{Name: "Jerry Jeudy", Position: "WR"}}, // what team-kernel-panic RECEIVED
		Drops:       []TransactionPlayer{{Name: "Chris Olave", Position: "WR"}}, // what team-kernel-panic GAVE UP
	}
	action, player := activityLine(txn)
	if action != "gives" {
		t.Fatalf("action = %q, want %q", action, "gives")
	}
	if want := "Chris Olave (WR) for Jerry Jeudy (WR)"; player != want {
		t.Fatalf("player = %q, want %q (gives what they GAVE UP first, then what they RECEIVED)", player, want)
	}
}

// TestActivityMapsDraftRowCarriesRoundAndPick is wave 7's item 2: a
// draft-pick row's own "player" label gains " — R# · P#" so the
// /activity draft line reads "drafts Ja'Marr Chase (WR) — R1 · P1", never
// just the bare name/position an add or drop row already carries.
func TestActivityMapsDraftRowCarriesRoundAndPick(t *testing.T) {
	svc := newTestService(t, true)
	svc.SetPlayerSource(func() ([]Player, int64, string) { return testPool(5), 1, "test" })
	base := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	state := PersistedState{
		Picks: []DraftPick{
			{Number: 1, Round: 1, TeamID: "team-1", PlayerID: "pool-001", MadeAt: base},
		},
	}
	rows := svc.activityMaps(state, 0)
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	want := "Pool Player 001 (QB) — R1 · P1"
	if rows[0]["player"] != want {
		t.Fatalf("draft row player = %q, want %q", rows[0]["player"], want)
	}
}

// TestActivityMapsAttributesAutoAndCommissionerPicksHonestly is the
// end-to-end half of F3 (gap-audit J2): activityMaps must route a real
// DraftPick's MadeBy through draftPickActivityLine, not just build the
// "drafts" default. A no-show's autopick and a commissioner's forced pick
// must never enter the permanent record indistinguishable from a manual
// pick.
func TestActivityMapsAttributesAutoAndCommissionerPicksHonestly(t *testing.T) {
	svc := newTestService(t, true)
	svc.SetPlayerSource(func() ([]Player, int64, string) { return testPool(5), 1, "test" })
	base := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	state := PersistedState{
		Picks: []DraftPick{
			{Number: 1, Round: 1, TeamID: "team-1", PlayerID: "pool-001", MadeAt: base, MadeBy: "manager"},
			{Number: 2, Round: 1, TeamID: "team-2", PlayerID: "pool-002", MadeAt: base.Add(time.Minute), MadeBy: "auto"},
			{Number: 3, Round: 1, TeamID: "team-3", PlayerID: "pool-003", MadeAt: base.Add(2 * time.Minute), MadeBy: "commissioner"},
		},
	}
	rows := svc.activityMaps(state, 0)
	if len(rows) != 3 {
		t.Fatalf("len(rows) = %d, want 3", len(rows))
	}
	team1, _, _ := svc.activityTeamDisplay(state, []string{"team-1"})
	team2, _, _ := svc.activityTeamDisplay(state, []string{"team-2"})
	team3, _, _ := svc.activityTeamDisplay(state, []string{"team-3"})

	// Newest first: commissioner (P3), auto (P2), manager (P1).
	if got, want := rows[0]["team"], "Commissioner"; got != want {
		t.Fatalf("commissioner pick team = %q, want %q", got, want)
	}
	if got, want := rows[0]["action"], "picks"; got != want {
		t.Fatalf("commissioner pick action = %q, want %q", got, want)
	}
	if got, want := rows[0]["player"], "Pool Player 003 (WR) — R1 · P3 for "+team3; got != want {
		t.Fatalf("commissioner pick player = %q, want %q", got, want)
	}

	if got, want := rows[1]["team"], "Autopick for "+team2; got != want {
		t.Fatalf("auto pick team = %q, want %q", got, want)
	}
	if got, want := rows[1]["action"], "selects"; got != want {
		t.Fatalf("auto pick action = %q, want %q", got, want)
	}

	if got, want := rows[2]["team"], team1; got != want {
		t.Fatalf("manager pick team = %q, want %q", got, want)
	}
	if got, want := rows[2]["action"], "drafts"; got != want {
		t.Fatalf("manager pick action = %q, want %q", got, want)
	}
	// Team filtering (team_search) still finds the auto/commissioner rows
	// under the real team's own code — the attribution prefix only
	// changes what renders, never who a "team=AQ2" filter matches.
	if search, _ := rows[1]["team_search"].(string); !strings.Contains(search, "team-2") {
		t.Fatalf("auto pick team_search = %q, missing team-2", search)
	}
	if search, _ := rows[0]["team_search"].(string); !strings.Contains(search, "team-3") {
		t.Fatalf("commissioner pick team_search = %q, missing team-3", search)
	}
}

// TestDraftPickActivityLineAttributesByMadeBy pins F3 (gap-audit J2): a
// no-show's autopick and a commissioner's forced pick used to enter
// /activity as "<team> drafts <player>", identical to a manual pick, with
// nothing telling a manager or the commissioner apart from the machine or
// each other. This is the shared line builder both /activity
// (activityMaps) and the room's own pick tape (draftPickAttributionSentence,
// TapePick.AttributionLine) read.
func TestDraftPickActivityLineAttributesByMadeBy(t *testing.T) {
	const team = "Los Delfines del Norte (AQ2)"
	const player = "Jaxon Smith-Njigba (WR) — R1 · P8"

	for _, tc := range []struct {
		madeBy                           string
		wantTeam, wantAction, wantPlayer string
	}{
		{"manager", team, "drafts", player},
		{"", team, "drafts", player}, // pre-provenance state (model.go) reads as manager
		{"auto", "Autopick for " + team, "selects", player},
		{"commissioner", "Commissioner", "picks", player + " for " + team},
	} {
		t.Run("MadeBy="+tc.madeBy, func(t *testing.T) {
			gotTeam, gotAction, gotPlayer := draftPickActivityLine(tc.madeBy, team, player)
			if gotTeam != tc.wantTeam || gotAction != tc.wantAction || gotPlayer != tc.wantPlayer {
				t.Fatalf("draftPickActivityLine(%q) = (%q, %q, %q), want (%q, %q, %q)",
					tc.madeBy, gotTeam, gotAction, gotPlayer, tc.wantTeam, tc.wantAction, tc.wantPlayer)
			}
		})
	}
}

// TestDraftPickAttributionSentenceMatchesTheActivityLine proves the room's
// own tape sentence (TapePick.AttributionLine) reads as the exact
// concatenation of the same three parts the activity feed renders as
// separate team/action/player columns, so the two surfaces never drift.
func TestDraftPickAttributionSentenceMatchesTheActivityLine(t *testing.T) {
	const team = "In Shedeur Time (AQ1)"
	const player = "Bucky Irving (RB) — R1 · P7"

	for _, tc := range []struct {
		madeBy string
		want   string
	}{
		{"manager", "In Shedeur Time (AQ1) drafts Bucky Irving (RB) — R1 · P7"},
		{"auto", "Autopick for In Shedeur Time (AQ1) selects Bucky Irving (RB) — R1 · P7"},
		{"commissioner", "Commissioner picks Bucky Irving (RB) — R1 · P7 for In Shedeur Time (AQ1)"},
	} {
		t.Run("MadeBy="+tc.madeBy, func(t *testing.T) {
			if got := draftPickAttributionSentence(tc.madeBy, team, player); got != tc.want {
				t.Fatalf("draftPickAttributionSentence(%q) = %q, want %q", tc.madeBy, got, tc.want)
			}
		})
	}
}

// TestActivityMapsOrderingAndLimit checks activityMaps merges Picks and
// Transactions newest-first and honors the row limit (the dashboard's
// "newest five" contract, section 8.4).
func TestActivityMapsOrderingAndLimit(t *testing.T) {
	svc := newTestService(t, true)
	svc.SetPlayerSource(func() ([]Player, int64, string) { return testPool(5), 1, "test" })
	base := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	state := PersistedState{
		Picks: []DraftPick{
			{Number: 1, TeamID: "team-1", PlayerID: "pool-001", MadeAt: base},
		},
		Transactions: []Transaction{
			{ID: "txn-1", Type: "add", TeamID: "team-2", Adds: []TransactionPlayer{{Name: "FA One", Position: "RB"}}, At: base.Add(time.Hour)},
			{ID: "txn-2", Type: "drop", TeamID: "team-3", Drops: []TransactionPlayer{{Name: "FA Two", Position: "WR"}}, At: base.Add(2 * time.Hour)},
		},
	}
	rows := svc.activityMaps(state, 0)
	if len(rows) != 3 {
		t.Fatalf("len(rows) = %d, want 3", len(rows))
	}
	if rows[0]["action"] != "drops" || rows[1]["action"] != "signs" || rows[2]["action"] != "drafts" {
		t.Fatalf("order = [%v %v %v], want [drops signs drafts] (newest first)", rows[0]["action"], rows[1]["action"], rows[2]["action"])
	}

	limited := svc.activityMaps(state, 2)
	if len(limited) != 2 {
		t.Fatalf("len(limited) = %d, want 2", len(limited))
	}
	if limited[0]["action"] != "drops" || limited[1]["action"] != "signs" {
		t.Fatalf("limited order = [%v %v], want [drops signs]", limited[0]["action"], limited[1]["action"])
	}
}

// TestActivityDataPaginatesFullFeed checks /activity keeps the complete
// record addressable without sending an ever-growing season log in one page.
func TestActivityDataPaginatesFullFeed(t *testing.T) {
	svc := newTestService(t, true)
	svc.SetPlayerSource(func() ([]Player, int64, string) { return testPool(5), 1, "test" })
	base := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 107; i++ {
		txn := Transaction{
			ID: fmt.Sprintf("txn-%d", i), Type: "add", TeamID: "team-1",
			Adds: []TransactionPlayer{{PlayerID: fmt.Sprintf("p-%d", i), Name: fmt.Sprintf("Player %03d", i), Position: "RB"}},
			At:   base.Add(time.Duration(i) * time.Minute),
		}
		if err := svc.store.RecordTransaction(txn, 999); err != nil {
			t.Fatal(err)
		}
	}
	request, _ := http.NewRequest(http.MethodGet, "/activity?page=2", nil)
	data := svc.ActivityData(request)
	rows, _ := data["transactions"].([]map[string]any)
	if len(rows) != poolPageSize {
		t.Fatalf("len(rows) = %d, want %d", len(rows), poolPageSize)
	}
	if data["transactions_count"] != 107 || data["filtered_count"] != 107 {
		t.Fatalf("counts = total %v filtered %v, want 107/107", data["transactions_count"], data["filtered_count"])
	}
	if data["page"] != 2 || data["pages"] != 3 || data["page_start"] != 51 || data["page_end"] != 100 {
		t.Fatalf("pagination = page %v/%v rows %v-%v", data["page"], data["pages"], data["page_start"], data["page_end"])
	}
	if data["previous_href"] != "/activity" || data["next_href"] != "/activity?page=3" {
		t.Fatalf("pagination hrefs = previous %v next %v", data["previous_href"], data["next_href"])
	}
	if rows[0]["player"] != "Player 056 (RB)" || rows[len(rows)-1]["player"] != "Player 007 (RB)" {
		t.Fatalf("page 2 ordering = first %v last %v", rows[0]["player"], rows[len(rows)-1]["player"])
	}
	if data["transactions_empty"].(bool) {
		t.Fatal("transactions_empty must be false with matching rows")
	}
}

func TestActivityDataFiltersByTeamAndQueryAndPreservesState(t *testing.T) {
	svc := newTestService(t, true)
	base := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	transactions := []Transaction{
		{ID: "txn-1", Type: "add", TeamID: "team-1", Adds: []TransactionPlayer{{Name: "Needle Runner", Position: "RB"}}, At: base},
		{ID: "txn-2", Type: "add", TeamID: "team-2", Adds: []TransactionPlayer{{Name: "Needle Receiver", Position: "WR"}}, At: base.Add(time.Minute)},
		{ID: "txn-3", Type: "add", TeamID: "team-1", Adds: []TransactionPlayer{{Name: "Different Player", Position: "TE"}}, At: base.Add(2 * time.Minute)},
	}
	for _, txn := range transactions {
		if err := svc.store.RecordTransaction(txn, 99); err != nil {
			t.Fatal(err)
		}
	}
	team := svc.teamByID("team-1").Abbreviation
	request, _ := http.NewRequest(http.MethodGet, "/activity?team="+team+"&q=needle", nil)
	data := svc.ActivityData(request)
	rows, _ := data["transactions"].([]map[string]any)
	if len(rows) != 1 || rows[0]["player"] != "Needle Runner (RB)" {
		t.Fatalf("filtered rows = %+v, want team-1's Needle Runner only", rows)
	}
	if data["transactions_count"] != 3 || data["filtered_count"] != 1 || data["has_filters"] != true {
		t.Fatalf("filter state = total %v filtered %v active %v", data["transactions_count"], data["filtered_count"], data["has_filters"])
	}
	if data["previous_href"] != "/activity?q=needle&team="+team {
		t.Fatalf("preserved href = %v", data["previous_href"])
	}

	request, _ = http.NewRequest(http.MethodGet, "/activity?q=not-found", nil)
	data = svc.ActivityData(request)
	if data["has_transactions"] != true || data["transactions_empty"] != true || data["page_start"] != 0 {
		t.Fatalf("no-match state = has %v empty %v start %v", data["has_transactions"], data["transactions_empty"], data["page_start"])
	}
}

// TestActivityDataTeamOptionsCarryTheTeamNameWithTheCodeSecondary is item
// 9's own regression test (2026-08-31 post-wave audit): the /activity
// team filter used to expose only bare abbreviations ("teams", still kept
// for backward compatibility) with no team name anywhere — team_options
// pairs each option's value (the abbreviation the "team" query param and
// activityTeamMatches key on) with a label naming the team ("Team Name
// (CODE)"), so app/activity/page.gsx (tamarack) can render the filter
// <select> the same "name with code secondary" way the /players owner
// chip and waiver-order strip now do.
func TestActivityDataTeamOptionsCarryTheTeamNameWithTheCodeSecondary(t *testing.T) {
	svc := newTestService(t, true)
	svc.teams = []Team{
		{ID: "team-1", Name: "Alpha Aces", Abbreviation: "ALP"},
		{ID: "team-2", Name: "Beta Bears", Abbreviation: "BET"},
	}

	request, _ := http.NewRequest(http.MethodGet, "/activity?team=BET", nil)
	data := svc.ActivityData(request)
	options, ok := data["team_options"].([]map[string]any)
	if !ok || len(options) != 2 {
		t.Fatalf("team_options = %#v, want 2 entries", data["team_options"])
	}
	if options[0]["value"] != "ALP" || options[0]["label"] != "Alpha Aces (ALP)" || options[0]["selected"] != false {
		t.Fatalf("team_options[0] = %+v, want value=ALP label=\"Alpha Aces (ALP)\" selected=false", options[0])
	}
	if options[1]["value"] != "BET" || options[1]["label"] != "Beta Bears (BET)" || options[1]["selected"] != true {
		t.Fatalf("team_options[1] = %+v, want value=BET label=\"Beta Bears (BET)\" selected=true", options[1])
	}
}

// TestActivityDataTeamOptionsReflectALiveRename is item 1's own regression
// test (2026-09-02 audit): team_options used to read straight off
// s.Teams() (the config seed), so a renamed team's filter option kept
// printing the SEED name ("Aqua 1 (AQ1)") even though every feed row and
// the sidebar already read the live, renamed name via teamView/
// activityTeamDisplay — the same live-name lookup this function now uses.
func TestActivityDataTeamOptionsReflectALiveRename(t *testing.T) {
	svc := newTestService(t, true)
	svc.teams = []Team{
		{ID: "team-1", Name: "Aqua 1", Abbreviation: "AQ1"},
		{ID: "team-2", Name: "Aqua 2", Abbreviation: "AQ2"},
	}
	if err := svc.store.SetTeamName("team-1", "In Shedeur Time"); err != nil {
		t.Fatalf("SetTeamName: %v", err)
	}
	if err := svc.store.SetTeamName("team-2", "Los Delfines del Norte"); err != nil {
		t.Fatalf("SetTeamName: %v", err)
	}

	request, _ := http.NewRequest(http.MethodGet, "/activity", nil)
	data := svc.ActivityData(request)
	options, ok := data["team_options"].([]map[string]any)
	if !ok || len(options) != 2 {
		t.Fatalf("team_options = %#v, want 2 entries", data["team_options"])
	}
	if options[0]["label"] != "In Shedeur Time (AQ1)" {
		t.Errorf("team_options[0] label = %v, want the live renamed team name, not the config seed", options[0]["label"])
	}
	if options[1]["label"] != "Los Delfines del Norte (AQ2)" {
		t.Errorf("team_options[1] label = %v, want the live renamed team name, not the config seed", options[1]["label"])
	}
}

// TestActivityDataUnknownTeamCodeSurfacesANotice is item 1's second
// regression test: a "team" query value coded to no real team used to
// filter to zero rows while the <select> silently rendered as if "All
// teams" were chosen — no visible sign the code itself was the problem.
func TestActivityDataUnknownTeamCodeSurfacesANotice(t *testing.T) {
	svc := activityParityService(t)

	request, _ := http.NewRequest(http.MethodGet, "/activity?team=E1", nil)
	data := svc.ActivityData(request)
	if data["team_unknown"] != true {
		t.Fatalf("team_unknown = %v, want true for a code no team carries", data["team_unknown"])
	}
	if want := "No team is coded E1."; data["team_unknown_notice"] != want {
		t.Fatalf("team_unknown_notice = %q, want %q", data["team_unknown_notice"], want)
	}
	options, ok := data["team_options"].([]map[string]any)
	if !ok {
		t.Fatalf("team_options = %#v, want []map[string]any", data["team_options"])
	}
	for _, option := range options {
		if option["selected"] == true {
			t.Errorf("team_options = %+v, want no option selected for an unknown code", options)
		}
	}

	knownRequest, _ := http.NewRequest(http.MethodGet, "/activity?team=ALP", nil)
	knownData := svc.ActivityData(knownRequest)
	if knownData["team_unknown"] != false || knownData["team_unknown_notice"] != "" {
		t.Fatalf("team_unknown state for a real code = unknown:%v notice:%q, want false/empty", knownData["team_unknown"], knownData["team_unknown_notice"])
	}
}

func activityParityService(t *testing.T) *Service {
	t.Helper()
	svc := newTestService(t, true)
	svc.teams = []Team{
		{ID: "team-1", Name: "Alpha Aces", Abbreviation: "ALP"},
		{ID: "team-2", Name: "Beta Bears", Abbreviation: "BET"},
		{ID: "team-3", Name: "Gamma Goats", Abbreviation: "GAM"},
	}
	return svc
}

func TestActivityTradeAppearsForBothPartiesWithoutDuplicating(t *testing.T) {
	svc := activityParityService(t)
	base := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	transactions := []Transaction{
		{
			ID: "trade-1", Type: "trade", TeamID: "team-1", OtherTeamID: "team-2",
			Adds:  []TransactionPlayer{{Name: "Incoming Runner", Position: "RB"}},
			Drops: []TransactionPlayer{{Name: "Outgoing Receiver", Position: "WR"}},
			At:    base.Add(2 * time.Minute),
		},
		{
			ID: "add-1", Type: "add", TeamID: "team-1", OtherTeamID: "team-2",
			Adds: []TransactionPlayer{{Name: "Alpha Free Agent", Position: "TE"}},
			At:   base.Add(time.Minute),
		},
	}
	svc.store.state.Transactions = append(svc.store.state.Transactions, transactions...)

	request, _ := http.NewRequest(http.MethodGet, "/activity?team=BET", nil)
	data := svc.ActivityData(request)
	rows, _ := data["transactions"].([]map[string]any)
	if len(rows) != 1 {
		t.Fatalf("counterparty team filter rows = %+v, want one trade row", rows)
	}
	row := rows[0]
	if row["team"] != "Alpha Aces (ALP) ↔ Beta Bears (BET)" {
		t.Fatalf("trade team display = %v, want the team NAME with its code as a secondary label", row["team"])
	}
	teams, _ := row["teams"].([]string)
	if len(teams) != 2 || teams[0] != "ALP" || teams[1] != "BET" {
		t.Fatalf("trade teams = %#v, want [ALP BET]", teams)
	}
	names, _ := row["team_names"].([]string)
	if len(names) != 2 || names[0] != "Alpha Aces" || names[1] != "Beta Bears" {
		t.Fatalf("trade team names = %#v, want both names", names)
	}

	request, _ = http.NewRequest(http.MethodGet, "/activity?q=Beta+Bears", nil)
	data = svc.ActivityData(request)
	rows, _ = data["transactions"].([]map[string]any)
	if len(rows) != 1 || rows[0]["action"] != "gives" {
		t.Fatalf("counterparty name search rows = %+v, want the trade", rows)
	}

	request, _ = http.NewRequest(http.MethodGet, "/activity?q=BET", nil)
	data = svc.ActivityData(request)
	rows, _ = data["transactions"].([]map[string]any)
	if len(rows) != 1 || rows[0]["action"] != "gives" {
		t.Fatalf("counterparty abbreviation search rows = %+v, want the trade", rows)
	}

	for _, tc := range []struct {
		query string
		want  int
	}{
		{query: "team-1", want: 2},
		{query: "team-2", want: 1},
	} {
		request, _ = http.NewRequest(http.MethodGet, "/activity?q="+tc.query, nil)
		data = svc.ActivityData(request)
		rows, _ = data["transactions"].([]map[string]any)
		if len(rows) != tc.want {
			t.Fatalf("team ID search %q rows = %+v, want %d", tc.query, rows, tc.want)
		}
		if rows[0]["action"] != "gives" {
			t.Fatalf("team ID search %q newest row = %+v, want trade", tc.query, rows[0])
		}
	}

	request, _ = http.NewRequest(http.MethodGet, "/activity?team=ALP", nil)
	data = svc.ActivityData(request)
	rows, _ = data["transactions"].([]map[string]any)
	if len(rows) != 2 {
		t.Fatalf("initiating team filter rows = %+v, want trade plus ordinary add", rows)
	}
	tradeRows := 0
	for _, candidate := range rows {
		if candidate["action"] == "gives" {
			tradeRows++
		}
	}
	if tradeRows != 1 {
		t.Fatalf("trade rows = %d, want exactly one row", tradeRows)
	}
}

// TestActivityMapsCarriesRFC3339InstantForEveryRow checks the wave-2 audit
// fix (finding 1): every /activity row — an ordinary team move and a
// commissioner event alike — must carry a real RFC3339 "time_iso" value
// for the template's <time datetime=…> element; a page audit found every
// row's <time> with no datetime attribute at all.
func TestActivityMapsCarriesRFC3339InstantForEveryRow(t *testing.T) {
	svc := newTestService(t, true)
	base := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	state := PersistedState{
		Transactions: []Transaction{
			{ID: "txn-1", Type: "add", TeamID: "team-2", Adds: []TransactionPlayer{{Name: "FA One", Position: "RB"}}, At: base},
		},
		CommissionerEvents: []CommissionerEvent{
			{ID: "ce-1", ActorEmail: "alex@example.com", ActorName: "Alex", Kind: "announcement.post", Summary: "posted an announcement", At: base.Add(time.Hour)},
		},
	}
	rows := svc.activityMaps(state, 0)
	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2", len(rows))
	}
	for _, row := range rows {
		iso, _ := row["time_iso"].(string)
		if iso == "" {
			t.Fatalf("row %+v has no time_iso value", row)
		}
		if _, err := time.Parse(time.RFC3339, iso); err != nil {
			t.Fatalf("time_iso %q does not parse as RFC3339: %v", iso, err)
		}
	}
	if rows[0]["time_iso"] != "2026-09-01T13:00:00Z" {
		t.Fatalf("commissioner row time_iso = %v, want %q", rows[0]["time_iso"], "2026-09-01T13:00:00Z")
	}
	if rows[1]["time_iso"] != "2026-09-01T12:00:00Z" {
		t.Fatalf("team-move row time_iso = %v, want %q", rows[1]["time_iso"], "2026-09-01T12:00:00Z")
	}
}

// TestActivityMapsRendersCommissionerEventAsDistinctActorClass checks the
// wave-2 commissioner-console audit trail: a CommissionerEvent joins the
// merged feed (newest-first, alongside ordinary team moves), attributed to
// the acting PERSON — not a seat code — with a distinct "COMMISSIONER"
// actor class, a plain-language summary, and both an exact league-local
// time and a relative label.
func TestActivityMapsRendersCommissionerEventAsDistinctActorClass(t *testing.T) {
	svc := newTestService(t, true)
	base := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return base.Add(2 * time.Hour) }
	state := PersistedState{
		Transactions: []Transaction{
			{ID: "txn-1", Type: "add", TeamID: "team-2", Adds: []TransactionPlayer{{Name: "FA One", Position: "RB"}}, At: base},
		},
		CommissionerEvents: []CommissionerEvent{
			{
				ID: "ce-1", ActorEmail: "alex@example.com", ActorName: "Alex",
				Kind: "announcement.post", Summary: "posted an announcement",
				At: base.Add(time.Hour),
			},
		},
	}
	rows := svc.activityMaps(state, 0)
	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2", len(rows))
	}
	// Newest first: the commissioner event (base+1h) precedes the
	// transaction (base).
	row := rows[0]
	if row["kind"] != activityActorClassCommissioner || row["actor_class"] != "COMMISSIONER" {
		t.Fatalf("commissioner row kind/actor_class = %v/%v", row["kind"], row["actor_class"])
	}
	if row["team"] != "Alex" || row["actor_name"] != "Alex" {
		t.Fatalf("commissioner row is attributed to %v, want the person's own name", row["team"])
	}
	if row["action"] != "posted an announcement" {
		t.Fatalf("commissioner row action = %v", row["action"])
	}
	if row["is_commissioner_event"] != true {
		t.Fatalf("is_commissioner_event = %v, want true", row["is_commissioner_event"])
	}
	if row["time_relative"] != "1 hour ago" {
		t.Fatalf("commissioner row time_relative = %v, want %q", row["time_relative"], "1 hour ago")
	}

	// The ordinary team-move row is unchanged: no actor class, still a
	// team-abbreviation row.
	moveRow := rows[1]
	if moveRow["kind"] != "" || moveRow["actor_class"] != "" || moveRow["is_commissioner_event"] != false {
		t.Fatalf("ordinary move row leaked a commissioner actor class: %+v", moveRow)
	}
}

func TestActivityMapsUsesLeagueTimezoneAndZoneLabel(t *testing.T) {
	svc := activityParityService(t)
	svc.cfg.Timezone = "America/Los_Angeles"
	svc.draftTZ = nil
	at := time.Date(2026, 1, 1, 7, 30, 0, 0, time.UTC)
	state := PersistedState{
		Transactions: []Transaction{
			{
				ID: "txn-timezone", Type: "add", TeamID: "team-1",
				Adds: []TransactionPlayer{{Name: "Pacific Add", Position: "RB"}}, At: at,
			},
		},
	}
	rows := svc.activityMaps(state, 0)
	if len(rows) != 1 {
		t.Fatalf("timezone rows = %d, want 1", len(rows))
	}
	if rows[0]["time"] != "Dec 31, 11:30 PM PST" {
		t.Fatalf("activity time = %q, want configured-zone date rollover", rows[0]["time"])
	}
	if rows[0]["timezone"] != "Pacific Time" {
		t.Fatalf("row timezone = %q, want the friendly label for the configured zone", rows[0]["timezone"])
	}
	request, _ := http.NewRequest(http.MethodGet, "/activity", nil)
	data := svc.ActivityData(request)
	if data["timezone"] != "Pacific Time" {
		t.Fatalf("activity data timezone = %q, want the friendly label for the configured zone", data["timezone"])
	}
}
