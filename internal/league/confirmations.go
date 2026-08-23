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
