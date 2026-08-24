package league

import (
	"fmt"
	"strings"
)

// These values are intentionally action-specific. They are not secrets; the
// form must carry the value so a native POST cannot accidentally invoke an
// irreversible mutation without the manager deliberately opening its
// confirmation disclosure and checking the consequence acknowledgement.
const (
	playerDropConfirmation    = "drop-player"
	playerAddDropConfirmation = "add-drop-player"
	tradeAcceptConfirmation   = "accept-trade"
	tradeApproveConfirmation  = "approve-trade"
	tradeVetoConfirmation     = "veto-trade"
	// ResetDraftConfirmation and ResetLeagueConfirmation are intentionally
	// distinct, human-readable phrases. They are part of the destructive
	// action contract: a browser cannot accidentally invoke one reset with the
	// confirmation meant for the other.
	ResetDraftConfirmation  = "RESET DRAFT"
	ResetLeagueConfirmation = "RESET LEAGUE"
)

// requireMutationConfirmation is the server-side enforcement point for
// irreversible manager and commissioner actions. Callers must pass the
// action-specific form value explicitly; keeping this value in the service
// signature makes omission a compile-time failure for every caller.
func requireMutationConfirmation(expected, actual string) error {
	if strings.TrimSpace(actual) != expected {
		return fmt.Errorf("this action requires explicit confirmation")
	}
	return nil
}
