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
