package league

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
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
	tradeDeclineConfirmation  = "decline-trade"
	tradeApproveConfirmation  = "approve-trade"
	tradeVetoConfirmation     = "veto-trade"
	// boardClearConfirmation and lockerRemoveConfirmation match drop-player
	// and accept-trade's own action-specific, human-readable shape: the
	// gated <details>/required-checkbox pattern (wave-6 item 9) is only a
	// UI affordance unless the value it submits is also enforced here, the
	// same way tradeAcceptConfirmation already is.
	boardClearConfirmation   = "clear-board"
	lockerRemoveConfirmation = "remove-locker-item"
	// ResetDraftConfirmation and ResetLeagueConfirmation are intentionally
	// distinct, human-readable phrases. They are part of the destructive
	// action contract: a browser cannot accidentally invoke one reset with the
	// confirmation meant for the other.
	ResetDraftConfirmation  = "RESET DRAFT"
	ResetLeagueConfirmation = "RESET LEAGUE"
	// ForceCurrentPickConfirmation is the exact typed acknowledgement for
	// the commissioner's destructive current-pick action. The action may
	// consume the on-clock seat's Big Board target (or best available
	// fallback) immediately, even when the clock is paused.
	ForceCurrentPickConfirmation = "FORCE CURRENT PICK"
	// RunWaiversConfirmation is the exact typed acknowledgement for the
	// commissioner's force-run waiver action (F5, commissioner oversight
	// for a run that is stuck or overdue): an out-of-cycle run resolves
	// every currently due claim immediately and cannot be undone from
	// this screen, the same irreversibility class as
	// ForceCurrentPickConfirmation. Moved here from rosterops.go
	// (2026-08-30 review, finding 10) to sit with its peers.
	RunWaiversConfirmation = "RUN WAIVERS NOW"
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

// seatReleaseToken binds a rendered destructive confirmation to one durable
// seat generation and its exact current occupants. It is deliberately opaque
// and non-secret: its job is compare-and-set freshness, not authorization.
func seatReleaseToken(state PersistedState, teamID, teamName string) string {
	occupants := make([]string, 0, 3)
	for email, member := range state.Members {
		if member.TeamID == teamID {
			occupants = append(occupants, "member:"+email+":"+member.Role)
		}
	}
	for email, pendingTeamID := range state.CoInvites {
		if pendingTeamID == teamID {
			occupants = append(occupants, "pending:"+email)
		}
	}
	sort.Strings(occupants)
	payload := strings.Join([]string{teamID, strings.TrimSpace(teamName),
		strconv.FormatUint(state.SeatRevisions[teamID], 10), strings.Join(occupants, "\x00")}, "\x01")
	digest := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(digest[:])
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

// draftCurrentPickToken binds a rendered commissioner control to the one
// current pick it displayed. It is intentionally opaque and non-secret: the
// server re-computes it while holding the Store lock, so a delayed browser
// cannot extend or force a different pick after a pick, pause, resume, or
// other clock transition has committed.
func draftCurrentPickToken(state PersistedState) string {
	number := len(state.Picks) + 1
	teamID := teamOnClock(state.DraftOrder, number)
	deadline := ""
	if !state.ClockDeadline.IsZero() {
		deadline = state.ClockDeadline.UTC().Format("2006-01-02T15:04:05.999999999Z07:00")
	}
	payload := strings.Join([]string{
		"draft-current-pick-v1",
		strconv.Itoa(number),
		teamID,
		deadline,
		strconv.FormatBool(state.ClockPaused),
		strconv.Itoa(state.ClockRemainingSec),
		strconv.Itoa(state.ClockDurationSec),
		strconv.FormatBool(state.DraftStarted),
	}, "\x01")
	digest := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(digest[:])
}

// draftPreviousPickToken binds undo to the exact last pick and the current
// clock state shown with it. After an undo the last pick changes (or
// disappears), so replaying the same native form fails closed before a
// second mutation can occur.
func draftPreviousPickToken(state PersistedState) string {
	if len(state.Picks) == 0 {
		return ""
	}
	pick := state.Picks[len(state.Picks)-1]
	deadline := ""
	if !state.ClockDeadline.IsZero() {
		deadline = state.ClockDeadline.UTC().Format("2006-01-02T15:04:05.999999999Z07:00")
	}
	payload := strings.Join([]string{
		"draft-previous-pick-v1",
		strconv.Itoa(pick.Number),
		pick.TeamID,
		pick.PlayerID,
		pick.MadeAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"),
		pick.MadeBy,
		deadline,
		strconv.FormatBool(state.ClockPaused),
		strconv.Itoa(state.ClockRemainingSec),
		strconv.Itoa(state.ClockDurationSec),
	}, "\x01")
	digest := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(digest[:])
}

// waiverRunToken binds AdminRunWaivers' confirmation screen to the exact
// open-claim set and processing watermark it displayed (2026-08-30
// review, finding 10, matching draftCurrentPickToken's staleness
// pattern): a run already completed, or a claim filed/canceled, since the
// screen rendered changes this token, so a stale double-submit is
// rejected — errAdminActionStale — rather than silently re-running.
func waiverRunToken(state PersistedState) string {
	ids := make([]string, 0, len(state.WaiverClaims))
	for _, c := range state.WaiverClaims {
		ids = append(ids, c.ID)
	}
	sort.Strings(ids)
	processedThrough := ""
	if !state.WaiversProcessedThrough.IsZero() {
		processedThrough = state.WaiversProcessedThrough.UTC().Format("2006-01-02T15:04:05.999999999Z07:00")
	}
	payload := strings.Join([]string{
		"waiver-run-v1",
		processedThrough,
		strings.Join(ids, "\x00"),
	}, "\x01")
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
