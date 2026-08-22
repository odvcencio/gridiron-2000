package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"golang.org/x/net/html"
	"m31labs.dev/gosx"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
)

type navigationViewerFixture struct {
	signedIn     bool
	demo         bool
	hasSeat      bool
	commissioner bool
	seatsOpen    bool
}

type renderedNavigationGroup struct {
	Name  string
	Links []string
}

func renderNavigationLayout(t *testing.T, target string, viewer navigationViewerFixture) string {
	t.Helper()
	root := t.TempDir()
	layoutSource, err := os.ReadFile(filepath.Join("app", "layout.gsx"))
	if err != nil {
		t.Fatalf("read layout: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "layout.gsx"), layoutSource, 0o644); err != nil {
		t.Fatalf("write layout fixture: %v", err)
	}
	pagePath := filepath.Join(root, "pickem", "page.gsx")
	if err := os.MkdirAll(filepath.Dir(pagePath), 0o755); err != nil {
		t.Fatalf("mkdir page fixture: %v", err)
	}
	if err := os.WriteFile(pagePath, []byte(`package app

func Page() Node {
	return <main id="main-content">
		<h1>Fixture</h1>
	</main>
}
`), 0o644); err != nil {
		t.Fatalf("write page fixture: %v", err)
	}

	modules := route.NewFileModuleRegistry()
	if err := modules.Register(route.FileModuleFor(pagePath, route.FileModuleOptions{
		Load: func(*route.RouteContext, route.FilePage) (any, error) {
			return map[string]any{
				"viewer": map[string]any{
					"signed_in":       viewer.signedIn,
					"demo":            viewer.demo,
					"has_seat":        viewer.hasSeat,
					"is_commissioner": viewer.commissioner,
					"initials":        "QA",
					"team_name":       "Quality Agents",
				},
				"league": map[string]any{
					"name":                 "Test League",
					"short_code":           "TL",
					"tagline":              "Truth over folklore",
					"fantasy_seats_open":   viewer.seatsOpen,
					"latest_announcement":  map[string]any{"has": false, "body": "", "posted_at": ""},
					"has_footer_line":      false,
					"footer_line":          "",
					"matchup_footer_live":  false,
					"matchup_footer_label": "MATCHUPS SCHEDULED",
				},
			}, nil
		},
	})); err != nil {
		t.Fatalf("register fixture module: %v", err)
	}

	router := route.NewRouter()
	router.SetLayout(func(ctx *route.RouteContext, body gosx.Node) gosx.Node {
		ctx.SetLanguage("en")
		return server.HTMLDocument(ctx.Document("Navigation fixture", body))
	})
	if err := router.AddDir(root, route.FileRoutesOptions{Modules: modules}); err != nil {
		t.Fatalf("AddDir: %v", err)
	}
	handler, err := router.BuildChecked()
	if err != nil {
		t.Fatalf("BuildChecked: %v", err)
	}

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, target, nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200; body: %s", target, recorder.Code, recorder.Body.String())
	}
	return recorder.Body.String()
}

func parseNavigationDocument(t *testing.T, body string) *html.Node {
	t.Helper()
	document, err := html.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatalf("parse rendered navigation: %v", err)
	}
	return document
}

func nodeAttr(node *html.Node, name string) string {
	for _, attr := range node.Attr {
		if attr.Key == name {
			return attr.Val
		}
	}
	return ""
}

func hasClass(node *html.Node, className string) bool {
	for _, value := range strings.Fields(nodeAttr(node, "class")) {
		if value == className {
			return true
		}
	}
	return false
}

func findNodes(root *html.Node, match func(*html.Node) bool) []*html.Node {
	var found []*html.Node
	var visit func(*html.Node)
	visit = func(node *html.Node) {
		if match(node) {
			found = append(found, node)
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			visit(child)
		}
	}
	visit(root)
	return found
}

func descendantText(node *html.Node) string {
	var builder strings.Builder
	var visit func(*html.Node)
	visit = func(current *html.Node) {
		if current.Type == html.TextNode {
			builder.WriteString(current.Data)
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			visit(child)
		}
	}
	visit(node)
	return strings.Join(strings.Fields(builder.String()), " ")
}

