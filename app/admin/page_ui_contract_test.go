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
