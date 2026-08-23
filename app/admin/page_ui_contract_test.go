package admin

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"gridiron-2000/internal/commissionerhq"
)

func TestStableAdminSectionsAndAllowlistedFocus(t *testing.T) {
	source, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	page := string(source)
	for _, key := range []string{"draft-control", "schedule", "week-close", "seats", "invites", "draft-order", "data", "clock", "roster", "announcements", "danger"} {
		if !strings.Contains(page, "id="+string(rune(34))+"admin-"+key+string(rune(34))) {
			t.Errorf("admin section %q has no stable id", key)
		}
		if !strings.Contains(page, "data-admin-section="+string(rune(34))+key+string(rune(34))) {
			t.Errorf("admin section %q has no stable data key", key)
		}
	}
	for _, raw := range []string{"/admin?section=data", "/admin?section=data#admin-data", "/admin?section=../../"} {
		request := httptest.NewRequest("GET", raw, nil)
		got := adminSection(request)
		if raw == "/admin?section=data" && got != "data" {
			t.Fatalf("allowlisted section = %q", got)
		}
		if strings.Contains(raw, "..") && got != "" {
			t.Fatalf("hostile section accepted: %q", got)
		}
	}
	if adminSectionClass("data", "data") != " admin-section--focused" || adminSectionClass("data", "clock") != "" {
		t.Fatal("section focus class is not exact")
	}
}

