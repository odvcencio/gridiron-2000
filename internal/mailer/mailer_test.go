package mailer

import (
	"strings"
	"testing"
)

func TestFromEnvDefaultsPortAndFrom(t *testing.T) {
	t.Setenv("SMTP_HOST", "smtp.example.com")
	t.Setenv("SMTP_PORT", "")
	t.Setenv("SMTP_USER", "commish@example.com")
	t.Setenv("SMTP_PASS", "secret")
	t.Setenv("SMTP_FROM", "")

	config := FromEnv()
	if config.Port != "587" {
		t.Errorf("Port = %q, want default 587", config.Port)
	}
	if config.From != config.User {
		t.Errorf("From = %q, want it to fall back to User %q", config.From, config.User)
	}
	if !config.Enabled() {
		t.Error("config with host, user, and pass should be enabled")
	}
}

func TestEnabledRequiresHostUserPass(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
		want bool
	}{
		{"all set", Config{Host: "h", User: "u", Pass: "p"}, true},
		{"missing host", Config{User: "u", Pass: "p"}, false},
		{"missing user", Config{Host: "h", Pass: "p"}, false},
		{"missing pass", Config{Host: "h", User: "u"}, false},
		{"empty", Config{}, false},
	}
	for _, tc := range cases {
		if got := tc.cfg.Enabled(); got != tc.want {
			t.Errorf("%s: Enabled() = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestSendRejectsDisabledConfig(t *testing.T) {
	var config Config
	if err := config.Send("to@example.com", "Subject", "Body"); err == nil {
		t.Fatal("Send on a disabled config should fail")
	}
}

func TestBuildMessageComposesHeadersAndBody(t *testing.T) {
	config := Config{Host: "smtp.example.com", Port: "587", User: "commish@example.com", Pass: "secret", From: "commish@example.com"}
	raw := config.buildMessage("manager@example.com", "You're invited", "Hello there.\nSee you at the draft.")
	message := string(raw)

	wantLines := []string{
		"From: commish@example.com",
		"To: manager@example.com",
		"Subject: You're invited",
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=utf-8",
	}
	for _, line := range wantLines {
		if !strings.Contains(message, line+"\r\n") {
			t.Errorf("message missing header line %q\nfull message:\n%s", line, message)
		}
	}

	headerEnd := strings.Index(message, "\r\n\r\n")
	if headerEnd == -1 {
		t.Fatal("message has no blank line separating headers from body")
	}
	body := message[headerEnd+4:]
	if body != "Hello there.\nSee you at the draft." {
		t.Errorf("body = %q, want it unchanged after the blank line", body)
	}

	// Headers must all appear before the blank-line separator.
	for _, line := range wantLines {
		if strings.Index(message, line) > headerEnd {
			t.Errorf("header %q appears after the blank line", line)
		}
	}
}

func TestBuildMessageDefaultFromEnv(t *testing.T) {
	t.Setenv("SMTP_HOST", "smtp.example.com")
	t.Setenv("SMTP_PORT", "2525")
	t.Setenv("SMTP_USER", "bot@example.com")
	t.Setenv("SMTP_PASS", "hunter2")
	t.Setenv("SMTP_FROM", "invites@example.com")

	config := FromEnv()
	message := string(config.buildMessage("manager@example.com", "Hi", "Body"))
	if !strings.Contains(message, "From: invites@example.com\r\n") {
		t.Errorf("expected explicit SMTP_FROM in message, got:\n%s", message)
	}
}
