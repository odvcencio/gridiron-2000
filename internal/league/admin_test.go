package league

import (
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

// TestInviteEmailTemplateCarriesFactsAndEmail pins the neutral shipped
// default's invite copy (productization spec section 4.3, owner decision):
// no real league name, no divisions baked in, no venue clause — the
// unconfigured checkout never leaks this project's own reference league.
func TestInviteEmailTemplateCarriesFactsAndEmail(t *testing.T) {
	service := newTestService(t, true)
	t.Setenv("LEAGUE_URL", "")

	subject, text, _ := service.InviteEmailTemplate("manager@example.com")

	if !strings.Contains(subject, service.cfg.Name) {
		t.Errorf("subject missing league name: %q", subject)
	}
	if !strings.HasPrefix(subject, "You're invited:") {
		t.Errorf("subject missing invite lead-in: %q", subject)
	}
	for _, want := range []string{
		service.cfg.Name, "manager@example.com",
		service.cfg.URL, "Rules page", "— The Commissioner",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("text body missing %q\ntext:\n%s", want, text)
		}
	}
	for _, unwanted := range []string{"Aqua", "Orange", "Dolphins", "GRIDIRON 2000"} {
		if strings.Contains(text, unwanted) {
			t.Errorf("neutral default invite text must not carry reference-league flavor %q:\n%s", unwanted, text)
		}
	}
}

// TestInviteEmailTemplateCarryoverLineMatchesMode proves the "rosters
// carry over" claim only appears for a DYNASTY-mode league (SK launch-prep
// finding: the invite text asserted this unconditionally, which was false
// for a REDRAFT-labeled league).
func TestInviteEmailTemplateCarryoverLineMatchesMode(t *testing.T) {
	service := newTestService(t, true)
	service.cfg.ModeLabel = "DYNASTY"
	_, dynastyText, _ := service.InviteEmailTemplate("manager@example.com")
	if !strings.Contains(dynastyText, "Rosters carry over") {
		t.Errorf("DYNASTY mode: text missing the carryover line:\n%s", dynastyText)
	}

	service.cfg.ModeLabel = "REDRAFT"
	_, redraftText, _ := service.InviteEmailTemplate("manager@example.com")
	if strings.Contains(redraftText, "Rosters carry over") {
		t.Errorf("REDRAFT mode: text must not claim rosters carry over:\n%s", redraftText)
	}
	if !strings.Contains(redraftText, "— The Commissioner") {
		t.Errorf("REDRAFT mode: text missing its sign-off:\n%s", redraftText)
	}
}

// TestInviteEmailTemplateReproducesDeployedLeagueFacts simulates the
// reference deployment's own league.json (Aqua/Orange divisions, the
// Dolphins venue line) and checks the derived blurb and venue clause carry
// those facts — the "behaviorally identical to today's build" invariant,
// exercised through config instead of a compiled literal.
func TestInviteEmailTemplateReproducesDeployedLeagueFacts(t *testing.T) {
	service := newTestService(t, true)
	t.Setenv("LEAGUE_URL", "")
	service.cfg = referenceDeploymentConfig()
	service.teams = teamsFromSeeds(service.cfg.Teams)

	subject, text, htmlBody := service.InviteEmailTemplate("manager@example.com")
	if !strings.Contains(subject, service.cfg.Name) {
		t.Errorf("subject missing league name: %q", subject)
	}
	for _, want := range []string{
		"Aqua", "Orange", "Dolphins", "manager@example.com", "Rules page", "— The Commissioner",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("text body missing %q\ntext:\n%s", want, text)
		}
	}
	if !strings.Contains(htmlBody, "Dolphins") {
		t.Errorf("html body missing the venue row for a config with copy.venue_line set:\n%s", htmlBody)
	}
}

