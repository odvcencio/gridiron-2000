package trades

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gridiron-2000/internal/league"
	"m31labs.dev/gosx"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
)

// TestTradesComposerAndVetoCopyRenderThroughTextBlock is the text-flow
// wave's render check (2026-09-05) for the trade partner chip (a
// counterparty's team name), the "You get from" composer header, and
// the veto policy sentence: all three now flow through the GoSX
// TextBlock substrate.
func TestTradesComposerAndVetoCopyRenderThroughTextBlock(t *testing.T) {
	dataFile := filepath.Join(t.TempDir(), "league-state.json")
	t.Setenv("DATA_FILE", dataFile)
	t.Setenv("DEMO_MODE", "true")
	t.Setenv("GOOGLE_CLIENT_ID", "")
	store := league.NewStore(dataFile)
	if _, _, err := store.AssignMember("one@example.com", "One"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.AssignMember("two@example.com", "Two"); err != nil {
		t.Fatal(err)
	}

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
	req := httptest.NewRequest(http.MethodGet, "/?counterparty=team-2", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /?counterparty=team-2 (trades page) = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `class="position-filters" aria-label="Choose a trade partner"`) {
		t.Fatalf("trades page rendered no partner picker to check: %s", body)
	}
	if !strings.Contains(body, `data-gosx-text-layout-max-lines="1"`) {
		t.Errorf("partner chip name missing the maxLines=1 TextBlock clamp: %s", body)
	}
	if !strings.Contains(body, `data-gosx-text-layout-max-lines="2"`) {
		t.Errorf("\"You get from\" header missing the maxLines=2 TextBlock clamp: %s", body)
	}
	if !strings.Contains(body, `data-gosx-text-layout-source="Trades are reviewed`) {
		t.Errorf("veto policy sentence did not render through TextBlock: %s", body)
	}
}

// TestTradesOfferRowsRenderThroughTextBlock is a source contract check
// (page.gsx does not have an easy fixture path to seed a real trade
// offer through every one of its six list states — inbox, outbox,
// pending review, commissioner review, vote, and history — so this
// pins the actual template text the same way
// TestTradesPageDeadlineRenderContract does above): every offer row's
// own team-name line and Give/Get player list now render through
// TextBlock instead of a bare <strong>/<small>.
func TestTradesOfferRowsRenderThroughTextBlock(t *testing.T) {
	source, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	page := string(source)
	for _, want := range []string{
		// Inbox.
		`text={"From " + offer.FromTeam}`,
		// Outbox.
		`text={"To " + offer.ToTeam + " · " + offer.StatusLabel}`,
		// Pending review.
		`text={"From " + offer.FromTeam + " · " + offer.StatusLabel}`,
		// Commissioner review and league vote (identical shape, twice).
		`text={offer.FromTeam + " ↔ " + offer.ToTeam}`,
	} {
		if strings.Count(page, want) < 1 {
			t.Errorf("offer row team-name line no longer renders through TextBlock: %q", want)
		}
	}
	if got := strings.Count(page, `text={offer.FromTeam + " ↔ " + offer.ToTeam}`); got != 3 {
		t.Errorf(`offer.FromTeam + " ↔ " + offer.ToTeam TextBlock count = %d, want 3 (review, vote, and history)`, got)
	}
	if got := strings.Count(page, `<TextBlock as="small" font="400 13px Plus Jakarta Sans" lineHeight={18}>`); got < 6 {
		t.Errorf("expected at least 6 flowing Give/Get TextBlocks (one per offer-row template), got %d", got)
	}
}
