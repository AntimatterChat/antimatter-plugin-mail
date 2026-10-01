// Copyright (c) 2026-present Antimatter contributors.
// See LICENSE.txt for license information.

package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"html/template"
	"net/http"
	"net/mail"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/mattermost/mattermost/server/public/model"
	"github.com/pkg/errors"
	"golang.org/x/oauth2"
)

// Mail providers users can sign in with, rather than with passwords.
const (
	providerGoogle    = "google"
	providerMicrosoft = "microsoft"
)

const (
	// oauthCallbackPath is where providers send users back after they signed in, under the
	// plugin's route: <Site URL>/plugins/com.antimatterchat.mail/oauth/callback.
	oauthCallbackPath = "/oauth/callback"

	// oauthStateLifetime is how long users have to sign in.
	oauthStateLifetime = 10 * time.Minute
	// oauthTimeout bounds the requests to the providers.
	oauthTimeout = 20 * time.Second
	// tokenRefreshMargin is how long before it expires an access token is refreshed.
	tokenRefreshMargin = time.Minute
)

// oauthProvider is a mail provider users sign in with.
type oauthProvider struct {
	// Name is shown to users.
	Name string
	// Endpoint is the provider's OAuth 2.0 endpoint; "{tenant}" in its URLs is replaced with the
	// Microsoft tenant.
	Endpoint oauth2.Endpoint
	Scopes   []string
	// AuthParams are added to the authorization URL.
	AuthParams map[string]string
	// IMAP and SMTP are the provider's servers, without the username.
	IMAP, SMTP ServerSettings
}

// builtinProviders are the providers users can sign in with, once the admin sets up their OAuth
// client.
var builtinProviders = map[string]*oauthProvider{
	providerGoogle: {
		Name: "Google",
		Endpoint: oauth2.Endpoint{
			AuthURL:  "https://accounts.google.com/o/oauth2/v2/auth",
			TokenURL: "https://oauth2.googleapis.com/token",
		},
		Scopes: []string{"https://mail.google.com/", "openid", "email", "profile"},
		// A refresh token is only given with offline access, and again only after consent
		AuthParams: map[string]string{"access_type": "offline", "prompt": "consent"},
		IMAP:       ServerSettings{Host: "imap.gmail.com", Port: 993, Security: SecurityTLS},
		SMTP:       ServerSettings{Host: "smtp.gmail.com", Port: 465, Security: SecurityTLS},
	},
	providerMicrosoft: {
		Name: "Microsoft",
		Endpoint: oauth2.Endpoint{
			AuthURL:  "https://login.microsoftonline.com/{tenant}/oauth2/v2.0/authorize",
			TokenURL: "https://login.microsoftonline.com/{tenant}/oauth2/v2.0/token",
		},
		Scopes: []string{
			"https://outlook.office.com/IMAP.AccessAsUser.All",
			"https://outlook.office.com/SMTP.Send",
			"offline_access", "openid", "email", "profile",
		},
		AuthParams: map[string]string{"prompt": "select_account"},
		IMAP:       ServerSettings{Host: "outlook.office365.com", Port: 993, Security: SecurityTLS},
		SMTP:       ServerSettings{Host: "smtp.office365.com", Port: 587, Security: SecurityStartTLS},
	},
}

// providerOrder is the order providers are offered in.
var providerOrder = []string{providerGoogle, providerMicrosoft}

// validTenant matches the Microsoft tenants that can go in a URL path.
var validTenant = regexp.MustCompile(`^[A-Za-z0-9.-]+$`)

// oauthProviderInfo is a provider as shown to users.
type oauthProviderInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// oauthState is a sign-in in progress.
type oauthState struct {
	UserID   string `json:"user_id"`
	Provider string `json:"provider"`
	Verifier string `json:"verifier"`
}

