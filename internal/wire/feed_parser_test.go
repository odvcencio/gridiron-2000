package wire

import (
	"strings"
	"testing"
)

func TestParseRSSAndAtomFeeds(t *testing.T) {
	rss := `<?xml version="1.0"?><rss><channel><item><guid>story-1</guid><title>Player ruled out</title><description><![CDATA[<b>Source:</b> Player will not play.<script>bad()</script>]]></description><link>/nfl/story?utm_source=test</link><pubDate>Sat, 8 Aug 2026 20:32:39 EST</pubDate></item></channel></rss>`
	items, err := parseSyndication([]byte(rss), "https://example.com/feed")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Link != "https://example.com/nfl/story?utm_source=test" || strings.Contains(items[0].Description, "bad()") {
		t.Fatalf("RSS items = %+v", items)
	}
	if items[0].PublishedAt.IsZero() {
		t.Fatal("RSS publication time was not parsed")
	}

	atom := `<feed xmlns="http://www.w3.org/2005/Atom"><entry><id>tag:example,1</id><title>Limited practice</title><summary>Receiver was limited.</summary><link rel="alternate" href="https://example.com/post"/><updated>2026-08-09T01:02:03Z</updated></entry></feed>`
	items, err = parseSyndication([]byte(atom), "https://example.com/feed.atom")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Link != "https://example.com/post" || items[0].UpdatedAt.IsZero() {
		t.Fatalf("Atom items = %+v", items)
	}
}

func TestCanonicalSourceURLDropsTrackingButKeepsEvidenceIdentity(t *testing.T) {
	got := canonicalSourceURL("HTTPS://Example.COM/story/?utm_source=feed&player=42#comments")
	if got != "https://example.com/story?player=42" {
		t.Fatalf("canonical URL = %q", got)
	}
}
