package wire

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gridiron-2000/internal/openstats"
	signalwire "gridiron-2000/internal/wire"
	"m31labs.dev/gosx"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
)

// TestWirePageRendersSignalCardsWithRealData is the regression guard for
// the untyped-legacy retirement: SignalCard and WireEmptyState used to
// read dynamic map fields (props.category, props.wire_configured); they
// are now strict components whose props must resolve through a real
// render. It drives a real HTTP GET through the actual file router — the
// same route.AddDir mechanism main.go uses to mount every page —
// following app/matchups and app/join's harness, after submitting a real
// community sighting so data.signals is genuinely non-empty.
func TestWirePageRendersSignalCardsWithRealData(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestWirePageRendersSignalCardsWithRealDataFixtureProcess$")
	cmd.Env = append(os.Environ(),
		"WIRE_RENDER_FIXTURE=1",
		"DATA_FILE="+filepath.Join(t.TempDir(), "league-state.json"),
		"WIRE_ROOT="+t.TempDir(),
		"DEMO_MODE=true",
		"GOOGLE_CLIENT_ID=",
		"APP_ENV=",
		"LEAGUE_FILE=",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("wire real-data fixture process: %v\n%s", err, output)
	}
	body := string(output)
	for _, want := range []string{"wire-event", "League group chat", "DEGRADED"} {
		if !strings.Contains(body, want) {
			t.Fatalf("wire fixture missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, "NO SIGNALS YET") {
		t.Fatalf("wire fixture rendered the empty state: %s", body)
	}
}

func TestWirePageRendersSignalCardsWithRealDataFixtureProcess(t *testing.T) {
	if os.Getenv("WIRE_RENDER_FIXTURE") == "" {
		t.Skip("fixture helper")
	}
	t.Setenv("DATA_FILE", filepath.Join(t.TempDir(), "league-state.json"))
	t.Setenv("DEMO_MODE", "true")
	t.Setenv("GOOGLE_CLIENT_ID", "")
	// signalwire.ConfigFromEnv defaults WIRE_ROOT to the on-disk,
	// cwd-relative "data/signal-wire" — the same DATA_FILE-not-DATA_DIR
	// class of footgun this task's instructions warn about for the league
	// store. Scope it to this test's own temp dir so a real sighting
	// submission below never writes into (or reads stale dedup state
	// from) the shared local checkout.
	t.Setenv("WIRE_ROOT", t.TempDir())

	signals, err := signalwire.Default()
	if err != nil {
		t.Fatalf("signalwire.Default: %v", err)
	}
	if _, err := signals.SubmitSighting(signalwire.CommunitySubmission{
		ReporterID:   "demo-commissioner",
		ReporterName: "Demo commissioner",
		EvidenceType: "community",
		SourceName:   "League group chat",
		Summary:      "Starting RB1 looked limited in pregame warmups.",
	}); err != nil {
		t.Fatalf("seed sighting: %v", err)
	}

	router := route.NewRouter()
	router.SetLayout(func(ctx *route.RouteContext, body gosx.Node) gosx.Node {
		ctx.SetLanguage("en")
		return server.HTMLDocument(ctx.Document("Test", body))
	})
	// "." is this package's own directory (app/wire): AddDir treats it as
	// the route tree's root, so page.gsx here answers "/" — enough to
	// drive one real render without pulling every other page's file
	// modules (and their own env/store needs) into this test.
	if err := router.AddDir(".", route.FileRoutesOptions{}); err != nil {
		t.Fatalf("AddDir: %v", err)
	}
	handler, err := router.BuildChecked()
	if err != nil {
		t.Fatalf("BuildChecked: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET / (wire page) = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, "WENT DARK") || strings.Contains(body, "render strict component") {
		t.Fatalf("wire page rendered the error page instead of signal cards: %s", body)
	}
	if !strings.Contains(body, "wire-event") {
		t.Fatalf("expected at least one rendered wire-event card in the response, got: %s", body)
	}
	if !strings.Contains(body, "League group chat") {
		t.Fatalf("expected the seeded sighting's source to render, got: %s", body)
	}
	if strings.Contains(body, "NO SIGNALS YET") {
		t.Fatalf("expected the seeded sighting to render, not the empty state: %s", body)
	}
	if !strings.Contains(body, "DEGRADED") {
		t.Fatalf("expected never-checked default feeds to render as DEGRADED: %s", body)
	}
	recent := signals.Recent(50, "")
	if len(recent) != 1 {
		t.Fatalf("seeded wire has %d recent signals, want exactly one for parity", len(recent))
	}
	card := wireSignalCard(recent[0], signals.Status(), time.Now().UTC())
	wantCard := renderWireComponent(t, "SignalCard", card)
	if !strings.Contains(body, wantCard) {
		t.Fatalf("full page did not contain the exact typed SignalCard render %q: %s", wantCard, body)
	}
	fragment, err := FeedFragmentWithError(httptest.NewRequest(http.MethodGet, "/wire/fragment", nil), signals)
	if err != nil {
		t.Fatalf("FeedFragmentWithError: %v", err)
	}
	if got := gosx.RenderHTML(fragment); got != wantCard {
		t.Fatalf("live fragment = %q, want byte-identical typed SignalCard %q", got, wantCard)
	}
	for _, token := range []string{
		signalwire.ModeAwaitingSources,
		signalwire.ModeSyndicationReady,
		signalwire.ModeResolvingSources,
		signalwire.ModeConnecting,
		signalwire.ModeReconnecting,
		signalwire.ModeSourceError,
	} {
		if strings.Contains(body, token) {
			t.Fatalf("render leaked machine mode token %q: %s", token, body)
		}
	}
	_, _ = os.Stdout.WriteString(body)
}

func TestWireModeLabelsCoverServiceVocabulary(t *testing.T) {
	cases := map[string]string{
		signalwire.ModeDisabled:         "OFF",
		signalwire.ModeAwaitingSources:  "OFF",
		signalwire.ModeReady:            "READY",
		signalwire.ModeSyndicationReady: "READY",
		signalwire.ModeSyndicating:      "LIVE",
		signalwire.ModeResolvingSources: "STARTING",
		signalwire.ModeConnecting:       "STARTING",
		signalwire.ModeStreaming:        "LIVE",
		signalwire.ModeReconnecting:     "CATCHING UP",
		signalwire.ModeSourceError:      "QUIET",
		signalwire.ModeStopped:          "OFF",
	}
	for mode, want := range cases {
		if got := wireModeLabel(mode); got != want {
			t.Errorf("wireModeLabel(%q) = %q, want %q", mode, got, want)
		}
	}
	if got := wireModeLabel("future_runtime_mode"); got != "UNAVAILABLE" {
		t.Fatalf("unknown wire mode = %q, want UNAVAILABLE", got)
	}
	unknown := signalwire.Status{
		Mode:  "future_runtime_mode",
		Feeds: []signalwire.FeedStatus{{Name: "Broken", State: "error", LastChecked: time.Now(), LastError: "timeout"}},
	}
	if got := wirePresentationLabel(unknown, time.Now()); got != "UNAVAILABLE" {
		t.Fatalf("unknown wire mode with feed failure = %q, want UNAVAILABLE", got)
	}
}

func TestWirePageAndPulseShareAggregateModeVocabulary(t *testing.T) {
	signals, err := signalwire.NewService(signalwire.Config{
		Root: t.TempDir(), Enabled: true, FeedsEnabled: false,
		DIDs: []string{"did:plc:reporter"}, JetstreamURL: "wss://stream.example.test/subscribe",
	})
	if err != nil {
		t.Fatal(err)
	}
	stats, err := openstats.NewService(openstats.Config{Root: t.TempDir(), Season: 2026, Enabled: false})
	if err != nil {
		t.Fatal(err)
	}
	data := wirePageData(httptest.NewRequest(http.MethodGet, "/wire", nil), signals, stats)
	pulse := PulseData(signals)
	if got, want := data["wire_mode"], pulse["mode"]; got != want {
		t.Fatalf("initial page mode = %#v, pulse mode = %#v", got, want)
	}
}

func TestWirePresentationMarksPartialFeedOutageAndRetainsSignals(t *testing.T) {
	now := time.Date(2026, 8, 23, 20, 0, 0, 0, time.UTC)
	status := signalwire.Status{
		Configured:        true,
		BlueskyConfigured: true,
		Mode:              signalwire.ModeStreaming,
		Feeds: []signalwire.FeedStatus{
			{Name: "Healthy Publisher", State: "ready", LastChecked: now, LastPublished: now.Add(-time.Minute)},
			{Name: "Broken Publisher", State: "error", LastChecked: now, LastPublished: now.Add(-time.Hour), LastError: "feed returned HTTP 503"},
		},
	}
	if got := wirePresentationLabel(status, now); got != "DEGRADED" {
		t.Fatalf("partial presentation = %q, want DEGRADED", got)
	}
	if got := wireHealthLabel(status, now); got != "PARTIAL" {
		t.Fatalf("partial health = %q, want PARTIAL", got)
	}
	sourceFailure := status
	sourceFailure.Mode = signalwire.ModeSourceError
	sourceFailure.Feeds = []signalwire.FeedStatus{status.Feeds[0]}
	if got := wirePresentationLabel(sourceFailure, now); got != "DEGRADED" {
		t.Fatalf("healthy-feed/source-error presentation = %q, want DEGRADED", got)
	}
	partialSources := signalwire.Status{
		Configured: true, BlueskyConfigured: true, Mode: signalwire.ModeStreaming,
		Sources:        []signalwire.SourceStatus{{Handle: "healthy.example", DID: "did:plc:healthy"}},
		SourcesPartial: true, SourceIssue: "Some Bluesky sources could not be resolved: broken.example",
	}
	if got := wirePresentationLabel(partialSources, now); got != "DEGRADED" {
		t.Fatalf("partial Bluesky resolution presentation = %q, want DEGRADED", got)
	}
	if got := wireHealthLabel(partialSources, now); got != "PARTIAL" {
		t.Fatalf("partial Bluesky resolution health = %q, want PARTIAL", got)
	}
	retained := wireSignalCard(signalwire.Signal{
		ID: "retained", Source: signalwire.SourceFeed, SourceName: "Broken Publisher", OccurredAt: now.Add(-time.Hour),
	}, status, now)
	if !retained.Retained {
		t.Fatalf("failed-feed signal was not marked retained: %+v", retained)
	}
	streaming := wireSignalCard(signalwire.Signal{
		ID: "streaming", Source: signalwire.SourceBluesky, OccurredAt: now.Add(-time.Minute),
	}, status, now)
	if streaming.Retained {
		t.Fatalf("healthy Bluesky signal was incorrectly marked retained: %+v", streaming)
	}

	html := renderWireComponent(t, "SignalCard", retained)
	if !strings.Contains(html, "RETAINED · AS OF") {
		t.Fatalf("retained signal did not render its as-of label: %s", html)
	}
}

func TestWireFeedHealthFollowsConfiguredCadence(t *testing.T) {
	service, err := signalwire.NewService(signalwire.Config{
		Root: t.TempDir(), Enabled: true, FeedsEnabled: true,
		FeedInterval: time.Hour,
		FeedSources:  []signalwire.FeedSource{{Name: "Hourly Publisher", URL: "https://publisher.example/feed", EvidenceType: "news", Enabled: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	status := service.Status()
	t0 := time.Date(2026, 8, 23, 20, 0, 0, 0, time.UTC)
	feed := signalwire.FeedStatus{Name: "Hourly Publisher", State: "ready", LastChecked: t0}
	if got := wireFeedHealthLabelForStatus(feed, status, t0.Add(15*time.Minute+time.Second)); got != "READY" {
		t.Fatalf("hourly feed at 15m+1s = %q, want READY", got)
	}
	if got := wireFeedHealthLabelForStatus(feed, status, t0.Add(status.FeedStaleAfter+time.Second)); got != "STALE" {
		t.Fatalf("hourly feed after derived threshold = %q, want STALE (threshold %v)", got, status.FeedStaleAfter)
	}
}

func TestWireFeedHealthDistinguishesNeverCheckedStaleAndError(t *testing.T) {
	now := time.Date(2026, 8, 23, 20, 0, 0, 0, time.UTC)
	cases := []struct {
		name  string
		feed  signalwire.FeedStatus
		state string
	}{
		{name: "never checked", feed: signalwire.FeedStatus{Name: "New", State: "waiting"}, state: "NEVER CHECKED"},
		{name: "stale", feed: signalwire.FeedStatus{Name: "Old", State: "ready", LastChecked: now.Add(-signalwire.DeriveFeedStaleAfter(0) - time.Second)}, state: "STALE"},
		{name: "error", feed: signalwire.FeedStatus{Name: "Broken", State: "error", LastChecked: now, LastError: "timeout"}, state: "ERROR"},
		{name: "ready", feed: signalwire.FeedStatus{Name: "Current", State: "ready", LastChecked: now}, state: "READY"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := wireFeedHealthLabel(test.feed, now); got != test.state {
				t.Fatalf("feed health = %q, want %q", got, test.state)
			}
		})
	}
	if got := wireFeedCheckedLabel(cases[0].feed); got != "NEVER CHECKED" {
		t.Fatalf("never-checked timestamp = %q", got)
	}
	if got := wireFeedPublishedLabel(cases[0].feed); got != "NEVER PUBLISHED" {
		t.Fatalf("never-published timestamp = %q", got)
	}
	if got := wireFeedPublishedLabel(signalwire.FeedStatus{LastPublished: now}); got == "NEVER PUBLISHED" {
		t.Fatalf("published timestamp was rendered as never published")
	}
}

func TestWirePageAndPulseExposePartialSourceIssue(t *testing.T) {
	resolver := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Query().Get("handle") == "broken.example" {
			writer.WriteHeader(http.StatusBadGateway)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"did":"did:plc:healthy"}`))
	}))
	defer resolver.Close()

	service, err := signalwire.NewService(signalwire.Config{
		Root: t.TempDir(), Enabled: true, FeedsEnabled: false,
		Handles: []string{"healthy.example", "broken.example"}, ResolveURL: resolver.URL,
		JetstreamURL: "wss://stream.invalid/subscribe", ReconnectMin: time.Millisecond, ReconnectMax: time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	service.Start(ctx)
	deadline := time.Now().Add(2 * time.Second)
	for service.Status().SourceIssue == "" && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	status := service.Status()
	if status.SourceIssue == "" || !status.SourcesPartial {
		t.Fatalf("source status = %+v, want persistent partial issue", status)
	}
	stats, err := openstats.NewService(openstats.Config{Root: t.TempDir(), Season: 2026, Enabled: false})
	if err != nil {
		t.Fatal(err)
	}
	data := wirePageData(httptest.NewRequest(http.MethodGet, "/wire", nil), service, stats)
	pulse := PulseData(service)
	if got := data["wire_source_issue"]; got != status.SourceIssue {
		t.Fatalf("page source issue = %#v, want %q", got, status.SourceIssue)
	}
	if got := pulse["source_issue"]; got != status.SourceIssue {
		t.Fatalf("pulse source issue = %#v, want %q", got, status.SourceIssue)
	}
	if got := data["wire_mode"]; got != "DEGRADED" {
		t.Fatalf("page mode = %#v, want DEGRADED", got)
	}
	if got := pulse["mode"]; got != "DEGRADED" {
		t.Fatalf("pulse mode = %#v, want DEGRADED", got)
	}
	if got := pulse["status"].(string); !strings.Contains(got, status.SourceIssue) {
		t.Fatalf("pulse status %q does not expose source issue %q", got, status.SourceIssue)
	}
}

func TestWireCopyContractsMatchFirstRenderAndFeedFragment(t *testing.T) {
	pageBytes, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	source := string(pageBytes)
	const (
		reportLink       = "Read the report ↗"
		unconfiguredCopy = "Ask the commissioner to add news sources."
		configuredCopy   = "Relevant feed items and league sightings appear here, and stay provisional until the official stats catch up."
		oldReportLink    = "Inspect source ↗"
		oldUnconfigured  = "No sources are on the wire yet. Ask the commissioner to add reporters."
	)
	for _, want := range []string{
		">" + reportLink + "</a>",
		"<p>" + unconfiguredCopy + "</p>",
		"<p>" + configuredCopy + "</p>",
	} {
		if !strings.Contains(source, want) {
			t.Errorf("first-render Wire copy missing %q", want)
		}
	}
	for _, old := range []string{oldReportLink, oldUnconfigured} {
		if strings.Contains(source, old) {
			t.Errorf("first-render Wire source still contains retired copy %q", old)
		}
	}

	cardHTML := renderWireComponent(t, "SignalCard", WireSignalCard{
		ID: "copy-contract", Category: "news", Label: "NEWS", Text: "A signal",
		Source: "Publisher", Evidence: "REPORT", Trust: "VERIFIED", Confidence: "80",
		Time: "NOW", URL: "https://example.test/report", HasURL: true,
	})
	if !strings.Contains(cardHTML, reportLink) {
		t.Fatalf("feed-fragment SignalCard copy = %q, want %q", cardHTML, reportLink)
	}
	if strings.Contains(cardHTML, oldReportLink) {
		t.Fatalf("feed-fragment SignalCard still contains retired copy %q", oldReportLink)
	}

	for _, test := range []struct {
		name       string
		configured bool
		want       string
		unwanted   string
	}{
		{name: "unconfigured", configured: false, want: unconfiguredCopy, unwanted: oldUnconfigured},
		{name: "configured", configured: true, want: configuredCopy, unwanted: oldUnconfigured},
	} {
		t.Run(test.name, func(t *testing.T) {
			html := renderWireComponent(t, "WireEmptyState", WireEmptyView{
				WireConfigured: test.configured,
				WireIssue:      "Wire is not configured.",
			})
			if !strings.Contains(html, test.want) {
				t.Fatalf("feed-fragment WireEmptyState copy = %q, want %q", html, test.want)
			}
			if strings.Contains(html, test.unwanted) {
				t.Fatalf("feed-fragment WireEmptyState still contains retired copy %q", test.unwanted)
			}
		})
	}
}

func renderWireComponent(t *testing.T, component string, props any) string {
	t.Helper()
	program, err := route.LoadFileProgramHere("page.gsx")
	if err != nil {
		t.Fatalf("LoadFileProgramHere(page.gsx): %v", err)
	}
	node, err := route.RenderProgramComponentNode(program, component, route.ProgramRenderEnv{Props: props})
	if err != nil {
		t.Fatalf("RenderProgramComponentNode(%s): %v", component, err)
	}
	return gosx.RenderHTML(node)
}
