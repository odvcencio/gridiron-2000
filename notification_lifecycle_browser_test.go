package main

import (
	"bufio"
	"context"
	"encoding/json"
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

	"gridiron-2000/internal/league"
	"gridiron-2000/internal/notify"
	"gridiron-2000/internal/sim/draft"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"
)

const (
	notificationLifecycleEvaluatePath = "/__test/notifications/evaluate"
	notificationLifecycleStartPath    = "/__test/notifications/start"
	notificationLifecycleCapturePath  = "/__test/notifications/capture"
	notificationLifecyclePublicURL    = "https://league.example.test"
	notificationLifecycleSeasonStart  = "2026-09-10T17:00:00Z"
)

var notificationLifecycleNow = time.Date(2026, 9, 10, 16, 0, 0, 0, time.UTC)

type notificationLifecycleCapture struct {
	mu       sync.Mutex
	messages []notify.Message
}

func (c *notificationLifecycleCapture) add(message notify.Message) {
	c.mu.Lock()
	c.messages = append(c.messages, message)
	c.mu.Unlock()
}

func (c *notificationLifecycleCapture) snapshot() []notify.Message {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]notify.Message(nil), c.messages...)
}

type notificationLifecycleRuntime struct {
	service  *league.Service
	queue    *notify.Queue
	capture  *notificationLifecycleCapture
	ctx      context.Context
	workerMu sync.Mutex
	started  bool
}

type notificationLifecycleMessage struct {
	Key      string `json:"key"`
	Category string `json:"category"`
	To       string `json:"to"`
	Subject  string `json:"subject"`
	Text     string `json:"text"`
	HTML     string `json:"html"`
}

type notificationLifecycleResponse struct {
	Depth     int                            `json:"depth"`
	Delivered int                            `json:"delivered"`
	Started   bool                           `json:"started"`
	Messages  []notificationLifecycleMessage `json:"messages,omitempty"`
	Error     string                         `json:"error,omitempty"`
}

func notificationLifecycleWriteJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func (r *notificationLifecycleRuntime) response() notificationLifecycleResponse {
	r.workerMu.Lock()
	started := r.started
	r.workerMu.Unlock()
	return notificationLifecycleResponse{
		Depth:     r.queue.Depth(),
		Delivered: len(r.capture.snapshot()),
		Started:   started,
	}
}

func (r *notificationLifecycleRuntime) evaluate(w http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "notification evaluation requires POST", http.StatusMethodNotAllowed)
		return
	}
	if err := r.service.EvaluateNotificationsForTest(); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		notificationLifecycleWriteJSON(w, notificationLifecycleResponse{Error: err.Error()})
		return
	}
	notificationLifecycleWriteJSON(w, r.response())
}

func (r *notificationLifecycleRuntime) start(w http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "notification worker start requires POST", http.StatusMethodNotAllowed)
		return
	}
	r.workerMu.Lock()
	if !r.started {
		r.queue.Start(r.ctx)
		r.started = true
	}
	r.workerMu.Unlock()
	notificationLifecycleWriteJSON(w, r.response())
}

func (r *notificationLifecycleRuntime) captureMessages(w http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "notification capture requires GET", http.StatusMethodNotAllowed)
		return
	}
	messages := r.capture.snapshot()
	response := r.response()
	response.Messages = make([]notificationLifecycleMessage, 0, len(messages))
	for _, message := range messages {
		response.Messages = append(response.Messages, notificationLifecycleMessage{
			Key: message.Key, Category: message.Category, To: message.To,
			Subject: message.Subject, Text: message.Text, HTML: message.HTML,
		})
	}
	notificationLifecycleWriteJSON(w, response)
}