func TestInviteEmailTemplateHonorsLeagueURLEnv(t *testing.T) {
	service := newTestService(t, true)
	t.Setenv("LEAGUE_URL", "https://league.example.com")

	_, text, _ := service.InviteEmailTemplate("manager@example.com")
	if !strings.Contains(text, "1. Open https://league.example.com/join") {
		t.Errorf("text body did not honor LEAGUE_URL override:\n%s", text)
	}
	if strings.Contains(text, service.cfg.URL) {
		t.Errorf("text body should not fall back to the default URL when LEAGUE_URL is set:\n%s", text)
	}
}

func TestInviteEmailTemplateHTMLCarriesCTALinkAndFacts(t *testing.T) {
	service := newTestService(t, true)
	t.Setenv("LEAGUE_URL", "")

	_, _, htmlBody := service.InviteEmailTemplate("manager@example.com")

	if !strings.Contains(htmlBody, `href="`+service.cfg.URL+`/join"`) {
		t.Errorf("html body missing CTA link to the seat-claim route:\n%s", htmlBody)
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

func TestLeaguePathURLJoinsHostedRoutesCleanly(t *testing.T) {
	service := newTestService(t, true)
	for _, tc := range []struct {
		name string
		base string
		want string
	}{
		{name: "origin", base: "https://league.example.com", want: "https://league.example.com/join"},
		{name: "trailing slash", base: "https://league.example.com/", want: "https://league.example.com/join"},
		{name: "hosted base path", base: "https://league.example.com/fantasy/", want: "https://league.example.com/fantasy/join"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("LEAGUE_URL", tc.base)
			if got := service.leaguePathURL("/join"); got != tc.want {
				t.Fatalf("leaguePathURL(/join) = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestInviteEmailTemplateDirectsInviteesToSeatClaim(t *testing.T) {
	service := newTestService(t, true)
	t.Setenv("LEAGUE_URL", "https://league.example.com/fantasy/")

	_, text, htmlBody := service.InviteEmailTemplate("manager@example.com")
	want := "https://league.example.com/fantasy/join"
	if !strings.Contains(text, "1. Open "+want) {
		t.Errorf("text invite must lead directly to seat claim:\n%s", text)
	}
	if !strings.Contains(htmlBody, `href="`+want+`"`) {
		t.Errorf("HTML invite must lead directly to seat claim:\n%s", htmlBody)
	}
}

// TestInviteEmailTemplateOmitsVenueRowWhenUnset pins the spec section 3.2
// rule: an empty copy.venue_line drops the VENUE row and the text clause
// entirely, rather than inventing a venue for a league that has none.
func TestInviteEmailTemplateOmitsVenueRowWhenUnset(t *testing.T) {
	service := newTestService(t, true)
	_, text, htmlBody := service.InviteEmailTemplate("manager@example.com")
	if strings.Contains(htmlBody, "VENUE") {
		t.Errorf("html body should omit the VENUE row when copy.venue_line is empty:\n%s", htmlBody)
	}
	draft := service.draftSummary(time.Now())
	draftTime, _ := draft["time"].(string)
	longDate, _ := draft["long_date"].(string)
	wantSentence := "The startup snake draft is " + longDate + " at " + draftTime + "."
	if !strings.Contains(text, wantSentence) {
		t.Errorf("text body should end the draft sentence cleanly with no venue clause:\nwant substring %q\ngot:\n%s", wantSentence, text)
	}
}

// referenceDeploymentConfig builds the config equivalent of this project's
// own reference deployment's (gitignored) league.json — the same shape
// config/league-real.json.example documents — so tests can pin
// "deployed-league behavior preserved" without a real file on disk.
func referenceDeploymentConfig() Config {
	cfg := DefaultConfig()
	cfg.Name = "GRIDIRON 2000"
	cfg.ShortCode = "G2K"
	cfg.Tagline = "Dynasty Fantasy League"
	cfg.ModeLabel = "DYNASTY"
	cfg.URL = "https://gridiron.draco.quest"
	cfg.Teams = []TeamSeed{
		{ID: "team-1", Name: "Aqua 1", Abbreviation: "AQ1", Division: "Aqua", Tone: "cyan"},
		{ID: "team-2", Name: "Aqua 2", Abbreviation: "AQ2", Division: "Aqua", Tone: "blue"},
		{ID: "team-3", Name: "Aqua 3", Abbreviation: "AQ3", Division: "Aqua", Tone: "violet"},
		{ID: "team-4", Name: "Aqua 4", Abbreviation: "AQ4", Division: "Aqua", Tone: "lime"},
		{ID: "team-5", Name: "Orange 1", Abbreviation: "OR1", Division: "Orange", Tone: "orange"},
		{ID: "team-6", Name: "Orange 2", Abbreviation: "OR2", Division: "Orange", Tone: "gold"},
		{ID: "team-7", Name: "Orange 3", Abbreviation: "OR3", Division: "Orange", Tone: "magenta"},
		{ID: "team-8", Name: "Orange 4", Abbreviation: "OR4", Division: "Orange", Tone: "pink"},
	}
	cfg.Rounds = 17
	cfg.RosterPresetName = "gridiron-house"
	cfg.Roster = rosterPresets["gridiron-house"]
	cfg.Copy = CopyBlock{
		VenueLine: "During the Dolphins preseason game — bring both screens.",
	}
	cfg.Source = "file:league.json"
	return cfg
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

func TestInviteEmailTemplateHTMLEscapesUnsafeJoinURL(t *testing.T) {
	service := newTestService(t, true)
	unsafeURL := `https://league.example.com/" onmouseover="alert(1)`

	htmlBody := service.inviteEmailHTML("AUG 22", "Saturday, August 22", "4:00 PM", unsafeURL, "manager@example.com", "a league")

	if strings.Contains(htmlBody, `href="`+unsafeURL+`"`) {
		t.Errorf("html body must not insert the raw join URL into an attribute:\n%s", htmlBody)
	}
	if !strings.Contains(htmlBody, "&#34; onmouseover=&#34;") {
		t.Errorf("html body should attribute-escape the join URL:\n%s", htmlBody)
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
	if subject, _ := preview["subject"].(string); !strings.Contains(subject, service.cfg.Name) {
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

func TestAdminDataReportsInviteAcceptanceAndReadiness(t *testing.T) {
	service := newTestService(t, true)
	request, _ := http.NewRequest(http.MethodGet, "/admin", nil)
	for _, email := range []string{"waiting@example.com", "signed-in@example.com", "ready@example.com"} {
		if err := service.AdminAddInvite(request, email); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := service.store.EnsureMember("signed-in@example.com", "Seatless Person"); err != nil {
		t.Fatal(err)
	}
	member, _, err := service.store.AssignMember("ready@example.com", "Ready Manager")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.store.ToggleReady(member.TeamID); err != nil {
		t.Fatal(err)
	}

	data := service.AdminData(request)
	if got := data["invite_count"]; got != 3 {
		t.Fatalf("invite_count = %v, want 3", got)
	}
	if got := data["invite_signed_in_count"]; got != 2 {
		t.Errorf("invite_signed_in_count = %v, want 2", got)
	}
	if got := data["invite_seated_count"]; got != 1 {
		t.Errorf("invite_seated_count = %v, want 1", got)
	}
	if got := data["invite_ready_count"]; got != 1 {
		t.Errorf("invite_ready_count = %v, want 1", got)
	}
	if got := data["invite_waiting_count"]; got != 1 {
		t.Errorf("invite_waiting_count = %v, want 1", got)
	}
	if got := data["invite_seatless_count"]; got != 1 {
		t.Errorf("invite_seatless_count = %v, want 1", got)
	}
	if got := data["member_count"]; got != 1 {
		t.Errorf("member_count = %v, want 1 claimed seat; seatless sign-ins must not inflate the masthead", got)
	}

	byEmail := map[string]map[string]any{}
	for _, invite := range data["invites"].([]map[string]any) {
		byEmail[invite["email"].(string)] = invite
	}
	if got := byEmail["waiting@example.com"]["status"]; got != "WAITING" {
		t.Errorf("waiting status = %v", got)
	}
	if got := byEmail["signed-in@example.com"]["status"]; got != "SIGNED IN" {
		t.Errorf("signed-in status = %v", got)
	}
	ready := byEmail["ready@example.com"]
	if ready["status"] != "READY" || ready["ready"] != true || ready["seated"] != true {
		t.Errorf("ready invite = %+v", ready)
	}
	if ready["team_name"] == "" || ready["role_label"] != "PRIMARY MANAGER" {
		t.Errorf("ready invite lacks team/role context: %+v", ready)
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

// TestDraftIsLive checks that scheduled time and demo mode never replace
// the persisted commissioner start.
func TestDraftIsLive(t *testing.T) {
	draftAt := time.Date(2026, 8, 22, 16, 0, 0, 0, time.UTC)
	live := newTestService(t, false)
	live.store.draftLifecycleBypass = false
	live.store.state.DraftStarted = false
	if live.draftIsLive(draftAt.Add(-time.Second)) {
		t.Error("draft must be closed before explicit start")
	}
	if live.draftIsLive(draftAt.Add(time.Hour)) {
		t.Error("scheduled time must not open the draft")
	}
	live.store.state.DraftStarted = true
	if !live.draftIsLive(draftAt.Add(-time.Hour)) {
		t.Error("explicit start may open before the scheduled window")
	}
	demo := newTestService(t, true)
	demo.store.draftLifecycleBypass = false
	demo.store.state.DraftStarted = false
	if demo.draftIsLive(draftAt.Add(time.Hour)) {
		t.Error("demo mode still requires explicit start")
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

// TestAdminUndoPickRequiresCommissioner checks that a non-commissioner
// request is rejected before it can touch the draft at all.
func TestAdminUndoPickRequiresCommissioner(t *testing.T) {
	service := newTestService(t, false)
	request, _ := http.NewRequest(http.MethodGet, "/admin", nil)
	t.Setenv("COMMISSIONER_EMAILS", "boss@example.com")

	if err := service.AdminUndoPick(request); err == nil {
		t.Fatal("a non-commissioner request must be rejected")
	}
}

// TestAdminSetReady checks AdminSetReady's authority gate, its explicit
// (non-toggle) assignment, and that it rejects an unknown team — the
// commissioner path that sets any seat's Ready flag on the commissioner's
// own authority, mirroring AdminSetAutopick.
func TestAdminSetReady(t *testing.T) {
	t.Run("sets a different seat's ready flag for the commissioner", func(t *testing.T) {
		service := newTestService(t, true) // demo mode grants commissioner
		request, _ := http.NewRequest(http.MethodGet, "/admin", nil)

		if err := service.AdminSetReady(request, "team-4", true); err != nil {
			t.Fatalf("AdminSetReady: %v", err)
		}
		state := service.store.Snapshot()
		if !state.Ready["team-4"] {
			t.Fatal("team-4 ready = false, want true")
		}
		if state.Ready["team-1"] {
			t.Fatal("team-1 ready = true, want untouched (false)")
		}
	})

	t.Run("rejected for non-commissioners", func(t *testing.T) {
		service := newTestService(t, false)
		request, _ := http.NewRequest(http.MethodGet, "/admin", nil)
		t.Setenv("COMMISSIONER_EMAILS", "boss@example.com")

		if err := service.AdminSetReady(request, "team-4", true); err == nil {
			t.Fatal("a non-commissioner request must be rejected")
		}
	})

	t.Run("rejected for an unknown team", func(t *testing.T) {
		service := newTestService(t, true) // demo mode grants commissioner
		request, _ := http.NewRequest(http.MethodGet, "/admin", nil)

		if err := service.AdminSetReady(request, "team-not-real", true); err == nil {
			t.Fatal("an unknown team must be rejected")
		}
	})

	t.Run("setting on twice is idempotent, not a toggle", func(t *testing.T) {
		service := newTestService(t, true) // demo mode grants commissioner
		request, _ := http.NewRequest(http.MethodGet, "/admin", nil)

		if err := service.AdminSetReady(request, "team-2", true); err != nil {
			t.Fatalf("AdminSetReady (first): %v", err)
		}
		if err := service.AdminSetReady(request, "team-2", true); err != nil {
			t.Fatalf("AdminSetReady (second): %v", err)
		}
		if !service.store.Snapshot().Ready["team-2"] {
			t.Fatal("team-2 ready = false after two explicit sets to true, want true (a toggle would flip back to false)")
		}
	})
}

// TestAdminUndoPickRearmsClockWithInjectedClock checks that AdminUndoPick
// resolves the re-armed deadline from the service's injected clock
// (service.now) plus the resolved pick-clock duration, the same
// duration-resolution helper the manual pick flow (MakePick) uses.
func TestAdminUndoPickRearmsClockWithInjectedClock(t *testing.T) {
	service := newTestService(t, true) // demo mode grants commissioner
	fixedNow := time.Date(2026, 8, 22, 16, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return fixedNow }
	request, _ := http.NewRequest(http.MethodGet, "/admin", nil)

	team := teamOnClock(nil, 1)
	if _, err := service.store.MakePick(team, "p-01", "manager", fixedNow, fixedNow.Add(90*time.Second)); err != nil {
		t.Fatal(err)
	}

	if err := service.AdminUndoPick(request); err != nil {
		t.Fatalf("AdminUndoPick: %v", err)
	}
	state := service.store.Snapshot()
	if len(state.Picks) != 0 {
		t.Fatalf("picks after undo = %d, want 0", len(state.Picks))
	}
	want := fixedNow.Add(service.pickClock(state))
	if !state.ClockDeadline.Equal(want) {
		t.Fatalf("ClockDeadline = %v, want %v (injected clock + pick clock duration)", state.ClockDeadline, want)
	}
}

// TestAdminUndoPickEmptyDraft checks that AdminUndoPick surfaces the
// store's "no picks to undo" error unchanged through the service layer.
func TestAdminUndoPickEmptyDraft(t *testing.T) {
	service := newTestService(t, true)
	request, _ := http.NewRequest(http.MethodGet, "/admin", nil)

	err := service.AdminUndoPick(request)
	if err == nil {
		t.Fatal("expected an error undoing a pick on an empty draft")
	}
	if !strings.Contains(err.Error(), "no picks to undo") {
		t.Fatalf("err = %q, want it to contain %q", err.Error(), "no picks to undo")
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

func TestAdminReleaseSeatRequiresCurrentTargetConfirmation(t *testing.T) {
	service := newTestService(t, true)
	request, _ := http.NewRequest(http.MethodPost, "/admin", nil)
	member, _, err := service.store.AssignMember("primary@example.com", "Primary")
	if err != nil {
		t.Fatal(err)
	}
	team := service.Teams()[0]
	expected := seatReleaseConfirmation(team.ID, team.Name)
	token := seatReleaseToken(service.store.Snapshot(), team.ID, team.Name)
	for _, confirmation := range []string{"", "RELEASE TEAM-2 WRONG"} {
		before := service.store.Snapshot()
		if _, err := service.AdminReleaseSeat(request, team.ID, confirmation, token); err == nil || err.Error() != "this action requires explicit confirmation" {
			t.Fatalf("confirmation %q error = %v, want explicit confirmation rejection", confirmation, err)
		}
		after := service.store.Snapshot()
		if after.Members["primary@example.com"].TeamID != before.Members["primary@example.com"].TeamID {
			t.Fatalf("rejected release changed member binding: before=%+v after=%+v", before.Members, after.Members)
		}
	}
	if _, err := service.AdminReleaseSeat(request, team.ID, expected, ""); !errors.Is(err, errAdminActionStale) {
		t.Fatalf("missing seat token error = %v, want stale-action rejection", err)
	}
	if _, err := service.AdminRenameTeam(request, team.ID, "Renamed Franchise"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.AdminReleaseSeat(request, team.ID, expected, token); !errors.Is(err, errAdminActionStale) {
		t.Fatalf("stale label confirmation error = %v, want stale-action rejection", err)
	}
	if got := service.store.Snapshot().Members[member.Email].TeamID; got != team.ID {
		t.Fatalf("stale release changed member binding to %q", got)
	}
	current := seatReleaseConfirmation(team.ID, "Renamed Franchise")
	currentToken := seatReleaseToken(service.store.Snapshot(), team.ID, "Renamed Franchise")
	if _, err := service.AdminReleaseSeat(request, team.ID, current, currentToken); err != nil {
		t.Fatalf("current target confirmation: %v", err)
	}
	if got := service.store.Snapshot().Members[member.Email].TeamID; got != "" {
		t.Fatalf("confirmed release left member binding %q", got)
	}
	if _, err := service.AdminReleaseSeat(request, team.ID, current, currentToken); !errors.Is(err, errAdminActionStale) {
		t.Fatalf("duplicate release error = %v, want consumed-token rejection", err)
	}

	second, _, err := service.store.AssignMember("second@example.com", "Second")
	if err != nil || second.TeamID != team.ID {
		t.Fatalf("second claim = %+v, %v; want %s", second, err, team.ID)
	}
	if _, err := service.AdminReleaseSeat(request, team.ID, current, currentToken); !errors.Is(err, errAdminActionStale) {
		t.Fatalf("first occupant replay error = %v, want stale-action rejection", err)
	}
	if got := service.store.Snapshot().Members[second.Email].TeamID; got != team.ID {
		t.Fatalf("stale first-occupant replay evicted second occupant: %q", got)
	}

	secondToken := seatReleaseToken(service.store.Snapshot(), team.ID, "Renamed Franchise")
	if _, err := service.AdminReleaseSeat(request, team.ID, current, secondToken); err != nil {
		t.Fatalf("release second occupant: %v", err)
	}
	reclaimed, _, err := service.store.AssignMember(second.Email, second.Name)
	if err != nil || reclaimed.TeamID != team.ID {
		t.Fatalf("same-occupant reclaim = %+v, %v; want %s", reclaimed, err, team.ID)
	}
	if _, err := service.AdminReleaseSeat(request, team.ID, current, secondToken); !errors.Is(err, errAdminActionStale) {
		t.Fatalf("same-occupant replay error = %v, want stale-action rejection", err)
	}
	if got := service.store.Snapshot().Members[second.Email].TeamID; got != team.ID {
		t.Fatalf("stale same-occupant replay evicted reclaimed seat: %q", got)
	}
}

func TestAdminReleaseSeatRequiresCommissioner(t *testing.T) {
	service := newTestService(t, false)
	request, _ := http.NewRequest(http.MethodPost, "/admin", nil)
	member, _, err := service.store.AssignMember("manager@example.com", "Manager")
	if err != nil {
		t.Fatal(err)
	}
	team := service.teamByID(member.TeamID)
	state := service.store.Snapshot()
	token := seatReleaseToken(state, team.ID, team.Name)
	if _, err := service.AdminReleaseSeat(request, team.ID, seatReleaseConfirmation(team.ID, team.Name), token); err == nil || err.Error() != "commissioner access is required" {
		t.Fatalf("non-commissioner release error = %v, want role rejection", err)
	}
	if got := service.store.Snapshot().Members[member.Email].TeamID; got != team.ID {
		t.Fatalf("role-rejected release changed member binding to %q", got)
	}
}

func TestAdminReleaseSeatPersistFailureRestoresSeatState(t *testing.T) {
	service := newTestService(t, true)
	request, _ := http.NewRequest(http.MethodPost, "/admin", nil)
	member, _, err := service.store.AssignMember("primary-failure@example.com", "Primary")
	if err != nil {
		t.Fatal(err)
	}
	if err := service.store.InviteCoManager(member.TeamID, "pending-co@example.com"); err != nil {
		t.Fatal(err)
	}
	if err := service.store.SetReady(member.TeamID, true); err != nil {
		t.Fatal(err)
	}
	team := service.teamByID(member.TeamID)
	before := service.store.Snapshot()
	token := seatReleaseToken(before, team.ID, team.Name)
	failThisStorePersist(service.store)
	if _, err := service.AdminReleaseSeat(request, team.ID, seatReleaseConfirmation(team.ID, team.Name), token); !errors.Is(err, errInjectedPersist) {
		t.Fatalf("release persist failure = %v, want injected failure", err)
	}
	after := service.store.Snapshot()
	if !reflect.DeepEqual(after.Members, before.Members) ||
		!reflect.DeepEqual(after.CoInvites, before.CoInvites) ||
		!reflect.DeepEqual(after.Ready, before.Ready) ||
		!reflect.DeepEqual(after.SeatRevisions, before.SeatRevisions) {
		t.Fatalf("failed release published partial seat state:\n before=%+v\n after=%+v", before, after)
	}
	durable := reloadStoredState(t, service.store.filePath)
	if !reflect.DeepEqual(durable.Members, before.Members) ||
		!reflect.DeepEqual(durable.CoInvites, before.CoInvites) ||
		!reflect.DeepEqual(durable.Ready, before.Ready) ||
		!reflect.DeepEqual(durable.SeatRevisions, before.SeatRevisions) {
		t.Fatalf("failed release changed durable seat state:\n before=%+v\n durable=%+v", before, durable)
	}
}

func TestAdminClockViewHasOneTruthfulState(t *testing.T) {
	service := newTestService(t, true)
	now := time.Date(2026, 8, 23, 18, 0, 0, 0, time.UTC)
	cases := []struct {
		name       string
		state      PersistedState
		wantState  string
		wantArmed  bool
		wantPause  bool
		wantResume bool
	}{
		{name: "unarmed", state: PersistedState{}, wantState: "NOT RUNNING", wantArmed: false, wantPause: false, wantResume: false},
		{name: "paused", state: PersistedState{DraftStarted: true, ClockPaused: true, ClockRemainingSec: 42}, wantState: "PAUSED", wantArmed: true, wantPause: false, wantResume: true},
		{name: "running", state: PersistedState{DraftStarted: true, ClockDeadline: now.Add(time.Minute)}, wantState: "RUNNING", wantArmed: true, wantPause: true, wantResume: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			view := service.clockView(tc.state, now)
			if view["state"] != tc.wantState || view["armed"] != tc.wantArmed {
				t.Fatalf("clock view state=%v armed=%v, want %s/%v: %+v", view["state"], view["armed"], tc.wantState, tc.wantArmed, view)
			}
			if view["can_pause"] != tc.wantPause || view["can_resume"] != tc.wantResume {
				t.Fatalf("clock controls pause=%v resume=%v, want %v/%v: %+v", view["can_pause"], view["can_resume"], tc.wantPause, tc.wantResume, view)
			}
		})
	}
}

func TestAdminClockActionsRejectImpossibleTransitions(t *testing.T) {
	service := newTestService(t, true)
	request, _ := http.NewRequest(http.MethodPost, "/admin", nil)
	if err := service.AdminPauseClock(request); err == nil || err.Error() != "the clock is not running" {
		t.Fatalf("pause unarmed error = %v, want not-running rejection", err)
	}
	if err := service.AdminResumeClock(request); err == nil || err.Error() != "start the draft before resuming its clock" {
		t.Fatalf("resume pre-draft error = %v, want lifecycle rejection", err)
	}
	service.store.state.DraftStarted = true
	service.store.state.ClockPaused = true
	service.store.state.ClockRemainingSec = 30
	if err := service.AdminResumeClock(request); err != nil {
		t.Fatalf("resume paused clock: %v", err)
	}
	if err := service.AdminResumeClock(request); err == nil || err.Error() != "the clock is already running" {
		t.Fatalf("resume running error = %v, want already-running rejection", err)
	}
}
