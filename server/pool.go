// Copyright (c) 2026-present Antimatter contributors.
// See LICENSE.txt for license information.

package main

import (
	"net/http"
	"sync"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/pkg/errors"
)

const (
	// poolIdleTimeout is how long an unused connection stays open for the next request.
	poolIdleTimeout = 3 * time.Minute
	// poolMaxIdlePerUser is the number of unused connections kept open per user.
	poolMaxIdlePerUser = 2
	// poolMaxIdle is the number of unused connections kept open overall.
	poolMaxIdle = 100
	// poolCheckAfter is how long a connection can stay unused before it's checked with a NOOP
	// when taken from the pool: the server may have dropped it.
	poolCheckAfter = 30 * time.Second
	// poolCheckTimeout bounds that check.
	poolCheckTimeout = 10 * time.Second
	// poolSweepInterval is how often connections unused for too long are closed.
	poolSweepInterval = 30 * time.Second
	// logoutTimeout bounds how long closing a connection waits for the server.
	logoutTimeout = 5 * time.Second
)

// pooledConn is a connection to a user's IMAP server, with the connection slot it holds.
type pooledConn struct {
	session *imapSession
	release func()
	// key identifies the account settings the connection was made with.
	key      string
	lastUsed time.Time
}

func (c *pooledConn) close() {
	c.session.begin(logoutTimeout)
	c.session.Close()
	c.release()
}

// imapPool keeps the IMAP connections of recent requests open for the next ones, so that a user
// clicking through their mail doesn't log in for each request. The open connections keep their
// connection slots: the pool gives up its oldest connection when the server runs out of slots.
type imapPool struct {
	mu     sync.Mutex
	idle   map[string][]*pooledConn // by user, oldest first
	count  int
	closed bool

	maxIdle, maxIdlePerUser int
	idleTimeout             time.Duration
	now                     func() time.Time
}

func newIMAPPool() *imapPool {
	return &imapPool{
		idle:           map[string][]*pooledConn{},
		maxIdle:        poolMaxIdle,
		maxIdlePerUser: poolMaxIdlePerUser,
		idleTimeout:    poolIdleTimeout,
		now:            time.Now,
	}
}

// take returns an unused connection of a user made with the given account settings, or nil.
// Connections made with other settings are closed.
func (p *imapPool) take(userID, key string) *pooledConn {
	p.mu.Lock()
	var found *pooledConn
	var stale []*pooledConn
	conns := p.idle[userID]
	// The newest connection is the most likely to still be open
	for i := len(conns) - 1; i >= 0; i-- {
		switch {
		case conns[i].key != key || conns[i].session.isClosed():
			stale = append(stale, conns[i])
		case found == nil:
			found = conns[i]
		default:
			continue
		}
		conns = append(conns[:i], conns[i+1:]...)
		p.count--
	}
	p.setUserConns(userID, conns)
	p.mu.Unlock()

	for _, c := range stale {
		c.close()
	}
	return found
}

func (p *imapPool) setUserConns(userID string, conns []*pooledConn) {
	if len(conns) == 0 {
		delete(p.idle, userID)
	} else {
		p.idle[userID] = conns
	}
}

// put gives back a connection for later requests, or closes it if the pool is full.
func (p *imapPool) put(userID string, c *pooledConn) {
	c.lastUsed = p.now()
	p.mu.Lock()
	if p.closed || c.session.isClosed() {
		p.mu.Unlock()
		c.close()
		return
	}
	var evicted *pooledConn
	conns := p.idle[userID]
	switch {
	case len(conns) >= p.maxIdlePerUser:
		evicted, conns = conns[0], conns[1:]
	case p.count >= p.maxIdle:
		evicted = p.removeOldestLocked()
		conns = p.idle[userID]
		p.count++
	default:
		p.count++
	}
	p.idle[userID] = append(conns, c)
	p.mu.Unlock()

	if evicted != nil {
		evicted.close()
	}
}

// removeOldestLocked removes the connection unused for the longest time from the pool.
func (p *imapPool) removeOldestLocked() *pooledConn {
	var oldestUser string
	var oldest *pooledConn
	for userID, conns := range p.idle {
		if oldest == nil || conns[0].lastUsed.Before(oldest.lastUsed) {
			oldestUser, oldest = userID, conns[0]
		}
	}
	if oldest == nil {
		return nil
	}
	p.setUserConns(oldestUser, p.idle[oldestUser][1:])
	p.count--
	return oldest
}

// evictOldest closes the connection unused for the longest time, giving its connection slot
// back. It returns false if the pool is empty.
func (p *imapPool) evictOldest() bool {
	p.mu.Lock()
	oldest := p.removeOldestLocked()
	p.mu.Unlock()
	if oldest == nil {
		return false
	}
	oldest.close()
	return true
}

// drop closes the unused connections of a user, e.g. when their account changes.
func (p *imapPool) drop(userID string) {
	p.mu.Lock()
	conns := p.idle[userID]
	delete(p.idle, userID)
	p.count -= len(conns)
	p.mu.Unlock()

	for _, c := range conns {
		c.close()
	}
}

// sweep closes the connections unused for too long.
func (p *imapPool) sweep() {
	now := p.now()
	var expired []*pooledConn
	p.mu.Lock()
	for userID, conns := range p.idle {
		kept := conns[:0]
		for _, c := range conns {
			if now.Sub(c.lastUsed) >= p.idleTimeout || c.session.isClosed() {
				expired = append(expired, c)
			} else {
				kept = append(kept, c)
			}
		}
		p.count -= len(conns) - len(kept)
		p.setUserConns(userID, kept)
	}
	p.mu.Unlock()

	for _, c := range expired {
		c.close()
	}
}

// Run closes the connections unused for too long until stop is closed, then closes them all.
func (p *imapPool) Run(stop <-chan struct{}) {
	ticker := time.NewTicker(poolSweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			p.closeAll()
			return
		case <-ticker.C:
			p.sweep()
		}
	}
}

func (p *imapPool) closeAll() {
	p.mu.Lock()
	p.closed = true
	all := p.idle
	p.idle = map[string][]*pooledConn{}
	p.count = 0
	p.mu.Unlock()

	for _, conns := range all {
		for _, c := range conns {
			c.close()
		}
	}
}

// reusable returns whether a connection can serve other requests after an operation failed with
// the given error: the server answered, so the connection still works.
func reusable(err error) bool {
	if err == nil {
		return true
	}
	var imapErr *imap.Error
	if errors.As(err, &imapErr) {
		return true
	}
	var apiErr *apiError
	return errors.As(err, &apiErr) && apiErr.status < http.StatusInternalServerError
}
