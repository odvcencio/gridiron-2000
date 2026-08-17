package main

import (
	"bytes"
	"net/http"
	"strings"
	"time"

	"gridiron-2000/internal/league"
	"m31labs.dev/gosx/session"
)

// badgeUploadHandler serves POST /avatar/badge: a plain http.Handler
// outside gosx's action registry, the same "raw POST + 303 redirect"
// shape avatarUploadHandler uses (see that handler's own doc comment).
// Session and CSRF protection apply here via the same global app.Use
// middleware every other mount gets — see main.go.
//
// The form ships data-gosx-managed="false", mirroring
// app/team/page.gsx's badge-picker cells and app/admin/page.gsx's
// SeatRow avatar-upload form.
//
// A request with action=release, or with an empty (or missing) motif
// field, releases the team's current claim instead of setting one — the
// picker's own "Use default badge" button submits exactly that shape.
func badgeUploadHandler(svc *league.Service) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		redirectTo := strings.TrimSpace(r.FormValue("redirect_to"))
		// Only a same-site, absolute path is accepted: a leading "//" is a
		// scheme-relative URL (an open-redirect vector), not a path — the
		// same guard avatarUploadHandler applies.
		if redirectTo == "" || !strings.HasPrefix(redirectTo, "/") || strings.HasPrefix(redirectTo, "//") {
			redirectTo = "/team"
		}
		teamID := strings.TrimSpace(r.FormValue("team_id"))
		motif := strings.TrimSpace(r.FormValue("motif"))
		action := strings.TrimSpace(r.FormValue("action"))

		var err error
		if action == "release" || motif == "" {
			err = svc.ReleaseBadge(r, teamID)
		} else {
			err = svc.ClaimBadge(r, teamID, motif)
		}
		if err != nil {
			session.AddFlash(r, "avatar_error", err.Error())
			http.Redirect(w, r, redirectTo, http.StatusSeeOther)
			return
		}
		session.AddFlash(r, "notice", "Badge updated.")
		http.Redirect(w, r, redirectTo, http.StatusSeeOther)
	})
}

// badgeServeStartedAt is the fixed instant badgeServeHandler hands
// http.ServeContent as every served render's modification time. A tinted
// badge render has no on-disk file (and therefore no real mtime) to
// report: the served bytes' content, not this timestamp, is what changes
// when a claim changes — the URL's ?v=<motif> query, set by avatarView,
// busts the browser cache instead (see BadgeImage's doc comment). A
// fixed, process-start instant just keeps http.ServeContent's
// conditional-request handling well-defined.
var badgeServeStartedAt = time.Now()

// badgeServeHandler serves GET /avatars/badge/{file}, where file must be
// "{teamID}.png" for a known team with a current badge claim. It is
// registered as the subtree pattern "GET /avatars/badge/" (more specific
// than, and therefore preferred over, the sibling "GET /avatars/"
// subtree — see main.go), and file is read from r.URL.Path directly
// rather than r.PathValue, the same reasoning avatarServeHandler's own
// doc comment gives: gosx's App.Mount dispatch resolves a mounted
// handler via (*http.ServeMux).Handler, which never calls SetPathValue,
// so a {name} wildcard pattern's path values never populate through
// Mount.
//
// Unlike avatarServeHandler, there is no file to os.Open here: the
// tinted PNG is rendered on demand (and cached in memory) by
// league.Service.BadgeImage. A missing claim, or an unrecognized {file}
// value, 404s with no body — the render layer's own avatarView is what
// actually keeps a page from linking here without a claim; this 404 is
// the defense-in-depth backstop, matching avatarServeHandler's own.
func badgeServeHandler(svc *league.Service) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		file := strings.TrimPrefix(r.URL.Path, "/avatars/badge/")
		teamID := strings.TrimSuffix(file, ".png")
		if teamID == "" || teamID == file {
			http.NotFound(w, r)
			return
		}
		data, _, ok := svc.BadgeImage(teamID)
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "public, max-age=86400")
		w.Header().Set("Content-Type", "image/png")
		http.ServeContent(w, r, file, badgeServeStartedAt, bytes.NewReader(data))
	})
}
