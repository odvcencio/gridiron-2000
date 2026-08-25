package topic

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
)

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
