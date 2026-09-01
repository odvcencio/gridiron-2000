package admin

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"

	"gridiron-2000/internal/actionui"
	"m31labs.dev/gosx/action"
	"m31labs.dev/gosx/session"
)

// TestAdminPlainLanguageErrorNeverEchoesAStoreError pins gap-audit item 3's
// "never echo a store error" rule for the two errors a bypassed UI gate
// (draw order, playoff preview) can still reach: DrawDraftOrder's and
// AdminPreviewPlayoffs' own internal/league wording must never reach the
// commissioner, even directly.
func TestAdminPlainLanguageErrorNeverEchoesAStoreError(t *testing.T) {
	for _, raw := range []string{
		"reset the draft before changing the order",
		"playoff preview requires the playoffs phase",
	} {
		mapped := adminPlainLanguageError(errors.New(raw))
		if mapped == nil || mapped.Error() == raw {
			t.Errorf("adminPlainLanguageError(%q) = %v, want a rewritten plain-language message", raw, mapped)
		}
	}
	other := errors.New("some other admin store error")
	if mapped := adminPlainLanguageError(other); mapped != other {
		t.Errorf("adminPlainLanguageError must pass through unrecognized errors unchanged, got %v", mapped)
	}
	if adminPlainLanguageError(nil) != nil {
		t.Error("adminPlainLanguageError(nil) must return nil")
	}
}

// adminActionSection names every /admin/__actions/<name> handler's owning
// section (gap-audit item 1). All 33 handlers in page.server.go's Actions
// map must redirect back to this section on success — 27 previously landed
// on a hard "/admin" with focus reset to <main> and scrollY 0; the other 2
// (order-randomize, announcement-post) and the 4 playoff actions (behind
// adminPlayoffRedirect) already did. clock-set-autopick is submitted from
// the per-seat AUTO toggle in 01 // SEATS, not 05 // DRAFT CLOCK, so its
// section is seats, not clock.
var adminActionSection = map[string]string{
	"schedule-generate":    "schedule",
	"schedule-regenerate":  "schedule",
	"close-week-ready":     "week-close",
	"close-week-force":     "week-close",
	"run-waivers":          "week-close",
	"playoff-preview":      "playoffs",
	"playoff-publish":      "playoffs",
	"playoff-advance":      "playoffs",
	"playoff-correct":      "playoffs",
	"draft-start":          "draft-control",
	"draft-reschedule":     "draft-control",
	"invite-add":           "invites",
	"invite-send":          "invites",
	"invite-remove":        "invites",
	"seat-release":         "seats",
	"co-detach":            "seats",
	"team-rename":          "seats",
	"avatar-reset":         "seats",
	"draft-reset":          "danger",
	"draft-undo":           "danger",
	"league-reset":         "danger",
	"seat-trim":            "draft-order",
	"order-randomize":      "draft-order",
	"clock-pause":          "clock",
	"clock-resume":         "clock",
	"clock-force-autopick": "clock",
	"clock-extend":         "clock",
	"clock-set-duration":   "clock",
	"clock-set-autopick":   "seats",
	"roster-shape-apply":   "roster",
	"roster-shape-reset":   "roster",
	"announcement-post":    "announcements",
	"announcement-delete":  "announcements",
}

// actionHandlerBoundary finds each "<name>": func(ctx *action.Context) error {
// entry in the Actions map literal, in source order, so a handler's body can
// be isolated without a full Go parse.
var actionHandlerBoundary = regexp.MustCompile(`"([a-z-]+)":\s*func\(ctx \*action\.Context\) error \{`)

