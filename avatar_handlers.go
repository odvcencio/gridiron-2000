package main

import (
	"bytes"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"gridiron-2000/internal/league"
	"gridiron-2000/internal/navigation"
	"m31labs.dev/gosx/session"
)

// avatarReadLimit bounds how many bytes avatarUploadHandler reads out of
// the uploaded file part into memory. It is deliberately generous versus
// league.AvatarMaxBytes (the real, exact-message business rule enforced
// inside league.Service.UploadAvatar): this is a coarse safety backstop,
// not the "image must be 2MB or smaller" check itself.
const avatarReadLimit = league.AvatarMaxBytes + 64*1024

// avatarMultipartEnvelopeMaxBytes caps the complete multipart request — all
// boundaries, headers, fields, and the avatar file together. It deliberately
// leaves bounded room above AvatarMaxBytes for the form envelope while
// keeping unrelated fields from turning a 2 MB upload into an unbounded body.
const avatarMultipartEnvelopeMaxBytes = league.AvatarMaxBytes + 256*1024

// avatarMultipartEnvelopeLimit is the path-specific outer middleware for the
// native multipart upload. It must run before the session/CSRF middleware's
// FormValue call, because parsing FormValue first would consume an unlimited
// multipart body before this route handler can apply a file-part limit.
// main.go integration is one outermost line, before sessions.Middleware and
// Protect: app.Use(avatarMultipartEnvelopeLimit).
//
// The middleware buffers at most max+1 bytes and replaces the request body
// with those exact bytes for the downstream global middleware and handler.
// Thus Content-Length and chunked requests receive the same whole-envelope
// cap, while malformed-but-small multipart input still reaches the ordinary
// parser and its normal validation path.
func avatarMultipartEnvelopeLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/avatar/upload" || r.Body == nil {
			next.ServeHTTP(w, r)
			return
		}
		if r.ContentLength > avatarMultipartEnvelopeMaxBytes {
			_ = r.Body.Close()
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, avatarMultipartEnvelopeMaxBytes+1))
		_ = r.Body.Close()
		if err != nil {
			http.Error(w, "could not read request body", http.StatusBadRequest)
			return
		}
		if int64(len(body)) > avatarMultipartEnvelopeMaxBytes {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		r.ContentLength = int64(len(body))
		next.ServeHTTP(w, r)
	})
}

type avatarUploader interface {
	UploadAvatar(*http.Request, string, []byte) (league.AvatarUploadResult, error)
}

type avatarReader interface {
	ReadAvatarObject(string, string) ([]byte, time.Time, bool)
	IdentityHealthy() bool
}

// avatarUploadHandler serves POST /avatar/upload as a plain http.Handler
// outside GoSX's managed action registry. GoSX v0.49.0 now has File/Files and
// MaxActionBodyBytes for managed actions, but this route still needs the
// outer complete-multipart cap until the bounded-multipart contract is
// released and adopted by the production consumer. Session and CSRF
// protection still apply here: both are wired as global app.Use middleware in
// main(), ahead of every mount, including this one.
//
// The form ships data-gosx-managed="false" (a native, full-page POST) —
// see app/team/page.gsx and app/admin/page.gsx's SeatRow doc comments.
func avatarUploadHandler(svc avatarUploader) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		rawRedirect := r.FormValue("redirect_to")
		redirectTo := navigation.SafeReturnPath(rawRedirect)
		if rawRedirect == "" || (redirectTo == navigation.DefaultReturnPath && rawRedirect != navigation.DefaultReturnPath) {
			redirectTo = "/team"
		}
		teamID := strings.TrimSpace(r.FormValue("team_id"))
		file, _, err := r.FormFile("avatar")
		if err != nil {
			session.AddFlash(r, "avatar_error", "choose an image to upload")
			http.Redirect(w, r, redirectTo, http.StatusSeeOther)
			return
		}
		defer file.Close()
		data, err := io.ReadAll(io.LimitReader(file, avatarReadLimit+1))
		if err != nil {
			log.Printf("avatar upload: reading file part failed: %v", err)
			session.AddFlash(r, "avatar_error", "could not save the image")
			http.Redirect(w, r, redirectTo, http.StatusSeeOther)
			return
		}
		result, err := svc.UploadAvatar(r, teamID, data)
		if err != nil {
			session.AddFlash(r, "avatar_error", avatarUploadErrorMessage(err))
			http.Redirect(w, r, redirectTo, http.StatusSeeOther)
			return
		}
		notice := "Avatar updated."
		if result.BadgeReleased {
			notice += " This seat’s former badge is now available to the league."
		}
		session.AddFlash(r, "notice", notice)
		http.Redirect(w, r, redirectTo, http.StatusSeeOther)
	})
}

// avatarUploadErrorMessage exposes only the explicit validation/auth contract
// to a browser. Filesystem, database, path, and other operational details go
// to the server log and become one neutral user-facing message.
func avatarUploadErrorMessage(err error) string {
	switch {
	case errors.Is(err, league.ErrAvatarWrongType):
		return league.ErrAvatarWrongType.Error()
	case errors.Is(err, league.ErrAvatarTooLarge):
		return league.ErrAvatarTooLarge.Error()
	case errors.Is(err, league.ErrAvatarBadDimensions):
		return league.ErrAvatarBadDimensions.Error()
	case errors.Is(err, league.ErrAvatarForbidden):
		return league.ErrAvatarForbidden.Error()
	case errors.Is(err, league.ErrPersistenceIndeterminate):
		return identityUnavailableFlash
	default:
		log.Printf("avatar upload failed: %v", err)
		return "could not save the image"
	}
}

// avatarServeHandler serves GET /avatars/custom/{teamID}/{sha256}.png. It
// resolves through the current DB AvatarRef; a legacy file, stale hash, stable
// alias, or guessed team never reaches the filesystem. The opened bytes are
// re-hashed before serving, so local corruption cannot masquerade under a
// valid content address. The URL can be cached for a year because replacing
// an avatar publishes a different hash.
//
// The route is registered as the subtree pattern "GET /avatars/" (see
// main.go), and file is read from r.URL.Path directly rather than
// r.PathValue: gosx's App.Mount dispatch resolves a mounted handler via
// (*http.ServeMux).Handler, and net/http's own doc comment on that method
// is explicit that it "does not populate named path wildcards, so
// r.PathValue will always return the empty string" — a {name} wildcard
// pattern would silently never match anything through Mount.
func avatarServeHandler(svc avatarReader) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		file := strings.TrimPrefix(r.URL.Path, "/avatars/")
		if !svc.IdentityHealthy() {
			http.Error(w, "avatar identity unavailable", http.StatusServiceUnavailable)
			return
		}
		parts := strings.Split(file, "/")
		if len(parts) != 3 || parts[0] != "custom" || !strings.HasSuffix(parts[2], ".png") {
			http.NotFound(w, r)
			return
		}
		teamID := parts[1]
		ref := strings.TrimSuffix(parts[2], ".png")
		data, modTime, ok := svc.ReadAvatarObject(teamID, ref)
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Type", "image/png")
		// Serve the exact bytes that were bounded and hashed above. Reopening
		// or seeking the path after hashing would reintroduce a TOCTOU window.
		http.ServeContent(w, r, parts[2], modTime, bytes.NewReader(data))
	})
}
