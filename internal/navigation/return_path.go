// Package navigation contains small, shared URL helpers for browser-facing
// navigation. Keeping return-path validation here gives the session gate,
// login page, and OAuth handlers one security contract instead of three
// subtly different copies.
package navigation

import (
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"
)

const (
	DefaultReturnPath = "/"
	// maxReturnPathBytes leaves ample room in the encrypted session cookie for
	// OAuth state, PKCE values, and session metadata while keeping the caller's
	// return target comfortably below the browser's 4096-byte cookie limit.
	maxReturnPathBytes = 1024
)

// SafeReturnPath accepts only an absolute path on this application, with an
// optional query string. It deliberately rejects hosts, schemes, opaque
// values, protocol-relative paths, malformed escapes, and control
// characters. Fragments are not accepted because browsers never send them
// to the server.
func SafeReturnPath(raw string) string {
	if raw == "" || len(raw) > maxReturnPathBytes || strings.Contains(raw, "#") || strings.ContainsFunc(raw, unsafeReturnRune) {
		return DefaultReturnPath
	}

	// Require a path-absolute reference with exactly one leading slash before
	// parsing. This rejects absolute and protocol-relative URLs even when the
	// parser would otherwise treat their authority as path data in request
	// mode.
	if !strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "//") {
		return DefaultReturnPath
	}

	parsed, err := url.ParseRequestURI(raw)
	if err != nil || parsed == nil || parsed.Scheme != "" || parsed.Opaque != "" || parsed.Host != "" || parsed.User != nil {
		return DefaultReturnPath
	}
	if parsed.Path == "" || !strings.HasPrefix(parsed.Path, "/") || strings.HasPrefix(parsed.Path, "//") || strings.Contains(parsed.Path, "\\") || !utf8.ValidString(parsed.Path) {
		return DefaultReturnPath
	}
	if hasDoubleEncodedAmbiguity(raw) {
		return DefaultReturnPath
	}

	// ParseRequestURI validates path escapes but deliberately leaves RawQuery
	// untouched. Validate the query separately, then inspect decoded values
	// so encoded controls and backslashes cannot reach a Location header.
	decodedQuery, err := url.QueryUnescape(parsed.RawQuery)
	if err != nil || !utf8.ValidString(decodedQuery) || strings.Contains(decodedQuery, "\\") || strings.ContainsFunc(decodedQuery, unsafeReturnRune) {
		return DefaultReturnPath
	}
	if strings.ContainsFunc(parsed.Path, unsafeReturnRune) {
		return DefaultReturnPath
	}

	// RequestURI preserves the caller's encoded path/query while ensuring any
	// ordinary spaces in a parsed path are escaped for the Location header.
	target := parsed.RequestURI()
	if len(target) > maxReturnPathBytes || !strings.HasPrefix(target, "/") || strings.HasPrefix(target, "//") {
		return DefaultReturnPath
	}
	return target
}

// OAuthStartPath builds the login CTA target from a validated application
// path. QueryEscape is intentionally left to url.Values so the complete
// path and its own query string remain one next= value.
func OAuthStartPath(next string) string {
	values := url.Values{}
	values.Set("next", SafeReturnPath(next))
	return "/auth/google/start?" + values.Encode()
}

// LoginPathForRequest returns the safe login URL for an unauthenticated
// request. A POST cannot safely be replayed after OAuth, so only browser
// GET/HEAD requests carry a destination.
func LoginPathForRequest(r *http.Request) string {
	if r == nil || (r.Method != http.MethodGet && r.Method != http.MethodHead) || r.URL == nil {
		return "/login"
	}
	next := SafeReturnPath(r.URL.RequestURI())
	values := url.Values{}
	values.Set("next", next)
	return "/login?" + values.Encode()
}

func unsafeReturnRune(r rune) bool {
	return r == '\\' || r <= 0x1f || r == 0x7f
}

func hasDoubleEncodedAmbiguity(raw string) bool {
	lower := strings.ToLower(raw)
	return strings.Contains(lower, "%255c") || strings.Contains(lower, "%252f%252f")
}
