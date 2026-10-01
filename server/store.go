// Copyright (c) 2026-present Antimatter contributors.
// See LICENSE.txt for license information.

package main

import (
	"encoding/json"
	"net/http"
	"net/mail"
	"strings"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/pkg/errors"
)

const (
	// accountKeyPrefix prefixes the key of the mail account of each user.
	accountKeyPrefix = "account_"

	atomicUpdateAttempts = 5
)

// kvAPI is the part of the plugin API used to store data (implemented by plugin.API).
type kvAPI interface {
	KVGet(key string) ([]byte, *model.AppError)
	KVSetWithOptions(key string, value []byte, options model.PluginKVSetOptions) (bool, *model.AppError)
	KVDelete(key string) *model.AppError
}

// Security is how the connection to a mail server is secured.
type Security string

const (
	// SecurityTLS is implicit TLS (IMAPS on port 993, SMTPS on port 465).
	SecurityTLS Security = "tls"
	// SecurityStartTLS upgrades a plain connection with STARTTLS (IMAP on port 143, submission on port 587).
	SecurityStartTLS Security = "starttls"
	// SecurityNone doesn't encrypt the connection. Admins must allow it.
	SecurityNone Security = "none"
)

// ServerSettings say how to connect to an IMAP or SMTP server.
type ServerSettings struct {
	Host     string   `json:"host"`
	Port     int      `json:"port"`
	Security Security `json:"security"`
	Username string   `json:"username"`
}

func (s ServerSettings) validate(what string) error {
	if s.Host == "" || strings.ContainsAny(s.Host, " /\\@") {
		return newAPIError(http.StatusBadRequest, what+": invalid server name")
	}
	if s.Port < 1 || s.Port > 65535 {
		return newAPIError(http.StatusBadRequest, what+": invalid port")
	}
	switch s.Security {
	case SecurityTLS, SecurityStartTLS, SecurityNone:
	default:
		return newAPIError(http.StatusBadRequest, what+": invalid connection security")
	}
	if s.Username == "" {
		return newAPIError(http.StatusBadRequest, what+": a username is required")
	}
	return nil
}

// Account is the mail account of a user. Passwords are only kept decrypted in memory.
type Account struct {
	// Email is the address mail is sent from.
	Email string `json:"email"`
	// Name is the name mail is sent from.
	Name string `json:"name"`

	IMAP ServerSettings `json:"imap"`
	SMTP ServerSettings `json:"smtp"`

	IMAPPassword string `json:"-"`
	SMTPPassword string `json:"-"`
}

// Validate checks the account settings, but not whether the servers accept them.
func (a *Account) Validate() error {
	if _, err := mail.ParseAddress(a.Email); err != nil || strings.ContainsAny(a.Email, "<>") {
		return newAPIError(http.StatusBadRequest, "invalid email address")
	}
	if err := a.IMAP.validate("IMAP"); err != nil {
		return err
	}
	if err := a.SMTP.validate("SMTP"); err != nil {
		return err
	}
	if a.IMAPPassword == "" || a.SMTPPassword == "" {
		return newAPIError(http.StatusBadRequest, "a password is required")
	}
	return nil
}

// storedAccount is an account as stored in the KV store, with encrypted passwords.
type storedAccount struct {
	Account
	IMAPPasswordSealed string `json:"imap_password"`
	SMTPPasswordSealed string `json:"smtp_password"`
	UpdatedAt          int64  `json:"updated_at"`
}

// Store keeps the users' mail accounts in the plugin's KV store.
type Store struct {
	kv  kvAPI
	box *secretBox
}

func NewStore(kv kvAPI, box *secretBox) *Store {
	return &Store{kv: kv, box: box}
}

func accountKey(userID string) string {
	return accountKeyPrefix + userID
}

func secretContext(userID, field string) string {
	return userID + "/" + field
}

// GetAccount returns the mail account of a user, or nil if the user has none.
func (s *Store) GetAccount(userID string) (*Account, error) {
	data, appErr := s.kv.KVGet(accountKey(userID))
	if appErr != nil {
		return nil, errors.Wrap(appErr, "failed to read the mail account")
	}
	if data == nil {
		return nil, nil
	}

	var stored storedAccount
	if err := json.Unmarshal(data, &stored); err != nil {
		return nil, errors.Wrap(err, "failed to decode the mail account")
	}
	account := stored.Account

	var err error
	if account.IMAPPassword, err = s.box.Open(stored.IMAPPasswordSealed, secretContext(userID, "imap")); err != nil {
		return nil, errors.Wrap(err, "failed to decrypt the IMAP password")
	}
	if account.SMTPPassword, err = s.box.Open(stored.SMTPPasswordSealed, secretContext(userID, "smtp")); err != nil {
		return nil, errors.Wrap(err, "failed to decrypt the SMTP password")
	}
	return &account, nil
}

// SaveAccount stores the mail account of a user, encrypting the passwords.
func (s *Store) SaveAccount(userID string, account *Account) error {
	stored := storedAccount{Account: *account, UpdatedAt: model.GetMillis()}

	var err error
	if stored.IMAPPasswordSealed, err = s.box.Seal(account.IMAPPassword, secretContext(userID, "imap")); err != nil {
		return err
	}
	if stored.SMTPPasswordSealed, err = s.box.Seal(account.SMTPPassword, secretContext(userID, "smtp")); err != nil {
		return err
	}

	data, err := json.Marshal(stored)
	if err != nil {
		return errors.Wrap(err, "failed to encode the mail account")
	}
	if _, appErr := s.kv.KVSetWithOptions(accountKey(userID), data, model.PluginKVSetOptions{}); appErr != nil {
		return errors.Wrap(appErr, "failed to save the mail account")
	}
	return nil
}

// DeleteAccount forgets the mail account of a user.
func (s *Store) DeleteAccount(userID string) error {
	if appErr := s.kv.KVDelete(accountKey(userID)); appErr != nil {
		return errors.Wrap(appErr, "failed to delete the mail account")
	}
	return nil
}
