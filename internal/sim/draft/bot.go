// Package draft drives simulated managers against a running Gridiron
// instance that was started with GRIDIRON_TEST_AUTH=1.
package draft

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// Bot is one simulated manager (or the commissioner) with its own session.
type Bot struct {
	BaseURL string
	Email   string
	Name    string
	TeamID  string
	client  *http.Client
	csrf    string
}

// New builds a bot pointed at baseURL with the given @sim.test identity.
func New(baseURL, email, name string) *Bot {
	jar, _ := cookiejar.New(nil)
	return &Bot{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Email:   email,
		Name:    name,
		client:  &http.Client{Jar: jar, Timeout: 10 * time.Second},
	}
}

func (b *Bot) identity() string { return b.Email + "|" + b.Name }

func (b *Bot) get(path string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, b.BaseURL+path, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("X-Test-User", b.identity())
	res, err := b.client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode >= 400 {
		return "", fmt.Errorf("GET %s = %d: %s", path, res.StatusCode, firstBytes(body, 200))
	}
	return string(body), nil
}

// firstBytes returns the first n bytes of body for an error message,
// marking a truncation so a 401 body is not mistaken for a complete 404 page.
func firstBytes(body []byte, n int) string {
	if len(body) <= n {
		return string(body)
	}
	return string(body[:n]) + "..."
}

var (
	csrfPattern  = regexp.MustCompile(`name="csrf_token"\s+value="([^"]+)"`)
	motifPattern = regexp.MustCompile(`name="motif"\s+value="([^"]+)"`)
)

// csrfTokenFrom pulls the CSRF token out of a rendered page's hidden input.
func csrfTokenFrom(html string) string {
	if m := csrfPattern.FindStringSubmatch(html); m != nil {
		return m[1]
	}
	return ""
}

// firstMotifFrom picks the first badge motif radio value on a rendered
// /join page, so a simulated signup does not have to know the motif catalog.
func firstMotifFrom(html string) string {
	if m := motifPattern.FindStringSubmatch(html); m != nil {
		return m[1]
	}
	return ""
}

// Prime loads /join once to establish the session cookie and CSRF token.
// /join always renders a form for a signed-in seatless viewer; /draft may not.
func (b *Bot) Prime() error {
	html, err := b.get("/join")
	if err != nil {
		return err
	}
	if b.csrf = csrfTokenFrom(html); b.csrf == "" {
		// an already-seated viewer is redirected to /team; take the token there
		html, err = b.get("/team")
		if err != nil {
			return err
		}
		b.csrf = csrfTokenFrom(html)
	}
	if b.csrf == "" {
		return errors.New("no csrf token on /join or /team")
	}
	return nil
}

// ActionResult is the JSON envelope gosx actions return.
type ActionResult struct {
	OK          bool              `json:"ok"`
	Message     string            `json:"message"`
	FieldErrors map[string]string `json:"fieldErrors"`
}

