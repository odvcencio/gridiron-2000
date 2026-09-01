package commissioner

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"gridiron-2000/internal/commissionerhq"
)

func TestCommissionerSSRIsRichReadOnlyAndPIIFree(t *testing.T) {
	source, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	page := string(source)
	for _, want := range []string{
		"commissioner-hq__queue",
		"commissioner-hq__provenance",
		"APP VERSION",
		"SOURCE GIT SHA",
		"BUILD TIMESTAMP",
		"FRAMEWORK VERSION",
		"UNKNOWN",
		"commissioner-hq__ledger",
		"commissioner-hq__details",
		"datetime={props.GeneratedAtISO}",
		"draft_start_copy",
		"planning target",
		"YEAR ONE PLAYOFFS",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("commissioner SSR missing %q", want)
		}
	}
	if strings.Contains(page, "<form") {
		t.Error("commissioner HQ must remain read-only")
	}
	for _, forbidden := range []string{"email", "service_url", "service.internal", "bearer", "token", "session_id"} {
		if strings.Contains(strings.ToLower(page), forbidden) {
			t.Errorf("commissioner SSR contains forbidden privacy material %q", forbidden)
		}
	}
}

func TestFleetViewSortsAttentionAndKeepsOnlyQualifiedPublicLinks(t *testing.T) {
	now := time.Date(2026, time.August, 22, 18, 0, 0, 0, time.UTC)
	entry := commissionerhq.FleetEntry{
		PeerID:    "flagship",
		PublicURL: "https://gridiron.example",
		Summary: commissionerhq.Summary{
			GeneratedAt: now,
			Instance:    commissionerhq.Instance{Name: "GRIDIRON 2000", ShortCode: "G2K", Mode: "dynasty", Season: 2026, PublicURL: "https://gridiron.example"},
			Runtime:     commissionerhq.Runtime{Ready: true, AppVersion: "0.53.0", FrameworkVersion: "0.53.0", GitSHA: "abcdef0123456789", Build: "2026-08-22T17:00:00Z"},
			Membership:  commissionerhq.Membership{Seats: 8, ClaimedSeats: 7, ReadySeats: 6, SeatLedger: []commissionerhq.SeatLedgerEntry{{Seat: 1, Claimed: true, Ready: true}}},
			Draft:       commissionerhq.Draft{Status: "live", Started: true, ScheduledAt: now, OrderSet: true, ClockArmed: true, ClockRemainingSec: 60, Order: []int{1}},
			Season: commissionerhq.Season{
				Phase: "preseason", CurrentWeek: 1,
				Schedule:  commissionerhq.Schedule{Published: true, WeekCount: 14, StartWeek: 1, EndWeek: 14, CurrentWeek: 1, TotalMatchups: 56, FinalWeeks: 0, FinalMatchups: 0},
				WeekClose: commissionerhq.WeekClose{Week: 1, GamesTotal: 8},
				Playoffs:  commissionerhq.Playoffs{Note: "Year one note"},
			},
			Pool: commissionerhq.Pool{Mode: "live", Actual: 120, Target: 140, RosterCapacity: 80, Cushion: 40, ActualCoverage: 1.5, TargetCoverage: 1.75, RosterCoverage: 1.5, LastSync: now},
			Attention: []commissionerhq.Attention{
				{Code: "pool_shortfall", Severity: "warning", Count: 2, Area: "pool", Message: "Pool needs planning headroom"},
				{Code: "draft_start_required", Severity: "critical", Count: 1, Area: "draft", Message: "Commissioner start required"},
			},
		},
	}
	failed := commissionerhq.FleetEntry{PeerID: "skl", PublicURL: "https://sk.example", Error: "League unavailable"}
	view := buildFleetView([]commissionerhq.FleetEntry{entry, failed}, now, time.UTC)
	if len(view.Cards) != 2 || view.LeagueCount != 2 {
		t.Fatalf("fleet cards = %#v", view.Cards)
	}
	if len(view.Attention) != 3 || view.Attention[0].Severity != "CRITICAL" {
		t.Fatalf("attention order = %#v", view.Attention)
	}
	if !strings.Contains(view.Attention[0].OwnerURL, "/admin?section=draft-control#admin-draft-control") {
		t.Fatalf("qualified owner URL = %q", view.Attention[0].OwnerURL)
	}
	if view.Cards[1].PublicURL != "https://sk.example" || view.Cards[1].HomeURL != "https://sk.example/" {
		t.Fatalf("failed peer link = %#v", view.Cards[1])
	}
	payload, err := json.Marshal(view.toData())
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"email", "service.internal", "bearer-token", "service_url", "raw_error"} {
		if strings.Contains(strings.ToLower(string(payload)), forbidden) {
			t.Errorf("fleet view leaked %q: %s", forbidden, payload)
		}
	}
}

func TestNonCommissionerSSRDoesNotFetchFleet(t *testing.T) {
	request := httptest.NewRequest("GET", "/commissioner", nil)
	called := false
	data := commissionerPageDataWithReader(request, false, func(_ context.Context) []commissionerhq.FleetEntry {
		called = true
		return nil
	}, true)
	if called {
		t.Fatal("noncommissioner SSR fetched fleet state")
	}
	if data["is_commissioner"] != false {
		t.Fatalf("noncommissioner SSR data = %#v", data)
	}
	readout, ok := data["fleet"].(fleetReadoutProps)
	if !ok || readout.IsCommissioner || readout.LeagueCount != 0 {
		t.Fatalf("noncommissioner fleet readout = %#v", data["fleet"])
	}
}

func TestWeekCloseReadyAttentionRoutesToNormalCloseControl(t *testing.T) {
	card := cardView(commissionerhq.FleetEntry{
		PeerID: "g2k", PublicURL: "https://gridiron.example",
		Summary: commissionerhq.Summary{
			Instance:  commissionerhq.Instance{Name: "GRIDIRON 2000", PublicURL: "https://gridiron.example"},
			Season:    commissionerhq.Season{WeekClose: commissionerhq.WeekClose{Week: 1, Ready: true}},
			Attention: []commissionerhq.Attention{{Code: "week_close_ready", Severity: "warning", Area: "schedule", Message: "ready"}},
		},
	}, time.Now(), time.UTC)
	if len(card.Attention) != 1 || card.Attention[0].Section != "week-close" || card.Attention[0].OwnerURL != "https://gridiron.example/admin?section=week-close#admin-week-close" {
		t.Fatalf("week-close attention route = %+v", card.Attention)
	}
}
