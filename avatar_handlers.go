package main

import (
	"io"
	"net/http"
	"os"
	"strings"

	"gridiron-2000/internal/league"
	"m31labs.dev/gosx/session"
)

// avatarReadLimit bounds how many bytes avatarUploadHandler reads out of
// the uploaded file part into memory. It is deliberately generous versus
// league.AvatarMaxBytes (the real, exact-message business rule enforced
// inside league.Service.UploadAvatar): this is a coarse safety backstop,
// not the "image must be 2MB or smaller" check itself.
const avatarReadLimit = league.AvatarMaxBytes + 64*1024

// avatarUploadHandler serves POST /avatar/upload: a plain http.Handler
// outside gosx's action registry (m31labs.dev/gosx/action). That registry's
// ServeHandler hard-caps every request body at 1 MiB
// (action.maxActionBodyBytes, unexported but load-bearing: see
// m31labs.dev/gosx@v0.42.2/action/action.go), well under the 2 MB avatar
// limit this feature needs, and there is no ctx.Files seam yet to read a
// raw upload through route.FileActions at any size (gosx#187). Session and
// CSRF protection still apply here: both are wired as global app.Use
// middleware in main(), ahead of every mount, including this one.
//
// The form ships data-gosx-managed="false" (a native, full-page POST) —
// see app/team/page.gsx and app/admin/page.gsx's SeatRow doc comments.
// Reading gosx's navigation-runtime source (client/runtime/host/navigation.ts)
// shows the managed-fetch path actually would carry the file correctly (it
// passes the form's real FormData straight to fetch's body, which the
// Fetch API multipart-encodes with the File blob intact) — so the brief's
// "may or may not carry files" uncertainty resolves in files' favor at the
// JS layer. The blocker is server-side: reusing the managed contract here
// would mean hand-rolling action.Result's JSON shape and redirect handling
// outside the framework's own tested dispatch, a materially larger and
// riskier surface than a plain multipart POST + 303 redirect for a
// rehearsal-week ship. TODO(gosx#187): once ctx.Files lands upstream and
// the action-body cap question is resolved, revisit moving this to the
// managed path.
func avatarUploadHandler(svc *league.Service) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		redirectTo := strings.TrimSpace(r.FormValue("redirect_to"))
		// Only a same-site, absolute path is accepted: a leading "//" is a
		// scheme-relative URL (an open-redirect vector), not a path.
		if redirectTo == "" || !strings.HasPrefix(redirectTo, "/") || strings.HasPrefix(redirectTo, "//") {
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
			session.AddFlash(r, "avatar_error", "could not read the uploaded file")
			http.Redirect(w, r, redirectTo, http.StatusSeeOther)
			return
		}
		if _, err := svc.UploadAvatar(r, teamID, data); err != nil {
			session.AddFlash(r, "avatar_error", err.Error())
			http.Redirect(w, r, redirectTo, http.StatusSeeOther)
			return
		}
		session.AddFlash(r, "notice", "Avatar updated.")
		http.Redirect(w, r, redirectTo, http.StatusSeeOther)
	})
}

// avatarServeHandler serves GET /avatars/{file}, where file must be
// "{teamID}.png" for a known team (design decision 3). It serves the
// persisted upload straight from disk with a fixed 24-hour public cache
// lifetime; the render layer's ?v= query (see league.Service.AvatarVersion)
// busts that cache the instant a replacement avatar is written, since the
// query string — not the path — is what changes. A missing file or an
// unrecognized {file} value 404s with no body, so a page never renders an
// <img> pointing at a URL this handler cannot serve (the render layer's own
// has_avatar_image gate is what actually keeps that from happening — this
// 404 is the defense-in-depth backstop, not the primary control).
//
// The route is registered as the subtree pattern "GET /avatars/" (see
// main.go), and file is read from r.URL.Path directly rather than
// r.PathValue: gosx's App.Mount dispatch resolves a mounted handler via
// (*http.ServeMux).Handler, and net/http's own doc comment on that method
// is explicit that it "does not populate named path wildcards, so
// r.PathValue will always return the empty string" — a {name} wildcard
// pattern would silently never match anything through Mount.
func avatarServeHandler(svc *league.Service) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		file := strings.TrimPrefix(r.URL.Path, "/avatars/")
		teamID := strings.TrimSuffix(file, ".png")
		if teamID == "" || teamID == file {
			http.NotFound(w, r)
			return
		}
		path, ok := svc.AvatarFilePath(teamID)
		if !ok {
			http.NotFound(w, r)
			return
		}
		f, err := os.Open(path)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "public, max-age=86400")
		w.Header().Set("Content-Type", "image/png")
		http.ServeContent(w, r, file, info.ModTime(), f)
	})
}
