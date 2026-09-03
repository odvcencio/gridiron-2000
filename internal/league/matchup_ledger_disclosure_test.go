package league

import "testing"

// TestLedgerLineupTextMapsEveryProvenanceToken covers wave-8 audit item 3:
// every Provenance token StarterLedgerRow can carry renders as a labelled,
// plain-word "Lineup: ..." segment, never the bare enum string.
func TestLedgerLineupTextMapsEveryProvenanceToken(t *testing.T) {
	cases := map[string]string{
		"empty":       "Lineup: no player in this slot",
		"pinned":      "Lineup: locked for the closed week",
		"auto-filled": "Lineup: auto-filled",
		"explicit":    "Lineup: set by the manager",
	}
	for provenance, want := range cases {
		if got := ledgerLineupText(provenance); got != want {
			t.Errorf("ledgerLineupText(%q) = %q, want %q", provenance, got, want)
		}
	}
}

// TestLedgerStatsTextOmitsEmptyAndCarriesItsOwnSeparator covers wave-8
// audit item 3: the "empty slot" JoinState carries no Stats segment at
// all (Lineup already said so) — no dangling " · " left behind — while
// every other JoinState renders its own leading " · Stats: ..." segment.
func TestLedgerStatsTextOmitsEmptyAndCarriesItsOwnSeparator(t *testing.T) {
	if got := ledgerStatsText("empty"); got != "" {
		t.Fatalf("ledgerStatsText(empty) = %q, want an empty string (no dangling separator)", got)
	}
	cases := map[string]string{
		"matched":           " · Stats: scored",
		"missing-join":      " · Stats: no stat row yet",
		"stats-unavailable": " · Stats: source unavailable",
		"stats-empty":       " · Stats: none yet",
	}
	for joinState, want := range cases {
		if got := ledgerStatsText(joinState); got != want {
			t.Errorf("ledgerStatsText(%q) = %q, want %q", joinState, got, want)
		}
	}
}

// TestLedgerSourceTextOmitsEmptyAndCarriesItsOwnSeparator covers wave-8
// audit item 3: an unmatched row (Source == "") renders no Source
// segment and no dangling separator; a matched row's Source renders its
// own leading " · Source: ..." segment.
func TestLedgerSourceTextOmitsEmptyAndCarriesItsOwnSeparator(t *testing.T) {
	if got := ledgerSourceText(""); got != "" {
		t.Fatalf("ledgerSourceText(\"\") = %q, want an empty string (no dangling separator)", got)
	}
	cases := map[string]string{
		StatSourceLive:       " · Source: live box score",
		StatSourceLiveFinal:  " · Source: final box score",
		StatSourceLedgerLive: " · Source: ledger + live box score",
		StatSourceLedger:     " · Source: weekly ledger",
	}
	for source, want := range cases {
		if got := ledgerSourceText(source); got != want {
			t.Errorf("ledgerSourceText(%q) = %q, want %q", source, got, want)
		}
	}
}
