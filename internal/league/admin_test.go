package league

import (
	"net/http"
	"strings"
	"testing"
)

func TestInviteEmailTemplateCarriesFactsAndEmail(t *testing.T) {
	service := newTestService(t, true)
	t.Setenv("LEAGUE_URL", "")

	subject, body := service.InviteEmailTemplate("manager@example.com")

	if !strings.Contains(subject, "GRIDIRON 2000") {
		t.Errorf("subject missing league name: %q", subject)
	}
	if !strings.HasPrefix(subject, "You're invited:") {
		t.Errorf("subject missing invite lead-in: %q", subject)
	}
	for _, want := range []string{
		"Aqua", "Orange", "Dolphins", "manager@example.com",
		defaultLeagueURL, "Rules page", "— The Commissioner",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q\nbody:\n%s", want, body)
		}
	}
}

func TestInviteEmailTemplateHonorsLeagueURLEnv(t *testing.T) {
	service := newTestService(t, true)
	t.Setenv("LEAGUE_URL", "https://league.example.com")

	_, body := service.InviteEmailTemplate("manager@example.com")
	if !strings.Contains(body, "https://league.example.com") {
		t.Errorf("body did not honor LEAGUE_URL override:\n%s", body)
	}
	if strings.Contains(body, defaultLeagueURL) {
		t.Errorf("body should not fall back to the default URL when LEAGUE_URL is set:\n%s", body)
	}
}

func TestAdminSendInviteRequiresCommissioner(t *testing.T) {
	service := newTestService(t, false)
	request, _ := http.NewRequest(http.MethodGet, "/admin", nil)
	t.Setenv("COMMISSIONER_EMAILS", "boss@example.com")
	t.Setenv("SMTP_HOST", "")

	if _, err := service.AdminSendInvite(request, "x@example.com"); err == nil {
		t.Fatal("unauthenticated invite-send must fail")
	}
}

func TestAdminSendInviteWithoutSMTPAddsInviteAndReportsNotSent(t *testing.T) {
	service := newTestService(t, true) // demo mode grants commissioner
	request, _ := http.NewRequest(http.MethodGet, "/admin", nil)
	t.Setenv("SMTP_HOST", "")
	t.Setenv("SMTP_USER", "")
	t.Setenv("SMTP_PASS", "")

	sent, err := service.AdminSendInvite(request, " Manager@Example.com ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sent {
		t.Fatal("sent should be false when SMTP is not configured")
	}
	if !service.store.Invited("manager@example.com") {
		t.Fatal("invite should still be recorded without SMTP configured")
	}
}

func TestAdminSendInviteWithSMTPAttemptsSendAndReportsSent(t *testing.T) {
	service := newTestService(t, true)
	request, _ := http.NewRequest(http.MethodGet, "/admin", nil)
	// A configured-but-unreachable SMTP host lets us confirm the mailer
	// path runs (and its error surfaces) without opening a real socket.
	t.Setenv("SMTP_HOST", "127.0.0.1")
	t.Setenv("SMTP_PORT", "1")
	t.Setenv("SMTP_USER", "commish@example.com")
	t.Setenv("SMTP_PASS", "secret")

	sent, err := service.AdminSendInvite(request, "manager@example.com")
	if !sent {
		t.Fatal("sent should be true once SMTP is configured, even if delivery fails")
	}
	if err == nil {
		t.Fatal("expected a delivery error against an unreachable SMTP host")
	}
	if !service.store.Invited("manager@example.com") {
		t.Fatal("invite should be recorded even when delivery fails")
	}
}

func TestAdminDataMailFieldsAndMailto(t *testing.T) {
	service := newTestService(t, true)
	request, _ := http.NewRequest(http.MethodGet, "/admin", nil)
	t.Setenv("SMTP_HOST", "")
	t.Setenv("SMTP_USER", "")
	t.Setenv("SMTP_PASS", "")

	if err := service.AdminAddInvite(request, "manager@example.com"); err != nil {
		t.Fatal(err)
	}
	data := service.AdminData(request)

	if data["mail_enabled"] != false {
		t.Errorf("mail_enabled = %v, want false without SMTP env", data["mail_enabled"])
	}
	preview, ok := data["invite_preview"].(map[string]any)
	if !ok {
		t.Fatalf("invite_preview missing or wrong type: %#v", data["invite_preview"])
	}
	if subject, _ := preview["subject"].(string); !strings.Contains(subject, "GRIDIRON 2000") {
		t.Errorf("invite_preview subject wrong: %q", subject)
	}
	if body, _ := preview["body"].(string); !strings.Contains(body, "their-email@example.com") {
		t.Errorf("invite_preview body should address the sample email: %q", body)
	}

	invites, _ := data["invites"].([]map[string]any)
	found := false
	for _, invite := range invites {
		if invite["email"] != "manager@example.com" {
			continue
		}
		found = true
		mailto, _ := invite["mailto"].(string)
		if !strings.HasPrefix(mailto, "mailto:manager@example.com?subject=") {
			t.Errorf("mailto malformed: %q", mailto)
		}
		if !strings.Contains(mailto, "&body=") {
			t.Errorf("mailto missing body param: %q", mailto)
		}
	}
	if !found {
		t.Fatalf("invite missing from admin data: %+v", invites)
	}
}

func TestAdminDataMailEnabledTrueWithSMTP(t *testing.T) {
	service := newTestService(t, true)
	request, _ := http.NewRequest(http.MethodGet, "/admin", nil)
	t.Setenv("SMTP_HOST", "smtp.example.com")
	t.Setenv("SMTP_USER", "commish@example.com")
	t.Setenv("SMTP_PASS", "secret")

	data := service.AdminData(request)
	if data["mail_enabled"] != true {
		t.Errorf("mail_enabled = %v, want true with SMTP env set", data["mail_enabled"])
	}
}
