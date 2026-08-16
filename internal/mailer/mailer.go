// Package mailer sends invite email through Resend or SMTP, as plain text or
// as a multipart text+HTML message. It stays optional: without credentials,
// Config.Enabled reports false and callers fall back to a mailto: link
// instead of a live send. When both are configured, Resend wins.
package mailer

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/smtp"
	"os"
	"sort"
	"strings"
	"time"
)

const defaultResendURL = "https://api.resend.com/emails"

// Config holds the mail transport settings. Build one with FromEnv.
type Config struct {
	Host string
	Port string
	User string
	Pass string
	From string

	ResendKey  string
	ResendFrom string
	resendURL  string
}

// FromEnv reads transport settings from the environment. Resend:
// RESEND_API_KEY and RESEND_FROM. SMTP: SMTP_HOST, SMTP_PORT (default
// "587"), SMTP_USER, SMTP_PASS, SMTP_FROM (default SMTP_USER).
func FromEnv() Config {
	port := strings.TrimSpace(os.Getenv("SMTP_PORT"))
	if port == "" {
		port = "587"
	}
	user := strings.TrimSpace(os.Getenv("SMTP_USER"))
	from := strings.TrimSpace(os.Getenv("SMTP_FROM"))
	if from == "" {
		from = user
	}
	return Config{
		Host:       strings.TrimSpace(os.Getenv("SMTP_HOST")),
		Port:       port,
		User:       user,
		Pass:       strings.TrimSpace(os.Getenv("SMTP_PASS")),
		From:       from,
		ResendKey:  strings.TrimSpace(os.Getenv("RESEND_API_KEY")),
		ResendFrom: strings.TrimSpace(os.Getenv("RESEND_FROM")),
	}
}

// Enabled reports whether Config carries enough credentials for either
// transport: a Resend key with a from address, or full SMTP credentials.
func (c Config) Enabled() bool {
	if c.ResendKey != "" && c.resendFromAddress() != "" {
		return true
	}
	return c.Host != "" && c.User != "" && c.Pass != ""
}

func (c Config) resendFromAddress() string {
	if c.ResendFrom != "" {
		return c.ResendFrom
	}
	return c.From
}

// Send delivers a message to one recipient. text is required; html is
// optional and, when given, sends alongside text as a multipart alternative
// so mail clients render the designed body but keep a plain-text fallback.
// Resend is preferred when configured; SMTP is the fallback. It fails fast
// when the config is not Enabled, so callers may check Send's error instead
// of calling Enabled twice. Send is a thin wrapper around SendMessage; use
// SendMessage directly for Resend tags, an idempotency key, or a reply-to
// address.
func (c Config) Send(to, subject, text, html string) error {
	return c.SendMessage(Message{To: to, Subject: subject, Text: text, HTML: html})
}

// Message extends Send with optional Resend features (design spec section
// 6.5). Tags and IdempotencyKey are Resend-only; the SMTP path ignores
// both but still honors ReplyTo.
type Message struct {
	To, Subject, Text, HTML string
	// Tags become the Resend "tags" array, e.g. {"category":"on_the_clock"}.
	Tags map[string]string
	// IdempotencyKey becomes the Resend "Idempotency-Key" header. The
	// caller's persisted ledger remains the durable send guard; this
	// header is defense-in-depth against a retry after a lost response.
	IdempotencyKey string
	// ReplyTo becomes the Reply-To header on both transports.
	ReplyTo string
}

// SendMessage delivers m to one recipient. Resend is preferred when
// configured; SMTP is the fallback. It fails fast when the config is not
// Enabled.
func (c Config) SendMessage(m Message) error {
	if c.ResendKey != "" && c.resendFromAddress() != "" {
		return c.sendResend(m)
	}
	if !c.Enabled() {
		return fmt.Errorf("mailer: no transport configured (need RESEND_API_KEY+RESEND_FROM or SMTP_HOST/USER/PASS)")
	}
	message := c.buildMessage(m.To, m.Subject, m.Text, m.HTML, m.ReplyTo)
	addr := c.Host + ":" + c.Port
	auth := smtp.PlainAuth("", c.User, c.Pass, c.Host)
	if err := smtp.SendMail(addr, auth, c.From, []string{m.To}, message); err != nil {
		return fmt.Errorf("mailer: send to %s: %w", m.To, err)
	}
	return nil
}

