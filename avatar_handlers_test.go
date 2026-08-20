package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gridiron-2000/internal/league"
	"m31labs.dev/gosx/session"
)

type fakeAvatarUploader struct {
	result league.AvatarUploadResult
	err    error
	calls  int
}

func (f *fakeAvatarUploader) UploadAvatar(_ *http.Request, _ string, _ []byte) (league.AvatarUploadResult, error) {
	f.calls++
	return f.result, f.err
}

func avatarMultipartRequest(t *testing.T) *http.Request {
	return avatarMultipartRequestTo(t, "/team")
}

func avatarMultipartRequestTo(t *testing.T, redirectTo string) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("team_id", "team-1"); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteField("redirect_to", redirectTo); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteField("csrf_token", "test-token"); err != nil {
		t.Fatal(err)
	}
	part, err := writer.CreateFormFile("avatar", "team.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("normalized test bytes")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "http://localhost/avatar/upload", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	return request
}

func avatarMultipartRequestWithParts(t *testing.T, avatar []byte, extraName string, extra []byte) *http.Request {
	return avatarMultipartRequestWithCSRF(t, "test-token", avatar, extraName, extra)
}

func avatarMultipartRequestWithCSRF(t *testing.T, csrfToken string, avatar []byte, extraName string, extra []byte) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("team_id", "team-1"); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteField("redirect_to", "/team"); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteField("csrf_token", csrfToken); err != nil {
		t.Fatal(err)
	}
	part, err := writer.CreateFormFile("avatar", "team.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(avatar); err != nil {
		t.Fatal(err)
	}
	if extraName != "" {
		if extraName == "notes" {
			if err := writer.WriteField(extraName, string(extra)); err != nil {
				t.Fatal(err)
			}
		} else {
			extraPart, err := writer.CreateFormFile(extraName, "unrelated.bin")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := extraPart.Write(extra); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "http://localhost/avatar/upload", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	return request
}

func TestAvatarMultipartEnvelopeLimitRunsBeforeFormValueAndAllowsBoundedFile(t *testing.T) {
	fake := &fakeAvatarUploader{}
	formValueCalled := false
	downstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		formValueCalled = true
		_ = r.FormValue("csrf_token")
		avatarUploadHandler(fake).ServeHTTP(w, r)
	})
	manager, err := session.New("avatar-envelope-valid-secret", session.Options{CookieName: "avatar_envelope_valid", AllowInsecure: true})
	if err != nil {
		t.Fatal(err)
	}
	request := avatarMultipartRequestWithParts(t, []byte("valid file bytes"), "", nil)
	response := httptest.NewRecorder()
	avatarMultipartEnvelopeLimit(manager.Middleware(downstream)).ServeHTTP(response, request)
	if response.Code != http.StatusSeeOther {
		t.Fatalf("bounded upload status = %d, want 303", response.Code)
	}
	if !formValueCalled || fake.calls != 1 {
		t.Fatalf("bounded upload downstream calls = formValue:%v upload:%d, want true and 1", formValueCalled, fake.calls)
	}
}

func TestAvatarMultipartEnvelopeLimitPreservesExactFileTooLargeMessage(t *testing.T) {
	fake := &fakeAvatarUploader{err: league.ErrAvatarTooLarge}
	manager, err := session.New("avatar-envelope-file-secret", session.Options{CookieName: "avatar_envelope_file", AllowInsecure: true})
	if err != nil {
		t.Fatal(err)
	}
	// The file itself is over the business limit but still below the complete
	// envelope cap, so the outer middleware must let the handler reach the
	// exact image-size validation.
	request := avatarMultipartRequestWithParts(t, bytes.Repeat([]byte("x"), league.AvatarMaxBytes+1), "", nil)
	response := httptest.NewRecorder()
	avatarMultipartEnvelopeLimit(manager.Middleware(avatarUploadHandler(fake))).ServeHTTP(response, request)
	if response.Code != http.StatusSeeOther {
		t.Fatalf("oversized-file status = %d, want 303", response.Code)
	}
	if fake.calls != 1 {
		t.Fatalf("oversized-file UploadAvatar calls = %d, want 1", fake.calls)
	}
	follow := httptest.NewRequest(http.MethodGet, "http://localhost/team", nil)
	for _, cookie := range response.Result().Cookies() {
		follow.AddCookie(cookie)
	}
	var failure string
	manager.Middleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		if values := session.FlashValues(r)["avatar_error"]; len(values) == 1 {
			failure, _ = values[0].(string)
		}
	})).ServeHTTP(httptest.NewRecorder(), follow)
	if failure != league.ErrAvatarTooLarge.Error() {
		t.Fatalf("oversized-file flash = %q, want %q", failure, league.ErrAvatarTooLarge)
	}
}

