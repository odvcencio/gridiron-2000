package openstats

import (
	"strings"
	"unicode"
)

// NormalizePlayerKey builds a lookup key for matching a player across
// datasets that spell names differently (punctuation, casing, suffixes). It
// lowercases the name, strips punctuation and a trailing generational suffix,
// and appends the upper-cased position so same-named players at different
// positions do not collide.
func NormalizePlayerKey(name, position string) string {
	parts := normalizedNameParts(name)
	var builder strings.Builder
	for _, part := range parts {
		builder.WriteString(part)
	}
	builder.WriteByte('|')
	builder.WriteString(strings.ToUpper(strings.TrimSpace(position)))
	return builder.String()
}

func normalizedNameParts(name string) []string {
	parts := strings.FieldsFunc(name, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	for i := range parts {
		parts[i] = strings.ToLower(parts[i])
	}
	if len(parts) > 1 {
		switch parts[len(parts)-1] {
		case "jr", "sr", "ii", "iii", "iv", "v":
			parts = parts[:len(parts)-1]
		}
	}
	return parts
}
