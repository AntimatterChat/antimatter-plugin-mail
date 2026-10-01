// Copyright (c) 2026-present Antimatter contributors.
// See LICENSE.txt for license information.

package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
)

// testProvider is an OAuth 2.0 token endpoint that issues the tokens the test mail servers accept.
type testProvider struct {
	mu       sync.Mutex
	requests []url.Values
	issued   int
	// refreshToken is the refresh token it accepts, rotated at each refresh.
	refreshToken string
	email        string
}

func (p *testProvider) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	p.mu.Lock()
	defer p.mu.Unlock()
	p.requests = append(p.requests, r.PostForm)

	fail := func(code string) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = fmt.Fprintf(w, `{"error": %q}`, code)
	}
	if r.PostForm.Get("client_id") != "client-id" || r.PostForm.Get("client_secret") != "client-secret" {
		fail("invalid_client")
		return
	}
	response := map[string]any{"token_type": "Bearer", "expires_in": 3600}
	switch r.PostForm.Get("grant_type") {
	case "authorization_code":
		if r.PostForm.Get("code") != "good-code" || r.PostForm.Get("code_verifier") == "" {
			fail("invalid_grant")
			return
		}
		claims, _ := json.Marshal(map[string]string{"email": p.email, "name": "Ada Lovelace"})
		response["id_token"] = "e30." + base64.RawURLEncoding.EncodeToString(claims) + ".sig"
	case "refresh_token":
		if r.PostForm.Get("refresh_token") != p.refreshToken {
			fail("invalid_grant")
			return
		}
	default:
		fail("unsupported_grant_type")
		return
	}
	p.issued++
	p.refreshToken = "refresh-" + strconv.Itoa(p.issued)
	response["access_token"] = "valid-" + strconv.Itoa(p.issued)
	response["refresh_token"] = p.refreshToken
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

func (p *testProvider) lastRequest() url.Values {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.requests[len(p.requests)-1]
}

func setUpOAuth(t *testing.T, e *testEnv) *testProvider {
	t.Helper()
	provider := &testProvider{email: "ada@example.com"}
	server := httptest.NewServer(provider)
	t.Cleanup(server.Close)

	google := *builtinProviders[providerGoogle]
	google.Endpoint = oauth2.Endpoint{AuthURL: "https://accounts.example.com/authorize", TokenURL: server.URL + "/token"}
	google.IMAP, google.SMTP = e.account.IMAP, e.account.SMTP
	e.p.providers = map[string]*oauthProvider{providerGoogle: &google}
	e.p.configuration.GoogleClientID = "client-id"
	e.p.configuration.GoogleClientSecret = "client-secret"
	e.api.On("GetConfig").Return(&model.Config{ServiceSettings: model.ServiceSettings{SiteURL: model.NewPointer("https://chat.example.com/")}}).Maybe()
	return provider
}