// availableProviders returns the providers the admin set up.
func (p *Plugin) availableProviders() []oauthProviderInfo {
	cfg := p.getConfiguration()
	providers := []oauthProviderInfo{}
	for _, id := range providerOrder {
		if provider, ok := p.providers[id]; ok {
			if clientID, _ := cfg.oauthClient(id); clientID != "" {
				providers = append(providers, oauthProviderInfo{ID: id, Name: provider.Name})
			}
		}
	}
	return providers
}

// oauthConfig returns a provider and its OAuth 2.0 client.
func (p *Plugin) oauthConfig(id string) (*oauthProvider, *oauth2.Config, error) {
	provider, ok := p.providers[id]
	cfg := p.getConfiguration()
	clientID, secret := cfg.oauthClient(id)
	if !ok || clientID == "" {
		return nil, nil, newAPIError(http.StatusNotFound, "signing in with this provider isn't set up on this server")
	}
	siteURL := ""
	if config := p.API.GetConfig(); config != nil && config.ServiceSettings.SiteURL != nil {
		siteURL = strings.TrimRight(*config.ServiceSettings.SiteURL, "/")
	}
	if siteURL == "" {
		return nil, nil, errors.New("the Site URL must be set to sign in with a provider")
	}
	tenant := cfg.microsoftTenant()
	if !validTenant.MatchString(tenant) {
		return nil, nil, errors.New("invalid Microsoft tenant")
	}

	endpoint := provider.Endpoint
	endpoint.AuthURL = strings.ReplaceAll(endpoint.AuthURL, "{tenant}", tenant)
	endpoint.TokenURL = strings.ReplaceAll(endpoint.TokenURL, "{tenant}", tenant)
	endpoint.AuthStyle = oauth2.AuthStyleInParams
	return provider, &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: secret,
		Endpoint:     endpoint,
		RedirectURL:  siteURL + "/plugins/" + manifest.Id + oauthCallbackPath,
		Scopes:       provider.Scopes,
	}, nil
}

// oauthContext returns the context of the requests to the providers.
func oauthContext() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(context.Background(), oauthTimeout)
	return context.WithValue(ctx, oauth2.HTTPClient, &http.Client{Timeout: oauthTimeout}), cancel
}

// handleStartOAuth starts signing in with a provider: it returns the provider's URL to open, which
// sends the user back to the callback.
func (p *Plugin) handleStartOAuth(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["provider"]
	provider, config, err := p.oauthConfig(id)
	if err != nil {
		p.writeError(w, err)
		return
	}

	state := model.NewId() + model.NewId()
	verifier := oauth2.GenerateVerifier()
	if err := p.store.SaveOAuthState(state, &oauthState{UserID: userID(r), Provider: id, Verifier: verifier}); err != nil {
		p.writeError(w, err)
		return
	}

	params := []oauth2.AuthCodeOption{oauth2.S256ChallengeOption(verifier)}
	for key, value := range provider.AuthParams {
		params = append(params, oauth2.SetAuthURLParam(key, value))
	}
	p.writeJSON(w, map[string]string{"url": config.AuthCodeURL(state, params...)})
}

// handleOAuthCallback ends signing in with a provider: the provider sends the user back here with
// an authorization code, exchanged for the tokens of the account, which is then connected. It
// answers with a page that tells the mail panel and closes itself.
func (p *Plugin) handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	state, err := p.store.TakeOAuthState(query.Get("state"))
	if err != nil {
		p.API.LogError("Failed to read an OAuth sign-in", "err", err.Error())
		p.writeOAuthPage(w, http.StatusInternalServerError, "", "Something went wrong, try again later.")
		return
	}
	if state == nil || state.UserID != userID(r) {
		p.writeOAuthPage(w, http.StatusBadRequest, "", "This sign-in expired. Start again from the mail panel.")
		return
	}
	if providerErr := query.Get("error"); providerErr != "" {
		message := query.Get("error_description")
		if message == "" {
			message = providerErr
		}
		p.writeOAuthPage(w, http.StatusBadRequest, "", "The sign-in was refused: "+message)
		return
	}

	account, err := p.connectOAuth(state, query.Get("code"))
	if err != nil {
		status := statusFor(err)
		message := err.Error()
		if status >= http.StatusInternalServerError && status != http.StatusBadGateway {
			p.API.LogError("Failed to connect a mailbox with OAuth", "provider", state.Provider, "err", err.Error())
			message = "Something went wrong, try again later."
		}
		p.writeOAuthPage(w, status, "", message)
		return
	}
	p.pool.drop(state.UserID)
	p.unread.Forget(state.UserID)
	p.writeOAuthPage(w, http.StatusOK, account.Email, "")
}

