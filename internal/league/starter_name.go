package league

import (
	"strings"
	"unicode/utf8"
)

// StarterPlayerShortName keeps initial HTML and live mobile labels identical.
// Twelve runes fit the phone name cell; longer names use a first initial.
func StarterPlayerShortName(name string) string {
	if utf8.RuneCountInString(name) <= 12 {
		return name
	}
	first, last, ok := strings.Cut(name, " ")
	if !ok || first == "" || last == "" {
		return name
	}
	return string([]rune(first)[0]) + ". " + last
}