func TestAvatarMultipartEnvelopeLimitRejectsHugeUnrelatedPartBeforeFormValue(t *testing.T) {
	fake := &fakeAvatarUploader{}
	formValueCalled := false
	downstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		formValueCalled = true
		_ = r.FormValue("csrf_token")
		avatarUploadHandler(fake).ServeHTTP(w, r)
	})
	request := avatarMultipartRequestWithParts(t, []byte("tiny avatar"), "unrelated", bytes.Repeat([]byte("x"), avatarMultipartEnvelopeMaxBytes))
	response := httptest.NewRecorder()
	avatarMultipartEnvelopeLimit(downstream).ServeHTTP(response, request)
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("huge unrelated part status = %d, want 413", response.Code)
	}
	if formValueCalled || fake.calls != 0 {
		t.Fatalf("huge unrelated part reached downstream: formValue:%v upload:%d", formValueCalled, fake.calls)
	}
}

func TestAvatarMultipartEnvelopeLimitRejectsHugeFieldBeforeFormValue(t *testing.T) {
	fake := &fakeAvatarUploader{}
	formValueCalled := false
	downstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		formValueCalled = true
		_ = r.FormValue("csrf_token")
		avatarUploadHandler(fake).ServeHTTP(w, r)
	})
	request := avatarMultipartRequestWithParts(t, []byte("tiny avatar"), "notes", bytes.Repeat([]byte("x"), avatarMultipartEnvelopeMaxBytes))
	response := httptest.NewRecorder()
	avatarMultipartEnvelopeLimit(downstream).ServeHTTP(response, request)
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("huge field status = %d, want 413", response.Code)
	}
	if formValueCalled || fake.calls != 0 {
		t.Fatalf("huge field reached downstream: formValue:%v upload:%d", formValueCalled, fake.calls)
	}
}

func TestAvatarMultipartEnvelopeLimitCatchesLyingChunkedBodies(t *testing.T) {
	tests := []struct {
		name string
		body func() *http.Request
	}{
		{
			name: "huge unrelated part",
			body: func() *http.Request {
				request := avatarMultipartRequestWithParts(t, []byte("tiny avatar"), "unrelated", bytes.Repeat([]byte("x"), avatarMultipartEnvelopeMaxBytes))
				request.ContentLength = -1
				return request
			},
		},
		{
			name: "huge field",
			body: func() *http.Request {
				request := avatarMultipartRequestWithParts(t, []byte("tiny avatar"), "notes", bytes.Repeat([]byte("x"), avatarMultipartEnvelopeMaxBytes))
				request.ContentLength = avatarMultipartEnvelopeMaxBytes - 1
				return request
			},
		},
		{
			name: "malformed overcap",
			body: func() *http.Request {
				request := httptest.NewRequest(http.MethodPost, "http://localhost/avatar/upload", strings.NewReader(strings.Repeat("x", avatarMultipartEnvelopeMaxBytes+64)))
				request.Header.Set("Content-Type", "multipart/form-data; boundary=missing")
				request.ContentLength = avatarMultipartEnvelopeMaxBytes - 1
				return request
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			downstream := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true })
			response := httptest.NewRecorder()
			avatarMultipartEnvelopeLimit(downstream).ServeHTTP(response, tc.body())
			if response.Code != http.StatusRequestEntityTooLarge {
				t.Fatalf("status = %d, want 413", response.Code)
			}
			if called {
				t.Fatal("lying-under-cap body reached downstream")
			}
		})
	}
}

