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

// simChildStderrTail is how much of a child's standard error the parent
// keeps for a failure report.
const simChildStderrTail = 2 << 10

// tailBuffer keeps only the last limit bytes written to it. The parent
// mirrors a child's standard error to its own so a failure is visible
// live, and keeps this bounded copy so Stop can report the tail of it
// without holding a whole run's log in memory.
type tailBuffer struct {
	mu    sync.Mutex
	limit int
	data  []byte
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.data = append(b.data, p...)
	if len(b.data) > b.limit {
		b.data = append(b.data[:0], b.data[len(b.data)-b.limit:]...)
	}
	return len(p), nil
}

func (b *tailBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.data)
}

// simChild is one running child server owned by the parent test.
type simChild struct {
	URL      string
	DataFile string

	t        *testing.T
	cmd      *exec.Cmd
	stdin    *os.File
	stdout   *os.File // the parent's read end; the scanner goroutine owns it
	stderr   *tailBuffer
	scanDone chan struct{}
	stopOnce sync.Once
	killed   bool
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
	stderr := &tailBuffer{limit: simChildStderrTail}
	// Mirrored, not swallowed: a live run still shows the child's log lines
	// while Stop keeps the tail for a non-zero exit report.
	cmd.Stderr = io.MultiWriter(os.Stderr, stderr)
	// Plain os.Pipe ends, not cmd.StdoutPipe/StdinPipe: os/exec closes a
	// StdoutPipe during Wait, which races the scanner goroutine below, and
	// a StdinPipe's write end is closed by Wait too. Owning both ends here
	// keeps Stop's order explicit — signal, then wait, then join.
	stdoutRead, stdoutWrite, err := os.Pipe()
	if err != nil {
		t.Fatalf("sim child stdout pipe: %v", err)
	}
	cmd.Stdout = stdoutWrite
	stdinRead, stdinWrite, err := os.Pipe()
	if err != nil {
		stdoutRead.Close()
		stdoutWrite.Close()
		t.Fatalf("sim child stdin pipe: %v", err)
	}
	cmd.Stdin = stdinRead
	if err := cmd.Start(); err != nil {
		stdoutRead.Close()
		stdoutWrite.Close()
		stdinRead.Close()
		stdinWrite.Close()
		t.Fatalf("start sim child: %v", err)
	}
	// The child holds its own copies now. Closing the parent's write end is
	// what lets the scanner below reach EOF once the child exits.
	stdoutWrite.Close()
	stdinRead.Close()
	child := &simChild{
		DataFile: dataFile,
		t:        t,
		cmd:      cmd,
		stdin:    stdinWrite,
		stdout:   stdoutRead,
		stderr:   stderr,
		scanDone: make(chan struct{}),
	}
	t.Cleanup(child.Stop)
	addr := make(chan string, 1)
	go func() {
		defer close(child.scanDone)
		scanner := bufio.NewScanner(stdoutRead)
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
// closes stdin so a child that is still reading unblocks, waits up to 8
// seconds before it kills the process, then joins the scanner goroutine.
// A child that exited non-zero on its own gets one log line with the tail
// of its standard error; a child this function killed does not, because
// that exit status is the parent's own doing. Stop never blocks forever
// and is safe to call more than once.
func (c *simChild) Stop() {
	c.stopOnce.Do(func() {
		if c.stdin != nil {
			_, _ = c.stdin.WriteString("stop\n")
			_ = c.stdin.Close()
		}
		done := make(chan error, 1)
		go func() { done <- c.cmd.Wait() }()
		var waitErr error
		select {
		case waitErr = <-done:
		case <-time.After(8 * time.Second):
			c.killed = true
			_ = c.cmd.Process.Kill()
			waitErr = <-done
		}
		// The scanner owns the read end, so join it before closing.
		select {
		case <-c.scanDone:
		case <-time.After(2 * time.Second):
		}
		if c.stdout != nil {
			_ = c.stdout.Close()
		}
		if waitErr != nil && !c.killed && c.t != nil {
			c.t.Logf("sim child exited: %v\nstderr tail:\n%s", waitErr, c.stderr.String())
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
