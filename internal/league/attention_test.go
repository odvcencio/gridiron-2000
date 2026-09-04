package league

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"m31labs.dev/gosx/auth"
)

// signedInPickemRequest returns a *http.Request already carrying an
// authenticated auth.User for email, the same recipe trades_test.go's
// tradeActionAs uses — auth.Current's context key is unexported, so a
// request must pass through a real auth.Manager's Middleware to carry one.
func signedInPickemRequest(email string) *http.Request {
	authn := auth.New(nil, auth.Options{
		Provider: auth.ProviderFunc(func(*http.Request) (auth.User, bool) {
			return auth.User{ID: email, Email: email, Name: email}, true
		}),
	})
	var captured *http.Request
	handler := authn.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	return captured
}

// TestAttentionMapDerivesFromOpenAndAcceptedTradesAndOpenPickemGames is
// gap-audit item 6's data contract, updated by item 5(a)/(b) (2026-08-31
// post-wave audit): leagueMap's "attention" key must list an OPEN
// (awaiting a response) trade offer, an ACCEPTED (review-pending) trade,
// and this week's still-open pick'em games — the same underlying facts
// that put those tasks on the home Action Center (tradeActions/
// pickemActions, hq.go), so the shared chrome chip and the home page
// never silently disagree. Before item 5(b)'s fix, an open offer never
// appeared here even though it already drove the Action Center's own
// "Review incoming trade" task — the field report's "2 vs 3" chip/panel
// mismatch.
func TestAttentionMapDerivesFromOpenAndAcceptedTradesAndOpenPickemGames(t *testing.T) {
	svc := newTestService(t, false)
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	teamIDs := defaultTeamIDs()
	if len(teamIDs) < 2 {
		t.Fatal("fixture league needs at least two teams")
	}
	fromID, toID := teamIDs[0], teamIDs[1]
	svc.SetScheduleSource(func() []GameInfo {
		return []GameInfo{
			{ID: "game-open", Week: 2, Kickoff: now.Add(2 * time.Hour), Away: "MIA", Home: "BUF"},
			{ID: "game-kicked", Week: 2, Kickoff: now.Add(-2 * time.Hour), Away: "NYJ", Home: "NE"},
		}
	})
	state := PersistedState{
		TradeOffers: []TradeOffer{
			{ID: "trd-open", FromTeamID: fromID, ToTeamID: toID, Status: TradeStatusOpen},
			{ID: "trd-review", FromTeamID: fromID, ToTeamID: toID, Status: TradeStatusAccepted},
		},
	}

	attention := svc.attentionMap(state, now)
	items, ok := attention["items"].([]map[string]any)
	if !ok {
		t.Fatalf("attention[items] = %#v, want []map[string]any", attention["items"])
	}
	if got := attention["urgent_count"]; got != len(items) {
		t.Fatalf("urgent_count = %v, want len(items) = %d", got, len(items))
	}
	if attention["has_items"] != true {
		t.Fatalf("has_items = %v, want true", attention["has_items"])
	}
	if len(items) != 3 {
		t.Fatalf("items = %#v, want exactly 3 (one open trade, one review trade, one open-pickem summary)", items)
	}

	fromName, toName := svc.teamByID(fromID).Name, svc.teamByID(toID).Name
	var sawOpenTrade, sawReviewTrade, sawPickem bool
	for _, item := range items {
		route, _ := item["route"].(string)
		label, _ := item["label"].(string)
		switch route {
		case "/trades#trade-trd-review":
			sawReviewTrade = true
			if !strings.Contains(label, fromName) || !strings.Contains(label, toName) {
				t.Errorf("review trade item label %q does not name both %q and %q", label, fromName, toName)
			}
		case "/trades#trade-trd-open":
			sawOpenTrade = true
			if !strings.Contains(label, fromName) || !strings.Contains(label, toName) {
				t.Errorf("open trade item label %q does not name both %q and %q", label, fromName, toName)
			}
		case "/pickem":
			sawPickem = true
			if !strings.Contains(label, "1 open pick'em game") {
				t.Errorf("pickem item label = %q, want a 1-open-game count", label)
			}
		default:
			t.Errorf("unexpected attention item: %#v", item)
		}
	}
	if !sawOpenTrade {
		t.Error("attention items missing the open-trade-offer entry")
	}
	if !sawReviewTrade {
		t.Error("attention items missing the accepted-trade-in-review entry")
	}
	if !sawPickem {
		t.Error("attention items missing the open-pickem-games entry")
	}

	// pickem_hot/trades_hot and their *_attention_text counterparts (build
	// item 2, rail-dot leftover) are pre-shaped scalars app/layout.gsx's
	// legacy PrimaryNavigation component reads directly — no route-prefix
	// filter in the template. Trades carries two items now (open + review).
	if attention["pickem_hot"] != true {
		t.Fatalf("pickem_hot = %v, want true", attention["pickem_hot"])
	}
	if attention["pickem_attention_text"] != "1 item needs attention" {
		t.Fatalf("pickem_attention_text = %q, want %q", attention["pickem_attention_text"], "1 item needs attention")
	}
	if attention["trades_hot"] != true {
		t.Fatalf("trades_hot = %v, want true", attention["trades_hot"])
	}
	if want := "2 items need attention"; attention["trades_attention_text"] != want {
		t.Fatalf("trades_attention_text = %q, want %q", attention["trades_attention_text"], want)
	}
	if want := "3 items need attention in the Action Center"; attention["chip_label"] != want {
		t.Fatalf("chip_label = %q, want %q", attention["chip_label"], want)
	}
}