func TestAvatarMultipartEnvelopeLimitRunsOutsideRealGoSXSessionProtection(t *testing.T) {
	fake := &fakeAvatarUploader{}
	manager, err := session.New("avatar-envelope-real-csrf-secret", session.Options{CookieName: "avatar_envelope_real_csrf", AllowInsecure: true})
	if err != nil {
		t.Fatal(err)
	}
	getResponse := httptest.NewRecorder()
	var token string
	manager.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token = session.Token(r)
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(getResponse, httptest.NewRequest(http.MethodGet, "http://localhost/team", nil))
	if token == "" {
		t.Fatal("real session middleware did not issue a CSRF token")
	}
	request := avatarMultipartRequestWithCSRF(t, token, []byte("valid file bytes"), "", nil)
	for _, cookie := range getResponse.Result().Cookies() {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	// This is the production order: the envelope helper is outside both the
	// session loader and CSRF FormValue parser, while the handler still gets a
	// complete multipart body and can call FormFile.
	avatarMultipartEnvelopeLimit(manager.Middleware(manager.Protect(avatarUploadHandler(fake)))).ServeHTTP(response, request)
	if response.Code != http.StatusSeeOther {
		t.Fatalf("real CSRF multipart status = %d, want 303", response.Code)
	}
	if fake.calls != 1 {
		t.Fatalf("real CSRF multipart UploadAvatar calls = %d, want 1", fake.calls)
	}
}

func TestAvatarMultipartEnvelopeLimitRejectsOversizedBodiesThroughRealSessionProtection(t *testing.T) {
	manager, err := session.New("avatar-envelope-real-oversize-secret", session.Options{CookieName: "avatar_envelope_real_oversize", AllowInsecure: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, testCase := range []struct {
		name string
		body func() *http.Request
	}{
		{
			name: "chunked huge unrelated part",
			body: func() *http.Request {
				request := avatarMultipartRequestWithParts(t, []byte("tiny avatar"), "unrelated", bytes.Repeat([]byte("x"), avatarMultipartEnvelopeMaxBytes))
				request.ContentLength = -1
				return request
			},
		},
		{
			name: "lying content length malformed body",
			body: func() *http.Request {
				request := httptest.NewRequest(http.MethodPost, "http://localhost/avatar/upload", strings.NewReader(strings.Repeat("x", avatarMultipartEnvelopeMaxBytes+64)))
				request.Header.Set("Content-Type", "multipart/form-data; boundary=missing")
				request.ContentLength = avatarMultipartEnvelopeMaxBytes - 1
				return request
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fake := &fakeAvatarUploader{}
			downstreamCalled := false
			downstream := manager.Middleware(manager.Protect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				downstreamCalled = true
				avatarUploadHandler(fake).ServeHTTP(w, r)
			})))
			response := httptest.NewRecorder()
			avatarMultipartEnvelopeLimit(downstream).ServeHTTP(response, testCase.body())
			if response.Code != http.StatusRequestEntityTooLarge {
				t.Fatalf("status = %d, want 413", response.Code)
			}
			if downstreamCalled || fake.calls != 0 {
				t.Fatalf("oversized body reached real session chain: downstream=%v upload=%d", downstreamCalled, fake.calls)
			}
		})
	}
}

func TestAvatarMultipartEnvelopeLimitPassesSmallMalformedBodyToParser(t *testing.T) {
	fake := &fakeAvatarUploader{}
	formValueCalled := false
	downstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		formValueCalled = true
		_ = r.FormValue("csrf_token")
		avatarUploadHandler(fake).ServeHTTP(w, r)
	})
	manager, err := session.New("avatar-envelope-malformed-secret", session.Options{CookieName: "avatar_envelope_malformed", AllowInsecure: true})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "http://localhost/avatar/upload", strings.NewReader("not multipart"))
	request.Header.Set("Content-Type", "multipart/form-data; boundary=missing")
	response := httptest.NewRecorder()
	avatarMultipartEnvelopeLimit(manager.Middleware(downstream)).ServeHTTP(response, request)
	if response.Code != http.StatusSeeOther {
		t.Fatalf("small malformed body status = %d, want handler redirect 303", response.Code)
	}
	if !formValueCalled || fake.calls != 0 {
		t.Fatalf("small malformed body downstream = formValue:%v upload:%d, want true and 0", formValueCalled, fake.calls)
	}
}

