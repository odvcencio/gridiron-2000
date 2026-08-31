package main

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"gridiron-2000/internal/league"
	"m31labs.dev/gosx/session"
)

type fakeBadgeUpdater struct {
	claimErr      error
	releaseErr    error
	claims        int
	releases      int
	transition    league.BadgeTransition
	transitionSet bool
}

func (f *fakeBadgeUpdater) ClaimBadge(*http.Request, string, string) error {
	f.claims++
	return f.claimErr
}

func (f *fakeBadgeUpdater) ReleaseBadge(*http.Request, string) error {
	f.releases++
	return f.releaseErr
}

func (f *fakeBadgeUpdater) ClaimBadgeWithTransition(r *http.Request, team, motif string) (league.BadgeTransition, error) {
	f.claims++
	if f.transitionSet {
		return f.transition, f.claimErr
	}
	return league.BadgeTransition{}, f.claimErr
}

type fakeBadgeImageReader struct {
	data      []byte
	version   string
	ok        bool
	largeData []byte
	largeVer  string
	largeOK   bool
	healthy   bool
}

func (f fakeBadgeImageReader) BadgeImage(string) ([]byte, string, bool) {
	return f.data, f.version, f.ok
}

func (f fakeBadgeImageReader) BadgeImageLarge(string) ([]byte, string, bool) {
	return f.largeData, f.largeVer, f.largeOK
}

func (f fakeBadgeImageReader) IdentityHealthy() bool { return f.healthy }

func badgeFormRequest(t *testing.T, target, csrf string) *http.Request {
	t.Helper()
	form := url.Values{
		"csrf_token":  {csrf},
		"team_id":     {"team-1"},
		"motif":       {"wolf"},
		"redirect_to": {target},
	}
	req := httptest.NewRequest(http.MethodPost, "http://localhost/avatar/badge", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req
}

func sessionWithCSRFCookie(t *testing.T, name string) (*session.Manager, string, []*http.Cookie) {
	t.Helper()
	manager, err := session.New("badge-handler-"+name+"-secret", session.Options{CookieName: name, AllowInsecure: true})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	var token string
	manager.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token = session.Token(r)
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://localhost/team", nil))
	if token == "" {
		t.Fatal("session middleware did not issue a CSRF token")
	}
	return manager, token, response.Result().Cookies()
}

func TestBadgeUploadHandlerUsesSafeReturnPathAndNeutralOperationalFlash(t *testing.T) {
	fake := &fakeBadgeUpdater{claimErr: errors.New("sqlite /srv/gridiron/data: disk is full")}
	manager, token, cookies := sessionWithCSRFCookie(t, "badge_safe_redirect")
	req := badgeFormRequest(t, "https://evil.example/steal", token)
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	manager.Middleware(manager.Protect(badgeUploadHandler(fake))).ServeHTTP(response, req)
	if response.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", response.Code)
	}
	if got := response.Header().Get("Location"); got != "/team" {
		t.Fatalf("hostile redirect location = %q, want /team", got)
	}
	if fake.claims != 1 {
		t.Fatalf("ClaimBadge calls = %d, want 1", fake.claims)
	}

	follow := httptest.NewRequest(http.MethodGet, "http://localhost/team", nil)
	for _, cookie := range response.Result().Cookies() {
		follow.AddCookie(cookie)
	}
	var flash string
	manager.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		values := session.FlashValues(r)["avatar_error"]
		if len(values) == 1 {
			flash, _ = values[0].(string)
		}
	})).ServeHTTP(httptest.NewRecorder(), follow)
	if flash != "Could not update the badge right now. Try again." {
		t.Fatalf("operational flash = %q, want neutral copy", flash)
	}
	if strings.Contains(flash, "/srv/gridiron") || strings.Contains(flash, "disk is full") {
		t.Fatalf("operational error leaked into flash: %q", flash)
	}
}

func TestBadgeUploadHandlerRunsBehindRealCSRFProtection(t *testing.T) {
	fake := &fakeBadgeUpdater{}
	manager, token, cookies := sessionWithCSRFCookie(t, "badge_real_csrf")
	req := badgeFormRequest(t, "/team?tab=badges", token)
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	manager.Middleware(manager.Protect(badgeUploadHandler(fake))).ServeHTTP(response, req)
	if response.Code != http.StatusSeeOther {
		t.Fatalf("valid CSRF status = %d, want 303", response.Code)
	}
	if got := response.Header().Get("Location"); got != "/team?tab=badges" {
		t.Fatalf("valid redirect location = %q", got)
	}

	bad := badgeFormRequest(t, "/team", "wrong-token")
	for _, cookie := range cookies {
		bad.AddCookie(cookie)
	}
	response = httptest.NewRecorder()
	manager.Middleware(manager.Protect(badgeUploadHandler(fake))).ServeHTTP(response, bad)
	if response.Code == http.StatusSeeOther {
		t.Fatal("invalid CSRF request reached badge handler")
	}
	if fake.claims != 1 {
		t.Fatalf("ClaimBadge calls after invalid CSRF = %d, want 1", fake.claims)
	}
}

func TestBadgeUploadHandlerPreservesIdentityEditorFragment(t *testing.T) {
	fake := &fakeBadgeUpdater{}
	manager, token, cookies := sessionWithCSRFCookie(t, "badge_identity_fragment")
	const target = "/team?identity=edit#team-identity"
	req := badgeFormRequest(t, target, token)
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	manager.Middleware(manager.Protect(badgeUploadHandler(fake))).ServeHTTP(response, req)
	if response.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", response.Code)
	}
	if got := response.Header().Get("Location"); got != target {
		t.Fatalf("identity editor redirect location = %q, want %q", got, target)
	}
}

