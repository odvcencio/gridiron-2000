package league

import (
	"fmt"
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

// TestCommissionerForceAutopick checks AdminForceAutopick's authority gate,
// its live-draft gate, and that it fires while paused with provenance
// MadeBy == "commissioner".
//
// Spec delta: test 15 also asks for "rejected for non-commissioners" and
// "rejected pre-draft" from the *same* otherwise-authorized commissioner
// request. That combination is not reachable in this test package:
// auth.Current keys its context value with an unexported type from
// m31labs.dev/gosx/auth, so no external package — including this one — can
// forge a signed-in user, and IsCommissioner in live (non-demo) mode
// requires exactly that. Demo mode is the only way this suite reaches a
// positive commissioner check, and demo mode is defined (section 8.5) to
// never gate on draftAt. The two gates are therefore covered separately:
// the commissioner gate below, live, with no forged identity; the live-draft
// gate via the extracted draftIsLive helper in TestDraftIsLive, pure and
// without any auth dependency.
func TestCommissionerForceAutopick(t *testing.T) {
	t.Run("rejected for non-commissioners", func(t *testing.T) {
		service := newTestService(t, false)
		request, _ := http.NewRequest(http.MethodGet, "/admin", nil)
		t.Setenv("COMMISSIONER_EMAILS", "boss@example.com")
		if _, _, _, err := service.AdminForceAutopick(request); err == nil {
			t.Fatal("a non-commissioner request must be rejected")
		}
	})

	t.Run("fires while paused with commissioner provenance", func(t *testing.T) {
		service := newTestService(t, true) // demo mode grants commissioner
		service.SetPlayerSource(func() ([]Player, int64, string) { return testPool(20), 1, "live" })
		request, _ := http.NewRequest(http.MethodGet, "/admin", nil)

		if err := service.store.ArmClock(time.Now().Add(90 * time.Second)); err != nil {
			t.Fatal(err)
		}
		if err := service.AdminPauseClock(request); err != nil {
			t.Fatal(err)
		}

		pick, player, team, err := service.AdminForceAutopick(request)
		if err != nil {
			t.Fatalf("force auto-pick while paused: %v", err)
		}
		if pick.MadeBy != "commissioner" {
			t.Fatalf("MadeBy = %q, want commissioner", pick.MadeBy)
		}
		if player.ID == "" || team.ID == "" {
			t.Fatalf("force auto-pick returned an empty player or team: %+v %+v", player, team)
		}
		if got := service.store.Snapshot().Picks; len(got) != 1 {
			t.Fatalf("picks = %d, want 1", len(got))
		}
	})

	t.Run("rejected once the draft is complete", func(t *testing.T) {
		service := newTestService(t, true)
		service.SetPlayerSource(func() ([]Player, int64, string) { return testPool(200), 1, "live" })
		request, _ := http.NewRequest(http.MethodGet, "/admin", nil)

		total := len(defaultTeams()) * DraftRounds
		for number := 1; number <= total; number++ {
			team := teamOnClock(nil, number)
			playerID := fmt.Sprintf("pool-%03d", number)
			if _, err := service.store.MakePick(team, playerID, "manager", time.Now(), time.Time{}); err != nil {
				t.Fatalf("seed pick %d: %v", number, err)
			}
		}
		if _, _, _, err := service.AdminForceAutopick(request); err == nil {
			t.Fatal("force auto-pick must be rejected once the draft is complete")
		}
	})
}

// TestDraftIsLive checks the live-draft gate AdminForceAutopick and
// MakePick share: always live in demo mode, gated on draftAt otherwise.
// See TestCommissionerForceAutopick's doc comment for why this pure check
// stands in for testing AdminForceAutopick's live gate directly.
func TestDraftIsLive(t *testing.T) {
	draftAt := time.Date(2026, 8, 22, 16, 0, 0, 0, time.UTC)

	live := &Service{draftAt: draftAt}
	if live.draftIsLive(draftAt.Add(-time.Second)) {
		t.Error("live mode must gate before draftAt")
	}
	if !live.draftIsLive(draftAt) {
		t.Error("live mode must open exactly at draftAt")
	}

	demo := &Service{draftAt: draftAt, demoMode: true}
	if !demo.draftIsLive(draftAt.Add(-time.Hour)) {
		t.Error("demo mode must never gate on draftAt")
	}
}

// TestAdminForceAutopickFiresN6Hook checks that AdminForceAutopick wires
// the N6 autopick-made hook (the commissioner call site WP-E3 left
// unwired — see the design spec section 3, N6's "commissioner" trigger,
// and internal/league/notifications_test.go's TestAutopickMadeNotification
// for notifyAutopickMade's own, provenance-agnostic coverage). A
// commissioner-forced pick for a manager who is not CONNECTED must land
// exactly one autopick: ledger entry and enqueue one message.
func TestAdminForceAutopickFiresN6Hook(t *testing.T) {
	draftAt := time.Now().Add(-time.Hour)
	service, _ := newNotifyTestService(t, draftAt, draftAt)
	service.demoMode = true // grants commissioner authority without a forged session
	service.SetPlayerSource(func() ([]Player, int64, string) { return testPool(20), 1, "live" })
	if _, _, err := service.store.AssignMember("a@example.com", "A"); err != nil { // team-1, first on the clock
		t.Fatal(err)
	}
	// No presence.record call: the tracker floors an unseen key to AWAY,
	// so notifyAutopickMade's CONNECTED skip does not apply here.

	request, _ := http.NewRequest(http.MethodGet, "/admin", nil)
	pick, _, _, err := service.AdminForceAutopick(request)
	if err != nil {
		t.Fatalf("AdminForceAutopick: %v", err)
	}
	if pick.MadeBy != "commissioner" {
		t.Fatalf("MadeBy = %q, want commissioner", pick.MadeBy)
	}

	if got := sentLogCount(service.store.Snapshot(), "autopick:"); got != 1 {
		t.Fatalf("autopick: ledger entries = %d, want 1 (AdminForceAutopick must fire the N6 hook)", got)
	}
	if got := service.notifyQueue.Depth(); got != 1 {
		t.Fatalf("notify queue depth = %d, want 1 (AdminForceAutopick must enqueue the N6 email)", got)
	}
}

func TestAdminGenerateScheduleCreatesAndPersists(t *testing.T) {
	service := newTestService(t, true)
	request, _ := http.NewRequest(http.MethodGet, "/admin", nil)

	sched, err := service.AdminGenerateSchedule(request, 14, 1, 42)
	if err != nil {
		t.Fatal(err)
	}
	if len(sched.Weeks) != 14 {
		t.Fatalf("weeks = %d, want 14", len(sched.Weeks))
	}
	if sched.Seed != 42 {
		t.Errorf("seed = %d, want 42 (explicit seed must be stored as given)", sched.Seed)
	}
	if sched.GeneratedAt.IsZero() {
		t.Error("GeneratedAt must be stamped by AdminGenerateSchedule")
	}
	stored := service.store.Snapshot().Schedule
	if stored == nil || len(stored.Weeks) != 14 {
		t.Fatalf("schedule was not persisted: %+v", stored)
	}
}

func TestAdminGenerateScheduleFailsIfScheduleExists(t *testing.T) {
	service := newTestService(t, true)
	request, _ := http.NewRequest(http.MethodGet, "/admin", nil)
	if _, err := service.AdminGenerateSchedule(request, 14, 1, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := service.AdminGenerateSchedule(request, 14, 1, 2); err == nil {
		t.Error("expected an error generating a schedule when one already exists")
	}
}

func TestAdminGenerateScheduleRequiresCommissioner(t *testing.T) {
	service := newTestService(t, false)
	request, _ := http.NewRequest(http.MethodGet, "/admin", nil)
	if _, err := service.AdminGenerateSchedule(request, 14, 1, 1); err == nil {
		t.Error("expected an error for a non-commissioner request")
	}
}

func TestAdminGenerateScheduleDrawsSeedWhenZero(t *testing.T) {
	service := newTestService(t, true)
	request, _ := http.NewRequest(http.MethodGet, "/admin", nil)
	sched, err := service.AdminGenerateSchedule(request, 14, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if sched.Seed == 0 {
		t.Error("expected a nonzero drawn seed when the caller passes 0")
	}
}

func TestAdminRegenerateScheduleDrawsFreshSeedAndPersists(t *testing.T) {
	service := newTestService(t, true)
	request, _ := http.NewRequest(http.MethodGet, "/admin", nil)
	first, err := service.AdminGenerateSchedule(request, 14, 1, 111)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.AdminRegenerateSchedule(request, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if second.Seed == first.Seed {
		t.Error("expected AdminRegenerateSchedule to draw a fresh seed")
	}
	if len(second.Weeks) != len(first.Weeks) {
		t.Errorf("weeks = %d, want %d (reused from the existing schedule)", len(second.Weeks), len(first.Weeks))
	}
	stored := service.store.Snapshot().Schedule
	if stored.Seed != second.Seed {
		t.Fatalf("persisted schedule does not match the regenerated one: %+v", stored)
	}
}

func TestAdminRegenerateScheduleFailsWithoutExistingSchedule(t *testing.T) {
	service := newTestService(t, true)
	request, _ := http.NewRequest(http.MethodGet, "/admin", nil)
	if _, err := service.AdminRegenerateSchedule(request, 14, 1); err == nil {
		t.Error("expected an error regenerating a schedule that does not exist yet")
	}
}

// TestAdminRegenerateScheduleFailsAfterSeasonStart checks the section 2.3
// guard's first half: now < seasonStartAt().
func TestAdminRegenerateScheduleFailsAfterSeasonStart(t *testing.T) {
	t.Setenv("SEASON_START_AT", "2026-09-10T20:20:00-04:00")
	service := newTestService(t, true)
	seasonStart := seasonStartAt()
	service.now = func() time.Time { return seasonStart.Add(-time.Hour) }
	request, _ := http.NewRequest(http.MethodGet, "/admin", nil)
	if _, err := service.AdminGenerateSchedule(request, 14, 1, 1); err != nil {
		t.Fatal(err)
	}

	service.now = func() time.Time { return seasonStart.Add(time.Hour) } // after kickoff
	if _, err := service.AdminRegenerateSchedule(request, 14, 1); err == nil {
		t.Error("expected an error regenerating the schedule after the season starts")
	}
}

// TestAdminRegenerateScheduleFailsAfterFinalMatchup checks the section 2.3
// guard's second half: no matchup has Final == true.
func TestAdminRegenerateScheduleFailsAfterFinalMatchup(t *testing.T) {
	service := newTestService(t, true)
	request, _ := http.NewRequest(http.MethodGet, "/admin", nil)
	sched, err := service.AdminGenerateSchedule(request, 14, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	week := sched.Weeks[0]
	week.Matchups[0].Final = true
	if err := service.store.SetScheduleWeek(week); err != nil {
		t.Fatal(err)
	}
	if _, err := service.AdminRegenerateSchedule(request, 14, 1); err == nil {
		t.Error("expected an error regenerating the schedule once a matchup has scored")
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
