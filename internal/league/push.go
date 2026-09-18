package league

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

// PushSubscription is one browser's Web Push subscription as the browser
// reports it: the push service endpoint and the two public keys the
// service needs to encrypt for that device. It carries nothing the league
// minted.
type PushSubscription struct {
	Endpoint  string    `json:"endpoint"`
	P256dh    string    `json:"p256dh"`
	Auth      string    `json:"auth"`
	UserAgent string    `json:"userAgent,omitempty"`
	AddedAt   time.Time `json:"addedAt"`
}

// PushConfig is the operator's VAPID public key, the half the browser
// needs to subscribe. The private key never enters the league package: it
// lives inside the PushSender closure the app wires (app_build.go).
type PushConfig struct {
	PublicKey string
}

// PushPayload is the compact body a device shows: a title, one line, and
// the page to open. The email carries the full rendering.
type PushPayload struct {
	Title    string `json:"title"`
	Body     string `json:"body"`
	URL      string `json:"url"`
	Category string `json:"category"`
	Key      string `json:"key"`
}

// PushSender delivers one payload to one device. It returns
// ErrPushSubscriptionGone when the push service reports the subscription
// no longer exists (404/410), which drops the device.
type PushSender func(sub PushSubscription, payload PushPayload) error

// ErrPushSubscriptionGone is the sender's signal that a device
// unsubscribed or expired.
var ErrPushSubscriptionGone = errors.New("push subscription gone")

const pushBodyMaxRunes = 140

// SetPushConfig wires push. A nil sender or an empty public key leaves
// push off: /settings hides the device panel and the send path skips it.
func (s *Service) SetPushConfig(cfg PushConfig, sender PushSender) {
	s.poolMu.Lock()
	defer s.poolMu.Unlock()
	s.pushCfg = cfg
	s.pushSend = sender
}

func (s *Service) pushConfigured() (PushConfig, PushSender, bool) {
	s.poolMu.Lock()
	defer s.poolMu.Unlock()
	if s.pushSend == nil || strings.TrimSpace(s.pushCfg.PublicKey) == "" {
		return PushConfig{}, nil, false
	}
	return s.pushCfg, s.pushSend, true
}

