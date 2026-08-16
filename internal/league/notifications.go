// Notification catalog, key builders, preference precedence, and the
// N1-N7 template builders (design spec "Notification Email System",
// sections 3, 6.4, 7.1). The catalog registers all 13 entries so the
// default matrix stays total (spec section 9, test 18); only N1-N7 carry
// template builders here. N8-N13's evaluation and templates land in later
// work packages — see notifierTick's TODO markers.
package league

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"gridiron-2000/internal/emailkit"
	"gridiron-2000/internal/notify"
)

// Notification preference categories (spec section 3). Every notification
// type belongs to exactly one of these eight; /settings (WP-E5) groups
// toggles by category, not by individual type.
const (
	categoryOnboarding     = "onboarding"
	categoryDraftReminders = "draft_reminders"
	categoryDraftLive      = "draft_live"
	categoryDraftRecap     = "draft_recap"
	categoryPickem         = "pickem"
	categoryWeeklyRecap    = "weekly_recap"
	categoryLeagueNews     = "league_news"
	categoryBroadcast      = "broadcast"
)

// notificationCategories lists the eight-category set the catalog matrix
// test pins every entry's Category against (spec section 3, section 9 test
// 18).
var notificationCategories = []string{
	categoryOnboarding, categoryDraftReminders, categoryDraftLive, categoryDraftRecap,
	categoryPickem, categoryWeeklyRecap, categoryLeagueNews, categoryBroadcast,
}

// Urgency levels (spec section 3). Only urgencyLow entries (N9, N12, N13)
// observe quiet hours; that gate lands with those entries' evaluation
// logic in WP-E4/E5.
const (
	urgencyLow    = "low"
	urgencyNormal = "normal"
	urgencyHigh   = "high"
)

// categoryLabel renders each category's human-readable name for the
// shell's PrefLine ("{label} alerts: on.", spec section 4.2, section 5's
// worked example: "Draft-room alerts: on.").
var categoryLabel = map[string]string{
	categoryOnboarding:     "Onboarding",
	categoryDraftReminders: "Draft-reminder",
	categoryDraftLive:      "Draft-room",
	categoryDraftRecap:     "Draft-recap",
	categoryPickem:         "Pick'em",
	categoryWeeklyRecap:    "Weekly-recap",
	categoryLeagueNews:     "League-news",
	categoryBroadcast:      "Broadcast",
}

// Brand constants reused verbatim from the invite email (admin.go:120-152)
// until league.json's config surface lands (productization WP1). Every
// read through these — and through leagueURL() below — is a documented
// cfg.* follow-up: cfg.Name, cfg.ShortCode, cfg.Tagline, and cfg.URL will
// replace them in WP-E5 without changing any template's shape.
const (
	leagueWordmark   = "GRIDIRON 2000"
	leagueShortCode  = "G2K"
	leagueTagline    = "DYNASTY FANTASY LEAGUE"
	leagueFooterLine = "Eight seats. One trophy. Permanent group-chat evidence."
)

// leagueURL resolves the league's public URL exactly as
// InviteEmailTemplate does today (admin.go): LEAGUE_URL, falling back to
// defaultLeagueURL. TODO(productization WP1): replace with cfg.URL from
// league.json once config.go exists.
func leagueURL() string {
	url := strings.TrimSpace(os.Getenv("LEAGUE_URL"))
	if url == "" {
		url = defaultLeagueURL
	}
	return url
}

// keyContext carries every value a catalog entry's key builder might need.
// Each entry's Key func reads only the fields its own key shape uses (spec
// section 3, per entry).
type keyContext struct {
	TeamID       string
	Epoch        int64
	LeadHours    int
	DraftAt      time.Time
	OrderHash8   string
	Season       string
	Week         int
	ScoringHash8 string
	BroadcastID  string
}

// catalogEntry is one row of the notification catalog (spec section 3):
// its identity, audience category, urgency, default enablement, whether it
// carries a time/derived freshness window, and a key builder that embeds
// the recipient email into its idempotency key shape.
type catalogEntry struct {
	ID         string
	TypeKey    string
	Category   string
	Urgency    string
	Default    bool
	TimeDriven bool
	// Freshness is the representative window length for a TimeDriven entry
	// (spec section 3's per-entry freshness rule). Zero when TimeDriven is
	// false.
	Freshness time.Duration
	// Key builds this entry's idempotency key for one recipient email and
	// a context. Every implementation embeds email in the result.
	Key func(email string, ctx keyContext) string
}

// catalogEntries is the full 13-entry registry (spec section 3's summary
// matrix, verbatim). Only N1-N7 have template builders in this package;
// N8-N13 register catalog metadata and a key builder now so the pref
// precedence, ledger, and admin-matrix machinery are total from day one,
// and their template builders land in WP-E4 (N8-N11) and WP-E5 (N12-N13).
var catalogEntries = buildCatalog()

