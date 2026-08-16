package wire

import (
	"fmt"
	"testing"
	"time"
)

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
