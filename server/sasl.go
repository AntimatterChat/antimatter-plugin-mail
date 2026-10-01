// Copyright (c) 2026-present Antimatter contributors.
// See LICENSE.txt for license information.

package main

import (
	"github.com/emersion/go-sasl"
)

// credentials sign in to a mail server: a password, or an OAuth 2.0 access token.
type credentials struct {
	password string
	token    string
}

func passwordCredentials(password string) credentials {
	return credentials{password: password}
}

// mechXOAuth2 is the SASL mechanism Google and Microsoft use for OAuth 2.0 access tokens.
const mechXOAuth2 = "XOAUTH2"

// xoauth2Client signs in with an OAuth 2.0 access token, with the XOAUTH2 mechanism.
type xoauth2Client struct {
	username string
	token    string
}

func (c *xoauth2Client) Start() (string, []byte, error) {
	return mechXOAuth2, []byte("user=" + c.username + "\x01auth=Bearer " + c.token + "\x01\x01"), nil
}

// Next answers the challenge a server sends when it refuses the token, which details the error:
// the empty response asks it to end the exchange with the failure.
func (c *xoauth2Client) Next([]byte) ([]byte, error) {
	return []byte{}, nil
}

// oauthClient returns the SASL client that signs in with an access token, with a mechanism the
// server offers, or nil if it offers none.
func oauthClient(username, token string, offers func(mech string) bool) sasl.Client {
	switch {
	case offers(mechXOAuth2):
		return &xoauth2Client{username: username, token: token}
	case offers(sasl.OAuthBearer):
		return sasl.NewOAuthBearerClient(&sasl.OAuthBearerOptions{Username: username, Token: token})
	default:
		return nil
	}
}
