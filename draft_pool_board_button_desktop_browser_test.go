package main

import (
	"strconv"
	"strings"
	"testing"
)

// TestBrowserPoolBoardButtonStaysOneLineAtDesktop pins the wave E
// integration fix for the pool row's "+ BOARD" control: at desktop the
// label must render on one line inside a row of ordinary height, not
// break letter by letter into a tall column (seen at 138px x 83px in
// the season3 build, with every pool row pushed to 213px).
func TestBrowserPoolBoardButtonStaysOneLineAtDesktop(t *testing.T) {
	if testing.Short() {
		t.Skip("sim scenario: skipped under -short")
	}
	child, league, ctx := startBrowserDraft(t)
	viewer := league.bots[0]
	signInBrowserSeat(t, ctx, child, viewer, "/draft", 1440, 900)

	got := evalString(t, ctx, `(function(){
		var b = document.querySelector('.avail-row__rank-button');
		if (!b) { return 'missing'; }
		var r = b.getBoundingClientRect();
		var row = b.closest('tr').getBoundingClientRect();
		return Math.round(r.height) + ',' + Math.round(row.height);
	})()`)
	if got == "missing" {
		t.Fatal("no .avail-row__rank-button rendered in the pool at 1440px")
	}
	parts := strings.Split(got, ",")
	if len(parts) != 2 {
		t.Fatalf("unexpected measurement %q", got)
	}
	buttonHeight, _ := strconv.Atoi(parts[0])
	rowHeight, _ := strconv.Atoi(parts[1])
	if buttonHeight > 52 {
		t.Errorf("+ BOARD control is %dpx tall at 1440px, want a single line (<= 52px)", buttonHeight)
	}
	if rowHeight > 130 {
		t.Errorf("pool row is %dpx tall at 1440px, want <= 130px (two stacked 44px controls plus padding)", rowHeight)
	}
}
