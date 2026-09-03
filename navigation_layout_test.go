package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
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
	canClaimSeat bool
	// pickemHot/tradesHot and their AttentionText counterparts mirror
	// internal/league/service.go attentionMap's pre-shaped rail-dot
	// fields (build item 2), so this fixture can drive
	// PrimaryNavigation's attention-driven hot dot the same way the real
	// leagueMap does.
	pickemHot           bool
	pickemAttentionText string
	tradesHot           bool
	tradesAttentionText string
	// attentionHasItems/attentionUrgentCount/attentionChipLabel drive the
	// rail-head/mobile-bar attention chip (build item 1): chip_label is
	// internal/league/service.go attentionChipLabel's pre-shaped
	// aria-label, replacing the layout's old hand-concatenated
	// urgent_count + " items need attention..." string.
	attentionHasItems    bool
	attentionUrgentCount int
	attentionChipLabel   string
	// draftComplete (wave 7, item 6) mirrors leagueMap's own
	// "draft_complete" field (internal/league/service.go) — the fixture's
	// "league" map below carries it exactly the same way, so
	// PrimaryNavigation's "Draft results" link renders only when this is
	// true, matching the real leagueMap-driven contract.
	draftComplete bool
	// hasFooterLine/footerLine mirror leagueMap's own "has_footer_line"/
	// "footer_line" fields: the footer's "// {footer_line}" divider text
	// (matching the site-wide "LABEL // detail" convention — see
	// section-index spans throughout app/*.gsx) only renders while
	// hasFooterLine is true. Both default false/"" so every existing
	// fixture literal in this file keeps rendering no footer line.
	hasFooterLine bool
	footerLine    string
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

	// A second fixture page at the root route ("/") — the mobile bottom
	// bar's own Home slot links there, so TestMobileBottomBar* below needs
	// a route that path actually resolves to, not just the "/pickem"
	// fixture every other navigation test in this file uses.
	indexPagePath := filepath.Join(root, "page.gsx")
	if err := os.WriteFile(indexPagePath, []byte(`package app

func Page() Node {
	return <main id="main-content">
		<h1>Index fixture</h1>
	</main>
}
`), 0o644); err != nil {
		t.Fatalf("write index page fixture: %v", err)
	}

	fixtureData := func(*route.RouteContext, route.FilePage) (any, error) {
		return map[string]any{
			"viewer": map[string]any{
				"signed_in":           viewer.signedIn,
				"demo":                viewer.demo,
				"has_seat":            viewer.hasSeat,
				"seat_claim_eligible": viewer.canClaimSeat,
				"is_commissioner":     viewer.commissioner,
				"initials":            "QA",
				"team_name":           "Quality Agents",
			},
			"league": map[string]any{
				"name":                 "Test League",
				"short_code":           "TL",
				"tagline":              "Truth over folklore",
				"fantasy_seats_open":   viewer.seatsOpen,
				"latest_announcement":  map[string]any{"has": false, "body": "", "posted_at": ""},
				"has_footer_line":      viewer.hasFooterLine,
				"footer_line":          viewer.footerLine,
				"matchup_footer_live":  false,
				"matchup_footer_label": "MATCHUPS SCHEDULED",
				"attention": map[string]any{
					"pickem_hot":            viewer.pickemHot,
					"pickem_attention_text": viewer.pickemAttentionText,
					"trades_hot":            viewer.tradesHot,
					"trades_attention_text": viewer.tradesAttentionText,
					"has_items":             viewer.attentionHasItems,
					"urgent_count":          viewer.attentionUrgentCount,
					"chip_label":            viewer.attentionChipLabel,
				},
				"draft_complete": viewer.draftComplete,
			},
		}, nil
	}

	modules := route.NewFileModuleRegistry()
	if err := modules.Register(route.FileModuleFor(pagePath, route.FileModuleOptions{Load: fixtureData})); err != nil {
		t.Fatalf("register fixture module: %v", err)
	}
	if err := modules.Register(route.FileModuleFor(indexPagePath, route.FileModuleOptions{Load: fixtureData})); err != nil {
		t.Fatalf("register index fixture module: %v", err)
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
	body := recorder.Body.String()
	assertNoStrayCommentLines(t, target, body, viewer.footerLine)
	return body
}

