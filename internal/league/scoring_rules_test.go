// Tests for the Rules & Scoring page's data assembly (rules-page rebuild
// spec): each section's facts against a known config, and the isolation
// proof that two different league.json fixtures render two different
// rulesets from the same binary and the same code path.
package league

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newRulesTestService builds a minimal Service carrying cfg as-is (no
// LoadConfig/validateConfig indirection — the same "construct a Service
// literal directly" precedent newTradesTestService and newTestService
// already use) so ScoringData renders exactly what cfg says, nothing
// derived from a boot-time global mutation (see applyActiveConfig's doc
// comment in config.go for why tests never call it).
func newRulesTestService(t *testing.T, cfg Config, draftAt time.Time) *Service {
	t.Helper()
	return &Service{
		store:    NewStore(filepath.Join(t.TempDir(), "state.json")),
		draftAt:  draftAt,
		demoMode: true,
		teams:    defaultTeams(),
		players:  defaultPlayers(),
		cfg:      cfg,
	}
}

// rulesFixtureConfig returns a legal, distinct Config for the isolation
// test below; suffix ("a"/"b") tags every field that must differ from its
// sibling fixture so a passing test cannot be an accident of two fixtures
// that happen to agree.
func rulesFixtureConfig(suffix string) Config {
	cfg := DefaultConfig()
	cfg.Source = "file:league-" + suffix + ".json"
	if suffix == "a" {
		cfg.Name = "Fixture League A"
		cfg.ModeLabel = "DYNASTY"
		cfg.Season = 2026
		cfg.Timezone = "America/New_York"
		cfg.Waivers = WaiversBlock{Mode: "perf-priority", SeasonWeightPct: 60, FAABBudget: 100, ClearDays: 2, ProcessTime: "09:00"}
		cfg.Trades = TradesBlock{Veto: "commissioner", ReviewHours: 24, Deadline: ""}
		return cfg
	}
	cfg.Name = "Fixture League B"
	cfg.ModeLabel = "REDRAFT"
	cfg.Season = 2031
	cfg.Timezone = "America/Los_Angeles"
	cfg.Waivers = WaiversBlock{Mode: "faab", SeasonWeightPct: 25, FAABBudget: 250, ClearDays: 4, ProcessTime: "18:30"}
	cfg.Trades = TradesBlock{Veto: "vote", ReviewHours: 48, Deadline: "2031-11-25T00:00:00Z"}
	return cfg
}

func rulesTestRequest(t *testing.T) *http.Request {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, "/scoring", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	return request
}

// ---------------------------------------------------------------------
// Section-by-section data assembly, against a known config.
// ---------------------------------------------------------------------

func TestRulesIdentityMapReadsConfig(t *testing.T) {
	cfg := rulesFixtureConfig("a")
	draftAt := time.Date(2026, 8, 22, 17, 0, 0, 0, time.UTC)
	svc := newRulesTestService(t, cfg, draftAt)
	location, _ := time.LoadLocation("America/New_York")

	got := svc.rulesIdentityMap(time.Now(), location)
	want := map[string]any{
		"name":       "Fixture League A",
		"short_code": cfg.ShortCode,
		"mode_label": "DYNASTY",
		"season":     2026,
		"team_count": len(defaultTeams()),
		"timezone":   "America/New_York",
	}
	for key, wantValue := range want {
		if got[key] != wantValue {
			t.Errorf("identity_rules[%q] = %v, want %v", key, got[key], wantValue)
		}
	}
	if got["draft_date"] != "Saturday, August 22, 2026" {
		t.Errorf("draft_date = %v, want Saturday, August 22, 2026", got["draft_date"])
	}
}

func TestRulesMembershipMapHonest(t *testing.T) {
	svc := newRulesTestService(t, DefaultConfig(), time.Now())

	// No invites stored yet: the league is open.
	open := svc.rulesMembershipMap(svc.store.Snapshot())
	if open["open"] != true {
		t.Errorf("open league: open = %v, want true", open["open"])
	}
	if open["claimed_seats"] != 0 {
		t.Errorf("open league: claimed_seats = %v, want 0", open["claimed_seats"])
	}

	if err := svc.store.AddInvite("manager@example.com"); err != nil {
		t.Fatalf("AddInvite: %v", err)
	}
	if _, _, err := svc.store.AssignMember("manager@example.com", "Test Manager"); err != nil {
		t.Fatalf("AssignMember: %v", err)
	}
	restricted := svc.rulesMembershipMap(svc.store.Snapshot())
	if restricted["open"] != false {
		t.Errorf("invited league: open = %v, want false", restricted["open"])
	}
	if restricted["claimed_seats"] != 1 {
		t.Errorf("invited league: claimed_seats = %v, want 1", restricted["claimed_seats"])
	}
	if restricted["seat_count"] != len(defaultTeams()) {
		t.Errorf("seat_count = %v, want %d", restricted["seat_count"], len(defaultTeams()))
	}
}

// TestRulesRosterMapIsRenderTolerant proves the Roster section renders
// whatever CurrentRoster() exposes rather than a hardcoded slot list: a
// shape missing K and DST entirely renders no row for either, and a shape
// carrying them renders both — the section adapts to the live shape with
// no template or Go change (owner directive: render-tolerant of shape
// concepts, driven by iterating the accessors).
func TestRulesRosterMapIsRenderTolerant(t *testing.T) {
	svc := newRulesTestService(t, DefaultConfig(), time.Now())

	setRosterShape(RosterPreset{Name: "no-kicker", Slots: map[string]int{"QB": 1, "RB": 2, "WR": 2, "FLEX": 1}, Bench: 4})
	t.Cleanup(clearRosterShape)
	got := svc.rulesRosterMap()
	slots := got["slots"].([]map[string]any)
	seen := map[string]bool{}
	for _, row := range slots {
		seen[row["key"].(string)] = true
	}
	if seen["K"] || seen["DST"] || seen["P"] {
		t.Fatalf("shape with no K/DST/P rendered one anyway: %+v", slots)
	}
	if !seen["FLEX"] {
		t.Fatalf("shape with FLEX did not render a FLEX row: %+v", slots)
	}
	if got["starters"] != 6 || got["bench"] != 4 || got["total"] != 10 || got["rounds"] != 10 {
		t.Fatalf("roster totals = %+v, want starters=6 bench=4 total=10 rounds=10", got)
	}

	setRosterShape(RosterPreset{Name: "with-kicker", Slots: map[string]int{"QB": 1, "RB": 2, "WR": 2, "K": 1, "DST": 1}, Bench: 3})
	got = svc.rulesRosterMap()
	slots = got["slots"].([]map[string]any)
	seen = map[string]bool{}
	for _, row := range slots {
		seen[row["key"].(string)] = true
	}
	if !seen["K"] || !seen["DST"] {
		t.Fatalf("shape with K/DST did not render them: %+v", slots)
	}
}

func TestRulesDraftMapUsesLiveClockAndTimezone(t *testing.T) {
	cfg := rulesFixtureConfig("b")
	svc := newRulesTestService(t, cfg, time.Now().Add(time.Hour))
	if err := svc.store.SetClockDuration(45); err != nil {
		t.Fatalf("SetClockDuration: %v", err)
	}
	got := svc.rulesDraftMap(svc.store.Snapshot(), time.Now())
	if got["timezone"] != "America/Los_Angeles" {
		t.Errorf("timezone = %v, want America/Los_Angeles", got["timezone"])
	}
	if got["clock_seconds"] != 45 {
		t.Errorf("clock_seconds = %v, want 45 (the live override)", got["clock_seconds"])
	}
}

func TestRulesLineupsMapReadsInjuryPrefixConstant(t *testing.T) {
	svc := newRulesTestService(t, DefaultConfig(), time.Now())
	got := svc.rulesLineupsMap()
	want := strings.Join(injuryWarnPrefixes, ", ")
	if got["warn_prefixes"] != want {
		t.Errorf("warn_prefixes = %v, want %v (injuryWarnPrefixes)", got["warn_prefixes"], want)
	}
}

func TestRulesSeasonMapHonestBeforeScheduleAndReportsPhase(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SeasonStartAt = time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	svc := newRulesTestService(t, cfg, time.Now())
	now := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)

	got := svc.rulesSeasonMap(svc.store.Snapshot(), now)
	if got["schedule_generated"] != false || got["playoffs_seeded"] != false {
		t.Fatalf("fresh state season_rules = %+v, want both honest-false", got)
	}
	if got["phase"] != "preseason" {
		t.Errorf("phase before season_start_at with no schedule = %v, want preseason", got["phase"])
	}
}

