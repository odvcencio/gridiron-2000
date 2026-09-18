package team

import (
	"os"
	"strings"
	"testing"
)

// TestTeamTradeBlockPanelContract pins the team page's trade block panel:
// it renders only once the draft is complete (trade_block_open), its one
// managed form posts the checked player_id values and one note to the
// "trade-block" action with the page's CSRF token and team id, and page.server.go
// registers the action against league.Service.UpdateTradeBlock.
func TestTeamTradeBlockPanelContract(t *testing.T) {
	page, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	source := string(page)
	at := strings.Index(source, `id="trade-block"`)
	if at < 0 {
		t.Fatal("page.gsx has no trade block panel")
	}
	panel := source[at:]
	panel = panel[:strings.Index(panel, "</section>")]
	for _, want := range []string{
		`action={actionPath("trade-block")}`, `data-gosx-managed="true"`,
		`name="csrf_token" value={csrf.token}`, `name="team_id" value={data.team.id}`,
		`<Each of={data.trade_block_options} as="opt">`,
		`type="checkbox" name="player_id" value={opt.ID} checked={opt.Listed}`,
		`name="note" value={data.trade_block_note}`, `maxlength="240"`,
		`{data.trade_block_count}`, `href="/trades#trade-block"`,
	} {
		if !strings.Contains(panel, want) {
			t.Errorf("trade block panel is missing %q", want)
		}
	}
	if !strings.Contains(source[:at], "<If cond={data.trade_block_open}>") {
		t.Error("the trade block panel must be guarded by data.trade_block_open")
	}

	server, err := os.ReadFile("page.server.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(server), `"trade-block": func(ctx *action.Context) error {`) {
		t.Error(`page.server.go does not register a "trade-block" action`)
	}
	if !strings.Contains(string(server), `UpdateTradeBlock(ctx.Request, ctx.FormData["team_id"], ctx.Request.Form["player_id"], ctx.FormData["note"])`) {
		t.Error("the trade-block action must pass every checked player_id (Request.Form, not FormData) to UpdateTradeBlock")
	}
}
