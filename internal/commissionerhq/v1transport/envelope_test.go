package v1transport

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestErrorEnvelopeGolden(t *testing.T) {
	t.Parallel()
	var fixture struct {
		RequestID string                     `json:"request_id"`
		Statuses  map[string]json.RawMessage `json:"statuses"`
	}
	data, err := os.ReadFile(filepath.Join("testdata", "error_envelopes.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	for rawStatus, want := range fixture.Statuses {
		status, err := strconv.Atoi(rawStatus)
		if err != nil {
			t.Fatal(err)
		}
		recorder := httptest.NewRecorder()
		writeEnvelope(recorder, status, fixture.RequestID)
		if recorder.Code != status {
			t.Errorf("status %d wrote %d", status, recorder.Code)
		}
		var got any
		if err := json.Unmarshal(recorder.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		var expected any
		if err := json.Unmarshal(want, &expected); err != nil {
			t.Fatal(err)
		}
		gotJSON, _ := json.Marshal(got)
		wantJSON, _ := json.Marshal(expected)
		if string(gotJSON) != string(wantJSON) {
			t.Errorf("status %d envelope = %s, want %s", status, gotJSON, wantJSON)
		}
		if recorder.Header().Get("Cache-Control") != "private, no-store" || recorder.Header().Get("Content-Type") != "application/json; charset=utf-8" {
			t.Errorf("status %d headers = %v", status, recorder.Header())
		}
	}
}