func surfaceGroups(surface *html.Node) []renderedNavigationGroup {
	groups := findNodes(surface, func(node *html.Node) bool {
		return node.Type == html.ElementNode && nodeAttr(node, "data-navigation-group") != ""
	})
	out := make([]renderedNavigationGroup, 0, len(groups))
	for _, group := range groups {
		entry := renderedNavigationGroup{Name: nodeAttr(group, "data-navigation-group")}
		links := findNodes(group, func(node *html.Node) bool {
			return node.Type == html.ElementNode && node.Data == "a" && hasClass(node, "navigation-link")
		})
		for _, link := range links {
			entry.Links = append(entry.Links, nodeAttr(link, "href")+"|"+descendantText(link))
		}
		out = append(out, entry)
	}
	return out
}

func expectedNavigationGroups(viewer navigationViewerFixture) []renderedNavigationGroup {
	groups := []renderedNavigationGroup{
		{Name: "today", Links: []string{"/|01 HQ", "/pickem|02 Pick'em", "/matchups|03 Matchups"}},
		{Name: "my-team"},
		{Name: "game-day", Links: []string{"/draft|08 Draft room", "/blitz|09 Preseason Blitz"}},
		{Name: "league", Links: []string{"/wire|10 Signal Wire", "/activity|11 Activity", "/scoring|12 Rules & scoring"}},
		{Name: "help", Links: []string{"/guide|13 Manager guide"}},
	}
	team := &groups[1].Links
	switch {
	case viewer.hasSeat:
		*team = append(*team, "/team|04 My team")
	case viewer.seatsOpen:
		*team = append(*team, "/join|04 Join a team")
	default:
		*team = append(*team, "/team|04 Fantasy status")
	}
	*team = append(*team, "/board|05 Draft board")
	if viewer.hasSeat {
		*team = append(*team, "/players|06 Player pool")
	}
	if viewer.hasSeat || viewer.commissioner {
		*team = append(*team, "/trades|07 Trades")
	}
	if viewer.commissioner {
		groups = append(groups, renderedNavigationGroup{Name: "commissioner", Links: []string{
			"/commissioner|14 All leagues",
			"/admin|15 League settings",
		}})
	}
	return groups
}

func TestPrimaryNavigationRoleSeatAndSurfaceMatrix(t *testing.T) {
	tests := []struct {
		name   string
		viewer navigationViewerFixture
	}{
		{name: "seated manager", viewer: navigationViewerFixture{signedIn: true, hasSeat: true, seatsOpen: true}},
		{name: "seatless with opening", viewer: navigationViewerFixture{signedIn: true, seatsOpen: true}},
		{name: "seatless full", viewer: navigationViewerFixture{signedIn: true}},
		{name: "demo commissioner without signed in", viewer: navigationViewerFixture{demo: true, commissioner: true}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			body := renderNavigationLayout(t, "/pickem?week=2", test.viewer)
			document := parseNavigationDocument(t, body)
			surfaces := findNodes(document, func(node *html.Node) bool {
				return node.Type == html.ElementNode && hasClass(node, "primary-navigation")
			})
			if len(surfaces) != 3 {
				t.Fatalf("primary navigation surface count = %d, want desktop/enhanced/static", len(surfaces))
			}
			want := expectedNavigationGroups(test.viewer)
			for index, surface := range surfaces {
				if got := surfaceGroups(surface); !reflect.DeepEqual(got, want) {
					t.Errorf("surface %d groups = %#v, want %#v", index, got, want)
				}
			}
			if test.viewer.signedIn && strings.Count(body, ">Sign out</button>") != 3 {
				t.Errorf("signed-in account surfaces did not each carry sign out")
			}
			if !test.viewer.signedIn && strings.Contains(body, ">Sign out</button>") {
				t.Error("signed-out/demo navigation exposed sign out")
			}
		})
	}
}

