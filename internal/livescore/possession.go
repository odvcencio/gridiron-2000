package livescore

import "strings"

// possessionCandidateKeys is GC-2b's tolerant seam over the raw box-score
// payload: a small, ordered, single pinnable table of flat top-level keys
// that MIGHT carry a possession signal directly. None is verified against
// a real Tank01 capture yet — see possessionFromLineScore below for the
// one shape that is. Checked only after the line-score shape comes back
// unknown, in this order; the first key present with a non-empty string
// value wins. Pin the real key here from the first live capture (the
// Thursday 2026-09-10 window is the pinning event, per the spec); until
// then every one of these is a guess, and a guess that finds nothing
// costs nothing — ExtractPossession never fabricates a team.
var possessionCandidateKeys = []string{
	"possession",
	"teamInPossession",
	"gameStatusPossession",
}

// ExtractPossession is GC-2b's possession extraction seam: given the raw,
// already-decoded getNFLBoxScore body (fantasy.BoxScore.Raw), it returns
// the nflverse abbreviation of the team currently on offense and whether
// that team is actually known. It never guesses: a nil payload, an absent
// field, an unparseable value, or a contradictory one all return ("",
// false), exactly like a payload that carries no possession signal at
// all. Callers gate this on the game actually being in progress
// (addBoxToSnapshot only calls it when box.InProgress) — possession has
// no honest meaning for a pre-game or final box score.
func ExtractPossession(raw map[string]any) (team string, known bool) {
	if raw == nil {
		return "", false
	}
	if team, known := possessionFromLineScore(raw); known {
		return team, known
	}
	for _, key := range possessionCandidateKeys {
		value, ok := raw[key]
		if !ok {
			continue
		}
		text, ok := value.(string)
		if !ok {
			continue
		}
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		return NormalizeTeam(text), true
	}
	return "", false
}

// possessionFromLineScore reads the one shape verified against real
// Tank01 box-score captures in this repo: a "lineScore" object with
// "away" and "home" sub-objects, each carrying its own
// "currentlyInPossession" string ("True"/"False"). Exactly one side
// reading true resolves to that side's own top-level team abbreviation
// (raw["away"]/raw["home"], the same fields parseBoxScore reads for
// BoxScore.Away/Home); neither side true, both sides true (a
// contradictory payload), or the shape being absent entirely all return
// unknown — never a guess.
func possessionFromLineScore(raw map[string]any) (string, bool) {
	lineScore, ok := raw["lineScore"].(map[string]any)
	if !ok {
		return "", false
	}
	awaySide, _ := lineScore["away"].(map[string]any)
	homeSide, _ := lineScore["home"].(map[string]any)
	awayHas := sideCurrentlyInPossession(awaySide)
	homeHas := sideCurrentlyInPossession(homeSide)
	switch {
	case awayHas && !homeHas:
		return rawTeamAbbrev(raw, "away")
	case homeHas && !awayHas:
		return rawTeamAbbrev(raw, "home")
	default:
		return "", false
	}
}

// sideCurrentlyInPossession reads one lineScore side's own
// "currentlyInPossession" flag, tolerant of Tank01's usual string-encoded
// booleans ("True"/"False") and a plain JSON bool, should the live
// payload ever ship one instead. A nil side, a missing key, or any other
// shape reads as false — not possessing — never a guess in the other
// direction.
func sideCurrentlyInPossession(side map[string]any) bool {
	if side == nil {
		return false
	}
	switch value := side["currentlyInPossession"].(type) {
	case string:
		return strings.EqualFold(strings.TrimSpace(value), "true")
	case bool:
		return value
	default:
		return false
	}
}

// rawTeamAbbrev resolves raw[side] (side is "away" or "home") to a
// normalized nflverse team abbreviation. A missing or non-string value
// reports unknown rather than an empty team.
func rawTeamAbbrev(raw map[string]any, side string) (string, bool) {
	value, ok := raw[side].(string)
	if !ok {
		return "", false
	}
	value = strings.ToUpper(strings.TrimSpace(value))
	if value == "" {
		return "", false
	}
	return NormalizeTeam(value), true
}
