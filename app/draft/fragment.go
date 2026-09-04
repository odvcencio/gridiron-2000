package draft

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"gridiron-2000/app/liveaccess"
	"gridiron-2000/internal/league"
	"m31labs.dev/gosx/route"
)

const (
	draftRoomRegion      = "room"
	draftWorkspaceRegion = "workspace"
	draftCommandRegion   = "command"
	draftTapeRegion      = "tape"
	draftTapeRowsRegion  = "tape-rows"
	draftAvailableRegion = "available"
	draftQueueRegion     = "queue"
	// draftPickBarRegion (spruce audit, J1 F1, 2026-09-04): the phone
	// sticky action strip (DraftPickBar, page.gsx) — the one pane Page()
	// used to render with no region and no live bind in either live_mode,
	// so it never refreshed at all. Shares draftAvailableView (the same
	// data draftAvailableRegion already serves): DraftPickBar reads only
	// viewer/draft/next_queued/viewer_ready/viewer_autopick, all already
	// on that view, so no new server-side type was needed.
	draftPickBarRegion = "pickbar"

	// draftTapeSinceKey is the tape pane's own "?since=" cursor: a
	// non-negative pick number below which the pane's rows are already on
	// screen. It shares its string with live.go's draftLiveSinceKey by
	// coincidence only (that one is the hub reconnect's fingerprint cursor,
	// on a different endpoint); the two never appear on the same request.
	//
	// 2026-08-30 review (findings 1/2/3/6): target mode no longer sends
	// this cursor at all — DraftHistory's own inner region
	// (draftTapeRowsRegion, TapeRowsFragmentHandler below) is a single
	// PLAIN REPLACE region, page.gsx's data-gosx-region-url={props.TapeURL}
	// with no data-gosx-region-mode/-key/-cursor and no "{cursor}" token,
	// nested inside the pane's own live root; a full replace on every
	// draft:pick/draft:undo/draft:state never accumulates stale rows, so
	// the prepend-and-cursor machinery this key used to drive is gone.
	// draftTapeSinceKey and the "?since=" handling below stay live ONLY
	// for the original "tape" region (draftTapeRegion) — fallback mode's
	// own full-pane region and any external API caller — never requested
	// by target mode's markup again.
	draftTapeSinceKey = "since"

	// draftHistoryViewQueryKey is item 1a's own cursor (2026-08-30 review):
	// which ONE of the Tape/Board/Teams sub-views a request renders — both
	// the tape fragment's own "?view=" and the full page's "/draft?view="
	// (attachDraftFragmentView runs against either). The segment and the
	// mobile Teams tab are plain data-gosx-link navigations to
	// "/draft?view=X" (DraftHistoryHead's own doc comment, page.gsx,
	// explains why not a client-side signal write).
	draftHistoryViewQueryKey = "view"

	// draftHistoryPickKey is item 1's own "?pick=" cursor (2026-08-30
	// review): the tape row a request should render OPEN, its detail body
	// inline. attachDraftFragmentPick, below, is the only place that reads
	// it for hydration; attachDraftFragmentView also reads it (through
	// parseDraftHistoryPick) purely to keep it alive across a region
	// refresh — see history_tape_url's own doc comment.
	draftHistoryPickKey = "pick"

	// draftHistoryRoundsKey is item 3's own "?rounds=all" cursor
	// (2026-08-30 review): the ONE recognized value, "all", tells
	// attachDraftFragmentView to skip capTapeRounds on a full render, so
	// the "Older rounds ↓" link's own target sees every round the draft
	// has made, not just the newest draftTapeMaxRenderedRounds.
	draftHistoryRoundsKey = "rounds"

	draftHistoryViewTape  = "tape"
	draftHistoryViewBoard = "board"
	draftHistoryViewTeams = "teams"
)

// draftFragmentLoader adapts DraftDataReadOnlyOptions to the load
// func(*http.Request) map[string]any) shape draftFragmentHandler expects
// (P1 perf fix, 2026-08-30 review): includeHistory is false for every
// region below except the tape pane, which is the only one page.gsx
// renders <DraftHistory> or <DraftTapeRows> for — the other five fragments
// otherwise paid DraftHistory's pick-count-scaled build cost on every poll
// for a value their own component never reads.
func draftFragmentLoader(service *league.Service, includeHistory bool) func(*http.Request) map[string]any {
	return func(r *http.Request) map[string]any {
		return service.DraftDataReadOnlyOptions(r, includeHistory)
	}
}

// RoomFragmentHandler returns the authoritative room chrome without the
// player workspace. Periodic GETs deliberately use a read-only loader so a
// browser tab can never provision membership or persist presence as a side
// effect of observing league state.
func RoomFragmentHandler(service *league.Service) http.Handler {
	return draftFragmentHandler(draftRoomRegion, draftFragmentAccess(service), draftFragmentLoader(service, false))
}

