package wire

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestFeedSyncClassifiesAndConditionallyRefetches(t *testing.T) {
	now := time.Date(2026, 8, 9, 2, 0, 0, 0, time.UTC)
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests++
		if request.Header.Get("If-None-Match") == `"news-v1"` {
			writer.WriteHeader(http.StatusNotModified)
			return
		}
		writer.Header().Set("ETag", `"news-v1"`)
		writer.Header().Set("Content-Type", "application/rss+xml")
		_, _ = writer.Write([]byte(`<?xml version="1.0"?><rss><channel><item><guid>1</guid><title>Receiver ruled out</title><description>He will not play Sunday.</description><link>https://publisher.example/story</link><pubDate>Sat, 8 Aug 2026 20:00:00 GMT</pubDate></item></channel></rss>`))
	}))
	defer server.Close()

	service, err := NewService(Config{
		Root: t.TempDir(), Enabled: true, FeedsEnabled: true,
		FeedSources:  []FeedSource{{Name: "Test Publisher", URL: server.URL, EvidenceType: "news", Enabled: true}},
		FeedInterval: time.Hour, FeedMaxAge: 72 * time.Hour, FeedMaxBytes: 1 << 20,
		HTTPClient: server.Client(), Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	service.syncFeeds(t.Context())
	recent := service.Recent(10, "")
	if len(recent) != 1 || recent[0].SourceName != "Test Publisher" || recent[0].TrustTier != "PUBLISHER" || recent[0].Category != "inactive" {
		t.Fatalf("recent signals = %+v", recent)
	}
	service.syncFeeds(t.Context())
	if requests != 2 || service.Status().Feeds[0].Accepted != 1 {
		t.Fatalf("requests=%d feed=%+v", requests, service.Status().Feeds)
	}
}
