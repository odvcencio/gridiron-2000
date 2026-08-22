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
