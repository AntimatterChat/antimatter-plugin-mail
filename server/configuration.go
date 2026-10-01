// Copyright (c) 2019-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package main

import (
	"reflect"
	"strings"
	"time"

	"github.com/pkg/errors"
)

// configuration captures the plugin's external configuration as exposed in the Antimatter server
// configuration, as well as values computed from the configuration. Any public fields will be
// deserialized from the Antimatter server configuration in OnConfigurationChange.
//
// As plugins are inherently concurrent (hooks being called asynchronously), and the plugin
// configuration can change at any time, access to the configuration must be synchronized. The
// strategy used in this plugin is to guard a pointer to the configuration, and clone the entire
// struct whenever it changes. You may replace this with whatever strategy you choose.
type configuration struct {
	// AllowPrivateNetworks lets users connect to mail servers on loopback, private and link-local
	// addresses. Off by default, so users can't reach the internal services of the network the
	// server runs in.
	AllowPrivateNetworks bool

	// AllowInsecureConnections lets users connect to mail servers without TLS.
	AllowInsecureConnections bool

	// UnreadCheckMinutes is how often the unread count of a user's inbox is checked while they
	// are online.
	UnreadCheckMinutes int

	// MaxMessageSizeMB is the largest message the plugin opens.
	MaxMessageSizeMB int

	// The OAuth 2.0 clients users sign in to Google and Microsoft with. A provider is offered
	// when its client ID and secret are set.
	GoogleClientID        string
	GoogleClientSecret    string
	MicrosoftClientID     string
	MicrosoftClientSecret string
	// MicrosoftTenant is the Microsoft Entra tenant users sign in to: "common" (the default) for
	// any work, school or personal account, "organizations", "consumers", or a tenant ID or domain.
	MicrosoftTenant string
}

const (
	defaultUnreadCheckMinutes = 2
	defaultMaxMessageSizeMB   = 25
)

// unreadCheckInterval returns how often unread counts are checked.
func (c *configuration) unreadCheckInterval() time.Duration {
	if c.UnreadCheckMinutes < 1 {
		return defaultUnreadCheckMinutes * time.Minute
	}
	return time.Duration(c.UnreadCheckMinutes) * time.Minute
}

// maxMessageSize returns the size of the largest message the plugin opens, in bytes.
func (c *configuration) maxMessageSize() int64 {
	if c.MaxMessageSizeMB < 1 {
		return defaultMaxMessageSizeMB << 20
	}
	return int64(c.MaxMessageSizeMB) << 20
}

// oauthClient returns the OAuth 2.0 client ID and secret of a provider, empty if it isn't set up.
func (c *configuration) oauthClient(provider string) (string, string) {
	var id, secret string
	switch provider {
	case providerGoogle:
		id, secret = c.GoogleClientID, c.GoogleClientSecret
	case providerMicrosoft:
		id, secret = c.MicrosoftClientID, c.MicrosoftClientSecret
	}
	id, secret = strings.TrimSpace(id), strings.TrimSpace(secret)
	if id == "" || secret == "" {
		return "", ""
	}
	return id, secret
}

// microsoftTenant returns the Microsoft Entra tenant users sign in to.
func (c *configuration) microsoftTenant() string {
	tenant := strings.TrimSpace(c.MicrosoftTenant)
	if tenant == "" {
		return "common"
	}
	return tenant
}

// Clone shallow copies the configuration. Your implementation may require a deep copy if
// your configuration has reference types.
func (c *configuration) Clone() *configuration {
	clone := *c
	return &clone
}

// getConfiguration retrieves the active configuration under lock, making it safe to use
// concurrently. The active configuration may change underneath the client of this method, but
// the struct returned by this API call is considered immutable.
func (p *Plugin) getConfiguration() *configuration {
	p.configurationLock.RLock()
	defer p.configurationLock.RUnlock()

	if p.configuration == nil {
		return &configuration{}
	}

	return p.configuration
}

// setConfiguration replaces the active configuration under lock.
//
// Do not call setConfiguration while holding the configurationLock, as sync.Mutex is not
// reentrant. In particular, avoid using the plugin API entirely, as this may in turn trigger a
// hook back into the plugin. If that hook attempts to acquire this lock, a deadlock may occur.
//
// This method panics if setConfiguration is called with the existing configuration. This almost
// certainly means that the configuration was modified without being cloned and may result in
// an unsafe access.
func (p *Plugin) setConfiguration(configuration *configuration) {
	p.configurationLock.Lock()
	defer p.configurationLock.Unlock()

	if configuration != nil && p.configuration == configuration {
		// Ignore assignment if the configuration struct is empty. Go will optimize the
		// allocation for same to point at the same memory address, breaking the check
		// above.
		if reflect.ValueOf(*configuration).NumField() == 0 {
			return
		}

		panic("setConfiguration called with the existing configuration")
	}

	p.configuration = configuration
}

// OnConfigurationChange is invoked when configuration changes may have been made.
func (p *Plugin) OnConfigurationChange() error {
	configuration := new(configuration)

	// Load the public configuration fields from the Antimatter server configuration.
	if err := p.API.LoadPluginConfiguration(configuration); err != nil {
		return errors.Wrap(err, "failed to load plugin configuration")
	}

	p.setConfiguration(configuration)

	return nil
}
