// Copyright (c) 2026-present Antimatter contributors.
// See LICENSE.txt for license information.

package main

import (
	"slices"
	"testing"

	"github.com/emersion/go-sasl"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOAuthClient(t *testing.T) {
	offers := func(mechs ...string) func(string) bool {
		return func(mech string) bool { return slices.Contains(mechs, mech) }
	}

	client := oauthClient("ada@example.com", "tok", offers(sasl.OAuthBearer, mechXOAuth2))
	mech, ir, err := client.Start()
	require.NoError(t, err)
	assert.Equal(t, "XOAUTH2", mech)
	assert.Equal(t, "user=ada@example.com\x01auth=Bearer tok\x01\x01", string(ir))
	response, err := client.Next([]byte(`{"status":"401"}`))
	require.NoError(t, err)
	assert.Empty(t, response, "a refused token ends the exchange")

	client = oauthClient("ada@example.com", "tok", offers(sasl.OAuthBearer))
	mech, _, err = client.Start()
	require.NoError(t, err)
	assert.Equal(t, sasl.OAuthBearer, mech)

	assert.Nil(t, oauthClient("ada@example.com", "tok", offers(sasl.Plain)))
}
