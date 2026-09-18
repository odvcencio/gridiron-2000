package league

import (
	"fmt"
	"log"
	"strings"
	"time"
)

// The Locker Room recap is posted under a reserved system identity, never
// a member's. The address uses the RFC 2606 .invalid domain, so it is
// never routable and never matches an admitted identity, so the post cannot be removed as "yours" by any manager; the
// commissioner's removal path still applies.
const (
	lockerRecapAuthorEmail = "recap@gridiron.invalid"
	lockerRecapAuthorName  = "Week recap"
)

func lockerRecapHeadline(week int) string {
	return fmt.Sprintf("WEEK %d IS FINAL.", week)
}

// lockerWeekRecapBody is the shared, in-app counterpart of the per-member
// recap email (buildMatchupRecap, notifications.go): one headline, one
// line per matchup with the margin (or a plain tie), the bye team, then
// the week's awards from the same awardsForWeek every trophy case reads.
// It stays under lockerBodyMaxRunes for a full slate.
func (s *Service) lockerWeekRecapBody(state PersistedState, week ScheduleWeek) string {
	name := func(teamID string) string {
		if label := strings.TrimSpace(s.teamView(state, teamID).Name); label != "" {
			return label
		}
		return teamID
	}
	var b strings.Builder
	b.WriteString(lockerRecapHeadline(week.Week))
	for _, m := range week.Matchups {
		home, away := name(m.HomeTeamID), name(m.AwayTeamID)
		fmt.Fprintf(&b, "\n%s %.1f @ %s %.1f — ", away, m.AwayScore, home, m.HomeScore)
		switch {
		case m.HomeScore > m.AwayScore:
			fmt.Fprintf(&b, "%s by %.1f", home, m.HomeScore-m.AwayScore)
		case m.AwayScore > m.HomeScore:
			fmt.Fprintf(&b, "%s by %.1f", away, m.AwayScore-m.HomeScore)
		default:
			b.WriteString("tie")
		}
	}
	if week.ByeTeamID != "" {
		fmt.Fprintf(&b, "\nBye: %s", name(week.ByeTeamID))
	}
	for _, award := range s.awardsForWeek(state, week) {
		holder := name(award.TeamID)
		if award.Kind == weeklyAwardPickem && award.Email != "" {
			if member, ok := memberByEmail(state.Members, award.Email); ok && strings.TrimSpace(member.Name) != "" {
				holder = strings.TrimSpace(member.Name)
			}
		}
		detail := award.Detail
		if at := strings.Index(detail, " · "); at >= 0 {
			detail = detail[at+len(" · "):]
		}
		fmt.Fprintf(&b, "\n%s %s: %s — %s", award.Icon, award.Title, holder, detail)
	}
	return b.String()
}

// postLockerWeekRecap posts the week's recap to the Locker Room once. A
// week that already has a recap (the same headline under the system
// author) is left alone, so a re-close after a correction never doubles
// the post. A failure to post is logged, never surfaced: the close itself
// has already committed and the email recap still goes out.
func (s *Service) postLockerWeekRecap(state PersistedState, week ScheduleWeek, now time.Time) {
	headline := lockerRecapHeadline(week.Week)
	for _, post := range state.LockerPosts {
		if post.AuthorEmail == lockerRecapAuthorEmail && strings.HasPrefix(post.Body, headline) {
			return
		}
	}
	body := s.lockerWeekRecapBody(state, week)
	if _, err := s.store.PostLocker("", body, lockerRecapAuthorEmail, lockerRecapAuthorName, "", false, now); err != nil {
		log.Printf("locker recap for week %d: %v", week.Week, err)
	}
}
