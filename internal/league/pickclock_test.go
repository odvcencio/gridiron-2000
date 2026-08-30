package league

import (
	"encoding/json"
	"os"
	"testing"
	"time"
)

// minimalLeagueJSON starts from the shipped example (which LoadConfig already
// accepts, documentation_contract_test.go:99-128) and sets one draft field, so
// the test never guesses at the validation surface.
func minimalLeagueJSON(t *testing.T, field string, value any) []byte {
	t.Helper()
	raw, err := os.ReadFile("../../config/league.json.example")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	doc["draft"].(map[string]any)[field] = value
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestPickClockDefaultIs120Seconds(t *testing.T) {
	if DefaultPickClock != 120*time.Second || parsePickClock("") != 120*time.Second {
		t.Fatalf("default = %v, parse(\"\") = %v", DefaultPickClock, parsePickClock(""))
	}
}

func TestPickClockPrecedenceEnvOverConfigOverDefault(t *testing.T) {
	for _, c := range []struct {
		env    string
		config int
		want   time.Duration
	}{{"", 0, 120 * time.Second}, {"", 180, 180 * time.Second}, {"90", 180, 90 * time.Second}, {"", 900, 600 * time.Second}} {
		if got := resolvePickClockDefault(c.env, c.config); got != c.want {
			t.Fatalf("resolvePickClockDefault(%q, %d) = %v, want %v", c.env, c.config, got, c.want)
		}
	}
}

func TestLeagueConfigReadsPickClockSeconds(t *testing.T) {
	cfg, _, err := LoadConfigBytes("league.json", minimalLeagueJSON(t, "pick_clock_seconds", 300)) // (Config, []string, error), config.go:327
	if err != nil || cfg.PickClockSeconds != 300 {
		t.Fatalf("cfg.PickClockSeconds = %d, err = %v", cfg.PickClockSeconds, err)
	}
	if _, _, err := LoadConfigBytes("league.json", minimalLeagueJSON(t, "pick_clock_seconds", 5)); err == nil {
		t.Fatal("5 seconds must fail validation")
	}
}
