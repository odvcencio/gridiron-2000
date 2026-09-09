package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gridiron-2000/internal/league"
	"gridiron-2000/internal/sim/draft"

	"github.com/chromedp/chromedp"
)

// helpComprehensionRole is the small representative matrix used by the
// comprehension gate. The matrix mirrors the product packet's six
// supported combinations without creating membership through a browser
// mutation: the child starts from a persisted, explicit fixture instead.
type helpComprehensionRole struct {
	Name         string
	Email        string
	Mode         string
	TeamID       string
	Role         string
	RoleLabel    string
	Required     string
	Commissioner bool
}

func writeHelpComprehensionFixture(t *testing.T, mode string, members map[string]league.Member) (configPath, statePath string) {
	t.Helper()
	dir := t.TempDir()
	config, err := os.ReadFile(filepath.Join("config", "league.json.example"))
	if err != nil {
		t.Fatalf("read league example: %v", err)
	}
	config = bytes.Replace(config, []byte(`"mode_label": "DYNASTY"`), []byte(fmt.Sprintf(`"mode_label": "%s"`, strings.ToUpper(mode))), 1)
	configPath = filepath.Join(dir, "league.json")
	if err := os.WriteFile(configPath, config, 0o600); err != nil {
		t.Fatalf("write league fixture: %v", err)
	}
	raw, err := json.Marshal(league.PersistedState{Members: members})
	if err != nil {
		t.Fatalf("marshal help comprehension state: %v", err)
	}
	statePath = filepath.Join(dir, "league-state.json")
	if err := os.WriteFile(statePath, raw, 0o600); err != nil {
		t.Fatalf("write help comprehension state: %v", err)
	}
	return configPath, statePath
}

func startHelpComprehensionChild(t *testing.T, root, mode string, roles []helpComprehensionRole) *simChild {
	t.Helper()
	members := make(map[string]league.Member, len(roles))
	commissioners := make([]string, 0, len(roles))
	for _, role := range roles {
		members[role.Email] = league.Member{
			TeamID: role.TeamID,
			Name:   role.Name,
			Email:  role.Email,
			Role:   role.Role,
		}
		if role.Commissioner {
			commissioners = append(commissioners, role.Email)
		}
	}
	configPath, statePath := writeHelpComprehensionFixture(t, mode, members)
	return startSimChild(t, statePath,
		"GOSX_APP_ROOT="+root,
		"LEAGUE_FILE="+configPath,
		"COMMISSIONER_EMAILS="+strings.Join(commissioners, ","),
	)
}

type helpComprehensionAction struct {
	Href string `json:"href"`
	Text string `json:"text"`
}

type helpComprehensionSnapshot struct {
	Body             string                    `json:"body"`
	Mode             string                    `json:"mode"`
	Role             string                    `json:"role"`
	HasOverlay       bool                      `json:"hasOverlay"`
	Actions          []helpComprehensionAction `json:"actions"`
	TopicLinks       int                       `json:"topicLinks"`
	DfnCount         int                       `json:"dfnCount"`
	InteractiveCount int                       `json:"interactiveCount"`
	KeyboardBlocked  []string                  `json:"keyboardBlocked"`
	SearchLabel      string                    `json:"searchLabel"`
	SearchText       string                    `json:"searchText"`
}

