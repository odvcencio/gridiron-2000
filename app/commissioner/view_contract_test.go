package commissioner

import (
	"os"
	"strings"
	"testing"
	"time"

	"gridiron-2000/internal/commissionerhq"
)

func TestCommissionerTimeFormattingPreservesProvidedOffset(t *testing.T) {
	offset := time.FixedZone("EDT", -4*60*60)
	value := time.Date(2026, time.August, 22, 16, 0, 0, 0, offset)

	if got := displayTime(value); !strings.Contains(got, "4:00 PM EDT") {
		t.Fatalf("display time lost supplied offset: %q", got)
	}
	if got := displayTime(value); strings.Contains(got, "8:00 PM") {
		t.Fatalf("display time converted league timestamp to local/UTC: %q", got)
	}
	if got, want := isoTime(value), "2026-08-22T16:00:00-04:00"; got != want {
		t.Fatalf("ISO time = %q, want supplied-offset %q", got, want)
	}
}

func TestWeekCloseBadgePrecedenceAndBoundedWaitingReason(t *testing.T) {
	longReason := strings.Repeat("readiness detail ", 20)
	cases := []struct {
		name       string
		close      commissionerhq.WeekClose
		badge      string
		waiting    bool
		reasonWant string
	}{
		{
			name:  "final wins over ready",
			close: commissionerhq.WeekClose{Final: true, Ready: true, Reason: "stale final reason"},
			badge: "FINAL",
		},
		{
			name:  "ready wins over waiting",
			close: commissionerhq.WeekClose{Ready: true, Reason: "stale ready reason"},
			badge: "READY",
		},
		{
			name:       "waiting carries bounded reason",
			close:      commissionerhq.WeekClose{Reason: longReason},
			badge:      "WAITING",
			waiting:    true,
			reasonWant: "readiness detail ",
		},
		{
			name:       "waiting has honest default reason",
			close:      commissionerhq.WeekClose{},
			badge:      "WAITING",
			waiting:    true,
			reasonWant: "Waiting for the week-close readiness checks.",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := weekCloseBadge(tc.close); got != tc.badge {
				t.Fatalf("badge = %q, want %q", got, tc.badge)
			}
			got := weekCloseWaitingReason(tc.close)
			if tc.reasonWant == "" {
				if got != "" {
					t.Fatalf("non-waiting reason = %q, want empty", got)
				}
			} else {
				if !strings.HasPrefix(got, tc.reasonWant) {
					t.Fatalf("waiting reason = %q, want prefix %q", got, tc.reasonWant)
				}
				if len([]rune(got)) > 161 {
					t.Fatalf("waiting reason is unbounded: %d runes", len([]rune(got)))
				}
			}
			card := cardView(commissionerhq.FleetEntry{
				PeerID:    "fixture",
				PublicURL: "https://fixture.example",
				Summary: commissionerhq.Summary{
					Instance: commissionerhq.Instance{Name: "Fixture", PublicURL: "https://fixture.example"},
					Season:   commissionerhq.Season{WeekClose: tc.close},
				},
			})
			payload := card.toMap()
			if got := payload["week_close_badge"]; got != tc.badge {
				t.Fatalf("render badge = %#v, want %q", got, tc.badge)
			}
			if got := payload["week_close_waiting"]; got != tc.waiting {
				t.Fatalf("render waiting flag = %#v, want %t", got, tc.waiting)
			}
			if got := payload["week_close_waiting_reason"]; got != weekCloseWaitingReason(tc.close) {
				t.Fatalf("render reason = %#v, want %q", got, weekCloseWaitingReason(tc.close))
			}
		})
	}
}

func TestCommissionerWeekCloseRenderContracts(t *testing.T) {
	source, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	page := string(source)
	for _, want := range []string{
		"{card.week_close_badge}",
		"<If cond={card.week_close_waiting}>",
		"{card.week_close_waiting_reason}",
		"GAMES FINAL",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("commissioner week-close render missing %q", want)
		}
	}
	finals := finalText(commissionerhq.Schedule{FinalWeeks: 1, WeekCount: 2, FinalMatchups: 5, TotalMatchups: 10})
	if !strings.Contains(finals, "5/10 matchups final") || strings.Contains(finals, "games final") {
		t.Fatalf("season final label = %q, want matchup wording and no ambiguous game wording", finals)
	}
}