// TestAttentionMapTradesHotPluralizesAcrossMultipleAcceptedOffers proves
// trades_attention_text agrees in both noun and verb once more than one
// team has an accepted trade awaiting review — Plural alone only handles
// the noun, so attentionDotText must carry the verb too.
func TestAttentionMapTradesHotPluralizesAcrossMultipleAcceptedOffers(t *testing.T) {
	svc := newTestService(t, false)
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	teamIDs := defaultTeamIDs()
	if len(teamIDs) < 4 {
		t.Fatal("fixture league needs at least four teams")
	}
	svc.SetScheduleSource(func() []GameInfo { return nil })
	state := PersistedState{
		TradeOffers: []TradeOffer{
			{ID: "trd-a", FromTeamID: teamIDs[0], ToTeamID: teamIDs[1], Status: TradeStatusAccepted},
			{ID: "trd-b", FromTeamID: teamIDs[2], ToTeamID: teamIDs[3], Status: TradeStatusAccepted},
		},
	}

	attention := svc.attentionMap(state, now)
	if attention["trades_hot"] != true {
		t.Fatalf("trades_hot = %v, want true", attention["trades_hot"])
	}
	if want := "2 items need attention"; attention["trades_attention_text"] != want {
		t.Fatalf("trades_attention_text = %q, want %q", attention["trades_attention_text"], want)
	}
	// No open pick'em games in this fixture: the pickem dot must stay off.
	if attention["pickem_hot"] != false {
		t.Fatalf("pickem_hot = %v, want false", attention["pickem_hot"])
	}
	if attention["pickem_attention_text"] != "" {
		t.Fatalf("pickem_attention_text = %q, want empty", attention["pickem_attention_text"])
	}
}