// SetPushSubscription records or refreshes one device for a member, keyed
// by its endpoint.
func (s *Store) SetPushSubscription(email string, sub PushSubscription, now time.Time) error {
	email = strings.ToLower(strings.TrimSpace(email))
	sub.Endpoint = strings.TrimSpace(sub.Endpoint)
	if email == "" || sub.Endpoint == "" {
		return errors.New("a signed-in member and a push endpoint are required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeErrorLocked(); err != nil {
		return err
	}
	if s.state.PushSubscriptions == nil {
		s.state.PushSubscriptions = map[string]map[string]PushSubscription{}
	}
	devices := s.state.PushSubscriptions[email]
	if devices == nil {
		devices = map[string]PushSubscription{}
		s.state.PushSubscriptions[email] = devices
	}
	if existing, ok := devices[sub.Endpoint]; ok && !existing.AddedAt.IsZero() {
		sub.AddedAt = existing.AddedAt
	} else {
		sub.AddedAt = now.UTC()
	}
	devices[sub.Endpoint] = sub
	return s.persistLocked(colScalars)
}

// RemovePushSubscription drops one device. An unknown endpoint is a no-op.
func (s *Store) RemovePushSubscription(email, endpoint string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	endpoint = strings.TrimSpace(endpoint)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeErrorLocked(); err != nil {
		return err
	}
	devices := s.state.PushSubscriptions[email]
	if _, ok := devices[endpoint]; !ok {
		return nil
	}
	delete(devices, endpoint)
	if len(devices) == 0 {
		delete(s.state.PushSubscriptions, email)
	}
	return s.persistLocked(colScalars)
}

// ClearPushSubscriptions drops every device a member registered.
func (s *Store) ClearPushSubscriptions(email string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeErrorLocked(); err != nil {
		return err
	}
	if _, ok := s.state.PushSubscriptions[email]; !ok {
		return nil
	}
	delete(s.state.PushSubscriptions, email)
	return s.persistLocked(colScalars)
}

// browserPushSubscription is the wire shape PushSubscription.toJSON()
// produces in the browser.
type browserPushSubscription struct {
	Endpoint string `json:"endpoint"`
	Keys     struct {
		P256dh string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
}

// RegisterPushSubscription stores the signed-in member's device from the
// browser's own subscription JSON. Push must be configured, the member
// must be admitted (the same gate the watchlist uses), and the payload
// must carry an https endpoint and both keys.
func (s *Service) RegisterPushSubscription(r *http.Request, raw, userAgent string) (string, error) {
	if _, _, ok := s.pushConfigured(); !ok {
		return "", errors.New("push notifications are not configured on this league")
	}
	state := s.store.Snapshot()
	email := s.watchViewerEmail(r, state)
	if email == "" {
		return "", errors.New("sign in as a league member to turn on push notifications")
	}
	var parsed browserPushSubscription
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &parsed); err != nil {
		return "", errors.New("the browser did not send a readable push subscription")
	}
	endpoint := strings.TrimSpace(parsed.Endpoint)
	if !strings.HasPrefix(endpoint, "https://") || parsed.Keys.P256dh == "" || parsed.Keys.Auth == "" {
		return "", errors.New("the browser did not send a complete push subscription")
	}
	sub := PushSubscription{Endpoint: endpoint, P256dh: parsed.Keys.P256dh, Auth: parsed.Keys.Auth, UserAgent: truncateRunes(strings.TrimSpace(userAgent), 200)}
	if err := s.store.SetPushSubscription(email, sub, s.clock()); err != nil {
		return "", err
	}
	return "Push notifications are on for this device.", nil
}

// ClearPushSubscriptionsFor turns push off on every device the signed-in
// member registered.
func (s *Service) ClearPushSubscriptionsFor(r *http.Request) (string, error) {
	state := s.store.Snapshot()
	email := s.watchViewerEmail(r, state)
	if email == "" {
		return "", errors.New("sign in as a league member to change push notifications")
	}
	if err := s.store.ClearPushSubscriptions(email); err != nil {
		return "", err
	}
	return "Push notifications are off on every device.", nil
}

// PushSettingsView is what /settings renders: whether push is configured,
// the public key the browser subscribes with, and how many devices the
// viewer has registered.
type PushSettingsView struct {
	Available   bool
	PublicKey   string
	DeviceCount int
	SignedIn    bool
}

func (s *Service) PushSettingsView(r *http.Request) PushSettingsView {
	cfg, _, ok := s.pushConfigured()
	view := PushSettingsView{Available: ok, PublicKey: cfg.PublicKey}
	state := s.store.Snapshot()
	email := s.watchViewerEmail(r, state)
	view.SignedIn = email != ""
	view.DeviceCount = len(state.PushSubscriptions[email])
	return view
}

// pushURLForCategory is the page a tap opens for a category.
func pushURLForCategory(category string) string {
	switch category {
	case categoryPickem:
		return "/pickem"
	case categoryLineups:
		return "/team"
	case categoryTransactions:
		return "/players"
	case categoryWeeklyRecap:
		return "/matchups"
	case categoryDraftReminders, categoryDraftLive, categoryDraftRecap:
		return "/draft"
	default:
		return "/"
	}
}

func truncateRunes(text string, max int) string {
	if utf8.RuneCountInString(text) <= max {
		return text
	}
	runes := []rune(text)
	return string(runes[:max])
}

// pushNotify fans one rendered notification out to every device the
// member registered, off the request path. A device the push service
// reports gone is dropped; any other failure is logged and the device
// kept, so one flaky delivery never unsubscribes a phone.
func (s *Service) pushNotify(email string, rn renderedNotification, url string) {
	_, send, ok := s.pushConfigured()
	if !ok {
		return
	}
	email = strings.ToLower(strings.TrimSpace(email))
	devices := s.store.Snapshot().PushSubscriptions[email]
	if len(devices) == 0 {
		return
	}
	body := rn.Text
	if at := strings.IndexByte(body, '\n'); at >= 0 {
		body = body[:at]
	}
	payload := PushPayload{Title: rn.Subject, Body: truncateRunes(strings.TrimSpace(body), pushBodyMaxRunes), URL: url, Category: rn.Category, Key: rn.Key}
	for _, sub := range devices {
		s.pushWG.Add(1)
		go func(sub PushSubscription) {
			defer s.pushWG.Done()
			err := send(sub, payload)
			switch {
			case err == nil:
			case errors.Is(err, ErrPushSubscriptionGone):
				if removeErr := s.store.RemovePushSubscription(email, sub.Endpoint); removeErr != nil {
					log.Printf("push: drop gone device for %s: %v", email, removeErr)
				}
			default:
				log.Printf("push: %s %s: %v", rn.Category, sub.Endpoint, err)
			}
		}(sub)
	}
}

// waitForPush blocks until every in-flight push send returns. Tests and
// shutdown use it; the request path never does.
func (s *Service) waitForPush() {
	s.pushWG.Wait()
}