func TestCommissionerLeagueSwitcherIsAllowlistedAndPreservesAdminSection(t *testing.T) {
	publicURL, _ := url.Parse("https://sk.example")
	service, err := commissionerhq.New(commissionerhq.Config{
		InstanceID: "g2k", Timeout: time.Second,
		Peers: []commissionerhq.Peer{{ID: "skl", PublicURL: publicURL}},
	}, func() commissionerhq.Summary {
		return commissionerhq.Summary{Instance: commissionerhq.Instance{
			ID: "g2k", Name: "GRIDIRON 2000", PublicURL: "https://gridiron.example",
		}}
	})
	if err != nil {
		t.Fatal(err)
	}

	options, visible := adminLeagueSwitcherData(service, true)
	if !visible || len(options) != 2 || options[0]["current"] != true || options[1]["id"] != "skl" {
		t.Fatalf("switcher options = %#v, visible=%v", options, visible)
	}
	if options, visible := adminLeagueSwitcherData(service, false); visible || len(options) != 0 {
		t.Fatalf("noncommissioner received switcher topology: %#v", options)
	}

	handler := switchHandler(service, func(*http.Request) bool { return true })
	request := httptest.NewRequest(http.MethodGet, "/commissioner/switch?league=skl&section=data", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "https://sk.example/admin?section=data#admin-data" {
		t.Fatalf("switch response = %d %q", response.Code, response.Header().Get("Location"))
	}

	for _, target := range []string{"https://attacker.example", "skl%0d%0aLocation:%20https://attacker.example"} {
		request := httptest.NewRequest(http.MethodGet, "/commissioner/switch?league="+target, nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest || response.Header().Get("Location") != "" {
			t.Fatalf("hostile target %q = %d %q", target, response.Code, response.Header().Get("Location"))
		}
	}

	request = httptest.NewRequest(http.MethodGet, "/commissioner/switch?league=skl&section=../../danger", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "https://sk.example/admin" {
		t.Fatalf("hostile section was preserved: %d %q", response.Code, response.Header().Get("Location"))
	}

	denied := switchHandler(service, func(*http.Request) bool { return false })
	response = httptest.NewRecorder()
	denied.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/commissioner/switch?league=skl", nil))
	if response.Code != http.StatusForbidden || response.Header().Get("Location") != "" {
		t.Fatalf("noncommissioner switch = %d %q", response.Code, response.Header().Get("Location"))
	}
}

func TestAdminTaskNavigationGroupsAndRoutineOrder(t *testing.T) {
	source, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	markup := string(source)
	quote := string(rune(34))
	navStart := strings.Index(markup, "class="+quote+"admin-task-nav")
	navEnd := strings.Index(markup[navStart:], "</nav>")
	if navStart < 0 || navEnd < 0 {
		t.Fatal("admin task navigation is missing")
	}
	nav := markup[navStart : navStart+navEnd]
	for _, group := range []string{
		"Draft preparation and live operation",
		"Season operation",
		"People and access",
		"League configuration and communication",
		"Danger Zone",
	} {
		if !strings.Contains(nav, group) {
			t.Errorf("task navigation missing group %q", group)
		}
	}
	for _, target := range []string{
		"section=draft-control#admin-draft-control",
		"section=draft-order#admin-draft-order",
		"section=data#admin-data",
		"section=clock#admin-clock",
		"section=roster#admin-roster",
		"section=schedule#admin-schedule",
		"section=week-close#admin-week-close",
		"section=seats#admin-seats",
		"section=invites#admin-invites",
		"section=announcements#admin-announcements",
		"section=danger#admin-danger",
	} {
		if !strings.Contains(nav, target) {
			t.Errorf("task navigation missing target %q", target)
		}
	}

	sectionIndex := func(id string) int {
		return strings.Index(markup, "<section id="+quote+id+quote)
	}
	danger := sectionIndex("admin-danger")
	if danger < 0 {
		t.Fatal("danger section is missing")
	}
	for _, id := range []string{
		"admin-draft-control", "admin-schedule", "admin-week-close",
		"admin-seats", "admin-invites", "admin-draft-order", "admin-data",
		"admin-clock", "admin-roster", "admin-announcements",
	} {
		if index := sectionIndex(id); index < 0 || index > danger {
			t.Errorf("routine section %q does not precede Danger Zone: %d vs %d", id, index, danger)
		}
	}
	if strings.Contains(markup, "03 // RESET") {
		t.Fatal("destructive section retained the old routine-looking 03 // RESET label")
	}
	if !strings.Contains(markup, "99 // DANGER ZONE") {
		t.Fatal("destructive section lacks isolated 99 // DANGER ZONE label")
	}

	for _, id := range []string{
		"admin-draft-control", "admin-schedule", "admin-week-close",
		"admin-seats", "admin-invites", "admin-draft-order", "admin-data",
		"admin-clock", "admin-roster", "admin-announcements", "admin-danger",
	} {
		section := "id=" + quote + id + quote + " aria-labelledby=" + quote + id + "-heading" + quote + " tabindex=" + quote + "-1" + quote
		if !strings.Contains(markup, section) {
			t.Errorf("section %q lacks a focusable labelled landmark", id)
		}
		if !strings.Contains(markup, "id="+quote+id+"-heading"+quote) {
			t.Errorf("section %q lacks its labelled heading", id)
		}
	}

	for _, action := range []string{
		"draft-start", "schedule-generate", "schedule-regenerate",
		"close-week-ready", "close-week-force", "seat-release", "team-rename",
		"clock-set-autopick", "avatar-reset", "co-detach", "invite-add",
		"invite-send", "invite-remove", "draft-reset", "draft-undo", "league-reset",
		"seat-trim", "order-randomize", "clock-pause", "clock-resume",
		"clock-force-autopick", "clock-extend", "clock-set-duration",
		"roster-shape-apply", "roster-shape-reset", "announcement-post",
		"announcement-delete",
	} {
		if !strings.Contains(markup, "actionPath("+quote+action+quote+")") {
			t.Errorf("existing admin action disappeared: %s", action)
		}
	}
}

func TestAdminTaskNavigationRendersAccessibleLinks(t *testing.T) {
	body := renderAdminPage(t)
	quote := string(rune(34))
	if !strings.Contains(body, "class="+quote+"admin-task-nav") {
		t.Fatal("rendered admin page omitted task navigation")
	}
	for _, target := range []string{
		"section=draft-control#admin-draft-control",
		"section=seats#admin-seats",
		"section=danger#admin-danger",
	} {
		if !strings.Contains(body, target) {
			t.Errorf("rendered task navigation missing %s", target)
		}
	}
	if !strings.Contains(body, "aria-labelledby="+quote+"admin-danger-heading"+quote) ||
		!strings.Contains(body, "tabindex="+quote+"-1"+quote) {
		t.Fatal("rendered admin sections lost accessible focus metadata")
	}
}

func TestCommissionerLeagueSwitcherMarkupAndRouteContract(t *testing.T) {
	page, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	markup := string(page)
	for _, want := range []string{
		`action="/commissioner/switch"`, `name="league"`, `name="section"`,
		`selected={league.current}`, `href="/commissioner"`,
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("league switcher markup missing %q", want)
		}
	}
	mainSource, err := os.ReadFile("../../main.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(mainSource), `app.Mount("GET /commissioner/switch", adminpage.SwitchHandler(hqService))`) {
		t.Fatal("commissioner league switch route is not mounted")
	}
}
