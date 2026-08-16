package league

import (
	"crypto/rand"
	"fmt"
	"html"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"gridiron-2000/internal/mailer"
)

// AdminData assembles the commissioner console: seat claims, invites, and
// league state counters. The page itself renders a restricted notice for
// non-commissioners; every action re-checks authority server-side.
func (s *Service) AdminData(r *http.Request) map[string]any {
	state := s.store.Snapshot()
	seats := make([]map[string]any, 0, len(s.teams))
	for _, team := range s.teams {
		member := memberForTeam(state.Members, team.ID)
		item := s.teamMap(s.teamView(state, team.ID))
		item["email"] = member.Email
		item["ready"] = state.Ready[team.ID]
		seats = append(seats, item)
	}
	envEmails := splitEmails(os.Getenv("LEAGUE_ALLOWED_EMAILS"))
	envSet := make(map[string]bool, len(envEmails))
	invites := make([]map[string]any, 0, len(envEmails)+len(state.Invites))
	for _, email := range envEmails {
		envSet[email] = true
		invites = append(invites, map[string]any{"email": email, "source": "ENV", "removable": false, "mailto": inviteMailto(s, email)})
	}
	for _, email := range state.Invites {
		if envSet[email] {
			continue
		}
		invites = append(invites, map[string]any{"email": email, "source": "INVITE", "removable": true, "mailto": inviteMailto(s, email)})
	}
	orderIDs := state.DraftOrder
	if len(orderIDs) == 0 {
		orderIDs = defaultTeamIDs()
	}
	draftOrder := make([]map[string]any, 0, len(orderIDs))
	for _, teamID := range orderIDs {
		draftOrder = append(draftOrder, s.teamMap(s.teamView(state, teamID)))
	}
	previewSubject, previewText, previewHTML := s.InviteEmailTemplate("their-email@example.com")
	now := s.clock()
	return map[string]any{
		"viewer":           s.Viewer(r),
		"is_commissioner":  s.IsCommissioner(r),
		"seats":            seats,
		"invites":          invites,
		"invite_count":     len(invites),
		"league_open":      len(invites) == 0,
		"member_count":     len(state.Members),
		"pick_count":       len(state.Picks),
		"seat_count":       len(s.teams),
		"draft":            s.draftSummary(now),
		"demo_mode":        s.demoMode,
		"draft_order":      draftOrder,
		"order_randomized": len(state.DraftOrder) > 0,
		"pool":             s.poolStatusMap(),
		"mail_enabled":     mailer.FromEnv().Enabled(),
		"invite_preview":   map[string]any{"subject": previewSubject, "body": previewText, "html": previewHTML},
		// Draft clock card: armed/paused state, both deadlines, and where
		// the duration comes from (env default or a commissioner override).
		"clock":                 s.clockView(state, now),
		"clock_duration_source": clockDurationSource(state),
		// Mail-health card (design spec section 6.6): whether notifications
		// are wired and enabled, queue depth, the last transport failure,
		// and how many sends landed in the last 24 hours.
		"mail": s.notifyMailMap(now),
	}
}

// clockDurationSource reports whether the pick-clock duration comes from
// the commissioner's persisted override or the PICK_CLOCK environment
// default, for the admin console's "Draft clock" card.
func clockDurationSource(state PersistedState) string {
	if state.ClockDurationSec > 0 {
		return "override"
	}
	return "env"
}

// inviteMailto builds a prefilled mailto: link for one invite email, using
// that email's own copy of the invite template's plain-text body (mailto:
// links cannot carry HTML, so the text version is the only option here).
func inviteMailto(s *Service, email string) string {
	subject, text, _ := s.InviteEmailTemplate(email)
	return "mailto:" + email + "?subject=" + url.QueryEscape(subject) + "&body=" + url.QueryEscape(text)
}

func (s *Service) requireCommissioner(r *http.Request) error {
	if !s.IsCommissioner(r) {
		return fmt.Errorf("commissioner access is required")
	}
	return nil
}

// AdminAddInvite adds a manager email to the invite list.
func (s *Service) AdminAddInvite(r *http.Request, email string) error {
	if err := s.requireCommissioner(r); err != nil {
		return err
	}
	return s.store.AddInvite(email)
}

// defaultLeagueURL is the fallback landing page when LEAGUE_URL is unset.
const defaultLeagueURL = "https://gridiron.draco.quest"