// TestEveryAdminActionReturnsToItsSection pins the section-preserving
// redirect for all 33 admin actions (gap-audit item 1): each handler's body
// must call RedirectBackWithNotice with that action's own section target
// (directly, or through adminPlayoffRedirect for the 4 playoff actions),
// never a bare RedirectWithNotice("/admin", ...) that drops the commissioner
// at scrollY 0 with focus reset to <main>.
func TestEveryAdminActionReturnsToItsSection(t *testing.T) {
	source, err := os.ReadFile("page.server.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	matches := actionHandlerBoundary.FindAllStringSubmatchIndex(text, -1)
	if len(matches) == 0 {
		t.Fatal("no admin action handlers found in page.server.go")
	}
	bodies := make(map[string]string, len(matches))
	for i, m := range matches {
		name := text[m[2]:m[3]]
		bodyStart := m[1]
		bodyEnd := len(text)
		if i+1 < len(matches) {
			bodyEnd = matches[i+1][0]
		}
		bodies[name] = text[bodyStart:bodyEnd]
	}
	if len(bodies) != len(adminActionSection) {
		t.Fatalf("found %d action handlers in source, want %d named in adminActionSection (update whichever inventory drifted)", len(bodies), len(adminActionSection))
	}
	for name, section := range adminActionSection {
		body, ok := bodies[name]
		if !ok {
			t.Errorf("action %q has no handler body in page.server.go", name)
			continue
		}
		if strings.Contains(body, `actionui.RedirectWithNotice(ctx, "/admin"`) {
			t.Errorf("action %q still redirects to a hard /admin instead of its own section", name)
		}
		// playoff-* route through adminPlayoffRedirect and close-week-* route
		// through adminCloseWeek; both helpers carry the literal
		// adminSectionTarget(...) call themselves (checked separately
		// below), so a call to either helper is accepted here in place of
		// the literal in the action's own body.
		want := `adminSectionTarget("` + section + `")`
		indirect := strings.Contains(body, "adminPlayoffRedirect(ctx,") || strings.Contains(body, "adminCloseWeek(ctx,")
		if !strings.Contains(body, want) && !indirect {
			t.Errorf("action %q must redirect back to %s (want %q in its handler body)", name, section, want)
		}
	}
	// The two indirection points themselves must carry the real target, or
	// the check above would accept a helper that silently fell back to
	// "/admin".
	if !strings.Contains(text, `func adminCloseWeek(ctx *action.Context, week int, alreadyFinal bool) error {`) {
		t.Fatal("adminCloseWeek helper signature moved; update this test's indirection check")
	}
	closeWeekBody := text[strings.Index(text, "func adminCloseWeek("):]
	if !strings.Contains(closeWeekBody[:strings.Index(closeWeekBody, "\n}\n")], `adminSectionTarget("week-close")`) {
		t.Error("adminCloseWeek must redirect back to week-close")
	}
	if !strings.Contains(text, `adminPlayoffNotice(ctx, adminSectionTarget("playoffs")`) {
		t.Error("adminPlayoffRedirect must redirect back to playoffs")
	}
}

// TestAdminSectionTargetsCarrySectionQueryAndAnchor is the literal
// contract gap-audit item 1 measures against: every admin section's
// redirect target must carry both the ?section= query and the #admin-
// fragment so the commissioner lands scrolled to, and focused on, the
// section their action just ran in.
func TestAdminSectionTargetsCarrySectionQueryAndAnchor(t *testing.T) {
	seen := map[string]bool{}
	for _, section := range adminActionSection {
		if seen[section] {
			continue
		}
		seen[section] = true
		target := adminSectionTarget(section)
		wantQuery := "section=" + section
		wantAnchor := "#admin-" + section
		if !strings.Contains(target, wantQuery) || !strings.HasSuffix(target, wantAnchor) {
			t.Errorf("adminSectionTarget(%q) = %q, want it to carry %q and end with %q", section, target, wantQuery, wantAnchor)
		}
	}
}

func TestAdminDraftOrderRedirectBackUsesSubmittedAnchorManaged(t *testing.T) {
	returnTarget := adminSectionTarget("draft-order")
	values := url.Values{
		action.ReturnTargetField: {returnTarget},
		"confirm":                {"REDRAW ORDER"},
	}
	request := httptest.NewRequest(http.MethodPost, "/admin/__actions/order-randomize", strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	request.SetPathValue("name", "order-randomize")
	response := httptest.NewRecorder()

	action.ServeHandler(response, request, func(ctx *action.Context) error {
		if _, ok := ctx.FormData[action.ReturnTargetField]; ok {
			t.Fatal("reserved return target reached the Admin action handler")
		}
		actionui.RedirectBackWithNotice(ctx, adminSectionTarget("draft-order"), "Order drawn.")
		return nil
	})

	if response.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", response.Code)
	}
	var result action.Result
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if !result.OK || result.Message != "Order drawn." || result.Redirect != returnTarget {
		t.Fatalf("result = %+v, want managed draft-order target %q", result, returnTarget)
	}
	if _, ok := result.Values[action.ReturnTargetField]; ok {
		t.Fatalf("reserved return target leaked into managed values: %#v", result.Values)
	}
}

func TestAdminAnnouncementRedirectBackUsesSubmittedAnchorNative(t *testing.T) {
	returnTarget := adminSectionTarget("announcements")
	manager := session.MustNew("admin-return-target-native-secret", session.Options{
		CookieName:    "admin_return_target_native",
		AllowInsecure: true,
	})
	handler := manager.Middleware(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodPost {
			action.ServeHandler(writer, request, func(ctx *action.Context) error {
				if _, ok := ctx.FormData[action.ReturnTargetField]; ok {
					t.Fatal("reserved return target reached the Announcement action handler")
				}
				actionui.RedirectBackWithNotice(ctx, adminSectionTarget("announcements"), "Announcement posted.")
				return nil
			})
			return
		}

		store := session.Current(request)
		if store == nil {
			t.Fatal("native Admin GET missing session")
		}
		flashes := store.Flashes("notice")
		if len(flashes) != 1 || flashes[0] != "Announcement posted." {
			t.Fatalf("notice flashes = %#v, want one trimmed notice", flashes)
		}
		view, ok := action.State(request, "announcement-post")
		if !ok {
			t.Fatal("native announcement action did not flash redirect-back state")
		}
		if view.Redirect() != returnTarget || view.Message() != "Announcement posted." {
			t.Fatalf("flashed view = %+v, want anchor and message", view)
		}
		if _, ok := view.Result.Values[action.ReturnTargetField]; ok {
			t.Fatalf("reserved return target leaked into native values: %#v", view.Result.Values)
		}
		writer.WriteHeader(http.StatusNoContent)
	}))

	values := url.Values{
		action.ReturnTargetField: {returnTarget},
		"body":                   {"The draft room opens soon."},
	}
	postRequest := httptest.NewRequest(http.MethodPost, "/admin/__actions/announcement-post", strings.NewReader(values.Encode()))
	postRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postRequest.Header.Set("Accept", "text/html,application/xhtml+xml")
	postRequest.SetPathValue("name", "announcement-post")
	postResponse := httptest.NewRecorder()
	handler.ServeHTTP(postResponse, postRequest)

	if postResponse.Code != http.StatusSeeOther {
		t.Fatalf("POST status = %d, want 303", postResponse.Code)
	}
	if got := postResponse.Header().Get("Location"); got != returnTarget {
		t.Fatalf("Location = %q, want submitted announcement target %q", got, returnTarget)
	}
	cookies := postResponse.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("POST wrote %d session cookies, want one", len(cookies))
	}
	getRequest := httptest.NewRequest(http.MethodGet, "/admin?section=announcements", nil)
	getRequest.AddCookie(cookies[0])
	getResponse := httptest.NewRecorder()
	handler.ServeHTTP(getResponse, getRequest)
	if getResponse.Code != http.StatusNoContent {
		t.Fatalf("GET status = %d, want 204", getResponse.Code)
	}
}
