// Copyright (c) 2026-present Antimatter contributors.
// See LICENSE.txt for license information.

package main

import (
	"sync"
	"time"

	"github.com/pkg/errors"
)

const (
	// watchIdleTimeout is how long a user's inbox is watched after their last request. The
	// webapp asks for the unread count every few minutes while it's open.
	watchIdleTimeout = 15 * time.Minute
	// maxWatchedUsers bounds the number of inboxes watched at once.
	maxWatchedUsers = 2000
	// maxConcurrentChecks bounds the number of inboxes checked at once.
	maxConcurrentChecks = 10
	// maxCheckBackoff is the longest wait between the checks of an inbox that fail.
	maxCheckBackoff = time.Hour
	// watchTick is how often the watcher looks for inboxes to check.
	watchTick = 15 * time.Second
	// maxIdleWatches bounds the number of inboxes watched with IMAP IDLE at once, each of which
	// keeps a connection open. It leaves most connection slots to requests.
	maxIdleWatches = maxConnections / 2
	// idleRetryAfter is how long to poll an inbox whose IDLE connection failed before trying IDLE
	// again; it doubles with each failure.
	idleRetryAfter = time.Minute
	// noIdleRecheck is how long to poll the inbox of a server without IDLE before checking again
	// whether it supports it.
	noIdleRecheck = 6 * time.Hour
)

var (
	// errIdleUnsupported is returned by idle functions when the server doesn't support IDLE.
	errIdleUnsupported = errors.New("the server doesn't support IDLE")
	// errIdleNoSlot is returned by idle functions when no connection slot is free.
	errIdleNoSlot = errors.New("no connection slot for IDLE")
)

// idleFunc watches a user's inbox with IMAP IDLE until stop is closed, calling report with its
// unread count at first and whenever it may have changed. It returns errIdleUnsupported if the
// server doesn't support IDLE.
type idleFunc func(userID string, stop <-chan struct{}, report func(unread int)) error

// watchedInbox is the state of the inbox of an online user.
type watchedInbox struct {
	lastSeen  time.Time
	lastCheck time.Time
	unread    int
	known     bool
	failures  int
	checking  bool

	// stopIdle stops the IDLE connection watching the inbox, if there's one.
	stopIdle chan struct{}
	// idleFailures counts the IDLE connections that failed in a row; IDLE is tried again at
	// idleRetry.
	idleFailures int
	idleRetry    time.Time
}

// unreadWatcher keeps the unread counts of the inboxes of online users up to date, telling users
// when their count changes. It watches inboxes with IMAP IDLE where the server supports it and a
// connection slot is free, and checks the others periodically.
type unreadWatcher struct {
	mu      sync.Mutex
	inboxes map[string]*watchedInbox
	running chan struct{}
	idlers  int

	// check returns the unread count of a user's inbox.
	check func(userID string) (int, error)
	// idle watches a user's inbox with IDLE; nil to only poll.
	idle idleFunc
	// publish tells a user their unread count changed.
	publish func(userID string, unread int)
	// interval returns the time between two checks of an inbox.
	interval func() time.Duration
	now      func() time.Time
}

func newUnreadWatcher(check func(string) (int, error), publish func(string, int), interval func() time.Duration) *unreadWatcher {
	return &unreadWatcher{
		inboxes:  map[string]*watchedInbox{},
		running:  make(chan struct{}, maxConcurrentChecks),
		check:    check,
		publish:  publish,
		interval: interval,
		now:      time.Now,
	}
}

// Touch records that a user is online, starting to watch their inbox.
func (w *unreadWatcher) Touch(userID string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if inbox, ok := w.inboxes[userID]; ok {
		inbox.lastSeen = w.now()
		return
	}
	if len(w.inboxes) < maxWatchedUsers {
		w.inboxes[userID] = &watchedInbox{lastSeen: w.now()}
	}
}

// Forget stops watching a user's inbox, e.g. when they disconnect their account.
func (w *unreadWatcher) Forget(userID string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if inbox, ok := w.inboxes[userID]; ok {
		inbox.stopIdling()
		delete(w.inboxes, userID)
	}
}

func (inbox *watchedInbox) stopIdling() {
	if inbox.stopIdle != nil {
		close(inbox.stopIdle)
		inbox.stopIdle = nil
	}
}

// Cached returns the last known unread count of a user's inbox, if it's recent enough or watched
// with IDLE.
func (w *unreadWatcher) Cached(userID string) (int, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	inbox, ok := w.inboxes[userID]
	if !ok || !inbox.known || inbox.stopIdle == nil && w.now().Sub(inbox.lastCheck) > w.interval() {
		return 0, false
	}
	return inbox.unread, true
}

