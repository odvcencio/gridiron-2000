package draft

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"

	"gridiron-2000/internal/league"
)

// LedgerCSVHandler serves GET /draft/ledger.csv: the completed draft's full
// pick ledger, ascending, one row per pick — the export link DraftHistory's
// FINAL LEDGER offers once the draft completes (page.gsx, D4). Any
// signed-in viewer may fetch it (draftFragmentAccess, the same gate every
// other draft surface uses); the file carries no viewer-specific tint, so
// who fetches it never changes its rows.
func LedgerCSVHandler(service *league.Service) http.Handler {
	allowed := draftFragmentAccess(service)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		if allowed == nil || !allowed(r) {
			http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}
		if service == nil {
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Cache-Control", "private, no-store")
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="draft-ledger.csv"`)
		w.WriteHeader(http.StatusOK)
		writer := csv.NewWriter(w)
		_ = writer.Write([]string{"pick", "round", "label", "drafted_label", "team", "manager", "player", "position", "nfl_team", "made_by", "time_to_pick", "vs_adp"})
		for _, pick := range service.DraftLedger() {
			vsADP := ""
			if pick.HasValue {
				vsADP = pick.ValueLabel
			}
			_ = writer.Write([]string{
				strconv.Itoa(pick.Number), strconv.Itoa(pick.Round), pick.Label, ledgerDraftedLabel(pick), pick.TeamName, pick.Manager,
				pick.PlayerName, pick.Position, pick.NFLTeam, pick.MadeBy, pick.TimeToPick, vsADP,
			})
		}
		writer.Flush()
	})
}

// ledgerDraftedLabel formats one pick's round·pick as "R3 · P28" — the
// same plain-language label /players' owner chip and /activity's draft
// line render (service.go's playerMap/activityMaps, wave 7 item 2/5),
// distinct from pick.Label ("3.04", the tape's own dotted round.slot
// form the "label" column already carries). A named function rather than
// an inline fmt.Sprintf in the writer loop so a unit test can prove the
// format without a live draft to read a ledger row from.
func ledgerDraftedLabel(pick league.TapePick) string {
	return fmt.Sprintf("R%d · P%d", pick.Round, pick.Number)
}