func readHelpComprehensionSnapshot(t *testing.T, ctx context.Context) helpComprehensionSnapshot {
	t.Helper()
	var snapshot helpComprehensionSnapshot
	script := `(function(){
		var visible=function(e){var r=e.getBoundingClientRect();var s=getComputedStyle(e);return r.width>0&&r.height>0&&s.visibility!=='hidden'&&s.display!=='none';};
		var actions=Array.from(document.querySelectorAll('#checklists a')).filter(visible).map(function(a){return {href:a.getAttribute('href')||'',text:(a.textContent||'').trim()};});
		var blocked=Array.from(document.querySelectorAll('#checklists a, #checklists button, #checklists input')).filter(visible).filter(function(e){return e.tabIndex<0;}).map(function(e){return e.tagName+' '+(e.textContent||e.getAttribute('aria-label')||'').trim();});
		var roleCards=Array.from(document.querySelectorAll('#checklists .guide-card .section-index')).map(function(e){return (e.textContent||'').trim();});
		return {
			body:document.body.textContent||'',
			mode:(document.querySelector('.help-masthead .page-kicker')||{}).textContent||'',
			role:roleCards.length?roleCards[0]:'',
			hasOverlay:roleCards.some(function(value){return value.indexOf('COMMISSIONER OVERLAY')>=0;}),
			actions:actions,
			topicLinks:document.querySelectorAll('a[href^="/help/"]').length,
			dfnCount:document.querySelectorAll('#glossary dfn[data-gosx-text-layout]').length,
			interactiveCount:document.querySelectorAll('#checklists a, #checklists button, #checklists input').length,
			keyboardBlocked:blocked,
			searchLabel:(document.querySelector('label[for="help-query"]')||{}).textContent||'',
			searchText:(document.querySelector('#help-query')||{}).getAttribute ? document.querySelector('#help-query').getAttribute('aria-label')||document.querySelector('#help-query').getAttribute('placeholder')||'' : ''
		};
	})()`
	if err := chromedp.Run(ctx, chromedp.Evaluate(script, &snapshot)); err != nil {
		t.Fatalf("read Help comprehension snapshot: %v", err)
	}
	return snapshot
}

func waitHelpGlossaryTextLayout(t *testing.T, ctx context.Context) {
	t.Helper()
	if err := chromedp.Run(ctx, chromedp.ScrollIntoView(`#glossary dfn`, chromedp.ByQuery)); err != nil {
		t.Fatalf("scroll glossary term into view: %v", err)
	}
	probe := waitTextLayoutReady(t, ctx, `#glossary dfn`, 2)
	if !probe.HasTextLayout || !probe.Ready || strings.TrimSpace(probe.Text) == "" {
		t.Fatalf("glossary dfn did not reach readable TextBlock state: %+v", probe)
	}
}

