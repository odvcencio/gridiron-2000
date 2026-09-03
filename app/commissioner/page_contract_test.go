package commissioner

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"gridiron-2000/internal/commissionerhq"
)

func TestUnavailableCardRendersConfiguredPublicLeagueLink(t *testing.T) {
	source, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	page := string(source)
	for _, want := range []string{
		`<If cond={card.available == false}>`,
		`<a href={card.public_url}>Open {card.peer_id} directly →</a>`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("unavailable commissioner card missing %q", want)
		}
	}
	for _, forbidden := range []string{"service_url", "service.internal", "token"} {
		if strings.Contains(page, forbidden) {
			t.Errorf("unavailable commissioner card template mentions trust material %q", forbidden)
		}
	}

	const publicURL = "https://sk.gridiron.draco.quest"
	card := fleetCard(commissionerhq.FleetEntry{
		PeerID: "skl", PublicURL: publicURL, Error: "League unavailable",
	})
	if card["available"] != false || card["peer_id"] != "skl" || card["public_url"] != publicURL {
		t.Fatalf("unavailable card data = %#v", card)
	}
	encoded, err := json.Marshal(card)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"service.internal", "bearer-token", "service_url"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Errorf("unavailable card data leaked %q: %s", forbidden, encoded)
		}
	}
}

// TestSeatLedgerAndDraftOrderNameTeams is item 10's decisive proof
// (2026-09-02 audit): /commissioner used to print a bare "SEAT 1 …
// SEAT 8" ledger and a bare-ordinal draft order ("6 · 3 · 4 · 7 · 5 ·
// 8 · 1 · 2") with no team name anywhere, even though the SAME
// SeatLedger already carries every team's own code and name.
func TestSeatLedgerAndDraftOrderNameTeams(t *testing.T) {
	entry := commissionerhq.FleetEntry{
		PeerID: "flag", PublicURL: "https://flag.gridiron.example",
		Summary: commissionerhq.Summary{
			Instance: commissionerhq.Instance{Name: "FLAG LEAGUE", ShortCode: "FLAG", Mode: "dynasty", Season: 2026, PublicURL: "https://flag.gridiron.example"},
			Runtime:  commissionerhq.Runtime{Ready: true},
			Membership: commissionerhq.Membership{
				Seats: 3, ClaimedSeats: 3, ReadySeats: 3, Members: 3,
				SeatLedger: []commissionerhq.SeatLedgerEntry{
					{Seat: 1, Abbreviation: "E1", Name: "East 1", Claimed: true, Ready: true},
					{Seat: 2, Abbreviation: "E2", Name: "East 2", Claimed: true, Ready: true},
					{Seat: 3, Abbreviation: "W1", Name: "West 1", Claimed: true, Ready: true},
				},
			},
			Draft: commissionerhq.Draft{Order: []int{3, 1, 2}, OrderSet: true},
		},
	}
	card := fleetCard(entry)
	ledger, ok := card["seat_ledger"].([]map[string]any)
	if !ok || len(ledger) != 3 {
		t.Fatalf("seat_ledger = %#v, want 3 named rows", card["seat_ledger"])
	}
	if ledger[0]["abbreviation"] != "E1" || ledger[0]["name"] != "East 1" || ledger[0]["has_team_name"] != true {
		t.Errorf("seat 1 ledger row = %#v, want E1/East 1/has_team_name=true", ledger[0])
	}
	draftOrder, _ := card["draft_order"].(string)
	for _, want := range []string{"3 W1", "1 E1", "2 E2"} {
		if !strings.Contains(draftOrder, want) {
			t.Errorf("draft_order = %q, want it to name each team (missing %q)", draftOrder, want)
		}
	}

	// A peer summary from before this field existed (empty Abbreviation/
	// Name) must still render a plain "SEAT N" and a bare-ordinal order,
	// never a name-shaped gap like "SEAT 1 ·  · ".
	oldPeer := commissionerhq.FleetEntry{
		PeerID: "legacy", PublicURL: "https://legacy.gridiron.example",
		Summary: commissionerhq.Summary{
			Instance: commissionerhq.Instance{Name: "LEGACY LEAGUE", ShortCode: "LEG", Mode: "dynasty", Season: 2026, PublicURL: "https://legacy.gridiron.example"},
			Runtime:  commissionerhq.Runtime{Ready: true},
			Membership: commissionerhq.Membership{
				Seats: 1, SeatLedger: []commissionerhq.SeatLedgerEntry{{Seat: 1, Claimed: true, Ready: true}},
			},
			Draft: commissionerhq.Draft{Order: []int{1}, OrderSet: true},
		},
	}
	legacyCard := fleetCard(oldPeer)
	legacyLedger, ok := legacyCard["seat_ledger"].([]map[string]any)
	if !ok || len(legacyLedger) != 1 || legacyLedger[0]["has_team_name"] != false {
		t.Fatalf("legacy peer seat_ledger = %#v, want has_team_name=false", legacyCard["seat_ledger"])
	}
	if got := legacyCard["draft_order"]; got != "1" {
		t.Errorf("legacy peer draft_order = %v, want the bare ordinal \"1\"", got)
	}
}
