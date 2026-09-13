package fantasy

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
)

func TestParseBoxScoreRetainsExplicitZeroScoringRows(t *testing.T) {
	for _, tc := range []struct {
		name, group, rawKey, normalized, value string
	}{
		{"passing", "Passing", "passYds", "passYds", `"0"`},
		{"rushing", "Rushing", "rushYds", "rushYds", `0`},
		{"receiving", "Receiving", "receptions", "receptions", `" 0 "`},
		{"kicking", "Kicking", "fgMade", "fgMade", `"-0"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := []byte(fmt.Sprintf(`{"body":{"gameStatus":"Completed","gameStatusCode":"2","playerStats":{"zero":{"longName":"Scoreless Player","teamAbv":"DET",%q:{%q:%s}}}}}`, tc.group, tc.rawKey, tc.value))
			box := ParseBoxScore(raw)
			line, present := box.Players["zero"]
			if !box.Final || !present || line.Name != "Scoreless Player" || line.Team != "DET" {
				t.Fatalf("confirmed final zero row lost: final=%v present=%v line=%+v", box.Final, present, line)
			}
			if value, known := line.Stats[tc.normalized]; !known || value != 0 {
				t.Fatalf("explicit zero %q = %v (known=%v)", tc.normalized, value, known)
			}
		})
	}
}

func TestParseBoxScoreDoesNotInventZeroScoringRows(t *testing.T) {
	for _, fields := range []string{
		``,
		`,"Receiving":{}`,
		`,"Receiving":{"receptions":null}`,
		`,"Receiving":{"receptions":""}`,
		`,"Receiving":{"receptions":"unavailable"}`,
		`,"Receiving":{"receptions":false}`,
		`,"Receiving":{"receptions":[]}`,
		`,"Receiving":{"receptions":"NaN"}`,
		`,"Receiving":{"receptions":"Infinity"}`,
		`,"Receiving":{"receptions":"-Inf"}`,
		`,"Defense":{"sacks":"0","tackles":"1"}`,
		`,"Kicking":{"kickReturnYds":"0","kickReturnTD":"0"}`,
		`,"Punting":{"puntReturnYds":"0","puntReturnTD":"0"}`,
		`,"fumblesLost":"0","Fumbles":{"fumblesLost":"0"}`,
	} {
		t.Run(fields, func(t *testing.T) {
			raw := []byte(`{"body":{"gameStatus":"Completed","gameStatusCode":"2","playerStats":{"missing":{"longName":"Unconfirmed Player"` + fields + `}}}}`)
			if box := ParseBoxScore(raw); len(box.Players) != 0 {
				t.Fatalf("missing or invalid scoring evidence fabricated a row: %+v", box.Players)
			}
		})
	}
}

func TestPreseasonBoxScoreStillDropsExplicitZeroScoringFields(t *testing.T) {
	raw := json.RawMessage(`{"gameStatus":"Completed","gameStatusCode":"2","playerStats":{"zero":{"Receiving":{"receptions":"0","recYds":0}},"mixed":{"Rushing":{"rushYds":"-2","rushTD":"0"},"Receiving":{"receptions":"1","recYds":"0"}},"returner":{"Punting":{"puntReturnTD":"1"}}}}`)
	got, final := parsePreseasonBoxScore(raw)
	want := map[string]map[string]float64{
		"mixed":    {"rushYds": -2, "receptions": 1},
		"returner": {"returnTD": 1},
	}
	if !final || !reflect.DeepEqual(got, want) {
		t.Fatalf("preseason offense/kicking semantics changed: final=%v stats=%v", final, got)
	}
}