// Record stores an unread count known from elsewhere (such as a request), telling the user if it
// changed.
func (w *unreadWatcher) Record(userID string, unread int) {
	w.mu.Lock()
	inbox, ok := w.inboxes[userID]
	if !ok {
		w.mu.Unlock()
		return
	}
	changed := inbox.known && inbox.unread != unread
	inbox.unread, inbox.known, inbox.lastCheck, inbox.failures = unread, true, w.now(), 0
	w.mu.Unlock()

	if changed {
		w.publish(userID, unread)
	}
}

func (w *unreadWatcher) backoff(failures int) time.Duration {
	return doubled(w.interval(), failures, maxCheckBackoff)
}

// doubled returns wait doubled n times, up to limit.
func doubled(wait time.Duration, n int, limit time.Duration) time.Duration {
	for range n {
		wait *= 2
		if wait >= limit {
			return limit
		}
	}
	return wait
}

// tick drops the inboxes of users that went offline, and checks the inboxes that are due. It
// returns the users whose inbox it started checking.
func (w *unreadWatcher) tick() []string {
	now := w.now()
	var due []string

	w.mu.Lock()
	for userID, inbox := range w.inboxes {
		if now.Sub(inbox.lastSeen) > watchIdleTimeout {
			inbox.stopIdling()
			delete(w.inboxes, userID)
			continue
		}
		if inbox.stopIdle != nil {
			continue
		}
		if w.idle != nil && !inbox.checking && w.idlers < maxIdleWatches && !now.Before(inbox.idleRetry) {
			inbox.stopIdle = make(chan struct{})
			w.idlers++
			go w.idleInbox(userID, inbox.stopIdle)
			continue
		}
		if inbox.checking || now.Sub(inbox.lastCheck) < w.backoff(inbox.failures) {
			continue
		}
		select {
		case w.running <- struct{}{}:
			inbox.checking = true
			due = append(due, userID)
		default:
			// Enough checks running, the others wait for the next tick
		}
	}
	w.mu.Unlock()

	for _, userID := range due {
		go w.checkInbox(userID)
	}
	return due
}

func (w *unreadWatcher) checkInbox(userID string) {
	defer func() { <-w.running }()
	unread, err := w.check(userID)

	w.mu.Lock()
	inbox, ok := w.inboxes[userID]
	if !ok {
		w.mu.Unlock()
		return
	}
	inbox.checking = false
	inbox.lastCheck = w.now()
	if err != nil {
		inbox.failures++
		w.mu.Unlock()
		return
	}
	changed := !inbox.known || inbox.unread != unread
	inbox.unread, inbox.known, inbox.failures = unread, true, 0
	w.mu.Unlock()

	if changed {
		w.publish(userID, unread)
	}
}

// idleInbox watches a user's inbox with IDLE until stop is closed or IDLE fails, after which the
// inbox is checked periodically again.
func (w *unreadWatcher) idleInbox(userID string, stop chan struct{}) {
	err := w.idle(userID, stop, func(unread int) { w.update(userID, stop, unread) })

	w.mu.Lock()
	w.idlers--
	inbox, ok := w.inboxes[userID]
	if !ok || inbox.stopIdle != stop {
		// Stopped
		w.mu.Unlock()
		return
	}
	inbox.stopIdle = nil
	now := w.now()
	switch {
	case errors.Is(err, errIdleUnsupported):
		inbox.idleRetry = now.Add(noIdleRecheck)
	case err != nil:
		inbox.idleFailures++
		inbox.idleRetry = now.Add(doubled(idleRetryAfter, inbox.idleFailures-1, maxCheckBackoff))
	default:
		inbox.idleFailures = 0
	}
	w.mu.Unlock()
}

// update records the unread count reported by the IDLE connection of a user's inbox, telling the
// user if it changed.
func (w *unreadWatcher) update(userID string, stop chan struct{}, unread int) {
	w.mu.Lock()
	inbox, ok := w.inboxes[userID]
	if !ok || inbox.stopIdle != stop {
		w.mu.Unlock()
		return
	}
	changed := !inbox.known || inbox.unread != unread
	inbox.unread, inbox.known, inbox.lastCheck, inbox.failures, inbox.idleFailures = unread, true, w.now(), 0, 0
	w.mu.Unlock()

	if changed {
		w.publish(userID, unread)
	}
}

// Run checks inboxes until stop is closed.
func (w *unreadWatcher) Run(stop <-chan struct{}) {
	ticker := time.NewTicker(watchTick)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			w.stopAll()
			return
		case <-ticker.C:
			w.tick()
		}
	}
}

// stopAll stops the IDLE connections.
func (w *unreadWatcher) stopAll() {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, inbox := range w.inboxes {
		inbox.stopIdling()
	}
}
