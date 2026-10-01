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
}

func newConnLimiter(maxTotal, maxPerUser int, wait time.Duration) *connLimiter {
	return &connLimiter{perUser: map[string]int{}, slots: make(chan struct{}, maxTotal), maxUser: maxPerUser, wait: wait}
}

// acquire takes a connection slot for a user. It returns the function that gives it back.
func (l *connLimiter) acquire(userID string) (func(), error) {
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

	timer := time.NewTimer(l.wait)
	defer timer.Stop()
	select {
	case l.slots <- struct{}{}:
	case <-timer.C:
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
