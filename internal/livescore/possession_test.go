package livescore

import (
	"os"
	"path/filepath"
	"testing"

	"gridiron-2000/internal/fantasy"
)

// TestExtractPossessionAgainstRealFixtures covers the spec's own named
// evidence: internal/fantasy/testdata/box-20250904_DAL-PHI.json and
// preseason-boxscore-sample.json really do carry the lineScore shape
// (both sides "False", since both are completed games) — the tolerant
// seam's job is to read that honestly as unknown, not to fabricate a
// team merely because the shape is present.
func TestExtractPossessionAgainstRealFixtures(t *testing.T) {
	for _, name := range []string{"box-20250904_DAL-PHI.json", "preseason-boxscore-sample.json"} {
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("..", "fantasy", "testdata", name))
			if err != nil {
				t.Fatal(err)
			}
			box := fantasy.ParseBoxScore(raw)
			if box.Raw == nil {
				t.Fatal("fixture parsed with no Raw payload")
			}
			if _, ok := box.Raw["lineScore"]; !ok {
				t.Fatal("fixture no longer carries the verified lineScore shape; update this test's own doc comment")
			}
			if team, known := ExtractPossession(box.Raw); known {
				t.Fatalf("a completed game's lineScore (both sides False) must read unknown, got (%q, true)", team)
			}
		})
	}
	// box-inprogress-sample.json is the synthetic in-progress fixture:
	// verified to carry NO possession signal of any shape at all.
	raw, err := os.ReadFile(filepath.Join("..", "fantasy", "testdata", "box-inprogress-sample.json"))
	if err != nil {
		t.Fatal(err)
	}
	box := fantasy.ParseBoxScore(raw)
	if !box.InProgress {
		t.Fatal("box-inprogress-sample.json must parse InProgress=true")
	}
	if team, known := ExtractPossession(box.Raw); known {
		t.Fatalf("box-inprogress-sample.json carries no possession field; got (%q, true)", team)
	}
}

// TestExtractPossessionFromLineScore covers the one shape verified
// against real Tank01 box-score fixtures in this repo: a "lineScore"
// object with per-side "currentlyInPossession" strings.
func TestExtractPossessionFromLineScore(t *testing.T) {
	awayPossessing := map[string]any{
		"away": "BAL", "home": "BUF",
		"lineScore": map[string]any{
			"away": map[string]any{"currentlyInPossession": "True"},
			"home": map[string]any{"currentlyInPossession": "False"},
		},
	}
	team, known := ExtractPossession(awayPossessing)
	if !known || team != "BAL" {
		t.Fatalf("away possessing = (%q, %v), want (BAL, true)", team, known)
	}

	homePossessing := map[string]any{
		"away": "BAL", "home": "BUF",
		"lineScore": map[string]any{
			"away": map[string]any{"currentlyInPossession": "False"},
			"home": map[string]any{"currentlyInPossession": "True"},
		},
	}
	team, known = ExtractPossession(homePossessing)
	if !known || team != "BUF" {
		t.Fatalf("home possessing = (%q, %v), want (BUF, true)", team, known)
	}
}

// TestExtractPossessionToleratesAMissingSideObject covers a payload that
// omits the non-possessing side's lineScore object entirely (only the
// possessing side's own flag needs to resolve unambiguously true, with
// no side reading true) — not a guess, since sideCurrentlyInPossession
// reads a missing/nil side as simply not possessing, the same as an
// explicit "False".
func TestExtractPossessionToleratesAMissingSideObject(t *testing.T) {
	raw := map[string]any{
		"away": "BAL", "home": "BUF",
		"lineScore": map[string]any{
			"home": map[string]any{"currentlyInPossession": "True"},
		},
	}
	team, known := ExtractPossession(raw)
	if !known || team != "BUF" {
		t.Fatalf("missing away side object = (%q, %v), want (BUF, true)", team, known)
	}
}

