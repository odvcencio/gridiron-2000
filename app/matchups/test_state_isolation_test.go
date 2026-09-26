package matchups

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gridiron-2000/internal/league"
)

func TestMatchupsDefaultStateDoesNotWriteWorkingDirectory(t *testing.T) {
	if os.Getenv("MATCHUPS_ISOLATION_CHILD") == "1" {
		if err := league.Default().PersistenceError(); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join("data", "league.db")); !os.IsNotExist(err) {
			t.Fatalf("default league wrote SQLite state into the working directory: %v", err)
		}
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestMatchupsDefaultStateDoesNotWriteWorkingDirectory$")
	cmd.Dir = t.TempDir()
	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, "DATA_FILE=") && !strings.HasPrefix(value, "MATCHUPS_RENDER_FIXTURE=") && !strings.HasPrefix(value, "LEAGUE_FILE=") && !strings.HasPrefix(value, "GOSX_APP_ROOT=") {
			cmd.Env = append(cmd.Env, value)
		}
	}
	cmd.Env = append(cmd.Env, "MATCHUPS_ISOLATION_CHILD=1")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("isolated matchups test process: %v\n%s", err, output)
	}
}
