// Copyright (c) 2026-present Antimatter contributors.
// See LICENSE.txt for license information.

package main

import (
	"crypto/tls"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin"
	"github.com/pkg/errors"
)

// wsEventUnread tells a user the unread count of their inbox changed. Clients receive it as
// custom_com.antimatterchat.mail_unread.
const wsEventUnread = "unread"

// Plugin implements the interface expected by the Antimatter server to communicate between the
// server and plugin processes.
type Plugin struct {
	plugin.MattermostPlugin

	// configurationLock synchronizes access to the configuration.
	configurationLock sync.RWMutex

	// configuration is the active plugin configuration. Consult getConfiguration and
	// setConfiguration for usage.
	configuration *configuration

	store   *Store
	limiter *connLimiter
	pool    *imapPool
	router  *mux.Router
	unread  *unreadWatcher

	// stopWatching stops the unread watcher and the connection pool, set while the plugin is
	// active.
	stopWatching chan struct{}

	// tlsConfig is nil for the default TLS configuration (tests set their own CA).
	tlsConfig *tls.Config
}

// OnActivate is invoked when the plugin is activated.
func (p *Plugin) OnActivate() error {
	key, err := loadEncryptionKey(p.API)
	if err != nil {
		return err
	}
	box, err := newSecretBox(key)
	if err != nil {
		return errors.Wrap(err, "failed to set up the encryption")
	}

	p.store = NewStore(p.API, box)
	p.limiter = newConnLimiter(maxConnections, maxConnectionsPerUser, connectionWait)
	p.pool = newIMAPPool()
	p.limiter.reclaim = p.pool.evictOldest
	p.router = p.newRouter()

	p.unread = newUnreadWatcher(p.checkUnread, p.publishUnread, func() time.Duration {
		return p.getConfiguration().unreadCheckInterval()
	})
	p.stopWatching = make(chan struct{})
	go p.unread.Run(p.stopWatching)
	go p.pool.Run(p.stopWatching)
	return nil
}

// OnDeactivate is invoked when the plugin is deactivated.
func (p *Plugin) OnDeactivate() error {
	if p.stopWatching != nil {
		close(p.stopWatching)
		p.stopWatching = nil
	}
	return nil
}

// checkUnread returns the unread count of a user's inbox.
func (p *Plugin) checkUnread(userID string) (int, error) {
	var unread int
	err := p.withIMAP(userID, func(_ *Account, session *imapSession) error {
		var err error
		unread, err = session.Unread("INBOX")
		return err
	})
	if errors.Is(err, errNoAccount) {
		p.unread.Forget(userID)
	}
	return unread, err
}

// publishUnread tells a user the unread count of their inbox.
func (p *Plugin) publishUnread(userID string, unread int) {
	p.API.PublishWebSocketEvent(wsEventUnread, map[string]any{"unread": unread}, &model.WebsocketBroadcast{UserId: userID})
}

// ServeHTTP serves the plugin's REST API.
func (p *Plugin) ServeHTTP(_ *plugin.Context, w http.ResponseWriter, r *http.Request) {
	p.router.ServeHTTP(w, r)
}