func TestRulesFreeAgencyMapTracksDraftCompletionAndLiveCap(t *testing.T) {
	svc := newRulesTestService(t, DefaultConfig(), time.Now())
	setRosterShape(RosterPreset{Name: "tiny", Slots: map[string]int{"QB": 1}, Bench: 1})
	t.Cleanup(clearRosterShape)

	before := svc.rulesFreeAgencyMap(svc.store.Snapshot())
	if before["open"] != false {
		t.Errorf("free agency before the draft completes: open = %v, want false", before["open"])
	}
	if before["roster_cap"] != 2 {
		t.Errorf("roster_cap = %v, want 2 (1 starter + 1 bench)", before["roster_cap"])
	}

	total := len(defaultTeams()) * CurrentDraftRounds()
	for number := 1; number <= total; number++ {
		team := teamOnClock(nil, number)
		playerID := fmt.Sprintf("draft-fixture-%03d", number)
		if _, err := svc.store.MakePick(team, playerID, "manager", time.Now(), time.Time{}); err != nil {
			t.Fatalf("seed pick %d: %v", number, err)
		}
	}
	after := svc.rulesFreeAgencyMap(svc.store.Snapshot())
	if after["open"] != true {
		t.Errorf("free agency once every roster spot is drafted: open = %v, want true", after["open"])
	}
}

// ---------------------------------------------------------------------
// The isolation proof: two config fixtures, one code path, two rendered
// rulesets.
// ---------------------------------------------------------------------

