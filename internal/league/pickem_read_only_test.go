package league

import (
	"net/http"
	"reflect"
	"testing"
	"time"
)

func TestPickemDataReadOnlyDoesNotReconcileOrBackfill(t *testing.T) {
	service := newTestService(t, true)
	now := time.Date(2026, 9, 10, 18, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	games := []GameInfo{
		{
			ID:                "future",
			Week:              1,
			Kickoff:           now.Add(2 * time.Hour),
			Away:              "AAA",
			Home:              "BBB",
			SpreadLineTenths:  25,
			SpreadLinePresent: true,
			SourceObservedAt:  now.Add(-time.Hour),
		},
	}
	service.SetScheduleSource(func() []GameInfo { return games })
	request, err := http.NewRequest(http.MethodGet, "/pickem?week=1", nil)
	if err != nil {
		t.Fatal(err)
	}

	before := service.store.Snapshot()
	data := service.PickemDataReadOnly(request)
	after := service.store.Snapshot()
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("read-only Pickem projection changed durable state: before=%+v after=%+v", before, after)
	}
	if got := data["week"]; got != 1 {
		t.Fatalf("read-only Pickem week = %v, want selected week 1", got)
	}
	if _, ok := after.PickemMarkets["future"]; ok {
		t.Fatal("read-only Pickem projection reconciled a market candidate")
	}
}
