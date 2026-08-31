package wire

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRuntimeModeVocabularyIsExhaustive(t *testing.T) {
	expected := []string{
		ModeDisabled,
		ModeAwaitingSources,
		ModeReady,
		ModeSyndicationReady,
		ModeSyndicating,
		ModeResolvingSources,
		ModeConnecting,
		ModeStreaming,
		ModeReconnecting,
		ModeSourceError,
		ModeStopped,
	}
	if len(runtimeModes) != len(expected) {
		t.Fatalf("runtime mode count = %d, want %d", len(runtimeModes), len(expected))
	}
	for index, mode := range expected {
		if runtimeModes[index] != mode {
			t.Fatalf("runtime mode %d = %q, want %q", index, runtimeModes[index], mode)
		}
	}

	cases := []struct {
		name   string
		config Config
		want   string
	}{
		{name: "disabled", config: Config{Root: t.TempDir(), Enabled: false}, want: ModeDisabled},
		{name: "awaiting sources", config: Config{Root: t.TempDir(), Enabled: true, FeedsEnabled: false}, want: ModeAwaitingSources},
		{name: "ready", config: Config{Root: t.TempDir(), Enabled: true, FeedsEnabled: false, DIDs: []string{"did:plc:reporter"}, JetstreamURL: defaultJetstreamURL}, want: ModeReady},
		{name: "syndication ready", config: Config{
			Root: t.TempDir(), Enabled: true, FeedsEnabled: true,
			FeedSources: []FeedSource{{Name: "Test Publisher", URL: "https://publisher.example/feed", EvidenceType: "news", Enabled: true}},
		}, want: ModeSyndicationReady},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			service, err := NewService(test.config)
			if err != nil {
				t.Fatal(err)
			}
			if got := service.Status().Mode; got != test.want {
				t.Fatalf("initial mode = %q, want %q", got, test.want)
			}
		})
	}

	service, err := NewService(Config{Root: t.TempDir(), Enabled: false})
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range expected {
		service.setRuntimeState(mode, true, "", time.Time{})
		if got := service.Status().Mode; got != mode {
			t.Fatalf("set runtime mode = %q, want %q", got, mode)
		}
	}
}

