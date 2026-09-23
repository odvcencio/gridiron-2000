// Google OAuth email_verified gate.
//
// gosx's built-in Google resolver (m31labs.dev/gosx/auth, GoogleProvider +
// its unexported userFromOAuthPayload) fetches
// https://openidconnect.googleapis.com/v1/userinfo and maps sub/email/
// name/picture into an auth.User, but it never reads that same response's
// email_verified claim — gridiron, which only ever sees the finished
// auth.User (auth.Current), has no way to recover a claim gosx already
// discarded. Per the OpenID Connect spec, an OP that grants the "email"
// scope (googleProvider.Scopes here includes it) MUST include
// email_verified alongside email, so Google's response always carries it;
// gosx simply drops it on the floor.
//
// The fix does not touch gosx: auth.OAuthProvider.Resolver is a public
// extension point (auth.GitHubProvider already uses it, for a different
// reason — an email-less GitHub profile). This file supplies gridiron's
// own Resolver for the Google provider, so the exact same userinfo
// request gosx would have made instead runs here, where email_verified
// is read and enforced before a session is ever created: an unverified
// email refuses the sign-in outright (auth.OAuth.Callback and
// CallbackHandler already turn a Resolver error into a clean
// /login?error=oauth redirect, or a 401 for a JSON caller — no gosx
// change needed for that path either).
//
// This closes the gap the audit named: commissioner rights and league
// admission (service.go's IsCommissioner/EmailAllowed) key off email
// alone, so a spoofed-but-unverified address must never reach them.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"m31labs.dev/gosx/auth"
)

// ErrGoogleEmailNotVerified is returned (wrapped with the offending
// address) when Google's userinfo response reports email_verified=false
// or omits the claim. Fail closed: a missing claim is exactly as
// unproven as an explicit false. auth.OAuth.Callback returns this error
// to its caller unchanged, so it also drives the "/login?error=oauth"
// flash message.
var ErrGoogleEmailNotVerified = fmt.Errorf("google account email is not verified")

// googleUserInfo is the subset of Google's OIDC userinfo response
// (https://openidconnect.googleapis.com/v1/userinfo) this resolver reads.
// email_verified is deliberately typed as json.RawMessage, not bool: the
// OIDC spec requires a JSON boolean here, but this reads it defensively
// (parseEmailVerified below accepts a stringly "true"/"false" too) rather
// than letting a provider quirk fail the whole decode and lock out every
// manager.
type googleUserInfo struct {
	Sub           string          `json:"sub"`
	Email         string          `json:"email"`
	EmailVerified json.RawMessage `json:"email_verified"`
	Name          string          `json:"name"`
	Picture       string          `json:"picture"`
}

// parseEmailVerified reads the OIDC email_verified claim leniently: a
// bare JSON `true`/`false`, or a quoted `"true"`/`"false"` string (some
// non-conformant OIDC providers send the latter; Google's own userinfo
// endpoint sends a real boolean, but this does not assume that forever).
// Anything else — absent, null, malformed — is treated as unverified.
func parseEmailVerified(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var b bool
	if err := json.Unmarshal(raw, &b); err == nil {
		return b
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		if parsed, err := strconv.ParseBool(strings.TrimSpace(s)); err == nil {
			return parsed
		}
	}
	return false
}

// googleOAuthResolver fetches provider.UserInfoURL itself (the same
// request gosx's own default resolver would make for a Resolver-less
// provider) and refuses to resolve a user whose email is not verified.
// It is registered on googleProvider.Resolver in app_build.go's BuildApp,
// so it runs in place of gosx's built-in Google resolver, not alongside
// it.
func googleOAuthResolver() auth.OAuthUserResolver {
	return auth.OAuthUserResolverFunc(func(ctx context.Context, provider auth.OAuthProvider, client *http.Client, token auth.OAuthToken) (auth.User, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, provider.UserInfoURL, nil)
		if err != nil {
			return auth.User{}, err
		}
		req.Header.Set("Authorization", "Bearer "+token.AccessToken)
		req.Header.Set("Accept", "application/json")
		res, err := client.Do(req)
		if err != nil {
			return auth.User{}, err
		}
		defer res.Body.Close()
		if res.StatusCode >= 400 {
			body, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
			return auth.User{}, fmt.Errorf("google userinfo failed: %s", strings.TrimSpace(string(body)))
		}
		var payload googleUserInfo
		if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
			return auth.User{}, err
		}
		email := strings.ToLower(strings.TrimSpace(payload.Email))
		if !parseEmailVerified(payload.EmailVerified) {
			return auth.User{}, fmt.Errorf("%w: %s", ErrGoogleEmailNotVerified, email)
		}
		id := strings.TrimSpace(payload.Sub)
		if id == "" {
			id = email
		}
		name := strings.TrimSpace(payload.Name)
		if name == "" {
			name = email
		}
		meta := map[string]any{"provider": provider.Name, "email_verified": true}
		if picture := strings.TrimSpace(payload.Picture); picture != "" {
			meta["avatar"] = picture
		}
		return auth.User{
			ID:    provider.Name + ":" + id,
			Email: email,
			Name:  name,
			Meta:  meta,
		}, nil
	})
}
