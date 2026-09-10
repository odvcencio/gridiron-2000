package league

import "testing"

// TestInjuryResolverTakesTheWorseNews is the 2026-09-10 canonical-status
// fix. Two feeds report a player's availability — the Tank01 pool field
// and the mirrored nflverse weekly report — and before this they were
// consumed by two different halves of the app: the visible chip read the
// pool, the IR gate read the weekly report, and nothing reconciled them.
// A late scratch the weekly report knew about showed no indicator at all.
func TestInjuryResolverTakesTheWorseNews(t *testing.T) {
	weekly := func(designation string) InjuryDesignationSource {
		return func(name, position, nflTeam string) (string, bool) {
			if designation == "" {
				return "", false
			}
			return designation, true
		}
	}
	cases := []struct {
		name       string
		poolField  string
		weeklySays string
		wantCode   string
		wantSource string
	}{
		{
			// The reported case: the pool has nothing, the weekly report
			// has the scratch. It must show.
			name:      "late scratch only the weekly report knows",
			poolField: "", weeklySays: "Out", wantCode: "O", wantSource: "nflverse weekly report",
		},
		{
			// The reverse: only the pool knows.
			name:      "only the pool knows",
			poolField: "Questionable", weeklySays: "", wantCode: "Q", wantSource: "Tank01 pool",
		},
		{
			// They disagree: the worse news wins, because a manager
			// setting a lineup is better served by it.
			name: "weekly is worse", poolField: "Questionable", weeklySays: "Out", wantCode: "O", wantSource: "nflverse weekly report",
		},
		{
			name: "pool is worse", poolField: "Out", weeklySays: "Questionable", wantCode: "O", wantSource: "Tank01 pool",
		},
		{
			name: "both silent", poolField: "", weeklySays: "", wantCode: "", wantSource: "",
		},
		{
			// An unrecognized word is kept, not dropped: a real feed said
			// something and hiding it would be worse than showing a word
			// the table does not know yet.
			name: "unknown designation survives", poolField: "Limited", weeklySays: "", wantCode: "Limited", wantSource: "Tank01 pool",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveInjury(Player{Name: "Brock Bowers", Position: "TE", NFLTeam: "LV", Injury: tc.poolField}, weekly(tc.weeklySays))
			if got.Code != tc.wantCode {
				t.Errorf("code = %q, want %q", got.Code, tc.wantCode)
			}
			if got.Source != tc.wantSource {
				t.Errorf("source = %q, want %q", got.Source, tc.wantSource)
			}
		})
	}
}

// TestInjuryWarnsOnlyForRealAbsence pins which designations warn a manager
// with the player in a starting slot. Questionable does not: that player
// can still play.
func TestInjuryWarnsOnlyForRealAbsence(t *testing.T) {
	for _, tc := range []struct {
		designation string
		wantWarn    bool
	}{
		{"Questionable", false},
		{"Doubtful", true},
		{"Out", true},
		{"Injured reserve", true},
		{"Suspended", true},
		{"", false},
	} {
		if got := NormalizeInjury(tc.designation, "test").Warns(); got != tc.wantWarn {
			t.Errorf("%q warns = %v, want %v", tc.designation, got, tc.wantWarn)
		}
	}
}

// TestInjuryChipCodesAreShort keeps the chip legible: the codes a lineup
// row has room for, not a full word.
func TestInjuryChipCodesAreShort(t *testing.T) {
	for designation, want := range map[string]string{
		"Out": "O", "Doubtful": "D", "Questionable": "Q",
		"Injured reserve": "IR", "Suspended": "SUS",
	} {
		if got := NormalizeInjury(designation, "test").Code; got != want {
			t.Errorf("%q code = %q, want %q", designation, got, want)
		}
	}
}
