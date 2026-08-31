package locker

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"gridiron-2000/internal/league"
)

func TestLockerFragmentURLPreservesPage(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/locker?page=3&ignored=private", nil)
	if got, want := lockerFragmentURL(request), "/locker/fragment?page=3"; got != want {
		t.Fatalf("locker fragment URL = %q, want %q", got, want)
	}
	if got, want := lockerFragmentURL(httptest.NewRequest(http.MethodGet, "/locker", nil)), "/locker/fragment"; got != want {
		t.Fatalf("locker fragment URL (no page) = %q, want %q", got, want)
	}
}

func TestLockerFragmentContract(t *testing.T) {
	page, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	pageSource := string(page)
	for _, want := range []string{
		`data-gosx-region-url={data.locker_fragment_url}`,
		`data-gosx-region-on="locker:changed"`,
		`func LockerBoard() Node`,
		`data-gosx-managed="true"`,
		`name="body"`,
	} {
		if !strings.Contains(pageSource, want) {
			t.Errorf("locker page missing synchronization contract %q", want)
		}
	}
	server, err := os.ReadFile("page.server.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"EnableBootstrap", "LockerData", "lockerFragmentURL"} {
		if !strings.Contains(string(server), want) {
			t.Errorf("locker server missing synchronization contract %q", want)
		}
	}
}

func TestLockerFragmentETagAndConvergence(t *testing.T) {
	version := 1
	handler := lockerFragmentHandlerWithRenderer(
		func(*http.Request) bool { return true },
		func(request *http.Request) map[string]any {
			return map[string]any{"version": version, "query": request.URL.RawQuery, "has_posts": false}
		},
		func(data map[string]any) (string, error) {
			return fmt.Sprintf(`<section data-locker-version="%v" data-query="%v">board</section>`, data["version"], data["query"]), nil
		},
	)
	first := httptest.NewRecorder()
	handler.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/locker/fragment?page=2", nil))
	if first.Code != http.StatusOK || !strings.Contains(first.Body.String(), `data-locker-version="1"`) || !strings.Contains(first.Body.String(), `page=2`) {
		t.Fatalf("first locker fragment = %d %q", first.Code, first.Body.String())
	}
	etag := first.Header().Get("ETag")
	if etag == "" || first.Header().Get("Cache-Control") != "private, no-store" || first.Header().Get("Vary") != "Cookie" {
		t.Fatalf("privacy/etag headers = %#v", first.Header())
	}
	unchanged := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/locker/fragment?page=2", nil)
	request.Header.Set("If-None-Match", etag)
	handler.ServeHTTP(unchanged, request)
	if unchanged.Code != http.StatusNotModified || unchanged.Body.Len() != 0 {
		t.Fatalf("unchanged locker fragment = %d %q", unchanged.Code, unchanged.Body.String())
	}
	version = 2
	converged := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/locker/fragment?page=2", nil)
	request.Header.Set("If-None-Match", etag)
	handler.ServeHTTP(converged, request)
	if converged.Code != http.StatusOK || !strings.Contains(converged.Body.String(), `data-locker-version="2"`) {
		t.Fatalf("converged locker fragment = %d %q", converged.Code, converged.Body.String())
	}
}

func TestLockerFragmentRejectsAnonymousAllowsSignedInOrDemo(t *testing.T) {
	denied := lockerFragmentHandlerWithRenderer(
		func(*http.Request) bool { return false },
		func(*http.Request) map[string]any { return map[string]any{"has_posts": false} },
		func(map[string]any) (string, error) { return "<section>board</section>", nil },
	)
	response := httptest.NewRecorder()
	denied.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/locker/fragment", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous fragment = %d, want 401", response.Code)
	}

	allowed := lockerFragmentHandlerWithRenderer(
		func(*http.Request) bool { return true },
		func(*http.Request) map[string]any { return map[string]any{"has_posts": false} },
		func(map[string]any) (string, error) { return "<section>board</section>", nil },
	)
	get := httptest.NewRecorder()
	allowed.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/locker/fragment", nil))
	if get.Code != http.StatusOK || get.Body.String() != "<section>board</section>" {
		t.Fatalf("signed-in/demo fragment = %d %q", get.Code, get.Body.String())
	}

	post := httptest.NewRecorder()
	allowed.ServeHTTP(post, httptest.NewRequest(http.MethodPost, "/locker/fragment", nil))
	if post.Code != http.StatusMethodNotAllowed || post.Header().Get("Allow") != http.MethodGet {
		t.Fatalf("POST fragment = %d allow=%q", post.Code, post.Header().Get("Allow"))
	}
}