// TestExtractPossessionUnknownOnCompletedGameShape mirrors the two
// fixtures GC-2b's spec names directly (box-20250904_DAL-PHI.json,
// preseason-boxscore-sample.json): a real lineScore shape whose two
// sides both read "False" (a completed game, so nobody has the ball).
// Never fabricated to either team.
func TestExtractPossessionUnknownOnCompletedGameShape(t *testing.T) {
	raw := map[string]any{
		"away": "ARI", "home": "LV",
		"lineScore": map[string]any{
			"away": map[string]any{"currentlyInPossession": "False", "teamAbv": "ARI"},
			"home": map[string]any{"currentlyInPossession": "False", "teamAbv": "LV"},
		},
	}
	if team, known := ExtractPossession(raw); known {
		t.Fatalf("both sides False must read unknown, got (%q, true)", team)
	}
}

// TestExtractPossessionAbsentPayload covers the tolerant seam's baseline
// case: the two fixtures GC-2b names for the live poller's in-progress
// synthetic test data (box-inprogress-sample.json) carry no possession
// signal of any shape at all.
func TestExtractPossessionAbsentPayload(t *testing.T) {
	if team, known := ExtractPossession(nil); known || team != "" {
		t.Fatalf("nil payload = (%q, %v), want (\"\", false)", team, known)
	}
	if team, known := ExtractPossession(map[string]any{"away": "AWY", "home": "HOM", "gameStatus": "Live - In Progress"}); known || team != "" {
		t.Fatalf("payload with no possession field = (%q, %v), want (\"\", false)", team, known)
	}
}

// TestExtractPossessionGarbageNeverGuesses covers a battery of malformed
// or contradictory shapes: every one must read unknown, never guessed.
func TestExtractPossessionGarbageNeverGuesses(t *testing.T) {
	cases := map[string]map[string]any{
		"lineScore not an object": {
			"lineScore": "garbage",
		},
		"both sides claim possession": {
			"away": "BAL", "home": "BUF",
			"lineScore": map[string]any{
				"away": map[string]any{"currentlyInPossession": "True"},
				"home": map[string]any{"currentlyInPossession": "True"},
			},
		},
		"currentlyInPossession is a number": {
			"away": "BAL", "home": "BUF",
			"lineScore": map[string]any{
				"away": map[string]any{"currentlyInPossession": 1.0},
				"home": map[string]any{"currentlyInPossession": 0.0},
			},
		},
		"away side possessing but away abbreviation missing": {
			"home": "BUF",
			"lineScore": map[string]any{
				"away": map[string]any{"currentlyInPossession": "True"},
				"home": map[string]any{"currentlyInPossession": "False"},
			},
		},
		"flat candidate key with a non-string value": {
			"possession": 42.0,
		},
		"flat candidate key blank": {
			"possession": "   ",
		},
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if team, known := ExtractPossession(raw); known {
				t.Fatalf("%s: got (%q, true), want unknown", name, team)
			}
		})
	}
}

// TestExtractPossessionFlatCandidateKeyFallback covers the speculative
// top-level fallback keys: checked only once the lineScore shape is
// absent, and NormalizeTeam-d the same way the verified shape is.
func TestExtractPossessionFlatCandidateKeyFallback(t *testing.T) {
	for _, key := range possessionCandidateKeys {
		raw := map[string]any{key: "lar"}
		team, known := ExtractPossession(raw)
		if !known || team != "LA" {
			t.Fatalf("candidate key %q = (%q, %v), want (LA, true)", key, team, known)
		}
	}
}

// TestExtractPossessionLineScoreWinsOverFlatCandidate covers ordering:
// the verified lineScore shape is checked first.
func TestExtractPossessionLineScoreWinsOverFlatCandidate(t *testing.T) {
	raw := map[string]any{
		"away": "BAL", "home": "BUF",
		"possession": "BUF",
		"lineScore": map[string]any{
			"away": map[string]any{"currentlyInPossession": "True"},
			"home": map[string]any{"currentlyInPossession": "False"},
		},
	}
	team, known := ExtractPossession(raw)
	if !known || team != "BAL" {
		t.Fatalf("lineScore shape did not win: got (%q, %v), want (BAL, true)", team, known)
	}
}
