package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestSimChildProcess is the child entry point. The parent re-executes the
// test binary with GRIDIRON_SIM_CHILD=1; the child builds the real app,
// listens on a free loopback port, prints SIM_ADDR, and serves until stdin
// closes.
func TestSimChildProcess(t *testing.T) {
	if os.Getenv("GRIDIRON_SIM_CHILD") != "1" {
		t.Skip("child entry")
	}
	cfg, err := AppConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	app, rt, err := BuildApp(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()
	if rt.StopNotify != nil {
		defer rt.StopNotify()
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rt.Start(ctx)
	// Port 0 asks the kernel for a free loopback port, so two child
	// processes never collide and the parent never picks a port itself.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: app.Build()}
	go func() { _ = server.Serve(listener) }()
	fmt.Printf("SIM_ADDR=%s\n", listener.Addr().String())
	_, _ = bufio.NewReader(os.Stdin).ReadString('\n') // the parent closes stdin to stop us
	shutdown, cancelShutdown := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelShutdown()
	_ = server.Shutdown(shutdown)
}

// simChild is one running child server owned by the parent test.
type simChild struct {
	URL      string
	DataFile string
	cmd      *exec.Cmd
	stdin    *os.File
	stopOnce sync.Once
}

// simChildEnvKeys lists every variable the child must not inherit from the
// developer's shell. hermeticEnv clears the same set for an in-process
// build; a child process needs the same guarantee, so an exported
// TANK01_API_KEY cannot start a poller or a provider listener here either.
var simChildEnvKeys = []string{
	"TANK01_API_KEY",
	"TANK01_BASE_URL",
	"RESEND_API_KEY",
	"SMTP_HOST",
	"COMMISSIONER_HQ_LEAGUE_ID",
	"COMMISSIONER_HQ_PROVIDER_KEY_ID",
	"COMMISSIONER_HQ_PROVIDER_SECRET",
	"COMMISSIONER_HQ_PROVIDER_SECRET_FILE",
	"COMMISSIONER_HQ_PROVIDER_ADDR",
	"COMMISSIONER_HQ_V1_REGISTRY_FILE",
	"COMMISSIONER_HQ_PEERS",
	"COMMISSIONER_HQ_TOKEN",
}

// simChildBaseEnv is the child's complete harness configuration: harness
// auth on, the offline pool relabelled live so a draft can start, no
// upstream feed, no mail transport, and one known commissioner.
func simChildBaseEnv(dataFile string) []string {
	return []string{
		"GRIDIRON_SIM_CHILD=1",
		"GRIDIRON_TEST_AUTH=1",
		"GRIDIRON_TEST_POOL=offline-live",
		"DATA_FILE=" + dataFile,
		"DEMO_MODE=false",
		"APP_ENV=test",
		"GOOGLE_CLIENT_ID=",
		"LEAGUE_FILE=",
		"WIRE_ENABLED=false",
		"OPEN_STATS_ENABLED=false",
		"COMMISSIONER_EMAILS=commish@sim.test",
		"PICK_CLOCK=30",
		"SESSION_SECRET=sim-session-secret-0123456789abcdef0123456789",
	}
}

// simChildEnv builds the child environment: the parent's environment minus
// every key simChildEnvKeys names, then the harness settings, then the
// caller's extra "KEY=value" overrides last so a scenario can change one.
func simChildEnv(dataFile string, extraEnv []string) []string {
	drop := make(map[string]bool, len(simChildEnvKeys))
	for _, key := range simChildEnvKeys {
		drop[key] = true
	}
	env := make([]string, 0, len(os.Environ())+16)
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if drop[key] {
			continue
		}
		env = append(env, entry)
	}
	env = append(env, simChildBaseEnv(dataFile)...)
	return append(env, extraEnv...)
}

// startSimChild re-executes this test binary as a server process and waits
// for the address it prints. dataFile may be empty, which gives the child a
// fresh state file under the test's own temporary directory; a restart
// scenario passes the same path twice to reopen one league.
func startSimChild(t *testing.T, dataFile string, extraEnv ...string) *simChild {
	t.Helper()
	if dataFile == "" {
		dataFile = filepath.Join(t.TempDir(), "league-state.json")
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestSimChildProcess$")
	cmd.Env = simChildEnv(dataFile, extraEnv)
	cmd.Stderr = os.Stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("sim child stdout: %v", err)
	}
	// An os.Pipe (not cmd.StdinPipe) keeps the write end usable after Wait
	// returns and gives Stop one explicit close, so the child's blocking
	// read always ends.
	stdinRead, stdinWrite, err := os.Pipe()
	if err != nil {
		t.Fatalf("sim child stdin pipe: %v", err)
	}
	cmd.Stdin = stdinRead
	if err := cmd.Start(); err != nil {
		stdinRead.Close()
		stdinWrite.Close()
		t.Fatalf("start sim child: %v", err)
	}
	stdinRead.Close() // the child holds its own copy now
	child := &simChild{DataFile: dataFile, cmd: cmd, stdin: stdinWrite}
	t.Cleanup(child.Stop)
	addr := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			if value, found := strings.CutPrefix(line, "SIM_ADDR="); found {
				select {
				case addr <- strings.TrimSpace(value):
				default:
				}
				continue
			}
			fmt.Fprintln(os.Stderr, "sim child: "+line)
		}
		close(addr)
		_, _ = io.Copy(io.Discard, stdout)
	}()
	select {
	case value, ok := <-addr:
		if !ok || value == "" {
			t.Fatal("sim child exited before it printed SIM_ADDR")
		}
		child.URL = "http://" + value
	case <-time.After(60 * time.Second):
		t.Fatal("sim child did not print SIM_ADDR within 60s")
	}
	return child
}

// Stop ends the child politely, then forcibly. It writes the stop line,
// closes stdin so a child that is still reading unblocks, and waits up to
// 8 seconds before it kills the process. Stop never blocks forever and is
// safe to call more than once.
func (c *simChild) Stop() {
	c.stopOnce.Do(func() {
		if c.stdin != nil {
			_, _ = c.stdin.WriteString("stop\n")
			_ = c.stdin.Close()
		}
		done := make(chan error, 1)
		go func() { done <- c.cmd.Wait() }()
		select {
		case <-done:
		case <-time.After(8 * time.Second):
			_ = c.cmd.Process.Kill()
			<-done
		}
	})
}

// Get performs one loopback GET against the child and returns the status.
func (c *simChild) Get(t *testing.T, path string) int {
	t.Helper()
	response, err := http.Get(c.URL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, response.Body)
	return response.StatusCode
}

// TestSimChildBoots proves the parent can start a real server process and
// reach it over HTTP.
func TestSimChildBoots(t *testing.T) {
	child := startSimChild(t, "")
	if status := child.Get(t, "/api/live"); status != http.StatusOK {
		t.Fatalf("GET /api/live = %d, want 200", status)
	}
}