func assertHelpComprehensionSnapshot(t *testing.T, ctx context.Context, role helpComprehensionRole, width int64) {
	t.Helper()
	snapshot := readHelpComprehensionSnapshot(t, ctx)
	body := strings.ToLower(snapshot.Body)
	if !strings.Contains(body, strings.ToLower(role.Mode)) {
		t.Errorf("%s at %dpx: Help omitted configured mode %q", role.Name, width, role.Mode)
	}
	if !strings.Contains(strings.ToLower(snapshot.Role), strings.ToLower(role.RoleLabel)) {
		t.Errorf("%s at %dpx: Help role label = %q, want %q", role.Name, width, snapshot.Role, role.RoleLabel)
	}
	if !strings.Contains(body, strings.ToLower(role.Required)) {
		t.Errorf("%s at %dpx: Help omitted role guidance %q", role.Name, width, role.Required)
	}
	if snapshot.HasOverlay != role.Commissioner {
		t.Errorf("%s at %dpx: commissioner overlay = %t, want %t", role.Name, width, snapshot.HasOverlay, role.Commissioner)
	}
	if snapshot.TopicLinks == 0 {
		t.Error("Help rendered no same-origin topic links")
	}
	if snapshot.InteractiveCount == 0 || snapshot.SearchLabel == "" || snapshot.SearchText == "" {
		t.Errorf("Help search/interactive controls are not readable: %+v", snapshot)
	}
	if len(snapshot.Actions) == 0 {
		t.Error("Help checklist rendered no actionable links")
	}
	for _, action := range snapshot.Actions {
		if action.Href == "" || !strings.HasPrefix(action.Href, "/") || action.Text == "" {
			t.Errorf("Help checklist action is not a named local link: %+v", action)
		}
	}
	if len(snapshot.KeyboardBlocked) != 0 {
		t.Errorf("visible Help checklist controls are removed from keyboard order: %q", snapshot.KeyboardBlocked)
	}

	// This is a browser semantic/text-flow smoke check, not a human
	// comprehension or WCAG certification: the real dfn element has the
	// GoSX TextBlock marker and reaches ready=true with visible text.
	waitHelpGlossaryTextLayout(t, ctx)

	var controls struct {
		InputWidth   float64 `json:"inputWidth"`
		InputHeight  float64 `json:"inputHeight"`
		ButtonWidth  float64 `json:"buttonWidth"`
		ButtonHeight float64 `json:"buttonHeight"`
		InputRight   float64 `json:"inputRight"`
		ButtonRight  float64 `json:"buttonRight"`
		InnerWidth   float64 `json:"innerWidth"`
		InputFont    float64 `json:"inputFont"`
		ButtonFont   float64 `json:"buttonFont"`
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(function(){
		var input=document.querySelector('#help-query'),button=document.querySelector('.help-search button');
		var ir=input.getBoundingClientRect(),br=button.getBoundingClientRect();
		return {inputWidth:ir.width,inputHeight:ir.height,buttonWidth:br.width,buttonHeight:br.height,inputRight:ir.right,buttonRight:br.right,innerWidth:window.innerWidth,inputFont:parseFloat(getComputedStyle(input).fontSize)||0,buttonFont:parseFloat(getComputedStyle(button).fontSize)||0};
	})()`, &controls)); err != nil {
		t.Fatalf("measure Help controls: %v", err)
	}
	if controls.InputWidth <= 0 || controls.InputHeight <= 0 || controls.ButtonWidth <= 0 || controls.ButtonHeight <= 0 || controls.InputRight > controls.InnerWidth+1 || controls.ButtonRight > controls.InnerWidth+1 {
		t.Errorf("Help controls are clipped at %dpx: %+v", width, controls)
	}
	if controls.InputFont < 12 || controls.ButtonFont < 12 {
		t.Errorf("Help controls have unexpectedly small rendered text at %dpx: %+v", width, controls)
	}
	if width <= 390 {
		scrollWidth, innerWidth := documentOverflowPx(t, ctx)
		if scrollWidth > innerWidth {
			t.Errorf("Help overflows horizontally at %dpx: scrollWidth=%d innerWidth=%d", width, scrollWidth, innerWidth)
		}
	}

	if err := chromedp.Run(ctx, chromedp.Focus(`#help-query`, chromedp.ByQuery), chromedp.KeyEvent("\t")); err != nil {
		t.Fatalf("tab from Help search field at %dpx: %v", width, err)
	}
	var active struct {
		Tag   string `json:"tag"`
		Class string `json:"class"`
		Text  string `json:"text"`
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(function(){var e=document.activeElement;return {tag:e?e.tagName:'',class:e?e.className:'',text:e?(e.textContent||'').trim():''};})()`, &active)); err != nil {
		t.Fatalf("read Help tab focus at %dpx: %v", width, err)
	}
	if active.Tag != "BUTTON" || strings.TrimSpace(active.Text) == "" {
		t.Errorf("Tab from Help search did not reach a named search control at %dpx: %+v", width, active)
	}
}

func assertHelpRoute200(t *testing.T, child *simChild, bot *draft.Bot, rawHref string) {
	t.Helper()
	parsed, err := url.Parse(rawHref)
	if err != nil || parsed.IsAbs() || !strings.HasPrefix(parsed.Path, "/help/") {
		t.Fatalf("invalid local Help href %q: %v", rawHref, err)
	}
	req, err := http.NewRequest(http.MethodGet, child.URL+parsed.Path, nil)
	if err != nil {
		t.Fatalf("make Help route request %q: %v", rawHref, err)
	}
	req.Header.Set("X-Test-User", bot.Email+"|"+bot.Name)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET Help route %q: %v", rawHref, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET Help route %q = %d, want 200", rawHref, resp.StatusCode)
	}
}

func runHelpSearchAndTopicJourney(t *testing.T, ctx context.Context, child *simChild, bot *draft.Bot, width, height int64) {
	t.Helper()
	signInBrowserSeat(t, ctx, child, bot, "/help", width, height)
	if err := chromedp.Run(ctx,
		chromedp.SetValue(`#help-query`, "Big Board", chromedp.ByQuery),
		chromedp.Submit(`.help-search`, chromedp.ByQuery),
		chromedp.WaitVisible(`.help-result`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("submit real Help search at %dpx: %v", width, err)
	}
	var href string
	if err := chromedp.Run(ctx, chromedp.AttributeValue(`.help-result`, "href", &href, nil, chromedp.ByQuery)); err != nil {
		t.Fatalf("read Help search result href: %v", err)
	}
	if href != "/help/big-board-and-autopick" {
		t.Fatalf("Big Board search result href = %q, want /help/big-board-and-autopick", href)
	}
	assertHelpRoute200(t, child, bot, href)
	if err := chromedp.Run(ctx, chromedp.Click(`.help-result`, chromedp.ByQuery, chromedp.NodeVisible)); err != nil {
		t.Fatalf("click Big Board Help result: %v", err)
	}
	waitForLocation(t, ctx, child.URL+href)
	if err := chromedp.Run(ctx, chromedp.WaitVisible(`#main-content`, chromedp.ByQuery)); err != nil {
		t.Fatalf("Big Board topic did not render after real search click: %v", err)
	}
	body := strings.ToLower(evalString(t, ctx, `document.body.textContent || ''`))
	if !strings.Contains(body, "big board") || !strings.Contains(body, "auto") {
		t.Fatalf("Big Board topic omitted its visible guidance: %q", body[:minHelpTextLen(len(body))])
	}
	if err := chromedp.Run(ctx, chromedp.WaitVisible(`nav[aria-label="Topic actions"]`, chromedp.ByQuery)); err != nil {
		t.Fatalf("Big Board topic omitted its action navigation: %v", err)
	}
	var hasBackToHelp bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(`Array.from(document.querySelectorAll('nav[aria-label="Topic actions"] a')).some(function(a){return new URL(a.href,window.location.href).pathname==='/help';})`, &hasBackToHelp)); err != nil {
		t.Fatalf("inspect Big Board topic Back to help center link: %v", err)
	}
	if !hasBackToHelp {
		t.Fatal("Big Board topic omitted Back to help center link")
	}
}