func TestLockerFragmentConcurrentReads(t *testing.T) {
	handler := lockerFragmentHandlerWithRenderer(
		func(*http.Request) bool { return true },
		func(*http.Request) map[string]any { return map[string]any{"has_posts": false} },
		func(map[string]any) (string, error) { return "<section>stable board</section>", nil },
	)
	const readers = 24
	var wait sync.WaitGroup
	wait.Add(readers)
	for i := 0; i < readers; i++ {
		go func() {
			defer wait.Done()
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/locker/fragment", nil))
			if response.Code != http.StatusOK || response.Body.String() != "<section>stable board</section>" {
				t.Errorf("concurrent locker fragment = %d %q", response.Code, response.Body.String())
			}
		}()
	}
	wait.Wait()
}

// TestLockerBoardRendersPostsRepliesReplyComposerAndTombstones is the
// render-contract proof for the actual page.gsx template (not an injected
// fake renderer): it drives lockerFragmentRender directly with a
// hand-built data map — no *league.Service, no store, no singleton — the
// same hermetic technique app/draft's own render-contract tests use.
func TestLockerBoardRendersPostsRepliesReplyComposerAndTombstones(t *testing.T) {
	data := map[string]any{
		"has_posts":            true,
		"posts_count":          2,
		"can_post":             true,
		"page":                 1,
		"pages":                1,
		"has_previous":         false,
		"has_next":             false,
		"previous_href":        "/locker",
		"next_href":            "/locker",
		"csrf_token":           "tok-1",
		"locker_post_action":   "/locker/__actions/locker-post",
		"locker_remove_action": "/locker/__actions/locker-remove",
		"posts": []league.LockerPostView{
			{
				ID: "post-1", AuthorLabel: "Primary Manager (Ravens Of Thunder)", TimeLabel: "Sep 1, 12:00 PM UTC",
				Body: "Welcome to the Locker Room.", CanRemove: true,
				Replies: []league.LockerPostView{
					{ID: "reply-1", AuthorLabel: "Co-Manager", TimeLabel: "Sep 1, 12:05 PM UTC", Body: "Good to be here.", CanRemove: false},
				},
			},
			{
				ID: "post-2", AuthorLabel: "Departed Manager", TimeLabel: "Sep 1, 11:00 AM UTC",
				Removed: true, RemovedLabel: "Removed by the commissioner.",
			},
		},
	}
	html, err := lockerFragmentRender(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Welcome to the Locker Room.",
		"Primary Manager (Ravens Of Thunder)",
		"Good to be here.",
		"Co-Manager",
		"Removed by the commissioner.",
		`id="post-1"`,
		`id="reply-1"`,
		`id="post-2"`,
		`name="parent_id" value="post-1"`,
		`name="post_id" value="post-1"`,
		// The framework renders data-gosx-managed="true" (page.gsx's own
		// source attribute) into this progressive-enhancement contract:
		// data-gosx-fallback="native-form" is GoSX's own proof that the
		// form still posts without JavaScript.
		`data-gosx-fallback="native-form"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("rendered board missing %q:\n%s", want, html)
		}
	}
	if strings.Contains(html, "Departed Manager") {
		t.Error("a removed post's stored author still rendered outside its tombstone label")
	}
	// The reply itself carries no CanRemove/remove form and no further
	// reply composer of its own — GC-4's one-level-flat rule holds in the
	// rendered markup, not only in the store.
	if strings.Contains(html, `name="post_id" value="reply-1"`) {
		t.Error("a reply the viewer may not remove still rendered a remove form")
	}
	if strings.Contains(html, `name="parent_id" value="reply-1"`) {
		t.Error("a reply rendered its own reply composer (a second nesting level)")
	}
}

func TestLockerBoardTruthfulEmptyState(t *testing.T) {
	html, err := lockerFragmentRender(map[string]any{
		"has_posts": false, "posts": []league.LockerPostView{}, "page": 1, "pages": 1,
		"has_previous": false, "has_next": false, "previous_href": "/locker", "next_href": "/locker",
		"can_post": false, "csrf_token": "", "locker_post_action": "/locker/__actions/locker-post",
		"locker_remove_action": "/locker/__actions/locker-remove",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html, "NO POSTS YET") {
		t.Fatalf("expected the honest empty-board state, got: %s", html)
	}
}
