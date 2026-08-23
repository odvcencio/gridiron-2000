package wire

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
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