// startOAuth starts signing in, returning the state of the sign-in.
func startOAuth(t *testing.T, e *testEnv) string {
	t.Helper()
	w := e.request(t, http.MethodPost, "/api/v1/oauth/google/start", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	authURL, err := url.Parse(decode[map[string]string](t, w)["url"])
	require.NoError(t, err)
	query := authURL.Query()
	assert.Equal(t, "accounts.example.com", authURL.Host)
	assert.Equal(t, "client-id", query.Get("client_id"))
	assert.Equal(t, "https://chat.example.com/plugins/com.antimatterchat.mail/oauth/callback", query.Get("redirect_uri"))
	assert.Equal(t, "S256", query.Get("code_challenge_method"))
	assert.NotEmpty(t, query.Get("code_challenge"))
	assert.Equal(t, "offline", query.Get("access_type"))
	assert.Contains(t, query.Get("scope"), "https://mail.google.com/")
	return query.Get("state")
}

func TestAPIOAuth(t *testing.T) {
	e := newTestEnv(t)

	w := e.request(t, http.MethodGet, "/api/v1/config", nil)
	assert.JSONEq(t, `{"allow_insecure_connections": true, "oauth_providers": []}`, w.Body.String())
	w = e.request(t, http.MethodPost, "/api/v1/oauth/google/start", nil)
	assert.Equal(t, http.StatusNotFound, w.Code, "not set up")

	provider := setUpOAuth(t, e)
	w = e.request(t, http.MethodGet, "/api/v1/config", nil)
	assert.JSONEq(t, `{"allow_insecure_connections": true, "oauth_providers": [{"id": "google", "name": "Google"}]}`, w.Body.String())

	// Signing in for someone else, refused by the user, or with an unknown state fails
	state := startOAuth(t, e)
	me := e.userID
	e.userID = model.NewId()
	w = e.request(t, http.MethodGet, "/oauth/callback?code=good-code&state="+state, nil)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "expired")
	e.userID = me
	w = e.request(t, http.MethodGet, "/oauth/callback?code=good-code&state="+state, nil)
	assert.Equal(t, http.StatusBadRequest, w.Code, "a state is used once")
	w = e.request(t, http.MethodGet, "/oauth/callback?error=access_denied&error_description=%3Cb%3Eno%3C%2Fb%3E&state="+startOAuth(t, e), nil)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "&lt;b&gt;no&lt;/b&gt;", "the provider's text is escaped")
	w = e.request(t, http.MethodGet, "/oauth/callback?code=bad-code&state="+startOAuth(t, e), nil)
	assert.Equal(t, http.StatusBadGateway, w.Code)

	// Signing in
	w = e.request(t, http.MethodGet, "/oauth/callback?code=good-code&state="+startOAuth(t, e), nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "ada@example.com")
	assert.Contains(t, w.Header().Get("Content-Security-Policy"), "script-src 'nonce-")
	assert.NotEmpty(t, provider.lastRequest().Get("code_verifier"))

	w = e.request(t, http.MethodGet, "/api/v1/account", nil)
	account := decode[map[string]*accountView](t, w)["account"]
	require.NotNil(t, account)
	assert.Equal(t, "google", account.OAuthProvider)
	assert.Equal(t, "ada@example.com", account.Email)
	assert.Equal(t, "Ada Lovelace", account.Name)
	assert.Equal(t, "ada@example.com", account.IMAP.Username)
	stored, err := e.p.store.GetAccount(e.userID)
	require.NoError(t, err)
	assert.Equal(t, "refresh-1", stored.OAuth.RefreshToken)

	// The mail servers take the access token
	w = e.request(t, http.MethodGet, "/api/v1/folders", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	// An expired token is refreshed, and the new refresh token saved
	e.p.pool.drop(e.userID)
	entry := e.p.tokens.entry(e.userID)
	entry.mu.Lock()
	entry.token.Expiry = time.Now()
	entry.mu.Unlock()
	w = e.request(t, http.MethodPost, "/api/v1/send", sendRequest{OutgoingMessage: OutgoingMessage{To: []Address{{Email: "marie@example.com"}}, Text: "hi"}})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, "refresh_token", provider.lastRequest().Get("grant_type"))
	assert.Equal(t, "refresh-1", provider.lastRequest().Get("refresh_token"))
	stored, err = e.p.store.GetAccount(e.userID)
	require.NoError(t, err)
	assert.Equal(t, "refresh-2", stored.OAuth.RefreshToken)
	require.Len(t, e.smtp.received, 1)

	// Renaming keeps the sign-in
	w = e.request(t, http.MethodPut, "/api/v1/account", map[string]any{
		"email": "ada@example.com", "name": "Ada", "oauth_provider": "google", "imap": account.IMAP, "smtp": account.SMTP,
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	stored, err = e.p.store.GetAccount(e.userID)
	require.NoError(t, err)
	assert.Equal(t, "Ada", stored.Name)
	assert.Equal(t, "refresh-2", stored.OAuth.RefreshToken)

	// A revoked sign-in asks to sign in again
	e.p.pool.drop(e.userID)
	e.p.tokens.forget(e.userID)
	provider.mu.Lock()
	provider.refreshToken = "revoked"
	provider.mu.Unlock()
	w = e.request(t, http.MethodGet, "/api/v1/folders", nil)
	assert.Equal(t, http.StatusConflict, w.Code)
	assert.Contains(t, w.Body.String(), "sign in again")
}

func TestIDTokenIdentity(t *testing.T) {
	token := func(claims string) *oauth2.Token {
		return (&oauth2.Token{}).WithExtra(map[string]any{"id_token": "e30." + base64.RawURLEncoding.EncodeToString([]byte(claims)) + ".sig"})
	}
	email, name, err := idTokenIdentity(token(`{"email": "ada@example.com", "name": "Ada"}`))
	require.NoError(t, err)
	assert.Equal(t, "ada@example.com", email)
	assert.Equal(t, "Ada", name)

	email, _, err = idTokenIdentity(token(`{"preferred_username": "ada@contoso.com"}`))
	require.NoError(t, err)
	assert.Equal(t, "ada@contoso.com", email)

	for _, claims := range []string{`{}`, `{"email": "Ada <ada@example.com>"}`, `{"preferred_username": "ada"}`, `not json`} {
		_, _, err = idTokenIdentity(token(claims))
		assert.Error(t, err, claims)
	}
	_, _, err = idTokenIdentity(&oauth2.Token{})
	assert.Error(t, err)
}