func TestScoringDataVariesAcrossConfigFixtures(t *testing.T) {
	svcA := newRulesTestService(t, rulesFixtureConfig("a"), time.Now().Add(48*time.Hour))
	svcB := newRulesTestService(t, rulesFixtureConfig("b"), time.Now().Add(72*time.Hour))

	dataA := svcA.ScoringData(rulesTestRequest(t))
	dataB := svcB.ScoringData(rulesTestRequest(t))

	identityA := dataA["identity_rules"].(map[string]any)
	identityB := dataB["identity_rules"].(map[string]any)
	for _, key := range []string{"name", "mode_label", "season", "timezone"} {
		if identityA[key] == identityB[key] {
			t.Errorf("identity_rules[%q] identical across fixtures: %v", key, identityA[key])
		}
	}

	waiversA := dataA["waivers_rules"].(map[string]any)
	waiversB := dataB["waivers_rules"].(map[string]any)
	if waiversA["mode"] != "perf-priority" || waiversB["mode"] != "faab" {
		t.Fatalf("waivers mode = %v / %v, want perf-priority / faab", waiversA["mode"], waiversB["mode"])
	}
	if waiversA["season_weight_pct"] == waiversB["season_weight_pct"] {
		t.Errorf("season_weight_pct identical across fixtures: %v", waiversA["season_weight_pct"])
	}
	if waiversA["faab_budget"] == waiversB["faab_budget"] {
		t.Errorf("faab_budget identical across fixtures: %v", waiversA["faab_budget"])
	}

	tradesA := dataA["trades_rules"].(map[string]any)
	tradesB := dataB["trades_rules"].(map[string]any)
	if tradesA["veto_mode"] != "commissioner" || tradesB["veto_mode"] != "vote" {
		t.Fatalf("veto_mode = %v / %v, want commissioner / vote", tradesA["veto_mode"], tradesB["veto_mode"])
	}
	if tradesA["is_commissioner"] != true || tradesA["is_vote"] != false {
		t.Errorf("fixture A veto flags = %+v, want is_commissioner=true is_vote=false", tradesA)
	}
	if tradesB["is_vote"] != true || tradesB["is_commissioner"] != false {
		t.Errorf("fixture B veto flags = %+v, want is_vote=true is_commissioner=false", tradesB)
	}
	if tradesA["has_deadline"] != false {
		t.Errorf("fixture A has no deadline: has_deadline = %v, want false", tradesA["has_deadline"])
	}
	if tradesB["has_deadline"] != true {
		t.Errorf("fixture B set a deadline: has_deadline = %v, want true", tradesB["has_deadline"])
	}
	if tradesA["review_hours"] == tradesB["review_hours"] {
		t.Errorf("review_hours identical across fixtures: %v", tradesA["review_hours"])
	}
	// The fixed 7-day expiry (tradeOfferMaxAge) is a code constant, not a
	// config knob: it must agree across both fixtures.
	wantExpiry := int(tradeOfferMaxAge.Hours() / 24)
	if tradesA["expiry_days"] != wantExpiry || tradesB["expiry_days"] != wantExpiry {
		t.Errorf("expiry_days = %v / %v, want %d for both (tradeOfferMaxAge is not a knob)", tradesA["expiry_days"], tradesB["expiry_days"], wantExpiry)
	}

	versionA := dataA["rules_version"].(map[string]any)
	versionB := dataB["rules_version"].(map[string]any)
	if versionA["config_source"] == versionB["config_source"] {
		t.Errorf("config_source identical across fixtures: %v", versionA["config_source"])
	}
}

// TestRulesFingerprintTracksRosterAndScoring pins rulesFingerprint's actual
// scope: it changes when the roster shape or the effective scoring values
// change, and stays stable when neither does, regardless of what else
// differs (identity, waivers, trades) — the citable "what was in force"
// mark for a roster or scoring dispute specifically.
func TestRulesFingerprintTracksRosterAndScoring(t *testing.T) {
	svc := newRulesTestService(t, DefaultConfig(), time.Now())
	baseline := svc.currentScoringValues()

	first := rulesFingerprint(baseline)
	second := rulesFingerprint(svc.currentScoringValues())
	if first != second {
		t.Errorf("fingerprint changed with no roster/scoring change: %q vs %q", first, second)
	}

	if err := svc.store.SetScoringValue("passTD", 5); err != nil {
		t.Fatalf("SetScoringValue: %v", err)
	}
	third := rulesFingerprint(svc.currentScoringValues())
	if third == first {
		t.Errorf("fingerprint unchanged after a scoring override: %q", third)
	}

	setRosterShape(RosterPreset{Name: "fingerprint-fixture", Slots: map[string]int{"QB": 1, "RB": 2}, Bench: 3})
	t.Cleanup(clearRosterShape)
	fourth := rulesFingerprint(svc.currentScoringValues())
	if fourth == third {
		t.Errorf("fingerprint unchanged after a roster-shape override: %q", fourth)
	}
}
