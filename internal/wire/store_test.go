package wire

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStoreRedactsDeletedPostTextAndKeepsMetadataJournal(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root, 20)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 13, 20, 12, 0, 0, time.UTC)
	signal := Signal{
		SchemaVersion: SchemaVersion,
		ID:            "signal-1",
		Source:        SourceBluesky,
		SourceDID:     "did:plc:test",
		SourceURI:     "at://did:plc:test/app.bsky.feed.post/abc",
		SourceURL:     "https://bsky.app/profile/did:plc:test/post/abc",
		CID:           "cid-1",
		Category:      "touchdown",
		Label:         "TOUCHDOWN",
		Text:          "Secret post body that must be redacted",
		TextHash:      "hash-1",
		Rule:          "Touchdown",
		Confidence:    0.78,
		Provisional:   true,
		OccurredAt:    now,
		ObservedAt:    now,
	}
	accepted, err := store.Apply(signal, "create", now.UnixMicro())
	if err != nil || !accepted {
		t.Fatalf("apply create: accepted=%v err=%v", accepted, err)
	}
	journalPath := filepath.Join(root, "events", "2026-09-13.ndjson")
	journal, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(journal), signal.Text) {
		t.Fatal("metadata journal retained source post text")
	}

	tombstoneAt := now.Add(time.Minute)
	accepted, err = store.Apply(Signal{ID: signal.ID, ObservedAt: tombstoneAt}, "delete", tombstoneAt.UnixMicro())
	if err != nil || !accepted {
		t.Fatalf("apply delete: accepted=%v err=%v", accepted, err)
	}
	if recent := store.Recent(10, ""); len(recent) != 0 {
		t.Fatalf("deleted signal remained visible: %+v", recent)
	}
	state, err := os.ReadFile(filepath.Join(root, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(state), signal.Text) {
		t.Fatal("current-state file retained deleted post text")
	}

	reloaded, err := NewStore(root, 20)
	if err != nil {
		t.Fatal(err)
	}
	relevant, _, deleted, _ := reloaded.Metrics()
	if relevant != 1 || deleted != 1 {
		t.Fatalf("reloaded metrics = relevant %d deleted %d", relevant, deleted)
	}
}

func TestStoreAdvancesCursorWithoutCountingDuplicateCID(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root, 20)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 13, 20, 12, 0, 0, time.UTC)
	signal := Signal{
		ID:         "signal-1",
		Source:     SourceBluesky,
		SourceDID:  "did:plc:test",
		SourceURI:  "at://did:plc:test/app.bsky.feed.post/abc",
		CID:        "cid-1",
		Category:   "touchdown",
		Text:       "Touchdown reported",
		TextHash:   "hash-1",
		ObservedAt: now,
	}
	accepted, err := store.Apply(signal, "create", 100)
	if err != nil || !accepted {
		t.Fatalf("initial apply: accepted=%v err=%v", accepted, err)
	}

	signal.ObservedAt = now.Add(time.Second)
	accepted, err = store.Apply(signal, "create", 200)
	if err != nil {
		t.Fatal(err)
	}
	if accepted {
		t.Fatal("duplicate CID was accepted")
	}
	if got := store.Cursor(); got != 200 {
		t.Fatalf("cursor = %d, want 200", got)
	}
	relevant, _, _, _ := store.Metrics()
	if relevant != 1 {
		t.Fatalf("relevant signals = %d, want 1", relevant)
	}
}

func TestRecentClustersIndependentReportsAndKeepsStrongestEvidence(t *testing.T) {
	store, err := NewStore(t.TempDir(), 20)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 13, 20, 12, 0, 0, time.UTC)
	reports := []Signal{
		{
			ID: "community-report", Source: SourceLeague, SourceURI: "league://member-a/report-1",
			SourceName: "PrizePicks", EvidenceType: "market", TrustTier: "MARKET WATCH",
			ClusterID: "cluster-story", CID: "community-cid", Category: "market", Text: "Player removed from the board",
			Confidence: 0.21, ObservedAt: now,
		},
		{
			ID: "publisher-report", Source: SourceFeed, SourceURI: "feed://espn/item-1",
			SourceName: "ESPN NFL Headlines", EvidenceType: "news", TrustTier: "PUBLISHER",
			ClusterID: "cluster-story", CID: "publisher-cid", Category: "injury", Text: "Player ruled out",
			Confidence: 0.72, ObservedAt: now.Add(time.Minute),
		},
	}
	for _, report := range reports {
		if accepted, applyErr := store.Apply(report, "create", 0); applyErr != nil || !accepted {
			t.Fatalf("apply report: accepted=%v err=%v", accepted, applyErr)
		}
	}
	recent := store.Recent(10, "")
	if len(recent) != 1 {
		t.Fatalf("recent clusters = %d, want 1", len(recent))
	}
	if recent[0].SourceName != "ESPN NFL Headlines" || recent[0].Corroborations != 2 {
		t.Fatalf("cluster view = %+v", recent[0])
	}
}

func TestRecentSortsBatchIngestByPublishedTime(t *testing.T) {
	store, err := NewStore(t.TempDir(), 20)
	if err != nil {
		t.Fatal(err)
	}
	observed := time.Date(2026, 9, 13, 21, 0, 0, 0, time.UTC)
	for _, signal := range []Signal{
		{ID: "newer", Source: SourceFeed, SourceURI: "feed://test/newer", CID: "newer", Category: "injury", OccurredAt: observed.Add(-time.Minute), ObservedAt: observed},
		{ID: "older", Source: SourceFeed, SourceURI: "feed://test/older", CID: "older", Category: "injury", OccurredAt: observed.Add(-time.Hour), ObservedAt: observed},
	} {
		if accepted, applyErr := store.Apply(signal, "create", 0); applyErr != nil || !accepted {
			t.Fatalf("apply signal: accepted=%v err=%v", accepted, applyErr)
		}
	}
	recent := store.Recent(10, "")
	if len(recent) != 2 || recent[0].ID != "newer" || recent[1].ID != "older" {
		t.Fatalf("recent order = %+v", recent)
	}
}
