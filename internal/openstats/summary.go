package openstats

import (
	"strings"
	"unicode"
)

// NormalizePlayerKey builds a lookup key for matching a player across
// datasets that spell names differently (punctuation, casing, suffixes). It
// lowercases the name, strips every non-alphanumeric character, and appends
// the upper-cased position so same-named players at different positions do
// not collide.
func NormalizePlayerKey(name, position string) string {
	var builder strings.Builder
	for _, r := range name {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			builder.WriteRune(unicode.ToLower(r))
		}
	}
	builder.WriteByte('|')
	builder.WriteString(strings.ToUpper(strings.TrimSpace(position)))
	return builder.String()
}
