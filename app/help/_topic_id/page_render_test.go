package topic

import (
	"html"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	helpcontent "gridiron-2000/app/help"
	"m31labs.dev/gosx"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
)

func renderTopicRoute(t *testing.T, target string) string {
	t.Helper()
	t.Setenv("DATA_FILE", filepath.Join(t.TempDir(), "topic-state.json"))
	router := route.NewRouter()
	router.SetLayout(func(ctx *route.RouteContext, body gosx.Node) gosx.Node {
		ctx.SetLanguage("en")
		return server.HTMLDocument(ctx.Document("Help topic", body))
	})
	if err := router.AddDir("..", route.FileRoutesOptions{}); err != nil {
		t.Fatal(err)
	}
	handler, err := router.BuildChecked()
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, target, nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("topic GET %s = %d: %s", target, recorder.Code, recorder.Body.String())
	}
	return recorder.Body.String()
}

func TestTopicRouteRendersCanonicalWorkflowContract(t *testing.T) {
	t.Setenv("DATA_FILE", filepath.Join(t.TempDir(), "topic-state.json"))
	router := route.NewRouter()
	router.SetLayout(func(ctx *route.RouteContext, body gosx.Node) gosx.Node {
		ctx.SetLanguage("en")
		return server.HTMLDocument(ctx.Document("Help topic", body))
	})
	if err := router.AddDir("..", route.FileRoutesOptions{}); err != nil {
		t.Fatal(err)
	}
	handler, err := router.BuildChecked()
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/big-board-and-autopick", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("topic GET = %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		"Big Board and autopick",
		"Actor",
		"PREREQUISITE",
		"STATE + TIME",
		"CONSEQUENCE",
		"REVERSIBILITY",
		"FAILURE + RECOVERY",
		"Runtime source",
		"/board",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("topic omitted %q", want)
		}
	}
}

// TestTopicPrimaryActionNamesTheDestination is J5 F19 (2026-09-04 audit):
// the getting-started topic's own ActionRoute is "/" (the corpus's own
// entry-surface fallback), and its primary button used to read "Open
// owning action →" — a schema field name, not a destination — sending
// an anonymous visitor straight back to the page they came from with no
// warning. It must now name the destination, never that generic phrase.
func TestTopicPrimaryActionNamesTheDestination(t *testing.T) {
	t.Setenv("DATA_FILE", filepath.Join(t.TempDir(), "topic-action-label-state.json"))
	router := route.NewRouter()
	router.SetLayout(func(ctx *route.RouteContext, body gosx.Node) gosx.Node {
		ctx.SetLanguage("en")
		return server.HTMLDocument(ctx.Document("Help topic", body))
	})
	if err := router.AddDir("..", route.FileRoutesOptions{}); err != nil {
		t.Fatal(err)
	}
	handler, err := router.BuildChecked()
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/getting-started", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("topic GET = %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, "Open owning action") {
		t.Error(`topic page still renders "Open owning action" — a schema field name, not a destination`)
	}
	if !strings.Contains(body, "Go to the league home →") {
		t.Error(`topic page missing "Go to the league home →" for the getting-started topic's own ActionRoute ("/")`)
	}

	// The metadata table (Actor/Supported/Runtime source/Verified source)
	// must sit AFTER the prose answer (the lede) and behind a "Sources"
	// disclosure, not ahead of it in the masthead.
	ledeAt := strings.Index(body, `class="guide-lede"`)
	sourcesAt := strings.Index(body, `<details class="guide-sources"`)
	if ledeAt < 0 {
		t.Fatal("topic page missing its own lede (guide-lede)")
	}
	if sourcesAt < 0 {
		t.Fatal(`topic page missing <details class="guide-sources"`)
	}
	if sourcesAt < ledeAt {
		t.Error("the Sources disclosure must render after the prose lede, not before it")
	}
	if !strings.Contains(body[sourcesAt:], "Sources") {
		t.Error(`the metadata disclosure summary must read "Sources"`)
	}
	actorAt := strings.Index(body[sourcesAt:], "<span>Actor</span>")
	if actorAt < 0 {
		t.Error("the Actor/Supported/Runtime source/Verified source table must live inside the Sources disclosure")
	}
}

func TestTopicRouteRendersContextualStateAndFieldHelp(t *testing.T) {
	t.Setenv("DATA_FILE", filepath.Join(t.TempDir(), "context-state.json"))
	router := route.NewRouter()
	router.SetLayout(func(ctx *route.RouteContext, body gosx.Node) gosx.Node {
		ctx.SetLanguage("en")
		return server.HTMLDocument(ctx.Document("Help topic", body))
	})
	if err := router.AddDir("..", route.FileRoutesOptions{}); err != nil {
		t.Fatal(err)
	}
	handler, err := router.BuildChecked()
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/big-board-and-autopick?state=stale&field=deadline", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("contextual topic GET = %d: %s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	for _, want := range []string{"CONTEXTUAL HELP", "STATE // stale", "FIELD // deadline", "Runtime-owned field help", "current deadline"} {
		if !strings.Contains(body, want) {
			t.Errorf("contextual topic omitted %q", want)
		}
	}
}

func TestTopicRouteRendersStateSemanticsValidationAndOwningTopic(t *testing.T) {
	topic, ok := helpcontent.FindTopic("big-board-and-autopick")
	if !ok {
		t.Fatal("Big Board topic missing")
	}

	stale := renderTopicRoute(t, "/big-board-and-autopick?state=stale&field=validation")
	for _, want := range []string{
		"STATE // stale",
		"<strong>Last success:</strong> The owning page supplies the last-success time and age",
		"href=\"/help/big-board-and-autopick?state=stale\"",
		"FIELD // validation",
		"Validation for Big Board and autopick uses the owning action /board.",
		"Open the Big Board",
		"href=\"/help/big-board-and-autopick\"",
	} {
		if !strings.Contains(stale, want) {
			t.Errorf("stale validation render omitted %q", want)
		}
	}
	for name, want := range map[string]string{
		"failure":        topic.Failure,
		"recovery":       topic.Recovery,
		"runtime source": topic.RuntimeSource,
	} {
		if !strings.Contains(stale, html.EscapeString(want)) {
			t.Errorf("stale validation render omitted selected topic %s %q", name, want)
		}
	}

	failed := renderTopicRoute(t, "/big-board-and-autopick?state=failed&field=validation")
	for _, want := range []string{
		"STATE // failed",
		"<strong>Last success:</strong> Use the owning page&#39;s last-success value for reads; mutation outcome remains unknown until reread.",
		"If the outcome is unknown, reread before retrying; never replay a stale mutation.",
		"Validation for Big Board and autopick uses the owning action /board.",
	} {
		if !strings.Contains(failed, want) {
			t.Errorf("failed validation render omitted %q", want)
		}
	}

	fallback := renderTopicRoute(t, "/big-board-and-autopick?state=unrecognized-state")
	for _, want := range []string{
		"STATE // unrecognized-state",
		"<strong>Last success:</strong> No last-success value is available for this scope.",
		"Bounded retry; never fabricate zero/live.",
		"href=\"/help/big-board-and-autopick?state=unrecognized-state\"",
		"Owning topic ID: big-board-and-autopick",
	} {
		if !strings.Contains(fallback, want) {
			t.Errorf("unknown state render omitted safe fallback %q", want)
		}
	}
}
