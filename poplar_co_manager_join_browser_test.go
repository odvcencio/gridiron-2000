package main

import (
	"strings"
	"testing"
	"time"

	"gridiron-2000/internal/sim/draft"

	"github.com/chromedp/chromedp"
)

// TestBrowserCoManagerInviteRequiresExplicitJoin is Decision 3 (J5 F11):
// a co-manager invite used to bind silently on the invitee's first
// sign-in. On first sign-in through a co-manager invite, the member now
// sees a real confirm question on /login — who invited them, a Join
// button, and a Not now link — and the seat binds only on Join.
func TestBrowserCoManagerInviteRequiresExplicitJoin(t *testing.T) {
	child, league, ctx := startSeatedBrowserChild(t)
	primary := league.bots[0]
	const inviteeEmail = "poplar-co-invitee@sim.test"
	if err := primary.InviteCoManager(inviteeEmail); err != nil {
		t.Fatalf("invite co-manager: %v", err)
	}

	invitee := draft.New(child.URL, inviteeEmail, "Poplar Invitee")
	signInBrowserSeat(t, ctx, child, invitee, "/login", 1440, 900)

	if err := chromedp.Run(ctx, chromedp.WaitVisible(`.co-manager-join-form`, chromedp.ByQuery)); err != nil {
		t.Fatalf("no co-manager join form on /login after sign-in through a pending invite: %v", err)
	}

	var headline, detail string
	if err := chromedp.Run(ctx,
		chromedp.Text(`.login-poster__headline`, &headline, chromedp.ByQuery),
		chromedp.Text(`.account-team + p`, &detail, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("read the pending-invite headline/detail: %v", err)
	}
	if !strings.Contains(strings.ToUpper(headline), "AS CO-MANAGER") {
		t.Errorf("headline = %q, want it to ask about joining as co-manager", headline)
	}
	if !strings.Contains(detail, primary.Name) {
		t.Errorf("detail = %q, want it to name who invited this identity (%q)", detail, primary.Name)
	}

	var joinText string
	if err := chromedp.Run(ctx, chromedp.Text(`.co-manager-join-form button`, &joinText, chromedp.ByQuery)); err != nil {
		t.Fatalf("read the Join button text: %v", err)
	}
	if !strings.Contains(strings.ToUpper(joinText), "JOIN") {
		t.Errorf("join button text = %q, want it to read Join", joinText)
	}

	var notNowHref string
	if err := chromedp.Run(ctx, chromedp.AttributeValue(`.co-manager-join-form ~ a`, "href", &notNowHref, nil, chromedp.ByQuery)); err != nil {
		t.Fatalf("read the Not now link: %v", err)
	}
	if notNowHref != "/" {
		t.Errorf("Not now href = %q, want %q", notNowHref, "/")
	}

	// Clicking Join binds the seat and lands on the home page. The
	// managed submit navigates client-side, so #main-content (present on
	// every page, including /login itself) is not a reliable "arrived"
	// signal — poll the URL instead.
	if err := chromedp.Run(ctx, chromedp.Click(`.co-manager-join-form button`, chromedp.ByQuery)); err != nil {
		t.Fatalf("click Join: %v", err)
	}
	var afterJoinURL string
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if err := chromedp.Run(ctx, chromedp.Location(&afterJoinURL)); err != nil {
			t.Fatalf("read location after Join: %v", err)
		}
		if !strings.HasSuffix(afterJoinURL, "/login") {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !strings.HasSuffix(afterJoinURL, "/") || strings.HasSuffix(afterJoinURL, "/login") {
		t.Errorf("location after Join = %q, want the home page", afterJoinURL)
	}
}

// TestBrowserCoManagerNotNowLeavesInviteUnbound is the companion negative
// case: choosing "Not now" must not bind the seat, and the invite stays
// reachable for a later visit.
func TestBrowserCoManagerNotNowLeavesInviteUnbound(t *testing.T) {
	child, league, ctx := startSeatedBrowserChild(t)
	primary := league.bots[1]
	const inviteeEmail = "poplar-co-invitee-declines@sim.test"
	if err := primary.InviteCoManager(inviteeEmail); err != nil {
		t.Fatalf("invite co-manager: %v", err)
	}

	invitee := draft.New(child.URL, inviteeEmail, "Poplar Decliner")
	signInBrowserSeat(t, ctx, child, invitee, "/login", 1440, 900)
	if err := chromedp.Run(ctx, chromedp.WaitVisible(`.co-manager-join-form`, chromedp.ByQuery)); err != nil {
		t.Fatalf("no co-manager join form on /login: %v", err)
	}

	if err := chromedp.Run(ctx, chromedp.Click(`.co-manager-join-form ~ a`, chromedp.ByQuery)); err != nil {
		t.Fatalf("click Not now: %v", err)
	}
	if err := chromedp.Run(ctx, chromedp.WaitVisible(`#main-content`, chromedp.ByQuery)); err != nil {
		t.Fatalf("no #main-content after Not now: %v", err)
	}

	// Revisiting /login must still show the same pending confirm — Not
	// now must not have consumed or dismissed the invite.
	if err := chromedp.Run(ctx, chromedp.Navigate(child.URL+"/login")); err != nil {
		t.Fatalf("revisit /login: %v", err)
	}
	if err := chromedp.Run(ctx, chromedp.WaitVisible(`.co-manager-join-form`, chromedp.ByQuery)); err != nil {
		t.Fatalf("the pending co-manager invite disappeared after Not now: %v", err)
	}
}