func TestPrimaryNavigationPublicAndDisclosureContracts(t *testing.T) {
	public := renderNavigationLayout(t, "/pickem?week=2", navigationViewerFixture{})
	if strings.Contains(public, "primary-navigation__groups") {
		t.Fatal("public navigation exposed authenticated league IA")
	}
	for _, want := range []string{`href="/guide"`, `href="/login"`, `aria-label="Public navigation"`} {
		if !strings.Contains(public, want) {
			t.Errorf("public navigation omitted %q", want)
		}
	}
	for _, forbidden := range []string{"Draft room", "Signal Wire", "League settings", ">Sign out</button>"} {
		if strings.Contains(public, forbidden) {
			t.Errorf("public navigation exposed %q", forbidden)
		}
	}

	body := renderNavigationLayout(t, "/pickem?week=2", navigationViewerFixture{signedIn: true, hasSeat: true})
	document := parseNavigationDocument(t, body)
	for id, want := range map[string]int{"primary-navigation-dialog": 1, "primary-navigation-title": 1} {
		got := len(findNodes(document, func(node *html.Node) bool { return nodeAttr(node, "id") == id }))
		if got != want {
			t.Errorf("id %q count = %d, want %d", id, got, want)
		}
	}
	for _, fragment := range []string{
		`aria-controls="primary-navigation-dialog"`,
		`aria-expanded="false"`,
		`data-gosx-disclosure-target="#primary-navigation-dialog"`,
		`id="primary-navigation-dialog"`,
		`data-gosx-disclosure-modal`,
		`role="dialog"`,
		`aria-modal="true"`,
		`aria-labelledby="primary-navigation-title"`,
		`data-gosx-disclosure-close="#primary-navigation-dialog"`,
		`data-gosx-disclosure-initial-focus`,
		`data-gosx-disclosure-backdrop="#primary-navigation-dialog"`,
		`href="#main-content"`,
		`id="main-content"`,
	} {
		if !strings.Contains(body, fragment) {
			t.Errorf("rendered layout omitted %q", fragment)
		}
	}
	for _, forbidden := range []string{`type="checkbox"`, `<label`, `role="menu"`, `role="menuitem"`} {
		if strings.Contains(body, forbidden) {
			t.Errorf("rendered layout retained forbidden navigation contract %q", forbidden)
		}
	}
}

func TestPrimaryNavigationQuerylessCurrentLinkUsesGoSXContract(t *testing.T) {
	body := renderNavigationLayout(t, "/pickem?week=2", navigationViewerFixture{signedIn: true, hasSeat: true})
	document := parseNavigationDocument(t, body)
	links := findNodes(document, func(node *html.Node) bool {
		return node.Type == html.ElementNode && node.Data == "a" && nodeAttr(node, "href") == "/pickem" && hasClass(node, "navigation-link")
	})
	if len(links) != 3 {
		t.Fatalf("pickem primary link count = %d, want 3", len(links))
	}
	for index, link := range links {
		if got := nodeAttr(link, "aria-current"); got != "page" {
			t.Errorf("pickem link %d aria-current = %q, want page", index, got)
		}
	}
}

func TestPrimaryNavigationCSSProgressiveEnhancementContract(t *testing.T) {
	styles, err := os.ReadFile(filepath.Join("public", "styles.css"))
	if err != nil {
		t.Fatalf("read styles: %v", err)
	}
	css := string(styles)
	for _, want := range []string{
		`.navigation-group__label`,
		`.navigation-link[aria-current="page"]`,
		`min-height: 2.75rem`,
		`height: 100dvh`,
		`overscroll-behavior: contain`,
		`@media (scripting: enabled)`,
		`html:has(script[data-gosx-navigation="true"]) .mobile-navigation-static`,
		`html:has(script[data-gosx-navigation="true"]) .mobile-navigation-enhanced`,
		`body:has(#primary-navigation-dialog:not([hidden]))`,
		`.mobile-navigation-dialog:not([hidden])`,
		`:focus-visible`,
		`@media (prefers-reduced-motion: reduce)`,
	} {
		if !strings.Contains(css, want) {
			t.Errorf("navigation CSS omitted %q", want)
		}
	}
	for _, forbidden := range []string{".drawer-check", ":checked ~", ".drawer-open", ".drawer-close"} {
		if strings.Contains(css, forbidden) {
			t.Errorf("navigation CSS retained checkbox drawer selector %q", forbidden)
		}
	}
}
