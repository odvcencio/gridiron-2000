package trades

import (
	"os"
	"strings"
	"testing"
)

// TestTradeDeskTradeBlockSectionContract pins the trade desk's league-wide
// block: one section per team with listings (data.trade_block), the
// viewer's own card linking to the Team terminal panel, every other card
// linking into the composer with that counterparty (ProposeHref), each
// player with position, NFL team, and name, the team's shared note once,
// and an
// honest empty state.
func TestTradeDeskTradeBlockSectionContract(t *testing.T) {
	page, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	source := string(page)
	at := strings.Index(source, `id="trade-block"`)
	if at < 0 {
		t.Fatal("page.gsx has no trade block section")
	}
	section := source[at:]
	section = section[:strings.Index(section, "</section>")]
	for _, want := range []string{
		`<If cond={data.trade_block_empty}>`, `NOTHING ON THE BLOCK`,
		`<Each of={data.trade_block} as="team">`,
		`<If cond={team.IsViewer}><a href="/team#trade-block"`,
		`<If cond={team.IsViewer == false}><a href={team.ProposeHref}`,
		`<Each of={team.Players} as="player">`,
		`{player.Position} · {player.NFLTeam}`, `{player.Name}`,
		`<If cond={team.HasNote}>`, `{team.Note}`,
	} {
		if !strings.Contains(section, want) {
			t.Errorf("trade block section is missing %q", want)
		}
	}
	inbox := strings.Index(source, `id="inbox"`)
	if inbox < at {
		t.Error("the trade block must render before the inbox, right after the composer")
	}
}
