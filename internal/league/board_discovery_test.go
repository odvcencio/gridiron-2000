package league

import (
	"net/http"
	"strings"
	"testing"
)

func TestBoardDataFiltersGloballyAndPaginatesAvailablePool(t *testing.T) {
	service := newTestService(t, true)
	pool := testPool(595)
	service.SetPlayerSource(func() ([]Player, int64, string) { return pool, 1, "live" })
	request, _ := http.NewRequest(http.MethodGet, "/board?page=999", nil)

	data := service.BoardData(request)
	available, ok := data["available"].([]map[string]any)
	if !ok {
		t.Fatalf("available = %#v, want typed rows", data["available"])
	}
	if len(available) != 45 {
		t.Fatalf("clamped final page = %d rows, want 45", len(available))
	}
	if data["available_count"] != 595 || data["matching_count"] != 595 {
		t.Fatalf("unfiltered counts = available:%v matching:%v, want 595/595", data["available_count"], data["matching_count"])
	}
	if data["pool_page"] != 12 || data["pool_pages"] != 12 || data["pool_page_start"] != 551 || data["pool_page_end"] != 595 {
		t.Fatalf("clamped page = %v/%v range %v-%v, want 12/12 range 551-595", data["pool_page"], data["pool_pages"], data["pool_page_start"], data["pool_page_end"])
	}
	if available[0]["id"] != "pool-551" || available[len(available)-1]["id"] != "pool-595" {
		t.Fatalf("canonical final-page order = %v ... %v, want pool-551 ... pool-595", available[0]["id"], available[len(available)-1]["id"])
	}
	for _, raw := range data["position_filters"].([]map[string]any) {
		if href, _ := raw["href"].(string); !strings.HasSuffix(href, "#board-pool") {
			t.Fatalf("position filter href = %q, want #board-pool anchor", href)
		}
	}
	for _, raw := range data["pool_page_links"].([]map[string]any) {
		if href, _ := raw["href"].(string); !strings.HasSuffix(href, "#board-pool") {
			t.Fatalf("page link href = %q, want #board-pool anchor", href)
		}
	}

	// A name that lives after the first 50 rows must still be found by the
	// server; a client-only filter over page one would return no rows here.
	request, _ = http.NewRequest(http.MethodGet, "/board?q=560", nil)
	data = service.BoardData(request)
	available, _ = data["available"].([]map[string]any)
	if data["matching_count"] != 1 || len(available) != 1 || available[0]["id"] != "pool-560" {
		t.Fatalf("global search = count:%v rows:%v, want pool-560 only", data["matching_count"], available)
	}

	request, _ = http.NewRequest(http.MethodGet, "/board?pos=wr&page=999", nil)
	data = service.BoardData(request)
	available, _ = data["available"].([]map[string]any)
	if data["pos"] != "WR" || data["matching_count"] != 99 || data["pool_page"] != 2 || len(available) != 49 {
		t.Fatalf("position filter = pos:%v count:%v page:%v rows:%d, want WR/99/page2/49", data["pos"], data["matching_count"], data["pool_page"], len(available))
	}
	for _, row := range available {
		if row["position"] != "WR" {
			t.Fatalf("position filter leaked %v", row["position"])
		}
	}
	if data["pool_previous_href"] != "/board?pos=WR#board-pool" {
		t.Fatalf("position previous href = %v, want /board?pos=WR#board-pool", data["pool_previous_href"])
	}

	request, _ = http.NewRequest(http.MethodGet, "/board?pos=not-a-position", nil)
	data = service.BoardData(request)
	if data["pos"] != "" || data["matching_count"] != 595 {
		t.Fatalf("invalid position filter = pos:%v matching:%v, want empty/595", data["pos"], data["matching_count"])
	}
}

func TestBoardDataExcludesBoardAndPickedPlayersFromAvailableCounts(t *testing.T) {
	service := newTestService(t, true)
	pool := testPool(595)
	service.SetPlayerSource(func() ([]Player, int64, string) { return pool, 1, "live" })
	request, _ := http.NewRequest(http.MethodGet, "/board", nil)

	if _, err := service.BoardAdd(request, "pool-001"); err != nil {
		t.Fatalf("BoardAdd: %v", err)
	}
	if _, _, _, err := service.MakePick(request, teamOnClock(nil, 1), "pool-002"); err != nil {
		t.Fatalf("MakePick: %v", err)
	}
	data := service.BoardData(request)
	if data["board_count"] != 1 || data["available_count"] != 593 || data["matching_count"] != 593 {
		t.Fatalf("board/picked counts = board:%v available:%v matching:%v, want 1/593/593", data["board_count"], data["available_count"], data["matching_count"])
	}
	available, _ := data["available"].([]map[string]any)
	for _, row := range available {
		if row["id"] == "pool-001" || row["id"] == "pool-002" {
			t.Fatalf("excluded player leaked into available page: %v", row["id"])
		}
	}
}

func TestBoardDataDistinguishesEmptyPoolFromEmptyFilter(t *testing.T) {
	service := newTestService(t, true)
	pool := testPool(1)
	service.SetPlayerSource(func() ([]Player, int64, string) { return pool, 1, "live" })
	request, _ := http.NewRequest(http.MethodGet, "/board?q=not-found", nil)

	data := service.BoardData(request)
	if data["available_empty"] != false || data["matching_empty"] != true || data["available_count"] != 1 || data["matching_count"] != 0 {
		t.Fatalf("no-match state = empty:%v matching-empty:%v available:%v matching:%v, want false/true/1/0", data["available_empty"], data["matching_empty"], data["available_count"], data["matching_count"])
	}

	if _, err := service.BoardAdd(request, "pool-001"); err != nil {
		t.Fatalf("BoardAdd: %v", err)
	}
	data = service.BoardData(request)
	if data["available_empty"] != true || data["matching_empty"] != true || data["available_count"] != 0 || data["matching_count"] != 0 {
		t.Fatalf("empty-pool state = empty:%v matching-empty:%v available:%v matching:%v, want true/true/0/0", data["available_empty"], data["matching_empty"], data["available_count"], data["matching_count"])
	}
}

func TestBoardPositionFilterUsesPoolAllowlist(t *testing.T) {
	for _, test := range []struct {
		input string
		want  string
	}{
		{input: " wr ", want: "WR"},
		{input: "ALL", want: ""},
		{input: "flex", want: ""},
		{input: "QB<script>", want: ""},
	} {
		if got := BoardPositionFilter(test.input); got != test.want {
			t.Errorf("BoardPositionFilter(%q) = %q, want %q", test.input, got, test.want)
		}
	}
}
