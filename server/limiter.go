// Copyright (c) 2026-present Antimatter contributors.
// See LICENSE.txt for license information.

package main

import (
	"net/http"
	"sync"
	"time"
)

const (
	// maxConnectionsPerUser is the number of connections to mail servers a user can have open at
	// once.
	maxConnectionsPerUser = 4
	// maxConnections is the number of connections to mail servers the plugin opens at once.
	maxConnections = 200
	// connectionWait is how long a request waits for a connection slot.
	connectionWait = 10 * time.Second
)

var (
	errTooManyUserConnections = newAPIError(http.StatusTooManyRequests, "too many mail requests at once, try again in a moment")
	errTooManyConnections     = newAPIError(http.StatusServiceUnavailable, "the mail service is busy, try again in a moment")
)

// connLimiter bounds the number of connections to mail servers, per user and overall.
type connLimiter struct {
	mu      sync.Mutex
	perUser map[string]int
	slots   chan struct{}
	maxUser int
	wait    time.Duration

	// reclaim closes an unused connection to give its slot back, when all the slots are taken. It
	// returns false if there's none.
	reclaim func() bool
}

func newConnLimiter(maxTotal, maxPerUser int, wait time.Duration) *connLimiter {
	return &connLimiter{perUser: map[string]int{}, slots: make(chan struct{}, maxTotal), maxUser: maxPerUser, wait: wait}
}

// acquire takes a connection slot for a user, waiting for one if needed. It returns the function
// that gives it back.
func (l *connLimiter) acquire(userID string) (func(), error) {
	return l.take(userID, true)
}

// tryAcquire takes a connection slot for a user if one is free right away, for connections that
// can do without. It returns the function that gives it back.
func (l *connLimiter) tryAcquire(userID string) (func(), error) {
	return l.take(userID, false)
}

func (l *connLimiter) take(userID string, wait bool) (func(), error) {
	l.mu.Lock()
	if l.perUser[userID] >= l.maxUser {
		l.mu.Unlock()
		return nil, errTooManyUserConnections
	}
	l.perUser[userID]++
	l.mu.Unlock()

	releaseUser := func() {
		l.mu.Lock()
		l.perUser[userID]--
		if l.perUser[userID] == 0 {
			delete(l.perUser, userID)
		}
		l.mu.Unlock()
	}

	if !l.takeSlot(wait) {
		releaseUser()
		return nil, errTooManyConnections
	}

	var once sync.Once
	return func() {
		once.Do(func() {
			<-l.slots
			releaseUser()
		})
	}, nil
}

func (l *connLimiter) takeSlot(wait bool) bool {
	select {
	case l.slots <- struct{}{}:
		return true
	default:
	}
	if !wait {
		return false
	}

	// Unused connections give their slots back before anyone waits
	for l.reclaim != nil && l.reclaim() {
		select {
		case l.slots <- struct{}{}:
			return true
		default:
		}
	}

	timer := time.NewTimer(l.wait)
	defer timer.Stop()
	select {
	case l.slots <- struct{}{}:
		return true
	case <-timer.C:
		return false
	}
}

// userConnections returns the number of connection slots a user holds.
func (l *connLimiter) userConnections(userID string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.perUser[userID]
}