func TestAvatarUploadHandlerSanitizesRedirectBoundary(t *testing.T) {
	fake := &fakeAvatarUploader{}
	manager, err := session.New("avatar-handler-redirect-secret", session.Options{CookieName: "avatar_handler_redirect", AllowInsecure: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, testCase := range []struct {
		raw  string
		want string
	}{
		{raw: "/admin?seat=team-1", want: "/admin?seat=team-1"},
		{raw: "", want: "/team"},
		{raw: "https://evil.example/", want: "/team"},
		{raw: "//evil.example/", want: "/team"},
		{raw: `/%5c%5cevil.example`, want: "/team"},
		{raw: " /admin", want: "/team"},
	} {
		response := httptest.NewRecorder()
		manager.Middleware(avatarUploadHandler(fake)).ServeHTTP(response, avatarMultipartRequestTo(t, testCase.raw))
		if got := response.Header().Get("Location"); got != testCase.want {
			t.Errorf("redirect for %q = %q, want %q", testCase.raw, got, testCase.want)
		}
	}
}

func TestAvatarUploadHandlerFlashesNeutralBadgeReleaseCopy(t *testing.T) {
	fake := &fakeAvatarUploader{result: league.AvatarUploadResult{BadgeReleased: true}}
	manager, err := session.New("avatar-handler-test-secret", session.Options{CookieName: "avatar_handler", AllowInsecure: true})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	manager.Middleware(avatarUploadHandler(fake)).ServeHTTP(response, avatarMultipartRequest(t))
	if response.Code != http.StatusSeeOther {
		t.Fatalf("upload status = %d, want 303", response.Code)
	}
	if got := response.Header().Get("Location"); got != "/team" {
		t.Fatalf("redirect location = %q, want /team", got)
	}
	if fake.calls != 1 {
		t.Fatalf("UploadAvatar calls = %d, want 1", fake.calls)
	}

	follow := httptest.NewRequest(http.MethodGet, "http://localhost/team", nil)
	for _, cookie := range response.Result().Cookies() {
		follow.AddCookie(cookie)
	}
	var notice string
	reader := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		values := session.FlashValues(r)["notice"]
		if len(values) == 1 {
			notice, _ = values[0].(string)
		}
	})
	manager.Middleware(reader).ServeHTTP(httptest.NewRecorder(), follow)
	const want = "Avatar updated. This seat’s former badge is now available to the league."
	if notice != want {
		t.Fatalf("notice = %q, want %q", notice, want)
	}
}

func TestAvatarUploadHandlerUsesPlainSuccessWhenNoBadgeWasReleased(t *testing.T) {
	fake := &fakeAvatarUploader{result: league.AvatarUploadResult{BadgeReleased: false}}
	manager, err := session.New("avatar-handler-no-badge-secret", session.Options{CookieName: "avatar_handler_no_badge", AllowInsecure: true})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	manager.Middleware(avatarUploadHandler(fake)).ServeHTTP(response, avatarMultipartRequest(t))
	follow := httptest.NewRequest(http.MethodGet, "http://localhost/team", nil)
	for _, cookie := range response.Result().Cookies() {
		follow.AddCookie(cookie)
	}
	var notice string
	manager.Middleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		values := session.FlashValues(r)["notice"]
		if len(values) == 1 {
			notice, _ = values[0].(string)
		}
	})).ServeHTTP(httptest.NewRecorder(), follow)
	if notice != "Avatar updated." {
		t.Fatalf("notice = %q, want plain success without a fictional badge release", notice)
	}
}

func TestAvatarUploadHandlerDoesNotFlashSuccessOnFailure(t *testing.T) {
	fake := &fakeAvatarUploader{err: league.ErrAvatarWrongType}
	manager, err := session.New("avatar-handler-failure-secret", session.Options{CookieName: "avatar_handler_failure", AllowInsecure: true})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	manager.Middleware(avatarUploadHandler(fake)).ServeHTTP(response, avatarMultipartRequest(t))
	if response.Code != http.StatusSeeOther {
		t.Fatalf("upload status = %d, want 303", response.Code)
	}
	follow := httptest.NewRequest(http.MethodGet, "http://localhost/team", nil)
	for _, cookie := range response.Result().Cookies() {
		follow.AddCookie(cookie)
	}
	var notice, failure string
	reader := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flashes := session.FlashValues(r)
		if values := flashes["notice"]; len(values) == 1 {
			notice, _ = values[0].(string)
		}
		if values := flashes["avatar_error"]; len(values) == 1 {
			failure, _ = values[0].(string)
		}
	})
	manager.Middleware(reader).ServeHTTP(httptest.NewRecorder(), follow)
	if notice != "" {
		t.Fatalf("failed upload emitted success notice %q", notice)
	}
	if failure != league.ErrAvatarWrongType.Error() {
		t.Fatalf("failure flash = %q, want %q", failure, league.ErrAvatarWrongType)
	}
}

func TestAvatarUploadHandlerMapsOperationalErrorsToNeutralFlash(t *testing.T) {
	fake := &fakeAvatarUploader{err: errors.New("open /srv/gridiron/data/avatars/objects: permission denied")}
	manager, err := session.New("avatar-handler-operational-secret", session.Options{CookieName: "avatar_handler_operational", AllowInsecure: true})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	manager.Middleware(avatarUploadHandler(fake)).ServeHTTP(response, avatarMultipartRequest(t))
	follow := httptest.NewRequest(http.MethodGet, "http://localhost/team", nil)
	for _, cookie := range response.Result().Cookies() {
		follow.AddCookie(cookie)
	}
	var failure string
	manager.Middleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		if values := session.FlashValues(r)["avatar_error"]; len(values) == 1 {
			failure, _ = values[0].(string)
		}
	})).ServeHTTP(httptest.NewRecorder(), follow)
	if failure != "could not save the image" {
		t.Fatalf("operational failure flash = %q, want neutral message", failure)
	}
	if strings.Contains(failure, "/srv/gridiron") || strings.Contains(failure, "permission denied") {
		t.Fatalf("operational failure leaked server detail: %q", failure)
	}
}

