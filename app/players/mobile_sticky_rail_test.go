package players

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPlayersPoolSearchIsAPhoneSearchFieldWithAVisibleLabel is item 2's own
// input contract: the pool search box is a native <input type="search">
// with the mobile keyboard hints (inputmode="search",
// enterkeyhint="search") a phone needs to show a "search" key instead of a
// generic "go", and keeps its own visible <label> (unchanged from before
// wave 7b) rather than relying on aria-label alone. Page() embeds
// PlayerPoolRegion() directly (item 1's root-cause fix, 2026-09-02
// route-crawl finding — rowan), so this one control — one id, not a
// "-sync-" duplicate — is what a manager sees on the initial render and
// after every region swap alike.
func TestPlayersPoolSearchIsAPhoneSearchFieldWithAVisibleLabel(t *testing.T) {
	page, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	source := string(page)
	for _, want := range []string{
		`<label class="mono" for="players-search">SEARCH //</label>`,
		`<input id="players-search" type="search" name="q" value={data.query} placeholder="Search player or team" inputmode="search" enterkeyhint="search" autocomplete="off"></input>`,
	} {
		if !strings.Contains(source, want) {
			t.Errorf("players page.gsx missing search-field contract %q", want)
		}
	}
	if strings.Contains(source, "players-sync-search") {
		t.Error("players page.gsx still carries a duplicate players-sync-search id — one id per control")
	}
}

// TestPlayersFAABBidCarriesANumericKeyboardAndARealLabel is item 2's own
// FAAB contract: the bid field used to be placeholder-only, with no
// <label> element at all and a bare type="number" that still shows a
// phone's full punctuation keyboard. It now carries a real (visually
// hidden, since the row already shows "Bid FAAB" as a placeholder)
// <label for=...>, a per-player unique id pairing them, and
// inputmode="numeric"/pattern="[0-9]*"/enterkeyhint="done" so a phone
// shows a numeric pad with a "done" key instead of "go". Page() embeds
// PlayerPoolRegion() directly (item 1's root-cause fix), so this one
// control — one id per player, not a "-sync-" duplicate — carries the
// contract on the initial render and after every region swap alike.
func TestPlayersFAABBidCarriesANumericKeyboardAndARealLabel(t *testing.T) {
	page, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	source := string(page)
	for _, want := range []string{
		`<label class="visually-hidden" for={"players-bid-" + player.id}>{"Bid FAAB for " + player.name}</label>`,
		`<input id={"players-bid-" + player.id} type="number" inputmode="numeric" pattern="[0-9]*" enterkeyhint="done" name="bid" min="0" max={data.my_faab_remaining} placeholder="Bid FAAB"></input>`,
	} {
		if !strings.Contains(source, want) {
			t.Errorf("players page.gsx missing FAAB bid contract %q", want)
		}
	}
	if strings.Contains(source, "players-bid-sync-") {
		t.Error("players page.gsx still carries a duplicate players-bid-sync- id — one id per control")
	}
}

// TestPlayersFilterRailIsStickyWithABackToTopLink covers item 2's sticky
// rail and "Back to top" affordance: .pool-filter-rail (id="pool-search")
// wraps the position filters and search form, the list is followed by a
// #pool-search back-to-top link, and the shared stylesheet's wave 7b
// block pins that rail under the fixed top bar with a sticky position at
// phone/tablet width — the fix for the pre-wave-7b audit finding that
// re-filtering cost a 1200px scroll back up. Page() embeds
// PlayerPoolRegion() directly (item 1's root-cause fix), so this markup
// has exactly one definition in page.gsx.
func TestPlayersFilterRailIsStickyWithABackToTopLink(t *testing.T) {
	page, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	source := string(page)
	if got := strings.Count(source, `<div class="pool-filter-rail" id="pool-search">`); got != 1 {
		t.Errorf(`page.gsx has %d copies of the pool-filter-rail wrapper, want 1 (PlayerPoolRegion())`, got)
	}
	if got := strings.Count(source, `<a class="access-link pool-back-to-top" href="#pool-search">`); got != 1 {
		t.Errorf(`page.gsx has %d back-to-top links, want 1 (PlayerPoolRegion())`, got)
	}

	styles, err := os.ReadFile(filepath.Join("..", "..", "public", "styles.css"))
	if err != nil {
		t.Fatal(err)
	}
	css := string(styles)
	blockStart := strings.Index(css, "/* wave 7b — ash")
	if blockStart < 0 {
		t.Fatal("styles.css is missing the wave 7b — ash block")
	}
	block := css[blockStart:]
	railStart := strings.Index(block, ".pool-filter-rail {")
	if railStart < 0 {
		t.Fatal("wave 7b — ash block is missing the .pool-filter-rail rule")
	}
	railRule := block[railStart : railStart+strings.Index(block[railStart:], "}")]
	for _, want := range []string{"position: sticky", "top: var(--mobile-bar-height)"} {
		if !strings.Contains(railRule, want) {
			t.Errorf(".pool-filter-rail rule missing %q: %s", want, railRule)
		}
	}
	if !strings.Contains(block, ".pool-back-to-top") {
		t.Error("wave 7b — ash block never styles .pool-back-to-top")
	}
}