// connectOAuth exchanges an authorization code for the tokens of a user's account, checks that
// the provider's mail servers accept them, and saves the account.
func (p *Plugin) connectOAuth(state *oauthState, code string) (*Account, error) {
	provider, config, err := p.oauthConfig(state.Provider)
	if err != nil {
		return nil, err
	}
	ctx, cancel := oauthContext()
	defer cancel()
	token, err := config.Exchange(ctx, code, oauth2.VerifierOption(state.Verifier))
	if err != nil {
		return nil, &apiError{status: http.StatusBadGateway, err: errors.Wrapf(err, "%s refused the sign-in", provider.Name)}
	}
	if token.RefreshToken == "" {
		return nil, &apiError{status: http.StatusBadGateway, err: errors.Errorf("%s didn't grant lasting access to the mailbox", provider.Name)}
	}
	email, name, err := idTokenIdentity(token)
	if err != nil {
		return nil, &apiError{status: http.StatusBadGateway, err: errors.Wrapf(err, "%s didn't tell the address of the mailbox", provider.Name)}
	}

	account := &Account{
		Email: email,
		Name:  name,
		IMAP:  provider.IMAP,
		SMTP:  provider.SMTP,
		OAuth: &OAuthGrant{Provider: state.Provider, RefreshToken: token.RefreshToken},
	}
	account.IMAP.Username, account.SMTP.Username = email, email
	if existing, err := p.store.GetAccount(state.UserID); err == nil && existing != nil && existing.Email == email && existing.Name != "" {
		// Keep the name the user chose
		account.Name = existing.Name
	}
	if err := account.Validate(); err != nil {
		return nil, err
	}

	// Check that both servers accept the token before saving the account
	creds := credentials{token: token.AccessToken}
	release, err := p.limiter.acquire(state.UserID)
	if err != nil {
		return nil, err
	}
	defer release()
	session, err := dialIMAP(account.IMAP, creds, p.imapOptions())
	if err != nil {
		return nil, err
	}
	session.Close()
	if err := checkSMTP(account, creds, p.smtpOptions()); err != nil {
		return nil, err
	}

	if err := p.store.SaveAccount(state.UserID, account); err != nil {
		return nil, err
	}
	p.tokens.set(state.UserID, account.OAuth.RefreshToken, token)
	return account, nil
}

