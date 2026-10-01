// Copyright (c) 2026-present Antimatter contributors.
// See LICENSE.txt for license information.

package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestStore(t *testing.T) (*Store, *fakeKV) {
	t.Helper()
	kv := newFakeKV()
	key, err := loadEncryptionKey(kv)
	require.NoError(t, err)
	box, err := newSecretBox(key)
	require.NoError(t, err)
	return NewStore(kv, box), kv
}

func testAccount() *Account {
	return &Account{
		Email:        "ada@example.com",
		Name:         "Ada",
		IMAP:         ServerSettings{Host: "imap.example.com", Port: 993, Security: SecurityTLS, Username: "ada"},
		SMTP:         ServerSettings{Host: "smtp.example.com", Port: 587, Security: SecurityStartTLS, Username: "ada"},
		IMAPPassword: "imap-secret",
		SMTPPassword: "smtp-secret",
	}
}

func TestStoreAccount(t *testing.T) {
	store, kv := newTestStore(t)

	account, err := store.GetAccount("user1")
	require.NoError(t, err)
	assert.Nil(t, account)

	require.NoError(t, store.SaveAccount("user1", testAccount()))
	assert.NotContains(t, string(kv.data[accountKey("user1")]), "imap-secret")
	assert.NotContains(t, string(kv.data[accountKey("user1")]), "smtp-secret")

	account, err = store.GetAccount("user1")
	require.NoError(t, err)
	assert.Equal(t, testAccount(), account)

	// A stored account copied to another user can't be decrypted
	kv.data[accountKey("user2")] = kv.data[accountKey("user1")]
	_, err = store.GetAccount("user2")
	assert.Error(t, err)

	require.NoError(t, store.DeleteAccount("user1"))
	account, err = store.GetAccount("user1")
	require.NoError(t, err)
	assert.Nil(t, account)
}

func TestStoreOAuthAccount(t *testing.T) {
	store, kv := newTestStore(t)

	account := testAccount()
	account.IMAPPassword, account.SMTPPassword = "", ""
	account.OAuth = &OAuthGrant{Provider: "google", RefreshToken: "refresh-secret"}
	require.NoError(t, account.Validate())
	require.NoError(t, store.SaveAccount("user1", account))
	assert.NotContains(t, string(kv.data[accountKey("user1")]), "refresh-secret")
	assert.NotContains(t, string(kv.data[accountKey("user1")]), "imap_password")

	stored, err := store.GetAccount("user1")
	require.NoError(t, err)
	assert.Equal(t, account, stored)

	kv.data[accountKey("user2")] = kv.data[accountKey("user1")]
	_, err = store.GetAccount("user2")
	assert.Error(t, err)

	account.OAuth.RefreshToken = ""
	assert.Error(t, account.Validate())
}

func TestAccountValidate(t *testing.T) {
	assert.NoError(t, testAccount().Validate())

	for name, change := range map[string]func(a *Account){
		"email":         func(a *Account) { a.Email = "not an address" },
		"email name":    func(a *Account) { a.Email = "Ada <ada@example.com>" },
		"host":          func(a *Account) { a.IMAP.Host = "" },
		"host path":     func(a *Account) { a.SMTP.Host = "smtp.example.com/x" },
		"port":          func(a *Account) { a.IMAP.Port = 0 },
		"big port":      func(a *Account) { a.SMTP.Port = 70000 },
		"security":      func(a *Account) { a.IMAP.Security = "ssl" },
		"username":      func(a *Account) { a.SMTP.Username = "" },
		"imap password": func(a *Account) { a.IMAPPassword = "" },
		"smtp password": func(a *Account) { a.SMTPPassword = "" },
	} {
		t.Run(name, func(t *testing.T) {
			a := testAccount()
			change(a)
			assert.Error(t, a.Validate())
		})
	}
}
