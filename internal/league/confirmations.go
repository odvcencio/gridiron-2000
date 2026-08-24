package league

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
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

var errAdminActionStale = errors.New("this commissioner action is stale; reload and review the current state")

// seatReleaseConfirmation is deliberately human-readable and target-specific.
// The stable seat ID prevents two similarly named franchises from sharing a
// phrase, while the current display label lets a commissioner see exactly
// which renamed seat the destructive action targets.
func seatReleaseConfirmation(teamID, teamName string) string {
	parts := []string{"RELEASE", strings.ToUpper(strings.TrimSpace(teamID))}
	if name := strings.ToUpper(strings.TrimSpace(teamName)); name != "" {
		parts = append(parts, name)
	}
	return strings.Join(parts, " ")
}

// seatTrimConfirmation makes the number being removed part of the deliberate
// acknowledgement. The companion seatTrimToken below detects a render/submit
// race even when the count happens to remain the same.
func seatTrimConfirmation(count int) string {
	return fmt.Sprintf("DROP %d UNCLAIMED SEAT%s", count, func() string {
		if count == 1 {
			return ""
		}
		return "S"
	}())
}

// seatTrimToken is a non-secret optimistic-concurrency token for the exact
// unclaimed-seat set shown on the page. It is opaque in the form so the error
// path never has to echo internal seat IDs, while the server can reject a
// stale render before changing topology or schedule state.
func seatTrimToken(ids []string, order []string, schedule *SeasonSchedule) string {
	scheduleDigest := ""
	if schedule != nil && len(schedule.Weeks) > 0 {
		payload, _ := json.Marshal(schedule)
		digest := sha256.Sum256(payload)
		scheduleDigest = hex.EncodeToString(digest[:])
	}
	payload := strings.Join(ids, "\x00") + "\x01" + strings.Join(order, "\x00") + "\x02" + scheduleDigest
	digest := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(digest[:])
}

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
