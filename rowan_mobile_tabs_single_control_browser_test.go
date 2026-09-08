package main

import (
	"testing"
)

// TestBrowserMobileTabsOfferOneControlPerDestination is Wave D item 7's
// own evidence (J5 F28: the panel navigation offered each destination
// twice, a radio and a link, in two different roles — a screen reader
// met five destinations as eight controls). tab-picks/tab-board/
// tab-teams (DraftMobileTabs, page.gsx) now carry aria-hidden and
// tabindex="-1", leaving their own sibling <a> as the one reachable,
// announced control for each of those three destinations.
func TestBrowserMobileTabsOfferOneControlPerDestination(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	child, league, ctx := startBrowserDraft(t)
	viewer := league.bots[0]
	signInBrowserSeat(t, ctx, child, viewer, "/draft", 390, 844)

	for _, id := range []string{"tab-picks", "tab-board", "tab-teams"} {
		hidden := evalString(t, ctx, `(function(){var e=document.getElementById('`+id+`');return e?e.getAttribute('aria-hidden'):''})()`)
		if hidden != "true" {
			t.Errorf("#%s must carry aria-hidden=\"true\" (the visible <a> beside it is the one reachable control): got %q", id, hidden)
		}
		tabindex := evalString(t, ctx, `(function(){var e=document.getElementById('`+id+`');return e?e.getAttribute('tabindex'):''})()`)
		if tabindex != "-1" {
			t.Errorf(`#%s must carry tabindex="-1" (removed from the tab order): got %q`, id, tabindex)
		}
	}

	// Pool/Big Board keep their existing one-control-each shape (a real
	// <label for="..."> pairing, D14's own doc comment) — never touched
	// by this fix, and never expected to carry aria-hidden.
	for _, id := range []string{"tab-players", "tab-queue"} {
		hidden := evalString(t, ctx, `(function(){var e=document.getElementById('`+id+`');return e?e.getAttribute('aria-hidden'):''})()`)
		if hidden == "true" {
			t.Errorf("#%s (Pool/Big Board, a label-paired radio) must not carry aria-hidden=\"true\"", id)
		}
	}

	// Every destination's own <a> stays fully reachable and visible.
	for _, want := range []string{"Picks", "Draft grid", "Teams"} {
		visible := evalString(t, ctx, `(function(){
			var links = document.querySelectorAll('.draft-tabbar__tab');
			for (var i=0;i<links.length;i++) {
				if (links[i].textContent.trim() === `+backtickQuote(want)+`) {
					var r = links[i].getBoundingClientRect();
					return String(r.width > 0 && r.height > 0);
				}
			}
			return 'not found';
		})()`)
		if visible != "true" {
			t.Errorf("the %q tab link is not visible/reachable: %q", want, visible)
		}
	}
}
