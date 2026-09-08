package main

import (
	"testing"

	"github.com/chromedp/chromedp"
)

// TestBrowserAdminGridSectionsRenderInDOMOrder pins J4 F34's own residue
// (wave E): the phone console used to promote Announcements and Week
// close ahead of the rest with CSS `order` alone — a purely visual
// reorder that left keyboard focus order and screen-reader order behind,
// since both follow DOM order, not grid/flex order. With the order
// removed and the template's own <section> sequence matching what the
// page shows, document.querySelectorAll's own order (a DOM-order read,
// the same sequence assistive tech and Tab both use) must already equal
// the visual (top-to-bottom) order — checked at 1440 and phone.
func TestBrowserAdminGridSectionsRenderInDOMOrder(t *testing.T) {
	for _, viewport := range []struct {
		name          string
		width, height int64
	}{
		{"desktop-1440", 1440, 900},
		{"phone-390", 390, 844},
	} {
		t.Run(viewport.name, func(t *testing.T) {
			child, league, ctx := startSeatedBrowserChild(t)
			signInBrowserSeat(t, ctx, child, league.commish, "/admin", viewport.width, viewport.height)

			var ids []string
			if err := chromedp.Run(ctx, chromedp.Evaluate(
				`Array.from(document.querySelectorAll('.admin-grid > section[id]')).map(e => e.id)`,
				&ids,
			)); err != nil {
				t.Fatalf("read .admin-grid section ids in DOM order: %v", err)
			}
			want := []string{
				"admin-draft-control", "admin-schedule", "admin-week-close",
				"admin-announcements", "admin-playoffs", "admin-seats",
				"admin-invites", "admin-draft-order", "admin-data",
				"admin-clock", "admin-roster", "admin-backup", "admin-danger",
			}
			if len(ids) != len(want) {
				t.Fatalf("got %d .admin-grid sections, want %d: %v", len(ids), len(want), ids)
			}
			for i, id := range want {
				if ids[i] != id {
					t.Errorf("DOM position %d = %q, want %q (full order: %v)", i, ids[i], id, ids)
				}
			}

			// The visual (top-to-bottom) order must match: no leftover CSS
			// order should place any section above one that precedes it in
			// the DOM.
			var tops []float64
			if err := chromedp.Run(ctx, chromedp.Evaluate(
				`Array.from(document.querySelectorAll('.admin-grid > section[id]')).map(e => e.getBoundingClientRect().top + window.scrollY)`,
				&tops,
			)); err != nil {
				t.Fatalf("read section tops: %v", err)
			}
			for i := 1; i < len(tops); i++ {
				// Desktop is a 2-column grid, so a later DOM section can sit
				// in the same row (equal top) as an earlier one, never above
				// it; phone is single-column, so tops must strictly climb.
				if tops[i] < tops[i-1] {
					t.Errorf("%s renders above %s (top %v < %v), but follows it in the DOM", want[i], want[i-1], tops[i], tops[i-1])
				}
			}

			var orderValues []string
			if err := chromedp.Run(ctx, chromedp.Evaluate(
				`Array.from(document.querySelectorAll('.admin-grid > section[id]')).map(e => getComputedStyle(e).order)`,
				&orderValues,
			)); err != nil {
				t.Fatalf("read computed order values: %v", err)
			}
			for i, value := range orderValues {
				if value != "0" {
					t.Errorf("%s has a non-default computed order (%q); the DOM order fix should leave no CSS order behind", want[i], value)
				}
			}
		})
	}
}