func TestBadgeUploadHandlerFlashesAvatarClearedTransition(t *testing.T) {
	fake := &fakeBadgeUpdater{
		transition:    league.BadgeTransition{AvatarCleared: true},
		transitionSet: true,
	}
	manager, token, cookies := sessionWithCSRFCookie(t, "badge_transition")
	req := badgeFormRequest(t, "/team", token)
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	manager.Middleware(manager.Protect(badgeUploadHandler(fake))).ServeHTTP(response, req)
	if response.Code != http.StatusSeeOther {
		t.Fatalf("transition status = %d, want 303", response.Code)
	}
	follow := httptest.NewRequest(http.MethodGet, "http://localhost/team", nil)
	for _, cookie := range response.Result().Cookies() {
		follow.AddCookie(cookie)
	}
	var notice string
	manager.Middleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		if values := session.FlashValues(r)["notice"]; len(values) == 1 {
			notice, _ = values[0].(string)
		}
	})).ServeHTTP(httptest.NewRecorder(), follow)
	if notice != "Badge updated. This seat’s custom avatar was cleared." {
		t.Fatalf("transition notice = %q, want explicit avatar-clear copy", notice)
	}
}

func TestBadgeServeHandlerUsesContentHashCacheAndExactBytes(t *testing.T) {
	body := []byte("rendered badge bytes")
	const version = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	handler := badgeServeHandler(fakeBadgeImageReader{data: body, version: version, ok: true, healthy: true})
	req := httptest.NewRequest(http.MethodGet, "http://localhost/avatars/badge/team-1.png?v="+version, nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	if response.Code != http.StatusOK || !bytes.Equal(response.Body.Bytes(), body) {
		t.Fatalf("served response = status %d body %q, want exact badge bytes", response.Code, response.Body.Bytes())
	}
	if got := response.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Fatalf("immutable Cache-Control = %q", got)
	}
	if got := response.Header().Get("ETag"); got != `"`+version+`"` {
		t.Fatalf("ETag = %q", got)
	}
	if got := response.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("nosniff = %q", got)
	}

	notModified := httptest.NewRecorder()
	conditional := httptest.NewRequest(http.MethodGet, "http://localhost/avatars/badge/team-1.png?v="+version, nil)
	conditional.Header.Set("If-None-Match", `"`+version+`"`)
	handler.ServeHTTP(notModified, conditional)
	if notModified.Code != http.StatusNotModified || notModified.Body.Len() != 0 {
		t.Fatalf("conditional response = status %d body %q, want 304 empty", notModified.Code, notModified.Body.Bytes())
	}

	stale := httptest.NewRecorder()
	handler.ServeHTTP(stale, httptest.NewRequest(http.MethodGet, "http://localhost/avatars/badge/team-1.png?v=ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff", nil))
	if stale.Code != http.StatusNotFound {
		t.Fatalf("stale version status = %d, want 404", stale.Code)
	}
	if stale.Header().Get("ETag") != "" || strings.HasPrefix(stale.Header().Get("Content-Type"), "image/") {
		t.Fatalf("stale version leaked render headers: ETag=%q Content-Type=%q", stale.Header().Get("ETag"), stale.Header().Get("Content-Type"))
	}

	mutable := httptest.NewRecorder()
	handler.ServeHTTP(mutable, httptest.NewRequest(http.MethodGet, "http://localhost/avatars/badge/team-1.png", nil))
	if got := mutable.Header().Get("Cache-Control"); got != "public, max-age=0, must-revalidate" {
		t.Fatalf("unversioned Cache-Control = %q", got)
	}
	if got := mutable.Body.Bytes(); !bytes.Equal(got, body) {
		t.Fatalf("unversioned body = %q, want exact bytes", got)
	}
}

// TestBadgeServeHandlerLargeSuffixRoutesToBadgeImageLarge checks that a
// "{teamID}-lg.png" request serves BadgeImageLarge's bytes, not
// BadgeImage's — the one large-variant surface avatarViewLarge's own
// tests exercise from the league package side.
func TestBadgeServeHandlerLargeSuffixRoutesToBadgeImageLarge(t *testing.T) {
	smallBody := []byte("small badge bytes")
	largeBody := []byte("large badge bytes")
	const largeVersion = "fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210"
	handler := badgeServeHandler(fakeBadgeImageReader{
		data: smallBody, version: "small-version", ok: true,
		largeData: largeBody, largeVer: largeVersion, largeOK: true,
		healthy: true,
	})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://localhost/avatars/badge/team-1-lg.png?v="+largeVersion, nil))
	if response.Code != http.StatusOK || !bytes.Equal(response.Body.Bytes(), largeBody) {
		t.Fatalf("large-suffix response = status %d body %q, want the large-render bytes", response.Code, response.Body.Bytes())
	}
	if got := response.Header().Get("ETag"); got != `"`+largeVersion+`"` {
		t.Fatalf("large-suffix ETag = %q, want the large version", got)
	}
}

func TestBadgeServeHandlerReturnsUnavailableWhenIdentityIsUnhealthy(t *testing.T) {
	handler := badgeServeHandler(fakeBadgeImageReader{healthy: false})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://localhost/avatars/badge/team-1.png", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("unhealthy badge status = %d, want 503", response.Code)
	}
}

func TestBadgeErrorMessageKeepsKnownErrorsUseful(t *testing.T) {
	if got := badgeErrorMessage(league.ErrBadgeUnknownMotif); got != league.ErrBadgeUnknownMotif.Error() {
		t.Fatalf("unknown motif message = %q", got)
	}
	if got := badgeErrorMessage(league.ErrPersistenceIndeterminate); got != identityUnavailableFlash {
		t.Fatalf("poison message = %q", got)
	}
}