// sendResend posts the message to the Resend HTTP API. The "html" key is
// omitted entirely when html is empty, so a plain-text send stays plain.
// "tags" is included only when Tags is non-empty; the Idempotency-Key
// header is set only when IdempotencyKey is non-empty.
func (c Config) sendResend(m Message) error {
	endpoint := c.resendURL
	if endpoint == "" {
		endpoint = defaultResendURL
	}
	body := map[string]any{
		"from":    c.resendFromAddress(),
		"to":      []string{m.To},
		"subject": m.Subject,
		"text":    m.Text,
	}
	if m.HTML != "" {
		body["html"] = m.HTML
	}
	if m.ReplyTo != "" {
		body["reply_to"] = m.ReplyTo
	}
	if len(m.Tags) > 0 {
		tags := make([]map[string]string, 0, len(m.Tags))
		names := make([]string, 0, len(m.Tags))
		for name := range m.Tags {
			names = append(names, name)
		}
		sort.Strings(names) // deterministic payload order
		for _, name := range names {
			tags = append(tags, map[string]string{"name": name, "value": m.Tags[name]})
		}
		body["tags"] = tags
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("mailer: encode resend payload: %w", err)
	}
	request, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("mailer: build resend request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+c.ResendKey)
	request.Header.Set("Content-Type", "application/json")
	// Some WAF rules on the API reject generic client user agents.
	request.Header.Set("User-Agent", "gridiron-2000-mailer/1.0")
	if m.IdempotencyKey != "" {
		request.Header.Set("Idempotency-Key", m.IdempotencyKey)
	}
	client := &http.Client{Timeout: 15 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("mailer: resend send to %s: %w", m.To, err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode > 299 {
		snippet, _ := io.ReadAll(io.LimitReader(response.Body, 300))
		return fmt.Errorf("mailer: resend returned %d for %s: %s", response.StatusCode, m.To, strings.TrimSpace(string(snippet)))
	}
	return nil
}

// buildMessage composes the RFC 5322 headers and body for an outgoing
// email. It carries no network dependency, so tests can check header
// composition without opening a socket. When html is empty the message is
// plain text, matching the previous behavior; when html is given the
// message becomes multipart/alternative with the text part first, then the
// html part, each under its own boundary-delimited part. replyTo, when
// non-empty, adds a Reply-To header (the SMTP path ignores Tags and
// IdempotencyKey, per Message's doc comment, but honors ReplyTo).
func (c Config) buildMessage(to, subject, text, html, replyTo string) []byte {
	var b strings.Builder
	b.WriteString("From: " + c.From + "\r\n")
	b.WriteString("To: " + to + "\r\n")
	b.WriteString("Subject: " + subject + "\r\n")
	if replyTo != "" {
		b.WriteString("Reply-To: " + replyTo + "\r\n")
	}
	b.WriteString("MIME-Version: 1.0\r\n")

	if html == "" {
		b.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
		b.WriteString("\r\n")
		b.WriteString(text)
		return []byte(b.String())
	}

	boundary := randomBoundary()
	b.WriteString("Content-Type: multipart/alternative; boundary=\"" + boundary + "\"\r\n")
	b.WriteString("\r\n")
	b.WriteString("--" + boundary + "\r\n")
	b.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	b.WriteString("\r\n")
	b.WriteString(text)
	b.WriteString("\r\n")
	b.WriteString("--" + boundary + "\r\n")
	b.WriteString("Content-Type: text/html; charset=utf-8\r\n")
	b.WriteString("\r\n")
	b.WriteString(html)
	b.WriteString("\r\n")
	b.WriteString("--" + boundary + "--\r\n")
	return []byte(b.String())
}

// randomBoundary returns a random hex string usable as a MIME multipart
// boundary. It falls back to a fixed boundary if the CSPRNG is unavailable,
// which should not happen in practice.
func randomBoundary() string {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "gridiron2000staticboundary"
	}
	return hex.EncodeToString(raw)
}