// WorkspaceFragmentHandler keeps the query-scoped player pool, personal board,
// and pick tape current without replacing the rest of the draft page.
func WorkspaceFragmentHandler(service *league.Service) http.Handler {
	return draftFragmentHandler(draftWorkspaceRegion, draftFragmentAccess(service), draftFragmentLoader(service, false))
}

// CommandFragmentHandler serves the shell's always-visible command bar
// region (Task 5a's app shell). Task 6 refines the ETag/?since behaviour
// this and the three handlers below share with RoomFragmentHandler and
// WorkspaceFragmentHandler above; the mount is load-bearing now, so the
// browser room's countdown and pick-label region swap on a real typed
// event instead of 404ing.
func CommandFragmentHandler(service *league.Service) http.Handler {
	return draftFragmentHandler(draftCommandRegion, draftFragmentAccess(service), draftFragmentLoader(service, false))
}

// TapeFragmentHandler serves the pick-history pane's full swapped body
// (Tape/Board/Teams, whichever "?view=" names): fallback mode's own outer
// pane-body region, plus "?since=" partial-row polling kept for API
// compatibility (2026-08-30 review, findings 1/2/6) — target mode's
// markup no longer requests this endpoint at all.
func TapeFragmentHandler(service *league.Service) http.Handler {
	return draftFragmentHandler(draftTapeRegion, draftFragmentAccess(service), draftFragmentLoader(service, true))
}

// TapeRowsFragmentHandler serves ONLY the tape's round-grouped rows
// (DraftTapeRows, never the pane shell, the live root, #tape-latest, the
// region element itself, or the role="status" stale-fallback paragraph) —
// findings 1/2/3/6 (2026-08-30 review). It is target mode's ONE tape
// region, a plain replace (the default mode), fetched fresh on every
// draft:pick/draft:undo/draft:state: every round header, the on-the-clock
// synthetic row, and the "NO PICKS YET" empty state render exactly as the
// current server state has them on every response, so nothing here can
// ever go stale or grow without bound the way the deleted prepend region
// could (draftRegionView, below, always answers this region with
// DraftTapeRows, never the full DraftHistory pane) — capTapeRounds
// (attachDraftFragmentView) still caps a full render to the newest three
// rounds, and "?rounds=all"/"?pick=" still expand/open it exactly as the
// full "tape" region's own URL does (history.TapeURL carries the same two
// query parameters, attachDraftFragmentView).
//
// Target mode's own markup never sends "?since=" to this endpoint. The
// parameter is still accepted here, for API compatibility only (2026-08-30
// review, finding 1): attachDraftFragmentSince runs for every region, so
// "?since=40" yields only the rows numbered above pick 40 — the same
// filtered DraftTapeRows body the "tape" region's own "?since=" poll
// returns (TestTapeRowsFragmentSinceReturnsOnlyNewerRows).
func TapeRowsFragmentHandler(service *league.Service) http.Handler {
	return draftFragmentHandler(draftTapeRowsRegion, draftFragmentAccess(service), draftFragmentLoader(service, true))
}

// AvailableFragmentHandler serves the available-players pane's swapped
// body, including the position-filtered ?pos= refetch the chips drive.
func AvailableFragmentHandler(service *league.Service) http.Handler {
	return draftFragmentHandler(draftAvailableRegion, draftFragmentAccess(service), draftFragmentLoader(service, false))
}

// QueueFragmentHandler serves the "my team" pane's swapped body.
func QueueFragmentHandler(service *league.Service) http.Handler {
	return draftFragmentHandler(draftQueueRegion, draftFragmentAccess(service), draftFragmentLoader(service, false))
}

// PickBarFragmentHandler serves the phone sticky action strip's swapped
// body (spruce audit, J1 F1, 2026-09-04). Loads the same read-only
// available view AvailableFragmentHandler does; draftRegionView picks
// DraftPickBar, not DraftAvailable, as the component for this region name.
func PickBarFragmentHandler(service *league.Service) http.Handler {
	return draftFragmentHandler(draftPickBarRegion, draftFragmentAccess(service), draftFragmentLoader(service, false))
}

// draftFragmentAccess is app/liveaccess.SignedInOrDemo under the name the
// rest of this package already calls it by (round-2 review of commit
// 917cf4f, finding 4: the predicate itself now lives in one shared place
// so the draft-live and scores-live hubs cannot drift apart).
func draftFragmentAccess(service *league.Service) func(*http.Request) bool {
	return liveaccess.SignedInOrDemo(service)
}

