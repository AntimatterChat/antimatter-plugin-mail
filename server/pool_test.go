// Copyright (c) 2026-present Antimatter contributors.
// See LICENSE.txt for license information.

package main

import (
	"net/http"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReusable(t *testing.T) {
	assert.True(t, reusable(nil))
	assert.True(t, reusable(&imap.Error{Type: imap.StatusResponseTypeNo, Text: "no such folder"}))
	assert.True(t, reusable(errMessageNotFound))
	assert.False(t, reusable(connectionError("IMAP", assert.AnError)))
	assert.False(t, reusable(assert.AnError))
}

func TestIMAPPool(t *testing.T) {
	server := startIMAPServer(t, imap.CapSet{imap.CapIMAP4rev1: {}})
	limiter := newConnLimiter(10, 4, 10*time.Millisecond)
	conn := func(userID, key string) *pooledConn {
		release, err := limiter.acquire(userID)
		require.NoError(t, err)
		session, err := dialIMAP(server.settings, "secret", testIMAPOptions)
		require.NoError(t, err)
		return &pooledConn{session: session, release: release, key: key}
	}

	now := time.Now()
	pool := newIMAPPool()
	pool.now = func() time.Time { return now }
	pool.maxIdle = 3
	t.Cleanup(pool.closeAll)

	assert.Nil(t, pool.take("a", "k1"))
	a1, a2, a3 := conn("a", "k1"), conn("a", "k1"), conn("a", "k1")
	pool.put("a", a1)
	pool.put("a", a2)
	pool.put("a", a3)
	assert.Equal(t, 2, pool.count, "a user keeps two unused connections")
	assert.True(t, a1.session.isClosed(), "the oldest one is closed")
	assert.Equal(t, 2, limiter.perUser["a"])

	assert.Same(t, a3, pool.take("a", "k1"), "the newest connection is taken first")
	pool.put("a", a3)

	// Connections made with other settings are closed
	assert.Nil(t, pool.take("a", "k2"))
	assert.Equal(t, 0, pool.count)
	assert.True(t, a2.session.isClosed())
	assert.True(t, a3.session.isClosed())
	assert.Empty(t, limiter.perUser)

	// The pool keeps the most recently used connections overall
	b, c, d := conn("b", "k"), conn("c", "k"), conn("d", "k")
	pool.put("b", b)
	now = now.Add(time.Second)
	pool.put("c", c)
	now = now.Add(time.Second)
	pool.put("d", d)
	e := conn("e", "k")
	now = now.Add(time.Second)
	pool.put("e", e)
	assert.Equal(t, 3, pool.count)
	assert.True(t, b.session.isClosed())
	assert.Nil(t, pool.take("b", "k"))

	// The oldest connection gives its slot back on demand
	assert.True(t, pool.evictOldest())
	assert.True(t, c.session.isClosed())
	assert.Equal(t, 2, pool.count)

	// Connections unused for too long are closed
	now = now.Add(poolIdleTimeout - time.Second)
	pool.sweep()
	assert.Equal(t, 1, pool.count)
	assert.True(t, d.session.isClosed())
	assert.False(t, e.session.isClosed())

	pool.drop("e")
	assert.Equal(t, 0, pool.count)
	assert.True(t, e.session.isClosed())
	assert.False(t, pool.evictOldest())
	assert.Empty(t, limiter.perUser)
	assert.Empty(t, limiter.slots)
}

func TestAPIReusesConnections(t *testing.T) {
	e := newTestEnv(t)
	e.connect(t)
	connected := e.imap.connections.Load()

	for range 3 {
		w := e.request(t, http.MethodGet, "/api/v1/folders", nil)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		w = e.request(t, http.MethodGet, "/api/v1/messages?folder=INBOX", nil)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	}
	assert.Equal(t, connected+1, e.imap.connections.Load(), "the requests share one connection")

	// A failed operation keeps a working connection
	w := e.request(t, http.MethodGet, "/api/v1/messages?folder=Nope", nil)
	require.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
	w = e.request(t, http.MethodGet, "/api/v1/folders", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, connected+1, e.imap.connections.Load())

	// Saving the account again closes the connections made with the old settings
	e.connect(t)
	assert.Zero(t, e.p.pool.count)
}

func TestLimiterReclaimsPooledConnections(t *testing.T) {
	e := newTestEnv(t)
	e.p.limiter = newConnLimiter(1, 1, 10*time.Millisecond)
	e.p.limiter.reclaim = e.p.pool.evictOldest
	e.connect(t)

	w := e.request(t, http.MethodGet, "/api/v1/folders", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, 1, e.p.pool.count)

	// Another user's request takes the only slot, which the unused connection gives back
	e.userID = "other"
	release, err := e.p.limiter.acquire("other")
	require.NoError(t, err)
	release()
	assert.Zero(t, e.p.pool.count)
}
