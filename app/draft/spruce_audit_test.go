package draft

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gridiron-2000/internal/league"
)

// spruceCommissioner mirrors shellCommissioner (shell_render_test.go): a
// distinct constant keeps this file's own fixture processes independent
// of that file's env, since each fixture runs in its own subprocess.
const spruceCommissioner = "spruce-commish@example.com"

// TestSpruceAuditPreDraftMarkup runs the audit's own D4/D5/D6/D7/D8/D9/
// D12/D14/D15 fixture: a pre-draft room (draft not started), long team
// names, and the default gridiron-house superflex roster preset — the
// same real-league shape the spruce audit screenshots were taken
// against.
// spruceLeagueJSON is a minimal, valid league config carrying the real
// league's own shape (gridiron-house superflex preset, 17 rounds) and a
// long, real-league team name (the audit's own repro: "DeBÍ TiRAR MáS
// TOUCHDOWNS") — the fixture the D9/D12/D13 checks below need. Every
// other field mirrors config/league.json.example.
const spruceLeagueJSON = `{
  "version": 1,
  "league": {
    "name": "SPRUCE AUDIT LEAGUE",
    "short_code": "SAL",
    "tagline": "Fixture League",
    "mode_label": "DYNASTY",
    "url": "http://localhost:8080",
    "timezone": "America/New_York",
    "season": 2026
  },
  "teams": [
    { "id": "team-1", "name": "DeBÍ TiRAR MáS TOUCHDOWNS", "abbreviation": "DTM", "division": "East", "tone": "cyan" },
    { "id": "team-2", "name": "East 2", "abbreviation": "E2", "division": "East", "tone": "blue" },
    { "id": "team-3", "name": "East 3", "abbreviation": "E3", "division": "East", "tone": "violet" },
    { "id": "team-4", "name": "East 4", "abbreviation": "E4", "division": "East", "tone": "lime" },
    { "id": "team-5", "name": "West 1", "abbreviation": "W1", "division": "West", "tone": "orange" },
    { "id": "team-6", "name": "West 2", "abbreviation": "W2", "division": "West", "tone": "gold" },
    { "id": "team-7", "name": "West 3", "abbreviation": "W3", "division": "West", "tone": "magenta" },
    { "id": "team-8", "name": "West 4", "abbreviation": "W4", "division": "West", "tone": "pink" }
  ],
  "draft": {
    "at": "2099-01-01T00:00:00Z",
    "rounds": 17,
    "pick_clock_seconds": 120,
    "format_label": ""
  },
  "season_start_at": "2099-01-08T00:00:00Z",
  "scoring_format": "half_ppr",
  "copy": { "hero_kicker": "", "footer_line": "", "venue_line": "", "invite_blurb": "" },
  "membership": { "allowed_domain": "" },
  "roster": { "preset": "gridiron-house", "reserve": {}, "ir": 0, "limits": {} },
  "waivers": { "mode": "perf-priority", "season_weight_pct": 60, "faab_budget": 100, "clear_days": 2, "process_time": "09:00" },
  "trades": { "deadline": "", "veto": "commissioner", "review_hours": 24 },
  "postseason": {
    "teamCount": 4, "startWeek": 15, "roundLengthWeeks": 1,
    "qualification": "division-winners-wildcards",
    "tiebreakOrder": ["record", "head-to-head", "points-for", "pickem", "seeded-draw"],
    "byes": 0, "divisionWinnersFirst": true, "reseed": true, "consolation": false, "toiletBowl": false
  }
}`

