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

func testFleet() Fleet {
	return Fleet{
		Version:           SchemaVersion,
		Image:             testImage,
		StatrelayOrigin:   "http://statrelay.gridiron.svc.cluster.local",
		IngressClass:      "traefik",
		CertificateIssuer: "cloudflare-issuer",
		Instances: []Instance{{
			ID: "alpha", Namespace: "alpha", ResourcePrefix: "alpha-app",
			PublicOrigin: "https://alpha.example.test", LeagueConfigPath: "league.json",
			PVCStorage: "1Gi", HQParticipant: true,
		}},
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

func TestLoadStrictJSONAndRequiredFields(t *testing.T) {
	dir := t.TempDir()
	writeLeague(t, dir, "league.json")
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"unknown", `{"version":1,"image":"` + testImage + `","statrelay_origin":"http://relay.test","ingress_class":"traefik","certificate_issuer":"issuer","instances":[],"unexpected":true}`, "unknown field"},
		{"trailing", `{"version":1,"image":"` + testImage + `","statrelay_origin":"http://relay.test","ingress_class":"traefik","certificate_issuer":"issuer","instances":[]} {}`, "trailing"},
		{"duplicate", `{"version":1,"version":1,"image":"` + testImage + `","statrelay_origin":"http://relay.test","ingress_class":"traefik","certificate_issuer":"issuer","instances":[]}`, "duplicate object key"},
		{"version", `{"version":2,"image":"` + testImage + `","statrelay_origin":"http://relay.test","ingress_class":"traefik","certificate_issuer":"issuer","instances":[{}]}`, "exactly 1"},
		{"empty", `{"version":1,"image":"` + testImage + `","statrelay_origin":"http://relay.test","ingress_class":"traefik","certificate_issuer":"issuer","instances":[]}`, "at least one"},
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
		{"missing hq bool", nil, "hq_participant"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeLeague(t, dir, "league.json")
			fleet := testFleet()
			if tc.edit != nil {
				tc.edit(&fleet)
				path := writeFleet(t, dir, fleet)
				if _, _, err := Load(path); err == nil || !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tc.want)) {
					t.Fatalf("Load error = %v, want %q", err, tc.want)
				}
				return
			}
			raw := `{"version":1,"image":"` + testImage + `","statrelay_origin":"http://relay.test","ingress_class":"traefik","certificate_issuer":"issuer","instances":[{"id":"alpha","namespace":"alpha","resource_prefix":"alpha-app","public_origin":"https://alpha.example.test","league_config_path":"league.json","pvc_storage":"1Gi"}]}`
			path := writeFleet(t, dir, Fleet{}, raw)
			if _, _, err := Load(path); err == nil || !strings.Contains(err.Error(), tc.want) {
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
			f.Instances = append(f.Instances, f.Instances[0])
			f.Instances[1].PublicOrigin = "https://beta.example.test"
			f.Instances[1].Namespace = "beta"
			f.Instances[1].ResourcePrefix = "beta-app"
		}, "duplicate instance id"},
		{"namespace", func(f *Fleet) {
			f.Instances = append(f.Instances, Instance{ID: "beta", Namespace: "alpha", ResourcePrefix: "beta-app", PublicOrigin: "https://beta.example.test", LeagueConfigPath: "league.json", PVCStorage: "1Gi"})
		}, "collides"},
		{"resource prefix", func(f *Fleet) {
			f.Instances = append(f.Instances, Instance{ID: "beta", Namespace: "beta", ResourcePrefix: "alpha-app", PublicOrigin: "https://beta.example.test", LeagueConfigPath: "league.json", PVCStorage: "1Gi"})
		}, "collides"},
		{"public host", func(f *Fleet) {
			f.Instances = append(f.Instances, Instance{ID: "beta", Namespace: "beta", ResourcePrefix: "beta-app", PublicOrigin: "https://alpha.example.test", LeagueConfigPath: "league.json", PVCStorage: "1Gi"})
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

func TestNarrowPVCQuantityGrammarAcceptsOnlyPositiveMiOrGi(t *testing.T) {
	for _, value := range []string{"1Gi", "512Mi", "20Gi"} {
		if err := validateQuantity(value); err != nil {
			t.Errorf("validateQuantity(%q) = %v, want valid", value, err)
		}
	}
	for _, value := range []string{"0Gi", "1.5Gi", "1GG", "1GiGi", "512MiMi", "1G", "1"} {
		if err := validateQuantity(value); err == nil {
			t.Errorf("validateQuantity(%q) succeeded, want rejection", value)
		}
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

func TestLoadPreflightsAllLeaguesAndAttributesWarnings(t *testing.T) {
	dir := t.TempDir()
	writeLeague(t, dir, "good.json")
	if err := os.WriteFile(filepath.Join(dir, "bad.json"), []byte(`{"version":1,"league":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	fleet := testFleet()
	fleet.Instances = []Instance{
		fleet.Instances[0],
		{ID: "broken", Namespace: "broken", ResourcePrefix: "broken-app", PublicOrigin: "https://broken.example.test", LeagueConfigPath: "bad.json", PVCStorage: "1Gi"},
	}
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

func TestLoadUsesExactValidatedLeagueBytesAndRejectsNonRegularSources(t *testing.T) {
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
		t.Fatalf("resolved source bytes differ from the one file snapshot")
	}
	bundle, err := Compile(fleetPath)
	if err != nil {
		t.Fatal(err)
	}
	configMap := ""
	for _, file := range bundle.Files {
		if file.Path == "instances/alpha/league-config.yaml" {
			configMap = string(file.Data)
		}
	}
	if configMap == "" || !strings.Contains(configMap, "Test League") {
		t.Fatalf("league ConfigMap missing validated source")
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

func TestCompilePeersDeterministicBundleAndHardening(t *testing.T) {
	dir := t.TempDir()
	writeLeague(t, dir, "league.json")
	fleet := testFleet()
	fleet.Instances = []Instance{
		{ID: "charlie", Namespace: "charlie", ResourcePrefix: "charlie-app", PublicOrigin: "https://charlie.example.test", LeagueConfigPath: "league.json", PVCStorage: "2Gi", HQParticipant: true},
		{ID: "alpha", Namespace: "alpha", ResourcePrefix: "alpha-app", PublicOrigin: "https://alpha.example.test", LeagueConfigPath: "league.json", PVCStorage: "1Gi", HQParticipant: true},
		{ID: "bravo", Namespace: "bravo", ResourcePrefix: "bravo-app", PublicOrigin: "https://bravo.example.test", LeagueConfigPath: "league.json", PVCStorage: "3Gi", HQParticipant: false},
	}
	path := writeFleet(t, dir, fleet)
	bundleA, err := Compile(path)
	if err != nil {
		t.Fatal(err)
	}
	bundleB, err := Compile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(bundleA.Files, bundleB.Files) {
		t.Fatal("repeated compile produced different files")
	}
	paths := make([]string, len(bundleA.Files))
	for i, file := range bundleA.Files {
		paths[i] = file.Path
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
	if !sort.StringsAreSorted(paths) || len(paths) != 28 {
		t.Fatalf("paths sorted/count = %v/%d", sort.StringsAreSorted(paths), len(paths))
	}
	byID := map[string]DerivedInstance{}
	for _, instance := range bundleA.Instances {
		byID[instance.Spec.ID] = instance
	}
	wantAlphaPeers := "charlie=" + byID["charlie"].ServiceOrigin + "|https://charlie.example.test"
	if got := byID["alpha"].HQPeersValue; got != wantAlphaPeers {
		t.Fatalf("alpha peers = %q, want exactly %q", got, wantAlphaPeers)
	}
	if byID["bravo"].HQPeersValue != "" {
		t.Fatalf("nonparticipant peers = %q", byID["bravo"].HQPeersValue)
	}

	find := func(path string) string {
		for _, file := range bundleA.Files {
			if file.Path == path {
				return string(file.Data)
			}
		}
		t.Fatalf("missing %s", path)
		return ""
	}
	deployment := find("instances/alpha/deployment.yaml")
	for _, needle := range []string{"replicas: 1", "type: Recreate", "image: \"" + testImage + "\"", "imagePullPolicy: IfNotPresent", "runAsUser: 65532", "runAsGroup: 65532", "fsGroup: 65532", "allowPrivilegeEscalation: false", "readOnlyRootFilesystem: true", "- ALL", "APP_ENV", "DEMO_MODE", "PORT", "LEAGUE_FILE", "TANK01_BASE_URL", "/api/health", "/api/live", "cpu: 100m", "memory: 128Mi", "cpu: 500m", "memory: 512Mi"} {
		if !strings.Contains(deployment, needle) {
			t.Fatalf("deployment missing %q", needle)
		}
	}
	for _, needle := range []string{"initialDelaySeconds: 3", "periodSeconds: 10", "timeoutSeconds: 5", "initialDelaySeconds: 15", "periodSeconds: 30", "timeoutSeconds: 10"} {
		if !strings.Contains(deployment, needle) {
			t.Fatalf("deployment probe missing %q", needle)
		}
	}
	if !strings.Contains(deployment, "readinessProbe:\n            httpGet:\n              path: /api/health\n              port: http\n            initialDelaySeconds: 3\n            periodSeconds: 10\n            timeoutSeconds: 5") {
		t.Fatal("readiness probe does not match the hardened deployment contract")
	}
	if !strings.Contains(deployment, "livenessProbe:\n            httpGet:\n              path: /api/live\n              port: http\n            initialDelaySeconds: 15\n            periodSeconds: 30\n            timeoutSeconds: 10") {
		t.Fatal("liveness probe does not match the hardened deployment contract")
	}
	ingress := find("instances/alpha/ingress.yaml")
	for _, needle := range []string{"cert-manager.io/cluster-issuer", "cloudflare-issuer", "ingressClassName", "traefik", "router.tls", "alpha.example.test", "alpha-app-tls"} {
		if !strings.Contains(ingress, needle) {
			t.Fatalf("ingress missing %q", needle)
		}
	}
	redirect := find("instances/alpha/http-redirect.yaml")
	for _, needle := range []string{"redirectScheme", "scheme: https", "permanent: true", "router.entrypoints", "web"} {
		if !strings.Contains(redirect, needle) {
			t.Fatalf("redirect missing %q", needle)
		}
	}
	security := find("instances/alpha/security-headers.yaml")
	for _, needle := range []string{"stsSeconds: 31536000", "stsIncludeSubdomains: false", "stsPreload: false", "forceSTSHeader: true", "contentTypeNosniff: true", "frameDeny: true", "strict-origin-when-cross-origin", "Permissions-Policy"} {
		if !strings.Contains(security, needle) {
			t.Fatalf("security middleware missing %q", needle)
		}
	}
	secret := find("instances/alpha/secret.example.yaml")
	if !strings.Contains(secret, "GOOGLE_REDIRECT_URL: \"https://alpha.example.test/auth/google/callback\"") || !strings.Contains(secret, "REPLACE_ME") {
		t.Fatalf("secret callback/placeholders missing: %s", secret)
	}
	checklist := find("operator-checklist.md")
	for _, needle := range []string{"First install generation", "Existing SK-first canary release", "DNS, OAuth registration, Secret values, and kubectl apply are operator actions", "https://alpha.example.test/auth/google/callback", "node-local", "reclaim policy", "CSP remains application-owned"} {
		if !strings.Contains(checklist, needle) {
			t.Fatalf("checklist missing %q", needle)
		}
	}
}

func TestCompilePeerSetsScaleToThirtyThree(t *testing.T) {
	dir := t.TempDir()
	writeLeague(t, dir, "league.json")
	fleet := testFleet()
	fleet.Instances = make([]Instance, 0, 33)
	for i := 0; i < 33; i++ {
		id := fmt.Sprintf("hq-%02d", i)
		fleet.Instances = append(fleet.Instances, Instance{ID: id, Namespace: id, ResourcePrefix: id + "-app", PublicOrigin: "https://" + id + ".example.test", LeagueConfigPath: "league.json", PVCStorage: "1Gi", HQParticipant: true})
	}
	path := writeFleet(t, dir, fleet)
	bundle, err := Compile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(bundle.Instances) != 33 {
		t.Fatalf("instances = %d", len(bundle.Instances))
	}
	for _, instance := range bundle.Instances {
		if len(instance.HQPeers) != 32 {
			t.Fatalf("%s peer count = %d", instance.Spec.ID, len(instance.HQPeers))
		}
		seen := map[string]bool{}
		for _, peer := range instance.HQPeers {
			if peer.ID == instance.Spec.ID || seen[peer.ID] {
				t.Fatalf("%s self/duplicate peer %q", instance.Spec.ID, peer.ID)
			}
			seen[peer.ID] = true
		}
	}
}
