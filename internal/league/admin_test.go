package league

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
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

	subject, text, _ := service.InviteEmailTemplate(nil, "manager@example.com")

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

// TestInviteEmailTemplateStatesDynastySeasonBoundary keeps both invite
// representations honest: DYNASTY remains the long-term format intent, but
// this release does not promise automated multi-season roster rollover.
func TestInviteEmailTemplateStatesDynastySeasonBoundary(t *testing.T) {
	service := newTestService(t, true)
	service.cfg.ModeLabel = "DYNASTY"
	_, dynastyText, dynastyHTML := service.InviteEmailTemplate(nil, "manager@example.com")
	for _, body := range []string{dynastyText, dynastyHTML} {
		if strings.Contains(strings.ToLower(body), "rosters carry over") {
			t.Errorf("DYNASTY invite must not promise automatic carryover:\n%s", body)
		}
	}
	for _, want := range []string{
		"dynasty format", "fresh draft", "not automated yet",
		"commissioner", "carryover",
	} {
		if !strings.Contains(strings.ToLower(dynastyText), strings.ToLower(want)) {
			t.Errorf("DYNASTY text missing explicit season boundary %q:\n%s", want, dynastyText)
		}
	}
	if !strings.Contains(dynastyHTML, "Dynasty format is the long-term league intent") ||
		!strings.Contains(dynastyHTML, "commissioner-managed until automation ships") {
		t.Errorf("DYNASTY HTML missing explicit season boundary:\n%s", dynastyHTML)
	}

	service.cfg.ModeLabel = "REDRAFT"
	_, redraftText, redraftHTML := service.InviteEmailTemplate(nil, "manager@example.com")
	for _, body := range []string{redraftText, redraftHTML} {
		if strings.Contains(strings.ToLower(body), "rosters carry over") ||
			strings.Contains(strings.ToLower(body), "roster rollover") {
			t.Errorf("REDRAFT invite must not carry dynasty rollover language:\n%s", body)
		}
	}
	if !strings.Contains(redraftText, "— The Commissioner") {
		t.Errorf("REDRAFT mode: text missing its sign-off:\n%s", redraftText)
	}
	if !strings.Contains(redraftHTML, "This is a fresh-season league") {
		t.Errorf("REDRAFT HTML missing fresh-season posture:\n%s", redraftHTML)
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

	subject, text, htmlBody := service.InviteEmailTemplate(nil, "manager@example.com")
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

	_, text, _ := service.InviteEmailTemplate(nil, "manager@example.com")
	if !strings.Contains(text, "1. Open https://league.example.com/join") {
		t.Errorf("text body did not honor LEAGUE_URL override:\n%s", text)
	}
	if strings.Contains(text, service.cfg.URL) {
		t.Errorf("text body should not fall back to the default URL when LEAGUE_URL is set:\n%s", text)
	}
}

// TestInviteEmailTemplateUsesRequestOriginWhenUnconfigured pins the
// 2026-09-01 wave-1-verification finding: an unconfigured instance's own
// /admin invite preview and sent invites printed "1. Open
// http://localhost:8080/join" (config.go's defaultConfigURL, never a real
// address a manager could reach) instead of the address the commissioner
// is actually viewing the console from. A real league.json url or
// LEAGUE_URL still wins outright — this fallback only replaces the
// placeholder default, mirroring the HQ fleet card's own "the local
// instance never trusts a placeholder public URL" rule.
func TestInviteEmailTemplateUsesRequestOriginWhenUnconfigured(t *testing.T) {
	service := newTestService(t, true)
	t.Setenv("LEAGUE_URL", "")
	request, _ := http.NewRequest(http.MethodGet, "/admin", nil)
	request.Host = "gridiron.example.org"

	_, text, htmlBody := service.InviteEmailTemplate(request, "manager@example.com")
	if !strings.Contains(text, "1. Open http://gridiron.example.org/join") {
		t.Errorf("text body did not use the request's own origin:\n%s", text)
	}
	if !strings.Contains(htmlBody, `href="http://gridiron.example.org/join"`) {
		t.Errorf("html body did not use the request's own origin:\n%s", htmlBody)
	}
	if strings.Contains(text, "localhost:8080") || strings.Contains(htmlBody, "localhost:8080") {
		t.Errorf("invite copy must never print the unconfigured default URL:\ntext:\n%s\nhtml:\n%s", text, htmlBody)
	}
}

// TestInviteEmailTemplateUsesRequestOriginForFileSourcedPlaceholder pins
// the wave-2-verification finding: config/league.json.example line 8 ships
// "url": "http://localhost:8080" verbatim, so any league.json copied from
// it (and never edited) loads with Source == "file:..." and the identical
// placeholder text DefaultConfig() carries. The original fix only checked
// cfg.Source != "defaults", so a file-sourced config with this placeholder
// read as "configured" and still printed the unreachable loopback address.
func TestInviteEmailTemplateUsesRequestOriginForFileSourcedPlaceholder(t *testing.T) {
	service := newTestService(t, true)
	t.Setenv("LEAGUE_URL", "")
	service.cfg.Source = "file:league.json"
	service.cfg.URL = defaultConfigURL
	request, _ := http.NewRequest(http.MethodGet, "/admin", nil)
	request.Host = "gridiron.example.org"

	_, text, htmlBody := service.InviteEmailTemplate(request, "manager@example.com")
	if !strings.Contains(text, "1. Open http://gridiron.example.org/join") {
		t.Errorf("text body did not use the request's own origin for a file-sourced placeholder:\n%s", text)
	}
	if !strings.Contains(htmlBody, `href="http://gridiron.example.org/join"`) {
		t.Errorf("html body did not use the request's own origin for a file-sourced placeholder:\n%s", htmlBody)
	}
	if strings.Contains(text, "localhost:8080") || strings.Contains(htmlBody, "localhost:8080") {
		t.Errorf("file-sourced placeholder URL must never print in invite copy:\ntext:\n%s\nhtml:\n%s", text, htmlBody)
	}
}

// TestInviteEmailTemplateUsesRequestOriginForLoopbackHostMismatch pins the
// broader unconfigured shape: an operator's league.json url need not be
// the exact defaultConfigURL string to be a placeholder — any localhost or
// 127.0.0.1 host is unreachable from a real request landing on a
// different host, so it falls back the same way. A request that also
// lands on localhost (matching the configured host) is left alone; that
// is a legitimate same-host local deployment, not a stale placeholder.
func TestInviteEmailTemplateUsesRequestOriginForLoopbackHostMismatch(t *testing.T) {
	service := newTestService(t, true)
	t.Setenv("LEAGUE_URL", "")
	service.cfg.Source = "file:league.json"
	service.cfg.URL = "http://127.0.0.1:9090"

	mismatched, _ := http.NewRequest(http.MethodGet, "/admin", nil)
	mismatched.Host = "gridiron.example.org"
	_, text, _ := service.InviteEmailTemplate(mismatched, "manager@example.com")
	if !strings.Contains(text, "1. Open http://gridiron.example.org/join") {
		t.Errorf("a loopback config url must fall back to the request origin:\n%s", text)
	}
	if strings.Contains(text, "127.0.0.1") {
		t.Errorf("loopback config url leaked into copy despite a mismatched request host:\n%s", text)
	}

	sameHost, _ := http.NewRequest(http.MethodGet, "/admin", nil)
	sameHost.Host = "127.0.0.1:9090"
	_, sameHostText, _ := service.InviteEmailTemplate(sameHost, "manager@example.com")
	if !strings.Contains(sameHostText, "1. Open http://127.0.0.1:9090/join") {
		t.Errorf("a loopback config url matching the request host must stay configured:\n%s", sameHostText)
	}
}

// TestInviteEmailTemplateConfiguredURLOutranksRequestOrigin keeps a real
// deployment's own configured URL authoritative even when a request is
// available — the request origin is strictly a fallback for the
// unconfigured default, never a silent override of an operator's choice.
func TestInviteEmailTemplateConfiguredURLOutranksRequestOrigin(t *testing.T) {
	service := newTestService(t, true)
	t.Setenv("LEAGUE_URL", "https://league.example.com")
	request, _ := http.NewRequest(http.MethodGet, "/admin", nil)
	request.Host = "some-other-host.example.org"

	_, text, _ := service.InviteEmailTemplate(request, "manager@example.com")
	if !strings.Contains(text, "1. Open https://league.example.com/join") {
		t.Errorf("configured LEAGUE_URL must outrank the request origin:\n%s", text)
	}
	if strings.Contains(text, "some-other-host.example.org") {
		t.Errorf("request origin leaked into copy despite a configured LEAGUE_URL:\n%s", text)
	}
}

func TestInviteEmailTemplateHTMLCarriesCTALinkAndFacts(t *testing.T) {
	service := newTestService(t, true)
	t.Setenv("LEAGUE_URL", "")

	_, _, htmlBody := service.InviteEmailTemplate(nil, "manager@example.com")

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

	_, text, htmlBody := service.InviteEmailTemplate(nil, "manager@example.com")
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
	_, text, htmlBody := service.InviteEmailTemplate(nil, "manager@example.com")
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

// TestInviteEmailTemplateStatesUnpublishedDraftDateCleanly pins the
// 2026-09-01 wave-1-verification finding: the demo /admin invite preview
// printed "The startup snake draft is Draft time not published yet at ."
// — draftSummaryForState's own unpublished placeholders ("Draft time not
// published yet" for long_date, "" for time) interpolated straight into
// the "is %s at %s." sentence. An unpublished date needs its own sentence
// with no dangling "at" clause.
func TestInviteEmailTemplateStatesUnpublishedDraftDateCleanly(t *testing.T) {
	service := newTestService(t, true)
	service.draftAt = time.Now().Add(500 * 24 * time.Hour)

	_, text, _ := service.InviteEmailTemplate(nil, "manager@example.com")
	want := "The startup snake draft date is not published yet."
	if !strings.Contains(text, want) {
		t.Errorf("text body must state the unpublished date cleanly:\nwant substring %q\ngot:\n%s", want, text)
	}
	for _, bad := range []string{"Draft time not published yet at", " at .", "is Draft time not published"} {
		if strings.Contains(text, bad) {
			t.Errorf("text body must not interpolate the unpublished placeholder into the draft sentence: %q found in:\n%s", bad, text)
		}
	}
}

// TestInviteEmailTemplateSubjectAndBodyAgreeOnDraftDate guards wave-6 item
// 7(e): the subject always interpolated the raw draft-summary date fields
// directly, while the body's sentence separately guarded on the summary's
// "published" bool. A league whose draft already started — with a real,
// audited start date/time — but whose originally scheduled date was never
// published kept "published" false, so the subject showed a real date
// ("draft TUE · SEP 1") beside a body claiming no date was published at
// all. Both must now derive from the same signal.
func TestInviteEmailTemplateSubjectAndBodyAgreeOnDraftDate(t *testing.T) {
	t.Run("unpublished and never started", func(t *testing.T) {
		service := newTestService(t, true)
		service.draftAt = time.Now().Add(500 * 24 * time.Hour)

		subject, text, _ := service.InviteEmailTemplate(nil, "manager@example.com")
		if !strings.Contains(subject, "draft TBD") {
			t.Errorf("subject = %q, want it to state TBD, not a fabricated date", subject)
		}
		if !strings.Contains(text, "The startup snake draft date is not published yet.") {
			t.Errorf("text body must state the unpublished date cleanly:\n%s", text)
		}
	})

	t.Run("started with a real recorded instant, schedule never published", func(t *testing.T) {
		service := newTestService(t, true)
		service.draftAt = time.Now().Add(500 * 24 * time.Hour)
		started := time.Date(2026, time.September, 1, 19, 0, 0, 0, time.UTC)
		service.store.state.DraftStarted = true
		service.store.state.DraftStartedAt = started

		subject, text, _ := service.InviteEmailTemplate(nil, "manager@example.com")
		if strings.Contains(subject, "draft TBD") {
			t.Errorf("subject dropped the real started date: %q", subject)
		}
		if strings.Contains(text, "date is not published yet") {
			t.Errorf("body claims unpublished despite a real recorded start instant:\n%s", text)
		}
		if !strings.Contains(text, "The startup snake draft is") {
			t.Errorf("body did not state the real draft date/time:\n%s", text)
		}
	})
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

	_, _, htmlBody := service.InviteEmailTemplate(nil, `<script>alert(1)</script>@example.com`)

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

// TestAdminInviteMapNudgesASeatedManagerInsteadOfInviting pins F20
// (gap-audit J2): the console's only outbound control sent an already-
// seated manager the exact same "You're invited... you've got a seat
// waiting" copy it sends someone who has never claimed a seat — the one
// way to chase a not-ready manager doubled as inviting them to a seat
// they already hold. Once seated, the mailto must carry no claim
// language and must link the draft room instead of the join page.
func TestAdminInviteMapNudgesASeatedManagerInsteadOfInviting(t *testing.T) {
	service := newTestService(t, true)
	request, _ := http.NewRequest(http.MethodGet, "/admin", nil)
	if err := service.AdminAddInvite(request, "jorge@example.com"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.store.AssignMember("jorge@example.com", "Jorge V"); err != nil {
		t.Fatal(err)
	}

	data := service.AdminData(request)
	invites, _ := data["invites"].([]map[string]any)
	var seated map[string]any
	for _, invite := range invites {
		if invite["email"] == "jorge@example.com" {
			seated = invite
		}
	}
	if seated == nil {
		t.Fatalf("invites = %+v, missing jorge@example.com", invites)
	}
	if seated["seated"] != true {
		t.Fatalf("seated flag = %v, want true", seated["seated"])
	}
	mailto, _ := seated["mailto"].(string)
	if !strings.HasPrefix(mailto, "mailto:jorge@example.com?subject=") {
		t.Fatalf("mailto malformed: %q", mailto)
	}
	decoded, err := url.QueryUnescape(mailto)
	if err != nil {
		t.Fatalf("decode mailto: %v", err)
	}
	for _, notWant := range []string{"invited", "seat waiting", "/join"} {
		if strings.Contains(strings.ToLower(decoded), strings.ToLower(notWant)) {
			t.Errorf("nudge mailto still carries claim language %q: %s", notWant, decoded)
		}
	}
	for _, want := range []string{"check in", "/draft"} {
		if !strings.Contains(strings.ToLower(decoded), strings.ToLower(want)) {
			t.Errorf("nudge mailto missing %q: %s", want, decoded)
		}
	}
}

// TestAdminDataDraftOrderCarriesPickNumbers pins F29 (gap-audit J2): the
// published draft order listed eight teams with a division chip and no
// ordinal at all — "who picks seventh?" is the week's most common
// question, and neither commissioner page answered it directly.
// pick_number must be 1-indexed and match each team's own position in
// the persisted draft order.
func TestAdminDataDraftOrderCarriesPickNumbers(t *testing.T) {
	service := newTestService(t, true)
	request, _ := http.NewRequest(http.MethodGet, "/admin", nil)

	order := defaultTeamIDs()
	if err := service.store.SetDraftOrder(order); err != nil {
		t.Fatal(err)
	}

	data := service.AdminData(request)
	draftOrder, ok := data["draft_order"].([]map[string]any)
	if !ok || len(draftOrder) != len(order) {
		t.Fatalf("draft_order = %#v, want %d entries", data["draft_order"], len(order))
	}
	for index, row := range draftOrder {
		want := index + 1
		got, _ := row["pick_number"].(int)
		if got != want {
			t.Fatalf("draft_order[%d] pick_number = %v, want %d (team %v)", index, row["pick_number"], want, row["id"])
		}
		if row["id"] != order[index] {
			t.Fatalf("draft_order[%d] id = %v, want %v (pick_number must track the real order, not a seat index)", index, row["id"], order[index])
		}
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

// TestAnnouncementAdminMapsUsesLeagueZoneWithRelative is the gap-audit
// finding for admin.go:554: the Announcements panel formatted PostedAt
// directly (whatever zone the stored instant carried, typically UTC) with
// no relative text. September 13 falls in Eastern daylight time (EDT),
// the league's default zone.
func TestAnnouncementAdminMapsUsesLeagueZoneWithRelative(t *testing.T) {
	service := newTestService(t, true)
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	state := PersistedState{Announcements: []Announcement{
		{ID: "ann-1", Body: "Draft night moved.", PostedBy: "Commissioner", PostedAt: now.Add(-3 * time.Hour).UTC()},
	}}
	rows := service.announcementAdminMaps(state)
	if len(rows) != 1 {
		t.Fatalf("announcementAdminMaps = %+v, want 1 row", rows)
	}
	if got := rows[0]["posted_at"]; got != "Sep 13, 5:00 AM EDT · 3 hours ago" {
		t.Fatalf("posted_at = %v, want the league-zone stamp plus relative suffix", got)
	}
	// wave-2-verification item 5: the delete control's aria-label needs the
	// absolute stamp alone — a relative fragment like "3 hours ago" baked
	// into an accessible name goes stale in the accessibility tree, which
	// reads the name once rather than live-updating it.
	if got := rows[0]["posted_at_absolute"]; got != "Sep 13, 5:00 AM EDT" {
		t.Fatalf("posted_at_absolute = %v, want the league-zone stamp with no relative suffix", got)
	}
}

// TestCommissionerAttentionReadOnlyGeneratedAtSplitsDisplayISOAndRelative
// pins wave-2-verification item 6: the "READ AT" row's generated_at field
// used to be a bare now.UTC().Format(time.RFC3339) instant with no
// datetime-valid pairing and no league-local display — every other stored
// instant on the console routes through leagueTimeStamp's split. This
// splits the same instant into a league-local display stamp (no relative
// suffix; the caller supplies its own relative label), a datetime-valid
// ISO string, and a relative label — mirroring the fleet card's
// GeneratedAt/GeneratedAtISO/GeneratedAtRelative fields
// (app/commissioner/view.go).
func TestCommissionerAttentionReadOnlyGeneratedAtSplitsDisplayISOAndRelative(t *testing.T) {
	service := newTestService(t, true)
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	request, _ := http.NewRequest(http.MethodGet, "/admin", nil)

	data := service.CommissionerAttentionDataReadOnly(request)
	if got := data["generated_at"]; got != "Sep 13, 8:00 AM EDT" {
		t.Errorf("generated_at = %v, want the league-zone stamp with no relative suffix", got)
	}
	if got := data["generated_at_iso"]; got != "2026-09-13T12:00:00Z" {
		t.Errorf("generated_at_iso = %v, want a datetime-valid RFC3339 instant", got)
	}
	if got := data["generated_at_relative"]; got != "just now" {
		t.Errorf(`generated_at_relative = %v, want "just now" (relative to the same generation instant)`, got)
	}
}

// TestCommissionerAttentionReadOnlyExposesFriendlyDraftDateAndPresence
// is the item 5 data-layer proof (2026-09-02 audit): the live readout's
// own "draft" block used to carry only the raw RFC3339 "at" instant, and
// each seat carried only the raw snake_case presence enum with no
// human-readable counterpart — /admin's own AdminAttentionReadout
// component (fragment.go) had no friendly value to bind for either.
func TestCommissionerAttentionReadOnlyExposesFriendlyDraftDateAndPresence(t *testing.T) {
	service := newTestService(t, true)
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	request, _ := http.NewRequest(http.MethodGet, "/admin", nil)
	meetingAt := now.Add(4 * 24 * time.Hour).Format("2006-01-02T15:04")
	if err := service.AdminRescheduleDraft(request, meetingAt); err != nil {
		t.Fatalf("reschedule draft: %v", err)
	}

	data := service.CommissionerAttentionDataReadOnly(request)
	draft, ok := data["draft"].(map[string]any)
	if !ok {
		t.Fatalf("draft = %#v, want a map", data["draft"])
	}
	wantSummary := service.draftSummaryForState(now, service.store.Snapshot())
	if draft["date"] != wantSummary["date"] || draft["time"] != wantSummary["time"] || draft["published"] != wantSummary["published"] {
		t.Fatalf("draft date/time/published = %+v, want date=%v time=%v published=%v", draft, wantSummary["date"], wantSummary["time"], wantSummary["published"])
	}
	if at, _ := draft["date"].(string); at == "" || strings.Contains(at, "T") {
		t.Errorf("draft date = %q, want a league-local formatted date, not a raw RFC3339 fragment", at)
	}

	seats, ok := data["seats"].([]map[string]any)
	if !ok || len(seats) == 0 {
		t.Fatalf("seats = %#v, want a non-empty slice", data["seats"])
	}
	for _, seat := range seats {
		presence, _ := seat["presence"].(string)
		label, _ := seat["presence_label"].(string)
		if want := presenceReadableLabel(presence); label != want {
			t.Errorf("seat %v presence_label = %q, want %q (for presence %q)", seat["id"], label, want, presence)
		}
		if label == "" || strings.Contains(label, "_") {
			t.Errorf("seat %v presence_label = %q, must be plain words, not a snake_case enum", seat["id"], label)
		}
	}
}

// TestPendingInviteCountExcludesAlreadySeatedInvitees is the item 4
// decisive proof (2026-09-02 audit): an invite stays "pending" only
// until its own email claims a seat. CommissionerAttentionDataReadOnly's
// own "invite_count" (the /admin "X PENDING" readout) and
// commissioner_summary_v1.go's Membership.PendingInvites both used to
// sum every issued invite address with no seated check, so a league
// where every invited manager had already claimed a seat still reported
// one pending invite per address ever sent — 8, on the flagship, where
// the truth was 1.
func TestPendingInviteCountExcludesAlreadySeatedInvitees(t *testing.T) {
	service := newTestService(t, true)
	request, _ := http.NewRequest(http.MethodGet, "/admin", nil)
	// Every invite below has already claimed (or co-claimed) a seat,
	// except one: the true, still-pending invite.
	if err := service.AdminAddInvite(request, "claimed-1@example.com"); err != nil {
		t.Fatal(err)
	}
	if err := service.AdminAddInvite(request, "still-pending@example.com"); err != nil {
		t.Fatal(err)
	}
	teamID := service.Teams()[1].ID
	if err := service.store.InviteCoManager(teamID, "claimed-2@example.com"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.store.AssignMember("claimed-1@example.com", "Seated One"); err != nil {
		t.Fatal(err)
	}
	if _, bound, err := service.store.BindCoManager("claimed-2@example.com", "Seated Co"); err != nil || !bound {
		t.Fatalf("BindCoManager = bound=%v err=%v, want bound=true", bound, err)
	}

	if got := pendingInviteCount(service.store.Snapshot()); got != 1 {
		t.Fatalf("pendingInviteCount = %d, want 1 (only still-pending@example.com)", got)
	}
	if got := service.CommissionerAttentionDataReadOnly(request)["invite_count"]; got != 1 {
		t.Fatalf("/admin invite_count = %v, want 1", got)
	}

	cfg, _, data, release := commissionerV1Fixture()
	state := service.store.Snapshot()
	v1Summary, err := projectCommissionerSummaryV1(commissionerSummaryV1Tuple{
		config: cfg, state: state, data: data, release: release, now: commissionerV1TestNow,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := *v1Summary.Membership.PendingInvites; got != 1 {
		t.Fatalf("commissioner_summary_v1 PendingInvites = %d, want 1 (agrees with /admin)", got)
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
		if _, _, _, err := service.AdminForceAutopick(request, ForceCurrentPickConfirmation, "stale"); err == nil {
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

		pick, player, team, err := service.AdminForceAutopick(request, ForceCurrentPickConfirmation, draftCurrentPickToken(service.store.Snapshot()))
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
		if _, _, _, err := service.AdminForceAutopick(request, ForceCurrentPickConfirmation, draftCurrentPickToken(service.store.Snapshot())); err == nil {
			t.Fatal("force auto-pick must be rejected once the draft is complete")
		}
	})
}

// TestAdminRunWaivers pins F5's commissioner force-run: the same
// authority/confirmation gates AdminForceAutopick uses, a matching
// errAdminActionStale freshness token (2026-08-30 review, finding 10), and
// a resolved run that reuses Store.ProcessWaivers exactly (the ordinary
// cycle's own resolution path), recording WaiversProcessedThrough at the
// commissioner's own clock instant rather than a computed nextRun.
func TestAdminRunWaivers(t *testing.T) {
	t.Run("rejected for non-commissioners", func(t *testing.T) {
		service := newTestService(t, false)
		request, _ := http.NewRequest(http.MethodGet, "/admin", nil)
		t.Setenv("COMMISSIONER_EMAILS", "boss@example.com")
		if _, err := service.AdminRunWaivers(request, RunWaiversConfirmation, "stale"); err == nil {
			t.Fatal("a non-commissioner request must be rejected")
		}
	})

	t.Run("rejected without the exact confirmation", func(t *testing.T) {
		service := newTestService(t, true) // demo mode grants commissioner
		request, _ := http.NewRequest(http.MethodGet, "/admin", nil)
		token := waiverRunToken(service.store.Snapshot())
		if _, err := service.AdminRunWaivers(request, "wrong", token); err == nil {
			t.Fatal("a missing/incorrect confirmation must be rejected")
		}
	})

	t.Run("rejected for a missing or stale token", func(t *testing.T) {
		service := newTestService(t, true)
		now := time.Date(2026, 9, 20, 15, 0, 0, 0, time.UTC)
		service.now = func() time.Time { return now }
		service.SetPlayerSource(func() ([]Player, int64, string) { return processWaiversPool(), 1, "test" })
		service.SetScheduleSource(func() []GameInfo {
			return []GameInfo{{ID: "g-fixture", Week: 1, Away: "AAA", Home: "BBB", Kickoff: now.Add(-24 * time.Hour), Final: true}}
		})
		request, _ := http.NewRequest(http.MethodGet, "/admin", nil)
		if _, err := service.AdminRunWaivers(request, RunWaiversConfirmation, ""); !errors.Is(err, errAdminActionStale) {
			t.Fatalf("empty token = %v, want stale-action rejection", err)
		}
		if _, err := service.AdminRunWaivers(request, RunWaiversConfirmation, "stale-token"); !errors.Is(err, errAdminActionStale) {
			t.Fatalf("stale token = %v, want stale-action rejection", err)
		}
	})

	t.Run("a replayed token after a successful run is rejected", func(t *testing.T) {
		service := newTestService(t, true)
		now := time.Date(2026, 9, 20, 15, 0, 0, 0, time.UTC)
		service.now = func() time.Time { return now }
		service.SetPlayerSource(func() ([]Player, int64, string) { return processWaiversPool(), 1, "test" })
		service.SetScheduleSource(func() []GameInfo {
			return []GameInfo{{ID: "g-fixture", Week: 1, Away: "AAA", Home: "BBB", Kickoff: now.Add(-24 * time.Hour), Final: true}}
		})
		if err := service.store.SetDraftOrder(defaultTeamIDs()); err != nil {
			t.Fatal(err)
		}
		if err := service.store.FileClaim(WaiverClaim{ID: "clm-replay", TeamID: "team-7", AddID: "wv-1", FiledAt: now.Add(-time.Hour)}); err != nil {
			t.Fatal(err)
		}
		request, _ := http.NewRequest(http.MethodGet, "/admin", nil)
		token := waiverRunToken(service.store.Snapshot())
		if _, err := service.AdminRunWaivers(request, RunWaiversConfirmation, token); err != nil {
			t.Fatalf("first run: %v", err)
		}
		if _, err := service.AdminRunWaivers(request, RunWaiversConfirmation, token); !errors.Is(err, errAdminActionStale) {
			t.Fatalf("replayed token after a completed run = %v, want stale-action rejection", err)
		}
	})

	t.Run("resolves a due claim immediately, out of cycle", func(t *testing.T) {
		service := newTestService(t, true)
		now := time.Date(2026, 9, 20, 15, 0, 0, 0, time.UTC)
		service.now = func() time.Time { return now }
		service.SetPlayerSource(func() ([]Player, int64, string) { return processWaiversPool(), 1, "test" })
		service.SetScheduleSource(func() []GameInfo {
			return []GameInfo{{ID: "g-fixture", Week: 1, Away: "AAA", Home: "BBB", Kickoff: now.Add(-24 * time.Hour), Final: true}}
		})
		if err := service.store.SetDraftOrder(defaultTeamIDs()); err != nil {
			t.Fatal(err)
		}
		if err := service.store.FileClaim(WaiverClaim{ID: "clm-1", TeamID: "team-7", AddID: "wv-1", FiledAt: now.Add(-time.Hour)}); err != nil {
			t.Fatal(err)
		}
		request, _ := http.NewRequest(http.MethodGet, "/admin", nil)
		token := waiverRunToken(service.store.Snapshot())
		results, err := service.AdminRunWaivers(request, RunWaiversConfirmation, token)
		if err != nil {
			t.Fatalf("AdminRunWaivers: %v", err)
		}
		if len(results) != 1 || results[0].Outcome != "won" {
			t.Fatalf("results = %+v, want one won outcome", results)
		}
		state := service.store.Snapshot()
		if len(state.WaiverClaims) != 0 || len(state.Transactions) != 1 {
			t.Fatalf("claims/transactions = %d/%d, want 0/1", len(state.WaiverClaims), len(state.Transactions))
		}
		if !state.WaiversProcessedThrough.Equal(now.UTC()) {
			t.Fatalf("WaiversProcessedThrough = %v, want %v (the commissioner's own clock instant)", state.WaiversProcessedThrough, now)
		}
	})

	// TestAdminRunWaivers/records a commissioner event checks the wave-2
	// commissioner-console audit trail: a forced out-of-cycle run is a
	// commissioner-gated mutation (F5), so it must leave a durable,
	// person-attributed row the same way AdminForceAutopick does.
	t.Run("records a commissioner event", func(t *testing.T) {
		service := newTestService(t, true)
		now := time.Date(2026, 9, 20, 15, 0, 0, 0, time.UTC)
		service.now = func() time.Time { return now }
		service.SetPlayerSource(func() ([]Player, int64, string) { return processWaiversPool(), 1, "test" })
		service.SetScheduleSource(func() []GameInfo {
			return []GameInfo{{ID: "g-fixture", Week: 1, Away: "AAA", Home: "BBB", Kickoff: now.Add(-24 * time.Hour), Final: true}}
		})
		if err := service.store.SetDraftOrder(defaultTeamIDs()); err != nil {
			t.Fatal(err)
		}
		if err := service.store.FileClaim(WaiverClaim{ID: "clm-audit", TeamID: "team-7", AddID: "wv-1", FiledAt: now.Add(-time.Hour)}); err != nil {
			t.Fatal(err)
		}
		request, _ := http.NewRequest(http.MethodGet, "/admin", nil)
		token := waiverRunToken(service.store.Snapshot())
		if _, err := service.AdminRunWaivers(request, RunWaiversConfirmation, token); err != nil {
			t.Fatalf("AdminRunWaivers: %v", err)
		}
		events := service.store.Snapshot().CommissionerEvents
		if len(events) != 1 || events[0].Kind != "waivers.force_run" || events[0].ActorEmail == "" {
			t.Fatalf("commissioner events = %+v, want one waivers.force_run row with actor identity", events)
		}
	})
}

// TestAdminWaiversMapHasOpenClaimsGatesTheForceRunControl pins finding 8
// of the 2026-08-30 review round 2: has_open_claims was computed and
// handed to every admin render but nothing ever read it, so the force-run
// control rendered identically whether or not there was anything for a
// forced run to resolve. This checks the value itself is correct in both
// states — app/admin/page.gsx's own render test covers the markup that
// now gates on it.
func TestAdminWaiversMapHasOpenClaimsGatesTheForceRunControl(t *testing.T) {
	service := newTestService(t, true)
	now := time.Date(2026, 9, 20, 15, 0, 0, 0, time.UTC)

	state := service.store.Snapshot()
	waivers := service.adminWaiversMap(state, now)
	if got, ok := waivers["has_open_claims"].(bool); !ok || got {
		t.Fatalf("has_open_claims = %v, want false with no open claims", waivers["has_open_claims"])
	}
	if got, ok := waivers["open_claim_count"].(int); !ok || got != 0 {
		t.Fatalf("open_claim_count = %v, want 0", waivers["open_claim_count"])
	}

	if err := service.store.FileClaim(WaiverClaim{ID: "clm-open", TeamID: "team-7", AddID: "wv-1", FiledAt: now.Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	state = service.store.Snapshot()
	waivers = service.adminWaiversMap(state, now)
	if got, ok := waivers["has_open_claims"].(bool); !ok || !got {
		t.Fatalf("has_open_claims = %v, want true with one open claim", waivers["has_open_claims"])
	}
	if got, ok := waivers["open_claim_count"].(int); !ok || got != 1 {
		t.Fatalf("open_claim_count = %v, want 1", waivers["open_claim_count"])
	}
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
	pick, _, _, err := service.AdminForceAutopick(request, ForceCurrentPickConfirmation, draftCurrentPickToken(service.store.Snapshot()))
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

	if _, _, _, err := service.AdminUndoPick(request, ""); err == nil {
		t.Fatal("a non-commissioner request must be rejected")
	}
}

func claimReadyTestSeats(t *testing.T, service *Service, count int) {
	t.Helper()
	for index := 1; index <= count; index++ {
		member, created, err := service.store.AssignMember(
			fmt.Sprintf("ready-manager-%d@example.com", index),
			fmt.Sprintf("Ready Manager %d", index),
		)
		if err != nil {
			t.Fatalf("claim ready test seat %d: %v", index, err)
		}
		wantTeamID := fmt.Sprintf("team-%d", index)
		if !created || member.TeamID != wantTeamID {
			t.Fatalf("claim ready test seat %d = created %v, team %q; want true, %q", index, created, member.TeamID, wantTeamID)
		}
	}
}

// TestAdminSetReady checks AdminSetReady's authority gate, its explicit
// (non-toggle) assignment, and that it rejects unknown and unclaimed teams —
// the commissioner path that sets a claimed seat's Ready flag on the
// commissioner's own authority, mirroring AdminSetAutopick.
func TestAdminSetReady(t *testing.T) {
	t.Run("sets a different seat's ready flag for the commissioner", func(t *testing.T) {
		service := newTestService(t, true) // demo mode grants commissioner
		request, _ := http.NewRequest(http.MethodGet, "/admin", nil)
		claimReadyTestSeats(t, service, 4)

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
		claimReadyTestSeats(t, service, 4)

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

	t.Run("rejected for an unclaimed seat", func(t *testing.T) {
		service := newTestService(t, true) // demo mode grants commissioner
		request, _ := http.NewRequest(http.MethodGet, "/admin", nil)

		if err := service.AdminSetReady(request, "team-2", true); err == nil || err.Error() != "READY requires a claimed or managed seat" {
			t.Fatalf("unclaimed AdminSetReady error = %v, want membership rejection", err)
		}
	})

	t.Run("setting on twice is idempotent, not a toggle", func(t *testing.T) {
		service := newTestService(t, true) // demo mode grants commissioner
		request, _ := http.NewRequest(http.MethodGet, "/admin", nil)
		claimReadyTestSeats(t, service, 2)

		if err := service.AdminSetReady(request, "team-2", true); err != nil {
			t.Fatalf("AdminSetReady (first): %v", err)
		}
		if err := service.AdminSetReady(request, "team-2", true); err != nil {
			t.Fatalf("AdminSetReady (second): %v", err)
		}
		if !service.store.Snapshot().Ready["team-2"] {
			t.Fatal("team-2 ready = false after two explicit sets to true, want true (a toggle would flip back to false)")
		}
		if err := service.AdminSetReady(request, "team-2", false); err != nil {
			t.Fatalf("AdminSetReady clear (first): %v", err)
		}
		if err := service.AdminSetReady(request, "team-2", false); err != nil {
			t.Fatalf("AdminSetReady clear (second): %v", err)
		}
		if service.store.Snapshot().Ready["team-2"] {
			t.Fatal("team-2 ready = true after two explicit sets to false, want false")
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

	if _, _, _, err := service.AdminUndoPick(request, draftPreviousPickToken(service.store.Snapshot())); err != nil {
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

	_, _, _, err := service.AdminUndoPick(request, "")
	if err == nil {
		t.Fatal("expected a stale-token error when undoing an empty draft")
	}
	if !errors.Is(err, errAdminActionStale) {
		t.Fatalf("err = %q, want errAdminActionStale", err.Error())
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

// TestAdminGenerateScheduleUsesConfiguredSeason pins buildSchedule to
// cfg.Season, the one source of league-season truth commissioner HQ and the
// schedule panel label already read (commissioner_summary.go,
// app/admin/page.server.go's init override comment). buildSchedule
// previously stamped SeasonSchedule.Season from seasonStartAt().Year(), a
// sentinel date unrelated to the configured season, so a deployment whose
// SEASON_START_AT year disagreed with its own league.json season persisted
// the wrong year into every generated schedule.
func TestAdminGenerateScheduleUsesConfiguredSeason(t *testing.T) {
	service := newTestService(t, true)
	service.cfg.Season = seasonStartAt().Year() + 5
	request, _ := http.NewRequest(http.MethodGet, "/admin", nil)

	sched, err := service.AdminGenerateSchedule(request, 4, 1, 42)
	if err != nil {
		t.Fatal(err)
	}
	if sched.Season != service.cfg.Season {
		t.Fatalf("schedule.Season = %d, want configured season %d (not the seasonStartAt sentinel year %d)", sched.Season, service.cfg.Season, seasonStartAt().Year())
	}
	stored := service.store.Snapshot().Schedule
	if stored == nil || stored.Season != service.cfg.Season {
		t.Fatalf("persisted schedule.Season = %+v, want configured season %d", stored, service.cfg.Season)
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

// TestPluralRendersCountedNoun pins the shared plural helper (gap-audit item
// 11): admin and HQ console copy printed "1 LEAGUES", "1 occurrence(s)", and
// "2 day(s)" instead of "1 league" and "2 days". Only the exact count 1 gets
// the singular form; every other count, including 0, gets the plural.
// TestPoolStatusMapExposesActualAndTargetCoverageSeparately guards
// wave-6 item 7(k): /admin's "Pool coverage" stat printed the TARGET
// ratio bare (status.Target / rosterCapacity), reading as a live
// measurement of the actual pool. It now also exposes actual_coverage
// (status.Players / rosterCapacity), matching Commissioner HQ's own
// "ACTUAL {x} · TARGET {y}" presentation.
func TestPoolStatusMapExposesActualAndTargetCoverageSeparately(t *testing.T) {
	service := newTestService(t, true)
	service.SetPlayerSource(func() ([]Player, int64, string) { return testPool(20), 1, "live" })
	pool := service.poolStatusMap()

	actual, ok := pool["actual_coverage"].(string)
	if !ok || actual == "" {
		t.Fatalf("pool actual_coverage = %#v, want a formatted ratio string", pool["actual_coverage"])
	}
	target, ok := pool["coverage"].(string)
	if !ok || target == "" {
		t.Fatalf("pool coverage = %#v, want a formatted ratio string", pool["coverage"])
	}
	if !strings.HasSuffix(actual, "×") || !strings.HasSuffix(target, "×") {
		t.Fatalf("actual_coverage=%q coverage=%q, want both formatted as N.N×", actual, target)
	}
	// This fixture's 20-player pool is far below its target/roster
	// capacity, so the two ratios must differ — proving actual_coverage
	// is a genuinely distinct, live measurement, not a copy of the
	// static target.
	if actual == target {
		t.Fatalf("actual_coverage (%q) == coverage (%q); fixture pool (20 players) should diverge from the configured target", actual, target)
	}
}

// TestAdminDataUsesLeagueMapForViewer pins wave-6 glue item 2: AdminData's
// "league" key must come from the viewer-aware leagueMapForViewer(r), not
// the request-less leagueMap(), so an anonymous demo visitor's admin
// console (demo mode grants commissioner authority without a signed-in
// session) reads the same honest empty attention shape every other route's
// shared chrome does — not a chip naming trades that belong to the real
// seated managers. See
// TestLeagueMapForViewerSuppressesAttentionForAnonymousDemoViewer in
// attention_test.go for the underlying leagueMapForViewer contract.
func TestAdminDataUsesLeagueMapForViewer(t *testing.T) {
	teamIDs := defaultTeamIDs()
	if len(teamIDs) < 2 {
		t.Fatal("fixture league needs at least two teams")
	}
	service := newTestService(t, true) // demo mode grants commissioner
	service.SetScheduleSource(func() []GameInfo { return nil })
	service.store.state.TradeOffers = []TradeOffer{
		{ID: "trd-admin", FromTeamID: teamIDs[0], ToTeamID: teamIDs[1], Status: TradeStatusAccepted},
	}
	request, err := http.NewRequest(http.MethodGet, "/admin", nil)
	if err != nil {
		t.Fatal(err)
	}

	data := service.AdminData(request)
	league, ok := data["league"].(map[string]any)
	if !ok {
		t.Fatalf("AdminData()[league] = %#v, want map[string]any", data["league"])
	}
	attention, ok := league["attention"].(map[string]any)
	if !ok {
		t.Fatalf("AdminData()[league][attention] = %#v, want map[string]any", league["attention"])
	}
	if attention["has_items"] != false || attention["urgent_count"] != 0 {
		t.Fatalf("anonymous demo admin attention = %+v, want the honest empty shape", attention)
	}
}

func TestPluralRendersCountedNoun(t *testing.T) {
	cases := []struct {
		n    int
		word string
		want string
	}{
		{0, "day", "0 days"},
		{1, "day", "1 day"},
		{2, "day", "2 days"},
		{7, "day", "7 days"},
		{-1, "day", "-1 days"},
		{1, "league", "1 league"},
		{3, "league", "3 leagues"},
	}
	for _, c := range cases {
		if got := CountNoun(c.n, c.word); got != c.want {
			t.Errorf("CountNoun(%d, %q) = %q, want %q", c.n, c.word, got, c.want)
		}
	}
}