func TestSourceErrorIsNotHiddenBySyndication(t *testing.T) {
	resolver := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusBadGateway)
	}))
	defer resolver.Close()

	service, err := NewService(Config{
		Root: t.TempDir(), Enabled: true, FeedsEnabled: true,
		Handles: []string{"reporter.example"}, ResolveURL: resolver.URL,
		JetstreamURL: defaultJetstreamURL,
		FeedSources:  []FeedSource{{Name: "Healthy Publisher", URL: "https://publisher.example/feed", EvidenceType: "news", Enabled: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	service.run(context.Background())
	if got := service.Status().Mode; got != ModeSourceError {
		t.Fatalf("source resolution failure mode = %q, want %q", got, ModeSourceError)
	}
}

func TestMixedSourceResolutionKeepsHealthySourcesAndIssue(t *testing.T) {
	resolver := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Query().Get("handle") == "broken.example" {
			writer.WriteHeader(http.StatusBadGateway)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"did":"did:plc:healthy"}`))
	}))
	defer resolver.Close()

	service, err := NewService(Config{
		Root: t.TempDir(), Enabled: true, FeedsEnabled: false,
		Handles: []string{"healthy.example", "broken.example"}, ResolveURL: resolver.URL,
		JetstreamURL: defaultJetstreamURL,
	})
	if err != nil {
		t.Fatal(err)
	}
	sources, err := service.resolveSources(context.Background())
	if err != nil {
		t.Fatalf("mixed source resolution returned an error: %v", err)
	}
	if len(sources) != 1 || sources[0].DID != "did:plc:healthy" {
		t.Fatalf("resolved sources = %+v, want only the healthy DID", sources)
	}
	status := service.Status()
	if !status.SourcesPartial {
		t.Fatal("mixed resolution did not mark the source set partial")
	}
	if status.SourceIssue == "" || !strings.Contains(status.SourceIssue, "broken.example") {
		t.Fatalf("source issue = %q, want a safe failure for broken.example", status.SourceIssue)
	}
	service.setRuntimeState(ModeReconnecting, true, "temporary stream disconnect", time.Time{})
	if got := service.Status().SourceIssue; got != status.SourceIssue {
		t.Fatalf("source issue changed during reconnect: %q -> %q", status.SourceIssue, got)
	}
}

func TestFeedStaleAfterFollowsConfiguredInterval(t *testing.T) {
	service, err := NewService(Config{
		Root: t.TempDir(), Enabled: true, FeedsEnabled: true,
		FeedInterval: time.Hour,
		FeedSources:  []FeedSource{{Name: "Hourly Publisher", URL: "https://publisher.example/feed", EvidenceType: "news", Enabled: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	status := service.Status()
	if status.FeedStaleAfter <= 15*time.Minute {
		t.Fatalf("hourly feed stale threshold = %v, want it above the old 15m floor", status.FeedStaleAfter)
	}
	if got, want := status.FeedStaleAfter, DeriveFeedStaleAfter(time.Hour); got != want {
		t.Fatalf("status stale threshold = %v, want centralized derivation %v", got, want)
	}
}

func TestServiceIngestsAndDeletesJetstreamPost(t *testing.T) {
	now := time.Date(2026, 9, 13, 20, 12, 0, 0, time.UTC)
	service, err := NewService(Config{
		Root:    t.TempDir(),
		Enabled: false,
		Now:     func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	create := fmt.Sprintf(`{
		"did":"did:plc:reporter",
		"time_us":%d,
		"kind":"commit",
		"commit":{
			"operation":"create",
			"collection":"app.bsky.feed.post",
			"rkey":"post1",
			"cid":"cid1",
			"record":{"$type":"app.bsky.feed.post","text":"Touchdown on a 20-yard pass","createdAt":"2026-09-13T20:11:58Z"}
		}
	}`, now.UnixMicro())
	accepted, err := service.IngestJSON([]byte(create))
	if err != nil || !accepted {
		t.Fatalf("ingest create: accepted=%v err=%v", accepted, err)
	}
	signals := service.Recent(10, "")
	if len(signals) != 1 || signals[0].Category != "touchdown" || !signals[0].Provisional {
		t.Fatalf("unexpected signals: %+v", signals)
	}
	deleteEvent := fmt.Sprintf(`{
		"did":"did:plc:reporter",
		"time_us":%d,
		"kind":"commit",
		"commit":{
			"operation":"delete",
			"collection":"app.bsky.feed.post",
			"rkey":"post1"
		}
	}`, now.Add(time.Minute).UnixMicro())
	accepted, err = service.IngestJSON([]byte(deleteEvent))
	if err != nil || !accepted {
		t.Fatalf("ingest delete: accepted=%v err=%v", accepted, err)
	}
	if got := service.Recent(10, ""); len(got) != 0 {
		t.Fatalf("deleted post remained visible: %+v", got)
	}
}

// TestOnSignalFiresForARelevantSignalOnly covers the GC-2 subscription
// seam: a registered callback runs once for a relevant, classified
// signal, carrying its category and text, and never runs for a post the
// classifier ignores as noise.
func TestOnSignalFiresForARelevantSignalOnly(t *testing.T) {
	now := time.Date(2026, 9, 13, 20, 12, 0, 0, time.UTC)
	service, err := NewService(Config{Root: t.TempDir(), Enabled: false, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	var got []Signal
	service.OnSignal(func(signal Signal) { got = append(got, signal) })

	touchdown := fmt.Sprintf(`{
		"did":"did:plc:reporter",
		"time_us":%d,
		"kind":"commit",
		"commit":{
			"operation":"create",
			"collection":"app.bsky.feed.post",
			"rkey":"post1",
			"cid":"cid1",
			"record":{"$type":"app.bsky.feed.post","text":"Touchdown Bills!","createdAt":"2026-09-13T20:11:58Z"}
		}
	}`, now.UnixMicro())
	if _, err := service.IngestJSON([]byte(touchdown)); err != nil {
		t.Fatalf("ingest touchdown: %v", err)
	}

	noise := fmt.Sprintf(`{
		"did":"did:plc:reporter",
		"time_us":%d,
		"kind":"commit",
		"commit":{
			"operation":"create",
			"collection":"app.bsky.feed.post",
			"rkey":"post2",
			"cid":"cid2",
			"record":{"$type":"app.bsky.feed.post","text":"Who do you like in mock drafts this year?","createdAt":"2026-09-13T20:12:30Z"}
		}
	}`, now.Add(time.Second).UnixMicro())
	if _, err := service.IngestJSON([]byte(noise)); err != nil {
		t.Fatalf("ingest noise: %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("callback fired %d times, want 1 (only the relevant signal): %+v", len(got), got)
	}
	if got[0].Category != CategoryTouchdown || !strings.Contains(got[0].Text, "Bills") {
		t.Fatalf("callback signal = %+v, want category %q naming Bills", got[0], CategoryTouchdown)
	}
}

// TestOnSignalRunsEveryRegisteredCallbackInOrder covers multi-registration:
// two callbacks both fire, in the order registered.
func TestOnSignalRunsEveryRegisteredCallbackInOrder(t *testing.T) {
	now := time.Date(2026, 9, 13, 20, 12, 0, 0, time.UTC)
	service, err := NewService(Config{Root: t.TempDir(), Enabled: false, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	service.OnSignal(func(Signal) { order = append(order, "first") })
	service.OnSignal(func(Signal) { order = append(order, "second") })

	event := fmt.Sprintf(`{
		"did":"did:plc:reporter",
		"time_us":%d,
		"kind":"commit",
		"commit":{
			"operation":"create",
			"collection":"app.bsky.feed.post",
			"rkey":"post1",
			"cid":"cid1",
			"record":{"$type":"app.bsky.feed.post","text":"Fumble recovered by the defense","createdAt":"2026-09-13T20:11:58Z"}
		}
	}`, now.UnixMicro())
	if _, err := service.IngestJSON([]byte(event)); err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if len(order) != 2 || order[0] != "first" || order[1] != "second" {
		t.Fatalf("callback order = %v, want [first second]", order)
	}
}