// startNotificationLifecycleChild is deliberately separate from the shared
// simulation child: this child wires a fake sender and the three loopback-only
// controls needed to observe queued versus delivered mail without adding a
// production route or transport endpoint.
func startNotificationLifecycleChild(t *testing.T, configPath, statePath string) *simChild {
	t.Helper()
	root := browserAppRoot(t)
	cmd := exec.Command(os.Args[0], "-test.run=^TestNotificationLifecycleChildProcess$")
	cmd.Env = simChildEnv(statePath, []string{
		"GOSX_APP_ROOT=" + root,
		"LEAGUE_FILE=" + configPath,
		"LEAGUE_URL=" + notificationLifecyclePublicURL,
		"SEASON_START_AT=" + notificationLifecycleSeasonStart,
	})
	stderr := &tailBuffer{limit: simChildStderrTail}
	cmd.Stderr = io.MultiWriter(os.Stderr, stderr)
	stdoutRead, stdoutWrite, err := os.Pipe()
	if err != nil {
		t.Fatalf("notification child stdout pipe: %v", err)
	}
	cmd.Stdout = stdoutWrite
	stdinRead, stdinWrite, err := os.Pipe()
	if err != nil {
		stdoutRead.Close()
		stdoutWrite.Close()
		t.Fatalf("notification child stdin pipe: %v", err)
	}
	cmd.Stdin = stdinRead
	if err := cmd.Start(); err != nil {
		stdoutRead.Close()
		stdoutWrite.Close()
		stdinRead.Close()
		stdinWrite.Close()
		t.Fatalf("start notification lifecycle child: %v", err)
	}
	stdoutWrite.Close()
	stdinRead.Close()
	child := &simChild{
		DataFile: statePath,
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
			fmt.Fprintln(os.Stderr, "notification lifecycle child: "+line)
		}
	}()
	select {
	case value := <-addr:
		if value == "" {
			t.Fatal("notification lifecycle child printed an empty address")
		}
		child.URL = "http://" + value
	case <-time.After(60 * time.Second):
		t.Fatal("notification lifecycle child did not print SIM_ADDR within 60s")
	}
	return child
}

func TestNotificationLifecycleChildProcess(t *testing.T) {
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
	fixed := notificationLifecycleNow
	capture := &notificationLifecycleCapture{}
	queue := notify.New(func(message notify.Message) error {
		capture.add(message)
		return nil
	}, func(string, ...any) {})
	service := league.Default()
	service.SetNotifier(queue, true)
	service.SetClockForTest(func() time.Time { return fixed })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runtime := &notificationLifecycleRuntime{service: service, queue: queue, capture: capture, ctx: ctx}
	rt.Start(ctx)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc(notificationLifecycleEvaluatePath, testRoutesLoopbackOnly(runtime.evaluate))
	mux.HandleFunc(notificationLifecycleStartPath, testRoutesLoopbackOnly(runtime.start))
	mux.HandleFunc(notificationLifecycleCapturePath, testRoutesLoopbackOnly(runtime.captureMessages))
	mux.Handle("/", app.Build())
	server := &http.Server{Handler: mux}
	go func() { _ = server.Serve(listener) }()
	fmt.Printf("SIM_ADDR=%s\n", listener.Addr().String())
	_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
	shutdown, cancelShutdown := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelShutdown()
	_ = server.Shutdown(shutdown)
}

