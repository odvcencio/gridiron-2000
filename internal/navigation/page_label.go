package navigation

import "strings"

// pageLabelEntries maps a route path prefix to the plain-language
// destination name the primary navigation already uses for it
// (app/layout.gsx's own Link titles). Order matters: a longer, more
// specific prefix (for example /draft/results) must be tried before a
// shorter sibling (/draft) so it is never shadowed by it. PageLabel below
// still resolves by longest match regardless of table order, but keeping
// specific routes first here documents the intent.
var pageLabelEntries = []struct {
	prefix string
	label  string
}{
	{"/draft/results", "the draft results"},
	{"/draft", "the Draft room"},
	{"/team", "your Team terminal"},
	{"/join", "the join-a-team page"},
	{"/board", "your Big Board"},
	{"/players", "the Player pool"},
	{"/trades", "Trades"},
	{"/pickem", "Pick'em HQ"},
	{"/matchups", "Matchups"},
	{"/blitz", "Preseason Blitz"},
	{"/wire", "the Signal Wire"},
	{"/activity", "Activity"},
	{"/locker", "the Locker Room"},
	{"/scoring", "Rules & scoring"},
	{"/guide", "the manager guide"},
	{"/help", "the Help center"},
	{"/settings", "Notification settings"},
	{"/commissioner", "Commissioner HQ"},
	{"/admin", "League settings"},
	{"/", "the league home"},
}

// PageLabel is F24 (comb — maple, 2026-09-04 UX pass): /login?next=...
// used to promise "the page you requested" without naming it, so a
// visitor who followed a deep link had no way to confirm sign-in kept the
// right destination. PageLabel maps a validated return path to the same
// plain-language name the primary navigation already gives that route,
// matched by the longest path-segment prefix so a specific route is never
// shadowed by a shorter sibling. A path this table does not name (a route
// the navigation does not carry, or one added after this table) falls
// back to the path itself, which is still true, just less friendly.
func PageLabel(returnPath string) string {
	clean := returnPath
	if idx := strings.IndexAny(clean, "?#"); idx >= 0 {
		clean = clean[:idx]
	}
	best, bestLen := "", -1
	for _, entry := range pageLabelEntries {
		if clean != entry.prefix && !strings.HasPrefix(clean, entry.prefix+"/") {
			continue
		}
		if len(entry.prefix) > bestLen {
			best, bestLen = entry.label, len(entry.prefix)
		}
	}
	if bestLen >= 0 {
		return best
	}
	return returnPath
}