// InviteEmailTemplate builds the subject, plain-text body, and HTML body of
// the invite email sent to one manager. It draws the draft date and time
// from the live draft summary, so the copy always matches the console. The
// HTML body carries the same facts as the text body, dressed in the
// league's Neo-Retro Stadium OS look; the text body is unchanged from the
// plain-text-only era, since its mailto: use depends on the wording.
func (s *Service) InviteEmailTemplate(email string) (subject, text, htmlBody string) {
	draft := s.draftSummary(time.Now())
	shortDate, _ := draft["date"].(string)
	longDate, _ := draft["long_date"].(string)
	draftTime, _ := draft["time"].(string)
	leagueURL := strings.TrimSpace(os.Getenv("LEAGUE_URL"))
	if leagueURL == "" {
		leagueURL = defaultLeagueURL
	}

	subject = fmt.Sprintf("You're invited: GRIDIRON 2000 — dynasty league, draft %s", shortDate)
	text = fmt.Sprintf(`Hi there,

You've got a seat waiting in GRIDIRON 2000, an eight-manager dynasty
league split into the Aqua and Orange divisions.

The startup snake draft is %s at %s — during the Dolphins
preseason game, so bring both screens.

Here's what to do before then:
  1. Open %s
  2. Sign in with Google using this address (%s is on the list)
  3. Claim your seat and rename your team
  4. Build your draft board before the clock starts

The full scoring system is on the Rules page.

Rosters carry over season to season, so draft like it matters.

— The Commissioner`, longDate, draftTime, leagueURL, email)
	htmlBody = inviteEmailHTML(shortDate, longDate, draftTime, leagueURL, email)
	return subject, text, htmlBody
}

// inviteEmailHTML renders the designed HTML invite body: a single 600px
// table with every style inline and no external assets, so it survives
// clipping and dark/light rendering across mail clients. email and
// leagueURL are attacker-influenced (email via the invite form, leagueURL
// via the environment) and are HTML-escaped before insertion.
func inviteEmailHTML(shortDate, longDate, draftTime, leagueURL, email string) string {
	safeEmail := html.EscapeString(email)
	safeURL := html.EscapeString(leagueURL)
	safeShortDate := html.EscapeString(shortDate)
	safeLongDate := html.EscapeString(longDate)
	safeDraftTime := html.EscapeString(draftTime)
	return fmt.Sprintf(inviteEmailHTMLTemplate, safeShortDate, safeLongDate, safeDraftTime, safeEmail, safeURL)
}

