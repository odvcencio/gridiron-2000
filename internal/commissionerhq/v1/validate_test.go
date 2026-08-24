package v1

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGoldenFixtures(t *testing.T) {
	t.Parallel()
	for _, name := range []string{
		"healthy_dynasty_null_digest.json",
		"healthy_redraft_full_digest.json",
		"minimal_capabilities.json",
		"unknown_additive.json",
	} {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			data := fixture(t, name)
			summary, err := Decode(data)
			if err != nil {
				t.Fatalf("Decode() error = %v", err)
			}
			if summary.Contract != ContractName || summary.SchemaVersion != SchemaVersion {
				t.Fatalf("identity = %q/%q", summary.Contract, summary.SchemaVersion)
			}
		})
	}
}

func TestGoldenReleaseVariants(t *testing.T) {
	t.Parallel()
	nullDigest, err := Decode(fixture(t, "healthy_dynasty_null_digest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if nullDigest.Release.ImageDigest != nil {
		t.Fatal("null digest fixture reported a digest")
	}
	fullDigest, err := Decode(fixture(t, "healthy_redraft_full_digest.json"))
	if err != nil {
		t.Fatal(err)
	}
	want := "sha256:48cf409e12251652e64eb55a3b8a914a906097bf3b7e4bb365c0c00bdd944d0b"
	if fullDigest.Release.ImageDigest == nil || *fullDigest.Release.ImageDigest != want {
		t.Fatalf("full digest = %v", fullDigest.Release.ImageDigest)
	}
}

func TestUnknownAdditiveFieldsAndEnumsAreCompatible(t *testing.T) {
	t.Parallel()
	summary, err := Decode(fixture(t, "unknown_additive.json"))
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if summary.League.Format != "guillotine" || summary.Competition.Phase != "midseason" || summary.DataHealth.Quality != "warming" {
		t.Fatalf("unknown additive values were not retained: %+v", summary)
	}
}

func TestInvalidMatrix(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		mutate func(map[string]any)
		want   string
	}{
		{"missing contract", func(m map[string]any) { delete(m, "contract") }, "summary.contract is required"},
		{"null required object", func(m map[string]any) { m["membership"] = nil }, "membership is required"},
		{"missing nested required", func(m map[string]any) { delete(object(m, "league"), "timezone") }, "league.timezone is required"},
		{"wrong contract", func(m map[string]any) { m["contract"] = "other" }, "contract must equal"},
		{"bad severity", func(m map[string]any) { object(array(m, "attention_items")[0], "")["severity"] = "urgent" }, "severity must be"},
		{"negative team count", func(m map[string]any) { object(object(m, "competition"), "teams")["occupied"] = -1 }, "occupied must be nonnegative"},
		{"team sum mismatch", func(m map[string]any) { object(object(m, "competition"), "teams")["vacant"] = 2 }, "occupied + vacant"},
		{"occupied exceeds total with unknown vacant", func(m map[string]any) {
			teams := object(object(m, "competition"), "teams")
			teams["occupied"], teams["vacant"], teams["total"] = 9, nil, 8
			object(m, "membership")["claimed_teams"] = 9
			object(m, "membership")["open_teams"] = nil
		}, "occupied teams must not exceed"},
		{"membership mismatch", func(m map[string]any) { object(m, "membership")["claimed_teams"] = 7 }, "claimed_teams must equal"},
		{"configuration mismatch", func(m map[string]any) { object(m, "configuration")["team_count"] = 7 }, "team_count must equal"},
		{"draft missing capability", func(m map[string]any) { m["capabilities"] = []any{"readiness.v1"} }, "draft must be omitted"},
		{"draft capability missing object", func(m map[string]any) { delete(m, "draft") }, "draft is required"},
		{"readiness capability missing object", func(m map[string]any) { delete(m, "readiness") }, "readiness is required"},
		{"draft capacity product", func(m map[string]any) { object(m, "draft")["pick_capacity"] = 135 }, "pick_capacity must equal"},
		{"capacity missing despite known factors", func(m map[string]any) { object(m, "draft")["pick_capacity"] = nil }, "when both factors are known"},
		{"capacity guessed without rounds", func(m map[string]any) { object(m, "draft")["draft_rounds"] = nil }, "capacity factor is unprovable"},
		{"ready order missing expected", func(m map[string]any) { object(m, "draft")["expected_teams"] = nil }, "expected_teams is required"},
		{"scheduled count", func(m map[string]any) { object(m, "draft")["pick_count"] = 1 }, "scheduled/open"},
		{"complete count wrong state", func(m map[string]any) { d := object(m, "draft"); d["pick_count"] = 136 }, "complete pick count"},
		{"scheduled clock", func(m map[string]any) { object(m, "draft")["on_clock_team_id"] = "team-1" }, "on_clock_team_id is not allowed"},
		{"missing scheduled time", func(m map[string]any) { object(m, "draft")["scheduled_at"] = nil }, "scheduled_at presence"},
		{"ready exceeds expected", func(m map[string]any) { object(m, "draft")["ready_teams"] = 9 }, "ready_teams must not exceed"},
		{"non UTC time", func(m map[string]any) { m["produced_at"] = "2026-08-24T16:30:05-04:00" }, "produced_at must be"},
		{"invalid timezone", func(m map[string]any) { object(m, "league")["timezone"] = "Mars/Olympus" }, "league.timezone must be"},
		{"local pseudo timezone", func(m map[string]any) { object(m, "league")["timezone"] = "Local" }, "league.timezone must be"},
		{"configuration format mismatch", func(m map[string]any) { object(m, "configuration")["format"] = "redraft" }, "configuration.format must equal"},
		{"build after response", func(m map[string]any) { object(m, "release")["built_at"] = "2026-08-25T20:00:00Z" }, "release.built_at must not be after"},
		{"zero sha sentinel", func(m map[string]any) { object(m, "release")["git_sha"] = strings.Repeat("0", 40) }, "nonsentinel full lowercase"},
		{"f digest sentinel", func(m map[string]any) { object(m, "release")["image_digest"] = "sha256:" + strings.Repeat("f", 64) }, "nonsentinel sha256"},
		{"duplicate attention code", func(m map[string]any) {
			items := array(m, "attention_items")
			m["attention_items"] = append(items, clone(items[0]))
		}, "duplicate code"},
		{"attention league mismatch", func(m map[string]any) { object(array(m, "attention_items")[0], "")["league_id"] = "other" }, "does not match"},
		{"warning order", func(m map[string]any) {
			m["warnings"] = []any{map[string]any{"code": "z", "severity": "info", "summary": "Info."}, map[string]any{"code": "a", "severity": "blocking", "summary": "Blocking."}}
		}, "normative severity/code order"},
		{"member email in warning source", func(m map[string]any) {
			m["warnings"] = []any{map[string]any{"code": "provider_notice", "severity": "info", "summary": "Provider notice.", "source": "person@example.com"}}
		}, "must not contain member identity"},
		{"activity order", func(m map[string]any) {
			items := array(object(m, "recent_activity"), "items")
			items[0], items[1] = items[1], items[0]
		}, "normative time/id order"},
		{"unapproved link", func(m map[string]any) { object(m, "links")["draft"] = "/admin" }, "links.draft must equal"},
		{"disabled feature link", func(m map[string]any) { c := object(m, "configuration"); c["pickem_enabled"] = false }, "links.pickem must be null"},
		{"unsupported draft board link", func(m map[string]any) {
			m["capabilities"] = []any{"readiness.v1", "data-health.v1"}
			delete(m, "draft")
			object(m, "links")["draft"] = nil
		}, "links.draft and links.board must be null"},
		{"external href", func(m map[string]any) {
			object(array(m, "attention_items")[0], "")["href"] = "https://evil.example/draft"
		}, "approved root-relative"},
		{"unapproved query", func(m map[string]any) {
			object(array(object(m, "recent_activity"), "items")[0], "")["href"] = "/activity?next=https%3A%2F%2Fevil.example"
		}, "not approved"},
		{"member email in summary", func(m map[string]any) {
			object(array(m, "attention_items")[0], "")["summary"] = "person@example.com is not ready."
		}, "must not contain member identity"},
		{"href for unavailable feature", func(m map[string]any) {
			object(m, "links")["pickem"] = nil
			item := object(array(m, "attention_items")[0], "")
			item["category"] = "pickem"
			item["href"] = "/pickem?week=1"
		}, "corresponding native link is unavailable"},
		{"missing attention href with available route", func(m map[string]any) {
			object(array(m, "attention_items")[0], "")["href"] = nil
		}, "required when an approved native route is available"},
		{"missing calendar href with available route", func(m map[string]any) {
			object(object(m, "calendar"), "next_deadline")["href"] = nil
		}, "required when an approved native route is available"},
		{"duplicate capability", func(m map[string]any) { m["capabilities"] = []any{"draft.v1", "draft.v1", "readiness.v1"} }, "duplicate"},
		{"not reported has as of", func(m map[string]any) { h := object(m, "data_health"); h["quality"] = "not_reported" }, "as_of must be null"},
		{"cached source claims live", func(m map[string]any) { h := object(m, "data_health"); h["source_mode"] = "cached" }, "must not be live"},
	}
	base := fixture(t, "healthy_dynasty_null_digest.json")
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var value map[string]any
			if err := json.Unmarshal(base, &value); err != nil {
				t.Fatal(err)
			}
			test.mutate(value)
			data, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			_, err = Decode(data)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Decode() error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestDecodeEnvelopeAndSize(t *testing.T) {
	t.Parallel()
	valid := fixture(t, "minimal_capabilities.json")
	if _, err := Decode(append(append([]byte{}, valid...), []byte(" {}")...)); err == nil || !strings.Contains(err.Error(), "multiple JSON values") {
		t.Fatalf("trailing value error = %v", err)
	}
	oversized := bytes.Repeat([]byte{' '}, 256*1024+1)
	if _, err := Decode(oversized); err == nil || !strings.Contains(err.Error(), "exceeds 256 KiB") {
		t.Fatalf("oversized byte slice error = %v", err)
	}
	if _, err := DecodeReader(bytes.NewReader(oversized)); err == nil || !strings.Contains(err.Error(), "exceeds 256 KiB") {
		t.Fatalf("oversized error = %v", err)
	}
}

func TestDecodeRejectsInvalidUTF8BeforeJSONNormalization(t *testing.T) {
	t.Parallel()
	valid := fixture(t, "minimal_capabilities.json")
	invalid := bytes.Replace(valid, []byte("MINIMAL LEAGUE"), []byte{'M', 0xff, 'N'}, 1)
	if _, err := Decode(invalid); err == nil || !strings.Contains(err.Error(), "valid UTF-8") {
		t.Fatalf("invalid UTF-8 error = %v", err)
	}
}

func TestDecodeRejectsUnpairedUTF16EscapeBeforeJSONNormalization(t *testing.T) {
	t.Parallel()
	valid := fixture(t, "minimal_capabilities.json")
	invalid := bytes.Replace(valid, []byte(`"MINIMAL LEAGUE"`), []byte(`"\ud800"`), 1)
	if _, err := Decode(invalid); err == nil || !strings.Contains(err.Error(), "well-formed Unicode escapes") {
		t.Fatalf("unpaired surrogate error = %v", err)
	}
	paired := bytes.Replace(valid, []byte(`"MINIMAL LEAGUE"`), []byte(`"\ud83c\udfc8 LEAGUE"`), 1)
	if _, err := Decode(paired); err != nil {
		t.Fatalf("paired surrogate Decode() error = %v", err)
	}
	escapedLiteral := bytes.Replace(valid, []byte(`"MINIMAL LEAGUE"`), []byte(`"literal \\ud800"`), 1)
	if _, err := Decode(escapedLiteral); err != nil {
		t.Fatalf("escaped literal Decode() error = %v", err)
	}
}

func TestApprovedRouteRegistry(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		value string
		path  string
		ok    bool
	}{
		{"/activity", "/activity", true},
		{"/activity?page=2&team=blue-bombers", "/activity", true},
		{"/activity?team=blue-bombers&page=2", "/activity", false},
		{"/activity?page=2&page=3", "/activity", false},
		{"//evil.example/activity", "/activity", false},
		{"/activity#private", "/activity", false},
		{"/activity?next=%2Fadmin", "/activity", false},
		{"/activity?q=person%40example.com", "/activity", false},
		{"/join?invite=super-secret-token", "/join", false},
		{"/pickem?week=01", "/pickem", false},
		{"/pickem?week=1", "/pickem", true},
		{"/draft/../admin", "/draft", false},
	} {
		err := validateRoute(test.value, test.path)
		if (err == nil) != test.ok {
			t.Errorf("validateRoute(%q, %q) error = %v, ok=%v", test.value, test.path, err, test.ok)
		}
	}
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func object(parent any, key string) map[string]any {
	if key == "" {
		return parent.(map[string]any)
	}
	return parent.(map[string]any)[key].(map[string]any)
}

func array(parent map[string]any, key string) []any { return parent[key].([]any) }

func clone(value any) any {
	data, _ := json.Marshal(value)
	var result any
	_ = json.Unmarshal(data, &result)
	return result
}
