package mailer

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestResendPreferredAndPayload(t *testing.T) {
	var got map[string]any
	var auth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &got)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"test"}`))
	}))
	defer server.Close()

	config := Config{ResendKey: "re_test_key", ResendFrom: "commish@example.com", resendURL: server.URL}
	if !config.Enabled() {
		t.Fatal("resend-only config must be enabled")
	}
	if err := config.Send("manager@example.com", "Subject line", "Body text", ""); err != nil {
		t.Fatalf("send: %v", err)
	}
	if auth != "Bearer re_test_key" {
		t.Errorf("auth header = %q", auth)
	}
	if got["from"] != "commish@example.com" || got["subject"] != "Subject line" || got["text"] != "Body text" {
		t.Errorf("payload wrong: %+v", got)
	}
	to, _ := got["to"].([]any)
	if len(to) != 1 || to[0] != "manager@example.com" {
		t.Errorf("to wrong: %v", got["to"])
	}
	if _, present := got["html"]; present {
		t.Errorf("payload should omit the html key when html is empty: %+v", got)
	}
}

func TestResendPayloadCarriesHTMLWhenGiven(t *testing.T) {
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &got)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"test"}`))
	}))
	defer server.Close()

	config := Config{ResendKey: "re_test_key", ResendFrom: "commish@example.com", resendURL: server.URL}
	if err := config.Send("manager@example.com", "Subject line", "Body text", "<p>Body text</p>"); err != nil {
		t.Fatalf("send: %v", err)
	}
	if got["text"] != "Body text" {
		t.Errorf("payload text wrong: %+v", got)
	}
	if got["html"] != "<p>Body text</p>" {
		t.Errorf("payload html wrong: %+v", got)
	}
}

func TestResendErrorSurfacesStatusAndBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"invalid from domain"}`))
	}))
	defer server.Close()
	config := Config{ResendKey: "re_bad", ResendFrom: "x@nope.example", resendURL: server.URL}
	err := config.Send("a@example.com", "s", "b", "")
	if err == nil {
		t.Fatal("expected error")
	}
	if want := "403"; !contains(err.Error(), want) || !contains(err.Error(), "invalid from domain") {
		t.Errorf("error lacks detail: %v", err)
	}
}

// TestSendMessageResendCarriesTagsIdempotencyKeyAndReplyTo checks that
// SendMessage's Resend path sends Tags as the "tags" array, IdempotencyKey
// as the Idempotency-Key header, and ReplyTo as "reply_to" in the payload
// (design spec section 6.5).
func TestSendMessageResendCarriesTagsIdempotencyKeyAndReplyTo(t *testing.T) {
	var got map[string]any
	var idempotencyHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		idempotencyHeader = r.Header.Get("Idempotency-Key")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &got)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"test"}`))
	}))
	defer server.Close()

	config := Config{ResendKey: "re_test_key", ResendFrom: "commish@example.com", resendURL: server.URL}
	err := config.SendMessage(Message{
		To:             "manager@example.com",
		Subject:        "Rules update",
		Text:           "The math changed.",
		Tags:           map[string]string{"category": "scoring_change", "league": "g2k"},
		IdempotencyKey: "scoring:deadbeef:manager@example.com",
		ReplyTo:        "commissioner@example.com",
	})
	if err != nil {
		t.Fatalf("send: %v", err)
	}

	if idempotencyHeader != "scoring:deadbeef:manager@example.com" {
		t.Errorf("Idempotency-Key header = %q", idempotencyHeader)
	}
	if got["reply_to"] != "commissioner@example.com" {
		t.Errorf("reply_to = %v, want commissioner@example.com", got["reply_to"])
	}

	rawTags, ok := got["tags"].([]any)
	if !ok || len(rawTags) != 2 {
		t.Fatalf("tags = %+v, want a two-element array", got["tags"])
	}
	tags := map[string]string{}
	for _, entry := range rawTags {
		m, ok := entry.(map[string]any)
		if !ok {
			t.Fatalf("tag entry = %+v, want a {name,value} object", entry)
		}
		name, _ := m["name"].(string)
		value, _ := m["value"].(string)
		tags[name] = value
	}
	if tags["category"] != "scoring_change" || tags["league"] != "g2k" {
		t.Errorf("tags = %+v, want category=scoring_change league=g2k", tags)
	}
}

// TestSendMessageResendOmitsEmptyOptionalFields checks that SendMessage
// omits the tags key, the reply_to key, and the Idempotency-Key header
// entirely when the message carries none of them, matching Send's
// existing plain-message behavior.
func TestSendMessageResendOmitsEmptyOptionalFields(t *testing.T) {
	var got map[string]any
	var idempotencyHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		idempotencyHeader = r.Header.Get("Idempotency-Key")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &got)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"test"}`))
	}))
	defer server.Close()

	config := Config{ResendKey: "re_test_key", ResendFrom: "commish@example.com", resendURL: server.URL}
	if err := config.SendMessage(Message{To: "manager@example.com", Subject: "Hi", Text: "Body"}); err != nil {
		t.Fatalf("send: %v", err)
	}
	if _, present := got["tags"]; present {
		t.Errorf("tags key should be omitted when Tags is empty: %+v", got)
	}
	if _, present := got["reply_to"]; present {
		t.Errorf("reply_to key should be omitted when ReplyTo is empty: %+v", got)
	}
	if idempotencyHeader != "" {
		t.Errorf("Idempotency-Key header should be absent when IdempotencyKey is empty, got %q", idempotencyHeader)
	}
}

// TestSendIsAThinSendMessageWrapper checks that Send still produces the
// same "from/to/subject/text" payload it always has, now via SendMessage.
func TestSendIsAThinSendMessageWrapper(t *testing.T) {
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &got)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"test"}`))
	}))
	defer server.Close()

	config := Config{ResendKey: "re_test_key", ResendFrom: "commish@example.com", resendURL: server.URL}
	if err := config.Send("manager@example.com", "Subject line", "Body text", ""); err != nil {
		t.Fatalf("send: %v", err)
	}
	if got["from"] != "commish@example.com" || got["subject"] != "Subject line" || got["text"] != "Body text" {
		t.Errorf("payload wrong: %+v", got)
	}
}

func TestResendFromFallsBackToSMTPFrom(t *testing.T) {
	config := Config{ResendKey: "re_key", From: "smtp-from@example.com"}
	if config.resendFromAddress() != "smtp-from@example.com" {
		t.Errorf("from fallback = %q", config.resendFromAddress())
	}
	if !config.Enabled() {
		t.Error("resend key with SMTP_FROM fallback must be enabled")
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (haystack == needle || len(needle) == 0 ||
		(func() bool {
			for i := 0; i+len(needle) <= len(haystack); i++ {
				if haystack[i:i+len(needle)] == needle {
					return true
				}
			}
			return false
		})())
}
