package board

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/action"
	"m31labs.dev/gosx/auth"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
)

func renderBoardForUser(t *testing.T, handler http.Handler, target, email string) string {
	t.Helper()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, target, nil)
	request.Header.Set("X-Test-User", email)
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET %s for %s = %d, want 200; body: %s", target, email, recorder.Code, recorder.Body.String())
	}
	return recorder.Body.String()
}

func buildBoardAuthenticatedHandler(t *testing.T, currentEmail *string) http.Handler {
	t.Helper()
	authn := auth.New(nil, auth.Options{
		Provider: auth.ProviderFunc(func(r *http.Request) (auth.User, bool) {
			email := *currentEmail
			if header := r.Header.Get("X-Test-User"); header != "" {
				email = header
			}
			return auth.User{ID: email, Email: email, Name: "Board Render Fixture"}, true
		}),
	})
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
	return authn.Middleware(handler)
}

func TestBoardPageUsesServerDiscoveryAndKeepsPoolAnchor(t *testing.T) {
	t.Setenv("DATA_FILE", filepath.Join(t.TempDir(), "league-state.json"))
	t.Setenv("DEMO_MODE", "true")
	t.Setenv("GOOGLE_CLIENT_ID", "")
	currentEmail := "board-render@example.com"
	handler := buildBoardAuthenticatedHandler(t, &currentEmail)
	body := renderBoardForUser(t, handler, "/?pos=WR&q=ja&page=2", currentEmail)

	for _, want := range []string{
		`id="board-pool"`,
		`action="/board#board-pool"`,
		`name="q"`,
		`name="pos"`,
		`name="page"`,
		`name="__gosx_return_to"`,
		`value="/board?pos=WR&amp;q=ja#board-pool"`,
		`pool-pagination`,
		`#board-pool`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("board page missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, `data-gosx-filter="board-pool-rows"`) || strings.Contains(body, `data-gosx-filter-text=`) {
		t.Fatalf("board page retained client-only filtering attributes: %s", body)
	}
}

func TestBoardReturnTargetForDataUsesCanonicalPoolState(t *testing.T) {
	data := map[string]any{
		"pool_position": " wr ",
		"pool_query":    " Tom & / ",
		"pool_page":     3,
	}
	want := "/board?page=3&pos=WR&q=Tom+%26+%2F#board-pool"
	if got := boardReturnTargetForData(data); got != want {
		t.Fatalf("boardReturnTargetForData = %q, want %q", got, want)
	}
}

func TestBoardRedirectTargetIsCanonicalAndAnchored(t *testing.T) {
	got := boardRedirectTarget(" wR ", " Tom & / ", "3")
	want := "/board?page=3&pos=WR&q=Tom+%26+%2F#board-pool"
	if got != want {
		t.Fatalf("boardRedirectTarget = %q, want %q", got, want)
	}
	for _, input := range []string{"https://evil.example", "//evil.example", "/other"} {
		got := boardRedirectTarget(input, "q", "1")
		if !strings.HasPrefix(got, "/board") || strings.Contains(got, "evil.example") || strings.Contains(got, "other") {
			t.Fatalf("unsafe position %q influenced redirect target %q", input, got)
		}
	}
}

func TestBoardValidationStateRestoresCanonicalFilters(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/board?old=discard", nil)
	view := action.View{Result: action.Result{Values: map[string]string{
		"q":    "  deep pool  ",
		"pos":  " wr ",
		"page": "4",
	}}}
	filtered := boardRequestWithActionFilters(request, view)
	if got := filtered.URL.Query().Get("q"); got != "deep pool" {
		t.Fatalf("restored query = %q, want deep pool", got)
	}
	if got := filtered.URL.Query().Get("pos"); got != "WR" {
		t.Fatalf("restored position = %q, want WR", got)
	}
	if got := filtered.URL.Query().Get("page"); got != "4" {
		t.Fatalf("restored page = %q, want 4", got)
	}
	if got := filtered.URL.Query().Get("old"); got != "discard" {
		t.Fatalf("unrelated query = %q, want discard", got)
	}
}

func TestBoardNativeReorderControlsPreserveContextAndManagedFeedback(t *testing.T) {
	page, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatalf("read Board page: %v", err)
	}
	source := string(page)
	for _, want := range []string{
		"MoveAction={actionPath(\"board-move\")}",
		"name=\"direction\" value=\"up\"",
		"name=\"direction\" value=\"down\"",
		"name=\"pos\" value={props.Position}",
		"name=\"q\" value={props.Query}",
		"name=\"page\" value={props.Page}",
		"name={props.ReturnTargetField} value={props.ReturnTarget}",
		"CanMoveUp={entry.board_can_move_up}",
		"CanMoveDown={entry.board_can_move_down}",
		"aria-label={\"Move \" + props.Player.name + \" up\"}",
		"aria-label={\"Move \" + props.Player.name + \" down\"}",
		"type=\"button\" disabled=\"disabled\"",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("page.gsx missing native reorder contract %q", want)
		}
	}

	server, err := os.ReadFile("page.server.go")
	if err != nil {
		t.Fatalf("read Board server: %v", err)
	}
	serverSource := string(server)
	for _, want := range []string{
		"return ctx.Success(\"Board order updated.\", nil)",
		"actionui.RedirectBackWithNotice(ctx, boardRedirectTarget(ctx.FormData[\"pos\"], ctx.FormData[\"q\"], ctx.FormData[\"page\"]), \"Board order updated.\")",
		"actionui.RedirectBackWithNotice(ctx, boardRedirectTarget(ctx.FormData[\"pos\"], ctx.FormData[\"q\"], ctx.FormData[\"page\"]), \"Player removed from your board.\")",
		"actionui.RedirectBackWithNotice(ctx, boardRedirectTarget(ctx.FormData[\"pos\"], ctx.FormData[\"q\"], ctx.FormData[\"page\"]), \"Your board is cleared.\")",
	} {
		if !strings.Contains(serverSource, want) {
			t.Fatalf("page.server.go missing Board action continuity contract %q", want)
		}
	}
}