func (b *Bot) postAction(path string, fields map[string]string) (ActionResult, error) {
	form := url.Values{}
	for k, v := range fields {
		form.Set(k, v)
	}
	req, err := http.NewRequest(http.MethodPost, b.BaseURL+path, strings.NewReader(form.Encode()))
	if err != nil {
		return ActionResult{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Test-User", b.identity())
	req.Header.Set("X-CSRF-Token", b.csrf)
	res, err := b.client.Do(req)
	if err != nil {
		return ActionResult{}, err
	}
	defer res.Body.Close()
	var result ActionResult
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return ActionResult{}, fmt.Errorf("POST %s: status %d, decode: %w", path, res.StatusCode, err)
	}
	return result, nil
}

func (b *Bot) simpleAction(path string, fields map[string]string) error {
	if fields == nil {
		fields = map[string]string{}
	}
	r, err := b.postAction(path, fields)
	if err != nil {
		return err
	}
	if !r.OK {
		return fmt.Errorf("%s: %s %v", path, r.Message, r.FieldErrors)
	}
	return nil
}

// Join claims an open seat through the real signup action.
func (b *Bot) Join(teamName string) error {
	html, err := b.get("/join")
	if err != nil {
		return err
	}
	if token := csrfTokenFrom(html); token != "" {
		b.csrf = token
	}
	return b.simpleAction("/join/__actions/signup-claim", map[string]string{"team_name": teamName, "motif": firstMotifFrom(html)})
}

// ToggleReady flips the caller's seat between ready and not-ready.
func (b *Bot) ToggleReady() error {
	return b.simpleAction("/draft/__actions/toggle-ready", map[string]string{"team_id": b.TeamID})
}

// MakePick submits a pick for the caller's seat. The caller must be on the
// clock: the server may reject an off-clock pick, so this returns the raw
// ActionResult instead of collapsing a rejection into an error.
func (b *Bot) MakePick(playerID string) (ActionResult, error) {
	return b.postAction("/draft/__actions/make-pick", map[string]string{"team_id": b.TeamID, "player_id": playerID})
}

// StartDraft opens the draft room (commissioner only).
func (b *Bot) StartDraft() error {
	return b.simpleAction("/draft/__actions/draft-start", map[string]string{"confirm": "START"})
}

// Pause stops the pick clock (commissioner only).
func (b *Bot) Pause() error { return b.simpleAction("/draft/__actions/clock-pause", nil) }

// Resume restarts the pick clock (commissioner only).
func (b *Bot) Resume() error { return b.simpleAction("/draft/__actions/clock-resume", nil) }

// Extend adds seconds to the current pick's deadline (commissioner only).
func (b *Bot) Extend(seconds int, token string) error {
	return b.simpleAction("/draft/__actions/clock-extend", map[string]string{"seconds": strconv.Itoa(seconds), "current_pick_token": token})
}

// ForcePick immediately resolves the current on-clock pick (commissioner only).
func (b *Bot) ForcePick(token string) error {
	return b.simpleAction("/draft/__actions/clock-force-autopick", map[string]string{"confirm": "FORCE CURRENT PICK", "current_pick_token": token})
}

// Undo reverses the last-made pick (commissioner only).
func (b *Bot) Undo(previousToken string) error {
	if _, err := b.get("/admin"); err != nil { // refresh the session before an /admin action
		return err
	}
	return b.simpleAction("/admin/__actions/draft-undo", map[string]string{"confirm": "UNDO", "previous_pick_token": previousToken})
}

// AddToBoard puts a player on the seat's Big Board (the autopick queue).
func (b *Bot) AddToBoard(playerID string) error {
	if _, err := b.get("/board"); err != nil {
		return err
	}
	return b.simpleAction("/board/__actions/board-add", map[string]string{"player_id": playerID})
}

// Presence pings the league presence endpoint the way an active tab would.
func (b *Bot) Presence() error {
	_, err := b.get("/api/league/presence")
	return err
}

// DraftState is the harness JSON from GET /test/draft.
type DraftState struct {
	Started       bool             `json:"started"`
	Complete      bool             `json:"complete"`
	PickNumber    int              `json:"pick_number"`
	OnClockID     string           `json:"on_clock_id"`
	Token         string           `json:"current_pick_token"`
	PreviousToken string           `json:"previous_pick_token"`
	Clock         map[string]any   `json:"clock"`
	Picks         []map[string]any `json:"picks"`
	Available     []map[string]any `json:"available"`
	Teams         []map[string]any `json:"teams"`
}

// PickTeamID reads picks[i].team.id.
func PickTeamID(pick map[string]any) string {
	team, _ := pick["team"].(map[string]any)
	id, _ := team["id"].(string)
	return id
}

// PickPlayerID reads picks[i].player.id.
func PickPlayerID(pick map[string]any) string {
	player, _ := pick["player"].(map[string]any)
	id, _ := player["id"].(string)
	return id
}

// PickNumber reads picks[i].number.
func PickNumber(pick map[string]any) int {
	n, _ := pick["number"].(float64)
	return int(n)
}

// EligiblePick returns the first available row the server marks
// draft_eligible, or "" when this page has none.
func (s DraftState) EligiblePick() string {
	for _, row := range s.Available {
		if eligible, _ := row["draft_eligible"].(bool); eligible {
			id, _ := row["id"].(string)
			return id
		}
	}
	return ""
}

func (b *Bot) stateFrom(path string) (DraftState, error) {
	var state DraftState
	body, err := b.get(path)
	if err != nil {
		return state, err
	}
	err = json.Unmarshal([]byte(body), &state)
	return state, err
}

// State reads the current draft state from the harness endpoint.
func (b *Bot) State() (DraftState, error) { return b.stateFrom("/test/draft") }

// NextPick finds an eligible player: the first page, then K, then DST
// (the late rounds require a kicker or defense to keep the roster viable).
func (b *Bot) NextPick() (string, error) {
	for _, path := range []string{"/test/draft", "/test/draft?pos=K", "/test/draft?pos=DST"} {
		state, err := b.stateFrom(path)
		if err != nil {
			return "", err
		}
		if id := state.EligiblePick(); id != "" {
			return id, nil
		}
	}
	return "", errors.New("no eligible player on any page")
}

// Socket subscribes to the draft hub. Origin must match the server host and
// the session cookie must ride along so the auth gate before the upgrade passes.
func (b *Bot) Socket(since string) (*websocket.Conn, error) {
	target := "ws" + strings.TrimPrefix(b.BaseURL, "http") + "/draft/live?since=" + url.QueryEscape(since)
	header := http.Header{"Origin": []string{b.BaseURL}, "X-Test-User": []string{b.identity()}}
	if u, err := url.Parse(b.BaseURL); err == nil {
		for _, c := range b.client.Jar.Cookies(u) {
			header.Add("Cookie", c.String())
		}
	}
	conn, resp, err := websocket.DefaultDialer.Dial(target, header)
	if err != nil {
		// resp is non-nil whenever the server answered but declined the
		// upgrade, so its status distinguishes 401 (not signed in) from
		// 404 (Origin mismatch or a wrong path) instead of collapsing
		// both into gorilla/websocket's generic "bad handshake".
		if resp != nil {
			return nil, fmt.Errorf("dial %s: %w (status %s)", target, err, resp.Status)
		}
		return nil, fmt.Errorf("dial %s: %w", target, err)
	}
	return conn, nil
}

// HubEvent is the wire envelope the draft live hub sends.
type HubEvent struct {
	Event string          `json:"event"`
	Data  json.RawMessage `json:"data"`
	At    time.Time       `json:"-"`
}

// ReadEvent reads and decodes one hub event, failing after timeout.
func ReadEvent(conn *websocket.Conn, timeout time.Duration) (HubEvent, error) {
	_ = conn.SetReadDeadline(time.Now().Add(timeout))
	var event HubEvent
	if err := conn.ReadJSON(&event); err != nil {
		return event, err
	}
	event.At = time.Now()
	return event, nil
}

// Fingerprint reads /api/league/version.
func (b *Bot) Fingerprint() (string, error) {
	body, err := b.get("/api/league/version")
	if err != nil {
		return "", err
	}
	var v struct {
		Fingerprint string `json:"fingerprint"`
	}
	err = json.Unmarshal([]byte(body), &v)
	return v.Fingerprint, err
}
