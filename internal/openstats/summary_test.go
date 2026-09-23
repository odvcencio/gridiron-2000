package openstats

import "testing"

func TestNormalizePlayerKeyCollapsesStyleVariants(t *testing.T) {
	cases := []struct {
		name        string
		nameA, posA string
		nameB, posB string
	}{
		{
			name:  "punctuation and casing collapse",
			nameA: "Amon-Ra St. Brown", posA: "WR",
			nameB: "amon-ra st. brown", posB: "wr",
		},
		{
			name:  "apostrophe and spacing collapse",
			nameA: "Ka'imi Fairbairn", posA: "K",
			nameB: "kaimi  fairbairn", posB: "k",
		},
		{
			name:  "suffix punctuation collapses",
			nameA: "Michael Pittman Jr.", posA: "WR",
			nameB: "michael pittman jr", posB: "WR",
		},
		{name: "Andrew and Drew Ogletree", nameA: "Andrew Ogletree", posA: "TE", nameB: "Drew Ogletree", posB: "TE"},
		{name: "Marquise and Hollywood Brown", nameA: "Marquise Brown", posA: "WR", nameB: "Hollywood Brown", posB: "WR"},
		{name: "Josh and Joshua Palmer", nameA: "Josh Palmer", posA: "WR", nameB: "Joshua Palmer", posB: "WR"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			keyA := NormalizePlayerKey(tc.nameA, tc.posA)
			keyB := NormalizePlayerKey(tc.nameB, tc.posB)
			if keyA != keyB {
				t.Fatalf("keys did not collapse: %q vs %q", keyA, keyB)
			}
		})
	}
}

func TestNormalizePlayerKeyDistinguishesPosition(t *testing.T) {
	// Same normalized name, different position: must not collide, since a
	// team can carry a QB and a WR who share a surname-driven key otherwise.
	first := NormalizePlayerKey("Josh Allen", "QB")
	second := NormalizePlayerKey("Josh Allen", "LB")
	if first == second {
		t.Fatalf("expected distinct keys for different positions, got %q for both", first)
	}
}

func TestNormalizePlayerKeyExactFormat(t *testing.T) {
	got := NormalizePlayerKey("Amon-Ra St. Brown", "WR")
	want := "amonrastbrown|WR"
	if got != want {
		t.Fatalf("NormalizePlayerKey = %q, want %q", got, want)
	}
}

func TestNormalizePlayerKeyJoinsNamesWithAndWithoutSuffix(t *testing.T) {
	withSuffix := NormalizePlayerKey("Travis Etienne Jr.", "RB")
	withoutSuffix := NormalizePlayerKey("Travis Etienne", "RB")
	if withSuffix != withoutSuffix {
		t.Fatalf("suffix variants did not join: %q != %q", withSuffix, withoutSuffix)
	}
}
