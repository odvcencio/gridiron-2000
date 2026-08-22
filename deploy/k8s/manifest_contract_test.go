package k8s

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const sharedRelayURL = "http://statrelay.gridiron.svc.cluster.local"

var tankKeyField = regexp.MustCompile(`(?m)^\s*TANK01_API_KEY\s*:`)

// Every independently stateful league shares the fleet's statrelay cache and
// request budget. Keep this invariant discoverable: adding another instance
// automatically brings its deployment and Secret example under this test.
func TestLeagueInstancesUseSharedTankRelay(t *testing.T) {
	var deployments []string
	var leagueSecrets []string
	err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		switch entry.Name() {
		case "deployment.yaml":
			deployments = append(deployments, path)
		case "secret.example.yaml":
			leagueSecrets = append(leagueSecrets, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(deployments) == 0 || len(leagueSecrets) == 0 {
		t.Fatalf("discovered %d league deployments and %d league Secret examples", len(deployments), len(leagueSecrets))
	}

	for _, path := range deployments {
		body := readManifest(t, path)
		if !strings.Contains(body, "- name: TANK01_BASE_URL") || !strings.Contains(body, `value: "`+sharedRelayURL+`"`) {
			t.Errorf("%s must route Tank01 through %s", path, sharedRelayURL)
		}
	}
	for _, path := range leagueSecrets {
		if tankKeyField.MatchString(readManifest(t, path)) {
			t.Errorf("%s must not provision TANK01_API_KEY; statrelay owns it", path)
		}
	}

	relaySecret := readManifest(t, "statrelay-secret.example.yaml")
	if !tankKeyField.MatchString(relaySecret) {
		t.Error("statrelay-secret.example.yaml must provision the fleet's sole TANK01_API_KEY")
	}
}

func readManifest(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}