func buildCatalog() []catalogEntry {
	return []catalogEntry{
		{
			ID: "N1", TypeKey: "seat-claimed", Category: categoryOnboarding, Urgency: urgencyNormal, Default: true,
			Key: func(email string, ctx keyContext) string { return keySeatClaimed(ctx.TeamID, email) },
		},
		{
			ID: "N2", TypeKey: "draft-reminder", Category: categoryDraftReminders, Urgency: urgencyNormal, Default: true,
			TimeDriven: true, Freshness: 12 * time.Hour,
			Key: func(email string, ctx keyContext) string { return keyDraftReminder(24, ctx.DraftAt, email) },
		},
		{
			ID: "N3", TypeKey: "draft-reminder", Category: categoryDraftReminders, Urgency: urgencyHigh, Default: true,
			TimeDriven: true, Freshness: 30 * time.Minute,
			Key: func(email string, ctx keyContext) string { return keyDraftReminder(1, ctx.DraftAt, email) },
		},
		{
			ID: "N4", TypeKey: "draft-order-drawn", Category: categoryDraftReminders, Urgency: urgencyNormal, Default: true,
			Key: func(email string, ctx keyContext) string { return keyDraftOrderDrawn(ctx.OrderHash8, email) },
		},
		{
			ID: "N5", TypeKey: "on-the-clock", Category: categoryDraftLive, Urgency: urgencyHigh, Default: true,
			Key: func(email string, ctx keyContext) string { return keyOnTheClock(ctx.TeamID, ctx.Epoch, email) },
		},
		{
			ID: "N6", TypeKey: "autopick-made", Category: categoryDraftLive, Urgency: urgencyHigh, Default: true,
			Key: func(email string, ctx keyContext) string { return keyAutopickMade(ctx.TeamID, ctx.Epoch, email) },
		},
		{
			ID: "N7", TypeKey: "draft-complete", Category: categoryDraftRecap, Urgency: urgencyNormal, Default: true,
			TimeDriven: true, Freshness: 48 * time.Hour,
			Key: func(email string, ctx keyContext) string { return keyDraftComplete(ctx.OrderHash8, email) },
		},
		{
			ID: "N8", TypeKey: "pickem-reminder", Category: categoryPickem, Urgency: urgencyNormal, Default: true,
			TimeDriven: true, Freshness: 24 * time.Hour,
			Key: func(email string, ctx keyContext) string { return keyPickemRemind(ctx.Season, ctx.Week, email) },
		},
		{
			ID: "N9", TypeKey: "pickem-results", Category: categoryPickem, Urgency: urgencyLow, Default: true,
			TimeDriven: true, Freshness: 7 * 24 * time.Hour,
			Key: func(email string, ctx keyContext) string { return keyPickemResults(ctx.Season, ctx.Week, email) },
		},
		{
			ID: "N10", TypeKey: "scoring-change", Category: categoryLeagueNews, Urgency: urgencyNormal, Default: true,
			TimeDriven: true, Freshness: 15 * time.Minute,
			Key: func(email string, ctx keyContext) string { return keyScoringChange(ctx.ScoringHash8, email) },
		},
		{
			ID: "N11", TypeKey: "commissioner-broadcast", Category: categoryBroadcast, Urgency: urgencyNormal, Default: true,
			Key: func(email string, ctx keyContext) string { return keyBroadcast(ctx.BroadcastID, email) },
		},
		{
			ID: "N12", TypeKey: "season-kickoff", Category: categoryLeagueNews, Urgency: urgencyLow, Default: true,
			TimeDriven: true, Freshness: 48 * time.Hour,
			Key: func(email string, ctx keyContext) string { return keyKickoff(ctx.Season, email) },
		},
		{
			ID: "N13", TypeKey: "matchup-recap", Category: categoryWeeklyRecap, Urgency: urgencyLow, Default: true,
			TimeDriven: true, Freshness: 7 * 24 * time.Hour,
			Key: func(email string, ctx keyContext) string { return keyMatchupRecap(ctx.Season, ctx.Week, email) },
		},
	}
}

// Key builders (spec section 3, per-entry "Key" formula). Each embeds the
// lower-cased recipient email, matching Store.SentLog's key convention
// (spec section 6.2).
func keySeatClaimed(teamID, email string) string {
	return fmt.Sprintf("seat:%s:%s", teamID, normalizeEmail(email))
}

func keyDraftReminder(leadHours int, draftAt time.Time, email string) string {
	return fmt.Sprintf("draftrem:%d:%s:%s", leadHours, draftAt.UTC().Format(time.RFC3339), normalizeEmail(email))
}

func keyDraftOrderDrawn(orderHash8, email string) string {
	return fmt.Sprintf("order:%s:%s", orderHash8, normalizeEmail(email))
}

func keyOnTheClock(teamID string, epoch int64, email string) string {
	return fmt.Sprintf("onclock:%s:%d:%s", teamID, epoch, normalizeEmail(email))
}

func keyAutopickMade(teamID string, epoch int64, email string) string {
	return fmt.Sprintf("autopick:%s:%d:%s", teamID, epoch, normalizeEmail(email))
}

func keyDraftComplete(orderHash8, email string) string {
	return fmt.Sprintf("draftdone:%s:%s", orderHash8, normalizeEmail(email))
}

// keyPickemRemind, keyPickemResults, keyScoringChange, keyBroadcast,
// keyKickoff, and keyMatchupRecap back N8-N13's catalog entries. Their
// evaluation and template builders are later work packages (WP-E4, WP-E5);
// the key shapes land now so the catalog matrix is total.
func keyPickemRemind(season string, week int, email string) string {
	return fmt.Sprintf("pickem-remind:%s-w%d:%s", season, week, normalizeEmail(email))
}

func keyPickemResults(season string, week int, email string) string {
	return fmt.Sprintf("pickem-results:%s-w%d:%s", season, week, normalizeEmail(email))
}

func keyScoringChange(scoringHash8, email string) string {
	return fmt.Sprintf("scoring:%s:%s", scoringHash8, normalizeEmail(email))
}

func keyBroadcast(id, email string) string {
	return fmt.Sprintf("broadcast:%s:%s", id, normalizeEmail(email))
}

func keyKickoff(season, email string) string {
	return fmt.Sprintf("kickoff:%s:%s", season, normalizeEmail(email))
}