func runHelpReturnContextJourney(t *testing.T, ctx context.Context, child *simChild, bot *draft.Bot, width, height int64) {
	t.Helper()
	signInBrowserSeat(t, ctx, child, bot, "/team?week=1", width, height)
	if err := chromedp.Run(ctx, chromedp.WaitVisible(`.team-lineup-region`, chromedp.ByQuery)); err != nil {
		t.Fatalf("Team region did not render for Help return journey at %dpx: %v", width, err)
	}
	helpHref := readContextualHelpHref(t, ctx, `.team-lineup-region a[href^="/help/lineups-locks-matchups-and-scoring"]`)
	want := contextualHelpTopicPath + "?return_to=%2Fteam%3Fweek%3D1%23lineup"
	if helpHref != want {
		t.Fatalf("Team contextual Help href = %q, want %q", helpHref, want)
	}
	if err := chromedp.Run(ctx, chromedp.Click(`.team-lineup-region a[href^="/help/lineups-locks-matchups-and-scoring"]`, chromedp.ByQuery, chromedp.NodeVisible)); err != nil {
		t.Fatalf("click Team contextual Help link at %dpx: %v", width, err)
	}
	waitForLocation(t, ctx, child.URL+helpHref)
	returnHref := contextualHrefPath(t, readContextualHelpHref(t, ctx, `a[href$="/team?week=1#lineup"]`))
	if returnHref != "/team?week=1#lineup" {
		t.Fatalf("Help return CTA = %q, want /team?week=1#lineup", returnHref)
	}
	if err := chromedp.Run(ctx, chromedp.Click(`a[href$="/team?week=1#lineup"]`, chromedp.ByQuery, chromedp.NodeVisible)); err != nil {
		t.Fatalf("click Help return CTA at %dpx: %v", width, err)
	}
	waitForLocation(t, ctx, child.URL+returnHref)
	if err := chromedp.Run(ctx, chromedp.WaitVisible(`.team-lineup-region`, chromedp.ByQuery)); err != nil {
		t.Fatalf("Team region did not return from Help at %dpx: %v", width, err)
	}
}

