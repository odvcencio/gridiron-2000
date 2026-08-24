package fleetconfig

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"syscall"
	"testing"

	v1fleet "gridiron-2000/internal/commissionerhq/v1fleet"
)

const testLeagueJSON = `{
  "version": 1,
  "league": {"name":"Test League","short_code":"TL","tagline":"Fantasy","mode_label":"DYNASTY","url":"http://localhost:8080","timezone":"America/New_York","season":2026},
  "teams": [
    {"id":"team-1","name":"East 1","abbreviation":"E1","division":"East","tone":"cyan"},
    {"id":"team-2","name":"East 2","abbreviation":"E2","division":"East","tone":"blue"},
    {"id":"team-3","name":"East 3","abbreviation":"E3","division":"East","tone":"violet"},
    {"id":"team-4","name":"East 4","abbreviation":"E4","division":"East","tone":"lime"},
    {"id":"team-5","name":"West 1","abbreviation":"W1","division":"West","tone":"orange"},
    {"id":"team-6","name":"West 2","abbreviation":"W2","division":"West","tone":"gold"},
    {"id":"team-7","name":"West 3","abbreviation":"W3","division":"West","tone":"magenta"},
    {"id":"team-8","name":"West 4","abbreviation":"W4","division":"West","tone":"pink"}
  ],
  "draft":{"at":"2099-01-01T00:00:00Z","rounds":15,"format_label":""},
  "season_start_at":"2099-01-08T00:00:00Z",
  "scoring_format":"half_ppr",
  "copy":{"hero_kicker":"","footer_line":"","venue_line":"","invite_blurb":""},
  "membership":{"allowed_domain":""},
  "roster":{"preset":"standard","reserve":{},"ir":0,"limits":{}},
  "waivers":{"mode":"perf-priority","season_weight_pct":60,"faab_budget":100,"clear_days":2,"process_time":"09:00"},
  "trades":{"deadline":"","veto":"commissioner","review_hours":24}
}`

const testImage = "harbor.example/gridiron@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func hq(leagueID string, order int, accent, keyID string, host bool) *CommissionerHQ {
	return &CommissionerHQ{LeagueID: leagueID, Order: order, Accent: accent, KeyID: keyID, Host: host}
}

func instance(id string, config *CommissionerHQ) Instance {
	return Instance{
		ID: id, Namespace: id, ResourcePrefix: id + "-app",
		PublicOrigin: "https://" + id + ".example.test", LeagueConfigPath: "league.json",
		PVCStorage: "1Gi", CommissionerHQ: config,
	}
}

func testFleet() Fleet {
	return Fleet{
		Version: SchemaVersion, Image: testImage,
		StatrelayOrigin: "http://statrelay.gridiron.svc.cluster.local",
		IngressClass:    "traefik", CertificateIssuer: "cloudflare-issuer",
		Instances: []Instance{instance("alpha", hq("alpha-league", 0, "cyan", "hq-alpha", true))},
	}
}

