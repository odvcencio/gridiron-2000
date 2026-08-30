package draft

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gridiron-2000/internal/league"
)

// TestLedgerCSVHandlerRejectsMethodAndUnauthorized proves the CSV export's
// routing guards without needing a live draft: POST is rejected before any
// service call, and an unauthorized (non-demo, signed-out) request never
// reaches league.Service at all.
func TestLedgerCSVHandlerRejectsMethodAndUnauthorized(t *testing.T) {
	handler := LedgerCSVHandler(nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/draft/ledger.csv", nil))
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST status = %d, want %d", response.Code, http.StatusMethodNotAllowed)
	}
	if got := response.Header().Get("Allow"); got != http.MethodGet {
		t.Fatalf("Allow = %q, want GET", got)
	}

	nilService := httptest.NewRecorder()
	LedgerCSVHandler(nil).ServeHTTP(nilService, httptest.NewRequest(http.MethodGet, "/draft/ledger.csv", nil))
	if nilService.Code != http.StatusUnauthorized {
		t.Fatalf("nil-service GET status = %d, want %d (draftFragmentAccess must fail closed)", nilService.Code, http.StatusUnauthorized)
	}
}

// TestLedgerCSVHandlerServesCSV runs against a real league.Service in demo
// mode (draftFragmentAccess admits any request once DemoMode() is true, so
// this needs no signed-in session): a GET answers 200 text/csv with an
// attachment disposition and a header row naming every ledger column.
func TestLedgerCSVHandlerServesCSV(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestLedgerCSVHandlerServesCSVFixtureProcess$")
	cmd.Env = append(os.Environ(), "LEDGER_CSV_FIXTURE=1", "DATA_FILE="+filepath.Join(t.TempDir(), "league-state.json"), "DEMO_MODE=true", "GOOGLE_CLIENT_ID=", "APP_ENV=", "LEAGUE_FILE=")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ledger csv fixture process: %v\n%s", err, output)
	}
}

func TestLedgerCSVHandlerServesCSVFixtureProcess(t *testing.T) {
	if os.Getenv("LEDGER_CSV_FIXTURE") == "" {
		t.Skip("fixture helper")
	}
	service := league.Default()
	handler := LedgerCSVHandler(service)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/draft/ledger.csv", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", response.Code, response.Body.String())
	}
	for name, want := range map[string]string{
		"Content-Type":        "text/csv; charset=utf-8",
		"Content-Disposition": `attachment; filename="draft-ledger.csv"`,
		"Cache-Control":       "private, no-store",
	} {
		if got := response.Header().Get(name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
	header := strings.SplitN(response.Body.String(), "\n", 2)[0]
	for _, column := range []string{"pick", "round", "label", "team", "manager", "player", "position", "nfl_team", "made_by", "time_to_pick", "vs_adp"} {
		if !strings.Contains(header, column) {
			t.Errorf("CSV header missing column %q: %s", column, header)
		}
	}
}
