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

// TestTopicTitleAndLedeUseTextBlock pins the textflow wave (2026-09-05):
// a help topic's own title (<h1>) and summary lede (<p class="guide-
// lede">) render through <TextBlock> — maxLines={2} on the title,
// maxLines={3} on the lede.
func TestTopicTitleAndLedeUseTextBlock(t *testing.T) {
	t.Setenv("DATA_FILE", filepath.Join(t.TempDir(), "topic-textflow-state.json"))
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

	at := strings.Index(body, "<h1")
	if at < 0 {
		t.Fatal("no <h1 in the rendered topic page")
	}
	tag := body[at : at+strings.Index(body[at:], ">")]
	if !strings.Contains(tag, "data-gosx-text-layout") || !strings.Contains(tag, `data-gosx-text-layout-max-lines="2"`) {
		t.Errorf("topic title missing TextBlock maxLines=2 attrs: %s", tag)
	}

	ledeAt := strings.Index(body, `class="guide-lede"`)
	if ledeAt < 0 {
		t.Fatal("no .guide-lede in the rendered topic page")
	}
	ledeTagStart := strings.LastIndex(body[:ledeAt], "<p")
	ledeTag := body[ledeTagStart : ledeAt+strings.Index(body[ledeAt:], ">")]
	if !strings.Contains(ledeTag, "data-gosx-text-layout") || !strings.Contains(ledeTag, `data-gosx-text-layout-max-lines="3"`) {
		t.Errorf("topic lede missing TextBlock maxLines=3 attrs: %s", ledeTag)
	}
}