// navigationTagPattern replaces every tag with a newline, the same way
// this codebase's own manual verification command does (curl ... | sed
// 's/<[^>]*>/\n/g' | grep -c '^\s*//'): GoSX does not collapse a text
// node's own whitespace the way JSX does, so a tag boundary is where a
// rendered line actually breaks, not just where an original .gsx source
// line happened to break.
var navigationTagPattern = regexp.MustCompile(`<[^>]*>`)

// assertNoStrayCommentLines fails the test when the rendered page's text,
// with every tag replaced by a newline, carries a line that starts with
// "//". Before GoSX v0.53.11, a markup block never stripped a Go-style
// "//" comment the way plain Go source does, so a developer comment left
// inside a return <...> block (or, here, the shared layout chrome every
// route renders) rendered as visible text instead of staying source-only
// (wave-7 re-audit, item 5). GoSX v0.53.11 now strips that comment at
// compile time; every call to renderNavigationLayout still carries this
// render-level guard automatically, so it backs every page's chrome, not
// only the fixture routes this file exercises directly.
//
// Two lines are allowed to start with "//" without being a leftover
// comment:
//
//  1. A bare "//" with nothing else on the line. This is the site's own
//     "LABEL // detail" divider glyph (every section-index span in
//     app/*.gsx uses it, e.g. "01 // MATCHUP PREVIEW", traced back to
//     the original "GRIDIRON 2000 // Eight seats. One trophy." tagline).
//     Between two independently tagged elements — app/page.gsx's
//     live-bound score-ticker spans — it renders as its own isolated
//     line under this exact tag-to-newline check, with no way to merge
//     it into a neighboring live-bound span's own text without risking a
//     live-patch overwriting it. A genuine leftover developer comment is
//     never contentless, so a bare "//" line is never itself a defect.
//  2. "// " followed by allowedFooterLine, when the caller explicitly
//     configured that footer line (navigationViewerFixture.footerLine):
//     app/layout.gsx's own footer divider renders "// {footer_line}" the
//     same way, and only a caller that deliberately set it — proving the
//     content is expected, not coincidental — is exempted.
//
// See TestGSXMarkupCommentsCompileAway (root package,
// gsx_markup_comment_contract_test.go) for the matching compile-time
// guard, and its own doc comment for the identical reasoning applied to
// the .gsx source instead of rendered HTML.
func assertNoStrayCommentLines(t *testing.T, label, html, allowedFooterLine string) {
	t.Helper()
	allowedFooterDivider := ""
	if allowedFooterLine != "" {
		allowedFooterDivider = "// " + allowedFooterLine
	}
	text := navigationTagPattern.ReplaceAllString(html, "\n")
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "//") {
			continue
		}
		if trimmed == "//" || (allowedFooterDivider != "" && trimmed == allowedFooterDivider) {
			continue
		}
		t.Errorf("%s rendered a stray comment line: %q", label, trimmed)
	}
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

// railIndex (D15, spruce audit) mirrors layout.gsx's own DraftComplete-
// gated index spans: "09 Draft results" is the only OPTIONAL rail slot
// (present only once the draft is complete), so every following link's
// two-digit index shifts down by one while it is absent — the rail must
// never skip a number (08 Draft, then 10 Preseason Blitz) just because
// one destination in the middle is hidden.
func railIndex(complete bool, n int) string {
	if !complete {
		n--
	}
	return fmt.Sprintf("%02d", n)
}

