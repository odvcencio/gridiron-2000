package draft

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"sync"

	"gridiron-2000/internal/league"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/session"
)

// Practice room wiring (practice draft, internal/league/practice.go).
//
// The practice room is the SAME page.gsx as the real room, rendered from a
// PracticeDraft's own data (league.PracticeDraft.Data) instead of
// league.Default().DraftData: prepareDraftData builds the identical typed
// views, and the room_path/fragment_base/live_src/live_hub keys the sandbox
// stamps on its data point every href, region, action, and live root at the
// endpoints below rather than the real room's. The page itself lives in
// app/draft/practice (its own file module, so it gets the layout, the
// action routes, and CSRF for free); it calls back into this package to
// render because page.gsx's components are this package's own.

// practiceRegion is the practice strip's own region name: the strip is a
// server-rendered part of the command header that re-renders on every
// pick and on the practice's end (the command bar's own live binds carry
// no practice fields).
const practiceRegion = "practice"

var (
	practiceMu       sync.RWMutex
	practiceRegistry *league.PracticeRegistry
)

// InstallPractice publishes the registry app_build.go built, for the
// practice page module (app/draft/practice) to find at request time.
func InstallPractice(registry *league.PracticeRegistry) {
	practiceMu.Lock()
	practiceRegistry = registry
	practiceMu.Unlock()
}

// Practice returns the installed registry, or nil before InstallPractice.
func Practice() *league.PracticeRegistry {
	practiceMu.RLock()
	defer practiceMu.RUnlock()
	return practiceRegistry
}

// practiceFragmentHandler resolves the viewer's own session first, then
// renders region from that session's data. A viewer with no session gets
// 404: the page that polled is on its way to the lobby.
func practiceFragmentHandler(region string, base *league.Service, registry *league.PracticeRegistry) http.Handler {
	allowed := draftFragmentAccess(base)
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet {
			writer.Header().Set("Allow", http.MethodGet)
			http.Error(writer, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		if registry == nil || allowed == nil || !allowed(request) {
			http.Error(writer, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}
		session, ok := registry.Current(request)
		if !ok {
			http.Error(writer, "no practice draft is open", http.StatusNotFound)
			return
		}
		draftFragmentHandler(region, allowed, session.Data).ServeHTTP(writer, request)
	})
}

// PracticeFragmentHandler serves one region of the practice room: the six
// regions the real room's fragment endpoints serve, plus "practice" (the
// strip). Unknown regions answer 500 the way draftRegionView already does.
func PracticeFragmentHandler(region string, base *league.Service, registry *league.PracticeRegistry) http.Handler {
	return practiceFragmentHandler(region, base, registry)
}

// PracticeRegions lists every fragment region the practice room mounts.
func PracticeRegions() []string {
	return []string{draftRoomRegion, draftWorkspaceRegion, draftCommandRegion, draftTapeRegion, draftTapeRowsRegion, draftAvailableRegion, draftQueueRegion, draftPickBarRegion, practiceRegion}
}

// PracticeLiveViewHandler serves GET /draft/practice/live.json: the
// viewer's own sandbox's full bind object, the practice twin of
// LiveViewHandler.
func PracticeLiveViewHandler(base *league.Service, registry *league.PracticeRegistry) http.Handler {
	allowed := draftFragmentAccess(base)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		if registry == nil || allowed == nil || !allowed(r) {
			http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}
		session, ok := registry.Current(r)
		if !ok {
			http.Error(w, "no practice draft is open", http.StatusNotFound)
			return
		}
		body, err := json.Marshal(session.LiveView(r))
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

// PracticeQueueRefusalMessage is the one sentence every board edit inside
// a practice answers with: the board is the manager's REAL board, and the
// practice never writes anything.
const PracticeQueueRefusalMessage = "Edit your Big Board in the real room."

// PracticeQueueRefusalHandler answers POST /draft/practice/queue — the
// drag-reorder runtime's target inside a practice — with the refusal, in
// the same JSON shape queueMoveHandler's own rejection uses, so the
// runtime restores the previous order and shows its error line.
func PracticeQueueRefusalHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		http.Error(w, `{"ok":false,"message":`+fmt.Sprintf("%q", PracticeQueueRefusalMessage)+`}`, http.StatusUnprocessableEntity)
	})
}

// PracticePageData builds the practice room's whole page data for one
// request: the sandbox's draft data through the exact pipeline the real
// page's Load runs (prepareDraftData, the "?view=", "?pick=", and
// request-state passes), then the notice/error keys Page() reads.
func PracticePageData(request *http.Request, practice *league.PracticeDraft) map[string]any {
	data := attachDraftRequestState(attachDraftFragmentPick(attachDraftFragmentView(prepareDraftData(practice.Data(request), request), request), request), request)
	data["has_notice"] = false
	data["notice"] = ""
	if store := session.Current(request); store != nil {
		if flashes := store.Flashes("notice"); len(flashes) > 0 {
			data["has_notice"] = true
			data["notice"] = fmt.Sprint(flashes[0])
		}
	}
	data["has_pick_error"] = false
	data["pick_error"] = ""
	data["force_current_pick_confirm"] = ""
	return data
}

// PracticeRoomProgramPath resolves the real room's page.gsx from the
// practice module's own FilePage: the module lives one directory below
// it. Derived from the router's own resolved path (never runtime.Caller),
// so a -trimpath build finds the same file the router serves.
func PracticeRoomProgramPath(page route.FilePage) string {
	return filepath.Join(filepath.Dir(filepath.Dir(page.FilePath)), "page.gsx")
}

// RenderPracticeRoom renders the real room's Page component with the
// practice data: the same program the real /draft renders, loaded through
// the same stat-keyed cache, so an edit to page.gsx reaches both rooms.
func RenderPracticeRoom(page route.FilePage, data map[string]any) (gosx.Node, error) {
	program, err := route.LoadFileProgram(PracticeRoomProgramPath(page))
	if err != nil {
		return gosx.Node{}, err
	}
	return route.RenderProgramComponentNode(program, "Page", route.ProgramRenderEnv{
		Values: map[string]any{"data": data, "params": map[string]string{}},
	})
}

// PracticeRedirectTarget is draftRedirectTarget for the practice room: a
// practice pick lands back on /draft/practice with the viewer's own pool
// position, search, and page preserved, exactly as a real pick does.
func PracticeRedirectTarget(pos, query, page string) string {
	return draftRedirectTargetFor(league.PracticeRoomPath, pos, query, page)
}