// TestAttentionMapEmptyWhenNoUrgentFacts is the honest-empty-state
// counterpart: no accepted trades and no schedule at all must render zero
// items, not a placeholder or a stale count.
func TestAttentionMapEmptyWhenNoUrgentFacts(t *testing.T) {
	svc := newTestService(t, false)
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	svc.SetScheduleSource(func() []GameInfo { return nil })

	attention := svc.attentionMap(PersistedState{}, now)
	items, ok := attention["items"].([]map[string]any)
	if !ok {
		t.Fatalf("attention[items] = %#v, want []map[string]any", attention["items"])
	}
	if len(items) != 0 {
		t.Fatalf("items = %#v, want empty", items)
	}
	if attention["urgent_count"] != 0 {
		t.Fatalf("urgent_count = %v, want 0", attention["urgent_count"])
	}
	if attention["has_items"] != false {
		t.Fatalf("has_items = %v, want false", attention["has_items"])
	}
	if attention["pickem_hot"] != false || attention["trades_hot"] != false {
		t.Fatalf("hot flags = pickem:%v trades:%v, want both false", attention["pickem_hot"], attention["trades_hot"])
	}
	if attention["pickem_attention_text"] != "" || attention["trades_attention_text"] != "" {
		t.Fatalf("attention text = pickem:%q trades:%q, want both empty", attention["pickem_attention_text"], attention["trades_attention_text"])
	}
	if attention["chip_label"] != "" {
		t.Fatalf("chip_label = %q, want empty when there are no items", attention["chip_label"])
	}
}

// TestAttentionChipLabelPluralizesCorrectly is item 5(c)'s own regression
// test (2026-08-31 post-wave audit): app/layout.gsx used to build its
// rail-head/mobile-bar chip aria-label by concatenating
// data.league.attention.urgent_count + " items need attention in the
// Action Center" directly — always plural, so a single item read "1 items
// need attention...". attention.chip_label now carries the whole,
// correctly pluralized string; hickory (app/layout.gsx) reads this key
// instead of concatenating.
func TestAttentionChipLabelPluralizesCorrectly(t *testing.T) {
	svc := newTestService(t, false)
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	teamIDs := defaultTeamIDs()
	if len(teamIDs) < 2 {
		t.Fatal("fixture league needs at least two teams")
	}
	svc.SetScheduleSource(func() []GameInfo { return nil })

	// One item: singular "item"/"needs".
	single := svc.attentionMap(PersistedState{
		TradeOffers: []TradeOffer{{ID: "trd-a", FromTeamID: teamIDs[0], ToTeamID: teamIDs[1], Status: TradeStatusAccepted}},
	}, now)
	if want := "1 item needs attention in the Action Center"; single["chip_label"] != want {
		t.Fatalf("chip_label (1 item) = %q, want %q", single["chip_label"], want)
	}

	// Two items: plural "items"/"need".
	double := svc.attentionMap(PersistedState{
		TradeOffers: []TradeOffer{
			{ID: "trd-a", FromTeamID: teamIDs[0], ToTeamID: teamIDs[1], Status: TradeStatusAccepted},
			{ID: "trd-b", FromTeamID: teamIDs[0], ToTeamID: teamIDs[1], Status: TradeStatusOpen},
		},
	}, now)
	if want := "2 items need attention in the Action Center"; double["chip_label"] != want {
		t.Fatalf("chip_label (2 items) = %q, want %q", double["chip_label"], want)
	}
}

// TestLeagueMapEmbedsAttention pins leagueMap's own composition: every
// route's data function already includes "league": s.leagueMap() (the
// pattern latest_announcement relies on above it in service.go), so
// nesting attention there — rather than threading a *http.Request through
// leagueMap's 19 call sites across 10 other files — is what actually gets
// it to every route's shared layout chrome.
func TestLeagueMapEmbedsAttention(t *testing.T) {
	svc := newTestService(t, false)
	got := svc.leagueMap()
	attention, ok := got["attention"].(map[string]any)
	if !ok {
		t.Fatalf("leagueMap()[attention] = %#v, want map[string]any", got["attention"])
	}
	for _, key := range []string{
		"urgent_count", "items", "has_items", "chip_label",
		"pickem_hot", "pickem_attention_text", "trades_hot", "trades_attention_text",
	} {
		if _, ok := attention[key]; !ok {
			t.Errorf("leagueMap()[attention] missing key %q: %#v", key, attention)
		}
	}
}

