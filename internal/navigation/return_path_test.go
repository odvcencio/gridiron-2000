package navigation

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSafeReturnPath(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "empty", raw: "", want: "/"},
		{name: "page", raw: "/draft", want: "/draft"},
		{name: "page query", raw: "/draft?week=1", want: "/draft?week=1"},
		{name: "wire query", raw: "/wire?category=injury", want: "/wire?category=injury"},
		{name: "trailing slash", raw: "/draft/", want: "/draft/"},
		{name: "absolute URL", raw: "https://evil.example/", want: "/"},
		{name: "protocol relative", raw: "//evil.example/", want: "/"},
		{name: "scheme-like path", raw: "javascript:alert(1)", want: "/"},
		{name: "query only", raw: "?next=/draft", want: "/"},
		{name: "malformed escape", raw: "/draft?x=%zz", want: "/"},
		{name: "newline", raw: "/draft\r\nLocation:%20https://evil.example", want: "/"},
		{name: "backslash", raw: `/draft\\\\evil`, want: "/"},
		{name: "encoded backslash", raw: "/draft%5C%5Cevil", want: "/"},
		{name: "encoded protocol relative", raw: "/%2F%2Fevil.example/", want: "/"},
		{name: "encoded control", raw: "/draft%0d%0aLocation:%20https://evil.example", want: "/"},
		{name: "encoded query backslash", raw: "/draft?next=%5C%5Cevil.example", want: "/"},
		{name: "encoded query control", raw: "/draft?next=%00", want: "/"},
		{name: "encoded query delete", raw: "/draft?next=%7f", want: "/"},
		{name: "fragment", raw: "/draft?week=1#evil", want: "/"},
		{name: "double encoded backslash", raw: "/draft%255c%255cevil.example", want: "/"},
		{name: "double encoded protocol relative", raw: "/%252f%252fevil.example", want: "/"},
		{name: "invalid utf8 escape", raw: "/draft/%ff", want: "/"},
		{name: "nested query punctuation", raw: "/wire?tag=a+b%2Bc%26d%3De", want: "/wire?tag=a+b%2Bc%26d%3De"},
		{name: "login endpoint", raw: "/login", want: "/"},
		{name: "login trailing slash", raw: "/login/", want: "/"},
		{name: "oauth start endpoint", raw: "/auth/google/start", want: "/"},
		{name: "oauth callback endpoint", raw: "/auth/google/callback?code=stale", want: "/"},
		{name: "logout endpoint", raw: "/auth/logout", want: "/"},
		{name: "encoded callback endpoint", raw: "/auth%2fgoogle%2fcallback", want: "/"},
		{name: "callback dot segments", raw: "/auth/google/./callback/", want: "/"},
		{name: "login traversal", raw: "/draft/../login", want: "/"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SafeReturnPath(tt.raw); got != tt.want {
				t.Fatalf("SafeReturnPath(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestSafeActionReturnPathPreservesRelativeFragmentTargets(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "team identity editor", raw: "/team?identity=edit#team-identity", want: "/team?identity=edit#team-identity"},
		{name: "query and fragment", raw: "/wire?category=injury#schedule", want: "/wire?category=injury#schedule"},
		{name: "admin action context", raw: "/admin?seat=team-1#identity", want: "/admin?seat=team-1#identity"},
		{name: "external URL", raw: "https://evil.example/#steal", want: "/"},
		{name: "protocol relative", raw: "//evil.example/steal#x", want: "/"},
		{name: "encoded protocol relative", raw: "/%2F%2Fevil.example/#x", want: "/"},
		{name: "encoded backslash", raw: "/team#bad%5Ctarget", want: "/"},
		{name: "raw backslash", raw: "/team#bad\\target", want: "/"},
		{name: "encoded control", raw: "/team#bad%0d%0atarget", want: "/"},
		{name: "malformed fragment escape", raw: "/team#bad%zz", want: "/"},
		{name: "malformed query escape", raw: "/team?next=%zz#identity", want: "/"},
		{name: "double encoded backslash", raw: "/team#bad%255ctarget", want: "/"},
		{name: "oversized fragment", raw: "/team#" + strings.Repeat("x", maxReturnPathBytes), want: "/"},
		{name: "login endpoint", raw: "/login#identity", want: "/"},
		{name: "oauth callback endpoint", raw: "/auth/google/callback#identity", want: "/"},
		{name: "avatar action endpoint", raw: "/avatar/upload#identity", want: "/"},
		{name: "badge action endpoint", raw: "/avatar/badge#identity", want: "/"},
		{name: "gosx action endpoint", raw: "/team/__actions/save#identity", want: "/"},
		{name: "encoded badge action endpoint", raw: "/avatar%2Fbadge#identity", want: "/"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SafeActionReturnPath(tt.raw); got != tt.want {
				t.Fatalf("SafeActionReturnPath(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
	if got := SafeReturnPath("/team?identity=edit#team-identity"); got != DefaultReturnPath {
		t.Fatalf("SafeReturnPath accepted an action-only fragment target: %q", got)
	}
}

func TestSafeReturnPathBudgetBoundaries(t *testing.T) {
	exact := "/" + strings.Repeat("a", maxReturnPathBytes-1)
	if len(exact) != maxReturnPathBytes {
		t.Fatalf("exact fixture length = %d, want %d", len(exact), maxReturnPathBytes)
	}
	if got := SafeReturnPath(exact); got != exact {
		t.Fatalf("exact-budget target was rejected or changed: len=%d", len(got))
	}

	firstOver := "/" + strings.Repeat("a", maxReturnPathBytes)
	if len(firstOver) != maxReturnPathBytes+1 {
		t.Fatalf("first-over fixture length = %d, want %d", len(firstOver), maxReturnPathBytes+1)
	}
	if got := SafeReturnPath(firstOver); got != DefaultReturnPath {
		t.Fatalf("first-over target = %q, want root fallback", got)
	}
}

func TestSafeReturnPathRejectsEncodedTargetOverBudget(t *testing.T) {
	// Raw spaces are accepted by net/url and are escaped by RequestURI. This
	// stays under the raw budget but crosses it after final encoding, proving
	// the post-parse byte check is active too.
	raw := "/" + strings.Repeat(" ", (maxReturnPathBytes-1)/3+1)
	if len(raw) >= maxReturnPathBytes {
		t.Fatalf("raw fixture unexpectedly exceeds budget: %d", len(raw))
	}
	if got := SafeReturnPath(raw); got != DefaultReturnPath {
		t.Fatalf("encoded-over-budget target = %q, want root fallback", got)
	}
}

func TestLoginPathForRequestPreservesGETAndHEADTargets(t *testing.T) {
	tests := []struct {
		name   string
		method string
		target string
		want   string
	}{
		{name: "page query", method: http.MethodGet, target: "/draft?week=1", want: "/login?next=%2Fdraft%3Fweek%3D1"},
		{name: "head page", method: http.MethodHead, target: "/wire?category=injury", want: "/login?next=%2Fwire%3Fcategory%3Dinjury"},
		{name: "post", method: http.MethodPost, target: "/draft", want: "/login"},
		{name: "api", method: http.MethodGet, target: "/api/data/status", want: "/login?next=%2Fapi%2Fdata%2Fstatus"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.target, nil)
			if got := LoginPathForRequest(req); got != tt.want {
				t.Fatalf("LoginPathForRequest(%s %s) = %q, want %q", tt.method, tt.target, got, tt.want)
			}
		})
	}
}

func TestOAuthStartPathSanitizesDirectCaller(t *testing.T) {
	if got, want := OAuthStartPath("https://evil.example/"), "/auth/google/start?next=%2F"; got != want {
		t.Fatalf("OAuthStartPath(external) = %q, want %q", got, want)
	}
	if got, want := OAuthStartPath("/draft?week=1"), "/auth/google/start?next=%2Fdraft%3Fweek%3D1"; got != want {
		t.Fatalf("OAuthStartPath(page) = %q, want %q", got, want)
	}
}