func keyMatchupRecap(season string, week int, email string) string {
	return fmt.Sprintf("recap:%s-w%d:%s", season, week, normalizeEmail(email))
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// orderHash8 hashes the joined draft order and returns its first 8 bytes
// as 16 hex characters (spec section 3, N4/N7: "the first 8 hex bytes of
// SHA-256"). A redraw with a different order produces a new hash; drawing
// the same order twice deduplicates, which is correct.
func orderHash8(order []string) string {
	sum := sha256.Sum256([]byte(strings.Join(order, ",")))
	return hex.EncodeToString(sum[:8])
}

// categoryCatalogDefault reports the catalog default for category: every
// entry's Default is true in v1 (spec section 3), so this returns the
// first registered entry's Default for that category (they all agree
// today); an unknown category fails open, matching "every default is on."
func categoryCatalogDefault(category string) bool {
	for _, entry := range catalogEntries {
		if entry.Category == category {
			return entry.Default
		}
	}
	return true
}

// notifyEnabled resolves whether email should receive a category email,
// first hit wins (spec section 7.1):
//  1. the member's own preference (state.NotifyPrefs);
//  2. the league.json default — TODO(WP-E5, productization WP1): read
//     notifications.categories[category] from config.go, which does not
//     exist yet; this step is a no-op pass-through until it lands;
//  3. the catalog default (all on in v1).
//
// Every builder in this file calls notifyEnabled; nothing else reads
// state.NotifyPrefs (spec section 7.1).
func (s *Service) notifyEnabled(state PersistedState, email, category string) bool {
	email = normalizeEmail(email)
	if prefs, ok := state.NotifyPrefs[email]; ok {
		if enabled, ok := prefs[category]; ok {
			return enabled
		}
	}
	return categoryCatalogDefault(category)
}

// renderedNotification is one fully composed email, ready for the ledger
// and the queue.
type renderedNotification struct {
	Key      string
	Category string
	To       string
	Subject  string
	Text     string
	HTML     string
}

// enqueueRendered hands rn to the notify queue. Callers must already have
// committed rn.Key with Store.FirstSend before calling this — enqueueRendered
// only delivers (spec section 6.3's build-then-FirstSend-then-Enqueue
// order lives in recordAndSend, below).
func (s *Service) enqueueRendered(rn renderedNotification) {
	s.poolMu.Lock()
	queue := s.notifyQueue
	s.poolMu.Unlock()
	if queue == nil {
		return
	}
	queue.Enqueue(notify.Message{
		Key: rn.Key, Category: rn.Category, To: rn.To,
		Subject: rn.Subject, Text: rn.Text, HTML: rn.HTML,
	})
}

// recordAndSend is the at-most-once send path every hook and derivation
// uses (spec section 6.3): check the recipient's preference, commit key to
// the ledger, and only when the key was new, build and enqueue. build runs
// lazily so an already-sent recipient never pays rendering cost on a later
// tick.
func (s *Service) recordAndSend(state PersistedState, email, category, key string, now time.Time, build func() renderedNotification) {
	if !s.notifyEnabled(state, email, category) {
		return
	}
	sent, err := s.store.FirstSend(key, now)
	if err != nil {
		log.Printf("notify: ledger write failed: key=%s err=%v", key, err)
		return
	}
	if !sent {
		return
	}
	s.enqueueRendered(build())
}

// recordOnly commits key to the ledger without building or enqueuing
// anything: the "skipped and recorded" branch of a freshness window that
// has already closed. A stale reminder is worse than none (spec section 3,
// N2/N3/N7).
func (s *Service) recordOnly(key string, now time.Time) {
	if _, err := s.store.FirstSend(key, now); err != nil {
		log.Printf("notify: ledger write failed: key=%s err=%v", key, err)
	}
}

// shellFor builds the shared shell fields for a member email (spec section
// 4.2, 5).
func (s *Service) shellFor(category, signal string) emailkit.Shell {
	label := categoryLabel[category]
	if label == "" {
		label = "League"
	}
	return emailkit.Shell{
		Wordmark:   leagueWordmark,
		ShortCode:  leagueShortCode,
		Tagline:    leagueTagline,
		Signal:     signal,
		Signoff:    "— The Commissioner",
		FooterJoke: leagueWordmark + " · " + leagueFooterLine,
		PrefLine:   fmt.Sprintf("You hold a seat in %s. %s alerts: on.", leagueWordmark, label),
		PrefURL:    leagueURL() + "/settings",
	}
}

// shellForInvitee builds the shell for the N2 unclaimed-invitee variant
// (spec section 7.1): no seat, no prefs, a claim link instead of settings.
func (s *Service) shellForInvitee(signal string) emailkit.Shell {
	return emailkit.Shell{
		Wordmark:   leagueWordmark,
		ShortCode:  leagueShortCode,
		Tagline:    leagueTagline,
		Signal:     signal,
		Signoff:    "— The Commissioner",
		FooterJoke: leagueWordmark + " · " + leagueFooterLine,
		PrefLine:   fmt.Sprintf("You were invited to %s. Claim your seat to manage alerts.", leagueWordmark),
		PrefURL:    leagueURL(),
	}
}

// pickLabel formats a pick number as "{round}.{slot}" (for example
// "3.07"), the pick-tape convention used throughout the draft room. n is
// the league's team count.
func pickLabel(number, n int) string {
	round := ((number - 1) / n) + 1
	pos := ((number - 1) % n) + 1
	return fmt.Sprintf("%d.%02d", round, pos)
}

// formatMMSS renders a duration as "m:ss" (spec section 5's worked
// example: "0:20", "1:30"). Negative durations floor at zero.
func formatMMSS(d time.Duration) string {
	total := int(d.Seconds())
	if total < 0 {
		total = 0
	}
	return fmt.Sprintf("%d:%02d", total/60, total%60)
}

func indexOfTeam(order []string, teamID string) int {
	for i, id := range order {
		if id == teamID {
			return i
		}
	}
	return -1
}

// ---------------------------------------------------------------------
// N1 — seat-claimed
// ---------------------------------------------------------------------

// notifySeatClaimed fires N1 for a brand-new member. Called from
// assignMember only when Store.AssignMember reports created == true (spec
// section 3, N1).
func (s *Service) notifySeatClaimed(member Member) {
	if !s.notifyReady() {
		return
	}
	state := s.store.Snapshot()
	now := s.clock()
	key := keySeatClaimed(member.TeamID, member.Email)
	s.recordAndSend(state, member.Email, categoryOnboarding, key, now, func() renderedNotification {
		return s.buildSeatClaimed(state, member)
	})
}

func (s *Service) buildSeatClaimed(state PersistedState, member Member) renderedNotification {
	team := s.teamView(state, member.TeamID)
	draft := s.draftSummary(s.clock())
	date, _ := draft["date"].(string)
	dtime, _ := draft["time"].(string)

	subject := fmt.Sprintf("Seat claimed: %s is yours — %s", team.Name, leagueWordmark)
	shell := s.shellFor(categoryOnboarding, "SEAT CLAIMED // "+team.Abbreviation)
	blocks := []emailkit.Block{
		emailkit.Headline{
			Title: "WELCOME TO THE ROOM.",
			Lede: fmt.Sprintf("%s is yours, in the %s division. The startup draft is %s at %s.",
				team.Name, strings.ToUpper(team.Division), date, dtime),
		},
		emailkit.PickList{
			Title: "NEXT STEPS",
			Items: []string{
				"Build your Big Board before the clock starts.",
				"Your board drives your autopick when you can't be there.",
				"Read the Rules page for the full scoring system.",
				"Bookmark the draft room — that's where it all happens.",
			},
		},
		emailkit.CTA{Label: "BUILD YOUR BOARD →", URL: leagueURL() + "/board"},
	}
	text, html := emailkit.Render(shell, blocks)
	return renderedNotification{
		Key: keySeatClaimed(member.TeamID, member.Email), Category: categoryOnboarding,
		To: member.Email, Subject: subject, Text: text, HTML: html,
	}
}

// ---------------------------------------------------------------------
// N2 / N3 — draft reminders
// ---------------------------------------------------------------------

// reminderLead describes one draft-reminder catalog entry's lead (N2's 24h
// default, N3's 1h default). league.json's
// notifications.draft_reminder_lead_hours makes this a configurable list
// in WP-E5; until config.go lands, these two defaults are the whole set
// (spec section 3, section 8).
type reminderLead struct {
	hours           int
	includeInvitees bool
}

var reminderLeads = []reminderLead{
	{hours: 24, includeInvitees: true},
	{hours: 1, includeInvitees: false},
}

// draftReminderWindow returns one lead's freshness window (spec section 3,
// N2/N3): [draftAt-lead, min(draftAt, draftAt-lead+lead/2)].
func draftReminderWindow(draftAt time.Time, hours int) (start, end time.Time) {
	lead := time.Duration(hours) * time.Hour
	start = draftAt.Add(-lead)
	end = start.Add(lead / 2)
	if end.After(draftAt) {
		end = draftAt
	}
	return start, end
}

// evalDraftReminders evaluates N2 and N3 on every notifier tick (spec
// section 6.4): a recipient before their window does nothing (re-checked
// next tick); inside the window, build and send; a window that already
// closed is recorded without a send — a stale reminder is worse than none.
func (s *Service) evalDraftReminders(state PersistedState, now time.Time) {
	for _, lead := range reminderLeads {
		start, end := draftReminderWindow(s.draftAt, lead.hours)
		if now.Before(start) {
			continue
		}
		due := !now.After(end)

		for email, member := range state.Members {
			key := keyDraftReminder(lead.hours, s.draftAt, email)
			if due {
				s.recordAndSend(state, email, categoryDraftReminders, key, now, func() renderedNotification {
					return s.buildDraftReminder(state, member, lead)
				})
			} else {
				s.recordOnly(key, now)
			}
		}

		if !lead.includeInvitees {
			continue
		}
		for _, email := range state.Invites {
			if _, claimed := state.Members[normalizeEmail(email)]; claimed {
				continue
			}
			key := keyDraftReminder(lead.hours, s.draftAt, email)
			if due {
				s.recordAndSend(state, email, categoryDraftReminders, key, now, func() renderedNotification {
					return s.buildDraftReminderInvitee(state, email, lead)
				})
			} else {
				s.recordOnly(key, now)
			}
		}
	}
}

// draftReminderSubject and its T-code/lead label share the two leads'
// formula split (spec section 3): the 24h subject and lede differ in
// wording from the 1h one beyond the number.
func draftReminderSubject(hours int, shortDate, draftTime string) (subject, tCode, leadLabel string) {
	if hours == 24 {
		return fmt.Sprintf("T-MINUS 24 HOURS — draft %s · %s", shortDate, draftTime), "T-24H", "24 hours"
	}
	return fmt.Sprintf("T-MINUS 1 HOUR — the clock arms at %s", draftTime), "T-1H", "1 hour"
}

func (s *Service) buildDraftReminder(state PersistedState, member Member, lead reminderLead) renderedNotification {
	now := s.clock()
	draft := s.draftSummary(now)
	shortDate, _ := draft["date"].(string)
	draftTime, _ := draft["time"].(string)
	format, _ := draft["format"].(string)
	duration := s.pickClock(state)

	subject, tCode, leadLabel := draftReminderSubject(lead.hours, shortDate, draftTime)
	shell := s.shellFor(categoryDraftReminders, "DRAFT EVENT // "+tCode)
	blocks := []emailkit.Block{
		emailkit.Headline{
			Title: "THE CLOCK IS COMING.",
			Lede: fmt.Sprintf("%s. %d-second picks. The clock arms in %s — get your board ready.",
				format, int(duration.Seconds()), leadLabel),
		},
		emailkit.Panel{Rows: []emailkit.PanelRow{
			{Label: "DRAFT", Value: shortDate + " · " + draftTime},
			{Label: "ORDER", Value: s.draftOrderSummary(state)},
			{Label: "YOUR SLOT", Value: s.yourSlotSummary(state, member.TeamID)},
		}},
		emailkit.CTA{Label: "ENTER THE DRAFT ROOM →", URL: leagueURL() + "/draft"},
	}
	text, html := emailkit.Render(shell, blocks)
	return renderedNotification{
		Key: keyDraftReminder(lead.hours, s.draftAt, member.Email), Category: categoryDraftReminders,
		To: member.Email, Subject: subject, Text: text, HTML: html,
	}
}

func (s *Service) buildDraftReminderInvitee(state PersistedState, email string, lead reminderLead) renderedNotification {
	draft := s.draftSummary(s.clock())
	shortDate, _ := draft["date"].(string)
	draftTime, _ := draft["time"].(string)
	format, _ := draft["format"].(string)
	duration := s.pickClock(state)

	subject, tCode, leadLabel := draftReminderSubject(lead.hours, shortDate, draftTime)
	shell := s.shellForInvitee("DRAFT EVENT // " + tCode)
	blocks := []emailkit.Block{
		emailkit.Headline{
			Title: "THE CLOCK IS COMING.",
			Lede: fmt.Sprintf("%s. %d-second picks. The clock arms in %s. Claim your seat before the room fills.",
				format, int(duration.Seconds()), leadLabel),
		},
		emailkit.Panel{Rows: []emailkit.PanelRow{
			{Label: "DRAFT", Value: shortDate + " · " + draftTime},
			{Label: "ORDER", Value: s.draftOrderSummary(state)},
		}},
		emailkit.CTA{Label: "CLAIM YOUR SEAT →", URL: leagueURL()},
	}
	text, html := emailkit.Render(shell, blocks)
	return renderedNotification{
		Key: keyDraftReminder(lead.hours, s.draftAt, email), Category: categoryDraftReminders,
		To: email, Subject: subject, Text: text, HTML: html,
	}
}

// draftOrderSummary renders the drawn order as "1. OR3 2. AQ4 ..." (spec
// section 3, N2/N3), or the not-drawn-yet placeholder.
func (s *Service) draftOrderSummary(state PersistedState) string {
	if len(state.DraftOrder) == 0 {
		return "Not drawn yet — watch for the draw announcement"
	}
	parts := make([]string, 0, len(state.DraftOrder))
	for i, teamID := range state.DraftOrder {
		parts = append(parts, fmt.Sprintf("%d. %s", i+1, s.teamAbbreviation(teamID)))
	}
	return strings.Join(parts, "  ")
}

// yourSlotSummary reports the recipient's round-1 and round-2 pick
// positions once the order is drawn (spec section 3, N2/N3).
func (s *Service) yourSlotSummary(state PersistedState, teamID string) string {
	if len(state.DraftOrder) == 0 {
		return "Not assigned yet — the draw sets every slot at once"
	}
	index := indexOfTeam(state.DraftOrder, teamID)
	if index < 0 {
		return "Not assigned yet — the draw sets every slot at once"
	}
	slot := index + 1
	n := len(state.DraftOrder)
	round2Slot := n + 1 - slot
	round2Overall := n + round2Slot
	return fmt.Sprintf("Round 1: pick %d (overall %d) · Round 2: pick %d (overall %d)",
		slot, slot, round2Slot, round2Overall)
}

// ---------------------------------------------------------------------
// N4 — draft-order-drawn
// ---------------------------------------------------------------------

// notifyDraftOrderDrawn fires N4 for every seated member once a new draft
// order is drawn. Called from AdminRandomizeDraftOrder after
// Store.SetDraftOrder succeeds (spec section 3, N4).
func (s *Service) notifyDraftOrderDrawn(order []string) {
	if !s.notifyReady() {
		return
	}
	state := s.store.Snapshot()
	now := s.clock()
	hash := orderHash8(order)
	for email, member := range state.Members {
		key := keyDraftOrderDrawn(hash, email)
		s.recordAndSend(state, email, categoryDraftReminders, key, now, func() renderedNotification {
			return s.buildDraftOrderDrawn(state, order, hash, member)
		})
	}
}

func (s *Service) buildDraftOrderDrawn(state PersistedState, order []string, hash string, member Member) renderedNotification {
	n := len(order)
	slot := indexOfTeam(order, member.TeamID) + 1
	subject := fmt.Sprintf("THE ORDER IS DRAWN — you pick %d of %d", slot, n)

	rows := make([][]string, 0, n)
	mark := -1
	for i, teamID := range order {
		team := s.teamView(state, teamID)
		manager := strings.TrimSpace(team.Manager)
		if manager == "" {
			manager = "UNCLAIMED"
		}
		rows = append(rows, []string{strconv.Itoa(i + 1), team.Name, manager})
		if teamID == member.TeamID {
			mark = i
		}
	}

	shell := s.shellFor(categoryDraftReminders, "DRAFT ORDER // OFFICIAL")
	blocks := []emailkit.Block{
		emailkit.Headline{
			Title: "FATE HAS SPOKEN.",
			Lede: fmt.Sprintf("A cryptographic shuffle set the order — no re-rolls, unless the commissioner "+
				"resets, and everyone will see that. You hold slot %d. Round 2 snakes back to you at pick %d.",
				slot, 2*n+1-slot),
		},
		emailkit.StatTable{Title: "THE ORDER", Header: []string{"SLOT", "TEAM", "MANAGER"}, Rows: rows, Mark: mark},
		emailkit.CTA{Label: "SEE THE BOARD →", URL: leagueURL() + "/draft"},
	}
	text, html := emailkit.Render(shell, blocks)
	return renderedNotification{
		Key: keyDraftOrderDrawn(hash, member.Email), Category: categoryDraftReminders,
		To: member.Email, Subject: subject, Text: text, HTML: html,
	}
}

// ---------------------------------------------------------------------
// N5 — on-the-clock (AWAY managers only)
// ---------------------------------------------------------------------

// evalOnTheClock evaluates N5 for the current on-clock seat, called from
// clockTick every tick (spec section 3, N5). It fires once per absence
// episode: an AWAY, seated manager whose seat is not Autopick-toggled and
// whose epoch key has not already sent. CONNECTED and IDLE managers, an
// unclaimed seat, and an Autopick-toggled seat never receive it.
func (s *Service) evalOnTheClock(state PersistedState, now time.Time) {
	if !s.notifyReady() {
		return
	}
	if state.ClockPaused || state.ClockDeadline.IsZero() {
		return
	}
	n := len(defaultTeams())
	if len(state.Picks) >= n*DraftRounds {
		return
	}
	number := len(state.Picks) + 1
	teamID := teamOnClock(state.DraftOrder, number)
	if state.Autopick[teamID] {
		return
	}
	member := memberForTeam(state.Members, teamID)
	if member.Email == "" {
		return
	}
	key := s.presenceKeyForTeam(state, teamID)
	if key == "" {
		return
	}
	seenAt := s.presenceFloor(key)
	if presenceState(seenAt, now) != "away" {
		return
	}
	epoch := seenAt.Unix()
	ledgerKey := keyOnTheClock(teamID, epoch, member.Email)
	s.recordAndSend(state, member.Email, categoryDraftLive, ledgerKey, now, func() renderedNotification {
		return s.buildOnTheClock(state, now, teamID, member, epoch)
	})
}

func (s *Service) buildOnTheClock(state PersistedState, now time.Time, teamID string, member Member, epoch int64) renderedNotification {
	n := len(defaultTeams())
	number := len(state.Picks) + 1
	label := pickLabel(number, n)
	subject := fmt.Sprintf("ON THE CLOCK — pick %s is yours. The room is watching.", label)
	signal := fmt.Sprintf("DRAFT LIVE // PICK %s // OVERALL %d", label, number)

	boardKey := s.presenceKeyForTeam(state, teamID)
	shell := s.shellFor(categoryDraftLive, signal)
	blocks := []emailkit.Block{
		emailkit.Headline{
			Title: "YOU'RE ON THE CLOCK.",
			Lede: fmt.Sprintf("%d picks are on the tape. The board just turned to your seat and stopped. "+
				"Everything from here is your call — or your autopick's.", number-1),
		},
		emailkit.Panel{Rows: []emailkit.PanelRow{
			{Label: "PICK", Value: fmt.Sprintf("%s · overall %d", label, number)},
			{Label: "CLOCK", Value: s.onClockClockRow(state, now)},
			{Label: "YOUR BOARD", Value: s.onClockBoardRow(state, boardKey)},
		}},
		emailkit.CTA{Label: "TAKE YOUR PICK →", URL: leagueURL() + "/draft"},
		emailkit.Note{Text: "If the cap hits zero, the server drafts the top of your Big Board for you. " +
			"No board? Best available by ADP. Either way the tape reads AUTO next to your name — forever. " +
			"One tap fixes that."},
	}
	text, html := emailkit.Render(shell, blocks)
	return renderedNotification{
		Key: keyOnTheClock(teamID, epoch, member.Email), Category: categoryDraftLive,
		To: member.Email, Subject: subject, Text: text, HTML: html,
	}
}

// onClockClockRow renders the CLOCK panel row from effectiveDeadline's
// reason (spec section 5): "away-cap" gets the cap copy, "paused" gets the
// paused copy, and the plain "clock" reason (only reachable in a
// presence-flip race) gets the remaining time.
func (s *Service) onClockClockRow(state PersistedState, now time.Time) string {
	effective, reason := s.effectiveDeadline(state, now)
	duration := s.pickClock(state)
	remaining := effective.Sub(now)
	switch reason {
	case "away-cap":
		return fmt.Sprintf("%s away cap armed — the server reads you as AWAY. Reconnect and your full %s comes back.",
			formatMMSS(remaining), formatMMSS(duration))
	case "paused":
		return "PAUSED — the commissioner stopped the clock. Your pick waits for you."
	default:
		return fmt.Sprintf("%s remaining on the clock.", formatMMSS(remaining))
	}
}

// onClockBoardRow renders the YOUR BOARD panel row: the top available
// board entry, or the spec's exact empty-board fallback (spec section 5).
func (s *Service) onClockBoardRow(state PersistedState, key string) string {
	pool := s.pool()
	picked := make(map[string]bool, len(state.Picks))
	for _, pick := range state.Picks {
		picked[pick.PlayerID] = true
	}
	for _, id := range state.Boards[key] {
		if picked[id] {
			continue
		}
		if player, ok := pool.byID[id]; ok {
			return fmt.Sprintf("%s · %s · %s still sits at #1 on your board.", player.Name, player.Position, player.NFLTeam)
		}
	}
	return "Your board is empty. Autopick will take best available ADP. You have opinions — go use them."
}

// ---------------------------------------------------------------------
// N6 — autopick-made
// ---------------------------------------------------------------------

// notifyAutopickMade fires N6 for one auto-fired pick, skipping when the
// seat's manager was CONNECTED at pick time (spec section 3, N6). state
// must be the pre-fire snapshot (the same world the auto-pick decision
// saw), so board-source and epoch reads agree with what actually happened;
// pick is the just-recorded DraftPick, and reason is effectiveDeadline's
// pre-fire reason ("clock", "away-cap", or "autopick"), used for the MODE
// row.
func (s *Service) notifyAutopickMade(preState PersistedState, pick DraftPick, reason string, now time.Time) {
	if !s.notifyReady() {
		return
	}
	member := memberForTeam(preState.Members, pick.TeamID)
	if member.Email == "" {
		return
	}
	key := s.presenceKeyForTeam(preState, pick.TeamID)
	if key != "" && presenceState(s.presenceFloor(key), now) == "connected" {
		return
	}
	epoch := s.presenceFloor(key).Unix()
	ledgerKey := keyAutopickMade(pick.TeamID, epoch, member.Email)
	s.recordAndSend(preState, member.Email, categoryDraftLive, ledgerKey, now, func() renderedNotification {
		return s.buildAutopickMade(preState, pick, member, reason, epoch)
	})
}

func (s *Service) buildAutopickMade(state PersistedState, pick DraftPick, member Member, reason string, epoch int64) renderedNotification {
	n := len(defaultTeams())
	label := pickLabel(pick.Number, n)
	pool := s.pool()
	player := pool.byID[pick.PlayerID]

	sourceLabel := "best available ADP"
	if reason == "commissioner" {
		sourceLabel = "forced by the commissioner"
	} else {
		boardKey := s.presenceKeyForTeam(state, pick.TeamID)
		for _, id := range state.Boards[boardKey] {
			if id == pick.PlayerID {
				sourceLabel = "top of your Big Board"
				break
			}
		}
	}

	mode := "AWAY cap"
	switch reason {
	case "autopick":
		mode = "AUTOPICK toggle"
	case "commissioner":
		mode = "COMMISSIONER"
	}

	subject := fmt.Sprintf("AUTO: %s is yours — pick %s", player.Name, label)
	signal := fmt.Sprintf("AUTOPICK // PICK %s", label)

	shell := s.shellFor(categoryDraftLive, signal)
	blocks := []emailkit.Block{
		emailkit.Headline{
			Title: "THE MACHINE DRAFTED FOR YOU.",
			Lede:  fmt.Sprintf("%s (%s, %s) is now yours — %s.", player.Name, player.Position, player.NFLTeam, sourceLabel),
		},
		emailkit.Panel{Rows: []emailkit.PanelRow{
			{Label: "PICK", Value: fmt.Sprintf("%s · overall %d", label, pick.Number)},
			{Label: "NEXT UP", Value: s.nextUpSummary(state, pick.TeamID, pick.Number)},
			{Label: "MODE", Value: mode},
		}},
		emailkit.Note{Text: "Further auto picks stay quiet until you return."},
		emailkit.CTA{Label: "RETAKE THE WHEEL →", URL: leagueURL() + "/draft"},
	}
	text, html := emailkit.Render(shell, blocks)
	return renderedNotification{
		Key: keyAutopickMade(pick.TeamID, epoch, member.Email), Category: categoryDraftLive,
		To: member.Email, Subject: subject, Text: text, HTML: html,
	}
}

// nextUpSummary renders the NEXT UP panel row: the recipient's next pick
// number and a rough time estimate from the pick-clock duration (spec
// section 3, N6).
func (s *Service) nextUpSummary(state PersistedState, teamID string, after int) string {
	n := len(defaultTeams())
	order := state.DraftOrder
	if len(order) == 0 {
		order = defaultTeamIDs()
	}
	total := n * DraftRounds
	for number := after + 1; number <= total; number++ {
		if teamOnClock(order, number) == teamID {
			eta := time.Duration(number-after) * s.pickClock(state)
			return fmt.Sprintf("%s · overall %d · roughly %s away", pickLabel(number, n), number, formatMMSS(eta))
		}
	}
	return "You're done — the draft wraps after this round."
}

// ---------------------------------------------------------------------
// N7 — draft-complete recap
// ---------------------------------------------------------------------

// evalDraftComplete evaluates N7 on every notifier tick (spec section 3,
// N7). Deriving from state rather than hooking the final pick makes the
// send restart-proof: a crash right after the last pick still sends on the
// next tick. Past the 48h freshness window the key is recorded without a
// send, mirroring N2/N3.
func (s *Service) evalDraftComplete(state PersistedState, now time.Time) {
	n := len(defaultTeams())
	total := n * DraftRounds
	if len(state.Picks) < total {
		return
	}
	lastPick := state.Picks[len(state.Picks)-1]
	within := now.Sub(lastPick.MadeAt) <= 48*time.Hour

	order := state.DraftOrder
	if len(order) == 0 {
		order = defaultTeamIDs()
	}
	hash := orderHash8(order)
	for email, member := range state.Members {
		key := keyDraftComplete(hash, email)
		if within {
			s.recordAndSend(state, email, categoryDraftRecap, key, now, func() renderedNotification {
				return s.buildDraftComplete(state, member, hash)
			})
		} else {
			s.recordOnly(key, now)
		}
	}
}

func (s *Service) buildDraftComplete(state PersistedState, member Member, hash string) renderedNotification {
	pool := s.pool()
	n := len(defaultTeams())

	manual, auto, commissioner := 0, 0, 0
	for _, pick := range state.Picks {
		switch pick.MadeBy {
		case "auto":
			auto++
		case "commissioner":
			commissioner++
		default:
			manual++
		}
	}
	var duration time.Duration
	if len(state.Picks) > 0 {
		duration = state.Picks[len(state.Picks)-1].MadeAt.Sub(state.Picks[0].MadeAt)
	}

	subject := fmt.Sprintf("DRAFT COMPLETE — %d picks on the tape. Your haul inside.", len(state.Picks))
	signal := fmt.Sprintf("DRAFT COMPLETE // %d PICKS", len(state.Picks))

	haulRows := make([][]string, 0, DraftRounds)
	for _, pick := range state.Picks {
		if pick.TeamID != member.TeamID {
			continue
		}
		player := pool.byID[pick.PlayerID]
		haulRows = append(haulRows, []string{strconv.Itoa(pick.Round), pickLabel(pick.Number, n), player.Name, player.Position, player.NFLTeam})
	}

	order := state.DraftOrder
	if len(order) == 0 {
		order = defaultTeamIDs()
	}
	leagueRows := make([][]string, 0, n)
	mark := -1
	for i, teamID := range order {
		team := s.teamView(state, teamID)
		round1 := ""
		counts := map[string]int{}
		for _, pick := range state.Picks {
			if pick.TeamID != teamID {
				continue
			}
			player := pool.byID[pick.PlayerID]
			counts[player.Position]++
			if pick.Round == 1 {
				round1 = player.Name
			}
		}
		posSummary := fmt.Sprintf("%dQB/%dRB/%dWR/%dTE", counts["QB"], counts["RB"], counts["WR"], counts["TE"])
		leagueRows = append(leagueRows, []string{strconv.Itoa(i + 1), team.Name, round1, posSummary})
		if teamID == member.TeamID {
			mark = i
		}
	}

	shell := s.shellFor(categoryDraftRecap, signal)
	blocks := []emailkit.Block{
		emailkit.Headline{
			Title: "THE TAPE IS SEALED.",
			Lede: fmt.Sprintf("%s of drafting. %d manual, %d auto, %d commissioner picks on the tape.",
				duration.Round(time.Minute), manual, auto, commissioner),
		},
		emailkit.StatTable{Title: "YOUR HAUL", Header: []string{"ROUND", "PICK", "PLAYER", "POS", "TEAM"}, Rows: haulRows, Mark: -1},
		emailkit.StatTable{Title: "AROUND THE LEAGUE", Header: []string{"SLOT", "TEAM", "ROUND 1", "POSITIONS"}, Rows: leagueRows, Mark: mark},
		emailkit.CTA{Label: "SEE THE FULL BOARD →", URL: leagueURL() + "/draft"},
	}
	text, html := emailkit.Render(shell, blocks)
	return renderedNotification{
		Key: keyDraftComplete(hash, member.Email), Category: categoryDraftRecap,
		To: member.Email, Subject: subject, Text: text, HTML: html,
	}
}

// ---------------------------------------------------------------------
// Notifier ticker and admin mail-health map
// ---------------------------------------------------------------------

// notifierTickPeriod is StartNotifier's evaluation interval (spec section
// 6.4). Sixty-second resolution is exact enough for every window in the
// catalog; the tightest is the 1-hour reminder.
const notifierTickPeriod = 60 * time.Second

// sentLogRetention is how long a SentLog entry survives the daily prune
// (spec section 6.2).
const sentLogRetention = 180 * 24 * time.Hour

// StartNotifier runs the 60-second trigger-evaluation and ledger-pruning
// loop. Call it once from main.go, beside StartDraftClock. All decision
// logic lives in notifierTick, which tests drive directly with a fake
// clock — no goroutine, no sleeps, in the test suite (spec section 6.4).
// Without a live, wired transport the loop never starts at all (spec
// section 6.6): notifications stay off, and nothing is built or recorded.
func (s *Service) StartNotifier(ctx context.Context) {
	if !s.notifyReady() {
		log.Printf("notify: no mail transport configured; notifications disabled")
		return
	}
	go func() {
		ticker := time.NewTicker(notifierTickPeriod)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.notifierTick(s.clock())
			}
		}
	}()
}

