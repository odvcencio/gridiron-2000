package admin

import (
	"net/http/httptest"
	"os"
	"strings"
	"testing"
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
