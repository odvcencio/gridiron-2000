package league

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
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
	"m31labs.dev/gosx/auth"
)

// AdminData assembles the commissioner console: seat claims, invites, and
// league state counters. The page itself renders a restricted notice for
// non-commissioners; every action re-checks authority server-side.
func (s *Service) AdminData(r *http.Request) map[string]any {
	state := s.store.Snapshot()
	seats := make([]map[string]any, 0, len(s.Teams()))
	for _, team := range s.Teams() {
		member := memberForTeam(state.Members, team.ID)
		item := s.teamMap(s.teamView(state, team.ID))
		item["email"] = member.Email
		item["ready"] = state.Ready[team.ID]
		// co_email (registration wave, build item 4): the admin seats grid
		// shows both managers, not just the primary — teamMembers returns
		// primary first, then the co-manager if one is bound.
		item["co_email"] = ""
		item["has_co"] = false
		for _, candidate := range teamMembers(state.Members, team.ID) {
			if candidate.Role == "co" {
				item["co_email"] = candidate.Email
				item["has_co"] = true
			}
		}
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
	// domainGate mirrors rulesMembershipMap's fix (scoring.go): "no invite
	// list" alone does not mean any Google account may claim a seat when a
	// membership.allowed_domain gate is configured — the admin console's
	// own "Who may claim a seat" banner made exactly that false claim for
	// a domain-gated league (SK launch-prep dry run finding) until this.
	domainGate := strings.TrimSpace(s.cfg.Membership.AllowedDomain)
	return map[string]any{
		"viewer":              s.Viewer(r),
		"is_commissioner":     s.IsCommissioner(r),
		"seats":               seats,
		"invites":             invites,
		"invite_count":        len(invites),
		"league_open":         domainGate == "" && len(invites) == 0,
		"league_domain_gated": domainGate != "",
		"league_domain":       domainGate,
		"member_count":        len(state.Members),
		"pick_count":          len(state.Picks),
		"seat_count":          len(s.Teams()),
		// ready_count feeds the masthead's "N/seats ready" line: the same
		// aggregate the draft room shows, so the commissioner reads seat
		// readiness at a glance without scrolling to 01 // SEATS.
		"ready_count":      readyCount(state.Ready),
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
		"mail":   s.notifyMailMap(now),
		"league": s.leagueMap(),
		// config_source shows which league.json is live (productization
		// spec section 3.4): "defaults" on an unconfigured checkout, or
		// "file:<path>" once one loads. is_default_config drives the
		// admin console's "running the built-in reference league" banner.
		"config_source":     s.cfg.Source,
		"is_default_config": s.cfg.Source == "defaults",
		// Roster shape panel (roster-shape-editor spec): the live shape, its
		// computed starters/bench/rounds line, whether an override is active
		// (drives the Reset button), and whether the draft has already
		// started (drives the read-only lock view).
		"roster_shape": s.rosterShapeMap(state),
		// Announcements panel (league-announcements spec): the full stored
		// feed (newest first), for per-item delete. GSX conditions cannot
		// call .length on server data, so announcements_empty ships as its
		// own bool (see DashboardData's transactions_empty precedent).
		"announcements":       s.announcementAdminMaps(state),
		"announcements_empty": len(state.Announcements) == 0,
	}
}

// rosterShapeMap renders the admin console's Roster shape panel data: every
// slot in engine order with its live count, the computed starters/bench/
// rounds line, whether a commissioner override is active, and whether the
// draft has already started (picks lock the shape — see
// Store.SetRosterOverride).
func (s *Service) rosterShapeMap(state PersistedState) map[string]any {
	roster := CurrentRoster()
	slots := make([]map[string]any, 0, len(slotTable))
	for _, slot := range slotTable {
		slots = append(slots, map[string]any{
			"key":        slot.Key,
			"field_name": "slot_" + slot.Key,
			"count":      roster.Slots[slot.Key],
		})
	}
	// Reserve/Limits editor rows (SK spec): one row per real player
	// position, so the form always offers the full, fixed key set
	// regardless of whether the active shape currently uses it — the same
	// "every key, current count" shape the starter slots grid above uses.
	reserveRows := make([]map[string]any, 0, len(playerPoolPositions))
	limitRows := make([]map[string]any, 0, len(playerPoolPositions))
	for _, position := range playerPoolPositions {
		reserveRows = append(reserveRows, map[string]any{
			"key": position, "field_name": "reserve_" + position, "count": roster.Reserve[position],
		})
		limitRows = append(limitRows, map[string]any{
			"key": position, "field_name": "limit_" + position, "count": roster.Limits[position],
		})
	}
	return map[string]any{
		"slots":         slots,
		"bench":         roster.Bench,
		"starters":      roster.Starters(),
		"rounds":        roster.Total(),
		"has_override":  state.RosterOverride != nil,
		"draft_started": len(state.Picks) > 0,
		// Zones/Limits (roster-ops SK spec): additive fields onto the same
		// panel. reserve_total counts toward rounds; ir sits outside it.
		"reserve_rows":  reserveRows,
		"reserve_total": roster.ReserveTotal(),
		"ir":            roster.IR,
		"limit_rows":    limitRows,
	}
}

// announcementAdminMaps renders the Announcements panel's current list
// (newest first, as stored) for the admin console's per-item delete.
func (s *Service) announcementAdminMaps(state PersistedState) []map[string]any {
	out := make([]map[string]any, 0, len(state.Announcements))
	for _, a := range state.Announcements {
		out = append(out, map[string]any{
			"id":        a.ID,
			"body":      a.Body,
			"posted_by": a.PostedBy,
			"posted_at": a.PostedAt.Format("Jan 2, 3:04 PM MST"),
		})
	}
	return out
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

// commissionerProvenance renders the acting commissioner's identity for an
// admin-authored record (announcements today): the signed-in email, or "the
// commissioner" in demo mode or when no email is on hand — mirroring
// Viewer's own signed-in/demo split (service.go), the closest existing
// precedent for "who is this request" in the package.
func (s *Service) commissionerProvenance(r *http.Request) string {
	if user, ok := auth.Current(r); ok && strings.TrimSpace(user.Email) != "" {
		return strings.ToLower(strings.TrimSpace(user.Email))
	}
	return "the commissioner"
}

// AdminSetRosterShape applies a commissioner-chosen roster shape override
// (roster-shape-editor spec): persists it (Store.SetRosterOverride, which
// validates and enforces the post-first-pick lock) and, on success, updates
// the runtime accessors (CurrentRoster/CurrentDraftRounds) immediately.
func (s *Service) AdminSetRosterShape(r *http.Request, o RosterOverride) (RosterPreset, error) {
	if err := s.requireCommissioner(r); err != nil {
		return RosterPreset{}, err
	}
	if err := s.store.SetRosterOverride(o); err != nil {
		return RosterPreset{}, err
	}
	preset := rosterOverridePreset(o)
	setRosterShape(preset)
	return preset, nil
}

// AdminResetRosterShape drops the commissioner's roster-shape override,
// restoring the config-resolved default, and updates the runtime
// accessors to match.
func (s *Service) AdminResetRosterShape(r *http.Request) error {
	if err := s.requireCommissioner(r); err != nil {
		return err
	}
	if err := s.store.ClearRosterOverride(); err != nil {
		return err
	}
	clearRosterShape()
	return nil
}

// TrimUnclaimedSeats applies the SK unclaimed-seat spec's T-1hr commissioner
// action: every seat with no member (primary or co-manager) is dropped, and
// the league locks to its claimed team count for the rest of the season —
// schedule generation, draft order, and waiver priority all read
// defaultTeams() from this point on and see only the kept teams. Draft
// rounds are unaffected: they derive from the roster shape
// (CurrentDraftRounds), never from team count. Commissioner-only; rejected
// once the draft has picks on the tape or the claimed count would fall
// below the engine's team floor (Store.TrimUnclaimedSeats).
func (s *Service) TrimUnclaimedSeats(r *http.Request) (kept []Team, removed []string, err error) {
	if err := s.requireCommissioner(r); err != nil {
		return nil, nil, err
	}
	kept, removed, err = s.store.TrimUnclaimedSeats()
	if err != nil {
		return nil, nil, err
	}
	// Two writes, deliberately: applySeatTrim updates the package-level
	// override every non-Service call site reads (store.go, roster.go's
	// draftComplete, draftclock.go), and setTeams updates this Service
	// instance's own teams field, which every Service-method read site
	// reads through Teams() instead. Production only ever runs one
	// Service instance, so both agree from this call onward.
	applySeatTrim(kept)
	s.setTeams(kept)
	return kept, removed, nil
}

// AdminPostAnnouncement posts a new league announcement (league-
// announcements spec). alsoEmail fires the N11 commissioner-broadcast
// email to every seated member, respecting each member's own notify
// preference (notifyAnnouncement, notifications.go); it is a no-op when
// notifications are not wired, matching every other notify hook.
func (s *Service) AdminPostAnnouncement(r *http.Request, body string, alsoEmail bool) (Announcement, error) {
	if err := s.requireCommissioner(r); err != nil {
		return Announcement{}, err
	}
	postedBy := s.commissionerProvenance(r)
	announcement, err := s.store.PostAnnouncement(body, postedBy, s.clock())
	if err != nil {
		return Announcement{}, err
	}
	if alsoEmail {
		s.notifyAnnouncement(announcement)
	}
	return announcement, nil
}

// AdminDeleteAnnouncement removes one announcement by ID.
func (s *Service) AdminDeleteAnnouncement(r *http.Request, id string) error {
	if err := s.requireCommissioner(r); err != nil {
		return err
	}
	return s.store.DeleteAnnouncement(id)
}

// AdminAddInvite adds a manager email to the invite list.
func (s *Service) AdminAddInvite(r *http.Request, email string) error {
	if err := s.requireCommissioner(r); err != nil {
		return err
	}
	return s.store.AddInvite(email)
}

// inviteBlurb renders the invite's opening description: the config's own
// copy.invite_blurb when set, otherwise a generated line from the team
// count, mode, and division set (spec section 3.2: "derives from count,
// mode, and divisions"). With the reference league's config (8 teams,
// DYNASTY, Aqua/Orange) this reproduces "an eight-manager dynasty league
// split into the Aqua and Orange divisions" exactly.
func (s *Service) inviteBlurb() string {
	if blurb := strings.TrimSpace(s.cfg.Copy.InviteBlurb); blurb != "" {
		return blurb
	}
	word := countWord(len(s.Teams()))
	blurb := fmt.Sprintf("%s %s-manager %s league", article(word), word, strings.ToLower(s.cfg.ModeLabel))
	if divisions := s.divisionList(); len(divisions) > 0 {
		blurb += " split into the " + strings.Join(divisions, " and ") + " divisions"
	}
	return blurb
}

// InviteEmailTemplate builds the subject, plain-text body, and HTML body of
// the invite email sent to one manager. It draws the draft date and time
// from the live draft summary, so the copy always matches the console, and
// every identity fact (wordmark, short code, tagline, blurb, venue,
// footer) comes from config (productization spec section 5.2) — an empty
// copy.venue_line drops the venue clause and the HTML VENUE row entirely
// rather than inventing one for a league that has none. The HTML body
// carries the same facts as the text body, dressed in the league's
// Neo-Retro Stadium OS look; the text body is unchanged in shape from the
// plain-text-only era, since its mailto: use depends on the wording.
func (s *Service) InviteEmailTemplate(email string) (subject, text, htmlBody string) {
	draft := s.draftSummary(time.Now())
	shortDate, _ := draft["date"].(string)
	longDate, _ := draft["long_date"].(string)
	draftTime, _ := draft["time"].(string)
	leagueURL := s.leagueURL()
	blurb := s.inviteBlurb()

	venueClause := ""
	if venue := strings.TrimSpace(s.cfg.Copy.VenueLine); venue != "" {
		venueClause = " " + venue
	}
	// carryoverLine asserted "rosters carry over season to season"
	// unconditionally until this fix — true for the flagship's own
	// dynasty format, false for any other mode.ModeLabel (SK's "REDRAFT",
	// or a future keeper/dynasty-hybrid league): this platform now runs
	// more than one format, so the invite copy must not assume the
	// flagship's own. Any label other than exactly "DYNASTY" omits the
	// claim entirely rather than guess at what a redraft or keeper league
	// wants said instead — the same "omit rather than invent" rule
	// copy.venue_line's empty-string case already follows.
	carryoverLine := ""
	if strings.EqualFold(s.cfg.ModeLabel, "DYNASTY") {
		carryoverLine = "\n\nRosters carry over season to season, so draft like it matters."
	}

	subject = fmt.Sprintf("You're invited: %s — %s league, draft %s", s.cfg.Name, strings.ToLower(s.cfg.ModeLabel), shortDate)
	text = fmt.Sprintf(`Hi there,

You've got a seat waiting in %s, %s.

The startup snake draft is %s at %s.%s

Here's what to do before then:
  1. Open %s
  2. Sign in with Google using this address (%s is on the list)
  3. Claim your seat and rename your team
  4. Build your draft board before the clock starts

The full scoring system is on the Rules page.%s

— The Commissioner`, s.cfg.Name, blurb, longDate, draftTime, venueClause, leagueURL, email, carryoverLine)
	htmlBody = s.inviteEmailHTML(shortDate, longDate, draftTime, leagueURL, email, blurb)
	return subject, text, htmlBody
}

// inviteEmailHTML renders the designed HTML invite body: a single 600px
// table with every style inline and no external assets, so it survives
// clipping and dark/light rendering across mail clients. Every field is
// operator- or attacker-influenced (email via the invite form, leagueURL
// via the environment, name/short_code/tagline/blurb/venue/footer via
// league.json) and is HTML-escaped before insertion.
func (s *Service) inviteEmailHTML(shortDate, longDate, draftTime, leagueURL, email, blurb string) string {
	venueRow := ""
	if venue := strings.TrimSpace(s.cfg.Copy.VenueLine); venue != "" {
		venueRow = fmt.Sprintf(inviteEmailVenueRowTemplate, html.EscapeString(venue))
	}
	footerLine := s.cfg.Copy.FooterLine
	footerJoke := s.cfg.Name
	if footerLine != "" {
		footerJoke += " " + emDash + " " + footerLine
	}
	return fmt.Sprintf(inviteEmailHTMLTemplate,
		html.EscapeString(s.cfg.Name),
		html.EscapeString(s.cfg.ShortCode),
		html.EscapeString(s.cfg.Name),
		html.EscapeString(s.cfg.Tagline),
		html.EscapeString(shortDate),
		html.EscapeString(blurb),
		html.EscapeString(longDate),
		html.EscapeString(draftTime),
		venueRow,
		html.EscapeString(email),
		leagueURL,
		html.EscapeString(footerJoke),
	)
}

// emDash is the invite footer's separator ("Name — footer line"), spelled
// once here instead of as a raw literal buried in a format call.
const emDash = "—"

// inviteEmailVenueRowTemplate is the invite HTML's optional VENUE row,
// spliced into inviteEmailHTMLTemplate's %s slot only when
// copy.venue_line is set (spec section 3.2, 5.2).
const inviteEmailVenueRowTemplate = `<tr>
<td style="padding:16px 20px; border-bottom:1px solid #26305A;">
<span style="display:inline-block; width:88px; color:#8995B8; font-family:'SFMono-Regular',Consolas,Menlo,monospace; font-size:11px; letter-spacing:0.06em; text-transform:uppercase; vertical-align:top;">VENUE</span>
<span style="color:#F7F4EA; font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif; font-size:14px;">%s</span>
</td>
</tr>
`

// inviteEmailHTMLTemplate is the invite email's HTML body: a single
// 600px-max table, all styles inline, system font stack only (no external
// assets or webfonts), dark ground with light-enough text to survive a
// client forcing light mode. %s placeholders fill, in order: the page
// title (name), the short-code badge, the wordmark, the tagline, the short
// draft date ("SAT · AUG 22" style) for the signal line, the invite blurb,
// the short draft date again (DRAFT row), the long draft date, the draft
// time, the optional VENUE row (empty string when copy.venue_line is
// unset), the invited email, the league URL (CTA href), and the footer
// joke ("{name} — {footer_line}", or just "{name}" when footer_line is
// empty).
const inviteEmailHTMLTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>%s</title>
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
<table role="presentation" cellpadding="0" cellspacing="0" border="0" style="border:1px solid #38E8FF; border-radius:2px;"><tr><td style="width:40px; height:40px; text-align:center; vertical-align:middle; color:#38E8FF; font-family:'SFMono-Regular',Consolas,Menlo,monospace; font-size:13px; font-weight:700;">%s</td></tr></table>
</td>
<td valign="middle">
<div style="color:#F7F4EA; font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif; font-size:19px; font-weight:800; letter-spacing:-0.02em; text-transform:uppercase;">%s</div>
<div style="color:#8995B8; font-family:'SFMono-Regular',Consolas,Menlo,monospace; font-size:11px; letter-spacing:0.08em; text-transform:uppercase; margin-top:3px;">%s</div>
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
<div style="color:#C2CAE1; font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif; font-size:15px; line-height:1.6; margin-top:12px;">A seat is holding for you in %s. Rosters carry over. Receipts are forever.</div>
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
%s<tr>
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
<div style="color:#5C6690; font-family:'SFMono-Regular',Consolas,Menlo,monospace; font-size:10px; letter-spacing:0.04em; text-transform:uppercase; margin-top:10px;">%s</div>
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

// RenameTeam sets teamID's display name for the seat's own manager, or
// for the commissioner acting on any seat — the canSetAvatar authority
// model: team identity (name, avatar, badge) is the manager's own to
// shape, unlike the Admin* league-management actions above. Validation
// (trim, 40-character cap, empty clears the override back to the config
// name) lives in Store.SetTeamName, shared with the commissioner path.
func (s *Service) RenameTeam(r *http.Request, teamID, name string) (Team, error) {
	teamID = strings.TrimSpace(teamID)
	if !knownTeam(teamID) {
		return Team{}, fmt.Errorf("unknown team %q", teamID)
	}
	if !s.canSetAvatar(r, teamID) {
		return Team{}, errors.New("only the seat's manager or the commissioner can rename this team")
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

// AdminUndoPick removes the most recent pick and re-arms the clock for
// the reopened slot, using the same duration resolution the manual pick
// flow uses (see MakePick and pickClock). UndoLastPick itself skips the
// re-arm while the clock is paused, so this always passes a computed
// deadline; it is simply unused in that case (see AdminResumeClock for
// the same "state, then one store call" shape).
func (s *Service) AdminUndoPick(r *http.Request) error {
	if err := s.requireCommissioner(r); err != nil {
		return err
	}
	now := s.clock()
	state := s.store.Snapshot()
	return s.store.UndoLastPick(now.Add(s.pickClock(state)))
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
	totalPicks := len(s.Teams()) * CurrentDraftRounds()
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

// AdminGenerateSchedule generates and persists the first regular-season
// schedule from the league's current team list and divisions
// (competition-formats spec section 2.3). weeks is the regular-season
// length; league weeks map 1:1 onto NFL weeks. startWeek <= 0 defaults to
// 1. seed == 0 draws a fresh seed from crypto/rand; a nonzero seed is
// stored as given, matching a commissioner-supplied schedule.seed config
// value. It fails once a schedule already exists — use
// AdminRegenerateSchedule to redraw one.
func (s *Service) AdminGenerateSchedule(r *http.Request, weeks, startWeek int, seed int64) (SeasonSchedule, error) {
	if err := s.requireCommissioner(r); err != nil {
		return SeasonSchedule{}, err
	}
	state := s.store.Snapshot()
	if state.Schedule != nil {
		return SeasonSchedule{}, fmt.Errorf("a schedule already exists; regenerate it instead")
	}
	return s.buildAndStoreSchedule(weeks, startWeek, seed)
}

// AdminRegenerateSchedule redraws the schedule with a fresh seed. The
// section 2.3 guard requires both: now < seasonStartAt(), and no matchup
// in the current schedule has Final == true — once week 1 kicks off the
// schedule is immutable, mirroring ScoringLocked. weeks <= 0 or startWeek
// <= 0 reuse the existing schedule's values.
func (s *Service) AdminRegenerateSchedule(r *http.Request, weeks, startWeek int) (SeasonSchedule, error) {
	if err := s.requireCommissioner(r); err != nil {
		return SeasonSchedule{}, err
	}
	if !s.clock().Before(seasonStartAt()) {
		return SeasonSchedule{}, fmt.Errorf("the schedule is locked once the season starts")
	}
	state := s.store.Snapshot()
	if state.Schedule == nil {
		return SeasonSchedule{}, fmt.Errorf("no schedule exists yet; generate one first")
	}
	if scheduleHasFinalMatchup(*state.Schedule) {
		return SeasonSchedule{}, fmt.Errorf("the schedule is locked once a matchup has been scored")
	}
	if weeks <= 0 {
		weeks = len(state.Schedule.Weeks)
	}
	if startWeek <= 0 {
		startWeek = state.Schedule.StartWeek
	}
	seed, err := randomScheduleSeed()
	if err != nil {
		return SeasonSchedule{}, err
	}
	return s.buildAndStoreSchedule(weeks, startWeek, seed)
}

// buildAndStoreSchedule runs the pure GenerateSchedule against the
// league's current team list and divisions, stamps GeneratedAt from the
// service clock (GenerateSchedule itself never touches the clock), and
// persists the result.
func (s *Service) buildAndStoreSchedule(weeks, startWeek int, seed int64) (SeasonSchedule, error) {
	if startWeek <= 0 {
		startWeek = 1
	}
	if seed == 0 {
		drawn, err := randomScheduleSeed()
		if err != nil {
			return SeasonSchedule{}, err
		}
		seed = drawn
	}
	sched, err := GenerateSchedule(ScheduleParams{
		Season:    seasonStartAt().Year(),
		TeamIDs:   teamIDList(s.Teams()),
		Divisions: teamDivisionMap(s.Teams()),
		StartWeek: startWeek,
		Weeks:     weeks,
		Seed:      seed,
	})
	if err != nil {
		return SeasonSchedule{}, err
	}
	sched.GeneratedAt = s.clock().UTC()
	if err := s.store.SetSchedule(sched); err != nil {
		return SeasonSchedule{}, err
	}
	return sched, nil
}

// scheduleHasFinalMatchup reports whether any matchup in sch has scored
// (Final == true) — the section 2.3 regeneration guard's second half.
func scheduleHasFinalMatchup(sch SeasonSchedule) bool {
	for _, wk := range sch.Weeks {
		for _, m := range wk.Matchups {
			if m.Final {
				return true
			}
		}
	}
	return false
}

// teamIDList and teamDivisionMap adapt the league's team list to
// GenerateSchedule's ScheduleParams shape. A team with an empty Division
// is omitted from the map, matching "empty = none" (section 2.2).
func teamIDList(teams []Team) []string {
	ids := make([]string, len(teams))
	for i, team := range teams {
		ids[i] = team.ID
	}
	return ids
}

func teamDivisionMap(teams []Team) map[string]string {
	out := make(map[string]string, len(teams))
	for _, team := range teams {
		if team.Division != "" {
			out[team.ID] = team.Division
		}
	}
	return out
}

// randomScheduleSeed draws a non-negative int64 seed from crypto/rand
// (section 2.3: "the seed comes from schedule.seed config when set, else
// from crypto/rand, and is stored").
func randomScheduleSeed() (int64, error) {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return 0, err
	}
	return int64(binary.BigEndian.Uint64(buf[:]) &^ (1 << 63)), nil
}