func draftFragmentHandler(
	region string,
	allowed func(*http.Request) bool,
	load func(*http.Request) map[string]any,
) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet {
			writer.Header().Set("Allow", http.MethodGet)
			http.Error(writer, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		if allowed == nil || !allowed(request) {
			http.Error(writer, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}
		if load == nil {
			http.Error(writer, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		prepared := prepareDraftData(load(request), request)
		prepared = attachDraftFragmentSince(prepared, request)
		prepared = attachDraftFragmentView(prepared, request)
		prepared = attachDraftFragmentPick(prepared, request)
		view, component, err := draftRegionView(prepared, region)
		if err != nil {
			http.Error(writer, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		etag, err := draftRegionETag(region, view)
		if err != nil {
			http.Error(writer, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		setDraftFragmentHeaders(writer, etag)
		if etagMatches(request.Header.Get("If-None-Match"), etag) {
			writer.WriteHeader(http.StatusNotModified)
			return
		}

		prepared = attachDraftRequestState(prepared, request)
		view, component, err = draftRegionView(prepared, region)
		if err != nil {
			http.Error(writer, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		program, err := route.LoadFileProgramHere("page.gsx")
		if err != nil {
			http.Error(writer, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		html, err := route.RenderProgramComponent(program, component, route.ProgramRenderEnv{
			Values: map[string]any{"props": view},
		})
		if err != nil {
			http.Error(writer, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte(html))
	})
}

func draftRegionView(data map[string]any, region string) (any, string, error) {
	switch region {
	case draftRoomRegion:
		view, ok := data["room"].(draftRoomView)
		if !ok {
			return nil, "", errInvalidDraftRegion
		}
		return view, "DraftRoom", nil
	case draftWorkspaceRegion:
		view, ok := data["workspace"].(draftWorkspaceView)
		if !ok {
			return nil, "", errInvalidDraftRegion
		}
		return view, "DraftWorkspace", nil
	case draftCommandRegion:
		view, ok := data["command"].(draftCommandView)
		if !ok {
			return nil, "", errInvalidDraftRegion
		}
		// DraftCommandHeader, not DraftCommandBar (spruce audit, J1 F1/F7,
		// J2 F7, 2026-09-04): the h1 now travels inside this same fragment
		// (DraftCommandHeader's own doc comment, page.gsx) — answering with
		// the bar alone here would drop the h1 from the DOM the first time
		// the fallback-mode region actually refetched it, even though the
		// initial SSR page (Page(), which also calls DraftCommandHeader)
		// painted it correctly at load.
		return view, "DraftCommandHeader", nil
	case draftTapeRegion:
		view, ok := data["history"].(draftHistoryView)
		if !ok {
			return nil, "", errInvalidDraftRegion
		}
		if view.Since >= 0 {
			return view, "DraftTapeRows", nil
		}
		return view, "DraftHistory", nil
	case draftTapeRowsRegion:
		// Always DraftTapeRows, regardless of Since — target mode's single
		// region never sends "?since=" (draftTapeSinceKey's own doc
		// comment), so this is unconditional rather than mirroring
		// draftTapeRegion's Since >= 0 check above.
		view, ok := data["history"].(draftHistoryView)
		if !ok {
			return nil, "", errInvalidDraftRegion
		}
		return view, "DraftTapeRows", nil
	case draftAvailableRegion:
		view, ok := data["available"].(draftAvailableView)
		if !ok {
			return nil, "", errInvalidDraftRegion
		}
		return view, "DraftAvailable", nil
	case draftQueueRegion:
		view, ok := data["queue"].(draftQueueView)
		if !ok {
			return nil, "", errInvalidDraftRegion
		}
		return view, "DraftMyTeam", nil
	case draftPickBarRegion:
		view, ok := data["available"].(draftAvailableView)
		if !ok {
			return nil, "", errInvalidDraftRegion
		}
		return view, "DraftPickBar", nil
	case practiceRegion:
		// The practice strip renders from the command view (practice
		// draft, practice_handlers.go): it reads props.Data.practice,
		// props.Actions.practice_leave/practice_restart, and props.CSRF —
		// the same three fields DraftCommandBar already carries.
		view, ok := data["command"].(draftCommandView)
		if !ok {
			return nil, "", errInvalidDraftRegion
		}
		return view, "DraftPracticeStrip", nil
	default:
		return nil, "", errInvalidDraftRegion
	}
}

// attachDraftFragmentSince copies a valid "?since=" into the tape pane's
// history view. A non-negative integer switches draftRegionView's tape case
// from the full DraftHistory render to DraftTapeRows — the rows newer than
// since alone, each preceded by its round header once. A missing,
// negative, or non-numeric "?since=" leaves Since at prepareDraftData's -1
// default, so every other fragment (and a plain GET /draft/fragment/tape)
// keeps rendering the full pane untouched.
//
// request.URL.Query().Has, not Get, decides whether "?since=" was given at
// all. A present-but-empty "?since=" (for example "/draft/fragment/tape?since=")
// counts as "since=0" and switches to the incremental DraftTapeRows
// render, the full made-picks list. A request that never asks for
// "since" at all (every ordinary GET /draft/fragment/tape, and the outer
// replace-mode region's own "?view=" URL) still renders the full pane.
// Get alone cannot tell these two cases apart: both read "" from an
// absent key and from a present-but-empty one.
//
// This distinction exists for API compatibility only (2026-08-30
// review, findings 1/2). No current client sends an empty "?since=";
// the prepend region that once relied on it is gone (TapeFragmentHandler
// and TapeRowsFragmentHandler doc comments, above).
func attachDraftFragmentSince(data map[string]any, request *http.Request) map[string]any {
	query := request.URL.Query()
	if !query.Has(draftTapeSinceKey) {
		return data
	}
	raw := strings.TrimSpace(query.Get(draftTapeSinceKey))
	since := 0
	if raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 {
			return data
		}
		since = parsed
	}
	history, ok := data["history"].(draftHistoryView)
	if !ok {
		return data
	}
	history.Since = since
	history.Rounds = filterTapeRoundsSince(history.Rounds, since)
	// The on-the-clock synthetic row belongs on a full pane render only
	// (Task 7 Step 4): repeating it on every "?since=" poll would duplicate
	// it above each newly-arrived round. The same is true of the "NO PICKS
	// YET" empty-tape message (RoundsEmpty): a "?since=" poll legitimately
	// returns zero new rounds whenever nothing has changed, which is not
	// the same fact as "the draft holds no picks at all".
	history.HasOnClock = false
	history.RoundsEmpty = false
	data["history"] = history
	return data
}

// attachDraftFragmentView selects the ONE Tape/Board/Teams sub-view the
// pane region actually renders (item 1a, 2026-08-30 review): a missing,
// empty, or unrecognized "?view=" all normalize to the tape default. Since
// a "?since=" cursor already switches draftRegionView's tape case straight
// to the bare DraftTapeRows partial (bypassing DraftHistory, and with it
// this View selection, entirely), the two query parameters never interact:
// "?since=" always means "the tape view's own incremental rows", "?view="
// only matters for a full-pane (no "?since=") render.
//
// It also runs against a full PAGE request, not just the fragment
// endpoint (page.server.go's Load calls it too): Page()'s own segment and
// mobile Teams tab are plain data-gosx-link navigations to "/draft?view=
// X" (see DraftHistoryHead's own doc comment for why — every click-based
// signal-writing attribute gosx@v0.53.9 offers unconditionally cancels
// its own triggering click's native default action, which would have
// meant a native radio never actually became checked). A soft nav
// re-runs Load with the new query string and swaps in a whole fresh
// server render, so BOTH the page's own initial view selection AND the
// region's own "?view=" (below) must come from the SAME request, not a
// client-side signal — that is what history_tape_url is for: a plain,
// pre-formatted string field (never a chained data.history.View
// expression in the .gsx template — this file's history_tape_url comment
// three lines down explains why), so the tape pane's own region binds a
// STATIC URL, re-resolved fresh on every full/soft page load, with no
// data-gosx-region-signal or "{value}" token at all.
func attachDraftFragmentView(data map[string]any, request *http.Request) map[string]any {
	history, ok := data["history"].(draftHistoryView)
	if !ok {
		return data
	}
	query := request.URL.Query()
	rawView := strings.TrimSpace(query.Get(draftHistoryViewQueryKey))
	history.View = normalizeDraftHistoryView(strings.ToLower(rawView))
	history.ShowTape = history.View == draftHistoryViewTape
	history.ShowBoard = history.View == draftHistoryViewBoard
	history.ShowTeams = history.View == draftHistoryViewTeams

	pos := stringField(data, "pool_position")
	poolQuery := stringField(data, "pool_query")
	page := intField(data, "pool_page")
	history.OlderHref = draftHistoryHref(draftRoomPath(data), draftHistoryViewTape, pos, poolQuery, page, map[string]string{draftHistoryRoundsKey: "all"})

	// Item 2 (2026-08-30 review): capTapeRounds runs here, not inside
	// buildDraftHistoryView, and only for a full render (Since < 0) —
	// attachDraftFragmentSince (draftFragmentHandler, above this call)
	// already ran and trimmed Rounds to picks above its own "?since="
	// cursor when one was given, so a since-poll's Rounds must reach the
	// template exactly as that filter left them, never re-capped on top.
	// HasOlderRounds is computed from the UNCAPPED count, before the cap
	// (or "?rounds=all", item 3) removes any rounds at all.
	//
	// requestedAllRounds (2026-08-30 follow-up) is this request's own
	// literal "?rounds=all" — the value history_tape_url, below, echoes
	// back verbatim. It is distinct from allRounds, which also turns true
	// when a "?pick=" deep link names a row the cap would otherwise drop
	// (the next block): that expansion is re-derived fresh from the pick
	// number on every request, so it never needs to be echoed into the
	// URL itself.
	requestedAllRounds := strings.EqualFold(strings.TrimSpace(query.Get(draftHistoryRoundsKey)), "all")
	if history.Since < 0 {
		allRounds := requestedAllRounds
		capped := capTapeRounds(history.Rounds)
		// A "?pick=N" deep link whose round the cap would drop must still
		// open its row: attachDraftFragmentPick (below) only ever hydrates
		// a pick actually present in Rounds, so a capped pick from an
		// early round rendered nothing before this check — rendering
		// every round instead, exactly as an explicit "?rounds=all"
		// already does, is what makes the deep link work.
		if !allRounds {
			if pick := parseDraftHistoryPick(request); pick > 0 && !tapeRoundsHavePick(capped, pick) && tapeRoundsHavePick(history.Rounds, pick) {
				allRounds = true
			}
		}
		history.HasOlderRounds = !allRounds && len(history.Rounds) > draftTapeMaxRenderedRounds
		if !allRounds {
			history.Rounds = capped
		}
	}
	data["history"] = history

	// history_tape_url/history_view_tape/_board/_teams: flat top-level
	// fields (never a nested data.history.X chain in page.gsx — matching
	// this package's existing rule, RoundsEmpty's own doc comment,
	// against relying on unproven GSX template-side expression shapes)
	// for Page()'s own region URL and DraftHistoryHead/DraftMobileTabs'
	// server-computed "which segment is active" state.
	//
	// Item 1 (2026-08-30 review): history_tape_url carries "&pick=N"
	// whenever the request did, so the open row survives a region
	// refresh (draft:pick/draft:undo/draft:state, Page()'s own
	// data-gosx-region-on) that would otherwise re-fetch the pane closed.
	//
	// 2026-08-30 follow-up: history_tape_url likewise carries
	// "&rounds=all" whenever this request's own "?rounds=" did, so an
	// expanded tape (the "Older rounds ↓" target) survives the same
	// region refresh instead of silently re-collapsing to the newest
	// three rounds while the address bar still reads "rounds=all".
	tapeURL := draftFragmentBase(data) + "/tape?view=" + history.View
	if pick := parseDraftHistoryPick(request); pick > 0 {
		tapeURL += "&" + draftHistoryPickKey + "=" + strconv.Itoa(pick)
	}
	if requestedAllRounds {
		tapeURL += "&" + draftHistoryRoundsKey + "=all"
	}
	data["history_tape_url"] = tapeURL
	// TapeURL (findings 1/2/3/6, 2026-08-30 review): target mode's own
	// single tape region (page.gsx's DraftHistory, ShowTape branch) needs
	// its OWN static URL, never history_tape_url above — that one names
	// the full "tape" region (Tape/Board/Teams plus the pane shell),
	// which the deleted prepend design used to mis-fetch on every
	// draft:undo (re-nesting a whole second .draft-history, live root and
	// all, inside the first). TapeRowsFragmentHandler's own dedicated
	// endpoint renders ONLY DraftTapeRows, so this carries no "?view="
	// (every "?view=" answer from that endpoint is the same rows body) —
	// only "&pick=" and "&rounds=all", the same two cursors
	// history_tape_url carries, so an open pick or an expanded "Older
	// rounds" view survives the region's own draft:pick/draft:undo/
	// draft:state refresh instead of silently re-collapsing.
	rowsURL := draftFragmentBase(data) + "/tape-rows"
	sep := "?"
	if pick := parseDraftHistoryPick(request); pick > 0 {
		rowsURL += sep + draftHistoryPickKey + "=" + strconv.Itoa(pick)
		sep = "&"
	}
	if requestedAllRounds {
		rowsURL += sep + draftHistoryRoundsKey + "=all"
	}
	history.TapeURL = rowsURL
	data["history"] = history
	data["history_view_tape"] = history.ShowTape
	data["history_view_board"] = history.ShowBoard
	data["history_view_teams"] = history.ShowTeams
	// history_tape_explicit (item 4, 2026-08-30 review): true only when
	// THIS request's own "?view=" named tape explicitly (a click on the
	// desktop segment's Tape link or the phone Picks tab, or a shared/
	// bookmarked "?view=tape" URL) — never for the bare "/draft" landing
	// request, where ShowTape is ALSO true (buildDraftHistoryView's own
	// ambient default) but no view was actually requested.
	// DraftMobileTabs' own #tab-picks needs this distinction to stay
	// mutually exclusive with #tab-players: both would otherwise want
	// "checked" on the very first, un-clicked page load.
	data["history_tape_explicit"] = history.ShowTape && rawView != ""
	// history_tape_href/_board_href/_teams_href (item 6, 2026-08-30
	// review): the desktop segment's and the phone Picks/Teams tabs' own
	// navigation targets, carrying the viewer's current pool q/pos/page
	// so switching sub-views never resets a filtered/paged pool search.
	data["history_tape_href"] = draftHistoryHref(draftRoomPath(data), draftHistoryViewTape, pos, poolQuery, page, nil)
	data["history_board_href"] = draftHistoryHref(draftRoomPath(data), draftHistoryViewBoard, pos, poolQuery, page, nil)
	data["history_teams_href"] = draftHistoryHref(draftRoomPath(data), draftHistoryViewTeams, pos, poolQuery, page, nil)
	return data
}

// parseDraftHistoryPick reads and validates "?pick=" (item 1, 2026-08-30
// review), returning 0 for a missing, non-numeric, or non-positive value —
// the shared "nothing to open" case both attachDraftFragmentView (the
// tape region URL) and attachDraftFragmentPick (the row hydration below)
// treat identically.
func parseDraftHistoryPick(request *http.Request) int {
	raw := strings.TrimSpace(request.URL.Query().Get(draftHistoryPickKey))
	if raw == "" {
		return 0
	}
	number, err := strconv.Atoi(raw)
	if err != nil || number < 1 {
		return 0
	}
	return number
}

// tapeRoundsHavePick reports whether rounds carries a round whose Picks
// includes pick number number — attachDraftFragmentView's own test
// (2026-08-30 follow-up) for whether a "?pick=" deep link's row survived
// the round cap, or needs every round rendered instead so it does not
// silently open nothing.
func tapeRoundsHavePick(rounds []draftTapeRoundView, number int) bool {
	for _, round := range rounds {
		for _, pick := range round.Picks {
			if pick.Number == number {
				return true
			}
		}
	}
	return false
}

// attachDraftFragmentPick marks the tape row a "?pick=" query names as
// open and hydrates its detail fields inline (item 1, 2026-08-30 review):
// the fix's whole point is that a made pick's detail body renders
// SERVER-SIDE, from an ordinary link click, never from a client-side
// data-gosx-set write under a <summary> — gosx@v0.53.9's own capture-
// phase document click listener finds the nearest [data-gosx-set]
// ancestor and unconditionally calls preventDefault() on the triggering
// click, so a <summary> under (or carrying) data-gosx-set never actually
// toggles open (client/runtime/host/actions.ts ~474-478, the verified
// fact this whole item is built against).
//
// Only the ONE named pick is ever hydrated: every other row's detail
// fields stay at their zero value (tapeRoundsProps, page.server.go),
// keeping the tape fragment's own gzip size the way item 1b already
// brought it under the D3 refresh budget — this fix does not reopen that
// cost for every row, only for the one a viewer actually asked to see.
// A pick number that names no made pick at all still opens nothing (the
// loop below finds no matching row). A pick that DOES exist but the round
// cap would otherwise have dropped no longer falls into that case:
// attachDraftFragmentView (2026-08-30 follow-up) already expanded Rounds
// to every round for it, before this function ever runs.
func attachDraftFragmentPick(data map[string]any, request *http.Request) map[string]any {
	history, ok := data["history"].(draftHistoryView)
	if !ok {
		return data
	}
	number := parseDraftHistoryPick(request)
	if number <= 0 || history.detail == nil {
		return data
	}
	pos := stringField(data, "pool_position")
	poolQuery := stringField(data, "pool_query")
	page := intField(data, "pool_page")
	closeHref := draftHistoryHref(draftRoomPath(data), draftHistoryViewTape, pos, poolQuery, page, nil)
	for ri := range history.Rounds {
		for pi := range history.Rounds[ri].Picks {
			if history.Rounds[ri].Picks[pi].Number != number {
				continue
			}
			detail := history.detail(number)
			pick := history.Rounds[ri].Picks[pi]
			pick.Open = true
			pick.Href = closeHref
			pick.Projection = detail.Projection
			pick.Source = detail.Source
			pick.BestAvailable = bestAvailableProps(detail.BestAvailable)
			pick.TeamPicks = tapePicksProps(detail.TeamPicks)
			history.Rounds[ri].Picks[pi] = pick
			data["history"] = history
			return data
		}
	}
	return data
}

// normalizeDraftHistoryView maps any raw "?view=" value to one of the three
// recognized sub-views, defaulting to tape — never len() or a bare string
// compare against props inside the .gsx template itself (item 3); the
// three ShowX bools attachDraftFragmentView derives from this are what the
// template actually branches on.
func normalizeDraftHistoryView(raw string) string {
	switch raw {
	case draftHistoryViewBoard, draftHistoryViewTeams:
		return raw
	default:
		return draftHistoryViewTape
	}
}

// filterTapeRoundsSince keeps only the picks numbered above since, round
// order and pick order (both newest-first) unchanged; a round left with no
// qualifying picks is dropped rather than rendering an empty header.
//
// ShowHeader (review item 2, 2026-08-30): a round's header renders only
// when since < round.First — the client has never seen ANY pick from this
// round before, so the header is genuinely new. Once since reaches or
// passes round.First, the client's own copy of this round's header is
// already on the page from an earlier "?since=" response (or the initial
// full render); re-sending it would only be dropped by the prepend
// region's own data-tape-key dedupe (client/runtime/host/regions.ts),
// which discards the WHOLE node — including any fresher "N of M made"
// text it might have carried — before it ever reaches the DOM. Emitting
// it once, ever, and keeping it live via MadeBindKey/CurrentBindAttr
// (draftTapeRoundView's own doc comment) is the only path that actually
// reaches an already-rendered header.
func filterTapeRoundsSince(rounds []draftTapeRoundView, since int) []draftTapeRoundView {
	out := make([]draftTapeRoundView, 0, len(rounds))
	for _, round := range rounds {
		picks := make([]draftTapePickView, 0, len(round.Picks))
		for _, pick := range round.Picks {
			if pick.Number > since {
				picks = append(picks, pick)
			}
		}
		if len(picks) == 0 {
			continue
		}
		round.Picks = picks
		round.ShowHeader = since < round.First
		out = append(out, round)
	}
	return out
}

var errInvalidDraftRegion = &draftRegionError{}

type draftRegionError struct{}

func (*draftRegionError) Error() string { return "invalid draft region" }

func setDraftFragmentHeaders(writer http.ResponseWriter, etag string) {
	writer.Header().Set("Cache-Control", "private, no-store")
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	writer.Header().Set("Vary", "Cookie")
	writer.Header().Set("ETag", etag)
}

func draftRegionETag(region string, view any) (string, error) {
	payload := map[string]any{"region": region, "view": semanticDraftRegionView(view)}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return `"` + hex.EncodeToString(digest[:]) + `"`, nil
}

// semanticDraftRegionView excludes clock text derived only from wall time.
// The browser countdown owns those seconds between authoritative state
// changes; excluding them lets unchanged polls return a bodyless 304. Every
// draft region view shares this same treatment: room, workspace, and
// Task 6's four shell panes (command, tape/history, available, queue) all
// wrap the identical viewData map (prepareDraftData, page.server.go), so
// draftRegionData below extracts .Data by type switch alone.
func semanticDraftRegionView(view any) any {
	data, ok := draftRegionData(view)
	if !ok {
		return view
	}
	copyData := make(map[string]any, len(data))
	for key, value := range data {
		copyData[key] = value
	}
	if clock, ok := data["clock"].(map[string]any); ok {
		stable := make(map[string]any, len(clock))
		for key, value := range clock {
			if key != "server_now" && key != "remaining_seconds" && key != "remaining_label" {
				stable[key] = value
			}
		}
		copyData["clock"] = stable
	}
	if draft, ok := data["draft"].(map[string]any); ok {
		stable := make(map[string]any, len(draft))
		for key, value := range draft {
			if key != "countdown_label" && key != "days_until" {
				stable[key] = value
			}
		}
		copyData["draft"] = stable
	}
	return copyData
}

// draftRegionData extracts the shared viewData map from any of the six
// draft region view types. It returns (nil, false) for anything else, so
// semanticDraftRegionView's caller falls back to hashing the view as-is.
func draftRegionData(view any) (map[string]any, bool) {
	switch typed := view.(type) {
	case draftRoomView:
		return typed.Data, true
	case draftWorkspaceView:
		return typed.Data, true
	case draftCommandView:
		return typed.Data, true
	case draftHistoryView:
		// R1 (Task 6 review): draftHistoryView carries no shared .Data map
		// (unlike the other five region views) — Rounds/Board/Teams are
		// its own typed fields, and Since is the tape's "?since=" cursor.
		// Since must be part of the hashed payload, or a "?since=2" and a
		// "?since=0" request (different DraftTapeRows bodies) collapse onto
		// the same ETag and a real cursor change answers a wrong-bodied 304.
		return map[string]any{
			"rounds": typed.Rounds, "board": typed.Board, "teams": typed.Teams,
			"complete": typed.Complete, "latest": typed.Latest, "since": typed.Since,
			// view is part of the hashed payload for the same reason since
			// is (item 1a, 2026-08-30 review): "?view=tape" and
			// "?view=board" against identical underlying picks render
			// different DraftHistory bodies, so they must hash to
			// different ETags — otherwise a region that switches its
			// {value} from one view to another could be served a stale
			// 304 still carrying the PREVIOUS view's cached body (see
			// gosx-live-binds' regions.ts: record.etag survives a {value}
			// change on the same bound element).
			"view":         typed.View,
			"has_on_clock": typed.HasOnClock, "next_label": typed.NextLabel,
			"on_clock_name": typed.OnClockName, "on_clock_abbr": typed.OnClockAbbr, "on_clock_tone": typed.OnClockTone,
		}, true
	case draftAvailableView:
		return typed.Data, true
	case draftQueueView:
		return typed.Data, true
	default:
		return nil, false
	}
}

func etagMatches(header, current string) bool {
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(candidate), "W/"))
		if candidate == "*" || candidate == current {
			return true
		}
	}
	return false
}

// PickDetailFragmentHandler serves one pick's lazily-loaded detail body
// (item 1b, 2026-08-30 review): DraftPickDetail's <summary> sets a
// per-pick signal on open, and its own micro-region fetches this exactly
// once per row a viewer actually expands, through
// GET /draft/fragment/pick/{n} — the lever that brought the tape
// fragment's own gzip size back under the D3 refresh-budget ceiling
// (spec-draft-room-and-live-scoring-v0.1.md, 4 KB per pick in fallback
// mode), by no longer inlining every made pick's full detail eagerly.
func PickDetailFragmentHandler(service *league.Service) http.Handler {
	return pickDetailFragmentHandler(draftFragmentAccess(service), draftPickDetailLoader(service))
}

// draftPickDetailLoader adapts Service.DraftPickDetail to
// pickDetailFragmentHandler's load shape, converting the league-level
// PickDetail into the same page-level draftTapePickView every other
// pick-detail render already uses (tapePickProps/bestAvailableProps/
// tapePicksProps, page.server.go).
func draftPickDetailLoader(service *league.Service) func(*http.Request, int) (draftTapePickView, bool) {
	return func(r *http.Request, number int) (draftTapePickView, bool) {
		detail, ok := service.DraftPickDetail(r, number)
		if !ok {
			return draftTapePickView{}, false
		}
		view := tapePickProps(detail.TapePick)
		view.Projection = detail.Projection
		view.Source = detail.Source
		view.BestAvailable = bestAvailableProps(detail.BestAvailable)
		view.TeamPicks = tapePicksProps(detail.TeamPicks)
		return view, true
	}
}

func pickDetailFragmentHandler(allowed func(*http.Request) bool, load func(*http.Request, int) (draftTapePickView, bool)) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet {
			writer.Header().Set("Allow", http.MethodGet)
			http.Error(writer, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		if allowed == nil || !allowed(request) {
			http.Error(writer, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}
		number, err := strconv.Atoi(request.PathValue("n"))
		if err != nil || number < 1 {
			http.Error(writer, http.StatusText(http.StatusNotFound), http.StatusNotFound)
			return
		}
		if load == nil {
			http.Error(writer, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		view, ok := load(request, number)
		if !ok {
			http.Error(writer, http.StatusText(http.StatusNotFound), http.StatusNotFound)
			return
		}
		etag, err := draftPickDetailETag(number, view)
		if err != nil {
			http.Error(writer, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		setDraftFragmentHeaders(writer, etag)
		if etagMatches(request.Header.Get("If-None-Match"), etag) {
			writer.WriteHeader(http.StatusNotModified)
			return
		}
		program, err := route.LoadFileProgramHere("page.gsx")
		if err != nil {
			http.Error(writer, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		html, err := route.RenderProgramComponent(program, "DraftPickDetailBody", route.ProgramRenderEnv{
			Values: map[string]any{"props": view},
		})
		if err != nil {
			http.Error(writer, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte(html))
	})
}

// draftPickDetailETag hashes the pick number alongside its rendered view,
// the same semantic-ETag shape draftRegionETag uses for the other six
// fragments (excluding nothing here: a pick's own detail carries no
// wall-clock-only field the way the command bar's clock does).
func draftPickDetailETag(number int, view draftTapePickView) (string, error) {
	payload := map[string]any{"pick": number, "view": view}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return `"` + hex.EncodeToString(digest[:]) + `"`, nil
}