func minHelpTextLen(length int) int {
	if length < 500 {
		return length
	}
	return 500
}

// TestBrowserHelpComprehensionGateRoleMatrix is the automated evidence
// packet for the PX1 comprehension gate. It checks the real authenticated
// Help route for the dynasty/redraft primary, co-manager, seatless, and
// commissioner-overlay projections, then exercises search, a topic route,
// and an owning-route return target. The geometry/focus checks are browser
// smoke evidence only; a human reading/comprehension session remains
// intentionally NOT RUN.
func TestBrowserHelpComprehensionGateRoleMatrix(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	root := browserAppRoot(t)
	dynastyRoles := []helpComprehensionRole{
		{Name: "Dynasty Primary", Email: "dynasty-primary@example.test", Mode: "dynasty", TeamID: "team-1", RoleLabel: "PRIMARY MANAGER", Required: "Open the Team terminal", Commissioner: true},
		{Name: "Dynasty Co-manager", Email: "dynasty-co@example.test", Mode: "dynasty", TeamID: "team-1", Role: "co", RoleLabel: "CO-MANAGER", Required: "private team-seat order"},
		{Name: "Dynasty Seatless", Email: "dynasty-seatless@example.test", Mode: "dynasty", RoleLabel: "SEATLESS MEMBER", Required: "Big Board truth"},
	}
	redraftRoles := []helpComprehensionRole{
		{Name: "Redraft Primary", Email: "redraft-primary@example.test", Mode: "redraft", TeamID: "team-1", RoleLabel: "PRIMARY MANAGER", Required: "Open the Team terminal"},
		{Name: "Redraft Co-manager", Email: "redraft-co@example.test", Mode: "redraft", TeamID: "team-1", Role: "co", RoleLabel: "CO-MANAGER", Required: "private team-seat order", Commissioner: true},
		{Name: "Redraft Seatless", Email: "redraft-seatless@example.test", Mode: "redraft", RoleLabel: "SEATLESS MEMBER", Required: "Big Board truth", Commissioner: true},
	}

	for _, modeFixture := range []struct {
		name  string
		mode  string
		roles []helpComprehensionRole
	}{
		{name: "dynasty", mode: "dynasty", roles: dynastyRoles},
		{name: "redraft", mode: "redraft", roles: redraftRoles},
	} {
		t.Run(modeFixture.name, func(t *testing.T) {
			child := startHelpComprehensionChild(t, root, modeFixture.mode, modeFixture.roles)
			chrome := chromePath(t)
			ctx := newBrowserContext(t, chrome)
			for _, viewport := range []struct {
				name   string
				width  int64
				height int64
				all    bool
			}{
				{name: "phone-320", width: 320, height: 780, all: false},
				{name: "phone-390", width: 390, height: 844, all: true},
				{name: "desktop", width: 1440, height: 900, all: false},
			} {
				t.Run(viewport.name, func(t *testing.T) {
					roles := modeFixture.roles
					if !viewport.all {
						roles = []helpComprehensionRole{modeFixture.roles[0], modeFixture.roles[2]}
					}
					for _, role := range roles {
						bot := draft.New(child.URL, role.Email, role.Name)
						signInBrowserSeat(t, ctx, child, bot, "/help", viewport.width, viewport.height)
						assertHelpComprehensionSnapshot(t, ctx, role, viewport.width)
					}
				})
			}

			if modeFixture.mode == "dynasty" {
				bot := draft.New(child.URL, dynastyRoles[0].Email, dynastyRoles[0].Name)
				runHelpSearchAndTopicJourney(t, ctx, child, bot, 390, 844)
				runHelpReturnContextJourney(t, ctx, child, bot, 390, 844)
				runHelpReturnContextJourney(t, ctx, child, bot, 1440, 900)
			}
		})
	}
}
