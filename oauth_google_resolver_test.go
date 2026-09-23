package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"m31labs.dev/gosx/auth"
)

func TestParseEmailVerified(t *testing.T) {
	cases := map[string]struct {
		raw  string
		want bool
	}{
		"bool true":           {`true`, true},
		"bool false":          {`false`, false},
		"string true":         {`"true"`, true},
		"string TRUE":         {`"TRUE"`, true},
		"string false":        {`"false"`, false},
		"empty":               {``, false},
		"null":                {`null`, false},
		"malformed":           {`{"nope":1}`, false},
		"unrecognized string": {`"maybe"`, false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := parseEmailVerified(json.RawMessage(tc.raw))
			if got != tc.want {
				t.Errorf("parseEmailVerified(%q) = %v, want %v", tc.raw, got, tc.want)
			}
		})
	}
}

// googleUserInfoServer starts a test server serving one fixed userinfo
// JSON body, mirroring https://openidconnect.googleapis.com/v1/userinfo
// closely enough for googleOAuthResolver's own request/decode path.
func googleUserInfoServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-access-token" {
			t.Errorf("Authorization header = %q, want Bearer test-access-token", got)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server
}

func TestGoogleOAuthResolverRejectsUnverifiedEmail(t *testing.T) {
	server := googleUserInfoServer(t, `{"sub":"123","email":"manager@example.com","email_verified":false,"name":"Manager"}`)
	provider := auth.OAuthProvider{Name: "google", UserInfoURL: server.URL}
	token := auth.OAuthToken{AccessToken: "test-access-token"}

	_, err := googleOAuthResolver().ResolveOAuthUser(context.Background(), provider, server.Client(), token)
	if err == nil {
		t.Fatal("expected a refusal for email_verified=false")
	}
	if !errors.Is(err, ErrGoogleEmailNotVerified) {
		t.Errorf("err = %v, want it to wrap ErrGoogleEmailNotVerified", err)
	}
}

func TestGoogleOAuthResolverTreatsMissingClaimAsUnverified(t *testing.T) {
	server := googleUserInfoServer(t, `{"sub":"123","email":"manager@example.com","name":"Manager"}`)
	provider := auth.OAuthProvider{Name: "google", UserInfoURL: server.URL}
	token := auth.OAuthToken{AccessToken: "test-access-token"}

	_, err := googleOAuthResolver().ResolveOAuthUser(context.Background(), provider, server.Client(), token)
	if !errors.Is(err, ErrGoogleEmailNotVerified) {
		t.Errorf("err = %v, want ErrGoogleEmailNotVerified (fail closed on a missing claim)", err)
	}
}

func TestGoogleOAuthResolverAcceptsVerifiedEmail(t *testing.T) {
	server := googleUserInfoServer(t, `{"sub":"123","email":"Manager@Example.com","email_verified":true,"name":"Manager One","picture":"https://example.com/pic.png"}`)
	provider := auth.OAuthProvider{Name: "google", UserInfoURL: server.URL}
	token := auth.OAuthToken{AccessToken: "test-access-token"}

	user, err := googleOAuthResolver().ResolveOAuthUser(context.Background(), provider, server.Client(), token)
	if err != nil {
		t.Fatalf("unexpected refusal: %v", err)
	}
	if user.Email != "manager@example.com" {
		t.Errorf("Email = %q, want lowercase manager@example.com", user.Email)
	}
	if user.Name != "Manager One" {
		t.Errorf("Name = %q, want %q", user.Name, "Manager One")
	}
	if user.ID != "google:123" {
		t.Errorf("ID = %q, want google:123", user.ID)
	}
	if user.Meta["email_verified"] != true {
		t.Errorf("Meta[email_verified] = %v, want true", user.Meta["email_verified"])
	}
	if user.Meta["avatar"] != "https://example.com/pic.png" {
		t.Errorf("Meta[avatar] = %v, want the picture URL", user.Meta["avatar"])
	}
}

func TestGoogleOAuthResolverRejectsOnUserInfoHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "invalid_token", http.StatusUnauthorized)
	}))
	t.Cleanup(server.Close)
	provider := auth.OAuthProvider{Name: "google", UserInfoURL: server.URL}
	token := auth.OAuthToken{AccessToken: "test-access-token"}

	if _, err := googleOAuthResolver().ResolveOAuthUser(context.Background(), provider, server.Client(), token); err == nil {
		t.Fatal("expected an error when the userinfo endpoint fails")
	}
}