func writeLeague(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(testLeagueJSON+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeFleet(t *testing.T, dir string, fleet Fleet, raw ...string) string {
	t.Helper()
	path := filepath.Join(dir, "fleet.json")
	data := []byte{}
	if len(raw) > 0 {
		data = []byte(raw[0])
	} else {
		var err error
		data, err = json.MarshalIndent(fleet, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		data = append(data, '\n')
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func findFile(t *testing.T, bundle Bundle, path string) string {
	t.Helper()
	for _, file := range bundle.Files {
		if file.Path == path {
			return string(file.Data)
		}
	}
	t.Fatalf("missing generated file %s", path)
	return ""
}

func TestLoadStrictJSONAndRequiredTopologyFields(t *testing.T) {
	dir := t.TempDir()
	writeLeague(t, dir, "league.json")
	base := `{"version":2,"image":"` + testImage + `","statrelay_origin":"http://relay.test","ingress_class":"traefik","certificate_issuer":"issuer","instances":[{"id":"alpha","namespace":"alpha","resource_prefix":"alpha-app","public_origin":"https://alpha.example.test","league_config_path":"league.json","pvc_storage":"1Gi","commissioner_hq":null}]}`
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"unknown", strings.TrimSuffix(base, "}]}") + `,"unexpected":true}]}`, "unknown field"},
		{"trailing", base + "{}\n", "trailing"},
		{"duplicate", strings.Replace(base, `"version":2`, `"version":2,"version":2`, 1), "duplicate object key"},
		{"version", strings.Replace(base, `"version":2`, `"version":1`, 1), "exactly 2"},
		{"empty", `{"version":2,"image":"` + testImage + `","statrelay_origin":"http://relay.test","ingress_class":"traefik","certificate_issuer":"issuer","instances":[]}`, "at least one"},
		{"missing commissioner_hq", strings.Replace(base, `,"commissioner_hq":null`, "", 1), "commissioner_hq"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeFleet(t, dir, Fleet{}, tc.raw)
			if _, _, err := Load(path); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Load error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestLoadRejectsInvalidFleetFields(t *testing.T) {
	cases := []struct {
		name string
		edit func(*Fleet)
		want string
	}{
		{"image tag", func(f *Fleet) { f.Image = "harbor.example/gridiron:latest" }, "immutable"},
		{"image uppercase digest", func(f *Fleet) {
			f.Image = "harbor.example/gridiron@sha256:0123456789ABCDEF0123456789abcdef0123456789abcdef0123456789abcdef"
		}, "immutable"},
		{"relay path", func(f *Fleet) { f.StatrelayOrigin = "http://relay.test/cache" }, "no credentials"},
		{"relay credentials", func(f *Fleet) { f.StatrelayOrigin = "http://user:pass@relay.test" }, "origin"},
		{"relay query", func(f *Fleet) { f.StatrelayOrigin = "http://relay.test?x=1" }, "origin"},
		{"empty id", func(f *Fleet) { f.Instances[0].ID = "" }, "id"},
		{"uppercase namespace", func(f *Fleet) { f.Instances[0].Namespace = "Alpha" }, "namespace"},
		{"long prefix", func(f *Fleet) { f.Instances[0].ResourcePrefix = strings.Repeat("a", 50) }, "unsafe"},
		{"public http", func(f *Fleet) { f.Instances[0].PublicOrigin = "http://alpha.example.test" }, "HTTPS"},
		{"public path", func(f *Fleet) { f.Instances[0].PublicOrigin = "https://alpha.example.test/path" }, "no credentials"},
		{"public credentials", func(f *Fleet) { f.Instances[0].PublicOrigin = "https://u:p@alpha.example.test" }, "credentials"},
		{"public query", func(f *Fleet) { f.Instances[0].PublicOrigin = "https://alpha.example.test?q=1" }, "query"},
		{"public port", func(f *Fleet) { f.Instances[0].PublicOrigin = "https://alpha.example.test:8443" }, "port"},
		{"bad host underscore", func(f *Fleet) { f.Instances[0].PublicOrigin = "https://bad_host.example.test" }, "DNS-1123"},
		{"bad host leading hyphen", func(f *Fleet) { f.Instances[0].PublicOrigin = "https://-bad.example.test" }, "DNS-1123"},
		{"bad host trailing hyphen", func(f *Fleet) { f.Instances[0].PublicOrigin = "https://bad-.example.test" }, "DNS-1123"},
		{"bad host empty label", func(f *Fleet) { f.Instances[0].PublicOrigin = "https://bad..example.test" }, "DNS-1123"},
		{"bad host overlong label", func(f *Fleet) { f.Instances[0].PublicOrigin = "https://" + strings.Repeat("a", 64) + ".example.test" }, "DNS-1123"},
		{"public IPv4", func(f *Fleet) { f.Instances[0].PublicOrigin = "https://127.0.0.1" }, "IP address"},
		{"public IPv6", func(f *Fleet) { f.Instances[0].PublicOrigin = "https://[::1]" }, "IP address"},
		{"bad pvc", func(f *Fleet) { f.Instances[0].PVCStorage = "../../etc" }, "quantity"},
		{"zero pvc", func(f *Fleet) { f.Instances[0].PVCStorage = "0Gi" }, "positive integer"},
		{"decimal pvc", func(f *Fleet) { f.Instances[0].PVCStorage = "1.5Gi" }, "positive integer"},
		{"arbitrary pvc unit", func(f *Fleet) { f.Instances[0].PVCStorage = "1GG" }, "positive integer"},
		{"repeated pvc unit", func(f *Fleet) { f.Instances[0].PVCStorage = "1GiGi" }, "positive integer"},
		{"uppercase repository", func(f *Fleet) {
			f.Image = "Harbor.example/gridiron@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
		}, "immutable"},
		{"double slash repository", func(f *Fleet) {
			f.Image = "harbor.example//gridiron@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
		}, "immutable"},
		{"tagged repository", func(f *Fleet) {
			f.Image = "harbor.example/gridiron:latest@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
		}, "immutable"},
		{"bad registry port", func(f *Fleet) {
			f.Image = "harbor.example:latest/gridiron@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
		}, "immutable"},
		{"absolute league path", func(f *Fleet) { f.Instances[0].LeagueConfigPath = "/tmp/league.json" }, "fleet-relative"},
		{"traversal league path", func(f *Fleet) { f.Instances[0].LeagueConfigPath = "../league.json" }, "escape"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeLeague(t, dir, "league.json")
			fleet := testFleet()
			tc.edit(&fleet)
			path := writeFleet(t, dir, fleet)
			if _, _, err := Load(path); err == nil || !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tc.want)) {
				t.Fatalf("Load error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestLoadRejectsCollisions(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Fleet)
		want   string
	}{
		{"id", func(f *Fleet) {
			duplicate := f.Instances[0]
			duplicate.Namespace = "beta"
			duplicate.ResourcePrefix = "beta-app"
			duplicate.PublicOrigin = "https://beta.example.test"
			duplicate.CommissionerHQ = nil
			f.Instances = append(f.Instances, duplicate)
		}, "duplicate instance id"},
		{"namespace", func(f *Fleet) {
			candidate := instance("beta", nil)
			candidate.Namespace = "alpha"
			f.Instances = append(f.Instances, candidate)
		}, "collides"},
		{"resource prefix", func(f *Fleet) {
			candidate := instance("beta", nil)
			candidate.ResourcePrefix = "alpha-app"
			f.Instances = append(f.Instances, candidate)
		}, "collides"},
		{"public host", func(f *Fleet) {
			candidate := instance("beta", nil)
			candidate.PublicOrigin = "https://alpha.example.test"
			f.Instances = append(f.Instances, candidate)
		}, "public host"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeLeague(t, dir, "league.json")
			fleet := testFleet()
			tc.mutate(&fleet)
			path := writeFleet(t, dir, fleet)
			if _, _, err := Load(path); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Load error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestPublicOriginRejectsLegacyDottedIPv4ButAcceptsNumericDNS(t *testing.T) {
	for _, hostname := range []string{"127.000.000.001", "192.168.001.001", "001.002.003.004"} {
		t.Run("reject-"+hostname, func(t *testing.T) {
			dir := t.TempDir()
			writeLeague(t, dir, "league.json")
			fleet := testFleet()
			fleet.Instances[0].PublicOrigin = "https://" + hostname
			path := writeFleet(t, dir, fleet)
			if _, _, err := Load(path); err == nil || !strings.Contains(err.Error(), "IP address") {
				t.Fatalf("Load error = %v, want legacy IPv4 rejection", err)
			}
		})
	}
	for _, hostname := range []string{"alpha1.example.test", "123.example.test", "1.2.example.test"} {
		t.Run("accept-"+hostname, func(t *testing.T) {
			dir := t.TempDir()
			writeLeague(t, dir, "league.json")
			fleet := testFleet()
			fleet.Instances[0].PublicOrigin = "https://" + hostname
			path := writeFleet(t, dir, fleet)
			if _, _, err := Load(path); err != nil {
				t.Fatalf("Load error = %v, want valid numeric-containing DNS name", err)
			}
		})
	}
}

func TestImageRegistryPathAndLengthContracts(t *testing.T) {
	digest := "sha256:" + strings.Repeat("0", 64)
	if err := validateImage("harbor.example:5000/gridiron@" + digest); err != nil {
		t.Fatalf("canonical Harbor image rejected: %v", err)
	}
	if err := validateImage(strings.Repeat("a", 247) + "@" + digest); err != nil {
		t.Fatalf("247-character default-library repository rejected: %v", err)
	}
	if err := validateImage(strings.Repeat("a", 248) + "@" + digest); err == nil {
		t.Fatal("248-character default-library repository accepted")
	}
	if err := validateImage(strings.Repeat("a", 64) + ".example@" + digest); err != nil {
		t.Fatalf("dotted single-component default repository rejected: %v", err)
	}
	if err := validateImage("harbor:5000@" + digest); err == nil {
		t.Fatal("registry port without repository path accepted")
	}
	if err := validateImage("harbor.example/" + strings.Repeat("a", 255) + "@" + digest); err != nil {
		t.Fatalf("255-character repository path rejected: %v", err)
	}
	if err := validateImage("harbor.example/" + strings.Repeat("a", 256) + "@" + digest); err == nil {
		t.Fatal("256-character repository path accepted")
	}
	if err := validateImage(strings.Repeat("a", 64) + ".example/gridiron@" + digest); err == nil {
		t.Fatal("overlong dotted registry label accepted")
	}
}

func TestCommissionerHQTopologyValidation(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Fleet)
		want   string
	}{
		{"zero participants zero hosts is valid", func(f *Fleet) {
			f.Instances[0].CommissionerHQ = nil
		}, ""},
		{"participant requires one host", func(f *Fleet) {
			f.Instances[0].CommissionerHQ.Host = false
		}, "exactly one host"},
		{"duplicate league id", func(f *Fleet) {
			f.Instances = append(f.Instances, instance("bravo", hq("alpha-league", 1, "blue", "hq-bravo", false)))
		}, "duplicate commissioner_hq league_id"},
		{"duplicate order", func(f *Fleet) {
			f.Instances = append(f.Instances, instance("bravo", hq("bravo-league", 0, "blue", "hq-bravo", false)))
		}, "duplicate commissioner_hq order"},
		{"duplicate key id", func(f *Fleet) {
			f.Instances = append(f.Instances, instance("bravo", hq("bravo-league", 1, "blue", "hq-alpha", false)))
		}, "duplicate commissioner_hq key_id"},
		{"negative order", func(f *Fleet) {
			f.Instances[0].CommissionerHQ.Order = -1
		}, "nonnegative"},
		{"unsafe accent", func(f *Fleet) {
			f.Instances[0].CommissionerHQ.Accent = "Blue Accent"
		}, "safe token"},
		{"unsafe key id", func(f *Fleet) {
			f.Instances[0].CommissionerHQ.KeyID = "HQ KEY"
		}, "safe token"},
		{"two hosts", func(f *Fleet) {
			f.Instances = append(f.Instances, instance("bravo", hq("bravo-league", 1, "blue", "hq-bravo", true)))
		}, "exactly one host"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeLeague(t, dir, "league.json")
			fleet := testFleet()
			tc.mutate(&fleet)
			path := writeFleet(t, dir, fleet)
			_, _, err := Load(path)
			if tc.want == "" {
				if err != nil {
					t.Fatalf("Load = %v, want valid", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Load = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestLoadRequiresCommissionerHQObjectFields(t *testing.T) {
	dir := t.TempDir()
	writeLeague(t, dir, "league.json")
	base := `{"version":2,"image":"` + testImage + `","statrelay_origin":"http://relay.test","ingress_class":"traefik","certificate_issuer":"issuer","instances":[{"id":"alpha","namespace":"alpha","resource_prefix":"alpha-app","public_origin":"https://alpha.example.test","league_config_path":"league.json","pvc_storage":"1Gi","commissioner_hq":{`
	for _, field := range []string{"league_id", "order", "accent", "key_id", "host"} {
		raw := base
		for _, candidate := range []string{"league_id", "order", "accent", "key_id", "host"} {
			if candidate == field {
				continue
			}
			switch candidate {
			case "league_id":
				raw += `"league_id":"alpha-league",`
			case "order":
				raw += `"order":0,`
			case "accent":
				raw += `"accent":"cyan",`
			case "key_id":
				raw += `"key_id":"hq-alpha",`
			case "host":
				raw += `"host":true,`
			}
		}
		raw = strings.TrimSuffix(raw, ",") + "}}]}"
		path := writeFleet(t, dir, Fleet{}, raw)
		if _, _, err := Load(path); err == nil || !strings.Contains(err.Error(), "commissioner_hq."+field) {
			t.Fatalf("missing %s error = %v", field, err)
		}
	}
}

func TestLoadUsesExactLeagueBytesAndRejectsNonRegularSources(t *testing.T) {
	dir := t.TempDir()
	leaguePath := writeLeague(t, dir, "league.json")
	original, err := os.ReadFile(leaguePath)
	if err != nil {
		t.Fatal(err)
	}
	fleetPath := writeFleet(t, dir, testFleet())
	loaded, _, err := Load(fleetPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Resolved) != 1 || !reflect.DeepEqual(loaded.Resolved[0].SourceJSON, original) {
		t.Fatal("resolved source bytes differ from the one file snapshot")
	}
	bundle, err := Compile(fleetPath)
	if err != nil {
		t.Fatal(err)
	}
	configMap := findFile(t, bundle, "instances/alpha/league-config.yaml")
	if !strings.Contains(configMap, "Test League") {
		t.Fatal("league ConfigMap does not contain the exact validated source")
	}
	fifoDir := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(fifoDir, "league.fifo"), 0o600); err != nil {
		t.Fatal(err)
	}
	fifoFleet := testFleet()
	fifoFleet.Instances[0].LeagueConfigPath = "league.fifo"
	fifoPath := writeFleet(t, fifoDir, fifoFleet)
	if _, _, err := Load(fifoPath); err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("FIFO Load error = %v, want regular-file rejection", err)
	}
}

func TestLoadPreflightsAllLeaguesAndAttributesWarnings(t *testing.T) {
	dir := t.TempDir()
	writeLeague(t, dir, "good.json")
	if err := os.WriteFile(filepath.Join(dir, "bad.json"), []byte(`{"version":1,"league":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	fleet := testFleet()
	fleet.Instances[0].LeagueConfigPath = "good.json"
	fleet.Instances = append(fleet.Instances, func() Instance {
		candidate := instance("broken", nil)
		candidate.LeagueConfigPath = "bad.json"
		return candidate
	}())
	path := writeFleet(t, dir, fleet)
	if _, _, err := Load(path); err == nil || !strings.Contains(err.Error(), `instance "broken"`) || !strings.Contains(err.Error(), `bad.json`) {
		t.Fatalf("Load error = %v, want instance and path", err)
	}

	var object map[string]any
	if err := json.Unmarshal([]byte(testLeagueJSON), &object); err != nil {
		t.Fatal(err)
	}
	teams := object["teams"].([]any)
	for i := 9; i <= 14; i++ {
		teams = append(teams, map[string]any{"id": fmt.Sprintf("team-%d", i), "name": fmt.Sprintf("Extra %d", i), "abbreviation": fmt.Sprintf("X%d", i), "division": "West", "tone": ""})
	}
	object["teams"] = teams
	warningBytes, _ := json.Marshal(object)
	if err := os.WriteFile(filepath.Join(dir, "warning.json"), warningBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	fleet = testFleet()
	fleet.Instances[0].LeagueConfigPath = "warning.json"
	path = writeFleet(t, dir, fleet)
	loaded, warnings, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 1 || warnings[0].InstanceID != "alpha" || warnings[0].Path != "warning.json" || warnings[0].Message == "" {
		t.Fatalf("warnings = %#v", warnings)
	}
	if len(loaded.Resolved) != 1 || len(loaded.Resolved[0].Warnings) != 1 {
		t.Fatalf("resolved warnings = %#v", loaded.Resolved)
	}
}

func TestExplicitLeaguePathIgnoresEnvironmentAndCWD(t *testing.T) {
	dir := t.TempDir()
	writeLeague(t, dir, "league.json")
	other := t.TempDir()
	writeLeague(t, other, "ambient.json")
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(other); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
	t.Setenv("LEAGUE_FILE", filepath.Join(other, "ambient.json"))
	t.Setenv("GOSX_APP_ROOT", other)
	t.Setenv("DATA_FILE", filepath.Join(other, "ambient-state.json"))
	fleet := testFleet()
	path := writeFleet(t, dir, fleet)
	loaded, _, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(loaded.Resolved[0].Path, filepath.Join(string(filepath.Separator), "league.json")) {
		t.Fatalf("resolved path = %q", loaded.Resolved[0].Path)
	}
}

func TestCompileZeroHQAndParticipantSeparation(t *testing.T) {
	dir := t.TempDir()
	writeLeague(t, dir, "league.json")
	fleet := testFleet()
	fleet.Instances = []Instance{
		instance("charlie", nil),
		instance("alpha", hq("alpha-league", 20, "cyan", "hq-alpha", true)),
		instance("bravo", hq("bravo-league", 10, "blue", "hq-bravo", false)),
	}
	path := writeFleet(t, dir, fleet)
	bundle, err := Compile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(bundle.Files), 9*3+2*2+2+1; got != want {
		t.Fatalf("file count = %d, want %d", got, want)
	}
	if got, want := len(bundle.Instances), 3; got != want {
		t.Fatalf("derived instances = %d, want %d", got, want)
	}
	nonparticipantDeployment := findFile(t, bundle, "instances/charlie/deployment.yaml")
	for _, forbidden := range []string{"8091", "COMMISSIONER_HQ_", "hq-registry", "provider"} {
		if strings.Contains(nonparticipantDeployment, forbidden) {
			t.Fatalf("nonparticipant deployment contains %q: %s", forbidden, nonparticipantDeployment)
		}
	}
	for _, file := range bundle.Files {
		if strings.HasPrefix(file.Path, "instances/charlie/") &&
			(strings.Contains(file.Path, "provider") || strings.Contains(file.Path, "network-policy") || strings.Contains(file.Path, "hq-")) {
			t.Fatalf("nonparticipant provider file %s", file.Path)
		}
	}
	for _, id := range []string{"alpha", "bravo"} {
		deployment := findFile(t, bundle, "instances/"+id+"/deployment.yaml")
		for _, required := range []string{"containerPort: 8091", "COMMISSIONER_INSTANCE_ID", "COMMISSIONER_HQ_LEAGUE_ID", "COMMISSIONER_HQ_PROVIDER_KEY_ID", "COMMISSIONER_HQ_PROVIDER_ADDR", "APP_IMAGE_DIGEST"} {
			if !strings.Contains(deployment, required) {
				t.Fatalf("%s deployment missing %q", id, required)
			}
		}
		providerService := findFile(t, bundle, "instances/"+id+"/hq-provider-service.yaml")
		if !strings.Contains(providerService, "port: 8091") || strings.Contains(providerService, "port: 8080") {
			t.Fatalf("%s provider Service ports invalid: %s", id, providerService)
		}
		policy := findFile(t, bundle, "instances/"+id+"/network-policy.yaml")
		if !strings.Contains(policy, "kubernetes.io/metadata.name") || !strings.Contains(policy, `kubernetes.io/metadata.name: "alpha"`) || !strings.Contains(policy, `app: "alpha-app"`) {
			t.Fatalf("%s policy does not restrict to exact host labels: %s", id, policy)
		}
		if strings.Contains(policy, "ports:\n        - name:") || !strings.Contains(policy, "- port: 8080\n          protocol: TCP") || !strings.Contains(policy, "- port: 8091\n          protocol: TCP") {
			t.Fatalf("%s policy has invalid or incomplete NetworkPolicyPort fields: %s", id, policy)
		}
		secret := findFile(t, bundle, "instances/"+id+"/secret.example.yaml")
		if !strings.Contains(secret, "COMMISSIONER_HQ_PROVIDER_SECRET: \"REPLACE_ME\"") || strings.Contains(secret, "COMMISSIONER_HQ_TOKEN") {
			t.Fatalf("%s provider Secret contract invalid: %s", id, secret)
		}
	}
	publicService := findFile(t, bundle, "instances/alpha/service.yaml")
	if !strings.Contains(publicService, "port: 80") || !strings.Contains(publicService, "targetPort: http") || strings.Contains(publicService, "8091") {
		t.Fatalf("public Service exposes unexpected port: %s", publicService)
	}
	for _, id := range []string{"alpha", "bravo", "charlie"} {
		for _, suffix := range []string{"ingress.yaml", "http-redirect.yaml"} {
			if strings.Contains(findFile(t, bundle, "instances/"+id+"/"+suffix), "8091") {
				t.Fatalf("public ingress %s exposes 8091", id)
			}
		}
	}
	hostDeployment := findFile(t, bundle, "instances/alpha/deployment.yaml")
	for _, required := range []string{"COMMISSIONER_HQ_V1_REGISTRY_FILE", "/etc/gridiron-hq/registry.json", "subPath: registry.json", "readOnly: true", "alpha-app-hq-v1-client-secrets"} {
		if !strings.Contains(hostDeployment, required) {
			t.Fatalf("host deployment missing %q", required)
		}
	}
	registry := findFile(t, bundle, "instances/alpha/hq-registry.yaml")
	if strings.Contains(registry, "REPLACE_ME") || strings.Contains(registry, "provider_secret") || strings.Contains(registry, "secret value") {
		t.Fatalf("registry leaked secret material: %s", registry)
	}
	clientSecret := findFile(t, bundle, "instances/alpha/hq-client-secret.example.yaml")
	if !strings.Contains(clientSecret, "COMMISSIONER_HQ_V1_SECRET_BRAVO: \"REPLACE_ME\"") || !strings.Contains(clientSecret, "COMMISSIONER_HQ_V1_SECRET_ALPHA: \"REPLACE_ME\"") {
		t.Fatalf("host client Secret lacks per-participant placeholders: %s", clientSecret)
	}
	if strings.Contains(clientSecret, strings.Repeat("x", 32)) {
		t.Fatal("client Secret example accidentally contains an accepted-length credential")
	}
	registryJSON := strings.TrimSpace(registry[strings.Index(registry, "{"):])
	registryPath := filepath.Join(t.TempDir(), "registry.json")
	if err := os.WriteFile(registryPath, []byte(registryJSON+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(clientSecretEnv("alpha"), "")
	t.Setenv(clientSecretEnv("bravo"), "")
	decoded, err := v1fleet.Load(registryPath)
	if err != nil || len(decoded.Connections) != 2 {
		t.Fatalf("generated registry does not satisfy v1fleet decoder: %#v, %v", decoded, err)
	}
	for _, connection := range decoded.Connections {
		if !connection.Misconfigured {
			t.Fatalf("example credential for %s did not fail closed", connection.Key)
		}
	}

	paths := make([]string, len(bundle.Files))
	for index, file := range bundle.Files {
		paths[index] = file.Path
		if filepath.IsAbs(file.Path) || strings.Contains(file.Path, "..") || strings.Contains(file.Path, "\\") {
			t.Fatalf("unsafe generated path %q", file.Path)
		}
		if !strings.HasSuffix(string(file.Data), "\n") {
			t.Fatalf("%s lacks final newline", file.Path)
		}
		if strings.Contains(string(file.Data), "TANK01_API_KEY") || strings.Contains(string(file.Data), "manager@example") || strings.Contains(string(file.Data), "actual-token") {
			t.Fatalf("sensitive output in %s", file.Path)
		}
	}
	if !sort.StringsAreSorted(paths) {
		t.Fatal("generated paths are not sorted")
	}

	deployment := findFile(t, bundle, "instances/alpha/deployment.yaml")
	for _, needle := range []string{"replicas: 1", "type: Recreate", "image: \"" + testImage + "\"", "imagePullPolicy: IfNotPresent", "runAsUser: 65532", "runAsGroup: 65532", "fsGroup: 65532", "allowPrivilegeEscalation: false", "readOnlyRootFilesystem: true", "- ALL", "APP_ENV", "DEMO_MODE", "PORT", "LEAGUE_FILE", "TANK01_BASE_URL", "/api/health", "/api/live", "cpu: 100m", "memory: 128Mi", "cpu: 500m", "memory: 512Mi"} {
		if !strings.Contains(deployment, needle) {
			t.Fatalf("deployment missing %q", needle)
		}
	}
	ingress := findFile(t, bundle, "instances/alpha/ingress.yaml")
	for _, needle := range []string{"cert-manager.io/cluster-issuer", "cloudflare-issuer", "ingressClassName", "traefik", "router.tls", "alpha.example.test", "alpha-app-tls"} {
		if !strings.Contains(ingress, needle) {
			t.Fatalf("ingress missing %q", needle)
		}
	}
	redirect := findFile(t, bundle, "instances/alpha/http-redirect.yaml")
	for _, needle := range []string{"redirectScheme", "scheme: https", "permanent: true", "router.entrypoints", "web"} {
		if !strings.Contains(redirect, needle) {
			t.Fatalf("redirect missing %q", needle)
		}
	}
	security := findFile(t, bundle, "instances/alpha/security-headers.yaml")
	for _, needle := range []string{"stsSeconds: 31536000", "stsIncludeSubdomains: false", "stsPreload: false", "forceSTSHeader: true", "contentTypeNosniff: true", "frameDeny: true", "strict-origin-when-cross-origin", "Permissions-Policy"} {
		if !strings.Contains(security, needle) {
			t.Fatalf("security middleware missing %q", needle)
		}
	}
	checklist := findFile(t, bundle, "operator-checklist.md")
	for _, needle := range []string{"First install generation", "Existing SK-first canary release", "DNS, OAuth registration, Secret values, and kubectl apply are operator actions", "https://alpha.example.test/auth/google/callback", "node-local", "reclaim policy", "CSP remains application-owned"} {
		if !strings.Contains(checklist, needle) {
			t.Fatalf("checklist missing %q", needle)
		}
	}
}

func TestCompileDeterministicAcrossInputOrderAndSixtyFourParticipants(t *testing.T) {
	dir := t.TempDir()
	writeLeague(t, dir, "league.json")
	fleet := testFleet()
	fleet.Instances = make([]Instance, 0, 64)
	for i := 0; i < 64; i++ {
		id := fmt.Sprintf("hq-%02d", i)
		fleet.Instances = append(fleet.Instances, instance(id, hq("league-"+id, i, "cyan", "key-"+id, i == 0)))
	}
	pathA := writeFleet(t, dir, fleet)
	bundleA, err := Compile(pathA)
	if err != nil {
		t.Fatal(err)
	}
	reversed := append([]Instance(nil), fleet.Instances...)
	sort.Slice(reversed, func(i, j int) bool { return reversed[i].ID > reversed[j].ID })
	fleet.Instances = reversed
	pathB := filepath.Join(dir, "fleet-reversed.json")
	raw, _ := json.MarshalIndent(fleet, "", "  ")
	if err := os.WriteFile(pathB, append(raw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	bundleB, err := Compile(pathB)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(bundleA.Files, bundleB.Files) {
		t.Fatal("generated files depend on input instance order")
	}
	if got, want := len(bundleA.Files), 9*64+2*64+2+1; got != want {
		t.Fatalf("64-participant file count = %d, want %d", got, want)
	}
	for _, derived := range bundleA.Instances {
		if derived.Spec.CommissionerHQ == nil || derived.HQProviderOrigin == "" || derived.ImageDigest == "" {
			t.Fatalf("derived HQ/image identity incomplete: %#v", derived)
		}
	}
}

func TestCommissionerHQRejectsSixtyFiveParticipants(t *testing.T) {
	dir := t.TempDir()
	writeLeague(t, dir, "league.json")
	fleet := testFleet()
	fleet.Instances = make([]Instance, 0, 65)
	for i := 0; i < 65; i++ {
		id := fmt.Sprintf("hq-%02d", i)
		fleet.Instances = append(fleet.Instances, instance(id, hq("league-"+id, i, "cyan", "key-"+id, i == 0)))
	}
	path := writeFleet(t, dir, fleet)
	if _, _, err := Load(path); err == nil || !strings.Contains(err.Error(), "must not exceed 64") {
		t.Fatalf("Load error = %v, want 64-participant ceiling", err)
	}
}

func TestImageAndQuantityValidation(t *testing.T) {
	if err := validateImage(testImage); err != nil {
		t.Fatal(err)
	}
	for _, image := range []string{"harbor.example/gridiron:latest", "Harbor.example/gridiron@sha256:" + strings.Repeat("0", 64), "harbor.example/gridiron@sha256:bad"} {
		if err := validateImage(image); err == nil {
			t.Fatalf("validateImage(%q) succeeded", image)
		}
	}
	for _, value := range []string{"1Gi", "512Mi", "20Gi"} {
		if err := validateQuantity(value); err != nil {
			t.Fatalf("validateQuantity(%q) = %v", value, err)
		}
	}
	for _, value := range []string{"0Gi", "1.5Gi", "1GG", "1GiGi", "512MiMi", "1G", "1"} {
		if err := validateQuantity(value); err == nil {
			t.Fatalf("validateQuantity(%q) succeeded", value)
		}
	}
}
