package league

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestInviteEmailTemplateCarriesFactsAndEmail(t *testing.T) {
	service := newTestService(t, true)
	t.Setenv("LEAGUE_URL", "")

	subject, text, _ := service.InviteEmailTemplate("manager@example.com")

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
		if !strings.Contains(text, want) {
			t.Errorf("text body missing %q\ntext:\n%s", want, text)
		}
	}
}

func TestInviteEmailTemplateHonorsLeagueURLEnv(t *testing.T) {
	service := newTestService(t, true)
	t.Setenv("LEAGUE_URL", "https://league.example.com")

	_, text, _ := service.InviteEmailTemplate("manager@example.com")
	if !strings.Contains(text, "https://league.example.com") {
		t.Errorf("text body did not honor LEAGUE_URL override:\n%s", text)
	}
	if strings.Contains(text, defaultLeagueURL) {
		t.Errorf("text body should not fall back to the default URL when LEAGUE_URL is set:\n%s", text)
	}
}

func TestInviteEmailTemplateHTMLCarriesCTALinkAndFacts(t *testing.T) {
	service := newTestService(t, true)
	t.Setenv("LEAGUE_URL", "")

	_, _, htmlBody := service.InviteEmailTemplate("manager@example.com")

	if !strings.Contains(htmlBody, `href="`+defaultLeagueURL+`"`) {
		t.Errorf("html body missing CTA link to the league URL:\n%s", htmlBody)
	}
	if !strings.Contains(htmlBody, "CLAIM YOUR SEAT") {
		t.Errorf("html body missing the CTA label:\n%s", htmlBody)
	}
	if !strings.Contains(htmlBody, "manager@example.com") {
		t.Errorf("html body missing the escaped invite email:\n%s", htmlBody)
	}
	draft := service.draftSummary(time.Now())
	longDate, _ := draft["long_date"].(string)
	if !strings.Contains(htmlBody, longDate) {
		t.Errorf("html body missing the long draft date %q:\n%s", longDate, htmlBody)
	}
}

func TestInviteEmailTemplateHTMLEscapesUnsafeEmail(t *testing.T) {
	service := newTestService(t, true)

	_, _, htmlBody := service.InviteEmailTemplate(`<script>alert(1)</script>@example.com`)

	if strings.Contains(htmlBody, "<script>") {
		t.Errorf("html body should escape a raw '<' in the email address:\n%s", htmlBody)
	}
	if !strings.Contains(htmlBody, "&lt;script&gt;") {
		t.Errorf("html body should carry the escaped email address:\n%s", htmlBody)
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
	if htmlBody, _ := preview["html"].(string); !strings.Contains(htmlBody, "their-email@example.com") {
		t.Errorf("invite_preview html should address the sample email: %q", htmlBody)
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
