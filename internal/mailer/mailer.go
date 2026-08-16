// Package mailer sends plain-text invite email through Resend or SMTP. It
// stays optional: without credentials, Config.Enabled reports false and
// callers fall back to a mailto: link instead of a live send. When both are
// configured, Resend wins.
package mailer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/smtp"
	"os"
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

// Send delivers a plain-text message to one recipient. Resend is preferred
// when configured; SMTP is the fallback. It fails fast when the config is
// not Enabled, so callers may check Send's error instead of calling Enabled
// twice.
func (c Config) Send(to, subject, body string) error {
	if c.ResendKey != "" && c.resendFromAddress() != "" {
		return c.sendResend(to, subject, body)
	}
	if !c.Enabled() {
		return fmt.Errorf("mailer: no transport configured (need RESEND_API_KEY+RESEND_FROM or SMTP_HOST/USER/PASS)")
	}
	message := c.buildMessage(to, subject, body)
	addr := c.Host + ":" + c.Port
	auth := smtp.PlainAuth("", c.User, c.Pass, c.Host)
	if err := smtp.SendMail(addr, auth, c.From, []string{to}, message); err != nil {
		return fmt.Errorf("mailer: send to %s: %w", to, err)
	}
	return nil
}

// sendResend posts the message to the Resend HTTP API.
func (c Config) sendResend(to, subject, body string) error {
	endpoint := c.resendURL
	if endpoint == "" {
		endpoint = defaultResendURL
	}
	payload, err := json.Marshal(map[string]any{
		"from":    c.resendFromAddress(),
		"to":      []string{to},
		"subject": subject,
		"text":    body,
	})
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
	client := &http.Client{Timeout: 15 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("mailer: resend send to %s: %w", to, err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode > 299 {
		snippet, _ := io.ReadAll(io.LimitReader(response.Body, 300))
		return fmt.Errorf("mailer: resend returned %d for %s: %s", response.StatusCode, to, strings.TrimSpace(string(snippet)))
	}
	return nil
}

// buildMessage composes the RFC 5322 headers and body for an outgoing
// plain-text email. It carries no network dependency, so tests can check
// header composition without opening a socket.
func (c Config) buildMessage(to, subject, body string) []byte {
	var b strings.Builder
	b.WriteString("From: " + c.From + "\r\n")
	b.WriteString("To: " + to + "\r\n")
	b.WriteString("Subject: " + subject + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	b.WriteString("\r\n")
	b.WriteString(body)
	return []byte(b.String())
}