// TestLeagueMapForViewerSuppressesAttentionForAnonymousDemoViewer is item
// 5(d)'s own regression test (2026-08-31 post-wave audit): Viewer's own
// "has_seat": s.demoMode (service.go) lets any unauthenticated visitor to
// a demo-mode league act as team-1 for interactive purposes, and
// app/layout.gsx gates the attention chip on has_seat — so a pure,
// unauthenticated demo spectator (managing nothing) used to see the chip
// anyway, naming trades and pick'em games that belong to the real seated
// managers. leagueMapForViewer must read attention as the honest empty
// shape for that one case, and must NOT touch it for a real (non-demo)
// league or for a genuinely signed-in viewer even in demo mode.
func TestLeagueMapForViewerSuppressesAttentionForAnonymousDemoViewer(t *testing.T) {
	teamIDs := defaultTeamIDs()
	if len(teamIDs) < 2 {
		t.Fatal("fixture league needs at least two teams")
	}
	urgentState := func(svc *Service) {
		svc.SetScheduleSource(func() []GameInfo { return nil })
		svc.store.state.TradeOffers = []TradeOffer{
			{ID: "trd-demo", FromTeamID: teamIDs[0], ToTeamID: teamIDs[1], Status: TradeStatusAccepted},
		}
	}

	demo := newTestService(t, true)
	urgentState(demo)
	anonymous := httptest.NewRequest("GET", "/", nil)
	got := demo.leagueMapForViewer(anonymous)
	attention, ok := got["attention"].(map[string]any)
	if !ok {
		t.Fatalf("leagueMapForViewer(anonymous demo)[attention] = %#v, want map[string]any", got["attention"])
	}
	if attention["has_items"] != false || attention["urgent_count"] != 0 {
		t.Fatalf("anonymous demo attention = %+v, want the honest empty shape", attention)
	}

	real := newTestService(t, false)
	urgentState(real)
	realReq := httptest.NewRequest("GET", "/", nil)
	realGot := real.leagueMapForViewer(realReq)
	realAttention, ok := realGot["attention"].(map[string]any)
	if !ok {
		t.Fatalf("leagueMapForViewer(non-demo)[attention] = %#v, want map[string]any", realGot["attention"])
	}
	if realAttention["has_items"] != true || realAttention["urgent_count"] != 1 {
		t.Fatalf("non-demo attention = %+v, want the real accepted-trade item (has_seat=false already hides the chip in the chrome, not here)", realAttention)
	}
}

