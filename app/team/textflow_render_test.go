package team

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
)

// TestTeamHeroNameAndOperatedByRenderThroughTextBlock is the text-flow
// wave's render check (2026-09-05) for the hero team name (h1) and the
// "Operated by" manager name (flow, no maxLines — an inline name
// mid-sentence, not a standalone row), both through the GoSX TextBlock
// substrate instead of a plain element.
func TestTeamHeroNameAndOperatedByRenderThroughTextBlock(t *testing.T) {
	router := route.NewRouter()
	router.SetLayout(func(ctx *route.RouteContext, body gosx.Node) gosx.Node {
		ctx.SetLanguage("en")
		return server.HTMLDocument(ctx.Document("Test", body))
	})
	if err := router.AddDir(".", route.FileRoutesOptions{}); err != nil {
		t.Fatalf("AddDir: %v", err)
	}
	handler, err := router.BuildChecked()
	if err != nil {
		t.Fatalf("BuildChecked: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET / (team page) = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `id="team-identity-hero"`) {
		t.Fatalf("team page rendered no hero to check: %s", body)
	}
	hero := body[strings.Index(body, `id="team-identity-hero"`):]
	if end := strings.Index(hero, "</section>"); end >= 0 {
		hero = hero[:end]
	}
	if strings.Contains(hero, `data-gosx-text-layout-max-lines=`) {
		t.Errorf("hero team name still carries a maxLines clamp; important identity should flow naturally: %s", hero)
	}
	if !strings.Contains(hero, `data-gosx-text-layout`) {
		t.Errorf("hero team name did not render through the TextBlock layout substrate: %s", hero)
	}
	if !strings.Contains(body, `Operated by`) {
		t.Fatalf("team page rendered no claimed manager to check: %s", body)
	}
	if !strings.Contains(body, `data-gosx-text-layout`) {
		t.Errorf("no TextBlock markers rendered at all: %s", body)
	}

	source, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(source), `<TextBlock as="span" font="400 15px Plus Jakarta Sans" lineHeight={22} text={data.team.manager} />`) {
		t.Errorf("Operated by manager name should flow (no maxLines) through TextBlock — it sits inline mid-sentence, not as a standalone row")
	}
}

// TestTeamChecklistCopyRendersThroughTextBlock covers the pre-draft
// checklist's own item title and detail sentence (checklist-item__text):
// both flow through TextBlock rather than a bare <strong>/<small>.
func TestTeamChecklistCopyRendersThroughTextBlock(t *testing.T) {
	router := route.NewRouter()
	router.SetLayout(func(ctx *route.RouteContext, body gosx.Node) gosx.Node {
		ctx.SetLanguage("en")
		return server.HTMLDocument(ctx.Document("Test", body))
	})
	if err := router.AddDir(".", route.FileRoutesOptions{}); err != nil {
		t.Fatalf("AddDir: %v", err)
	}
	handler, err := router.BuildChecked()
	if err != nil {
		t.Fatalf("BuildChecked: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET / (team page) = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `checklist-item__text`) {
		t.Fatalf("team page rendered no pre-draft checklist to check: %s", body)
	}
	if !strings.Contains(body, `data-gosx-text-layout-source="Claim and personalize your franchise"`) {
		t.Errorf("checklist item title did not render through TextBlock: %s", body)
	}
}