// TestBoardHistBlocksIncludeScoringNote is the finding 4 regression
// (adversarial review, 2026-08-31): both of page.gsx's has_hist blocks
// (the BoardRow component and the inline board-pool row) must render the
// "Scored under this league's own rules" caption alongside the Hist line
// itself, matching the pattern app/players/page.gsx already follows —
// before this fix, board.gsx rendered {player.hist} with no
// {player.hist_label} caption at all.
func TestBoardHistBlocksIncludeScoringNote(t *testing.T) {
	page, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatalf("read Board page: %v", err)
	}
	// Whitespace-normalized: this proves ordering and adjacency (the
	// hist-note caption immediately follows the Hist line inside the same
	// has_hist block) without pinning to exact indentation.
	normalized := strings.Join(strings.Fields(string(page)), " ")
	for _, want := range []string{
		`<p class="stat-tip__hist mono">{props.Player.hist}</p> <p class="stat-tip__hist-note">{props.Player.hist_label}</p>`,
		`<p class="stat-tip__hist mono">{player.hist}</p> <p class="stat-tip__hist-note">{player.hist_label}</p>`,
	} {
		if !strings.Contains(normalized, want) {
			t.Fatalf("page.gsx missing the Hist scoring-note caption immediately after the Hist line: %q", want)
		}
	}
}

func TestBoardStylesKeepDesktopRowsAndPagerControlsInBounds(t *testing.T) {
	styles, err := os.ReadFile(filepath.Join("..", "..", "public", "styles.css"))
	if err != nil {
		t.Fatalf("read board styles: %v", err)
	}
	source := string(styles)
	for _, want := range []string{
		".board-page #board-pool .pool-list--tall",
		"overflow-x: clip;",
		".board-page #board-pool .pool-row",
		"grid-template-columns: 2.5rem minmax(0, 1fr) 3.5rem 4.25rem auto;",
		".board-page #board-pool .position-filters > .filter-button",
		".board-page #board-pool .pool-pagination > .filter-button",
		"min-width: 2.75rem;",
		"min-height: 2.75rem;",
		"min-inline-size: 44px;",
		"min-block-size: 44px;",
		"@media (min-width: 38.0625rem)",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("public/styles.css missing Board-scoped layout contract %q", want)
		}
	}
}
