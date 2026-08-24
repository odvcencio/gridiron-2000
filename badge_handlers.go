package main

import (
	"bytes"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"gridiron-2000/internal/league"
	"gridiron-2000/internal/navigation"
	"m31labs.dev/gosx/session"
)

type badgeUpdater interface {
	ClaimBadge(*http.Request, string, string) error
	ReleaseBadge(*http.Request, string) error
}

type badgeTransitionUpdater interface {
	ClaimBadgeWithTransition(*http.Request, string, string) (league.BadgeTransition, error)
}

type badgeImageReader interface {
	BadgeImage(string) ([]byte, string, bool)
	IdentityHealthy() bool
}

const identityUnavailableFlash = "Badge and avatar identity is temporarily unavailable. Try again later."

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
func badgeUploadHandler(svc badgeUpdater) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		rawRedirect := r.FormValue("redirect_to")
		redirectTo := navigation.SafeActionReturnPath(rawRedirect)
		if rawRedirect == "" || (redirectTo == navigation.DefaultReturnPath && rawRedirect != navigation.DefaultReturnPath) {
			redirectTo = "/team"
		}
		teamID := strings.TrimSpace(r.FormValue("team_id"))
		motif := strings.TrimSpace(r.FormValue("motif"))
		action := strings.TrimSpace(r.FormValue("action"))

		var (
			err        error
			transition league.BadgeTransition
		)
		if action == "release" || motif == "" {
			err = svc.ReleaseBadge(r, teamID)
		} else if transitionSvc, ok := svc.(badgeTransitionUpdater); ok {
			transition, err = transitionSvc.ClaimBadgeWithTransition(r, teamID, motif)
		} else {
			err = svc.ClaimBadge(r, teamID, motif)
		}
		if err != nil {
			session.AddFlash(r, "avatar_error", badgeErrorMessage(err))
			http.Redirect(w, r, redirectTo, http.StatusSeeOther)
			return
		}
		notice := "Badge updated."
		if transition.AvatarCleared {
			notice = "Badge updated. This seat’s custom avatar was cleared."
		}
		session.AddFlash(r, "notice", notice)
		http.Redirect(w, r, redirectTo, http.StatusSeeOther)
	})
}

func badgeErrorMessage(err error) string {
	switch {
	case errors.Is(err, league.ErrBadgeUnknownMotif):
		return league.ErrBadgeUnknownMotif.Error()
	case errors.Is(err, league.ErrBadgeForbidden):
		return league.ErrBadgeForbidden.Error()
	case errors.Is(err, league.ErrBadgeTaken):
		return err.Error()
	case errors.Is(err, league.ErrPersistenceIndeterminate):
		return identityUnavailableFlash
	default:
		log.Printf("badge update failed: %v", err)
		return "Could not update the badge right now. Try again."
	}
}

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
func badgeServeHandler(svc badgeImageReader) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !svc.IdentityHealthy() {
			http.Error(w, "badge identity unavailable", http.StatusServiceUnavailable)
			return
		}
		file := strings.TrimPrefix(r.URL.Path, "/avatars/badge/")
		teamID := strings.TrimSuffix(file, ".png")
		if teamID == "" || teamID == file {
			http.NotFound(w, r)
			return
		}
		data, version, ok := svc.BadgeImage(teamID)
		if !ok {
			http.NotFound(w, r)
			return
		}
		requestedVersion := strings.TrimSpace(r.URL.Query().Get("v"))
		if requestedVersion != "" {
			// A mutable path with a stale content hash must not return new
			// bytes under an old immutable cache key.
			if requestedVersion != version {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "public, max-age=0, must-revalidate")
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Type", "image/png")
		etag := `"` + version + `"`
		w.Header().Set("ETag", etag)
		if ifNoneMatchIncludes(r.Header.Get("If-None-Match"), etag) {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		http.ServeContent(w, r, file, time.Time{}, bytes.NewReader(data))
	})
}

func ifNoneMatchIncludes(header, etag string) bool {
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "*" || candidate == etag {
			return true
		}
	}
	return false
}