// inviteEmailHTMLTemplate is the invite email's HTML body: a single
// 600px-max table, all styles inline, system font stack only (no external
// assets or webfonts), dark ground with light-enough text to survive a
// client forcing light mode. %s placeholders fill, in order: the short
// draft date ("SAT · AUG 22" style) for the signal line, the long draft
// date, the draft time, the invited email, and the league URL (used twice,
// as the CTA href and its escaped text is not repeated so no dupe risk).
const inviteEmailHTMLTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>GRIDIRON 2000</title>
</head>
<body style="margin:0; padding:0; background-color:#070A16;">
<table role="presentation" width="100%%" cellpadding="0" cellspacing="0" border="0" style="width:100%%; background-color:#070A16; margin:0; padding:0;">
<tr>
<td align="center" style="padding:32px 16px;">
<table role="presentation" width="600" cellpadding="0" cellspacing="0" border="0" style="width:600px; max-width:600px; background-color:#0B1024; border:1px solid #26305A; border-radius:4px;">
<tr>
<td style="padding:24px 32px 20px 32px; border-bottom:1px solid #26305A;">
<table role="presentation" cellpadding="0" cellspacing="0" border="0">
<tr>
<td width="44" valign="middle" style="padding-right:12px;">
<table role="presentation" cellpadding="0" cellspacing="0" border="0" style="border:1px solid #38E8FF; border-radius:2px;"><tr><td style="width:40px; height:40px; text-align:center; vertical-align:middle; color:#38E8FF; font-family:'SFMono-Regular',Consolas,Menlo,monospace; font-size:13px; font-weight:700;">G2K</td></tr></table>
</td>
<td valign="middle">
<div style="color:#F7F4EA; font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif; font-size:19px; font-weight:800; letter-spacing:-0.02em; text-transform:uppercase;">GRIDIRON 2000</div>
<div style="color:#8995B8; font-family:'SFMono-Regular',Consolas,Menlo,monospace; font-size:11px; letter-spacing:0.08em; text-transform:uppercase; margin-top:3px;">DYNASTY FANTASY LEAGUE</div>
</td>
</tr>
</table>
</td>
</tr>
<tr>
<td style="padding:20px 32px 0 32px;">
<div style="color:#38E8FF; font-family:'SFMono-Regular',Consolas,Menlo,monospace; font-size:12px; letter-spacing:0.08em; text-transform:uppercase;">&#9679; DRAFT EVENT // %s</div>
</td>
</tr>
<tr>
<td style="padding:14px 32px 0 32px;">
<div style="color:#F7F4EA; font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif; font-size:32px; font-weight:800; letter-spacing:-0.03em; text-transform:uppercase; line-height:1.08;">YOU'RE IN.</div>
<div style="color:#C2CAE1; font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif; font-size:15px; line-height:1.6; margin-top:12px;">A seat is holding for you in an eight-manager dynasty league. Aqua vs Orange. Rosters carry over. Receipts are forever.</div>
</td>
</tr>
<tr>
<td style="padding:24px 32px 0 32px;">
<table role="presentation" width="100%%" cellpadding="0" cellspacing="0" border="0" style="width:100%%; border:1px solid #26305A; border-radius:4px; background-color:#131B3B;">
<tr>
<td style="padding:16px 20px; border-bottom:1px solid #26305A;">
<span style="display:inline-block; width:88px; color:#8995B8; font-family:'SFMono-Regular',Consolas,Menlo,monospace; font-size:11px; letter-spacing:0.06em; text-transform:uppercase; vertical-align:top;">DRAFT</span>
<span style="color:#F7F4EA; font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif; font-size:14px;">%s &middot; %s</span>
</td>
</tr>
<tr>
<td style="padding:16px 20px; border-bottom:1px solid #26305A;">
<span style="display:inline-block; width:88px; color:#8995B8; font-family:'SFMono-Regular',Consolas,Menlo,monospace; font-size:11px; letter-spacing:0.06em; text-transform:uppercase; vertical-align:top;">VENUE</span>
<span style="color:#F7F4EA; font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif; font-size:14px;">During the Dolphins preseason game &mdash; bring both screens.</span>
</td>
</tr>
<tr>
<td style="padding:16px 20px;">
<span style="display:inline-block; width:88px; color:#8995B8; font-family:'SFMono-Regular',Consolas,Menlo,monospace; font-size:11px; letter-spacing:0.06em; text-transform:uppercase; vertical-align:top;">YOUR KEY</span>
<span style="color:#F7F4EA; font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif; font-size:14px;">Sign in with Google as %s</span>
</td>
</tr>
</table>
</td>
</tr>
<tr>
<td align="center" style="padding:28px 32px 0 32px;">
<table role="presentation" cellpadding="0" cellspacing="0" border="0">
<tr>
<td style="border-radius:2px; background-color:#38E8FF;">
<a href="%s" style="display:inline-block; padding:14px 30px; color:#070A16; font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif; font-size:14px; font-weight:800; letter-spacing:0.04em; text-transform:uppercase; text-decoration:none; border-radius:2px;">CLAIM YOUR SEAT &rarr;</a>
</td>
</tr>
</table>
</td>
</tr>
<tr>
<td style="padding:28px 32px 0 32px;">
<div style="color:#8995B8; font-family:'SFMono-Regular',Consolas,Menlo,monospace; font-size:11px; letter-spacing:0.08em; text-transform:uppercase; margin-bottom:12px;">NEXT STEPS //</div>
<table role="presentation" width="100%%" cellpadding="0" cellspacing="0" border="0">
<tr><td style="padding:5px 0; color:#C2CAE1; font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif; font-size:14px;"><span style="color:#38E8FF; font-family:'SFMono-Regular',Consolas,Menlo,monospace; font-weight:700;">1.</span>&nbsp; Claim your seat</td></tr>
<tr><td style="padding:5px 0; color:#C2CAE1; font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif; font-size:14px;"><span style="color:#38E8FF; font-family:'SFMono-Regular',Consolas,Menlo,monospace; font-weight:700;">2.</span>&nbsp; Rename your team</td></tr>
<tr><td style="padding:5px 0; color:#C2CAE1; font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif; font-size:14px;"><span style="color:#38E8FF; font-family:'SFMono-Regular',Consolas,Menlo,monospace; font-weight:700;">3.</span>&nbsp; Build your Big Board</td></tr>
<tr><td style="padding:5px 0; color:#C2CAE1; font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif; font-size:14px;"><span style="color:#38E8FF; font-family:'SFMono-Regular',Consolas,Menlo,monospace; font-weight:700;">4.</span>&nbsp; Read the Rules page</td></tr>
</table>
</td>
</tr>
<tr>
<td style="padding:28px 32px 32px 32px;">
<div style="border-top:1px solid #26305A; padding-top:20px; color:#C2CAE1; font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif; font-size:14px;">&mdash; The Commissioner</div>
<div style="color:#5C6690; font-family:'SFMono-Regular',Consolas,Menlo,monospace; font-size:10px; letter-spacing:0.04em; text-transform:uppercase; margin-top:10px;">GRIDIRON 2000 &middot; Eight seats. One trophy. Permanent group-chat evidence.</div>
</td>
</tr>
</table>
</td>
</tr>
</table>
</body>
</html>
`

// AdminSendInvite adds email to the invite list and, when SMTP is
// configured, sends it the warm invite template. Without SMTP credentials
// it still adds the invite and reports sent=false so the console can offer
// a mailto: link instead.
func (s *Service) AdminSendInvite(r *http.Request, email string) (bool, error) {
	if err := s.requireCommissioner(r); err != nil {
		return false, err
	}
	email = strings.ToLower(strings.TrimSpace(email))
	if err := s.store.AddInvite(email); err != nil {
		return false, err
	}
	subject, text, htmlBody := s.InviteEmailTemplate(email)
	config := mailer.FromEnv()
	if !config.Enabled() {
		return false, nil
	}
	err := config.Send(email, subject, text, htmlBody)
	return true, err
}

// AdminRemoveInvite removes a stored invite. Environment-pinned emails stay.
func (s *Service) AdminRemoveInvite(r *http.Request, email string) error {
	if err := s.requireCommissioner(r); err != nil {
		return err
	}
	return s.store.RemoveInvite(email)
}

// AdminReleaseSeat unbinds whoever holds the team seat.
func (s *Service) AdminReleaseSeat(r *http.Request, teamID string) (Team, error) {
	if err := s.requireCommissioner(r); err != nil {
		return Team{}, err
	}
	if err := s.store.ReleaseSeat(teamID); err != nil {
		return Team{}, err
	}
	return s.teamView(s.store.Snapshot(), teamID), nil
}

// AdminResetDraft clears picks and ready flags. Seats and boards survive.
func (s *Service) AdminResetDraft(r *http.Request) error {
	if err := s.requireCommissioner(r); err != nil {
		return err
	}
	return s.store.ResetDraft()
}

// AdminResetLeague clears picks, seats, ready flags, and boards.
func (s *Service) AdminResetLeague(r *http.Request) error {
	if err := s.requireCommissioner(r); err != nil {
		return err
	}
	return s.store.ResetLeague()
}

// AdminRenameTeam overrides a team's display name. An empty name restores
// the default name.
func (s *Service) AdminRenameTeam(r *http.Request, teamID, name string) (Team, error) {
	if err := s.requireCommissioner(r); err != nil {
		return Team{}, err
	}
	if err := s.store.SetTeamName(teamID, name); err != nil {
		return Team{}, err
	}
	return s.teamView(s.store.Snapshot(), teamID), nil
}

// AdminRandomizeDraftOrder draws a new draft order for the eight teams with
// a cryptographic Fisher-Yates shuffle. It fails once picks exist; reset the
// draft first to redraw the order.
func (s *Service) AdminRandomizeDraftOrder(r *http.Request) error {
	if err := s.requireCommissioner(r); err != nil {
		return err
	}
	order := defaultTeamIDs()
	for i := len(order) - 1; i > 0; i-- {
		j, err := rand.Int(rand.Reader, big.NewInt(int64(i+1)))
		if err != nil {
			return err
		}
		order[i], order[j.Int64()] = order[j.Int64()], order[i]
	}
	if err := s.store.SetDraftOrder(order); err != nil {
		return err
	}
	// N4: notify every seated member the order is drawn (spec section 3,
	// N4). A no-op when notifications are not wired.
	s.notifyDraftOrderDrawn(order)
	return nil
}

// AdminSetScoring overrides one scoring rule's point value. It requires
// commissioner access and fails once scoring locks for the season.
func (s *Service) AdminSetScoring(r *http.Request, key, rawValue string) (ScoringRule, error) {
	if err := s.requireCommissioner(r); err != nil {
		return ScoringRule{}, err
	}
	if s.ScoringLocked(time.Now()) {
		return ScoringRule{}, fmt.Errorf("scoring is locked for the season")
	}
	rule, ok := scoringRuleByKey(key)
	if !ok {
		return ScoringRule{}, fmt.Errorf("unknown scoring key %q", key)
	}
	points, err := strconv.ParseFloat(strings.TrimSpace(rawValue), 64)
	if err != nil {
		return ScoringRule{}, fmt.Errorf("enter a number")
	}
	if err := s.store.SetScoringValue(key, points); err != nil {
		return ScoringRule{}, err
	}
	rule.Points = points
	return rule, nil
}

// AdminResetScoring clears every scoring override, restoring the default
// rules. It requires commissioner access and fails once scoring locks.
func (s *Service) AdminResetScoring(r *http.Request) error {
	if err := s.requireCommissioner(r); err != nil {
		return err
	}
	if s.ScoringLocked(time.Now()) {
		return fmt.Errorf("scoring is locked for the season")
	}
	return s.store.ResetScoring()
}

// AdminPauseClock stops the running pick clock. Picks stay allowed; only
// the timer and auto-pick stop (see the pick-clock spec, section 4.4).
func (s *Service) AdminPauseClock(r *http.Request) error {
	if err := s.requireCommissioner(r); err != nil {
		return err
	}
	return s.store.PauseClock(s.clock())
}

// AdminResumeClock restarts the pick clock from its stored remaining time,
// or grants a full duration when nothing was captured. In demo mode this
// doubles as "start the clock" for rehearsals: demo never self-arms (see
// clockTick), so resume is the only way to begin one.
func (s *Service) AdminResumeClock(r *http.Request) error {
	if err := s.requireCommissioner(r); err != nil {
		return err
	}
	state := s.store.Snapshot()
	return s.store.ResumeClock(s.clock(), s.pickClock(state))
}

// AdminExtendClock adds secs to the running deadline, clamped to
// [now+MinPickClock, now+MaxPickClock]. It is current-pick mercy only: a
// paused or unarmed clock has no running deadline to extend.
func (s *Service) AdminExtendClock(r *http.Request, secs int) error {
	if err := s.requireCommissioner(r); err != nil {
		return err
	}
	return s.store.ExtendClock(s.clock(), time.Duration(secs)*time.Second)
}

// AdminSetClockSeconds overrides the persisted pick-clock duration,
// clamped to [MinPickClock, MaxPickClock]. It applies starting with the
// next arm; the running deadline stays untouched (one control, one
// effect — ExtendClock and PauseClock cover current-pick mercy).
func (s *Service) AdminSetClockSeconds(r *http.Request, secs int) error {
	if err := s.requireCommissioner(r); err != nil {
		return err
	}
	return s.store.SetClockDuration(secs)
}

// AdminSetAutopick toggles any seat's Autopick flag on the commissioner's
// authority, rather than the manager's own (compare ToggleAutopick).
func (s *Service) AdminSetAutopick(r *http.Request, teamID string, on bool) error {
	if err := s.requireCommissioner(r); err != nil {
		return err
	}
	return s.store.SetAutopick(teamID, on)
}

// AdminForceAutopick fires an immediate auto-pick for the on-clock seat,
// with provenance "commissioner". It works while the clock is paused (this
// is the whole point: the commissioner is the human confirming a pick
// should happen right now) and is rejected before the draft is live or
// once every roster slot is filled.
func (s *Service) AdminForceAutopick(r *http.Request) (DraftPick, Player, Team, error) {
	if err := s.requireCommissioner(r); err != nil {
		return DraftPick{}, Player{}, Team{}, err
	}
	now := s.clock()
	if !s.draftIsLive(now) {
		return DraftPick{}, Player{}, Team{}, fmt.Errorf("the draft room is not open yet")
	}
	state := s.store.Snapshot()
	totalPicks := len(defaultTeams()) * DraftRounds
	number := len(state.Picks) + 1
	if number > totalPicks {
		return DraftPick{}, Player{}, Team{}, fmt.Errorf("the draft is complete")
	}
	teamID := teamOnClock(state.DraftOrder, number)
	playerID, ok := s.autopickChoice(state, teamID)
	if !ok {
		return DraftPick{}, Player{}, Team{}, fmt.Errorf("no undrafted player is available to auto-pick")
	}
	nextDeadline := time.Time{}
	if !state.ClockPaused && number < totalPicks {
		nextDeadline = now.Add(s.pickClock(state))
	}
	pick, err := s.store.AutoPick(teamID, playerID, "commissioner", number, state.ClockDeadline, now, nextDeadline)
	if err != nil {
		return DraftPick{}, Player{}, Team{}, err
	}
	// N6: notify the seat's manager that a pick fired on their behalf,
	// skipping a manager who was CONNECTED at pick time (spec section 3,
	// N6). state is the pre-fire snapshot already read above, matching the
	// world the commissioner's action actually saw — the same pattern
	// clockTick's own AutoPick call site uses (draftclock.go). This was
	// the one N6 call site WP-E3 left unwired: notifyAutopickMade itself
	// is provenance-agnostic and already fires for MadeBy=="commissioner"
	// from clockTick's path, but nothing called it from here.
	s.notifyAutopickMade(state, pick, "commissioner", now)
	return pick, s.pool().byID[playerID], s.teamByID(teamID), nil
}