func TestAvatarUploadHandlerUsesSharedUnavailableCopyForIndeterminateIdentity(t *testing.T) {
	fake := &fakeAvatarUploader{err: league.ErrPersistenceIndeterminate}
	manager, err := session.New("avatar-handler-indeterminate-secret", session.Options{CookieName: "avatar_handler_indeterminate", AllowInsecure: true})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	manager.Middleware(avatarUploadHandler(fake)).ServeHTTP(response, avatarMultipartRequest(t))
	follow := httptest.NewRequest(http.MethodGet, "http://localhost/team", nil)
	for _, cookie := range response.Result().Cookies() {
		follow.AddCookie(cookie)
	}
	var failure string
	manager.Middleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		if values := session.FlashValues(r)["avatar_error"]; len(values) == 1 {
			failure, _ = values[0].(string)
		}
	})).ServeHTTP(httptest.NewRecorder(), follow)
	if failure != identityUnavailableFlash {
		t.Fatalf("indeterminate avatar flash = %q, want %q", failure, identityUnavailableFlash)
	}
}

type fakeAvatarReader struct {
	healthy bool
	team    string
	ref     string
	path    string
}

func (f fakeAvatarReader) IdentityHealthy() bool { return f.healthy }

func (f fakeAvatarReader) ReadAvatarObject(team, ref string) ([]byte, time.Time, bool) {
	if team != f.team || ref != f.ref {
		return nil, time.Time{}, false
	}
	file, err := os.Open(f.path)
	if err != nil {
		return nil, time.Time{}, false
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, time.Time{}, false
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o222 != 0 {
		return nil, time.Time{}, false
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, time.Time{}, false
	}
	if len(data) > league.AvatarMaxBytes {
		return nil, time.Time{}, false
	}
	digest := sha256.Sum256(data)
	if hex.EncodeToString(digest[:]) != ref {
		return nil, time.Time{}, false
	}
	return data, info.ModTime(), true
}

func TestAvatarServeHandlerImmutableRoutesFailClosed(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "avatar.png")
	body := []byte("png bytes")
	if err := os.WriteFile(path, body, 0o444); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(body)
	ref := hex.EncodeToString(digest[:])
	reader := fakeAvatarReader{healthy: true, team: "team-1", ref: ref, path: path}
	handler := avatarServeHandler(reader)

	route := "/avatars/custom/team-1/" + ref + ".png"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://localhost"+route, nil))
	if response.Code != http.StatusOK {
		t.Fatalf("%s status = %d, want 200", route, response.Code)
	}
	if got := response.Body.String(); got != "png bytes" {
		t.Fatalf("%s body = %q", route, got)
	}
	if got := response.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Fatalf("%s Cache-Control = %q", route, got)
	}
	if got := response.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("%s nosniff = %q", route, got)
	}

	for _, route := range []string{
		"/avatars/custom/team-1/ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff.png",
		"/avatars/custom/team-1/not-a-ref.png",
		"/avatars/team-1.png",
		"/avatars/team-2.png",
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://localhost"+route, nil))
		if response.Code != http.StatusNotFound {
			t.Fatalf("stale route %s status = %d, want 404", route, response.Code)
		}
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("corrupt"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o444); err != nil {
		t.Fatal(err)
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://localhost"+route, nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("corrupt object status = %d, want 404", response.Code)
	}
	// A corrupt oversized object is rejected before ServeContent and never
	// gets a chance to stream an unbounded file.
	oversized := bytes.Repeat([]byte("x"), league.AvatarMaxBytes+1)
	oversizedDigest := sha256.Sum256(oversized)
	oversizedPath := filepath.Join(root, "oversized.png")
	if err := os.WriteFile(oversizedPath, oversized, 0o444); err != nil {
		t.Fatal(err)
	}
	reader = fakeAvatarReader{healthy: true, team: "team-1", ref: hex.EncodeToString(oversizedDigest[:]), path: oversizedPath}
	handler = avatarServeHandler(reader)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://localhost/avatars/custom/team-1/"+reader.ref+".png", nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("oversized object status = %d, want 404", response.Code)
	}
	reader.healthy = false
	response = httptest.NewRecorder()
	handler = avatarServeHandler(reader)
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://localhost"+route, nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("unhealthy route status = %d, want 503", response.Code)
	}
}
