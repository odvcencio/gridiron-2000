package navigation

import "testing"

// TestPageLabelNamesTheDestination is F24 (comb — maple, 2026-09-04 UX
// pass): /login?next=... used to promise "the page you requested" without
// naming it. PageLabel maps a validated return path to the same
// plain-language name the primary navigation uses (app/layout.gsx), so the
// login page can say where sign-in actually sends the visitor.
func TestPageLabelNamesTheDestination(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"/draft", "the Draft room"},
		{"/draft?week=1", "the Draft room"},
		{"/draft/results", "the draft results"},
		{"/team", "your Team terminal"},
		{"/board", "your Big Board"},
		{"/", "the league home"},
		{"/some/unknown/route", "/some/unknown/route"},
	}
	for _, tt := range tests {
		if got := PageLabel(tt.path); got != tt.want {
			t.Errorf("PageLabel(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}