// notifierTick is the whole time-driven and derived trigger evaluation for
// one instant (spec section 6.4), in the spec's exact order: N2/N3
// reminder windows, then N7's draft-complete derivation, then the entries
// N8-N13 own (their evaluation lands in later work packages — see the TODO
// markers), then the daily SentLog prune.
func (s *Service) notifierTick(now time.Time) {
	state := s.store.Snapshot()

	s.evalDraftReminders(state, now) // N2, N3
	s.evalDraftComplete(state, now)  // N7

	// TODO(WP-E4): N8 pickem-reminder window evaluation.
	// TODO(WP-E4): N9 pickem-results derivation.
	// TODO(WP-E4): N10 scoring-change coalescing.
	// TODO(WP-E5): N12 season-kickoff window evaluation (gated on
	// state.Schedule != nil, once competition-formats WP2 lands).
	// TODO(WP-E5): N13 weekly matchup-recap derivation (the ticker-derived
	// half; week-close's direct-enqueue half is a separate hook).

	s.dailyPrune(now)
}

// dailyPrune drops SentLog entries older than sentLogRetention, once per
// day and once at boot (spec section 6.2). notifyLastPruneAt is
// process-local and starts zero, so the first tick after startup always
// prunes; pruning is idempotent, so a restart re-running it costs one lock
// and one persist, nothing more.
func (s *Service) dailyPrune(now time.Time) {
	s.poolMu.Lock()
	due := s.notifyLastPruneAt.IsZero() || now.Sub(s.notifyLastPruneAt) >= 24*time.Hour
	if due {
		s.notifyLastPruneAt = now
	}
	s.poolMu.Unlock()
	if !due {
		return
	}
	if err := s.store.PruneSentLog(now.Add(-sentLogRetention)); err != nil {
		log.Printf("notify: daily prune failed: %v", err)
	}
}

// notifyMailMap renders the admin console's mail-health card: whether
// notifications are wired and enabled, the queue's current depth, the most
// recent send failure, and how many ledger entries landed in the last 24
// hours (spec section 6.6).
func (s *Service) notifyMailMap(now time.Time) map[string]any {
	s.poolMu.Lock()
	queue := s.notifyQueue
	enabled := s.notifyTransportEnabled
	s.poolMu.Unlock()

	queueDepth := 0
	lastError := ""
	var lastErrorAt time.Time
	if queue != nil {
		queueDepth = queue.Depth()
		lastError, lastErrorAt = queue.LastError()
	}
	sentToday := 0
	for _, sentAt := range s.store.Snapshot().SentLog {
		if now.Sub(sentAt) < 24*time.Hour {
			sentToday++
		}
	}
	lastErrorAtStr := ""
	if !lastErrorAt.IsZero() {
		lastErrorAtStr = lastErrorAt.UTC().Format(time.RFC3339)
	}
	return map[string]any{
		"enabled":       enabled && queue != nil,
		"queue_depth":   queueDepth,
		"last_error":    lastError,
		"last_error_at": lastErrorAtStr,
		"sent_today":    sentToday,
	}
}