func expectedNavigationGroups(viewer navigationViewerFixture) []renderedNavigationGroup {
	gameDay := []string{"/draft|08 Draft"}
	if viewer.draftComplete {
		gameDay = append(gameDay, "/draft/results|09 Draft results")
	}
	gameDay = append(gameDay, "/blitz|"+railIndex(viewer.draftComplete, 10)+" Preseason Blitz")
	groups := []renderedNavigationGroup{
		{Name: "today", Links: []string{"/|01 Home", "/pickem|02 Pick'em", "/matchups|03 Matchups"}},
		{Name: "my-team"},
		{Name: "game-day", Links: gameDay},
		{Name: "league", Links: []string{
			"/wire|" + railIndex(viewer.draftComplete, 11) + " Signal Wire",
			"/activity|" + railIndex(viewer.draftComplete, 12) + " Activity",
			"/locker|" + railIndex(viewer.draftComplete, 13) + " Locker Room",
			"/scoring|" + railIndex(viewer.draftComplete, 14) + " Rules & scoring",
		}},
		{Name: "help", Links: []string{
			"/guide|" + railIndex(viewer.draftComplete, 15) + " Manager guide",
			"/help|" + railIndex(viewer.draftComplete, 16) + " Help center",
		}},
	}
	team := &groups[1].Links
	switch {
	case viewer.hasSeat:
		*team = append(*team, "/team|04 Team terminal")
	case viewer.seatsOpen && viewer.canClaimSeat:
		*team = append(*team, "/join|04 Join a team")
	default:
		// One name per object (wave 6): every role with a /team
		// destination reads "Team terminal" — the seatless branch used
		// to say "Team status" while the document title said Terminal.
		*team = append(*team, "/team|04 Team terminal")
	}
	*team = append(*team, "/board|05 Big Board")
	if viewer.hasSeat || viewer.signedIn {
		*team = append(*team, "/players|06 Player pool")
	}
	if viewer.hasSeat || viewer.commissioner {
		*team = append(*team, "/trades|07 Trades")
	}
	if viewer.commissioner {
		groups = append(groups, renderedNavigationGroup{Name: "commissioner", Links: []string{
			"/commissioner|" + railIndex(viewer.draftComplete, 17) + " All leagues",
			"/admin|" + railIndex(viewer.draftComplete, 18) + " League settings",
		}})
	}
	return groups
}

func TestPrimaryNavigationAuthenticatedSeatlessPlayerPoolAcrossMobileSurfaces(t *testing.T) {
	body := renderNavigationLayout(t, "/pickem?week=2", navigationViewerFixture{signedIn: true, seatsOpen: true})
	document := parseNavigationDocument(t, body)
	surfaces := findNodes(document, func(node *html.Node) bool {
		return node.Type == html.ElementNode && hasClass(node, "primary-navigation")
	})
	if len(surfaces) != 3 {
		t.Fatalf("primary navigation surface count = %d, want desktop/enhanced/static", len(surfaces))
	}
	for index, surface := range surfaces {
		players := findNodes(surface, func(node *html.Node) bool {
			return node.Type == html.ElementNode && node.Data == "a" &&
				nodeAttr(node, "href") == "/players" && hasClass(node, "navigation-link")
		})
		if len(players) != 1 {
			t.Errorf("surface %d player-pool link count = %d, want 1", index, len(players))
		}
	}
	for index, navigation := range surfaces {
		nav := findNodes(navigation, func(node *html.Node) bool {
			return node.Type == html.ElementNode && node.Data == "nav"
		})
		if len(nav) != 1 || nodeAttr(nav[0], "aria-label") != "Primary navigation" {
			t.Errorf("surface %d lacks the labelled primary navigation landmark", index)
		}
	}
	dialogs := findNodes(document, func(node *html.Node) bool {
		return node.Type == html.ElementNode && nodeAttr(node, "data-navigation-surface") == "mobile-enhanced-dialog"
	})
	if len(dialogs) != 1 || nodeAttr(dialogs[0], "role") != "dialog" || nodeAttr(dialogs[0], "aria-modal") != "true" {
		t.Fatalf("enhanced mobile navigation dialog lost its modal accessibility contract")
	}
}

