package draft

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"gridiron-2000/internal/league"
)

// queueMoveHandler serves POST /draft/queue for data-gosx-reorder. It
// answers JSON and never redirects. session.Protect (app.Use, app_build.go)
// already checked the X-CSRF-Token the reorder runtime sends
// (navigation.ts:5127) before this handler runs.
func queueMoveHandler(move func(*http.Request, string, int) error) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		index, err := strconv.Atoi(strings.TrimSpace(r.FormValue("index")))
		if err != nil {
			http.Error(w, `{"ok":false,"message":"invalid position"}`, http.StatusBadRequest)
			return
		}
		if move == nil {
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		if err := move(r, strings.TrimSpace(r.FormValue("item_id")), index); err != nil {
			http.Error(w, `{"ok":false,"message":`+strconv.Quote(err.Error())+`}`, http.StatusUnprocessableEntity)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
}

// QueueMoveHandler wires the my-team pane's reorder control
// (data-gosx-reorder-action="POST /draft/queue", page.gsx) to
// Store.BoardMoveTo through the service: authorization and validation are
// BoardMoveTo's own, the same ownership rules queue-add/queue-remove
// already carry (page.server.go); this handler is routing only.
func QueueMoveHandler(service *league.Service) http.Handler {
	return queueMoveHandler(service.BoardMoveTo)
}

// LiveViewHandler serves GET /draft/live.json: the same full bind object a
// stale hub reconnect's repair receives (DraftLiveView, draft_events.go),
// so a manager (or an external tool) can always fetch the room's
// authoritative current state over plain HTTP. GET only, gated the same
// way the shell's own fragments are (draftFragmentAccess); private,
// no-store, with an ETag over the served JSON so a repeat fetch between
// two authoritative changes can 304.
func LiveViewHandler(service *league.Service) http.Handler {
	allowed := draftFragmentAccess(service)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		if allowed == nil || !allowed(r) {
			http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}
		body, err := json.Marshal(service.DraftLiveView(r))
		if err != nil {
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		digest := sha256.Sum256(body)
		etag := `"` + hex.EncodeToString(digest[:]) + `"`
		w.Header().Set("Cache-Control", "private, no-store")
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Vary", "Cookie")
		w.Header().Set("ETag", etag)
		if etagMatches(r.Header.Get("If-None-Match"), etag) {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	})
}
