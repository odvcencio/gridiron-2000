package fantasy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
)

// tank01Client speaks the Tank01 RapidAPI surface. Every response arrives as
// {"statusCode": <int>, "body": <payload>}; the payload shapes drift between
// endpoints and releases, so every parser below is tolerant of strings where
// numbers are expected, missing keys, and list-versus-map containers.
type tank01Client struct {
	host     string
	apiKey   string
	baseURL  string
	client   *http.Client
	maxBody  int64
	requests atomic.Int64
}

func (t *tank01Client) get(ctx context.Context, endpoint string, params map[string]string) (json.RawMessage, error) {
	query := url.Values{}
	for key, value := range params {
		if value != "" {
			query.Set(key, value)
		}
	}
	// baseURL, when set (TANK01_BASE_URL), points every request at a
	// shared caching relay (cmd/statrelay) instead of RapidAPI directly;
	// it already carries its own scheme, so it replaces the hardcoded
	// "https://" + host form entirely rather than composing with it. With
	// no baseURL this is byte-identical to the original construction.
	var requestURL string
	if t.baseURL != "" {
		requestURL = t.baseURL + "/" + endpoint
	} else {
		requestURL = "https://" + t.host + "/" + endpoint
	}
	if encoded := query.Encode(); encoded != "" {
		requestURL += "?" + encoded
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, err
	}
	// The RapidAPI headers are only meaningful — and only sent — when a
	// key is configured. In relay mode (baseURL set) the relay holds the
	// real key and injects its own headers upstream, so a league instance
	// pointed at the relay needs no key at all; sending an empty
	// x-rapidapi-key would be actively wrong (RapidAPI would reject it),
	// not merely redundant.
	if t.apiKey != "" {
		request.Header.Set("x-rapidapi-key", t.apiKey)
		request.Header.Set("x-rapidapi-host", t.host)
	}
	request.Header.Set("Accept", "application/json")
	response, err := t.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	t.requests.Add(1)
	raw, err := io.ReadAll(io.LimitReader(response.Body, t.maxBody+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > t.maxBody {
		return nil, fmt.Errorf("%s: response exceeds %d bytes", endpoint, t.maxBody)
	}
	if response.StatusCode != http.StatusOK {
		return nil, &HTTPStatusError{Endpoint: endpoint, Status: response.StatusCode}
	}
	return unwrapEnvelope(raw), nil
}

// HTTPStatusError reports a non-200 upstream reply. The live poller opens
// its circuit on a 429 through errors.As.
type HTTPStatusError struct {
	Endpoint string
	Status   int
}

func (e *HTTPStatusError) Error() string { return fmt.Sprintf("%s: HTTP %d", e.Endpoint, e.Status) }

// unwrapEnvelope returns the payload inside {"statusCode":..,"body":..} or the
// raw document when the envelope is absent.
func unwrapEnvelope(raw []byte) json.RawMessage {
	var envelope struct {
		Body json.RawMessage `json:"body"`
	}
	if err := json.Unmarshal(raw, &envelope); err == nil && len(envelope.Body) > 0 {
		return envelope.Body
	}
	return raw
}

var fantasyPositions = map[string]string{
	"QB": "QB", "RB": "RB", "WR": "WR", "TE": "TE",
	"PK": "K", "K": "K", "DST": "DST", "DEF": "DST", "D/ST": "DST",
	// P: punters (roster-ops spec section 4.1.2 / WP-R0). The live feed
	// carries 59 punters with pos "P" (verified live); they arrive with
	// espnHeadshot and jerseyNum populated but no ADP and no projections —
	// mergePool's projection-descending sort and ADPRank>0 display guards
	// (service.go) already handle a zero-ADP, zero-projection player
	// honestly, so no further change was needed there.
	"P": "P",
}

// parsePlayerList maps playerID to a base Player for fantasy positions.
func parsePlayerList(raw json.RawMessage) map[string]Player {
	var entries []map[string]any
	if err := json.Unmarshal(raw, &entries); err != nil {
		return map[string]Player{}
	}
	players := make(map[string]Player, len(entries)/4)
	for _, entry := range entries {
		id := flexString(entry["playerID"])
		position, ok := fantasyPositions[strings.ToUpper(flexString(entry["pos"]))]
		if id == "" || !ok {
			continue
		}
		name := flexString(entry["longName"])
		if name == "" {
			name = flexString(entry["espnName"])
		}
		if name == "" {
			continue
		}
		player := Player{
			ID:       id,
			Name:     name,
			Position: position,
			NFLTeam:  strings.ToUpper(flexString(entry["team"])),
		}
		if jersey := flexString(entry["jerseyNum"]); jersey != "" && jersey != "0" {
			player.Jersey = jersey
		}
		if headshot := flexString(entry["espnHeadshot"]); strings.HasPrefix(headshot, "https://") {
			player.Headshot = headshot
		}
		if injury, ok := entry["injury"].(map[string]any); ok {
			designation := flexString(injury["designation"])
			if designation != "" && !strings.EqualFold(designation, "healthy") {
				player.Injury = designation
			}
		}
		player.Exp = flexString(entry["exp"])
		// draftInfo carries the player's real NFL draft slot (round, pick)
		// straight on the same getNFLPlayerList row — no second Tank01 call,
		// no ID cross-reference against another provider. pick is the
		// OVERALL pick number (verified live 2026-08-18: round 2 begins at
		// pick 33). Absent entirely for an undrafted free agent; DraftCapital
		// (model.go) is the one place that decides whether a nonzero value
		// here is a meaningful "presumed usage" signal for THIS season.
		if draftInfo, ok := entry["draftInfo"].(map[string]any); ok {
			player.DraftRound = flexInt(draftInfo["round"])
			player.DraftPick = flexInt(draftInfo["pick"])
		}
		players[id] = player
	}
	return players
}

type adpEntry struct {
	PlayerID string
	ADP      float64
	Name     string
	Team     string
	Position string
}

// parseADP accepts {"adpList":[...]} or a bare list and keeps entries in a
// deterministic ascending ADP order.
func parseADP(raw json.RawMessage) []adpEntry {
	var wrapper struct {
		ADPList []map[string]any `json:"adpList"`
	}
	entries := wrapper.ADPList
	if err := json.Unmarshal(raw, &wrapper); err != nil || len(wrapper.ADPList) == 0 {
		var bare []map[string]any
		if err := json.Unmarshal(raw, &bare); err != nil {
			return nil
		}
		entries = bare
	} else {
		entries = wrapper.ADPList
	}
	out := make([]adpEntry, 0, len(entries))
	for _, entry := range entries {
		id := flexString(entry["playerID"])
		adp := flexFloat(entry["overallADP"])
		if adp == 0 {
			adp = flexFloat(entry["adp"])
		}
		if adp == 0 {
			adp = flexFloat(entry["averagePick"])
		}
		position := strings.ToUpper(flexString(entry["pos"]))
		if position == "" {
			// The live feed carries position only inside posADP ("DST1", "K12").
			position = strings.ToUpper(strings.TrimRight(flexString(entry["posADP"]), "0123456789"))
		}
		team := strings.ToUpper(flexString(entry["team"]))
		if team == "" {
			team = strings.ToUpper(flexString(entry["teamAbv"]))
		}
		if id == "" {
			// Team defenses have no playerID; derive a stable one.
			if normalized, ok := fantasyPositions[position]; ok && team != "" {
				id = normalized + "-" + team
			}
		}
		if id == "" || adp <= 0 {
			continue
		}
		out = append(out, adpEntry{
			PlayerID: id,
			ADP:      adp,
			Name:     flexString(entry["longName"]),
			Team:     team,
			Position: position,
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ADP < out[j].ADP })
	return out
}

// projEntry is one player's projected fantasy points plus the flattened stat
// line the projection was built from.
type projEntry struct {
	Points float64
	Stats  map[string]float64
}

// passingStatKeys maps a raw Passing group field to its normalized stat key.
var passingStatKeys = map[string]string{
	"passYds":         "passYds",
	"passTD":          "passTD",
	"int":             "passInt",
	"passAttempts":    "passAttempts",
	"passCompletions": "passCompletions",
}

// rushingStatKeys maps a raw Rushing group field to its normalized stat key.
var rushingStatKeys = map[string]string{
	"rushYds": "rushYds",
	"rushTD":  "rushTD",
	"carries": "carries",
}

// receivingStatKeys maps a raw Receiving group field to its normalized stat key.
var receivingStatKeys = map[string]string{
	"receptions": "receptions",
	"targets":    "targets",
	"recYds":     "recYds",
	"recTD":      "recTD",
}

// parseProjections maps playerID to a projEntry for the scoring format. The
// live feed nests per-play-type stats under "Rushing", "Passing", and
// "Receiving" groups with string-encoded numbers; defense and kicker entries
// carry no groups at all.
func parseProjections(raw json.RawMessage, scoring string) map[string]projEntry {
	var wrapper struct {
		PlayerProjections json.RawMessage `json:"playerProjections"`
	}
	payload := raw
	if err := json.Unmarshal(raw, &wrapper); err == nil && len(wrapper.PlayerProjections) > 0 {
		payload = wrapper.PlayerProjections
	}
	out := map[string]projEntry{}
	var byID map[string]map[string]any
	if err := json.Unmarshal(payload, &byID); err == nil {
		for id, entry := range byID {
			if points := projectionPoints(entry, scoring); points > 0 {
				out[id] = projEntry{Points: points, Stats: projectionStats(entry)}
			}
		}
		return out
	}
	var list []map[string]any
	if err := json.Unmarshal(payload, &list); err == nil {
		for _, entry := range list {
			id := flexString(entry["playerID"])
			if id == "" {
				continue
			}
			if points := projectionPoints(entry, scoring); points > 0 {
				out[id] = projEntry{Points: points, Stats: projectionStats(entry)}
			}
		}
	}
	return out
}

func projectionPoints(entry map[string]any, scoring string) float64 {
	if defaults, ok := entry["fantasyPointsDefault"].(map[string]any); ok {
		key := map[string]string{"ppr": "PPR", "standard": "standard", "half_ppr": "halfPPR"}[scoring]
		if points := flexFloat(defaults[key]); points > 0 {
			return points
		}
	}
	return flexFloat(entry["fantasyPoints"])
}

// projectionStats flattens the Rushing, Passing, and Receiving groups plus
// the top-level fumblesLost into one normalized map. Zero values are dropped
// to keep the payload small; an entry with no groups (defense, kicker)
// returns an empty, non-nil map.
func projectionStats(entry map[string]any) map[string]float64 {
	stats := map[string]float64{}
	addGroup := func(groupKey string, keyMap map[string]string) {
		group, ok := entry[groupKey].(map[string]any)
		if !ok {
			return
		}
		for rawKey, normKey := range keyMap {
			if value := flexFloat(group[rawKey]); value != 0 {
				stats[normKey] = value
			}
		}
	}
	addGroup("Passing", passingStatKeys)
	addGroup("Rushing", rushingStatKeys)
	addGroup("Receiving", receivingStatKeys)
	if value := flexFloat(entry["fumblesLost"]); value != 0 {
		stats["fumblesLost"] = value
	}
	return stats
}

// parseNews maps a player key to the latest matching headline. The live feed
// carries no playerID on fantasy news, so headlines like
// "Keon Coleman: suffered a lower body injury" key on the name prefix; pool
// lookups try the ID first and the player name second.
func parseNews(raw json.RawMessage) map[string]string {
	var entries []map[string]any
	if err := json.Unmarshal(raw, &entries); err != nil {
		return map[string]string{}
	}
	out := map[string]string{}
	for _, entry := range entries {
		title := flexString(entry["title"])
		if title == "" {
			continue
		}
		key := flexString(entry["playerID"])
		if key == "" {
			if name, _, found := strings.Cut(title, ":"); found {
				key = strings.TrimSpace(name)
			}
		}
		if key == "" {
			continue
		}
		if _, seen := out[key]; !seen {
			out[key] = title
		}
	}
	return out
}

// parseTeamByes maps a team abbreviation to its bye week for the season.
func parseTeamByes(raw json.RawMessage, season int) map[string]int {
	var entries []map[string]any
	if err := json.Unmarshal(raw, &entries); err != nil {
		return map[string]int{}
	}
	seasonKey := strconv.Itoa(season)
	out := map[string]int{}
	for _, entry := range entries {
		team := strings.ToUpper(flexString(entry["teamAbv"]))
		if team == "" {
			continue
		}
		byes, ok := entry["byeWeeks"].(map[string]any)
		if !ok {
			continue
		}
		weeks, ok := byes[seasonKey]
		if !ok {
			for _, value := range byes {
				weeks = value
				break
			}
		}
		switch typed := weeks.(type) {
		case []any:
			if len(typed) > 0 {
				out[team] = flexInt(typed[0])
			}
		default:
			out[team] = flexInt(typed)
		}
	}
	return out
}

func flexString(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case json.Number:
		return typed.String()
	default:
		return ""
	}
}

func flexFloat(value any) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		if err != nil {
			return 0
		}
		return parsed
	case json.Number:
		parsed, err := typed.Float64()
		if err != nil {
			return 0
		}
		return parsed
	default:
		return 0
	}
}

func flexInt(value any) int {
	return int(flexFloat(value))
}
