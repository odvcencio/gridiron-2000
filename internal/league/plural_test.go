package league

import "testing"

func TestPluralSingularStaysUnchanged(t *testing.T) {
	if got := Plural(1, "LEAGUE"); got != "LEAGUE" {
		t.Fatalf("Plural(1, LEAGUE) = %q, want %q", got, "LEAGUE")
	}
	if got := Plural(1, "League"); got != "League" {
		t.Fatalf("Plural(1, League) = %q, want %q", got, "League")
	}
}

func TestPluralAppendsSuffixMatchingCase(t *testing.T) {
	if got := Plural(0, "LEAGUE"); got != "LEAGUES" {
		t.Fatalf("Plural(0, LEAGUE) = %q, want %q", got, "LEAGUES")
	}
	if got := Plural(2, "LEAGUE"); got != "LEAGUES" {
		t.Fatalf("Plural(2, LEAGUE) = %q, want %q", got, "LEAGUES")
	}
	if got := Plural(3, "League"); got != "Leagues" {
		t.Fatalf("Plural(3, League) = %q, want %q", got, "Leagues")
	}
}