// idTokenIdentity returns the address and name of the account an OpenID Connect ID token is
// about. The token comes straight from the provider's token endpoint over TLS, so its signature
// needn't be checked (OpenID Connect Core 1.0, section 3.1.3.7).
func idTokenIdentity(token *oauth2.Token) (string, string, error) {
	raw, _ := token.Extra("id_token").(string)
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return "", "", errors.New("no ID token")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", "", errors.Wrap(err, "invalid ID token")
	}
	var claims struct {
		Email             string `json:"email"`
		PreferredUsername string `json:"preferred_username"`
		Name              string `json:"name"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", "", errors.Wrap(err, "invalid ID token")
	}
	// Microsoft only gives the address as the username of some accounts
	email := claims.Email
	if email == "" {
		email = claims.PreferredUsername
	}
	if address, err := mail.ParseAddress(email); err != nil || address.Address != email {
		return "", "", errors.New("no address in the ID token")
	}
	return email, claims.Name, nil
}

var oauthPage = template.Must(template.New("oauth").Parse(`<!DOCTYPE html>
<html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width">
<title>Mail</title>
<style>body{margin:0;display:grid;place-items:center;min-height:100vh;font:15px/1.5 system-ui,sans-serif;color:#1b1b1f;background:#f6f6f8}main{max-width:420px;padding:24px}</style>
</head><body><main>
{{if .Email}}<p>Your mailbox <b>{{.Email}}</b> is connected. You can close this window.</p>{{else}}<p>{{.Error}}</p><p>You can close this window.</p>{{end}}
</main>
<script nonce="{{.Nonce}}">
if (window.opener) { window.opener.postMessage({type: 'com.antimatterchat.mail.oauth', ok: {{.OK}}, error: {{.Error}}}, window.location.origin); }
{{if .OK}}window.close();{{end}}
</script>
</body></html>
`))

// writeOAuthPage answers the browser window the user signed in with: it tells the mail panel
// that opened it how the sign-in went, and closes itself when it went well.
func (p *Plugin) writeOAuthPage(w http.ResponseWriter, status int, email, errorMessage string) {
	nonce := model.NewId()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; script-src 'nonce-"+nonce+"'")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.WriteHeader(status)
	err := oauthPage.Execute(w, map[string]any{"Email": email, "Error": errorMessage, "OK": errorMessage == "", "Nonce": nonce})
	if err != nil {
		p.API.LogWarn("Failed to write the sign-in page", "err", err.Error())
	}
}

// tokenCache keeps the access tokens of OAuth accounts until they expire.
type tokenCache struct {
	mu     sync.Mutex
	tokens map[string]*cachedToken
}

// cachedToken is a user's access token, with the refresh token it was made from. Its lock is held
// while refreshing it, so a user's token is refreshed once at a time.
type cachedToken struct {
	mu           sync.Mutex
	refreshToken string
	token        *oauth2.Token
}

func newTokenCache() *tokenCache {
	return &tokenCache{tokens: map[string]*cachedToken{}}
}

func (c *tokenCache) entry(userID string) *cachedToken {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.tokens[userID]
	if !ok {
		entry = &cachedToken{}
		c.tokens[userID] = entry
	}
	return entry
}

func (c *tokenCache) set(userID, refreshToken string, token *oauth2.Token) {
	entry := c.entry(userID)
	entry.mu.Lock()
	defer entry.mu.Unlock()
	entry.refreshToken, entry.token = refreshToken, token
}

func (c *tokenCache) forget(userID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.tokens, userID)
}

// accessToken returns an access token of a user's OAuth account, refreshing it when it expires.
// Providers may give a new refresh token with it, which is saved.
func (p *Plugin) accessToken(userID string, account *Account) (string, error) {
	entry := p.tokens.entry(userID)
	entry.mu.Lock()
	defer entry.mu.Unlock()
	if entry.token != nil && entry.refreshToken == account.OAuth.RefreshToken && entry.token.AccessToken != "" &&
		time.Until(entry.token.Expiry) > tokenRefreshMargin {
		return entry.token.AccessToken, nil
	}

	provider, config, err := p.oauthConfig(account.OAuth.Provider)
	if err != nil {
		return "", err
	}
	ctx, cancel := oauthContext()
	defer cancel()
	token, err := config.TokenSource(ctx, &oauth2.Token{RefreshToken: account.OAuth.RefreshToken}).Token()
	if err != nil {
		var retrieveErr *oauth2.RetrieveError
		if errors.As(err, &retrieveErr) && retrieveErr.ErrorCode == "invalid_grant" {
			return "", newAPIError(http.StatusConflict, "your "+provider.Name+" sign-in expired or was revoked: sign in again in the mail account settings")
		}
		return "", &apiError{status: http.StatusBadGateway, err: errors.Wrapf(err, "couldn't renew the %s sign-in", provider.Name)}
	}

	if token.RefreshToken != "" && token.RefreshToken != account.OAuth.RefreshToken {
		account.OAuth.RefreshToken = token.RefreshToken
		if err := p.store.SaveAccount(userID, account); err != nil {
			return "", err
		}
	}
	entry.refreshToken, entry.token = account.OAuth.RefreshToken, token
	return token.AccessToken, nil
}
