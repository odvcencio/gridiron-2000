package identity

import "testing"

func TestNewResolvesNormalizedOneWayAliases(t *testing.T) {
	r, err := New(map[string]string{" ALIAS@EXAMPLE.COM ": " Canonical@Example.com "})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := r.Resolve("ALIAS@example.com"); got != "canonical@example.com" {
		t.Fatalf("Resolve(alias) = %q", got)
	}
	if got := r.Resolve(" canonical@example.com "); got != "canonical@example.com" {
		t.Fatalf("Resolve(canonical) = %q", got)
	}
	if got := r.Resolve("other@example.com"); got != "other@example.com" {
		t.Fatalf("Resolve(other) = %q", got)
	}
	if pairs := r.Pairs(); len(pairs) != 1 || pairs[0].Alias != "alias@example.com" || pairs[0].Canonical != "canonical@example.com" {
		t.Fatalf("Pairs = %#v", pairs)
	}
}

func TestNewRejectsUnsafeMappings(t *testing.T) {
	tests := []struct {
		name string
		in   map[string]string
	}{
		{"empty alias", map[string]string{"": "canonical@example.com"}},
		{"empty canonical", map[string]string{"alias@example.com": ""}},
		{"self", map[string]string{"alias@example.com": "alias@example.com"}},
		{"chain", map[string]string{"alias@example.com": "middle@example.com", "middle@example.com": "canonical@example.com"}},
		{"invalid alias", map[string]string{"not-an-email": "canonical@example.com"}},
		{"invalid canonical", map[string]string{"alias@example.com": "not-an-email"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := New(tt.in); err == nil {
				t.Fatal("New succeeded; want validation error")
			}
		})
	}
}

func TestFromEnvRejectsDuplicateConflictingAndMalformedMappings(t *testing.T) {
	tests := []string{
		"alias@example.com=one@example.com,alias@example.com=two@example.com",
		"alias@example.com",
		"alias@example.com=one@example.com=",
		"alias@example.com=one@example.com,,other@example.com=one@example.com",
	}
	for _, value := range tests {
		t.Setenv("IDENTITY_ALIASES", value)
		if _, err := FromEnv(); err == nil {
			t.Fatalf("FromEnv(%q) succeeded; want error", value)
		}
	}
}

func TestFromEnvBlankIsDisabled(t *testing.T) {
	t.Setenv("IDENTITY_ALIASES", " ")
	r, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}
	if r.Enabled() || r.Resolve(" Alias@Example.com ") != "alias@example.com" {
		t.Fatalf("blank env produced %#v", r)
	}
}
