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
	"errors"
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

// ErrSMTPTransport wraps every error SendMessage's SMTP path returns, so a
// caller can tell an SMTP failure from a Resend failure with errors.Is
// (finding M3). smtp.SendMail can return an error after the server already
// accepted the message (for example at the QUIT step), so a caller that
// retries blindly risks a duplicate; a Resend failure carries no such risk
// because its Idempotency-Key header protects a retry.
var ErrSMTPTransport = errors.New("mailer: smtp transport error")

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
// Enabled. The recipient address is not free text: a control character in
// m.To (for example a stray \r\n) is rejected outright rather than
// silently stripped, on both transports (finding B1). Subject and ReplyTo
// are free text and are sanitized instead, by sanitizeHeader below.
func (c Config) SendMessage(m Message) error {
	if containsControl(m.To) {
		return fmt.Errorf("mailer: recipient address %q carries a control character", m.To)
	}
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
		return fmt.Errorf("%w: send to %s: %v", ErrSMTPTransport, m.To, err)
	}
	return nil
}

// containsControl reports whether s carries \r, \n, or any other C0
// control character (finding B1). SendMessage uses it to reject a
// recipient address outright instead of silently stripping it.
func containsControl(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

// sanitizeHeader strips \r, \n, and every other C0 control character from
// a header value bound for an outgoing message (subject or Reply-To). A
// raw \r\n in a header value written straight into the SMTP message bytes
// can terminate the header block early and let the rest of the value
// become the message body, or add a forged header such as Bcc (finding
// B1). Applied on both transports for uniformity: buildMessage calls it
// for the SMTP path, and sendResend calls it before building the JSON
// payload, even though Resend's JSON encoding already escapes control
// characters on the wire — neither path should depend on the other's
// protection.
func sanitizeHeader(s string) string {
	clean := true
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			clean = false
			break
		}
	}
	if clean {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// sendResend posts the message to the Resend HTTP API. The "html" key is
// omitted entirely when html is empty, so a plain-text send stays plain.
// "tags" is included only when at least one tag survives sanitizeResendTag
// filtering; the Idempotency-Key header is set only when IdempotencyKey is
// non-empty. subject and reply_to run through sanitizeHeader for the same
// reason buildMessage's SMTP path does (finding B1).
func (c Config) sendResend(m Message) error {
	endpoint := c.resendURL
	if endpoint == "" {
		endpoint = defaultResendURL
	}
	body := map[string]any{
		"from":    c.resendFromAddress(),
		"to":      []string{m.To},
		"subject": sanitizeHeader(m.Subject),
		"text":    m.Text,
	}
	if m.HTML != "" {
		body["html"] = m.HTML
	}
	if replyTo := sanitizeHeader(m.ReplyTo); replyTo != "" {
		body["reply_to"] = replyTo
	}
	if len(m.Tags) > 0 {
		tags := make([]map[string]string, 0, len(m.Tags))
		names := make([]string, 0, len(m.Tags))
		for name := range m.Tags {
			names = append(names, name)
		}
		sort.Strings(names) // deterministic payload order
		for _, name := range names {
			// finding m6: Resend documents an ASCII letters/digits/_/-
			// charset for tag names and values; an empty or out-of-charset
			// tag risks a 422 that would burn the retry after the ledger
			// already recorded the send as sent, so a tag that has no name
			// or value left after filtering is omitted rather than sent.
			cleanName := sanitizeResendTag(name)
			cleanValue := sanitizeResendTag(m.Tags[name])
			if cleanName == "" || cleanValue == "" {
				continue
			}
			tags = append(tags, map[string]string{"name": cleanName, "value": cleanValue})
		}
		if len(tags) > 0 {
			body["tags"] = tags
		}
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
		// finding nit 8: a CRLF in a header value makes net/http reject the
		// whole request outright (http.Header.Set/net/http's request
		// writer both refuse control characters), which would lose the
		// email entirely rather than just the idempotency guard. Keys are
		// always internally generated (the league package's ledger key
		// builders), so a charset check with a clear error is enough —
		// this should never fire in practice, but fails loudly instead of
		// surfacing as a confusing low-level net/http error if it ever did.
		if containsControl(m.IdempotencyKey) {
			return fmt.Errorf("mailer: idempotency key %q contains a control character", m.IdempotencyKey)
		}
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
// IdempotencyKey, per Message's doc comment, but honors ReplyTo). subject
// and replyTo run through sanitizeHeader before either is written, closing
// the header-injection path a raw \r\n would otherwise open (finding B1).
func (c Config) buildMessage(to, subject, text, html, replyTo string) []byte {
	subject = sanitizeHeader(subject)
	replyTo = sanitizeHeader(replyTo)
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

// isResendTagRune reports whether r belongs to the charset Resend documents
// for tag names and values: ASCII letters, digits, underscore, and hyphen
// (design spec section 6.5; finding m6).
func isResendTagRune(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
		return true
	default:
		return false
	}
}

// sanitizeResendTag filters s down to the documented Resend tag charset,
// dropping every other character (finding m6).
func sanitizeResendTag(s string) string {
	var b strings.Builder
	for _, r := range s {
		if isResendTagRune(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}
