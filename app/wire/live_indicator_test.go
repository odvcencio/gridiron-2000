package wire

import (
	"os"
	"strings"
	"testing"
	"time"

	signalwire "gridiron-2000/internal/wire"
)

func TestWireLiveIndicatorOnlyLightsHealthyStreamingState(t *testing.T) {
	now := time.Date(2026, 8, 23, 20, 0, 0, 0, time.UTC)
	cases := []struct {
		name   string
		status signalwire.Status
		want   string
	}{
		{name: "healthy streaming", status: signalwire.Status{Configured: true, Mode: signalwire.ModeStreaming}, want: "LIVE"},
		{name: "healthy syndicating", status: signalwire.Status{Configured: true, Mode: signalwire.ModeSyndicating}, want: "LIVE"},
		{name: "ready but not streaming", status: signalwire.Status{Configured: true, Mode: signalwire.ModeReady}, want: ""},
		{name: "partial live feed", status: signalwire.Status{Configured: true, Mode: signalwire.ModeStreaming, Feeds: []signalwire.FeedStatus{{Name: "broken", State: "error", LastChecked: now, LastError: "timeout"}}}, want: ""},
		{name: "source error", status: signalwire.Status{Configured: true, Mode: signalwire.ModeSourceError}, want: ""},
		{name: "off", status: signalwire.Status{Mode: signalwire.ModeDisabled}, want: ""},
		{name: "unknown", status: signalwire.Status{Mode: "future-mode"}, want: ""},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := wireLiveIndicator(test.status, now); got != test.want {
				t.Fatalf("wireLiveIndicator = %q, want %q (presentation=%q)", got, test.want, wirePresentationLabel(test.status, now))
			}
		})
	}
}

func TestWireLiveIndicatorIsBoundInInitialAndPulseContracts(t *testing.T) {
	sourceBytes, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	source := string(sourceBytes)
	for _, want := range []string{
		`class="live-dot live-dot--bound"`,
		`data-gosx-live-bind="indicator"`,
		`{data.wire_indicator}`,
	} {
		if !strings.Contains(source, want) {
			t.Errorf("wire initial render missing %q", want)
		}
	}
	serverBytes, err := os.ReadFile("page.server.go")
	if err != nil {
		t.Fatal(err)
	}
	server := string(serverBytes)
	for _, want := range []string{`"wire_indicator":`, `"indicator":`, `wireLiveIndicator(`} {
		if !strings.Contains(server, want) {
			t.Errorf("wire server/pulse output missing %q", want)
		}
	}
}
