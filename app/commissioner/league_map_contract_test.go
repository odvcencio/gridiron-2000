package commissioner

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestCommissionerPageDataIncludesLeagueMapForNavigation pins J4 F15:
// app/layout.gsx's PrimaryNavigation reads data.league.draft_complete to
// decide whether to render the "Draft results" link and to number every
// nav item after it. Before this fix commissionerPageDataWithReader
// returned only "viewer", "is_commissioner", and "fleet" — no "league"
// key at all — so both of PrimaryNavigation's <If cond={props.
// DraftComplete}> branches evaluated false: the /commissioner sidebar
// dropped "Draft results" entirely and printed an empty index span
// (<span class="navigation-link__index mono"></span>) for every link
// after "08 DRAFT", while /admin (whose AdminData already passes this
// same map) numbered every entry correctly.
func TestCommissionerPageDataIncludesLeagueMapForNavigation(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/commissioner", nil)
	for _, isCommissioner := range []bool{true, false} {
		data := commissionerPageDataWithReader(request, isCommissioner, nil, false)
		leagueMap, ok := data["league"].(map[string]any)
		if !ok {
			t.Fatalf("commissioner page data (is_commissioner=%v) = %#v, want a \"league\" key carrying the same map /admin's AdminData already passes to app/layout.gsx", isCommissioner, data)
		}
		if _, ok := leagueMap["draft_complete"].(bool); !ok {
			t.Fatalf("commissioner page data's league map (is_commissioner=%v) = %#v, want a boolean draft_complete key — the same key PrimaryNavigation reads to number the sidebar", isCommissioner, leagueMap)
		}
	}
}