func TestSpruceAuditPreDraftMarkup(t *testing.T) {
	leagueFile := filepath.Join(t.TempDir(), "league.json")
	if err := os.WriteFile(leagueFile, []byte(spruceLeagueJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestSpruceAuditPreDraftMarkupFixtureProcess$")
	cmd.Env = append(os.Environ(),
		"SPRUCE_PREDRAFT_FIXTURE=1",
		"DATA_FILE="+filepath.Join(t.TempDir(), "league-state.json"),
		"DEMO_MODE=false", "GOOGLE_CLIENT_ID=", "APP_ENV=", "LEAGUE_FILE="+leagueFile,
		"COMMISSIONER_EMAILS="+spruceCommissioner,
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("spruce pre-draft fixture process: %v\n%s", err, output)
	}
}

func TestSpruceAuditPreDraftMarkupFixtureProcess(t *testing.T) {
	if os.Getenv("SPRUCE_PREDRAFT_FIXTURE") == "" {
		t.Skip("fixture helper")
	}
	t.Setenv("DATA_FILE", os.Getenv("DATA_FILE"))
	t.Setenv("DEMO_MODE", "false")
	t.Setenv("GOOGLE_CLIENT_ID", "")

	service := league.Default()
	service.SetPlayerSource(livePool())
	if _, err := service.AssignManager(spruceCommissioner, "Commissioner Seat"); err != nil {
		t.Fatal(err)
	}
	const seated = "long-name-manager@example.com"
	if _, err := service.AssignManager(seated, "Long Name Manager"); err != nil {
		t.Fatal(err)
	}

	handler := buildDraftAuthenticatedHandler(t)
	body := renderDraftForUserPath(t, handler, spruceCommissioner, "/")

	// D15: pre-draft h1 never claims a round/pick that has not happened.
	h1Start := strings.Index(body, `class="draft-command__title"`)
	h1End := strings.Index(body[h1Start:], "</h1>") + h1Start
	if h1Start < 0 || h1End < h1Start {
		t.Fatal("draft-command__title h1 not found")
	}
	h1 := body[h1Start:h1End]
	if !strings.Contains(h1, "opens") {
		t.Errorf("pre-draft h1 must say the room \"opens\" ..., got: %s", h1)
	}
	if strings.Contains(h1, "Round") || strings.Contains(h1, "Pick") {
		t.Errorf("pre-draft h1 must not claim a round/pick before the draft starts: %s", h1)
	}

	// D15: the pill must say "Draft not started" plus a ready count, never
	// a fake on-the-clock team, and a commissioner sees a START trigger.
	if !strings.Contains(body, "Draft not started") {
		t.Error("pre-draft pill missing \"Draft not started\"")
	}
	if !strings.Contains(body, "DRAFT NOT STARTED") {
		t.Error("pre-draft pill status chip missing \"DRAFT NOT STARTED\"")
	}
	if strings.Contains(body, "On the clock") {
		t.Error("pre-draft pill must not claim anyone is on the clock")
	}
	if !strings.Contains(body, "Start the draft") {
		t.Error("commissioner must see a START control pre-draft")
	}

	// D15: the tape pane must not show a synthetic on-clock row above
	// "NO PICKS YET".
	tapeStart := strings.Index(body, `id="tape-latest"`)
	tapeEnd := strings.Index(body[tapeStart:], "NO PICKS YET") + tapeStart
	if tapeStart < 0 || tapeEnd < tapeStart {
		t.Fatal("tape pane / \"NO PICKS YET\" not found")
	}
	tape := body[tapeStart:tapeEnd]
	if strings.Contains(tape, "On the clock") {
		t.Errorf("tape pane must not render a fake on-the-clock row before pick 1: %s", tape)
	}

	// D4: the checklist moved out of the pool pane into its own compact
	// strip above the panes; the available pane itself starts with its
	// head row (a real <table>, D5), never the checklist.
	if strings.Contains(body, `class="draft-available" data-has-adp`) {
		available := body[strings.Index(body, `class="draft-available" data-has-adp`):]
		window := available
		if len(window) > 2000 {
			window = window[:2000]
		}
		if strings.Contains(window, "BEFORE THE ROOM OPENS") {
			t.Error("D4: the checklist still opens the available pane's own body")
		}
	}
	if !strings.Contains(body, `class="draft-preflight"`) {
		t.Error("D4: the pre-draft checklist strip (.draft-preflight) is missing")
	}
	preflightIndex := strings.Index(body, `class="draft-preflight"`)
	panesIndex := strings.Index(body, `class="draft-panes"`)
	tableIndex := strings.Index(body, `class="avail-table"`)
	if preflightIndex < 0 || panesIndex < 0 || tableIndex < 0 || !(panesIndex < preflightIndex && preflightIndex < tableIndex) {
		t.Errorf("D4: expected draft-panes(%d) < draft-preflight(%d) < avail-table(%d)", panesIndex, preflightIndex, tableIndex)
	}

	// D5/D6: the pool is one real <table> (thead/tbody share tracks with
	// the browser's own table layout) and every row carries a dedicated
	// info cell for the news icon, never crowding the name cell.
	if !strings.Contains(body, "<table class=\"avail-table\">") {
		t.Error("D5: pool is not a <table class=\"avail-table\">")
	}
	if !strings.Contains(body, "<thead>") || !strings.Contains(body, "<tbody") {
		t.Error("D5: pool table missing <thead>/<tbody>")
	}
	if !strings.Contains(body, `scope="col"`) {
		t.Error("D5: pool header cells missing scope=\"col\"")
	}
	if !strings.Contains(body, `class="idx avail-row__info-head"`) || !strings.Contains(body, `class="avail-row__info"`) {
		t.Error("D6: pool missing its dedicated info column (head and/or cell)")
	}

	// D7: chips render from the server's own position list, including P,
	// and ALL carries aria-pressed="true" on the unfiltered pre-draft
	// load — never a client-only guess.
	chipsStart := strings.Index(body, `class="draft-available-head__chips"`)
	chipsEnd := strings.Index(body[chipsStart:], `class="draft-available-head__sort"`) + chipsStart
	if chipsStart < 0 || chipsEnd < chipsStart {
		t.Fatal("D7: position chip row not found")
	}
	chips := body[chipsStart:chipsEnd]
	if !strings.Contains(chips, ">P</a>") {
		t.Error("D7: no P chip in the position filter row")
	}
	if !strings.Contains(chips, `aria-pressed="true">ALL</a>`) {
		t.Errorf("D7: ALL chip must be aria-pressed on the unfiltered load: %s", chips)
	}

	// D8: before the draft starts, the VS ADP column is hidden entirely
	// (not a negative delta from a pick that has not happened). The
	// column's own <th> abbr is the specific check — the pool legend's
	// prose (below the chips) explains the term unconditionally, so a
	// bare substring match on the phrase would pass even with the column
	// wrongly showing.
	if strings.Contains(body, `<th scope="col" class="idx"><abbr title="value if drafted at the next pick`) {
		t.Error("D8: VS ADP column must not render before the draft starts")
	}

	// D9: the sort toggle exists and defaults to HOUSE for this league's
	// superflex (gridiron-house) roster preset.
	if !strings.Contains(body, `class="draft-available-head__sort"`) {
		t.Fatal("D9: sort toggle row missing")
	}
	sortStart := strings.Index(body, `class="draft-available-head__sort"`)
	sortEnd := strings.Index(body[sortStart:], "</div>") + sortStart
	sort := body[sortStart:sortEnd]
	if !strings.Contains(sort, ">ADP</a>") || !strings.Contains(sort, ">HOUSE</a>") {
		t.Errorf("D9: sort toggle missing ADP/HOUSE options: %s", sort)
	}
	if !strings.Contains(sort, `aria-pressed="true">HOUSE</a>`) {
		t.Errorf("D9: HOUSE must default active for a superflex preset: %s", sort)
	}

	// D12: team identity is structured (name/manager on separate
	// elements, never concatenated), and a team with no picks yet gets an
	// honest note instead of nine identical zero chips. The Teams pane
	// (DraftByTeam) renders only on "?view=teams" (DraftHistory picks one
	// sub-view per request), so this needs its own fetch.
	teamsBody := renderDraftForUserPath(t, handler, spruceCommissioner, "/?view=teams")
	if !strings.Contains(teamsBody, `DeBÍ TiRAR MáS TOUCHDOWNS`) {
		t.Fatal("long team name fixture did not take")
	}
	nameIdx := strings.Index(teamsBody, `class="team-column__name"`)
	if nameIdx < 0 {
		t.Fatal("D12: team-column__name missing")
	}
	identitySlice := teamsBody[nameIdx : nameIdx+400]
	if !strings.Contains(identitySlice, `class="team-column__manager"`) {
		t.Errorf("D12: team-column__manager missing near the name: %s", identitySlice)
	}
	if !strings.Contains(teamsBody, "Picks appear here as they are made.") {
		t.Error("D12: no honest pre-draft note for a team with no picks yet")
	}

	// D14: current-state is exclusive — the three link tabs never carry
	// aria-current (native radio checked/aria-checked is the one signal).
	tabbarStart := strings.Index(body, `class="draft-tabbar"`)
	tabbarEnd := strings.Index(body[tabbarStart:], "</nav>") + tabbarStart
	tabbar := body[tabbarStart:tabbarEnd]
	if strings.Contains(tabbar, "aria-current") {
		t.Errorf("D14: mobile tab bar must not carry aria-current anywhere: %s", tabbar)
	}

	// D13: the mobile "Pick history" band gets a real visible label, not
	// just an isolated "↓ Latest" link.
	if !strings.Contains(body, `class="draft-history-head__label mono"`) {
		t.Error("D13: draft-history-head__row is missing its own visible label")
	}
}

// TestSpruceAuditPosPChipPressed covers D7's own repro: "?pos=P shows a
// pressed P chip" — the position chip row must reflect the SERVER's own
// "?pos=" exactly, not a client-only default.
func TestSpruceAuditPosPChipPressed(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestSpruceAuditPosPChipPressedFixtureProcess$")
	cmd.Env = append(os.Environ(),
		"SPRUCE_POSP_FIXTURE=1",
		"DATA_FILE="+filepath.Join(t.TempDir(), "league-state.json"),
		"DEMO_MODE=true", "GOOGLE_CLIENT_ID=", "APP_ENV=", "LEAGUE_FILE=",
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("spruce pos=P fixture process: %v\n%s", err, output)
	}
}

func TestSpruceAuditPosPChipPressedFixtureProcess(t *testing.T) {
	if os.Getenv("SPRUCE_POSP_FIXTURE") == "" {
		t.Skip("fixture helper")
	}
	handler := buildDraftAuthenticatedHandler(t)
	body := renderDraftForUserPath(t, handler, "posp-viewer@example.com", "/?pos=P")
	chipsStart := strings.Index(body, `class="draft-available-head__chips"`)
	chipsEnd := strings.Index(body[chipsStart:], `class="draft-available-head__sort"`) + chipsStart
	if chipsStart < 0 || chipsEnd < chipsStart {
		t.Fatal("position chip row not found")
	}
	chips := body[chipsStart:chipsEnd]
	if !strings.Contains(chips, `aria-pressed="true">P</a>`) {
		t.Errorf("?pos=P must render the P chip pressed: %s", chips)
	}
	if strings.Contains(chips, `aria-pressed="true">ALL</a>`) {
		t.Errorf("?pos=P must not also leave ALL pressed: %s", chips)
	}
}

// TestSpruceAuditBoardGridTwoLineTeamName covers D13's board-grid__team
// fix: the team code and full name are separate elements (a two-line
// markup redwood can style without mid-word clipping), each column
// carries a real title attribute, and the pool pane stays reachable
// beside the grid via the always-visible Picks segment tab.
func TestSpruceAuditBoardGridTwoLineTeamName(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestSpruceAuditBoardGridTwoLineTeamNameFixtureProcess$")
	cmd.Env = append(os.Environ(),
		"SPRUCE_BOARD_FIXTURE=1",
		"DATA_FILE="+filepath.Join(t.TempDir(), "league-state.json"),
		"DEMO_MODE=true", "GOOGLE_CLIENT_ID=", "APP_ENV=", "LEAGUE_FILE=",
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("spruce board fixture process: %v\n%s", err, output)
	}
}

func TestSpruceAuditBoardGridTwoLineTeamNameFixtureProcess(t *testing.T) {
	if os.Getenv("SPRUCE_BOARD_FIXTURE") == "" {
		t.Skip("fixture helper")
	}
	handler := buildDraftAuthenticatedHandler(t)
	body := renderDraftForUserPath(t, handler, "board-viewer@example.com", "/?view=board")
	if !strings.Contains(body, `class="board-grid__code mono"`) {
		t.Error("D13: board-grid__team missing its own two-line code element")
	}
	if !strings.Contains(body, `class="board-grid__fullname"`) {
		t.Error("D13: board-grid__team missing its own two-line full-name element")
	}
	// The Picks segment tab (DraftHistoryHead) sits outside the swapped
	// board view and stays visible regardless of "?view=board" — a
	// manager can always reach the pool/Draft button through it.
	if !strings.Contains(body, `class="segment__option" href=`) {
		t.Error("D13: the Picks/Draft grid/Teams segment must stay reachable on the board view")
	}
}