// TestLeagueMapForViewerPickemItemTracksTheAdmittedViewersOwnUnpickedGames
// is item 2's own regression test (2026-09-02 audit): attentionMap's
// pick'em item used to count every game still open this week league-wide,
// so a manager who had already called all of them still saw "1 URGENT"
// for the eleven days those games stayed open — the chip and the home
// Action Center (already correctly viewer-scoped) silently disagreed.
// leagueMapForViewer now re-scopes the item to the admitted viewer's own
// unpicked count.
func TestLeagueMapForViewerPickemItemTracksTheAdmittedViewersOwnUnpickedGames(t *testing.T) {
	svc := newTestService(t, false)
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	svc.SetClockForTest(func() time.Time { return now })
	const email = "viewer@example.com"
	if _, _, err := svc.store.AssignMember(email, "Viewer"); err != nil {
		t.Fatalf("AssignMember(%s): %v", email, err)
	}
	games := []GameInfo{
		{ID: "g1", Week: 1, Kickoff: now.Add(2 * time.Hour), Away: "MIA", Home: "BUF", SpreadLinePresent: true},
		{ID: "g2", Week: 1, Kickoff: now.Add(3 * time.Hour), Away: "NYJ", Home: "NE", SpreadLinePresent: true},
		{ID: "g3", Week: 1, Kickoff: now.Add(4 * time.Hour), Away: "KC", Home: "DEN", SpreadLinePresent: true},
	}
	svc.SetScheduleSource(func() []GameInfo { return games })
	req := signedInPickemRequest(email)

	// Three open games, none picked yet: the chip must name the viewer's
	// own count in the "N pick'em games to call" phrasing item 2 specifies.
	before := svc.leagueMapForViewer(req)
	beforeAttention, ok := before["attention"].(map[string]any)
	if !ok {
		t.Fatalf("leagueMapForViewer(3 unpicked)[attention] = %#v, want map[string]any", before["attention"])
	}
	beforeItems, _ := beforeAttention["items"].([]map[string]any)
	var sawPickem bool
	for _, item := range beforeItems {
		if item["route"] != "/pickem" {
			continue
		}
		sawPickem = true
		if want := "3 pick'em games to call"; item["label"] != want {
			t.Errorf("pickem item label = %q, want %q", item["label"], want)
		}
	}
	if !sawPickem {
		t.Fatalf("attention items = %#v, want a pick'em item for a viewer with 3 unpicked open games", beforeItems)
	}

	// The same viewer calls every open game: the item must disappear, not
	// linger at a stale league-wide "still open" count.
	for _, game := range games {
		if err := svc.store.SetPickem(email, game.ID, "BUF", now.Add(-time.Hour)); err != nil {
			t.Fatalf("SetPickem(%s): %v", game.ID, err)
		}
	}
	after := svc.leagueMapForViewer(req)
	afterAttention, ok := after["attention"].(map[string]any)
	if !ok {
		t.Fatalf("leagueMapForViewer(all picked)[attention] = %#v, want map[string]any", after["attention"])
	}
	afterItems, _ := afterAttention["items"].([]map[string]any)
	for _, item := range afterItems {
		if item["route"] == "/pickem" {
			t.Fatalf("attention items = %#v, want no pick'em item once every open game is picked", afterItems)
		}
	}
	if afterAttention["pickem_hot"] != false {
		t.Fatalf("pickem_hot = %v, want false once every open game is picked", afterAttention["pickem_hot"])
	}
}

// TestAttentionChipAndActionCenterAgreeOnOneOpenIncomingTrade pins wave-P
// finding J3 F7 (chip part): with exactly one open incoming trade offer,
// the shared chrome's attention chip must count 1, and the home Action
// Center's own trade-inbox card must carry Urgent — before this fix the
// card stayed Priority Stable/Urgent false while the two locked-out
// Pick'em cards next to it both carried the URGENT badge, so a manager
// reading the page could not tell the chip's count included a live trade
// at all.
func TestAttentionChipAndActionCenterAgreeOnOneOpenIncomingTrade(t *testing.T) {
	svc := newTestService(t, false)
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	teamIDs := defaultTeamIDs()
	if len(teamIDs) < 2 {
		t.Fatal("fixture league needs at least two teams")
	}
	fromID, toID := teamIDs[0], teamIDs[1]
	const email = "viewer@example.com"

	state := svc.store.Snapshot()
	state.Members[email] = Member{Email: email, Name: "Viewer", TeamID: toID}
	state.TradeOffers = []TradeOffer{{ID: "trd-open", FromTeamID: fromID, ToTeamID: toID, Status: TradeStatusOpen}}

	attention := svc.attentionMap(state, now)
	if got := attention["urgent_count"]; got != 1 {
		t.Fatalf("urgent_count = %v, want 1 for one open incoming trade offer", got)
	}

	viewer := map[string]any{"signed_in": true, "email": email, "has_seat": true, "team_id": toID}
	request, err := http.NewRequest(http.MethodGet, "/", nil)
	if err != nil {
		t.Fatal(err)
	}
	data := svc.actionCenterDataForSnapshot(request, state, viewer, map[string]any{}, now)
	actions, _ := data["actions"].([]map[string]any)
	var inbox map[string]any
	for _, action := range actions {
		if action["id"] == "trade-inbox" {
			inbox = action
		}
	}
	if inbox == nil {
		t.Fatalf("actions = %#v, want a trade-inbox card for one open incoming offer", actions)
	}
	if inbox["urgent"] != true {
		t.Fatalf("trade-inbox urgent = %v, want true so the card agrees with the chip", inbox["urgent"])
	}
}
