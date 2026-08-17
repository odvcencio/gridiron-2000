package league

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// TransactionPlayer snapshots a player's display identity onto a
// Transaction record (roster-ops spec section 7.1), so the activity feed
// survives pool churn: a player who later leaves the live pool (a Tank01
// sync drop, a corrected ID) still reads correctly in old feed lines.
type TransactionPlayer struct {
	PlayerID string `json:"playerId"`
	Name     string `json:"name"`
	Position string `json:"position"`
	NFLTeam  string `json:"nflTeam"`
}

// Transaction is one append-only roster-mutation log entry (roster-ops
// spec section 7.1). WP-R3 only ever writes Type "add" or "drop"; "claim",
// "trade", "auto-drop", and "commissioner" are reserved for WP-R4, WP-R5,
// and the admin console so their entries slot into the same log and the
// same currentRosters/activityMaps replay with no schema change.
type Transaction struct {
	ID          string              `json:"id"`
	Season      int                 `json:"season"`
	Week        int                 `json:"week"`
	Type        string              `json:"type"`
	TeamID      string              `json:"teamId"`
	OtherTeamID string              `json:"otherTeamId,omitempty"`
	Adds        []TransactionPlayer `json:"adds,omitempty"`
	Drops       []TransactionPlayer `json:"drops,omitempty"`
	Bid         int                 `json:"bid,omitempty"`
	Position    int                 `json:"position,omitempty"`
	OfferID     string              `json:"offerId,omitempty"`
	// By is the mutation's provenance: "manager" for every WP-R3 add/drop
	// (a manager always initiates a player-add/player-drop action), the
	// same MadeBy convention DraftPick already carries (model.go:108-111).
	By   string    `json:"by"`
	Note string    `json:"note,omitempty"`
	At   time.Time `json:"at"`
}

// transactionPlayerFromPlayer snapshots one pool Player's display
// identity onto a TransactionPlayer at commit time (section 7.1).
func transactionPlayerFromPlayer(player Player) TransactionPlayer {
	return TransactionPlayer{
		PlayerID: player.ID,
		Name:     player.Name,
		Position: player.Position,
		NFLTeam:  player.NFLTeam,
	}
}

// randomTransactionID draws "txn-" + 8 random hex characters from
// crypto/rand (section 7.1), the same randomness source
// randomScheduleSeed (admin.go) already uses.
func randomTransactionID() (string, error) {
	var buf [4]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return "txn-" + hex.EncodeToString(buf[:]), nil
}

// transactionPlayerNames joins a TransactionPlayer list into one display
// string ("J. Chase (WR)", or "J. Chase (WR), M. Evans (RB)" for more
// than one), matching the pool's "Name (Position)" label idiom.
func transactionPlayerNames(players []TransactionPlayer) string {
	out := ""
	for index, p := range players {
		if index > 0 {
			out += ", "
		}
		out += fmt.Sprintf("%s (%s)", p.Name, p.Position)
	}
	return out
}

// activityLine renders one Transaction's human-readable feed line
// (roster-ops spec section 8.4): a short verb (action) plus the affected
// player names (player). WP-R3 only ever produces "add" and "drop"
// entries; the remaining branches are forward-compatible defaults for
// WP-R4's claim/auto-drop and WP-R5's trade/commissioner entries so the
// merged feed never renders a blank line once those packages land.
func activityLine(t Transaction) (action string, player string) {
	adds := transactionPlayerNames(t.Adds)
	drops := transactionPlayerNames(t.Drops)
	switch t.Type {
	case "add":
		if drops != "" {
			return "signs", adds + " · drops " + drops
		}
		return "signs", adds
	case "drop":
		return "drops", drops
	case "claim":
		if drops != "" {
			return "claims", adds + " · drops " + drops
		}
		return "claims", adds
	case "trade":
		return "trades", adds + " for " + drops
	case "auto-drop":
		return "auto-drops", drops
	case "commissioner":
		if adds != "" && drops != "" {
			return "commissioner signs", adds + " · drops " + drops
		}
		if adds != "" {
			return "commissioner adds", adds
		}
		return "commissioner drops", drops
	default:
		text := adds
		if drops != "" {
			if text != "" {
				text += " · "
			}
			text += drops
		}
		return t.Type, text
	}
}