func writeNotificationLifecycleFixture(t *testing.T) (configPath, statePath string) {
	t.Helper()
	dir := t.TempDir()
	configPath = filepath.Join(dir, "league.json")
	statePath = filepath.Join(dir, "league-state.json")
	configBytes, err := os.ReadFile(filepath.Join("config", "league.json.example"))
	if err != nil {
		t.Fatalf("read league config example: %v", err)
	}
	config := string(configBytes)
	if !strings.Contains(config, `"url": "http://localhost:8080"`) {
		t.Fatal("league config example no longer has its documented URL fixture")
	}
	config = strings.Replace(config, `"url": "http://localhost:8080"`, `"url": "`+notificationLifecyclePublicURL+`"`, 1)
	if !strings.Contains(config, `"season_start_at": "2099-01-08T00:00:00Z"`) {
		t.Fatal("league config example no longer has its documented season-start fixture")
	}
	config = strings.Replace(config, `"season_start_at": "2099-01-08T00:00:00Z"`, `"season_start_at": "`+notificationLifecycleSeasonStart+`"`, 1)
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatalf("write notification config: %v", err)
	}

	state := league.PersistedState{
		SchemaVersion: 11,
		Ready:         map[string]bool{},
		Picks:         []league.DraftPick{},
		Members: map[string]league.Member{
			"a@example.com": {TeamID: "team-1", Name: "Manager A", Email: "a@example.com"},
			"b@example.com": {TeamID: "team-2", Name: "Manager B", Email: "b@example.com"},
		},
		TeamNames:   map[string]string{"team-1": "Aces", "team-2": "Bruisers"},
		SentLog:     map[string]time.Time{},
		NotifyPrefs: map[string]map[string]bool{},
		Schedule: &league.SeasonSchedule{
			Season: 2026, Seed: 17, GeneratedAt: notificationLifecycleNow.Add(-24 * time.Hour), StartWeek: 1,
			Weeks: []league.ScheduleWeek{{Week: 1, Matchups: []league.LeagueMatchup{{
				ID: "2026-w01-team-1-team-2", Week: 1, HomeTeamID: "team-1", AwayTeamID: "team-2",
				HomeScore: 0, AwayScore: 0, Final: false,
			}}}},
		},
	}
	stateBytes, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatalf("marshal notification state: %v", err)
	}
	if err := os.WriteFile(statePath, stateBytes, 0o600); err != nil {
		t.Fatalf("write notification state: %v", err)
	}
	return configPath, statePath
}

func notificationLifecycleRequest(t *testing.T, child *simChild, path, method string) notificationLifecycleResponse {
	t.Helper()
	request, err := http.NewRequest(method, child.URL+path, nil)
	if err != nil {
		t.Fatalf("build notification %s request: %v", path, err)
	}
	client := &http.Client{Timeout: 5 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("call notification %s: %v", path, err)
	}
	defer response.Body.Close()
	var result notificationLifecycleResponse
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatalf("decode notification %s response (status %d): %v", path, response.StatusCode, err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("notification %s status = %d, error=%q", path, response.StatusCode, result.Error)
	}
	return result
}

func notificationLifecycleMessages(t *testing.T, child *simChild) []notificationLifecycleMessage {
	t.Helper()
	result := notificationLifecycleRequest(t, child, notificationLifecycleCapturePath, http.MethodGet)
	return result.Messages
}

func waitNotificationLifecycleMessages(t *testing.T, child *simChild, want int) []notificationLifecycleMessage {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	var messages []notificationLifecycleMessage
	for time.Now().Before(deadline) {
		messages = notificationLifecycleMessages(t, child)
		if len(messages) == want {
			return messages
		}
		time.Sleep(browserPollInterval)
	}
	t.Fatalf("notification capture did not converge to %d message(s): %+v", want, messages)
	return nil
}

func notificationLifecycleReadBody(t *testing.T, ctx context.Context) string {
	t.Helper()
	var body string
	if err := chromedp.Run(ctx, chromedp.Text("body", &body, chromedp.ByQuery)); err != nil {
		t.Fatalf("read notification page body: %v", err)
	}
	return body
}

func waitNotificationLifecycleBody(t *testing.T, ctx context.Context, want string) string {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	var body string
	for time.Now().Before(deadline) {
		body = notificationLifecycleReadBody(t, ctx)
		if strings.Contains(body, want) {
			return body
		}
		time.Sleep(browserPollInterval)
	}
	t.Fatalf("notification page never contained %q: %q", want, body)
	return body
}

func reloadNotificationLifecycleSettings(t *testing.T, ctx context.Context, child *simChild) {
	t.Helper()
	if err := chromedp.Run(ctx,
		chromedp.Navigate(child.URL+"/settings"),
		chromedp.WaitVisible("#notify-league_news", chromedp.ByQuery),
	); err != nil {
		t.Fatalf("reload notification settings: %v", err)
	}
}

