package league

import "strings"

// Plural renders singular unchanged for a count of exactly one, and with
// an "s" (or "S", matching singular's own casing) appended for every
// other count — the one pluralization rule a page's copy needs for a
// league count, a seat count, or a critical-item count (wave-2
// commissioner console: Commissioner HQ read "1 LEAGUES" for a
// single-league fleet). Callers precompute the rendered word server-side
// and pass it to the page as a plain string; GoSX templates cannot call
// an arbitrary Go function inline.
func Plural(n int, singular string) string {
	if n == 1 {
		return singular
	}
	if singular == strings.ToUpper(singular) {
		return singular + "S"
	}
	return singular + "s"
}
