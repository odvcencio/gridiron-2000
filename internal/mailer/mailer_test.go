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
	if err := config.Send("to@example.com", "Subject", "Body", ""); err == nil {
		t.Fatal("Send on a disabled config should fail")
	}
}

func TestBuildMessageComposesHeadersAndBody(t *testing.T) {
	config := Config{Host: "smtp.example.com", Port: "587", User: "commish@example.com", Pass: "secret", From: "commish@example.com"}
	raw := config.buildMessage("manager@example.com", "You're invited", "Hello there.\nSee you at the draft.", "")
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
	message := string(config.buildMessage("manager@example.com", "Hi", "Body", ""))
	if !strings.Contains(message, "From: invites@example.com\r\n") {
		t.Errorf("expected explicit SMTP_FROM in message, got:\n%s", message)
	}
}

func TestBuildMessageWithHTMLIsMultipartAlternative(t *testing.T) {
	config := Config{Host: "smtp.example.com", Port: "587", User: "commish@example.com", Pass: "secret", From: "commish@example.com"}
	raw := config.buildMessage("manager@example.com", "You're invited", "Hello there.\nSee you at the draft.", "<p>Hello there.</p>")
	message := string(raw)

	if !strings.Contains(message, `Content-Type: multipart/alternative; boundary="`) {
		t.Fatalf("top-level header missing multipart/alternative content type:\n%s", message)
	}

	// Pull the declared boundary out of the top-level header and confirm the
	// body actually uses it to delimit both parts.
	const marker = `boundary="`
	start := strings.Index(message, marker) + len(marker)
	end := strings.Index(message[start:], `"`)
	if start < len(marker) || end == -1 {
		t.Fatalf("could not find boundary value in header:\n%s", message)
	}
	boundary := message[start : start+end]
	if boundary == "" {
		t.Fatal("boundary value is empty")
	}

	delimiter := "--" + boundary
	if strings.Count(message, delimiter) < 3 { // opening x2 + closing
		t.Errorf("message does not use the declared boundary to delimit two parts:\n%s", message)
	}
	if !strings.Contains(message, delimiter+"--\r\n") {
		t.Errorf("message missing the closing boundary marker:\n%s", message)
	}

	textIndex := strings.Index(message, "Content-Type: text/plain; charset=utf-8")
	htmlIndex := strings.Index(message, "Content-Type: text/html; charset=utf-8")
	if textIndex == -1 || htmlIndex == -1 {
		t.Fatalf("message missing a text or html part header:\n%s", message)
	}
	if textIndex > htmlIndex {
		t.Errorf("text part should appear before the html part:\n%s", message)
	}
	if !strings.Contains(message, "Hello there.\nSee you at the draft.") {
		t.Errorf("message missing plain-text body:\n%s", message)
	}
	if !strings.Contains(message, "<p>Hello there.</p>") {
		t.Errorf("message missing html body:\n%s", message)
	}
}