func assertNotificationLifecycleSettings(t *testing.T, ctx context.Context, expectedState, ownEmail string) string {
	t.Helper()
	body := notificationLifecycleReadBody(t, ctx)
	if !strings.Contains(body, "EMAIL READY") {
		t.Fatalf("notification settings did not report EMAIL READY: %q", body)
	}
	if !strings.Contains(body, ownEmail) {
		t.Fatalf("notification settings omitted the signed-in identity %q: %q", ownEmail, body)
	}
	if strings.Contains(body, "b@example.com") && ownEmail != "b@example.com" {
		t.Fatalf("notification settings exposed another member identity: %q", body)
	}
	var field string
	if err := chromedp.Run(ctx, chromedp.Text("#notify-league_news", &field, chromedp.ByQuery)); err != nil {
		t.Fatalf("read league-news preference field: %v", err)
	}
	if !strings.Contains(strings.ToUpper(field), strings.ToUpper(expectedState)) {
		t.Fatalf("league-news preference state = %q, want %q", field, expectedState)
	}
	return body
}

func clickNotificationLifecyclePreference(t *testing.T, ctx context.Context, formNumber int) {
	t.Helper()
	selector := fmt.Sprintf("#notify-league_news form:nth-of-type(%d) button.notification-choice", formNumber)
	if err := chromedp.Run(ctx,
		chromedp.WaitVisible(selector, chromedp.ByQuery),
		chromedp.Click(selector, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("click notification preference form %d: %v", formNumber, err)
	}
}

func assertNotificationLifecycleMessage(t *testing.T, message notificationLifecycleMessage, email string) {
	t.Helper()
	wantKey := "kickoff:2026:" + email
	if message.Key != wantKey {
		t.Fatalf("notification key for %s = %q, want %q", email, message.Key, wantKey)
	}
	if message.Category != "league_news" {
		t.Fatalf("notification category = %q, want league_news", message.Category)
	}
	if message.To != email {
		t.Fatalf("notification recipient = %q, want %q", message.To, email)
	}
	if message.Subject != "SEASON KICKOFF — the league is about to open" {
		t.Fatalf("notification subject = %q, want kickoff subject", message.Subject)
	}
	if !strings.Contains(message.Text, notificationLifecyclePublicURL+"/matchups") && !strings.Contains(message.HTML, notificationLifecyclePublicURL+"/matchups") {
		t.Fatalf("notification omitted safe Matchups CTA: text=%q html=%q", message.Text, message.HTML)
	}
	if strings.Contains(message.Text, "a@example.com") || strings.Contains(message.HTML, "a@example.com") {
		if strings.Contains(message.Text, notificationLifecyclePublicURL+"/matchups?") || strings.Contains(message.HTML, notificationLifecyclePublicURL+"/matchups?") {
			t.Fatalf("notification CTA carried a private query: text=%q html=%q", message.Text, message.HTML)
		}
		t.Fatalf("notification for %s exposed A's private identity: text=%q html=%q", email, message.Text, message.HTML)
	}
}

func TestBrowserNotificationLifecycleQueuesPreferencesAndDeliversOnce(t *testing.T) {
	if testing.Short() {
		t.Skip("notification lifecycle scenario: skipped under -short")
	}
	chrome := chromePath(t)
	configPath, statePath := writeNotificationLifecycleFixture(t)
	child := startNotificationLifecycleChild(t, configPath, statePath)
	a := &draft.Bot{Email: "a@example.com", Name: "Manager A", TeamID: "team-1"}

	// A sees real delivery readiness and only A's own identity on the mobile
	// settings page. The managed form's OFF action must report its scoped
	// confirmation and persist through a fresh full-document read.
	phone := newBrowserContext(t, chrome)
	signInBrowserSeat(t, phone, child, a, "/settings", 390, 844)
	assertNotificationLifecycleSettings(t, phone, "Current state: ON", a.Email)
	if scrollWidth, innerWidth := documentOverflowPx(t, phone); scrollWidth > innerWidth {
		t.Fatalf("notification settings overflows at 390px: scrollWidth=%d innerWidth=%d", scrollWidth, innerWidth)
	}
	clickNotificationLifecyclePreference(t, phone, 2) // league_news Off
	waitNotificationLifecycleBody(t, phone, "League news is now OFF.")
	reloadNotificationLifecycleSettings(t, phone, child)
	assertNotificationLifecycleSettings(t, phone, "Current state: OFF", a.Email)

	// The queue is deliberately stopped during these two evaluations. One
	// real preference suppression leaves only B buffered; the repeat adds no
	// second ledger key and no second queued message.
	first := notificationLifecycleRequest(t, child, notificationLifecycleEvaluatePath, http.MethodPost)
	if first.Started || first.Depth != 1 || first.Delivered != 0 {
		t.Fatalf("first evaluation status = %+v, want stopped worker, depth=1 delivered=0", first)
	}
	repeat := notificationLifecycleRequest(t, child, notificationLifecycleEvaluatePath, http.MethodPost)
	if repeat.Depth != 1 || repeat.Delivered != 0 {
		t.Fatalf("repeat evaluation status = %+v, want unchanged depth=1 delivered=0", repeat)
	}
	started := notificationLifecycleRequest(t, child, notificationLifecycleStartPath, http.MethodPost)
	if !started.Started {
		t.Fatalf("notification worker did not report started: %+v", started)
	}
	messages := waitNotificationLifecycleMessages(t, child, 1)
	if len(messages) != 1 || messages[0].To != "b@example.com" {
		t.Fatalf("OFF preference did not suppress A while delivering B: %+v", messages)
	}
	assertNotificationLifecycleMessage(t, messages[0], "b@example.com")

	// Re-enable A through the same real managed settings form, then evaluate
	// once more. B's canonical kickoff key is already recorded; only A can be
	// captured, making the joined preference -> event -> delivery path visible.
	clickNotificationLifecyclePreference(t, phone, 1) // league_news On
	waitNotificationLifecycleBody(t, phone, "League news is now ON.")
	reloadNotificationLifecycleSettings(t, phone, child)
	assertNotificationLifecycleSettings(t, phone, "Current state: ON", a.Email)
	next := notificationLifecycleRequest(t, child, notificationLifecycleEvaluatePath, http.MethodPost)
	messages = waitNotificationLifecycleMessages(t, child, 2)
	if next.Delivered > 2 {
		t.Fatalf("A-on evaluation reported too many deliveries: %+v", next)
	}
	finalRepeat := notificationLifecycleRequest(t, child, notificationLifecycleEvaluatePath, http.MethodPost)
	messages = waitNotificationLifecycleMessages(t, child, 2)
	if finalRepeat.Delivered != 2 {
		t.Fatalf("repeat after A-on evaluation reported %d deliveries, want 2", finalRepeat.Delivered)
	}
	seen := map[string]string{}
	for _, message := range messages {
		if _, exists := seen[message.Key]; exists {
			t.Fatalf("duplicate captured notification key %q: %+v", message.Key, messages)
		}
		seen[message.Key] = message.To
		assertNotificationLifecycleMessage(t, message, message.To)
	}
	if len(seen) != 2 || seen["kickoff:2026:a@example.com"] != "a@example.com" || seen["kickoff:2026:b@example.com"] != "b@example.com" {
		t.Fatalf("captured keys/recipients = %+v, want one canonical key per member", seen)
	}

	// A second desktop context has script execution disabled before navigation,
	// so these are native browser POSTs. They still name the category in the
	// scoped confirmation and persist after a new document load.
	desktop := newBrowserContext(t, chrome)
	if err := chromedp.Run(desktop, emulation.SetScriptExecutionDisabled(true)); err != nil {
		t.Fatalf("disable JavaScript for native notification settings form: %v", err)
	}
	signInBrowserSeat(t, desktop, child, a, "/settings", 1440, 900)
	assertNotificationLifecycleSettings(t, desktop, "Current state: ON", a.Email)
	clickNotificationLifecyclePreference(t, desktop, 2)
	waitNotificationLifecycleBody(t, desktop, "League news is now OFF.")
	reloadNotificationLifecycleSettings(t, desktop, child)
	assertNotificationLifecycleSettings(t, desktop, "Current state: OFF", a.Email)
	clickNotificationLifecyclePreference(t, desktop, 1)
	waitNotificationLifecycleBody(t, desktop, "League news is now ON.")
	reloadNotificationLifecycleSettings(t, desktop, child)
	assertNotificationLifecycleSettings(t, desktop, "Current state: ON", a.Email)
}