func TestPrimaryNavigationRoleSeatAndSurfaceMatrix(t *testing.T) {
	tests := []struct {
		name   string
		viewer navigationViewerFixture
	}{
		{name: "seated manager", viewer: navigationViewerFixture{signedIn: true, hasSeat: true, seatsOpen: true}},
		{name: "seatless with opening", viewer: navigationViewerFixture{signedIn: true, seatsOpen: true, canClaimSeat: true}},
		{name: "pending co-manager with opening", viewer: navigationViewerFixture{signedIn: true, seatsOpen: true}},
		{name: "seatless full", viewer: navigationViewerFixture{signedIn: true}},
		{name: "demo commissioner without signed in", viewer: navigationViewerFixture{demo: true, commissioner: true}},
		// Wave 7 item 6: the post-draft fixture — "Draft results" joins
		// the game-day group only once the draft has actually completed.
		{name: "seated manager post-draft", viewer: navigationViewerFixture{signedIn: true, hasSeat: true, seatsOpen: true, draftComplete: true}},
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

// TestPrimaryNavigationAttentionDotDrivenByLeagueAttention is build item
// 2's contract: PrimaryNavigation's hot dot comes from
// data.league.attention's pre-shaped per-route fields (internal/league's
// attentionMap), not the old colour-only class the rail hardcoded on
// /pickem. A seated manager with a hot /trades item (an accepted trade
// in review) sees the dot and its visually-hidden count on every
// surface; with no hot /pickem item, /pickem stays plain.
func TestPrimaryNavigationAttentionDotDrivenByLeagueAttention(t *testing.T) {
	body := renderNavigationLayout(t, "/pickem?week=2", navigationViewerFixture{
		signedIn:            true,
		hasSeat:             true,
		tradesHot:           true,
		tradesAttentionText: "1 item needs attention",
	})
	document := parseNavigationDocument(t, body)
	surfaces := findNodes(document, func(node *html.Node) bool {
		return node.Type == html.ElementNode && hasClass(node, "primary-navigation")
	})
	if len(surfaces) != 3 {
		t.Fatalf("primary navigation surface count = %d, want desktop/enhanced/static", len(surfaces))
	}
	for index, surface := range surfaces {
		tradesLinks := findNodes(surface, func(node *html.Node) bool {
			return node.Type == html.ElementNode && node.Data == "a" &&
				nodeAttr(node, "href") == "/trades" && hasClass(node, "navigation-link")
		})
		if len(tradesLinks) != 1 {
			t.Fatalf("surface %d trades link count = %d, want 1", index, len(tradesLinks))
		}
		if !hasClass(tradesLinks[0], "navigation-link--hot") {
			t.Errorf("surface %d trades link missing navigation-link--hot with a hot trades item", index)
		}
		if got := descendantText(tradesLinks[0]); !strings.Contains(got, "1 item needs attention") {
			t.Errorf("surface %d trades link text = %q, want it to carry the visually-hidden count text", index, got)
		}

		pickemLinks := findNodes(surface, func(node *html.Node) bool {
			return node.Type == html.ElementNode && node.Data == "a" &&
				nodeAttr(node, "href") == "/pickem" && hasClass(node, "navigation-link")
		})
		if len(pickemLinks) != 1 {
			t.Fatalf("surface %d pickem link count = %d, want 1", index, len(pickemLinks))
		}
		if hasClass(pickemLinks[0], "navigation-link--hot") {
			t.Errorf("surface %d pickem link carries navigation-link--hot with no pickem attention item", index)
		}
		if got, want := descendantText(pickemLinks[0]), "02 Pick'em"; got != want {
			t.Errorf("surface %d pickem link text = %q, want plain %q", index, got, want)
		}
	}
}

// TestPrimaryNavigationAttentionDotOffWithoutSeat pins "keep the dot off
// for viewers without a seat": a hot trades item must not surface a dot
// or hidden text for a seatless commissioner, even though the /trades
// link itself still renders for them (props.Commissioner gates the
// link's own visibility independently of the seat-gated dot).
func TestPrimaryNavigationAttentionDotOffWithoutSeat(t *testing.T) {
	body := renderNavigationLayout(t, "/pickem?week=2", navigationViewerFixture{
		signedIn:            true,
		commissioner:        true,
		tradesHot:           true,
		tradesAttentionText: "1 item needs attention",
	})
	document := parseNavigationDocument(t, body)
	tradesLinks := findNodes(document, func(node *html.Node) bool {
		return node.Type == html.ElementNode && node.Data == "a" &&
			nodeAttr(node, "href") == "/trades" && hasClass(node, "navigation-link")
	})
	if len(tradesLinks) != 3 {
		t.Fatalf("trades link count = %d, want 3 (commissioner sees /trades without a seat)", len(tradesLinks))
	}
	for index, link := range tradesLinks {
		if hasClass(link, "navigation-link--hot") {
			t.Errorf("trades link %d carries navigation-link--hot for a seatless viewer", index)
		}
		if got, want := descendantText(link), "07 Trades"; got != want {
			t.Errorf("trades link %d text = %q, want plain %q", index, got, want)
		}
	}
}

// TestRailAttentionChipAriaLabelUsesChipLabel pins build item 1: the
// rail-head and mobile-bar attention chips read their aria-label from
// data.league.attention.chip_label (internal/league/service.go's
// attentionChipLabel), not a hand-concatenated
// urgent_count + " items need attention..." string, which pluralized
// "item" wrong for a single urgent item. A seated, non-demo viewer with
// one urgent item sees "1 item needs attention in the Action Center" on
// both chips.
func TestRailAttentionChipAriaLabelUsesChipLabel(t *testing.T) {
	body := renderNavigationLayout(t, "/pickem?week=2", navigationViewerFixture{
		signedIn:             true,
		hasSeat:              true,
		attentionHasItems:    true,
		attentionUrgentCount: 1,
		attentionChipLabel:   "1 item needs attention in the Action Center",
	})
	document := parseNavigationDocument(t, body)
	chips := findNodes(document, func(node *html.Node) bool {
		return node.Type == html.ElementNode && node.Data == "a" && hasClass(node, "rail-attention-chip")
	})
	if len(chips) != 2 {
		t.Fatalf("rail-attention-chip count = %d, want 2 (desktop rail + mobile bar)", len(chips))
	}
	for index, chip := range chips {
		if got, want := nodeAttr(chip, "aria-label"), "1 item needs attention in the Action Center"; got != want {
			t.Errorf("chip %d aria-label = %q, want %q", index, got, want)
		}
	}
}

// TestSignOutFormIsNotManaged pins the wave-1 sign-out fix: GoSX's managed
// navigation layer (client/runtime/host/navigation.ts) intercepts a
// data-gosx-managed form's submit, fetches with Accept: application/json,
// and only performs a soft navigation when the parsed JSON result carries a
// "redirect" field. POST /auth/logout is a raw http.HandlerFunc that always
// answers a plain 303 Location: /login with no JSON body, so a managed
// submit followed the redirect inside the fetch, received HTML it could not
// parse as JSON, and left the browser on the current page with only a
// generic "Action completed." toast — the cookie rotated, but the URL and
// DOM never changed. The logout form must opt out of managed handling
// (data-gosx-managed="false", the same shape team/page.gsx already uses for
// its own full-navigation avatar-upload form) so the browser submits it
// natively and follows the 303 itself.
func TestSignOutFormIsNotManaged(t *testing.T) {
	body := renderNavigationLayout(t, "/pickem?week=2", navigationViewerFixture{signedIn: true, hasSeat: true})
	document := parseNavigationDocument(t, body)
	forms := findNodes(document, func(node *html.Node) bool {
		return node.Type == html.ElementNode && node.Data == "form" && nodeAttr(node, "action") == "/auth/logout"
	})
	if len(forms) == 0 {
		t.Fatal("rendered layout has no /auth/logout form")
	}
	for index, form := range forms {
		if got := nodeAttr(form, "data-gosx-managed"); got != "false" {
			t.Errorf("logout form %d data-gosx-managed = %q, want \"false\" — a managed logout form intercepts the submit and never leaves the current page", index, got)
		}
		// The shorthand only expands into the runtime-contract attribute
		// (data-gosx-form, gosx.FormAttr) when it evaluates truthy; a
		// literal data-gosx-managed="false" leaves it absent. This is the
		// same attribute isManagedFormElement checks client-side, so its
		// absence is the authoritative signal the browser will treat this
		// as a plain, unmanaged form.
		for _, attribute := range form.Attr {
			if attribute.Key == "data-gosx-form" {
				t.Errorf("logout form %d still carries data-gosx-form; the managed-form contract was not opted out", index)
			}
		}
	}
}

// TestMobileBottomBarFourSlotsWithCurrentMarker is item 5's own test
// (2026-09-01 gap audit): the mobile bottom bar renders exactly four
// links/controls — Home, Team, Matchups, More — with the active route
// marked aria-current="page" (GoSX's Link sets this server-side, the same
// queryless contract TestPrimaryNavigationQuerylessCurrentLinkUsesGoSXContract
// already pins for the rail), and More opens the existing
// #primary-navigation-dialog exactly like the phone header's own
// disclosure trigger does.
func TestMobileBottomBarFourSlotsWithCurrentMarker(t *testing.T) {
	body := renderNavigationLayout(t, "/", navigationViewerFixture{signedIn: true, hasSeat: true})
	document := parseNavigationDocument(t, body)
	bars := findNodes(document, func(node *html.Node) bool {
		return node.Type == html.ElementNode && hasClass(node, "app-tabbar")
	})
	if len(bars) != 1 {
		t.Fatalf("app-tabbar count = %d, want 1", len(bars))
	}
	bar := bars[0]

	tabs := findNodes(bar, func(node *html.Node) bool {
		return node.Type == html.ElementNode && (node.Data == "a" || node.Data == "button") && hasClass(node, "app-tabbar__tab")
	})
	if len(tabs) != 4 {
		t.Fatalf("app-tabbar slot count = %d, want 4 (Home, Team, Matchups, More)", len(tabs))
	}

	home := findNodes(bar, func(node *html.Node) bool {
		return node.Type == html.ElementNode && node.Data == "a" && nodeAttr(node, "href") == "/" && hasClass(node, "app-tabbar__tab")
	})
	if len(home) != 1 {
		t.Fatalf("app-tabbar Home link count = %d, want 1", len(home))
	}
	if got := nodeAttr(home[0], "aria-current"); got != "page" {
		t.Errorf("app-tabbar Home link aria-current = %q, want \"page\" (the current route)", got)
	}

	team := findNodes(bar, func(node *html.Node) bool {
		return node.Type == html.ElementNode && node.Data == "a" && nodeAttr(node, "href") == "/team" && hasClass(node, "app-tabbar__tab")
	})
	if len(team) != 1 {
		t.Errorf("app-tabbar Team link count = %d, want 1", len(team))
	}

	matchups := findNodes(bar, func(node *html.Node) bool {
		return node.Type == html.ElementNode && node.Data == "a" && nodeAttr(node, "href") == "/matchups" && hasClass(node, "app-tabbar__tab")
	})
	if len(matchups) != 1 {
		t.Errorf("app-tabbar Matchups link count = %d, want 1", len(matchups))
	}

	more := findNodes(bar, func(node *html.Node) bool {
		return node.Type == html.ElementNode && node.Data == "button" && hasClass(node, "app-tabbar__tab")
	})
	if len(more) != 1 {
		t.Fatalf("app-tabbar More button count = %d, want 1", len(more))
	}
	if got := nodeAttr(more[0], "data-gosx-disclosure-target"); got != "#primary-navigation-dialog" {
		t.Errorf("app-tabbar More button data-gosx-disclosure-target = %q, want \"#primary-navigation-dialog\"", got)
	}
}

// TestMobileBottomBarSignedOutAbsent pins that the bottom bar renders only
// for signed-in/demo viewers, matching the rail and phone header it sits
// beside — a signed-out visitor gets the minimal public header instead.
func TestMobileBottomBarSignedOutAbsent(t *testing.T) {
	body := renderNavigationLayout(t, "/", navigationViewerFixture{})
	if strings.Contains(body, "app-tabbar") {
		t.Error("signed-out layout rendered the mobile bottom bar")
	}
}

// TestPrimaryNavigationLinksCarryTitleTooltip covers wave-6 audit item 10:
// body:has(.draft-shell) .site-rail:not(:hover, :focus-within) collapses
// the rail to its 4rem icon column at rest (font-size: 0 on
// .navigation-link, public/styles.css), leaving only each link's two-digit
// index visible — every navigation-link now carries a title attribute
// repeating its own visible label, so a hovering/focused pointer still
// gets a native tooltip naming the destination the bare index number
// alone did not.
func TestPrimaryNavigationLinksCarryTitleTooltip(t *testing.T) {
	body := renderNavigationLayout(t, "/pickem?week=2", navigationViewerFixture{signedIn: true, hasSeat: true, commissioner: true})
	document := parseNavigationDocument(t, body)
	links := findNodes(document, func(node *html.Node) bool {
		return node.Type == html.ElementNode && node.Data == "a" && hasClass(node, "navigation-link")
	})
	if len(links) == 0 {
		t.Fatal("no .navigation-link rendered")
	}
	for _, link := range links {
		title := nodeAttr(link, "title")
		if title == "" {
			t.Errorf("navigation-link href=%q has no title attribute", nodeAttr(link, "href"))
			continue
		}
		// descendantText includes the mono index span ("01 Home") and, on a
		// hot link, its visually-hidden attention sentence too — a
		// substring check against the full text is enough to prove the
		// title repeats the link's own visible label rather than some
		// unrelated string, without needing to strip the index span out.
		text := descendantText(link)
		if !strings.Contains(text, title) {
			t.Errorf("navigation-link href=%q title=%q is not part of its own visible text %q", nodeAttr(link, "href"), title, text)
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
		// Wave-6 audit item 2: the switch keys on the navigation runtime's
		// own boot attribute (html[data-gosx-navigation-state], set
		// synchronously by client/runtime/host/navigation.ts's
		// setNavigationState "init" call), not on the mere presence of the
		// external <script src="/gosx-nav/..."> tag — a stale hash or a
		// blocked script request left the tag in the HTML with no runtime
		// behind it, permanently hiding the no-JS fallback.
		`html[data-gosx-navigation-state] .mobile-navigation-static`,
		`html[data-gosx-navigation-state] .mobile-navigation-enhanced`,
		`body:has(#primary-navigation-dialog:not([hidden]))`,
		`.mobile-navigation-dialog:not([hidden])`,
		`:focus-visible`,
		`@media (prefers-reduced-motion: reduce)`,
		// Item 5 (2026-09-01 gap audit): the four-slot mobile bottom bar,
		// extracted from (and shared with) /draft's own nav.draft-tabbar.
		`.app-tabbar`,
		`.app-tabbar__tab`,
		`env(safe-area-inset-bottom)`,
		`body:has(.draft-shell) .app-tabbar`,
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
	// Wave-6 audit item 2: no live rule may still key the enhanced/static
	// switch on script presence — html:has(script[data-gosx-navigation=
	// "true"]) is only a valid substring inside this file's own historical
	// comment explaining the fix (see the "Item 2 (wave-6 audit)" comment
	// above the @media (scripting: enabled) block), never inside a
	// selector, so this checks for the selector form specifically: the
	// construct followed by a space and a class selector, exactly how
	// every live rule used it before the fix.
	if regexp.MustCompile(`html:has\(script\[data-gosx-navigation="true"\]\)\s+\.`).MatchString(css) {
		t.Error("navigation CSS still keys a live rule on html:has(script[data-gosx-navigation=\"true\"]) instead of the runtime's own boot attribute")
	}
}

// TestFooterLineRendersAsDividerTextNotACommentLine pins the footer's
// "// {footer_line}" divider text (the same "LABEL // detail" convention
// every section-index span in app/*.gsx uses) as intentional, rendered
// content — not a leftover Go comment. Before wave-7's re-audit, the "//"
// sat alone on its own source line, which is exactly the shape
// TestGSXMarkupCommentsCompileAway (root package) flags as a possible
// stray comment; app/layout.gsx now keeps the divider on the same line as
// its neighboring content so the compile-time guard does not have to
// special-case genuine "//"-styled UI copy.
func TestFooterLineRendersAsDividerTextNotACommentLine(t *testing.T) {
	body := renderNavigationLayout(t, "/pickem", navigationViewerFixture{
		signedIn:      true,
		hasSeat:       true,
		hasFooterLine: true,
		footerLine:    "Eight seats. One trophy.",
	})
	document := parseNavigationDocument(t, body)
	footers := findNodes(document, func(node *html.Node) bool {
		return node.Type == html.ElementNode && hasClass(node, "site-footer")
	})
	if len(footers) != 1 {
		t.Fatalf("rendered %d .site-footer elements, want 1", len(footers))
	}
	paragraphs := findNodes(footers[0], func(node *html.Node) bool {
		return node.Type == html.ElementNode && node.Data == "p"
	})
	if len(paragraphs) == 0 {
		t.Fatal(".site-footer has no <p> element")
	}
	if got, want := descendantText(paragraphs[0]), "Test League // Eight seats. One trophy."; got != want {
		t.Errorf("footer line text = %q, want %q", got, want)
	}
}
