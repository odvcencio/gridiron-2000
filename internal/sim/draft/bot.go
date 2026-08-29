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
	jar, err := cookiejar.New(nil)
	if err != nil {
		// cookiejar.New cannot fail with nil Options today — its only error
		// path is a broken PublicSuffixList — but silently carrying on with
		// a nil jar would panic confusingly far from here, on the first
		// cookie read inside Socket. Fail loudly and immediately instead.
		panic(fmt.Sprintf("draft.New: cookiejar.New(nil): %v", err))
	}
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
	// csrfTagPattern and motifTagPattern match the whole <input ...> tag
	// rather than an anchored "name then value" sequence, so they find
	// name="csrf_token" (or name="motif") no matter where in the tag it
	// falls relative to its value attribute — a renderer is free to emit
	// attributes in either order.
	csrfTagPattern   = regexp.MustCompile(`<input\b[^>]*\bname="csrf_token"[^>]*>`)
	motifTagPattern  = regexp.MustCompile(`<input\b[^>]*\bname="motif"[^>]*>`)
	valueAttrPattern = regexp.MustCompile(`\bvalue="([^"]*)"`)
)

// csrfTokenFrom pulls the CSRF token out of a rendered page's hidden input.
func csrfTokenFrom(html string) string {
	return valueFromTag(csrfTagPattern.FindString(html))
}

// firstMotifFrom picks the first badge motif radio value on a rendered
// /join page, so a simulated signup does not have to know the motif catalog.
func firstMotifFrom(html string) string {
	return valueFromTag(motifTagPattern.FindString(html))
}

// valueFromTag extracts one <input ...>'s value attribute, wherever it
// falls within the tag.
func valueFromTag(tag string) string {
	if m := valueAttrPattern.FindStringSubmatch(tag); m != nil {
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
	body, _ := io.ReadAll(res.Body)
	var result ActionResult
	if err := json.Unmarshal(body, &result); err != nil {
		return ActionResult{}, fmt.Errorf("POST %s: status %d, decode: %w: %s", path, res.StatusCode, err, firstBytes(body, 200))
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

// Join claims an open seat through the real signup action, then re-reads
// /test/draft to learn which seat the server actually assigned (see
// State's doc comment for why the re-read, not the submitted team_id
// field, is authoritative).
func (b *Bot) Join(teamName string) error {
	html, err := b.get("/join")
	if err != nil {
		return err
	}
	if token := csrfTokenFrom(html); token != "" {
		b.csrf = token
	}
	if err := b.simpleAction("/join/__actions/signup-claim", map[string]string{"team_name": teamName, "motif": firstMotifFrom(html)}); err != nil {
		return err
	}
	_, err = b.State()
	return err
}

// InviteCoManager invites email as the caller's own team's co-manager
// (POST /team/__actions/co-invite). The caller must already hold a seat
// (b.TeamID set, normally by Join); the server re-checks that the caller
// is that seat's primary before touching the store.
func (b *Bot) InviteCoManager(email string) error {
	if _, err := b.get("/team"); err != nil { // refresh the session before a /team action
		return err
	}
	return b.simpleAction("/team/__actions/co-invite", map[string]string{"team_id": b.TeamID, "email": email})
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
	Started    bool   `json:"started"`
	Complete   bool   `json:"complete"`
	PickNumber int    `json:"pick_number"`
	OnClockID  string `json:"on_clock_id"`
	// ViewerTeamID is the requesting identity's own seat, if any. It is the
	// only reliable way a scenario learns its seat: the server ignores a
	// submitted team_id form field on every action and derives the acting
	// seat from the signed-in identity instead (actingTeam,
	// internal/league/service.go).
	ViewerTeamID  string           `json:"viewer_team_id"`
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

// State reads the current draft state from the harness endpoint. If the
// response names the caller's own seat (ViewerTeamID) and b.TeamID is not
// already set, State adopts it — the one place besides Join a scenario can
// learn its seat, for a bot built with New and driven straight to State
// without ever calling Join (for example a bot re-attaching to a seat an
// earlier process claimed).
func (b *Bot) State() (DraftState, error) {
	state, err := b.stateFrom("/test/draft")
	if err != nil {
		return state, err
	}
	if state.ViewerTeamID != "" && b.TeamID == "" {
		b.TeamID = state.ViewerTeamID
	}
	return state, nil
}

// NextPick finds an eligible player for whichever team the server currently
// has on the clock, scanning the first page, then K, then DST (the late
// rounds require a kicker or defense to keep the roster viable). Every
// row's draft_eligible reflects the ON-CLOCK team's roster fit, not the
// caller's: a scenario must confirm it is actually b.TeamID's turn (compare
// against State's OnClockID) before treating the returned player as its
// own legal pick.
func (b *Bot) NextPick() (string, error) {
	state, err := b.State()
	if err != nil {
		return "", err
	}
	if !state.Started || state.OnClockID == "" {
		return "", errors.New("draft not on the clock")
	}
	if id := state.EligiblePick(); id != "" {
		return id, nil
	}
	for _, path := range []string{"/test/draft?pos=K", "/test/draft?pos=DST"} {
		next, err := b.stateFrom(path)
		if err != nil {
			return "", err
		}
		if id := next.EligiblePick(); id != "" {
			return id, nil
		}
	}
	return "", errors.New("no eligible player on any page")
}

// draftLiveHubPath mirrors app/draft.DraftLiveHubPath. It is kept as a
// literal rather than an import: pulling in app/draft would drag the page
// program's own dependency tree (route, hub, and the rest of gosx's page
// machinery) into every binary that links this bot package.
const draftLiveHubPath = "/draft/live"

// Socket subscribes to the draft hub. Origin must match the server host and
// the session cookie must ride along so the auth gate before the upgrade passes.
func (b *Bot) Socket(since string) (*websocket.Conn, error) {
	base, err := url.Parse(b.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse base URL %q: %w", b.BaseURL, err)
	}
	target := *base
	switch base.Scheme {
	case "https":
		target.Scheme = "wss"
	default:
		target.Scheme = "ws"
	}
	target.Path = draftLiveHubPath
	query := url.Values{}
	query.Set("since", since)
	target.RawQuery = query.Encode()

	header := http.Header{"Origin": []string{b.BaseURL}, "X-Test-User": []string{b.identity()}}
	if cookies := b.client.Jar.Cookies(base); len(cookies) > 0 {
		// A Cookie request header is one line of "; "-joined pairs, not
		// repeated Cookie headers — send it that way rather than one
		// header.Add call per cookie.
		parts := make([]string, 0, len(cookies))
		for _, c := range cookies {
			parts = append(parts, c.Name+"="+c.Value)
		}
		header.Set("Cookie", strings.Join(parts, "; "))
	}
	conn, resp, err := websocket.DefaultDialer.Dial(target.String(), header)
	if err != nil {
		// resp is non-nil whenever the server answered but declined the
		// upgrade, so its status distinguishes 401 (not signed in) from
		// 404 (Origin mismatch or a wrong path) instead of collapsing
		// both into gorilla/websocket's generic "bad handshake".
		if resp != nil {
			return nil, fmt.Errorf("dial %s: %w (status %s)", target.String(), err, resp.Status)
		}
		return nil, fmt.Errorf("dial %s: %w", target.String(), err)
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
