package league

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"m31labs.dev/gosx/auth"
)

func authenticatedLineupRequest(t *testing.T, email, name, path string) *http.Request {
	t.Helper()
	authn := auth.New(nil, auth.Options{Provider: auth.ProviderFunc(func(*http.Request) (auth.User, bool) {
		return auth.User{ID: email, Email: email, Name: name}, true
	})})
	var authenticated *http.Request
	authn.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authenticated = r
		w.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))
	if authenticated == nil {
		t.Fatalf("authenticate %s: middleware did not forward the request", email)
	}
	return authenticated
}

func claimedLineupViewService(t *testing.T) (*Service, *http.Request, *http.Request, *http.Request) {
	t.Helper()
	service := newTestService(t, false)
	if _, err := service.EnsureMember("commissioner-lineup@example.com", "Commissioner"); err != nil {
		t.Fatal(err)
	}
	primary, err := service.AssignManager("lineup-primary@example.com", "Primary")
	if err != nil || primary.TeamID != "team-1" {
		t.Fatalf("primary seat = %+v, %v", primary, err)
	}
	secondary, err := service.AssignManager("lineup-secondary@example.com", "Secondary")
	if err != nil || secondary.TeamID != "team-2" {
		t.Fatalf("secondary seat = %+v, %v", secondary, err)
	}
	if _, err := service.AssignManager("commissioner-lineup@example.com", "Commissioner"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("COMMISSIONER_EMAILS", "commissioner-lineup@example.com")
	commissioner := authenticatedLineupRequest(t, "commissioner-lineup@example.com", "Commissioner", "/team?team=team-2&week=2")
	manager := authenticatedLineupRequest(t, "lineup-primary@example.com", "Primary", "/team?team=team-2&week=2")
	seatlessCommissioner := authenticatedLineupRequest(t, "commissioner-lineup@example.com", "Commissioner", "/team?team=team-2&week=2")
	return service, commissioner, manager, seatlessCommissioner
}

func TestCommissionerLineupViewTargetIsClaimedAndScoped(t *testing.T) {
	service, commissioner, manager, _ := claimedLineupViewService(t)

	data := service.TeamData(commissioner)
	if data["lineup_intervention"] != true || data["lineup_target_id"] != "team-2" || data["has_seat"] != true {
		t.Fatalf("commissioner target projection = %#v", data)
	}
	team, ok := data["team"].(map[string]any)
	if !ok || team["id"] != "team-2" {
		t.Fatalf("commissioner target team = %#v", data["team"])
	}
	if team["manager"] != "" {
		t.Fatalf("commissioner intervention leaked target manager: %#v", team)
	}
	coManager, ok := data["co_manager"].(map[string]any)
	if !ok || coManager["primary_name"] != "" || coManager["co_name"] != "" || coManager["can_invite"] == true || coManager["can_detach"] == true {
		t.Fatalf("commissioner intervention leaked co-manager controls: %#v", data["co_manager"])
	}
	if badges, ok := data["badge_grid"].([]map[string]any); !ok || len(badges) != 0 {
		t.Fatalf("commissioner intervention leaked badge controls: %#v", data["badge_grid"])
	}
	if data["predraft_visible"] != false {
		t.Fatal("commissioner intervention must not expose the target's setup checklist")
	}

	managerData := service.TeamData(manager)
	if managerData["lineup_intervention"] == true || managerData["lineup_target_id"] != "team-1" {
		t.Fatalf("ordinary manager target projection = %#v", managerData)
	}
	managerTeam, ok := managerData["team"].(map[string]any)
	if !ok || managerTeam["id"] != "team-1" {
		t.Fatalf("ordinary manager escaped own seat = %#v", managerData["team"])
	}

	for _, target := range []string{"team-4", "unknown-team", "team-2%26evil"} {
		request := authenticatedLineupRequest(t, "commissioner-lineup@example.com", "Commissioner", "/team?team="+target+"&week=2")
		fallback := service.TeamData(request)
		if fallback["lineup_intervention"] == true || fallback["lineup_target_id"] != "team-3" {
			t.Fatalf("rejected commissioner target %q leaked into projection: %#v", target, fallback)
		}
	}
}

func TestSeatlessCommissionerMayEnterClaimedLineupIntervention(t *testing.T) {
	service := newTestService(t, false)
	if _, err := service.AssignManager("lineup-primary-seatless@example.com", "Primary"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.AssignManager("lineup-secondary-seatless@example.com", "Secondary"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.EnsureMember("seatless-lineup-commissioner@example.com", "Commissioner"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("COMMISSIONER_EMAILS", "seatless-lineup-commissioner@example.com")
	seatlessCommissioner := authenticatedLineupRequest(t, "seatless-lineup-commissioner@example.com", "Commissioner", "/team?team=team-2&week=2")
	data := service.TeamData(seatlessCommissioner)
	if data["lineup_intervention"] != true || data["lineup_target_id"] != "team-2" || data["has_seat"] != true {
		t.Fatalf("seatless commissioner target projection = %#v", data)
	}
}

func TestCommissionerLineupTargetAuthorizationAndWriteScope(t *testing.T) {
	service := newTestService(t, false)
	now := time.Date(2026, 8, 23, 18, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	service.SetScheduleSource(func() []GameInfo {
		return []GameInfo{{ID: "lineup-open", Week: 1, Kickoff: now.Add(time.Hour), Away: "PIT", Home: "NYJ"}}
	})
	service.SetPlayerSource(func() ([]Player, int64, string) {
		return []Player{{ID: "rb-open", Name: "Open Runner", Position: "RB", NFLTeam: "PIT", Projection: 10}}, 1, "test"
	})
	if _, err := service.AssignManager("lineup-owner@example.com", "Owner"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.AssignManager("lineup-other@example.com", "Other"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.EnsureMember("lineup-commissioner@example.com", "Commissioner"); err != nil {
		t.Fatal(err)
	}
	// The second pick belongs to team-2 in the default snake order.
	if _, err := service.store.MakePick("team-1", "filler", "manager", now, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.store.MakePick("team-2", "rb-open", "manager", now, time.Time{}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("COMMISSIONER_EMAILS", "lineup-commissioner@example.com")
	commissioner := authenticatedLineupRequest(t, "lineup-commissioner@example.com", "Commissioner", "/team?team=team-2&week=1")
	ordinary := authenticatedLineupRequest(t, "lineup-owner@example.com", "Owner", "/team?team=team-2&week=1")

	if !service.LineupTargetAllowed(commissioner, "team-2") {
		t.Fatal("commissioner should be allowed to return to claimed team-2")
	}
	if service.LineupTargetAllowed(ordinary, "team-2") {
		t.Fatal("ordinary manager must not validate a cross-seat target")
	}
	if service.LineupTargetAllowed(commissioner, "team-3") {
		t.Fatal("unclaimed commissioner target must not validate")
	}
	if service.LineupTargetAllowed(commissioner, "unknown-team") {
		t.Fatal("unknown commissioner target must not validate")
	}

	if _, err := service.SetLineup(commissioner, "team-2", 1, "RB1", "rb-open"); err != nil {
		t.Fatalf("commissioner lineup intervention: %v", err)
	}
	state := service.store.Snapshot()
	if state.Lineups["team-2"][1]["RB1"] != "rb-open" {
		t.Fatalf("commissioner lineup write = %#v", state.Lineups)
	}
	before := state.Lineups
	if _, err := service.SetLineup(ordinary, "team-2", 1, "RB1", "rb-open"); err == nil || !strings.Contains(err.Error(), "not on your roster") {
		t.Fatalf("ordinary cross-seat action error = %v", err)
	}
	after := service.store.Snapshot()
	if len(after.Lineups["team-1"]) != 0 || after.Lineups["team-2"][1]["RB1"] != "rb-open" {
		t.Fatalf("ordinary action broadened lineup scope: before=%#v after=%#v", before, after.Lineups)
	}
	if _, err := service.SetLineup(commissioner, "team-3", 1, "RB1", "rb-open"); err == nil {
		t.Fatal("commissioner must not write an unclaimed franchise")
	}
	if _, err := service.LineupAuto(commissioner, "unknown-team", 1); err == nil {
		t.Fatal("commissioner must not auto-set an unknown franchise")
	}
}
